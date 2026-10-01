package main

import (
	"fmt"
	"strconv"
	"strings"
)

// The core checker: the checker described in ai/type-core-calculus.typ,
// built beside the old one and selected by MSH_CHECKER=core
// (ai/type-system-plan.md, stage 3).
//
// It walks the parse tree with a concrete type stack of slots, each a type
// and a fresh mark. A def body is one unit and the top-level script is
// another. A unit owns its unification variables and its variable scope,
// and ends with the checks the design asks for once its types are solved:
// every unified pair again (unification is not trusted), and every store
// against its variable's final type.
//
// Checking positions (def and builtin arguments, stores, def outputs)
// compare a slot with a wanted type. When either mentions an unsolved
// variable they are unified, by equality; otherwise the slot's type must
// be below the wanted one (<=), or, for a fresh slot, retypable to it.

// CoreBase is the state every core check starts from: the builtin table,
// the std signatures and the types they mention, in an arena that is
// frozen once built. A check works in an overlay of it, so starting one
// copies nothing.
type CoreBase struct {
	arena   *TypeArena
	names   *NameTable
	table   *coreTable
	aliases map[NameId]TypeId
}

// NewCoreBase builds the base: the builtin table, and the signatures of
// stdlibDefs (their bodies are not checked, as with the old checker).
func NewCoreBase(stdlibDefs []MShellDefinition) *CoreBase {
	arena, names := NewTypeArena(), NewNameTable()
	res := &coreResolver{arena: arena, names: names, rel: NewRelations(arena), aliases: map[NameId]TypeId{}}
	res.declareJson()
	table := buildCoreTable(res)
	for i := range stdlibDefs {
		def := &stdlibDefs[i]
		id := names.Intern(def.Name)
		if table.name(id) != nil {
			continue
		}
		parts := res.resolveSig(def.Inputs, def.Outputs)
		res.errs = res.errs[:0]
		table.setName(id, []coreSig{newCoreSig(arena, parts)})
	}
	return &CoreBase{arena: arena, names: names, table: table, aliases: res.aliases}
}

// CoreTypeCheckProgram checks file with the core checker. It returns the
// formatted errors, and whether there were none.
func CoreTypeCheckProgram(file *MShellFile, stdlibDefs []MShellDefinition) ([]string, bool) {
	return NewCoreBase(stdlibDefs).Check(file)
}

// Check checks file in a new overlay of the base.
func (b *CoreBase) Check(file *MShellFile) ([]string, bool) {
	c := b.newChecker()
	c.checkFile(file)
	var out []string
	for _, e := range c.errs {
		if e.Severity == SeverityError {
			out = append(out, e.Format(c.arena, c.names))
		}
	}
	return out, len(out) == 0
}

func (b *CoreBase) newChecker() *coreChecker {
	arena, names := b.arena.Overlay(), b.names.Overlay()
	rel := NewRelations(arena)
	c := &coreChecker{
		arena: arena,
		names: names,
		rel:   rel,
		table: b.table,
		defs:  map[NameId]*coreSig{},
	}
	aliases := make(map[NameId]TypeId, len(b.aliases))
	for k, v := range b.aliases {
		aliases[k] = v
	}
	c.res = coreResolver{arena: arena, names: names, rel: rel, aliases: aliases}
	c.uni = NewUnifier(arena, &c.subst, rel)
	return c
}

// coreRetKind is the return context of a unit (the R of the typing rules).
type coreRetKind uint8

const (
	retNone  coreRetKind = iota // no return: a quote body or a `never` def
	retExact                    // a def: return leaves exactly its outputs
	retAny                      // top-level code: return ends the script
)

// coreVar is a variable of the current scope. Its type is a unification
// variable fixed by the first store (design doc, "Variable scopes").
type coreVar struct {
	gen       uint32 // the scope generation this entry belongs to
	t         TypeId
	stored    bool
	firstLoad Token
	loaded    bool
}

// coreStore is a store, checked again when its unit is solved.
type coreStore struct {
	tok   Token
	name  NameId
	t     TypeId
	v     TypeId
	fresh bool
}

type coreChecker struct {
	arena *TypeArena
	names *NameTable
	rel   *Relations
	subst Substitution
	uni   *Unifier
	res   coreResolver
	table *coreTable
	defs  map[NameId]*coreSig
	errs  []TypeError

	stack []coreSlot
	// floor is the lowest slot the current code may use: a list literal's
	// body runs on its own stack.
	floor     int
	diverged  bool
	abandoned bool
	listDepth int
	ret       coreRetKind
	retOuts   []TypeId

	// vars is indexed by NameId; an entry belongs to the current scope only
	// when its gen is varGen, so entering a scope clears nothing.
	vars     []coreVar
	varGen   uint32
	unitVars []NameId
	stores   []coreStore

	// saved holds stacks saved for branches, as a stack of slot runs.
	saved []coreSlot
	// mentionsVar caches, per TypeId, whether a type mentions a
	// unification variable: 0 not yet known, 1 no, 2 yes.
	mentionsVar []uint8
	genBuf      []TypeId
}

