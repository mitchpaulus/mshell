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

func TestInferEmptyQuote(t *testing.T) {
	c := freshChecker()
	sig := c.InferQuoteSig(nil)
	if !sigEquals(sig, nil, nil) {
		t.Fatalf("empty quote: want ( -- ), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
}

func TestInferLiteralOnly(t *testing.T) {
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{mkTok(INTEGER, "5")})
	if !sigEquals(sig, nil, []TypeId{TidInt}) {
		t.Fatalf("[5]: want ( -- int), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
}

func TestInferPartialPlus(t *testing.T) {
	// [2 +] : (int -- int)
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	})
	if !sigEquals(sig, []TypeId{TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[2 +]: want (int -- int), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
}

func TestInferFullPlus(t *testing.T) {
	// [+] : (int int -- int)
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{mkTok(PLUS, "+")})
	if !sigEquals(sig, []TypeId{TidInt, TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[+]: want (int int -- int), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
}

func TestInferDoublePlus(t *testing.T) {
	// [+ +] : (int int int -- int)
	// First + needs (a, b) — fresh v1 v2 at bottom. After: stack [int].
	// Second + needs (deeper, top) — synthesize v3 at bottom. Inputs = [v3, v1, v2].
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{
		mkTok(PLUS, "+"),
		mkTok(PLUS, "+"),
	})
	if !sigEquals(sig, []TypeId{TidInt, TidInt, TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[+ +]: want (int int int -- int), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
}

func TestInferComparison(t *testing.T) {
	// [<] : (int int -- bool)
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{mkTok(LESSTHAN, "<")})
	if !sigEquals(sig, []TypeId{TidInt, TidInt}, []TypeId{TidBool}) {
		t.Fatalf("[<]: want (int int -- bool), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
}

func TestInferProducesMaybe(t *testing.T) {
	// [just] : (T -- Maybe[T]) — note: T is not generalized, see Phase-7
	// header comment. The inferred sig has whatever fresh var was
	// allocated; we check the structural shape.
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{mkTok(LITERAL, "just")})
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
	// [none] : ( -- Maybe[T]) — outputs Maybe with a fresh var.
	c := freshChecker()
	sig := c.InferQuoteSig([]Token{mkTok(LITERAL, "none")})
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

	sig := c.InferQuoteSig([]Token{
		mkTok(INTEGER, "1"),
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	})

	if !sigEquals(sig, nil, []TypeId{TidInt}) {
		t.Fatalf("inner sig wrong: want ( -- int), got %s",
			FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
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
	// [+] called context-free works (both inputs become int). But
	// [2 "x" +] should produce a TErrTypeMismatch since the str
	// literal cannot satisfy the int demand of `+`.
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
	// the type checker accepts it.
	c := freshChecker()
	innerSig := c.InferQuoteSig([]Token{
		mkTok(INTEGER, "2"),
		mkTok(PLUS, "+"),
	})
	if !sigEquals(innerSig, []TypeId{TidInt}, []TypeId{TidInt}) {
		t.Fatalf("[2 +] should infer to (int -- int)")
	}
	innerType := c.arena.MakeQuote(innerSig)

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
