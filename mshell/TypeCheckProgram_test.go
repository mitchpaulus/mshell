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
	return TypeCheckProgram(file, nil)
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
		`1.5 2.0 + str wl`,
		`1.0 2.0 / str wl`,
		`1.5 2.0 < str wl`,
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

// TestTypeCheckProgramPrefixQuoteOverFreeVarShape guards against a
// regression where the `fn.` block form (syntax sugar for `(...) fn`)
// diverged from the plain quote form. The prefix path seeded the block
// body's input with the receiver's element type; when that element was a
// shape carrying a free type var (here `parseExcel`, whose data cells are
// str|float|bool|Maybe[T] with T unconstrained), reading two fields in the
// body tangled the receiver's free var into the inferred quote sig and
// the consuming `each` then failed to unify. The prefix and quote forms
// must type-check identically.
func TestTypeCheckProgramPrefixQuoteOverFreeVarShape(t *testing.T) {
	src := "`f.xlsx` parseExcel each. sheet!\n" +
		"  @sheet :data? val!\n" +
		"  @sheet :name? drop\n" +
		"end"
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected prefix-quote over free-var shape to type-check; errs=%v", errs)
	}
}

// Indexer/slice quotes route through the shared overload machinery rather
// than the old indexerResultType punt, which collapsed an unknown receiver
// to a disconnected fresh output var. These three tests pin the resulting
// behavior:

// A bare slice quote infers as an overloaded quote whose output is tied to
// its input, so `map` recovers the precise element type. If the old punt
// returned a disconnected var, `(1:) map` over [[int]] would yield [T]; here
// the program must type-check, exercising the tied list arm end to end.
func TestTypeCheckIndexerSliceQuoteMapPropagatesElement(t *testing.T) {
	errs, ok := parseAndCheck(t, "[[1 2 3] [4 5 6]] (1:) map")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected (1:) map over [[int]] to type-check; errs=%v", errs)
	}
}

// The element-extractor `(:0:)` over rows of [int] must resolve to int, not
// a free var. listToDict requires a str key, so an int key is a genuine type
// error. Under the old disconnected-var behavior the extractor's output var
// bound spuriously to str and this passed — that masked unsoundness is what
// the fix removes.
func TestTypeCheckIndexerExtractorKeyTypeEnforced(t *testing.T) {
	errs, ok := parseAndCheck(t, "[[1 2 3] [4 5 6]] (:0:) (:1:) listToDict")
	if ok && len(errs) == 0 {
		t.Fatalf("expected int key extractor to be rejected by listToDict's str key")
	}
}

// Coercing the extracted key to str satisfies listToDict, confirming the
// overloaded indexer quote resolves cleanly against an expected (T -- str).
func TestTypeCheckIndexerExtractorKeyCoercible(t *testing.T) {
	errs, ok := parseAndCheck(t, "[[1 2 3] [4 5 6]] (:0: str) (:1:) listToDict")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected str-coerced key extractor to type-check; errs=%v", errs)
	}
}

// Indexing a quote-bound variable whose type isn't pinned yet (here `@row`,
// fixed later by the enclosing `map`) must NOT fan out across the container
// overload arms — doing so binds the shared variable to a different container
// per branch and corrupts the inferred element type. The body builds a [str],
// so the map yields [[str]]; the trailing `:0: :0:` only type-checks if that
// nested-list shape survived (the buggy fan-out collapsed it to [int], on
// which the second index errors).
func TestTypeCheckIndexerOnUnpinnedBoundVarDefers(t *testing.T) {
	errs, ok := parseAndCheck(t, "[[1 2] [3 4]] map. row! [ @row :0: str ] end :0: :0:")
	if !ok || len(errs) != 0 {
		t.Fatalf("expected indexing an unpinned bound var inside map to preserve [[str]]; errs=%v", errs)
	}
}

