package main

// Substitution and generic instantiation. Phase 6 turns the Phase-3
// "structural acceptance check" into a real Hindley-Milner-style unifier:
// type variables can stand in for concrete types and become bound during
// unification.
//
// Substitution storage is a flat slice indexed by TypeVarId. Apply walks
// composites and rebuilds them through the arena (preserving hashconsing)
// when any inner type resolves to something different. Bind performs an
// occurs check and refuses to bind a variable to a type that mentions it.

// Substitution maps TypeVarIds to the TypeIds they currently resolve to.
// An entry of TidNothing means the variable is unbound. Variables are
// allocated densely from 0 upward via FreshVar, so the slice can be
// indexed directly without bounds-grow logic on Bind (FreshVar is the
// only way to create a var, and it sizes the slice).
type Substitution struct {
	bound []TypeId
}

// FreshVar allocates a new generic variable, reserves its slot in the
// substitution (initially unbound), and returns the variable's TypeId.
// Each call yields a distinct variable.
func (s *Substitution) FreshVar(arena *TypeArena) TypeId {
	id := TypeVarId(len(s.bound))
	s.bound = append(s.bound, TidNothing)
	return arena.MakeVar(id)
}

// SubstCheckpoint records the substitution's state at a point in time
// so it can be rolled back. Used by overload resolution (Phase 9) to
// trial-unify each candidate without polluting state for the next.
type SubstCheckpoint struct {
	bound []TypeId
}

// Checkpoint returns a snapshot of the current substitution. Detached
// from the live state.
func (s *Substitution) Checkpoint() SubstCheckpoint {
	out := make([]TypeId, len(s.bound))
	copy(out, s.bound)
	return SubstCheckpoint{bound: out}
}

// Rollback restores the substitution to a prior snapshot, including
// shrinking the bound slice if the snapshot was smaller. Vars
// allocated since the checkpoint become inaccessible (their indices
// fall off the slice); they remain in the arena but no longer
// resolve through this substitution.
func (s *Substitution) Rollback(snap SubstCheckpoint) {
	if cap(s.bound) >= len(snap.bound) {
		s.bound = s.bound[:len(snap.bound)]
	} else {
		s.bound = make([]TypeId, len(snap.bound))
	}
	copy(s.bound, snap.bound)
}

// Apply resolves a TypeId against the current substitution, walking into
// composites and rebuilding them if any inner type changed. Path
// compression is applied to variable chains so repeated lookups are fast.
func (s *Substitution) Apply(arena *TypeArena, t TypeId) TypeId {
	return s.applyImpl(arena, t, nil)
}

