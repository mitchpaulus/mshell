package main

import "testing"

// Tests of type variables, the substitution, and equality unification.

// unifyHarness is the substitution and unifier a check uses.
type unifyHarness struct {
	arena *TypeArena
	names *NameTable
	subst *Substitution
	uni   *Unifier
}

func (h *unifyHarness) unify(a, b TypeId) bool { return h.uni.Unify(a, b) }

func newCheckerForUnify() *unifyHarness {
	arena := NewTypeArena()
	h := &unifyHarness{arena: arena, names: NewNameTable(), subst: &Substitution{}}
	h.uni = NewUnifier(arena, h.subst, NewRelations(arena))
	return h
}

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
