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
// alternative inference paths through a quote body OR a top-level /
// def-body walk. The substitution is checkpointed (full bound-slice
// copy) because per-branch unifications must not leak into siblings.
//
// `inferring` controls underflow handling for applySig: in quote-body
// inference it is true (stack underflow synthesizes fresh inputs); in
// top-level / def-body walks it is false (stack underflow is a real
// error). Storing it per-branch lets one shared driver serve both.
type quoteBranch struct {
	stack       []TypeId
	vars        map[NameId]TypeId
	maybeVars   map[NameId]TypeId
	inferInputs []TypeId
	diverged    bool
	inferring   bool
	substCp     SubstCheckpoint
}

// InferQuoteSig runs branching inference over a raw-token body and
// returns the set of viable signatures. Used by older tests and the
// raw-token path; the parse-tree variant is InferQuoteSigItems.
func (c *Checker) InferQuoteSig(body []Token) []QuoteSig {
	step := func(tok Token) {
		c.checkOne(tok)
	}
	return c.inferQuoteSigsTokens(body, step)
}

// InferQuoteSigItems is the parse-tree variant of InferQuoteSig.
func (c *Checker) InferQuoteSigItems(body []MShellParseItem) []QuoteSig {
	return c.inferQuoteSigsItems(body)
}

// inferQuoteSigsTokens drives branching inference over a raw-token
// body. For each token the driver determines whether it has multiple
// candidate sigs (overloaded builtin / named def); if so, every
// branch fans out by one viable candidate per overload. Non-
// overloaded tokens advance each branch deterministically via the
// supplied step function.
func (c *Checker) inferQuoteSigsTokens(body []Token, step func(Token)) []QuoteSig {
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

	branches := []quoteBranch{c.initialQuoteBranch(outerSnap)}
	branches = c.driveBranches(branches, len(body), func(i int) func() {
		tok := body[i]
		return func() { step(tok) }
	})

	if len(branches) == 0 {
		return c.recoveryQuoteSigs()
	}
	return c.collectQuoteSigs(branches, outerSnap)
}

// inferQuoteSigsItems is the parse-item analog of inferQuoteSigsTokens.
// Most parse items aren't tokens — composite items (lists, dicts,
// nested quotes, if/match blocks, etc.) are walked deterministically
// per branch through checkParseItem and contribute one continuation
// each. Only Token items with overloaded candidates fan out.
func (c *Checker) inferQuoteSigsItems(body []MShellParseItem) []QuoteSig {
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

	branches := []quoteBranch{c.initialQuoteBranch(outerSnap)}
	branches = c.driveBranchesOverItems(branches, body)

	if len(branches) == 0 {
		return c.recoveryQuoteSigs()
	}
	return c.collectQuoteSigs(branches, outerSnap)
}

// driveBranchesOverItems walks a parse-item body. Each item advances
// every live branch through checkParseItem; multi-dispatch sites
// inside the step (resolveAndApply with more than one viable
// candidate, INTERPRET on an overloaded quote, the prefix-quote
// handler) fan out via the checker's branchSpawn slice, which
// tryBranchStep reads. There is no fan-out at this level — the driver
// is intentionally one path so token-level specials (tryIff, tryGridJoin,
// tryReturn, tryRedirect, ...) inside checkOne always fire before
// dispatch.
func (c *Checker) driveBranchesOverItems(initial []quoteBranch, body []MShellParseItem) []quoteBranch {
	return c.driveBranches(initial, len(body), func(i int) func() {
		item := body[i]
		return func() { c.checkParseItem(item) }
	})
}

