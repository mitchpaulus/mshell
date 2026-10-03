package main

// The substitution of type variables, which equality unification
// (TypeUnifier.go) extends.
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
//
// Checkpoints are positions in a trail: once a checkpoint has been
// taken, every write logs the value it replaces, and Rollback undoes the
// writes after its checkpoint, newest first. Checkpoint is O(1), a write
// appends one entry to one reused slice, and Rollback costs the writes it
// undoes. Checkpoints are used last in, first out: rolling back to a
// checkpoint discards every checkpoint taken after it, while it stays
// valid itself (a trial may be rolled back to the same point several
// times). The checker only tries a candidate and rolls back to just before
// it, so that is all it needs.
//
// Ids are never reused: Rollback leaves the slice at full length, and
// slots allocated after the checkpoint just revert to unbound. So a type
// that escapes one branch can't alias a variable another branch creates.
type Substitution struct {
	bound []TypeId
	// trail holds the old value of every write since the first
	// checkpoint. Before one is taken no state can be returned to, so
	// writes need no log (logging is false).
	trail   []substWrite
	logging bool
}

// substWrite records that slot v held t before a write.
type substWrite struct {
	v TypeVarId
	t TypeId
}

// Reset empties the substitution for a new unit, keeping its storage.
func (s *Substitution) Reset() {
	s.bound = s.bound[:0]
	s.trail = s.trail[:0]
	s.logging = false
}

// FreshVar allocates a new generic variable, reserves its slot in the
// substitution (initially unbound), and returns the variable's TypeId.
// Each call yields a distinct variable.
func (s *Substitution) FreshVar(arena *TypeArena) TypeId {
	id := TypeVarId(len(s.bound))
	s.bound = append(s.bound, TidNothing)
	return arena.MakeVar(id)
}

// set writes slot v, logging the old value so a checkpoint can be restored.
func (s *Substitution) set(v TypeVarId, t TypeId) {
	if s.logging {
		s.trail = append(s.trail, substWrite{v, s.bound[v]})
	}
	s.bound[v] = t
}

// SubstCheckpoint records the substitution's state at a point in time
// so it can be rolled back: trying an overload candidate, or a match that
// may fail, without leaving its bindings behind. It is the trail's length.
type SubstCheckpoint struct {
	n int
}

// Checkpoint returns the current state. Writes after it don't change it.
func (s *Substitution) Checkpoint() SubstCheckpoint {
	s.logging = true
	return SubstCheckpoint{n: len(s.trail)}
}

// Rollback restores the state snap was taken at, undoing the writes made
// since, newest first. Checkpoints taken after snap are no longer valid.
func (s *Substitution) Rollback(snap SubstCheckpoint) {
	for i := len(s.trail) - 1; i >= snap.n; i-- {
		w := s.trail[i]
		s.bound[w.v] = w.t
	}
	s.trail = s.trail[:snap.n]
}

// Apply resolves a TypeId against the current substitution, walking into
// composites and rebuilding them through the arena (so hash-consing holds)
// when any inner type changed; an unchanged subtree returns the original
// TypeId, so callers can compare ids cheaply. A variable's binding is
// path-compressed, so repeated lookups are fast.
func (s *Substitution) Apply(a *TypeArena, t TypeId) TypeId {
	n := a.Node(t)
	if n.Flags&NodeHasVar == 0 {
		return t
	}
	switch n.Kind {
	case TKVar:
		v := TypeVarId(n.A)
		if int(v) >= len(s.bound) || s.bound[v] == TidNothing {
			return t
		}
		r := s.Apply(a, s.bound[v])
		if r != s.bound[v] {
			s.set(v, r)
		}
		return r
	case TKList:
		inner := s.Apply(a, TypeId(n.A))
		if inner == TypeId(n.A) {
			return t
		}
		return a.MakeList(inner)
	case TKUnion:
		arms, changed := s.applySpan(a, a.unionMembers[n.Extra])
		if !changed {
			return t
		}
		return a.MakeUnion(arms)
	case TKCommand:
		argv := s.Apply(a, TypeId(n.A))
		if argv == TypeId(n.A) {
			return t
		}
		return a.MakeCommand(argv, CommandCaptureMode(n.B), CommandCaptureMode(n.Extra))
	case TKQuote:
		sig := a.quoteSigs[n.Extra]
		ins, inChanged := s.applySpan(a, sig.Inputs)
		outs, outChanged := s.applySpan(a, sig.Outputs)
		if !inChanged && !outChanged {
			return t
		}
		return a.MakeQuote(QuoteSig{Inputs: ins, Outputs: outs, Diverges: sig.Diverges})
	case TKRecord:
		// The fields are copied only once one changes. Apply may add
		// records, so the record is read again by index each time.
		var fields []RecordField
		nf := len(a.records[n.Extra].Fields)
		for i := range nf {
			f := a.records[n.Extra].Fields[i]
			if f.Type == TidNothing {
				continue
			}
			ft := s.Apply(a, f.Type)
			if ft != f.Type && fields == nil {
				fields = make([]RecordField, nf)
				copy(fields, a.records[n.Extra].Fields)
			}
			if fields != nil {
				fields[i].Type = ft
			}
		}
		rest := a.records[n.Extra].Rest
		if rest.Type != TidNothing {
			rest.Type = s.Apply(a, rest.Type)
		}
		if fields == nil {
			if rest.Type == a.records[n.Extra].Rest.Type {
				return t
			}
			fields = a.records[n.Extra].Fields
		}
		return a.MakeRecord(fields, rest)
	case TKEnum:
		args, changed := s.applySpan(a, a.enumArgs[n.Extra])
		if !changed {
			return t
		}
		return a.MakeEnum(n.A, args)
	case TKGrid, TKGridView, TKGridRow:
		rec := s.Apply(a, TypeId(n.A))
		if rec == TypeId(n.A) {
			return t
		}
		return a.MakeGridOf(n.Kind, rec)
	}
	return t
}

// applySpan applies the substitution to each type of span. It returns span
// itself when nothing changed, and a new slice otherwise.
func (s *Substitution) applySpan(a *TypeArena, span []TypeId) ([]TypeId, bool) {
	var out []TypeId
	for i, x := range span {
		rx := s.Apply(a, x)
		if rx != x && out == nil {
			out = make([]TypeId, len(span))
			copy(out, span[:i])
		}
		if out != nil {
			out[i] = rx
		}
	}
	if out == nil {
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
	s.set(v, t)
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
	if n.Flags&NodeHasVar == 0 {
		return false
	}
	switch n.Kind {
	case TKVar:
		return visit(TypeVarId(n.A))
	case TKList, TKCommand, TKGrid, TKGridView, TKGridRow:
		return a.walkTypeVars(TypeId(n.A), visit)
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
	case TKRecord:
		rec := a.records[n.Extra]
		for _, f := range rec.Fields {
			if f.Type != TidNothing && a.walkTypeVars(f.Type, visit) {
				return true
			}
		}
		if rec.Rest.Type != TidNothing && a.walkTypeVars(rec.Rest.Type, visit) {
			return true
		}
	case TKEnum:
		for _, t := range a.enumArgs[n.Extra] {
			if a.walkTypeVars(t, visit) {
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