// ---------------------------------------------------------------------------
// Files and units

func (c *coreChecker) checkFile(file *MShellFile) {
	for _, item := range file.Items {
		if d, ok := item.(*MShellTypeDecl); ok {
			c.declareType(d)
		}
	}
	for i := range file.Definitions {
		def := &file.Definitions[i]
		parts := c.res.resolveSig(def.Inputs, def.Outputs)
		c.takeResolveErrors()
		sig := newCoreSig(c.arena, parts)
		c.defs[c.names.Intern(def.Name)] = &sig
	}
	for i := range file.Definitions {
		c.checkDef(&file.Definitions[i])
	}
	c.beginUnit()
	c.stack = c.stack[:0]
	c.ret, c.retOuts = retAny, nil
	c.walk(file.Items)
	c.finishUnit()
}

// declareType declares `type X = T` as a transparent alias. Declarations
// are read in order; stage 4 reads them in three passes.
func (c *coreChecker) declareType(d *MShellTypeDecl) {
	name := c.names.Intern(d.Name)
	idx := c.arena.DeclareAlias(name)
	c.res.aliases[name] = c.arena.MakeAliasRef(idx)
	c.arena.SetAliasBody(idx, c.res.resolveType(d.Body))
	c.takeResolveErrors()
}

func (c *coreChecker) takeResolveErrors() {
	c.errs = append(c.errs, c.res.errs...)
	c.res.errs = c.res.errs[:0]
}

func (c *coreChecker) beginUnit() {
	c.varGen++
	c.unitVars = c.unitVars[:0]
	c.stores = c.stores[:0]
	c.subst.bound = c.subst.bound[:0]
	c.subst.root = nil
	c.uni.pairs = c.uni.pairs[:0]
	c.diverged, c.abandoned = false, false
	c.floor, c.listDepth = 0, 0
}

// finishUnit makes the checks that wait for the unit's final substitution.
func (c *coreChecker) finishUnit() {
	// A variable left unsolved is ⊥ (design doc, "A ⊥ in a store fixes
	// nothing"); nothing constrained it, so any type would do.
	for v, t := range c.subst.bound {
		if t == TidNothing {
			c.subst.bound[v] = TidBottom
		}
	}
	for _, p := range c.uni.Recheck() {
		c.errs = append(c.errs, TypeError{Kind: TErrCoreInternal, Pos: Token{}, Hint: fmt.Sprintf(
			"unified types differ once solved: %s and %s",
			FormatType(c.arena, c.names, p.A), FormatType(c.arena, c.names, p.B))})
	}
	for _, s := range c.stores {
		t, v := c.subst.Apply(c.arena, s.t), c.subst.Apply(c.arena, s.v)
		if !c.below(s.fresh, t, v) {
			c.errs = append(c.errs, c.storeError(s.tok, s.name, t, v))
		}
	}
	for _, name := range c.unitVars {
		v := &c.vars[name]
		if v.loaded && !v.stored {
			c.errs = append(c.errs, TypeError{Kind: TErrUnknownIdentifier, Pos: v.firstLoad, Name: v.firstLoad.Lexeme})
		}
	}
}

// checkDef checks a def body once, with its generics rigid.
func (c *coreChecker) checkDef(def *MShellDefinition) {
	sig := c.defs[c.names.Intern(def.Name)]
	c.beginUnit()
	for _, out := range def.Outputs {
		if _, ok := out.(*TypeNewExpr); ok {
			c.unsupported(def.NameToken, "`new` outputs")
			return
		}
	}
	rigid := make([]TypeId, len(sig.gens))
	for i, g := range sig.gens {
		rigid[i] = c.arena.MakeRigid(g)
	}
	c.stack = c.stack[:0]
	for i, t := range sig.ins {
		if sig.genIn&(1<<i) != 0 {
			t = c.rel.SubstParams(t, rigid)
		}
		c.stack = append(c.stack, coreSlot{t: t})
	}
	outs := make([]TypeId, len(sig.outs))
	for i, t := range sig.outs {
		if sig.genOut&(1<<i) != 0 {
			t = c.rel.SubstParams(t, rigid)
		}
		outs[i] = t
	}
	if sig.diverges {
		c.ret, c.retOuts = retNone, nil
	} else {
		c.ret, c.retOuts = retExact, outs
	}
	c.walk(def.Items)
	switch {
	case c.abandoned, c.diverged:
	case sig.diverges:
		c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: def.NameToken, Name: def.Name,
			Hint: "the signature says it never returns, but the body can return; a def that returns on some paths declares what it returns"})
	default:
		c.checkOutputs(def, outs)
	}
	c.finishUnit()
}

