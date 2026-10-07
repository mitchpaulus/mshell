package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// declErrs are errors in the startup files' declarations, which stop
	// every program (the runtime refuses them too); defErrs are errors in
	// their definitions' signatures and bodies, formatted, which only
	// refuse the code that calls those definitions (checkStartupBodies).
	declErrs []TypeError
	defErrs  []string
	// pool holds checkers that finished a check, to start the next one
	// with their storage (diagnose).
	pool sync.Pool
}

// coreBuiltins is the part of a base that depends only on the binary: an
// arena and name table holding the built-in aliases and enums, and the
// builtin table. A base adds the startup files' declarations and
// signatures to the same arena, so each coreBuiltins is used by one base.
type coreBuiltins struct {
	arena *TypeArena
	names *NameTable
	res   *coreResolver
	table *coreTable
	// The built-in enums' constructors and the names the built-in
	// declarations take (Prompt.go).
	ctors    map[NameId]*coreCtor
	declared map[NameId]Token
}

func newCoreBuiltins() *coreBuiltins {
	arena, names := NewTypeArena(), NewNameTable()
	res := &coreResolver{arena: arena, names: names, rel: NewRelations(arena), aliases: map[NameId]TypeId{}, self: -1}
	res.declareJson()
	res.declareHtmlNode()
	// Before the table, whose signatures name them.
	c := &coreChecker{arena: arena, names: names, rel: res.rel, table: &coreTable{}, res: *res, defs: map[NameId]*coreSig{}}
	c.declareBuiltins()
	res.aliases, res.enums = c.res.aliases, c.res.enums
	res.builtin = true
	table := buildCoreTable(res)
	res.builtin = false
	return &coreBuiltins{arena: arena, names: names, res: res, table: table, ctors: c.ctors, declared: c.declared}
}

// prebuilt receives the builtins PrebuildCoreBuiltins is building, for
// the next base to take.
var prebuilt atomic.Pointer[chan *coreBuiltins]

// PrebuildCoreBuiltins starts building the builtin table on another
// goroutine, for the next base. The shell calls it before it reads the
// script and loads the startup files, which take longer and do not depend
// on it, so a checked script does not wait for the table: in a new process,
// building it takes 1.3 ms or more, most of the checker's cost for a short
// script.
func PrebuildCoreBuiltins() {
	ch := make(chan *coreBuiltins, 1)
	go func() { ch <- newCoreBuiltins() }()
	prebuilt.Store(&ch)
}

// takeCoreBuiltins returns the prebuilt builtins if there are any, and
// builds them otherwise.
func takeCoreBuiltins() *coreBuiltins {
	if ch := prebuilt.Swap(nil); ch != nil {
		return <-*ch
	}
	return newCoreBuiltins()
}

// NewCoreBase builds the base: the builtin table, the startup files'
// declarations, decls, and the signatures of their defs, stdlibDefs, whose
// bodies are checked too (checkStartupBodies).
func NewCoreBase(stdlibDefs []MShellDefinition, decls []MShellParseItem) *CoreBase {
	bi := takeCoreBuiltins()
	arena, names, res, table := bi.arena, bi.names, bi.res, bi.table
	b := &CoreBase{arena: arena, names: names, table: table, enums: res.enums, ctors: bi.ctors, declared: bi.declared}
	if len(decls) > 0 {
		// Declared in the base itself, so every check sees them, and before
		// the startup files' signatures, which may name them.
		defNames := make(map[string]Token, len(stdlibDefs))
		for i := range stdlibDefs {
			if _, ok := defNames[stdlibDefs[i].Name]; !ok {
				defNames[stdlibDefs[i].Name] = withFile(stdlibDefs[i].NameToken, stdlibDefs[i].File)
			}
		}
		c := &coreChecker{arena: arena, names: names, rel: res.rel, table: table, res: *res, defs: map[NameId]*coreSig{},
			ctors: b.ctors, declared: b.declared}
		c.declareAll(decls, defNames)
		res.aliases, res.enums = c.res.aliases, c.res.enums
		b.enums, b.ctors, b.declared, b.declErrs = c.res.enums, c.ctors, c.declared, c.errs
	}
	// owned are the startup defs that define their names (a name a builtin
	// or an earlier def took is refused at run time anyway).
	var owned []int
	for i := range stdlibDefs {
		def := &stdlibDefs[i]
		id := names.Intern(def.Name)
		if table.name(id) != nil {
			continue
		}
		owned = append(owned, i)
		if table.startupDefs == nil {
			table.startupDefs = map[NameId]Token{}
		}
		table.startupDefs[id] = withFile(def.NameToken, def.File)
		parts := res.resolveSig(def.Inputs, def.Outputs)
		// A signature that does not resolve is reported with its file, and
		// a call to the def is refused: its callers would otherwise see a
		// type with nothing in it.
		for _, e := range res.errs {
			e.Pos = withFile(e.Pos, def.File)
			b.brokenDef(def, id, "signature", e, arena, names)
		}
		broken := len(res.errs) > 0
		res.errs = res.errs[:0]
		sig := newCoreSig(arena, parts)
		sig.freeOut = outputOnlyGeneric(arena, parts)
		sig.broken = broken
		table.setName(id, []coreSig{sig})
		if e := completionSigError(arena, names, res.rel, table, def, &sig); e != nil {
			e.Pos = withFile(e.Pos, def.File)
			b.defErrs = append(b.defErrs, formatStartupError(*e, arena, names))
		}
	}
	b.aliases = res.aliases
	c := &coreChecker{arena: b.arena, names: b.names, rel: res.rel, table: b.table, res: *res,
		defs: map[NameId]*coreSig{}, ctors: b.ctors, declared: b.declared}
	c.uni = NewUnifier(c.arena, &c.subst, c.rel)
	b.checkStartupBodies(c, stdlibDefs, owned)
	return b
}

