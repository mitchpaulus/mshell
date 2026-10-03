package main

import (
	"maps"
	"slices"
)

// A REPL session checked one line at a time (ai/type-system-plan.md, stage
// 9; design doc, "Checking by default").
//
// The REPL runs a line only if it checks. The script unit stays open from
// one line to the next: the stack, the variables, the definitions and the
// declarations of the lines before are where the next line starts. Running
// lines one after another is running their concatenation (types are erased
// and evaluation is deterministic), so checking line k+1 as the
// continuation of lines 1..k is checking that concatenation, and the
// soundness theorem covers the session.
//
// Each line is solved when it ends, so it never runs on a promise a later
// line could break: the end-of-unit checks are made with every unsolved
// variable read as ⊥ (Substitution.unboundBottom). The defaults are not
// kept: a variable unsolved after a line stays open, so `[] l!` on one line
// and `@l 1 append` on the next check. Checks that still mention an unsolved
// variable are kept, watched by those variables, and made again after a
// later line that binds one of them (openSet); checks that are ground, or
// compare a type with itself, are dropped. So a line costs about the same
// early and late in a long session.
//
// A line that does not check leaves the session as it was. A line that
// checks and then stops with a runtime error leaves the runtime's stack
// restored to before the line, less the new values the line took
// (RuntimeError); `repl_error_commit` in formal-ver/Repl.v proves the
// restored stack has the types the checker gives it.
type CoreSession struct {
	c *coreChecker
	// dc checks the bodies of the defs a line declares: a def body is a
	// unit of its own, and c's unit stays open.
	dc *coreChecker
	// defNames are the names of the defs the session declared, for the
	// name checks (startup files' defs are in the table).
	defNames map[string]Token

	file   []MShellParseItem // the line being checked
	open   bool
	line   sessionMark
	walkAt sessionMark
	pre    []coreSlot // the stack before the line
	// The checks earlier lines left open: c.uni.pairs, c.stores and
	// c.deferred up to each set's kept(). Each is made again only when a
	// line binds one of the variables it mentions.
	openPairs, openStores, openDefer openSet
	// boundAt[v] is boundEpoch when the line bound variable v.
	boundAt    []uint32
	boundEpoch uint32
	depth  int        // how many of pre the line takes
	decl   *sessionDecls
	added  []string // defs the line declared
	// picked are the open choices finishLine made for the line's checks
	// and takes back; undone is pickChoice's scratch.
	picked, undone []int32
	slotBuf        [][]coreSlot
	// partsAt, originsAt, litListsAt are the compaction limits (compact).
	partsAt, originsAt, litListsAt int
}

// appendVars appends the unsolved variables t mentions to vs, once each.
func appendVars(c *coreChecker, vs []TypeVarId, t TypeId) []TypeVarId {
	c.arena.walkTypeVars(c.subst.Apply(c.arena, t), func(v TypeVarId) bool {
		if !slices.Contains(vs, v) {
			vs = append(vs, v)
		}
		return false
	})
	return vs
}

// sessionMark is the checker's state at a point in a line, to go back to.
type sessionMark struct {
	uni                                         UnifierCheckpoint
	stack                                       int
	unitVars, stores, deferred, escapes, parts int
	litLists, origins, pending, choices, errs  int
	setLog, unsetReads, firstLoads, unwraps    int
	dbgs                                        int
	// varUndo is where the variables' saved entries start.
	varUndo int
}

// sessionDecls holds the declaration tables before a line that declares
// types or enums.
type sessionDecls struct {
	declared map[NameId]Token
	aliases  map[NameId]TypeId
	enums    map[NameId]uint32
	ctors    map[NameId]*coreCtor
}

