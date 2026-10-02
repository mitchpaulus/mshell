package main

import "testing"

// Tests of the type-expression parser and the checker's resolver.
//
// Each test lexes a source snippet representing only a type expression
// (no surrounding program), feeds the resulting tokens to parseTypeExpr,
// and inspects the resolved TypeId.

func parseTypeExprSrc(t *testing.T, c *coreChecker, src string) (TypeId, []TypeError) {
	t.Helper()
	l := NewLexer(src, nil)
	p := NewMShellParser(l)
	p.ensureInitialized()
	item, errs := p.parseTypeExpr()
	id := c.res.resolveType(item)
	// Resolution errors too, so tests of unknown types see them.
	errs = append(errs, c.res.errs...)
	c.res.errs = c.res.errs[:0]
	return id, errs
}

func newCheckerForTypeExpr(t *testing.T) *coreChecker {
	t.Helper()
	return NewCoreBase(nil, nil).newChecker()
}

func TestTypeExprPrimitives(t *testing.T) {
	cases := map[string]TypeId{
		"int":   TidInt,
		"float": TidFloat,
		"bool":  TidBool,
		"str":   TidStr,
		"bytes": TidBytes,
		"null":  TidNull,
	}
	for src, want := range cases {
		c := newCheckerForTypeExpr(t)
		got, errs := parseTypeExprSrc(t, c, src)
		if len(errs) != 0 {
			t.Errorf("%q: errors %+v", src, errs)
		}
		if got != want {
			t.Errorf("%q: got %d, want %d", src, got, want)
		}
	}
}

// `none` is the empty constructor of Maybe, not a type. Naming it in a
// type expression must be rejected (callers should use Maybe[T] or null).
func TestTypeExprNoneIsNotAType(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	_, errs := parseTypeExprSrc(t, c, "none")
	if len(errs) == 0 {
		t.Fatalf("expected an error for 'none' used as a type, got none")
	}
}

func TestTypeExprList(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "[int]")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeList(TidInt)
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprNestedList(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, _ := parseTypeExprSrc(t, c, "[[str]]")
	want := c.arena.MakeList(c.arena.MakeList(TidStr))
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprMaybe(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "Maybe[int]")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeMaybeEnum(TidInt)
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprMaybeMissingArg(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	_, errs := parseTypeExprSrc(t, c, "Maybe")
	if len(errs) != 1 || errs[0].Kind != TErrTypeParse {
		t.Fatalf("expected one parse error, got %+v", errs)
	}
}

func TestTypeExprDict(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "{str: int}")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeStrDict(TidInt)
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprShape(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "{a: int, b: str}")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeRecord([]RecordField{
		{Name: c.names.Intern("a"), Status: FieldRequired, Type: TidInt},
		{Name: c.names.Intern("b"), Status: FieldRequired, Type: TidStr},
	}, RecordField{Status: FieldOpen})
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprEmptyShape(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "{}")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeRecord(nil, RecordField{Status: FieldOpen})
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprShapeDuplicateFieldRejected(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	_, errs := parseTypeExprSrc(t, c, "{a: int, a: str}")
	if len(errs) == 0 {
		t.Fatalf("expected duplicate-field error")
	}
}

func TestTypeExprDictRejectsMultiplePairs(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	_, errs := parseTypeExprSrc(t, c, "{str: int, str: bool}")
	if len(errs) == 0 {
		t.Fatalf("expected error for multi-pair dict")
	}
}

func TestTypeExprUnion(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "int | str")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr})
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprUnionThreeArms(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, _ := parseTypeExprSrc(t, c, "int | str | bool")
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr, TidBool})
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprQuote(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "(int int -- int)")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeQuote(QuoteSig{
		Inputs:  []TypeId{TidInt, TidInt},
		Outputs: []TypeId{TidInt},
	})
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprQuoteEmpty(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, errs := parseTypeExprSrc(t, c, "( -- )")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	want := c.arena.MakeQuote(QuoteSig{})
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprQuoteMissingDoubledash(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	_, errs := parseTypeExprSrc(t, c, "(int)")
	if len(errs) == 0 {
		t.Fatalf("expected error for missing '--'")
	}
}

func TestTypeExprGridFamily(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	for _, name := range []string{"Grid", "GridView", "GridRow"} {
		got, errs := parseTypeExprSrc(t, c, name)
		if len(errs) != 0 {
			t.Fatalf("%s: errs %+v", name, errs)
		}
		k := c.arena.Kind(got)
		want := map[string]TypeKind{"Grid": TKGrid, "GridView": TKGridView, "GridRow": TKGridRow}[name]
		if k != want {
			t.Fatalf("%s: kind %s, want %s", name, k, want)
		}
	}
}

func TestTypeExprUserDeclaredType(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	file, err := NewMShellParser(NewLexer("type Result = int | str", nil)).ParseFile()
	if err != nil {
		t.Fatal(err)
	}
	c.declareAll(file.Items, map[string]Token{})
	declared, ok := c.res.aliases[c.names.Intern("Result")]
	if !ok || len(c.errs) > 0 {
		t.Fatalf("declaring Result failed: %+v", c.errs)
	}
	got, errs := parseTypeExprSrc(t, c, "Result")
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	if got != declared {
		t.Fatalf("got %d, want %d (declared)", got, declared)
	}
}

func TestTypeExprUnknownIdentifierErrors(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	_, errs := parseTypeExprSrc(t, c, "Nope")
	if len(errs) != 1 || errs[0].Kind != TErrTypeParse {
		t.Fatalf("expected unknown-type error, got %+v", errs)
	}
}

func TestTypeExprListOfMaybes(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, _ := parseTypeExprSrc(t, c, "[Maybe[int]]")
	want := c.arena.MakeList(c.arena.MakeMaybeEnum(TidInt))
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprUnionInList(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, _ := parseTypeExprSrc(t, c, "[int | str]")
	want := c.arena.MakeList(c.arena.MakeUnion([]TypeId{TidInt, TidStr}))
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprConsumedCount(t *testing.T) {
	// After parsing one type expression, the next token should remain
	// available on the parser so the surrounding program can continue.
	c := newCheckerForTypeExpr(t)
	l := NewLexer("int extra", nil)
	p := NewMShellParser(l)
	p.ensureInitialized()
	item, errs := p.parseTypeExpr()
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	if id := c.res.resolveType(item); id != TidInt {
		t.Fatalf("id %d, want TidInt", id)
	}
	if p.curr.Type != LITERAL || p.curr.Lexeme != "extra" {
		t.Fatalf("next token = %v, want LITERAL 'extra'", p.curr)
	}
}
