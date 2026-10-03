package main

// Subtyping, fresh retyping and branch joins for the checker described in
// ai/type-core-calculus.typ. Each is a port of a function proved in
// formal-ver/, and follows it case by case, in the same order:
//
//	Sub       subq in Decide.v: a <= b, subtyping
//	Retype    rsubq in Decide.v: a fresh (unshared) value of type a may be
//	          given type b
//	JoinSlot  join_slot in Join.v, given Sub and Retype (le_alg in Decide.v)
//
// The proofs say a yes from Sub or Retype is right (subq_sound,
// rsubq_sound), and that a join is above both arms (join_slot_ub_alg). A no
// is always safe. formal-ver/oracle/ is the proved functions extracted to a
// program; the tests compare these ports with it.
//
// Recursive aliases make types infinite trees. Sub and Retype decide them
// with a set of assumed pairs, and three rules from the proof matter:
//
//   - The set is looked up and extended only at the children of a type
//     constructor: a list's element, a field, a quote's inputs and outputs,
//     an enum argument (child). Union and unfolding steps (level) never
//     look at it. Looking at it on every step accepts `str <= V` for
//     `type V = int | V` (H13).
//   - The set is threaded through the whole query, and put back as it was
//     when a union alternative fails.
//   - Each relation has its own set. Where Retype needs <= (a quote, an enum
//     argument that is not fresh-covariant) it starts a new Sub query.
//     Sharing one set accepts H12.
//
// After a query says yes, every pair in its final set holds and is kept, so
// later queries answer it at once (subq_set_sound). After a no, nothing is
// kept: a pair can be accepted under an assumption the query then refutes
// (cache_early).
//
// A query also has a limit on its work. Guarded aliases make every query
// finish well within it; reaching it answers no, which is safe.

import "slices"

type typePair struct{ a, b TypeId }

// assumedPairs is the set of pairs assumed during one query: the tail of
// the Relations' shared buffer from start. A query that starts inside
// another (Retype asks Sub) takes the buffer above it and gives it back
// when it ends, so each query sees only its own pairs and nothing is
// allocated per query. Small sets are searched linearly; past
// assumedLinear pairs a map is kept as well.
type assumedPairs struct {
	r     *Relations
	start int
	has   map[typePair]struct{}
}

const assumedLinear = 64

func (r *Relations) newAssumedPairs() assumedPairs {
	return assumedPairs{r: r, start: len(r.pairBuf)}
}

// pairs is the set's contents.
func (s *assumedPairs) pairs() []typePair { return s.r.pairBuf[s.start:] }

func (s *assumedPairs) contains(p typePair) bool {
	if s.has != nil {
		_, ok := s.has[p]
		return ok
	}
	for _, q := range s.pairs() {
		if q == p {
			return true
		}
	}
	return false
}

func (s *assumedPairs) add(p typePair) {
	s.r.pairBuf = append(s.r.pairBuf, p)
	if s.has != nil {
		s.has[p] = struct{}{}
	} else if len(s.r.pairBuf)-s.start > assumedLinear {
		s.has = make(map[typePair]struct{}, 2*assumedLinear)
		for _, q := range s.pairs() {
			s.has[q] = struct{}{}
		}
	}
}

// mark and rollback put the set back as it was, for a failed union
// alternative.
func (s *assumedPairs) mark() int { return len(s.r.pairBuf) }

func (s *assumedPairs) rollback(m int) {
	if s.has != nil {
		for _, p := range s.r.pairBuf[m:] {
			delete(s.has, p)
		}
	}
	s.r.pairBuf = s.r.pairBuf[:m]
}

// release gives the set's part of the buffer back.
func (s *assumedPairs) release() { s.r.pairBuf = s.r.pairBuf[:s.start] }

// relationWorkLimit bounds the steps of one top-level query.
const relationWorkLimit = 1 << 20

// Relations answers Sub, Retype and JoinSlot over one arena, and keeps the
// pairs its earlier queries proved.
type Relations struct {
	arena       *TypeArena
	subKnown    map[typePair]struct{}
	retypeKnown map[typePair]struct{}
	work        int
	active      int
	// pairBuf holds the assumption sets of the queries in progress.
	pairBuf []typePair
}