// NewSession starts a session whose stack holds stackLen values of
// unknown type, and whose variables vars are set to values of unknown type:
// whatever the startup files left. Startup code is not checked, so the
// checker cannot know more; and a variable must have a type that covers
// its value even after a line that would have stored it stops early.
func (b *CoreBase) NewSession(stackLen int, vars ...string) *CoreSession {
	c := b.newChecker()
	c.session = true
	// The tables a line's declarations add to are shared with dc, so they
	// must exist before it is made: the base leaves them nil when the
	// startup files declare nothing.
	if c.ctors == nil {
		c.ctors = map[NameId]*coreCtor{}
	}
	if c.declared == nil {
		c.declared = map[NameId]Token{}
	}
	c.beginUnit()
	for range stackLen {
		c.stack = append(c.stack, coreSlot{t: TidUnknown})
	}
	for _, name := range vars {
		v := c.varOf(c.names.Intern(name))
		v.t, v.stored, v.set = TidUnknown, true, true
	}
	c.varUndo = c.varUndo[:0]
	dc := &coreChecker{
		arena: c.arena, names: c.names, rel: c.rel, table: c.table,
		defs: c.defs, ctors: c.ctors, declared: c.declared, res: c.res, session: true,
	}
	dc.res.gens, dc.res.bodyGens, dc.res.badNames, dc.res.errs, dc.res.params, dc.res.unions = nil, nil, nil, nil, nil, nil
	dc.uni = NewUnifier(dc.arena, &dc.subst, dc.rel)
	return &CoreSession{c: c, dc: dc, defNames: map[string]Token{}}
}

// Errors, arena and names format the errors Check returns.
func (s *CoreSession) Arena() *TypeArena { return s.c.arena }
func (s *CoreSession) Names() *NameTable { return s.c.names }

// Len is the number of values the checker has on the stack.
func (s *CoreSession) Len() int { return len(s.c.stack) }

