package main

// Reading and writing dicts by key in the core checker
// (ai/type-core-calculus.typ, "Shapes", "Runtime keys", "Type-changing
// updates of fresh records"; ai/builtin-audit/dictgrid.md).
//
// A string literal on the stack keeps its text in its slot (coreSlot.lit),
// and shuffles keep it, so a key below the top is still a literal key:
// `dict "a" 1 set`, `dict "a" 0 getDef`.
//
// With a literal key, a read gives the label's type (Get, Get-Opt); with a
// runtime key, a type above every label (Get-Key). The runtime always gives
// a Maybe for get and the getter, none when the key is missing. A write with
// a literal key stores the label's type (Set), or, on a fresh dict, gives
// the label the value's type (Set-Fresh); with a runtime key it needs
// `{str: T}`, which is in the table.

// recordOf is the dict-kinded type a value of type t is read as, or
// TidNothing when it is not dict-kinded.
func (c *coreChecker) recordOf(t TypeId) TypeId {
	t = c.unfold(c.subst.Apply(c.arena, t))
	if t == TidUnknown || c.arena.nodes[t].Kind == TKAbstract || c.arena.nodes[t].Kind == TKRigid {
		return TidNothing
	}
	if c.arena.nodes[t].Kind == TKRecord {
		return t
	}
	return TidNothing
}

// labelRead is the type a read of label name gives in record rec, before
// the Maybe the runtime adds: the label's type, ⊥ when it is absent, and
// unknown when it is open.
func (c *coreChecker) labelRead(rec TypeId, name NameId) (TypeId, FieldStatus) {
	f := c.arena.records[c.arena.nodes[rec].Extra].FieldAt(name)
	switch f.Status {
	case FieldAbsent:
		return TidBottom, f.Status
	case FieldOpen:
		return TidUnknown, f.Status
	}
	return f.Type, f.Status
}

// keyRead is the type a read with a runtime key gives (Get-Key): a type
// above every label of rec, unknown when a label is open. ok is false when
// the labels' types have no join.
func (c *coreChecker) keyRead(rec TypeId) (TypeId, bool) {
	r := c.arena.records[c.arena.nodes[rec].Extra]
	acc := coreSlot{t: TidBottom}
	for _, f := range append(r.Fields, r.Rest) {
		switch f.Status {
		case FieldAbsent:
			continue
		case FieldOpen:
			return TidUnknown, true
		}
		j, ok := c.joinSlot(acc, coreSlot{t: f.Type})
		if !ok {
			return TidNothing, false
		}
		acc = j
	}
	return acc.t, true
}

// dictArg checks that slot i holds a dict-kinded value and returns its
// record type.
func (c *coreChecker) dictArg(i int, tok Token) (TypeId, bool) {
	c.force(i)
	t := c.subst.Apply(c.arena, c.stack[i].t)
	if c.arena.nodes[t].Kind == TKVar {
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "'" + tok.Lexeme + "' reads a value whose type is not known here (" + c.format(t) + "); annotate it"})
		c.abandoned = true
		return TidNothing, false
	}
	if rec := c.recordOf(t); rec != TidNothing {
		return rec, true
	}
	if u := c.unfold(t); u == TidUnknown || c.arena.nodes[u].Kind == TKAbstract || c.arena.nodes[u].Kind == TKRigid {
		// Unknown contents: any dict, read only.
		return c.arena.MakeRecord(nil, RecordField{Status: FieldOpen}), true
	}
	hint := "'" + tok.Lexeme + "' needs a dict, got " + c.format(t)
	if c.arena.nodes[c.unfold(t)].Kind == TKUnion {
		hint += "; take the dict out first with `match dict d :`"
	}
	c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: hint})
	c.abandoned = true
	return TidNothing, false
}

// readType is a read of key slot k (a literal, or not) from record rec.
func (c *coreChecker) readType(rec TypeId, k coreSlot, tok Token) (TypeId, bool) {
	if name := k.key(); name != NameNone {
		t, _ := c.labelRead(rec, name)
		return t, true
	}
	t, ok := c.keyRead(rec)
	if !ok {
		c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
			Hint: "the values of " + c.format(rec) + " have no common type, so a read with a key known only at run time has none; use a literal key"})
		c.abandoned = true
	}
	return t, ok
}

// keyArg checks that slot i is a key: a str or a path.
func (c *coreChecker) keyArg(i int, tok Token) bool {
	c.force(i)
	if !c.check(c.stack[i], c.arena.MakeUnion([]TypeId{TidStr, TidPath}, NameNone)) {
		c.mismatch(tok, i, TidStr, c.stack[i].t)
		return false
	}
	return true
}

