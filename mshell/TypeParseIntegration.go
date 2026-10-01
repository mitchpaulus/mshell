package main

// Parse-tree nodes that introduce a type expression at the program level:
//
//   - MShellTypeDecl: `type Name = <typeExpr>` top-level declaration.
//   - MShellAsCast:    `<value> as <typeExpr>` postfix cast.
//   - MShellTryAs:     `<value> tryAs <typeExpr>`, validation.
//   - MShellIsPattern: `is <typeExpr> name`, validation in a match arm.
//
// Each stores the parsed-but-unresolved type AST. Resolution to TypeIds
// happens when the checker walks the parse tree, so forward references
// to user-declared types work in declaration order. At evaluation time
// `as` and `type` are no-ops; `tryAs` and `is` resolve their type once
// and validate values against it (Validate.go).

import (
	"fmt"
	"strings"
)

// MShellTypeDecl is a top-level `type Name = <typeExpr>` declaration.
type MShellTypeDecl struct {
	Name      string
	NameToken Token
	File      *TokenFile // the file it is in, or nil
	StartTok  Token // the TYPE keyword
	Body      MShellParseItem
}

func (d *MShellTypeDecl) ToJson() string {
	return fmt.Sprintf("{\"kind\": \"typeDecl\", \"name\": %q}", d.Name)
}

func (d *MShellTypeDecl) DebugString() string {
	return fmt.Sprintf("type %s = ...", d.Name)
}

func (d *MShellTypeDecl) GetStartToken() Token { return d.StartTok }
func (d *MShellTypeDecl) GetEndToken() Token   { return d.NameToken }

// MShellEnumDecl is a top-level enum declaration:
//
//	enum Name = m1 | m2 T1 T2 | ... end
//	enum Name[a b] = m1 a | m2 [b] | ... end
//
// Each member is a name followed by its payload types; members are separated
// by `|` (a leading `|` is allowed) and `end` closes the declaration.
// MemberPayloads is parallel to Members, empty for a member with no payload.
type MShellEnumDecl struct {
	Name           string
	NameToken      Token
	StartTok       Token // the ENUM keyword
	File           *TokenFile // the file it is in, or nil
	Params         []Token
	Members        []string
	MemberToks     []Token
	MemberPayloads [][]MShellParseItem
	EndTok         Token
}

func (d *MShellEnumDecl) ToJson() string {
	parts := make([]string, len(d.Members))
	for i, m := range d.Members {
		parts[i] = fmt.Sprintf("%q", m)
	}
	return fmt.Sprintf("{\"kind\": \"enumDecl\", \"name\": %q, \"members\": [%s]}", d.Name, strings.Join(parts, ", "))
}

func (d *MShellEnumDecl) DebugString() string {
	return fmt.Sprintf("enum %s = %s end", d.Name, strings.Join(d.Members, " | "))
}

func (d *MShellEnumDecl) GetStartToken() Token { return d.StartTok }
func (d *MShellEnumDecl) GetEndToken() Token   { return d.EndTok }

