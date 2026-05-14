package main

// Type-expression parsing.
//
// Type expressions only appear in three places: def signatures
// `(<inputs> -- <outputs>)`, `type X = <typeExpr>` declarations, and
// `<value> as <typeExpr>` casts. The parser knows when it has entered
// each of these contexts and calls into the productions below
// directly. Single-token lookahead drives every decision: PIPE means
// union, LEFT_PAREN means a quote signature, LEFT_CURLY means dict or
// shape (disambiguated by peeking past the first key), LEFT_SQUARE_BRACKET
// means a list, LITERAL means a named type (with `Maybe[T]` as the only
// built-in parametric form). No tokens are interpreted ambiguously.
//
// The resulting AST nodes are MShellParseItems, so the type checker can
// walk them through the same dispatch path as every other parse item;
// resolution to a TypeId happens lazily so forward references to user
// `type` declarations work in declaration order.

import (
	"fmt"
	"strings"
)

// Node types ---------------------------------------------------------------

// TypePrim is a primitive type keyword (int, float, bool, str).
type TypePrim struct {
	Tok Token
	Tid TypeId // resolved at parse time; primitives have no environment dependency
}

func (a *TypePrim) GetStartToken() Token { return a.Tok }
func (a *TypePrim) GetEndToken() Token   { return a.Tok }
func (a *TypePrim) ToJson() string       { return fmt.Sprintf("{\"type\": %q}", a.Tok.Lexeme) }
func (a *TypePrim) DebugString() string  { return a.Tok.Lexeme }

// TypeNamed is a named type reference (path, datetime, Grid, GridView,
// GridRow, bytes, none, Maybe[T], user `type X = ...` references, or
// implicit generics introduced by a def signature).
type TypeNamed struct {
	Tok  Token
	Name string
	Args []MShellParseItem // populated only for `Maybe[T]`-style application
}

func (a *TypeNamed) GetStartToken() Token { return a.Tok }
func (a *TypeNamed) GetEndToken() Token   { return a.Tok }
func (a *TypeNamed) ToJson() string {
	if len(a.Args) == 0 {
		return fmt.Sprintf("{\"named\": %q}", a.Name)
	}
	parts := make([]string, len(a.Args))
	for i, arg := range a.Args {
		parts[i] = arg.ToJson()
	}
	return fmt.Sprintf("{\"named\": %q, \"args\": [%s]}", a.Name, strings.Join(parts, ", "))
}
func (a *TypeNamed) DebugString() string {
	if len(a.Args) == 0 {
		return a.Name
	}
	parts := make([]string, len(a.Args))
	for i, arg := range a.Args {
		parts[i] = arg.DebugString()
	}
	return fmt.Sprintf("%s[%s]", a.Name, strings.Join(parts, ", "))
}

// TypeListExpr is a homogeneous list type `[T]`.
type TypeListExpr struct {
	OpenTok Token
	Elem    MShellParseItem
}

func (a *TypeListExpr) GetStartToken() Token { return a.OpenTok }
func (a *TypeListExpr) GetEndToken() Token   { return a.OpenTok }
func (a *TypeListExpr) ToJson() string       { return fmt.Sprintf("{\"list\": %s}", a.Elem.ToJson()) }
func (a *TypeListExpr) DebugString() string  { return "[" + a.Elem.DebugString() + "]" }

// TypeDictExpr is a single key:value pair dict type `{K: V}`.
type TypeDictExpr struct {
	OpenTok Token
	Key     MShellParseItem
	Value   MShellParseItem
}

func (a *TypeDictExpr) GetStartToken() Token { return a.OpenTok }
func (a *TypeDictExpr) GetEndToken() Token   { return a.OpenTok }
func (a *TypeDictExpr) ToJson() string {
	return fmt.Sprintf("{\"dict\": {\"key\": %s, \"value\": %s}}", a.Key.ToJson(), a.Value.ToJson())
}
func (a *TypeDictExpr) DebugString() string {
	return "{" + a.Key.DebugString() + ": " + a.Value.DebugString() + "}"
}

// TypeShapeField is one entry in a shape literal.
type TypeShapeField struct {
	Tok  Token
	Name string
	Type MShellParseItem
}

// TypeShapeExpr is a structural record `{a: T, b: U, ...}`. A
// non-nil Wildcard captures a trailing `*: T` entry: it's preserved
// for source-faithful printing, but width subtyping on shapes
// already covers the "extra keys allowed" semantics so the resolver
// drops it.
type TypeShapeExpr struct {
	OpenTok  Token
	Fields   []TypeShapeField
	Wildcard MShellParseItem
}

