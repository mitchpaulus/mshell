package main

import "strconv"

// Quotes in the core checker (ai/type-system-plan.md, section 5 and
// stage 3; design doc, "Quotes, definitions, control flow" and "Quotes
// that break").
//
// A quote literal is not typed where it is written. Its slot waits, holding
// the literal, until something uses it:
//
//   - a word that takes a quote checks the body against its parameter type,
//     after its other arguments are known: `@xs (1 +) map` checks the body
//     with an int input;
//   - `x`, `iff` and `loop` given a literal run its body inline, on the
//     current stack: this is the elaboration of a literal quote at those
//     sites into `if` and `loop{...}`;
//   - anything else (dup, a store, a list literal, a def output) types the
//     quote on its own first: inputs it reads below its own pushes become
//     new variables.
//
// Shuffles that only move a slot leave it waiting.

// corePending is a quote literal that has not been typed yet.
type corePending struct {
	items []MShellParseItem
	tok   Token
	// t is a unification variable that stands for the quote's type until
	// it is known, and is unified with it then.
	t    TypeId
	done bool
}

// coreLoopKind is a break or continue context (L in the typing rules).
type coreLoopKind uint8

const (
	loopNone  coreLoopKind = iota // no loop: break is an error
	loopExact                     // in a loop body: break leaves the loop's stack
	loopChild                     // in a child-stack body in a loop: the stack is discarded
)

type coreLoopCtx struct {
	kind  coreLoopKind
	stack savedRun // the loop's stack, for loopExact
	// da is 1 + the index of the loop's definite-assignment record in
	// daLoops, or 0.
	da int
}

// coreInfer is a quote being typed on its own: underflow below floor makes
// new input variables instead of an error.
type coreInfer struct {
	floor int
	ins   []TypeId
}

// pushQuote pushes a waiting quote literal.
func (c *coreChecker) pushQuote(items []MShellParseItem, tok Token) {
	c.pending = append(c.pending, corePending{items: items, tok: tok, t: c.subst.FreshVar(c.arena)})
	c.stack = append(c.stack, coreSlot{t: c.pending[len(c.pending)-1].t, pq: uint32(len(c.pending))})
}

// waiting returns the waiting quote in a slot, or nil.
func (c *coreChecker) waiting(s coreSlot) *corePending {
	if s.pq == 0 {
		return nil
	}
	p := &c.pending[s.pq-1]
	if p.done {
		return nil
	}
	return p
}

// force types the quote waiting in stack slot i on its own.
func (c *coreChecker) force(i int) {
	if p := c.waiting(c.stack[i]); p != nil {
		c.inferPending(c.stack[i].pq)
	}
	c.stack[i].pq = 0
}

// forceWaiting types every quote literal still waiting on the stack, as
// an if, match, iff, and/or or loop starts. Their arms, or the loop's
// back edge, would otherwise share one waiting literal: the first arm to
// use it decides its type, inline consumers without checking the body
// against it, and the other arms see that type.
func (c *coreChecker) forceWaiting() {
	for i := c.floor; i < len(c.stack); i++ {
		if c.stack[i].pq != 0 {
			c.force(i)
		}
	}
}

// forceTop forces the top n slots.
func (c *coreChecker) forceTop(n int) {
	for i := len(c.stack) - n; i < len(c.stack); i++ {
		if i >= 0 {
			c.force(i)
		}
	}
}

// inferPending types a waiting quote on its own and records its type.
func (c *coreChecker) inferPending(pq uint32) {
	p := &c.pending[pq-1]
	p.done = true
	items, tok, placeholder := p.items, p.tok, p.t
	t := c.inferQuote(items, tok)
	if t != TidNothing {
		c.uni.Unify(placeholder, t)
	}
}

// coreFrame is the state saved around a body checked on its own stack.
type coreFrame struct {
	outer     savedRun
	floor     int
	diverged  bool
	listDepth int
	ret       coreRetKind
	retOuts   []TypeId
	brk, cont coreLoopCtx
	infer     *coreInfer
}