// checkOutputs checks that the stack at the end of a def body is its
// declared outputs.
func (c *coreChecker) checkOutputs(def *MShellDefinition, outs []TypeId) {
	if len(c.stack) != len(outs) {
		c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: def.NameToken, Name: def.Name,
			Hint: "declared " + strconv.Itoa(len(outs)) + " output(s) " + c.formatTypes(outs) +
				", body produced " + strconv.Itoa(len(c.stack)) + " " + c.formatSlots(c.stack)})
		return
	}
	for i, want := range outs {
		if !c.check(c.stack[i], want) {
			got := c.subst.Apply(c.arena, c.stack[i].t)
			c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: def.NameToken, Name: def.Name,
				Hint: "output " + strconv.Itoa(i) + " is declared " + c.format(want) + ", body produced " + c.format(got)})
		}
	}
}

// unsupported reports a construct the core checker does not check yet,
// and stops checking the unit.
func (c *coreChecker) unsupported(tok Token, what string) {
	c.errs = append(c.errs, TypeError{Kind: TErrCoreUnsupported, Pos: tok, Hint: what})
	c.abandoned = true
}

// ---------------------------------------------------------------------------
// Walking

func (c *coreChecker) walk(items []MShellParseItem) {
	for _, item := range items {
		if c.diverged || c.abandoned {
			// Words after a diverging word are not checked (t_div).
			return
		}
		c.step(item)
	}
}

func (c *coreChecker) step(item MShellParseItem) {
	switch it := item.(type) {
	case Token:
		c.token(it)
	case MShellVarstoreList:
		for i := len(it.VarStores) - 1; i >= 0; i-- {
			tok := it.VarStores[i]
			c.store(tok, c.names.Intern(strings.TrimSuffix(tok.Lexeme, "!")))
		}
	case *MShellParseList:
		c.listLiteral(it)
	case *MShellParseDict:
		c.dictLiteral(it)
	case *MShellParseIfBlock:
		c.ifBlock(it)
	case *MShellTypeDecl:
	case *MShellParseQuote:
		c.unsupported(it.StartToken, "quotes")
	case *MShellParsePrefixQuote:
		c.unsupported(it.StartToken, "prefix quotes")
	case *MShellParseMatchBlock:
		c.unsupported(it.StartToken, "match")
	case *MShellParseGrid:
		c.unsupported(it.GetStartToken(), "grid literals")
	case *MShellIndexerList:
		c.unsupported(it.GetStartToken(), "indexing")
	case *MShellGetter:
		c.unsupported(it.Token, "getters")
	case *MShellParseFormatString:
		c.unsupported(it.GetStartToken(), "format strings")
	case *MShellAsCast:
		c.unsupported(it.AsToken, "as")
	default:
		c.unsupported(item.GetStartToken(), fmt.Sprintf("%T", item))
	}
}

func (c *coreChecker) push(t TypeId, fresh bool) {
	c.stack = append(c.stack, coreSlot{t: t, fresh: fresh})
}

// need reports whether n slots are available, reporting an underflow if not.
func (c *coreChecker) need(n int, tok Token) bool {
	if len(c.stack)-c.floor >= n {
		return true
	}
	c.errs = append(c.errs, TypeError{Kind: TErrStackUnderflow, Pos: tok})
	c.abandoned = true
	return false
}

func (c *coreChecker) token(tok Token) {
	switch tok.Type {
	case INTEGER:
		c.push(TidInt, true)
	case FLOAT:
		c.push(TidFloat, true)
	case STRING, SINGLEQUOTESTRING, FORMATSTRING:
		c.push(TidStr, true)
	case TRUE, FALSE:
		c.push(TidBool, true)
	case PATH:
		c.push(TidPath, true)
	case DATETIME:
		c.push(TidDateTime, true)
	case VARRETRIEVE:
		c.load(tok)
	case ENVRETREIVE:
		c.push(TidStr, true)
	case ENVCHECK:
		c.push(TidBool, true)
	case ENVSTORE:
		if c.need(1, tok) {
			c.stack = c.stack[:len(c.stack)-1]
		}
	case LITERAL:
		c.word(tok)
	default:
		if sigs := c.table.token(tok.Type); sigs != nil {
			c.call(sigs, tok)
			return
		}
		c.unsupported(tok, "'"+tok.Lexeme+"'")
	}
}