func NewRelations(arena *TypeArena) *Relations {
	return &Relations{
		arena:       arena,
		subKnown:    make(map[typePair]struct{}),
		retypeKnown: make(map[typePair]struct{}),
	}
}

// reset forgets every answer, for a new check whose arena reuses ids.
func (r *Relations) reset() {
	clear(r.subKnown)
	clear(r.retypeKnown)
	r.work, r.active = 0, 0
	r.pairBuf = r.pairBuf[:0]
}

// begin and end bracket every exported query, so the work limit counts a
// whole top-level question, including the queries it starts.
func (r *Relations) begin() {
	if r.active == 0 {
		r.work = relationWorkLimit
	}
	r.active++
}

func (r *Relations) end() { r.active-- }

func (r *Relations) spend() bool {
	r.work--
	return r.work >= 0
}

// ---------------------------------------------------------------------------
// Sub: subtyping

// Sub reports whether a <= b.
func (r *Relations) Sub(a, b TypeId) bool {
	r.begin()
	defer r.end()
	if a == b {
		return true
	}
	q := subQuery{r: r, set: r.newAssumedPairs()}
	defer q.set.release()
	if !q.child(a, b) {
		return false
	}
	for _, p := range q.set.pairs() {
		r.subKnown[p] = struct{}{}
	}
	return true
}

// Equal reports whether a and b are the same type: each below the other.
func (r *Relations) Equal(a, b TypeId) bool {
	return a == b || (r.Sub(a, b) && r.Sub(b, a))
}

type subQuery struct {
	r   *Relations
	set assumedPairs
}

// child compares the children of a type constructor: chk in Decide.v.
func (q *subQuery) child(x, y TypeId) bool {
	p := typePair{x, y}
	if q.set.contains(p) {
		return true
	}
	if _, ok := q.r.subKnown[p]; ok {
		return true
	}
	q.set.add(p)
	return q.level(x, y)
}

// both compares an invariant position.
func (q *subQuery) both(x, y TypeId) bool {
	return q.child(x, y) && q.child(y, x)
}

// level compares one level: lvl and step in Decide.v.
func (q *subQuery) level(a, b TypeId) bool {
	if !q.r.spend() {
		return false
	}
	if a == b {
		return true
	}
	ar := q.r.arena
	if a == TidNothing || b == TidNothing {
		return false
	}
	if a == TidBottom || b == TidUnknown {
		return true
	}
	an, bn := ar.Node(a), ar.Node(b)
	if an.Kind == TKUnion {
		for _, m := range ar.unionMembers[an.Extra] {
			if !q.level(m, b) {
				return false
			}
		}
		return true
	}
	if an.Kind == TKAlias {
		return q.level(ar.aliases[an.A].Body, b)
	}
	if bn.Kind == TKAlias {
		return q.level(a, ar.aliases[bn.A].Body)
	}
	if bn.Kind == TKUnion {
		for _, m := range ar.unionMembers[bn.Extra] {
			mk := q.set.mark()
			if q.level(a, m) {
				return true
			}
			q.set.rollback(mk)
		}
		return false
	}
	if bn.Kind == TKCommand && (an.Kind == TKList || an.Kind == TKCommand) {
		x, xo, xe := commandOf(ar, a)
		y, yo, ye := commandOf(ar, b)
		return commandStateBelow(xo, yo) && commandStateBelow(xe, ye) && q.both(x, y)
	}
	if an.Kind != bn.Kind {
		return false
	}
	switch an.Kind {
	case TKList:
		return q.both(TypeId(an.A), TypeId(bn.A))
	case TKRecord:
		return recordLabels(ar.records[an.Extra], ar.records[bn.Extra], q.fieldView)
	case TKQuote:
		return quoteLevel(ar.quoteSigs[an.Extra], ar.quoteSigs[bn.Extra], q.child)
	case TKEnum:
		if an.A != bn.A {
			return false
		}
		return q.enumArgs(ar.enumDecls[an.A].Params, ar.enumArgs[an.Extra], ar.enumArgs[bn.Extra])
	case TKGrid, TKGridView, TKGridRow:
		// Columns are labels: a grid's schema is compared as a record.
		return an.A != 0 && bn.A != 0 && q.child(TypeId(an.A), TypeId(bn.A))
	}
	return false
}