// Check checks a line. With no errors the line is open: Commit keeps it
// (the REPL runs it), Abort takes it back. With errors the session is
// left as it was before the line. The informational diagnostics and `dbg`
// snapshots come back with the errors, as for a file.
func (s *CoreSession) Check(file *MShellFile) (diags []TypeError, ok bool) {
	if s.open {
		s.Abort()
	}
	c := s.c
	s.file = file.Items
	s.pre = append(s.pre[:0], c.stack...)
	s.mark(&s.line)
	s.decl, s.added = nil, s.added[:0]
	s.declare(file)
	if hasError(c.errs) {
		diags = slices.Clone(c.errs)
		s.restore(s.line)
		s.undoDecls()
		return diags, false
	}
	s.mark(&s.walkAt)

	// The line's inputs: the fewest of the stack's top slots it checks
	// with (c.floor keeps a line's words off the slots below, as it keeps
	// a list literal's body off the stack around it). A line that takes
	// none is the common case and costs one walk.
	n := len(s.pre)
	if s.tryDepth(0) {
		s.depth = 0
		s.open = true
		return slices.Clone(c.errs), true
	}
	if n == 0 {
		diags = slices.Clone(c.errs)
		s.restore(s.line)
		s.undoDecls()
		return diags, false
	}
	s.restore(s.walkAt)
	if !s.tryDepth(n) {
		diags = slices.Clone(c.errs)
		s.restore(s.line)
		s.undoDecls()
		return diags, false
	}
	// It checks with every slot. Find the fewest, doubling then halving;
	// any depth it checks with is a sound one.
	hi, lo := n, 0 // checks with hi, not with lo
	for d := 1; d < hi; d *= 2 {
		s.restore(s.walkAt)
		if s.tryDepth(d) {
			hi = d
			break
		}
		lo = d
	}
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		s.restore(s.walkAt)
		if s.tryDepth(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	s.restore(s.walkAt)
	if !s.tryDepth(hi) {
		// Cannot happen: it checked with hi before.
		s.restore(s.walkAt)
		s.tryDepth(n)
		hi = n
	}
	s.depth = hi
	s.open = true
	return slices.Clone(c.errs), true
}

func hasError(errs []TypeError) bool {
	for i := range errs {
		if errs[i].Severity == SeverityError {
			return true
		}
	}
	return false
}

// declare checks the line's definition names, its type and enum
// declarations, the defs' signatures and their bodies.
func (s *CoreSession) declare(file *MShellFile) {
	c := s.c
	if len(declarationItems(file.Items)) > 0 {
		s.decl = &sessionDecls{declared: maps.Clone(c.declared), aliases: maps.Clone(c.res.aliases),
			enums: maps.Clone(c.res.enums), ctors: maps.Clone(c.ctors)}
	}
	for _, def := range file.Definitions {
		if _, ok := s.defNames[def.Name]; !ok {
			s.added = append(s.added, def.Name)
		}
		c.checkDefName(def, s.defNames)
		// A name an earlier line declared as a type, an enum or a member;
		// in a file, declareAll's reserve sees the clash.
		if prev, ok := c.declared[c.names.Intern(def.Name)]; ok {
			c.errs = append(c.errs, TypeError{Kind: TErrDeclaration, Pos: def.NameToken,
				Hint: "'" + def.Name + "' is already declared at " + tokenPosStr(prev)})
		}
	}
	c.declareAll(file.Items, s.defNames)
	if len(file.Definitions) == 0 {
		return
	}
	for i := range file.Definitions {
		def := &file.Definitions[i]
		nerr := len(c.res.errs)
		parts := c.res.resolveSig(def.Inputs, def.Outputs)
		broken := len(c.res.errs) > nerr
		c.takeResolveErrors()
		sig := newCoreSig(c.arena, parts)
		sig.broken = broken
		c.defs[c.names.Intern(def.Name)] = &sig
		if e := completionSigError(c.arena, c.names, c.rel, c.table, def, &sig); e != nil {
			c.errs = append(c.errs, *e)
		}
	}
	dc := s.dc
	dc.res.aliases, dc.res.enums = c.res.aliases, c.res.enums
	dc.errs = dc.errs[:0]
	dc.checkDefs(file.Definitions)
	c.errs = append(c.errs, dc.errs...)
}

// undoDecls takes back the definitions and declarations of a line that is
// not kept.
func (s *CoreSession) undoDecls() {
	c := s.c
	for _, name := range s.added {
		delete(s.defNames, name)
		delete(c.defs, c.names.Intern(name))
	}
	s.added = s.added[:0]
	if d := s.decl; d != nil {
		refill(c.declared, d.declared)
		refill(c.res.aliases, d.aliases)
		refill(c.res.enums, d.enums)
		refill(c.ctors, d.ctors)
		s.decl = nil
	}
}

// tryDepth walks the line taking at most d of the stack's slots, and
// solves it. It reports whether that checked with no errors and left the
// slots below untouched.
func (s *CoreSession) tryDepth(d int) bool {
	c := s.c
	n := len(s.pre)
	c.diverged, c.abandoned = false, false
	c.floor, c.listDepth = n-d, 0
	c.brk, c.cont, c.brkSeen, c.infer = coreLoopCtx{}, coreLoopCtx{}, false, nil
	c.later, c.assertive = 0, false
	c.ret, c.retOuts = retAny, nil
	c.walk(s.file)
	c.floor = 0
	s.finishLine()
	if hasError(c.errs) || len(c.stack) < n-d {
		return false
	}
	// The floor kept the line's words off the slots below it; check that
	// it did, rather than trust it.
	for i := range n - d {
		if c.stack[i] != s.pre[i] {
			return false
		}
	}
	return true
}

// finishLine makes the end-of-unit checks with unsolved variables read as
// ⊥, then takes the defaults back.
func (s *CoreSession) finishLine() {
	c := s.c
	for i := range c.pending {
		if !c.pending[i].done && !c.abandoned {
			c.inferPending(uint32(i + 1))
		}
	}
	// An overload choice still open stays open for the lines after it, as
	// in a file a later word may decide it: `(len) q!`, then `"abc" @q x`.
	// The line still needs a typing of its own, so each open choice is made
	// here, the line checked with it, and the choice taken back. Past
	// maxOpenChoices the oldest are made for good instead, so a line's cost
	// stays bounded.
	cp := c.checkpoint()
	s.picked = s.picked[:0]
	if !c.abandoned {
		c.choiceVersion = -1
		c.retryChoices()
		for i := range c.choices {
			if !c.choices[i].done {
				s.picked = append(s.picked, int32(i))
			}
		}
		permanent := max(0, len(s.picked)-maxOpenChoices)
		for _, i := range s.picked[:permanent] {
			s.pickChoice(int(i))
		}
		s.picked = s.picked[permanent:]
		cp = c.checkpoint()
		for _, i := range s.picked {
			s.pickChoice(int(i))
		}
	}
	// The variables this line bound: the trail holds every write since the
	// line's checkpoint. The open checks that mention one are made again,
	// and the line's own checks; no other answer can have changed.
	s.boundEpoch++
	s.openPairs.reset()
	s.openStores.reset()
	s.openDefer.reset()
	for _, w := range c.subst.trail[s.line.uni.subst.n:] {
		if int(w.v) >= len(s.boundAt) {
			s.boundAt = slices.Grow(s.boundAt, len(c.subst.bound)-len(s.boundAt))[:len(c.subst.bound)]
		}
		if s.boundAt[w.v] == s.boundEpoch {
			continue
		}
		s.boundAt[w.v] = s.boundEpoch
		s.openPairs.bound(w.v)
		s.openStores.bound(w.v)
		s.openDefer.bound(w.v)
	}
	c.subst.unboundBottom = true
	for i := range s.openPairs.toCheck(len(c.uni.pairs)) {
		p := c.uni.pairs[i]
		if a, b := c.subst.Apply(c.arena, p.a), c.subst.Apply(c.arena, p.b); !c.rel.Equal(a, b) {
			c.errs = append(c.errs, TypeError{Kind: TErrCoreInternal, Hint: "unified types differ once solved: " +
				c.format(a) + " and " + c.format(b)})
		}
	}
	// Stores and deferred checks before the line's marks are earlier
	// lines'; an error from one is marked so (TypeError.Earlier).
	earlier := func(from int, isEarlier bool) {
		for i := from; isEarlier && i < len(c.errs); i++ {
			c.errs[i].Earlier = true
		}
	}
	for i := range s.openStores.toCheck(len(c.stores)) {
		st := c.stores[i]
		n := len(c.errs)
		t, v := c.subst.Apply(c.arena, st.t), c.subst.Apply(c.arena, st.v)
		if ok, _ := c.markBelow(st.mark, t, v); !ok {
			c.errs = append(c.errs, c.storeError(st.tok, st.name, t, v))
		}
		earlier(n, i < s.line.stores)
	}
	for i := range s.openDefer.toCheck(len(c.deferred)) {
		d := c.deferred[i]
		n := len(c.errs)
		earlierCheck := i < s.line.deferred
		if d.rule != 0 {
			if !c.keyAllowed(d.t, d.rule) {
				c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: d.tok,
					Hint: "'" + d.tok.Lexeme + "' needs " + keyRuleText(d.rule) + ", got " + c.format(c.subst.Apply(c.arena, d.t))})
			}
			earlier(n, earlierCheck)
			continue
		}
		t, want := c.subst.Apply(c.arena, d.t), c.subst.Apply(c.arena, d.want)
		if ok, _ := c.markBelow(d.mark, t, want); !ok {
			c.deferredError(&d, t, want)
		}
		earlier(n, earlierCheck)
	}
	c.finishEscapes()
	c.finishAssign()
	for _, u := range c.unwraps {
		if c.alwaysNone(u.t) {
			c.errs = append(c.errs, TypeError{Kind: TErrUnwrapAlwaysFails, Severity: SeverityInfo, Pos: u.tok,
				Hint: "'?' unwraps a value that can only be none; this fails at run time"})
		}
	}
	for _, d := range c.dbgs {
		c.errs = append(c.errs, TypeError{Kind: TErrDebugDump, Severity: SeverityInfo, Pos: d.tok, Hint: c.formatDbg(d)})
	}
	for _, tok := range c.firstLoads {
		if v := &c.vars[c.names.Intern(trimAt(tok.Lexeme))]; !v.stored {
			c.errs = append(c.errs, TypeError{Kind: TErrUnknownIdentifier, Pos: tok, Name: tok.Lexeme})
		}
	}
	c.subst.unboundBottom = false
	c.rollback(cp)
	for _, i := range s.picked {
		c.choices[i].done = false
	}
	c.choiceVersion = -1
}

