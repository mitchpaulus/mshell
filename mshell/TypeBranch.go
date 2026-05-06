package main

// Phase 6b: branch reconciliation and variable-environment scoping.
//
// All branching constructs (`if`/`else`, `match`, the eventual `try:`)
// share one shape:
//
//   1. Snapshot the entry state (stack + vars) before evaluating arms.
//   2. For each arm, fork: reset stack/vars to the entry copy, run the
//      arm's body through the checker, then capture its tail state.
//   3. After all arms, reconcile: stacks must agree on size, var sets
//      must agree on names, and per-slot / per-var types are unioned.
//      Diverged arms (exit, infinite loop, propagated fail in Phase 2)
//      contribute nothing — they are skipped in size/var checks and
//      drop out of the unions.
//
// The substitution is intentionally NOT rolled back between arms. A
// type-variable binding made inside an arm sticks for the rest of the
// session. This is a deliberate simplification: alternative arms in
// the source program are mutually exclusive at runtime, but the
// substitution is global to the type-check pass; collisions across
// sibling arms surface as type errors and signal real ambiguity in
// the program. If this proves too coarse in practice, snapshotting
// the substitution becomes a localized fix.

// ScopeSnapshot captures enough state to fork the checker into an arm
// and to restore its entry state between arms. It does not capture
// the substitution — that is intentionally global, see file header.
type ScopeSnapshot struct {
	stack []TypeId
	vars  map[NameId]TypeId
}

// Snapshot returns a copy of the checker's current stack and var env.
// The returned snapshot is detached from the live state — mutating the
// checker after calling Snapshot does not change it.
func (c *Checker) Snapshot() ScopeSnapshot {
	stackCopy := make([]TypeId, len(c.stack.items))
	copy(stackCopy, c.stack.items)
	varsCopy := make(map[NameId]TypeId, len(c.vars.bound))
	for k, v := range c.vars.bound {
		varsCopy[k] = v
	}
	return ScopeSnapshot{stack: stackCopy, vars: varsCopy}
}

// Fork resets the checker's stack and var env to a copy of snap. The
// snapshot itself is untouched, so it can be reused for sibling arms.
func (c *Checker) Fork(snap ScopeSnapshot) {
	c.stack.items = c.stack.items[:0]
	c.stack.items = append(c.stack.items, snap.stack...)
	c.vars.bound = make(map[NameId]TypeId, len(snap.vars))
	for k, v := range snap.vars {
		c.vars.bound[k] = v
	}
}

// BranchArm is the result of running the checker over a single arm of
// a branching construct. The caller produces one BranchArm per arm by
// snapshotting before, forking, running the arm body, and calling
// CaptureArm at the tail. Diverged is true when the arm cannot fall
// through (exit, infinite loop, propagated fail).
type BranchArm struct {
	Stack    []TypeId
	Vars     map[NameId]TypeId
	Diverged bool
}

// CaptureArm reads the checker's current stack and vars into a
// BranchArm. The diverged flag is the caller's call — the checker
// has no way to detect every divergent path on its own (e.g. a
// definition that always exits cannot be inferred at this level).
func (c *Checker) CaptureArm(diverged bool) BranchArm {
	stackCopy := make([]TypeId, len(c.stack.items))
	copy(stackCopy, c.stack.items)
	varsCopy := make(map[NameId]TypeId, len(c.vars.bound))
	for k, v := range c.vars.bound {
		varsCopy[k] = v
	}
	return BranchArm{Stack: stackCopy, Vars: varsCopy, Diverged: diverged}
}