// applyImpl is the shared resolver behind Apply. `skip` holds TypeVarIds that
// must be left untouched: a quote signature's locally-scoped generics are
// symbolic (renamed by Instantiate at each use site) and don't address the
// live substitution, so resolving one could bake an unrelated binding for the
// same TypeVarId into the stored sig — e.g. the `T` in a `(len)` quote
// inferred as `([T] -- int)`. skip is nil for ordinary Apply and is populated
// only while rebuilding the inputs/outputs of a quote whose Generics are
// still in place.
//
// Path compression (writing the resolved type back into the bound slice) only
// happens on full resolves (skip empty); a skipped resolve may leave some
// vars unresolved, so caching it would be wrong.
func (s *Substitution) applyImpl(arena *TypeArena, t TypeId, skip map[TypeVarId]struct{}) TypeId {
	n := arena.Node(t)
	switch n.Kind {
	case TKVar:
		v := TypeVarId(n.A)
		if _, blocked := skip[v]; blocked {
			return t
		}
		if int(v) >= len(s.bound) {
			return t
		}
		bv := s.bound[v]
		if bv == TidNothing {
			return t
		}
		resolved := s.applyImpl(arena, bv, skip)
		if len(skip) == 0 {
			s.bound[v] = resolved // path compression
		}
		return resolved
	case TKMaybe:
		inner := s.applyImpl(arena, TypeId(n.A), skip)
		if inner == TypeId(n.A) {
			return t
		}
		return arena.MakeMaybe(inner)
	case TKList:
		inner := s.applyImpl(arena, TypeId(n.A), skip)
		if inner == TypeId(n.A) {
			return t
		}
		return arena.MakeList(inner)
	case TKDict:
		k := s.applyImpl(arena, TypeId(n.A), skip)
		v := s.applyImpl(arena, TypeId(n.B), skip)
		if k == TypeId(n.A) && v == TypeId(n.B) {
			return t
		}
		return arena.MakeDict(k, v)
	case TKShape:
		fields := arena.shapeFields[n.Extra]
		var rebuilt []ShapeField
		changed := false
		for i, f := range fields {
			rt := s.applyImpl(arena, f.Type, skip)
			if rt != f.Type && !changed {
				rebuilt = make([]ShapeField, len(fields))
				copy(rebuilt, fields[:i])
				changed = true
			}
			if changed {
				rebuilt[i] = ShapeField{Name: f.Name, Type: rt}
			}
		}
		if !changed {
			return t
		}
		return arena.MakeShape(rebuilt)
	case TKUnion:
		arms := arena.unionMembers[n.Extra]
		var rebuilt []TypeId
		changed := false
		for i, a := range arms {
			ra := s.applyImpl(arena, a, skip)
			if ra != a && !changed {
				rebuilt = make([]TypeId, len(arms))
				copy(rebuilt, arms[:i])
				changed = true
			}
			if changed {
				rebuilt[i] = ra
			}
		}
		if !changed {
			return t
		}
		return arena.MakeUnion(rebuilt, NameId(n.A))
	case TKBrand:
		under := s.applyImpl(arena, TypeId(n.B), skip)
		if under == TypeId(n.B) {
			return t
		}
		return arena.MakeBrand(NameId(n.A), under)
	case TKCommand:
		argv := s.applyImpl(arena, TypeId(n.A), skip)
		if argv == TypeId(n.A) {
			return t
		}
		return arena.MakeCommand(argv, CommandCaptureMode(n.B), CommandCaptureMode(n.Extra))
	case TKQuote:
		sig := arena.quoteSigs[n.Extra]
		inner := genericsSkip(sig)
		newIn, inChanged := s.mapSpan(arena, sig.Inputs, inner)
		newOut, outChanged := s.mapSpan(arena, sig.Outputs, inner)
		if !inChanged && !outChanged {
			return t
		}
		return arena.MakeQuote(QuoteSig{
			Inputs:   newIn,
			Outputs:  newOut,
			Diverges: sig.Diverges,
			Generics: sig.Generics,
		})
	case TKOverloadedQuote:
		sigs := arena.overloadedQuoteSigs[n.Extra]
		rebuilt := make([]QuoteSig, len(sigs))
		changed := false
		for i, sig := range sigs {
			inner := genericsSkip(sig)
			newIn, inChanged := s.mapSpan(arena, sig.Inputs, inner)
			newOut, outChanged := s.mapSpan(arena, sig.Outputs, inner)
			rebuilt[i] = QuoteSig{
				Inputs:   newIn,
				Outputs:  newOut,
				Diverges: sig.Diverges,
				Generics: sig.Generics,
			}
			if inChanged || outChanged {
				changed = true
			}
		}
		if !changed {
			return t
		}
		return arena.MakeOverloadedQuote(rebuilt)
	}
	return t
}

// mapSpan walks a slice of TypeIds through applyImpl, returning a new slice if
// any element resolved to something different and signaling whether the
// rebuild happened. The original slice is returned untouched on no-change so
// callers can compare slice headers cheaply. `skip` is threaded into each
// element's resolution (empty for the ordinary case).
func (s *Substitution) mapSpan(arena *TypeArena, span []TypeId, skip map[TypeVarId]struct{}) ([]TypeId, bool) {
	var out []TypeId
	changed := false
	for i, x := range span {
		rx := s.applyImpl(arena, x, skip)
		if rx != x && !changed {
			out = make([]TypeId, len(span))
			copy(out, span[:i])
			changed = true
		}
		if changed {
			out[i] = rx
		}
	}
	if !changed {
		return span, false
	}
	return out, true
}

// genericsSkip builds the skip-set for a quote signature's locally-scoped
// generics, or nil when the sig is monomorphic.
func genericsSkip(sig QuoteSig) map[TypeVarId]struct{} {
	if len(sig.Generics) == 0 {
		return nil
	}
	skip := make(map[TypeVarId]struct{}, len(sig.Generics))
	for _, v := range sig.Generics {
		skip[v] = struct{}{}
	}
	return skip
}