// enterBody saves the walking state and starts an empty stack for a body.
// The body has no return context and no loop, until the caller sets them.
func (c *coreChecker) enterBody() coreFrame {
	f := coreFrame{
		outer: c.saveStack(), floor: c.floor, diverged: c.diverged, listDepth: c.listDepth,
		ret: c.ret, retOuts: c.retOuts, brk: c.brk, cont: c.cont, infer: c.infer,
	}
	c.stack = c.stack[:0]
	c.floor, c.diverged, c.listDepth = 0, false, 0
	c.ret, c.retOuts = retNone, nil
	c.brk, c.cont = coreLoopCtx{}, coreLoopCtx{}
	c.infer = nil
	return f
}

func (c *coreChecker) leaveBody(f coreFrame) {
	c.restoreStack(f.outer)
	c.saved = c.saved[:f.outer.start]
	c.floor, c.diverged, c.listDepth = f.floor, f.diverged, f.listDepth
	c.ret, c.retOuts = f.ret, f.retOuts
	c.brk, c.cont = f.brk, f.cont
	c.infer = f.infer
}

// inferQuote types a quote body on its own: the Quote rule, with the inputs
// the body reads below its own pushes as new variables. It returns the
// quote type, or TidNothing when the unit was abandoned.
func (c *coreChecker) inferQuote(items []MShellParseItem, tok Token) TypeId {
	f := c.enterBody()
	inf := &coreInfer{floor: 0}
	c.infer = inf
	daMark := len(c.setLog)
	c.later++
	c.walk(items)
	c.later--
	c.daRestore(daMark)
	var t TypeId
	if !c.abandoned {
		c.forceTop(len(c.stack))
		sig := QuoteSig{Inputs: inf.ins, Diverges: c.diverged}
		if !c.diverged {
			sig.Outputs = make([]TypeId, len(c.stack))
			for i, s := range c.stack {
				sig.Outputs[i] = s.t
			}
		}
		if sig.Inputs == nil {
			sig.Inputs = []TypeId{}
		}
		t = c.arena.MakeQuote(sig)
	}
	abandoned := c.abandoned
	c.leaveBody(f)
	c.abandoned = abandoned
	return t
}

// checkPending checks a waiting quote's body against want, a quote type, as
// the argument of a word that takes a quote. child says the word runs the
// quote on a child stack (each, map, ...); outerBase is the stack height
// once the word has taken its arguments, for the break context.
func (c *coreChecker) checkPending(pq uint32, want TypeId, child bool, outerBase int, tok Token) bool {
	p := &c.pending[pq-1]
	want = c.subst.Apply(c.arena, want)
	if c.arena.nodes[want].Kind != TKQuote {
		c.inferPending(pq)
		return c.check(coreSlot{t: p.t}, want)
	}
	p.done = true
	sig := c.arena.quoteSigs[c.arena.nodes[want].Extra]
	items, placeholder := p.items, p.t

	brk := coreLoopCtx{}
	if child {
		brk = c.childLoopCtx(outerBase)
	}
	f := c.enterBody()
	c.brk, c.cont = brk, brk
	for _, in := range sig.Inputs {
		c.stack = append(c.stack, coreSlot{t: in})
	}
	// The word may run the quote zero times: its stores do not count after.
	daMark := len(c.setLog)
	defer c.daRestore(daMark)
	c.walk(items)
	ok := true
	if !c.abandoned && !c.diverged {
		c.forceTop(len(c.stack))
		if sig.Diverges {
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: p.tok,
				Hint: "'" + tok.Lexeme + "' wants a quote that never returns, but this one can return"})
			ok = false
		} else if len(c.stack) != len(sig.Outputs) {
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: p.tok,
				Hint: "'" + tok.Lexeme + "' wants a quote " + c.format(want) + ", but this one leaves " +
					strconv.Itoa(len(c.stack)) + " value(s) " + c.formatSlots(c.stack)})
			ok = false
		} else {
			for i, out := range sig.Outputs {
				if !c.check(c.stack[i], out) {
					c.mismatch(p.tok, i, out, c.stack[i].t)
					ok = false
				}
			}
		}
	}
	abandoned := c.abandoned
	c.leaveBody(f)
	c.abandoned = abandoned
	c.uni.Unify(placeholder, want)
	return ok
}