// word checks a LITERAL token: return, a stack shuffle, a def, a builtin,
// or a bare word in a list literal.
func (c *coreChecker) word(tok Token) {
	if c.shuffle(tok) {
		return
	}
	if tok.Lexeme == "return" {
		c.doReturn(tok)
		return
	}
	if id, ok := c.names.Lookup(tok.Lexeme); ok {
		if sig := c.defs[id]; sig != nil {
			c.apply(sig, tok)
			return
		}
		if sigs := c.table.name(id); sigs != nil {
			c.call(sigs, tok)
			return
		}
	}
	if _, ok := BuiltInList[tok.Lexeme]; ok {
		c.unsupported(tok, "the builtin '"+tok.Lexeme+"'")
		return
	}
	if c.listDepth > 0 {
		// A bare word in a list literal is a string: `[ls -l]`.
		c.push(TidStr, true)
		return
	}
	c.errs = append(c.errs, TypeError{Kind: TErrUnknownIdentifier, Pos: tok, Name: tok.Lexeme})
	c.abandoned = true
}

// shuffle checks the stack words. A word that copies a reference makes
// both copies shared; one that only moves slots keeps their marks.
func (c *coreChecker) shuffle(tok Token) bool {
	s := c.stack
	n := len(s)
	switch tok.Lexeme {
	case "dup":
		if c.need(1, tok) {
			s[n-1].fresh = false
			c.stack = append(s, s[n-1])
		}
	case "drop":
		if c.need(1, tok) {
			c.stack = s[:n-1]
		}
	case "swap":
		if c.need(2, tok) {
			s[n-2], s[n-1] = s[n-1], s[n-2]
		}
	case "over":
		if c.need(2, tok) {
			s[n-2].fresh = false
			c.stack = append(s, s[n-2])
		}
	case "rot":
		if c.need(3, tok) {
			s[n-3], s[n-2], s[n-1] = s[n-2], s[n-1], s[n-3]
		}
	case "-rot":
		if c.need(3, tok) {
			s[n-3], s[n-2], s[n-1] = s[n-1], s[n-3], s[n-2]
		}
	case "nip":
		if c.need(2, tok) {
			s[n-2] = s[n-1]
			c.stack = s[:n-1]
		}
	default:
		return false
	}
	return true
}

func (c *coreChecker) doReturn(tok Token) {
	switch c.ret {
	case retAny:
	case retNone:
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "'return' is not allowed in a def that never returns"})
	case retExact:
		if len(c.stack) != len(c.retOuts) {
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
				Hint: "'return' leaves " + strconv.Itoa(len(c.stack)) + " value(s) " + c.formatSlots(c.stack) +
					", the def declares " + strconv.Itoa(len(c.retOuts)) + " " + c.formatTypes(c.retOuts)})
			break
		}
		for i, want := range c.retOuts {
			if !c.check(c.stack[i], want) {
				c.mismatch(tok, i, want, c.stack[i].t)
			}
		}
	}
	c.diverged = true
}

// ---------------------------------------------------------------------------
// Variables

func (c *coreChecker) varOf(name NameId) *coreVar {
	for int(name) >= len(c.vars) {
		c.vars = append(c.vars, coreVar{})
	}
	v := &c.vars[name]
	if v.gen != c.varGen {
		*v = coreVar{gen: c.varGen, t: c.subst.FreshVar(c.arena)}
		c.unitVars = append(c.unitVars, name)
	}
	return v
}

func (c *coreChecker) load(tok Token) {
	v := c.varOf(c.names.Intern(strings.TrimPrefix(tok.Lexeme, "@")))
	if !v.loaded {
		v.loaded, v.firstLoad = true, tok
	}
	c.push(v.t, false)
}

// store checks `name!`: the value must fit the variable's one type. A ⊥ in
// the value's type is replaced by a new variable first, so it fixes
// nothing (`none r!` before `5 just r!`).
func (c *coreChecker) store(tok Token, name NameId) {
	if !c.need(1, tok) {
		return
	}
	slot := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	v := c.varOf(name)
	v.stored = true
	opened := c.openBottom(c.subst.Apply(c.arena, slot.t))
	if !c.check(coreSlot{t: opened, fresh: slot.fresh}, v.t) {
		c.errs = append(c.errs, c.storeError(tok, name, c.subst.Apply(c.arena, slot.t), c.subst.Apply(c.arena, v.t)))
		return
	}
	c.stores = append(c.stores, coreStore{tok: tok, name: name, t: slot.t, v: v.t, fresh: slot.fresh})
}

