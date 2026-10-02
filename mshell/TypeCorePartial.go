package main

import "strings"

// Partly new values (ai/type-core-calculus.typ, "Freshness, per object";
// formal-ver/Partial.v).
//
// A value may be new at the top while values inside it are stored:
// `{url: "x", cookieJar: @jar}` is a new dict that holds a stored list. Its
// own type may change, as any new value's may, so it may gain optional
// labels; the stored list keeps its type, so at that position only Sub
// applies. Such a slot has part != 0: it is the mark at the slot's top,
// c.parts[part-1]. The checker builds these only for list and dict
// literals and for `set` with a literal key on a new dict; everything else
// treats them as shared values, which is always sound (committing,
// ss_m_forget in formal-ver/Typing.v).

// coreMark is the freshness of one position inside a value: markShared,
// markNew, or 2+i for the partly new value c.parts[i].
type coreMark uint32

const (
	markShared coreMark = 0
	markNew    coreMark = 1
)

// corePart is a new list whose elements have mark elem, or a new dict
// whose label l has the mark labels gives it, and rest otherwise (MList
// and MRec in formal-ver/Typing.v).
type corePart struct {
	list   bool
	elem   coreMark
	labels []coreLabelMark
	rest   coreMark
}

type coreLabelMark struct {
	name NameId
	m    coreMark
}

// maxParts bounds the partly new marks of one unit; past it a value is
// treated as shared, which is sound.
const maxParts = 1<<16 - 1

func (p *corePart) labelMark(name NameId) coreMark {
	for _, l := range p.labels {
		if l.name == name {
			return l.m
		}
	}
	return p.rest
}

// newPart records a partly new mark and returns the slot's part for it,
// or 0 when there are too many.
func (c *coreChecker) newPart(p corePart) uint16 {
	if len(c.parts) >= maxParts {
		return 0
	}
	c.parts = append(c.parts, p)
	return uint16(len(c.parts))
}

// slotMark is the mark of a slot's value: partly new, new, or shared.
func slotMark(s coreSlot) coreMark {
	switch {
	case s.part != 0:
		return coreMark(s.part) + 1
	case s.fresh:
		return markNew
	}
	return markShared
}

// innerMark is the mark a value gets as an element or a dict value: as for
// a slot, and a value with no list, dict or grid in it counts as new
// (ss_imm).
func (c *coreChecker) innerMark(s coreSlot) coreMark {
	if s.part == 0 && c.freshish(s) {
		return markNew
	}
	return slotMark(s)
}

// share makes a slot a shared value: its value is now referenced twice,
// or from a variable.
func (s *coreSlot) share() {
	s.fresh = false
	s.part = 0
	// A list literal's names hold only while nothing else can change the
	// list: a copy changed in place would leave them stale.
	if s.lit&litListTag != 0 {
		s.lit = NameNone
	}
}

// markBelow decides a checking position between solved types for a value
// with mark m. A partly new value is retyped position by position (msub),
// or else committed and compared by Sub; kept reports which, since after a
// commit the value is shared.
func (c *coreChecker) markBelow(m coreMark, got, want TypeId) (ok, kept bool) {
	switch m {
	case markShared:
		return c.rel.Sub(got, want), false
	case markNew:
		return c.rel.Retype(got, want), true
	}
	if c.msub(m, got, want) {
		return true, true
	}
	// Retyped position by position to the union member of its kind, then
	// committed (ss_m, then ss_m_forget: the member is below the union).
	if mem, ok := c.unionMemberOfKind(got, want); ok && c.msub(m, got, mem) {
		return true, false
	}
	return c.rel.Sub(got, want), false
}

// unionMemberOfKind finds the member of want, a union (or an alias that is
// not recursive whose body is one), with the kind of got. Members have
// distinct kinds, so there is at most one.
func (c *coreChecker) unionMemberOfKind(got, want TypeId) (TypeId, bool) {
	want = c.plainAlias(want)
	wn := c.arena.Node(want)
	if wn.Kind != TKUnion {
		return 0, false
	}
	k, ok := c.rel.kindOf(c.plainAlias(got))
	if !ok {
		return 0, false
	}
	for _, mem := range c.arena.unionMembers[wn.Extra] {
		if c.rel.hasKind(k, mem) {
			return mem, true
		}
	}
	return 0, false
}

