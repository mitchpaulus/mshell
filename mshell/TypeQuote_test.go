package main

import "testing"

// Phase-7 tests: quote-body inference.

func sigEquals(a QuoteSig, ins []TypeId, outs []TypeId) bool {
	if len(a.Inputs) != len(ins) || len(a.Outputs) != len(outs) {
		return false
	}
	for i := range a.Inputs {
		if a.Inputs[i] != ins[i] {
			return false
		}
	}
	for i := range a.Outputs {
		if a.Outputs[i] != outs[i] {
			return false
		}
	}
	return true
}

// sigsContain reports whether the inferred sig set contains a sig
// with the given (inputs, outputs) shape.
func sigsContain(sigs []QuoteSig, ins []TypeId, outs []TypeId) bool {
	for _, s := range sigs {
		if sigEquals(s, ins, outs) {
			return true
		}
	}
	return false
}

func TestInferEmptyQuote(t *testing.T) {
	c := freshChecker()
	sigs := c.InferQuoteSig(nil)
	if len(sigs) != 1 || !sigEquals(sigs[0], nil, nil) {
		t.Fatalf("empty quote: want ( -- ), got %d sigs", len(sigs))
	}
}

func TestInferLiteralOnly(t *testing.T) {
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{mkTok(INTEGER, "5")})
	if len(sigs) != 1 || !sigEquals(sigs[0], nil, []TypeId{TidInt}) {
		t.Fatalf("[5]: want ( -- int), got %d sigs", len(sigs))
	}
}

