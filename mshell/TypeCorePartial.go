package main

import (
	"slices"
	"strings"
)

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
	// A recursive alias is retyped to through its body, then committed:
	// ss_m to the body, ss_m_forget, and the body is below the alias. So
	// a new node holding stored children, `{name: @n, kids: @ks}`, is a
	// Person. msub itself never unfolds one, so a partly new value is
	// never kept at a recursive type.
	if wn := c.arena.Node(want); wn.Kind == TKAlias {
		body := c.unfold(want)
		if c.msub(m, got, body) {
			return true, false
		}
		if mem, ok := c.unionMemberOfKind(got, body); ok && c.msub(m, got, mem) {
			return true, false
		}
	}
	return c.rel.Sub(got, want), false
}

// unionMemberOfKind finds the member of want, a union, with the kind of
// got. Members have distinct kinds, so there is at most one.
func (c *coreChecker) unionMemberOfKind(got, want TypeId) (TypeId, bool) {
	wn := c.arena.Node(want)
	if wn.Kind != TKUnion {
		return 0, false
	}
	k, ok := c.rel.kindOf(got)
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

// Joining partly new arms (formal-ver/PartialJoin.v).
//
// `if {a: @xs} else {a: @xs, b: [2]} end` leaves a new dict holding a
// stored list in each arm. Relations.JoinSlot joins only shared or fresh
// slots, so without this the arms were committed and the join was a
// shared {a: [int]}, which no wider dict type is above. Here the arms get
// one mark (fmerge) and their types are joined position by position under
// it (mjoin), so the result is partly new too and may still gain optional
// labels. Each step mirrors the function of the same name in PartialJoin.v.

// partialJoin is pjoin in PartialJoin.v: the join of two solved slots when
// either is partly new and both can take one partly new mark.
func (c *coreChecker) partialJoin(a, b coreSlot) (coreSlot, bool) {
	if a.part == 0 && b.part == 0 {
		return coreSlot{}, false
	}
	if c.hasVars(a.t) || c.hasVars(b.t) {
		return coreSlot{}, false
	}
	at, bt := c.subst.Apply(c.arena, a.t), c.subst.Apply(c.arena, b.t)
	m, ok := c.fmerge(slotMark(a), slotMark(b), at, bt)
	if !ok || m < 2 {
		return coreSlot{}, false
	}
	t, ok := c.mjoin(m, at, bt)
	if !ok {
		return coreSlot{}, false
	}
	return coreSlot{t: t, part: uint16(m - 1)}, true
}

// asListMark is aslist: a new list is a partly new one whose elements are
// all new.
func (c *coreChecker) asListMark(m coreMark) (coreMark, bool) {
	switch {
	case m == markNew:
		return markNew, true
	case m >= 2 && c.parts[m-2].list:
		return c.parts[m-2].elem, true
	}
	return 0, false
}

// asRecMark is asrec: a new dict is a partly new one whose labels are all
// new.
func (c *coreChecker) asRecMark(m coreMark) (corePart, bool) {
	switch {
	case m == markNew:
		return corePart{rest: markNew}, true
	case m >= 2 && !c.parts[m-2].list:
		return c.parts[m-2], true
	}
	return corePart{}, false
}

// fieldTy is fty: the type of a value under a label with this status.
func fieldTy(f RecordField) TypeId {
	switch f.Status {
	case FieldAbsent:
		return TidBottom
	case FieldOpen:
		return TidUnknown
	}
	return f.Type
}

// partKeys is the labels a join of two dicts looks at: those the marks
// list and those the types list, each once.
func partKeys(lists ...[]NameId) []NameId {
	var out []NameId
	for _, l := range lists {
		for _, k := range l {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	return out
}

func labelNames(ls []coreLabelMark) []NameId {
	out := make([]NameId, len(ls))
	for i, l := range ls {
		out[i] = l.name
	}
	return out
}

func partFieldNames(fs []RecordField) []NameId {
	out := make([]NameId, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

// fmerge gives two arms with marks m1 and m2 and types a and b one mark:
// the arm's own where only one holds a value, the merge of both where each
// does. A position stored in one arm and new in the other has none.
func (c *coreChecker) fmerge(m1, m2 coreMark, a, b TypeId) (coreMark, bool) {
	if m1 == m2 && m1 < 2 {
		return m1, true
	}
	ar := c.arena
	an, bn := ar.Node(a), ar.Node(b)
	switch {
	case an.Kind == TKList && bn.Kind == TKList:
		e1, ok1 := c.asListMark(m1)
		e2, ok2 := c.asListMark(m2)
		if !ok1 || !ok2 {
			return 0, false
		}
		e, ok := c.fmerge(e1, e2, TypeId(an.A), TypeId(bn.A))
		if !ok {
			return 0, false
		}
		return c.partMark(corePart{list: true, elem: e})
	case an.Kind == TKRecord && bn.Kind == TKRecord:
		p1, ok1 := c.asRecMark(m1)
		p2, ok2 := c.asRecMark(m2)
		if !ok1 || !ok2 {
			return 0, false
		}
		x, y := ar.records[an.Extra], ar.records[bn.Extra]
		keys := partKeys(labelNames(p1.labels), labelNames(p2.labels), partFieldNames(x.Fields), partFieldNames(y.Fields))
		out := corePart{labels: make([]coreLabelMark, 0, len(keys))}
		for _, k := range keys {
			m, ok := c.fmat(x.FieldAt(k), y.FieldAt(k), p1.labelMark(k), p2.labelMark(k))
			if !ok {
				return 0, false
			}
			out.labels = append(out.labels, coreLabelMark{name: k, m: m})
		}
		rest, ok := c.fmat(x.Rest, y.Rest, p1.rest, p2.rest)
		if !ok {
			return 0, false
		}
		out.rest = rest
		return c.partMark(out)
	}
	return 0, false
}

// fmat is the merged mark of one label.
func (c *coreChecker) fmat(f1, f2 RecordField, m1, m2 coreMark) (coreMark, bool) {
	switch {
	case f1.Status == FieldAbsent:
		return m2, true
	case f2.Status == FieldAbsent:
		return m1, true
	}
	return c.fmerge(m1, m2, fieldTy(f1), fieldTy(f2))
}

// partMark records p and returns its mark, or fails when there are too
// many.
func (c *coreChecker) partMark(p corePart) (coreMark, bool) {
	part := c.newPart(p)
	if part == 0 {
		return 0, false
	}
	return coreMark(part) + 1, true
}

// mjoin joins two types under one mark: Relations.JoinSlot at a stored or
// new position, and position by position inside a partly new one, so
// each type is below the result by msub.
func (c *coreChecker) mjoin(m coreMark, a, b TypeId) (TypeId, bool) {
	if m < 2 {
		fr := m == markNew
		s, ok := c.rel.JoinSlot(Slot{Type: a, Fresh: fr}, Slot{Type: b, Fresh: fr})
		return s.Type, ok
	}
	p := c.parts[m-2]
	ar := c.arena
	an, bn := ar.Node(a), ar.Node(b)
	if p.list {
		if an.Kind != TKList || bn.Kind != TKList {
			return 0, false
		}
		e, ok := c.mjoin(p.elem, TypeId(an.A), TypeId(bn.A))
		if !ok {
			return 0, false
		}
		return ar.MakeList(e), true
	}
	if an.Kind != TKRecord || bn.Kind != TKRecord {
		return 0, false
	}
	x, y := ar.records[an.Extra], ar.records[bn.Extra]
	rest, ok := c.mfield(p.rest, x.Rest, y.Rest)
	if !ok {
		return 0, false
	}
	keys := partKeys(labelNames(p.labels), partFieldNames(x.Fields), partFieldNames(y.Fields))
	fields := make([]RecordField, 0, len(keys))
	for _, k := range keys {
		f, ok := c.mfield(p.labelMark(k), x.FieldAt(k), y.FieldAt(k))
		if !ok {
			return 0, false
		}
		f.Name = k
		fields = append(fields, f)
	}
	return ar.MakeRecord(fields, rest), true
}

// mfield joins one label under its mark: as Relations.fieldJoin, but a
// label present in one arm only is joined with itself (a partly new value
// is not always msub of itself), and a label holding a partly new value is
// not made open.
func (c *coreChecker) mfield(m coreMark, f, g RecordField) (RecordField, bool) {
	j := func(status FieldStatus, x, y TypeId) (RecordField, bool) {
		t, ok := c.mjoin(m, x, y)
		return RecordField{Status: status, Type: t}, ok
	}
	var h RecordField
	ok := true
	present := func(s FieldStatus) bool {
		return s == FieldRequired || s == FieldOptional
	}
	switch {
	case f.Status == FieldOpen || g.Status == FieldOpen:
		h = RecordField{Status: FieldOpen}
	case f.Status == FieldAbsent && g.Status == FieldAbsent:
		h = RecordField{Status: FieldAbsent}
	case f.Status == FieldRequired && g.Status == FieldRequired:
		h, ok = j(FieldRequired, f.Type, g.Type)
	case f.Status == FieldDeletable && g.Status == FieldDeletable:
		h, ok = j(FieldDeletable, f.Type, g.Type)
	case f.Status == FieldAbsent && g.Status == FieldDeletable:
		h, ok = j(FieldDeletable, g.Type, g.Type)
	case f.Status == FieldDeletable && g.Status == FieldAbsent:
		h, ok = j(FieldDeletable, f.Type, f.Type)
	case f.Status == FieldAbsent && present(g.Status):
		h, ok = j(FieldOptional, g.Type, g.Type)
	case present(f.Status) && g.Status == FieldAbsent:
		h, ok = j(FieldOptional, f.Type, f.Type)
	default:
		h, ok = j(FieldOptional, f.Type, g.Type)
	}
	if !ok || (m >= 2 && h.Status == FieldOpen) {
		return RecordField{}, false
	}
	return h, true
}
