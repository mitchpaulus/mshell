package main

// Type representation for the static type checker.
//
// Types are uint32 indices (TypeId) into a hashconsed arena. Identical
// structural types share an id, so type equality is integer equality.
// This is Phase 1 scope: arena, primitives, hashconsing infrastructure,
// name interning. Composite kinds are wired up but most do not have
// public constructors yet — those land in later phases as they are needed.
//
// See ai/type_checker.md for the full design.

import (
	"strconv"
	"strings"
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
	TidNone
	TidPath     // path literal (`...`) and the path runtime type
	TidDateTime // date/time literal (YYYY-MM-DD[THH:MM[:SS]]) and now/date ops
	TidBottom   // divergent: exit, infinite loop, (Phase 2) propagated fail
)

// firstCompositeId is the id at which user-constructed composite types begin.
// Anything below this is a primitive baked into the arena at init time.
const firstCompositeId TypeId = TidBottom + 1

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
	TKUnion                    // A = brand id (or 0); Extra = index into unionMembers
	TKBrand                    // A = brand id; B = underlying TypeId
	TKVar                      // A = TypeVarId

	// Grid family — built-in like Maybe; see "Grid types" in ai/type_checker.md.
	TKGrid     // Extra = index into gridSchemas (0 = unknown schema)
	TKGridView // Extra = index into gridSchemas (0 = unknown schema)
	TKGridRow  // Extra = index into gridSchemas (0 = unknown schema)
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
	case TKUnion:
		return "Union"
	case TKBrand:
		return "Brand"
	case TKVar:
		return "Var"
	case TKGrid:
		return "Grid"
	case TKGridView:
		return "GridView"
	case TKGridRow:
		return "GridRow"
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

// TypeVarId identifies a generic type variable. Fresh ids are issued at
// generic-instantiation sites (each call to a polymorphic function yields
// fresh variables).
type TypeVarId uint32