// fieldView reports whether a view whose status at a label is g is safe on
// an object whose own status there is f: tfchk in Decide.v. Every writable
// label needs the two types equal.
func (q *subQuery) fieldView(f, g RecordField) bool {
	switch {
	case f.Status == FieldRequired && g.Status == FieldRequired,
		f.Status == FieldRequired && g.Status == FieldOptional,
		f.Status == FieldOptional && g.Status == FieldOptional,
		f.Status == FieldDeletable && g.Status == FieldOptional,
		f.Status == FieldDeletable && g.Status == FieldDeletable:
		return q.both(f.Type, g.Type)
	case f.Status == FieldAbsent && g.Status == FieldAbsent:
		return true
	case g.Status == FieldOpen:
		return true
	}
	return false
}

// enumArgs compares enum arguments by each parameter's variance: tvchk.
func (q *subQuery) enumArgs(params []EnumParam, xs, ys []TypeId) bool {
	if len(xs) != len(params) || len(ys) != len(params) {
		return false
	}
	for i, p := range params {
		var ok bool
		switch p.Variance {
		case VarCo:
			ok = q.child(xs[i], ys[i])
		case VarContra:
			ok = q.child(ys[i], xs[i])
		default:
			ok = q.both(xs[i], ys[i])
		}
		if !ok {
			return false
		}
	}
	return true
}

// recordLabels checks every label of two dict-kinded types: each label
// either side declares, then the two remainders, which stand for every
// other label (trec in Decide.v).
func recordLabels(x, y RecordType, fc func(f, g RecordField) bool) bool {
	for _, f := range x.Fields {
		if !fc(x.FieldAt(f.Name), y.FieldAt(f.Name)) {
			return false
		}
	}
	for _, g := range y.Fields {
		if !fc(x.FieldAt(g.Name), y.FieldAt(g.Name)) {
			return false
		}
	}
	return fc(x.Rest, y.Rest)
}

// quoteLevel compares two quote types: inputs contravariant, outputs
// covariant, a `never` quote below every quote with the same inputs. The
// proof writes stacks top first, so they are compared from the top.
func quoteLevel(x, y QuoteSig, c func(a, b TypeId) bool) bool {
	if len(x.Inputs) != len(y.Inputs) {
		return false
	}
	for i := len(x.Inputs) - 1; i >= 0; i-- {
		if !c(y.Inputs[i], x.Inputs[i]) {
			return false
		}
	}
	if x.Diverges {
		return true
	}
	if y.Diverges || len(x.Outputs) != len(y.Outputs) {
		return false
	}
	for i := len(x.Outputs) - 1; i >= 0; i-- {
		if !c(x.Outputs[i], y.Outputs[i]) {
			return false
		}
	}
	return true
}

// commandOf reads a list or command type as its argument list and stream
// states; a plain list's streams have no destination.
func commandOf(ar *TypeArena, t TypeId) (argv TypeId, out, errs CommandCaptureMode) {
	n := ar.nodes[t]
	if n.Kind == TKCommand {
		return TypeId(n.A), CommandCaptureMode(n.B), CommandCaptureMode(n.Extra)
	}
	return t, CommandCaptureNone, CommandCaptureNone
}

// commandStateBelow orders stream states: each is below itself and below
// the varied state.
func commandStateBelow(x, y CommandCaptureMode) bool {
	return x == y || (y&^CommandPipe == CommandDestVaried && x&CommandPipe == y&CommandPipe)
}

// ---------------------------------------------------------------------------
// Retype: fresh retyping

// Retype reports whether a fresh value of type a may be given type b.
func (r *Relations) Retype(a, b TypeId) bool {
	r.begin()
	defer r.end()
	if a == b {
		return true
	}
	q := retypeQuery{r: r, set: r.newAssumedPairs()}
	defer q.set.release()
	if !q.child(a, b) {
		return false
	}
	for _, p := range q.set.pairs() {
		r.retypeKnown[p] = struct{}{}
	}
	return true
}