func TestTypeCheckProgramOverloadedQuoteResolvesFromExpectedQuote(t *testing.T) {
	src := `
def useDateCmp (datetime datetime (datetime datetime -- bool) -- bool)
    op! b! a!
    @a @b @op x
end

2025-01-01 2025-01-02 (>) useDateCmp
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected overloaded quote to resolve as datetime comparison; errs=%v", errs)
	}
}

func TestTypeCheckProgramIffReturnBranchDiverges(t *testing.T) {
	src := `
def returnTest (str -- str)
    a!
    @a "a" = ("Found a" return) iff
    @a
end

"a" returnTest wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected iff return branch to type-check as divergent; errs=%v", errs)
	}
}

func TestTypeCheckProgramIffExitBranchDiverges(t *testing.T) {
	src := `
args (a! @a "--help" = @a "-h" = or) any
(
    "usage" wl
    0 exit
) iff
"body" wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected iff exit branch to type-check as divergent; errs=%v", errs)
	}
}

func TestTypeCheckProgramAnyQuoteInputStaysString(t *testing.T) {
	src := `args (a! @a "--help" = @a "-h" = or) any drop`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected any quote input to stay string; errs=%v", errs)
	}
}

func TestTypeCheckProgramLoopBreakBranchPreservesStack(t *testing.T) {
	src := `
1
(
    read not (drop break) () iff
    linetext! num!
    @num 1 +
) loop
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected break branch to preserve loop stack shape; errs=%v", errs)
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
		`1 2 = str wl`,
		`"hello" len wl`,
		`"a" "b" != str wl`,
		`42 abs wl`,
		`42 toFloat str wl`,
		`"42" toInt drop`,
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
	// stack slot becomes int|str. `drop` is polymorphic so the
	// union-typed slot consumes cleanly.
	errs, ok := parseAndCheck(t, `true if 42 else "hi" end drop`)
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

func TestTypeCheckProgramFilterPredicateWithIndexer(t *testing.T) {
	src := `"scripts" lsDir (isFile) filter (readFile lines :0: "env msh" in) filter drop`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected filter predicate with indexer to type-check; errs=%v", errs)
	}
}

func TestTypeCheckProgramWritePathRejected(t *testing.T) {
	errs, ok := parseAndCheck(t, "`file.txt` wl")
	if ok {
		t.Fatalf("expected path write to fail type checking; errs=%v", errs)
	}
}

func TestTypeCheckProgramWriteRejectsNonStringIntTypes(t *testing.T) {
	// Runtime's write switch (Evaluator.go) only stringifies str / int
	// (and bytes for w/we). Anything else crashes — datetime, float, bool,
	// list, dict, etc. The checker must reject these statically so the
	// program never reaches the runtime failure.
	cases := []string{
		`2026-01-01 wl`,    // datetime
		`1.5 wl`,           // float
		`true wl`,          // bool
		`[1 2 3] wl`,       // list
		`2026-01-01 w`,     // datetime via w as well
		`1.5 we`,           // float via we
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if ok {
			t.Errorf("%q: expected type-check failure; errs=%v", src, errs)
		}
	}
}

func TestTypeCheckProgramWriteBinarySplitByVariant(t *testing.T) {
	// w/we accept bytes; wl/wle do not (a trailing newline after raw
	// bytes is rarely intended — runtime also rejects this).
	if _, ok := parseAndCheck(t, `"x" utf8Bytes w`); !ok {
		t.Errorf("w should accept binary")
	}
	if _, ok := parseAndCheck(t, `"x" utf8Bytes we`); !ok {
		t.Errorf("we should accept binary")
	}
	if _, ok := parseAndCheck(t, `"x" utf8Bytes wl`); ok {
		t.Errorf("wl should reject binary")
	}
	if _, ok := parseAndCheck(t, `"x" utf8Bytes wle`); ok {
		t.Errorf("wle should reject binary")
	}
}

func TestTypeCheckProgramPrefixQuoteBodyChecked(t *testing.T) {
	src := "[`file.txt`] each. f! @f wl end"
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected path write inside prefix quote to fail type checking; errs=%v", errs)
	}
}