// ShapeField is one field in a TKShape's column list. ShapeFields are stored
// in TypeArena.shapeFields, sorted by Name, with no duplicates.
type ShapeField struct {
	Name NameId
	Type TypeId
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

// QuoteSig is a function or quote signature. Inputs are listed bottom-to-top
// (so the last element is the top of the consumed stack). Outputs are also
// listed bottom-to-top. Generics names are local to this sig.
//
// Fail and Pure are reserved for Phase 2; in V1 they are TidNothing and
// false respectively. Including them in the struct now means Phase 2 won't
// require a struct migration.
type QuoteSig struct {
	Inputs   []TypeId
	Outputs  []TypeId
	Fail     TypeId // Phase 2
	Pure     bool   // Phase 2
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
	nodes []TypeNode
	cons  map[string]TypeId

	shapeFields  [][]ShapeField
	quoteSigs    []QuoteSig
	unionMembers [][]TypeId // each slice is sorted, deduped
	gridSchemas  []GridSchema
}

// NewTypeArena constructs an arena pre-populated with the primitive ids
// TidBool through TidBottom. After this call, primitive constants resolve
// to live nodes.
func NewTypeArena() *TypeArena {
	a := &TypeArena{
		cons: make(map[string]TypeId, 64),
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
	return a
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
	return a.intern(TKMaybe, uint32(inner), 0, 0, "")
}

// MakeList returns the canonical TypeId for [elem].
func (a *TypeArena) MakeList(elem TypeId) TypeId {
	return a.intern(TKList, uint32(elem), 0, 0, "")
}

// MakeDict returns the canonical TypeId for {key: value}.
func (a *TypeArena) MakeDict(key, value TypeId) TypeId {
	return a.intern(TKDict, uint32(key), uint32(value), 0, "")
}

// MakeVar returns the canonical TypeId for the generic type variable v.
// Two calls with the same TypeVarId always return the same TypeId.
func (a *TypeArena) MakeVar(v TypeVarId) TypeId {
	return a.intern(TKVar, uint32(v), 0, 0, "")
}

// MakeBrand returns a nominal-branded type wrapping underlying.
// Two calls with the same brandId always return the same TypeId, even if
// underlying differs (which is a programmer error caught at higher levels).
func (a *TypeArena) MakeBrand(brandId NameId, underlying TypeId) TypeId {
	return a.intern(TKBrand, uint32(brandId), uint32(underlying), 0, "")
}

// MakeShape returns the canonical TypeId for a record/shape type with the
// given fields. The fields are normalized (sorted by Name, duplicate-checked)
// before lookup so two equivalent shapes always share a TypeId. A duplicate
// field name is a programmer error and panics.
func (a *TypeArena) MakeShape(fields []ShapeField) TypeId {
	normalized := normalizeShapeFields(fields)
	key := encodeShapeKey(normalized)
	if id, ok := a.cons[key]; ok {
		return id
	}
	idx := uint32(len(a.shapeFields))
	a.shapeFields = append(a.shapeFields, normalized)
	id := a.append(TypeNode{Kind: TKShape, Extra: idx})
	a.cons[key] = id
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
	key := encodeUnionKey(flat, brandId)
	if id, ok := a.cons[key]; ok {
		return id
	}
	idx := uint32(len(a.unionMembers))
	a.unionMembers = append(a.unionMembers, flat)
	id := a.append(TypeNode{Kind: TKUnion, A: uint32(brandId), Extra: idx})
	a.cons[key] = id
	return id
}

// MakeQuote returns the canonical TypeId for a quote/function signature.
// In V1, sig.Fail must be TidNothing and sig.Pure must be false; Phase 2
// will populate them.
func (a *TypeArena) MakeQuote(sig QuoteSig) TypeId {
	key := encodeQuoteKey(sig)
	if id, ok := a.cons[key]; ok {
		return id
	}
	idx := uint32(len(a.quoteSigs))
	a.quoteSigs = append(a.quoteSigs, sig)
	id := a.append(TypeNode{Kind: TKQuote, Extra: idx})
	a.cons[key] = id
	return id
}

// MakeGrid returns the canonical TypeId for a grid type. schemaIdx of 0
// denotes "schema unknown" (the V1 default until schema tracking lands).
func (a *TypeArena) MakeGrid(schemaIdx uint32) TypeId {
	return a.intern(TKGrid, 0, 0, schemaIdx, "")
}

// MakeGridView returns the canonical TypeId for a grid-view type.
func (a *TypeArena) MakeGridView(schemaIdx uint32) TypeId {
	return a.intern(TKGridView, 0, 0, schemaIdx, "")
}

// MakeGridRow returns the canonical TypeId for a grid-row type.
func (a *TypeArena) MakeGridRow(schemaIdx uint32) TypeId {
	return a.intern(TKGridRow, 0, 0, schemaIdx, "")
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
// a new node if none existed. extraStr is reserved for a future variant
// where extras might need to participate in the key but no atomic kind uses
// it today.
func (a *TypeArena) intern(kind TypeKind, x, y, extra uint32, extraStr string) TypeId {
	key := encodeAtomicKey(kind, x, y, extra, extraStr)
	if id, ok := a.cons[key]; ok {
		return id
	}
	id := a.append(TypeNode{Kind: kind, A: x, B: y, Extra: extra})
	a.cons[key] = id
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

// encodeAtomicKey builds the cons-table key for kinds that have all their
// data in TypeNode (no side-table content).
func encodeAtomicKey(kind TypeKind, a, b, extra uint32, extraStr string) string {
	var sb strings.Builder
	sb.WriteByte(byte(kind) + 'a')
	sb.WriteByte(':')
	sb.WriteString(strconv.FormatUint(uint64(a), 10))
	sb.WriteByte(':')
	sb.WriteString(strconv.FormatUint(uint64(b), 10))
	sb.WriteByte(':')
	sb.WriteString(strconv.FormatUint(uint64(extra), 10))
	if extraStr != "" {
		sb.WriteByte(':')
		sb.WriteString(extraStr)
	}
	return sb.String()
}

// encodeShapeKey builds the cons-table key for a normalized shape.
func encodeShapeKey(fields []ShapeField) string {
	var sb strings.Builder
	sb.WriteString("S:")
	for i, f := range fields {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(f.Name), 10))
		sb.WriteByte('=')
		sb.WriteString(strconv.FormatUint(uint64(f.Type), 10))
	}
	return sb.String()
}

// encodeUnionKey builds the cons-table key for a flattened, sorted union.
func encodeUnionKey(arms []TypeId, brandId NameId) string {
	var sb strings.Builder
	sb.WriteString("U:")
	sb.WriteString(strconv.FormatUint(uint64(brandId), 10))
	sb.WriteByte(':')
	for i, arm := range arms {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(arm), 10))
	}
	return sb.String()
}

// encodeQuoteKey builds the cons-table key for a quote signature.
func encodeQuoteKey(sig QuoteSig) string {
	var sb strings.Builder
	sb.WriteString("Q:")
	for i, in := range sig.Inputs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(in), 10))
	}
	sb.WriteString(";")
	for i, out := range sig.Outputs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(out), 10))
	}
	sb.WriteByte(';')
	sb.WriteString(strconv.FormatUint(uint64(sig.Fail), 10))
	sb.WriteByte(';')
	if sig.Pure {
		sb.WriteByte('P')
	} else {
		sb.WriteByte('-')
	}
	sb.WriteByte(';')
	for i, g := range sig.Generics {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(g), 10))
	}
	return sb.String()
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
)

// NameTable interns strings into NameIds. Within a single checking session,
// every distinct identifier maps to a unique id.
type NameTable struct {
	ids   map[string]NameId
	names []string
}

// NewNameTable constructs an empty name table.
func NewNameTable() *NameTable {
	return &NameTable{
		ids:   make(map[string]NameId, 256),
		names: []string{""},
	}
}

// Intern returns the NameId for s, allocating one if it hasn't been seen.
// The empty string is mapped to NameNone.
func (t *NameTable) Intern(s string) NameId {
	if s == "" {
		return NameNone
	}
	if id, ok := t.ids[s]; ok {
		return id
	}
	id := NameId(len(t.names))
	t.names = append(t.names, s)
	t.ids[s] = id
	return id
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
	case "int", "float", "str", "bool", "bytes", "none",
		"Maybe", "Grid", "GridView", "GridRow":
		return true
	}
	return false
}
