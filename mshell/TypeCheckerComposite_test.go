package main

import "testing"

// Phase-3 unify tests. These build composite TypeIds directly via the
// arena and call Checker.unify, since the parser-driven path doesn't land
// until Phase 10.

func newCheckerForUnify() *Checker {
	return NewChecker(NewTypeArena(), NewNameTable())
}

func TestUnifyListEqual(t *testing.T) {
	c := newCheckerForUnify()
	a := c.arena.MakeList(TidInt)
	b := c.arena.MakeList(TidInt)
	if a != b {
		t.Fatalf("hashconsing should give equal ids: %v vs %v", a, b)
	}
	if !c.unify(a, b) {
		t.Fatalf("equal list ids should unify")
	}
}

func TestUnifyListMismatch(t *testing.T) {
	c := newCheckerForUnify()
	li := c.arena.MakeList(TidInt)
	ls := c.arena.MakeList(TidStr)
	if c.unify(li, ls) {
		t.Fatalf("[int] vs [str] must not unify")
	}
}

func TestUnifyListVsDict(t *testing.T) {
	c := newCheckerForUnify()
	li := c.arena.MakeList(TidInt)
	di := c.arena.MakeDict(TidStr, TidInt)
	if c.unify(li, di) {
		t.Fatalf("[int] vs {str:int} must not unify")
	}
}

func TestUnifyMaybeRecurses(t *testing.T) {
	c := newCheckerForUnify()
	a := c.arena.MakeMaybe(TidInt)
	b := c.arena.MakeMaybe(TidStr)
	if c.unify(a, b) {
		t.Fatalf("Maybe[int] vs Maybe[str] must not unify")
	}
}

func TestUnifyShapeWidthSubtyping(t *testing.T) {
	c := newCheckerForUnify()
	nA := c.names.Intern("a")
	nB := c.names.Intern("b")
	nC := c.names.Intern("c")
	got := c.arena.MakeShape([]ShapeField{
		{Name: nA, Type: TidInt},
		{Name: nB, Type: TidStr},
		{Name: nC, Type: TidBool},
	})
	want := c.arena.MakeShape([]ShapeField{
		{Name: nA, Type: TidInt},
		{Name: nC, Type: TidBool},
	})
	if !c.unify(got, want) {
		t.Fatalf("shape with extra field should width-subtype into smaller shape")
	}
	// Reverse direction is not allowed: missing required field.
	if c.unify(want, got) {
		t.Fatalf("smaller shape must not unify into larger required shape")
	}
}

func TestUnifyShapeFieldTypeMismatch(t *testing.T) {
	c := newCheckerForUnify()
	n := c.names.Intern("a")
	got := c.arena.MakeShape([]ShapeField{{Name: n, Type: TidInt}})
	want := c.arena.MakeShape([]ShapeField{{Name: n, Type: TidStr}})
	if c.unify(got, want) {
		t.Fatalf("shape with same field name but wrong type must not unify")
	}
}

func TestUnifyUnionSubset(t *testing.T) {
	c := newCheckerForUnify()
	got := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, 0)
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr, TidBool}, 0)
	if !c.unify(got, want) {
		t.Fatalf("subset union must unify into superset union")
	}
	if c.unify(want, got) {
		t.Fatalf("superset union must not unify into subset")
	}
}

func TestUnifyBrandedUnionDistinct(t *testing.T) {
	c := newCheckerForUnify()
	bA := c.names.Intern("A")
	bB := c.names.Intern("B")
	uA := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, bA)
	uB := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, bB)
	if uA == uB {
		t.Fatalf("differently-branded unions must have distinct ids")
	}
	if c.unify(uA, uB) {
		t.Fatalf("differently-branded unions must not unify")
	}
}

func TestUnifyBrandNominal(t *testing.T) {
	c := newCheckerForUnify()
	bA := c.names.Intern("A")
	bB := c.names.Intern("B")
	tA := c.arena.MakeBrand(bA, TidInt)
	tB := c.arena.MakeBrand(bB, TidInt)
	if c.unify(tA, tB) {
		t.Fatalf("brand A(int) and brand B(int) must not unify (nominal)")
	}
	if !c.unify(tA, tA) {
		t.Fatalf("brand should unify with itself")
	}
	// Brand does not implicitly look through to its underlying type.
	if c.unify(tA, TidInt) {
		t.Fatalf("brand A(int) must not unify with bare int — explicit cast required")
	}
}

