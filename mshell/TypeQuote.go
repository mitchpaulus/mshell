package main

// Phase 7: quote-body inference.
//
// A quote literal in source — `[ tokens ]` — receives a static type
// derived from running the checker over its body against a fresh
// empty stack. Each underflow during that run synthesizes a fresh
// type variable; those variables become the quote's inputs, in
// caller-stack order (deepest first). Whatever remains on the
// stack at the end of the body is the quote's outputs.
//
// Example: `[2 +]`
//   - INTEGER 2 pushes int. Stack: [int].
//   - `+` (sig: int int -- int) needs 2 inputs but only has 1.
//     Synthesize v1 at the bottom. Stack: [v1, int]. inferInputs: [v1].
//     Unify binds v1=int. Pop both, push int. Stack: [int].
//   - End of body. Outputs: [int]. Inputs (after substitution): [int].
//   - Inferred sig: (int -- int).
//
// Phase 7 does NOT generalize free type variables in the inferred
// sig. A body like `[dup]` (no concrete-type pressure on the var)
// returns `(T0 -- T0 T0)` with T0 left as a free variable in the
// global substitution. Calling such a quote at two different types
// will currently conflict — generalization (let-polymorphism) is a
// follow-on once a real need emerges, likely alongside Phase 9 or
// when user-written defs without sigs need it.

// InferQuoteSig runs the checker over body tokens with a fresh empty
// stack and var environment, accumulating fresh-var demands for
// underflow and reading off the residual stack as outputs. The
// outer state is restored before returning. Errors discovered
// inside the quote body are appended to the checker's error list,
// same as for top-level tokens.
func (c *Checker) InferQuoteSig(body []Token) QuoteSig {
	outerSnap := c.Snapshot()
	outerInferring := c.inferring
	outerInputs := c.inferInputs

	c.stack.items = c.stack.items[:0]
	c.vars.bound = make(map[NameId]TypeId)
	c.inferring = true
	c.inferInputs = nil

	for _, tok := range body {
		c.checkOne(tok)
	}

	rawInputs := c.inferInputs
	rawOutputs := append([]TypeId(nil), c.stack.items...)

	c.inferring = outerInferring
	c.inferInputs = outerInputs
	c.Fork(outerSnap)

	inputs := make([]TypeId, len(rawInputs))
	for i, in := range rawInputs {
		inputs[i] = c.subst.Apply(c.arena, in)
	}
	outputs := make([]TypeId, len(rawOutputs))
	for i, out := range rawOutputs {
		outputs[i] = c.subst.Apply(c.arena, out)
	}

	return QuoteSig{Inputs: inputs, Outputs: outputs}
}
