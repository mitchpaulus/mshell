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
// applied; if more than one is viable the dispatch fans out via
// branchSpawn and the branching driver explores every alternative.
//
// Resolution algorithm:
//
//   1. Expand candidates into trial scenarios: union inputs facing an
//      unbound-variable operand split per arm, and overloaded-quote
//      operands facing a quote input split per arm (the operand slot
//      is pinned to a single-arm quote for that scenario).
//   2. Snapshot the stack and the substitution.
//   3. For each scenario:
//        a. Restore both snapshots and apply the scenario's pins.
//        b. Instantiate the candidate (fresh-rename its generics).
//        c. If the candidate's arity exceeds the stack and we are
//           not in inferring mode, drop it.
//        d. Trial-unify each input against the matching stack slot.
//           Any failure drops the scenario.
//   4. Restore both snapshots so the actual application starts clean.
//   5. With one viable, apply it. With zero, report
//      TErrNoMatchingOverload and recover. With multiple, fan out via
//      branchSpawn.

func (c *Checker) resolveAndApply(candidates []QuoteSig, callSite Token) {
	candidates = c.expandUnionOperands(candidates)
	scenarios := c.expandOverloadedQuoteOperands(candidates)
	if len(scenarios) == 1 {
		c.applyPins(scenarios[0].pins)
		c.applySig(scenarios[0].sig, callSite)
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

	var viable []dispatchScenario

	for _, sc := range scenarios {
		c.Fork(stackSnap)
		c.subst.Rollback(substSnap)
		c.applyPins(sc.pins)

		instantiated := c.Instantiate(sc.sig)
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
		for _, i := range c.inputUnifyOrder(instantiated.Inputs, base) {
			if !c.unify(c.stack.items[base+i], instantiated.Inputs[i]) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		viable = append(viable, sc)
	}

	c.Fork(stackSnap)
	c.subst.Rollback(substSnap)

	if len(viable) == 0 {
		// No candidate accepted the operands as-is. If an operand is a
		// union (e.g. `int | float` from a joined match/if or a
		// union-typed def input), try distributing: resolve the call
		// for every member-combination and, when all are covered, push
		// the union of their outputs. This is sound because each
		// candidate mirrors its runtime switch, so proving every
		// member-combination is handled proves every runtime value is
		// handled. Distribution runs before the inference punt below —
		// it requires the operands to actually be on the stack, so it
		// never preempts genuine underflow synthesis.
		if c.tryDistributeOverUnion(candidates) {
			return
		}
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
		c.applyPins(viable[0].pins)
		c.applySig(viable[0].sig, callSite)
		return
	}

	// Multiple scenarios remain viable. Fan out every viable
	// continuation into the branchSpawn slice and let the caller (the
	// branching driver via tryBranchStep) propagate the alternatives.
	// Downstream constraints prune; if none do, the end-of-walk
	// reconciler joins or reports TErrAmbiguousTyping.
	//
	// applySig (in inferring mode) mutates c.inferInputs by
	// prepending freshly-synthesized inputs. Capture the
	// pre-call value so each fan-out iteration sees the same
	// starting point — otherwise iteration k's branch would
	// carry the union of iterations 0..k-1's synthesized inputs.
	savedInferInputs := append([]TypeId(nil), c.inferInputs...)
	for _, sc := range viable {
		c.Fork(stackSnap)
		c.subst.Rollback(substSnap)
		c.applyPins(sc.pins)
		c.inferInputs = append([]TypeId(nil), savedInferInputs...)
		savedErrs := c.errors
		c.errors = nil
		c.applySig(sc.sig, callSite)
		stepErrs := c.errors
		c.errors = savedErrs
		if len(stepErrs) == 0 {
			c.branchSpawn = append(c.branchSpawn, c.captureBranch())
		}
	}
	c.Fork(stackSnap)
	c.subst.Rollback(substSnap)
	c.inferInputs = savedInferInputs
}

// dispatchScenario is one (candidate, operand-arm choice) combination
// explored by resolveAndApply. pins replace overloaded-quote stack
// operands with one of their single-arm quotes for the duration of the
// trial and the eventual application.
type dispatchScenario struct {
	sig  QuoteSig
	pins []slotPin
}

type slotPin struct {
	idx int // absolute index into the stack
	t   TypeId
}

func (c *Checker) applyPins(pins []slotPin) {
	for _, p := range pins {
		c.stack.items[p.idx] = p.t
	}
}

// expandOverloadedQuoteOperands turns candidates into trial scenarios.
// For every input position whose declared type is a quote and whose
// stack operand is an overloaded quote, the scenario set fans out one
// alternative per arm. Greedy first-arm unification inside unify cannot
// account for constraints that arrive from sibling operands or outputs
// — e.g. `f : (( -- t) ( -- t) -- t)` with an overloaded first thunk
// must choose the arm under which the second thunk still unifies.
// Pinning the operand to a single-arm quote per scenario lets the
// ordinary trial / fan-out machinery make that choice globally instead.
//
// Expansion only fires when the candidate input is itself TKQuote: a
// plain-variable input binds to the whole overloaded quote, preserving
// the alternatives for the eventual consumer (`x`, iff, ...) to
// resolve. Combinations beyond maxUnionOperandVariants are kept
// unexpanded, falling back to greedy unification.
func (c *Checker) expandOverloadedQuoteOperands(candidates []QuoteSig) []dispatchScenario {
	scenarios := make([]dispatchScenario, 0, len(candidates))
	for _, cand := range candidates {
		variants := []dispatchScenario{{sig: cand}}
		if len(c.stack.items) >= len(cand.Inputs) {
			base := len(c.stack.items) - len(cand.Inputs)
			for i, in := range cand.Inputs {
				if c.arena.Kind(in) != TKQuote {
					continue
				}
				got := c.subst.Apply(c.arena, c.stack.items[base+i])
				gn := c.arena.Node(got)
				if gn.Kind != TKOverloadedQuote {
					continue
				}
				arms := c.arena.overloadedQuoteSigs[gn.Extra]
				if len(variants)*len(arms) > maxUnionOperandVariants {
					continue
				}
				next := make([]dispatchScenario, 0, len(variants)*len(arms))
				for _, v := range variants {
					for _, arm := range arms {
						pins := append(append([]slotPin(nil), v.pins...),
							slotPin{idx: base + i, t: c.arena.MakeQuote(arm)})
						next = append(next, dispatchScenario{sig: v.sig, pins: pins})
					}
				}
				variants = next
			}
		}
		scenarios = append(scenarios, variants...)
	}
	return scenarios
}

// expandUnionOperands splits a candidate whose input is an unbranded
// union, while the corresponding stack operand is a still-unbound type
// variable, into one candidate per union arm. A union input is the
// compact spelling of same-output overload arms; for concrete operands
// the two are equivalent, but unifying an unbound variable against the
// whole union would freeze the variable as that union — downstream ops
// could then no longer narrow it the way the branching walker narrows
// overload alternatives. (E.g. inferring `(n! @n wl @n 3 <)`: `wl` is
// `(str | int -- )`; the bound variable must stay narrowable so the
// later `<` can prune to the int alternative.) Splitting restores the
// per-arm fan-out exactly in that case.
//
// Candidates whose expansion would exceed maxUnionOperandVariants are
// kept unsplit — the union-freezing behavior is still sound, just less
// permissive.
const maxUnionOperandVariants = 16

func (c *Checker) expandUnionOperands(candidates []QuoteSig) []QuoteSig {
	out := candidates
	changed := false
	for ci, cand := range candidates {
		variants := []QuoteSig{cand}
		if len(c.stack.items) >= len(cand.Inputs) {
			base := len(c.stack.items) - len(cand.Inputs)
			for i, in := range cand.Inputs {
				n := c.arena.Node(in)
				if n.Kind != TKUnion || n.A != 0 {
					continue
				}
				got := c.subst.Apply(c.arena, c.stack.items[base+i])
				if c.arena.Kind(got) != TKVar {
					continue
				}
				arms := c.arena.unionMembers[n.Extra]
				if len(variants)*len(arms) > maxUnionOperandVariants {
					continue
				}
				next := make([]QuoteSig, 0, len(variants)*len(arms))
				for _, v := range variants {
					for _, arm := range arms {
						nv := v
						nv.Inputs = append([]TypeId(nil), v.Inputs...)
						nv.Inputs[i] = arm
						next = append(next, nv)
					}
				}
				variants = next
			}
		}
		if len(variants) == 1 {
			if changed {
				out = append(out, variants[0])
			}
			continue
		}
		if !changed {
			out = append([]QuoteSig(nil), candidates[:ci]...)
			changed = true
		}
		out = append(out, variants...)
	}
	return out
}

// inputUnifyOrder returns the order in which a sig's inputs are unified
// against the stack: operands that are not quote values first, quote
// values last. Quote-valued operands (especially overloaded quotes from
// bare `(+)`-style literals) commit to one arm during unification, so
// every other operand gets to pin the sig's generics before that choice
// is made — e.g. foldl's accumulator and list must bind `a` and `t`
// before the folding quote picks among its overload arms.
func (c *Checker) inputUnifyOrder(inputs []TypeId, base int) []int {
	order := make([]int, 0, len(inputs))
	var quotes []int
	for i := range inputs {
		got := c.subst.Apply(c.arena, c.stack.items[base+i])
		if isQuoteKind(c.arena.Kind(got)) {
			quotes = append(quotes, i)
			continue
		}
		order = append(order, i)
	}
	return append(order, quotes...)
}

// maxUnionDistributionCombos caps the cartesian product explored by
// tryDistributeOverUnion so a call with several wide union operands
// can't blow up resolution time. Beyond the cap, distribution bails and
// the normal "no matching overload" error is produced.
const maxUnionDistributionCombos = 64

// tryDistributeOverUnion handles the case where overload resolution
// found no whole-stack match because an operand is a union. It treats
// each union operand as the set of its members, enumerates the
// cartesian product of member choices across the operand window, and
// resolves the call for each concrete assignment. If every assignment
// is handled by some candidate, the call is accepted and the per-slot
// union of the assignments' outputs is pushed; the operands are
// consumed as usual. If any assignment is unhandled (e.g. `int / float`
// inside `(int|float) / float`), distribution fails and the caller
// surfaces the normal error — so an unsound mixed combination is still
// rejected.
//
// Returns true if it applied the call (state mutated), false if it
// declined (state restored to the pre-call stack).
//
// Restrictions that keep this sound and bounded:
//   - All candidates must share input and output arity. Differing
//     arities make "how many operands to consume / expand" ambiguous.
//   - Every chosen output must be fully concrete (no free type vars).
//     A generic output would carry vars bound only under a per-assignment
//     substitution that is rolled back here, leaving a dangling var in
//     the merged union. Generic-on-the-whole-union calls already resolve
//     via the normal path (the var binds to the union), so they never
//     reach distribution.
func (c *Checker) tryDistributeOverUnion(candidates []QuoteSig) bool {
	arity := len(candidates[0].Inputs)
	outArity := len(candidates[0].Outputs)
	for _, cand := range candidates[1:] {
		if len(cand.Inputs) != arity || len(cand.Outputs) != outArity {
			return false
		}
	}
	if arity == 0 || c.stack.Len() < arity {
		return false
	}

	stackSnap := c.Snapshot()
	substSnap := c.subst.Checkpoint()
	base := c.stack.Len() - arity

	// Member list per operand slot: the union's arms, or a singleton for
	// a non-union operand. Bail unless at least one slot is a union.
	memberLists := make([][]TypeId, arity)
	anyUnion := false
	combos := 1
	for i := 0; i < arity; i++ {
		t := c.subst.Apply(c.arena, c.stack.items[base+i])
		n := c.arena.Node(t)
		if n.Kind == TKUnion {
			arms := c.arena.unionMembers[n.Extra]
			cp := make([]TypeId, len(arms))
			copy(cp, arms)
			memberLists[i] = cp
			anyUnion = true
		} else {
			memberLists[i] = []TypeId{t}
		}
		combos *= len(memberLists[i])
		if combos > maxUnionDistributionCombos {
			return false
		}
	}
	if !anyUnion {
		return false
	}

	// Walk the cartesian product as an odometer over member indices.
	// For each assignment, find a viable candidate and record its
	// resolved outputs per slot. Bail on the first unhandled assignment.
	outArms := make([][]TypeId, outArity)
	idx := make([]int, arity)
	for {
		found := false
		for _, cand := range candidates {
			c.Fork(stackSnap)
			c.subst.Rollback(substSnap)
			for i := 0; i < arity; i++ {
				c.stack.items[base+i] = memberLists[i][idx[i]]
			}
			inst := c.Instantiate(cand)
			ok := true
			for i, want := range inst.Inputs {
				if !c.unify(c.stack.items[base+i], want) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			// Record resolved outputs; bail if any carries a free var.
			resolved := make([]TypeId, outArity)
			for j, out := range inst.Outputs {
				rt := c.subst.Apply(c.arena, out)
				if c.arena.walkTypeVars(rt, func(TypeVarId) bool { return true }) {
					c.Fork(stackSnap)
					c.subst.Rollback(substSnap)
					return false
				}
				resolved[j] = rt
			}
			for j := 0; j < outArity; j++ {
				outArms[j] = append(outArms[j], resolved[j])
			}
			found = true
			break
		}
		if !found {
			c.Fork(stackSnap)
			c.subst.Rollback(substSnap)
			return false
		}
		k := arity - 1
		for ; k >= 0; k-- {
			idx[k]++
			if idx[k] < len(memberLists[k]) {
				break
			}
			idx[k] = 0
		}
		if k < 0 {
			break
		}
	}

	// Every combination is handled. Consume the operands and push the
	// per-slot union of the assignments' outputs.
	c.Fork(stackSnap)
	c.subst.Rollback(substSnap)
	c.stack.items = c.stack.items[:base]
	for j := 0; j < outArity; j++ {
		c.stack.Push(c.arena.MakeUnion(outArms[j], 0))
	}
	return true
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
