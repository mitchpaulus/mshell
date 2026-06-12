package main

import "testing"

// Phase-6b tests: branch reconciliation and exhaustiveness.

func freshChecker() *Checker {
	return NewChecker(NewTypeArena(), NewNameTable())
}

// runArm forks the checker to entry, runs body, and captures the arm.
// Body is a closure so tests can express small token-driven sequences.
func runArm(c *Checker, entry quoteBranch, diverged bool, body func()) quoteBranch {
	c.loadBranch(entry)
	body()
	b := c.captureBranch()
	b.diverged = diverged
	return b
}

func TestSnapshotIsDetached(t *testing.T) {
	c := freshChecker()
	c.stack.Push(TidInt)
	snap := c.Snapshot()
	c.stack.Push(TidStr) // mutate after snapshot
	if len(snap.stack) != 1 || snap.stack[0] != TidInt {
		t.Fatalf("snapshot should have only TidInt; got %v", snap.stack)
	}
}

func TestForkRestoresStateExactly(t *testing.T) {
	c := freshChecker()
	c.stack.Push(TidInt)
	c.vars.bound[c.names.Intern("x")] = TidStr
	snap := c.Snapshot()

	c.stack.Push(TidBool)
	c.vars.bound[c.names.Intern("y")] = TidInt
	c.Fork(snap)

	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("stack not restored: %v", c.stack.Snapshot())
	}
	if len(c.vars.bound) != 1 {
		t.Fatalf("vars not restored: %v", c.vars.bound)
	}
	if got := c.vars.bound[c.names.Intern("x")]; got != TidStr {
		t.Fatalf("x should be str, got %v", got)
	}
}

func TestReconcileSameTypes(t *testing.T) {
	c := freshChecker()
	entry := c.captureBranch()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("expected merged stack to be [int], got %v", c.stack.Snapshot())
	}
}

func TestReconcileDifferentTypesUnion(t *testing.T) {
	c := freshChecker()
	entry := c.captureBranch()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, false, func() { c.stack.Push(TidStr) })
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, 0)
	if c.stack.Len() != 1 || c.stack.Top() != want {
		t.Fatalf("expected merged stack to be int|str, got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestReconcileStackSizeMismatch(t *testing.T) {
	c := freshChecker()
	entry := c.captureBranch()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, false, func() {
		c.stack.Push(TidInt)
		c.stack.Push(TidStr)
	})
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrBranchStackSize {
		t.Fatalf("expected stack-size error, got %+v", errs)
	}
}

func TestReconcileVarSetLiftsToMaybeBound(t *testing.T) {
	// Names bound in only some arms lift to maybeBound after reconciliation
	// rather than triggering a hard error. Reading such a name downstream
	// is what produces a diagnostic (TErrMaybeUnset), not the reconcile itself.
	c := freshChecker()
	entry := c.captureBranch()
	x := c.names.Intern("x")
	y := c.names.Intern("y")
	a1 := runArm(c, entry, false, func() { c.vars.bound[x] = TidInt })
	a2 := runArm(c, entry, false, func() { c.vars.bound[y] = TidStr })
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if _, ok := c.vars.bound[x]; ok {
		t.Fatalf("expected x to lift out of bound, still present")
	}
	if _, ok := c.vars.bound[y]; ok {
		t.Fatalf("expected y to lift out of bound, still present")
	}
	if got, ok := c.vars.maybeBound[x]; !ok || got != TidInt {
		t.Fatalf("expected maybeBound[x] = int, got ok=%v val=%v", ok, got)
	}
	if got, ok := c.vars.maybeBound[y]; !ok || got != TidStr {
		t.Fatalf("expected maybeBound[y] = str, got ok=%v val=%v", ok, got)
	}
}

func TestReconcileVarTypeUnion(t *testing.T) {
	c := freshChecker()
	entry := c.captureBranch()
	x := c.names.Intern("x")
	a1 := runArm(c, entry, false, func() { c.vars.bound[x] = TidInt })
	a2 := runArm(c, entry, false, func() { c.vars.bound[x] = TidStr })
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, 0)
	if got := c.vars.bound[x]; got != want {
		t.Fatalf("x should merge to int|str, got %s",
			FormatType(c.arena, c.names, got))
	}
}

