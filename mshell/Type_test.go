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
	maybeInt := a.MakeMaybeEnum(TidInt)
	if maybeInt == listInt1 {
		t.Errorf("Maybe<Int> and List<Int> share id %d", listInt1)
	}
}

func TestHashconsNested(t *testing.T) {
	a := NewTypeArena()
	a1 := a.MakeMaybeEnum(a.MakeList(TidInt))
	a2 := a.MakeMaybeEnum(a.MakeList(TidInt))
	if a1 != a2 {
		t.Errorf("Maybe<List<Int>> not hashconsed: %d vs %d", a1, a2)
	}
}

func TestHashconsDict(t *testing.T) {
	a := NewTypeArena()
	d1 := a.MakeStrDict(TidInt)
	d2 := a.MakeStrDict(TidInt)
	if d1 != d2 {
		t.Errorf("{str: int} not hashconsed")
	}
	if d1 == a.MakeStrDict(TidStr) {
		t.Errorf("{str: int} and {str: str} share id")
	}
}

func TestShapeNormalization(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	nName := names.Intern("name")
	aName := names.Intern("age")

	// Two equivalent shapes specified in different field orders.
	open := RecordField{Status: FieldOpen}
	s1 := a.MakeRecord([]RecordField{
		{Name: nName, Status: FieldRequired, Type: TidStr},
		{Name: aName, Status: FieldRequired, Type: TidInt},
	}, open)
	s2 := a.MakeRecord([]RecordField{
		{Name: aName, Status: FieldRequired, Type: TidInt},
		{Name: nName, Status: FieldRequired, Type: TidStr},
	}, open)
	if s1 != s2 {
		t.Errorf("equivalent shapes not hashconsed: %d vs %d", s1, s2)
	}
}

func TestShapeDistinct(t *testing.T) {
	a := NewTypeArena()
	names := NewNameTable()
	nName := names.Intern("name")
	aName := names.Intern("age")

	open := RecordField{Status: FieldOpen}
	s1 := a.MakeRecord([]RecordField{
		{Name: nName, Status: FieldRequired, Type: TidStr},
		{Name: aName, Status: FieldRequired, Type: TidInt},
	}, open)
	// Different field type should yield a different id.
	s2 := a.MakeRecord([]RecordField{
		{Name: nName, Status: FieldRequired, Type: TidStr},
		{Name: aName, Status: FieldRequired, Type: TidFloat},
	}, open)
	if s1 == s2 {
		t.Errorf("shapes with different field types share id %d", s1)
	}
}

func TestUnionFlatten(t *testing.T) {
	a := NewTypeArena()
	// Build int|str
	u1 := a.MakeUnion([]TypeId{TidInt, TidStr})
	// Build (int|str)|float -- should flatten
	u2 := a.MakeUnion([]TypeId{u1, TidFloat})
	// And a direct int|float|str should match u2
	u3 := a.MakeUnion([]TypeId{TidInt, TidFloat, TidStr})
	if u2 != u3 {
		t.Errorf("flattened union not hashconsed with direct: %d vs %d", u2, u3)
	}
}

func TestUnionDedupe(t *testing.T) {
	a := NewTypeArena()
	u1 := a.MakeUnion([]TypeId{TidInt, TidInt, TidStr})
	u2 := a.MakeUnion([]TypeId{TidInt, TidStr})
	if u1 != u2 {
		t.Errorf("union with duplicate not deduped: %d vs %d", u1, u2)
	}
}

func TestUnionSingleArmCollapse(t *testing.T) {
	a := NewTypeArena()
	// A union of one arm collapses to that arm.
	u := a.MakeUnion([]TypeId{TidInt})
	if u != TidInt {
		t.Errorf("single-arm union did not collapse: got %d, want %d", u, TidInt)
	}
}

func TestUnionOrderInvariant(t *testing.T) {
	a := NewTypeArena()
	u1 := a.MakeUnion([]TypeId{TidInt, TidStr, TidFloat})
	u2 := a.MakeUnion([]TypeId{TidFloat, TidStr, TidInt})
	if u1 != u2 {
		t.Errorf("union order not canonicalized: %d vs %d", u1, u2)
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
	unknown := a.MakeRecord(nil, RecordField{Status: FieldOpen})
	g1 := a.MakeGridOf(TKGrid, unknown)
	g2 := a.MakeGridOf(TKGrid, unknown)
	if g1 != g2 {
		t.Errorf("Grid (unknown schema) not hashconsed")
	}
	// Grid vs GridView vs GridRow distinct
	gv := a.MakeGridOf(TKGridView, unknown)
	gr := a.MakeGridOf(TKGridRow, unknown)
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

// An overlay grows on its own and leaves its base unchanged: a check
// interns names without copying the base's.
func TestNameTableOverlay(t *testing.T) {
	base := NewNameTable()
	foo := base.Intern("foo")
	o := base.Overlay()
	if o.Intern("foo") != foo {
		t.Errorf("an overlay should find its base's names")
	}
	bar := o.Intern("bar")
	if bar < base.Len() || o.Name(bar) != "bar" || o.Name(foo) != "foo" {
		t.Errorf("overlay names: bar=%d (base len %d) %q %q", bar, base.Len(), o.Name(bar), o.Name(foo))
	}
	if _, ok := base.Lookup("bar"); ok {
		t.Errorf("an overlay must not change its base")
	}
	if o2 := base.Overlay(); o2.Intern("baz") != bar {
		t.Errorf("two overlays of one base should number their names alike")
	}
}