func (a *TypeShapeExpr) GetStartToken() Token { return a.OpenTok }
func (a *TypeShapeExpr) GetEndToken() Token   { return a.OpenTok }
func (a *TypeShapeExpr) ToJson() string {
	parts := make([]string, 0, len(a.Fields)+1)
	for _, f := range a.Fields {
		parts = append(parts, fmt.Sprintf("%q: %s", f.Name, f.Type.ToJson()))
	}
	if a.Wildcard != nil {
		parts = append(parts, fmt.Sprintf("%q: %s", "*", a.Wildcard.ToJson()))
	}
	return "{\"shape\": {" + strings.Join(parts, ", ") + "}}"
}
func (a *TypeShapeExpr) DebugString() string {
	parts := make([]string, 0, len(a.Fields)+1)
	for _, f := range a.Fields {
		parts = append(parts, f.Name+": "+f.Type.DebugString())
	}
	if a.Wildcard != nil {
		parts = append(parts, "*: "+a.Wildcard.DebugString())
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// TypeQuoteExpr is a quote signature `(in1 in2 -- out1 out2)`.
type TypeQuoteExpr struct {
	OpenTok Token
	Inputs  []MShellParseItem
	Outputs []MShellParseItem
}

func (a *TypeQuoteExpr) GetStartToken() Token { return a.OpenTok }
func (a *TypeQuoteExpr) GetEndToken() Token   { return a.OpenTok }
func (a *TypeQuoteExpr) ToJson() string {
	ins := make([]string, len(a.Inputs))
	for i, t := range a.Inputs {
		ins[i] = t.ToJson()
	}
	outs := make([]string, len(a.Outputs))
	for i, t := range a.Outputs {
		outs[i] = t.ToJson()
	}
	return fmt.Sprintf("{\"quote\": {\"in\": [%s], \"out\": [%s]}}",
		strings.Join(ins, ", "), strings.Join(outs, ", "))
}
func (a *TypeQuoteExpr) DebugString() string {
	ins := make([]string, len(a.Inputs))
	for i, t := range a.Inputs {
		ins[i] = t.DebugString()
	}
	outs := make([]string, len(a.Outputs))
	for i, t := range a.Outputs {
		outs[i] = t.DebugString()
	}
	return "(" + strings.Join(ins, " ") + " -- " + strings.Join(outs, " ") + ")"
}

// TypeUnionExpr is a union `A | B | C`.
type TypeUnionExpr struct {
	StartTok Token
	Arms     []MShellParseItem
}

func (a *TypeUnionExpr) GetStartToken() Token { return a.StartTok }
func (a *TypeUnionExpr) GetEndToken() Token   { return a.StartTok }
func (a *TypeUnionExpr) ToJson() string {
	parts := make([]string, len(a.Arms))
	for i, arm := range a.Arms {
		parts[i] = arm.ToJson()
	}
	return "{\"union\": [" + strings.Join(parts, ", ") + "]}"
}
func (a *TypeUnionExpr) DebugString() string {
	parts := make([]string, len(a.Arms))
	for i, arm := range a.Arms {
		parts[i] = arm.DebugString()
	}
	return strings.Join(parts, " | ")
}

// Parser productions -------------------------------------------------------
//
// All productions are methods on *MShellParser and consume tokens via
// parser.curr / parser.NextToken(), matching the rest of the parser's
// style. Errors are appended to parser-local slices and returned up; the
// productions never panic so the parser can recover and continue.
//
// The shape-vs-dict disambiguation needs one extra token of lookahead:
// after `{` and a LITERAL, we peek the next token to decide whether it's
// a shape field name (LITERAL ':') or a dict key type (the LITERAL was a
// named type used as the key). This is the only place in the type
// grammar that needs two-token lookahead, and it's implemented by
// staging the LITERAL and pulling one more token from the lexer rather
// than threading a buffer through every production.

// parseTypeExpr is the top-level entry point. It parses a union (which
// degrades to a single term when there's no `|`).
func (parser *MShellParser) parseTypeExpr() (MShellParseItem, []TypeError) {
	var errs []TypeError
	first := parser.parseTypePrimary(&errs)
	if parser.curr.Type != PIPE {
		return first, errs
	}
	start := first.GetStartToken()
	arms := []MShellParseItem{first}
	for parser.curr.Type == PIPE {
		parser.NextToken() // consume |
		arms = append(arms, parser.parseTypePrimary(&errs))
	}
	return &TypeUnionExpr{StartTok: start, Arms: arms}, errs
}

func (parser *MShellParser) parseTypePrimary(errs *[]TypeError) MShellParseItem {
	tok := parser.curr
	switch tok.Type {
	case TYPEINT:
		parser.NextToken()
		return &TypePrim{Tok: tok, Tid: TidInt}
	case TYPEFLOAT:
		parser.NextToken()
		return &TypePrim{Tok: tok, Tid: TidFloat}
	case TYPEBOOL:
		parser.NextToken()
		return &TypePrim{Tok: tok, Tid: TidBool}
	case STR:
		parser.NextToken()
		return &TypePrim{Tok: tok, Tid: TidStr}
	case LEFT_SQUARE_BRACKET:
		return parser.parseTypeList(errs)
	case LEFT_CURLY:
		return parser.parseTypeDictOrShape(errs)
	case LEFT_PAREN:
		return parser.parseTypeQuote(errs)
	case LITERAL:
		return parser.parseTypeNamed(errs)
	}
	*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: tok, Hint: "expected a type"})
	if tok.Type != EOF {
		parser.NextToken()
	}
	return &TypePrim{Tok: tok, Tid: TidNothing}
}