// dictWord checks the dict words the table cannot say. It reports false
// for a word it does not handle, or a set or setd with a key known only at
// run time, which the table handles.
func (c *coreChecker) dictWord(tok Token) bool {
	switch tok.Lexeme {
	case "get":
		// dict key get: Maybe of the value.
		if !c.need(2, tok) {
			return true
		}
		n := len(c.stack)
		if c.gridRead(n-2, tok) {
			return true
		}
		rec, ok := c.dictArg(n-2, tok)
		if !ok || !c.keyArg(n-1, tok) {
			return true
		}
		t, ok := c.readType(rec, c.stack[n-1], tok)
		if !ok {
			return true
		}
		c.stack = c.stack[:n-2]
		c.push(c.arena.MakeMaybeEnum(t), false)
	case "getDef":
		// dict key default getDef: the value, or the default.
		if !c.need(3, tok) {
			return true
		}
		n := len(c.stack)
		rec, ok := c.dictArg(n-3, tok)
		if !ok || !c.keyArg(n-2, tok) {
			return true
		}
		c.force(n - 1)
		k, def := c.stack[n-2], c.stack[n-1]
		var t TypeId
		if k.key() != NameNone {
			lt, status := c.labelRead(rec, k.key())
			switch status {
			case FieldRequired:
				t = lt
			case FieldAbsent:
				t = def.t
			case FieldOpen:
				t = TidUnknown
			default:
				t = c.joinOrFail(lt, def.t, tok)
			}
		} else {
			kt, ok := c.readType(rec, k, tok)
			if !ok {
				return true
			}
			t = c.joinOrFail(kt, def.t, tok)
		}
		if c.abandoned {
			return true
		}
		c.stack = c.stack[:n-3]
		c.push(t, false)
	case "values", "keyValues":
		if !c.need(1, tok) {
			return true
		}
		n := len(c.stack)
		rec, ok := c.dictArg(n-1, tok)
		if !ok {
			return true
		}
		v, ok := c.keyRead(rec)
		if !ok {
			c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
				Hint: "the values of " + c.format(rec) + " have no common type"})
			c.abandoned = true
			return true
		}
		elem := v
		if tok.Lexeme == "keyValues" {
			elem = c.arena.MakeRecord([]RecordField{
				{Name: c.names.Intern("k"), Status: FieldRequired, Type: TidStr},
				{Name: c.names.Intern("v"), Status: FieldRequired, Type: v},
			}, RecordField{Status: FieldAbsent})
		}
		c.stack = c.stack[:n-1]
		// A new list of the dict's values: fresh when they are immutable.
		c.push(c.arena.MakeList(elem), c.rel.Immutable(c.subst.Apply(c.arena, v)))
	case "set", "setd":
		return c.setLiteral(tok)
	case "map", "filter":
		return c.dictMapFilter(tok)
	case "urlEncode":
		return c.urlEncodeDict(tok)
	default:
		return false
	}
	return true
}

// receiverRecord is the record type of the dict at stack index i, or
// TidNothing when the value there is not known to be a dict.
func (c *coreChecker) receiverRecord(i int) TypeId {
	if i < c.floor || c.waiting(c.stack[i]) != nil {
		return TidNothing
	}
	return c.recordOf(c.stack[i].t)
}

// dictMapFilter checks the dict forms of map and filter. The quote runs on
// a child stack once per value, read at the type of Get-Key; the result is
// a new `{str: T}` over the quote's results (map) or the kept values
// (filter), fresh when T is immutable, as for new lists. It reports false
// when the receiver is not a dict, for the table's list and grid forms.
func (c *coreChecker) dictMapFilter(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	rec := c.receiverRecord(n - 2)
	if rec == TidNothing {
		return false
	}
	v, ok := c.keyRead(rec)
	if !ok {
		c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
			Hint: "the values of " + c.format(rec) + " have no common type, so '" + tok.Lexeme + "' has no type to give its quote"})
		c.abandoned = true
		return true
	}
	ar := c.arena
	var sig coreSig
	var elem TypeId
	if tok.Lexeme == "map" {
		b := ar.MakeParam(0)
		sig = coreSig{
			ins:  []TypeId{rec, ar.MakeQuote(QuoteSig{Inputs: []TypeId{v}, Outputs: []TypeId{b}})},
			outs: []TypeId{ar.MakeStrDict(b)},
			gens: []NameId{c.names.Intern("b")}, genIn: 1 << 1, genOut: 1, child: true,
		}
	} else {
		sig = coreSig{
			ins:   []TypeId{rec, ar.MakeQuote(QuoteSig{Inputs: []TypeId{v}, Outputs: []TypeId{TidBool}})},
			outs:  []TypeId{ar.MakeStrDict(v)},
			child: true,
		}
		elem = v
	}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	top := &c.stack[len(c.stack)-1]
	if elem == TidNothing {
		elem = ar.records[ar.nodes[c.subst.Apply(ar, top.t)].Extra].Rest.Type
	}
	top.fresh = c.rel.Immutable(c.subst.Apply(ar, elem))
	return true
}