// ParseEnumDecl parses an enum declaration. The ENUM keyword is the current
// token on entry; on return parser.curr is past the closing `end`. A member's
// payload types run until the next `|` or `end`, so the code after the
// declaration is never read as a payload.
func (parser *MShellParser) ParseEnumDecl() (*MShellEnumDecl, error) {
	startTok := parser.curr
	parser.NextToken() // consume ENUM
	if parser.curr.Type != LITERAL {
		return nil, fmt.Errorf("%d:%d: expected an enum name after 'enum', got %s",
			parser.curr.Line, parser.curr.Column, tokDesc(parser.curr))
	}
	nameTok := parser.curr
	parser.NextToken() // consume the name
	decl := &MShellEnumDecl{Name: nameTok.Lexeme, NameToken: nameTok, StartTok: startTok, File: parser.lexer.tokenFile}

	if parser.curr.Type == LEFT_SQUARE_BRACKET {
		open := parser.curr
		parser.NextToken() // consume [
		for parser.curr.Type == LITERAL {
			decl.Params = append(decl.Params, parser.curr)
			parser.NextToken()
		}
		if parser.curr.Type != RIGHT_SQUARE_BRACKET {
			return nil, fmt.Errorf("%d:%d: expected a parameter name or ']' in the parameters of enum '%s', got %s",
				parser.curr.Line, parser.curr.Column, decl.Name, tokDesc(parser.curr))
		}
		if len(decl.Params) == 0 {
			return nil, fmt.Errorf("%d:%d: enum '%s' has '[]' with no parameters; leave the brackets out",
				open.Line, open.Column, decl.Name)
		}
		parser.NextToken() // consume ]
	}

	if parser.curr.Type != EQUALS {
		return nil, fmt.Errorf("%d:%d: expected '=' in enum declaration '%s', got %s",
			parser.curr.Line, parser.curr.Column, decl.Name, tokDesc(parser.curr))
	}
	parser.NextToken() // consume =
	if parser.curr.Type == PIPE {
		parser.NextToken() // an optional leading |
	}

	var errs []TypeError
	for {
		if parser.curr.Type != LITERAL || parser.curr.Lexeme == "_" {
			return nil, fmt.Errorf("%d:%d: expected a member name in enum '%s', got %s. An enum is written `enum Name = m1 | m2 T ... end`",
				parser.curr.Line, parser.curr.Column, decl.Name, tokDesc(parser.curr))
		}
		memberTok := parser.curr
		decl.Members = append(decl.Members, memberTok.Lexeme)
		decl.MemberToks = append(decl.MemberToks, memberTok)
		parser.NextToken() // consume the member name

		var payloads []MShellParseItem
		for parser.curr.Type != PIPE && parser.curr.Type != END && parser.curr.Type != EOF {
			payloads = append(payloads, parser.parseTypePrimary(&errs))
		}
		decl.MemberPayloads = append(decl.MemberPayloads, payloads)

		if parser.curr.Type == PIPE {
			parser.NextToken() // consume |
			continue
		}
		if parser.curr.Type == END {
			decl.EndTok = parser.curr
			parser.NextToken() // consume end
			break
		}
		return nil, fmt.Errorf("%d:%d: expected 'end' to close the enum declaration '%s'",
			parser.curr.Line, parser.curr.Column, decl.Name)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("enum declaration '%s': %s", decl.Name, joinTypeErrs(errs))
	}
	return decl, nil
}

// MShellAsCast is a `<value> as <typeExpr>` postfix cast.
type MShellAsCast struct {
	AsToken Token
	Target  MShellParseItem
}

func (c *MShellAsCast) ToJson() string {
	return "{\"kind\": \"asCast\"}"
}

func (c *MShellAsCast) DebugString() string {
	return "as <type>"
}

func (c *MShellAsCast) GetStartToken() Token { return c.AsToken }
func (c *MShellAsCast) GetEndToken() Token   { return c.AsToken }

// MShellTryAs is `<value> tryAs <typeExpr>`: it validates the value against
// the type in place and pushes `just` the same value, or `none`
// (ai/type-core-calculus.typ, "Validation: tryAs and is").
type MShellTryAs struct {
	Tok    Token
	Target MShellParseItem
	// The target resolved by the runtime, once (Validate.go).
	resolved runtimeTarget
}

func (t *MShellTryAs) ToJson() string {
	return fmt.Sprintf("{\"kind\": \"tryAs\", \"type\": %s}", t.Target.ToJson())
}

func (t *MShellTryAs) DebugString() string {
	return "tryAs " + t.Target.DebugString()
}

func (t *MShellTryAs) GetStartToken() Token { return t.Tok }
func (t *MShellTryAs) GetEndToken() Token   { return t.Target.GetEndToken() }

// ParseTryAs handles a postfix `tryAs <typeExpr>`. The TRYAS keyword is the
// current token on entry.
func (parser *MShellParser) ParseTryAs() (*MShellTryAs, error) {
	tok := parser.curr
	parser.NextToken() // consume TRYAS
	target, errs := parser.parseTypeExpr()
	if len(errs) > 0 {
		return nil, fmt.Errorf("'tryAs' target: %s", joinTypeErrs(errs))
	}
	return &MShellTryAs{Tok: tok, Target: target}, nil
}

