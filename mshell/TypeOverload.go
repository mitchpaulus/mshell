package main

// Phase 9: overload dispatch.
//
// A name may map to multiple QuoteSigs ("overloads"). At each call
// site the checker picks the most-specific candidate that unifies
// against the current stack. Selection is per-call-site; nothing is
// memoized across uses (a future phase can add a monomorphic-call
// fast path if profiling justifies it).
//
// Resolution algorithm:
//
//   1. Snapshot the stack and the substitution.
//   2. For each candidate:
//        a. Restore both snapshots.
//        b. Instantiate the candidate (fresh-rename its generics).
//        c. If the candidate's arity exceeds the stack, drop it.
//        d. Trial-unify each input against the matching stack slot.
//           Any failure drops the candidate.
//        e. If it survives, score its specificity from the
//           pre-instantiation sig (so generic candidates with
//           remaining vars score lower than concrete ones).
//   3. Restore both snapshots once more (so the actual application
//      below starts from a clean state).
//   4. If exactly one candidate has the maximum score, apply it.
//      If multiple share the max, report TErrAmbiguousOverload and
//      apply the first of the tied candidates so downstream
//      type-checking has something coherent to continue against.
//      If none unified, report TErrNoMatchingOverload and apply the
//      first listed candidate to recover.
//
// Specificity score (higher = more specific): every non-TKVar arena
// node in an input contributes 1; TKVar contributes 0. Brand wrappers
// add an extra +1 to favor nominal matches over equivalent
// structural ones.

type viableOverload struct {
	sig   QuoteSig
	score int
}

func (c *Checker) resolveAndApply(candidates []QuoteSig, callSite Token) {
	if len(candidates) == 1 {
		c.applySig(candidates[0], callSite)
		return
	}
	// In quote-body inference mode, the stack may be intentionally
	// short — applySig synthesizes fresh vars for missing inputs.
	// We still want overload resolution to do its job when the
	// stack *does* have enough items, so try the normal path
	// first; only fall back to "punt to the first candidate" when
	// every candidate would drop on stack-too-short and synthesis
	// is the only way forward.
	inferringFallback := c.inferring

	stackSnap := c.Snapshot()
	substSnap := c.subst.Checkpoint()

	var viable []viableOverload

	for _, cand := range candidates {
		c.Fork(stackSnap)
		c.subst.Rollback(substSnap)

		instantiated := c.Instantiate(cand)
		if len(c.stack.items) < len(instantiated.Inputs) {
			if !c.inferring {
				continue
			}
			need := len(instantiated.Inputs) - len(c.stack.items)
			extra := make([]TypeId, need)
			for i := 0; i < need; i++ {
				extra[i] = c.subst.FreshVar(c.arena)
			}
			c.stack.items = append(append([]TypeId(nil), extra...), c.stack.items...)
		}
		base := len(c.stack.items) - len(instantiated.Inputs)
		unboundActual := make([]bool, len(instantiated.Inputs))
		for i := range instantiated.Inputs {
			actual := c.subst.Apply(c.arena, c.stack.items[base+i])
			unboundActual[i] = c.arena.Kind(actual) == TKVar
		}
		match := true
		for i, want := range instantiated.Inputs {
			if !c.unify(c.stack.items[base+i], want) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		score := 0
		for i, in := range cand.Inputs {
			score += specificityScore(c.arena, in)
			if unboundActual[i] && c.arena.Kind(in) != TKVar {
				score -= 100
			}
		}
		viable = append(viable, viableOverload{sig: cand, score: score})
	}

	c.Fork(stackSnap)
	c.subst.Rollback(substSnap)

	if len(viable) == 0 {
		// In inferring mode (quote body), every candidate may drop
		// purely because the stack is shorter than its arity. Hand
		// off to the first candidate so applySig's underflow-as-
		// fresh-var synthesis can run. Without this, we'd flag
		// "no matching overload" for any builtin called inside a
		// quote that needs more inputs than the body has supplied
		// so far.
		if inferringFallback {
			c.applySig(candidates[0], callSite)
			return
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrNoMatchingOverload,
			Pos:  callSite,
			Hint: "no listed signature accepts the current stack",
		})
		// Clean stack-shape recovery without re-running applySig
		// (which would add redundant errors). Pop the first
		// candidate's inputs (best effort) and push fresh vars
		// for outputs so downstream checking has a coherent stack.
		first := candidates[0]
		need := len(first.Inputs)
		if c.stack.Len() < need {
			need = c.stack.Len()
		}
		c.stack.items = c.stack.items[:c.stack.Len()-need]
		for range first.Outputs {
			c.stack.Push(c.subst.FreshVar(c.arena))
		}
		return
	}

	bestIdx := 0
	tied := false
	for i := 1; i < len(viable); i++ {
		switch {
		case viable[i].score > viable[bestIdx].score:
			bestIdx = i
			tied = false
		case viable[i].score == viable[bestIdx].score:
			tied = true
		}
	}
	if tied {
		// Suppress the diagnostic when the tie is caused by an
		// unbound input (typically a fresh var produced by an
		// upstream unknown identifier or no-match recovery). The
		// ambiguity is a cascade artifact, not a genuine
		// resolution problem — picking the first candidate
		// deterministically preserves stack shape without piling
		// on diagnostics that only restate the upstream gap.
		inputArity := len(viable[bestIdx].sig.Inputs)
		base := c.stack.Len() - inputArity
		hasUnboundInput := base < 0
		if base < 0 {
			base = 0
		}
		for i := 0; i < inputArity && base+i < c.stack.Len(); i++ {
			t := c.subst.Apply(c.arena, c.stack.items[base+i])
			if c.arena.Kind(t) == TKVar {
				hasUnboundInput = true
				break
			}
		}
		if !hasUnboundInput {
			c.errors = append(c.errors, TypeError{
				Kind: TErrAmbiguousOverload,
				Pos:  callSite,
				Hint: "multiple equally-specific overloads match",
			})
		} else if c.inferring {
			if merged, ok := c.mergeInputOnlyOverloads(viable); ok {
				c.applySig(merged, callSite)
				return
			}
		}
	}
	c.applySig(viable[bestIdx].sig, callSite)
}

