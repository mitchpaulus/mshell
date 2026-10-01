package main

import "strings"

// match in the core checker (ai/type-core-calculus.typ, "Unknown contents
// are abstract types", "Enums", "Patterns and validation").
//
// The subject is the top of the stack; an arm written `:` consumes it, one
// written `:>` leaves it, at the type the pattern found. Each arm is checked
// from the stack at the match and the arms are joined, as for an if.
//
// A kind pattern (`int n`, `list xs`) on a union gives the member of that
// kind, at its own type, writable. On a value whose contents are not known
// (unknown, an abstract type, a rigid generic) a scalar kind gives that base
// type, `dict` gives the read-only view of any dict, and `list` gives a
// list of a new abstract type, which may not escape the arm: it may not
// appear in any variable's type, the stack below the subject, the arm's
// output, or a loop's stack (the escape check, `kind_list_once` in
// Escape.v). Such a list cannot be bound to a name; `list :>` keeps it on
// the stack.
//
// Bindings are stores into the enclosing scope, so a name bound in two
// arms has one type.
//
// A match must cover every member of its subject's type, or have a `_` arm.

// coreEscape is an abstract type that must not escape its arm.
type coreEscape struct {
	k     TypeId
	tok   Token
	types []TypeId
}

// patternKinds maps a kind keyword in a pattern to the runtime kind it
// tests.
func (c *coreChecker) patternKind(tok Token) (valueKind, bool) {
	switch tok.Type {
	case TYPEINT:
		return valueKind{code: uint32(TidInt)}, true
	case TYPEFLOAT:
		return valueKind{code: uint32(TidFloat)}, true
	case STR:
		return valueKind{code: uint32(TidStr)}, true
	case TYPEBOOL:
		return valueKind{code: uint32(TidBool)}, true
	case LITERAL:
		switch tok.Lexeme {
		case "list":
			return valueKind{code: kindList}, true
		case "dict":
			return valueKind{code: kindDict}, true
		case "path":
			return valueKind{code: uint32(TidPath)}, true
		case "date":
			return valueKind{code: uint32(TidDateTime)}, true
		case "quotation":
			return valueKind{code: kindQuote}, true
		case "maybe":
			return valueKind{code: kindEnum, enum: EnumMaybe}, true
		case "binary":
			return valueKind{code: uint32(TidBytes)}, true
		}
	}
	return valueKind{}, false
}

// unknownContents reports whether t is a type whose contents a kind
// pattern does not know: unknown, an abstract type, a rigid generic.
func (c *coreChecker) unknownContents(t TypeId) bool {
	if t == TidUnknown {
		return true
	}
	switch c.arena.nodes[t].Kind {
	case TKAbstract, TKRigid:
		return true
	}
	return false
}

// unfold looks through aliases at the top of t.
func (c *coreChecker) unfold(t TypeId) TypeId {
	for i := 0; c.arena.nodes[t].Kind == TKAlias && i < 64; i++ {
		t = c.arena.aliases[c.arena.nodes[t].A].Body
	}
	return t
}

// memberOfKind finds the member of t of kind k. found is false when t has
// none; unknown is true when t, or a member, has unknown contents.
func (c *coreChecker) memberOfKind(t TypeId, k valueKind) (m TypeId, found, unknown bool) {
	t = c.unfold(t)
	if c.unknownContents(t) {
		return TidNothing, false, true
	}
	if c.arena.nodes[t].Kind == TKUnion {
		for _, x := range c.arena.unionMembers[c.arena.nodes[t].Extra] {
			if m, found, unknown = c.memberOfKind(x, k); found || unknown {
				return
			}
		}
		return TidNothing, false, false
	}
	if kk, ok := c.rel.kindOf(t); ok && kk == k {
		return t, true, false
	}
	return TidNothing, false, false
}

// unknownOfKind is the type a kind pattern gives a value of unknown
// contents, and the abstract type it introduces (or TidNothing).
func (c *coreChecker) unknownOfKind(k valueKind) (t, abstract TypeId) {
	switch k.code {
	case kindList:
		a := c.arena.MakeAbstract()
		return c.arena.MakeList(a), a
	case kindDict:
		return c.arena.MakeRecord(nil, RecordField{Status: FieldOpen}), TidNothing
	case kindEnum:
		a := c.arena.MakeAbstract()
		return c.arena.MakeMaybeEnum(a), a
	case kindQuote:
		return TidNothing, TidNothing
	}
	return TypeId(k.code), TidNothing
}

