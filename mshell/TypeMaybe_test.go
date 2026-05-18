package main

import "testing"

// Phase-4 tests: Maybe[T] constructors via name-keyed builtins.
//
// Match-arm dispatch is deferred to Phase 6b alongside the rest of the
// branching infrastructure (if/else, match, var-env reconciliation,
// exhaustiveness). The constructors are useful on their own.

func TestJustWrapsConcreteType(t *testing.T) {
	c := runTokens(
		mkTok(INTEGER, "5"),
		mkTok(LITERAL, "just"),
	)
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := c.arena.MakeMaybe(TidInt)
	if c.Stack().Top() != want {
		t.Fatalf("expected Maybe[int] on top, got %s",
			FormatType(c.arena, c.names, c.Stack().Top()))
	}
}

func TestJustOnDifferentTypesAtTwoCallSites(t *testing.T) {
	// Each call site should get fresh vars; the first call binding T=int
	// must not leak into the second call's T.
	c := runTokens(
		mkTok(INTEGER, "5"),
		mkTok(LITERAL, "just"),
		mkTok(STRING, "\"hi\""),
		mkTok(LITERAL, "just"),
	)
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.Stack().Len() != 2 {
		t.Fatalf("expected 2 items, got %d", c.Stack().Len())
	}
	wantInt := c.arena.MakeMaybe(TidInt)
	wantStr := c.arena.MakeMaybe(TidStr)
	got := c.Stack().Snapshot()
	if got[0] != wantInt {
		t.Fatalf("expected Maybe[int] at [0], got %s",
			FormatType(c.arena, c.names, got[0]))
	}
	if got[1] != wantStr {
		t.Fatalf("expected Maybe[str] at [1], got %s",
			FormatType(c.arena, c.names, got[1]))
	}
}

func TestNoneProducesUnboundMaybe(t *testing.T) {
	c := runTokens(mkTok(LITERAL, "none"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.Stack().Len() != 1 {
		t.Fatalf("expected 1 item, got %d", c.Stack().Len())
	}
	top := c.Stack().Top()
	n := c.arena.Node(top)
	if n.Kind != TKMaybe {
		t.Fatalf("expected Maybe kind, got %v", n.Kind)
	}
	// The inner should be a fresh, unbound variable.
	innerNode := c.arena.Node(TypeId(n.A))
	if innerNode.Kind != TKVar {
		t.Fatalf("none's inner should be a TKVar, got %v", innerNode.Kind)
	}
}

func TestNoneInferredFromContext(t *testing.T) {
	// (none) followed by a sig that demands Maybe[int] should bind the
	// var to int. We construct the demanding sig directly to avoid
	// needing parser-level features.
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	maybeInt := arena.MakeMaybe(TidInt)
	consumer := QuoteSig{Inputs: []TypeId{maybeInt}, Outputs: []TypeId{TidInt}}

	// Push none, then apply consumer.
	c.checkOne(mkTok(LITERAL, "none"))
	c.applySig(consumer, mkTok(LITERAL, "fromMaybe"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.Stack().Top() != TidInt {
		t.Fatalf("expected int output, got %s",
			FormatType(c.arena, c.names, c.Stack().Top()))
	}
}

func TestJustOutputAcceptedByMaybeConsumer(t *testing.T) {
	// `5 just` then a consumer that wants Maybe[int].
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	maybeInt := arena.MakeMaybe(TidInt)
	consumer := QuoteSig{Inputs: []TypeId{maybeInt}, Outputs: []TypeId{TidBool}}

	c.checkOne(mkTok(INTEGER, "5"))
	c.checkOne(mkTok(LITERAL, "just"))
	c.applySig(consumer, mkTok(LITERAL, "isJust"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.Stack().Top() != TidBool {
		t.Fatalf("expected bool output, got %s",
			FormatType(c.arena, c.names, c.Stack().Top()))
	}
}

func TestJustWithMismatchedConsumer(t *testing.T) {
	// `5 just` produces Maybe[int]; a consumer wanting Maybe[str] fails.
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	maybeStr := arena.MakeMaybe(TidStr)
	consumer := QuoteSig{Inputs: []TypeId{maybeStr}, Outputs: []TypeId{TidBool}}

	c.checkOne(mkTok(INTEGER, "5"))
	c.checkOne(mkTok(LITERAL, "just"))
	c.applySig(consumer, mkTok(LITERAL, "needsStr"))

	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrTypeMismatch {
		t.Fatalf("expected single type mismatch, got %+v", errs)
	}
}

func TestImplicitLiftDisallowed(t *testing.T) {
	// A bare int must NOT flow into a Maybe[int] slot — `just` is required.
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	maybeInt := arena.MakeMaybe(TidInt)
	consumer := QuoteSig{Inputs: []TypeId{maybeInt}, Outputs: []TypeId{TidBool}}

	c.checkOne(mkTok(INTEGER, "5"))
	c.applySig(consumer, mkTok(LITERAL, "isJust"))

	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrTypeMismatch {
		t.Fatalf("expected type mismatch (no implicit lift), got %+v", errs)
	}
}