// Bind sets the variable v's resolution to t. Returns false on occurs-check
// failure (binding would create an infinite type) or if v is already bound.
// Callers should typically have Apply'd both sides first so v is known to
// be unbound before reaching here.
func (s *Substitution) Bind(arena *TypeArena, v TypeVarId, t TypeId) bool {
	if int(v) >= len(s.bound) {
		return false
	}
	if s.bound[v] != TidNothing {
		return false
	}
	// If t is the same variable, nothing to do (vacuously consistent).
	tn := arena.Node(t)
	if tn.Kind == TKVar && TypeVarId(tn.A) == v {
		return true
	}
	if s.occurs(arena, v, t) {
		return false
	}
	s.bound[v] = t
	return true
}

// occurs reports whether v appears anywhere within t (after resolving
// chained variable bindings). Required to keep the substitution finite.
func (s *Substitution) occurs(arena *TypeArena, v TypeVarId, t TypeId) bool {
	return arena.walkTypeVars(t, func(x TypeVarId) bool {
		if x == v {
			return true
		}
		// Follow a bound variable's chain; an occurrence reachable only
		// through the binding still counts.
		if int(x) < len(s.bound) && s.bound[x] != TidNothing {
			return s.occurs(arena, v, s.bound[x])
		}
		return false
	})
}

// walkTypeVars visits each TKVar reachable from t by descending structurally
// through every composite kind. For every variable it calls visit(v); a true
// return short-circuits the whole walk and walkTypeVars returns true. The
// visit callback owns any substitution-chain following — it has the context
// to decide whether a bound variable's binding should be chased.
func (a *TypeArena) walkTypeVars(t TypeId, visit func(TypeVarId) bool) bool {
	n := a.Node(t)
	switch n.Kind {
	case TKVar:
		return visit(TypeVarId(n.A))
	case TKMaybe, TKList:
		return a.walkTypeVars(TypeId(n.A), visit)
	case TKDict:
		return a.walkTypeVars(TypeId(n.A), visit) || a.walkTypeVars(TypeId(n.B), visit)
	case TKBrand:
		return a.walkTypeVars(TypeId(n.B), visit)
	case TKCommand:
		return a.walkTypeVars(TypeId(n.A), visit)
	case TKShape:
		for _, f := range a.shapeFields[n.Extra] {
			if a.walkTypeVars(f.Type, visit) {
				return true
			}
		}
	case TKUnion:
		for _, m := range a.unionMembers[n.Extra] {
			if a.walkTypeVars(m, visit) {
				return true
			}
		}
	case TKQuote:
		if a.walkSigVars(a.quoteSigs[n.Extra], visit) {
			return true
		}
	case TKOverloadedQuote:
		for _, sig := range a.overloadedQuoteSigs[n.Extra] {
			if a.walkSigVars(sig, visit) {
				return true
			}
		}
	}
	return false
}

// walkSigVars visits the TKVars in a quote signature's inputs and outputs.
func (a *TypeArena) walkSigVars(sig QuoteSig, visit func(TypeVarId) bool) bool {
	for _, in := range sig.Inputs {
		if a.walkTypeVars(in, visit) {
			return true
		}
	}
	for _, out := range sig.Outputs {
		if a.walkTypeVars(out, visit) {
			return true
		}
	}
	return false
}

// Instantiate prepares a polymorphic sig for use at a call site by
// allocating fresh variables for every entry in sig.Generics and
// rewriting the sig's inputs/outputs to reference those fresh variables.
// A monomorphic sig (no generics) is returned unchanged.
func (c *Checker) Instantiate(sig QuoteSig) QuoteSig {
	if len(sig.Generics) == 0 {
		return sig
	}
	rename := make(map[TypeVarId]TypeId, len(sig.Generics))
	for _, oldVar := range sig.Generics {
		rename[oldVar] = c.subst.FreshVar(c.arena)
	}
	freshIn := make([]TypeId, len(sig.Inputs))
	for i, in := range sig.Inputs {
		freshIn[i] = c.renameVars(in, rename)
	}
	freshOut := make([]TypeId, len(sig.Outputs))
	for i, out := range sig.Outputs {
		freshOut[i] = c.renameVars(out, rename)
	}
	var freshBindings map[NameId]TypeId
	if len(sig.Bindings) > 0 {
		freshBindings = make(map[NameId]TypeId, len(sig.Bindings))
		for name, t := range sig.Bindings {
			freshBindings[name] = c.renameVars(t, rename)
		}
	}
	return QuoteSig{
		Inputs:   freshIn,
		Outputs:  freshOut,
		Diverges: sig.Diverges,
		Bindings: freshBindings,
		// Generics intentionally dropped: instantiation consumes them.
	}
}

