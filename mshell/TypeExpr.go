package main

// Type-expression parser (Phase 10).
//
// Two-stage design:
//
//  1. ParseTypeExprAST consumes a token stream and produces a TypeExprAST —
//     a stateless parse tree. No arena / Checker dependency. This is what
//     the main parser invokes when it encounters `type X = ...` or
//     `<value> as <T>`, so the parser stays decoupled from the type system
//     and forward references work naturally (the AST is resolved later).
//
//  2. ResolveTypeExprAST walks the AST and builds a TypeId via the
//     Checker's arena, looking up named types in the type environment.
//
// ParseTypeExpr (the slice-input convenience entry point) chains the two:
// parse to AST, then resolve. Existing tests use this form. New parser
// integration uses ParseTypeExprAST directly off the streaming token
// source and stores the AST on the parse tree.
//
// Grammar:
//
//   typeExpr := union
//   union    := primary ( '|' primary )*
//   primary  := '(' sig ')'
//             | '[' typeExpr ']'
//             | '{' entry ( ',' entry )* '}'
//             | named
//             | TYPEINT | TYPEFLOAT | TYPEBOOL | STR
//   sig      := typeExpr* '--' typeExpr*
//   entry    := key ':' typeExpr
//   named    := LITERAL ( '[' typeExpr ']' )?

// TokenSource is the abstraction over a token stream. The slice adapter
// powers the convenience API and unit tests; the streaming parser
// implements its own adapter so it can drive ParseTypeExprAST directly.
type TokenSource interface {
	Peek() Token
	Advance() Token
}

// SliceTokenSource adapts a []Token to TokenSource.
type SliceTokenSource struct {
	Tokens []Token
	Pos    int
}

func (s *SliceTokenSource) Peek() Token {
	if s.Pos >= len(s.Tokens) {
		return Token{Type: EOF}
	}
	return s.Tokens[s.Pos]
}

func (s *SliceTokenSource) Advance() Token {
	t := s.Peek()
	if s.Pos < len(s.Tokens) {
		s.Pos++
	}
	return t
}

// TypeExprAST is a parsed-but-unresolved type expression. Implementations
// hold the source token (for error reporting) and any sub-expressions.
type TypeExprAST interface {
	StartToken() Token
}

type TypePrimAST struct {
	Tok Token
	Tid TypeId // for primitives whose TypeId is known at lex time
}

func (a *TypePrimAST) StartToken() Token { return a.Tok }

type TypeNamedAST struct {
	Tok  Token  // the LITERAL token
	Name string // lexeme
	Args []TypeExprAST
}

func (a *TypeNamedAST) StartToken() Token { return a.Tok }

type TypeListAST struct {
	Tok  Token
	Elem TypeExprAST
}

func (a *TypeListAST) StartToken() Token { return a.Tok }

type TypeDictAST struct {
	Tok Token
	K   TypeExprAST
	V   TypeExprAST
}

func (a *TypeDictAST) StartToken() Token { return a.Tok }

type ShapeFieldAST struct {
	Tok  Token
	Name string
	Type TypeExprAST
}

type TypeShapeAST struct {
	Tok    Token
	Fields []ShapeFieldAST
}

func (a *TypeShapeAST) StartToken() Token { return a.Tok }

type TypeQuoteAST struct {
	Tok     Token
	Inputs  []TypeExprAST
	Outputs []TypeExprAST
}

func (a *TypeQuoteAST) StartToken() Token { return a.Tok }

type TypeUnionAST struct {
	Tok  Token
	Arms []TypeExprAST
}

func (a *TypeUnionAST) StartToken() Token { return a.Tok }

// astParser is the recursive-descent driver over a TokenSource.
type astParser struct {
	src  TokenSource
	errs []TypeError
}

func (p *astParser) errAt(tok Token, hint string) {
	p.errs = append(p.errs, TypeError{Kind: TErrTypeParse, Pos: tok, Hint: hint})
}

func (p *astParser) expect(tt TokenType, what string) (Token, bool) {
	tok := p.src.Peek()
	if tok.Type != tt {
		p.errAt(tok, "expected "+what)
		return tok, false
	}
	return p.src.Advance(), true
}

func (p *astParser) parseUnion() TypeExprAST {
	first := p.parsePrimary()
	if p.src.Peek().Type != PIPE {
		return first
	}
	startTok := first.StartToken()
	arms := []TypeExprAST{first}
	for p.src.Peek().Type == PIPE {
		p.src.Advance()
		arms = append(arms, p.parsePrimary())
	}
	return &TypeUnionAST{Tok: startTok, Arms: arms}
}