func TestReconcilePreBoundStaysBound(t *testing.T) {
	// A name bound before the branch stays in `bound` even if no arm
	// re-binds it: the entry binding survives every arm via Fork.
	c := freshChecker()
	x := c.names.Intern("x")
	c.vars.bound[x] = TidInt
	entry := c.captureBranch()
	a1 := runArm(c, entry, false, func() {})
	a2 := runArm(c, entry, false, func() { c.vars.bound[x] = TidStr })
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if _, maybe := c.vars.maybeBound[x]; maybe {
		t.Fatalf("pre-bound x should not lift to maybeBound")
	}
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, 0)
	if got := c.vars.bound[x]; got != want {
		t.Fatalf("x should merge to int|str, got %s",
			FormatType(c.arena, c.names, got))
	}
}

func TestReconcileMaybeFromOneArmLeaks(t *testing.T) {
	// A name bound on only one arm of a no-else if (modeled as one
	// arm binding + one no-op arm) lifts to maybeBound carrying the
	// type from the binding arm.
	c := freshChecker()
	entry := c.captureBranch()
	x := c.names.Intern("x")
	a1 := runArm(c, entry, false, func() { c.vars.bound[x] = TidInt })
	a2 := runArm(c, entry, false, func() {}) // implicit no-else arm
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if _, ok := c.vars.bound[x]; ok {
		t.Fatalf("x should not be in bound")
	}
	if got, ok := c.vars.maybeBound[x]; !ok || got != TidInt {
		t.Fatalf("expected maybeBound[x] = int, got ok=%v val=%v", ok, got)
	}
}

func TestReconcileDivergedArmDropped(t *testing.T) {
	// One arm exits, the other produces an int. Result is just int.
	c := freshChecker()
	entry := c.captureBranch()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, true, func() {
		// Diverged arm — its stack is irrelevant. Push something
		// distracting to verify the reconciler ignores it.
		c.stack.Push(TidStr)
		c.stack.Push(TidStr)
	})
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("expected merged stack to be [int] (diverged arm dropped); got %v",
			c.stack.Snapshot())
	}
}