// walkJoined drives items through the branching driver from the current
// checker state and reconciles the surviving branches back into a single
// live state: one survivor is loaded directly; several that agree on
// stack size join per-slot (per-slot type unions via joinArmBranches);
// disagreeing sizes fall back to the first survivor. Used by the
// sub-walks that need a single value/state at the end (dict-literal
// values, grid cells, format-string blocks, else-if conditions) and by
// CheckTokens. Returns false when every branch died — the failing
// step's error is already recorded.
func (c *Checker) walkJoined(items []MShellParseItem) bool {
	branches := c.driveBranchesOverItems([]quoteBranch{c.captureBranch()}, items)
	if len(branches) == 0 {
		return false
	}
	live := filterLiveBranches(branches)
	if len(live) == 0 {
		c.loadBranch(branches[0])
		return true
	}
	if len(live) == 1 {
		c.loadBranch(live[0])
		return true
	}
	wantSize := len(live[0].stack)
	for _, b := range live[1:] {
		if len(b.stack) != wantSize {
			c.loadBranch(live[0])
			return true
		}
	}
	c.joinArmBranches(live)
	return true
}

// driveBranches is the shared core branching driver. At each step the
// `next` callback supplies a "step" closure that advances one branch.
// The driver returns the surviving branches (nil if all died at some
// step; the first representative error is added to c.errors before
// returning). Fan-out is handled entirely by branchSpawn, populated
// inside the step by multi-dispatch sites in resolveAndApply.
func (c *Checker) driveBranches(
	branches []quoteBranch,
	steps int,
	next func(i int) func(),
) []quoteBranch {
	for i := 0; i < steps; i++ {
		step := next(i)
		nextBranches := make([]quoteBranch, 0, len(branches))
		var lastErrors []TypeError

		for _, b := range branches {
			// Diverged branches (return / exit / propagated fail)
			// don't consume further items — pass them through
			// unchanged so the residual body doesn't underflow them.
			if b.diverged {
				nextBranches = append(nextBranches, b)
				if len(nextBranches) >= quoteBranchCap {
					break
				}
				continue
			}
			nbs, errs, ok := c.tryBranchStep(b, step)
			if ok {
				nextBranches = append(nextBranches, nbs...)
				if len(nextBranches) >= quoteBranchCap {
					break
				}
			} else if len(errs) > 0 {
				lastErrors = errs
			}
		}

		if len(nextBranches) == 0 {
			// Every branch died at this step. Surface the failing
			// step's error verbatim; pick the first since they all
			// pointed at the same token with the same cause.
			if len(lastErrors) > 0 {
				c.errors = append(c.errors, lastErrors[0])
			}
			return nil
		}
		branches = nextBranches
	}
	return branches
}

// initialQuoteBranch builds the starting branch for a body walk:
// inherit the outer var environment (quotes are closures over the
// surrounding scope), start with an empty stack, and begin inferring
// with no fresh inputs accumulated yet.
func (c *Checker) initialQuoteBranch(outerSnap ScopeSnapshot) quoteBranch {
	return quoteBranch{
		stack:       nil,
		vars:        copyVarMap(outerSnap.vars),
		maybeVars:   copyVarMap(outerSnap.maybeVars),
		inferInputs: nil,
		diverged:    false,
		inferring:   true,
		substCp:     c.subst.Checkpoint(),
	}
}

