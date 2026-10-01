package main

// Type representation for the static type checker.
//
// Types are uint32 indices (TypeId) into a hashconsed arena. Identical
// structural types share an id, so type equality is integer equality.
// This is Phase 1 scope: arena, primitives, hashconsing infrastructure,
// name interning. Composite kinds are wired up but most do not have
// public constructors yet — those land in later phases as they are needed.
//
// The design of the checker being built on it is ai/type-core-calculus.typ.

import (
	"encoding/binary"
	"maps"
	"slices"
	"sort"
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
	TKPrim     TypeKind = iota // primitive; A unused
	TKMaybe                    // A = inner T
	TKList                     // A = element T
	TKDict                     // A = key T, B = value T
	TKShape                    // Extra = index into shapeFields
	TKQuote                    // Extra = index into quoteSigs
	TKOverloadedQuote          // Extra = index into overloadedQuoteSigs
	TKUnion                    // A = brand id (or 0); Extra = index into unionMembers
	TKBrand                    // A = brand id; B = underlying TypeId
	TKCommand                  // A = argv list TypeId; B = stdout capture; Extra = stderr capture
	TKVar                      // A = TypeVarId
	TKRigid                    // A = NameId of the declared generic; see MakeRigid

	// Grid family — built-in like Maybe; see "Grid types" in ai/type_checker.md.
	TKGrid     // Extra = index into gridSchemas (0 = unknown schema)
	TKGridView // Extra = index into gridSchemas (0 = unknown schema)
	TKGridRow  // Extra = index into gridSchemas (0 = unknown schema)

	// TKStrLit is a `str` refined with a statically known value: A holds the
	// interned NameId of the literal content. It is a subtype of `str` —
	// unify and every container constructor widen it back to TidStr — so it
	// behaves exactly like `str` everywhere except where a known key matters:
	// `get` reads it off the stack to resolve a shape field by name, the same
	// resolution the `:name` getter does from its token.
	TKStrLit // A = NameId of the literal string content

	// Kinds of the checker described in ai/type-core-calculus.typ.

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
	case TKMaybe:
		return "Maybe"
	case TKList:
		return "List"
	case TKDict:
		return "Dict"
	case TKShape:
		return "Shape"
	case TKQuote:
		return "Quote"
	case TKOverloadedQuote:
		return "OverloadedQuote"
	case TKUnion:
		return "Union"
	case TKBrand:
		return "Brand"
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
	case TKStrLit:
		return "StrLit"
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
	Kind  TypeKind
	A     uint32
	B     uint32
	Extra uint32
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
)

// TypeVarId identifies a generic type variable. Fresh ids are issued at
// generic-instantiation sites (each call to a polymorphic function yields
// fresh variables).
type TypeVarId uint32

// ShapeField is one field in a TKShape's column list. ShapeFields are stored
// in TypeArena.shapeFields, sorted by Name, with no duplicates.
type ShapeField struct {
	Name     NameId
	Type     TypeId
	Optional bool
}

// GridSchemaCol is one column in a TKGrid / TKGridView / TKGridRow schema.
// Order is meaningful (grids have column order).
type GridSchemaCol struct {
	Name NameId
	Type TypeId
}

// GridSchema is the full ordered column list for a grid-family type.
// The unknown schema has Columns == nil and lives at gridSchemas[0].
type GridSchema struct {
	Columns []GridSchemaCol
}

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
// listed bottom-to-top. Generics names are local to this sig.
type QuoteSig struct {
	Inputs   []TypeId
	Outputs  []TypeId
	Diverges bool // return/break/continue style control flow
	Bindings map[NameId]TypeId
	Generics []TypeVarId
}

// TypeArena is the storage for all types in a checking session.
//
// nodes is the primary store; a TypeId is an index into it.
// cons maps a structural fingerprint to the TypeId that owns it; new
// constructions look up here first to deduplicate (hashconsing).
//
// shapeFields, quoteSigs, unionMembers, and gridSchemas are side tables
// for variable-length data referenced from a TypeNode's Extra field.
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

	shapeFields    [][]ShapeField
	quoteSigs      []QuoteSig
	overloadedQuoteSigs [][]QuoteSig
	unionMembers   [][]TypeId // each slice is sorted, deduped
	gridSchemas    []GridSchema
	gridSchemaCons map[string]uint32
	records        []RecordType
	enumDecls      []EnumDecl
	enumArgs       [][]TypeId
	aliases        []AliasDecl
	abstractCount  uint32
	// keyBuf is scratch space for building composite cons keys.
	keyBuf []byte
}

