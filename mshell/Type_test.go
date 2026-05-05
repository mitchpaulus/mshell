package main

import "testing"

func TestArenaPrimitivesAreFixed(t *testing.T) {
	a := NewTypeArena()
	if a.Kind(TidBool) != TKPrim {
		t.Errorf("TidBool kind = %v, want TKPrim", a.Kind(TidBool))
	}
	if a.Kind(TidInt) != TKPrim {
		t.Errorf("TidInt kind = %v, want TKPrim", a.Kind(TidInt))
	}
	if a.Kind(TidBottom) != TKPrim {
		t.Errorf("TidBottom kind = %v, want TKPrim", a.Kind(TidBottom))
	}
	// TidNothing is the sentinel and lives at index 0.
	if TidNothing != 0 {
		t.Errorf("TidNothing = %d, want 0", TidNothing)
	}
}

func TestHashconsAtomic(t *testing.T) {
	a := NewTypeArena()
	listInt1 := a.MakeList(TidInt)
	listInt2 := a.MakeList(TidInt)
	if listInt1 != listInt2 {
		t.Errorf("List<Int> not hashconsed: %d vs %d", listInt1, listInt2)
	}
	listStr := a.MakeList(TidStr)
	if listStr == listInt1 {
		t.Errorf("List<Int> and List<Str> share id %d", listInt1)
	}
	maybeInt := a.MakeMaybe(TidInt)
	if maybeInt == listInt1 {
		t.Errorf("Maybe<Int> and List<Int> share id %d", listInt1)
	}
}

func TestHashconsNested(t *testing.T) {
	a := NewTypeArena()
	a1 := a.MakeMaybe(a.MakeList(TidInt))
	a2 := a.MakeMaybe(a.MakeList(TidInt))
	if a1 != a2 {
		t.Errorf("Maybe<List<Int>> not hashconsed: %d vs %d", a1, a2)
	}
}

func TestHashconsDict(t *testing.T) {
	a := NewTypeArena()
	d1 := a.MakeDict(TidStr, TidInt)
	d2 := a.MakeDict(TidStr, TidInt)
	if d1 != d2 {
		t.Errorf("Dict<Str,Int> not hashconsed")
	}
	// Order matters: Dict<Str,Int> != Dict<Int,Str>
	d3 := a.MakeDict(TidInt, TidStr)
	if d1 == d3 {
		t.Errorf("Dict<Str,Int> and Dict<Int,Str> share id")
	}
}

func TestShapeNormalization(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	nName := names.Intern("name")
	aName := names.Intern("age")

	// Two equivalent shapes specified in different field orders.
	s1 := a.MakeShape([]ShapeField{
		{Name: nName, Type: TidStr},
		{Name: aName, Type: TidInt},
	})
	s2 := a.MakeShape([]ShapeField{
		{Name: aName, Type: TidInt},
		{Name: nName, Type: TidStr},
	})
	if s1 != s2 {
		t.Errorf("equivalent shapes not hashconsed: %d vs %d", s1, s2)
	}
}

func TestShapeDistinct(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	nName := names.Intern("name")
	aName := names.Intern("age")

	s1 := a.MakeShape([]ShapeField{
		{Name: nName, Type: TidStr},
		{Name: aName, Type: TidInt},
	})
	// Different field type should yield a different id.
	s2 := a.MakeShape([]ShapeField{
		{Name: nName, Type: TidStr},
		{Name: aName, Type: TidFloat},
	})
	if s1 == s2 {
		t.Errorf("shapes with different field types share id %d", s1)
	}
}

func TestUnionFlatten(t *testing.T) {
	a := NewTypeArena()
	// Build int|str
	u1 := a.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	// Build (int|str)|float -- should flatten
	u2 := a.MakeUnion([]TypeId{u1, TidFloat}, NameNone)
	// And a direct int|float|str should match u2
	u3 := a.MakeUnion([]TypeId{TidInt, TidFloat, TidStr}, NameNone)
	if u2 != u3 {
		t.Errorf("flattened union not hashconsed with direct: %d vs %d", u2, u3)
	}
}

func TestUnionDedupe(t *testing.T) {
	a := NewTypeArena()
	u1 := a.MakeUnion([]TypeId{TidInt, TidInt, TidStr}, NameNone)
	u2 := a.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	if u1 != u2 {
		t.Errorf("union with duplicate not deduped: %d vs %d", u1, u2)
	}
}