type retypeQuery struct {
	r   *Relations
	set assumedPairs
}

// child is rchk in Decide.v.
func (q *retypeQuery) child(x, y TypeId) bool {
	p := typePair{x, y}
	if q.set.contains(p) {
		return true
	}
	if _, ok := q.r.retypeKnown[p]; ok {
		return true
	}
	q.set.add(p)
	return q.level(x, y)
}

// level is rlvl and rstep in Decide.v. Anything below is also a retype; that
// question is a new Sub query, which never sees this query's set.
func (q *retypeQuery) level(a, b TypeId) bool {
	if !q.r.spend() {
		return false
	}
	if q.r.Sub(a, b) {
		return true
	}
	ar := q.r.arena
	if a == TidNothing || b == TidNothing {
		return false
	}
	an, bn := ar.Node(a), ar.Node(b)
	if an.Kind == TKUnion {
		for _, m := range ar.unionMembers[an.Extra] {
			if !q.level(m, b) {
				return false
			}
		}
		return true
	}
	if an.Kind == TKAlias {
		return q.level(ar.aliases[an.A].Body, b)
	}
	if bn.Kind == TKAlias {
		return q.level(a, ar.aliases[bn.A].Body)
	}
	if bn.Kind == TKUnion {
		for _, m := range ar.unionMembers[bn.Extra] {
			mk := q.set.mark()
			if q.level(a, m) {
				return true
			}
			q.set.rollback(mk)
		}
		return false
	}
	if an.Kind != bn.Kind {
		return false
	}
	switch an.Kind {
	case TKList:
		return q.child(TypeId(an.A), TypeId(bn.A))
	case TKRecord:
		return recordLabels(ar.records[an.Extra], ar.records[bn.Extra], q.fieldRetype)
	case TKEnum:
		if an.A != bn.A {
			return false
		}
		return q.enumArgs(ar.enumDecls[an.A].Params, ar.enumArgs[an.Extra], ar.enumArgs[bn.Extra])
	case TKGrid, TKGridView, TKGridRow:
		return an.A != 0 && bn.A != 0 && q.child(TypeId(an.A), TypeId(bn.A))
	}
	return false
}

// fieldRetype is frchk in Decide.v: a required label stays required;
// required, optional or deletable may become optional or deletable; an
// absent label may become optional or deletable; anything may become
// unknown. The types inside are retyped too.
func (q *retypeQuery) fieldRetype(f, g RecordField) bool {
	present := f.Status == FieldRequired || f.Status == FieldOptional || f.Status == FieldDeletable
	maybe := g.Status == FieldOptional || g.Status == FieldDeletable
	switch {
	case f.Status == FieldRequired && g.Status == FieldRequired:
		return q.child(f.Type, g.Type)
	case f.Status == FieldAbsent && (maybe || g.Status == FieldAbsent):
		return true
	case present && maybe:
		return q.child(f.Type, g.Type)
	case g.Status == FieldOpen:
		return true
	}
	return false
}