// msub is msub in formal-ver/Typing.v: Sub at a stored position, Retype
// at a new one, and for a partly new list or dict, its own type may change
// as a new value's does, with each position checked by its own mark. A
// label that holds a partly new value is not made open.
func (c *coreChecker) msub(m coreMark, got, want TypeId) bool {
	switch m {
	case markShared:
		return c.rel.Sub(got, want)
	case markNew:
		return c.rel.Retype(got, want)
	}
	p := &c.parts[m-2]
	ar := c.arena
	got, want = c.plainAlias(got), c.plainAlias(want)
	gn, wn := ar.Node(got), ar.Node(want)
	if p.list {
		return gn.Kind == TKList && wn.Kind == TKList && c.msub(p.elem, TypeId(gn.A), TypeId(wn.A))
	}
	if gn.Kind != TKRecord || wn.Kind != TKRecord {
		return false
	}
	x, y := ar.records[gn.Extra], ar.records[wn.Extra]
	for _, f := range x.Fields {
		if !c.fieldMsub(p.labelMark(f.Name), f, y.FieldAt(f.Name)) {
			return false
		}
	}
	for _, g := range y.Fields {
		if !c.fieldMsub(p.labelMark(g.Name), x.FieldAt(g.Name), g) {
			return false
		}
	}
	for _, l := range p.labels {
		if !c.fieldMsub(l.m, x.FieldAt(l.name), y.FieldAt(l.name)) {
			return false
		}
	}
	return c.fieldMsub(p.rest, x.Rest, y.Rest)
}

// fieldMsub is frsubR (msub m) in formal-ver/Subtyping.v, the per-label
// rule for retyping a new dict, with the value under the label retyped by
// its own mark.
func (c *coreChecker) fieldMsub(m coreMark, f, g RecordField) bool {
	if m >= 2 && g.Status == FieldOpen {
		return false
	}
	present := f.Status == FieldRequired || f.Status == FieldOptional || f.Status == FieldDeletable
	maybe := g.Status == FieldOptional || g.Status == FieldDeletable
	switch {
	case f.Status == FieldRequired && g.Status == FieldRequired:
		return c.msub(m, f.Type, g.Type)
	case f.Status == FieldAbsent && (maybe || g.Status == FieldAbsent):
		return true
	case present && maybe:
		return c.msub(m, f.Type, g.Type)
	case g.Status == FieldOpen:
		return true
	}
	return false
}

// storedBlocker is a stored value inside a partly new one that keeps it
// from being retyped: where it is ("`data`", "an element"), its type, and
// the type it would need.
type storedBlocker struct {
	where     string
	got, want TypeId
}

// storedBlockers finds the stored values inside a partly new value with
// mark m that keep it from being retyped from got to want: positions that
// would fit if they were new, but are stored, so only Sub applies there.
// The type needed is the union member of the value's kind when want is a
// union (a [str] needs the list member of UrlEncodable).
func (c *coreChecker) storedBlockers(m coreMark, got, want TypeId, path string) []storedBlocker {
	if m < 2 {
		return nil
	}
	p := &c.parts[m-2]
	ar := c.arena
	got, want = c.plainAlias(got), c.plainAlias(want)
	gn, wn := ar.Node(got), ar.Node(want)
	blocked := func(m coreMark, f, g RecordField, at string) []storedBlocker {
		switch {
		case m >= 2:
			if (f.Status == FieldRequired || f.Status == FieldOptional) && g.Status != FieldOpen && g.Status != FieldAbsent {
				return c.storedBlockers(m, f.Type, g.Type, at)
			}
		case m == markShared && !c.fieldMsub(markShared, f, g) && c.fieldMsub(markNew, f, g):
			need := g.Type
			if mem, ok := c.unionMemberOfKind(f.Type, g.Type); ok {
				need = mem
			}
			return []storedBlocker{{where: at, got: f.Type, want: need}}
		}
		return nil
	}
	var out []storedBlocker
	if p.list {
		if gn.Kind == TKList && wn.Kind == TKList {
			at := "an element"
			if path != "" {
				at = "an element in " + path
			}
			e := RecordField{Status: FieldRequired, Type: TypeId(gn.A)}
			w := RecordField{Status: FieldRequired, Type: TypeId(wn.A)}
			out = append(out, blocked(p.elem, e, w, at)...)
		}
		return out
	}
	if gn.Kind != TKRecord || wn.Kind != TKRecord {
		return nil
	}
	x, y := ar.records[gn.Extra], ar.records[wn.Extra]
	for _, f := range x.Fields {
		name := "`" + c.names.Name(f.Name) + "`"
		if path != "" {
			name = name + " in " + path
		}
		out = append(out, blocked(p.labelMark(f.Name), f, y.FieldAt(f.Name), name)...)
	}
	return out
}