// renameVars walks a type and replaces any TKVar listed in rename with its
// fresh substitute, rebuilding composites through the arena so hashconsing
// is preserved.
func (c *Checker) renameVars(t TypeId, rename map[TypeVarId]TypeId) TypeId {
	n := c.arena.Node(t)
	switch n.Kind {
	case TKVar:
		if fresh, ok := rename[TypeVarId(n.A)]; ok {
			return fresh
		}
		return t
	case TKMaybe:
		inner := c.renameVars(TypeId(n.A), rename)
		if inner == TypeId(n.A) {
			return t
		}
		return c.arena.MakeMaybe(inner)
	case TKList:
		inner := c.renameVars(TypeId(n.A), rename)
		if inner == TypeId(n.A) {
			return t
		}
		return c.arena.MakeList(inner)
	case TKDict:
		k := c.renameVars(TypeId(n.A), rename)
		v := c.renameVars(TypeId(n.B), rename)
		if k == TypeId(n.A) && v == TypeId(n.B) {
			return t
		}
		return c.arena.MakeDict(k, v)
	case TKShape:
		fields := c.arena.shapeFields[n.Extra]
		out := make([]ShapeField, len(fields))
		changed := false
		for i, f := range fields {
			rt := c.renameVars(f.Type, rename)
			out[i] = ShapeField{Name: f.Name, Type: rt}
			if rt != f.Type {
				changed = true
			}
		}
		if !changed {
			return t
		}
		return c.arena.MakeShape(out)
	case TKUnion:
		arms := c.arena.unionMembers[n.Extra]
		out := make([]TypeId, len(arms))
		changed := false
		for i, a := range arms {
			ra := c.renameVars(a, rename)
			out[i] = ra
			if ra != a {
				changed = true
			}
		}
		if !changed {
			return t
		}
		return c.arena.MakeUnion(out, NameId(n.A))
	case TKBrand:
		under := c.renameVars(TypeId(n.B), rename)
		if under == TypeId(n.B) {
			return t
		}
		return c.arena.MakeBrand(NameId(n.A), under)
	case TKCommand:
		argv := c.renameVars(TypeId(n.A), rename)
		if argv == TypeId(n.A) {
			return t
		}
		return c.arena.MakeCommand(argv, CommandCaptureMode(n.B), CommandCaptureMode(n.Extra))
	case TKQuote:
		sig := c.arena.quoteSigs[n.Extra]
		newIn := make([]TypeId, len(sig.Inputs))
		newOut := make([]TypeId, len(sig.Outputs))
		changed := false
		for i, in := range sig.Inputs {
			ri := c.renameVars(in, rename)
			newIn[i] = ri
			if ri != in {
				changed = true
			}
		}
		for i, out := range sig.Outputs {
			ro := c.renameVars(out, rename)
			newOut[i] = ro
			if ro != out {
				changed = true
			}
		}
		var bindings map[NameId]TypeId
		if len(sig.Bindings) > 0 {
			bindings = make(map[NameId]TypeId, len(sig.Bindings))
			for name, bindingType := range sig.Bindings {
				renamed := c.renameVars(bindingType, rename)
				bindings[name] = renamed
				if renamed != bindingType {
					changed = true
				}
			}
		}
		if !changed {
			return t
		}
		return c.arena.MakeQuote(QuoteSig{
			Inputs:   newIn,
			Outputs:  newOut,
			Diverges: sig.Diverges,
			Bindings: bindings,
			Generics: nil,
		})
	case TKOverloadedQuote:
		sigs := c.arena.overloadedQuoteSigs[n.Extra]
		out := make([]QuoteSig, len(sigs))
		changed := false
		for i, sig := range sigs {
			newIn := make([]TypeId, len(sig.Inputs))
			newOut := make([]TypeId, len(sig.Outputs))
			for j, in := range sig.Inputs {
				ri := c.renameVars(in, rename)
				newIn[j] = ri
				if ri != in {
					changed = true
				}
			}
			for j, outType := range sig.Outputs {
				ro := c.renameVars(outType, rename)
				newOut[j] = ro
				if ro != outType {
					changed = true
				}
			}
			out[i] = QuoteSig{
				Inputs:   newIn,
				Outputs:  newOut,
				Diverges: sig.Diverges,
				Generics: nil,
			}
		}
		if !changed {
			return t
		}
		return c.arena.MakeOverloadedQuote(out)
	}
	return t
}