func (parser *MShellParser) parseTypeList(errs *[]TypeError) MShellParseItem {
	open := parser.curr
	parser.NextToken() // consume [
	elem, subErrs := parser.parseTypeExpr()
	*errs = append(*errs, subErrs...)
	if parser.curr.Type != RIGHT_SQUARE_BRACKET {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ']'"})
	} else {
		parser.NextToken()
	}
	return &TypeListExpr{OpenTok: open, Elem: elem}
}

// parseTypeDictOrShape handles every `{...}` form in a type context:
//
//   {}                  -> empty shape
//   {T}                 -> wildcard dict (sugar for {*: T} = Dict[str, T])
//   {K: V}              -> dict with explicit key type
//   {*: T}              -> wildcard dict, Dict[str, T]
//   {"a": T, "b": U}    -> shape (quoted keys)
//   {a: T, b: U}        -> shape (unquoted keys)
//   {"a": T, *: U}      -> shape with wildcard fallback (wildcard
//                          arm kept in AST but dropped by resolver
//                          since width subtyping already covers it)
//
// Disambiguation is single-token at the top of the body, with one
// extra peek inside the LITERAL branch (to tell a shape field name
// from a named type used as the dict key).
func (parser *MShellParser) parseTypeDictOrShape(errs *[]TypeError) MShellParseItem {
	open := parser.curr
	parser.NextToken() // consume {
	if parser.curr.Type == RIGHT_CURLY {
		parser.NextToken()
		return &TypeShapeExpr{OpenTok: open}
	}
	switch parser.curr.Type {
	case ASTERISK:
		return parser.parseWildcardDictBody(open, errs)
	case STRING, SINGLEQUOTESTRING:
		return parser.parseTypeShapeBody(open, errs)
	case LITERAL:
		if !isPrimitiveLiteralType(parser.curr.Lexeme) {
			// Disambiguate shape field name vs named type used as a
			// dict key by peeking one token past the LITERAL.
			lit := parser.curr
			next := parser.scanToken()
			if next.Type == COLON {
				parser.curr = next
				parser.NextToken() // consume :
				fieldType, subErrs := parser.parseTypeExpr()
				*errs = append(*errs, subErrs...)
				shape := &TypeShapeExpr{
					OpenTok: open,
					Fields:  []TypeShapeField{{Tok: lit, Name: lit.Lexeme, Type: fieldType}},
				}
				parser.continueTypeShapeBody(shape, errs)
				return shape
			}
			// LITERAL is being used as a key type or bare-value type.
			key := &TypeNamed{Tok: lit, Name: lit.Lexeme}
			parser.curr = next
			if lit.Lexeme == "Maybe" {
				parser.applyMaybeArgs(key, errs)
			}
			return parser.finishDictOrBareType(open, key, errs)
		}
	}
	keyType, subErrs := parser.parseTypeExpr()
	*errs = append(*errs, subErrs...)
	return parser.finishDictOrBareType(open, keyType, errs)
}

