package main

// Type representation for the static type checker.
//
// Types are uint32 indices (TypeId) into a hashconsed arena. Identical
// structural types share an id, so type equality is integer equality.
//
// The design of the checker built on it is ai/type-core-calculus.typ.

import (
	"encoding/binary"
	"maps"
	"slices"
)

// TypeId is an opaque handle into TypeArena. Comparing TypeIds for equality
// is equivalent to comparing the underlying types for structural equality
// (a consequence of hashconsing).
type TypeId uint32

// Reserved primitive ids. Must be assigned in this exact order during arena
// construction so they line up with these constants.
const (
	TidNothing TypeId = iota // sentinel "no type"; used in forward-compat slots
	TidBool
	TidInt
	TidFloat
	TidStr
	TidBytes
	TidNone     // reserved slot; `none` is a Maybe constructor, NOT a nameable type (use Maybe[T] or null). Kept to preserve primitive ids.
	TidPath     // path literal (`...`) and the path runtime type
	TidDateTime // date/time literal (YYYY-MM-DD[THH:MM[:SS]]) and now/date ops
	TidBottom   // divergent: exit, infinite loop, (Phase 2) propagated fail
	TidNull     // JSON `null`; distinct from `none` (the empty Maybe case)
	TidUnknown  // unknown contents: every value has this type, and nothing but a kind pattern accepts it
)

// TypeKind categorizes a TypeNode. The interpretation of TypeNode.A, B, and
// Extra depends on the kind.
type TypeKind uint8

const (
	TKPrim    TypeKind = iota // primitive; A unused
	TKList                    // A = element T
	TKQuote                   // Extra = index into quoteSigs
	TKUnion                   // Extra = index into unionMembers
	TKCommand                 // A = argv list TypeId; B = stdout capture; Extra = stderr capture
	TKVar                     // A = TypeVarId
	TKRigid                   // A = NameId of the declared generic; see MakeRigid

	// Grid family. A is the schema: a record type with one label per
	// column (MakeGridOf).
	TKGrid
	TKGridView
	TKGridRow

	// TKRecord is every dict-kinded type: shapes and `{str: T}`.
	// Extra = index into records.
	TKRecord
	// TKEnum is an enum instance. A = index into enumDecls; Extra = index
	// into enumArgs (one argument per parameter).
	TKEnum
	// TKAlias is a reference to a `type` alias. A = index into aliases. The
	// alias is unfolded only when a relation needs to look inside it, so a
	// recursive alias stays a finite type.
	TKAlias
	// TKAbstract is an abstract type: the unknown contents a kind pattern
	// finds. A = a number unique to it; it equals only itself.
	TKAbstract
	// TKParam is the A'th parameter of an enum declaration. It appears only
	// in constructor payload types.
	TKParam
)

// String returns a debug name for a TypeKind.
func (k TypeKind) String() string {
	switch k {
	case TKPrim:
		return "Prim"
	case TKList:
		return "List"
	case TKQuote:
		return "Quote"
	case TKUnion:
		return "Union"
	case TKCommand:
		return "Command"
	case TKVar:
		return "Var"
	case TKRigid:
		return "Rigid"
	case TKGrid:
		return "Grid"
	case TKGridView:
		return "GridView"
	case TKGridRow:
		return "GridRow"
	case TKRecord:
		return "Record"
	case TKEnum:
		return "Enum"
	case TKAlias:
		return "Alias"
	case TKAbstract:
		return "Abstract"
	case TKParam:
		return "Param"
	}
	return "Unknown"
}

// TypeNode is the in-arena representation of a type. The interpretation of
// A, B, and Extra is dictated by Kind. Layout is fixed so the arena slice
// stays cache-friendly.
type TypeNode struct {
	Kind TypeKind
	// Flags are facts about the whole type, set when the node is made
	// from its children's (TypeArena.append); NodeHasVar is the only one.
	// They fill padding, so a node stays 16 bytes.
	Flags uint8
	A     uint32
	B     uint32
	Extra uint32
}

// NodeHasVar is set on a type that mentions a unification variable
// (TKVar), solved or not. A type without it is the same under every
// substitution, so Apply and the occurs check skip it at once.
const NodeHasVar uint8 = 1

