package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Overload dispatch.
//
// A name may map to multiple QuoteSigs ("overloads"). At each call
// site the checker trial-unifies every candidate against the current
// stack and keeps the viable set. There is no specificity ranking and
// no automatic pick: if exactly one candidate is viable it is
// applied; if more than one is viable the dispatch branches (under
// the branching walker) or reports TErrAmbiguousOverload (in the
// legacy direct-call path used by lower-level tests).
//
// Resolution algorithm:
//
//   1. Snapshot the stack and the substitution.
//   2. For each candidate:
//        a. Restore both snapshots.
//        b. Instantiate the candidate (fresh-rename its generics).
//        c. If the candidate's arity exceeds the stack and we are
//           not in inferring mode, drop it.
//        d. Trial-unify each input against the matching stack slot.
//           Any failure drops the candidate.
//   3. Restore both snapshots so the actual application starts clean.
//   4. With one viable, apply it. With zero, report
//      TErrNoMatchingOverload and recover. With multiple, fan out via
//      branchSpawn (branching walker) or report TErrAmbiguousOverload
//      (legacy path), applying the first viable candidate purely for
//      diagnostic recovery.

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

	var viable []QuoteSig

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
		viable = append(viable, cand)
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
			Hint: c.formatOverloadFailure(candidates),
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

	if len(viable) == 1 {
		c.applySig(viable[0], callSite)
		return
	}

	// Multiple candidates remain viable. In branching mode, fan out
	// every viable continuation into the branchSpawn slice and let
	// the caller (the branching driver via tryBranchStep) propagate
	// the alternatives. Downstream constraints prune; if none do,
	// the end-of-walk reconciler reports TErrAmbiguousTyping.
	if c.branchingEnabled {
		// applySig (in inferring mode) mutates c.inferInputs by
		// prepending freshly-synthesized inputs. Capture the
		// pre-call value so each fan-out iteration sees the same
		// starting point — otherwise iteration k's branch would
		// carry the union of iterations 0..k-1's synthesized inputs.
		savedInferInputs := append([]TypeId(nil), c.inferInputs...)
		for _, sig := range viable {
			c.Fork(stackSnap)
			c.subst.Rollback(substSnap)
			c.inferInputs = append([]TypeId(nil), savedInferInputs...)
			savedErrs := c.errors
			c.errors = nil
			c.applySig(sig, callSite)
			stepErrs := c.errors
			c.errors = savedErrs
			if len(stepErrs) == 0 {
				c.branchSpawn = append(c.branchSpawn, c.captureBranch())
			}
		}
		c.Fork(stackSnap)
		c.subst.Rollback(substSnap)
		c.inferInputs = savedInferInputs
		return
	}

	// Non-branching mode (legacy / direct CheckTokens path): the
	// caller can't fan out, so any pick is a magic pick. Surface the
	// ambiguity as an error and apply the first viable candidate
	// only for diagnostic recovery.
	c.errors = append(c.errors, TypeError{
		Kind: TErrAmbiguousOverload,
		Pos:  callSite,
		Hint: c.formatOverloadAmbiguityFromSigs(viable),
	})
	c.applySig(viable[0], callSite)
}

// formatOverloadFailure renders a multi-line hint describing the
// observed stack and every candidate signature. Used when no overload
// unified against the live stack — the reader needs to see *what*
// they had vs. *what was possible*.
func (c *Checker) formatOverloadFailure(candidates []QuoteSig) string {
	var sb strings.Builder
	sb.WriteString("no listed signature accepts the current stack\n")
	sb.WriteString("  stack (top first):")
	sb.WriteString(c.formatStackForDiagnostic())
	sb.WriteString("\n  candidates:")
	for _, cand := range candidates {
		sb.WriteString("\n    ")
		sb.WriteString(FormatType(c.arena, c.names, c.arena.MakeQuote(cand)))
	}
	return sb.String()
}

// formatOverloadAmbiguityFromSigs is the ambiguous-overload counterpart
// for the legacy (non-branching) path. Every viable candidate is listed
// since they're all the user needs to disambiguate.
func (c *Checker) formatOverloadAmbiguityFromSigs(viable []QuoteSig) string {
	var sb strings.Builder
	sb.WriteString("multiple overloads match\n")
	sb.WriteString("  stack (top first):")
	sb.WriteString(c.formatStackForDiagnostic())
	sb.WriteString("\n  viable candidates:")
	for _, sig := range viable {
		sb.WriteString("\n    ")
		sb.WriteString(FormatType(c.arena, c.names, c.arena.MakeQuote(sig)))
	}
	return sb.String()
}

// formatStackForDiagnostic prints the current stack with substitutions
// applied so type variables resolve to whatever they were bound to.
// One item per line, top of stack first.
func (c *Checker) formatStackForDiagnostic() string {
	if c.stack.Len() == 0 {
		return " <empty>"
	}
	var sb strings.Builder
	for i := c.stack.Len() - 1; i >= 0; i-- {
		sb.WriteString("\n    ")
		sb.WriteString(FormatType(c.arena, c.names, c.subst.Apply(c.arena, c.stack.items[i])))
	}
	return sb.String()
}

// emitDebugDump renders the current branch's stack and bound-var
// environment, appends an info-severity TypeError (so the LSP /
// editor squiggle layer surfaces it inline), and also writes the same
// snapshot to stderr so CLI users see it during --type-check-only
// runs. Each surviving branch hits its own emitDebugDump
// independently, so multiple branches naturally produce multiple
// outputs — the count tells you how many branches were alive here.
func (c *Checker) emitDebugDump(tok Token) {
	var sb strings.Builder
	sb.WriteString("\n  stack (top first):")
	sb.WriteString(c.formatStackForDiagnostic())
	sb.WriteString("\n  vars:")
	allNames := make(map[NameId]struct{}, len(c.vars.bound)+len(c.vars.maybeBound))
	for id := range c.vars.bound {
		allNames[id] = struct{}{}
	}
	for id := range c.vars.maybeBound {
		allNames[id] = struct{}{}
	}
	if len(allNames) == 0 {
		sb.WriteString(" <none>")
	} else {
		ordered := make([]NameId, 0, len(allNames))
		for id := range allNames {
			ordered = append(ordered, id)
		}
		sort.Slice(ordered, func(i, j int) bool {
			return c.names.Name(ordered[i]) < c.names.Name(ordered[j])
		})
		for _, id := range ordered {
			name := c.names.Name(id)
			if t, ok := c.vars.bound[id]; ok {
				sb.WriteString("\n    ")
				sb.WriteString(name)
				sb.WriteString(" : ")
				sb.WriteString(FormatType(c.arena, c.names, c.subst.Apply(c.arena, t)))
			}
			if t, ok := c.vars.maybeBound[id]; ok {
				sb.WriteString("\n    ?")
				sb.WriteString(name)
				sb.WriteString(" : ")
				sb.WriteString(FormatType(c.arena, c.names, c.subst.Apply(c.arena, t)))
			}
		}
	}
	hint := sb.String()
	c.errors = append(c.errors, TypeError{
		Severity: SeverityInfo,
		Kind:     TErrDebugDump,
		Pos:      tok,
		Hint:     hint,
	})
	fmt.Fprintf(os.Stderr, "dbg at line %d, column %d:%s\n", tok.Line, tok.Column, hint)
}