// maxOpenChoices is how many overload choices a session keeps open from
// one line to the next. Each is made again at the end of every line.
const maxOpenChoices = 16

// pickChoice makes the open choice i with the first candidate that fits
// and leaves no other choice without one, or, if every candidate does,
// the first that fits, with the errors that follows.
func (s *CoreSession) pickChoice(i int) {
	c := s.c
	if c.choices[i].done {
		return // a retry made it
	}
	first := -1
	for j := range c.choices[i].sigs {
		cp := c.checkpoint()
		nerr := len(c.errs)
		s.undone = s.undone[:0]
		for k := range c.choices {
			if !c.choices[k].done {
				s.undone = append(s.undone, int32(k))
			}
		}
		if !c.choiceFits(&c.choices[i].sigs[j], &c.choices[i]) {
			c.rollback(cp)
			continue
		}
		c.choices[i].done = true
		c.choiceVersion = -1
		c.retryChoices()
		if len(c.errs) == nerr {
			return
		}
		if first < 0 {
			first = j
		}
		c.errs = c.errs[:nerr]
		c.rollback(cp)
		for _, k := range s.undone {
			c.choices[k].done = false
		}
	}
	if first < 0 {
		return // cannot happen: a retry reports a choice nothing fits
	}
	c.commitChoice(&c.choices[i], first)
	c.choiceVersion = -1
	c.retryChoices()
}

