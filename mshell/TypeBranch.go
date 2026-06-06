package main

import (
	"fmt"
	"strings"
)

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
	stack      []TypeId
	vars       map[NameId]TypeId
	maybeVars  map[NameId]TypeId
	diverged   bool
}

// Snapshot returns a copy of the checker's current stack and var env.
// The returned snapshot is detached from the live state — mutating the
// checker after calling Snapshot does not change it.
func (c *Checker) Snapshot() ScopeSnapshot {
	return ScopeSnapshot{
		stack:     append([]TypeId(nil), c.stack.items...),
		vars:      copyVarMap(c.vars.bound),
		maybeVars: copyVarMap(c.vars.maybeBound),
		diverged:  c.diverged,
	}
}

// Fork resets the checker's stack and var env to a copy of snap. The
// snapshot itself is untouched, so it can be reused for sibling arms.
func (c *Checker) Fork(snap ScopeSnapshot) {
	c.stack.items = append(c.stack.items[:0], snap.stack...)
	c.vars.bound = copyVarMap(snap.vars)
	c.vars.maybeBound = copyVarMap(snap.maybeVars)
	c.diverged = snap.diverged
}

// BranchArm is the result of running the checker over a single arm of
// a branching construct. The caller produces one BranchArm per arm by
// snapshotting before, forking, running the arm body, and calling
// CaptureArm at the tail. Diverged is true when the arm cannot fall
// through (exit, infinite loop, propagated fail).
//
// Body is the parse-item slice the arm was built from. It's purely a
// breadcrumb for diagnostic rendering (e.g. the per-branch summary in
// the stack-size mismatch message) — the type effects come from Stack
// / Vars / MaybeVars. Callers set it after CaptureArm if they want
// richer error text.
type BranchArm struct {
	Stack     []TypeId
	Vars      map[NameId]TypeId
	MaybeVars map[NameId]TypeId
	Diverged  bool
	Body      []MShellParseItem
}

// CaptureArm reads the checker's current stack and vars into a
// BranchArm. The diverged flag is the caller's call — the checker
// has no way to detect every divergent path on its own (e.g. a
// definition that always exits cannot be inferred at this level).
func (c *Checker) CaptureArm(diverged bool) BranchArm {
	return BranchArm{
		Stack:     append([]TypeId(nil), c.stack.items...),
		Vars:      copyVarMap(c.vars.bound),
		MaybeVars: copyVarMap(c.vars.maybeBound),
		Diverged:  diverged,
	}
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

	// The post-branch is reachable iff at least one arm fell through
	// without diverging. Set c.diverged accordingly so the surrounding
	// scope's downstream checking sees the correct reachability —
	// otherwise a diverged arm (e.g. `none: ... exit`) would leak its
	// `diverged = true` past the join, silently suppressing later
	// def-body output checks.
	c.diverged = len(live) == 0

	if len(live) == 0 {
		// Whole branch is unreachable. Clear the stack/vars; downstream
		// code is dead. (No error here — Phase 7-or-later may flag it.)
		c.stack.items = c.stack.items[:0]
		c.vars.bound = make(map[NameId]TypeId)
		c.vars.maybeBound = make(map[NameId]TypeId)
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
			Hint: c.formatBranchSizeMismatch(arms, live),
		})
		// Recovery: take the first non-diverged arm's tail as the merged
		// state so downstream errors don't cascade off a missing stack.
		c.stack.items = append(c.stack.items[:0], first.Stack...)
		c.vars.bound = copyVarMap(first.Vars)
		c.vars.maybeBound = copyVarMap(first.MaybeVars)
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

	// Per-var reconciliation. A name is "definitely bound" after the
	// branch iff every live arm has it in `Vars` (bound). Otherwise
	// — bound in some live arms but not others, or sitting in
	// MaybeVars on any arm — it lifts to maybeBound. Types are
	// unioned across whichever arms carried the name in either map,
	// so `@name` at a downstream maybeBound site still has a useful
	// type for recovery.
	allNames := make(map[NameId]struct{})
	for _, i := range live {
		for n := range arms[i].Vars {
			allNames[n] = struct{}{}
		}
		for n := range arms[i].MaybeVars {
			allNames[n] = struct{}{}
		}
	}
	mergedBound := make(map[NameId]TypeId)
	mergedMaybe := make(map[NameId]TypeId)
	for name := range allNames {
		boundEverywhere := true
		scratch = scratch[:0]
		for _, i := range live {
			if t, ok := arms[i].Vars[name]; ok {
				scratch = append(scratch, t)
				continue
			}
			boundEverywhere = false
			if t, ok := arms[i].MaybeVars[name]; ok {
				scratch = append(scratch, t)
			}
		}
		merged := c.arena.MakeUnion(scratch, 0)
		if boundEverywhere {
			mergedBound[name] = merged
		} else {
			mergedMaybe[name] = merged
		}
	}
	c.vars.bound = mergedBound
	c.vars.maybeBound = mergedMaybe
}

