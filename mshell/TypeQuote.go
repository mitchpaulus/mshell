package main

// Quote-body inference — branching variant.
//
// A quote literal's body is type-checked against an empty stack (with
// underflow synthesizing fresh inputs) to derive its signature. When
// an internal operation is overloaded, instead of picking one
// overload arbitrarily and committing, the inference branches: we
// try every viable overload and continue. Each surviving branch
// yields a candidate QuoteSig. The resulting set is what the quote
// literal pushes — one sig collapses to TKQuote, multiple to
// TKOverloadedQuote. Downstream consumers (filter/map/each/iff/x)
// already know how to resolve an overloaded quote at the use site
// via the existing overload machinery, so the dispatch story is the
// same; we just don't lose alternatives at inference time.
//
// Errors from dead-end branches (some viable choice fails partway
// through the body) are dropped silently — same as overload
// resolution dropping non-matching candidates at a call site. Only
// when *every* branch dies at the same body step do we surface the
// error, and the error reported is whichever the failing step
// produced (typically TErrNoMatchingOverload pointing at the token).
//
// Branches are capped at quoteBranchCap to bound pathological
// constructions (e.g. several independent fresh inputs each consumed
// by a many-overload op). Realistic quotes stay well under the cap.

// quoteBranchCap is the maximum number of simultaneously-live
// inference branches. Exceeding it falls back to keeping the first
// quoteBranchCap branches and dropping the rest, which preserves
// soundness (each surviving branch is a valid typing) at the cost of
// completeness in pathological inputs.
const quoteBranchCap = 1024

// quoteBranch captures all checker state that varies across
// alternative inference paths through a quote body. The substitution
// is checkpointed (full bound-slice copy) because per-branch
// unifications must not leak into siblings.
type quoteBranch struct {
	stack       []TypeId
	vars        map[NameId]TypeId
	maybeVars   map[NameId]TypeId
	inferInputs []TypeId
	diverged    bool
	substCp     SubstCheckpoint
}

// InferQuoteSig runs branching inference over a raw-token body and
// returns the set of viable signatures. Used by older tests and the
// raw-token path; the parse-tree variant is InferQuoteSigItems.
func (c *Checker) InferQuoteSig(body []Token) []QuoteSig {
	step := func(tok Token) {
		c.checkOne(tok)
	}
	return c.inferQuoteSigsTokens(body, nil, step)
}

// InferQuoteSigItems is the parse-tree variant of InferQuoteSig.
func (c *Checker) InferQuoteSigItems(body []MShellParseItem) []QuoteSig {
	return c.InferQuoteSigItemsWithInputs(body, nil)
}

// InferQuoteSigItemsWithInputs is InferQuoteSigItems with caller-
// supplied initial inputs (used by prefix-quote desugaring).
func (c *Checker) InferQuoteSigItemsWithInputs(body []MShellParseItem, initialInputs []TypeId) []QuoteSig {
	return c.inferQuoteSigsItems(body, initialInputs)
}