// checkStartupBodies checks the bodies of the startup defs std[i] for i in
// owned, with checker c, as a file's are checked: a body is trusted no
// more than a script's (design doc, "Checking by default"; plan question
// 21). A def whose body has an error stays defined, and a call to it is
// refused with the reason.
func (b *CoreBase) checkStartupBodies(c *coreChecker, defs []MShellDefinition, owned []int) {
	// One file at a time, so each error gets its file; a def's errors are
	// at or after its name and before the next def's in the same file.
	type fileDefs struct {
		file *TokenFile
		defs []MShellDefinition
	}
	var files []fileDefs
	for start := 0; start < len(owned); {
		file := defs[owned[start]].File
		end := start
		group := make([]MShellDefinition, 0, len(owned)-start)
		for end < len(owned) && defs[owned[end]].File == file {
			def := defs[owned[end]]
			c.defs[c.names.Intern(def.Name)] = &b.table.name(c.names.Intern(def.Name))[0]
			group = append(group, def)
			end++
		}
		start = end
		slices.SortFunc(group, func(x, y MShellDefinition) int { return tokenOrder(x.NameToken, y.NameToken) })
		files = append(files, fileDefs{file, group})
	}
	// A def that calls one found broken is broken too: it was checked
	// trusting the other's signature. The other may be in a later file, so
	// check every file again until no def breaks; checkDefs skips the
	// broken ones, so each error is reported once, and with no errors this
	// is one pass.
	for broke := true; broke; {
		broke = false
		for _, f := range files {
			nerr := len(c.errs)
			c.checkDefs(f.defs)
			for _, e := range c.errs[nerr:] {
				if e.Severity != SeverityError {
					continue
				}
				var owner *MShellDefinition
				for i := range f.defs {
					if tokenOrder(f.defs[i].NameToken, e.Pos) <= 0 {
						owner = &f.defs[i]
					}
				}
				e.Pos = withFile(e.Pos, f.file)
				if owner == nil {
					b.defErrs = append(b.defErrs, formatStartupError(e, c.arena, c.names))
					continue
				}
				id := c.names.Intern(owner.Name)
				b.brokenDef(owner, id, "body", e, c.arena, c.names)
				if sig := &b.table.name(id)[0]; !sig.broken {
					sig.broken, broke = true, true
				}
			}
		}
	}
}

// tokenOrder compares two positions in one file.
func tokenOrder(a, b Token) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Column - b.Column
}

// brokenDef records error e in a startup def's signature or body (part),
// whose types are in arena: it is reported once, and the first one is why
// a call to it is refused.
func (b *CoreBase) brokenDef(def *MShellDefinition, id NameId, part string, e TypeError, arena *TypeArena, names *NameTable) {
	b.defErrs = append(b.defErrs, formatStartupError(e, arena, names))
	if b.table.brokenWhy == nil {
		b.table.brokenWhy = map[NameId]string{}
	}
	if _, ok := b.table.brokenWhy[id]; ok {
		return
	}
	msg := strings.TrimPrefix(e.Format(arena, names), "type error ")
	b.table.brokenWhy[id] = "'" + def.Name + "', defined at " + tokenPosStr(withFile(def.NameToken, def.File)) +
		", cannot be checked, so neither can a call to it: its " + part + " has a type error " + msg
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
	b.diagnose(file, func(diags []TypeError, arena *TypeArena, names *NameTable) {
		out, ok = b.formatCheck(diags, arena, names)
	})
	return out, ok
}

// StartupErrors are every error in the startup files, each naming its
// file: the interactive shell prints them once when it starts.
func (b *CoreBase) StartupErrors() []string {
	return append(b.formatStartup(b.declErrs), b.defErrs...)
}

// DefinitionErrors are the errors in the startup files' definitions: a
// call to a definition with one is refused, and the rest of the program
// runs. A script prints them as warnings.
func (b *CoreBase) DefinitionErrors() []string {
	return b.defErrs
}