// HasVarNode reports whether t mentions a unification variable at all.
func (a *TypeArena) HasVarNode(t TypeId) bool {
	return a.nodes[t].Flags&NodeHasVar != 0
}

// CommandCaptureMode is really a per-stream *destination state*: unset,
// captured to the stack (str/bytes/lines), redirected to a file, used for an
// in-place edit, or merged into the other stream. The checker uses it to
// enforce that each stream has exactly one destination, mirroring the
// runtime's conflict errors. Only the capture states produce stack outputs
// at `;` / `!` / `?`.
type CommandCaptureMode uint32

const (
	CommandCaptureNone CommandCaptureMode = iota
	CommandCaptureStr
	CommandCaptureBytes
	CommandCaptureLines
	CommandDestFile    // stream redirected to a file (>, >>, 2>, 2>>, &>, &>>)
	CommandDestInPlace // stdout claimed by an in-place redirect (<>)
	CommandDestMerged  // stream merged into the other stream (2>&1 / 1>&2)
	// CommandDestVaried: the join of commands whose destinations for this
	// stream differ (the core checker's list literals). Above every other
	// state; such a command can be piped, not run or redirected on its own.
	CommandDestVaried
)

// CommandPipe is set in a TKCommand's stdout state (with the state itself)
// when the value is a pipe rather than a list: a pipe and a list are
// different runtime objects.
const CommandPipe CommandCaptureMode = 1 << 16

// TypeVarId identifies a generic type variable. Fresh ids are issued at
// generic-instantiation sites (each call to a polymorphic function yields
// fresh variables).
type TypeVarId uint32

// FieldStatus is what a dict-kinded type says about one label
// (ai/type-core-calculus.typ, "The per-label reading").
type FieldStatus uint8

const (
	// FieldAbsent: never present. Every undeclared label of an exact shape
	// (a shape literal's type).
	FieldAbsent FieldStatus = iota
	// FieldRequired: present, and writable at its type.
	FieldRequired
	// FieldOptional: maybe present, writable at its type, not deletable. A
	// declared `name?: T`, or every undeclared label under a `*: T` remainder.
	FieldOptional
	// FieldDeletable: like FieldOptional, and it may also be deleted. Every
	// label of `{str: T}`.
	FieldDeletable
	// FieldOpen: maybe present, at an unknown type, read-only. Every
	// undeclared label of a written shape type.
	FieldOpen
)

// RecordField is one declared label of a TKRecord. Type is TidNothing for
// FieldAbsent and FieldOpen, which carry no type.
type RecordField struct {
	Name   NameId
	Status FieldStatus
	Type   TypeId
}

// RecordType is a TKRecord's content: its declared labels, sorted by Name,
// and Rest, the status of every other label. Rest's Name is unused.
//
//	shape literal {a: 1}       {a: int | Rest absent}
//	written shape {a: int}     {a: int | Rest open}
//	{a: int, *: str}           {a: int | Rest optional str}
//	{str: int}                 {        | Rest deletable int}
type RecordType struct {
	Fields []RecordField
	Rest   RecordField
}

// Variance is how an enum parameter's argument may change under subtyping.
type Variance uint8

const (
	VarCo Variance = iota
	VarContra
	VarInv
)

// EnumParam is one parameter of an enum declaration. Fresh says every
// occurrence is in a data position, so a fresh value may retype its
// argument covariantly (ai/type-core-calculus.typ, "Fresh-covariance").
type EnumParam struct {
	Name     NameId
	Variance Variance
	Fresh    bool
}

// EnumCtor is one constructor of an enum declaration. Its payload types
// may mention the enum's parameters as TKParam types.
type EnumCtor struct {
	Name    NameId
	Payload []TypeId
}

// EnumDecl is an enum declaration. Immutable says a value holds no list or
// dict when its arguments cannot; Checkable says no payload type mentions a
// quote. Both are computed from the constructors when the enum is declared.
type EnumDecl struct {
	Name      NameId
	Params    []EnumParam
	Ctors     []EnumCtor
	Immutable bool
	Checkable bool
}

// AliasDecl is a `type` alias. Body is TidNothing until the alias is
// resolved, which may be after references to it are made.
type AliasDecl struct {
	Name NameId
	Body TypeId
}

