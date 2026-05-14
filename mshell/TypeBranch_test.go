package main

import "testing"

// Phase-6b tests: branch reconciliation and exhaustiveness.

func freshChecker() *Checker {
	return NewChecker(NewTypeArena(), NewNameTable())
}

// runArm forks the checker to entry, runs body, and captures the arm.
// Body is a closure so tests can express small token-driven sequences.
func runArm(c *Checker, entry ScopeSnapshot, diverged bool, body func()) BranchArm {
	c.Fork(entry)
	body()
	return c.CaptureArm(diverged)
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
	entry := c.Snapshot()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("expected merged stack to be [int], got %v", c.stack.Snapshot())
	}
}

func TestReconcileDifferentTypesUnion(t *testing.T) {
	c := freshChecker()
	entry := c.Snapshot()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, false, func() { c.stack.Push(TidStr) })
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
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
	entry := c.Snapshot()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, false, func() {
		c.stack.Push(TidInt)
		c.stack.Push(TidStr)
	})
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrBranchStackSize {
		t.Fatalf("expected stack-size error, got %+v", errs)
	}
}

func TestReconcileVarSetMismatch(t *testing.T) {
	c := freshChecker()
	entry := c.Snapshot()
	x := c.names.Intern("x")
	y := c.names.Intern("y")
	a1 := runArm(c, entry, false, func() { c.vars.bound[x] = TidInt })
	a2 := runArm(c, entry, false, func() { c.vars.bound[y] = TidInt })
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrBranchVarSet {
		t.Fatalf("expected var-set error, got %+v", errs)
	}
}

func TestReconcileVarTypeUnion(t *testing.T) {
	c := freshChecker()
	entry := c.Snapshot()
	x := c.names.Intern("x")
	a1 := runArm(c, entry, false, func() { c.vars.bound[x] = TidInt })
	a2 := runArm(c, entry, false, func() { c.vars.bound[x] = TidStr })
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, 0)
	if got := c.vars.bound[x]; got != want {
		t.Fatalf("x should merge to int|str, got %s",
			FormatType(c.arena, c.names, got))
	}
}

func TestReconcileDivergedArmDropped(t *testing.T) {
	// One arm exits, the other produces an int. Result is just int.
	c := freshChecker()
	entry := c.Snapshot()
	a1 := runArm(c, entry, false, func() { c.stack.Push(TidInt) })
	a2 := runArm(c, entry, true, func() {
		// Diverged arm — its stack is irrelevant. Push something
		// distracting to verify the reconciler ignores it.
		c.stack.Push(TidStr)
		c.stack.Push(TidStr)
	})
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != TidInt {
		t.Fatalf("expected merged stack to be [int] (diverged arm dropped); got %v",
			c.stack.Snapshot())
	}
}

func TestReconcileAllDivergedClearsState(t *testing.T) {
	c := freshChecker()
	c.stack.Push(TidInt) // pre-branch state should be cleared too
	entry := c.Snapshot()
	a1 := runArm(c, entry, true, func() {})
	a2 := runArm(c, entry, true, func() {})
	c.ReconcileArms([]BranchArm{a1, a2}, mkTok(IF, "if"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Len() != 0 {
		t.Fatalf("expected empty stack after all-diverged branch, got %v",
			c.stack.Snapshot())
	}
	if len(c.vars.bound) != 0 {
		t.Fatalf("expected empty vars after all-diverged branch")
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

func TestReconcileWithMaybePattern(t *testing.T) {
	// Simulate a `match` over Maybe[int]: the just-arm extracts the int,
	// the none-arm produces a default. Reconciliation should give int.
	c := freshChecker()
	entry := c.Snapshot()
	justArm := runArm(c, entry, false, func() {
		// Imagine: the bound int from `just @v` ends up on the stack.
		c.stack.Push(TidInt)
	})
	noneArm := runArm(c, entry, false, func() {
		c.stack.Push(TidInt)
	})
	c.ReconcileArms([]BranchArm{justArm, noneArm}, mkTok(MATCH, "match"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("expected int after match; got %s",
			FormatType(c.arena, c.names, c.stack.Top()))
	}
}
