package main

import (
	"slices"
	"strconv"
)

// `new` on def outputs (ai/type-core-calculus.typ, "New def outputs").
//
// A def output marked `new` is fresh for its callers, so
// `loadConfig tryAs Config ?` validates in place. The mark must say exactly
// what the body does: written but the body leaves a shared value, or
// missing but every path leaves a new one, is an error at the output, with
// a fix the LSP offers. `new` on an output whose type holds no list, dict
// or grid means nothing, and is an error too. A type variable counts as
// mutable: `(a -- new a)` is deepCopy's type.
//
// For defs that call each other (or one that calls itself) more than one
// set of marks can be consistent: `[]` in one arm and the recursive result
// in the other. The marks must be the largest consistent set, so the
// unmarked outputs of such a group whose bodies leave a shared value are
// assumed new, the group's bodies are checked again, any output still not
// new on every path is dropped from the assumption, and so on until
// nothing changes; the outputs left are errors: "mark it `new`".

// exit records the stack at an exit of a def body (its end, or a return)
// whose outputs were just checked: which outputs are new there. tok is the
// return, or the zero token for the end of the body.
func (c *coreChecker) exit(tok Token) {
	if c.curDef == nil {
		return
	}
	c.exits++
	for i := range c.retOuts {
		if i >= 64 || i >= len(c.stack) {
			break
		}
		if s := c.stack[i]; !s.fresh || s.part != 0 {
			if c.exitNew&(1<<i) != 0 {
				c.exitShared[i] = tok
			}
			c.exitNew &^= 1 << i
		}
	}
}

// coreDefBody is what checking one def's body found, for its marks.
type coreDefBody struct {
	def        *MShellDefinition
	sig        *coreSig
	outs       []TypeId
	exitNew    uint64
	exitShared []Token
	// checked is whether the body was checked to an exit, so its exits
	// say something.
	checked bool
	// calls are the file's defs the body calls, by index.
	calls []int
}

// checkDefs checks each def body once, with its generics rigid, then the
// `new` marks on the outputs, one group of defs that call each other at
// a time.
func (c *coreChecker) checkDefs(defs []MShellDefinition) {
	index := make(map[*coreSig]int, len(defs))
	bodies := make([]coreDefBody, len(defs))
	for i := range defs {
		sig := c.defs[c.names.Intern(defs[i].Name)]
		if _, dup := index[sig]; !dup {
			index[sig] = i
		}
		bodies[i].def, bodies[i].sig = &defs[i], sig
	}
	for i := range bodies {
		b := &bodies[i]
		if b.sig.broken {
			continue
		}
		b.outs = c.checkBody(b.def, b.sig)
		b.exitNew, b.exitShared = c.exitNew, append([]Token(nil), c.exitShared...)
		b.checked = !c.abandoned && !b.sig.diverges && c.exits > 0
		for _, s := range c.calls {
			if j, ok := index[s]; ok && !slices.Contains(b.calls, j) {
				b.calls = append(b.calls, j)
			}
		}
	}
	for _, group := range defGroups(bodies) {
		c.checkNewMarks(bodies, group)
	}
}

// defGroups are the strongly connected components of the call graph of
// bodies (Tarjan), each a group of defs that call each other.
func defGroups(bodies []coreDefBody) [][]int {
	idx := make([]int, len(bodies))
	low := make([]int, len(bodies))
	on := make([]bool, len(bodies))
	for i := range idx {
		idx[i] = -1
	}
	var stack []int
	var groups [][]int
	next := 0
	var visit func(v int)
	visit = func(v int) {
		idx[v], low[v] = next, next
		next++
		stack = append(stack, v)
		on[v] = true
		for _, w := range bodies[v].calls {
			if idx[w] < 0 {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if on[w] {
				low[v] = min(low[v], idx[w])
			}
		}
		if low[v] == idx[v] {
			var g []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				g = append(g, w)
				if w == v {
					break
				}
			}
			slices.Sort(g)
			groups = append(groups, g)
		}
	}
	for v := range bodies {
		if idx[v] < 0 {
			visit(v)
		}
	}
	return groups
}