func TestUnionSingleArmCollapse(t *testing.T) {
	a := NewTypeArena()
	// Unbranded union of one arm should collapse to that arm.
	u := a.MakeUnion([]TypeId{TidInt}, NameNone)
	if u != TidInt {
		t.Errorf("single-arm unbranded union didn't collapse: got %d, want %d", u, TidInt)
	}
}

func TestUnionOrderInvariant(t *testing.T) {
	a := NewTypeArena()
	u1 := a.MakeUnion([]TypeId{TidInt, TidStr, TidFloat}, NameNone)
	u2 := a.MakeUnion([]TypeId{TidFloat, TidStr, TidInt}, NameNone)
	if u1 != u2 {
		t.Errorf("union order not canonicalized: %d vs %d", u1, u2)
	}
}

func TestBrandedUnionDistinctFromUnbranded(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	rId := names.Intern("Result")
	plain := a.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	branded := a.MakeUnion([]TypeId{TidInt, TidStr}, rId)
	if plain == branded {
		t.Errorf("branded and unbranded union share id %d", plain)
	}
}

func TestBrandedUnionsDistinctByBrand(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	r := names.Intern("Result")
	e := names.Intern("Either")
	a1 := a.MakeUnion([]TypeId{TidInt, TidStr}, r)
	a2 := a.MakeUnion([]TypeId{TidInt, TidStr}, e)
	if a1 == a2 {
		t.Errorf("Result|Int|Str and Either|Int|Str share id %d", a1)
	}
}

func TestBrandedUnionWithSingleArmDoesNotCollapse(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	bId := names.Intern("UserId")
	branded := a.MakeUnion([]TypeId{TidInt}, bId)
	if branded == TidInt {
		t.Errorf("branded single-arm union collapsed to underlying")
	}
}

func TestQuoteHashcons(t *testing.T) {
	a := NewTypeArena()
	q1 := a.MakeQuote(QuoteSig{
		Inputs:  []TypeId{TidInt, TidInt},
		Outputs: []TypeId{TidInt},
	})
	q2 := a.MakeQuote(QuoteSig{
		Inputs:  []TypeId{TidInt, TidInt},
		Outputs: []TypeId{TidInt},
	})
	if q1 != q2 {
		t.Errorf("identical quotes not hashconsed: %d vs %d", q1, q2)
	}
	q3 := a.MakeQuote(QuoteSig{
		Inputs:  []TypeId{TidInt},
		Outputs: []TypeId{TidInt, TidInt},
	})
	if q1 == q3 {
		t.Errorf("quotes with different in/out share id")
	}
}

func TestGridUnknownSchemaCanonical(t *testing.T) {
	a := NewTypeArena()
	g1 := a.MakeGrid(0)
	g2 := a.MakeGrid(0)
	if g1 != g2 {
		t.Errorf("Grid (unknown schema) not hashconsed")
	}
	// Grid vs GridView vs GridRow distinct
	gv := a.MakeGridView(0)
	gr := a.MakeGridRow(0)
	if g1 == gv || g1 == gr || gv == gr {
		t.Errorf("Grid family kinds collide at unknown schema")
	}
}

func TestVarFresh(t *testing.T) {
	a := NewTypeArena()
	v0 := a.MakeVar(0)
	v1 := a.MakeVar(1)
	v0again := a.MakeVar(0)
	if v0 != v0again {
		t.Errorf("MakeVar(0) not stable across calls")
	}
	if v0 == v1 {
		t.Errorf("MakeVar(0) and MakeVar(1) share id")
	}
}

func TestNameTable(t *testing.T) {
	tab := NewNameTable()
	if tab.Intern("") != NameNone {
		t.Errorf("empty name not mapped to NameNone")
	}
	a1 := tab.Intern("foo")
	a2 := tab.Intern("foo")
	if a1 != a2 {
		t.Errorf("intern not idempotent")
	}
	b := tab.Intern("bar")
	if a1 == b {
		t.Errorf("distinct names share id")
	}
	if tab.Name(a1) != "foo" {
		t.Errorf("name round-trip failed: %q", tab.Name(a1))
	}
}

func TestReservedTypeNames(t *testing.T) {
	for _, name := range []string{"int", "float", "str", "bool", "bytes", "none",
		"Maybe", "Grid", "GridView", "GridRow"} {
		if !IsReservedTypeName(name) {
			t.Errorf("expected %q to be reserved", name)
		}
	}
	for _, name := range []string{"Result", "Person", "x", "MyType"} {
		if IsReservedTypeName(name) {
			t.Errorf("expected %q to NOT be reserved", name)
		}
	}
}