// initialTopBranch builds the starting branch for a top-level or
// def-body walk: copy the current var environment, seed the stack from
// the caller-supplied initial inputs (def inputs / nil at top level),
// and run with inferring=false so underflow remains a real error.
func (c *Checker) initialTopBranch(initialStack []TypeId) quoteBranch {
	stack := append([]TypeId(nil), initialStack...)
	return quoteBranch{
		stack:       stack,
		vars:        copyVarMap(c.vars.bound),
		maybeVars:   copyVarMap(c.vars.maybeBound),
		inferInputs: nil,
		diverged:    false,
		inferring:   false,
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
	c.inferring = b.inferring
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
		inferring:   c.inferring,
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

// tryBranchStep runs one driver step against a branch. The step
// (typically checkParseItem or checkOne) does its own special-case
// handling and dispatches via resolveAndApply when needed; multi-
// dispatch sites populate branchSpawn instead of picking. The
// returned slice is the resulting branches: a single capture for a
// deterministic step, or every entry in branchSpawn if the step
// fanned out.
//
// Errors emitted by the step are split into two roles:
//   - When the step produced no branches and no spawn, the errors
//     are the reason the step failed; the caller propagates them up
//     and the branch dies.
//   - When the step both errored and fanned out (e.g. checkIfBlock
//     reports a non-bool condition while still walking its arms),
//     the errors are non-branch-local and get reattached to the
//     parent's c.errors so they surface alongside the surviving
//     branches.
func (c *Checker) tryBranchStep(b quoteBranch, step func()) ([]quoteBranch, []TypeError, bool) {
	c.loadBranch(b)
	savedSpawn := c.branchSpawn
	c.branchSpawn = nil
	savedErrs := c.errors
	c.errors = nil
	step()
	produced := c.errors
	c.errors = savedErrs
	spawned := c.branchSpawn
	c.branchSpawn = savedSpawn
	// Info-severity diagnostics (e.g. `dbg` dumps) are passthroughs:
	// they must not kill the branch, since the step still succeeded
	// type-wise. Split them out and reattach to the parent's errors
	// so they still surface in the final output.
	var fatal []TypeError
	var info []TypeError
	for _, e := range produced {
		if e.Severity == SeverityInfo {
			info = append(info, e)
		} else {
			fatal = append(fatal, e)
		}
	}
	if len(info) > 0 {
		c.errors = append(c.errors, info...)
	}
	if len(spawned) > 0 {
		if len(fatal) > 0 {
			c.errors = append(c.errors, fatal...)
		}
		return spawned, nil, true
	}
	if len(fatal) > 0 {
		return nil, fatal, false
	}
	return []quoteBranch{c.captureBranch()}, nil, true
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
func (c *Checker) collectQuoteSigs(branches []quoteBranch, outerSnap ScopeSnapshot) []QuoteSig {
	sigs := make([]QuoteSig, 0, len(branches))
	for _, b := range branches {
		c.loadBranch(b)

		rawInputs := append([]TypeId(nil), c.inferInputs...)
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
	return c.mergeUnionInputSigs(dedupeQuoteSigs(sigs))
}

// mergeUnionInputSigs coalesces candidate sigs that are identical
// except in a single input position into one sig whose input at that
// position is the union of the differing types. Iterated to a
// fixpoint, this turns a set of same-output overload arms that differ
// only in input flavor (e.g. (str -- path) and (path -- path)) into a
// single union-input sig (str|path -- path) — a plain TKQuote rather
// than a TKOverloadedQuote. That lets the quote be used where only a
// non-overloaded quote is accepted (notably `iff`/`loop` branches),
// which is the whole point: when every arm produces the same output,
// the overload is really just a union on the inputs.
//
// Two sigs merge only when they have the same arity, identical outputs,
// bindings, and effect flags, and their inputs differ in exactly one
// position. Differing in two-or-more positions is left alone: unioning
// each position independently could admit input tuples neither original
// arm accepted (merging (str,str) and (path,path) would wrongly accept
// (str,path)). Full cross-products still collapse, because they reduce
// pairwise one position at a time — e.g. cp's four (str|path, str|path)
// arms fold to a single sig.
//
// Sigs whose shared output is generic don't merge (their per-branch
// fresh vars compare unequal), so they stay overloaded; that's a missed
// collapse, never an unsound one.
//
// The merge is adopted ONLY when it collapses the whole set to a single
// sig — i.e. when the quote genuinely becomes a plain (non-overloaded)
// quote. If any arm remains (e.g. `len`, whose generic [T]/{V} arms
// never fold into the concrete ones), the original sig set is returned
// untouched: a partial merge would reshape a still-overloaded quote for
// no benefit (it can't be an `iff` branch either way) while perturbing
// downstream overload resolution.
func (c *Checker) mergeUnionInputSigs(sigs []QuoteSig) []QuoteSig {
	if len(sigs) <= 1 {
		return sigs
	}
	work := append([]QuoteSig(nil), sigs...)
	for {
		merged := false
	scan:
		for i := 0; i < len(work); i++ {
			for j := i + 1; j < len(work); j++ {
				pos, ok := mergeableInputDiff(c, work[i], work[j])
				if !ok {
					continue
				}
				combined := work[i]
				newInputs := append([]TypeId(nil), work[i].Inputs...)
				newInputs[pos] = c.arena.MakeUnion(
					[]TypeId{work[i].Inputs[pos], work[j].Inputs[pos]}, 0)
				combined.Inputs = newInputs
				combined.Generics = collectFreeTypeVars(
					c.arena, combined.Inputs, combined.Outputs, combined.Bindings)
				work[i] = combined
				work = append(work[:j], work[j+1:]...)
				merged = true
				break scan
			}
		}
		if !merged {
			break
		}
	}
	if len(work) == 1 {
		return work
	}
	return sigs
}

// mergeableInputDiff reports the sole differing input position between a
// and b (and true) when they are identical in arity, outputs, bindings,
// and effect flags and differ in exactly one input slot whose two types
// are both ground (free of type variables). Otherwise it returns
// (0, false).
//
// The ground-input restriction keeps the merge from rewriting overloads
// whose arms differ in a generic input position — e.g. `len`'s
// ([T] -- int) / ({str:V} -- int) arms. Unioning those with concrete
// arms yields a quote whose input is a giant mixed union, which then
// fails to unify as a `filter`/`map` predicate (the overloaded form
// resolves per-arm; the unioned form can't). Same-output overloads with
// purely concrete arms (the str|path file ops) are unaffected and still
// collapse to a single plain quote.
func mergeableInputDiff(c *Checker, a, b QuoteSig) (int, bool) {
	if len(a.Inputs) != len(b.Inputs) || len(a.Outputs) != len(b.Outputs) {
		return 0, false
	}
	if a.Diverges != b.Diverges {
		return 0, false
	}
	for i := range a.Outputs {
		if a.Outputs[i] != b.Outputs[i] {
			return 0, false
		}
	}
	if !bindingsEqual(a.Bindings, b.Bindings) {
		return 0, false
	}
	diff := -1
	for i := range a.Inputs {
		if a.Inputs[i] != b.Inputs[i] {
			if diff != -1 {
				return 0, false // more than one differing position
			}
			diff = i
		}
	}
	if diff == -1 {
		return 0, false // identical — dedupeQuoteSigs handles these
	}
	if !c.isGroundType(a.Inputs[diff]) || !c.isGroundType(b.Inputs[diff]) {
		return 0, false
	}
	return diff, true
}

// isGroundType reports whether t contains no free type variable. t must
// already be resolved (collectQuoteSigs Apply's every sig input through
// its own branch's substitution before this runs); re-applying here
// through the currently-loaded substitution would misjudge a var that
// is free in its own branch but bound in some sibling branch.
func (c *Checker) isGroundType(t TypeId) bool {
	seen := make(map[TypeVarId]struct{})
	var ordered []TypeVarId
	walkFreeTypeVars(c.arena, t, seen, &ordered)
	return len(ordered) == 0
}

func bindingsEqual(a, b map[NameId]TypeId) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
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
// TypeVarId to `ordered` (deduped via `seen`). Lookups don't go through
// the substitution because callers pass already-Apply'd TypeIds, so any
// remaining TKVar is genuinely free.
func walkFreeTypeVars(arena *TypeArena, t TypeId, seen map[TypeVarId]struct{}, ordered *[]TypeVarId) {
	arena.walkTypeVars(t, func(id TypeVarId) bool {
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			*ordered = append(*ordered, id)
		}
		return false // never short-circuit; collect them all
	})
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
	if a.Diverges != b.Diverges {
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
