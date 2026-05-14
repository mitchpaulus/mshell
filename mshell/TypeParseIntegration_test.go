package main

import "testing"

// Integration tests for the parser surface added in Phase 10 step 2:
// top-level `type X = ...` declarations and the postfix `as <T>` operator.

func parseSourceForIntegration(t *testing.T, src string) *MShellFile {
	t.Helper()
	l := NewLexer(src, nil)
	p := NewMShellParser(l)
	file, err := p.ParseFile()
	if err != nil {
		t.Fatalf("parse error: %v\nsource:\n%s", err, src)
	}
	return file
}

func TestParseTopLevelTypeDecl(t *testing.T) {
	file := parseSourceForIntegration(t, "type Result = int | str")
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}
	decl, ok := file.Items[0].(*MShellTypeDecl)
	if !ok {
		t.Fatalf("expected *MShellTypeDecl, got %T", file.Items[0])
	}
	if decl.Name != "Result" {
		t.Fatalf("name = %q, want Result", decl.Name)
	}
	union, ok := decl.Body.(*TypeUnionExpr)
	if !ok {
		t.Fatalf("body should be TypeUnionExpr, got %T", decl.Body)
	}
	if len(union.Arms) != 2 {
		t.Fatalf("expected 2 arms, got %d", len(union.Arms))
	}
}

func TestParseTypeDeclThenItem(t *testing.T) {
	// type decl followed by a regular program item; verify the parser
	// returns to the normal stream after consuming the type body.
	file := parseSourceForIntegration(t, "type UserId = int  42 wl")
	if len(file.Items) < 2 {
		t.Fatalf("expected >=2 items, got %d", len(file.Items))
	}
	if _, ok := file.Items[0].(*MShellTypeDecl); !ok {
		t.Fatalf("first item should be MShellTypeDecl, got %T", file.Items[0])
	}
}

func TestParseAsCast(t *testing.T) {
	file := parseSourceForIntegration(t, "type R = int  42 as R")
	// Items: [TypeDecl, 42-token, AsCast]
	var sawAs bool
	for _, it := range file.Items {
		if cast, ok := it.(*MShellAsCast); ok {
			sawAs = true
			named, ok := cast.Target.(*TypeNamed)
			if !ok {
				t.Fatalf("cast target should be TypeNamed, got %T", cast.Target)
			}
			if named.Name != "R" {
				t.Fatalf("cast target name = %q, want R", named.Name)
			}
		}
	}
	if !sawAs {
		t.Fatalf("expected an MShellAsCast in parse tree; items=%v", file.Items)
	}
}

func TestParseAsThenTrailingTokens(t *testing.T) {
	// 42 as Result then more tokens. Make sure the cast doesn't swallow
	// the trailing program.
	file := parseSourceForIntegration(t, "type Result = int  42 as Result wl")
	// Should see: typeDecl, 42, asCast, wl.
	tail := file.Items[len(file.Items)-1]
	tok, ok := tail.(Token)
	if !ok {
		t.Fatalf("last item should be a Token (the 'wl' literal), got %T", tail)
	}
	if tok.Lexeme != "wl" {
		t.Fatalf("trailing token = %q, want 'wl'", tok.Lexeme)
	}
}

func TestParseTypeDeclWithMaybe(t *testing.T) {
	file := parseSourceForIntegration(t, "type Box = Maybe[int]")
	decl := file.Items[0].(*MShellTypeDecl)
	named, ok := decl.Body.(*TypeNamed)
	if !ok {
		t.Fatalf("expected TypeNamed(Maybe), got %T", decl.Body)
	}
	if named.Name != "Maybe" || len(named.Args) != 1 {
		t.Fatalf("expected Maybe with 1 arg, got %+v", named)
	}
}

func TestParseTypeDeclWithShape(t *testing.T) {
	file := parseSourceForIntegration(t, "type Person = {name: str, age: int}")
	decl := file.Items[0].(*MShellTypeDecl)
	shape, ok := decl.Body.(*TypeShapeExpr)
	if !ok {
		t.Fatalf("expected TypeShapeExpr, got %T", decl.Body)
	}
	if len(shape.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(shape.Fields))
	}
}

func TestParseTypeDeclMissingName(t *testing.T) {
	l := NewLexer("type = int", nil)
	p := NewMShellParser(l)
	if _, err := p.ParseFile(); err == nil {
		t.Fatalf("expected parse error for missing name")
	}
}

func TestParseTypeDeclMissingEquals(t *testing.T) {
	l := NewLexer("type X int", nil)
	p := NewMShellParser(l)
	if _, err := p.ParseFile(); err == nil {
		t.Fatalf("expected parse error for missing '='")
	}
}
