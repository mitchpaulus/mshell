package main

import "testing"

// allCheckerErrors runs the full program checker and returns every
// diagnostic, including SeverityInfo ones (the CLI entry point filters
// those out, but the LSP path surfaces them, so tests inspect them here).
func allCheckerErrors(t *testing.T, src string) []TypeError {
	t.Helper()
	l := NewLexer(src, nil)
	p := NewMShellParser(l)
	file, err := p.ParseFile()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	arena := NewTypeArena()
	names := NewNameTable()
	checker := NewChecker(arena, names)
	checker.CheckProgram(file)
	return checker.Errors()
}

func fatalErrorCount(errs []TypeError) int {
	n := 0
	for _, e := range errs {
		if e.Severity == SeverityError {
			n++
		}
	}
	return n
}

func infoCount(errs []TypeError, kind TypeErrorKind) int {
	n := 0
	for _, e := range errs {
		if e.Severity == SeverityInfo && e.Kind == kind {
			n++
		}
	}
	return n
}

// --- Optional-field subtyping ------------------------------------------

func TestOptionalFieldOmittedAccepted(t *testing.T) {
	src := `def doReq ({ "url": str, "timeout"?: int } -- str) :url? end
{ "url": "x" } doReq drop`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n != 0 {
		t.Fatalf("omitting an optional field should type-check; got %d fatal errors", n)
	}
}

func TestOptionalFieldPresentRightTypeAccepted(t *testing.T) {
	src := `def doReq ({ "url": str, "timeout"?: int } -- str) :url? end
{ "url": "x", "timeout": 30 } doReq drop`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n != 0 {
		t.Fatalf("present optional field of the right type should type-check; got %d", n)
	}
}

func TestOptionalFieldPresentWrongTypeRejected(t *testing.T) {
	src := `def doReq ({ "url": str, "timeout"?: int } -- str) :url? end
{ "url": "x", "timeout": "soon" } doReq drop`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n == 0 {
		t.Fatal("a present optional field of the wrong type must be rejected")
	}
}

func TestRequiredFieldMissingRejected(t *testing.T) {
	src := `def doReq ({ "url": str, "timeout"?: int } -- str) :url? end
{ "timeout": 30 } doReq drop`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n == 0 {
		t.Fatal("a missing required field must be rejected")
	}
}

func TestRequiredValueSatisfiesOptionalParam(t *testing.T) {
	// A value whose field is required can stand in for an optional parameter.
	src := `def needsOpt ({ "timeout"?: int } -- int) :timeout 0 maybe end
def hasReq ({ "timeout": int } -- int) needsOpt end`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n != 0 {
		t.Fatalf("required value should satisfy optional parameter; got %d", n)
	}
}

func TestOptionalValueDoesNotSatisfyRequiredParam(t *testing.T) {
	src := `def needsReq ({ "timeout": int } -- int) :timeout? end
def hasOpt ({ "timeout"?: int } -- int) needsReq end`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n == 0 {
		t.Fatal("optional value must not satisfy a required parameter")
	}
}

// --- "always-fails unwrap" diagnostic ----------------------------------

func TestUnwrapDiagnosticUndeclaredShapeField(t *testing.T) {
	errs := allCheckerErrors(t, `{ "a": 1 } :b? drop`)
	if fatalErrorCount(errs) != 0 {
		t.Fatalf("undeclared-field unwrap must not be a fatal error; got %d", fatalErrorCount(errs))
	}
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 1 {
		t.Fatalf("expected exactly 1 always-fails info diagnostic, got %d", got)
	}
}

func TestUnwrapDiagnosticFlagsAtUnwrapThroughBinding(t *testing.T) {
	// The None value flows through a variable before being unwrapped; the
	// diagnostic must still fire (the failure happens at the `?`, wherever
	// the value came from).
	errs := allCheckerErrors(t, `{ "a": 1 } :b val! @val ?`)
	if fatalErrorCount(errs) != 0 {
		t.Fatalf("must not be a fatal error; got %d", fatalErrorCount(errs))
	}
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 1 {
		t.Fatalf("expected the always-fails hint to survive the binding, got %d", got)
	}
}

func TestUnwrapDiagnosticDirectUnwrapThroughBinding(t *testing.T) {
	// Same value, never unwrapped → no hint even though it is always None.
	errs := allCheckerErrors(t, `{ "a": 1 } :b val! @val drop`)
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 0 {
		t.Fatalf("a value that is never unwrapped must not be flagged, got %d", got)
	}
}

func TestUnwrapDiagnosticBareNone(t *testing.T) {
	// `none` is always Nothing, so unwrapping it always fails — flag it,
	// including when it flows through a binding first.
	for _, src := range []string{`none ?`, `none val! @val ?`} {
		errs := allCheckerErrors(t, src)
		if fatalErrorCount(errs) != 0 {
			t.Fatalf("%q: must not be a fatal error; got %d", src, fatalErrorCount(errs))
		}
		if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 1 {
			t.Fatalf("%q: expected the always-fails hint, got %d", src, got)
		}
	}
}

func TestNoneSafeUsesNotFlagged(t *testing.T) {
	for _, src := range []string{`none 5 maybe drop`, `none drop`, `none isNone drop`} {
		if got := infoCount(allCheckerErrors(t, src), TErrUnwrapAlwaysFails); got != 0 {
			t.Fatalf("%q: safe none use must not be flagged, got %d", src, got)
		}
	}
}