// checkNewMarks checks the `new` marks of a group of defs against what
// their bodies, just checked, leave at their exits.
func (c *coreChecker) checkNewMarks(bodies []coreDefBody, group []int) {
	recursive := len(group) > 1 || slices.Contains(bodies[group[0]].calls, group[0])
	retry := make([]uint64, len(group))
	any := false
	for gi, i := range group {
		b := &bodies[i]
		if !b.checked {
			continue
		}
		def, sig := b.def, b.sig
		for j, out := range b.outs {
			if j >= 64 || j >= len(def.Outputs) {
				break
			}
			bit := uint64(1) << j
			marked := sig.newOut&bit != 0
			at := def.Outputs[j]
			switch {
			case c.rel.Immutable(c.subst.Apply(c.arena, out)):
				if marked {
					nw := at.(*TypeNewExpr)
					c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: nw.Tok, Name: def.Name,
						Hint: "output " + strconv.Itoa(j) + " is " + c.format(out) + ", which holds no list, dict or grid," +
							" so `new` on it means nothing; remove it",
						Fix: TypeFix{Kind: FixDelete, Title: "Remove `new`", At: nw.Tok, Until: nw.Inner.GetStartToken()}})
				}
			case marked && b.exitNew&bit == 0:
				nw := at.(*TypeNewExpr)
				where := "at its end"
				if tok := b.exitShared[j]; tok.Line > 0 {
					where = "at the `return` on line " + strconv.Itoa(tok.Line)
				}
				c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: nw.Tok, Name: def.Name,
					Hint: "output " + strconv.Itoa(j) + " is marked `new`, but the value the body leaves there " + where +
						" is not new: something else may refer to it (a variable, a container, or a copy from dup);" +
						" return a value made in the body without storing it, or deepCopy it, or remove `new`",
					Fix: TypeFix{Kind: FixDelete, Title: "Remove `new`", At: nw.Tok, Until: nw.Inner.GetStartToken()}})
			case !marked && b.exitNew&bit != 0:
				c.errs = append(c.errs, c.markNewError(def, j))
			case !marked && recursive:
				retry[gi] |= bit
				any = true
			}
		}
	}
	if !any {
		return
	}
	// The largest consistent marks: assume the outputs in retry are new,
	// check the group again, and drop each that is then still shared.
	saved := make([]uint64, len(group))
	for gi, i := range group {
		saved[gi] = bodies[i].sig.newOut
	}
	nerr := len(c.errs)
	for {
		for gi, i := range group {
			bodies[i].sig.newOut = saved[gi] | retry[gi]
		}
		consistent, changed, left := true, false, false
		for gi, i := range group {
			c.checkBody(bodies[i].def, bodies[i].sig)
			if c.abandoned {
				consistent = false
			}
			if kept := retry[gi] & c.exitNew; kept != retry[gi] {
				retry[gi], changed = kept, true
			}
			left = left || retry[gi] != 0
		}
		consistent = consistent && len(c.errs) == nerr
		c.errs = c.errs[:nerr]
		for gi, i := range group {
			bodies[i].sig.newOut = saved[gi]
		}
		if !consistent || !left {
			return
		}
		if !changed {
			break
		}
	}
	for gi, i := range group {
		for j := range bodies[i].outs {
			if j < 64 && retry[gi]&(1<<j) != 0 {
				c.errs = append(c.errs, c.markNewError(bodies[i].def, j))
			}
		}
	}
}

// markNewError is the error for an output whose value is new on every
// path, but which is not marked `new`.
func (c *coreChecker) markNewError(def *MShellDefinition, i int) TypeError {
	at := def.Outputs[i].GetStartToken()
	return TypeError{Kind: TErrDefBodyMismatch, Pos: at, Name: def.Name,
		Hint: "output " + strconv.Itoa(i) + " is a new value on every path through the body; mark it `new`," +
			" so callers get it as new and can change its type in place",
		Fix: TypeFix{Kind: FixInsert, Title: "Mark this output `new`", At: at, Text: "new "}}
}
