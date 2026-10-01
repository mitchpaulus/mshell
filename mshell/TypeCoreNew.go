package main

import "strconv"

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
// For a def that calls itself both marks can be consistent: `[]` in one
// arm and the recursive result in the other. The mark must be the largest
// consistent one, so a body that leaves a shared value at an unmarked
// output is checked once more assuming the mark; if it then leaves a new
// value on every path, with no error, the output is new.

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

// checkNewMarks checks the `new` marks of def's outputs against what its
// body, just checked, leaves at its exits.
func (c *coreChecker) checkNewMarks(def *MShellDefinition, sig *coreSig, outs []TypeId) {
	if c.abandoned || sig.diverges || c.exits == 0 {
		return
	}
	exitNew, exitShared := c.exitNew, append([]Token(nil), c.exitShared...)
	var retry uint64
	for i, out := range outs {
		if i >= 64 || i >= len(def.Outputs) {
			break
		}
		bit := uint64(1) << i
		marked := sig.newOut&bit != 0
		at := def.Outputs[i]
		switch {
		case c.rel.Immutable(c.subst.Apply(c.arena, out)):
			if marked {
				nw := at.(*TypeNewExpr)
				c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: nw.Tok, Name: def.Name,
					Hint: "output " + strconv.Itoa(i) + " is " + c.format(out) + ", which holds no list, dict or grid," +
						" so `new` on it means nothing; remove it",
					Fix: TypeFix{Kind: FixDelete, Title: "Remove `new`", At: nw.Tok, Until: nw.Inner.GetStartToken()}})
			}
		case marked && exitNew&bit == 0:
			nw := at.(*TypeNewExpr)
			where := "at its end"
			if tok := exitShared[i]; tok.Line > 0 {
				where = "at the `return` on line " + strconv.Itoa(tok.Line)
			}
			c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: nw.Tok, Name: def.Name,
				Hint: "output " + strconv.Itoa(i) + " is marked `new`, but the value the body leaves there " + where +
					" is not new: something else may refer to it (a variable, a container, or a copy from dup);" +
					" return a value made in the body without storing it, or deepCopy it, or remove `new`",
				Fix: TypeFix{Kind: FixDelete, Title: "Remove `new`", At: nw.Tok, Until: nw.Inner.GetStartToken()}})
		case !marked && exitNew&bit != 0:
			c.errs = append(c.errs, c.markNewError(def, i))
		case !marked && c.selfCalled:
			retry |= bit
		}
	}
	if retry == 0 {
		return
	}
	// The largest consistent marks: assume the unmarked outputs are new.
	saved, nerr := sig.newOut, len(c.errs)
	sig.newOut |= retry
	c.checkBody(def, sig)
	consistent := len(c.errs) == nerr && !c.abandoned
	c.errs = c.errs[:nerr]
	sig.newOut = saved
	if !consistent {
		return
	}
	for i := range outs {
		if bit := uint64(1) << i; retry&bit != 0 && c.exitNew&bit != 0 {
			c.errs = append(c.errs, c.markNewError(def, i))
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