// blockersHint says which stored values keep a partly new value from being
// retyped, and how to fix it.
func (c *coreChecker) blockersHint(bs []storedBlocker) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = b.where + " is a stored " + c.format(b.got) + ", which cannot become " + c.format(b.want)
	}
	fix := "give each that type where it is made, or deepCopy them here"
	if len(bs) == 1 {
		fix = "give it that type where it is made (`... as " + c.format(bs[0].want) + "`), or deepCopy it here"
	}
	return strings.Join(parts, ", and ") + ": a stored value keeps its type, so " + fix
}

// listPart is the mark of a list literal whose elements are not all new:
// a new list of stored values (tw_nil_m, tw_push_m). An element that is
// new, or partly new, is committed first.
func (c *coreChecker) listPart() uint16 {
	return c.newPart(corePart{list: true, elem: markShared})
}

// recordPart is the mark of a new dict whose label name holds a value with
// mark m, built from d (a new dict, or a partly new one) by a literal or a
// `set` (tw_setk_m).
func (c *coreChecker) recordPart(d coreSlot, name NameId, m coreMark) uint16 {
	p := corePart{rest: markNew}
	if d.part != 0 {
		old := c.parts[d.part-1]
		p.rest = old.rest
		p.labels = make([]coreLabelMark, 0, len(old.labels)+1)
		for _, l := range old.labels {
			if l.name != name {
				p.labels = append(p.labels, l)
			}
		}
	}
	p.labels = append(p.labels, coreLabelMark{name: name, m: m})
	return c.newPart(p)
}

// plainAlias unfolds an alias that is not recursive: such an alias is its
// body (HttpRequest is a record). A recursive alias is left as it is: the
// proof's msub never unfolds one.
func (c *coreChecker) plainAlias(t TypeId) TypeId {
	for {
		n := c.arena.Node(t)
		if n.Kind != TKAlias || c.aliasRecursive(n.A) {
			return t
		}
		t = c.arena.aliases[n.A].Body
	}
}

// aliasRecursive reports whether the alias at idx refers to itself,
// directly or through other aliases.
func (c *coreChecker) aliasRecursive(idx uint32) bool {
	seen := map[uint32]bool{}
	var walk func(t TypeId) bool
	walk = func(t TypeId) bool {
		if t == TidNothing {
			return false
		}
		ar := c.arena
		n := ar.nodes[t]
		switch n.Kind {
		case TKAlias:
			if n.A == idx {
				return true
			}
			if seen[n.A] {
				return false
			}
			seen[n.A] = true
			return walk(ar.aliases[n.A].Body)
		case TKList, TKCommand:
			return walk(TypeId(n.A))
		case TKRecord:
			rec := ar.records[n.Extra]
			for _, f := range rec.Fields {
				if walk(f.Type) {
					return true
				}
			}
			return walk(rec.Rest.Type)
		case TKUnion:
			for _, m := range ar.unionMembers[n.Extra] {
				if walk(m) {
					return true
				}
			}
		case TKQuote:
			sig := ar.quoteSigs[n.Extra]
			for _, x := range append(append([]TypeId(nil), sig.Inputs...), sig.Outputs...) {
				if walk(x) {
					return true
				}
			}
		case TKEnum:
			for _, x := range ar.enumArgs[n.Extra] {
				if walk(x) {
					return true
				}
			}
		case TKGrid, TKGridView, TKGridRow:
			return walk(TypeId(n.A))
		}
		return false
	}
	return walk(c.arena.aliases[idx].Body)
}