func TestInferPartialPlus(t *testing.T) {
	// [2 +]: the int literal pins `+`'s second input to int, so
	// every branch except intIntInt dies. Single surviving sig.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	})
	if len(sigs) != 1 || !sigEquals(sigs[0], []TypeId{TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[2 +]: want (int -- int), got %d sigs", len(sigs))
	}
}

func TestInferFullPlus(t *testing.T) {
	// [+] is now genuinely overloaded: every viable `+` candidate
	// against fresh inputs survives. Verify the int and str sigs
	// are both present.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{mkTok(PLUS, "+")})
	if !sigsContain(sigs, []TypeId{TidInt, TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[+]: missing (int int -- int) among %d sigs", len(sigs))
	}
	if !sigsContain(sigs, []TypeId{TidStr, TidStr}, []TypeId{TidStr}) {
		t.Fatalf("[+]: missing (str str -- str) among %d sigs", len(sigs))
	}
	if len(sigs) < 2 {
		t.Fatalf("[+]: expected >=2 sigs, got %d", len(sigs))
	}
}

func TestInferDoublePlus(t *testing.T) {
	// [+ +]: the second + further narrows by its first operand
	// matching the first +'s output. Several typings survive (int,
	// float, str, list, path). Check the int and str paths.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{
		mkTok(PLUS, "+"),
		mkTok(PLUS, "+"),
	})
	if !sigsContain(sigs, []TypeId{TidInt, TidInt, TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[+ +]: missing (int int int -- int)")
	}
	if !sigsContain(sigs, []TypeId{TidStr, TidStr, TidStr}, []TypeId{TidStr}) {
		t.Fatalf("[+ +]: missing (str str str -- str)")
	}
}

func TestInferComparison(t *testing.T) {
	// [<] used to be reported as (int int -- bool) because the int
	// overload happened to be listed first. Under branching the int,
	// float, and datetime comparison sigs all survive.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{mkTok(LESSTHAN, "<")})
	if !sigsContain(sigs, []TypeId{TidInt, TidInt}, []TypeId{TidBool}) {
		t.Fatalf("[<]: missing (int int -- bool)")
	}
	if !sigsContain(sigs, []TypeId{TidFloat, TidFloat}, []TypeId{TidBool}) {
		t.Fatalf("[<]: missing (float float -- bool)")
	}
}

func TestInferProducesMaybe(t *testing.T) {
	// [just]: still single-sig because `just` has only one overload.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{mkTok(LITERAL, "just")})
	if len(sigs) != 1 {
		t.Fatalf("[just]: want 1 sig, got %d", len(sigs))
	}
	sig := sigs[0]
	if len(sig.Inputs) != 1 || len(sig.Outputs) != 1 {
		t.Fatalf("[just]: expected 1-in 1-out, got (%d -- %d)",
			len(sig.Inputs), len(sig.Outputs))
	}
	in := sig.Inputs[0]
	if c.arena.Node(in).Kind != TKVar {
		t.Fatalf("[just]: input should be a TKVar, got %s",
			FormatType(c.arena, c.names, in))
	}
	out := sig.Outputs[0]
	outNode := c.arena.Node(out)
	if outNode.Kind != TKMaybe {
		t.Fatalf("[just]: output should be Maybe[..], got %s",
			FormatType(c.arena, c.names, out))
	}
	if TypeId(outNode.A) != in {
		t.Fatalf("[just]: Maybe inner should equal input var, got Maybe[%s] vs input %s",
			FormatType(c.arena, c.names, TypeId(outNode.A)),
			FormatType(c.arena, c.names, in))
	}
}

func TestInferConsumesMaybe(t *testing.T) {
	// [none]: single-sig.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{mkTok(LITERAL, "none")})
	if len(sigs) != 1 {
		t.Fatalf("[none]: want 1 sig, got %d", len(sigs))
	}
	sig := sigs[0]
	if len(sig.Inputs) != 0 || len(sig.Outputs) != 1 {
		t.Fatalf("[none]: expected 0-in 1-out, got (%d -- %d)",
			len(sig.Inputs), len(sig.Outputs))
	}
	out := sig.Outputs[0]
	if c.arena.Node(out).Kind != TKMaybe {
		t.Fatalf("[none]: output should be Maybe[..], got %s",
			FormatType(c.arena, c.names, out))
	}
}

func TestInferRestoresOuterState(t *testing.T) {
	// Outer state must not leak into the quote, and must be restored
	// after inference completes regardless of success.
	c := freshChecker()
	x := c.names.Intern("x")
	c.stack.Push(TidStr)
	c.vars.bound[x] = TidBool

	sigs := c.InferQuoteSig([]Token{
		mkTok(INTEGER, "1"),
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	})

	if len(sigs) != 1 || !sigEquals(sigs[0], nil, []TypeId{TidInt}) {
		t.Fatalf("inner sig wrong: want ( -- int), got %d sigs", len(sigs))
	}
	// Outer state intact?
	if c.stack.Len() != 1 || c.stack.Top() != TidStr {
		t.Fatalf("outer stack not restored: %v", c.stack.Snapshot())
	}
	if c.vars.bound[x] != TidBool {
		t.Fatalf("outer vars not restored")
	}
	if c.inferring {
		t.Fatalf("inferring flag leaked outside InferQuoteSig")
	}
	if c.inferInputs != nil {
		t.Fatalf("inferInputs not reset")
	}
}

func TestInferTypeMismatchInsideQuote(t *testing.T) {
	// [2 "x" +]: every `+` candidate fails on (int, str) inputs.
	// Branching collapses to "every branch dies at +", surfacing
	// the type mismatch from the failing applySig.
	c := freshChecker()
	c.InferQuoteSig([]Token{
		mkTok(INTEGER, "2"),
		mkTok(STRING, "\"x\""),
		mkTok(PLUS, "+"),
	})
	errs := c.Errors()
	if len(errs) == 0 || errs[0].Kind != TErrTypeMismatch {
		t.Fatalf("expected type mismatch inside quote body, got %+v", errs)
	}
}

func TestInferAppliesAtCallSite(t *testing.T) {
	// Round-trip: infer a quote, hand it to a higher-order builtin
	// shaped like `apply : ((int -- int) int -- int)`, and verify
	// the type checker accepts it. [2 +] still infers single-sig
	// (int -- int) because the int literal pins +'s second operand.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	})
	if len(sigs) != 1 || !sigEquals(sigs[0], []TypeId{TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[2 +] should infer to (int -- int); got %d sigs", len(sigs))
	}
	innerType := c.arena.MakeQuote(sigs[0])

	// Build a sig: apply : ((int -- int) int -- int)
	apply := QuoteSig{
		Inputs:  []TypeId{innerType, TidInt},
		Outputs: []TypeId{TidInt},
	}

	c.stack.Push(innerType)
	c.stack.Push(TidInt)
	c.applySig(apply, mkTok(LITERAL, "apply"))

	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("apply should accept inferred quote: %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("expected int output from apply; got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestInferBranchingOverloadedQuote(t *testing.T) {
	// The headline case: `(len 0 !=)` should infer as a multi-sig
	// overloaded quote because `len` itself is overloaded over its
	// input shape. The ground receivers (str, path, grids) are one
	// union arm in the table, so they survive as a single union-input
	// sig; a downstream `filter` on `[str]` still unifies because the
	// contravariant input check distributes over the union.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{
		mkTok(LITERAL, "len"),
		mkTok(INTEGER, "0"),
		mkTok(NOTEQUAL, "!="),
	})
	ground := c.arena.MakeUnion([]TypeId{
		TidStr, TidPath, c.arena.MakeGrid(0), c.arena.MakeGridView(0), c.arena.MakeGridRow(0),
	}, 0)
	if !sigsContain(sigs, []TypeId{ground}, []TypeId{TidBool}) {
		t.Fatalf("(len 0 !=): missing (str|path|Grid|GridView|GridRow -- bool) among %d sigs", len(sigs))
	}
}

func TestInferUnionInputMergeToPlainQuote(t *testing.T) {
	// `cd` is overloaded (str -- ) | (path -- ); both arms produce the
	// same (empty) output, so the overload is really a union on the
	// input. Quote inference must coalesce the arms into a single plain
	// quote (str|path -- ) — not a two-arm overloaded quote — so the
	// quote can be used as an `iff`/`loop` branch.
	c := freshChecker()
	sigs := c.InferQuoteSig([]Token{mkTok(LITERAL, "cd")})
	if len(sigs) != 1 {
		t.Fatalf("(cd): want 1 merged sig, got %d: %v", len(sigs), sigs)
	}
	strPath := c.arena.MakeUnion([]TypeId{TidStr, TidPath}, 0)
	if !sigEquals(sigs[0], []TypeId{strPath}, nil) {
		t.Fatalf("(cd): want (str|path -- ), got (%s -- %s)",
			FormatType(c.arena, c.names, firstOr(sigs[0].Inputs)),
			FormatType(c.arena, c.names, firstOr(sigs[0].Outputs)))
	}
}

func firstOr(ts []TypeId) TypeId {
	if len(ts) == 0 {
		return TidBottom
	}
	return ts[0]
}