func (c *coreChecker) storeError(tok Token, name NameId, got, want TypeId) TypeError {
	return TypeError{Kind: TErrVarType, Pos: tok, Name: c.names.Name(name), Expected: want, Actual: got}
}

// openBottom replaces each ⊥ in t by a new unification variable.
func (c *coreChecker) openBottom(t TypeId) TypeId {
	ar := c.arena
	if t == TidBottom {
		return c.subst.FreshVar(ar)
	}
	n := ar.nodes[t]
	switch n.Kind {
	case TKList:
		if e := c.openBottom(TypeId(n.A)); e != TypeId(n.A) {
			return ar.MakeList(e)
		}
	case TKEnum:
		args := ar.enumArgs[n.Extra]
		var out []TypeId
		for i, x := range args {
			if y := c.openBottom(x); y != x {
				if out == nil {
					out = append([]TypeId(nil), args...)
				}
				out[i] = y
			}
		}
		if out != nil {
			return ar.MakeEnum(n.A, out)
		}
	case TKRecord:
		rec := ar.records[n.Extra]
		var fields []RecordField
		for i, f := range rec.Fields {
			if f.Type == TidNothing {
				continue
			}
			if y := c.openBottom(f.Type); y != f.Type {
				if fields == nil {
					fields = append([]RecordField(nil), rec.Fields...)
				}
				fields[i].Type = y
			}
		}
		if fields != nil {
			return ar.MakeRecord(fields, rec.Rest)
		}
	}
	return t
}

// ---------------------------------------------------------------------------
// Checking positions

// hasVars reports whether t, as solved so far, mentions an unsolved
// unification variable.
func (c *coreChecker) hasVars(t TypeId) bool {
	if !c.mentionsVars(t) {
		return false
	}
	return c.mentionsVars(c.subst.Apply(c.arena, t))
}

// mentionsVars reports whether t mentions a unification variable at all,
// solved or not. Types never change, so the answer is cached per TypeId.
func (c *coreChecker) mentionsVars(t TypeId) bool {
	for int(t) >= len(c.mentionsVar) {
		c.mentionsVar = append(c.mentionsVar, 0)
	}
	switch c.mentionsVar[t] {
	case 1:
		return false
	case 2:
		return true
	}
	m := typeMentions(c.arena, t, TKVar)
	c.mentionsVar[t] = 1
	if m {
		c.mentionsVar[t] = 2
	}
	return m
}

// freshish reports whether a slot's value is fresh, or has a type with no
// list, dict or grid in it, which is as good (design doc, slot subsumption).
func (c *coreChecker) freshish(s coreSlot) bool {
	return s.fresh || c.rel.Immutable(c.subst.Apply(c.arena, s.t))
}

// below decides a checking position between solved types: <= for a shared
// value, fresh retyping for a fresh one.
func (c *coreChecker) below(fresh bool, got, want TypeId) bool {
	if fresh {
		return c.rel.Retype(got, want)
	}
	return c.rel.Sub(got, want)
}

// check is a checking position: may the value in slot be used as want?
func (c *coreChecker) check(slot coreSlot, want TypeId) bool {
	if slot.t == want {
		return true
	}
	if c.hasVars(slot.t) || c.hasVars(want) {
		return c.uni.Unify(slot.t, want)
	}
	return c.below(slot.fresh, c.subst.Apply(c.arena, slot.t), c.subst.Apply(c.arena, want))
}

// call checks a builtin with one or more candidate signatures. The
// candidate is the one the arguments fit; with none, or more than one, it
// is an error.
func (c *coreChecker) call(sigs []coreSig, tok Token) {
	if len(sigs) == 1 && c.partial(tok) == "" {
		c.apply(&sigs[0], tok)
		return
	}
	fit, nfit := -1, 0
	for i := range sigs {
		cp := c.uni.Checkpoint()
		if c.argsFit(&sigs[i]) {
			fit, nfit = i, nfit+1
		}
		c.uni.Rollback(cp)
	}
	switch nfit {
	case 1:
		c.apply(&sigs[fit], tok)
	case 0:
		if what := c.partial(tok); what != "" {
			c.unsupported(tok, what)
			return
		}
		c.errs = append(c.errs, TypeError{Kind: TErrNoMatchingOverload, Pos: tok,
			Hint: "the stack has " + c.formatSlots(c.topSlots(sigs)) + "; " + c.formatCandidates(sigs)})
		c.abandoned = true
	default:
		c.errs = append(c.errs, TypeError{Kind: TErrAmbiguousTyping, Pos: tok,
			Hint: "more than one signature of '" + tok.Lexeme + "' fits " + c.formatSlots(c.topSlots(sigs)) + "; annotate the value"})
		c.abandoned = true
	}
}

