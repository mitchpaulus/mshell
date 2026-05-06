package main

// Type-expression parser (Phase 10, step 1).
//
// Consumes a token stream and produces a TypeId. Used by the main parser at
// every site that contains a type expression: the right-hand side of
// `type X = ...` declarations, the operand of `<value> as <T>`, and (later)
// the input/output lists of `def` signatures.
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
//
// The dict-vs-shape decision is made on the first key inside `{...}`:
//
//   - Key parses as a type expression (primitive, list, dict, etc.) -> dict.
//     Exactly one key:value pair is allowed for dicts.
//   - Key is a LITERAL identifier followed by ':' -> shape with named fields.
//
// Generics (a bare identifier that is not a known type) are NOT yet
// supported — they error out as "unknown type". Generics land alongside
// `def` signature parsing in the next chunk; until then, simple `type X =
// ...` declarations and `as <T>` casts only need monomorphic type
// expressions.

// ParseTypeExpr parses a single type expression from `tokens` starting at
// index 0. It returns the resulting TypeId, the number of tokens consumed,
// and any errors encountered. On error, the returned TypeId is best-effort
// (often TidNothing) and consumed >= 0.
func ParseTypeExpr(c *Checker, tokens []Token) (TypeId, int, []TypeError) {
	p := &typeExprParser{tokens: tokens, c: c}
	id := p.parseUnion()
	return id, p.pos, p.errs
}

type typeExprParser struct {
	tokens []Token
	pos    int
	c      *Checker
	errs   []TypeError
}

func (p *typeExprParser) atEnd() bool {
	return p.pos >= len(p.tokens)
}

func (p *typeExprParser) peek() Token {
	if p.atEnd() {
		return Token{Type: EOF}
	}
	return p.tokens[p.pos]
}

func (p *typeExprParser) advance() Token {
	t := p.peek()
	if !p.atEnd() {
		p.pos++
	}
	return t
}

func (p *typeExprParser) errAt(tok Token, hint string) {
	p.errs = append(p.errs, TypeError{
		Kind: TErrTypeParse,
		Pos:  tok,
		Hint: hint,
	})
}

func (p *typeExprParser) expect(tt TokenType, what string) (Token, bool) {
	tok := p.peek()
	if tok.Type != tt {
		p.errAt(tok, "expected "+what)
		return tok, false
	}
	return p.advance(), true
}

// parseUnion is the top of the grammar. It parses one primary, then folds
// any `| primary` arms into a single union TypeId.
func (p *typeExprParser) parseUnion() TypeId {
	first := p.parsePrimary()
	if p.peek().Type != PIPE {
		return first
	}
	arms := []TypeId{first}
	for p.peek().Type == PIPE {
		p.advance()
		arms = append(arms, p.parsePrimary())
	}
	return p.c.arena.MakeUnion(arms, NameNone)
}

