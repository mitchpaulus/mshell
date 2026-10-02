package main

import "strings"

// Stack slots, signatures and the builtin table of the core checker
// (ai/type-core-calculus.typ). Everything here is small and held by value:
// a slot is 16 bytes, a stack is one []coreSlot, and the builtin table is
// two slices indexed by NameId and TokenType.

// coreSlot is one stack slot: its type, and whether the value is fresh,
// meaning nothing else references it or anything inside it
// (design doc, "Freshness"). A value of an immutable type is as good as
// fresh wherever it is; see coreChecker.freshish.
type coreSlot struct {
	t TypeId
	// pq is 1 + the index of the quote literal waiting in this slot (see
	// TypeCoreQuote.go), or 0.
	pq uint32
	// lit is the text of a string literal in this slot, or NameNone: a
	// literal key (TypeCoreDict.go). For a list literal of string literals
	// it is litListTag | 1 + an index into the unit's litLists: the names
	// a grid word reads (TypeCoreGrid.go). Read it with key and litNames.
	lit   NameId
	fresh bool
	// part is 1 + the index of the slot's partly new mark in the unit's
	// parts, or 0 (TypeCorePartial.go). A slot with part != 0 is not fresh.
	part uint16
	// origin is 1 + the index in c.origins of the join that made the
	// slot's union type, or 0: an error about the value names the arm
	// each member came from.
	origin uint32
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
	// freeOut is set for a standard library def whose outputs mention a
	// generic that no input mentions. Its body is not checked, so nothing
	// says what that output is; a call is an error (TypeCore.go).
	freeOut bool
}

// litListTag marks a slot's lit as a list of literal names.
const litListTag NameId = 1 << 31

// key is the text of the string literal in the slot, or NameNone.
func (s coreSlot) key() NameId {
	if s.lit&litListTag != 0 {
		return NameNone
	}
	return s.lit
}

// litNames returns the names of the list literal of string literals in
// slot s; ok is false when s holds no such literal.
func (c *coreChecker) litNames(s coreSlot) ([]NameId, bool) {
	if s.lit&litListTag == 0 {
		return nil, false
	}
	return c.litLists[s.lit&^litListTag-1], true
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
	// index is the indexer `:n:`; slice is `n:`, `:n` and `a:b`.
	index, slice []coreSig
	// multi is a list of indexers (`:0:,2:`), whose parts the runtime
	// concatenates, which only lists, strings, paths and bytes do.
	// multiIndex is a list of `:n:` indexers only, which on a pipe gives a
	// pipe of those commands.
	multi, multiIndex []coreSig
	appendBelow  coreSig
	// urlEncodeLists are the list types a dict given to urlEncode may hold
	// (TypeCoreDict.go).
	urlEncodeLists []TypeId
	// completion is ([str] -- CompletionResult): every def with `complete`
	// metadata must be below it, since completionDefs gives its body as a
	// quote of that type.
	completion TypeId
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

// outputOnlyGeneric reports whether a signature has a generic that its
// outputs mention and its inputs do not.
func outputOnlyGeneric(ar *TypeArena, p coreSigParts) bool {
	for g := range p.gens {
		in, out := false, false
		for _, t := range p.ins {
			in = in || mentionsParam(ar, t, g)
		}
		for _, t := range p.outs {
			out = out || mentionsParam(ar, t, g)
		}
		if out && !in {
			return true
		}
	}
	return false
}

// mentionsParam reports whether t mentions the generic TKParam g.
func mentionsParam(ar *TypeArena, t TypeId, g int) bool {
	n := ar.nodes[t]
	switch n.Kind {
	case TKParam:
		return int(n.A) == g
	case TKList, TKCommand:
		return mentionsParam(ar, TypeId(n.A), g)
	case TKRecord:
		rec := ar.records[n.Extra]
		for _, f := range append(rec.Fields, rec.Rest) {
			if f.Type != TidNothing && mentionsParam(ar, f.Type, g) {
				return true
			}
		}
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			if mentionsParam(ar, m, g) {
				return true
			}
		}
	case TKQuote:
		sig := ar.quoteSigs[n.Extra]
		for _, x := range append(append([]TypeId(nil), sig.Inputs...), sig.Outputs...) {
			if mentionsParam(ar, x, g) {
				return true
			}
		}
	case TKEnum:
		for _, x := range ar.enumArgs[n.Extra] {
			if mentionsParam(ar, x, g) {
				return true
			}
		}
	case TKGrid, TKGridView, TKGridRow:
		return n.A != 0 && mentionsParam(ar, TypeId(n.A), g)
	}
	return false
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
		return typeMentions(ar, TypeId(n.A), kind)
	}
	return false
}

// formatCoreSig writes sig as a def would declare it: generics by name, and
// `new` on the outputs that are always new.
func formatCoreSig(arena *TypeArena, names *NameTable, rel *Relations, sig *coreSig) string {
	named := make([]TypeId, len(sig.gens))
	for g, name := range sig.gens {
		named[g] = arena.MakeRigid(name)
	}
	var sb strings.Builder
	sb.WriteByte('(')
	for i, t := range sig.ins {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(FormatType(arena, names, rel.SubstParams(t, named)))
	}
	sb.WriteString(" --")
	if sig.diverges {
		sb.WriteString(" never")
	}
	for j, t := range sig.outs {
		sb.WriteByte(' ')
		if sig.newOut&(1<<j) != 0 {
			sb.WriteString("new ")
		}
		sb.WriteString(FormatType(arena, names, rel.SubstParams(t, named)))
	}
	sb.WriteByte(')')
	return sb.String()
}