// partial names what a word's table entry does not cover yet, or "".
func (c *coreChecker) partial(tok Token) string {
	if tok.Type == LITERAL {
		if id, ok := c.names.Lookup(tok.Lexeme); ok {
			return c.table.partialName[id]
		}
		return ""
	}
	return c.table.partialToken[tok.Type]
}

// topSlots returns the slots a set of candidates would read.
func (c *coreChecker) topSlots(sigs []coreSig) []coreSlot {
	n := 0
	for i := range sigs {
		n = max(n, len(sigs[i].ins))
	}
	n = min(n, len(c.stack)-c.floor)
	return c.stack[len(c.stack)-n:]
}

// instantiate makes new unification variables for sig's generics.
func (c *coreChecker) instantiate(sig *coreSig) []TypeId {
	c.genBuf = c.genBuf[:0]
	for range sig.gens {
		c.genBuf = append(c.genBuf, c.subst.FreshVar(c.arena))
	}
	return c.genBuf
}

// argsFit reports whether the stack's top fits sig's inputs, unifying as
// it goes; the caller rolls back.
func (c *coreChecker) argsFit(sig *coreSig) bool {
	n := len(sig.ins)
	if len(c.stack)-c.floor < n {
		return false
	}
	gens := c.instantiate(sig)
	ok := true
	c.eachInput(sig, gens, func(i int, want TypeId) {
		if ok && !c.check(c.stack[len(c.stack)-n+i], want) {
			ok = false
		}
	})
	return ok
}

// eachInput visits sig's inputs, instantiated: first those with structure,
// then the bare generics, so `([a] a -- [a])` reads a from the list before
// it checks the element against it, whichever is on top. A generic that is
// still unsolved and is several bare inputs, as in `(a a -- bool)`, is
// first set to the join of those arguments: the join is above each of them
// (join_slot_ub), so it is a valid choice, and it does not depend on which
// argument is checked first.
func (c *coreChecker) eachInput(sig *coreSig, gens []TypeId, f func(i int, want TypeId)) {
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			c.joinRepeated(sig, gens)
		}
		for i, want := range sig.ins {
			bare := c.arena.nodes[want].Kind == TKParam
			if bare != (pass == 1) {
				continue
			}
			if sig.genIn&(1<<i) != 0 {
				want = c.rel.SubstParams(want, gens)
			}
			f(i, want)
		}
	}
}

// joinRepeated sets each unsolved generic that is two or more bare inputs
// of sig to the join of the arguments there, when they are all solved.
func (c *coreChecker) joinRepeated(sig *coreSig, gens []TypeId) {
	base := len(c.stack) - len(sig.ins)
	for g := range gens {
		if !c.hasVars(gens[g]) {
			continue
		}
		var joined coreSlot
		count := 0
		for i, want := range sig.ins {
			n := c.arena.nodes[want]
			if n.Kind != TKParam || int(n.A) != g {
				continue
			}
			arg := c.stack[base+i]
			if c.hasVars(arg.t) {
				count = 0
				break
			}
			arg.fresh = c.freshish(arg)
			if count == 0 {
				joined = arg
			} else if j, ok := c.joinSlot(joined, arg); ok {
				joined = j
			} else {
				count = 0
				break
			}
			count++
		}
		if count >= 2 {
			c.uni.Unify(gens[g], joined.t)
		}
	}
}

// apply checks sig at the top of the stack and replaces its inputs by its
// outputs.
func (c *coreChecker) apply(sig *coreSig, tok Token) {
	n := len(sig.ins)
	if !c.need(n, tok) {
		return
	}
	gens := c.instantiate(sig)
	base := len(c.stack) - n
	c.eachInput(sig, gens, func(i int, want TypeId) {
		if !c.check(c.stack[base+i], want) {
			c.mismatch(tok, i, want, c.stack[base+i].t)
		}
	})
	inputsFresh := false
	if sig.keepOut != 0 {
		inputsFresh = true
		for _, s := range c.stack[base:] {
			if !c.freshish(s) {
				inputsFresh = false
			}
		}
	}
	c.stack = c.stack[:base]
	if sig.diverges {
		c.diverged = true
		return
	}
	for j, t := range sig.outs {
		if sig.genOut&(1<<j) != 0 {
			t = c.rel.SubstParams(t, gens)
		}
		fresh := sig.newOut&(1<<j) != 0 || (sig.keepOut&(1<<j) != 0 && inputsFresh)
		if !fresh && sig.newListOut&(1<<j) != 0 {
			fresh = c.listOfImmutable(t)
		}
		c.push(t, fresh)
	}
}

