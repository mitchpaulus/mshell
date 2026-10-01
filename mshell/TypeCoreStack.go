package main

// Stack slots, signatures and the builtin table of the core checker
// (ai/type-core-calculus.typ). Everything here is small and held by value:
// a slot is 12 bytes, a stack is one []coreSlot, and the builtin table is
// two slices indexed by NameId and TokenType.

// coreSlot is one stack slot: its type, and whether the value is fresh,
// meaning nothing else references it or anything inside it
// (design doc, "Freshness"). A value of an immutable type is as good as
// fresh wherever it is; see coreChecker.freshish.
type coreSlot struct {
	t TypeId
	// pq is 1 + the index of the quote literal waiting in this slot (see
	// TypeCoreQuote.go), or 0.
	pq    uint32
	fresh bool
}

// coreSig is a builtin or def signature. Its generics are enum-parameter
// types: TKParam i is gens[i] (see TypeCoreResolve.go).
type coreSig struct {
	ins, outs []TypeId
	gens      []NameId
	// genIn and genOut have bit i set when ins[i] or outs[i] mentions a
	// generic, so instantiating skips the rest.
	genIn, genOut uint64
	// newOut has bit i set when outs[i] is always new (fresh).
	newOut uint64
	// newListOut has bit i set when outs[i] is a new list, fresh when its
	// elements are immutable.
	newListOut uint64
	// keepOut has bit i set when outs[i] is fresh when every input is fresh
	// or immutable: `just` and `?` keep freshness.
	keepOut  uint64
	diverges bool
	// child says the word runs its quote arguments on a child stack, as
	// each and map do, which decides where they may break.
	child bool
}

func newCoreSig(ar *TypeArena, p coreSigParts) coreSig {
	s := coreSig{ins: p.ins, outs: p.outs, gens: p.gens, newOut: p.newOut, diverges: p.diverges}
	if len(p.gens) > 0 {
		for i, t := range p.ins {
			if i < 64 && typeMentions(ar, t, TKParam) {
				s.genIn |= 1 << i
			}
		}
		for i, t := range p.outs {
			if i < 64 && typeMentions(ar, t, TKParam) {
				s.genOut |= 1 << i
			}
		}
	}
	return s
}

// coreTable holds the builtin signatures, by name and by token type.
type coreTable struct {
	byName  [][]coreSig
	byToken [][]coreSig
	// partialName and partialToken mark words whose entries cover only some
	// of what the runtime accepts, while the table is being ported: a call
	// that fits none of them is not checked yet, rather than an error.
	partialName  map[NameId]string
	partialToken map[TokenType]string
}

func (t *coreTable) setName(id NameId, sigs []coreSig) {
	for int(id) >= len(t.byName) {
		t.byName = append(t.byName, nil)
	}
	t.byName[id] = sigs
}

func (t *coreTable) setToken(tt TokenType, sigs []coreSig) {
	for int(tt) >= len(t.byToken) {
		t.byToken = append(t.byToken, nil)
	}
	t.byToken[tt] = sigs
}

func (t *coreTable) name(id NameId) []coreSig {
	if int(id) < len(t.byName) {
		return t.byName[id]
	}
	return nil
}

func (t *coreTable) token(tt TokenType) []coreSig {
	if int(tt) < len(t.byToken) {
		return t.byToken[tt]
	}
	return nil
}

// typeMentions reports whether t mentions a type of the given kind
// (TKVar or TKParam). Aliases are closed, so their bodies are skipped.
func typeMentions(ar *TypeArena, t TypeId, kind TypeKind) bool {
	n := ar.nodes[t]
	if n.Kind == kind {
		return true
	}
	switch n.Kind {
	case TKList:
		return typeMentions(ar, TypeId(n.A), kind)
	case TKRecord:
		rec := ar.records[n.Extra]
		for _, f := range rec.Fields {
			if f.Type != TidNothing && typeMentions(ar, f.Type, kind) {
				return true
			}
		}
		return rec.Rest.Type != TidNothing && typeMentions(ar, rec.Rest.Type, kind)
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			if typeMentions(ar, m, kind) {
				return true
			}
		}
	case TKQuote:
		sig := ar.quoteSigs[n.Extra]
		for _, x := range sig.Inputs {
			if typeMentions(ar, x, kind) {
				return true
			}
		}
		for _, x := range sig.Outputs {
			if typeMentions(ar, x, kind) {
				return true
			}
		}
	case TKEnum:
		for _, x := range ar.enumArgs[n.Extra] {
			if typeMentions(ar, x, kind) {
				return true
			}
		}
	case TKCommand:
		return typeMentions(ar, TypeId(n.A), kind)
	case TKGrid, TKGridView, TKGridRow:
		for _, col := range ar.gridSchemas[n.Extra].Columns {
			if typeMentions(ar, col.Type, kind) {
				return true
			}
		}
	}
	return false
}