// DeclarationErrors are the errors in the startup files' declarations,
// which stop every program: every check reports them. An error in a
// startup def only refuses the code that calls it, where it is reported.
func (b *CoreBase) DeclarationErrors() []string {
	return b.formatStartup(b.declErrs)
}

func (b *CoreBase) formatStartup(errs []TypeError) []string {
	var out []string
	for _, e := range errs {
		out = append(out, formatStartupError(e, b.arena, b.names))
	}
	return out
}

// formatStartupError formats an error in a startup file, naming the file.
func formatStartupError(e TypeError, arena *TypeArena, names *NameTable) string {
	where := ""
	if e.Pos.TokenFile != nil {
		where = "in " + e.Pos.TokenFile.Path + ": "
	}
	return where + e.Format(arena, names)
}

func (b *CoreBase) formatCheck(diags []TypeError, arena *TypeArena, names *NameTable) (out []string, ok bool) {
	out = make([]string, 0, len(b.declErrs)+len(diags))
	out = append(out, b.DeclarationErrors()...)
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

// diagnose is Diagnostics with a checker that a finished check left in the
// pool, reset: the overlays and buffers keep their storage, so a check
// repeated on every edit allocates little. The errors, arena and names
// are valid only inside fn; the checker goes back to the pool after it.
// Checks may run at the same time; each takes its own checker.
func (b *CoreBase) diagnose(file *MShellFile, fn func([]TypeError, *TypeArena, *NameTable)) {
	c, _ := b.pool.Get().(*coreChecker)
	if c == nil {
		c = b.newChecker()
	} else {
		c.reset(b)
	}
	c.checkFile(file)
	fn(c.errs, c.arena, c.names)
	b.pool.Put(c)
}

// reset makes a checker that finished a check on b ready for another:
// the overlays go back to the base, the tables copied from the base are
// copied again, and every buffer is emptied but keeps its storage.
// vars is kept with varGen counting on, so no old entry is current.
func (c *coreChecker) reset(b *CoreBase) {
	c.arena.resetOverlay()
	c.names.resetOverlay()
	c.rel.reset()
	c.subst.Reset()
	c.uni.pairs = c.uni.pairs[:0]
	clear(c.defs)
	refill(c.ctors, b.ctors)
	refill(c.declared, b.declared)
	r := c.res
	refill(r.aliases, b.aliases)
	refill(r.enums, b.enums)
	if r.ids != nil {
		// The signatures carved from it were the finished check's.
		r.ids.chunk = r.ids.chunk[:0]
	}
	c.res = coreResolver{arena: r.arena, names: r.names, rel: r.rel, aliases: r.aliases, enums: r.enums,
		self: -1, errs: r.errs[:0], unions: r.unions[:0], ids: r.ids}
	*c = coreChecker{
		arena: c.arena, names: c.names, rel: c.rel, subst: c.subst, uni: c.uni, res: c.res, table: b.table,
		defs: c.defs, ctors: c.ctors, declared: c.declared,
		vars: c.vars, varGen: c.varGen,
		errs: c.errs[:0], origins: c.origins[:0], stack: c.stack[:0], retOuts: c.retOuts[:0],
		unitVars: c.unitVars[:0], firstLoads: c.firstLoads[:0], unwraps: c.unwraps[:0], dbgs: c.dbgs[:0],
		stores: c.stores[:0], saved: c.saved[:0], pending: c.pending[:0], deferred: c.deferred[:0],
		choices: c.choices[:0], escapes: c.escapes[:0], genBuf: c.genBuf[:0], parts: c.parts[:0],
		litLists: c.litLists[:0], calls: c.calls[:0], exitShared: c.exitShared[:0],
		setLog: c.setLog[:0], daLoops: c.daLoops[:0], unsetReads: c.unsetReads[:0],
		runBuf: c.runBuf[:0], armBuf: c.armBuf[:0], daNames: c.daNames[:0], daSpans: c.daSpans[:0],
		liveBuf: c.liveBuf[:0],
	}
}

// refill makes dst a copy of src, keeping dst's storage.
func refill[K comparable, V any](dst, src map[K]V) {
	clear(dst)
	maps.Copy(dst, src)
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
	retAny                      // top-level code: return ends the script (or the REPL session)
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

// coreVarUndo is a variable's entry as it was at a session mark.
type coreVarUndo struct {
	name NameId
	v    coreVar
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
	// origins are the joins that made union types, and the reads that
	// made unknowns (coreSlot.origin).
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
	// session is set for a REPL session (TypeCoreSession.go). A session
	// saves a variable's entry in varUndo before its first change after
	// each mark: varEpochs[name] is the mark, numbered varEpoch, it was
	// last saved at. Kept apart from vars, which every check walks.
	session   bool
	varEpoch  uint32
	varEpochs []uint32
	varUndo   []coreVarUndo

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
	// keptChoices is how many of choices earlier REPL lines left open
	// (TypeCoreSession.go); 0 outside a session.
	keptChoices int
	// at is the word being checked, where a deferred check reports.
	at Token
	// assertive is set while the patterns of a `=>` are read.
	assertive bool
	escapes       []coreEscape
	genBuf      []TypeId // the generics stack (instantiate)
	// parts holds the unit's partly new marks (TypeCorePartial.go).
	parts []corePart
	// litLists holds the unit's list literals of string literals.
	litLists []litList

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

	// Scratch stacks for branches (armBegin): the saved runs of the arms,
	// the arms of a match, the variables each arm set, and joinArms's
	// arms that go on. A branch pushes above its mark and truncates back
	// to it when done, so nested branches share them.
	runBuf  []savedRun
	armBuf  []coreArm
	daNames []NameId
	daSpans []daSpan
	liveBuf []savedRun
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
		nerr := len(c.res.errs)
		parts := c.res.resolveSig(def.Inputs, def.Outputs)
		broken := len(c.res.errs) > nerr
		c.takeResolveErrors()
		sig := newCoreSig(c.arena, parts)
		sig.broken = broken
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
	c.choices, c.choiceVersion, c.keptChoices = c.choices[:0], 0, 0
	c.escapes = c.escapes[:0]
	c.brk, c.cont, c.brkSeen, c.infer = coreLoopCtx{}, coreLoopCtx{}, false, nil
	c.subst.Reset()
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
			c.deferredError(&d, t, want)
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
		if sig.genIn&genBit(i) != 0 {
			t = c.rel.SubstParams(t, rigid)
		}
		c.stack = append(c.stack, coreSlot{t: t})
	}
	outs := make([]TypeId, len(sig.outs))
	for i, t := range sig.outs {
		if sig.genOut&genBit(i) != 0 {
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
			hint := "output " + strconv.Itoa(i) + " is declared " + c.format(want) + ", body produced " + c.format(got)
			if h := c.originHint(c.stack[i]); h != "" {
				hint += "; " + h
			}
			c.errs = append(c.errs, TypeError{Kind: TErrDefBodyMismatch, Pos: def.NameToken, Name: def.Name, Hint: hint})
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
			c.groupBySpecs(l, *items[i+1].(*Token)) {
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
	tok, ok := item.(*Token)
	return ok && tok.Type == LITERAL && tok.Lexeme == name
}

func (c *coreChecker) step(item MShellParseItem) {
	switch it := item.(type) {
	case *Token:
		c.token(*it)
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
		// `each. ... end` is `(...) each`. The name is the lexeme without
		// its one trailing dot, as the runtime reads it.
		c.pushQuote(it.Items, it.StartToken)
		call := it.StartToken
		call.Type, call.Lexeme = LITERAL, strings.TrimSuffix(call.Lexeme, ".")
		if call.Lexeme == "return" {
			// The runtime calls a prefix quote's word as a def, a
			// constructor or a builtin, and return is none of them.
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: it.StartToken,
				Hint: "`return.` is not a word: return takes no quote; write `return` on its own"})
			c.abandoned = true
			return
		}
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
				if ix.(*Token).Type != INDEXER {
					sigs = c.table.multi
				}
			}
		} else if it.Indexers[0].(*Token).Type == INDEXER {
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
		if isCommandToken(tok.Type) {
			// A command word given something that is not a command.
			if c.need(1, tok) {
				c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok,
					Hint: "'" + tok.Lexeme + "' takes a command or a pipe, and the top of the stack is " +
						c.format(c.subst.Apply(c.arena, c.stack[len(c.stack)-1].t))})
				c.abandoned = true
			}
			return
		}
		switch tok.Type {
		case TYPEINT, TYPEFLOAT, TYPEBOOL, STR, DOUBLEDASH:
			// Type syntax where a word should be, as in a quote written
			// like its type: `(int -- int)`. The runtime refuses it too.
			hint := "'" + tok.Lexeme + "' is a type, not a word: types are written in a def signature, after `as` or `tryAs`, or as a match pattern"
			if tok.Type == DOUBLEDASH {
				hint = "'--' separates the inputs and outputs of a type, not words: to give a quote a type, write `as (int -- int)` after it"
			}
			c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: hint})
			c.abandoned = true
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
	id, named := c.names.Lookup(tok.Lexeme)
	if named {
		if sig := c.defs[id]; sig != nil {
			if sig.broken {
				// A def of this file whose signature has an error is reported
				// at the def. A startup def found broken while the startup
				// files' bodies are checked (checkStartupBodies) is not, and
				// a def that calls it is broken too.
				if ts := c.table.name(id); ts != nil && &ts[0] == sig {
					c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: c.table.brokenWhy[id]})
				}
				c.abandoned = true
				return
			}
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
	if named {
		if sigs := c.table.name(id); sigs != nil {
			if sigs[0].broken {
				// A startup file's def whose signature or body has an error:
				// a call to it is refused, with the reason.
				c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: c.table.brokenWhy[id]})
				c.abandoned = true
				return
			}
			c.call(sigs, tok)
			return
		}
	}
	if forms, ok := coreWalkerSigs[tok.Lexeme]; ok {
		// A word the walker types itself, given a stack none of its forms
		// fits: a mistake in the program, not a gap in the checker.
		top := c.stack[c.floor:]
		if len(top) > 4 {
			top = top[len(top)-4:]
		}
		c.errs = append(c.errs, TypeError{Kind: TErrNoMatchingOverload, Pos: tok,
			Hint: "the top of the stack is " + c.formatSlots(top) + "; it takes " + strings.Join(forms, " or ")})
		c.abandoned = true
		return
	}
	if _, ok := BuiltInList[tok.Lexeme]; ok {
		c.unsupported(tok, "the builtin '"+tok.Lexeme+"'")
		return
	}
	if strings.HasPrefix(tok.Lexeme, "~/") {
		// A path under the home directory, as a string.
		c.push(TidStr, true)
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
		// The list is the one below when that is a list, through an alias
		// too (`[[1]] as LL [5] append`).
		if b := c.unfold(c.subst.Apply(c.arena, c.stack[n-2].t)); c.arena.nodes[b].Kind != TKList {
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
		if c.session {
			c.saveVar(name)
		}
	} else if c.session {
		if c.saveVar(name) {
			c.varUndo = append(c.varUndo, coreVarUndo{name, *v})
		}
	}
	return v
}