func TestTypeCheckProgramInputRedirectionAfterCapture(t *testing.T) {
	cases := []string{
		`['cat'] * "stdin" < ! output! @output w`,
		`['cat'] * ` + "`stdin.txt`" + ` < ! output! @output w`,
		`['cat'] * "stdin" utf8Bytes < ! output! @output w`,
		`['cat'] *b "stdin" < ! output! @output w`,
		`['cat'] *| "stdin" < ! output! @output drop`,
		`['cat'] ^ "stdin" < ! output! @output w`,
		`['cat'] ^b "stdin" < ! output! @output w`,
		`['cat'] * ^ "stdin" < ! stdout! stderr! @stdout w @stderr w`,
		`['cat'] ^ * "stdin" < ! stderr! stdout! @stdout w @stderr w`,
	}
	for _, src := range cases {
		errs, ok := parseAndCheck(t, src)
		if !ok || len(errs) != 0 {
			t.Fatalf("%q: expected redirection after capture marker to type-check; errs=%v", src, errs)
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
	// `@v` retrieves an int. (`:n` is the dict/grid getter and pops
	// the stack — the wrong tool here.)
	src := `
5 just match
    just v : @v wl,
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

func TestTypeCheckProgramMatchPatternBindingEscapesArm(t *testing.T) {
	// A name introduced by a `just` pattern in the only live arm
	// (the `none` arm diverges) must be readable after the match.
	// Previously the checker stripped pattern bindings before
	// capture, so `@v` outside the match reported "unknown
	// identifier" even though the just arm was the only path
	// reaching that point.
	src := `
5 just match
    just v :,
    none: 1 exit
end
@v wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected just-binding to survive past match; errs=%v", errs)
	}
}

func TestTypeCheckProgramGetterOnDict(t *testing.T) {
	// `:name` pops a Dict (or GridRow) off the stack and pushes
	// Maybe[V]. Here {"n": 2} ":n" yields Maybe[int]; we just check
	// it type-checks and produces a single value.
	errs, ok := parseAndCheck(t, `{"n": 2} :n`)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected dict + getter to pass; errs=%v", errs)
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

func TestTypeCheckProgramIffMergesCommonQuoteBindings(t *testing.T) {
	src := `
true
(
    "tmp" local!
    "a" path!
)
(
    "b" path!
)
iff
@path wl
`
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected common iff quote binding to be visible; errs=%v", errs)
	}
}

func TestTypeCheckProgramIffDoesNotExposeOneSidedQuoteBinding(t *testing.T) {
	src := `
true
(
    "tmp" local!
    "a" path!
)
(
    "b" path!
)
iff
@local
`
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("expected one-sided iff quote binding to remain local")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "unknown identifier") && strings.Contains(e, "@local") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown-identifier @local error; errs=%v", errs)
	}
}

// Regression: a `dbg` token inside the branching driver appends a
// SeverityInfo TypeError to flag the diagnostic for the LSP. Previously
// tryBranchStep treated *any* error produced by a step as fatal and
// killed the branch, which short-circuited driveBranches and skipped
// every token after the `dbg`. The result: a real type error sitting
// downstream of a `dbg` silently disappeared. Putting `dbg` in front of
// a broken token "fixed" the error report, which is exactly the
// symptom that surfaced this bug.
func TestTypeCheckProgramDbgDoesNotSuppressLaterErrors(t *testing.T) {
	// Without the fix, `dbg` swallows the error from `"x" +`.
	errs, ok := parseAndCheck(t, `42 dbg "x" +`)
	if ok {
		t.Fatalf("expected type error after dbg to surface; errs=%v", errs)
	}
}

func TestTypeCheckProgramDbgInValidProgramPasses(t *testing.T) {
	// A valid program with `dbg` mid-stream still type-checks. The
	// dbg dump is recorded as a SeverityInfo diagnostic but does not
	// fail the program.
	errs, ok := parseAndCheck(t, `42 dbg 1 + wl`)
	if !ok {
		t.Fatalf("expected valid program with dbg to pass; errs=%v", errs)
	}
}