func TestReconcileAllDivergedMarksDiverged(t *testing.T) {
	c := freshChecker()
	c.stack.Push(TidInt)
	entry := c.captureBranch()
	a1 := runArm(c, entry, true, func() {})
	a2 := runArm(c, entry, true, func() {})
	c.reconcileArmBranches([]quoteBranch{a1, a2}, []string{"arm 1", "arm 2"}, entry, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	// Every arm diverged: the post-branch is dead code, so the checker
	// itself must be marked diverged regardless of residual state.
	if !c.diverged {
		t.Fatalf("expected checker to be diverged after all-diverged branch")
	}
}

func TestExhaustiveMaybeBoth(t *testing.T) {
	c := freshChecker()
	matched := c.arena.MakeMaybe(TidInt)
	ok := c.CheckMatchExhaustive(matched,
		[]MatchArmTag{{Kind: MatchArmJust}, {Kind: MatchArmNone}},
		mkTok(MATCH, "match"))
	if !ok {
		t.Fatalf("just+none should be exhaustive; errors: %+v", c.Errors())
	}
}

func TestExhaustiveMaybeMissingNone(t *testing.T) {
	c := freshChecker()
	matched := c.arena.MakeMaybe(TidInt)
	ok := c.CheckMatchExhaustive(matched,
		[]MatchArmTag{{Kind: MatchArmJust}},
		mkTok(MATCH, "match"))
	if ok {
		t.Fatalf("just only should NOT be exhaustive")
	}
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrNonExhaustiveMatch {
		t.Fatalf("expected non-exhaustive error, got %+v", errs)
	}
}

func TestExhaustiveMaybeWildcard(t *testing.T) {
	c := freshChecker()
	matched := c.arena.MakeMaybe(TidInt)
	ok := c.CheckMatchExhaustive(matched,
		[]MatchArmTag{{Kind: MatchArmJust}, {Kind: MatchArmWildcard}},
		mkTok(MATCH, "match"))
	if !ok {
		t.Fatalf("just+wildcard should be exhaustive; errors: %+v", c.Errors())
	}
}

func TestExhaustiveUnion(t *testing.T) {
	c := freshChecker()
	matched := c.arena.MakeUnion([]TypeId{TidInt, TidStr, TidBool}, 0)
	ok := c.CheckMatchExhaustive(matched,
		[]MatchArmTag{
			{Kind: MatchArmType, TypeArm: TidInt},
			{Kind: MatchArmType, TypeArm: TidStr},
			{Kind: MatchArmType, TypeArm: TidBool},
		},
		mkTok(MATCH, "match"))
	if !ok {
		t.Fatalf("all-arms covered should be exhaustive; errors: %+v", c.Errors())
	}
}

func TestExhaustiveUnionMissingArm(t *testing.T) {
	c := freshChecker()
	matched := c.arena.MakeUnion([]TypeId{TidInt, TidStr, TidBool}, 0)
	ok := c.CheckMatchExhaustive(matched,
		[]MatchArmTag{
			{Kind: MatchArmType, TypeArm: TidInt},
			{Kind: MatchArmType, TypeArm: TidStr},
		},
		mkTok(MATCH, "match"))
	if ok {
		t.Fatalf("missing bool arm should not be exhaustive")
	}
	if len(c.Errors()) != 1 || c.Errors()[0].Kind != TErrNonExhaustiveMatch {
		t.Fatalf("expected non-exhaustive error, got %+v", c.Errors())
	}
}

func TestExhaustiveUnionWildcard(t *testing.T) {
	c := freshChecker()
	matched := c.arena.MakeUnion([]TypeId{TidInt, TidStr, TidBool}, 0)
	ok := c.CheckMatchExhaustive(matched,
		[]MatchArmTag{
			{Kind: MatchArmType, TypeArm: TidInt},
			{Kind: MatchArmWildcard},
		},
		mkTok(MATCH, "match"))
	if !ok {
		t.Fatalf("wildcard should make match exhaustive; errors: %+v", c.Errors())
	}
}

func TestExhaustivePrimTotalArm(t *testing.T) {
	// A match on a `str` subject with a `str` type-pattern arm is total
	// on its own: the arm covers every inhabitant of str, so no wildcard
	// is required even though str has unbounded inhabitants.
	c := freshChecker()
	ok := c.CheckMatchExhaustive(TidStr,
		[]MatchArmTag{
			{Kind: MatchArmType, TypeArm: TidFloat},
			{Kind: MatchArmType, TypeArm: TidStr},
		},
		mkTok(MATCH, "match"))
	if !ok {
		t.Fatalf("str arm on str subject should be exhaustive; errors: %+v", c.Errors())
	}
}

func TestExhaustivePrimNoTotalArm(t *testing.T) {
	// A match on a `str` subject without a str arm (or wildcard) is not
	// exhaustive: a float arm covers none of str's inhabitants.
	c := freshChecker()
	ok := c.CheckMatchExhaustive(TidStr,
		[]MatchArmTag{{Kind: MatchArmType, TypeArm: TidFloat}},
		mkTok(MATCH, "match"))
	if ok {
		t.Fatalf("float-only arm on str subject should not be exhaustive")
	}
	if len(c.Errors()) != 1 || c.Errors()[0].Kind != TErrNonExhaustiveMatch {
		t.Fatalf("expected non-exhaustive error, got %+v", c.Errors())
	}
}

func TestReconcileWithMaybePattern(t *testing.T) {
	// Simulate a `match` over Maybe[int]: the just-arm extracts the int,
	// the none-arm produces a default. Reconciliation should give int.
	c := freshChecker()
	entry := c.captureBranch()
	justArm := runArm(c, entry, false, func() {
		// Imagine: the bound int from `just @v` ends up on the stack.
		c.stack.Push(TidInt)
	})
	noneArm := runArm(c, entry, false, func() {
		c.stack.Push(TidInt)
	})
	c.reconcileArmBranches([]quoteBranch{justArm, noneArm}, []string{"arm 1", "arm 2"}, entry, mkTok(MATCH, "match"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("expected int after match; got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}

func TestJoinArmBranchesPadsSubstitution(t *testing.T) {
	// Arm 2 allocates a fresh variable under its own (longer)
	// substitution checkpoint and leaves it free in its stack slot.
	// After the join installs on arm 1's shorter checkpoint, a newly
	// issued FreshVar must not re-use that variable's id — reuse would
	// silently alias two unrelated variables.
	c := freshChecker()
	entry := c.captureBranch()

	c.loadBranch(entry)
	c.stack.Push(TidInt)
	b1 := c.captureBranch()

	c.loadBranch(entry)
	v := c.subst.FreshVar(c.arena)
	c.stack.Push(v)
	b2 := c.captureBranch()

	c.joinArmBranches([]quoteBranch{b1, b2})

	fresh := c.subst.FreshVar(c.arena)
	if fresh == v {
		t.Fatalf("FreshVar re-issued a variable still referenced by the joined stack")
	}
}
