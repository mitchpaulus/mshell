package main

import (
	"testing"
)

// mkTok builds a Token with the bare minimum fields tests care about.
// Line/column default to 1 so error formatting has something to print.
func mkTok(t TokenType, lexeme string) Token {
	return Token{Line: 1, Column: 1, Lexeme: lexeme, Type: t}
}

// runTokens constructs a fresh Checker and feeds it tokens. Returns the
// checker so the test can inspect the stack and accumulated errors.
func runTokens(toks ...Token) *Checker {
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	c.CheckTokens(toks)
	return c
}

func TestCheckerIntPlusInt(t *testing.T) {
	c := runTokens(
		mkTok(INTEGER, "2"),
		mkTok(INTEGER, "3"),
		mkTok(PLUS, "+"),
	)
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("expected no errors, got %d: %+v", len(errs), errs)
	}
	if c.Stack().Len() != 1 {
		t.Fatalf("expected 1 item on stack, got %d", c.Stack().Len())
	}
	if got := c.Stack().Top(); got != TidInt {
		t.Fatalf("expected TidInt on top, got %v", got)
	}
}

func TestCheckerIntPlusStr(t *testing.T) {
	c := runTokens(
		mkTok(INTEGER, "2"),
		mkTok(STRING, "\"x\""),
		mkTok(PLUS, "+"),
	)
	errs := c.Errors()
	// `+` now has int and float overloads; neither matches int+str, so
	// the resolver reports TErrNoMatchingOverload.
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Kind != TErrNoMatchingOverload {
		t.Fatalf("expected TErrNoMatchingOverload, got %v", errs[0].Kind)
	}
	// Recovery pops 2, pushes 1 fresh var.
	if c.Stack().Len() != 1 {
		t.Fatalf("after no-match recovery stack should have 1 fresh var; got len=%d", c.Stack().Len())
	}
}

func TestCheckerPlusUnderflow(t *testing.T) {
	c := runTokens(mkTok(PLUS, "+"))
	errs := c.Errors()
	// With overloads, both arity-2 candidates drop on stack-too-short
	// and the resolver reports TErrNoMatchingOverload (rather than
	// the underlying underflow).
	if len(errs) != 1 || errs[0].Kind != TErrNoMatchingOverload {
		t.Fatalf("expected single no-match error, got %+v", errs)
	}
	// Recovery: pops 0 (empty), pushes 1 fresh var.
	if c.Stack().Len() != 1 {
		t.Fatalf("expected 1 fresh var after recovery, got len=%d", c.Stack().Len())
	}
}

func TestCheckerPlusUnderflowOneArg(t *testing.T) {
	c := runTokens(
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	)
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrNoMatchingOverload {
		t.Fatalf("expected single no-match error, got %+v", errs)
	}
	// Recovery: pops 1 (the int), pushes 1 fresh var.
	if c.Stack().Len() != 1 {
		t.Fatalf("expected 1 fresh var after recovery, got len=%d", c.Stack().Len())
	}
}

func TestCheckerComparison(t *testing.T) {
	c := runTokens(
		mkTok(INTEGER, "2"),
		mkTok(INTEGER, "3"),
		mkTok(LESSTHAN, "<"),
	)
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
	if c.Stack().Len() != 1 || c.Stack().Top() != TidBool {
		t.Fatalf("expected bool result, got len=%d top=%v", c.Stack().Len(), c.Stack().Top())
	}
}

func TestCheckerLiteralsPushPrimitives(t *testing.T) {
	c := runTokens(
		mkTok(INTEGER, "1"),
		mkTok(FLOAT, "2.0"),
		mkTok(STRING, "\"x\""),
		mkTok(TRUE, "true"),
	)
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := []TypeId{TidInt, TidFloat, TidStr, TidBool}
	got := c.Stack().Snapshot()
	if len(got) != len(want) {
		t.Fatalf("stack len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stack[%d]: want %v, got %v", i, want[i], got[i])
		}
	}
}

func TestCheckerUnknownIdentifier(t *testing.T) {
	// LITERAL tokens are not in the Phase-2 builtin table, so they read as
	// unknown identifiers. This will be replaced once the parser-level
	// integration in Phase 10 wires in user definitions.
	c := runTokens(mkTok(LITERAL, "noSuchThing"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrUnknownIdentifier {
		t.Fatalf("expected unknown-identifier error, got %+v", errs)
	}
	if errs[0].Name != "noSuchThing" {
		t.Fatalf("expected Name=noSuchThing, got %q", errs[0].Name)
	}
}

func TestCheckerErrorFormat(t *testing.T) {
	arena := NewTypeArena()
	names := NewNameTable()
	e := TypeError{
		Kind:     TErrTypeMismatch,
		Pos:      Token{Line: 7, Column: 12, Lexeme: "+", Type: PLUS},
		Expected: TidInt,
		Actual:   TidStr,
		ArgIndex: 1,
	}
	msg := e.Format(arena, names)
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	// Sanity: message should mention line/column and types.
	for _, want := range []string{"line 7", "column 12", "int", "str"} {
		if !contains(msg, want) {
			t.Errorf("expected %q in error message %q", want, msg)
		}
	}
}

// contains is a tiny helper to avoid pulling in strings just for tests.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
