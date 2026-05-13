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
	n := arena.Node(t)
	switch n.Kind {
	case TKVar:
		v := TypeVarId(n.A)
		if int(v) >= len(s.bound) {
			return t
		}
		bv := s.bound[v]
		if bv == TidNothing {
			return t
		}
		resolved := s.Apply(arena, bv)
		s.bound[v] = resolved // path compression
		return resolved
	case TKMaybe:
		inner := s.Apply(arena, TypeId(n.A))
		if inner == TypeId(n.A) {
			return t
		}
		return arena.MakeMaybe(inner)
	case TKList:
		inner := s.Apply(arena, TypeId(n.A))
		if inner == TypeId(n.A) {
			return t
		}
		return arena.MakeList(inner)
	case TKTuple:
		slots := arena.tupleSlots[n.Extra]
		var rebuilt []TypeId
		changed := false
		for i, slot := range slots {
			rs := s.Apply(arena, slot)
			if rs != slot && !changed {
				rebuilt = make([]TypeId, len(slots))
				copy(rebuilt, slots[:i])
				changed = true
			}
			if changed {
				rebuilt[i] = rs
			}
		}
		if !changed {
			return t
		}
		return arena.MakeTuple(rebuilt)
	case TKDict:
		k := s.Apply(arena, TypeId(n.A))
		v := s.Apply(arena, TypeId(n.B))
		if k == TypeId(n.A) && v == TypeId(n.B) {
			return t
		}
		return arena.MakeDict(k, v)
	case TKShape:
		fields := arena.shapeFields[n.Extra]
		var rebuilt []ShapeField
		changed := false
		for i, f := range fields {
			rt := s.Apply(arena, f.Type)
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
			ra := s.Apply(arena, a)
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
		under := s.Apply(arena, TypeId(n.B))
		if under == TypeId(n.B) {
			return t
		}
		return arena.MakeBrand(NameId(n.A), under)
	case TKCommand:
		argv := s.Apply(arena, TypeId(n.A))
		if argv == TypeId(n.A) {
			return t
		}
		return arena.MakeCommand(argv, CommandCaptureMode(n.B), CommandCaptureMode(n.Extra))
	case TKQuote:
		sig := arena.quoteSigs[n.Extra]
		newIn, inChanged := s.applySpan(arena, sig.Inputs)
		newOut, outChanged := s.applySpan(arena, sig.Outputs)
		if !inChanged && !outChanged {
			return t
		}
		return arena.MakeQuote(QuoteSig{
			Inputs:   newIn,
			Outputs:  newOut,
			Fail:     sig.Fail,
			Pure:     sig.Pure,
			Diverges: sig.Diverges,
			Generics: sig.Generics,
		})
	case TKOverloadedQuote:
		sigs := arena.overloadedQuoteSigs[n.Extra]
		rebuilt := make([]QuoteSig, len(sigs))
		changed := false
		for i, sig := range sigs {
			newIn, inChanged := s.applySpan(arena, sig.Inputs)
			newOut, outChanged := s.applySpan(arena, sig.Outputs)
			rebuilt[i] = QuoteSig{
				Inputs:   newIn,
				Outputs:  newOut,
				Fail:     sig.Fail,
				Pure:     sig.Pure,
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

// applySpan walks a slice of TypeIds, returning a new slice if any element
// resolved to something different and signaling whether the rebuild
// happened. The original slice is returned untouched on no-change so
// callers can compare slice headers cheaply.
func (s *Substitution) applySpan(arena *TypeArena, span []TypeId) ([]TypeId, bool) {
	var out []TypeId
	changed := false
	for i, x := range span {
		rx := s.Apply(arena, x)
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
	n := arena.Node(t)
	switch n.Kind {
	case TKVar:
		if TypeVarId(n.A) == v {
			return true
		}
		if int(n.A) < len(s.bound) && s.bound[n.A] != TidNothing {
			return s.occurs(arena, v, s.bound[n.A])
		}
		return false
	case TKMaybe, TKList:
		return s.occurs(arena, v, TypeId(n.A))
	case TKTuple:
		for _, slot := range arena.tupleSlots[n.Extra] {
			if s.occurs(arena, v, slot) {
				return true
			}
		}
		return false
	case TKDict:
		return s.occurs(arena, v, TypeId(n.A)) || s.occurs(arena, v, TypeId(n.B))
	case TKShape:
		for _, f := range arena.shapeFields[n.Extra] {
			if s.occurs(arena, v, f.Type) {
				return true
			}
		}
		return false
	case TKUnion:
		for _, a := range arena.unionMembers[n.Extra] {
			if s.occurs(arena, v, a) {
				return true
			}
		}
		return false
	case TKBrand:
		return s.occurs(arena, v, TypeId(n.B))
	case TKCommand:
		return s.occurs(arena, v, TypeId(n.A))
	case TKQuote:
		sig := arena.quoteSigs[n.Extra]
		for _, in := range sig.Inputs {
			if s.occurs(arena, v, in) {
				return true
			}
		}
		for _, out := range sig.Outputs {
			if s.occurs(arena, v, out) {
				return true
			}
		}
		return false
	case TKOverloadedQuote:
		for _, sig := range arena.overloadedQuoteSigs[n.Extra] {
			for _, in := range sig.Inputs {
				if s.occurs(arena, v, in) {
					return true
				}
			}
			for _, out := range sig.Outputs {
				if s.occurs(arena, v, out) {
					return true
				}
			}
		}
		return false
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
		Fail:     sig.Fail,
		Pure:     sig.Pure,
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
	case TKTuple:
		slots := c.arena.tupleSlots[n.Extra]
		out := make([]TypeId, len(slots))
		changed := false
		for i, slot := range slots {
			rs := c.renameVars(slot, rename)
			out[i] = rs
			if rs != slot {
				changed = true
			}
		}
		if !changed {
			return t
		}
		return c.arena.MakeTuple(out)
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
			Fail:     sig.Fail,
			Pure:     sig.Pure,
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
				Fail:     sig.Fail,
				Pure:     sig.Pure,
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