// childLoopCtx is the break context of a literal quote run on a child stack
// (the Each rule): break is allowed when the enclosing context allows it in
// a child body, or when the stack the loop sees after the word took its
// arguments is the loop's stack.
func (c *coreChecker) childLoopCtx(outerBase int) coreLoopCtx {
	switch c.brk.kind {
	case loopChild:
		return coreLoopCtx{kind: loopChild, da: c.brk.da}
	case loopExact:
		if c.stackFits(c.stack[:outerBase], c.brk.stack) {
			return coreLoopCtx{kind: loopChild, da: c.brk.da}
		}
	}
	return coreLoopCtx{}
}

// stackFits reports whether the slots fit the saved stack want, slot by
// slot.
func (c *coreChecker) stackFits(got []coreSlot, want savedRun) bool {
	want = c.padRun(want)
	if len(got) != want.end-want.start {
		return false
	}
	for i := range got {
		if !c.check(got[i], c.saved[want.start+i].t) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// x, iff, loop, break, continue

// interpret checks `x`. A literal runs inline on the current stack; any
// other quote needs a known arity.
func (c *coreChecker) interpret(tok Token) {
	if !c.need(1, tok) {
		return
	}
	top := c.stack[len(c.stack)-1]
	if p := c.waiting(top); p != nil {
		p.done = true
		c.uni.Unify(p.t, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{}}))
		c.stack = c.stack[:len(c.stack)-1]
		c.walkInline(p.items)
		return
	}
	c.stack = c.stack[:len(c.stack)-1]
	c.runQuoteValue(top, tok)
}

// runQuoteValue checks running a quote value that is not a waiting
// literal, as `x` does: its type must be a known quote type.
func (c *coreChecker) runQuoteValue(s coreSlot, tok Token) {
	t := c.subst.Apply(c.arena, s.t)
	n := c.arena.nodes[t]
	if n.Kind != TKQuote {
		if c.hasVars(t) {
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
				Hint: "'" + tok.Lexeme + "' runs a quote whose inputs and outputs are not known here; annotate this quote"})
		} else {
			c.mismatch(tok, 0, c.arena.MakeQuote(QuoteSig{}), t)
		}
		c.abandoned = true
		return
	}
	qs := c.arena.quoteSigs[n.Extra]
	sig := coreSig{ins: qs.Inputs, outs: qs.Outputs, diverges: qs.Diverges}
	c.apply(&sig, tok)
}

// iff checks `cond (a) (b) iff` and `cond (a) iff`: an if whose arms run
// the quotes on the current stack.
func (c *coreChecker) iff(tok Token) {
	if !c.need(2, tok) {
		return
	}
	n := len(c.stack)
	var arms []coreSlot
	if c.isQuoteSlot(c.stack[n-2]) {
		if !c.need(3, tok) {
			return
		}
		arms = []coreSlot{c.stack[n-2], c.stack[n-1]}
		c.stack = c.stack[:n-2]
	} else {
		arms = []coreSlot{c.stack[n-1]}
		c.stack = c.stack[:n-1]
	}
	if !c.condition(tok) {
		return
	}
	c.forceWaiting()
	mark := len(c.saved)
	entry := c.saveStack()
	var runs []savedRun
	daMark := len(c.setLog)
	var sets [][]NameId
	for _, q := range arms {
		c.restoreStack(entry)
		c.daRestore(daMark)
		if p := c.waiting(q); p != nil {
			p.done = true
			c.uni.Unify(p.t, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{}}))
			c.walkInline(p.items)
		} else {
			c.runQuoteValue(q, tok)
		}
		if c.abandoned {
			c.saved = c.saved[:mark]
			return
		}
		if !c.diverged {
			sets = append(sets, c.daSince(daMark))
		}
		run := c.saveArm()
		run.label, run.line = "the quote run when true", tok.Line
		if len(runs) == 1 {
			run.label = "the quote run when false"
		}
		runs = append(runs, run)
	}
	if len(arms) == 1 {
		c.restoreStack(entry)
		run := c.saveArm()
		run.label, run.line = "the missing quote for false", tok.Line
		runs = append(runs, run)
		sets = append(sets, nil)
	}
	c.joinArms(runs, tok)
	c.daJoin(daMark, sets)
	c.saved = c.saved[:mark]
}

