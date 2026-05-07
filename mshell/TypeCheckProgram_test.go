package main

import (
	"strings"
	"testing"
)

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

func TestTypeCheckProgramFloatArithmetic(t *testing.T) {
	cases := []string{
		`1.5 2.0 + wl`,
		`1.0 2.0 / wl`,
		`1.5 2.0 < wl`,
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if !ok || len(errs) != 0 {
			t.Errorf("%q: expected pass; errs=%v", src, errs)
		}
	}
}

func TestTypeCheckProgramStringOps(t *testing.T) {
	cases := []string{
		`"hello world" wsplit drop`,
		`["a" "b"] "," join wl`,
		`"a,b,c" "," split drop`,
		`"hello\nworld" lines drop`,
		`["a" "b"] unlines wl`,
		`" hi " trim wl`,
		`"hi" upper wl`,
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if !ok || len(errs) != 0 {
			t.Errorf("%q: expected pass; errs=%v", src, errs)
		}
	}
}

func TestTypeCheckProgramMaybeMap(t *testing.T) {
	src := `5 just (1 +) map drop`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected Maybe map to type-check; errs=%v", errs)
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

func TestTypeCheckProgramDefRegisteredAtCallSite(t *testing.T) {
	// User-defined function `inc (int -- int)` should make `5 inc`
	// type-check cleanly.
	src := `
def inc (int -- int)
    1 +
end
5 inc wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected clean check; errs=%v", errs)
	}
}

func TestTypeCheckProgramDefCallSiteTypeMismatch(t *testing.T) {
	src := `
def inc (int -- int)
    1 +
end
"hi" inc wl
`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected type-mismatch at call site; errs=%v", errs)
	}
}

func TestTypeCheckProgramDefGenericIdentity(t *testing.T) {
	// id (T -- T): polymorphic, should accept int or str at separate
	// call sites.
	src := `
def id (T -- T)
end
5 id wl
"x" id wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected polymorphic id to pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramDefList(t *testing.T) {
	// listLen consumes a list and produces an int — body uses the
	// registered `len` builtin so the body type-checks.
	src := `
def listLen ([T] -- int)
    len
end
[1 2 3] listLen wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramDefBodyArityMismatch(t *testing.T) {
	// declared (int -- int) but body produces (int int) on the stack.
	src := `
def bad (int -- int)
    dup
end
`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected body arity error; errs=%v", errs)
	}
}

func TestTypeCheckProgramDefBodyTypeError(t *testing.T) {
	// Body has a flow-level type error (can't add bool to int).
	src := `
def bad (int -- int)
    true +
end
`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected body type error; errs=%v", errs)
	}
}

func TestTypeCheckProgramDefRecursiveCallChecks(t *testing.T) {
	// Recursive self-call resolves through nameBuiltins (registered
	// in pre-pass 2 before bodies are checked).
	src := `
def rec (int -- int)
    rec
end
5 rec wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected recursive sig to type-check; errs=%v", errs)
	}
}

func TestTypeCheckProgramQuoteLiteralInferred(t *testing.T) {
	// `(2 +)` infers as (int -- int). Calling it requires a quote-
	// consumer; for this test we just push and drop to verify the
	// quote literal alone doesn't error.
	src := `(2 +) drop`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramQuoteWithBodyTypeError(t *testing.T) {
	// Body has a type error inside the quote — should surface
	// during inference.
	src := `(true 1 +) drop`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected body error to surface; errs=%v", errs)
	}
}

func TestTypeCheckProgramHigherOrderBuiltins(t *testing.T) {
	// map : ([T] (T -- U) -- [U]). Quote-body inference produces
	// the (int -- int) sig from `(1 +)`, which then unifies with
	// map's quote-input slot.
	cases := []string{
		`[1 2 3] (1 +) map drop`,
		`[1 2 3] (0 >) filter drop`,
		`[1 2 3] (wl) each`,
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if !ok || len(errs) != 0 {
			t.Errorf("%q: expected pass; errs=%v", src, errs)
		}
	}
}

func TestTypeCheckProgramMapFilterTypeMismatch(t *testing.T) {
	// filter wants (T -- bool); the quote produces an int instead.
	src := `[1 2 3] (1 +) filter drop`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected mismatch; errs=%v", errs)
	}
}

func TestTypeCheckProgramMatchValueArms(t *testing.T) {
	// All arms consume the subject (`:`) and produce nothing on the
	// stack. Reconciliation passes (all arms agree).
	src := `
"hello" match
    "hello" : "greeting" wl,
    "bye"   : "farewell" wl,
    _       : "unknown" wl,
end
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramMatchTypeArms(t *testing.T) {
	src := `
42 match
    int : "integer" wl,
    str : "string" wl,
    _   : "other" wl,
end
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramMatchPreserveSubject(t *testing.T) {
	// `:>` keeps the subject on the stack for the body. Each arm
	// produces a string on top, so the post-match stack is
	// [subject, str].
	src := `
42 match
    int :> str wl,
    _   :> str wl,
end
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramMatchJustBinding(t *testing.T) {
	// just v binds v to the inner of Maybe[T]. Inside the arm,
	// `:v` retrieves an int.
	src := `
5 just match
    just v : :v wl,
    none : "nothing" wl,
end
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramMatchArmStackMismatch(t *testing.T) {
	// One arm consumes (Consume=true) and produces nothing; another
	// preserves (Consume=false) and produces nothing — net stack
	// depth differs by the subject.
	src := `
42 match
    int : "i" wl,
    _ :> "other" wl,
end
`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected branch-size mismatch; errs=%v", errs)
	}
}

func TestTypeCheckProgramMatchEmptyStack(t *testing.T) {
	src := `match _ : "x" wl, end`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected stack-underflow; errs=%v", errs)
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

func TestTypeCheckProgramVarStoreThenAtRetrieve(t *testing.T) {
	// `name!` stores; `@name` retrieves. The VARSTORE lexeme is
	// `name!` and the VARRETRIEVE lexeme is `@name` — both must
	// intern to the same NameId for the lookup to succeed.
	errs, ok := parseAndCheck(t, "2 n! @n 3 +")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected varstore + @retrieve + arithmetic to pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramAtRetrieveInsideDef(t *testing.T) {
	// `n!` inside a def body must populate the per-def VarEnv so
	// the subsequent `@n` resolves to the captured input type.
	src := `
def myfn (int -- int)
    n!
    @n 1 +
end
5 myfn
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected pass; errs=%v", errs)
	}
}

func TestTypeCheckProgramAtRetrieveUnknown(t *testing.T) {
	// `@nope` with no prior `nope!` is reported as unknown.
	errs, ok := parseAndCheck(t, "@nope")
	if ok {
		t.Fatalf("expected unknown-identifier error; got pass")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "unknown identifier") && strings.Contains(e, "@nope") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown-identifier @nope error; errs=%v", errs)
	}
}
