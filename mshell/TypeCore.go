package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The type checker: the checker described in ai/type-core-calculus.typ
// (the "core checker" of ai/type-system-plan.md).
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
	// The declarations of the startup files (TypeCoreDecl.go): enum names,
	// constructors, every declared name, and their errors.
	enums    map[NameId]uint32
	ctors    map[NameId]*coreCtor
	declared map[NameId]Token
	declErrs []TypeError
}

// NewCoreBase builds the base: the builtin table, the signatures of
// stdlibDefs (their bodies are not checked: their signatures are trusted,
// like the builtins'), and
// the startup files' declarations, decls.
func NewCoreBase(stdlibDefs []MShellDefinition, decls []MShellParseItem) *CoreBase {
	arena, names := NewTypeArena(), NewNameTable()
	res := &coreResolver{arena: arena, names: names, rel: NewRelations(arena), aliases: map[NameId]TypeId{}, self: -1}
	res.declareJson()
	res.declareHtmlNode()
	res.builtin = true
	table := buildCoreTable(res)
	res.builtin = false
	b := &CoreBase{arena: arena, names: names, table: table}
	if len(decls) > 0 {
		// Declared in the base itself, so every check sees them, and before
		// the startup files' signatures, which may name them.
		defNames := make(map[string]Token, len(stdlibDefs))
		for i := range stdlibDefs {
			if _, ok := defNames[stdlibDefs[i].Name]; !ok {
				defNames[stdlibDefs[i].Name] = withFile(stdlibDefs[i].NameToken, stdlibDefs[i].File)
			}
		}
		c := &coreChecker{arena: arena, names: names, rel: res.rel, table: table, res: *res, defs: map[NameId]*coreSig{}}
		c.declareAll(decls, defNames)
		res.aliases, res.enums = c.res.aliases, c.res.enums
		b.enums, b.ctors, b.declared, b.declErrs = c.res.enums, c.ctors, c.declared, c.errs
	}
	for i := range stdlibDefs {
		def := &stdlibDefs[i]
		id := names.Intern(def.Name)
		if table.name(id) != nil {
			continue
		}
		parts := res.resolveSig(def.Inputs, def.Outputs)
		// A startup file's signature that does not resolve is reported with
		// its file, as its declarations are: its callers would otherwise see
		// a type with nothing in it.
		for _, e := range res.errs {
			e.Pos = withFile(e.Pos, def.File)
			b.declErrs = append(b.declErrs, e)
		}
		res.errs = res.errs[:0]
		sig := newCoreSig(arena, parts)
		sig.freeOut = outputOnlyGeneric(arena, parts)
		table.setName(id, []coreSig{sig})
		if e := completionSigError(arena, names, res.rel, table, def, &sig); e != nil {
			e.Pos = withFile(e.Pos, def.File)
			b.declErrs = append(b.declErrs, *e)
		}
	}
	b.aliases = res.aliases
	return b
}

// completionSigError checks a def with `complete` metadata: completionDefs
// gives its body as a quote of type ([str] -- CompletionResult), and the
// completion engine runs it on the words typed so far, so its signature
// must be below that type.
func completionSigError(arena *TypeArena, names *NameTable, rel *Relations, table *coreTable, def *MShellDefinition, sig *coreSig) *TypeError {
	if cmds, err := completionMetadataNames(*def); err != nil || len(cmds) == 0 {
		return nil
	}
	q := arena.MakeQuote(QuoteSig{Inputs: sig.ins, Outputs: sig.outs, Diverges: sig.diverges})
	if rel.Sub(q, table.completion) {
		return nil
	}
	return &TypeError{Kind: TErrTypeMismatch, Pos: def.NameToken,
		Hint: "the completion definition '" + def.Name + "' is run on the words typed so far and must leave a list of strings" +
			" or a completion dict: its signature must be below " + FormatType(arena, names, table.completion) +
			", and it is " + FormatType(arena, names, q)}
}

// CoreTypeCheckProgram checks file, with the startup files' definitions
// and declarations. It returns the formatted errors and `dbg` snapshots,
// and whether there were no errors.
func CoreTypeCheckProgram(file *MShellFile, stdlibDefs []MShellDefinition, decls []MShellParseItem) ([]string, bool) {
	return NewCoreBase(stdlibDefs, decls).Check(file)
}

// Check checks file in a new overlay of the base, and formats its errors
// and its `dbg` snapshots; ok is whether there were no errors. Errors in
// the startup files' declarations come first, with their file.
func (b *CoreBase) Check(file *MShellFile) (out []string, ok bool) {
	diags, arena, names := b.Diagnostics(file)
	out = make([]string, 0, len(b.declErrs)+len(diags))
	for _, e := range b.declErrs {
		where := ""
		if e.Pos.TokenFile != nil {
			where = "in " + e.Pos.TokenFile.Path + ": "
		}
		out = append(out, where+e.Format(b.arena, b.names))
	}
	ok = len(b.declErrs) == 0
	for _, e := range diags {
		switch {
		case e.Severity == SeverityError:
			ok = false
		case e.Kind != TErrDebugDump:
			continue
		}
		out = append(out, e.Format(arena, names))
	}
	return out, ok
}

// Errors checks file in a new overlay of the base and returns its errors,
// with the arena and names that format them.
func (b *CoreBase) Errors(file *MShellFile) ([]TypeError, *TypeArena, *NameTable) {
	all, arena, names := b.Diagnostics(file)
	out := all[:0]
	for _, e := range all {
		if e.Severity == SeverityError {
			out = append(out, e)
		}
	}
	return out, arena, names
}

// Diagnostics is Errors with the informational diagnostics too (a `?`
// that always fails), for the language server.
func (b *CoreBase) Diagnostics(file *MShellFile) ([]TypeError, *TypeArena, *NameTable) {
	c := b.newChecker()
	c.checkFile(file)
	return c.errs, c.arena, c.names
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
	c.res = coreResolver{arena: arena, names: names, rel: rel, aliases: maps.Clone(b.aliases), enums: maps.Clone(b.enums), self: -1}
	c.ctors, c.declared = maps.Clone(b.ctors), maps.Clone(b.declared)
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
	gen    uint32 // the scope generation this entry belongs to
	t      TypeId
	stored bool
	loaded bool
	// set is whether every path so far stored the variable, and unsetRead
	// whether a read without that was recorded (TypeCoreAssign.go).
	set       bool
	unsetRead bool
	// origin is the join origin of the value of the first store, when the
	// variable's type is that value's union (coreSlot.origin).
	origin uint32
}

// coreDbg is a `dbg` word, the types on the stack there (bottom first),
// and the variables set there.
type coreDbg struct {
	tok   Token
	stack []TypeId
	vars  []NameId
}

func (c *coreChecker) recordDbg(tok Token) {
	d := coreDbg{tok: tok, stack: make([]TypeId, 0, len(c.stack)-c.floor)}
	for _, s := range c.stack[c.floor:] {
		d.stack = append(d.stack, s.t)
	}
	for _, name := range c.unitVars {
		if c.vars[name].stored {
			d.vars = append(d.vars, name)
		}
	}
	c.dbgs = append(c.dbgs, d)
}

// formatDbg writes a `dbg` snapshot with the unit's final types: the stack,
// top first, and the variables by name.
func (c *coreChecker) formatDbg(d coreDbg) string {
	var sb strings.Builder
	sb.WriteString("\n  stack (top first):")
	if len(d.stack) == 0 {
		sb.WriteString(" <empty>")
	}
	for i := len(d.stack) - 1; i >= 0; i-- {
		sb.WriteString("\n    " + c.format(d.stack[i]))
	}
	sb.WriteString("\n  vars:")
	if len(d.vars) == 0 {
		sb.WriteString(" <none>")
	}
	slices.SortFunc(d.vars, func(x, y NameId) int { return strings.Compare(c.names.Name(x), c.names.Name(y)) })
	for _, name := range d.vars {
		sb.WriteString("\n    " + c.names.Name(name) + " : " + c.format(c.vars[name].t))
	}
	return sb.String()
}