// listOfImmutable reports whether t is a list whose elements are
// immutable, as solved so far.
func (c *coreChecker) listOfImmutable(t TypeId) bool {
	t = c.subst.Apply(c.arena, t)
	n := c.arena.nodes[t]
	return n.Kind == TKList && c.rel.Immutable(TypeId(n.A))
}

func (c *coreChecker) mismatch(tok Token, i int, want, got TypeId) {
	c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
		Expected: c.subst.Apply(c.arena, want), Actual: c.subst.Apply(c.arena, got), ArgIndex: i})
}

// ---------------------------------------------------------------------------
// Literals

// child runs items on their own stack, as the runtime runs a list
// literal's body or a dict value, and returns the start of the slots they
// leave.
func (c *coreChecker) child(items []MShellParseItem) (start int, outerFloor int) {
	outerFloor = c.floor
	c.floor = len(c.stack)
	c.walk(items)
	return c.floor, outerFloor
}

// listLiteral types `[...]`: a list of the join of its elements, fresh
// when every element is fresh or immutable (ShapeLit).
func (c *coreChecker) listLiteral(l *MShellParseList) {
	c.listDepth++
	start, outerFloor := c.child(l.Items)
	c.listDepth--
	c.floor = outerFloor
	if c.diverged || c.abandoned {
		return
	}
	elems := c.stack[start:]
	var elem coreSlot
	fresh := true
	if len(elems) == 0 {
		elem = coreSlot{t: c.subst.FreshVar(c.arena), fresh: true}
	} else {
		elem = elems[0]
		elem.fresh = c.freshish(elem)
		for _, e := range elems {
			if !c.freshish(e) {
				fresh = false
			}
		}
		for _, e := range elems[1:] {
			e.fresh = c.freshish(e)
			j, ok := c.joinSlot(elem, e)
			if !ok {
				c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: l.StartToken,
					Hint: "the elements of this list have no common type: " + c.format(elem.t) + " and " + c.format(e.t)})
				c.abandoned = true
				return
			}
			elem = j
		}
	}
	c.stack = append(c.stack[:start], coreSlot{t: c.arena.MakeList(elem.t), fresh: fresh})
}

// dictLiteral types `{k: v, ...}`: a shape with exactly its keys, fresh
// when every value is fresh or immutable.
func (c *coreChecker) dictLiteral(d *MShellParseDict) {
	fields := make([]RecordField, 0, len(d.Items))
	fresh := true
	for _, kv := range d.Items {
		start, outerFloor := c.child(kv.Value)
		c.floor = outerFloor
		if c.diverged || c.abandoned {
			return
		}
		if len(c.stack)-start != 1 {
			c.errs = append(c.errs, TypeError{Kind: TErrChildStack, Pos: d.StartToken,
				Hint: "the value for key '" + kv.Key + "' must leave exactly one value, but leaves " + strconv.Itoa(len(c.stack)-start)})
			c.abandoned = true
			return
		}
		v := c.stack[start]
		c.stack = c.stack[:start]
		if !c.freshish(v) {
			fresh = false
		}
		fields = append(fields, RecordField{Name: c.names.Intern(kv.Key), Status: FieldRequired, Type: v.t})
	}
	c.push(c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent}), fresh)
}

// ---------------------------------------------------------------------------
// if blocks and joins

// ifBlock checks `if ... else if ... else ... end`. The condition on the
// stack is a bool, or an int (0 is true). Each `else if` condition runs on
// the stack the earlier conditions left. The arms are joined slot by slot;
// arms that diverge are left out (design doc, "Branches and joins").
func (c *coreChecker) ifBlock(b *MShellParseIfBlock) {
	tok := b.StartToken
	if !c.condition(tok) {
		return
	}
	mark := len(c.saved)
	entry := c.saveStack()
	var arms []savedRun
	runArm := func(body []MShellParseItem) {
		c.walk(body)
		if !c.abandoned {
			arms = append(arms, savedRun{start: len(c.saved), diverged: c.diverged})
			c.saved = append(c.saved, c.stack...)
			arms[len(arms)-1].end = len(c.saved)
		}
		c.diverged = false
	}
	runArm(b.IfBody)
	from := entry
	for _, ei := range b.ElseIfs {
		if c.abandoned {
			break
		}
		c.restoreStack(from)
		c.walk(ei.Condition)
		if c.diverged || c.abandoned || !c.condition(tok) {
			c.diverged = false
			break
		}
		from = c.saveStack()
		runArm(ei.Body)
	}
	if !c.abandoned {
		c.restoreStack(from)
		if b.ElseBody != nil {
			runArm(b.ElseBody)
		} else {
			runArm(nil)
		}
	}
	if !c.abandoned {
		c.joinArms(arms, tok)
	}
	c.saved = c.saved[:mark]
}