// enumArgs is tvrchk in Decide.v: a fresh-covariant argument is retyped;
// any other changes only as its variance allows, decided by Sub.
func (q *retypeQuery) enumArgs(params []EnumParam, xs, ys []TypeId) bool {
	if len(xs) != len(params) || len(ys) != len(params) {
		return false
	}
	for i, p := range params {
		var ok bool
		switch {
		case p.Fresh:
			ok = q.child(xs[i], ys[i])
		case p.Variance == VarCo:
			ok = q.r.Sub(xs[i], ys[i])
		case p.Variance == VarContra:
			ok = q.r.Sub(ys[i], xs[i])
		default:
			ok = q.r.Sub(xs[i], ys[i]) && q.r.Sub(ys[i], xs[i])
		}
		if !ok {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Kinds

// valueKind is a runtime kind. Each base type is its own kind (code is its
// TypeId); each enum declaration is one kind, shared by all its instances.
type valueKind struct {
	code uint32
	enum uint32
}

const (
	kindList uint32 = 1000 + iota
	kindDict
	kindQuote
	kindEnum
	kindGrid
	kindGridView
	kindGridRow
	kindPipe
)

// kindOf is the runtime kind of a type that has exactly one: kind_of_ty in
// Typing.v. Bottom, unknown, unions, aliases and type variables have none.
func (r *Relations) kindOf(t TypeId) (valueKind, bool) {
	switch t {
	case TidBool, TidInt, TidFloat, TidStr, TidBytes, TidPath, TidDateTime, TidNull:
		return valueKind{code: uint32(t)}, true
	}
	n := r.arena.Node(t)
	switch n.Kind {
	case TKList:
		return valueKind{code: kindList}, true
	case TKCommand:
		// A pipe is its own runtime object, which no pattern matches; a
		// command is a list with redirects.
		if CommandCaptureMode(n.B)&CommandPipe != 0 {
			return valueKind{code: kindPipe}, true
		}
		return valueKind{code: kindList}, true
	case TKRecord:
		return valueKind{code: kindDict}, true
	case TKQuote:
		return valueKind{code: kindQuote}, true
	case TKEnum:
		return valueKind{code: kindEnum, enum: n.A}, true
	case TKGrid:
		return valueKind{code: kindGrid}, true
	case TKGridView:
		return valueKind{code: kindGridView}, true
	case TKGridRow:
		return valueKind{code: kindGridRow}, true
	}
	return valueKind{}, false
}

// Kinds returns the kinds of the members of t, looking through unions and
// aliases (akinds in Join.v). It fails when a member has no kind, or when an
// alias is its own member, which only an unguarded alias can be.
func (r *Relations) Kinds(t TypeId) ([]valueKind, bool) {
	return r.kinds(t, nil)
}

func (r *Relations) kinds(t TypeId, visiting []TypeId) ([]valueKind, bool) {
	if t == TidBottom {
		return nil, true
	}
	if t == TidNothing {
		return nil, false
	}
	n := r.arena.Node(t)
	switch n.Kind {
	case TKUnion:
		var out []valueKind
		for _, m := range r.arena.unionMembers[n.Extra] {
			ks, ok := r.kinds(m, visiting)
			if !ok {
				return nil, false
			}
			out = append(out, ks...)
		}
		return out, true
	case TKAlias:
		if slices.Contains(visiting, t) {
			return nil, false
		}
		return r.kinds(r.arena.aliases[n.A].Body, append(visiting, t))
	}
	k, ok := r.kindOf(t)
	if !ok {
		return nil, false
	}
	return []valueKind{k}, true
}

// hasKind reports whether some member of t has kind k: ukind in Join.v.
func (r *Relations) hasKind(k valueKind, t TypeId) bool {
	n := r.arena.Node(t)
	switch n.Kind {
	case TKUnion:
		for _, m := range r.arena.unionMembers[n.Extra] {
			if r.hasKind(k, m) {
				return true
			}
		}
		return false
	case TKAlias:
		ks, ok := r.Kinds(t)
		return ok && slices.Contains(ks, k)
	}
	k2, ok := r.kindOf(t)
	return ok && k2 == k
}

func kindsDisjoint(xs, ys []valueKind) bool {
	for _, x := range xs {
		if slices.Contains(ys, x) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Joins

// Slot is a stack slot: a type, and whether the value is fresh (nothing
// else references it).
type Slot struct {
	Type  TypeId
	Fresh bool
}

// JoinSlot is the type of a slot after an if or match whose arms leave p
// and q there: join_slot in Join.v. The result is fresh only when both arms
// are. It fails when the arms have no join the design allows.
func (r *Relations) JoinSlot(p, q Slot) (Slot, bool) {
	r.begin()
	defer r.end()
	fr := p.Fresh && q.Fresh
	t, ok := r.join(fr, p.Type, q.Type)
	return Slot{Type: t, Fresh: fr}, ok
}

// below is le_alg in Decide.v: <= for shared slots, the fresh retype when
// both slots are fresh.
func (r *Relations) below(fr bool, a, b TypeId) bool {
	if fr {
		return r.Retype(a, b)
	}
	return r.Sub(a, b)
}

// join is tjoin in Join.v. Where the types cannot be widened inside, the
// join is the other side when one side is below the other.
func (r *Relations) join(fr bool, a, b TypeId) (TypeId, bool) {
	if a == b {
		return a, true
	}
	ar := r.arena
	if a == TidNothing || b == TidNothing {
		return TidNothing, false
	}
	if ar.Node(a).Kind == TKAlias || ar.Node(b).Kind == TKAlias {
		return r.aliasJoin(fr, a, b)
	}
	if z, ok := r.joinCore(fr, a, b); ok {
		return z, true
	}
	if r.below(fr, a, b) {
		return b, true
	}
	if r.below(fr, b, a) {
		return a, true
	}
	return TidNothing, false
}

// joinCore is tjoin_core in Join.v: the joins that look inside the two
// types. It fails where they cannot be widened inside.
func (r *Relations) joinCore(fr bool, a, b TypeId) (TypeId, bool) {
	ar := r.arena
	an, bn := ar.Node(a), ar.Node(b)
	if a == TidBottom {
		return b, true
	}
	if b == TidBottom {
		return a, true
	}
	if an.Kind == TKUnion && bn.Kind == TKUnion {
		return TidNothing, false
	}
	if an.Kind == TKUnion {
		return r.joinIntoUnion(fr, a, b, true)
	}
	if bn.Kind == TKUnion {
		return r.joinIntoUnion(fr, b, a, false)
	}
	if (an.Kind == TKCommand || bn.Kind == TKCommand) &&
		(an.Kind == TKList || an.Kind == TKCommand) && (bn.Kind == TKList || bn.Kind == TKCommand) {
		// Two commands over the same arguments join stream by stream.
		x, xo, xe := commandOf(ar, a)
		y, yo, ye := commandOf(ar, b)
		// A pipe and a list are different runtime objects.
		pipe := xo & CommandPipe
		if x != y || pipe != yo&CommandPipe {
			return TidNothing, false
		}
		if xo != yo {
			xo = CommandDestVaried | pipe
		}
		if xe != ye {
			xe = CommandDestVaried
		}
		return ar.MakeCommand(x, xo, xe), true
	}
	switch {
	case an.Kind == TKList && bn.Kind == TKList:
		if !fr {
			return TidNothing, false
		}
		z, ok := r.join(true, TypeId(an.A), TypeId(bn.A))
		if !ok {
			return TidNothing, false
		}
		return ar.MakeList(z), true
	case an.Kind == TKRecord && bn.Kind == TKRecord:
		if !fr {
			return TidNothing, false
		}
		return r.recordJoin(ar.records[an.Extra], ar.records[bn.Extra])
	case an.Kind == bn.Kind && (an.Kind == TKGrid || an.Kind == TKGridView || an.Kind == TKGridRow):
		// Two new grids widen column by column, as two new records do.
		if !fr || an.A == 0 || bn.A == 0 {
			return TidNothing, false
		}
		x, y := ar.Node(TypeId(an.A)), ar.Node(TypeId(bn.A))
		z, ok := r.recordJoin(ar.records[x.Extra], ar.records[y.Extra])
		if !ok {
			return TidNothing, false
		}
		return ar.MakeGridOf(an.Kind, z), true
	case an.Kind == TKEnum && bn.Kind == TKEnum:
		if an.A != bn.A {
			// Two different enums are two kinds.
			return ar.MakeUnion([]TypeId{a, b}), true
		}
		args, ok := r.enumJoin(fr, ar.enumDecls[an.A].Params, ar.enumArgs[an.Extra], ar.enumArgs[bn.Extra])
		if !ok {
			return TidNothing, false
		}
		return ar.MakeEnum(an.A, args), true
	}
	ka, okA := r.kindOf(a)
	kb, okB := r.kindOf(b)
	if !okA || !okB || ka == kb {
		return TidNothing, false
	}
	return ar.MakeUnion([]TypeId{a, b}), true
}

// joinIntoUnion joins t into the union u: the member of t's kind is joined
// with t, or t is added when u has no member of that kind. unionFirst keeps
// the order of the arguments for the member join.
func (r *Relations) joinIntoUnion(fr bool, u, t TypeId, unionFirst bool) (TypeId, bool) {
	k, ok := r.kindOf(t)
	if !ok {
		return TidNothing, false
	}
	members := slices.Clone(r.arena.unionMembers[r.arena.Node(u).Extra])
	for i, m := range members {
		if !r.hasKind(k, m) {
			continue
		}
		var z TypeId
		if unionFirst {
			z, ok = r.join(fr, m, t)
		} else {
			z, ok = r.join(fr, t, m)
		}
		if !ok {
			return TidNothing, false
		}
		members[i] = z
		return r.arena.MakeUnion(members), true
	}
	return r.arena.MakeUnion(append(members, t)), true
}

// aliasJoin is ajoin in Join.v: an alias is never widened inside. The join
// is the other side when one side is below the other, the union when their
// kinds do not overlap, and otherwise there is none.
func (r *Relations) aliasJoin(fr bool, a, b TypeId) (TypeId, bool) {
	if a == TidBottom {
		return b, true
	}
	if b == TidBottom {
		return a, true
	}
	if r.below(fr, a, b) {
		return b, true
	}
	if r.below(fr, b, a) {
		return a, true
	}
	ka, okA := r.Kinds(a)
	kb, okB := r.Kinds(b)
	if okA && okB && kindsDisjoint(ka, kb) {
		return r.arena.MakeUnion([]TypeId{a, b}), true
	}
	return TidNothing, false
}

// recordJoin joins two fresh dict-kinded types label by label: rjoin.
func (r *Relations) recordJoin(x, y RecordType) (TypeId, bool) {
	var fields []RecordField
	seen := make(map[NameId]bool)
	for _, fs := range [][]RecordField{x.Fields, y.Fields} {
		for _, f := range fs {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			j, ok := r.fieldJoin(x.FieldAt(f.Name), y.FieldAt(f.Name))
			if !ok {
				return TidNothing, false
			}
			j.Name = f.Name
			fields = append(fields, j)
		}
	}
	rest, ok := r.fieldJoin(x.Rest, y.Rest)
	if !ok {
		return TidNothing, false
	}
	return r.arena.MakeRecord(fields, rest), true
}

// fieldJoin joins one label of two fresh records: fjoin in Join.v. Present
// in both stays present; otherwise it becomes optional; deletable stays
// deletable; unknown wins.
func (r *Relations) fieldJoin(f, g RecordField) (RecordField, bool) {
	switch {
	case f.Status == FieldOpen || g.Status == FieldOpen:
		return RecordField{Status: FieldOpen}, true
	case f.Status == FieldAbsent && g.Status == FieldAbsent:
		return RecordField{Status: FieldAbsent}, true
	case f.Status == FieldRequired && g.Status == FieldRequired:
		return r.fieldJoinType(FieldRequired, f.Type, g.Type)
	case f.Status == FieldDeletable && g.Status == FieldDeletable:
		return r.fieldJoinType(FieldDeletable, f.Type, g.Type)
	case f.Status == FieldAbsent && g.Status == FieldDeletable:
		return g, true
	case f.Status == FieldDeletable && g.Status == FieldAbsent:
		return f, true
	case f.Status == FieldAbsent:
		return RecordField{Status: FieldOptional, Type: g.Type}, true
	case g.Status == FieldAbsent:
		return RecordField{Status: FieldOptional, Type: f.Type}, true
	}
	return r.fieldJoinType(FieldOptional, f.Type, g.Type)
}

func (r *Relations) fieldJoinType(status FieldStatus, x, y TypeId) (RecordField, bool) {
	z, ok := r.join(true, x, y)
	return RecordField{Status: status, Type: z}, ok
}

// enumJoin joins enum arguments: ejoin in Join.v. A fresh-covariant
// argument of fresh values widens; a covariant one joins as shared values;
// any other must be equal.
func (r *Relations) enumJoin(fr bool, params []EnumParam, xs, ys []TypeId) ([]TypeId, bool) {
	if len(xs) != len(params) || len(ys) != len(params) {
		return nil, false
	}
	out := make([]TypeId, len(params))
	for i, p := range params {
		var z TypeId
		ok := true
		switch {
		case fr && p.Fresh:
			z, ok = r.join(true, xs[i], ys[i])
		case p.Variance == VarCo:
			z, ok = r.join(false, xs[i], ys[i])
		case xs[i] == ys[i]:
			z = xs[i]
		default:
			ok = false
		}
		if !ok {
			return nil, false
		}
		out[i] = z
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Immutable and checkable

// Immutable reports whether no value of type t holds a list, dict or grid:
// immutable in Subtyping.v. Such a value is fresh wherever it is, since
// nothing inside it can be written. A type variable, an abstract type and
// unknown are not immutable: an instance may be a list. Through an alias
// it is the greatest fixed point: an alias met again counts as immutable.
func (r *Relations) Immutable(t TypeId) bool {
	return r.immutable(t, nil)
}

func (r *Relations) immutable(t TypeId, visiting []TypeId) bool {
	switch t {
	case TidBool, TidInt, TidFloat, TidStr, TidBytes, TidPath, TidDateTime, TidNull, TidBottom:
		return true
	case TidUnknown, TidNothing:
		return false
	}
	ar := r.arena
	n := ar.Node(t)
	switch n.Kind {
	case TKParam, TKQuote:
		return true
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			if !r.immutable(m, visiting) {
				return false
			}
		}
		return true
	case TKEnum:
		if !ar.enumDecls[n.A].Immutable {
			return false
		}
		for _, x := range ar.enumArgs[n.Extra] {
			if !r.immutable(x, visiting) {
				return false
			}
		}
		return true
	case TKAlias:
		if slices.Contains(visiting, t) {
			return true
		}
		return r.immutable(ar.aliases[n.A].Body, append(visiting, t))
	}
	return false
}

// Checkable reports whether t can be a tryAs or `is` target: chk in
// Checkable.v. The validator cannot look inside a quote, and types are
// erased, so a checkable type mentions no quote, type variable or abstract
// type, and every enum it mentions is checkable. A grid is checkable when
// its schema is known and its column types are.
func (r *Relations) Checkable(t TypeId) bool {
	return r.checkable(t, false, nil)
}

// checkable takes params, whether an enum parameter may appear (in payload
// types, where it is substituted away before validation).
func (r *Relations) checkable(t TypeId, params bool, visiting []TypeId) bool {
	switch t {
	case TidBool, TidInt, TidFloat, TidStr, TidBytes, TidPath, TidDateTime, TidNull, TidBottom, TidUnknown:
		return true
	case TidNothing:
		return false
	}
	ar := r.arena
	n := ar.Node(t)
	switch n.Kind {
	case TKParam:
		return params
	case TKList:
		return r.checkable(TypeId(n.A), params, visiting)
	case TKRecord:
		rec := ar.records[n.Extra]
		for _, f := range rec.Fields {
			if f.Type != TidNothing && !r.checkable(f.Type, params, visiting) {
				return false
			}
		}
		if rec.Rest.Status == FieldRequired {
			return false
		}
		return rec.Rest.Type == TidNothing || r.checkable(rec.Rest.Type, params, visiting)
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			if !r.checkable(m, params, visiting) {
				return false
			}
		}
		return true
	case TKEnum:
		if !ar.enumDecls[n.A].Checkable {
			return false
		}
		for _, x := range ar.enumArgs[n.Extra] {
			if !r.checkable(x, params, visiting) {
				return false
			}
		}
		return true
	case TKAlias:
		if slices.Contains(visiting, t) {
			return true
		}
		return r.checkable(ar.aliases[n.A].Body, false, append(visiting, t))
	case TKGrid, TKGridView, TKGridRow:
		// The unknown schema cannot be validated against.
		s := ar.Node(TypeId(n.A))
		return s.Kind == TKRecord && ar.records[s.Extra].Rest.Status != FieldOpen &&
			r.checkable(TypeId(n.A), params, visiting)
	}
	return false
}