// inferQuoteSigsTokens drives branching inference over a raw-token
// body. For each token the driver determines whether it has multiple
// candidate sigs (overloaded builtin / named def); if so, every
// branch fans out by one viable candidate per overload. Non-
// overloaded tokens advance each branch deterministically via the
// supplied step function.
func (c *Checker) inferQuoteSigsTokens(body []Token, initialInputs []TypeId, step func(Token)) []QuoteSig {
	outerSnap := c.Snapshot()
	outerInferring := c.inferring
	outerInputs := c.inferInputs
	outerDiverged := c.diverged
	outerSubst := c.subst.Checkpoint()

	defer func() {
		c.inferring = outerInferring
		c.inferInputs = outerInputs
		c.Fork(outerSnap)
		c.diverged = outerDiverged
		// Roll the substitution back to what it was before this
		// inference. Each surviving sig has carried its Generics
		// list across, so consumers Instantiate per-use. Any
		// inference-time bindings that aren't captured in Generics
		// were branch-local and would leak into the surrounding
		// scope (binding fresh vars from outer inferInputs to
		// concrete types from this body's last branch).
		c.subst.Rollback(outerSubst)
	}()

	branches := []quoteBranch{c.initialQuoteBranch(initialInputs, outerSnap)}

	for _, tok := range body {
		candidates := c.tokenOverloadCandidates(tok)
		nextBranches := make([]quoteBranch, 0, len(branches))
		var lastErrors []TypeError

		for _, b := range branches {
			if len(candidates) > 1 {
				for _, cand := range candidates {
					nb, errs, ok := c.tryBranchApply(b, cand, tok)
					if ok {
						nextBranches = append(nextBranches, nb)
						if len(nextBranches) >= quoteBranchCap {
							break
						}
					} else if len(errs) > 0 {
						lastErrors = errs
					}
				}
			} else {
				nb, errs, ok := c.tryBranchStep(b, func() { step(tok) })
				if ok {
					nextBranches = append(nextBranches, nb)
				} else if len(errs) > 0 {
					lastErrors = errs
				}
			}
			if len(nextBranches) >= quoteBranchCap {
				break
			}
		}

		if len(nextBranches) == 0 {
			// Every branch died at this step. Surface the failing
			// step's error verbatim; pick the first since they all
			// pointed at the same token with the same cause.
			if len(lastErrors) > 0 {
				c.errors = append(c.errors, lastErrors[0])
			}
			return c.recoveryQuoteSigs()
		}
		branches = nextBranches
	}

	return c.collectQuoteSigs(branches, initialInputs, outerSnap)
}

// inferQuoteSigsItems is the parse-item analog of inferQuoteSigsTokens.
// Most parse items aren't tokens — composite items (lists, dicts,
// nested quotes, if/match blocks, etc.) are walked deterministically
// per branch through checkParseItem and contribute one continuation
// each. Only Token items with overloaded candidates fan out.
func (c *Checker) inferQuoteSigsItems(body []MShellParseItem, initialInputs []TypeId) []QuoteSig {
	outerSnap := c.Snapshot()
	outerInferring := c.inferring
	outerInputs := c.inferInputs
	outerDiverged := c.diverged
	outerSubst := c.subst.Checkpoint()

	defer func() {
		c.inferring = outerInferring
		c.inferInputs = outerInputs
		c.Fork(outerSnap)
		c.diverged = outerDiverged
		// See inferQuoteSigsTokens for why we roll back the
		// substitution explicitly here.
		c.subst.Rollback(outerSubst)
	}()

	branches := []quoteBranch{c.initialQuoteBranch(initialInputs, outerSnap)}

	for _, item := range body {
		var candidates []QuoteSig
		var callTok Token
		if tok, ok := item.(Token); ok {
			candidates = c.tokenOverloadCandidates(tok)
			callTok = tok
		}

		nextBranches := make([]quoteBranch, 0, len(branches))
		var lastErrors []TypeError

		for _, b := range branches {
			if len(candidates) > 1 {
				for _, cand := range candidates {
					nb, errs, ok := c.tryBranchApply(b, cand, callTok)
					if ok {
						nextBranches = append(nextBranches, nb)
						if len(nextBranches) >= quoteBranchCap {
							break
						}
					} else if len(errs) > 0 {
						lastErrors = errs
					}
				}
			} else {
				captured := item
				nb, errs, ok := c.tryBranchStep(b, func() { c.checkParseItem(captured) })
				if ok {
					nextBranches = append(nextBranches, nb)
				} else if len(errs) > 0 {
					lastErrors = errs
				}
			}
			if len(nextBranches) >= quoteBranchCap {
				break
			}
		}

		if len(nextBranches) == 0 {
			if len(lastErrors) > 0 {
				c.errors = append(c.errors, lastErrors[0])
			}
			return c.recoveryQuoteSigs()
		}
		branches = nextBranches
	}

	return c.collectQuoteSigs(branches, initialInputs, outerSnap)
}