// MShellIsPattern is the match pattern `is <typeExpr> name`: the arm runs
// when the value validates against the type, and the value is stored in
// name (`_` stores nothing).
type MShellIsPattern struct {
	IsTok   Token
	Target  MShellParseItem
	Binding Token
	// The target resolved by the runtime, once (Validate.go).
	resolved runtimeTarget
}

func (p *MShellIsPattern) ToJson() string {
	return fmt.Sprintf("{\"kind\": \"isPattern\", \"type\": %s, \"binding\": %q}", p.Target.ToJson(), p.Binding.Lexeme)
}

func (p *MShellIsPattern) DebugString() string {
	return "is " + p.Target.DebugString() + " " + p.Binding.Lexeme
}

func (p *MShellIsPattern) GetStartToken() Token { return p.IsTok }
func (p *MShellIsPattern) GetEndToken() Token   { return p.Binding }

// parseIsPattern parses `is <typeExpr> name`. The `is` word is the current
// token on entry.
func (parser *MShellParser) parseIsPattern() (*MShellIsPattern, error) {
	isTok := parser.curr
	parser.NextToken() // consume is
	target, errs := parser.parseTypeExpr()
	if len(errs) > 0 {
		return nil, fmt.Errorf("'is' pattern: %s", joinTypeErrs(errs))
	}
	if parser.curr.Type != LITERAL {
		hint := ""
		if parser.curr.Type == INTERPRET {
			hint = " 'x' runs a quotation, so it cannot be a name."
		}
		return nil, fmt.Errorf("%d:%d: Expected a name (or '_') after 'is %s', got %s. The typed pattern is written 'is T name'.%s",
			parser.curr.Line, parser.curr.Column, target.DebugString(), tokDesc(parser.curr), hint)
	}
	binding := parser.curr
	parser.NextToken() // consume the name
	return &MShellIsPattern{IsTok: isTok, Target: target, Binding: binding}, nil
}

// ParseTypeDecl handles a top-level `type Name = <typeExpr>`. The TYPE
// keyword is the current token on entry; on return, parser.curr is past
// the type expression.
func (parser *MShellParser) ParseTypeDecl() (*MShellTypeDecl, error) {
	startTok := parser.curr
	parser.NextToken() // consume TYPE
	if parser.curr.Type != LITERAL {
		return nil, fmt.Errorf("%d:%d: expected a type name after 'type', got %s",
			parser.curr.Line, parser.curr.Column, parser.curr.Type)
	}
	nameTok := parser.curr
	parser.NextToken() // consume LITERAL
	if parser.curr.Type != EQUALS {
		return nil, fmt.Errorf("%d:%d: expected '=' in type declaration, got %s",
			parser.curr.Line, parser.curr.Column, parser.curr.Type)
	}
	parser.NextToken() // consume =
	body, errs := parser.parseTypeExpr()
	if len(errs) > 0 {
		return nil, fmt.Errorf("type declaration body: %s", joinTypeErrs(errs))
	}
	return &MShellTypeDecl{
		Name:      nameTok.Lexeme,
		NameToken: nameTok,
		File:      parser.lexer.tokenFile,
		StartTok:  startTok,
		Body:      body,
	}, nil
}

// ParseAsCast handles a postfix `as <typeExpr>`. The AS keyword is the
// current token on entry.
func (parser *MShellParser) ParseAsCast() (*MShellAsCast, error) {
	asTok := parser.curr
	parser.NextToken() // consume AS
	target, errs := parser.parseTypeExpr()
	if len(errs) > 0 {
		return nil, fmt.Errorf("'as' target: %s", joinTypeErrs(errs))
	}
	return &MShellAsCast{AsToken: asTok, Target: target}, nil
}

func joinTypeErrs(errs []TypeError) string {
	var sb strings.Builder
	wrote := 0
	type pos struct{ line, col int }
	seen := make(map[pos]struct{}, len(errs))
	for _, e := range errs {
		key := pos{line: e.Pos.Line, col: e.Pos.Column}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if wrote > 0 {
			sb.WriteString("; ")
		}
		fmt.Fprintf(&sb, "%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Hint)
		wrote++
	}
	return sb.String()
}