// parsePrimary handles every form except a top-level union.
func (p *typeExprParser) parsePrimary() TypeId {
	tok := p.peek()
	switch tok.Type {
	case TYPEINT:
		p.advance()
		return TidInt
	case TYPEFLOAT:
		p.advance()
		return TidFloat
	case TYPEBOOL:
		p.advance()
		return TidBool
	case STR:
		p.advance()
		return TidStr
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
	if !p.atEnd() {
		p.advance()
	}
	return TidNothing
}

// parseList consumes `[ typeExpr ]` and returns the list TypeId.
func (p *typeExprParser) parseList() TypeId {
	p.advance() // [
	elem := p.parseUnion()
	p.expect(RIGHT_SQUARE_BRACKET, "']'")
	return p.c.arena.MakeList(elem)
}

// parseDictOrShape consumes `{ ... }`. The first key decides which form:
// a primitive/composite key means dict (single pair); a LITERAL key means
// shape (one or more named fields).
func (p *typeExprParser) parseDictOrShape() TypeId {
	openTok := p.advance() // {
	if p.peek().Type == RIGHT_CURLY {
		// Empty record / shape.
		p.advance()
		return p.c.arena.MakeShape(nil)
	}
	// Decide dict vs shape by lookahead: shape iff key is LITERAL followed
	// by ':' AND the LITERAL is not itself a primitive type spelled as a
	// LITERAL (e.g. "bytes", "none"). Primitives spelled with dedicated
	// tokens (TYPEINT, etc.) always mean dict.
	first := p.peek()
	if first.Type == LITERAL && p.lookaheadColon() && !isPrimitiveLiteralType(first.Lexeme) {
		return p.parseShape(openTok)
	}
	// Dict path.
	keyType := p.parseUnion()
	p.expect(COLON, "':'")
	valType := p.parseUnion()
	if p.peek().Type == COMMA {
		p.errAt(p.peek(), "dict types take a single key:value pair; use a shape `{a: T, b: U}` for multiple fields")
		// Consume to a closing brace for recovery.
		for !p.atEnd() && p.peek().Type != RIGHT_CURLY {
			p.advance()
		}
	}
	p.expect(RIGHT_CURLY, "'}'")
	return p.c.arena.MakeDict(keyType, valType)
}

// parseShape parses one or more `name: T` entries. The opening `{` has
// already been consumed.
func (p *typeExprParser) parseShape(openTok Token) TypeId {
	_ = openTok
	var fields []ShapeField
	seen := map[NameId]bool{}
	for {
		nameTok, ok := p.expect(LITERAL, "field name")
		if !ok {
			break
		}
		p.expect(COLON, "':'")
		t := p.parseUnion()
		nameId := p.c.names.Intern(nameTok.Lexeme)
		if seen[nameId] {
			p.errAt(nameTok, "duplicate shape field '"+nameTok.Lexeme+"'")
		} else {
			seen[nameId] = true
			fields = append(fields, ShapeField{Name: nameId, Type: t})
		}
		if p.peek().Type == COMMA {
			p.advance()
			continue
		}
		break
	}
	p.expect(RIGHT_CURLY, "'}'")
	return p.c.arena.MakeShape(fields)
}

// parseQuote consumes `( in* -- out* )` and returns a quote TypeId.
func (p *typeExprParser) parseQuote() TypeId {
	p.advance() // (
	var inputs, outputs []TypeId
	for !p.atEnd() && p.peek().Type != DOUBLEDASH && p.peek().Type != RIGHT_PAREN {
		inputs = append(inputs, p.parseUnion())
	}
	if p.peek().Type == DOUBLEDASH {
		p.advance()
		for !p.atEnd() && p.peek().Type != RIGHT_PAREN {
			outputs = append(outputs, p.parseUnion())
		}
	} else {
		p.errAt(p.peek(), "expected '--' in quote signature")
	}
	p.expect(RIGHT_PAREN, "')'")
	return p.c.arena.MakeQuote(QuoteSig{Inputs: inputs, Outputs: outputs})
}

// parseNamed handles a LITERAL token: a built-in non-keyword type name
// (Maybe, Grid, GridView, GridRow, bytes, none) or a user-declared type
// from the type environment. `Maybe` is the only one that takes a
// bracketed type argument.
func (p *typeExprParser) parseNamed() TypeId {
	tok := p.advance()
	switch tok.Lexeme {
	case "bytes":
		return TidBytes
	case "none":
		return TidNone
	case "Grid":
		return p.c.arena.MakeGrid(0)
	case "GridView":
		return p.c.arena.MakeGridView(0)
	case "GridRow":
		return p.c.arena.MakeGridRow(0)
	case "Maybe":
		if p.peek().Type != LEFT_SQUARE_BRACKET {
			p.errAt(tok, "Maybe requires a type argument: Maybe[T]")
			return TidNothing
		}
		p.advance() // [
		inner := p.parseUnion()
		p.expect(RIGHT_SQUARE_BRACKET, "']'")
		return p.c.arena.MakeMaybe(inner)
	}
	// User-declared type lookup.
	if id := p.c.LookupType(tok.Lexeme); id != TidNothing {
		return id
	}
	p.errAt(tok, "unknown type '"+tok.Lexeme+"'")
	return TidNothing
}

// lookaheadColon reports whether the token at p.pos+1 is COLON. Used to
// distinguish shape entries from dict pair forms.
func (p *typeExprParser) lookaheadColon() bool {
	if p.pos+1 >= len(p.tokens) {
		return false
	}
	return p.tokens[p.pos+1].Type == COLON
}

// isPrimitiveLiteralType reports whether a LITERAL lexeme names a built-in
// type (the ones that aren't given dedicated tokens). When such a name
// appears as a key inside `{...}`, the form is a dict (the user is using
// the primitive as the dict's key type), not a shape with that field name —
// per the design, reserved type names cannot be field names.
func isPrimitiveLiteralType(lex string) bool {
	switch lex {
	case "bytes", "none", "Maybe", "Grid", "GridView", "GridRow":
		return true
	}
	return false
}