func TestUnifyQuote(t *testing.T) {
	c := newCheckerForUnify()
	a := c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}})
	b := c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}})
	if a != b {
		t.Fatalf("hashconsing should give equal quote ids")
	}
	d := c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}})
	if c.unify(a, d) {
		t.Fatalf("quotes with different arity must not unify")
	}
	e := c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{TidInt, TidStr}, Outputs: []TypeId{TidInt}})
	if c.unify(a, e) {
		t.Fatalf("quotes with different input types must not unify")
	}
}

func TestUnifyOpaqueGrid(t *testing.T) {
	c := newCheckerForUnify()
	g1 := c.arena.MakeGrid(0)
	g2 := c.arena.MakeGrid(0)
	if g1 != g2 {
		t.Fatalf("two unknown-schema grids should hashcons to one id")
	}
	if !c.unify(g1, g2) {
		t.Fatalf("opaque grids should unify")
	}
	gv := c.arena.MakeGridView(0)
	if c.unify(g1, gv) {
		t.Fatalf("Grid and GridView are distinct kinds")
	}
}

func TestUnifyBottom(t *testing.T) {
	c := newCheckerForUnify()
	li := c.arena.MakeList(TidInt)
	if !c.unify(TidBottom, li) {
		t.Fatalf("TidBottom should unify with anything")
	}
	if !c.unify(li, TidBottom) {
		t.Fatalf("anything should accept TidBottom")
	}
}

func TestApplySigCompositeInputs(t *testing.T) {
	// Construct an artificial sig that consumes [int] and produces int,
	// then push a [int] from a literal-style construction and verify
	// applySig accepts it.
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	listInt := arena.MakeList(TidInt)
	sig := QuoteSig{Inputs: []TypeId{listInt}, Outputs: []TypeId{TidInt}}
	c.stack.Push(listInt)
	c.applySig(sig, Token{Line: 1, Column: 1, Lexeme: "len", Type: LITERAL})
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("expected int on top, got len=%d top=%v", c.stack.Len(), c.stack.Top())
	}
}

func TestApplySigCompositeMismatch(t *testing.T) {
	arena := NewTypeArena()
	names := NewNameTable()
	c := NewChecker(arena, names)
	listInt := arena.MakeList(TidInt)
	listStr := arena.MakeList(TidStr)
	sig := QuoteSig{Inputs: []TypeId{listInt}, Outputs: []TypeId{TidInt}}
	c.stack.Push(listStr)
	c.applySig(sig, Token{Line: 1, Column: 1, Lexeme: "len", Type: LITERAL})
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrTypeMismatch {
		t.Fatalf("expected single type-mismatch error, got %+v", errs)
	}
	// Format should mention both [int] and [str].
	msg := errs[0].Format(arena, names)
	if !contains(msg, "[int]") || !contains(msg, "[str]") {
		t.Fatalf("error message should mention list types: %q", msg)
	}
}

func TestFormatTypeComposites(t *testing.T) {
	arena := NewTypeArena()
	names := NewNameTable()
	cases := []struct {
		name string
		make func() TypeId
		want string
	}{
		{"list", func() TypeId { return arena.MakeList(TidInt) }, "[int]"},
		{"maybe", func() TypeId { return arena.MakeMaybe(TidStr) }, "Maybe[str]"},
		{"dict", func() TypeId { return arena.MakeDict(TidStr, TidInt) }, "{str: int}"},
		{"union", func() TypeId { return arena.MakeUnion([]TypeId{TidInt, TidStr}, 0) }, "int | str"},
		{"branded-union", func() TypeId {
			return arena.MakeUnion([]TypeId{TidInt, TidStr}, names.Intern("Mine"))
		}, "Mine(int | str)"},
		{"brand", func() TypeId { return arena.MakeBrand(names.Intern("UserId"), TidInt) }, "UserId(int)"},
		{"grid", func() TypeId { return arena.MakeGrid(0) }, "Grid"},
		{"quote", func() TypeId {
			return arena.MakeQuote(QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidStr}})
		}, "(int -- str)"},
	}
	for _, tc := range cases {
		got := FormatType(arena, names, tc.make())
		if got != tc.want {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}
