package main

import "testing"

// Overload-dispatch tests.
//
// The checker does not prioritize candidates by specificity: if more
// than one overload remains viable after trial unification against the
// current stack, the dispatch fans out under the branching driver and
// the surviving alternatives are pruned by downstream constraints or
// joined at the end of the walk. CheckTokens drives that machinery, so
// these tests observe joined post-states rather than ambiguity errors.
// The deeper branching-walker integration tests live in
// TypeCheckProgram_test.go.

// registerOverloads is a test helper that injects a name->[]sig entry
// into the checker's overload table.
func registerOverloads(c *Checker, name string, sigs ...QuoteSig) {
	id := c.names.Intern(name)
	c.nameBuiltins[id] = append(c.nameBuiltins[id], sigs...)
}

func TestOverloadConcreteAndGenericBothViableJoins(t *testing.T) {
	// f : (int -- int)         — concrete
	// f : (T -- T)             — generic
	// Calling with int on stack: both overloads unify. The dispatch
	// fans out, both branches produce int, and the join collapses
	// them back into a single int — no error.
	c := freshChecker()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{tType}, Generics: []TypeVarId{tVar}},
	)

	c.CheckTokens([]Token{mkTok(INTEGER, "5"), mkTok(LITERAL, "f")})

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.subst.Apply(c.arena, c.stack.Top()) != TidInt {
		t.Fatalf("expected int after join, got %s",
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

func TestOverloadEquivalentArmsJoin(t *testing.T) {
	// f : (T -- T)
	// f : (U -- U)    (different generic, same shape)
	// Both viable; the branches agree after substitution, so the join
	// collapses to a single int.
	c := freshChecker()
	tVar := TypeVarId(0)
	uVar := TypeVarId(0) // separate sigs, separate generics namespaces
	tType := c.arena.MakeVar(tVar)
	uType := c.arena.MakeVar(uVar)

	registerOverloads(c, "f",
		QuoteSig{Inputs: []TypeId{tType}, Outputs: []TypeId{tType}, Generics: []TypeVarId{tVar}},
		QuoteSig{Inputs: []TypeId{uType}, Outputs: []TypeId{uType}, Generics: []TypeVarId{uVar}},
	)

	c.CheckTokens([]Token{mkTok(INTEGER, "5"), mkTok(LITERAL, "f")})

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.subst.Apply(c.arena, c.stack.Top()) != TidInt {
		t.Fatalf("expected int after join, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestOverloadShapeAndVarBothViableJoins(t *testing.T) {
	// f : ({a: int} -- int)        — concrete shape
	// f : (T -- int)               — pure variable
	// Calling with {a: int}: both overloads unify and both produce
	// int, so the fan-out joins back to int without an error.
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
	c.CheckTokens([]Token{mkTok(LITERAL, "f")})

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.subst.Apply(c.arena, c.stack.Top()) != TidInt {
		t.Fatalf("expected int after join, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestOverloadBrandAndVarBothViableJoins(t *testing.T) {
	// f : (UserId -- int)         — branded int
	// f : (T -- int)              — pure variable
	// Calling with UserId: both overloads unify and both produce int,
	// so the fan-out joins back to int without an error.
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
	c.CheckTokens([]Token{mkTok(LITERAL, "f")})

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.subst.Apply(c.arena, c.stack.Top()) != TidInt {
		t.Fatalf("expected int after join, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
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

func TestOverloadedQuoteArmChoiceConsidersSiblingOperands(t *testing.T) {
	// f : (( -- t) ( -- t) -- t)   — two thunks sharing a generic.
	// Call with an overloaded first thunk ( -- int)|( -- str) and a
	// plain ( -- str) second thunk. The valid typing picks the str arm
	// (t = str). Greedy first-arm unification used to commit ( -- int),
	// bind t = int, and falsely reject the call; overloaded-quote
	// operand expansion now trials each arm as its own scenario.
	c := freshChecker()
	tv := TypeVarId(0)
	tt := c.arena.MakeVar(tv)
	thunkT := c.arena.MakeQuote(QuoteSig{Outputs: []TypeId{tt}})
	registerOverloads(c, "f", QuoteSig{
		Inputs:   []TypeId{thunkT, thunkT},
		Outputs:  []TypeId{tt},
		Generics: []TypeVarId{tv},
	})

	over := c.arena.MakeOverloadedQuote([]QuoteSig{
		{Outputs: []TypeId{TidInt}},
		{Outputs: []TypeId{TidStr}},
	})
	plainStr := c.arena.MakeQuote(QuoteSig{Outputs: []TypeId{TidStr}})

	c.stack.Push(over)
	c.stack.Push(plainStr)
	c.CheckTokens([]Token{mkTok(LITERAL, "f")})

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("valid typing exists (str arm, t=str) but checker rejected: %+v", errs)
	}
	if c.stack.Len() != 1 || c.subst.Apply(c.arena, c.stack.Top()) != TidStr {
		t.Fatalf("expected str result, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}
