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

// MShellEnumDecl is a top-level `enum Name = c1 | c2(T..) | ...`
// declaration: a generative tagged sum type. Each member is a constructor
// name with an optional parenthesized payload type list. MemberPayloads is
// parallel to Members; an entry is empty for a nullary member.
type MShellEnumDecl struct {
	Name           string
	NameToken      Token
	StartTok       Token // the ENUM keyword
	Members        []string
	MemberToks     []Token
	MemberPayloads [][]MShellParseItem
}

func (d *MShellEnumDecl) ToJson() string {
	parts := make([]string, len(d.Members))
	for i, m := range d.Members {
		parts[i] = fmt.Sprintf("%q", m)
	}
	return fmt.Sprintf("{\"kind\": \"enumDecl\", \"name\": %q, \"members\": [%s]}", d.Name, strings.Join(parts, ", "))
}

func (d *MShellEnumDecl) DebugString() string {
	return fmt.Sprintf("enum %s = %s", d.Name, strings.Join(d.Members, " | "))
}

func (d *MShellEnumDecl) GetStartToken() Token { return d.StartTok }
func (d *MShellEnumDecl) GetEndToken() Token {
	if len(d.MemberToks) > 0 {
		return d.MemberToks[len(d.MemberToks)-1]
	}
	return d.NameToken
}

// ParseEnumDecl handles a top-level `enum Name = member (| member)*`, where a
// member is a bare identifier (LITERAL) optionally followed by a parenthesized
// payload type list `(T1 T2 ...)`. The parentheses delimit the payload so the
// member set is unambiguous against following code (mshell has no statement
// terminator). The ENUM keyword is the current token on entry; on return,
// parser.curr is positioned past the last member.
func (parser *MShellParser) ParseEnumDecl() (*MShellEnumDecl, error) {
	startTok := parser.curr
	parser.NextToken() // consume ENUM
	if parser.curr.Type != LITERAL {
		return nil, fmt.Errorf("%d:%d: expected an enum name after 'enum', got %s",
			parser.curr.Line, parser.curr.Column, parser.curr.Type)
	}
	nameTok := parser.curr
	parser.NextToken() // consume name
	if parser.curr.Type != EQUALS {
		return nil, fmt.Errorf("%d:%d: expected '=' in enum declaration, got %s",
			parser.curr.Line, parser.curr.Column, parser.curr.Type)
	}
	parser.NextToken() // consume =

	decl := &MShellEnumDecl{Name: nameTok.Lexeme, NameToken: nameTok, StartTok: startTok}
	var errs []TypeError
	for {
		if parser.curr.Type != LITERAL {
			return nil, fmt.Errorf("%d:%d: expected an enum member name (an identifier), got %s",
				parser.curr.Line, parser.curr.Column, parser.curr.Type)
		}
		memberName := parser.curr.Lexeme
		decl.Members = append(decl.Members, memberName)
		decl.MemberToks = append(decl.MemberToks, parser.curr)
		parser.NextToken() // consume member

		var payloads []MShellParseItem
		if parser.curr.Type == LEFT_PAREN {
			openTok := parser.curr
			parser.NextToken() // consume (
			for parser.curr.Type != RIGHT_PAREN && parser.curr.Type != EOF {
				payloads = append(payloads, parser.parseTypePrimary(&errs))
			}
			if parser.curr.Type != RIGHT_PAREN {
				return nil, fmt.Errorf("%d:%d: expected ')' to close the payload list for enum member '%s'",
					openTok.Line, openTok.Column, memberName)
			}
			parser.NextToken() // consume )
			if len(payloads) == 0 {
				return nil, fmt.Errorf("%d:%d: enum member '%s' has an empty payload list '()'; omit the parentheses for a nullary member",
					openTok.Line, openTok.Column, memberName)
			}
		}
		decl.MemberPayloads = append(decl.MemberPayloads, payloads)

		if parser.curr.Type == PIPE {
			parser.NextToken() // consume |
			continue
		}
		break
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("enum declaration body: %s", joinTypeErrs(errs))
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