// coreUnwrap is a `?` and the type of what it unwraps.
type coreUnwrap struct {
	tok Token
	t   TypeId
}

// alwaysNone reports whether t, solved, is Maybe[⊥]: only none.
func (c *coreChecker) alwaysNone(t TypeId) bool {
	n := c.arena.nodes[c.subst.Apply(c.arena, t)]
	return n.Kind == TKEnum && n.A == EnumMaybe && c.arena.enumArgs[n.Extra][0] == TidBottom
}

// coreStore is a store, checked again when its unit is solved.
type coreStore struct {
	tok  Token
	name NameId
	t    TypeId
	v    TypeId
	mark coreMark
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
	// ctors are the enum members' constructors, and declared is every name
	// a declaration took, with where (TypeCoreDecl.go).
	ctors    map[NameId]*coreCtor
	declared map[NameId]Token
	errs  []TypeError
	// origins are the joins that made union types (coreSlot.origin).
	origins []coreOrigin

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
	// firstLoads is the unit's first read of each variable, reported
	// when the variable is never stored.
	firstLoads []Token
	// unwraps are the unit's `?` words and the types they unwrap, to
	// point out the ones that always fail once the unit is solved.
	unwraps []coreUnwrap
	// dbgs are the unit's `dbg` words, with the stack and the variables
	// set there, reported once the unit is solved.
	dbgs []coreDbg
	stores   []coreStore

	// saved holds stacks saved for branches, as a stack of slot runs.
	saved []coreSlot
	// pending holds the unit's quote literals (TypeCoreQuote.go).
	pending []corePending
	// brk and cont are the break and continue contexts; brkSeen records a
	// break that leaves the innermost loop.
	brk, cont coreLoopCtx
	brkSeen   bool
	infer     *coreInfer
	deferred  []coreDeferred
	// choices are the unit's waiting overload choices (TypeCoreChoice.go);
	// choiceVersion is len(uni.pairs) when they were last tried.
	choices       []coreChoice
	choiceVersion int
	// at is the word being checked, where a deferred check reports.
	at Token
	// assertive is set while the patterns of a `=>` are read.
	assertive bool
	escapes       []coreEscape
	// mentionsVar caches, per TypeId, whether a type mentions a
	// unification variable: 0 not yet known, 1 no, 2 yes.
	mentionsVar []uint8
	genBuf      []TypeId // the generics stack (instantiate)
	// parts holds the unit's partly new marks (TypeCorePartial.go).
	parts []corePart
	// litLists holds the unit's list literals of string literals.
	litLists [][]NameId

	// The def being checked, the defs of the file its body calls, and for
	// each of its outputs (bit i) whether every exit so far left a new
	// value there, with the first exit that did not (TypeCoreNew.go).
	curDef     *coreSig
	calls      []*coreSig
	exitNew    uint64
	exitShared []Token
	exits      int

	// Definite assignment (TypeCoreAssign.go): the variables set on this
	// path, in order; the loops being checked; how deep the walk is in
	// quotes typed on their own; reads that may find a variable unset.
	setLog     []NameId
	daLoops    []daLoop
	later      int
	unsetReads []coreUnsetRead
}

// ---------------------------------------------------------------------------
// Files and units

func (c *coreChecker) checkFile(file *MShellFile) {
	defNames := make(map[string]Token, len(file.Definitions))
	for _, def := range file.Definitions {
		c.checkDefName(def, defNames)
	}
	c.declareAll(file.Items, defNames)
	for i := range file.Definitions {
		def := &file.Definitions[i]
		parts := c.res.resolveSig(def.Inputs, def.Outputs)
		c.takeResolveErrors()
		sig := newCoreSig(c.arena, parts)
		c.defs[c.names.Intern(def.Name)] = &sig
		if e := completionSigError(c.arena, c.names, c.rel, c.table, def, &sig); e != nil {
			c.errs = append(c.errs, *e)
		}
	}
	c.checkDefs(file.Definitions)
	c.beginUnit()
	c.stack = c.stack[:0]
	c.ret, c.retOuts = retAny, nil
	c.walk(file.Items)
	c.finishUnit()
}

func (c *coreChecker) takeResolveErrors() {
	c.errs = append(c.errs, c.res.errs...)
	c.res.errs = c.res.errs[:0]
}

func (c *coreChecker) beginUnit() {
	c.varGen++
	c.unitVars = c.unitVars[:0]
	c.firstLoads = c.firstLoads[:0]
	c.unwraps = c.unwraps[:0]
	c.dbgs = c.dbgs[:0]
	c.stores = c.stores[:0]
	c.deferred = c.deferred[:0]
	c.parts = c.parts[:0]
	c.litLists = c.litLists[:0]
	c.setLog, c.daLoops, c.later, c.unsetReads = c.setLog[:0], c.daLoops[:0], 0, c.unsetReads[:0]
	c.pending = c.pending[:0]
	c.choices, c.choiceVersion = c.choices[:0], 0
	c.escapes = c.escapes[:0]
	c.brk, c.cont, c.brkSeen, c.infer = coreLoopCtx{}, coreLoopCtx{}, false, nil
	c.subst.bound = c.subst.bound[:0]
	c.subst.root = nil
	c.uni.pairs = c.uni.pairs[:0]
	c.diverged, c.abandoned = false, false
	c.floor, c.listDepth = 0, 0
}

// finishUnit makes the checks that wait for the unit's final substitution.
func (c *coreChecker) finishUnit() {
	// Every quote body is checked, even one that is never run.
	for i := range c.pending {
		if !c.pending[i].done && !c.abandoned {
			c.inferPending(uint32(i + 1))
		}
	}
	if !c.abandoned {
		c.finishChoices()
	}
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
		if ok, _ := c.markBelow(s.mark, t, v); !ok {
			c.errs = append(c.errs, c.storeError(s.tok, s.name, t, v))
		}
	}
	for _, d := range c.deferred {
		if d.rule != 0 {
			if !c.keyAllowed(d.t, d.rule) {
				c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: d.tok,
					Hint: "'" + d.tok.Lexeme + "' needs " + keyRuleText(d.rule) + ", got " + c.format(d.t)})
			}
			continue
		}
		t, want := c.subst.Apply(c.arena, d.t), c.subst.Apply(c.arena, d.want)
		if ok, _ := c.markBelow(d.mark, t, want); !ok {
			if d.validation {
				c.validationError(d.tok, t, want)
			} else {
				c.mismatch(d.tok, 0, want, t)
			}
		}
	}
	c.finishEscapes()
	c.finishAssign()
	for _, u := range c.unwraps {
		if c.alwaysNone(u.t) {
			c.errs = append(c.errs, TypeError{Kind: TErrUnwrapAlwaysFails, Severity: SeverityInfo, Pos: u.tok,
				Hint: "'?' unwraps a value that can only be none (a key the dict's type says is absent, or `none` itself); this fails at run time"})
		}
	}
	for _, d := range c.dbgs {
		c.errs = append(c.errs, TypeError{Kind: TErrDebugDump, Severity: SeverityInfo, Pos: d.tok, Hint: c.formatDbg(d)})
	}
	for _, tok := range c.firstLoads {
		if !c.vars[c.names.Intern(strings.TrimPrefix(tok.Lexeme, "@"))].stored {
			c.errs = append(c.errs, TypeError{Kind: TErrUnknownIdentifier, Pos: tok, Name: tok.Lexeme})
		}
	}
}