// coreArm is what a pattern says about the subject in its arm.
type coreArm struct {
	subject  TypeId // the subject's type in the arm (TidBottom: never matches)
	abstract TypeId // an abstract type the arm introduces, or TidNothing
	binds    []coreBinding
	all      bool       // matches every value
	kind     valueKind  // the kind it covers, when kindOK
	kindOK   bool
	maybe    int8 // 1: just, 2: none
	boolLit  int8 // 1: true, 2: false
	// listLen is the length a list pattern matches, or, with listRest, the
	// least length; -1 when the arm is not a list pattern.
	listLen  int
	listRest bool
}

type coreBinding struct {
	tok Token
	t   TypeId
}

func (c *coreChecker) matchBlock(m *MShellParseMatchBlock) {
	tok := m.StartToken
	if !c.need(1, tok) {
		return
	}
	c.forceTop(1)
	subj := c.stack[len(c.stack)-1]
	t := c.subst.Apply(c.arena, subj.t)
	if c.arena.nodes[t].Kind == TKVar {
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "the type of the value being matched is not known here (" + c.format(t) + "); annotate it, for example with a def signature"})
		c.abandoned = true
		return
	}
	mark := len(c.saved)
	entry := c.saveStack()
	var runs []savedRun
	var arms []coreArm
	for _, arm := range m.Arms {
		c.restoreStack(entry)
		a, ok := c.analyzePattern(arm.Pattern, t, tok)
		if !ok {
			c.saved = c.saved[:mark]
			return
		}
		arms = append(arms, a)
		below := len(c.stack) - 1
		if arm.Consume {
			c.stack = c.stack[:below]
		} else {
			c.stack[below].t = a.subject
		}
		for _, b := range a.binds {
			if c.mentionsAbstract(b.t) {
				c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: b.tok,
					Hint: "'" + b.tok.Lexeme + "' would have a type known only inside this arm (" + c.format(b.t) +
						"); keep the value on the stack with `:>` instead of binding it, or narrow it first with tryAs"})
				continue
			}
			c.push(b.t, false)
			c.store(b.tok, c.names.Intern(b.tok.Lexeme))
		}
		c.walk(arm.Body)
		if c.abandoned {
			c.saved = c.saved[:mark]
			return
		}
		if a.abstract != TidNothing {
			c.recordEscape(a.abstract, tok, entry, below)
		}
		runs = append(runs, c.saveArm())
	}
	if !m.Assertive && !c.exhaustive(arms, t) {
		c.errs = append(c.errs, TypeError{Kind: TErrNonExhaustiveMatch, Pos: tok,
			Hint: "the arms do not cover every " + c.format(t) + "; add the missing cases or a `_` arm"})
	}
	c.joinArms(runs, tok)
	c.saved = c.saved[:mark]
}

// recordEscape records the escape check for an abstract type introduced by
// an arm: the stack below the subject, the arm's output and the loop stacks
// must not mention it once the unit is solved; nor may any variable.
func (c *coreChecker) recordEscape(k TypeId, tok Token, entry savedRun, below int) {
	e := coreEscape{k: k, tok: tok}
	for _, s := range c.saved[entry.start : entry.start+below] {
		e.types = append(e.types, s.t)
	}
	if !c.diverged {
		for _, s := range c.stack {
			e.types = append(e.types, s.t)
		}
	}
	for _, ctx := range []coreLoopCtx{c.brk, c.cont} {
		if ctx.kind == loopExact {
			for _, s := range c.saved[ctx.stack.start:ctx.stack.end] {
				e.types = append(e.types, s.t)
			}
		}
	}
	c.escapes = append(c.escapes, e)
}

// finishEscapes makes the escape checks with the final substitution.
func (c *coreChecker) finishEscapes() {
	for _, e := range c.escapes {
		escaped := false
		for _, t := range e.types {
			if c.mentionsType(c.subst.Apply(c.arena, t), e.k) {
				escaped = true
			}
		}
		for _, name := range c.unitVars {
			if c.mentionsType(c.subst.Apply(c.arena, c.vars[name].t), e.k) {
				escaped = true
			}
		}
		if escaped {
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: e.tok,
				Hint: "a value whose element type is known only inside this match arm leaves the arm; " +
					"narrow it with tryAs first, or keep it inside the arm"})
		}
	}
}

