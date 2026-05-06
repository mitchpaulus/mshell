package main

import "testing"

// Phase 10 step 1 tests: type-expression parser.
//
// Each test lexes a source snippet representing only a type expression
// (no surrounding program), feeds the resulting tokens to ParseTypeExpr,
// and inspects the produced TypeId.

func parseTypeExprSrc(t *testing.T, c *Checker, src string) (TypeId, []TypeError) {
	t.Helper()
	l := NewLexer(src, nil)
	rawToks, err := l.Tokenize()
	if err != nil {
		t.Fatalf("lex %q: %v", src, err)
	}
	// Strip whitespace/comments/EOF — the type-expr parser doesn't expect them.
	var toks []Token
	for _, tk := range rawToks {
		if tk.Type == WHITESPACE || tk.Type == LINECOMMENT || tk.Type == EOF {
			continue
		}
		toks = append(toks, tk)
	}
	id, _, errs := ParseTypeExpr(c, toks)
	return id, errs
}

func newCheckerForTypeExpr(t *testing.T) *Checker {
	t.Helper()
	return NewChecker(NewTypeArena(), NewNameTable())
}

func TestTypeExprPrimitives(t *testing.T) {
	cases := map[string]TypeId{
		"int":   TidInt,
		"float": TidFloat,
		"bool":  TidBool,
		"str":   TidStr,
		"bytes": TidBytes,
		"none":  TidNone,
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
	want := c.arena.MakeMaybe(TidInt)
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
	want := c.arena.MakeDict(TidStr, TidInt)
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
	want := c.arena.MakeShape([]ShapeField{
		{Name: c.names.Intern("a"), Type: TidInt},
		{Name: c.names.Intern("b"), Type: TidStr},
	})
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
	want := c.arena.MakeShape(nil)
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
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprUnionThreeArms(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, _ := parseTypeExprSrc(t, c, "int | str | bool")
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr, TidBool}, NameNone)
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
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	declared, ok := c.DeclareType("Result", body)
	if !ok {
		t.Fatalf("DeclareType failed: %+v", c.Errors())
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
	want := c.arena.MakeList(c.arena.MakeMaybe(TidInt))
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprUnionInList(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	got, _ := parseTypeExprSrc(t, c, "[int | str]")
	want := c.arena.MakeList(c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone))
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestTypeExprConsumedCount(t *testing.T) {
	c := newCheckerForTypeExpr(t)
	src := "int extra"
	l := NewLexer(src, nil)
	rawToks, _ := l.Tokenize()
	var toks []Token
	for _, tk := range rawToks {
		if tk.Type == WHITESPACE || tk.Type == LINECOMMENT || tk.Type == EOF {
			continue
		}
		toks = append(toks, tk)
	}
	id, consumed, errs := ParseTypeExpr(c, toks)
	if len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	if id != TidInt {
		t.Fatalf("id %d, want TidInt", id)
	}
	if consumed != 1 {
		t.Fatalf("consumed %d, want 1 (the 'extra' literal should be left)", consumed)
	}
}