// QuoteSig is a function or quote signature. Inputs are listed bottom-to-top
// (so the last element is the top of the consumed stack). Outputs are also
// listed bottom-to-top. A quote that never returns has Diverges set and no
// outputs: its output side is `never`.
type QuoteSig struct {
	Inputs   []TypeId
	Outputs  []TypeId
	Diverges bool
}

// TypeArena is the storage for all types in a checking session.
//
// nodes is the primary store; a TypeId is an index into it.
// cons maps a structural fingerprint to the TypeId that owns it; new
// constructions look up here first to deduplicate (hashconsing).
//
// quoteSigs, unionMembers, records, enumArgs and aliases are side tables
// for variable-length data referenced from a TypeNode.
type TypeArena struct {
	// parent is the frozen arena this one is an overlay of (see Overlay),
	// or nil. Its types keep their ids here, and hash-consing looks in it
	// after this arena's own tables.
	parent *TypeArena

	nodes []TypeNode
	cons  map[string]TypeId
	// atomCons hashconses the kinds whose data fits entirely in TypeNode
	// (no side-table content). Keying on the struct itself avoids the
	// per-construction string key allocation on the checking hot path.
	atomCons map[TypeNode]TypeId

	quoteSigs     []QuoteSig
	unionMembers  [][]TypeId // each slice is sorted, deduped
	records       []RecordType
	enumDecls     []EnumDecl
	enumArgs      [][]TypeId
	aliases       []AliasDecl
	abstractCount uint32
	// keyBuf is scratch space for building composite cons keys; unionBuf
	// and fieldBuf hold a union's members and a record's fields while
	// they are looked up, copied only into a new type.
	keyBuf   []byte
	unionBuf []TypeId
	fieldBuf []RecordField
	// varIds and listOf find the variable and list types made here, by
	// variable id and by element id (MakeVar, MakeList).
	varIds []TypeId
	listOf []TypeId
}

// Overlay returns an arena that starts with every type in a and grows on
// its own, without copying a's tables: a check starts in constant time
// from a base built once. a must not change afterwards. The overlay's
// slices share a's backing arrays with their capacity capped, so the first
// append to each copies it, and its hash-consing maps hold only what the
// overlay adds, looked up before a's.
func (a *TypeArena) Overlay() *TypeArena {
	return &TypeArena{
		parent:        a,
		nodes:         slices.Clip(a.nodes),
		cons:          make(map[string]TypeId),
		atomCons:      make(map[TypeNode]TypeId),
		quoteSigs:     slices.Clip(a.quoteSigs),
		unionMembers:  slices.Clip(a.unionMembers),
		records:       slices.Clip(a.records),
		enumDecls:     slices.Clip(a.enumDecls),
		enumArgs:      slices.Clip(a.enumArgs),
		aliases:       slices.Clip(a.aliases),
		abstractCount: a.abstractCount,
	}
}

// lookupCons finds a composite type by its key, here or in a parent.
func (a *TypeArena) lookupCons(key []byte) (TypeId, bool) {
	for p := a; p != nil; p = p.parent {
		if id, ok := p.cons[string(key)]; ok {
			return id, true
		}
	}
	return TidNothing, false
}

// resetOverlay takes an overlay made by Overlay back to the state Overlay
// gave it, keeping its storage for the next check. Its slices hold the
// parent's entries first, unchanged, since the parent is frozen and the
// overlay writes only entries it added; each is cut back to the parent's
// length.
func (a *TypeArena) resetOverlay() {
	p := a.parent
	a.nodes = a.nodes[:len(p.nodes)]
	clear(a.cons)
	clear(a.atomCons)
	a.quoteSigs = a.quoteSigs[:len(p.quoteSigs)]
	a.unionMembers = a.unionMembers[:len(p.unionMembers)]
	a.records = a.records[:len(p.records)]
	a.enumDecls = a.enumDecls[:len(p.enumDecls)]
	a.enumArgs = a.enumArgs[:len(p.enumArgs)]
	a.aliases = a.aliases[:len(p.aliases)]
	a.abstractCount = p.abstractCount
	clear(a.varIds)
	a.varIds = a.varIds[:0]
	clear(a.listOf)
	a.listOf = a.listOf[:0]
}