// formatBranchSizeMismatch builds the Hint string for a stack-size
// disagreement across non-diverged arms. For up to 10 live arms each
// gets a line of the form
//
//   Branch N: <first10>...<last10>  (T1 -- T2 T3)
//
// where the body summary is derived from the arm's parse-item
// breadcrumbs (see BranchArm.Body) and the stack signature is the
// arm's tail stack rendered top-first. Beyond 10 arms the message
// falls back to a single line, since the per-branch detail would
// dominate the diagnostic.
func (c *Checker) formatBranchSizeMismatch(arms []BranchArm, live []int) string {
	const lead = "all branches must produce the same number of stack items"
	if len(live) > 10 {
		return lead
	}
	var sb strings.Builder
	sb.WriteString(lead)
	for n, i := range live {
		fmt.Fprintf(&sb, "\n  Branch %d: %s  %s",
			n+1,
			truncateBranchSnippet(formatItemsSnippet(arms[i].Body)),
			c.formatArmStack(arms[i].Stack),
		)
	}
	return sb.String()
}

// formatArmStack renders an arm's tail stack as a `(top -- ... -- bottom)`
// readout. Items are rendered top-first (last-pushed first), matching
// how the rest of the type-error formatter speaks about the stack.
func (c *Checker) formatArmStack(stack []TypeId) string {
	if len(stack) == 0 {
		return "( -- )"
	}
	var sb strings.Builder
	sb.WriteString("( -- ")
	for i := len(stack) - 1; i >= 0; i-- {
		if i != len(stack)-1 {
			sb.WriteByte(' ')
		}
		sb.WriteString(FormatType(c.arena, c.names, stack[i]))
	}
	sb.WriteString(" )")
	return sb.String()
}

// formatItemsSnippet collapses a slice of parse items to a single
// whitespace-separated string for diagnostic snippets. Composite
// items (lists, dicts, quotes, etc.) collapse to
// `<openLexeme>...<closeLexeme>` rather than recursing arbitrarily
// deep — the caller takes a first/last-N substring of the result so
// over-precision would be wasted work.
func formatItemsSnippet(items []MShellParseItem) string {
	if len(items) == 0 {
		return "<empty>"
	}
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteByte(' ')
		}
		switch v := it.(type) {
		case Token:
			sb.WriteString(v.Lexeme)
		default:
			start := v.GetStartToken().Lexeme
			end := v.GetEndToken().Lexeme
			sb.WriteString(start)
			if end != "" && end != start {
				sb.WriteString("...")
				sb.WriteString(end)
			}
		}
	}
	return sb.String()
}

// truncateBranchSnippet shortens a body snippet to first 10 / last 10
// chars joined by an ellipsis. Strings short enough to fit are
// returned unchanged. Whitespace runs are collapsed to single spaces
// first so multi-line bodies stay one row in the diagnostic.
func truncateBranchSnippet(s string) string {
	// Collapse internal whitespace runs.
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	collapsed := strings.TrimSpace(b.String())
	if len(collapsed) <= 23 {
		return collapsed
	}
	return collapsed[:10] + "..." + collapsed[len(collapsed)-10:]
}

// MatchArmKind tags the shape of a match arm for exhaustiveness analysis.
// `MatchArmType` carries the pattern's type in TypeArm.
type MatchArmKind uint8

const (
	MatchArmWildcard MatchArmKind = iota
	MatchArmJust
	MatchArmNone
	MatchArmType
	MatchArmTrue  // bool literal `true` pattern
	MatchArmFalse // bool literal `false` pattern
	// MatchArmEmptyList: `[]` pattern. Covers empty lists.
	MatchArmEmptyList
	// MatchArmListWithRest: `[a ...rest]`, `[a b ...rest]`, or
	// `[...rest]` — any list pattern with a `...name` element.
	// Covers all lists whose length is at least the number of
	// non-rest pattern elements.
	MatchArmListWithRest
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
		// A type-pattern arm whose type equals the matched type covers
		// every inhabitant of that type — it's a total arm. E.g. a
		// `str` arm in a match on a `str` subject is exhaustive on its
		// own, just like a wildcard.
		if arm.Kind == MatchArmType && c.subst.Apply(c.arena, arm.TypeArm) == matched {
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

	case TKPrim:
		// Booleans have a finite inhabitant set; `true`+`false` arms
		// cover them without a wildcard. Other primitives (int, str,
		// ...) have unbounded inhabitants and need a wildcard.
		if matched == TidBool {
			hasTrue, hasFalse := false, false
			for _, arm := range arms {
				switch arm.Kind {
				case MatchArmTrue:
					hasTrue = true
				case MatchArmFalse:
					hasFalse = true
				}
			}
			if hasTrue && hasFalse {
				return true
			}
		}

	case TKList:
		// A list's inhabitants split by length: zero (empty) vs
		// one-or-more. `[]` covers empty; any list pattern that ends
		// with `...rest` covers the rest. The pair is exhaustive
		// without a wildcard. More precise length-based coverage
		// (e.g. distinguishing `[a]` from `[a b ...rest]`) is a
		// future refinement; the current rule handles the common
		// "empty vs non-empty" idiom.
		hasEmpty, hasRest := false, false
		for _, arm := range arms {
			switch arm.Kind {
			case MatchArmEmptyList:
				hasEmpty = true
			case MatchArmListWithRest:
				hasRest = true
			}
		}
		if hasEmpty && hasRest {
			return true
		}
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
