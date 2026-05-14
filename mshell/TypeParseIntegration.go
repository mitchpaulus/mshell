package main

// Parse-tree nodes that introduce a type expression at the program level:
//
//   - MShellTypeDecl: `type Name = <typeExpr>` top-level declaration.
//   - MShellAsCast:    `<value> as <typeExpr>` postfix cast.
//
// Both store the parsed-but-unresolved type AST. Resolution to TypeIds
// happens when the checker walks the parse tree, so forward references
// to user-declared types work in declaration order. At evaluation time
// both nodes are no-ops — `as` is purely static and `type` declarations
// have no runtime effect by design.

import (
	"fmt"
	"strings"
)

// MShellTypeDecl is a top-level `type Name = <typeExpr>` declaration.
type MShellTypeDecl struct {
	Name      string
	NameToken Token
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
		return nil, fmt.Errorf("%d:%d: type declaration body: %s",
			startTok.Line, startTok.Column, joinTypeErrs(errs))
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
		return nil, fmt.Errorf("%d:%d: 'as' target: %s",
			asTok.Line, asTok.Column, joinTypeErrs(errs))
	}
	return &MShellAsCast{AsToken: asTok, Target: target}, nil
}

func joinTypeErrs(errs []TypeError) string {
	var sb strings.Builder
	for i, e := range errs {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(e.Hint)
	}
	return sb.String()
}
