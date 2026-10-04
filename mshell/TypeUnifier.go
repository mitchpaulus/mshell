package main

// Equality unification for the checker described in ai/type-core-calculus.typ
// (§Inference). It only makes two types equal by binding unification
// variables; it never takes a union or width step, which are subtyping and
// are checked separately once both sides are known. Where a union or width
// step would be needed, Unify fails and the checker asks for an annotation.
//
// Unification is not trusted. Every pair it is asked to unify is recorded,
// and once a def body or the script is solved, Recheck checks each pair
// again with the final substitution, using the proved relations: the two
// sides must be equal. So a mistake here shows up as a checker error, never
// as an accepted program.

// Unifier unifies types over a substitution and records what it unified.
type Unifier struct {
	arena *TypeArena
	subst *Substitution
	rel   *Relations
	pairs []typePair
}

func NewUnifier(arena *TypeArena, subst *Substitution, rel *Relations) *Unifier {
	return &Unifier{arena: arena, subst: subst, rel: rel}
}

// Unify makes a and b equal, binding unification variables, and records
// the pair when it succeeds. On failure the substitution may be partly
// changed; a caller that goes on rolls back to a checkpoint.
func (u *Unifier) Unify(a, b TypeId) bool {
	if !u.unify(a, b, nil) {
		return false
	}
	u.pairs = append(u.pairs, typePair{a, b})
	return true
}

// Require records a pair that must be equal once solved without unifying
// it: a check of its own that Recheck makes, as for the outputs of an
// overload choice (TypeCoreChoice.go).
func (u *Unifier) Require(a, b TypeId) {
	u.pairs = append(u.pairs, typePair{a, b})
}

// UnifierCheckpoint is a state of the substitution and the recorded pairs.
type UnifierCheckpoint struct {
	subst SubstCheckpoint
	pairs int
}

func (u *Unifier) Checkpoint() UnifierCheckpoint {
	return UnifierCheckpoint{subst: u.subst.Checkpoint(), pairs: len(u.pairs)}
}

// Rollback undoes the bindings and forgets the pairs recorded since cp, as
// for an overload candidate that did not fit.
func (u *Unifier) Rollback(cp UnifierCheckpoint) {
	u.subst.Rollback(cp.subst)
	u.pairs = u.pairs[:cp.pairs]
}

// UnifiedPair is a pair Recheck found unequal, with both sides resolved.
type UnifiedPair struct {
	A, B TypeId
}

// Recheck checks every recorded pair again with the final substitution and
// returns the pairs whose sides are not equal types.
func (u *Unifier) Recheck() []UnifiedPair {
	var bad []UnifiedPair
	for _, p := range u.pairs {
		a, b := u.subst.Apply(u.arena, p.a), u.subst.Apply(u.arena, p.b)
		if !u.rel.Equal(a, b) {
			bad = append(bad, UnifiedPair{a, b})
		}
	}
	return bad
}

// hasVars reports whether t mentions a unification variable.
func (u *Unifier) hasVars(t TypeId) bool {
	return u.arena.walkTypeVars(t, func(TypeVarId) bool { return true })
}

// unify makes a and b equal. assumed holds the pairs that meet an alias,
// already being unified further up: an alias is unfolded to reach the other
// side's constructors, and a pair met again holds by assumption, as in the
// relations (§Aliases).
func (u *Unifier) unify(a, b TypeId, assumed []typePair) bool {
	ar := u.arena
	a, b = u.subst.Apply(ar, a), u.subst.Apply(ar, b)
	if a == b {
		return true
	}
	if a == TidNothing || b == TidNothing {
		return false
	}
	an, bn := ar.Node(a), ar.Node(b)
	if an.Kind == TKVar {
		return u.subst.Bind(ar, TypeVarId(an.A), b)
	}
	if bn.Kind == TKVar {
		return u.subst.Bind(ar, TypeVarId(bn.A), a)
	}
	if !u.hasVars(a) && !u.hasVars(b) {
		return u.rel.Equal(a, b)
	}
	if an.Kind == TKAlias || bn.Kind == TKAlias {
		p := typePair{a, b}
		for _, q := range assumed {
			if q == p {
				return true
			}
		}
		assumed = append(assumed, p)
		if an.Kind == TKAlias {
			a = ar.aliases[an.A].Body
		}
		if bn.Kind == TKAlias {
			b = ar.aliases[bn.A].Body
		}
		return u.unify(a, b, assumed)
	}
	if an.Kind != bn.Kind {
		return false
	}
	switch an.Kind {
	case TKList:
		return u.unify(TypeId(an.A), TypeId(bn.A), assumed)
	case TKRecord:
		x, y := ar.records[an.Extra], ar.records[bn.Extra]
		return u.unifyField(x.Rest, y.Rest, assumed) &&
			u.unifyLabels(x, y, x.Fields, assumed) && u.unifyLabels(x, y, y.Fields, assumed)
	case TKQuote:
		x, y := ar.quoteSigs[an.Extra], ar.quoteSigs[bn.Extra]
		if x.Diverges != y.Diverges ||
			len(x.Inputs) != len(y.Inputs) || len(x.Outputs) != len(y.Outputs) {
			return false
		}
		for i := range x.Inputs {
			if !u.unify(x.Inputs[i], y.Inputs[i], assumed) {
				return false
			}
		}
		for i := range x.Outputs {
			if !u.unify(x.Outputs[i], y.Outputs[i], assumed) {
				return false
			}
		}
		return true
	case TKEnum:
		if an.A != bn.A {
			return false
		}
		xs, ys := ar.enumArgs[an.Extra], ar.enumArgs[bn.Extra]
		for i := range xs {
			if !u.unify(xs[i], ys[i], assumed) {
				return false
			}
		}
		return true
	case TKCommand:
		return an.B == bn.B && an.Extra == bn.Extra && u.unify(TypeId(an.A), TypeId(bn.A), assumed)
	case TKGrid, TKGridView, TKGridRow:
		return an.A != 0 && bn.A != 0 && u.unify(TypeId(an.A), TypeId(bn.A), assumed)
	}
	// Unions, and everything else not equal already: a union step is
	// subtyping, not unification.
	return false
}

// unifyLabels unifies the labels in fields of the records x and y.
func (u *Unifier) unifyLabels(x, y RecordType, fields []RecordField, assumed []typePair) bool {
	for _, f := range fields {
		if !u.unifyField(x.FieldAt(f.Name), y.FieldAt(f.Name), assumed) {
			return false
		}
	}
	return true
}

func (u *Unifier) unifyField(f, g RecordField, assumed []typePair) bool {
	if f.Status != g.Status {
		return false
	}
	if f.Type == TidNothing || g.Type == TidNothing {
		return f.Type == g.Type
	}
	return u.unify(f.Type, g.Type, assumed)
}