func (c *Checker) mergeInputOnlyOverloads(candidates []viableOverload) (QuoteSig, bool) {
	if len(candidates) < 2 {
		return QuoteSig{}, false
	}
	first := candidates[0].sig
	if len(first.Generics) != 0 {
		return QuoteSig{}, false
	}
	inputs := make([][]TypeId, len(first.Inputs))
	for i, in := range first.Inputs {
		inputs[i] = append(inputs[i], in)
	}
	for _, candidate := range candidates[1:] {
		sig := candidate.sig
		if len(sig.Generics) != 0 ||
			len(sig.Inputs) != len(first.Inputs) ||
			len(sig.Outputs) != len(first.Outputs) ||
			sig.Fail != first.Fail ||
			sig.Pure != first.Pure ||
			sig.Diverges != first.Diverges {
			return QuoteSig{}, false
		}
		for i := range sig.Outputs {
			if sig.Outputs[i] != first.Outputs[i] {
				return QuoteSig{}, false
			}
		}
		for i, in := range sig.Inputs {
			inputs[i] = append(inputs[i], in)
		}
	}

	merged := first
	merged.Inputs = make([]TypeId, len(inputs))
	for i, arms := range inputs {
		merged.Inputs[i] = c.arena.MakeUnion(arms, 0)
	}
	merged.Outputs = append([]TypeId(nil), first.Outputs...)
	return merged, true
}

// specificityScore counts how much "structural commitment" a type
// expresses. Every non-var node contributes 1; brand wrappers add
// an extra to favor nominal matches. The score is summed across an
// entire candidate's input list to rank candidates in
// resolveAndApply.
func specificityScore(arena *TypeArena, t TypeId) int {
	n := arena.Node(t)
	switch n.Kind {
	case TKVar:
		return 0
	case TKPrim, TKGrid, TKGridView, TKGridRow:
		return 1
	case TKMaybe, TKList:
		return 1 + specificityScore(arena, TypeId(n.A))
	case TKDict:
		return 1 + specificityScore(arena, TypeId(n.A)) + specificityScore(arena, TypeId(n.B))
	case TKShape:
		s := 1
		for _, f := range arena.shapeFields[n.Extra] {
			s += specificityScore(arena, f.Type)
		}
		return s
	case TKUnion:
		s := 1
		for _, arm := range arena.unionMembers[n.Extra] {
			s += specificityScore(arena, arm)
		}
		return s
	case TKBrand:
		return 2 + specificityScore(arena, TypeId(n.B))
	case TKQuote:
		sig := arena.quoteSigs[n.Extra]
		s := 1
		for _, in := range sig.Inputs {
			s += specificityScore(arena, in)
		}
		for _, out := range sig.Outputs {
			s += specificityScore(arena, out)
		}
		return s
	case TKOverloadedQuote:
		best := 0
		for _, sig := range arena.overloadedQuoteSigs[n.Extra] {
			score := 1
			for _, in := range sig.Inputs {
				score += specificityScore(arena, in)
			}
			for _, out := range sig.Outputs {
				score += specificityScore(arena, out)
			}
			if score > best {
				best = score
			}
		}
		return best
	}
	return 1
}