func (p *astParser) parsePrimary() TypeExprAST {
	tok := p.src.Peek()
	switch tok.Type {
	case TYPEINT:
		p.src.Advance()
		return &TypePrimAST{Tok: tok, Tid: TidInt}
	case TYPEFLOAT:
		p.src.Advance()
		return &TypePrimAST{Tok: tok, Tid: TidFloat}
	case TYPEBOOL:
		p.src.Advance()
		return &TypePrimAST{Tok: tok, Tid: TidBool}
	case STR:
		p.src.Advance()
		return &TypePrimAST{Tok: tok, Tid: TidStr}
	case LEFT_SQUARE_BRACKET:
		return p.parseList()
	case LEFT_CURLY:
		return p.parseDictOrShape()
	case LEFT_PAREN:
		return p.parseQuote()
	case LITERAL:
		return p.parseNamed()
	}
	p.errAt(tok, "expected a type")
	if tok.Type != EOF {
		p.src.Advance()
	}
	return &TypePrimAST{Tok: tok, Tid: TidNothing}
}

func (p *astParser) parseList() TypeExprAST {
	open := p.src.Advance() // [
	elem := p.parseUnion()
	p.expect(RIGHT_SQUARE_BRACKET, "']'")
	return &TypeListAST{Tok: open, Elem: elem}
}

func (p *astParser) parseDictOrShape() TypeExprAST {
	open := p.src.Advance() // {
	if p.src.Peek().Type == RIGHT_CURLY {
		p.src.Advance()
		return &TypeShapeAST{Tok: open}
	}
	first := p.src.Peek()
	if first.Type == LITERAL && p.peekIsColonAfterLiteral() && !isPrimitiveLiteralType(first.Lexeme) {
		return p.parseShape(open)
	}
	keyType := p.parseUnion()
	p.expect(COLON, "':'")
	valType := p.parseUnion()
	if p.src.Peek().Type == COMMA {
		p.errAt(p.src.Peek(), "dict types take a single key:value pair; use a shape `{a: T, b: U}` for multiple fields")
		for {
			t := p.src.Peek()
			if t.Type == RIGHT_CURLY || t.Type == EOF {
				break
			}
			p.src.Advance()
		}
	}
	p.expect(RIGHT_CURLY, "'}'")
	return &TypeDictAST{Tok: open, K: keyType, V: valType}
}

func (p *astParser) parseShape(openTok Token) TypeExprAST {
	var fields []ShapeFieldAST
	seen := map[string]bool{}
	for {
		nameTok, ok := p.expect(LITERAL, "field name")
		if !ok {
			break
		}
		p.expect(COLON, "':'")
		t := p.parseUnion()
		if seen[nameTok.Lexeme] {
			p.errAt(nameTok, "duplicate shape field '"+nameTok.Lexeme+"'")
		} else {
			seen[nameTok.Lexeme] = true
			fields = append(fields, ShapeFieldAST{Tok: nameTok, Name: nameTok.Lexeme, Type: t})
		}
		if p.src.Peek().Type == COMMA {
			p.src.Advance()
			continue
		}
		break
	}
	p.expect(RIGHT_CURLY, "'}'")
	return &TypeShapeAST{Tok: openTok, Fields: fields}
}

func (p *astParser) parseQuote() TypeExprAST {
	open := p.src.Advance() // (
	var inputs, outputs []TypeExprAST
	for {
		t := p.src.Peek()
		if t.Type == DOUBLEDASH || t.Type == RIGHT_PAREN || t.Type == EOF {
			break
		}
		inputs = append(inputs, p.parseUnion())
	}
	if p.src.Peek().Type == DOUBLEDASH {
		p.src.Advance()
		for {
			t := p.src.Peek()
			if t.Type == RIGHT_PAREN || t.Type == EOF {
				break
			}
			outputs = append(outputs, p.parseUnion())
		}
	} else {
		p.errAt(p.src.Peek(), "expected '--' in quote signature")
	}
	p.expect(RIGHT_PAREN, "')'")
	return &TypeQuoteAST{Tok: open, Inputs: inputs, Outputs: outputs}
}

func (p *astParser) parseNamed() TypeExprAST {
	tok := p.src.Advance()
	node := &TypeNamedAST{Tok: tok, Name: tok.Lexeme}
	// Maybe is the only built-in named form that takes a bracketed type
	// argument. User-declared types might (in the future) take args; for
	// now we only accept `[T]` after Maybe.
	if tok.Lexeme == "Maybe" {
		if p.src.Peek().Type != LEFT_SQUARE_BRACKET {
			p.errAt(tok, "Maybe requires a type argument: Maybe[T]")
			return node
		}
		p.src.Advance() // [
		inner := p.parseUnion()
		p.expect(RIGHT_SQUARE_BRACKET, "']'")
		node.Args = []TypeExprAST{inner}
	}
	return node
}

// peekIsColonAfterLiteral reports whether the token after the current
// LITERAL is COLON. The TokenSource interface only exposes single-token
// peek, so this is awkward — we work around it by peeking one token, then
// looking inside if the source is a SliceTokenSource. For the streaming
// parser, we instead disambiguate by attempting a lookahead via peek-after-
// consume. To keep the implementation simple and portable across both
// sources, we use a small wrapper: SliceTokenSource has direct access; the
// streaming parser overrides this through a method on its adapter.
//
// To avoid tying the AST parser to a specific source, we ask the source
// directly via a type assertion. Sources that don't support two-token
// lookahead default to "false", which means an ambiguous shape-vs-dict
// case at the streaming parser is parsed as a dict by default. The main
// parser's adapter implements two-token peek to keep the behavior
// consistent.
func (p *astParser) peekIsColonAfterLiteral() bool {
	if la, ok := p.src.(twoTokenPeeker); ok {
		return la.PeekAt(1).Type == COLON
	}
	return false
}