// condition pops an if condition: bool, or int.
func (c *coreChecker) condition(tok Token) bool {
	if !c.need(1, tok) {
		return false
	}
	slot := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	if c.hasVars(slot.t) {
		if !c.uni.Unify(slot.t, TidBool) {
			c.mismatch(tok, 0, TidBool, slot.t)
		}
		return true
	}
	t := c.subst.Apply(c.arena, slot.t)
	if !c.rel.Sub(t, c.arena.MakeUnion([]TypeId{TidBool, TidInt}, NameNone)) {
		c.mismatch(tok, 0, TidBool, t)
	}
	return true
}

// savedRun is a stack saved in c.saved.
type savedRun struct {
	start, end int
	diverged   bool
}

func (c *coreChecker) saveStack() savedRun {
	r := savedRun{start: len(c.saved)}
	c.saved = append(c.saved, c.stack...)
	r.end = len(c.saved)
	return r
}

func (c *coreChecker) restoreStack(r savedRun) {
	c.stack = append(c.stack[:0], c.saved[r.start:r.end]...)
}

// joinArms sets the stack to the join of the arms that did not diverge.
func (c *coreChecker) joinArms(arms []savedRun, tok Token) {
	var live []savedRun
	for _, a := range arms {
		if !a.diverged {
			live = append(live, a)
		}
	}
	if len(live) == 0 {
		c.diverged = true
		return
	}
	first := live[0]
	n := first.end - first.start
	for _, a := range live[1:] {
		if a.end-a.start != n {
			c.errs = append(c.errs, TypeError{Kind: TErrBranchStackSize, Pos: tok,
				Hint: "one arm leaves " + strconv.Itoa(n) + " value(s) " + c.formatSlots(c.saved[first.start:first.end]) +
					", another leaves " + strconv.Itoa(a.end-a.start) + " " + c.formatSlots(c.saved[a.start:a.end])})
			c.abandoned = true
			return
		}
	}
	c.stack = append(c.stack[:0], c.saved[first.start:first.end]...)
	for _, a := range live[1:] {
		for i := range n {
			j, ok := c.joinSlot(c.stack[i], c.saved[a.start+i])
			if !ok {
				c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
					Hint: "the arms leave " + c.format(c.stack[i].t) + " and " + c.format(c.saved[a.start+i].t) +
						" in the same place, which have no common type; declare the type you want and use `as` in each arm"})
				c.abandoned = true
				return
			}
			c.stack[i] = j
		}
	}
}

// joinSlot joins two slots. A slot with an unsolved variable is unified,
// never joined, so the answer does not depend on checking order.
func (c *coreChecker) joinSlot(a, b coreSlot) (coreSlot, bool) {
	fresh := a.fresh && b.fresh
	if a.t == b.t {
		return coreSlot{t: a.t, fresh: fresh}, true
	}
	if c.hasVars(a.t) || c.hasVars(b.t) {
		return coreSlot{t: a.t, fresh: fresh}, c.uni.Unify(a.t, b.t)
	}
	s, ok := c.rel.JoinSlot(
		Slot{Type: c.subst.Apply(c.arena, a.t), Fresh: c.freshish(a)},
		Slot{Type: c.subst.Apply(c.arena, b.t), Fresh: c.freshish(b)})
	return coreSlot{t: s.Type, fresh: s.Fresh}, ok
}

// ---------------------------------------------------------------------------
// Formatting

func (c *coreChecker) format(t TypeId) string {
	return FormatType(c.arena, c.names, c.subst.Apply(c.arena, t))
}

func (c *coreChecker) formatTypes(ts []TypeId) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = c.format(t)
	}
	return "(" + strings.Join(parts, " ") + ")"
}

func (c *coreChecker) formatSlots(ss []coreSlot) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = c.format(s.t)
	}
	return "(" + strings.Join(parts, " ") + ")"
}

func (c *coreChecker) formatCandidates(sigs []coreSig) string {
	parts := make([]string, len(sigs))
	for i := range sigs {
		named := make([]TypeId, len(sigs[i].gens))
		for g, name := range sigs[i].gens {
			named[g] = c.arena.MakeRigid(name)
		}
		ins := make([]TypeId, len(sigs[i].ins))
		for j, t := range sigs[i].ins {
			ins[j] = c.rel.SubstParams(t, named)
		}
		parts[i] = c.formatTypes(ins)
	}
	return "it takes " + strings.Join(parts, " or ")
}
