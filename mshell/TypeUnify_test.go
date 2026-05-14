package main

import "testing"

// Phase-6 tests: type variables, substitution, generic instantiation.

func TestSubstFreshVarsAreDistinct(t *testing.T) {
	arena := NewTypeArena()
	var s Substitution
	a := s.FreshVar(arena)
	b := s.FreshVar(arena)
	if a == b {
		t.Fatalf("FreshVar should produce distinct ids, got %v twice", a)
	}
	// Both should be unbound (Apply returns the var unchanged).
	if s.Apply(arena, a) != a {
		t.Fatalf("unbound var should Apply to itself")
	}
}

func TestUnifyVarBindsToConcrete(t *testing.T) {
	c := newCheckerForUnify()
	v := c.subst.FreshVar(c.arena)
	if !c.unify(v, TidInt) {
		t.Fatalf("var should bind to int")
	}
	if c.subst.Apply(c.arena, v) != TidInt {
		t.Fatalf("after binding, var should resolve to int")
	}
}

func TestUnifyVarBindsViaSecondSide(t *testing.T) {
	// Symmetry: var on the want side also binds.
	c := newCheckerForUnify()
	v := c.subst.FreshVar(c.arena)
	if !c.unify(TidStr, v) {
		t.Fatalf("var on right side should bind")
	}
	if c.subst.Apply(c.arena, v) != TidStr {
		t.Fatalf("after binding, var should resolve to str")
	}
}

func TestUnifySameVarVacuouslyOk(t *testing.T) {
	c := newCheckerForUnify()
	v := c.subst.FreshVar(c.arena)
	if !c.unify(v, v) {
		t.Fatalf("var should unify with itself")
	}
}

func TestUnifyTwoVarsThenBind(t *testing.T) {
	c := newCheckerForUnify()
	a := c.subst.FreshVar(c.arena)
	b := c.subst.FreshVar(c.arena)
	if !c.unify(a, b) {
		t.Fatalf("two free vars should unify")
	}
	// Now bind one to a concrete; the other should resolve to it.
	if !c.unify(a, TidBool) {
		t.Fatalf("unifying with concrete should succeed")
	}
	if c.subst.Apply(c.arena, b) != TidBool {
		t.Fatalf("transitive resolution failed: b should be bool")
	}
}

func TestUnifyVarConflict(t *testing.T) {
	c := newCheckerForUnify()
	v := c.subst.FreshVar(c.arena)
	if !c.unify(v, TidInt) {
		t.Fatalf("first bind should succeed")
	}
	if c.unify(v, TidStr) {
		t.Fatalf("second conflicting unify should fail")
	}
}

func TestUnifyOccursCheck(t *testing.T) {
	c := newCheckerForUnify()
	v := c.subst.FreshVar(c.arena)
	listOfV := c.arena.MakeList(v)
	if c.unify(v, listOfV) {
		t.Fatalf("occurs check must reject T = [T]")
	}
}

func TestUnifyListWithVarElement(t *testing.T) {
	c := newCheckerForUnify()
	v := c.subst.FreshVar(c.arena)
	listV := c.arena.MakeList(v)
	listInt := c.arena.MakeList(TidInt)
	if !c.unify(listV, listInt) {
		t.Fatalf("[T] should unify with [int], binding T=int")
	}
	if c.subst.Apply(c.arena, v) != TidInt {
		t.Fatalf("T should resolve to int after unification")
	}
}

func TestApplyRebuildsList(t *testing.T) {
	arena := NewTypeArena()
	var s Substitution
	v := s.FreshVar(arena)
	listV := arena.MakeList(v)
	// Bind directly to test Apply (without going through unify).
	if !s.Bind(arena, TypeVarId(arena.Node(v).A), TidInt) {
		t.Fatalf("Bind should succeed")
	}
	resolved := s.Apply(arena, listV)
	if resolved != arena.MakeList(TidInt) {
		t.Fatalf("Apply should rebuild [T] as [int] (hashconsed)")
	}
}