func trimAt(s string) string {
	if len(s) > 0 && s[0] == '@' {
		return s[1:]
	}
	return s
}

// Commit keeps the checked line: the REPL is about to run it. The checks
// that are now ground passed, and stay passed whatever later lines do, so
// they are dropped.
func (s *CoreSession) Commit() {
	if !s.open {
		return
	}
	s.open = false
	c := s.c
	c.subst.Commit()
	// A check whose two sides are ground passed with them, and one whose
	// two sides are the same type passes under any substitution: neither
	// can fail after a later line, so they are dropped. What is left
	// mentions a variable a later line may solve, and is watched for it.
	varsOf := func(ts ...TypeId) []TypeVarId {
		var vs []TypeVarId
		for _, t := range ts {
			vs = appendVars(c, vs, t)
		}
		return vs
	}
	open := func(a, b TypeId) ([]TypeVarId, bool) {
		a, b = c.subst.Apply(c.arena, a), c.subst.Apply(c.arena, b)
		if a == b || !c.mentionsVars(a) && !c.mentionsVars(b) {
			return nil, false
		}
		return varsOf(a, b), true
	}
	c.uni.pairs = commitOpen(&s.openPairs, c.uni.pairs, func(p *typePair) ([]TypeVarId, bool) {
		p.a, p.b = c.subst.Apply(c.arena, p.a), c.subst.Apply(c.arena, p.b)
		return open(p.a, p.b)
	})
	c.stores = commitOpen(&s.openStores, c.stores, func(st *coreStore) ([]TypeVarId, bool) {
		return open(st.t, st.v)
	})
	c.deferred = commitOpen(&s.openDefer, c.deferred, func(d *coreDeferred) ([]TypeVarId, bool) {
		if d.rule != 0 {
			vs := varsOf(d.t)
			return vs, len(vs) > 0
		}
		return open(d.t, d.want)
	})
	// Every quote literal is typed at the end of a line; nothing on the
	// stack waits for one any more.
	for i := range c.stack {
		c.stack[i].pq = 0
	}
	// The choices still open wait for a later line, watched by the
	// variables they mention.
	kept := c.choices[:0]
	for _, ch := range c.choices {
		if ch.done {
			continue
		}
		ch.watch = ch.watch[:0]
		for _, a := range ch.args {
			ch.watch = appendVars(c, ch.watch, a.t)
		}
		for _, o := range ch.outs {
			ch.watch = appendVars(c, ch.watch, o)
		}
		kept = append(kept, ch)
	}
	clear(c.choices[len(kept):])
	c.choices, c.keptChoices, c.choiceVersion = kept, len(kept), -1
	s.compact()
	c.pending = c.pending[:0]
	c.escapes = c.escapes[:0]
	c.setLog, c.daLoops, c.unsetReads = c.setLog[:0], c.daLoops[:0], c.unsetReads[:0]
	c.firstLoads, c.unwraps, c.dbgs, c.errs = c.firstLoads[:0], c.unwraps[:0], c.dbgs[:0], c.errs[:0]
	c.varUndo = c.varUndo[:0]
	c.varEpoch++
	s.added, s.decl = s.added[:0], nil
}

