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
// Checkpoints are versions of a persistent array (Baker's rerooting):
// bound holds the current version, and every other version is a chain of
// undo logs leading to it. Checkpoint is O(1), a write appends one undo
// entry, and Rollback costs the writes between the two versions, which is
// nothing when the walker returns to the state it just captured.
//
// Ids are never reused: Rollback leaves the slice at full length, and
// slots allocated after the checkpoint just revert to unbound. So a type
// that escapes one branch can't alias a variable another branch creates.
type Substitution struct {
	bound []TypeId
	// root is the newest version, which the undo log in root.undo takes
	// back from bound. Nil until the first Checkpoint: before that no
	// version can be returned to, so writes need no log.
	root *substVersion
}

// substVersion is one version of a Substitution. For the root, applying
// undo in reverse to bound gives this version; for any other, applying
// it in reverse to the version at next does.
type substVersion struct {
	next *substVersion
	undo []substWrite
}

// substWrite records that slot v held t before a write.
type substWrite struct {
	v TypeVarId
	t TypeId
}

// FreshVar allocates a new generic variable, reserves its slot in the
// substitution (initially unbound), and returns the variable's TypeId.
// Each call yields a distinct variable.
func (s *Substitution) FreshVar(arena *TypeArena) TypeId {
	id := TypeVarId(len(s.bound))
	s.bound = append(s.bound, TidNothing)
	return arena.MakeVar(id)
}

// set writes slot v, logging the old value so the root can be restored.
func (s *Substitution) set(v TypeVarId, t TypeId) {
	if s.root != nil {
		s.root.undo = append(s.root.undo, substWrite{v, s.bound[v]})
	}
	s.bound[v] = t
}

// SubstCheckpoint records the substitution's state at a point in time
// so it can be rolled back: trying an overload candidate, or a match that
// may fail, without leaving its bindings behind.
type SubstCheckpoint struct {
	v *substVersion
}

// Checkpoint returns the current version. Writes after it don't change it.
func (s *Substitution) Checkpoint() SubstCheckpoint {
	if s.root == nil || len(s.root.undo) > 0 {
		// Nothing written since the root was taken reuses it; otherwise
		// the current state becomes the new root, and the old root now
		// differs from it by exactly its log.
		v := &substVersion{}
		if s.root != nil {
			s.root.next = v
		}
		s.root = v
	}
	return SubstCheckpoint{v: s.root}
}

// Rollback restores the version snap was taken at, discarding writes made
// since the last Checkpoint, and makes it the root.
func (s *Substitution) Rollback(snap SubstCheckpoint) {
	// Undo writes since the root, which no version refers to.
	s.undoInto(s.root, nil)
	// Walk the path from the target to the root, then reroot along it
	// from the root end: each step moves the current state one version
	// toward the target, and logs the way back on the version it left.
	var path []*substVersion
	for v := snap.v; v != s.root; v = v.next {
		path = append(path, v)
	}
	for i := len(path) - 1; i >= 0; i-- {
		v, old := path[i], s.root
		s.undoInto(v, old)
		old.next = v
		v.next = nil
		s.root = v
	}
}

// undoInto applies v's log to bound in reverse and empties it. If back is
// not nil, it receives the log that redoes what was undone.
func (s *Substitution) undoInto(v *substVersion, back *substVersion) {
	for i := len(v.undo) - 1; i >= 0; i-- {
		w := v.undo[i]
		if back != nil {
			back.undo = append(back.undo, substWrite{w.v, s.bound[w.v]})
		}
		s.bound[w.v] = w.t
	}
	v.undo = v.undo[:0]
}

// Apply resolves a TypeId against the current substitution, walking into
// composites and rebuilding them through the arena (so hash-consing holds)
// when any inner type changed; an unchanged subtree returns the original
// TypeId, so callers can compare ids cheaply. A variable's binding is
// path-compressed, so repeated lookups are fast.
func (s *Substitution) Apply(a *TypeArena, t TypeId) TypeId {
	n := a.Node(t)
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
		rec := a.records[n.Extra]
		changed := false
		fields := make([]RecordField, len(rec.Fields))
		for i, f := range rec.Fields {
			fields[i] = f
			if f.Type != TidNothing {
				fields[i].Type = s.Apply(a, f.Type)
				changed = changed || fields[i].Type != f.Type
			}
		}
		rest := rec.Rest
		if rest.Type != TidNothing {
			rest.Type = s.Apply(a, rest.Type)
			changed = changed || rest.Type != rec.Rest.Type
		}
		if !changed {
			return t
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
