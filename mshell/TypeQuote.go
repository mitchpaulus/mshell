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
	outerDiverged := c.diverged

	c.stack.items = c.stack.items[:0]
	c.vars.bound = make(map[NameId]TypeId)
	c.inferring = true
	c.inferInputs = nil
	c.diverged = false

	for _, tok := range body {
		c.checkOne(tok)
	}

	rawInputs := c.inferInputs
	rawOutputs := append([]TypeId(nil), c.stack.items...)
	rawBindings := quoteBindingDelta(outerSnap.vars, c.vars.bound)
	diverged := c.diverged

	c.inferring = outerInferring
	c.inferInputs = outerInputs
	c.Fork(outerSnap)
	c.diverged = outerDiverged

	inputs := make([]TypeId, len(rawInputs))
	for i, in := range rawInputs {
		inputs[i] = c.subst.Apply(c.arena, in)
	}
	outputs := make([]TypeId, len(rawOutputs))
	for i, out := range rawOutputs {
		outputs[i] = c.subst.Apply(c.arena, out)
	}
	bindings := c.applyBindingTypes(rawBindings)

	return QuoteSig{Inputs: inputs, Outputs: outputs, Diverges: diverged, Bindings: bindings}
}

// InferQuoteSigItems is the parse-tree variant of InferQuoteSig. The
// new program walker (TypeCheckProgram.go / checkParseItem) consumes
// parse items rather than raw tokens, so quote literals encountered
// during program checking call this entry point. Same fresh-stack /
// underflow-as-fresh-var semantics; the difference is the walker.
func (c *Checker) InferQuoteSigItems(body []MShellParseItem) QuoteSig {
	outerSnap := c.Snapshot()
	outerInferring := c.inferring
	outerInputs := c.inferInputs
	outerDiverged := c.diverged

	c.stack.items = c.stack.items[:0]
	// Inherit the outer var environment so the quote body can
	// reference enclosing-scope bindings (`@i`, `@archive`, etc.).
	// Quotes are closures — `loop`, `iff` arms, and standalone
	// quotes all read variables from the surrounding scope at
	// runtime. The outer scope is restored from outerSnap after
	// the body finishes, so any stores inside the quote stay
	// local to it.
	inherited := make(map[NameId]TypeId, len(c.vars.bound))
	for k, v := range c.vars.bound {
		inherited[k] = v
	}
	c.vars.bound = inherited
	c.inferring = true
	c.inferInputs = nil
	c.diverged = false

	for _, item := range body {
		c.checkParseItem(item)
	}

	rawInputs := c.inferInputs
	rawOutputs := append([]TypeId(nil), c.stack.items...)
	rawBindings := quoteBindingDelta(outerSnap.vars, c.vars.bound)
	diverged := c.diverged

	c.inferring = outerInferring
	c.inferInputs = outerInputs
	c.Fork(outerSnap)
	c.diverged = outerDiverged

	inputs := make([]TypeId, len(rawInputs))
	for i, in := range rawInputs {
		inputs[i] = c.subst.Apply(c.arena, in)
	}
	outputs := make([]TypeId, len(rawOutputs))
	for i, out := range rawOutputs {
		outputs[i] = c.subst.Apply(c.arena, out)
	}
	bindings := c.applyBindingTypes(rawBindings)

	return QuoteSig{Inputs: inputs, Outputs: outputs, Diverges: diverged, Bindings: bindings}
}

func quoteBindingDelta(before, after map[NameId]TypeId) map[NameId]TypeId {
	delta := make(map[NameId]TypeId)
	for name, afterType := range after {
		if beforeType, ok := before[name]; !ok || beforeType != afterType {
			delta[name] = afterType
		}
	}
	return delta
}

func (c *Checker) applyBindingTypes(bindings map[NameId]TypeId) map[NameId]TypeId {
	if len(bindings) == 0 {
		return nil
	}
	out := make(map[NameId]TypeId, len(bindings))
	for name, t := range bindings {
		out[name] = c.subst.Apply(c.arena, t)
	}
	return out
}