// finishDictOrBareType expects either `: V}` (regular dict) or just `}`
// (bare-type sugar `{T}` = `{*: T}` = Dict[str, T]).
func (parser *MShellParser) finishDictOrBareType(open Token, first MShellParseItem, errs *[]TypeError) MShellParseItem {
	if parser.curr.Type == RIGHT_CURLY {
		// Bare-type sugar: `{T}` -> wildcard dict keyed by str.
		parser.NextToken()
		return &TypeDictExpr{OpenTok: open, Key: synthStrKey(open), Value: first}
	}
	if parser.curr.Type != COLON {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ':' or '}'"})
	} else {
		parser.NextToken()
	}
	val, subErrs := parser.parseTypeExpr()
	*errs = append(*errs, subErrs...)
	// Allow a trailing comma before `}` for parity with shape syntax.
	if parser.curr.Type == COMMA {
		parser.NextToken()
	}
	if parser.curr.Type == COMMA {
		*errs = append(*errs, TypeError{
			Kind: TErrTypeParse, Pos: parser.curr,
			Hint: "dict types take a single key:value pair; use a shape `{a: T, b: U}` for multiple fields",
		})
		for parser.curr.Type != RIGHT_CURLY && parser.curr.Type != EOF {
			parser.NextToken()
		}
	}
	if parser.curr.Type != RIGHT_CURLY {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected '}'"})
	} else {
		parser.NextToken()
	}
	return &TypeDictExpr{OpenTok: open, Key: first, Value: val}
}

// parseWildcardDictBody parses `*: T` (with optional trailing comma)
// and the closing `}`. The `*` is the current token on entry.
func (parser *MShellParser) parseWildcardDictBody(open Token, errs *[]TypeError) MShellParseItem {
	parser.NextToken() // consume *
	if parser.curr.Type != COLON {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ':' after '*'"})
	} else {
		parser.NextToken()
	}
	val, subErrs := parser.parseTypeExpr()
	*errs = append(*errs, subErrs...)
	if parser.curr.Type == COMMA {
		parser.NextToken()
	}
	if parser.curr.Type != RIGHT_CURLY {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected '}'"})
	} else {
		parser.NextToken()
	}
	return &TypeDictExpr{OpenTok: open, Key: synthStrKey(open), Value: val}
}

// parseTypeShapeBody handles the case where the first key inside `{`
// is a quoted string. continueTypeShapeBody does the heavy lifting.
func (parser *MShellParser) parseTypeShapeBody(open Token, errs *[]TypeError) MShellParseItem {
	shape := &TypeShapeExpr{OpenTok: open}
	parser.continueTypeShapeBody(shape, errs)
	return shape
}

// continueTypeShapeBody parses zero or more shape entries followed by
// the closing `}`. The caller may have already pushed the first field.
// A `*: T` entry sets shape.Wildcard rather than appending a field.
// Trailing commas are allowed.
func (parser *MShellParser) continueTypeShapeBody(shape *TypeShapeExpr, errs *[]TypeError) {
	for {
		if parser.curr.Type == COMMA {
			parser.NextToken()
		} else if len(shape.Fields) > 0 || shape.Wildcard != nil {
			break
		}
		if parser.curr.Type == RIGHT_CURLY {
			break
		}
		if parser.curr.Type == ASTERISK {
			parser.NextToken() // consume *
			if parser.curr.Type != COLON {
				*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ':' after '*'"})
			} else {
				parser.NextToken()
			}
			wildType, subErrs := parser.parseTypeExpr()
			*errs = append(*errs, subErrs...)
			if shape.Wildcard != nil {
				*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "duplicate wildcard '*' in shape"})
			} else {
				shape.Wildcard = wildType
			}
			continue
		}
		nameTok := parser.curr
		var name string
		switch nameTok.Type {
		case LITERAL:
			name = nameTok.Lexeme
			parser.NextToken()
		case STRING:
			parsed, perr := ParseRawString(nameTok.Lexeme)
			if perr != nil {
				*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: nameTok, Hint: "invalid shape field name"})
				name = nameTok.Lexeme
			} else {
				name = parsed
			}
			parser.NextToken()
		case SINGLEQUOTESTRING:
			if len(nameTok.Lexeme) >= 2 {
				name = nameTok.Lexeme[1 : len(nameTok.Lexeme)-1]
			}
			parser.NextToken()
		default:
			*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: nameTok, Hint: "expected shape field name"})
			return
		}
		if parser.curr.Type != COLON {
			*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ':'"})
		} else {
			parser.NextToken()
		}
		fieldType, subErrs := parser.parseTypeExpr()
		*errs = append(*errs, subErrs...)
		duplicate := false
		for _, existing := range shape.Fields {
			if existing.Name == name {
				duplicate = true
				break
			}
		}
		if duplicate {
			*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: nameTok, Hint: "duplicate shape field '" + name + "'"})
		} else {
			shape.Fields = append(shape.Fields, TypeShapeField{Tok: nameTok, Name: name, Type: fieldType})
		}
	}
	if parser.curr.Type != RIGHT_CURLY {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected '}'"})
	} else {
		parser.NextToken()
	}
}