// urlEncodeDict checks `dict urlEncode`: every value, read at the type of
// Get-Key, must be one the runtime writes as a string. It reports false
// when the receiver is not a dict.
func (c *coreChecker) urlEncodeDict(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 1 {
		return false
	}
	rec := c.receiverRecord(n - 1)
	if rec == TidNothing {
		return false
	}
	v, ok := c.keyRead(rec)
	ar := c.arena
	fits := false
	if ok {
		read := coreSlot{t: v, fresh: c.stack[n-1].fresh}
		for _, list := range c.table.urlEncodeLists {
			cp := c.checkpoint()
			if c.check(read, ar.MakeUnion([]TypeId{TidStr, TidInt, TidPath, list}, NameNone)) {
				fits = true
				break
			}
			c.rollback(cp)
		}
	}
	if !fits {
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "'urlEncode' writes each value of a dict as a string: a str, int or path, or a list of them; got " + c.format(rec)})
		c.abandoned = true
		return true
	}
	c.stack = c.stack[:n-1]
	c.push(TidStr, true)
	return true
}

// joinOrFail joins two shared types, or reports that they have none.
func (c *coreChecker) joinOrFail(a, b TypeId, tok Token) TypeId {
	j, ok := c.joinSlot(coreSlot{t: a}, coreSlot{t: b})
	if !ok {
		c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
			Hint: "the stored value (" + c.format(a) + ") and the default (" + c.format(b) + ") have no common type"})
		c.abandoned = true
	}
	return j.t
}

// setLiteral checks `dict "k" v set` and `setd` with a literal key. It
// reports false when the key is not a literal.
func (c *coreChecker) setLiteral(tok Token) bool {
	if len(c.stack)-c.floor < 3 || c.stack[len(c.stack)-2].key() == NameNone {
		return false
	}
	n := len(c.stack)
	rec, ok := c.dictArg(n-3, tok)
	if !ok {
		return true
	}
	c.force(n - 1)
	d, k, v := c.stack[n-3], c.stack[n-2], c.stack[n-1]
	label := k.key()
	vFresh := c.freshish(v)
	r := c.arena.records[c.arena.nodes[rec].Extra]
	f := r.FieldAt(label)
	var out TypeId
	var part uint16
	switch {
	case d.fresh || d.part != 0:
		// Set-Fresh: nothing else sees the dict, so the label takes the
		// value's type. A stored value keeps its own type inside the new
		// dict (tw_setk_m).
		if !(d.fresh && vFresh) {
			part = c.recordPart(d, label, c.innerMark(v))
		}
		fields := make([]RecordField, 0, len(r.Fields)+1)
		for _, g := range r.Fields {
			if g.Name != label {
				fields = append(fields, g)
			}
		}
		fields = append(fields, RecordField{Name: label, Status: FieldRequired, Type: c.subst.Apply(c.arena, v.t)})
		out = c.arena.MakeRecord(fields, r.Rest)
	case f.Status == FieldRequired || f.Status == FieldOptional || f.Status == FieldDeletable:
		// Set: the label is written at its own type.
		if !c.check(v, f.Type) {
			c.mismatch(tok, 2, f.Type, v.t)
		}
		out = c.stack[n-3].t
	default:
		hint := c.format(rec) + " has no key '" + c.names.Name(label) + "' that can be set" +
			"; a shared dict keeps its type: build it as a literal, or make a new one with deepCopy"
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: hint})
		c.abandoned = true
		return true
	}
	c.stack = c.stack[:n-3]
	if tok.Lexeme == "set" {
		c.stack = append(c.stack, coreSlot{t: out, fresh: d.fresh && vFresh, part: part})
	}
	return true
}

// getter checks `:name`: get with the literal key name.
func (c *coreChecker) getter(g *MShellGetter) {
	tok := g.Token
	if !c.need(1, tok) {
		return
	}
	n := len(c.stack)
	c.push(TidStr, true)
	c.stack[n].lit = c.names.Intern(g.String)
	tok.Lexeme = "get"
	c.dictWord(tok)
}

// gridRead checks get on a grid, a view or a row (getters too): a row
// gives Maybe of the column's cell type, a grid or view Maybe of a new list
// of its cells. A literal name reads that column (none when the schema says
// there is no such column); a name known only at run time reads at the
// type of Get-Key. It reports false when slot i is not one.
func (c *coreChecker) gridRead(i int, tok Token) bool {
	kind, rec, ok := c.gridOf(i)
	if !ok {
		return false
	}
	if !c.keyArg(len(c.stack)-1, tok) {
		return true
	}
	t, ok := c.readType(rec, c.stack[len(c.stack)-1], tok)
	if !ok {
		return true
	}
	fresh := false
	if kind != TKGridRow {
		fresh = c.rel.Immutable(c.subst.Apply(c.arena, t))
		t = c.arena.MakeList(t)
	}
	c.stack = c.stack[:len(c.stack)-2]
	c.push(c.arena.MakeMaybeEnum(t), fresh)
	return true
}