// tokenOverloadCandidates returns the overload set for a Token if it
// maps to one. Nil/empty means "no fan-out at this token" — the step
// is applied deterministically. A single-entry result also returns
// nil so the deterministic path is taken; only true >1 fan-out
// triggers branching.
//
// Some tokens get special handling in checkOne (tryIff, tryLoop,
// tryRejectPathWrite, the redirect detectors, etc.) before the
// resolveAndApply fallback. Naively calling applySig with each
// candidate skips that special handling and produces wrong results
// — e.g. an `iff` whose arms are quotes is reconciled through
// ReconcileArms, not raw input unification. For those tokens we
// return nil and the deterministic step path runs checkOne, which
// preserves the existing semantics.
func (c *Checker) tokenOverloadCandidates(tok Token) []QuoteSig {
	if tokenHasSpecialHandling(tok) {
		return nil
	}
	if sigs, ok := c.builtins[tok.Type]; ok && len(sigs) > 1 {
		return sigs
	}
	if tok.Type == LITERAL {
		nameId := c.names.Intern(tok.Lexeme)
		if sigs, ok := c.nameBuiltins[nameId]; ok && len(sigs) > 1 {
			return sigs
		}
	}
	return nil
}

// tokenHasSpecialHandling lists tokens that have structurally
// special pre-resolveAndApply handling in checkOne that can't be
// replicated by applying a single sig. The redirect tokens
// (`<`/`>`/etc.) are intentionally NOT here: their tryQuoteRedirect
// / tryCommandRedirect helpers only fire when the stack already has
// a TKQuote/TKCommand with a target string/path/bytes on top, which
// doesn't occur during fresh-stack quote-body inference. Branching
// for them resolves their comparison overloads correctly.
//
// The LITERAL specials (tryAppend, tryGet, tryReturn, etc.) are also
// not listed: their fast paths require specific stack shapes that
// aren't produced by inference-time fresh-var synthesis. If the user
// writes a quote body that does land on such a shape mid-walk,
// resolveAndApply (via applySig in a branch) still produces the
// right effect — just without the bespoke union-aware refinements
// tryAppend would add. That's an acceptable trade for not losing
// overload alternatives at inference time.
func tokenHasSpecialHandling(tok Token) bool {
	switch tok.Type {
	case IFF, BREAK, CONTINUE, LOOP, PIPE:
		return true
	}
	return false
}

// initialQuoteBranch builds the starting branch for a body walk:
// inherit the outer var environment (quotes are closures over the
// surrounding scope), seed the stack with the caller-supplied
// initial inputs (used by prefix-quote desugaring), and start
// inferring with no fresh inputs accumulated yet.
func (c *Checker) initialQuoteBranch(initialInputs []TypeId, outerSnap ScopeSnapshot) quoteBranch {
	stack := append([]TypeId(nil), initialInputs...)
	inheritedVars := make(map[NameId]TypeId, len(outerSnap.vars))
	for k, v := range outerSnap.vars {
		inheritedVars[k] = v
	}
	inheritedMaybe := make(map[NameId]TypeId, len(outerSnap.maybeVars))
	for k, v := range outerSnap.maybeVars {
		inheritedMaybe[k] = v
	}
	return quoteBranch{
		stack:       stack,
		vars:        inheritedVars,
		maybeVars:   inheritedMaybe,
		inferInputs: nil,
		diverged:    false,
		substCp:     c.subst.Checkpoint(),
	}
}

// loadBranch installs a branch's state onto the checker so the next
// step (applySig or checkParseItem) operates on it.
func (c *Checker) loadBranch(b quoteBranch) {
	c.subst.Rollback(b.substCp)
	c.stack.items = append(c.stack.items[:0], b.stack...)
	c.vars.bound = copyVarMap(b.vars)
	c.vars.maybeBound = copyVarMap(b.maybeVars)
	c.inferInputs = append([]TypeId(nil), b.inferInputs...)
	c.diverged = b.diverged
	c.inferring = true
}

// captureBranch reads the checker's current state into a quoteBranch
// after a step has finished applying.
func (c *Checker) captureBranch() quoteBranch {
	return quoteBranch{
		stack:       append([]TypeId(nil), c.stack.items...),
		vars:        copyVarMap(c.vars.bound),
		maybeVars:   copyVarMap(c.vars.maybeBound),
		inferInputs: append([]TypeId(nil), c.inferInputs...),
		diverged:    c.diverged,
		substCp:     c.subst.Checkpoint(),
	}
}