// Abort takes back the line Check left open.
func (s *CoreSession) Abort() {
	if !s.open {
		return
	}
	s.open = false
	s.restore(s.line)
	s.undoDecls()
}

// Diverges reports whether the line Check accepted never returns
// normally: an exit, a loop with no break, a call to a `never` def. If it
// comes back at all, it is with a runtime error.
func (s *CoreSession) Diverges() bool { return s.c.diverged }

// Depth is how many of the stack's slots before the last line the line
// takes; the slots below them are untouched by it.
func (s *CoreSession) Depth() int { return s.depth }

// ClearStack drops every value on the stack, between lines: the REPL has
// emptied its stack, and the checker follows, as if each value had been
// dropped.
func (s *CoreSession) ClearStack() {
	if s.open {
		panic("ClearStack with a line open")
	}
	s.c.stack = s.c.stack[:0]
}

// RuntimeError is called when the last committed line stopped with a
// runtime error. The REPL restores the stack it had before the line,
// except the new values the line took: keep[i] says whether to keep the
// i-th of the line's d inputs (bottom first), and the slots below them
// are kept. The checker's stack becomes the same.
//
// A shared input keeps its type: the line could not change it. A new input
// of an immutable type is as good as a shared one. Any other new input may
// have been changed or stored by the line, so it is dropped
// (repl_error_commit in formal-ver/Repl.v; plan question 20).
func (s *CoreSession) RuntimeError() (d int, keep []bool) {
	c := s.c
	d = s.depth
	n := len(s.pre)
	keep = make([]bool, d)
	c.stack = append(c.stack[:0], s.pre[:n-d]...)
	for i := range d {
		slot := s.pre[n-d+i]
		if slot.part != 0 {
			continue
		}
		if slot.fresh {
			t := c.subst.Apply(c.arena, slot.t)
			if c.hasVars(t) || !c.rel.Immutable(t) {
				continue
			}
			slot.fresh = false
		}
		keep[i] = true
		c.stack = append(c.stack, slot)
	}
	return d, keep
}

// mark records the checker's state in m. A variable's entry is saved the
// first time it changes after the mark (varOf), not here, so a mark costs
// the same however many variables the session has.
func (s *CoreSession) mark(m *sessionMark) {
	c := s.c
	c.varEpoch++
	*m = sessionMark{
		uni: c.uni.Checkpoint(), stack: len(c.stack),
		unitVars: len(c.unitVars), stores: len(c.stores), deferred: len(c.deferred), escapes: len(c.escapes),
		parts: len(c.parts), litLists: len(c.litLists), origins: len(c.origins), pending: len(c.pending),
		choices: len(c.choices), errs: len(c.errs), setLog: len(c.setLog), unsetReads: len(c.unsetReads),
		firstLoads: len(c.firstLoads), unwraps: len(c.unwraps), dbgs: len(c.dbgs), varUndo: len(c.varUndo),
	}
}

// restore goes back to m: the slices the line appended to are cut back,
// the variables it touched restored, the ones it made forgotten, and the
// stack is the one before the line.
func (s *CoreSession) restore(m sessionMark) {
	c := s.c
	c.uni.Rollback(m.uni)
	for _, name := range c.unitVars[m.unitVars:] {
		c.vars[name].gen = 0
	}
	c.unitVars = c.unitVars[:m.unitVars]
	for i := len(c.varUndo) - 1; i >= m.varUndo; i-- {
		u := c.varUndo[i]
		c.vars[u.name] = u.v
	}
	c.varUndo = c.varUndo[:m.varUndo]
	// Entries changed again after this must be saved again.
	c.varEpoch++
	c.stack = append(c.stack[:0], s.pre...)
	c.stores, c.deferred, c.escapes = c.stores[:m.stores], c.deferred[:m.deferred], c.escapes[:m.escapes]
	c.parts, c.litLists, c.origins = c.parts[:m.parts], c.litLists[:m.litLists], c.origins[:m.origins]
	// The choices before the mark are the ones earlier lines left open.
	c.pending, c.choices, c.choiceVersion = c.pending[:m.pending], c.choices[:m.choices], -1
	for i := range c.choices {
		c.choices[i].done = false
	}
	c.errs, c.setLog, c.unsetReads = c.errs[:m.errs], c.setLog[:m.setLog], c.unsetReads[:m.unsetReads]
	c.firstLoads, c.unwraps, c.dbgs = c.firstLoads[:m.firstLoads], c.unwraps[:m.unwraps], c.dbgs[:m.dbgs]
	c.daLoops, c.saved = c.daLoops[:0], c.saved[:0]
	c.diverged, c.abandoned, c.floor, c.listDepth = false, false, 0, 0
}

