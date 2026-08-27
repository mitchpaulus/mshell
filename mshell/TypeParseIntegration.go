package main

// Parse-tree nodes that introduce a type expression at the program level:
//
//   - MShellTypeDecl:   `type Name = <typeExpr>` top-level declaration.
//   - MShellAsCast:     `<value> as <typeExpr>` postfix cast.
//   - MShellTryAsCast:  `<value> tryAs <typeExpr>` checked cast.
//
// All store the parsed-but-unresolved type AST. Resolution to TypeIds
// happens when the checker walks the parse tree, so forward references
// to user-declared types work in declaration order. At evaluation time
// `as` is a no-op (purely static); `type` declarations register their
// body in the evaluator's type environment so `tryAs` can resolve named
// types; `tryAs` validates the top of the stack structurally and pushes
// a Maybe.

import (
	"fmt"
	"strings"
)

// MShellTypeDecl is a top-level `type Name = <typeExpr>` declaration.
type MShellTypeDecl struct {
	Name      string
	NameToken Token
	StartTok  Token // the TYPE keyword
	Body      TypeExpression
}

func (d *MShellTypeDecl) ToJson() string {
	return fmt.Sprintf("{\"kind\": \"typeDecl\", \"name\": %q}", d.Name)
}

func (d *MShellTypeDecl) DebugString() string {
	return fmt.Sprintf("type %s = ...", d.Name)
}

func (d *MShellTypeDecl) GetStartToken() Token { return d.StartTok }
func (d *MShellTypeDecl) GetEndToken() Token   { return d.NameToken }

// MShellAsCast is a `<value> as <typeExpr>` postfix cast.
type MShellAsCast struct {
	AsToken Token
	Target  TypeExpression
}

func (c *MShellAsCast) ToJson() string {
	return "{\"kind\": \"asCast\"}"
}

func (c *MShellAsCast) DebugString() string {
	return "as <type>"
}

func (c *MShellAsCast) GetStartToken() Token { return c.AsToken }
func (c *MShellAsCast) GetEndToken() Token   { return c.AsToken }

// MShellTryAsCast is a `<value> tryAs <typeExpr>` checked cast. Unlike
// `as`, it does runtime work: the evaluator validates the value against
// the type expression structurally and pushes a Maybe — `just value` on
// success, `none` on mismatch. Statically it consumes the value and
// produces Maybe[target].
type MShellTryAsCast struct {
	TryAsToken Token
	Target     TypeExpression
}

func (c *MShellTryAsCast) ToJson() string {
	return "{\"kind\": \"tryAsCast\"}"
}

func (c *MShellTryAsCast) DebugString() string {
	return "tryAs <type>"
}

func (c *MShellTryAsCast) GetStartToken() Token { return c.TryAsToken }
func (c *MShellTryAsCast) GetEndToken() Token   { return c.TryAsToken }

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

// ParseTryAsCast handles a postfix `tryAs <typeExpr>`. The TRYAS keyword
// is the current token on entry.
func (parser *MShellParser) ParseTryAsCast() (*MShellTryAsCast, error) {
	tryAsTok := parser.curr
	parser.NextToken() // consume TRYAS
	target, errs := parser.parseTypeExpr()
	if len(errs) > 0 {
		return nil, fmt.Errorf("'tryAs' target: %s", joinTypeErrs(errs))
	}
	return &MShellTryAsCast{TryAsToken: tryAsTok, Target: target}, nil
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