// checkBody checks a def's body against sig and returns its output types,
// with the generics rigid. It records each exit's freshness.
func (c *coreChecker) checkBody(def *MShellDefinition, sig *coreSig) []TypeId {
	c.beginUnit()
	c.curDef, c.calls = sig, c.calls[:0]
	c.res.bodyGens = sig.gens
	defer func() { c.res.bodyGens = nil }()
	c.exitNew, c.exits = ^uint64(0), 0
	c.exitShared = append(c.exitShared[:0], make([]Token, len(sig.outs))...)
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
	c.curDef = nil
	return outs
}

// checkOutputs checks that the stack at the end of a def body is its
// declared outputs.
func (c *coreChecker) checkOutputs(def *MShellDefinition, outs []TypeId) {
	c.forceTop(len(c.stack))
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
	c.exit(Token{})
}

// unsupported reports a construct the checker has no rule for, and stops
// checking the unit.
func (c *coreChecker) unsupported(tok Token, what string) {
	c.errs = append(c.errs, TypeError{Kind: TErrCoreUnsupported, Pos: tok, Hint: what})
	c.abandoned = true
}

// ---------------------------------------------------------------------------
// Walking

func (c *coreChecker) walk(items []MShellParseItem) {
	for i := 0; i < len(items); i++ {
		if c.diverged || c.abandoned {
			// Words after a diverging word are not checked (t_div).
			return
		}
		if l, ok := items[i].(*MShellParseList); ok && i+1 < len(items) && isWord(items[i+1], "groupBy") &&
			c.groupBySpecs(l, items[i+1].(Token)) {
			// A spec list written at a grid groupBy is checked against it
			// spec by spec (TypeCoreGrid.go).
			i++
		} else {
			c.step(items[i])
		}
		if len(c.choices) > 0 {
			c.retryChoices()
		}
	}
}

// isWord reports whether item is the word name.
func isWord(item MShellParseItem, name string) bool {
	tok, ok := item.(Token)
	return ok && tok.Type == LITERAL && tok.Lexeme == name
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
	case *MShellTypeDecl, *MShellEnumDecl:
	case *MShellParseQuote:
		c.pushQuote(it.Items, it.StartToken)
	case *MShellParsePrefixQuote:
		// `.each ... end` is `(...) each`.
		c.pushQuote(it.Items, it.StartToken)
		call := it.StartToken
		call.Type, call.Lexeme = LITERAL, strings.Trim(call.Lexeme, ".")
		c.word(call)
	case *MShellParseMatchBlock:
		c.matchBlock(it)
	case *MShellParseGrid:
		c.gridLiteral(it)
	case *MShellIndexerList:
		sigs := c.table.slice
		if len(it.Indexers) > 1 {
			sigs = c.table.multiIndex
			for _, ix := range it.Indexers {
				if ix.(Token).Type != INDEXER {
					sigs = c.table.multi
				}
			}
		} else if it.Indexers[0].(Token).Type == INDEXER {
			sigs = c.table.index
		}
		c.call(sigs, it.GetStartToken())
	case *MShellGetter:
		c.getter(it)
	case *MShellParseFormatString:
		c.formatString(it)
	case *MShellAsCast:
		c.ascribe(it)
	case *MShellTryAs:
		c.tryAs(it)
	default:
		c.unsupported(item.GetStartToken(), fmt.Sprintf("%T", item))
	}
}

func (c *coreChecker) push(t TypeId, fresh bool) {
	c.stack = append(c.stack, coreSlot{t: t, fresh: fresh})
}