// Clone returns an arena that grows independently: every TypeId already in
// the arena means the same in both, and neither sees what the other adds
// afterwards. Entries never change once added, so their contents are
// shared; only the tables that grow are copied.
func (a *TypeArena) Clone() *TypeArena {
	return &TypeArena{
		parent:              a.parent,
		nodes:               slices.Clone(a.nodes),
		cons:                maps.Clone(a.cons),
		atomCons:            maps.Clone(a.atomCons),
		shapeFields:         slices.Clone(a.shapeFields),
		quoteSigs:           slices.Clone(a.quoteSigs),
		overloadedQuoteSigs: slices.Clone(a.overloadedQuoteSigs),
		unionMembers:        slices.Clone(a.unionMembers),
		gridSchemas:         slices.Clone(a.gridSchemas),
		gridSchemaCons:      maps.Clone(a.gridSchemaCons),
		records:             slices.Clone(a.records),
		enumDecls:           slices.Clone(a.enumDecls),
		enumArgs:            slices.Clone(a.enumArgs),
		aliases:             slices.Clone(a.aliases),
		abstractCount:       a.abstractCount,
	}
}

// Overlay returns an arena that starts with every type in a and grows on
// its own, without copying a's tables: a check starts in constant time
// from a base built once. a must not change afterwards. The overlay's
// slices share a's backing arrays with their capacity capped, so the first
// append to each copies it, and its hash-consing maps hold only what the
// overlay adds, looked up before a's.
func (a *TypeArena) Overlay() *TypeArena {
	return &TypeArena{
		parent:              a,
		nodes:               slices.Clip(a.nodes),
		cons:                make(map[string]TypeId, 64),
		atomCons:            make(map[TypeNode]TypeId, 64),
		shapeFields:         slices.Clip(a.shapeFields),
		quoteSigs:           slices.Clip(a.quoteSigs),
		overloadedQuoteSigs: slices.Clip(a.overloadedQuoteSigs),
		unionMembers:        slices.Clip(a.unionMembers),
		gridSchemas:         slices.Clip(a.gridSchemas),
		gridSchemaCons:      make(map[string]uint32),
		records:             slices.Clip(a.records),
		enumDecls:           slices.Clip(a.enumDecls),
		enumArgs:            slices.Clip(a.enumArgs),
		aliases:             slices.Clip(a.aliases),
		abstractCount:       a.abstractCount,
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

func (a *TypeArena) lookupGridSchema(key []byte) (uint32, bool) {
	for p := a; p != nil; p = p.parent {
		if idx, ok := p.gridSchemaCons[string(key)]; ok {
			return idx, true
		}
	}
	return 0, false
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
	// Reserve gridSchemas[0] as the "unknown schema" sentinel.
	a.gridSchemas = append(a.gridSchemas, GridSchema{})
	// Reserve unionMembers[0] as a placeholder so non-zero Extra is meaningful.
	a.unionMembers = append(a.unionMembers, nil)
	// Same for shapeFields and quoteSigs.
	a.shapeFields = append(a.shapeFields, nil)
	a.quoteSigs = append(a.quoteSigs, QuoteSig{})
	a.overloadedQuoteSigs = append(a.overloadedQuoteSigs, nil)
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

// MakeMaybeEnum returns `Maybe[t]` as an instance of the built-in enum, the
// form the core checker and its relations use. (MakeMaybe is the old
// checker's TKMaybe.)
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

// MakeMaybe returns the canonical TypeId for Maybe[inner]. If a Maybe of
// the same inner type was constructed before, the existing id is returned.
func (a *TypeArena) MakeMaybe(inner TypeId) TypeId {
	return a.intern(TKMaybe, uint32(a.WidenStrLit(inner)), 0, 0)
}

// MakeList returns the canonical TypeId for [elem].
func (a *TypeArena) MakeList(elem TypeId) TypeId {
	return a.intern(TKList, uint32(a.WidenStrLit(elem)), 0, 0)
}

// MakeDict returns the canonical TypeId for {key: value}.
func (a *TypeArena) MakeDict(key, value TypeId) TypeId {
	return a.intern(TKDict, uint32(a.WidenStrLit(key)), uint32(a.WidenStrLit(value)), 0)
}

// MakeStrLit returns the canonical TypeId for a `str` refined to the literal
// value named by `name`. It is a subtype of TidStr.
func (a *TypeArena) MakeStrLit(name NameId) TypeId {
	return a.intern(TKStrLit, uint32(name), 0, 0)
}

// StrLitName returns the interned literal value of a TKStrLit type, or
// (0, false) if id is not a string literal.
func (a *TypeArena) StrLitName(id TypeId) (NameId, bool) {
	n := a.Node(id)
	if n.Kind != TKStrLit {
		return 0, false
	}
	return NameId(n.A), true
}

// WidenStrLit widens a top-level string-literal refinement to plain `str`;
// any other type is returned unchanged. Container constructors and unify
// funnel through this so a literal never escapes the stack slot it was
// produced on — it stays observable only where a known key is read.
func (a *TypeArena) WidenStrLit(id TypeId) TypeId {
	if a.Node(id).Kind == TKStrLit {
		return TidStr
	}
	return id
}

// MakeVar returns the canonical TypeId for the generic type variable v.
// Two calls with the same TypeVarId always return the same TypeId.
func (a *TypeArena) MakeVar(v TypeVarId) TypeId {
	return a.intern(TKVar, uint32(v), 0, 0)
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

// MakeBrand returns a nominal-branded type wrapping underlying.
// Two calls with the same brandId always return the same TypeId, even if
// underlying differs (which is a programmer error caught at higher levels).
func (a *TypeArena) MakeBrand(brandId NameId, underlying TypeId) TypeId {
	return a.intern(TKBrand, uint32(brandId), uint32(underlying), 0)
}

// MakeCommand returns the canonical TypeId for an executable command value.
// argv is the underlying command-list type. stdout/stderr capture modes
// determine the stack outputs produced by `?`, `;`, and `!`.
func (a *TypeArena) MakeCommand(argv TypeId, stdout, stderr CommandCaptureMode) TypeId {
	return a.intern(TKCommand, uint32(argv), uint32(stdout), uint32(stderr))
}

// MakeShape returns the canonical TypeId for a record/shape type with the
// given fields. The fields are normalized (sorted by Name, duplicate-checked)
// before lookup so two equivalent shapes always share a TypeId. A duplicate
// field name is a programmer error and panics.
func (a *TypeArena) MakeShape(fields []ShapeField) TypeId {
	// A field never holds a string-literal refinement; widen so shapes stay
	// keyed on plain value types (and hash-cons identically regardless of
	// whether a field value arrived as a literal).
	for i := range fields {
		if w := a.WidenStrLit(fields[i].Type); w != fields[i].Type {
			fields = append([]ShapeField(nil), fields...)
			for j := range fields {
				fields[j].Type = a.WidenStrLit(fields[j].Type)
			}
			break
		}
	}
	normalized := normalizeShapeFields(fields)
	a.keyBuf = appendShapeKey(a.keyBuf[:0], normalized)
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	idx := uint32(len(a.shapeFields))
	a.shapeFields = append(a.shapeFields, normalized)
	id := a.append(TypeNode{Kind: TKShape, Extra: idx})
	a.cons[string(a.keyBuf)] = id
	return id
}

// MakeUnion returns the canonical TypeId for a structural union of arms.
// Arms are flattened (nested unions are dissolved), sorted by TypeId, and
// deduplicated. A union with one arm collapses to that arm.
//
// brandId is 0 for an unbranded structural union, or a NameId for a
// nominally-branded one. Two unions with the same arms but different
// brand ids are distinct types.
func (a *TypeArena) MakeUnion(arms []TypeId, brandId NameId) TypeId {
	flat := a.flattenAndCanonicalizeUnion(arms)
	if len(flat) == 1 && brandId == 0 {
		return flat[0]
	}
	a.keyBuf = appendUnionKey(a.keyBuf[:0], flat, brandId)
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	idx := uint32(len(a.unionMembers))
	a.unionMembers = append(a.unionMembers, flat)
	id := a.append(TypeNode{Kind: TKUnion, A: uint32(brandId), Extra: idx})
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

// MakeOverloadedQuote returns the canonical TypeId for a quote value that
// still has multiple possible signatures. This is used for bare quoted
// overloaded words like `(>)`; the concrete arm is selected when context
// later supplies an expected quote type or `x` applies it to the live stack.
func (a *TypeArena) MakeOverloadedQuote(sigs []QuoteSig) TypeId {
	if len(sigs) == 1 {
		return a.MakeQuote(sigs[0])
	}
	a.keyBuf = appendOverloadedQuoteKey(a.keyBuf[:0], sigs)
	if id, ok := a.lookupCons(a.keyBuf); ok {
		return id
	}
	cp := make([]QuoteSig, len(sigs))
	copy(cp, sigs)
	idx := uint32(len(a.overloadedQuoteSigs))
	a.overloadedQuoteSigs = append(a.overloadedQuoteSigs, cp)
	id := a.append(TypeNode{Kind: TKOverloadedQuote, Extra: idx})
	a.cons[string(a.keyBuf)] = id
	return id
}

// MakeGrid returns the canonical TypeId for a grid type. schemaIdx of 0
// denotes "schema unknown" (the V1 default until schema tracking lands).
func (a *TypeArena) MakeGrid(schemaIdx uint32) TypeId {
	return a.intern(TKGrid, 0, 0, schemaIdx)
}

// MakeGridSchemaIdx interns a GridSchema and returns the schema index used by
// TKGrid / TKGridView / TKGridRow nodes. An empty cols slice maps to the
// "schema unknown" sentinel (idx 0). Two structurally equal column lists
// (same names in the same order, same type ids) share an index so that
// MakeGrid(idx) hash-conses to the same TypeId.
func (a *TypeArena) MakeGridSchemaIdx(cols []GridSchemaCol) uint32 {
	if len(cols) == 0 {
		return 0
	}
	a.keyBuf = appendGridSchemaKey(a.keyBuf[:0], cols)
	if a.gridSchemaCons == nil {
		a.gridSchemaCons = make(map[string]uint32, 8)
	}
	if idx, ok := a.lookupGridSchema(a.keyBuf); ok {
		return idx
	}
	cp := make([]GridSchemaCol, len(cols))
	copy(cp, cols)
	idx := uint32(len(a.gridSchemas))
	a.gridSchemas = append(a.gridSchemas, GridSchema{Columns: cp})
	a.gridSchemaCons[string(a.keyBuf)] = idx
	return idx
}

// MakeGridView returns the canonical TypeId for a grid-view type.
func (a *TypeArena) MakeGridView(schemaIdx uint32) TypeId {
	return a.intern(TKGridView, 0, 0, schemaIdx)
}

// MakeGridRow returns the canonical TypeId for a grid-row type.
func (a *TypeArena) MakeGridRow(schemaIdx uint32) TypeId {
	return a.intern(TKGridRow, 0, 0, schemaIdx)
}

// MakeRecord returns the canonical TypeId for a dict-kinded type with the
// given declared labels and remainder. Labels are sorted, the type of an
// absent or open label is dropped, and a declared label that says the same
// as the remainder is dropped, so two records that agree on every label
// share a TypeId. A duplicate label is a programmer error and panics.
func (a *TypeArena) MakeRecord(fields []RecordField, rest RecordField) TypeId {
	rest = normalizeRecordField(rest)
	rest.Name = NameNone
	out := make([]RecordField, 0, len(fields))
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
	a.records = append(a.records, RecordType{Fields: out, Rest: rest})
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

// ShapeFields returns the fields of a shape type. Caller must not mutate.
func (a *TypeArena) ShapeFields(id TypeId) []ShapeField {
	n := a.Node(id)
	if n.Kind != TKShape {
		panic("TypeArena.ShapeFields: not a shape")
	}
	return a.shapeFields[n.Extra]
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

// OverloadedQuoteSigs returns the candidate signatures of an overloaded
// quote type. Caller must not mutate.
func (a *TypeArena) OverloadedQuoteSigs(id TypeId) []QuoteSig {
	n := a.Node(id)
	if n.Kind != TKOverloadedQuote {
		panic("TypeArena.OverloadedQuoteSigs: not an overloaded quote")
	}
	return a.overloadedQuoteSigs[n.Extra]
}

// GridSchema returns the schema for a grid-family type. The unknown-schema
// sentinel is returned when no schema is tracked.
func (a *TypeArena) GridSchema(id TypeId) GridSchema {
	n := a.Node(id)
	switch n.Kind {
	case TKGrid, TKGridView, TKGridRow:
		return a.gridSchemas[n.Extra]
	}
	panic("TypeArena.GridSchema: not a grid-family type")
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

// append adds n and returns its TypeId.
func (a *TypeArena) append(n TypeNode) TypeId {
	id := TypeId(len(a.nodes))
	a.nodes = append(a.nodes, n)
	return id
}

// Len returns the current count of types in the arena (including primitives).
func (a *TypeArena) Len() int {
	return len(a.nodes)
}

// flattenAndCanonicalizeUnion takes a list of arm types and returns a sorted,
// deduplicated, brand-respecting flat list. Nested unbranded unions are
// dissolved; branded unions stay as a single arm (their brand is opaque).
func (a *TypeArena) flattenAndCanonicalizeUnion(arms []TypeId) []TypeId {
	out := make([]TypeId, 0, len(arms))
	for _, arm := range arms {
		arm = a.WidenStrLit(arm)
		n := a.Node(arm)
		if n.Kind == TKUnion && n.A == 0 {
			// Unbranded inner union: flatten its arms.
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

// appendShapeKey appends the key for a normalized shape.
func appendShapeKey(b []byte, fields []ShapeField) []byte {
	b = append(b, 'S')
	b = appendKeyU32(b, uint32(len(fields)))
	for _, f := range fields {
		b = appendKeyU32(b, uint32(f.Name))
		if f.Optional {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
		b = appendKeyU32(b, uint32(f.Type))
	}
	return b
}

// appendUnionKey appends the key for a flattened, sorted union.
func appendUnionKey(b []byte, arms []TypeId, brandId NameId) []byte {
	b = append(b, 'U')
	b = appendKeyU32(b, uint32(brandId))
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

// appendGridSchemaKey appends the key for a grid schema. Order is
// significant — grids carry column order — so columns are not sorted.
func appendGridSchemaKey(b []byte, cols []GridSchemaCol) []byte {
	b = append(b, 'G')
	b = appendKeyU32(b, uint32(len(cols)))
	for _, c := range cols {
		b = appendKeyU32(b, uint32(c.Name))
		b = appendKeyU32(b, uint32(c.Type))
	}
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
	b = appendKeyU32(b, uint32(len(sig.Bindings)))
	if len(sig.Bindings) > 0 {
		names := make([]int, 0, len(sig.Bindings))
		for name := range sig.Bindings {
			names = append(names, int(name))
		}
		sort.Ints(names)
		for _, name := range names {
			b = appendKeyU32(b, uint32(name))
			b = appendKeyU32(b, uint32(sig.Bindings[NameId(name)]))
		}
	}
	b = appendKeyU32(b, uint32(len(sig.Generics)))
	for _, g := range sig.Generics {
		b = appendKeyU32(b, uint32(g))
	}
	return b
}

// appendOverloadedQuoteKey appends the key for an overload set. Candidate
// order is significant: overload dispatch is most-specific-first with
// source/table order as the deterministic fallback.
func appendOverloadedQuoteKey(b []byte, sigs []QuoteSig) []byte {
	b = append(b, 'O')
	b = appendKeyU32(b, uint32(len(sigs)))
	for _, sig := range sigs {
		b = appendQuoteKey(b, sig)
	}
	return b
}

// normalizeShapeFields returns a sorted copy of fields with duplicate-name
// detection. Panics on duplicate names — duplicate fields in a shape literal
// is a static error and should be caught higher up; reaching here is a bug.
func normalizeShapeFields(fields []ShapeField) []ShapeField {
	out := make([]ShapeField, len(fields))
	copy(out, fields)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].Name == out[i].Name {
			panic("normalizeShapeFields: duplicate field name")
		}
	}
	return out
}

// NameId identifies an interned name (built-in name, user definition, field
// name, brand, type variable name). Comparison is integer equality.
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
	// parent is the frozen table this one is an overlay of, or nil.
	parent *NameTable
	ids    map[string]NameId
	names  []string
}

// Clone returns a name table with the same names that grows independently.
func (t *NameTable) Clone() *NameTable {
	return &NameTable{parent: t.parent, ids: maps.Clone(t.ids), names: slices.Clone(t.names)}
}

// Overlay returns a name table that starts with t's names and grows on its
// own without copying them, as TypeArena.Overlay. t must not change
// afterwards.
func (t *NameTable) Overlay() *NameTable {
	return &NameTable{parent: t, ids: make(map[string]NameId, 32), names: slices.Clip(t.names)}
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
	for p := t; p != nil; p = p.parent {
		if id, ok := p.ids[s]; ok {
			return id
		}
	}
	id := NameId(len(t.names))
	t.names = append(t.names, s)
	t.ids[s] = id
	return id
}

// Lookup returns the NameId for s if it has been interned.
func (t *NameTable) Lookup(s string) (NameId, bool) {
	for p := t; p != nil; p = p.parent {
		if id, ok := p.ids[s]; ok {
			return id, true
		}
	}
	return NameNone, false
}

// Name returns the string for an id. Panics on out-of-range ids.
func (t *NameTable) Name(id NameId) string {
	if int(id) >= len(t.names) {
		panic("NameTable.Name: id out of range")
	}
	return t.names[id]
}

// IsReservedTypeName reports whether name is a built-in type name that
// cannot be shadowed by a user `type` declaration.
func IsReservedTypeName(name string) bool {
	switch name {
	case "int", "float", "str", "bool", "bytes", "none", "null",
		"path", "datetime", "Maybe", "Grid", "GridView", "GridRow":
		return true
	}
	return false
}