// NewTypeArena constructs an arena pre-populated with the primitive ids
// TidBool through TidBottom. After this call, primitive constants resolve
// to live nodes.
func NewTypeArena() *TypeArena {
	a := &TypeArena{
		cons:     make(map[string]TypeId, 64),
		atomCons: make(map[TypeNode]TypeId, 64),
	}
	// Reserve nothing slot at index 0.
	a.nodes = append(a.nodes, TypeNode{Kind: TKPrim})
	// Reserve the canonical primitives in their fixed order.
	primitives := []TypeKind{
		TKPrim, // TidBool
		TKPrim, // TidInt
		TKPrim, // TidFloat
		TKPrim, // TidStr
		TKPrim, // TidBytes
		TKPrim, // TidNone
		TKPrim, // TidPath
		TKPrim, // TidDateTime
		TKPrim, // TidBottom
		TKPrim, // TidNull
		TKPrim, // TidUnknown
	}
	for i := range primitives {
		// Encode the primitive id directly in A so the cons key stays unique.
		a.nodes = append(a.nodes, TypeNode{Kind: TKPrim, A: uint32(i + 1)})
	}
	// Reserve entry 0 of the side tables, so a non-zero Extra is meaningful.
	a.unionMembers = append(a.unionMembers, nil)
	a.quoteSigs = append(a.quoteSigs, QuoteSig{})
	a.records = append(a.records, RecordType{})
	a.enumArgs = append(a.enumArgs, nil)
	// The built-in enum `Maybe[a] = just a | none end`: covariant and
	// fresh-covariant in a, immutable when a is, and checkable.
	a.DeclareEnum(EnumDecl{
		Name:   NameMaybe,
		Params: []EnumParam{{Variance: VarCo, Fresh: true}},
		Ctors: []EnumCtor{
			{Name: NameJust, Payload: []TypeId{a.MakeParam(0)}},
			{Name: NameNoneCtor},
		},
		Immutable: true,
		Checkable: true,
	})
	return a
}

// EnumMaybe is the index of the built-in `Maybe` enum declaration.
const EnumMaybe uint32 = 0

// MakeMaybeEnum returns `Maybe[t]`, an instance of the built-in enum.
func (a *TypeArena) MakeMaybeEnum(t TypeId) TypeId {
	return a.MakeEnum(EnumMaybe, []TypeId{t})
}

// Node returns the in-arena record for id. Out-of-range ids are a programmer
// error and panic immediately rather than returning a sentinel — callers
// should never construct a TypeId from raw input.
func (a *TypeArena) Node(id TypeId) TypeNode {
	if int(id) >= len(a.nodes) {
		panic("TypeArena.Node: id out of range")
	}
	return a.nodes[id]
}

// Kind returns the kind of id.
func (a *TypeArena) Kind(id TypeId) TypeKind {
	return a.Node(id).Kind
}

// MakeList returns the canonical TypeId for [elem].
func (a *TypeArena) MakeList(elem TypeId) TypeId {
	// Lists are found by their element's id, without hashing: in this
	// arena, then in its parent. Only MakeList makes list nodes.
	if int(elem) < len(a.listOf) && a.listOf[elem] != TidNothing {
		return a.listOf[elem]
	}
	if p := a.parent; p != nil && int(elem) < len(p.listOf) && p.listOf[elem] != TidNothing {
		return p.listOf[elem]
	}
	id := a.append(TypeNode{Kind: TKList, A: uint32(elem)})
	a.listOf = growIds(a.listOf, int(elem))
	a.listOf[elem] = id
	return id
}

// growIds makes ids long enough to index i, with TidNothing in new entries.
func growIds(ids []TypeId, i int) []TypeId {
	if i < len(ids) {
		return ids
	}
	if n := len(ids); i < cap(ids) {
		ids = ids[:i+1]
		clear(ids[n:])
		return ids
	}
	return append(ids, make([]TypeId, i+1-len(ids))...)
}

// MakeVar returns the canonical TypeId for the generic type variable v.
// Two calls with the same TypeVarId always return the same TypeId.
func (a *TypeArena) MakeVar(v TypeVarId) TypeId {
	// Variable ids are dense, so a slice finds one without hashing: in
	// this arena, then in its parent. Only MakeVar makes variable nodes.
	if int(v) < len(a.varIds) && a.varIds[v] != TidNothing {
		return a.varIds[v]
	}
	if p := a.parent; p != nil && int(v) < len(p.varIds) && p.varIds[v] != TidNothing {
		return p.varIds[v]
	}
	id := a.append(TypeNode{Kind: TKVar, A: uint32(v)})
	a.varIds = growIds(a.varIds, int(v))
	a.varIds[v] = id
	return id
}