func TestUnwrapDiagnosticDeclaredFieldSilent(t *testing.T) {
	errs := allCheckerErrors(t, `{ "a": 1 } :a? drop`)
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 0 {
		t.Fatalf("declared-field unwrap must not be flagged, got %d", got)
	}
}

func TestUnwrapDiagnosticHomogeneousDictSilent(t *testing.T) {
	errs := allCheckerErrors(t, `{ "a": 1 } as {str: int} :zzz? drop`)
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 0 {
		t.Fatalf("homogeneous-dict unwrap must not be flagged, got %d", got)
	}
}

func TestUnwrapDiagnosticOptionalFieldSilent(t *testing.T) {
	// `:timeout?` on a declared optional field is a permitted assert-present
	// (fromJust), never an "always fails".
	src := `def f ({ "timeout"?: int } -- int) :timeout? end`
	errs := allCheckerErrors(t, src)
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 0 {
		t.Fatalf("optional-field unwrap must not be flagged, got %d", got)
	}
}

func TestUnwrapDiagnosticBareGetterSilent(t *testing.T) {
	// `:b` without a trailing `?` is a normal Maybe getter; no diagnostic.
	errs := allCheckerErrors(t, `{ "a": 1 } :b drop`)
	if got := infoCount(errs, TErrUnwrapAlwaysFails); got != 0 {
		t.Fatalf("bare getter must not be flagged, got %d", got)
	}
}

// --- Re-typed builtins (optional-field option dicts) -------------------

func TestNumFmtEmptyOptionsAccepted(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `1.0 { } numFmt drop`)); n != 0 {
		t.Fatalf("numFmt with an empty options dict should type-check; got %d", n)
	}
}

func TestNumFmtKnownOptionAccepted(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `1.0 { 'decimals': 2 } numFmt drop`)); n != 0 {
		t.Fatalf("numFmt with a valid option should type-check; got %d", n)
	}
}

func TestNumFmtWrongTypedOptionRejected(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `1.0 { 'decimals': "two" } numFmt drop`)); n == 0 {
		t.Fatal("numFmt with a wrong-typed option must be rejected")
	}
}

func TestZipPackRequiresPath(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, "[ { archivePath: \"x\" } ] `out.zip` zipPack")); n == 0 {
		t.Fatal("zipPack entries without a required `path` must be rejected")
	}
}

func TestHttpGetRequiresUrl(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `{ "timeout": 5 } httpGet drop`)); n == 0 {
		t.Fatal("httpGet without a required `url` must be rejected")
	}
}

func TestHttpGetUrlOnlyAccepted(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `{ "url": "https://example.com" } httpGet drop`)); n != 0 {
		t.Fatalf("httpGet with only a url should type-check; got %d", n)
	}
}

func TestHttpGetFollowRedirectsBoolAccepted(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `{ "url": "https://example.com", "followRedirects": false } httpGet drop`)); n != 0 {
		t.Fatalf("httpGet with a bool `followRedirects` should type-check; got %d", n)
	}
}

func TestHttpGetFollowRedirectsNonBoolRejected(t *testing.T) {
	if n := fatalErrorCount(allCheckerErrors(t, `{ "url": "https://example.com", "followRedirects": 1 } httpGet drop`)); n == 0 {
		t.Fatal("httpGet with a non-bool `followRedirects` must be rejected")
	}
}

// A string literal key to `get` resolves the shape field just like the
// `:name` getter: `"body" get?` yields the response body's `bytes`, which
// writeFile accepts — it must NOT collapse the shape to the union of all
// field value types.
func TestGetLiteralKeyResolvesShapeField(t *testing.T) {
	src := `{ "url": "https://example.com" } httpGet? "body" get? "out.bin" writeFile`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n != 0 {
		t.Fatalf("`\"body\" get?` should resolve to bytes and satisfy writeFile; got %d errors", n)
	}
}

// The literal-key path is precise, not permissive: `"status" get?` resolves
// to the field's `int`, so feeding it to writeFile (str | bytes) is rejected.
func TestGetLiteralKeyIsFieldPrecise(t *testing.T) {
	src := `{ "url": "https://example.com" } httpGet? "status" get? "out.bin" writeFile`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n == 0 {
		t.Fatal("`\"status\" get?` resolves to int, which writeFile must reject")
	}
}

// The key value rides the stack as a `str` refinement, so it resolves even
// when it reaches `get` through a variable rather than inline — the literal
// need not be adjacent to `get`.
func TestGetLiteralKeyThroughVariable(t *testing.T) {
	src := `{ "url": "https://example.com" } httpGet? "body" k! @k get? "out.bin" writeFile`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n != 0 {
		t.Fatalf("a literal key bound to a variable should still resolve `body` to bytes; got %d errors", n)
	}
}

// A key whose value is not statically known (here an env var, a plain `str`)
// cannot resolve a specific field, so `get` falls back to the generic dict
// overload and yields the union of every field value type — which writeFile
// rejects.
func TestGetDynamicKeyStaysGeneric(t *testing.T) {
	src := `{ "url": "https://example.com" } httpGet? $KEY get? "out.bin" writeFile`
	if n := fatalErrorCount(allCheckerErrors(t, src)); n == 0 {
		t.Fatal("a non-literal key should fall back to the union-typed get and be rejected by writeFile")
	}
}
