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
	c.daRestore(mark)
	if len(sets) == 0 {
		return
	}
	for _, name := range sets[0] {
		inAll := true
		for _, s := range sets[1:] {
			found := false
			for _, n := range s {
				if n == name {
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