// synthStrKey returns a synthetic `str` TypePrim positioned at the
// `{` opener — used as the key of bare-type sugar `{T}` and pure
// wildcard `{*: T}`, both of which resolve to `Dict[str, T]`.
func synthStrKey(open Token) MShellParseItem {
	tok := open
	tok.Type = STR
	tok.Lexeme = "str"
	return &TypePrim{Tok: tok, Tid: TidStr}
}

func (parser *MShellParser) parseTypeQuote(errs *[]TypeError) MShellParseItem {
	open := parser.curr
	parser.NextToken() // consume (
	var inputs, outputs []MShellParseItem
	for parser.curr.Type != DOUBLEDASH && parser.curr.Type != RIGHT_PAREN && parser.curr.Type != EOF {
		item, subErrs := parser.parseTypeExpr()
		*errs = append(*errs, subErrs...)
		inputs = append(inputs, item)
	}
	if parser.curr.Type == DOUBLEDASH {
		parser.NextToken() // consume --
		for parser.curr.Type != RIGHT_PAREN && parser.curr.Type != EOF {
			item, subErrs := parser.parseTypeExpr()
			*errs = append(*errs, subErrs...)
			outputs = append(outputs, item)
		}
	} else {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected '--' in quote signature"})
	}
	if parser.curr.Type != RIGHT_PAREN {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ')'"})
	} else {
		parser.NextToken()
	}
	return &TypeQuoteExpr{OpenTok: open, Inputs: inputs, Outputs: outputs}
}

func (parser *MShellParser) parseTypeNamed(errs *[]TypeError) MShellParseItem {
	tok := parser.curr
	parser.NextToken()
	node := &TypeNamed{Tok: tok, Name: tok.Lexeme}
	if tok.Lexeme == "Maybe" {
		parser.applyMaybeArgs(node, errs)
	}
	return node
}

func (parser *MShellParser) applyMaybeArgs(node *TypeNamed, errs *[]TypeError) {
	if parser.curr.Type != LEFT_SQUARE_BRACKET {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: node.Tok, Hint: "Maybe requires a type argument: Maybe[T]"})
		return
	}
	parser.NextToken() // consume [
	inner, subErrs := parser.parseTypeExpr()
	*errs = append(*errs, subErrs...)
	if parser.curr.Type != RIGHT_SQUARE_BRACKET {
		*errs = append(*errs, TypeError{Kind: TErrTypeParse, Pos: parser.curr, Hint: "expected ']'"})
	} else {
		parser.NextToken()
	}
	node.Args = []MShellParseItem{inner}
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

// parseDefSignature consumes a def's `( <inputs> -- <outputs> )` block.
// The current token must be the opening LEFT_PAREN on entry; on return
// parser.curr is positioned past the closing RIGHT_PAREN.
func (parser *MShellParser) parseDefSignature() ([]MShellParseItem, []MShellParseItem, error) {
	if err := parser.MatchWithMessage(parser.curr, LEFT_PAREN, "Expected '(' to start type definition."); err != nil {
		return nil, nil, err
	}
	var errs []TypeError
	var inputs, outputs []MShellParseItem
	for parser.curr.Type != DOUBLEDASH && parser.curr.Type != RIGHT_PAREN && parser.curr.Type != EOF {
		item, subErrs := parser.parseTypeExpr()
		errs = append(errs, subErrs...)
		inputs = append(inputs, item)
	}
	if err := parser.Match(parser.curr, DOUBLEDASH); err != nil {
		return nil, nil, err
	}
	for parser.curr.Type != RIGHT_PAREN && parser.curr.Type != EOF {
		item, subErrs := parser.parseTypeExpr()
		errs = append(errs, subErrs...)
		outputs = append(outputs, item)
	}
	if err := parser.Match(parser.curr, RIGHT_PAREN); err != nil {
		return nil, nil, err
	}
	if len(errs) > 0 {
		return nil, nil, fmt.Errorf("def signature: %s", joinTypeErrs(errs))
	}
	return inputs, outputs, nil
}