// mentionsType reports whether t mentions the type k.
func (c *coreChecker) mentionsType(t, k TypeId) bool {
	if t == k {
		return true
	}
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKList, TKCommand:
		return c.mentionsType(TypeId(n.A), k)
	case TKRecord:
		rec := c.arena.records[n.Extra]
		for _, f := range rec.Fields {
			if f.Type != TidNothing && c.mentionsType(f.Type, k) {
				return true
			}
		}
		return rec.Rest.Type != TidNothing && c.mentionsType(rec.Rest.Type, k)
	case TKUnion:
		for _, m := range c.arena.unionMembers[n.Extra] {
			if c.mentionsType(m, k) {
				return true
			}
		}
	case TKQuote:
		sig := c.arena.quoteSigs[n.Extra]
		for _, x := range sig.Inputs {
			if c.mentionsType(x, k) {
				return true
			}
		}
		for _, x := range sig.Outputs {
			if c.mentionsType(x, k) {
				return true
			}
		}
	case TKEnum:
		for _, x := range c.arena.enumArgs[n.Extra] {
			if c.mentionsType(x, k) {
				return true
			}
		}
	}
	return false
}

// mentionsAbstract reports whether t, as solved so far, mentions an
// abstract type.
func (c *coreChecker) mentionsAbstract(t TypeId) bool {
	return typeMentions(c.arena, c.subst.Apply(c.arena, t), TKAbstract)
}

// analyzePattern reads one arm's pattern against a subject of type t.
func (c *coreChecker) analyzePattern(pattern []MShellParseItem, t TypeId, at Token) (coreArm, bool) {
	a := coreArm{subject: t, abstract: TidNothing, listLen: -1}
	if len(pattern) == 2 {
		first, ok1 := pattern[0].(Token)
		second, ok2 := pattern[1].(Token)
		if ok1 && ok2 && second.Type == LITERAL {
			if first.Type == LITERAL && first.Lexeme == "just" {
				a.maybe = 1
				a.kind, a.kindOK = valueKind{code: kindEnum, enum: EnumMaybe}, false
				payload := c.narrowKind(&a, valueKind{code: kindEnum, enum: EnumMaybe}, first)
				if payload != TidBottom {
					payload = c.arena.enumArgs[c.arena.nodes[payload].Extra][0]
				}
				c.bind(&a, second, payload)
				return a, true
			}
			if k, ok := c.patternKind(first); ok {
				a.kind, a.kindOK = k, true
				c.bind(&a, second, c.narrowKind(&a, k, first))
				return a, true
			}
		}
	}
	if len(pattern) != 1 {
		return c.badPattern(at, "")
	}
	switch p := pattern[0].(type) {
	case Token:
		switch {
		case p.Type == LITERAL && p.Lexeme == "_":
			a.all = true
		case p.Type == LITERAL && p.Lexeme == "none":
			a.maybe = 2
			c.narrowKind(&a, valueKind{code: kindEnum, enum: EnumMaybe}, p)
		case p.Type == LITERAL && p.Lexeme == "null":
			a.kind, a.kindOK = valueKind{code: uint32(TidNull)}, true
			c.narrowKind(&a, a.kind, p)
		case p.Type == TRUE:
			a.boolLit = 1
		case p.Type == FALSE:
			a.boolLit = 2
		case p.Type == INTEGER, p.Type == FLOAT, p.Type == STRING, p.Type == SINGLEQUOTESTRING, p.Type == PATH:
			// A value pattern tests equality; it narrows nothing.
		default:
			k, ok := c.patternKind(p)
			if !ok {
				return c.badPattern(p, "")
			}
			a.kind, a.kindOK = k, true
			c.narrowKind(&a, k, p)
		}
	case *MShellParseOrPattern:
	case *MShellParseList:
		elem := c.listElem(c.narrowKind(&a, valueKind{code: kindList}, p.StartToken))
		a.listLen = len(p.Items)
		for _, item := range p.Items {
			tok, ok := item.(Token)
			if !ok || tok.Type != LITERAL {
				return c.badPattern(p.StartToken, "a list pattern holds names, `_` and one `...rest`")
			}
			switch {
			case tok.Lexeme == "_":
			case tok.Lexeme == "..._":
				a.listRest, a.listLen = true, a.listLen-1
			case strings.HasPrefix(tok.Lexeme, "..."):
				a.listRest, a.listLen = true, a.listLen-1
				// The rest is a new list of the same elements.
				rest := tok
				rest.Lexeme = tok.Lexeme[3:]
				lt := TidBottom
				if elem != TidBottom {
					lt = c.arena.MakeList(elem)
				}
				c.bind(&a, rest, lt)
			default:
				c.bind(&a, tok, elem)
			}
		}
	case *MShellParseDict:
		rec := c.narrowKind(&a, valueKind{code: kindDict}, p.StartToken)
		for _, kv := range p.Items {
			if len(kv.Value) != 1 {
				return c.badPattern(p.StartToken, "a dict pattern's value is one name")
			}
			tok, ok := kv.Value[0].(Token)
			if !ok || tok.Type != LITERAL {
				return c.badPattern(p.StartToken, "a dict pattern's value is one name")
			}
			ft := TidBottom
			if rec != TidBottom {
				f := c.arena.records[c.arena.nodes[rec].Extra].FieldAt(c.names.Intern(kv.Key))
				switch f.Status {
				case FieldRequired, FieldOptional, FieldDeletable:
					ft = f.Type
				case FieldOpen:
					ft = TidUnknown
				case FieldAbsent:
					c.errs = append(c.errs, TypeError{Kind: TErrInvalidMatchPattern, Pos: p.StartToken,
						Hint: "this pattern never matches: " + c.format(rec) + " has no key '" + kv.Key + "'"})
				}
			}
			c.bind(&a, tok, ft)
		}
	default:
		return c.badPattern(at, "")
	}
	return a, true
}