// andOr checks `b (q) and` and `b (q) or` with a literal quote, as the
// elaborations `b if q else false end` and `b if true else q end`. The
// quote runs on the current stack (a current-stack word, design doc
// "Quotes that break"), so a break in it leaves the enclosing loop, and it
// must leave one bool. Anything else goes to the table's forms.
func (c *coreChecker) andOr(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	p := c.waiting(c.stack[n-1])
	if p == nil {
		return false
	}
	p.done = true
	c.uni.Unify(p.t, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{}, Outputs: []TypeId{TidBool}}))
	c.stack = c.stack[:n-1]
	if !c.popBool(tok) {
		return true
	}
	c.forceWaiting()
	mark := len(c.saved)
	entry := c.saveStack()
	daMark := len(c.setLog)
	var sets [][]NameId
	c.walkInline(p.items)
	if c.abandoned {
		c.saved = c.saved[:mark]
		return true
	}
	if !c.diverged {
		if !c.popBool(tok) {
			c.saved = c.saved[:mark]
			return true
		}
		c.push(TidBool, true)
		sets = append(sets, c.daSince(daMark))
	}
	runs := []savedRun{c.saveArm()}
	c.restoreStack(entry)
	c.daRestore(daMark)
	c.push(TidBool, true)
	runs = append(runs, c.saveArm())
	sets = append(sets, nil)
	c.joinArms(runs, tok)
	c.daJoin(daMark, sets)
	c.saved = c.saved[:mark]
	return true
}

// popBool pops a value that must be a bool.
func (c *coreChecker) popBool(tok Token) bool {
	if !c.need(1, tok) {
		return false
	}
	c.forceTop(1)
	slot := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	if c.hasVars(slot.t) {
		if !c.uni.Unify(slot.t, TidBool) {
			c.mismatch(tok, 0, TidBool, slot.t)
		}
		return true
	}
	if t := c.subst.Apply(c.arena, slot.t); !c.rel.Sub(t, TidBool) {
		c.mismatch(tok, 0, TidBool, t)
	}
	return true
}

// walkInline walks a literal quote's body on the current stack. A quote
// body is not in a list literal, even when the quote is.
func (c *coreChecker) walkInline(items []MShellParseItem) {
	depth := c.listDepth
	c.listDepth = 0
	c.walk(items)
	c.listDepth = depth
}

// isQuoteSlot reports whether a slot holds a quote, waiting or typed.
func (c *coreChecker) isQuoteSlot(s coreSlot) bool {
	if c.waiting(s) != nil {
		return true
	}
	return c.arena.nodes[c.subst.Apply(c.arena, s.t)].Kind == TKQuote
}

// saveArm saves the stack an arm left, and whether it diverged, and resets
// the diverged flag for the next arm.
func (c *coreChecker) saveArm() savedRun {
	r := c.saveStack()
	r.diverged = c.diverged
	c.diverged = false
	return r
}