// Resolution --------------------------------------------------------------

// typeResolveCtx is the per-call scope used while resolving a type AST to
// a TypeId. It carries the per-def map of generic names so two occurrences
// of `a` in one signature share a TypeVarId. A nil context (top-level
// `type X = ...` resolution, or `as T` casts) flags any unknown LITERAL
// as an unknown-type error rather than implicitly generalizing.
type typeResolveCtx struct {
	generics map[string]TypeVarId
	next     uint32
}

func (c *Checker) resolveTypeExpr(node MShellParseItem, ctx *typeResolveCtx) TypeId {
	switch n := node.(type) {
	case *TypePrim:
		return n.Tid
	case *TypeListExpr:
		return c.arena.MakeList(c.resolveTypeExpr(n.Elem, ctx))
	case *TypeDictExpr:
		return c.arena.MakeDict(c.resolveTypeExpr(n.Key, ctx), c.resolveTypeExpr(n.Value, ctx))
	case *TypeShapeExpr:
		fields := make([]ShapeField, 0, len(n.Fields))
		for _, f := range n.Fields {
			fields = append(fields, ShapeField{
				Name: c.names.Intern(f.Name),
				Type: c.resolveTypeExpr(f.Type, ctx),
			})
		}
		return c.arena.MakeShape(fields)
	case *TypeQuoteExpr:
		ins := make([]TypeId, 0, len(n.Inputs))
		for _, in := range n.Inputs {
			ins = append(ins, c.resolveTypeExpr(in, ctx))
		}
		outs := make([]TypeId, 0, len(n.Outputs))
		for _, out := range n.Outputs {
			outs = append(outs, c.resolveTypeExpr(out, ctx))
		}
		return c.arena.MakeQuote(QuoteSig{Inputs: ins, Outputs: outs})
	case *TypeUnionExpr:
		arms := make([]TypeId, 0, len(n.Arms))
		for _, a := range n.Arms {
			arms = append(arms, c.resolveTypeExpr(a, ctx))
		}
		return c.arena.MakeUnion(arms, NameNone)
	case *TypeNamed:
		switch n.Name {
		case "bytes":
			return TidBytes
		case "none":
			return TidNone
		case "path":
			return TidPath
		case "datetime":
			return TidDateTime
		case "Grid":
			return c.arena.MakeGrid(0)
		case "GridView":
			return c.arena.MakeGridView(0)
		case "GridRow":
			return c.arena.MakeGridRow(0)
		case "Maybe":
			if len(n.Args) != 1 {
				return TidNothing
			}
			return c.arena.MakeMaybe(c.resolveTypeExpr(n.Args[0], ctx))
		}
		if id := c.LookupType(n.Name); id != TidNothing {
			return id
		}
		if ctx != nil {
			if id, ok := ctx.generics[n.Name]; ok {
				return c.arena.MakeVar(id)
			}
			id := TypeVarId(ctx.next)
			ctx.next++
			if ctx.generics == nil {
				ctx.generics = map[string]TypeVarId{}
			}
			ctx.generics[n.Name] = id
			return c.arena.MakeVar(id)
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrTypeParse, Pos: n.Tok,
			Hint: "unknown type '" + n.Name + "'",
		})
		return TidNothing
	}
	return TidNothing
}

// resolveSigItems resolves a list of type AST nodes against a shared
// generic-scope context and returns the parallel TypeId slice.
func (c *Checker) resolveSigItems(items []MShellParseItem, ctx *typeResolveCtx) []TypeId {
	out := make([]TypeId, 0, len(items))
	for _, it := range items {
		out = append(out, c.resolveTypeExpr(it, ctx))
	}
	return out
}

// ResolveDefSig resolves a def signature (inputs / outputs in AST form)
// into a fully-baked QuoteSig with fresh TypeVarIds. Generics are scoped
// to this def: identical names across inputs and outputs share a var;
// across distinct calls each gets its own ctx.
func (c *Checker) ResolveDefSig(inputs, outputs []MShellParseItem) QuoteSig {
	ctx := &typeResolveCtx{}
	ins := c.resolveSigItems(inputs, ctx)
	outs := c.resolveSigItems(outputs, ctx)
	gens := make([]TypeVarId, 0, len(ctx.generics))
	for _, v := range ctx.generics {
		gens = append(gens, v)
	}
	return QuoteSig{Inputs: ins, Outputs: outs, Generics: gens}
}
