package main

import "testing"

// Phase-9 tests: overload dispatch.

// registerOverloads is a test helper that injects a name->[]sig entry
// into the checker's overload table.
func registerOverloads(c *Checker, name string, sigs ...QuoteSig) {
	id := c.names.Intern(name)
	c.nameBuiltins[id] = append(c.nameBuiltins[id], sigs...)
}

func TestOverloadConcreteWinsOverGeneric(t *testing.T) {
	// f : (int -- int)         — concrete
	// f : (T -- T)             — generic
	// Calling with int on stack should pick the concrete one.
	c := freshChecker()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{tType}, Generics: []TypeVarId{tVar}},
	)

	c.checkOne(mkTok(INTEGER, "5"))
	c.checkOne(mkTok(LITERAL, "f"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("expected int on top; got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestOverloadGenericFallback(t *testing.T) {
	// f : (int -- int)
	// f : (T -- T)
	// Calling with str on stack: only the generic matches.
	c := freshChecker()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{tType}, Generics: []TypeVarId{tVar}},
	)

	c.checkOne(mkTok(STRING, "\"hi\""))
	c.checkOne(mkTok(LITERAL, "f"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Top() != TidStr {
		t.Fatalf("expected str on top (generic preserves type); got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestOverloadDifferentArities(t *testing.T) {
	// f : (str -- str)
	// f : (int int -- int)
	// Calling with one str: pick the first.
	// Calling with two ints: pick the second.
	c := freshChecker()
	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidStr}},
		QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}},
	)

	c.checkOne(mkTok(STRING, "\"hi\""))
	c.checkOne(mkTok(LITERAL, "f"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors (str case): %+v", errs)
	}
	if c.stack.Top() != TidStr {
		t.Fatalf("str case: expected str output, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}

	c.stack.Reset()
	c.errors = nil
	c.checkOne(mkTok(INTEGER, "1"))
	c.checkOne(mkTok(INTEGER, "2"))
	c.checkOne(mkTok(LITERAL, "f"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors (int int case): %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("int int case: expected int, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestOverloadNoMatch(t *testing.T) {
	// f : (int -- int)
	// f : (str -- str)
	// Calling with bool: nothing matches.
	c := freshChecker()
	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		QuoteSig{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidStr}},
	)

	c.checkOne(mkTok(TRUE, "true"))
	c.checkOne(mkTok(LITERAL, "f"))

	errs := c.Errors()
	if len(errs) == 0 {
		t.Fatalf("expected an error; got none")
	}
	// Should be a no-match error followed by the recovery's downstream error.
	foundNoMatch := false
	for _, e := range errs {
		if e.Kind == TErrNoMatchingOverload {
			foundNoMatch = true
			break
		}
	}
	if !foundNoMatch {
		t.Fatalf("expected TErrNoMatchingOverload, got %+v", errs)
	}
}

func TestOverloadAmbiguous(t *testing.T) {
	// f : (T -- T)
	// f : (U -- U)    (different generic, same shape)
	// Both have the same specificity → ambiguous.
	c := freshChecker()
	tVar := TypeVarId(0)
	uVar := TypeVarId(0) // separate sigs, separate generics namespaces
	tType := c.arena.MakeVar(tVar)
	uType := c.arena.MakeVar(uVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{tType}, Generics: []TypeVarId{tVar}},
		QuoteSig{Inputs: []TypeId{uType}, Outputs: []TypeId{uType}, Generics: []TypeVarId{uVar}},
	)

	c.checkOne(mkTok(INTEGER, "5"))
	c.checkOne(mkTok(LITERAL, "f"))

	errs := c.Errors()
	foundAmbig := false
	for _, e := range errs {
		if e.Kind == TErrAmbiguousOverload {
			foundAmbig = true
			break
		}
	}
	if !foundAmbig {
		t.Fatalf("expected TErrAmbiguousOverload, got %+v", errs)
	}
}

func TestOverloadShapeBeatsVar(t *testing.T) {
	// f : ({a: int} -- int)        — concrete shape
	// f : (T -- int)               — pure variable
	// Calling with {a: int}: shape wins.
	c := freshChecker()
	nA := c.names.Intern("a")
	shapeAInt := c.arena.MakeShape([]ShapeField{{Name: nA, Type: TidInt}})
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{shapeAInt}, Outputs: []TypeId{TidInt}},
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{TidInt}, Generics: []TypeVarId{tVar}},
	)

	c.stack.Push(shapeAInt)
	c.checkOne(mkTok(LITERAL, "f"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestOverloadBrandBeatsStructural(t *testing.T) {
	// f : (UserId -- int)         — branded int
	// f : (T -- int)              — pure variable
	// Calling with UserId: brand wins (brand bonus).
	c := freshChecker()
	brandU := c.names.Intern("UserId")
	userId := c.arena.MakeBrand(brandU, TidInt)
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{userId}, Outputs: []TypeId{TidInt}},
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{TidInt}, Generics: []TypeVarId{tVar}},
	)

	c.stack.Push(userId)
	c.checkOne(mkTok(LITERAL, "f"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestOverloadIsolation(t *testing.T) {
	// Trial unification of one candidate must not contaminate
	// substitution state for the next. Sets up:
	//   f : (int int -- int)     (matches)
	//   f : (T str -- T)         (would bind T=int but conflict on str)
	// With stack [int, str], the first should win cleanly.
	// We then call again with [int, str] and verify the same result —
	// proving the failed second-candidate trial of the first call did
	// not leave any binding behind.
	c := freshChecker()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)

	// Reorder candidates so the conflicting generic is FIRST and would
	// pollute state if rollback didn't work.
	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{tType, TidStr}, Outputs: []TypeId{tType}, Generics: []TypeVarId{tVar}},
		QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}},
	)

	// Stack: [int, str]. Generic candidate would bind T=int but want str on top → succeeds!
	// Wait — actually (T str) would unify with (int str) just fine, binding T=int.
	// And the (int int) candidate fails because top is str.
	// So the generic wins. That's still a useful test of trial isolation:
	// the binding T=int from the first trial must not leak forward.
	c.checkOne(mkTok(INTEGER, "1"))
	c.checkOne(mkTok(STRING, "\"hi\""))
	c.checkOne(mkTok(LITERAL, "f"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors first call: %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("first call: expected int on top, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}

	// Second call with a different concrete: T should be free again.
	c.stack.Reset()
	c.errors = nil
	c.checkOne(mkTok(TRUE, "true"))
	c.checkOne(mkTok(STRING, "\"hi\""))
	c.checkOne(mkTok(LITERAL, "f"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors second call: %+v", errs)
	}
	if c.stack.Top() != TidBool {
		t.Fatalf("second call: T should rebind to bool, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestSpecificityScores(t *testing.T) {
	// Quick spot-checks on the scoring function.
	arena := NewTypeArena()
	names := NewNameTable()
	if specificityScore(arena, TidInt) != 1 {
		t.Errorf("int score should be 1")
	}
	if specificityScore(arena, arena.MakeVar(0)) != 0 {
		t.Errorf("var score should be 0")
	}
	if specificityScore(arena, arena.MakeList(TidInt)) != 2 {
		t.Errorf("[int] score should be 2")
	}
	if specificityScore(arena, arena.MakeList(arena.MakeVar(0))) != 1 {
		t.Errorf("[T] score should be 1")
	}
	brand := arena.MakeBrand(names.Intern("U"), TidInt)
	if specificityScore(arena, brand) != 3 {
		t.Errorf("UserId(int) score should be 3 (brand bonus + int)")
	}
}

func TestSubstCheckpointRollback(t *testing.T) {
	arena := NewTypeArena()
	var s Substitution
	v0 := s.FreshVar(arena)
	cp := s.Checkpoint()
	v1 := s.FreshVar(arena)
	if !s.Bind(arena, TypeVarId(arena.Node(v0).A), TidInt) {
		t.Fatalf("bind v0 should succeed")
	}
	if !s.Bind(arena, TypeVarId(arena.Node(v1).A), TidStr) {
		t.Fatalf("bind v1 should succeed")
	}
	s.Rollback(cp)
	if s.Apply(arena, v0) != v0 {
		t.Fatalf("after rollback, v0 should be unbound again")
	}
	// v1's slot was truncated; Apply on v1 should treat it as unbound.
	if s.Apply(arena, v1) != v1 {
		t.Fatalf("after rollback, v1 should be unbound (slot dropped)")
	}
}