// MakeRigid returns the canonical TypeId for a rigid (skolem) type
// standing in for a def's declared generic during body checking. A
// rigid type unifies only with itself (or a free variable, which it
// binds): the body must prove the declared signature for EVERY
// instantiation of the generic, so overload resolution inside the body
// may not pin it to one concrete type. name is the generic's source
// name (e.g. `a`), used for diagnostics.
func (a *TypeArena) MakeRigid(name NameId) TypeId {
	return a.intern(TKRigid, uint32(name), 0, 0)
}

// MakeCommand returns the canonical TypeId for an executable command value.
// argv is the underlying command-list type. stdout/stderr capture modes
// determine the stack outputs produced by `?`, `;`, and `!`.
func (a *TypeArena) MakeCommand(argv TypeId, stdout, stderr CommandCaptureMode) TypeId {
	return a.intern(TKCommand, uint32(argv), uint32(stdout), uint32(stderr))
}

// MakeUnion returns the canonical TypeId for a union of arms. Arms are
// flattened (nested unions are dissolved), sorted by TypeId, and
// deduplicated. A union with one arm collapses to that arm.
func (a *TypeArena) MakeUnion(arms []TypeId) TypeId {
	flat := a.flattenAndCanonicalizeUnion(arms)
	if len(flat) == 1 {
		return flat[0]
	}
	a.keyBuf = appendUnionKey(a.keyBuf[:0], flat)
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	idx := uint32(len(a.unionMembers))
	a.unionMembers = append(a.unionMembers, slices.Clone(flat))
	id := a.append(TypeNode{Kind: TKUnion, Extra: idx})
	a.cons[string(a.keyBuf)] = id
	return id
}

// MakeQuote returns the canonical TypeId for a quote/function signature.
func (a *TypeArena) MakeQuote(sig QuoteSig) TypeId {
	a.keyBuf = appendQuoteKey(a.keyBuf[:0], sig)
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	idx := uint32(len(a.quoteSigs))
	a.quoteSigs = append(a.quoteSigs, sig)
	id := a.append(TypeNode{Kind: TKQuote, Extra: idx})
	a.cons[string(a.keyBuf)] = id
	return id
}

// MakeGridOf returns the grid, view or row type (kind is
// TKGrid, TKGridView or TKGridRow) whose schema is the record type rec: a
// column is a label, required when it exists, at the type of its cells
// (ai/type-core-calculus.typ, "Grids are shapes of columns"). The unknown
// schema is the read-only `{| open}`.
func (a *TypeArena) MakeGridOf(kind TypeKind, rec TypeId) TypeId {
	return a.intern(kind, uint32(rec), 0, 0)
}

// GridRecord returns the schema record of a grid, view or row type.
func (a *TypeArena) GridRecord(t TypeId) TypeId {
	n := a.Node(t)
	switch n.Kind {
	case TKGrid, TKGridView, TKGridRow:
		return TypeId(n.A)
	}
	return TidNothing
}