func copyVarMap(m map[NameId]TypeId) map[NameId]TypeId {
	out := make(map[NameId]TypeId, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// tryBranchApply applies a single candidate sig to a forked branch.
// Errors generated by applySig are captured locally — they do not
// pollute c.errors unless every branch fails and the driver surfaces
// one of them. Returns the new branch state, the locally-captured
// errors, and a boolean indicating success.
func (c *Checker) tryBranchApply(b quoteBranch, cand QuoteSig, callTok Token) (quoteBranch, []TypeError, bool) {
	c.loadBranch(b)
	savedErrs := c.errors
	c.errors = nil
	c.applySig(cand, callTok)
	produced := c.errors
	c.errors = savedErrs
	if len(produced) > 0 {
		return quoteBranch{}, produced, false
	}
	return c.captureBranch(), nil, true
}

// tryBranchStep is the non-overloaded counterpart of tryBranchApply.
// Used for parse items whose effect is deterministic (literals,
// non-overloaded operations, composite items handled internally).
func (c *Checker) tryBranchStep(b quoteBranch, step func()) (quoteBranch, []TypeError, bool) {
	c.loadBranch(b)
	savedErrs := c.errors
	c.errors = nil
	step()
	produced := c.errors
	c.errors = savedErrs
	if len(produced) > 0 {
		return quoteBranch{}, produced, false
	}
	return c.captureBranch(), nil, true
}

// collectQuoteSigs converts surviving branches into their final
// QuoteSigs (inputs resolved through each branch's substitution,
// outputs and bindings likewise). Loads each branch in turn so the
// substitution state lines up before applying.
//
// Each sig's free TypeVarIds (those that remained unbound after the
// branch's walk) are collected into the sig's Generics list. This is
// load-bearing for the multi-sig case: every branch shares the same
// global substitution slots, so a free var in branch A's sig points
// to the same arena TKVar node as a free var in branch B's sig. If
// we leave them ungeneralized, a consumer applying the resulting
// TKOverloadedQuote later reads through the substitution at that
// later time — which may have arbitrary bindings from upstream — and
// gets the wrong type. Adding the free vars to Generics makes
// Instantiate rename them to fresh per-use-site vars, restoring
// independence.
func (c *Checker) collectQuoteSigs(branches []quoteBranch, initialInputs []TypeId, outerSnap ScopeSnapshot) []QuoteSig {
	sigs := make([]QuoteSig, 0, len(branches))
	for _, b := range branches {
		c.loadBranch(b)

		rawInputs := append(append([]TypeId(nil), c.inferInputs...), initialInputs...)
		rawOutputs := append([]TypeId(nil), c.stack.items...)
		rawBindings := quoteBindingDelta(outerSnap.vars, c.vars.bound)

		inputs := make([]TypeId, len(rawInputs))
		for i, in := range rawInputs {
			inputs[i] = c.subst.Apply(c.arena, in)
		}
		outputs := make([]TypeId, len(rawOutputs))
		for i, out := range rawOutputs {
			outputs[i] = c.subst.Apply(c.arena, out)
		}
		bindings := c.applyBindingTypes(rawBindings)

		generics := collectFreeTypeVars(c.arena, inputs, outputs, bindings)

		sigs = append(sigs, QuoteSig{
			Inputs:   inputs,
			Outputs:  outputs,
			Diverges: b.diverged,
			Bindings: bindings,
			Generics: generics,
		})
	}
	return dedupeQuoteSigs(sigs)
}

// collectFreeTypeVars returns the deduplicated list of TypeVarIds
// appearing in any of the supplied type slices / binding maps. Used
// by collectQuoteSigs to populate a QuoteSig's Generics so Instantiate
// at the consumer side renames them.
func collectFreeTypeVars(arena *TypeArena, inputs, outputs []TypeId, bindings map[NameId]TypeId) []TypeVarId {
	seen := make(map[TypeVarId]struct{})
	var ordered []TypeVarId
	visit := func(t TypeId) { walkFreeTypeVars(arena, t, seen, &ordered) }
	for _, in := range inputs {
		visit(in)
	}
	for _, out := range outputs {
		visit(out)
	}
	for _, t := range bindings {
		visit(t)
	}
	if len(ordered) == 0 {
		return nil
	}
	return ordered
}

// walkFreeTypeVars descends a TypeId and appends any TKVar's
// TypeVarId to `ordered` (deduped via `seen`). Composite kinds are
// recursed structurally; lookups don't go through the substitution
// because callers pass already-Apply'd TypeIds, so any remaining
// TKVar is genuinely free.
func walkFreeTypeVars(arena *TypeArena, t TypeId, seen map[TypeVarId]struct{}, ordered *[]TypeVarId) {
	n := arena.Node(t)
	switch n.Kind {
	case TKVar:
		id := TypeVarId(n.A)
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			*ordered = append(*ordered, id)
		}
	case TKMaybe:
		walkFreeTypeVars(arena, TypeId(n.A), seen, ordered)
	case TKList:
		walkFreeTypeVars(arena, TypeId(n.A), seen, ordered)
	case TKDict:
		walkFreeTypeVars(arena, TypeId(n.A), seen, ordered)
		walkFreeTypeVars(arena, TypeId(n.B), seen, ordered)
	case TKQuote:
		sig := arena.QuoteSig(t)
		for _, in := range sig.Inputs {
			walkFreeTypeVars(arena, in, seen, ordered)
		}
		for _, out := range sig.Outputs {
			walkFreeTypeVars(arena, out, seen, ordered)
		}
	case TKOverloadedQuote:
		for _, sig := range arena.overloadedQuoteSigs[n.Extra] {
			for _, in := range sig.Inputs {
				walkFreeTypeVars(arena, in, seen, ordered)
			}
			for _, out := range sig.Outputs {
				walkFreeTypeVars(arena, out, seen, ordered)
			}
		}
	case TKUnion:
		for _, member := range arena.unionMembers[n.Extra] {
			walkFreeTypeVars(arena, member, seen, ordered)
		}
	case TKShape:
		for _, f := range arena.shapeFields[n.Extra] {
			walkFreeTypeVars(arena, f.Type, seen, ordered)
		}
	}
}

// dedupeQuoteSigs drops sigs that are structurally identical to an
// earlier sig in the slice. Order is preserved (first occurrence
// wins) so callers can rely on candidate-source order when relevant.
func dedupeQuoteSigs(sigs []QuoteSig) []QuoteSig {
	if len(sigs) <= 1 {
		return sigs
	}
	out := make([]QuoteSig, 0, len(sigs))
	for i, s := range sigs {
		dup := false
		for j := 0; j < i; j++ {
			if quoteSigEqual(s, sigs[j]) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, s)
		}
	}
	return out
}

func quoteSigEqual(a, b QuoteSig) bool {
	if len(a.Inputs) != len(b.Inputs) || len(a.Outputs) != len(b.Outputs) {
		return false
	}
	if a.Diverges != b.Diverges || a.Fail != b.Fail || a.Pure != b.Pure {
		return false
	}
	for i := range a.Inputs {
		if a.Inputs[i] != b.Inputs[i] {
			return false
		}
	}
	for i := range a.Outputs {
		if a.Outputs[i] != b.Outputs[i] {
			return false
		}
	}
	return true
}

// recoveryQuoteSigs returns a synthetic single-sig set used when
// every branch died. The shape ( -- T0 ) lets downstream
// type-checking continue with a fresh-var output, mirroring the
// recovery strategy used elsewhere on overload failure.
func (c *Checker) recoveryQuoteSigs() []QuoteSig {
	fresh := c.subst.FreshVar(c.arena)
	return []QuoteSig{{Outputs: []TypeId{fresh}}}
}

// quoteBindingDelta returns the names whose bound type changed (or
// newly appeared) between an outer snapshot and the post-body var
// environment. Used to surface in-body `name!` stores on the quote's
// signature so callers like iff can pick them up.
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
