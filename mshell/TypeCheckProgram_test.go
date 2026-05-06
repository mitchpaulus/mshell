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
	// Until the builtin table grows, word builtins like `wl` surface as
	// unknown identifiers under --check-types. That's the expected
	// signal for what to register next.
	errs, ok := parseAndCheck(t, `42 wl`)
	if ok {
		t.Fatalf("expected unknown-identifier error for unregistered 'wl'; errs=%v", errs)
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

func TestTypeCheckProgramVarStoreThenGetter(t *testing.T) {
	// A varstore captures the top of the stack into a name; a `:name`
	// getter pushes that type back. Use only registered ops so the
	// stack stays well-typed end-to-end.
	errs, ok := parseAndCheck(t, "2 n! :n 3 +")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected varstore + getter + arithmetic to pass; errs=%v", errs)
	}
}