// ReconcileArms merges per-arm tail states into a single post-branch
// state, replacing the checker's live stack and vars. It records
// errors for stack-size and var-set mismatches across non-diverged
// arms. If every arm diverged, the checker's state is left empty —
// the post-branch is dead code, which a later phase may diagnose.
//
// Per-slot types are unioned across non-diverged arms via
// arena.MakeUnion (which handles flatten/dedupe). TidBottom would not
// normally appear in a non-diverged arm; if it does, MakeUnion
// folds it in harmlessly because it's a regular TypeId at this
// layer (the divergence semantics are encoded in the Diverged flag).
func (c *Checker) ReconcileArms(arms []BranchArm, callSite Token) {
	live := make([]int, 0, len(arms))
	for i, arm := range arms {
		if !arm.Diverged {
			live = append(live, i)
		}
	}

	if len(live) == 0 {
		// Whole branch is unreachable. Clear the stack/vars; downstream
		// code is dead. (No error here — Phase 7-or-later may flag it.)
		c.stack.items = c.stack.items[:0]
		c.vars.bound = make(map[NameId]TypeId)
		return
	}

	// Stack-size agreement across non-diverged arms.
	first := arms[live[0]]
	wantSize := len(first.Stack)
	sizesAgree := true
	for _, i := range live[1:] {
		if len(arms[i].Stack) != wantSize {
			sizesAgree = false
			break
		}
	}
	if !sizesAgree {
		c.errors = append(c.errors, TypeError{
			Kind: TErrBranchStackSize,
			Pos:  callSite,
			Hint: "all branches must produce the same number of stack items",
		})
		// Recovery: take the first non-diverged arm's tail as the merged
		// state so downstream errors don't cascade off a missing stack.
		c.stack.items = append(c.stack.items[:0], first.Stack...)
		c.vars.bound = make(map[NameId]TypeId, len(first.Vars))
		for k, v := range first.Vars {
			c.vars.bound[k] = v
		}
		return
	}

	// Var-set agreement: every non-diverged arm must bind the same names.
	varsAgree := true
	for _, i := range live[1:] {
		if !sameVarSet(first.Vars, arms[i].Vars) {
			varsAgree = false
			break
		}
	}
	if !varsAgree {
		c.errors = append(c.errors, TypeError{
			Kind: TErrBranchVarSet,
			Pos:  callSite,
			Hint: "all branches must bind the same set of variable names",
		})
		// Recovery: same as above.
		c.stack.items = append(c.stack.items[:0], first.Stack...)
		c.vars.bound = make(map[NameId]TypeId, len(first.Vars))
		for k, v := range first.Vars {
			c.vars.bound[k] = v
		}
		return
	}

	// Per-slot type union.
	merged := make([]TypeId, wantSize)
	scratch := make([]TypeId, 0, len(live))
	for slot := 0; slot < wantSize; slot++ {
		scratch = scratch[:0]
		for _, i := range live {
			scratch = append(scratch, arms[i].Stack[slot])
		}
		merged[slot] = c.arena.MakeUnion(scratch, 0)
	}
	c.stack.items = append(c.stack.items[:0], merged...)

	// Per-var type union.
	mergedVars := make(map[NameId]TypeId, len(first.Vars))
	for name := range first.Vars {
		scratch = scratch[:0]
		for _, i := range live {
			scratch = append(scratch, arms[i].Vars[name])
		}
		mergedVars[name] = c.arena.MakeUnion(scratch, 0)
	}
	c.vars.bound = mergedVars
}

// sameVarSet reports whether two var maps have identical key sets.
// Type comparison is left for the per-name union step.
func sameVarSet(a, b map[NameId]TypeId) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// MatchArmKind tags the shape of a match arm for exhaustiveness analysis.
// `MatchArmType` carries the pattern's type in TypeArm.
type MatchArmKind uint8

const (
	MatchArmWildcard MatchArmKind = iota
	MatchArmJust
	MatchArmNone
	MatchArmType
)

// MatchArmTag describes one pattern-side of a match arm. The body's
// type effects flow through ReconcileArms; this struct only feeds
// the exhaustiveness check.
type MatchArmTag struct {
	Kind    MatchArmKind
	TypeArm TypeId // valid when Kind == MatchArmType
}

// CheckMatchExhaustive verifies that arms cover every inhabitant of
// the matched type. Returns true if exhaustive; otherwise records a
// TErrNonExhaustiveMatch and returns false. A wildcard arm satisfies
// any matched type. For Maybe[T], both Just and None must appear (or
// a wildcard). For a union, every arm of the union's flattened arm
// list must be covered (by an exact-type pattern or wildcard).
func (c *Checker) CheckMatchExhaustive(matched TypeId, arms []MatchArmTag, callSite Token) bool {
	matched = c.subst.Apply(c.arena, matched)
	for _, arm := range arms {
		if arm.Kind == MatchArmWildcard {
			return true
		}
	}

	n := c.arena.Node(matched)
	switch n.Kind {
	case TKMaybe:
		hasJust, hasNone := false, false
		for _, arm := range arms {
			switch arm.Kind {
			case MatchArmJust:
				hasJust = true
			case MatchArmNone:
				hasNone = true
			}
		}
		if hasJust && hasNone {
			return true
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrNonExhaustiveMatch,
			Pos:  callSite,
			Hint: "Maybe[T] requires both 'just' and 'none' arms (or a wildcard)",
		})
		return false

	case TKUnion:
		members := c.arena.unionMembers[n.Extra]
		covered := make(map[TypeId]bool, len(members))
		for _, arm := range arms {
			if arm.Kind == MatchArmType {
				covered[c.subst.Apply(c.arena, arm.TypeArm)] = true
			}
		}
		missing := false
		for _, m := range members {
			if !covered[m] {
				missing = true
				break
			}
		}
		if !missing {
			return true
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrNonExhaustiveMatch,
			Pos:  callSite,
			Hint: "union match must cover every arm or include a wildcard",
		})
		return false
	}

	// Other kinds — no exhaustiveness rule encoded yet (shapes, brands,
	// primitives). Treat as non-exhaustive without an explicit
	// wildcard arm; the parser-driven path can flag this once it
	// knows the arm shapes.
	c.errors = append(c.errors, TypeError{
		Kind: TErrNonExhaustiveMatch,
		Pos:  callSite,
		Hint: "match on this type requires a wildcard arm",
	})
	return false
}