// twoTokenPeeker is an optional capability on TokenSource: peek N tokens
// ahead. SliceTokenSource and the streaming parser's adapter implement it.
type twoTokenPeeker interface {
	PeekAt(n int) Token
}

// PeekAt returns the token at offset n from the current position. Out of
// range returns an EOF token.
func (s *SliceTokenSource) PeekAt(n int) Token {
	idx := s.Pos + n
	if idx >= len(s.Tokens) || idx < 0 {
		return Token{Type: EOF}
	}
	return s.Tokens[idx]
}

// ParseTypeExprAST parses a single type expression off `src`. After return,
// `src` is positioned just past the consumed expression. Errors collect
// into the returned slice; on error, the AST may be partial.
func ParseTypeExprAST(src TokenSource) (TypeExprAST, []TypeError) {
	p := &astParser{src: src}
	ast := p.parseUnion()
	return ast, p.errs
}

// ResolveTypeExprAST converts a parsed AST into a TypeId via the Checker's
// arena, looking up named types in the type environment. Errors are
// appended to the Checker. Returns TidNothing on resolution failure (after
// emitting an error) so callers can still continue.
func ResolveTypeExprAST(c *Checker, ast TypeExprAST) TypeId {
	switch n := ast.(type) {
	case *TypePrimAST:
		return n.Tid
	case *TypeListAST:
		return c.arena.MakeList(ResolveTypeExprAST(c, n.Elem))
	case *TypeDictAST:
		return c.arena.MakeDict(ResolveTypeExprAST(c, n.K), ResolveTypeExprAST(c, n.V))
	case *TypeShapeAST:
		fields := make([]ShapeField, 0, len(n.Fields))
		for _, f := range n.Fields {
			fields = append(fields, ShapeField{
				Name: c.names.Intern(f.Name),
				Type: ResolveTypeExprAST(c, f.Type),
			})
		}
		return c.arena.MakeShape(fields)
	case *TypeQuoteAST:
		ins := make([]TypeId, 0, len(n.Inputs))
		for _, in := range n.Inputs {
			ins = append(ins, ResolveTypeExprAST(c, in))
		}
		outs := make([]TypeId, 0, len(n.Outputs))
		for _, out := range n.Outputs {
			outs = append(outs, ResolveTypeExprAST(c, out))
		}
		return c.arena.MakeQuote(QuoteSig{Inputs: ins, Outputs: outs})
	case *TypeUnionAST:
		arms := make([]TypeId, 0, len(n.Arms))
		for _, a := range n.Arms {
			arms = append(arms, ResolveTypeExprAST(c, a))
		}
		return c.arena.MakeUnion(arms, NameNone)
	case *TypeNamedAST:
		switch n.Name {
		case "bytes":
			return TidBytes
		case "none":
			return TidNone
		case "Grid":
			return c.arena.MakeGrid(0)
		case "GridView":
			return c.arena.MakeGridView(0)
		case "GridRow":
			return c.arena.MakeGridRow(0)
		case "Maybe":
			// Missing arg is already reported at parse time; just degrade
			// gracefully here so callers still get a sensible TypeId chain.
			if len(n.Args) != 1 {
				return TidNothing
			}
			return c.arena.MakeMaybe(ResolveTypeExprAST(c, n.Args[0]))
		}
		if id := c.LookupType(n.Name); id != TidNothing {
			return id
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrTypeParse, Pos: n.Tok,
			Hint: "unknown type '" + n.Name + "'",
		})
		return TidNothing
	}
	return TidNothing
}

// ParseTypeExpr is the convenience entry point used by tests and callers
// that already have a token slice. Returns (TypeId, consumed, errors).
func ParseTypeExpr(c *Checker, tokens []Token) (TypeId, int, []TypeError) {
	src := &SliceTokenSource{Tokens: tokens}
	ast, errs := ParseTypeExprAST(src)
	preLen := len(c.errors)
	id := ResolveTypeExprAST(c, ast)
	// Surface resolution-time errors back to caller (and remove them from
	// the checker's accumulator so this entry point stays self-contained).
	if len(c.errors) > preLen {
		errs = append(errs, c.errors[preLen:]...)
		c.errors = c.errors[:preLen]
	}
	return id, src.Pos, errs
}

// isPrimitiveLiteralType reports whether a LITERAL lexeme names a built-in
// type. When such a name appears as a key inside `{...}`, the form is a
// dict (the user is using the primitive as the dict's key type), not a
// shape with that field name.
func isPrimitiveLiteralType(lex string) bool {
	switch lex {
	case "bytes", "none", "Maybe", "Grid", "GridView", "GridRow":
		return true
	}
	return false
}