// MakeRecord returns the canonical TypeId for a dict-kinded type with the
// given declared labels and remainder. Labels are sorted, the type of an
// absent or open label is dropped, and a declared label that says the same
// as the remainder is dropped, so two records that agree on every label
// share a TypeId. A duplicate label is a programmer error and panics.
func (a *TypeArena) MakeRecord(fields []RecordField, rest RecordField) TypeId {
	rest = normalizeRecordField(rest)
	rest.Name = NameNone
	out := a.fieldBuf[:0]
	defer func() { a.fieldBuf = out[:0] }()
	for _, f := range fields {
		f = normalizeRecordField(f)
		if f.Status == rest.Status && f.Type == rest.Type {
			continue
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(x, y RecordField) int { return int(x.Name) - int(y.Name) })
	for i := 1; i < len(out); i++ {
		if out[i-1].Name == out[i].Name {
			panic("MakeRecord: duplicate label")
		}
	}
	a.keyBuf = appendRecordKey(a.keyBuf[:0], out, rest)
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	idx := uint32(len(a.records))
	a.records = append(a.records, RecordType{Fields: slices.Clone(out), Rest: rest})
	id := a.append(TypeNode{Kind: TKRecord, Extra: idx})
	a.cons[string(a.keyBuf)] = id
	return id
}

func normalizeRecordField(f RecordField) RecordField {
	if f.Status == FieldAbsent || f.Status == FieldOpen {
		f.Type = TidNothing
	}
	return f
}

// MakeStrDict returns `{str: value}`.
func (a *TypeArena) MakeStrDict(value TypeId) TypeId {
	return a.MakeRecord(nil, RecordField{Status: FieldDeletable, Type: value})
}

// Record returns the content of a TKRecord. Caller must not mutate.
func (a *TypeArena) Record(id TypeId) RecordType {
	n := a.Node(id)
	if n.Kind != TKRecord {
		panic("TypeArena.Record: not a record")
	}
	return a.records[n.Extra]
}

// FieldAt returns the status and type a record gives the label name:
// the declared label, or else the remainder.
func (r RecordType) FieldAt(name NameId) RecordField {
	i, found := slices.BinarySearchFunc(r.Fields, name, func(f RecordField, n NameId) int { return int(f.Name) - int(n) })
	if found {
		return r.Fields[i]
	}
	return r.Rest
}

// DeclareEnum adds an enum declaration and returns its index. Each call
// declares a distinct enum, even with the same name.
func (a *TypeArena) DeclareEnum(decl EnumDecl) uint32 {
	a.enumDecls = append(a.enumDecls, decl)
	return uint32(len(a.enumDecls) - 1)
}

// EnumDecl returns the declaration at index idx. Caller must not mutate.
func (a *TypeArena) EnumDecl(idx uint32) *EnumDecl {
	return &a.enumDecls[idx]
}

// MakeEnum returns the canonical TypeId for the enum declared at idx,
// instantiated at args (one per parameter).
func (a *TypeArena) MakeEnum(idx uint32, args []TypeId) TypeId {
	if len(args) != len(a.enumDecls[idx].Params) {
		panic("MakeEnum: wrong number of arguments")
	}
	a.keyBuf = append(a.keyBuf[:0], 'E')
	a.keyBuf = appendKeyU32(a.keyBuf, idx)
	a.keyBuf = appendKeyU32(a.keyBuf, uint32(len(args)))
	for _, t := range args {
		a.keyBuf = appendKeyU32(a.keyBuf, uint32(t))
	}
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	argIdx := uint32(len(a.enumArgs))
	a.enumArgs = append(a.enumArgs, slices.Clone(args))
	id := a.append(TypeNode{Kind: TKEnum, A: idx, Extra: argIdx})
	a.cons[string(a.keyBuf)] = id
	return id
}

// EnumArgs returns the arguments of a TKEnum. Caller must not mutate.
func (a *TypeArena) EnumArgs(id TypeId) []TypeId {
	n := a.Node(id)
	if n.Kind != TKEnum {
		panic("TypeArena.EnumArgs: not an enum")
	}
	return a.enumArgs[n.Extra]
}

// MakeParam returns the type of the i'th parameter of an enum declaration.
func (a *TypeArena) MakeParam(i int) TypeId {
	return a.intern(TKParam, uint32(i), 0, 0)
}

// DeclareAlias adds a `type` alias with no body yet and returns its index.
// The body is set with SetAliasBody once it is resolved; references to the
// alias (MakeAliasRef) can be made before that, which is how an alias
// refers to itself.
func (a *TypeArena) DeclareAlias(name NameId) uint32 {
	a.aliases = append(a.aliases, AliasDecl{Name: name, Body: TidNothing})
	return uint32(len(a.aliases) - 1)
}

// SetAliasBody sets the body of the alias at idx.
func (a *TypeArena) SetAliasBody(idx uint32, body TypeId) {
	a.aliases[idx].Body = body
}

// MakeAliasRef returns the type that refers to the alias at idx.
func (a *TypeArena) MakeAliasRef(idx uint32) TypeId {
	return a.intern(TKAlias, idx, 0, 0)
}

// AliasBody returns the body of the alias a TKAlias refers to: one step of
// unfolding. It is TidNothing while the alias is unresolved.
func (a *TypeArena) AliasBody(id TypeId) TypeId {
	n := a.Node(id)
	if n.Kind != TKAlias {
		panic("TypeArena.AliasBody: not an alias")
	}
	return a.aliases[n.A].Body
}

// MakeAbstract returns a new abstract type, equal only to itself.
func (a *TypeArena) MakeAbstract() TypeId {
	a.abstractCount++
	return a.intern(TKAbstract, a.abstractCount, 0, 0)
}

// UnionMembers returns the arms of a union type, sorted and deduplicated.
// Caller must not mutate.
func (a *TypeArena) UnionMembers(id TypeId) []TypeId {
	n := a.Node(id)
	if n.Kind != TKUnion {
		panic("TypeArena.UnionMembers: not a union")
	}
	return a.unionMembers[n.Extra]
}

// QuoteSig returns the signature of a quote type.
func (a *TypeArena) QuoteSig(id TypeId) QuoteSig {
	n := a.Node(id)
	if n.Kind != TKQuote {
		panic("TypeArena.QuoteSig: not a quote")
	}
	return a.quoteSigs[n.Extra]
}

// intern looks up an atomic composite type and returns its id, allocating
// a new node if none existed.
func (a *TypeArena) intern(kind TypeKind, x, y, extra uint32) TypeId {
	key := TypeNode{Kind: kind, A: x, B: y, Extra: extra}
	for p := a; p != nil; p = p.parent {
		if id, ok := p.atomCons[key]; ok {
			return id
		}
	}
	id := a.append(key)
	a.atomCons[key] = id
	return id
}

// append adds n, with its flags, and returns its TypeId. A composite's
// side table entry (record, union members, quote signature, enum
// arguments) is added before its node, so the flags can read it.
func (a *TypeArena) append(n TypeNode) TypeId {
	n.Flags = a.nodeFlags(n)
	id := TypeId(len(a.nodes))
	a.nodes = append(a.nodes, n)
	return id
}

// nodeFlags computes a new node's flags from its children's.
func (a *TypeArena) nodeFlags(n TypeNode) uint8 {
	var f uint8
	of := func(t TypeId) {
		if t != TidNothing {
			f |= a.nodes[t].Flags
		}
	}
	switch n.Kind {
	case TKVar:
		return NodeHasVar
	case TKList, TKCommand, TKGrid, TKGridView, TKGridRow:
		of(TypeId(n.A))
	case TKUnion:
		for _, m := range a.unionMembers[n.Extra] {
			of(m)
		}
	case TKQuote:
		sig := &a.quoteSigs[n.Extra]
		for _, t := range sig.Inputs {
			of(t)
		}
		for _, t := range sig.Outputs {
			of(t)
		}
	case TKRecord:
		rec := &a.records[n.Extra]
		for _, fl := range rec.Fields {
			of(fl.Type)
		}
		of(rec.Rest.Type)
	case TKEnum:
		for _, t := range a.enumArgs[n.Extra] {
			of(t)
		}
	}
	return f
}

// Len returns the current count of types in the arena (including primitives).
func (a *TypeArena) Len() int {
	return len(a.nodes)
}

// flattenAndCanonicalizeUnion takes a list of arm types and returns a sorted,
// deduplicated flat list. Nested unions are dissolved. The list is the
// arena's scratch space, valid until the next call.
func (a *TypeArena) flattenAndCanonicalizeUnion(arms []TypeId) []TypeId {
	out := a.unionBuf[:0]
	defer func() { a.unionBuf = out[:0] }()
	for _, arm := range arms {
		n := a.Node(arm)
		if n.Kind == TKUnion {
			out = append(out, a.unionMembers[n.Extra]...)
		} else {
			out = append(out, arm)
		}
	}
	// Sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	// Dedupe in-place.
	w := 0
	for i := 0; i < len(out); i++ {
		if i == 0 || out[i] != out[w-1] {
			out[w] = out[i]
			w++
		}
	}
	return out[:w]
}

// Cons-table keys for composite types are built into the arena's reusable
// keyBuf as fixed-width binary, with a kind byte and a count before every
// variable-length part so no two types share a key. Looking a key up as
// string(keyBuf) doesn't allocate; only a new entry copies it.

func appendKeyU32(b []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(b, v)
}

// appendUnionKey appends the key for a flattened, sorted union.
func appendUnionKey(b []byte, arms []TypeId) []byte {
	b = append(b, 'U')
	b = appendKeyU32(b, uint32(len(arms)))
	for _, arm := range arms {
		b = appendKeyU32(b, uint32(arm))
	}
	return b
}

// appendRecordKey appends the key for a normalized record.
func appendRecordKey(b []byte, fields []RecordField, rest RecordField) []byte {
	b = append(b, 'R')
	b = appendKeyU32(b, uint32(len(fields)))
	for _, f := range fields {
		b = appendKeyU32(b, uint32(f.Name))
		b = append(b, byte(f.Status))
		b = appendKeyU32(b, uint32(f.Type))
	}
	b = append(b, byte(rest.Status))
	b = appendKeyU32(b, uint32(rest.Type))
	return b
}

// appendQuoteKey appends the key for a quote signature.
func appendQuoteKey(b []byte, sig QuoteSig) []byte {
	b = append(b, 'Q')
	b = appendKeyU32(b, uint32(len(sig.Inputs)))
	for _, in := range sig.Inputs {
		b = appendKeyU32(b, uint32(in))
	}
	b = appendKeyU32(b, uint32(len(sig.Outputs)))
	for _, out := range sig.Outputs {
		b = appendKeyU32(b, uint32(out))
	}
	if sig.Diverges {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	return b
}

// NameId identifies an interned name (built-in name, user definition, field
// name, type variable name). Comparison is integer equality.
type NameId uint32

// Reserved name ids. Index 0 is the empty name and never returned by Intern.
const (
	NameNone NameId = 0
	// The built-in enum `Maybe[a] = just a | none end` and its constructors.
	NameMaybe    NameId = 1
	NameJust     NameId = 2
	NameNoneCtor NameId = 3
)

// NameTable interns strings into NameIds. Within a single checking session,
// every distinct identifier maps to a unique id.
type NameTable struct {
	// parent is the frozen table this one is an overlay of, or nil. An
	// overlay holds only its own names: ids below base are the parent's.
	parent *NameTable
	base   NameId
	ids    map[string]NameId
	names  []string
}

// Clone returns a name table with the same names that grows independently.
func (t *NameTable) Clone() *NameTable {
	return &NameTable{parent: t.parent, base: t.base, ids: maps.Clone(t.ids), names: slices.Clone(t.names)}
}

// resetOverlay empties an overlay made by Overlay, keeping its storage.
func (t *NameTable) resetOverlay() {
	clear(t.ids)
	t.names = t.names[:0]
}

// Overlay returns a name table that starts with t's names and grows on its
// own without copying them, as TypeArena.Overlay. t must not change
// afterwards.
func (t *NameTable) Overlay() *NameTable {
	return &NameTable{parent: t, base: t.Len(), ids: make(map[string]NameId)}
}

// NewNameTable constructs an empty name table.
func NewNameTable() *NameTable {
	t := &NameTable{
		ids:   make(map[string]NameId, 256),
		names: []string{""},
	}
	t.Intern("Maybe")
	t.Intern("just")
	t.Intern("none")
	return t
}

// Intern returns the NameId for s, allocating one if it hasn't been seen.
// The empty string is mapped to NameNone.
func (t *NameTable) Intern(s string) NameId {
	if s == "" {
		return NameNone
	}
	if id, ok := t.Lookup(s); ok {
		return id
	}
	id := t.Len()
	t.names = append(t.names, s)
	t.ids[s] = id
	return id
}

// Len is one more than the largest NameId in the table.
func (t *NameTable) Len() NameId {
	return t.base + NameId(len(t.names))
}

// Lookup returns the NameId for s if it has been interned.
func (t *NameTable) Lookup(s string) (NameId, bool) {
	// An overlay holds only names its parent lacks, so the order does not
	// matter; the parent first, since most words are builtins and std's.
	if t.parent != nil {
		if id, ok := t.parent.Lookup(s); ok {
			return id, true
		}
	}
	id, ok := t.ids[s]
	return id, ok
}

// Name returns the string for an id. Panics on out-of-range ids.
func (t *NameTable) Name(id NameId) string {
	if id < t.base {
		return t.parent.Name(id)
	}
	if id >= t.Len() {
		panic("NameTable.Name: id out of range")
	}
	return t.names[id-t.base]
}