// narrowKind sets the arm's subject to the member of t of kind k, and
// returns it; TidBottom when t has no such member (the arm never runs).
func (c *coreChecker) narrowKind(a *coreArm, k valueKind, tok Token) TypeId {
	m, found, unknown := c.memberOfKind(a.subject, k)
	switch {
	case unknown:
		ut, abstract := c.unknownOfKind(k)
		if ut == TidNothing {
			c.unsupported(tok, "a quotation pattern on a value of unknown type")
			return TidBottom
		}
		a.subject, a.abstract = ut, abstract
		return ut
	case found:
		a.subject = m
		return m
	}
	a.subject = TidBottom
	return TidBottom
}

// listElem is the element type of a list-kinded type (a list, or a
// command, whose arguments are a list), or TidBottom.
func (c *coreChecker) listElem(t TypeId) TypeId {
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKList:
		return TypeId(n.A)
	case TKCommand:
		return c.listElem(TypeId(n.A))
	}
	return TidBottom
}

// bind records a binding; `_` binds nothing.
func (c *coreChecker) bind(a *coreArm, tok Token, t TypeId) {
	if tok.Lexeme != "_" {
		a.binds = append(a.binds, coreBinding{tok: tok, t: t})
	}
}

func (c *coreChecker) badPattern(tok Token, hint string) (coreArm, bool) {
	c.errs = append(c.errs, TypeError{Kind: TErrInvalidMatchPattern, Pos: tok, Hint: hint})
	c.abandoned = true
	return coreArm{}, false
}

// exhaustive reports whether the arms cover every value of type t.
func (c *coreChecker) exhaustive(arms []coreArm, t TypeId) bool {
	for _, a := range arms {
		if a.all {
			return true
		}
	}
	var members []TypeId
	if !c.members(t, &members) {
		return false
	}
	for _, m := range members {
		k, ok := c.rel.kindOf(m)
		if !ok {
			return false
		}
		covered := false
		var just, none, tr, fa bool
		for _, a := range arms {
			switch {
			case a.kindOK && a.kind == k:
				covered = true
			case a.maybe == 1 && k.code == kindEnum && k.enum == EnumMaybe:
				just = true
			case a.maybe == 2 && k.code == kindEnum && k.enum == EnumMaybe:
				none = true
			case a.boolLit == 1 && m == TidBool:
				tr = true
			case a.boolLit == 2 && m == TidBool:
				fa = true
			}
		}
		if !covered && !(just && none) && !(tr && fa) && !(k.code == kindList && listsCover(arms)) {
			return false
		}
	}
	return true
}

// listsCover reports whether the arms' list patterns match every length:
// each length below the least one a `...rest` pattern takes is matched
// exactly by another pattern.
func listsCover(arms []coreArm) bool {
	least := -1
	for _, a := range arms {
		if a.listLen >= 0 && a.listRest && (least < 0 || a.listLen < least) {
			least = a.listLen
		}
	}
	if least < 0 {
		return false
	}
	for n := 0; n < least; n++ {
		found := false
		for _, a := range arms {
			if a.listLen == n && !a.listRest {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// members collects the members of t through unions and aliases; false when
// a member has unknown contents.
func (c *coreChecker) members(t TypeId, out *[]TypeId) bool {
	t = c.unfold(t)
	if c.unknownContents(t) {
		return false
	}
	if c.arena.nodes[t].Kind == TKUnion {
		for _, m := range c.arena.unionMembers[c.arena.nodes[t].Extra] {
			if !c.members(m, out) {
				return false
			}
		}
		return true
	}
	*out = append(*out, t)
	return true
}
