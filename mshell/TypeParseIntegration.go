package main

// Phase 10 step 2 — wiring the type-expression parser into the main
// parser.
//
// Two new parse-tree node types:
//
//   - MShellTypeDecl: `type Name = <typeExpr>` top-level declaration.
//   - MShellAsCast:    `<value> as <typeExpr>` postfix cast.
//
// Both store the parsed-but-unresolved type AST. Resolution to TypeIds
// happens later, when the checker pass runs over the parse tree (Phase 10
// step 3). At evaluation time both nodes are no-ops — `as` is purely
// static and `type` declarations have no runtime effect by design.
//
// The streaming adapter parserTokenSource lets ParseTypeExprAST drive the
// MShellParser's lexer directly. It maintains a small look-ahead buffer
// to support PeekAt(n) and writes the un-consumed lookahead back into
// parser.curr when finished.

import (
	"fmt"
	"strings"
)

// MShellTypeDecl is a top-level `type Name = <typeExpr>` declaration.
type MShellTypeDecl struct {
	Name      string
	NameToken Token
	StartTok  Token // the TYPE keyword
	Body      TypeExprAST
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
	Target  TypeExprAST
}

func (c *MShellAsCast) ToJson() string {
	return "{\"kind\": \"asCast\"}"
}

func (c *MShellAsCast) DebugString() string {
	return "as <type>"
}

func (c *MShellAsCast) GetStartToken() Token { return c.AsToken }
func (c *MShellAsCast) GetEndToken() Token   { return c.AsToken }

// parserTokenSource adapts an MShellParser to the TokenSource interface
// expected by ParseTypeExprAST. It buffers tokens pulled from the lexer
// so PeekAt(n) works for n > 0; the parser.curr field is treated as the
// first buffered token on construction.
type parserTokenSource struct {
	p   *MShellParser
	buf []Token
}

func newParserTokenSource(p *MShellParser) *parserTokenSource {
	return &parserTokenSource{p: p, buf: []Token{p.curr}}
}

// ensureN guarantees buf has at least n+1 tokens. Tokens are pulled from
// the parser's lexer.
func (s *parserTokenSource) ensureN(n int) {
	for len(s.buf) <= n {
		s.buf = append(s.buf, s.p.scanToken())
	}
}

func (s *parserTokenSource) Peek() Token { return s.PeekAt(0) }

func (s *parserTokenSource) PeekAt(n int) Token {
	s.ensureN(n)
	return s.buf[n]
}

func (s *parserTokenSource) Advance() Token {
	s.ensureN(0)
	t := s.buf[0]
	s.buf = s.buf[1:]
	return t
}

// finish writes any un-consumed buffered token back into parser.curr so
// the rest of the parser sees the right "next" token. The AST parser
// always leaves at most one un-consumed lookahead token (the token that
// signaled end of the type expression).
func (s *parserTokenSource) finish() {
	if len(s.buf) == 0 {
		s.p.NextToken()
		return
	}
	if len(s.buf) > 1 {
		// Shouldn't happen given the AST parser's lookahead is bounded by
		// PeekAt(1) and both branches always consume the peeked tokens.
		// If this fires, the AST parser has a buffering bug.
		panic("parserTokenSource: more than one un-consumed lookahead")
	}
	s.p.curr = s.buf[0]
	s.buf = nil
}

// parseTypeExprStreaming runs ParseTypeExprAST against the parser's live
// token stream and returns the resulting AST. The parser's curr is
// positioned just past the consumed type expression on return.
func (parser *MShellParser) parseTypeExprStreaming() (TypeExprAST, []TypeError) {
	src := newParserTokenSource(parser)
	ast, errs := ParseTypeExprAST(src)
	src.finish()
	return ast, errs
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
	body, errs := parser.parseTypeExprStreaming()
	if len(errs) > 0 {
		var sb strings.Builder
		for i, e := range errs {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(e.Hint)
		}
		return nil, fmt.Errorf("%d:%d: type declaration body: %s",
			startTok.Line, startTok.Column, sb.String())
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
	target, errs := parser.parseTypeExprStreaming()
	if len(errs) > 0 {
		var sb strings.Builder
		for i, e := range errs {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(e.Hint)
		}
		return nil, fmt.Errorf("%d:%d: 'as' target: %s",
			asTok.Line, asTok.Column, sb.String())
	}
	return &MShellAsCast{AsToken: asTok, Target: target}, nil
}
