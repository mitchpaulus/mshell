package main

// Definite assignment (ai/type-core-calculus.typ, "Variable scopes"): a
// read is accepted only if every path to it stored the variable. Reading
// an unset variable is a checked error at runtime, so this is not needed
// for soundness; it finds the mistake before the program runs.
//
// Each variable of the unit has a flag, set on every path so far, with an
// undo log (setLog) so a branch can be taken back. Stores inside a body
// that runs in place count: an `if` arm, a literal quote given to `x`,
// `iff` or `loop`, list, dict and grid literals, format strings. A branch
// keeps what every arm that goes on set; a loop keeps what every `break`
// that leaves it had set. Stores inside a quote that may run zero times
// (`each`, `map`, a def's quote argument) or later (a stored quote) do not
// count. Reads inside a quote typed on its own are not checked: it runs
// later, when more may be set.

// daLoop collects, for a loop being checked, the variables set at each
// break that leaves it.
type daLoop struct {
	mark int
	sets [][]NameId
}

// daSet records that name is set on this path.
func (c *coreChecker) daSet(name NameId) {
	if v := c.varOf(name); !v.set {
		v.set = true
		c.setLog = append(c.setLog, name)
	}
}

// daRestore takes back everything set since mark.
func (c *coreChecker) daRestore(mark int) {
	for _, name := range c.setLog[mark:] {
		c.vars[name].set = false
	}
	c.setLog = c.setLog[:mark]
}

// daSince returns the variables set since mark.
func (c *coreChecker) daSince(mark int) []NameId {
	return append([]NameId(nil), c.setLog[mark:]...)
}

// daJoin takes back everything set since mark, then sets what every one of
// sets set: the arms of a branch that go on.
func (c *coreChecker) daJoin(mark int, sets [][]NameId) {
	c.daJoinN(mark, len(sets), func(i int) []NameId { return sets[i] })
}

// daSpan is the variables one arm of a branch set: c.daNames[start:end].
type daSpan struct {
	start, end int32
}

// armMark is where a branch's entries start on the scratch stacks.
type armMark struct {
	runs, arms, spans, names int
}

// armBegin marks the scratch stacks for a branch; armEnd truncates them
// back. An arm adds its entries after the nested branches inside it have
// truncated theirs, so a branch's entries are contiguous.
func (c *coreChecker) armBegin() armMark {
	return armMark{runs: len(c.runBuf), arms: len(c.armBuf), spans: len(c.daSpans), names: len(c.daNames)}
}

func (c *coreChecker) armEnd(m armMark) {
	c.runBuf = c.runBuf[:m.runs]
	c.armBuf = c.armBuf[:m.arms]
	c.daSpans = c.daSpans[:m.spans]
	c.daNames = c.daNames[:m.names]
}

func (c *coreChecker) keepRun(r savedRun) {
	c.runBuf = append(c.runBuf, r)
}

// runsSince is the branch's saved arms. Its capacity is cut, so nothing
// appended through it can reach the entries above.
func (c *coreChecker) runsSince(m armMark) []savedRun {
	return c.runBuf[m.runs:len(c.runBuf):len(c.runBuf)]
}

// keepSet records the variables set since mark by an arm that goes on.
func (c *coreChecker) keepSet(mark int) {
	start := len(c.daNames)
	c.daNames = append(c.daNames, c.setLog[mark:]...)
	c.daSpans = append(c.daSpans, daSpan{int32(start), int32(len(c.daNames))})
}

// keepNoSet records an arm that goes on and sets nothing.
func (c *coreChecker) keepNoSet() {
	n := int32(len(c.daNames))
	c.daSpans = append(c.daSpans, daSpan{n, n})
}

// daJoinSince is daJoin over the sets the branch marked m kept.
func (c *coreChecker) daJoinSince(mark int, m armMark) {
	spans := c.daSpans[m.spans:]
	c.daJoinN(mark, len(spans), func(i int) []NameId { return c.daNames[spans[i].start:spans[i].end] })
}

func (c *coreChecker) daJoinN(mark int, n int, set func(int) []NameId) {
	c.daRestore(mark)
	if n == 0 {
		return
	}
	for _, name := range set(0) {
		inAll := true
		for i := 1; i < n; i++ {
			found := false
			for _, x := range set(i) {
				if x == name {
					found = true
					break
				}
			}
			if !found {
				inAll = false
				break
			}
		}
		if inAll {
			c.daSet(name)
		}
	}
}

// daRead records a read of a variable that no path so far has to have set.
// Whether it is an error waits for the end of the unit: a variable stored
// nowhere is reported as unknown instead.
func (c *coreChecker) daRead(tok Token, name NameId, v *coreVar) {
	if v.set || c.later > 0 || v.unsetRead {
		return
	}
	v.unsetRead = true
	c.unsetReads = append(c.unsetReads, coreUnsetRead{tok: tok, name: name})
}

type coreUnsetRead struct {
	tok  Token
	name NameId
}

// finishAssign reports the reads that some path reaches with the variable
// unset, for variables the unit stores somewhere.
func (c *coreChecker) finishAssign() {
	for _, r := range c.unsetReads {
		if v := &c.vars[r.name]; v.gen == c.varGen && v.stored {
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: r.tok,
				Hint: "'" + c.names.Name(r.name) + "' is read here, but a path reaches this read without setting it;" +
					" set it on every path before, for example before the if or loop that sets it"})
		}
	}
}