// loop checks `(body) loop`. A literal body runs inline: its stack must
// come back to the loop's stack, which is the stack at the loop with ⊥
// opened to new variables, so the body may fill them in (as at a store).
// The loop's stack is shared. Without a reachable break the loop diverges
// (Loop-Forever).
func (c *coreChecker) loop(tok Token) {
	if !c.need(1, tok) {
		return
	}
	top := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	p := c.waiting(top)
	if p == nil {
		// A stored quote cannot break: the loop never ends.
		t := c.subst.Apply(c.arena, top.t)
		n := c.arena.nodes[t]
		if n.Kind != TKQuote || len(c.arena.quoteSigs[n.Extra].Inputs) != 0 ||
			(!c.arena.quoteSigs[n.Extra].Diverges && len(c.arena.quoteSigs[n.Extra].Outputs) != 0) {
			c.mismatch(tok, 0, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{}, Outputs: []TypeId{}}), t)
		}
		c.diverged = true
		return
	}
	p.done = true
	c.uni.Unify(p.t, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{}}))
	c.forceWaiting()
	for i := c.floor; i < len(c.stack); i++ {
		s := &c.stack[i]
		opened := c.openBottom(c.subst.Apply(c.arena, s.t))
		if opened != s.t {
			c.deferCheck(tok, *s, opened)
		}
		s.t = opened
		s.share()
		// The slot holds what the last run left there, so neither a
		// literal key nor where a union came from carries over.
		s.lit, s.origin = NameNone, 0
	}
	mark := len(c.saved)
	loopStack := c.saveStack()
	brk, cont, seen := c.brk, c.cont, c.brkSeen
	c.daLoops = append(c.daLoops, daLoop{mark: len(c.setLog)})
	da := len(c.daLoops)
	c.brk = coreLoopCtx{kind: loopExact, stack: loopStack, da: da}
	c.cont = c.brk
	c.brkSeen = false
	c.walkInline(p.items)
	// After the loop, what every break that left it had set.
	if !c.abandoned {
		l := c.daLoops[da-1]
		c.daJoin(l.mark, l.sets)
	}
	c.daLoops = c.daLoops[:da-1]
	if !c.abandoned && !c.diverged {
		c.forceTop(len(c.stack) - c.floor)
		if !c.stackFits(c.stack, loopStack) {
			want := c.padRun(loopStack)
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
				Hint: "a loop body must leave the stack as it found it: it starts with " +
					c.formatSlots(c.saved[want.start:want.end]) + " and ends with " + c.formatSlots(c.stack)})
		}
	}
	broke := c.brkSeen
	c.brk, c.cont, c.brkSeen = brk, cont, seen
	if c.abandoned {
		c.saved = c.saved[:mark]
		return
	}
	c.restoreStack(loopStack)
	c.saved = c.saved[:mark]
	c.diverged = !broke
}

// breakOrContinue checks break and continue: they need a loop context, and
// in a loop body the stack must be the loop's stack.
func (c *coreChecker) breakOrContinue(tok Token) {
	ctx := c.cont
	if tok.Type == BREAK {
		ctx = c.brk
	}
	switch ctx.kind {
	case loopNone:
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "'" + tok.Lexeme + "' is allowed only in a loop body, or in a literal quote given to each, map or a similar word inside one"})
	case loopExact:
		c.forceTop(len(c.stack) - c.floor)
		if !c.stackFits(c.stack, ctx.stack) {
			want := c.padRun(ctx.stack)
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
				Hint: "'" + tok.Lexeme + "' leaves " + c.formatSlots(c.stack) + ", but the loop's stack is " +
					c.formatSlots(c.saved[want.start:want.end])})
		}
	}
	if tok.Type == BREAK && ctx.kind != loopNone {
		c.brkSeen = true
		if ctx.da > 0 && ctx.da <= len(c.daLoops) {
			l := &c.daLoops[ctx.da-1]
			l.sets = append(l.sets, c.daSince(l.mark))
		}
	}
	c.diverged = true
}

// deferCheck records a checking position to decide once the unit is
// solved: s must fit want then.
func (c *coreChecker) deferCheck(tok Token, s coreSlot, want TypeId) {
	c.deferred = append(c.deferred, coreDeferred{tok: tok, t: s.t, want: want, mark: slotMark(s)})
}

// coreDeferred is a check decided when the unit is solved: a checking
// position (t must fit want), or, when rule is set, that values of type t
// are ones the runtime takes under that rule (TypeCoreGrid.go).
type coreDeferred struct {
	tok     Token
	t, want TypeId
	mark    coreMark
	rule    keyRule
	// validation marks a shared value given to tryAs or `is`, for the
	// error's wording (TypeCoreValidate.go).
	validation bool
}