func TestInstantiatePolymorphicIdentity(t *testing.T) {
	// Sig: ( T -- T ) — id function. Each call site should get a fresh T.
	c := newCheckerForUnify()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)
	sig := QuoteSig{
		Inputs:   []TypeId{tType},
		Outputs:  []TypeId{tType},
		Generics: []TypeVarId{tVar},
	}

	// Call 1: feed it an int.
	c.stack.Push(TidInt)
	c.applySig(sig, mkTok(LITERAL, "id"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("call 1 should not error: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("call 1: expected int on top, got len=%d top=%v",
			c.stack.Len(), c.stack.Top())
	}

	// Call 2: feed it a str. Each call gets fresh vars, so no conflict
	// with the first call's binding.
	c.stack.Reset()
	c.stack.Push(TidStr)
	c.applySig(sig, mkTok(LITERAL, "id"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("call 2 should not error: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidStr {
		t.Fatalf("call 2: expected str on top, got len=%d top=%v",
			c.stack.Len(), c.stack.Top())
	}
}

func TestInstantiatePolymorphicMaybeJust(t *testing.T) {
	// Sig: ( T -- Maybe[T] ) — the `just` constructor.
	c := newCheckerForUnify()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)
	maybeT := c.arena.MakeMaybe(tType)
	sig := QuoteSig{
		Inputs:   []TypeId{tType},
		Outputs:  []TypeId{maybeT},
		Generics: []TypeVarId{tVar},
	}
	c.stack.Push(TidInt)
	c.applySig(sig, mkTok(LITERAL, "just"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("just should not error: %+v", errs)
	}
	want := c.arena.MakeMaybe(TidInt)
	if c.stack.Top() != want {
		t.Fatalf("expected Maybe[int] on top, got %v", c.stack.Top())
	}
}

func TestInstantiateTwoTypeVars(t *testing.T) {
	// Sig: ( T U -- {T: U} ) — pair-to-dict.
	c := newCheckerForUnify()
	tVar := TypeVarId(0)
	uVar := TypeVarId(1)
	tType := c.arena.MakeVar(tVar)
	uType := c.arena.MakeVar(uVar)
	dictTU := c.arena.MakeDict(tType, uType)
	sig := QuoteSig{
		Inputs:   []TypeId{tType, uType},
		Outputs:  []TypeId{dictTU},
		Generics: []TypeVarId{tVar, uVar},
	}
	c.stack.Push(TidStr)
	c.stack.Push(TidInt)
	c.applySig(sig, mkTok(LITERAL, "pairDict"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := c.arena.MakeDict(TidStr, TidInt)
	if c.stack.Top() != want {
		t.Fatalf("expected {str:int}, got %v", c.stack.Top())
	}
}

func TestInstantiateConstraintAcrossInputs(t *testing.T) {
	// Sig: ( T T -- T ) — both inputs must be the same type.
	c := newCheckerForUnify()
	tVar := TypeVarId(0)
	tType := c.arena.MakeVar(tVar)
	sig := QuoteSig{
		Inputs:   []TypeId{tType, tType},
		Outputs:  []TypeId{tType},
		Generics: []TypeVarId{tVar},
	}

	// Same types: ok.
	c.stack.Push(TidInt)
	c.stack.Push(TidInt)
	c.applySig(sig, mkTok(LITERAL, "same"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("(int int) should match (T T): %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("output should be int")
	}

	// Different types: error.
	c.stack.Reset()
	c.errors = nil
	c.stack.Push(TidInt)
	c.stack.Push(TidStr)
	c.applySig(sig, mkTok(LITERAL, "same"))
	if len(c.Errors()) == 0 {
		t.Fatalf("(int str) should not match (T T)")
	}
}

func TestApplyComposesQuote(t *testing.T) {
	// Apply on a quote whose inputs reference a bound var should rebuild
	// the quote with the var resolved.
	arena := NewTypeArena()
	var s Substitution
	v := s.FreshVar(arena)
	q := arena.MakeQuote(QuoteSig{
		Inputs:  []TypeId{v},
		Outputs: []TypeId{v},
	})
	if !s.Bind(arena, TypeVarId(arena.Node(v).A), TidInt) {
		t.Fatalf("Bind should succeed")
	}
	resolved := s.Apply(arena, q)
	want := arena.MakeQuote(QuoteSig{
		Inputs:  []TypeId{TidInt},
		Outputs: []TypeId{TidInt},
	})
	if resolved != want {
		t.Fatalf("Apply should rebuild quote with v resolved")
	}
}

func TestFormatTypeVar(t *testing.T) {
	arena := NewTypeArena()
	names := NewNameTable()
	v := arena.MakeVar(TypeVarId(7))
	got := FormatType(arena, names, v)
	if got != "T7" {
		t.Fatalf("expected T7, got %q", got)
	}
}
