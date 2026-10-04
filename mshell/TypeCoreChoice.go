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
	// watch, for a choice an earlier REPL line left open, is the unsolved
	// variables it mentions: it is tried again only once one of them is
	// bound (TypeCoreSession.go).
	watch []TypeVarId
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
	if same && !unsolved && sameEffect(fits) {
		// Every candidate that fits gives the same outputs, so nothing after
		// the word can tell them apart: any is a valid derivation, and the
		// first is taken (design doc, "Checking positions"). A new list
		// literal given to zipPack fits [str] and [PackEntry].
		c.apply(&fits[0], tok)
		return
	}
	if !unsolved {
		// Several fit with different outputs: take the one the arguments
		// fit as they are, without giving a new value a wider type, when
		// there is exactly one. `[1 2] uniq` fits `[int]` as it is, and the
		// mixed form only once widened.
		if sig := c.fitAsIs(fits); sig != nil {
			c.apply(sig, tok)
			return
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

// fitAsIs returns the one candidate of fits that the arguments fit with
// every value taken as it is (by <=, as a stored value would be), or nil
// when no candidate or more than one does.
func (c *coreChecker) fitAsIs(fits []coreSig) *coreSig {
	top := c.topSlots(fits)
	saved := append([]coreSlot(nil), top...)
	for i := range top {
		top[i].fresh, top[i].part = false, 0
	}
	var found *coreSig
	n := 0
	for i := range fits {
		cp := c.checkpoint()
		if c.argsFit(&fits[i]) {
			found, n = &fits[i], n+1
		}
		c.rollback(cp)
	}
	copy(top, saved)
	if n != 1 {
		return nil
	}
	return found
}

// sameEffect reports whether the candidates give the same outputs: the
// same types, with no generics, and the same freshness.
func sameEffect(sigs []coreSig) bool {
	f := &sigs[0]
	for i := range sigs {
		s := &sigs[i]
		if len(s.outs) != len(f.outs) || s.diverges != f.diverges || s.genOut != 0 ||
			s.newOut != f.newOut || s.newListOut != f.newListOut || s.keepOut != f.keepOut {
			return false
		}
		for j := range s.outs {
			if s.outs[j] != f.outs[j] {
				return false
			}
		}
	}
	return true
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
			if c.choices[i].done || i < c.keptChoices && !c.anyBound(c.choices[i].watch) {
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
				c.errs = append(c.errs, TypeError{Kind: TErrNoMatchingOverload, Pos: ch.tok, Earlier: i < c.keptChoices,
					Hint: "its arguments are " + c.formatSlots(ch.args) + " and its results are used as " +
						c.formatTypes(ch.outs) + "; " + c.formatCandidates(ch.sigs)})
			case 1:
				c.commitChoice(ch, fit)
				changed = true
			}
		}
	}
}

// anyBound reports whether any of vs is bound.
func (c *coreChecker) anyBound(vs []TypeVarId) bool {
	for _, v := range vs {
		if c.subst.bound[v] != TidNothing {
			return true
		}
	}
	return false
}

// commitChoice makes ch with its candidate j, which a trial just found
// fits. The unifier records only the pairs it unified, and a candidate that
// failed part way may have bound variables first, so the recheck alone
// would find such bindings consistent: it checks the constraints made, not
// that every one was. So the choice also records the candidate's whole
// typing as checks of their own, made with the final substitution by the
// proved relations however the trial went: each argument must fit its
// parameter, by <= or fresh retyping, and each output must equal the
// candidate's. A choice that breaks one is a checker error, never a typing
// (design doc, "Unification and subtyping, kept apart").
func (c *coreChecker) commitChoice(ch *coreChoice, j int) {
	ch.done = true
	sig := &ch.sigs[j]
	gens, mark := c.instantiate(sig)
	defer c.releaseGens(mark)
	cp := c.checkpoint()
	if !c.choiceFitsWith(sig, ch, gens) {
		c.rollback(cp)
		c.errs = append(c.errs, TypeError{Kind: TErrCoreInternal, Pos: ch.tok,
			Hint: "the signature chosen for '" + ch.tok.Lexeme + "' does not fit its arguments " + c.formatSlots(ch.args)})
		return
	}
	for i, want := range sig.ins {
		if sig.genIn&genBit(i) != 0 {
			want = c.rel.SubstParams(want, gens)
		}
		c.deferred = append(c.deferred, coreDeferred{tok: ch.tok, t: ch.args[i].t, want: want, mark: slotMark(ch.args[i]), choice: true})
	}
	for k, t := range sig.outs {
		if sig.genOut&genBit(k) != 0 {
			t = c.rel.SubstParams(t, gens)
		}
		c.uni.Require(ch.outs[k], t)
	}
}

// choiceFits checks a choice's arguments against sig and unifies its
// output variables with sig's outputs. On failure the substitution may be
// partly changed, so a caller rolls back.
func (c *coreChecker) choiceFits(sig *coreSig, ch *coreChoice) bool {
	gens, mark := c.instantiate(sig)
	defer c.releaseGens(mark)
	return c.choiceFitsWith(sig, ch, gens)
}

// choiceFitsWith is choiceFits with sig's generics instantiated as gens.
func (c *coreChecker) choiceFitsWith(sig *coreSig, ch *coreChoice, gens []TypeId) bool {
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
		if sig.genOut&genBit(j) != 0 {
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
		if j := c.sameOutputs(ch); j >= 0 {
			c.commitChoice(ch, j)
			c.choiceVersion = -1
			c.retryChoices()
			continue
		}
		c.errs = append(c.errs, TypeError{Kind: TErrAmbiguousTyping, Pos: ch.tok,
			Hint: "more than one signature of '" + ch.tok.Lexeme + "' fits its arguments " + c.formatSlots(ch.args) +
				"; annotate the quote or value they come from"})
	}
}

// sameOutputs returns the first candidate of a choice that still fits
// when every one that fits gives the same, fully known, output types, and
// -1 otherwise.
func (c *coreChecker) sameOutputs(ch *coreChoice) int {
	var first []TypeId
	firstFit := -1
	for j := range ch.sigs {
		cp := c.checkpoint()
		if c.choiceFits(&ch.sigs[j], ch) {
			outs := make([]TypeId, len(ch.outs))
			for k, o := range ch.outs {
				outs[k] = c.subst.Apply(c.arena, o)
				if c.hasVars(outs[k]) {
					c.rollback(cp)
					return -1
				}
			}
			if firstFit < 0 {
				first, firstFit = outs, j
			} else {
				for k := range outs {
					if outs[k] != first[k] {
						c.rollback(cp)
						return -1
					}
				}
			}
		}
		c.rollback(cp)
	}
	return firstFit
}