// need reports whether n slots are available, reporting an underflow if not.
func (c *coreChecker) need(n int, tok Token) bool {
	avail := len(c.stack) - c.floor
	if avail >= n {
		return true
	}
	if c.infer != nil && c.floor == c.infer.floor {
		// A quote typed on its own reads inputs from below its own pushes:
		// they become new variables, below everything on its stack.
		k := n - avail
		ins := make([]TypeId, k, k+len(c.infer.ins))
		for i := range ins {
			ins[i] = c.subst.FreshVar(c.arena)
		}
		c.infer.ins = append(ins, c.infer.ins...)
		c.stack = append(c.stack, make([]coreSlot, k)...)
		copy(c.stack[c.floor+k:], c.stack[c.floor:len(c.stack)-k])
		for i := range k {
			c.stack[c.floor+i] = coreSlot{t: ins[i]}
		}
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
	case STRING, SINGLEQUOTESTRING:
		c.push(TidStr, true)
		if v, ok := tok.Value.(MShellString); ok {
			c.stack[len(c.stack)-1].lit = c.names.Intern(v.Content)
		}
	case FORMATSTRING:
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
		// The runtime exports a str, a path or an int.
		if c.need(1, tok) {
			c.forceTop(1)
			i := len(c.stack) - 1
			want := c.arena.MakeUnion([]TypeId{TidStr, TidPath, TidInt})
			if !c.check(c.stack[i], want) {
				c.mismatch(tok, 0, want, c.stack[i].t)
			}
			c.stack = c.stack[:i]
		}
	case LITERAL:
		c.word(tok)
	case INTERPRET:
		c.interpret(tok)
	case IFF:
		c.iff(tok)
	case LOOP:
		c.loop(tok)
	case BREAK, CONTINUE:
		c.breakOrContinue(tok)
	default:
		if tok.Type == PLUS && c.gridConcat(tok) {
			return
		}
		if c.commandWord(tok) {
			return
		}
		if tok.Type == QUESTION && len(c.stack) > c.floor {
			c.unwraps = append(c.unwraps, coreUnwrap{tok: tok, t: c.stack[len(c.stack)-1].t})
		}
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
	if tok.Lexeme == "dbg" {
		c.recordDbg(tok)
	}
	if tok.Lexeme == "return" {
		c.doReturn(tok)
		return
	}
	if id, ok := c.names.Lookup(tok.Lexeme); ok {
		if sig := c.defs[id]; sig != nil {
			if c.curDef != nil {
				c.calls = append(c.calls, sig)
			}
			c.apply(sig, tok)
			return
		}
		if ct := c.ctors[id]; ct != nil {
			c.apply(&ct.sig, tok)
			return
		}
	}
	if c.gridWord(tok) || c.dictWord(tok) || c.commandWord(tok) {
		return
	}
	if (tok.Lexeme == "and" || tok.Lexeme == "or") && c.andOr(tok) {
		return
	}
	if tok.Lexeme == "append" {
		c.widenForAppend()
		if c.appendBelow(tok) {
			return
		}
	}
	if id, ok := c.names.Lookup(tok.Lexeme); ok {
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

// widenForAppend retypes a fresh list that `append` is about to add a
// fresh or immutable value to, so its element type covers the value: no
// other view of the list exists to see the change (Retype, as `as` would).
// The join is above both under fresh retyping (join_slot_ub).
func (c *coreChecker) widenForAppend() {
	if len(c.stack)-c.floor < 2 {
		return
	}
	n := len(c.stack)
	li, vi := n-2, n-1
	if t := c.subst.Apply(c.arena, c.stack[n-1].t); c.arena.nodes[t].Kind == TKList {
		if b := c.subst.Apply(c.arena, c.stack[n-2].t); c.arena.nodes[b].Kind != TKList {
			li, vi = n-1, n-2
		}
	}
	l, v := c.stack[li], c.stack[vi]
	lt := c.subst.Apply(c.arena, l.t)
	if !l.fresh || c.waiting(v) != nil || !c.freshish(v) || c.arena.nodes[lt].Kind != TKList {
		return
	}
	elem := TypeId(c.arena.nodes[lt].A)
	if c.hasVars(elem) || c.hasVars(v.t) {
		return
	}
	j, ok := c.rel.JoinSlot(Slot{Type: c.subst.Apply(c.arena, elem), Fresh: true}, Slot{Type: c.subst.Apply(c.arena, v.t), Fresh: true})
	if ok && j.Type != elem {
		c.stack[li].t = c.arena.MakeList(j.Type)
	}
}

// appendBelow checks `x [xs] append`: a list on top and, below it, a value
// that cannot be a list. It reports false otherwise, for the table.
func (c *coreChecker) appendBelow(tok Token) bool {
	if len(c.stack)-c.floor < 2 {
		return false
	}
	n := len(c.stack)
	top := c.subst.Apply(c.arena, c.stack[n-1].t)
	below := c.subst.Apply(c.arena, c.stack[n-2].t)
	if c.arena.nodes[top].Kind != TKList || c.waiting(c.stack[n-2]) != nil {
		return false
	}
	var ms []TypeId
	if !c.members(below, &ms) {
		return false
	}
	for _, m := range ms {
		if k := c.arena.nodes[m].Kind; k == TKList || k == TKCommand || k == TKVar {
			return false
		}
	}
	c.apply(&c.table.appendBelow, tok)
	return true
}

// shuffle checks the stack words. A word that copies a reference makes
// both copies shared; one that only moves slots keeps their marks.
func (c *coreChecker) shuffle(tok Token) bool {
	s := c.stack
	n := len(s)
	switch tok.Lexeme {
	case "dup":
		if c.need(1, tok) {
			c.forceTop(1)
			s, n = c.stack, len(c.stack)
			s[n-1].share()
			c.stack = append(s, s[n-1])
		}
	case "drop":
		if c.need(1, tok) {
			c.forceTop(1)
			c.stack = c.stack[:len(c.stack)-1]
		}
	case "swap":
		if c.need(2, tok) {
			s, n = c.stack, len(c.stack)
			s[n-2], s[n-1] = s[n-1], s[n-2]
		}
	case "over":
		if c.need(2, tok) {
			c.force(len(c.stack) - 2)
			s, n = c.stack, len(c.stack)
			s[n-2].share()
			c.stack = append(s, s[n-2])
		}
	case "rot":
		if c.need(3, tok) {
			s, n = c.stack, len(c.stack)
			s[n-3], s[n-2], s[n-1] = s[n-2], s[n-1], s[n-3]
		}
	case "-rot":
		if c.need(3, tok) {
			s, n = c.stack, len(c.stack)
			s[n-3], s[n-2], s[n-1] = s[n-1], s[n-3], s[n-2]
		}
	case "nip":
		if c.need(2, tok) {
			c.force(len(c.stack) - 2)
			s, n = c.stack, len(c.stack)
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
		c.forceTop(len(c.stack))
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
		c.exit(tok)
	}
	c.diverged = true
}

// ---------------------------------------------------------------------------
// Variables

func (c *coreChecker) varOf(name NameId) *coreVar {
	if int(name) >= len(c.vars) {
		// One allocation covers every name interned so far.
		n := max(int(name)+1, int(c.names.Len())+16)
		c.vars = slices.Grow(c.vars, n-len(c.vars))[:n]
	}
	v := &c.vars[name]
	if v.gen != c.varGen {
		*v = coreVar{gen: c.varGen, t: c.subst.FreshVar(c.arena)}
		c.unitVars = append(c.unitVars, name)
	}
	return v
}

func (c *coreChecker) load(tok Token) {
	name := c.names.Intern(strings.TrimPrefix(tok.Lexeme, "@"))
	v := c.varOf(name)
	if !v.loaded {
		v.loaded = true
		c.firstLoads = append(c.firstLoads, tok)
	}
	c.daRead(tok, name, v)
	c.push(v.t, false)
	c.stack[len(c.stack)-1].origin = v.origin
}

// store checks `name!`: the value must fit the variable's one type. A ⊥ in
// the value's type is replaced by a new variable first, so it fixes
// nothing (`none r!` before `5 just r!`).
func (c *coreChecker) store(tok Token, name NameId) {
	c.at = tok
	if !c.need(1, tok) {
		return
	}
	c.forceTop(1)
	slot := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	v := c.varOf(name)
	if !v.stored {
		v.origin = slot.origin
	}
	v.stored = true
	c.daSet(name)
	opened := c.openBottom(c.subst.Apply(c.arena, slot.t))
	if !c.check(coreSlot{t: opened, fresh: slot.fresh, part: slot.part}, v.t) {
		e := c.storeError(tok, name, c.subst.Apply(c.arena, slot.t), c.subst.Apply(c.arena, v.t))
		e.Hint = c.originHint(coreSlot{t: v.t, origin: v.origin})
		c.errs = append(c.errs, e)
		return
	}
	c.stores = append(c.stores, coreStore{tok: tok, name: name, t: slot.t, v: v.t, mark: slotMark(slot)})
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
	case TKGrid, TKGridView, TKGridRow:
		// A grid literal's column of only `none` cells is Maybe[⊥].
		if r := c.openBottom(TypeId(n.A)); r != TypeId(n.A) {
			return ar.MakeGridOf(n.Kind, r)
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
// When want has unsolved variables and the value's type has a ⊥ in it (the
// contents of `none`), the ⊥ is opened to a new variable before unifying,
// so it fixes nothing, and the value is checked against the solved type
// once the unit is solved: `none 5 maybe` gives `a = int`, and
// `Maybe[⊥] <= Maybe[int]` (design doc, "A ⊥ in a store fixes nothing").
func (c *coreChecker) check(slot coreSlot, want TypeId) bool {
	if slot.t == want {
		return true
	}
	if c.hasVars(slot.t) || c.hasVars(want) {
		if got := c.subst.Apply(c.arena, slot.t); c.hasVars(want) && c.mentionsType(got, TidBottom) {
			opened := c.openBottom(got)
			if !c.uni.Unify(opened, want) {
				return false
			}
			c.deferCheck(c.at, slot, want)
			return true
		}
		if c.uni.Unify(slot.t, want) {
			return true
		}
		// Record and covariant positions are matched label by label, then
		// checked in full once the unit is solved.
		// A partly new value is matched as a new one here; the full
		// check, position by position, is the deferred one.
		if c.matchSub(slot.t, want, slot.fresh || slot.part != 0) {
			c.deferCheck(c.at, slot, want)
			return true
		}
		return false
	}
	ok, _ := c.markBelow(slotMark(slot), c.subst.Apply(c.arena, slot.t), c.subst.Apply(c.arena, want))
	return ok
}

// matchSub matches got against want at a checking position where either
// mentions an unsolved variable and plain unification failed. It needs no
// guess: record labels are compared by the per-label rule (or the fresh
// one), with the types inside unified where the rule needs them equal;
// covariant enum arguments, and a fresh list's elements, recurse; anything
// else is unified. Unions are never entered. The caller checks the solved
// types in full when the unit is solved.
func (c *coreChecker) matchSub(got, want TypeId, fresh bool) bool {
	// An earlier label may have solved a variable this one mentions. An
	// alias that is not recursive is its body.
	got, want = c.plainAlias(c.subst.Apply(c.arena, got)), c.plainAlias(c.subst.Apply(c.arena, want))
	if got == want {
		return true
	}
	ar := c.arena
	gn, wn := ar.nodes[got], ar.nodes[want]
	// A recursive alias against a type that is not an alias is unfolded one
	// step. The other side is finite, and every unfolding reaches a
	// constructor before the next, so this ends.
	if gn.Kind == TKAlias && wn.Kind != TKAlias && wn.Kind != TKVar {
		return c.matchSub(ar.aliases[gn.A].Body, want, fresh)
	}
	if wn.Kind == TKAlias && gn.Kind != TKAlias && gn.Kind != TKVar {
		return c.matchSub(got, ar.aliases[wn.A].Body, fresh)
	}
	// A union is entered only by the kind of the other side: its members
	// have distinct kinds, so at most one can hold got, and taking it is no
	// guess (`[] as Json` matches `[T]` against `[Json]`).
	if wn.Kind == TKUnion && gn.Kind != TKUnion && gn.Kind != TKVar {
		if k, ok := c.rel.kindOf(got); ok {
			for _, m := range ar.unionMembers[wn.Extra] {
				if c.rel.hasKind(k, m) {
					return c.matchSub(got, m, fresh)
				}
			}
		}
	}
	if gn.Kind == TKVar || wn.Kind == TKVar || gn.Kind != wn.Kind {
		return c.uni.Unify(got, want)
	}
	if !c.hasVars(got) && !c.hasVars(want) {
		return c.below(fresh, got, want)
	}
	switch gn.Kind {
	case TKRecord:
		x, y := ar.records[gn.Extra], ar.records[wn.Extra]
		ok := true
		recordLabels(x, y, func(f, g RecordField) bool {
			if !ok {
				return false
			}
			ok = c.matchLabel(f, g, fresh)
			return ok
		})
		return ok
	case TKEnum:
		if gn.A != wn.A {
			return false
		}
		params := ar.enumDecls[gn.A].Params
		xs, ys := ar.enumArgs[gn.Extra], ar.enumArgs[wn.Extra]
		for i, p := range params {
			var ok bool
			switch {
			case fresh && p.Fresh:
				ok = c.matchSub(xs[i], ys[i], true)
			case p.Variance == VarCo:
				ok = c.matchSub(xs[i], ys[i], false)
			case p.Variance == VarContra:
				ok = c.matchSub(ys[i], xs[i], false)
			default:
				ok = c.uni.Unify(xs[i], ys[i])
			}
			if !ok {
				return false
			}
		}
		return true
	case TKList:
		if fresh {
			return c.matchSub(TypeId(gn.A), TypeId(wn.A), true)
		}
	case TKGrid, TKGridView, TKGridRow:
		// A grid's schema is a record of its columns.
		if gn.A != 0 && wn.A != 0 {
			return c.matchSub(TypeId(gn.A), TypeId(wn.A), fresh)
		}
	}
	return c.uni.Unify(got, want)
}

// matchLabel matches one label: the object's status f against the view's
// status g, as fieldView (shared) or fieldRetype (fresh) do.
func (c *coreChecker) matchLabel(f, g RecordField, fresh bool) bool {
	present := f.Status == FieldRequired || f.Status == FieldOptional || f.Status == FieldDeletable
	maybe := g.Status == FieldOptional || g.Status == FieldDeletable
	if fresh {
		switch {
		case f.Status == FieldRequired && g.Status == FieldRequired, present && maybe:
			return c.matchSub(f.Type, g.Type, true)
		case f.Status == FieldAbsent && (maybe || g.Status == FieldAbsent), g.Status == FieldOpen:
			return true
		}
		return false
	}
	switch {
	case f.Status == FieldRequired && (g.Status == FieldRequired || g.Status == FieldOptional),
		f.Status == FieldOptional && g.Status == FieldOptional,
		f.Status == FieldDeletable && maybe:
		return c.uni.Unify(f.Type, g.Type)
	case f.Status == FieldAbsent && g.Status == FieldAbsent, g.Status == FieldOpen:
		return true
	}
	return false
}

// coreCheckpoint is a state to roll a trial back to: the unifier's, and
// the checks deferred since.
type coreCheckpoint struct {
	uni      UnifierCheckpoint
	deferred int
}

func (c *coreChecker) checkpoint() coreCheckpoint {
	return coreCheckpoint{uni: c.uni.Checkpoint(), deferred: len(c.deferred)}
}

func (c *coreChecker) rollback(cp coreCheckpoint) {
	c.uni.Rollback(cp.uni)
	c.deferred = c.deferred[:cp.deferred]
}

// call checks a builtin with one or more candidate signatures. The
// candidate is the one the arguments fit; with none, or more than one, it
// is an error.
func (c *coreChecker) call(sigs []coreSig, tok Token) {
	c.at = tok
	if c.infer != nil && c.floor == c.infer.floor {
		// In a quote typed on its own, missing arguments become inputs,
		// as many as every candidate takes.
		arity := len(sigs[0].ins)
		for i := range sigs {
			if len(sigs[i].ins) != arity {
				arity = -1
				break
			}
		}
		if arity >= 0 {
			c.need(arity, tok)
		}
	}
	if len(sigs) == 1 {
		c.apply(&sigs[0], tok)
		return
	}
	fit, nfit := -1, 0
	for i := range sigs {
		if !c.mayFit(&sigs[i]) {
			continue
		}
		cp := c.checkpoint()
		if c.argsFit(&sigs[i]) {
			fit, nfit = i, nfit+1
		}
		c.rollback(cp)
	}
	switch nfit {
	case 1:
		c.apply(&sigs[fit], tok)
	case 0:
		if c.distribute(sigs, tok) {
			return
		}
		if (tok.Type == EQUALS || tok.Type == NOTEQUAL) && c.equality(tok) {
			return
		}
		hint := "the stack has " + c.formatSlots(c.topSlots(sigs)) + "; " + c.formatCandidates(sigs)
		if c.fitsIfNew(sigs) {
			hint += "; " + storedHint
		}
		for _, s := range c.topSlots(sigs) {
			if h := c.originHint(s); h != "" {
				hint += "; " + h
			}
		}
		c.errs = append(c.errs, TypeError{Kind: TErrNoMatchingOverload, Pos: tok, Hint: hint})
		c.abandoned = true
	default:
		c.choose(sigs, tok)
	}
}

// equality checks `=` and `!=` on two values the table's scalar forms do
// not cover: their join must be a type whose values compare without a
// runtime error (equatable).
func (c *coreChecker) equality(tok Token) bool {
	if len(c.stack)-c.floor < 2 {
		return false
	}
	n := len(c.stack)
	j, ok := c.joinSlot(c.stack[n-2], c.stack[n-1])
	if !ok || !c.equatable(c.subst.Apply(c.arena, j.t), false) {
		return false
	}
	c.stack = c.stack[:n-2]
	c.push(TidBool, true)
	return true
}

// equatable reports whether two values of type t compare with = and !=
// without a runtime error: a scalar; a Maybe of an equatable type; a dict
// whose every label holds equatable values; an enum whose every payload is
// equatable. Inside a dict or an enum's payload a union is fine, since
// values of different kinds compare false there (inDict). Through an alias,
// and through a recursive enum, it is the greatest fixed point: a type met
// again while it is being decided counts as equatable.
func (c *coreChecker) equatable(t TypeId, inDict bool) bool {
	return c.equatableIn(t, inDict, nil)
}

// isScalarTid reports whether t is a base type other than null.
func isScalarTid(t TypeId) bool {
	switch t {
	case TidInt, TidFloat, TidStr, TidBool, TidBytes, TidPath, TidDateTime:
		return true
	}
	return false
}

func (c *coreChecker) equatableIn(t TypeId, inDict bool, visiting []TypeId) bool {
	switch t {
	case TidInt, TidFloat, TidStr, TidBool, TidBytes, TidPath, TidDateTime, TidNull, TidBottom:
		return true
	}
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKEnum:
		args := c.arena.enumArgs[n.Extra]
		if n.A == EnumMaybe {
			return c.equatableIn(args[0], false, visiting)
		}
		if slices.Contains(visiting, t) {
			return true
		}
		// Enums that refer to each other with growing arguments
		// (`A[t] = a B[[t]]`, `B[t] = b A[t]`) never meet the same type
		// again; past this depth the answer is no, which is safe.
		if len(visiting) >= 64 {
			return false
		}
		visiting = append(visiting, t)
		for _, ctor := range c.arena.EnumDecl(n.A).Ctors {
			for _, p := range ctor.Payload {
				if !c.equatableIn(c.rel.SubstParams(p, args), true, visiting) {
					return false
				}
			}
		}
		return true
	case TKAlias:
		if slices.Contains(visiting, t) {
			return true
		}
		return c.equatableIn(c.arena.aliases[n.A].Body, inDict, append(visiting, t))
	case TKUnion:
		members := c.arena.unionMembers[n.Extra]
		if !inDict {
			// Values of different kinds are a runtime error, except that
			// null compares (unequal) with any scalar, on either side.
			return len(members) == 2 && slices.Contains(members, TidNull) &&
				slices.ContainsFunc(members, isScalarTid)
		}
		for _, m := range members {
			if !c.equatableIn(m, false, visiting) {
				return false
			}
		}
		return true
	case TKRecord:
		rec := c.arena.records[n.Extra]
		for _, f := range append(rec.Fields, rec.Rest) {
			switch f.Status {
			case FieldOpen:
				return false
			case FieldAbsent:
			default:
				if !c.equatableIn(f.Type, true, visiting) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// distribute checks an overloaded word whose argument is a union as a
// match with one arm per member: each member is checked on its own, and
// the arms are joined (design doc, "Elaboration": an overloaded op on a
// union operand). It reports false when no argument is a union.
func (c *coreChecker) distribute(sigs []coreSig, tok Token) bool {
	top := c.topSlots(sigs)
	idx := -1
	for i, s := range top {
		if c.waiting(s) == nil && c.arena.nodes[c.subst.Apply(c.arena, s.t)].Kind == TKUnion {
			idx = len(c.stack) - len(top) + i
			break
		}
	}
	if idx < 0 {
		return false
	}
	u := c.subst.Apply(c.arena, c.stack[idx].t)
	members := c.arena.unionMembers[c.arena.nodes[u].Extra]
	mark := len(c.saved)
	entry := c.saveStack()
	var runs []savedRun
	for _, m := range members {
		c.restoreStack(entry)
		c.stack[idx].t = m
		c.call(sigs, tok)
		if c.abandoned {
			c.saved = c.saved[:mark]
			return true
		}
		runs = append(runs, c.saveArm())
	}
	c.joinArms(runs, tok)
	c.saved = c.saved[:mark]
	return true
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

// fitsIfNew reports whether some candidate would fit if the stored values
// on the stack were new: they were made at a type the word could have
// taken, but not given it.
func (c *coreChecker) fitsIfNew(sigs []coreSig) bool {
	top := c.topSlots(sigs)
	saved := append([]coreSlot(nil), top...)
	changed := false
	for i := range top {
		if !top[i].fresh {
			top[i].fresh, top[i].part = true, 0
			changed = true
		}
	}
	fits := false
	if changed {
		for i := range sigs {
			cp := c.checkpoint()
			if c.argsFit(&sigs[i]) {
				fits = true
			}
			c.rollback(cp)
		}
	}
	copy(top, saved)
	return fits
}

// instantiate makes new unification variables for sig's generics, on the
// generics stack: they stay valid, even when a quote body checked in
// between instantiates again, until release(mark).
func (c *coreChecker) instantiate(sig *coreSig) (gens []TypeId, mark int) {
	mark = len(c.genBuf)
	for range sig.gens {
		c.genBuf = append(c.genBuf, c.subst.FreshVar(c.arena))
	}
	return c.genBuf[mark:len(c.genBuf):len(c.genBuf)], mark
}

func (c *coreChecker) releaseGens(mark int) { c.genBuf = c.genBuf[:mark] }

// mayFit is a quick test before argsFit: it is false when some argument
// is of a kind its parameter cannot hold, comparing only the heads of the
// two types, so a candidate that cannot fit costs no instantiation. It is
// true whenever it cannot tell (a generic, a variable, a union, an alias,
// ⊥, a waiting quote), so it never rejects a candidate argsFit accepts.
func (c *coreChecker) mayFit(sig *coreSig) bool {
	n := len(sig.ins)
	if len(c.stack)-c.floor < n {
		return false
	}
	base := len(c.stack) - n
	for i, want := range sig.ins {
		s := c.stack[base+i]
		if s.pq != 0 {
			continue
		}
		wk := c.arena.nodes[want].Kind
		switch wk {
		case TKParam, TKVar, TKRigid, TKUnion, TKAlias, TKAbstract:
			continue
		}
		got := c.subst.Apply(c.arena, s.t)
		gk := c.arena.nodes[got].Kind
		switch gk {
		case TKVar, TKRigid, TKUnion, TKAlias, TKAbstract:
			continue
		}
		if wk != gk {
			return false
		}
		if wk == TKPrim && got != want && got != TidBottom && want != TidUnknown {
			return false
		}
	}
	return true
}

// argsFit reports whether the stack's top fits sig's inputs, unifying as
// it goes; the caller rolls back.
func (c *coreChecker) argsFit(sig *coreSig) bool {
	n := len(sig.ins)
	if len(c.stack)-c.floor < n {
		return false
	}
	gens, mark := c.instantiate(sig)
	defer c.releaseGens(mark)
	base := len(c.stack) - n
	ok := true
	c.eachInput(sig, gens, c.stackArg(base), func(i int, want TypeId) {
		if !ok {
			return
		}
		if c.waiting(c.stack[base+i]) != nil {
			// A quote literal fits a quote parameter, or a generic; its
			// body is checked once a candidate is chosen.
			w := c.subst.Apply(c.arena, want)
			ok = c.arena.nodes[w].Kind == TKQuote || c.hasVars(w)
			return
		}
		ok = c.check(c.stack[base+i], want)
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
// A waiting quote literal is visited last, once the other arguments have
// fixed what its parameter type can be.
// arg reads argument i; it is read again after each visit, since checking
// a quote body can move the stack.
func (c *coreChecker) eachInput(sig *coreSig, gens []TypeId, arg func(i int) coreSlot, f func(i int, want TypeId)) {
	for pass := 0; pass < 3; pass++ {
		if pass == 1 {
			c.joinRepeated(sig, gens, arg)
		}
		for i, want := range sig.ins {
			waiting := c.waiting(arg(i)) != nil
			bare := c.arena.nodes[want].Kind == TKParam
			if waiting != (pass == 2) || (!waiting && bare != (pass == 1)) {
				continue
			}
			if sig.genIn&(1<<i) != 0 {
				want = c.rel.SubstParams(want, gens)
			}
			f(i, want)
		}
	}
}

// stackArg reads the arguments that start at stack index base.
func (c *coreChecker) stackArg(base int) func(i int) coreSlot {
	return func(i int) coreSlot { return c.stack[base+i] }
}

// joinRepeated sets each unsolved generic that is two or more bare inputs
// of sig to the join of the arguments there, when they are all solved.
func (c *coreChecker) joinRepeated(sig *coreSig, gens []TypeId, argAt func(i int) coreSlot) {
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
			arg := argAt(i)
			if c.waiting(arg) != nil || c.hasVars(arg.t) {
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
	c.at = tok
	if sig.freeOut {
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
			Hint: "the standard library's signature of '" + tok.Lexeme + "' gives an output whose type no input fixes," +
				" so its result has no type; the signature needs fixing"})
		c.abandoned = true
		return
	}
	n := len(sig.ins)
	if !c.need(n, tok) {
		return
	}
	gens, mark := c.instantiate(sig)
	defer c.releaseGens(mark)
	base := len(c.stack) - n
	c.eachInput(sig, gens, c.stackArg(base), func(i int, want TypeId) {
		if s := c.stack[base+i]; c.waiting(s) != nil {
			c.checkPending(s.pq, want, sig.child, base, tok)
			c.stack[base+i].pq = 0
			return
		}
		if !c.check(c.stack[base+i], want) {
			c.mismatchSlot(tok, i, want, c.stack[base+i])
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
			fresh = c.newOverImmutable(t)
		}
		c.push(t, fresh)
	}
}

// newOverImmutable reports whether t, the type of a new list or grid over
// shared elements or cells, is fresh: its elements, or every column, are
// immutable, as solved so far (design doc, "One rule for new lists").
func (c *coreChecker) newOverImmutable(t TypeId) bool {
	t = c.subst.Apply(c.arena, t)
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKList:
		return c.rel.Immutable(TypeId(n.A))
	case TKGrid:
		_, rec, ok := c.gridType(t)
		return ok && c.schemaImmutable(rec)
	}
	return false
}

// mismatchSlot is mismatch for the value in slot s, naming the arms its
// union came from when a join made it.
func (c *coreChecker) mismatchSlot(tok Token, i int, want TypeId, s coreSlot) {
	c.mismatch(tok, i, want, s.t)
	if h := c.originHint(s); h != "" {
		e := &c.errs[len(c.errs)-1]
		if e.Hint != "" {
			e.Hint += "; "
		}
		e.Hint += h
	}
}

func (c *coreChecker) mismatch(tok Token, i int, want, got TypeId) {
	want, got = c.subst.Apply(c.arena, want), c.subst.Apply(c.arena, got)
	e := TypeError{Kind: TErrTypeMismatch, Pos: tok, Expected: want, Actual: got, ArgIndex: i}
	if !c.hasVars(want) && !c.hasVars(got) && c.rel.Retype(got, want) {
		e.Hint = storedHint
	}
	c.errs = append(c.errs, e)
}

// storedHint is the fix for a stored value that would fit if it were new.
const storedHint = "a stored value keeps its type, so give it the type the word takes where it is made" +
	" (`... as T x!`), or make a new one with deepCopy"

// ascribe checks `as T` (design doc, "Freshness"): the value must be below
// T, or, when it is fresh, retypable to T. The slot keeps its fresh mark.
func (c *coreChecker) ascribe(a *MShellAsCast) {
	target := c.res.resolveType(a.Target)
	c.takeResolveErrors()
	if target == TidNothing || !c.need(1, a.AsToken) {
		return
	}
	c.forceTop(1)
	s := &c.stack[len(c.stack)-1]
	if s.part != 0 && !c.hasVars(s.t) {
		// Retyped position by position, it stays partly new; committed, it
		// is shared.
		if ok, kept := c.markBelow(slotMark(*s), c.subst.Apply(c.arena, s.t), target); ok {
			if !kept {
				s.share()
			}
			s.t = target
			return
		}
		s.share()
	}
	if !c.check(*s, target) {
		got := c.subst.Apply(c.arena, s.t)
		hint := "'as' needs evidence: " + c.format(got) + " is not below " + c.format(target)
		if !s.fresh && !c.rel.Immutable(got) && c.rel.Retype(got, target) {
			hint += "; a shared value keeps its type, so make a new one first with deepCopy"
		} else {
			hint += "; to check data from outside, use tryAs"
		}
		c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: a.AsToken, Hint: hint})
	}
	c.stack[len(c.stack)-1].t = target
}

// formatString checks a format string: each interpolation runs on its own
// stack and leaves one str, path or int; break and continue cannot leave
// it.
func (c *coreChecker) formatString(fs *MShellParseFormatString) {
	brk, cont := c.brk, c.cont
	c.brk, c.cont = coreLoopCtx{}, coreLoopCtx{}
	allowed := c.arena.MakeUnion([]TypeId{TidStr, TidPath, TidInt})
	for i, items := range fs.Interpolations {
		start, outerFloor := c.child(items)
		c.floor = outerFloor
		if c.diverged || c.abandoned {
			break
		}
		if len(c.stack)-start != 1 {
			c.errs = append(c.errs, TypeError{Kind: TErrChildStack, Pos: fs.InterpolationStart(i),
				Hint: "a format-string interpolation must leave exactly one value, but this one leaves " + strconv.Itoa(len(c.stack)-start)})
			c.abandoned = true
			break
		}
		c.forceTop(1)
		if v := c.stack[start]; !c.check(v, allowed) {
			c.mismatch(fs.InterpolationStart(i), 0, allowed, v.t)
		}
		c.stack = c.stack[:start]
	}
	c.brk, c.cont = brk, cont
	if !c.diverged && !c.abandoned {
		c.push(TidStr, true)
	}
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
// when every element is fresh or immutable (ShapeLit), and otherwise a new
// list of stored values (TypeCorePartial.go).
func (c *coreChecker) listLiteral(l *MShellParseList) {
	c.listDepth++
	start, outerFloor := c.child(l.Items)
	c.listDepth--
	c.floor = outerFloor
	if c.diverged || c.abandoned {
		return
	}
	c.forceTop(len(c.stack) - start)
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
	out := coreSlot{t: c.arena.MakeList(elem.t), fresh: fresh}
	if !fresh {
		out.part = c.listPart()
	}
	if names, ok := c.literalNames(elems); ok {
		out.lit = names
	}
	c.stack = append(c.stack[:start], out)
}

// literalNames records the elements of a list literal when every one is a
// string literal, and returns the slot's lit for them.
func (c *coreChecker) literalNames(elems []coreSlot) (NameId, bool) {
	names := make([]NameId, len(elems))
	for i, e := range elems {
		if names[i] = e.key(); names[i] == NameNone {
			return NameNone, false
		}
	}
	if len(c.litLists) >= int(litListTag-1) {
		return NameNone, false
	}
	c.litLists = append(c.litLists, names)
	return litListTag | NameId(len(c.litLists)), true
}

// dictLiteral types `{k: v, ...}`: a shape with exactly its keys, fresh
// when every value is fresh or immutable, and otherwise a new dict whose
// stored values keep their types (TypeCorePartial.go).
func (c *coreChecker) dictLiteral(d *MShellParseDict) {
	fields := make([]RecordField, 0, len(d.Items))
	fresh := true
	var labels []coreLabelMark
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
		c.forceTop(1)
		v := c.stack[start]
		c.stack = c.stack[:start]
		name := c.names.Intern(kv.Key)
		m := c.innerMark(v)
		if m != markNew {
			fresh = false
		}
		labels = append(labels, coreLabelMark{name: name, m: m})
		fields = append(fields, RecordField{Name: name, Status: FieldRequired, Type: v.t})
	}
	out := coreSlot{t: c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent}), fresh: fresh}
	if !fresh {
		out.part = c.newPart(corePart{labels: labels, rest: markNew})
	}
	c.stack = append(c.stack, out)
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
	// Definite assignment: an arm starts with what the conditions before it
	// set, and the if keeps what every arm that goes on set.
	daMark := len(c.setLog)
	var condSets, armSets [][]NameId
	label := "the `if` branch"
	runArm := func(body []MShellParseItem) {
		line := tok.Line
		if len(body) > 0 {
			line = body[0].GetStartToken().Line
		}
		c.daRestore(daMark)
		for _, s := range condSets {
			for _, n := range s {
				c.daSet(n)
			}
		}
		c.walk(body)
		if !c.abandoned {
			arms = append(arms, savedRun{start: len(c.saved), diverged: c.diverged, label: label, line: line})
			c.saved = append(c.saved, c.stack...)
			arms[len(arms)-1].end = len(c.saved)
			if !c.diverged {
				armSets = append(armSets, c.daSince(daMark))
			}
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
		c.daRestore(daMark)
		for _, s := range condSets {
			for _, n := range s {
				c.daSet(n)
			}
		}
		// The runtime refuses a break or continue in an else-if
		// condition, so the condition is walked outside the loop.
		brk, cont := c.brk, c.cont
		c.brk, c.cont = coreLoopCtx{}, coreLoopCtx{}
		c.walk(ei.Condition)
		c.brk, c.cont = brk, cont
		if c.diverged || c.abandoned || !c.condition(tok) {
			c.diverged = false
			break
		}
		condSets = append(condSets, c.daSince(daMark))
		from = c.saveStack()
		label = "an `else*` branch"
		runArm(ei.Body)
	}
	if !c.abandoned {
		c.restoreStack(from)
		if b.ElseBody != nil {
			label = "the `else` branch"
			runArm(b.ElseBody)
		} else {
			label = "the missing `else`"
			runArm(nil)
		}
	}
	if !c.abandoned {
		c.joinArms(arms, tok)
		c.daJoin(daMark, armSets)
	}
	c.saved = c.saved[:mark]
}

// condition pops an if condition: bool, or int.
func (c *coreChecker) condition(tok Token) bool {
	if !c.need(1, tok) {
		return false
	}
	c.forceTop(1)
	slot := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	if c.hasVars(slot.t) {
		if !c.uni.Unify(slot.t, TidBool) {
			c.mismatch(tok, 0, TidBool, slot.t)
		}
		return true
	}
	t := c.subst.Apply(c.arena, slot.t)
	if !c.rel.Sub(t, c.arena.MakeUnion([]TypeId{TidBool, TidInt})) {
		c.mismatchSlot(tok, 0, TidBool, slot)
	}
	return true
}

// savedRun is a stack saved in c.saved.
type savedRun struct {
	start, end int
	diverged   bool
	// label and line name the arm in a message about a union its join
	// made: "the `else` branch", and the line where it starts.
	label string
	line  int
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
	// A quote literal waiting in the same place in every arm stays waiting;
	// any other is typed first.
	for i := range n {
		pq := c.saved[first.start+i].pq
		same := true
		for _, a := range live[1:] {
			if c.saved[a.start+i].pq != pq {
				same = false
			}
		}
		if same {
			continue
		}
		for _, a := range live {
			if q := c.saved[a.start+i].pq; q != 0 {
				if c.pending[q-1].done {
					c.saved[a.start+i].pq = 0
					continue
				}
				c.inferPending(q)
				c.saved[a.start+i].pq = 0
			}
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
	c.noteOrigins(live, tok)
}

// coreOrigin is a join that made a union: which arm left which type in
// the slot, so an error about the value can point back at the arms.
type coreOrigin struct {
	tok  Token
	u    TypeId
	arms []coreOriginArm
}

type coreOriginArm struct {
	label string
	line  int
	t     TypeId
}

// noteOrigins records, for each slot where the join of the arms is a
// union that no arm left alone, which arm left which type.
func (c *coreChecker) noteOrigins(live []savedRun, tok Token) {
	if len(live) < 2 || live[0].label == "" {
		return
	}
	for i := range c.stack {
		if i >= live[0].end-live[0].start {
			break
		}
		u := c.subst.Apply(c.arena, c.stack[i].t)
		if c.arena.nodes[u].Kind != TKUnion {
			continue
		}
		made := true
		for _, a := range live {
			if c.subst.Apply(c.arena, c.saved[a.start+i].t) == u {
				made = false
			}
		}
		if !made {
			continue
		}
		o := coreOrigin{tok: tok, u: u}
		for _, a := range live {
			o.arms = append(o.arms, coreOriginArm{label: a.label, line: a.line, t: c.saved[a.start+i].t})
		}
		c.origins = append(c.origins, o)
		c.stack[i].origin = uint32(len(c.origins))
	}
}

// originHint names the arms a slot's union came from, when a join made
// it: "int | str comes from the `if` at line 2: the `if` branch (line 3)
// leaves int, the `else` branch (line 5) leaves str". It is "" for any
// other slot. The slot may hold one member of the union by then (an
// overloaded word checks a union member by member).
func (c *coreChecker) originHint(s coreSlot) string {
	if s.origin == 0 || int(s.origin) > len(c.origins) {
		return ""
	}
	o := c.origins[s.origin-1]
	var b strings.Builder
	b.WriteString(c.format(o.u) + " comes from the `" + o.tok.Lexeme + "` at line " + strconv.Itoa(o.tok.Line) + ":")
	for i, a := range o.arms {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(" " + a.label + " (line " + strconv.Itoa(a.line) + ") leaves " + c.format(a.t))
	}
	return b.String()
}

// joinSlot joins two slots. A slot with an unsolved variable is unified,
// never joined, so the answer does not depend on checking order.
func (c *coreChecker) joinSlot(a, b coreSlot) (coreSlot, bool) {
	fresh := a.fresh && b.fresh
	if a.t == b.t {
		return coreSlot{t: a.t, pq: a.pq, fresh: fresh}, true
	}
	// ⊥ joins to the other side; unifying would set a variable there to ⊥.
	if c.subst.Apply(c.arena, a.t) == TidBottom {
		return coreSlot{t: b.t, fresh: fresh}, true
	}
	if c.subst.Apply(c.arena, b.t) == TidBottom {
		return coreSlot{t: a.t, fresh: fresh}, true
	}
	if c.hasVars(a.t) || c.hasVars(b.t) {
		cp := c.checkpoint()
		if c.uni.Unify(a.t, b.t) {
			return coreSlot{t: a.t, fresh: fresh}, true
		}
		c.rollback(cp)
		// The other side, when one side is below it (the join table): a
		// side with variables is matched against a solved one, by <= or,
		// when both are fresh, by retyping, and checked in full once the
		// unit is solved. `[]` in one arm and a CompletionResult in the
		// other give CompletionResult.
		for _, p := range [2][2]coreSlot{{a, b}, {b, a}} {
			if c.hasVars(p[1].t) {
				continue
			}
			if c.check(coreSlot{t: p[0].t, fresh: fresh}, p[1].t) {
				return coreSlot{t: p[1].t, fresh: fresh}, true
			}
			c.rollback(cp)
		}
		return coreSlot{t: a.t, fresh: fresh}, false
	}
	at, bt := c.subst.Apply(c.arena, a.t), c.subst.Apply(c.arena, b.t)
	af, bf := c.freshish(a), c.freshish(b)
	if s, ok := c.rel.JoinSlot(Slot{Type: at, Fresh: af}, Slot{Type: bt, Fresh: bf}); ok {
		return coreSlot{t: s.Type, fresh: s.Fresh}, true
	}
	// A new or partly new arm may be retyped to the other arm's type and
	// then forgotten (t_sub twice: ss_dp or ss_m, then ss_forget or
	// ss_m_forget), so a shared arm's type is an upper bound of both:
	// `{values: [...]}` and a stored CompletionResult give CompletionResult.
	for _, p := range [2]struct {
		x      coreSlot
		xt, yt TypeId
		yf     bool
	}{{a, at, bt, bf}, {b, bt, at, af}} {
		if !p.yf && (p.x.fresh || p.x.part != 0) {
			if ok, _ := c.markBelow(slotMark(p.x), p.xt, p.yt); ok {
				return coreSlot{t: p.yt}, true
			}
		}
	}
	return coreSlot{}, false
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
