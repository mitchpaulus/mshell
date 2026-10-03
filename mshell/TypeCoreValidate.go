package main

// Validation in the core checker: `tryAs T` and the match pattern `is T x`
// (ai/type-core-calculus.typ, "Validation: tryAs and is"; tw_try_dp,
// tw_try_sub, tw_try_imm in formal-ver/Typing.v).
//
// The runtime validates the value in place and never copies it, so what
// the checker allows depends on whether the value is referenced elsewhere:
//
//   - a fresh value may be validated against any type: nothing else sees
//     it at its old type;
//   - a shared value only against a type it is already below, or one with
//     no list or dict in it (immutable), where the runtime's answer is
//     right for any value.
//
// Anything else would give a shared object a second type (R6), and the
// error suggests deepCopy. The target must be checkable: no quote, no type
// variable, no abstract type, and no enum that holds a quote.

// validationTarget resolves the type of a tryAs or `is` pattern and checks
// that values can be validated against it. ok is false after an error.
func (c *coreChecker) validationTarget(item MShellParseItem, tok Token) (TypeId, bool) {
	if name, ok := c.genericIn(item); ok {
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "'" + name + "' is a generic of this def, and types are not kept at run time, so there is nothing to validate against"})
		return TidNothing, false
	}
	target := c.res.resolveType(item)
	c.takeResolveErrors()
	if target == TidNothing {
		return TidNothing, false
	}
	if !c.rel.Checkable(target) {
		reason := c.uncheckableReason(target, nil)
		if reason == "" {
			reason = "it holds a type that is not known at run time"
		}
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "values cannot be validated against " + c.format(target) + ": " + reason})
		return TidNothing, false
	}
	return target, true
}

// genericIn finds a name in a type expression that is a generic of the def
// being checked.
func (c *coreChecker) genericIn(item MShellParseItem) (string, bool) {
	if c.curDef == nil || len(c.curDef.gens) == 0 {
		return "", false
	}
	var found string
	var walk func(it MShellParseItem)
	walk = func(it MShellParseItem) {
		if found != "" || it == nil {
			return
		}
		switch n := it.(type) {
		case *TypeNamed:
			if id, ok := c.names.Lookup(n.Name); ok && len(n.Args) == 0 {
				for _, g := range c.curDef.gens {
					if g == id {
						if _, alias := c.res.aliases[id]; !alias {
							found = n.Name
						}
					}
				}
			}
			for _, a := range n.Args {
				walk(a)
			}
		case *TypeListExpr:
			walk(n.Elem)
		case *TypeDictExpr:
			walk(n.Value)
		case *TypeShapeExpr:
			for _, f := range n.Fields {
				walk(f.Type)
			}
			walk(n.Wildcard)
		case *TypeUnionExpr:
			for _, a := range n.Arms {
				walk(a)
			}
		case *TypeQuoteExpr:
			for _, a := range n.Inputs {
				walk(a)
			}
			for _, a := range n.Outputs {
				walk(a)
			}
		}
	}
	walk(item)
	return found, found != ""
}

// uncheckableReason says why values cannot be validated against t.
func (c *coreChecker) uncheckableReason(t TypeId, visiting []TypeId) string {
	ar := c.arena
	for _, v := range visiting {
		if v == t {
			return ""
		}
	}
	n := ar.nodes[t]
	switch n.Kind {
	case TKQuote:
		return "a quotation cannot be looked inside, so no value is known to be a " + c.format(t)
	case TKEnum:
		if decl := ar.enumDecls[n.A]; !decl.Checkable {
			name := c.names.Name(decl.Name)
			for _, ctor := range decl.Ctors {
				for _, p := range ctor.Payload {
					if c.uncheckableReason(p, append(visiting, t)) != "" {
						return "the enum " + name + " holds a quotation, which cannot be looked inside"
					}
				}
			}
			return "the declaration of the enum " + name + " has an error"
		}
		for _, x := range ar.enumArgs[n.Extra] {
			if r := c.uncheckableReason(x, visiting); r != "" {
				return r
			}
		}
	case TKGrid, TKGridView, TKGridRow:
		return "a grid's columns cannot be written as a type, so there is nothing to validate them against"
	case TKList:
		return c.uncheckableReason(TypeId(n.A), visiting)
	case TKRecord:
		rec := ar.records[n.Extra]
		for _, f := range append(rec.Fields, rec.Rest) {
			if f.Type != TidNothing {
				if r := c.uncheckableReason(f.Type, visiting); r != "" {
					return r
				}
			}
		}
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			if r := c.uncheckableReason(m, visiting); r != "" {
				return r
			}
		}
	case TKAlias:
		if ar.aliases[n.A].Body == TidNothing {
			return "the declaration of " + c.names.Name(ar.aliases[n.A].Name) + " has an error"
		}
		return c.uncheckableReason(ar.aliases[n.A].Body, append(visiting, t))
	case TKParam:
		// An enum's parameter, given by its arguments.
	case TKVar, TKRigid, TKAbstract:
		return "its type " + c.format(t) + " is not known at run time"
	}
	return ""
}