// A session's partly new marks, join origins and literal name lists grow
// with every line, and are kept only while a slot, a variable or a kept
// check refers to them. Once one of them passes its limit, the ones still
// referred to are kept, renumbered, and the limit becomes twice that many
// (at least compactAt), so compacting costs a constant per line on
// average. Partly new marks are numbered with 16 bits (maxParts), and past
// that a value is treated as shared: without this a long session would
// start refusing partly new literals.
const compactAt = 1024

func (s *CoreSession) compact() {
	c := s.c
	if len(c.parts) > max(compactAt, s.partsAt) {
		s.compactParts()
		s.partsAt = 2 * len(c.parts)
	}
	if len(c.origins) > max(compactAt, s.originsAt) {
		s.compactOrigins()
		s.originsAt = 2 * len(c.origins)
	}
	if len(c.litLists) > max(compactAt, s.litListsAt) {
		s.compactLitLists()
		s.litListsAt = 2 * len(c.litLists)
	}
}

// slotLists are the slots a session keeps from one line to the next: the
// stack, the stack before the line (RuntimeError restores it), and the
// arguments of the choices left open.
func (s *CoreSession) slotLists() [][]coreSlot {
	ls := append(s.slotBuf[:0], s.c.stack, s.pre)
	for i := range s.c.choices {
		ls = append(ls, s.c.choices[i].args)
	}
	s.slotBuf = ls
	return ls
}

// compactOrigins keeps the join origins the kept slots and the variables
// refer to, renumbered.
func (s *CoreSession) compactOrigins() {
	c := s.c
	remap := make([]uint32, len(c.origins))
	var kept []coreOrigin
	ref := func(o uint32) uint32 {
		if o == 0 || int(o) > len(remap) {
			return 0
		}
		if remap[o-1] == 0 {
			kept = append(kept, c.origins[o-1])
			remap[o-1] = uint32(len(kept))
		}
		return remap[o-1]
	}
	for _, st := range s.slotLists() {
		for i := range st {
			st[i].origin = ref(st[i].origin)
		}
	}
	for _, name := range c.unitVars {
		c.vars[name].origin = ref(c.vars[name].origin)
	}
	clear(c.origins)
	c.origins = append(c.origins[:0], kept...)
}

// compactLitLists keeps the literal name lists the kept slots refer to,
// renumbered.
func (s *CoreSession) compactLitLists() {
	c := s.c
	remap := make([]NameId, len(c.litLists))
	var kept [][]NameId
	for _, st := range s.slotLists() {
		for i := range st {
			if st[i].lit&litListTag == 0 {
				continue
			}
			j := st[i].lit&^litListTag - 1
			if remap[j] == 0 {
				kept = append(kept, c.litLists[j])
				remap[j] = litListTag | NameId(len(kept))
			}
			st[i].lit = remap[j]
		}
	}
	clear(c.litLists)
	c.litLists = append(c.litLists[:0], kept...)
}

