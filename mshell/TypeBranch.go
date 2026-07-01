package main

import "strings"

// Scope save/restore and match exhaustiveness.
//
// Branch reconciliation itself lives on the quoteBranch machinery
// (TypeQuote.go / TypeCheckProgram.go): arms fork via captureBranch /
// loadBranch, which also checkpoint the substitution per arm, and join
// through reconcileArmBranches. This file keeps the lighter
// ScopeSnapshot (stack + vars only, no substitution) used by quote-body
// inference and overload trials, plus the match-arm exhaustiveness
// check.

// ScopeSnapshot captures the checker's stack and variable environment so
// a trial walk can be sandboxed and undone. It does not capture the
// substitution — callers that need that pair it with a
// Substitution.Checkpoint (see resolveAndApply, inferQuoteSigsItems).
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
	// MatchArmEnumMember: an enum constructor pattern (`member` or
	// `member b1 b2 ...`). TypeArm holds the enum type, EnumMember the
	// member's NameId.
	MatchArmEnumMember
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
	Kind       MatchArmKind
	TypeArm    TypeId // valid when Kind == MatchArmType or MatchArmEnumMember
	EnumMember NameId // valid when Kind == MatchArmEnumMember
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

	case TKEnum:
		variants := c.arena.enumVariants[n.Extra]
		covered := make(map[NameId]bool, len(variants))
		for _, arm := range arms {
			if arm.Kind == MatchArmEnumMember && c.subst.Apply(c.arena, arm.TypeArm) == matched {
				covered[arm.EnumMember] = true
			}
		}
		var missing []string
		for _, v := range variants {
			if !covered[v.Name] {
				missing = append(missing, c.names.Name(v.Name))
			}
		}
		if len(missing) == 0 {
			return true
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrNonExhaustiveMatch,
			Pos:  callSite,
			Hint: "enum match must cover every member or include a wildcard; missing: " + strings.Join(missing, ", "),
		})
		return false

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