// validates checks that the value in slot s may be validated in place
// against target, and reports whether the value stays new.
func (c *coreChecker) validates(s coreSlot, target TypeId, tok Token) bool {
	if s.fresh && s.part == 0 {
		return true
	}
	if c.rel.Immutable(target) {
		return false
	}
	// A partly new value is committed first, and is then shared.
	shared := coreSlot{t: s.t}
	if c.hasVars(s.t) {
		// Checked once the unit is solved; nothing here fixes s.t.
		c.deferCheck(tok, shared, target)
		c.deferred[len(c.deferred)-1].validation = true
		return false
	}
	got := c.subst.Apply(c.arena, s.t)
	if !c.rel.Sub(got, target) {
		c.validationError(tok, got, target)
	}
	return false
}

// validationError reports a shared value validated against a type it is not
// below.
func (c *coreChecker) validationError(tok Token, got, target TypeId) {
	hint := "this value may be referenced elsewhere (stored, or a def's input), so it can be validated in place only against a type it already has, " +
		"or one with no list or dict in it: " + c.format(got) + " is not below " + c.format(target)
	if k := c.kindPatternHint(got, target); k != "" {
		hint += ". " + k
	} else {
		hint += ". Validate the value where it is made (`parseJson tryAs T ?`), or validate a copy (`deepCopy tryAs T`)"
	}
	c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: hint})
}

// kindPatternHint suggests a kind pattern when every member of got of a
// kind target has is already below target: only those members could pass,
// and a kind pattern gives them at their own type with no validation
// (ai/type-system-plan.md, question 7, decided 2026-10-01). It is "" when
// that does not hold.
func (c *coreChecker) kindPatternHint(got, target TypeId) string {
	ks, ok := c.rel.Kinds(target)
	if !ok {
		return ""
	}
	var ms []TypeId
	if !c.members(got, &ms) {
		return ""
	}
	var relevant []TypeId
	for _, m := range ms {
		k, ok := c.rel.kindOf(m)
		if !ok {
			return ""
		}
		for _, tk := range ks {
			if tk == k {
				relevant = append(relevant, m)
				break
			}
		}
	}
	if len(relevant) == 0 {
		return ""
	}
	for _, m := range relevant {
		if !c.rel.Sub(m, target) {
			return ""
		}
	}
	if len(relevant) == 1 {
		k, _ := c.rel.kindOf(relevant[0])
		if word := c.kindWord(k); word != "" {
			return "Only its " + c.format(relevant[0]) + " member could pass, and that member already is a " + c.format(target) +
				": take it out with the kind pattern `" + word + " name` instead, which needs no validation"
		}
	}
	return "Only its members of the kinds " + c.format(target) + " has could pass, and those are already below it: " +
		"take them out with kind patterns (`list xs`, `dict d`, ...) instead, which need no validation"
}

// kindWord is the match pattern word for a kind, or "".
func (c *coreChecker) kindWord(k valueKind) string {
	switch k.code {
	case uint32(TidInt):
		return "int"
	case uint32(TidFloat):
		return "float"
	case uint32(TidStr):
		return "str"
	case uint32(TidBool):
		return "bool"
	case uint32(TidPath):
		return "path"
	case uint32(TidDateTime):
		return "datetime"
	case uint32(TidBytes):
		return "binary"
	case uint32(TidNull):
		return "null"
	case kindList:
		return "list"
	case kindDict:
		return "dict"
	case kindQuote:
		return "quotation"
	case kindEnum:
		if k.enum == EnumMaybe {
			return "maybe"
		}
		return c.names.Name(c.arena.EnumDecl(k.enum).Name)
	}
	return ""
}

// tryAs checks `tryAs T`: the value becomes `Maybe[T]`, new when the value
// was.
func (c *coreChecker) tryAs(t *MShellTryAs) {
	c.at = t.Tok
	target, ok := c.validationTarget(t.Target, t.Tok)
	if !c.need(1, t.Tok) {
		return
	}
	c.forceTop(1)
	s := c.stack[len(c.stack)-1]
	fresh := false
	if ok {
		fresh = c.validates(s, target, t.Tok)
	} else {
		// Keep checking with a target that nothing else depends on.
		target = TidBottom
	}
	c.stack[len(c.stack)-1] = coreSlot{t: c.arena.MakeMaybeEnum(target), fresh: fresh}
}

// isCovers reports whether an `is T` arm covers the type m: m and T are
// equivalent (design doc, "Patterns and validation"; T is checkable, since
// the arm checked).
func (c *coreChecker) isCovers(target, m TypeId) bool {
	m = c.subst.Apply(c.arena, m)
	return !c.hasVars(m) && c.rel.Sub(m, target) && c.rel.Sub(target, m)
}
