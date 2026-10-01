package main

// Pending overload choices (ai/type-system-plan.md, section 5).
//
// When more than one candidate of an overloaded word fits its arguments,
// because an argument is still an unsolved variable, the choice waits: the
// word pushes new variables for its outputs and records its arguments. The
// choice is tried again as the unit goes on, and is made as soon as exactly
// one candidate fits. A candidate that no longer fits never fits again,
// since the substitution only grows, so the order the choices are tried in
// does not matter. A choice with no candidate left is an error at the
// word; one with several left when the unit is solved asks for an
// annotation.

// coreChoice is a waiting overload choice.
type coreChoice struct {
	tok  Token
	sigs []coreSig
	args []coreSlot
	outs []TypeId
	done bool
}

// choose records a waiting choice among sigs for the arguments on the
// stack, or reports an ambiguity when the candidates disagree on their
// stack effect or no argument is unsolved.
func (c *coreChecker) choose(sigs []coreSig, tok Token) {
	var fits []coreSig
	for i := range sigs {
		cp := c.checkpoint()
		if c.argsFit(&sigs[i]) {
			fits = append(fits, sigs[i])
		}
		c.rollback(cp)
	}
	nin, nout, diverges := len(fits[0].ins), len(fits[0].outs), fits[0].diverges
	same := true
	for i := range fits {
		if len(fits[i].ins) != nin || len(fits[i].outs) != nout || fits[i].diverges != diverges {
			same = false
		}
	}
	unsolved := false
	for _, s := range c.stack[len(c.stack)-nin:] {
		if c.hasVars(s.t) {
			unsolved = true
		}
	}
	if !same || !unsolved || diverges {
		c.errs = append(c.errs, TypeError{Kind: TErrAmbiguousTyping, Pos: tok,
			Hint: "more than one signature of '" + tok.Lexeme + "' fits " + c.formatSlots(c.stack[len(c.stack)-nin:]) + "; annotate the value"})
		c.abandoned = true
		return
	}
	c.forceTop(nin)
	ch := coreChoice{tok: tok, sigs: fits, args: append([]coreSlot(nil), c.stack[len(c.stack)-nin:]...)}
	c.stack = c.stack[:len(c.stack)-nin]
	for range nout {
		v := c.subst.FreshVar(c.arena)
		ch.outs = append(ch.outs, v)
		c.push(v, false)
	}
	c.choices = append(c.choices, ch)
	c.choiceVersion = -1
}

// retryChoices makes every waiting choice that now has one candidate
// left, when anything was unified since the last try.
func (c *coreChecker) retryChoices() {
	if len(c.uni.pairs) == c.choiceVersion {
		return
	}
	for changed := true; changed; {
		changed = false
		c.choiceVersion = len(c.uni.pairs)
		for i := range c.choices {
			if c.choices[i].done {
				continue
			}
			ch := &c.choices[i]
			fit, nfit := -1, 0
			for j := range ch.sigs {
				cp := c.checkpoint()
				if c.choiceFits(&ch.sigs[j], ch) {
					fit, nfit = j, nfit+1
				}
				c.rollback(cp)
			}
			switch nfit {
			case 0:
				ch.done = true
				c.errs = append(c.errs, TypeError{Kind: TErrNoMatchingOverload, Pos: ch.tok,
					Hint: "its arguments are " + c.formatSlots(ch.args) + " and its results are used as " +
						c.formatTypes(ch.outs) + "; " + c.formatCandidates(ch.sigs)})
			case 1:
				ch.done = true
				c.choiceFits(&ch.sigs[fit], ch)
				changed = true
			}
		}
	}
}

// choiceFits checks a choice's arguments against sig and unifies its
// output variables with sig's outputs.
func (c *coreChecker) choiceFits(sig *coreSig, ch *coreChoice) bool {
	gens := append([]TypeId(nil), c.instantiate(sig)...)
	ok := true
	c.eachInput(sig, gens, func(i int) coreSlot { return ch.args[i] }, func(i int, want TypeId) {
		if ok && !c.check(ch.args[i], want) {
			ok = false
		}
	})
	if !ok {
		return false
	}
	for j, t := range sig.outs {
		if sig.genOut&(1<<j) != 0 {
			t = c.rel.SubstParams(t, gens)
		}
		if !c.uni.Unify(ch.outs[j], t) {
			return false
		}
	}
	return true
}

// finishChoices reports the choices still waiting when the unit is solved.
// When the candidates left all give the same outputs, nothing after the
// choice can tell them apart, and the first is taken: `[] sortV`.
func (c *coreChecker) finishChoices() {
	c.choiceVersion = -1
	c.retryChoices()
	for i := range c.choices {
		ch := &c.choices[i]
		if ch.done {
			continue
		}
		if c.sameOutputs(ch) {
			ch.done = true
			c.choiceFits(&ch.sigs[0], ch)
			c.choiceVersion = -1
			c.retryChoices()
			continue
		}
		c.errs = append(c.errs, TypeError{Kind: TErrAmbiguousTyping, Pos: ch.tok,
			Hint: "more than one signature of '" + ch.tok.Lexeme + "' fits its arguments " + c.formatSlots(ch.args) +
				"; annotate the quote or value they come from"})
	}
}

// sameOutputs reports whether every candidate of a choice that still fits
// gives the same, fully known, output types.
func (c *coreChecker) sameOutputs(ch *coreChoice) bool {
	var first []TypeId
	n := 0
	for j := range ch.sigs {
		cp := c.checkpoint()
		if c.choiceFits(&ch.sigs[j], ch) {
			outs := make([]TypeId, len(ch.outs))
			for k, o := range ch.outs {
				outs[k] = c.subst.Apply(c.arena, o)
				if c.hasVars(outs[k]) {
					c.rollback(cp)
					return false
				}
			}
			if n == 0 {
				first = outs
			} else {
				for k := range outs {
					if outs[k] != first[k] {
						c.rollback(cp)
						return false
					}
				}
			}
			n++
		}
		c.rollback(cp)
	}
	return n > 0
}