// compactParts keeps the partly new marks the kept slots and checks still
// refer to, renumbered.
func (s *CoreSession) compactParts() {
	c := s.c
	remap := make([]coreMark, len(c.parts))
	var kept []corePart
	var visit func(m coreMark) coreMark
	visit = func(m coreMark) coreMark {
		if m < 2 {
			return m
		}
		i := int(m) - 2
		if remap[i] != 0 {
			return remap[i]
		}
		p := c.parts[i]
		p.elem, p.rest = visit(p.elem), visit(p.rest)
		if len(p.labels) > 0 {
			labels := make([]coreLabelMark, len(p.labels))
			for j, l := range p.labels {
				labels[j] = coreLabelMark{l.name, visit(l.m)}
			}
			p.labels = labels
		}
		kept = append(kept, p)
		remap[i] = coreMark(len(kept) + 1)
		return remap[i]
	}
	// The stack before the line too: RuntimeError restores its slots. And
	// the arguments of the choices left open.
	for _, st := range s.slotLists() {
		for i := range st {
			if st[i].part != 0 {
				st[i].part = uint16(visit(coreMark(st[i].part)+1) - 1)
			}
		}
	}
	for i := range c.stores {
		c.stores[i].mark = visit(c.stores[i].mark)
	}
	for i := range c.deferred {
		c.deferred[i].mark = visit(c.deferred[i].mark)
	}
	c.parts = append(c.parts[:0], kept...)
}

// openSet is the checks of one kind that earlier lines left open: the
// first kept() of a slice of the checker's, with, for each, the unsolved
// variables it mentions. A check that closes is marked dead, and the slice
// is compacted once dead ones are most of it.
type openSet struct {
	vars  [][]TypeVarId
	dead  []bool
	ndead int
	watch map[TypeVarId][]int32 // the checks that mention a variable
	// redo lists the kept checks the last finishLine made again.
	redo   []int32
	isRedo []bool
}

func (o *openSet) kept() int { return len(o.vars) }

// reset forgets which checks the last finishLine made again.
func (o *openSet) reset() {
	for _, i := range o.redo {
		o.isRedo[i] = false
	}
	o.redo = o.redo[:0]
}

// bound marks the open checks that mention v to be made again.
func (o *openSet) bound(v TypeVarId) {
	for _, i := range o.watch[v] {
		if !o.dead[i] && !o.isRedo[i] {
			o.isRedo[i] = true
			o.redo = append(o.redo, i)
		}
	}
}

// toCheck yields the checks finishLine makes: the kept ones marked to be
// made again, and the line's own, kept() to n.
func (o *openSet) toCheck(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for _, i := range o.redo {
			if !yield(int(i)) {
				return
			}
		}
		for i := o.kept(); i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func (o *openSet) watchAll(i int32) {
	if o.watch == nil {
		o.watch = map[TypeVarId][]int32{}
	}
	for _, v := range o.vars[i] {
		ws := o.watch[v]
		if len(ws) == 0 || ws[len(ws)-1] != i {
			o.watch[v] = append(ws, i)
		}
	}
}

// commitOpen updates o and xs, the checker's slice of these checks, when a
// line is kept: open says whether a check is still open, and the variables
// it mentions. Only the checks the line made again and its own are asked.
func commitOpen[T any](o *openSet, xs []T, open func(*T) ([]TypeVarId, bool)) []T {
	for _, i := range o.redo {
		if vs, ok := open(&xs[i]); ok {
			o.vars[i] = vs
			o.watchAll(i)
		} else {
			o.dead[i] = true
			o.ndead++
		}
	}
	o.reset()
	n := o.kept()
	for i := n; i < len(xs); i++ {
		if vs, ok := open(&xs[i]); ok {
			xs[n] = xs[i]
			o.vars = append(o.vars, vs)
			o.dead = append(o.dead, false)
			o.isRedo = append(o.isRedo, false)
			o.watchAll(int32(n))
			n++
		}
	}
	xs = xs[:n]
	if o.ndead > 64 && 2*o.ndead > len(o.vars) {
		live := 0
		for i := range xs {
			if !o.dead[i] {
				xs[live], o.vars[live] = xs[i], o.vars[i]
				live++
			}
		}
		xs, o.vars = xs[:live], o.vars[:live]
		o.dead, o.isRedo = o.dead[:live], o.isRedo[:live]
		clear(o.dead)
		o.ndead = 0
		clear(o.watch)
		for i := range live {
			o.watchAll(int32(i))
		}
	}
	return xs
}