// saveVar marks name's entry saved at the current session mark, and reports
// whether it was not already.
func (c *coreChecker) saveVar(name NameId) bool {
	if int(name) >= len(c.varEpochs) {
		c.varEpochs = slices.Grow(c.varEpochs, len(c.vars)-len(c.varEpochs))[:len(c.vars)]
	}
	if c.varEpochs[name] == c.varEpoch {
		return false
	}
	c.varEpochs[name] = c.varEpoch
	return true
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
// solved or not (a flag set when the type was made).
func (c *coreChecker) mentionsVars(t TypeId) bool {
	return c.arena.HasVarNode(t)
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
	// An earlier label may have solved a variable this one mentions.
	got, want = c.subst.Apply(c.arena, got), c.subst.Apply(c.arena, want)
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
	// A quote literal is typed first: the join would otherwise fix its
	// type to the other side's without checking its body.
	c.forceTop(2)
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
	var u TypeId
	for i, s := range top {
		if c.waiting(s) != nil {
			continue
		}
		// A recursive alias, such as Json, is not split.
		t := c.subst.Apply(c.arena, s.t)
		if c.arena.nodes[t].Kind == TKUnion {
			idx, u = len(c.stack)-len(top)+i, t
			break
		}
	}
	if idx < 0 {
		return false
	}
	members := c.arena.unionMembers[c.arena.nodes[u].Extra]
	// Inputs a quote typed on its own gains in one arm go below, so the
	// union's slot is found from the top.
	fromTop := len(c.stack) - idx
	// A quote literal among the arguments is checked against each member's
	// candidate in turn, with that candidate's break context.
	var waiting []uint32
	for _, s := range top {
		if c.waiting(s) != nil {
			waiting = append(waiting, s.pq)
		}
	}
	mark := len(c.saved)
	entry := c.saveStack()
	var runs []savedRun
	for _, m := range members {
		c.restoreStack(entry)
		for _, pq := range waiting {
			c.pending[pq-1].done = false
		}
		c.stack[len(c.stack)-fromTop].t = m
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
			// A quote literal fits a quote parameter, a union with one
			// quote member, or a bare generic; its body is checked once a
			// candidate is chosen. A parameter that only mentions a
			// generic ([t], Maybe[a]) is not a quote.
			w := c.subst.Apply(c.arena, want)
			ok = c.arena.nodes[w].Kind == TKVar || c.literalQuote(w) != TidNothing
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
			if sig.genIn&genBit(i) != 0 {
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
			c.checkPending(s.pq, want, sig.child, sig.current, base, tok)
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
	var from uint32
	for _, s := range c.stack[base:] {
		if from = c.unknownOrigin(s); from != 0 {
			break
		}
	}
	c.stack = c.stack[:base]
	if sig.diverges {
		c.diverged = true
		return
	}
	for j, t := range sig.outs {
		if sig.genOut&genBit(j) != 0 {
			t = c.rel.SubstParams(t, gens)
		}
		fresh := sig.newOut&(1<<j) != 0 || (sig.keepOut&(1<<j) != 0 && inputsFresh)
		if !fresh && sig.newListOut&(1<<j) != 0 {
			fresh = c.newOverImmutable(t)
		}
		c.push(t, fresh)
		c.carryUnknown(len(c.stack)-1, from)
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
	} else if got == TidBytes && (tok.Lexeme == "wl" || tok.Lexeme == "wle") {
		e.Hint = "binary is not text with lines: write it with `w` or `we`, which add no newline"
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
	// A quote literal given a quote type, or a union with one quote member,
	// is checked against it, as a word that takes a quote checks its
	// argument: its inputs are known.
	if p := c.waiting(c.stack[len(c.stack)-1]); p != nil {
		if q := c.literalQuote(target); q != TidNothing {
			top := len(c.stack) - 1
			c.checkPending(c.stack[top].pq, q, false, false, top, a.AsToken)
			c.stack[top].pq = 0
			c.stack[top].t = target
			return
		}
	}
	c.forceTop(1)
	s := &c.stack[len(c.stack)-1]
	var blockers []storedBlocker
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
		blockers = c.storedBlockers(slotMark(*s), c.subst.Apply(c.arena, s.t), target, "")
		s.share()
	}
	if !c.check(*s, target) {
		got := c.subst.Apply(c.arena, s.t)
		hint := "'as' needs evidence: " + c.format(got) + " is not below " + c.format(target)
		if len(blockers) > 0 {
			// A new literal around stored values: say which stored value
			// is in the way.
			hint += "; " + c.blockersHint(blockers)
		} else if !s.fresh && !c.rel.Immutable(got) && c.rel.Retype(got, target) {
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
// A bare word is a string only directly in a list literal (inList), as
// the runtime reads it only in a list's own frame.
func (c *coreChecker) child(items []MShellParseItem) (start int, outerFloor int) {
	return c.childIn(items, false)
}

func (c *coreChecker) childIn(items []MShellParseItem, inList bool) (start int, outerFloor int) {
	outerFloor = c.floor
	c.floor = len(c.stack)
	depth := c.listDepth
	c.listDepth = 0
	if inList {
		c.listDepth = 1
	}
	// A break or continue throws the literal's own stack away, as a
	// child-stack word's: the loop is left with the stack under it.
	brk, cont := c.brk, c.cont
	c.brk, c.cont = c.bodyLoopCtx(brk, c.floor, true), c.bodyLoopCtx(cont, c.floor, true)
	c.walk(items)
	c.brk, c.cont = brk, cont
	c.listDepth = depth
	return c.floor, outerFloor
}

// listLiteral types `[...]`: a list of the join of its elements, fresh
// when every element is fresh or immutable (ShapeLit), and otherwise a new
// list of stored values (TypeCorePartial.go).
func (c *coreChecker) listLiteral(l *MShellParseList) {
	if c.stringListLiteral(l) {
		return
	}
	start, outerFloor := c.childIn(l.Items, true)
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
	var from uint32
	for _, e := range elems {
		if from = c.unknownOrigin(e); from != 0 {
			break
		}
	}
	c.stack = append(c.stack[:start], out)
	c.carryUnknown(start, from)
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
	c.litLists = append(c.litLists, litList{names: names})
	return litListTag | NameId(len(c.litLists)), true
}

// stringListLiteral types a list literal whose elements are all string
// literals without walking them, and says whether it did: walking them
// pushes a fresh str for each, which join to str, and records their names.
// Here the names are interned only when a grid word reads them (litNames);
// a long list of command options is never read that way.
func (c *coreChecker) stringListLiteral(l *MShellParseList) bool {
	if len(l.Items) == 0 || len(c.litLists) >= int(litListTag-1) {
		return false
	}
	for _, item := range l.Items {
		tok, ok := item.(*Token)
		if !ok || (tok.Type != STRING && tok.Type != SINGLEQUOTESTRING) {
			return false
		}
		if _, ok := tok.Value.(MShellString); !ok {
			return false
		}
	}
	c.litLists = append(c.litLists, litList{items: l.Items})
	c.stack = append(c.stack, coreSlot{t: c.arena.MakeList(TidStr), fresh: true, lit: litListTag | NameId(len(c.litLists))})
	return true
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
	c.forceWaiting()
	// The arms run in their own runtime frames, not a list literal's.
	depth := c.listDepth
	c.listDepth = 0
	defer func() { c.listDepth = depth }()
	mark := len(c.saved)
	entry := c.saveStack()
	am := c.armBegin()
	defer c.armEnd(am)
	// Definite assignment: an arm starts with what the conditions before it
	// set, and the if keeps what every arm that goes on set.
	daMark := len(c.setLog)
	var condSets [][]NameId
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
			run := c.saveStack()
			run.diverged, run.label, run.line = c.diverged, label, line
			c.keepRun(run)
			if !c.diverged {
				c.keepSet(daMark)
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
		c.joinArms(c.runsSince(am), tok)
		c.daJoinSince(daMark, am)
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
	// made: "the `else` branch", and the line where it starts. A match
	// arm is named by its pattern, pat, made into text only for a message.
	label string
	pat   []MShellParseItem
	line  int
	// infer and ins are the quote being typed on its own when the stack
	// was saved, and how many inputs it had then (see padRun).
	infer *coreInfer
	ins   int
}

func (c *coreChecker) saveStack() savedRun {
	r := savedRun{start: len(c.saved)}
	c.saved = append(c.saved, c.stack...)
	r.end = len(c.saved)
	if c.infer != nil {
		r.infer, r.ins = c.infer, len(c.infer.ins)
	}
	return r
}

// padRun gives a saved stack the inputs its quote gained since it was
// saved. A quote typed on its own reads inputs below its own pushes as it
// meets them (need), so an arm or loop body that reads below the entry
// stack adds slots to the bottom of the current stack only; every stack
// saved before must get the same slots, or another arm, the join or the
// loop's back edge would not see them. It appends the padded copy to
// c.saved, so it is valid as long as r is.
func (c *coreChecker) padRun(r savedRun) savedRun {
	if r.infer == nil || r.infer != c.infer || len(c.infer.ins) == r.ins {
		return r
	}
	k := len(c.infer.ins) - r.ins
	fl := c.infer.floor
	p := r
	p.start = len(c.saved)
	c.saved = append(c.saved, c.saved[r.start:r.start+fl]...)
	for _, t := range c.infer.ins[:k] {
		c.saved = append(c.saved, coreSlot{t: t})
	}
	c.saved = append(c.saved, c.saved[r.start+fl:r.end]...)
	p.end = len(c.saved)
	p.ins = len(c.infer.ins)
	return p
}

// restoreStack sets the stack to a saved one, padded as padRun says.
func (c *coreChecker) restoreStack(r savedRun) {
	r = c.padRun(r)
	c.stack = append(c.stack[:0], c.saved[r.start:r.end]...)
}

// joinArms sets the stack to the join of the arms that did not diverge.
func (c *coreChecker) joinArms(arms []savedRun, tok Token) {
	liveMark := len(c.liveBuf)
	defer func() { c.liveBuf = c.liveBuf[:liveMark] }()
	for _, a := range arms {
		if !a.diverged {
			c.liveBuf = append(c.liveBuf, c.padRun(a))
		}
	}
	// Typing a waiting literal below may check another branch, which
	// uses liveBuf above this one's.
	live := c.liveBuf[liveMark:len(c.liveBuf):len(c.liveBuf)]
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
// the slot, so an error about the value can point back at the arms. Or,
// when unk is set, the read at tok that made an unknown: unk says what
// kind of read, u is the dict's record or the grid's schema, and key the
// literal key, if any. The message is made only for an error.
type coreOrigin struct {
	tok  Token
	u    TypeId
	arms []coreOriginArm
	unk  unknownRead
	key  NameId
}

// unknownRead is the kind of read that gave a value of unknown type.
type unknownRead uint8

const (
	unkNone unknownRead = iota
	// unkKey reads a field with a key known only at run time.
	unkKey
	// unkLabel reads a literal key the shape does not declare.
	unkLabel
	// unkAll reads every field (values, keyValues).
	unkAll
	// unkGridKey and unkGridLabel are unkKey and unkLabel on a grid's
	// columns.
	unkGridKey
	unkGridLabel
)

type coreOriginArm struct {
	label string
	pat   []MShellParseItem
	line  int
	t     TypeId
}

// armName is the name of an arm in a message: label, or the match arm
// whose pattern is pat.
func armName(label string, pat []MShellParseItem) string {
	if pat != nil {
		return "the arm `" + formatPatternSnippet(pat) + "`"
	}
	return label
}

// noteOrigins records, for each slot where the join of the arms is a
// union that no arm left alone, which arm left which type.
func (c *coreChecker) noteOrigins(live []savedRun, tok Token) {
	if len(live) < 2 || (live[0].label == "" && live[0].pat == nil) {
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
			o.arms = append(o.arms, coreOriginArm{label: a.label, pat: a.pat, line: a.line, t: c.saved[a.start+i].t})
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
	if o.unk != unkNone {
		return c.unknownHint(o)
	}
	var b strings.Builder
	b.WriteString(c.format(o.u) + " comes from the `" + o.tok.Lexeme + "` at line " + strconv.Itoa(o.tok.Line) + ":")
	for i, a := range o.arms {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(" " + armName(a.label, a.pat) + " (line " + strconv.Itoa(a.line) + ") leaves " + c.format(a.t))
	}
	return b.String()
}

// unknownHint says which read made an unknown, why, and how to say what
// it holds: "unknown comes from the `get` at line 12, column 9: its key
// is known only at run time, so ...".
func (c *coreChecker) unknownHint(o coreOrigin) string {
	at := "unknown comes from the `" + o.tok.Lexeme + "` at line " + strconv.Itoa(o.tok.Line) +
		", column " + strconv.Itoa(o.tok.Column) + ": "
	switch o.unk {
	case unkKey:
		return at + "its key is known only at run time, so it may read a field that " + c.format(o.u) +
			" does not declare, which may hold anything; " + c.restFix(o.u)
	case unkLabel:
		k := c.names.Name(o.key)
		return at + c.format(o.u) + " does not declare '" + k + "', so it may hold anything; " +
			"check the key's spelling, or add '" + k + "' to the shape"
	case unkAll:
		return at + "it reads every field, and the fields " + c.format(o.u) +
			" does not declare may hold anything; " + c.restFix(o.u)
	case unkGridKey, unkGridLabel:
		const narrow = "narrow the cells with `match` or `tryAs`"
		cols := formatSchema(c.arena, c.names, c.schemaOf(c.subst.Apply(c.arena, o.u)))
		if cols == "" {
			return at + "the grid's columns are not known (as for `Grid` or `toGrid`), so its cells may hold anything; " + narrow
		}
		if o.unk == unkGridKey {
			return at + "its column name is known only at run time, so it may name a column other than " + cols +
				", which may hold anything; " + narrow
		}
		return at + "the grid's known columns, " + cols + ", do not include '" + c.names.Name(o.key) +
			"', so it may hold anything; check the column's spelling, or " + narrow
	}
	return ""
}

// restFix says how to declare what a shape's undeclared fields hold: with
// `*: T`, T the join of the fields it declares, when they have one.
func (c *coreChecker) restFix(rec TypeId) string {
	const fix = "say what undeclared fields hold with `*: T` in the shape"
	r := c.arena.records[c.arena.nodes[rec].Extra]
	acc := Slot{Type: TidBottom}
	for _, f := range r.Fields {
		if f.Status == FieldAbsent || f.Status == FieldOpen {
			continue
		}
		t := c.subst.Apply(c.arena, f.Type)
		if c.hasVars(t) {
			return fix
		}
		j, ok := c.rel.JoinSlot(acc, Slot{Type: t})
		if !ok {
			return fix
		}
		acc = j
	}
	if acc.Type == TidBottom || acc.Type == TidUnknown {
		return fix
	}
	return fix + ": " + c.format(c.arena.MakeRecord(r.Fields, RecordField{Status: FieldOptional, Type: acc.Type}))
}

// noteUnknown records that the read at tok made the unknown in the top
// slot (coreSlot.origin): an error about the value then names the read.
// rec is the dict's record or the grid's schema.
func (c *coreChecker) noteUnknown(tok Token, kind unknownRead, rec TypeId, key NameId) {
	s := &c.stack[len(c.stack)-1]
	if !c.mentionsType(c.subst.Apply(c.arena, s.t), TidUnknown) {
		return
	}
	c.origins = append(c.origins, coreOrigin{tok: tok, u: rec, unk: kind, key: key})
	s.origin = uint32(len(c.origins))
}

// unknownOrigin is the slot's origin when it is a read that made an
// unknown, or 0.
func (c *coreChecker) unknownOrigin(s coreSlot) uint32 {
	if s.origin == 0 || int(s.origin) > len(c.origins) || c.origins[s.origin-1].unk == unkNone {
		return 0
	}
	return s.origin
}

// carryUnknown gives slot i the read that made an unknown in a value it
// was made from (unknownOrigin), when it has an unknown and no origin of
// its own.
func (c *coreChecker) carryUnknown(i int, o uint32) {
	if o == 0 || c.stack[i].origin != 0 || !c.mentionsType(c.subst.Apply(c.arena, c.stack[i].t), TidUnknown) {
		return
	}
	c.stack[i].origin = o
}

// joinSlot joins two slots. A slot with an unsolved variable is unified,
// never joined, so the answer does not depend on checking order. An
// unknown in the join keeps the read that made it (coreSlot.origin).
func (c *coreChecker) joinSlot(a, b coreSlot) (coreSlot, bool) {
	// The same slot in both arms is the value from before the branch, which
	// neither arm touched: anything that copies, stores or rewrites it
	// changes the slot. So it keeps its literal text or names, partly new
	// mark and origin.
	if a == b {
		return a, true
	}
	j, ok := c.joinTypes(a, b)
	if ok && j.origin == 0 {
		o := c.unknownOrigin(a)
		if o == 0 {
			o = c.unknownOrigin(b)
		}
		if o != 0 && c.mentionsType(c.subst.Apply(c.arena, j.t), TidUnknown) {
			j.origin = o
		}
	}
	return j, ok
}

// joinTypes is joinSlot for two different slots.
func (c *coreChecker) joinTypes(a, b coreSlot) (coreSlot, bool) {
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
