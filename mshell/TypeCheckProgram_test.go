package main

import "testing"

// Phase 10 step 3 tests: TypeCheckProgram entry point.

func parseAndCheck(t *testing.T, src string) ([]string, bool) {
	t.Helper()
	l := NewLexer(src, nil)
	p := NewMShellParser(l)
	file, err := p.ParseFile()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return TypeCheckProgram(file)
}

func TestTypeCheckProgramEmpty(t *testing.T) {
	errs, ok := parseAndCheck(t, "")
	if !ok || len(errs) != 0 {
		t.Fatalf("empty program should pass; errs=%v ok=%v", errs, ok)
	}
}

func TestTypeCheckProgramValidTypeDecl(t *testing.T) {
	errs, ok := parseAndCheck(t, "type Result = int | str")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected no errors; errs=%v ok=%v", errs, ok)
	}
}

func TestTypeCheckProgramReservedTypeName(t *testing.T) {
	// `int`/`float`/`bool`/`str` are caught at parse time (they're token
	// types, not LITERALs). LITERAL-shaped reserved names (Maybe, Grid,
	// GridView, GridRow) reach the checker, which rejects them via
	// IsReservedTypeName.
	errs, ok := parseAndCheck(t, "type Maybe = int")
	if ok {
		t.Fatalf("expected failure for reserved type name 'Maybe'; errs=%v", errs)
	}
}

func TestTypeCheckProgramDuplicateTypeName(t *testing.T) {
	errs, ok := parseAndCheck(t, "type X = int  type X = str")
	if ok {
		t.Fatalf("expected failure for duplicate type name; errs=%v", errs)
	}
}

func TestTypeCheckProgramUnknownTypeInBody(t *testing.T) {
	errs, ok := parseAndCheck(t, "type X = Nope")
	if ok {
		t.Fatalf("expected failure for unknown type 'Nope' in body; errs=%v", errs)
	}
}

func TestTypeCheckProgramForwardRefAcrossDecls(t *testing.T) {
	// Decl order is preserved; B references A which is declared earlier.
	errs, ok := parseAndCheck(t, "type A = int  type B = A | str")
	if !ok {
		t.Fatalf("expected forward-ref to succeed; errs=%v", errs)
	}
}

func TestTypeCheckProgramAsCastTargetResolved(t *testing.T) {
	// Well-formed cast against a declared type.
	errs, ok := parseAndCheck(t, "type R = int  42 as R")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected clean check; errs=%v", errs)
	}
}

func TestTypeCheckProgramAsCastUnknownTarget(t *testing.T) {
	errs, ok := parseAndCheck(t, "42 as Nope")
	if ok {
		t.Fatalf("expected failure for unknown cast target; errs=%v", errs)
	}
}

func TestTypeCheckProgramAsCastInsideList(t *testing.T) {
	// The visitor recurses into composite parse items.
	errs, ok := parseAndCheck(t, "[42 as Nope]")
	if ok {
		t.Fatalf("expected failure for unknown cast target inside list; errs=%v", errs)
	}
}

func TestTypeCheckProgramArithmeticPasses(t *testing.T) {
	// Programs using only registered builtins (arithmetic, comparison)
	// pass the flow walker.
	errs, ok := parseAndCheck(t, "2 3 +")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected arithmetic to pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramArithmeticTypeMismatch(t *testing.T) {
	// `+` is registered as (int int -- int); int + str should fail.
	errs, ok := parseAndCheck(t, `2 "x" +`)
	if ok {
		t.Fatalf("expected type mismatch; errs=%v", errs)
	}
}

func TestTypeCheckProgramUnregisteredBuiltinFlagged(t *testing.T) {
	// Word builtins not yet in the table surface as unknown identifiers.
	// `gridCol` is one such (Grid-related ops haven't been registered).
	errs, ok := parseAndCheck(t, `42 gridCol`)
	if ok {
		t.Fatalf("expected unknown-identifier error for unregistered 'gridCol'; errs=%v", errs)
	}
}

func TestTypeCheckProgramRegisteredBuiltins(t *testing.T) {
	// Sanity: a small program using only registered builtins flow-checks.
	cases := []string{
		`42 wl`,
		`42 dup +`,
		`1 2 swap -`,
		`true not`,
		`42 str wl`,
		`42 dup drop wl`,
		`1 2 = wl`,
		`"hello" len wl`,
		`"a" "b" != wl`,
		`42 abs wl`,
		`42 toFloat wl`,
		`"42" toInt wl`,
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if !ok || len(errs) != 0 {
			t.Errorf("%q: expected pass; errs=%v", src, errs)
		}
	}
}

func TestTypeCheckProgramBuiltinTypeMismatches(t *testing.T) {
	cases := []struct {
		src    string
		reason string
	}{
		{`true not 5 +`, "bool + int via not output"},
		{`"hello" 1 +`, "str + int rejected"},
		{`true 1 =`, "bool = int (different types) rejected"},
	}
	for _, tc := range cases {
		errs, ok := parseAndCheck(t, tc.src)
		if ok {
			t.Errorf("%q (%s): expected failure; errs=%v", tc.src, tc.reason, errs)
		}
	}
}

func TestTypeCheckProgramAsCastDrivenAgainstStack(t *testing.T) {
	// 42 (int) cast to a Result = int|str should pass.
	errs, ok := parseAndCheck(t, "type Result = int | str  42 as Result")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected cast to pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramAsCastIncompatibleAgainstStack(t *testing.T) {
	// 42 (int) cast to a Bool brand should fail — int doesn't match the
	// underlying bool.
	errs, ok := parseAndCheck(t, "type Flag = bool  42 as Flag")
	if ok {
		t.Fatalf("expected invalid-cast error; errs=%v", errs)
	}
}

func TestTypeCheckProgramIfBlock(t *testing.T) {
	cases := []string{
		`true if "yes" wl else "no" wl end`,
		`1 2 < if "less" wl else "ge" wl end`,
		`true if "y" wl end`, // no else
		// else-if chain
		`1 if "one" wl else* 2 *if "two" wl else "other" wl end`,
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if !ok || len(errs) != 0 {
			t.Errorf("%q: expected pass; errs=%v", src, errs)
		}
	}
}

func TestTypeCheckProgramIfNonBoolCondition(t *testing.T) {
	// "hello" on top isn't bool/int.
	errs, ok := parseAndCheck(t, `"hello" if 1 wl else 2 wl end`)
	if ok {
		t.Fatalf("expected condition mismatch; errs=%v", errs)
	}
}

func TestTypeCheckProgramIfStackSizeMismatch(t *testing.T) {
	// True branch leaves 42 on the stack, false branch leaves nothing —
	// reconciliation rejects this.
	errs, ok := parseAndCheck(t, `true if 42 else end`)
	if ok {
		t.Fatalf("expected branch-size mismatch; errs=%v", errs)
	}
}

func TestTypeCheckProgramIfBranchTypeUnion(t *testing.T) {
	// Both branches push but with different types — the post-branch
	// stack slot becomes int|str. The rest of the program must be
	// compatible. `wl` accepts anything, so this should pass.
	errs, ok := parseAndCheck(t, `true if 42 else "hi" end wl`)
	if !ok || len(errs) != 0 {
		t.Errorf("expected pass via union; errs=%v", errs)
	}
}

func TestTypeCheckProgramVarStoreThenGetter(t *testing.T) {
	// A varstore captures the top of the stack into a name; a `:name`
	// getter pushes that type back. Use only registered ops so the
	// stack stays well-typed end-to-end.
	errs, ok := parseAndCheck(t, "2 n! :n 3 +")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected varstore + getter + arithmetic to pass; errs=%v", errs)
	}
}
