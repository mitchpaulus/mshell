package main

import (
	"slices"
	"strconv"
)

// Grids in the core checker (ai/type-core-calculus.typ, "Grids are shapes
// of columns"; ai/builtin-audit/dictgrid.md).
//
// A grid, view or row type's schema is a record type with one label per
// column (TypeArena.MakeGridOf). A grid literal's schema is exact: each
// column is required, at the join of its cells. A schema that is not known
// statically is the read-only `{| open}`: every schema is below it, a
// column read from it is unknown, and nothing writes into it unless the
// grid is new. toGrid's schema is `{*: str}`: its column names are data,
// and every column holds strings.
//
// Columns are labels, so the per-label rule decides which schemas are below
// which. A write into a shared grid keeps every column's type: updateCol,
// gridSetCell and extend write at a column's own type. Anything that
// changes the schema in place (a column's type, adding, removing or
// renaming a column) needs a new grid; otherwise it is an error that
// suggests deepCopy (P6).
//
// Words whose result schema a signature can say are in the table, with the
// schema as a generic (`(Grid_s (GridRow_s -- bool) -- GridView_s)`); the
// words here compute it.

// gridOf reads stack slot i as a grid, view or row: its kind and schema.
func (c *coreChecker) gridOf(i int) (TypeKind, TypeId, bool) {
	if i < c.floor || c.waiting(c.stack[i]) != nil {
		return 0, TidNothing, false
	}
	return c.gridType(c.stack[i].t)
}

// gridType reads t as a grid, view or row. A schema not solved yet reads
// as the unknown one, which only allows what is sound for every schema.
func (c *coreChecker) gridType(t TypeId) (TypeKind, TypeId, bool) {
	t = c.unfold(c.subst.Apply(c.arena, t))
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKGrid, TKGridView, TKGridRow:
	default:
		return 0, TidNothing, false
	}
	rec := TypeId(n.A)
	if rec == TidNothing || c.arena.nodes[rec].Kind != TKRecord {
		rec = c.res.unknownSchema()
	}
	return n.Kind, rec, true
}

func (c *coreChecker) schemaOf(rec TypeId) RecordType {
	return c.arena.records[c.arena.nodes[rec].Extra]
}

// exactSchema reports whether a schema names every column there is.
func (c *coreChecker) exactSchema(rec TypeId) bool {
	return c.schemaOf(rec).Rest.Status == FieldAbsent
}

// schemaImmutable reports whether no cell of a grid with this schema holds
// a list, dict or grid. A new grid over such cells is fresh, as a new list
// of immutable elements is (design doc, "One rule for new lists").
func (c *coreChecker) schemaImmutable(rec TypeId) bool {
	r := c.schemaOf(c.subst.Apply(c.arena, rec))
	for _, f := range append(r.Fields, r.Rest) {
		switch f.Status {
		case FieldAbsent:
			continue
		case FieldOpen:
			return false
		}
		if !c.rel.Immutable(c.subst.Apply(c.arena, f.Type)) {
			return false
		}
	}
	return true
}

// pushGrid pushes a grid of the given kind and schema.
func (c *coreChecker) pushGrid(kind TypeKind, rec TypeId, fresh bool) {
	c.push(c.arena.MakeGridOf(kind, rec), fresh)
}

// pushNewGrid pushes a new grid over shared cells: fresh when they are
// immutable.
func (c *coreChecker) pushNewGrid(rec TypeId) {
	c.pushGrid(TKGrid, rec, c.schemaImmutable(rec))
}

// columns describes a schema in a message.
func (c *coreChecker) columns(rec TypeId) string {
	rec = c.subst.Apply(c.arena, rec)
	if s := formatSchema(c.arena, c.names, c.schemaOf(rec)); s != "" {
		return s
	}
	return "not known"
}

// gridError reports an error about a grid word, and stops the unit.
func (c *coreChecker) gridError(tok Token, hint string) {
	c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: hint})
	c.abandoned = true
}

// noColumn reports a column the schema says the grid does not have.
func (c *coreChecker) noColumn(tok Token, rec TypeId, name NameId) {
	c.gridError(tok, "the grid has no column '"+c.names.Name(name)+"'; its columns are "+c.columns(rec))
}

// newSchemaHint is the fix for a schema change in place on a stored grid.
func newSchemaHint(word string) string {
	return "'" + word + "' changes the grid's columns in place, and the grid is stored, so another reference to it" +
		" would see columns its type does not say; make a new one first with deepCopy"
}

// columnRead is the type of the cells of the column key names, a literal
// or not: Get on the schema, or Get-Key when the name is known only at run
// time. A literal name the schema says is absent is an error, since the
// runtime fails there.
func (c *coreChecker) columnRead(rec TypeId, k coreSlot, tok Token) (TypeId, bool) {
	if name := k.key(); name != NameNone {
		t, status := c.labelRead(rec, name)
		if status == FieldAbsent {
			c.noColumn(tok, rec, name)
			return TidNothing, false
		}
		return t, true
	}
	return c.readType(rec, k, tok)
}

// writeAt reports whether a value in slot v may be written into the column
// name of a shared grid: the column is writable (declared, or under a
// `*: T` remainder) and v fits its type. With a name known only at run
// time, v must fit every column (Set-Key).
func (c *coreChecker) writeAt(rec TypeId, name NameId, v coreSlot) bool {
	r := c.schemaOf(rec)
	fits := func(f RecordField) bool {
		switch f.Status {
		case FieldRequired, FieldOptional, FieldDeletable:
			return c.check(v, f.Type)
		}
		return false
	}
	if name != NameNone {
		return fits(r.FieldAt(name))
	}
	for _, f := range append(r.Fields, r.Rest) {
		if f.Status != FieldAbsent && !fits(f) {
			return false
		}
	}
	return true
}

// withColumn is rec with column name required at type t. With a name
// known only at run time, any column the grid may lack, declared or not,
// may now be one of type t; the runtime refuses a column it has.
func (c *coreChecker) withColumn(rec TypeId, name NameId, t TypeId) TypeId {
	r := c.schemaOf(rec)
	if name != NameNone {
		fields := make([]RecordField, 0, len(r.Fields)+1)
		for _, f := range r.Fields {
			if f.Name != name {
				fields = append(fields, f)
			}
		}
		return c.arena.MakeRecord(append(fields, RecordField{Name: name, Status: FieldRequired, Type: t}), r.Rest)
	}
	fields := make([]RecordField, len(r.Fields))
	for i, f := range r.Fields {
		fields[i] = f
		if f.Status != FieldRequired {
			nf := c.restWith(RecordField{Status: f.Status, Type: f.Type}, t)
			nf.Name = f.Name
			fields[i] = nf
		}
	}
	return c.arena.MakeRecord(fields, c.restWith(r.Rest, t))
}

// restWith is a remainder under which a column of type t may also be.
func (c *coreChecker) restWith(rest RecordField, t TypeId) RecordField {
	switch rest.Status {
	case FieldAbsent:
		return RecordField{Status: FieldOptional, Type: t}
	case FieldOpen:
		return rest
	}
	if j, ok := c.joinSlot(coreSlot{t: rest.Type}, coreSlot{t: t}); ok {
		return RecordField{Status: FieldOptional, Type: j.t}
	}
	return RecordField{Status: FieldOpen}
}

// withoutColumn is rec without column name. With a name known only at run
// time, every column may be the one removed.
func (c *coreChecker) withoutColumn(rec TypeId, name NameId) TypeId {
	r := c.schemaOf(rec)
	fields := make([]RecordField, 0, len(r.Fields)+1)
	for _, f := range r.Fields {
		switch {
		case name == NameNone && f.Status == FieldRequired:
			f.Status = FieldOptional
		case f.Name == name:
			continue
		}
		fields = append(fields, f)
	}
	if name != NameNone {
		fields = append(fields, RecordField{Name: name, Status: FieldAbsent})
	}
	return c.arena.MakeRecord(fields, r.Rest)
}

// ---------------------------------------------------------------------------
// Literals

// gridLiteral types `[| meta; cols; rows |]`. Each cell and metadata dict
// runs on its own stack and leaves one value. A column's type is the join
// of its cells; a column with no cells gets a new variable, as `[]` does.
// The grid is fresh when every cell is fresh or immutable. Metadata dicts
// are kept by reference and only read back as `{}`, so their types add
// nothing.
func (c *coreChecker) gridLiteral(g *MShellParseGrid) {
	tok := g.StartToken
	// The runtime ignores a break or continue in a cell or a metadata
	// dict, so they cannot leave one (as in a format string).
	brk, cont := c.brk, c.cont
	c.brk, c.cont = coreLoopCtx{}, coreLoopCtx{}
	defer func() { c.brk, c.cont = brk, cont }()
	if g.GridMeta != nil && !c.metaDict(g.GridMeta) {
		return
	}
	names := make([]NameId, len(g.Columns))
	for i, col := range g.Columns {
		names[i] = c.names.Intern(col.Name)
		for _, prev := range names[:i] {
			if prev == names[i] {
				c.gridError(col.NameToken, "the grid has two columns named '"+col.Name+"'")
				return
			}
		}
		if col.Meta != nil && !c.metaDict(col.Meta) {
			return
		}
	}
	cols := make([]coreSlot, len(g.Columns))
	seen := make([]bool, len(g.Columns))
	fresh := true
	for r, row := range g.Rows {
		if len(row) != len(g.Columns) {
			c.gridError(tok, "row "+strconv.Itoa(r+1)+" has "+strconv.Itoa(len(row))+" cells, but the grid has "+
				strconv.Itoa(len(g.Columns))+" columns")
			return
		}
		for ci, cell := range row {
			start, outer := c.child([]MShellParseItem{cell})
			c.floor = outer
			if c.diverged || c.abandoned {
				return
			}
			if len(c.stack)-start != 1 {
				c.errs = append(c.errs, TypeError{Kind: TErrChildStack, Pos: cell.GetStartToken(),
					Hint: "a grid cell must leave exactly one value, but this one leaves " + strconv.Itoa(len(c.stack)-start)})
				c.abandoned = true
				return
			}
			c.forceTop(1)
			v := c.stack[start]
			c.stack = c.stack[:start]
			if v.part != 0 {
				v.share()
			}
			v.fresh = c.freshish(v)
			fresh = fresh && v.fresh
			if !seen[ci] {
				cols[ci], seen[ci] = v, true
				continue
			}
			j, ok := c.joinSlot(cols[ci], v)
			if !ok {
				c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: cell.GetStartToken(),
					Hint: "the cells of column '" + g.Columns[ci].Name + "' have no common type: " + c.format(cols[ci].t) + " and " + c.format(v.t)})
				c.abandoned = true
				return
			}
			cols[ci] = j
		}
	}
	fields := make([]RecordField, len(names))
	for i, name := range names {
		t := cols[i].t
		if !seen[i] {
			t = c.subst.FreshVar(c.arena)
		}
		fields[i] = RecordField{Name: name, Status: FieldRequired, Type: t}
	}
	c.pushGrid(TKGrid, c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent}), fresh)
}

// metaDict checks a grid literal's metadata dict on its own stack.
func (c *coreChecker) metaDict(d *MShellParseDict) bool {
	start, outer := c.child([]MShellParseItem{d})
	c.floor = outer
	if c.diverged || c.abandoned {
		return false
	}
	c.stack = c.stack[:start]
	return true
}

// ---------------------------------------------------------------------------
// Words

// gridWord checks the grid words whose result a signature cannot say. It
// reports false for a word it does not handle, or one whose receiver is
// not a grid, for the table's other forms.
func (c *coreChecker) gridWord(tok Token) bool {
	switch tok.Lexeme {
	case "gridCol":
		return c.gridCol(tok)
	case "gridValues":
		return c.gridValues(tok)
	case "toDict":
		return c.gridToDict(tok)
	case "select", "exclude":
		return c.gridProject(tok)
	case "derive":
		return c.gridDerive(tok)
	case "updateCol":
		return c.gridUpdateCol(tok)
	case "gridSetCell":
		return c.gridSetCell(tok)
	case "gridAddCol", "gridRemoveCol", "gridRenameCol":
		return c.gridColumns(tok)
	case "map":
		return c.gridMap(tok)
	case "extend":
		return c.gridExtend(tok)
	case "join", "leftJoin", "outerJoin":
		return c.gridJoin(tok)
	case "pivot":
		return c.gridPivot(tok)
	case "groupBy":
		return c.gridGroupBy(tok)
	case "sortBy":
		c.sortColumns(tok)
		return false
	}
	return false
}

// gridArg checks that slot i holds a grid (or a view, when views is set)
// and returns its kind and schema. It reports the error itself.
func (c *coreChecker) gridArg(i int, tok Token, views bool) (TypeKind, TypeId, bool) {
	c.force(i)
	kind, rec, ok := c.gridOf(i)
	if ok && (kind == TKGrid || views && kind == TKGridView) {
		return kind, rec, true
	}
	t := c.subst.Apply(c.arena, c.stack[i].t)
	want := "a Grid"
	if views {
		want = "a Grid or GridView"
	}
	if c.arena.nodes[t].Kind == TKVar {
		c.gridError(tok, "'"+tok.Lexeme+"' needs "+want+", but the type of the value here is not known; annotate it")
	} else {
		c.gridError(tok, "'"+tok.Lexeme+"' needs "+want+", got "+c.format(t))
	}
	return 0, TidNothing, false
}

// gridCol checks `grid name gridCol`: a new list of the column's cells.
func (c *coreChecker) gridCol(tok Token) bool {
	if !c.need(2, tok) {
		return true
	}
	n := len(c.stack)
	_, rec, ok := c.gridArg(n-2, tok, true)
	if !ok || !c.keyArg(n-1, tok) {
		return true
	}
	t, ok := c.columnRead(rec, c.stack[n-1], tok)
	if !ok {
		return true
	}
	c.stack = c.stack[:n-2]
	c.push(c.arena.MakeList(t), c.rel.Immutable(c.subst.Apply(c.arena, t)))
	return true
}

// gridValues checks `grid gridValues`: a new list of new row lists, each
// cell read at the type of Get-Key.
func (c *coreChecker) gridValues(tok Token) bool {
	if !c.need(1, tok) {
		return true
	}
	n := len(c.stack)
	_, rec, ok := c.gridArg(n-1, tok, true)
	if !ok {
		return true
	}
	t, ok := c.keyRead(rec)
	if !ok {
		c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
			Hint: "the columns " + c.columns(rec) + " have no common type, so a row of them has none"})
		c.abandoned = true
		return true
	}
	c.stack = c.stack[:n-1]
	c.push(c.arena.MakeList(c.arena.MakeList(t)), c.rel.Immutable(c.subst.Apply(c.arena, t)))
	return true
}

// gridToDict checks `row toDict`: a new dict of the row's cells, whose
// type is the schema.
func (c *coreChecker) gridToDict(tok Token) bool {
	if !c.need(1, tok) {
		return true
	}
	n := len(c.stack)
	c.force(n - 1)
	kind, rec, ok := c.gridOf(n - 1)
	if !ok || kind != TKGridRow {
		c.gridError(tok, "'toDict' needs a GridRow, got "+c.format(c.stack[n-1].t))
		return true
	}
	c.stack = c.stack[:n-1]
	c.push(rec, c.schemaImmutable(rec))
	return true
}

// gridProject checks `grid [names] select` and `exclude`: a new grid of
// the named columns, in that order, or of the others. With a list of
// literal names the schema follows them; otherwise select gives the
// unknown schema, and exclude may have dropped any column.
func (c *coreChecker) gridProject(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	if kind, _, ok := c.gridOf(n - 2); !ok || kind == TKGridRow {
		return false
	}
	_, rec, _ := c.gridArg(n-2, tok, true)
	c.force(n - 1)
	if !c.check(c.stack[n-1], c.arena.MakeList(TidStr)) {
		c.mismatch(tok, 1, c.arena.MakeList(TidStr), c.stack[n-1].t)
		return true
	}
	names, lit := c.litNames(c.stack[n-1])
	var out TypeId
	switch {
	case tok.Lexeme == "select" && !lit:
		out = c.res.unknownSchema()
	case tok.Lexeme == "select":
		fields := make([]RecordField, 0, len(names))
		for i, name := range names {
			for _, prev := range names[:i] {
				if prev == name {
					c.gridError(tok, "'select' names the column '"+c.names.Name(name)+"' twice")
					return true
				}
			}
			t, status := c.labelRead(rec, name)
			if status == FieldAbsent {
				c.noColumn(tok, rec, name)
				return true
			}
			fields = append(fields, RecordField{Name: name, Status: FieldRequired, Type: t})
		}
		out = c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent})
	case !lit:
		out = c.withoutColumn(rec, NameNone)
	default:
		out = rec
		for _, name := range names {
			if _, status := c.labelRead(out, name); status == FieldAbsent {
				c.noColumn(tok, rec, name)
				return true
			}
			out = c.withoutColumn(out, name)
		}
	}
	c.stack = c.stack[:n-2]
	c.pushNewGrid(out)
	return true
}

// gridDerive checks `grid name meta (GridRow -- a) derive`: a new grid with
// one more column, of the quote's result type.
func (c *coreChecker) gridDerive(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 4 {
		return false
	}
	kind, rec, ok := c.gridOf(n - 4)
	if !ok || kind == TKGridRow {
		return false
	}
	name := c.stack[n-3].key()
	if name != NameNone {
		if f := c.schemaOf(rec).FieldAt(name); f.Status == FieldRequired {
			c.gridError(tok, "the grid already has a column '"+c.names.Name(name)+"'")
			return true
		}
	}
	a := c.subst.FreshVar(c.arena)
	sig := coreSig{
		ins: []TypeId{c.stack[n-4].t, c.keyType(), c.res.unknownSchema(),
			c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{c.arena.MakeGridOf(TKGridRow, rec)}, Outputs: []TypeId{a}})},
		outs: []TypeId{TidUnknown}, child: true,
	}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	c.stack = c.stack[:len(c.stack)-1]
	c.pushNewGrid(c.withColumn(rec, name, c.subst.Apply(c.arena, a)))
	return true
}

// keyType is the type of a dict key or a column name: str or path.
func (c *coreChecker) keyType() TypeId {
	return c.arena.MakeUnion([]TypeId{TidStr, TidPath})
}

// gridUpdateCol checks `grid name (T -- u) updateCol`, where T is the
// column's type. On a Grid the column is rewritten in place: at its own
// type on any grid, at a new type only on a new one (P6). On a GridView
// the result is a new grid of the view's rows.
func (c *coreChecker) gridUpdateCol(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 3 {
		return false
	}
	kind, rec, ok := c.gridOf(n - 3)
	if !ok || kind == TKGridRow {
		return false
	}
	recv, k := c.stack[n-3], c.stack[n-2]
	if !c.keyArg(n-2, tok) {
		return true
	}
	name := k.key()
	in, ok := c.columnRead(rec, k, tok)
	if !ok {
		return true
	}
	u := c.subst.FreshVar(c.arena)
	sig := coreSig{
		ins:  []TypeId{recv.t, c.keyType(), c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{in}, Outputs: []TypeId{u}})},
		outs: []TypeId{TidUnknown}, child: true,
	}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	c.stack = c.stack[:len(c.stack)-1]
	c.deferNotContainer(tok, u)
	cell := coreSlot{t: u}
	// The new cells are the quote's results: shared values.
	fresh := recv.fresh && c.rel.Immutable(c.subst.Apply(c.arena, u))
	switch {
	case kind == TKGridView:
		c.pushNewGrid(c.replaceColumn(rec, name, u))
	case !recv.fresh:
		if !c.writeAt(rec, name, cell) {
			c.gridError(tok, "'updateCol' gives column "+c.columnLabel(name)+" the type "+c.format(u)+
				" in place, but the grid's type says "+c.format(in)+"; the grid is stored, so make a new one first with deepCopy")
			return true
		}
		c.pushGrid(TKGrid, rec, false)
	case !c.hasVars(u) && !c.hasVars(in) && c.rel.Sub(c.subst.Apply(c.arena, u), c.subst.Apply(c.arena, in)) &&
		c.writeAt(rec, name, cell):
		c.pushGrid(TKGrid, rec, fresh)
	default:
		c.pushGrid(TKGrid, c.replaceColumn(rec, name, u), fresh)
	}
	return true
}

// replaceColumn is rec with the cells of column name replaced by values of
// type t. With a name known only at run time it says nothing: any column
// may be the one replaced.
func (c *coreChecker) replaceColumn(rec TypeId, name NameId, t TypeId) TypeId {
	if name == NameNone {
		return c.res.unknownSchema()
	}
	return c.withColumn(rec, name, c.subst.Apply(c.arena, t))
}

// columnLabel names a column in a message.
func (c *coreChecker) columnLabel(name NameId) string {
	if name == NameNone {
		return "(named at run time)"
	}
	return "'" + c.names.Name(name) + "'"
}

// gridSetCell checks `grid name row value gridSetCell` on a Grid: the
// value is written at the column's own type. A value of another type is
// an error even on a new grid: the runtime keeps typed column storage and
// drops a value of another kind.
func (c *coreChecker) gridSetCell(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 4 {
		return false
	}
	if _, _, ok := c.gridOf(n - 4); !ok {
		return false
	}
	_, rec, ok := c.gridArg(n-4, tok, false)
	if !ok || !c.keyArg(n-3, tok) {
		return true
	}
	c.force(n - 2)
	if !c.check(c.stack[n-2], TidInt) {
		c.mismatch(tok, 2, TidInt, c.stack[n-2].t)
		return true
	}
	c.force(n - 1)
	recv, name, v := c.stack[n-4], c.stack[n-3].key(), c.stack[n-1]
	if name != NameNone {
		if _, status := c.labelRead(rec, name); status == FieldAbsent {
			c.noColumn(tok, rec, name)
			return true
		}
	}
	if !c.writeAt(rec, name, v) {
		c.gridError(tok, "'gridSetCell' writes a cell at its column's type: "+c.format(v.t)+
			" does not fit column "+c.columnLabel(name)+"; the grid's columns are "+c.columns(rec))
		return true
	}
	c.stack = c.stack[:n-4]
	c.pushGrid(TKGrid, rec, recv.fresh && c.freshish(v))
	return true
}

// gridColumns checks gridAddCol, gridRemoveCol and gridRenameCol: they
// change a Grid's columns in place, so the grid must be new.
func (c *coreChecker) gridColumns(tok Token) bool {
	nargs := map[string]int{"gridAddCol": 3, "gridRemoveCol": 2, "gridRenameCol": 3}[tok.Lexeme]
	n := len(c.stack)
	if n-c.floor < nargs {
		return false
	}
	base := n - nargs
	if _, _, ok := c.gridOf(base); !ok {
		return false
	}
	_, rec, ok := c.gridArg(base, tok, false)
	if !ok || !c.keyArg(base+1, tok) {
		return true
	}
	recv := c.stack[base]
	if !recv.fresh {
		c.gridError(tok, newSchemaHint(tok.Lexeme))
		return true
	}
	name := c.stack[base+1].key()
	var out TypeId
	fresh := true
	switch tok.Lexeme {
	case "gridAddCol":
		// A list gives one value per row; anything else is repeated.
		c.force(base + 2)
		v := c.stack[base+2]
		t, ok := c.addedColumn(v, tok)
		if !ok {
			return true
		}
		if name != NameNone && c.schemaOf(rec).FieldAt(name).Status == FieldRequired {
			c.gridError(tok, "the grid already has a column '"+c.names.Name(name)+"'")
			return true
		}
		out = c.withColumn(rec, name, t)
		fresh = c.freshish(v)
	case "gridRemoveCol":
		if name != NameNone {
			if _, status := c.labelRead(rec, name); status == FieldAbsent {
				c.noColumn(tok, rec, name)
				return true
			}
		}
		out = c.withoutColumn(rec, name)
	case "gridRenameCol":
		if !c.keyArg(base+2, tok) {
			return true
		}
		to := c.stack[base+2].key()
		if name == NameNone || to == NameNone {
			out = c.res.unknownSchema()
			break
		}
		t, status := c.labelRead(rec, name)
		if status == FieldAbsent {
			c.noColumn(tok, rec, name)
			return true
		}
		if c.schemaOf(rec).FieldAt(to).Status == FieldRequired {
			c.gridError(tok, "the grid already has a column '"+c.names.Name(to)+"'")
			return true
		}
		out = c.withColumn(c.withoutColumn(rec, name), to, t)
	}
	c.stack = c.stack[:base]
	c.pushGrid(TKGrid, out, fresh)
	return true
}

// addedColumn is the type of a column gridAddCol makes from the value in
// slot v: a list's element type, or the value's own type, joined over the
// members of a union.
func (c *coreChecker) addedColumn(v coreSlot, tok Token) (TypeId, bool) {
	t := c.subst.Apply(c.arena, v.t)
	if c.arena.nodes[t].Kind == TKVar {
		c.gridError(tok, "'gridAddCol' needs to know whether its value is a list, but its type is not known here; annotate it")
		return TidNothing, false
	}
	// The runtime makes each element of a list a cell, and any other value
	// one cell, so the type must say which (a def's generic does not).
	var ms []TypeId
	if !c.members(t, &ms) || slices.ContainsFunc(ms, func(m TypeId) bool { return c.arena.nodes[m].Kind == TKVar }) {
		c.gridError(tok, "'gridAddCol' needs to know whether its value is a list, but its type "+c.format(t)+" does not say; annotate it")
		return TidNothing, false
	}
	acc := coreSlot{t: TidBottom}
	for _, m := range ms {
		u := c.unfold(m)
		switch c.arena.nodes[u].Kind {
		case TKList:
			m = TypeId(c.arena.nodes[u].A)
		case TKCommand:
			// A command is a list of its arguments at run time; a pipe is
			// one value.
			if !c.isPipe(u) {
				argv, _, _, _ := c.commandParts(u)
				m = c.listElem(argv)
			}
		}
		j, ok := c.joinSlot(acc, coreSlot{t: m})
		if !ok {
			c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
				Hint: "the values given to 'gridAddCol' have no common type: " + c.format(t)})
			c.abandoned = true
			return TidNothing, false
		}
		acc = j
	}
	return acc.t, true
}

// gridMap checks `grid (GridRow -- dict | GridRow) map`: a new grid whose
// columns are the keys of the quote's results. The schema is known when
// the quote gives a dict with exactly its required keys, or a GridRow;
// otherwise it is the unknown schema.
func (c *coreChecker) gridMap(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	kind, rec, ok := c.gridOf(n - 2)
	if !ok || kind == TKGridRow {
		return false
	}
	b := c.subst.FreshVar(c.arena)
	sig := coreSig{
		ins:  []TypeId{c.stack[n-2].t, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{c.arena.MakeGridOf(TKGridRow, rec)}, Outputs: []TypeId{b}})},
		outs: []TypeId{TidUnknown}, child: true,
	}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	c.stack = c.stack[:len(c.stack)-1]
	open := c.res.unknownSchema()
	want := c.arena.MakeUnion([]TypeId{open, c.arena.MakeGridOf(TKGridRow, open)})
	if c.hasVars(b) {
		c.deferCheck(tok, coreSlot{t: b}, want)
	} else if !c.rel.Sub(c.subst.Apply(c.arena, b), want) {
		c.gridError(tok, "'map' on a grid wants its quote to give a dict or a GridRow for each row, got "+c.format(b))
		return true
	}
	out := open
	bt := c.unfold(c.subst.Apply(c.arena, b))
	switch bn := c.arena.nodes[bt]; bn.Kind {
	case TKRecord:
		if c.exactSchema(bt) {
			out = bt
			for _, f := range c.schemaOf(bt).Fields {
				if f.Status != FieldRequired {
					out = open
				}
			}
		}
	case TKGridRow:
		if _, r, ok := c.gridType(bt); ok {
			out = r
		}
	}
	c.pushNewGrid(out)
	return true
}

// gridConcat checks `+` on two grids or views: a new grid of the rows of
// both. The runtime matches columns by name, so both schemas must name the
// same columns; each column's type is the join of the two.
func (c *coreChecker) gridConcat(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	k1, r1, ok1 := c.gridOf(n - 2)
	k2, r2, ok2 := c.gridOf(n - 1)
	if !ok1 || !ok2 || k1 == TKGridRow || k2 == TKGridRow {
		return false
	}
	out, ok := c.sameColumns(r1, r2, tok, func(f, g RecordField) (TypeId, bool) {
		j, ok := c.joinSlot(coreSlot{t: f.Type}, coreSlot{t: g.Type})
		return j.t, ok
	})
	if !ok {
		return true
	}
	c.stack = c.stack[:n-2]
	c.pushNewGrid(out)
	return true
}

// sameColumns combines two schemas that must name the same columns, column
// by column. When either does not name all its columns, the result is the
// unknown schema.
func (c *coreChecker) sameColumns(r1, r2 TypeId, tok Token, col func(f, g RecordField) (TypeId, bool)) (TypeId, bool) {
	x, y := c.schemaOf(r1), c.schemaOf(r2)
	if !c.exactSchema(r1) || !c.exactSchema(r2) {
		return c.res.unknownSchema(), true
	}
	same := len(x.Fields) == len(y.Fields)
	for _, f := range x.Fields {
		if y.FieldAt(f.Name).Status == FieldAbsent {
			same = false
		}
	}
	if !same {
		c.gridError(tok, "'"+tok.Lexeme+"' needs grids with the same columns, got "+c.columns(r1)+" and "+c.columns(r2))
		return TidNothing, false
	}
	fields := make([]RecordField, len(x.Fields))
	for i, f := range x.Fields {
		g := y.FieldAt(f.Name)
		t, ok := col(f, g)
		if !ok {
			c.errs = append(c.errs, TypeError{Kind: TErrNoJoin, Pos: tok,
				Hint: "column '" + c.names.Name(f.Name) + "' is " + c.format(f.Type) + " in one grid and " + c.format(g.Type) + " in the other, which have no common type"})
			c.abandoned = true
			return TidNothing, false
		}
		status := FieldRequired
		if f.Status != FieldRequired || g.Status != FieldRequired {
			status = FieldOptional
		}
		fields[i] = RecordField{Name: f.Name, Status: status, Type: t}
	}
	return c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent}), true
}

// gridExtend checks `receiver source extend`: the source's rows are added
// to the receiver in place (to the grid under it, for a view). A column
// whose source cells fit the receiver's type is a plain write, allowed on
// any receiver; one that would widen the receiver's column is a type
// change, allowed only on a new Grid.
func (c *coreChecker) gridExtend(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	kind, r1, ok := c.gridOf(n - 2)
	if !ok || kind == TKGridRow {
		return false
	}
	_, r2, ok := c.gridArg(n-1, tok, true)
	if !ok {
		return true
	}
	recv, src := c.stack[n-2], c.stack[n-1]
	newRecv := recv.fresh && kind == TKGrid
	changed := false
	out, ok := c.sameColumns(r1, r2, tok, func(f, g RecordField) (TypeId, bool) {
		if f.Status != FieldOpen && g.Status != FieldOpen && c.check(coreSlot{t: g.Type}, f.Type) {
			return f.Type, true
		}
		changed = true
		j, ok := c.joinSlot(coreSlot{t: f.Type}, coreSlot{t: g.Type})
		return j.t, ok
	})
	if !ok {
		return true
	}
	if !c.exactSchema(r1) || !c.exactSchema(r2) {
		// The rows are written into columns the type does not name.
		changed, out = true, c.res.unknownSchema()
	}
	if changed && !newRecv {
		c.gridError(tok, "'extend' adds rows whose cells do not fit the receiver's columns ("+c.columns(r2)+
			" into "+c.columns(r1)+"), which changes their type in place; the receiver is stored, so make a new one first with deepCopy,"+
			" or give it the wider column type where it is made")
		return true
	}
	if !changed {
		out = r1
	}
	c.stack = c.stack[:n-2]
	c.pushGrid(kind, out, recv.fresh && (src.fresh || c.schemaImmutable(r2)))
	return true
}

// gridJoin checks the grid forms of join, leftJoin and outerJoin:
// `left right (GridRow -- a) (GridRow -- b) join`. The result has the left
// grid's columns, then the right grid's; a side whose rows may be missing
// from a result row has none in those cells.
func (c *coreChecker) gridJoin(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 4 || !c.isQuoteSlot(c.stack[n-1]) {
		return false
	}
	k1, r1, ok1 := c.gridOf(n - 4)
	k2, r2, ok2 := c.gridOf(n - 3)
	if !ok1 || !ok2 || k1 == TKGridRow || k2 == TKGridRow {
		return false
	}
	a, b := c.subst.FreshVar(c.arena), c.subst.FreshVar(c.arena)
	row := func(r TypeId) TypeId { return c.arena.MakeGridOf(TKGridRow, r) }
	sig := coreSig{
		ins: []TypeId{c.stack[n-4].t, c.stack[n-3].t,
			c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{row(r1)}, Outputs: []TypeId{a}}),
			c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{row(r2)}, Outputs: []TypeId{b}})},
		outs: []TypeId{TidUnknown}, child: true,
	}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	c.stack = c.stack[:len(c.stack)-1]
	c.deferJoinKey(tok, a)
	c.deferJoinKey(tok, b)
	if !c.exactSchema(r1) || !c.exactSchema(r2) {
		c.pushNewGrid(c.res.unknownSchema())
		return true
	}
	none := c.arena.MakeMaybeEnum(TidBottom)
	orNone := func(f RecordField) RecordField {
		t := c.subst.Apply(c.arena, f.Type)
		if c.hasVars(t) {
			f.Type = TidUnknown
			return f
		}
		if j, ok := c.joinSlot(coreSlot{t: t}, coreSlot{t: none}); ok {
			f.Type = j.t
		} else {
			f.Type = TidUnknown
		}
		return f
	}
	x, y := c.schemaOf(r1), c.schemaOf(r2)
	fields := make([]RecordField, 0, len(x.Fields)+len(y.Fields))
	for _, f := range x.Fields {
		if tok.Lexeme == "outerJoin" {
			f = orNone(f)
		}
		fields = append(fields, f)
	}
	for _, g := range y.Fields {
		if x.FieldAt(g.Name).Status != FieldAbsent {
			c.gridError(tok, "both grids have a column '"+c.names.Name(g.Name)+"'; rename or drop one first")
			return true
		}
		if tok.Lexeme != "join" {
			g = orNone(g)
		}
		fields = append(fields, g)
	}
	c.pushNewGrid(c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent}))
	return true
}

// gridPivot checks `grid [rowKeys] colKey (GridView -- a) pivot`. The
// result has the row-key columns, then one column per distinct value of
// the colKey column, named by the data, of type a or none.
func (c *coreChecker) gridPivot(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 4 {
		return false
	}
	kind, rec, ok := c.gridOf(n - 4)
	if !ok || kind == TKGridRow {
		return false
	}
	keys, col := c.stack[n-3], c.stack[n-2]
	a := c.subst.FreshVar(c.arena)
	sig := coreSig{
		ins: []TypeId{c.stack[n-4].t, c.arena.MakeList(TidStr), c.keyType(),
			c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{c.arena.MakeGridOf(TKGridView, rec)}, Outputs: []TypeId{a}})},
		outs: []TypeId{TidUnknown}, child: true,
	}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	c.stack = c.stack[:len(c.stack)-1]
	c.deferNotContainer(tok, a)
	// The colKey column must hold strings: they name the new columns.
	var colT TypeId
	if name := col.key(); name != NameNone {
		if colT, ok = c.columnRead(rec, col, tok); !ok {
			return true
		}
	} else if colT, ok = c.anyColumn(rec, tok); !ok {
		return true
	}
	c.deferCheck(tok, coreSlot{t: colT}, TidStr)
	if !c.deferKeyColumns(tok, rec, keys) {
		return true
	}
	names, lit := c.litNames(keys)
	at := c.subst.Apply(c.arena, a)
	cell, ok := c.joinSlot(coreSlot{t: at}, coreSlot{t: c.arena.MakeMaybeEnum(TidBottom)})
	if !lit || !ok || c.hasVars(at) {
		c.pushNewGrid(c.res.unknownSchema())
		return true
	}
	fields := make([]RecordField, 0, len(names))
	for i, name := range names {
		if slices.Contains(names[:i], name) {
			c.gridError(tok, "'pivot' names the row key column '"+c.names.Name(name)+"' twice")
			return true
		}
		t, _ := c.labelRead(rec, name)
		fields = append(fields, RecordField{Name: name, Status: FieldRequired, Type: t})
	}
	c.pushNewGrid(c.arena.MakeRecord(fields, RecordField{Status: FieldOptional, Type: cell.t}))
	return true
}

// groupBySpecs checks `grid [keys] [specs] groupBy` with the spec list
// written at the call: each spec is a dict literal with an `agg` quote,
// and `name` and `meta`. Each agg quote is checked against
// `(GridView -- t)` with its own t, as a quote literal given to a word is
// checked against the word's parameter, so the specs may give different
// column types. It reports false, before checking anything, when the
// stack or the list does not have that form.
func (c *coreChecker) groupBySpecs(l *MShellParseList, tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 2 {
		return false
	}
	kind, rec, ok := c.gridOf(n - 2)
	if !ok || kind == TKGridRow {
		return false
	}
	specs := make([]*MShellParseDict, 0, len(l.Items))
	for _, it := range l.Items {
		d, ok := it.(*MShellParseDict)
		if !ok {
			return false
		}
		agg := false
		for _, kv := range d.Items {
			switch kv.Key {
			case "agg":
				if len(kv.Value) != 1 {
					return false
				}
				if _, ok := kv.Value[0].(*MShellParseQuote); !ok {
					return false
				}
				agg = true
			case "name", "meta":
			default:
				return false
			}
		}
		if !agg {
			return false
		}
		specs = append(specs, d)
	}
	c.at = tok
	c.force(n - 1)
	keys := c.stack[n-1]
	if !c.check(keys, c.arena.MakeList(TidStr)) {
		c.mismatch(tok, 1, c.arena.MakeList(TidStr), keys.t)
		return true
	}
	view := c.arena.MakeGridOf(TKGridView, rec)
	known := true
	aggs := make([]RecordField, 0, len(specs))
	for i, d := range specs {
		col := RecordField{Name: c.names.Intern("AggCol" + strconv.Itoa(i+1)), Status: FieldRequired}
		for _, kv := range d.Items {
			switch kv.Key {
			case "agg":
				q := kv.Value[0].(*MShellParseQuote)
				col.Type = c.subst.FreshVar(c.arena)
				c.pushQuote(q.Items, q.StartToken)
				top := len(c.stack) - 1
				c.checkPending(c.stack[top].pq, c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{view}, Outputs: []TypeId{col.Type}}), true, false, n-2, tok)
				c.stack = c.stack[:top]
				c.deferNotContainer(tok, col.Type)
			case "name", "meta":
				start, outer := c.child(kv.Value)
				c.floor = outer
				if c.diverged || c.abandoned {
					return true
				}
				if len(c.stack)-start != 1 {
					c.errs = append(c.errs, TypeError{Kind: TErrChildStack, Pos: d.StartToken,
						Hint: "the value for key '" + kv.Key + "' must leave exactly one value, but leaves " + strconv.Itoa(len(c.stack)-start)})
					c.abandoned = true
					return true
				}
				c.forceTop(1)
				v := c.stack[start]
				c.stack = c.stack[:start]
				if kv.Key == "meta" {
					if !c.check(v, c.res.unknownSchema()) {
						c.mismatch(tok, 2, c.res.unknownSchema(), v.t)
					}
				} else if !c.check(v, TidStr) {
					c.mismatch(tok, 2, TidStr, v.t)
				} else if name := v.key(); name != NameNone {
					col.Name = name
				} else {
					known = false
				}
			}
			if c.abandoned {
				return true
			}
		}
		aggs = append(aggs, col)
	}
	if !c.deferKeyColumns(tok, rec, keys) {
		return true
	}
	names, lit := c.litNames(keys)
	c.stack = c.stack[:n-2]
	if !lit || !known {
		c.pushNewGrid(c.res.unknownSchema())
		return true
	}
	fields := make([]RecordField, 0, len(names)+len(aggs))
	for _, name := range names {
		t, _ := c.labelRead(rec, name)
		fields = append(fields, RecordField{Name: name, Status: FieldRequired, Type: t})
	}
	fields = append(fields, aggs...)
	for i, f := range fields {
		for _, g := range fields[:i] {
			if f.Name == g.Name {
				c.gridError(tok, "'groupBy' makes two columns named '"+c.names.Name(f.Name)+"'")
				return true
			}
		}
	}
	c.pushNewGrid(c.arena.MakeRecord(fields, RecordField{Status: FieldAbsent}))
	return true
}

// gridGroupBy checks a grid groupBy whose spec list is not written at the
// call (groupBySpecs takes that case): every agg quote gives one type a,
// which the runtime refuses when it is a container, and the result's
// columns are not known. A spec is exact: the runtime refuses a key other
// than agg, name and meta.
func (c *coreChecker) gridGroupBy(tok Token) bool {
	n := len(c.stack)
	if n-c.floor < 3 {
		return false
	}
	kind, rec, ok := c.gridOf(n - 3)
	if !ok || kind == TKGridRow {
		return false
	}
	keys := c.stack[n-2]
	a := c.subst.FreshVar(c.arena)
	spec := c.arena.MakeRecord([]RecordField{
		{Name: c.names.Intern("agg"), Status: FieldRequired,
			Type: c.arena.MakeQuote(QuoteSig{Inputs: []TypeId{c.arena.MakeGridOf(TKGridView, rec)}, Outputs: []TypeId{a}})},
		{Name: c.names.Intern("name"), Status: FieldOptional, Type: TidStr},
		{Name: c.names.Intern("meta"), Status: FieldOptional, Type: c.res.unknownSchema()},
	}, RecordField{Status: FieldAbsent})
	sig := coreSig{ins: []TypeId{c.stack[n-3].t, c.arena.MakeList(TidStr), c.arena.MakeList(spec)}, outs: []TypeId{TidUnknown}}
	c.apply(&sig, tok)
	if c.abandoned || c.diverged {
		return true
	}
	c.stack = c.stack[:len(c.stack)-1]
	c.deferNotContainer(tok, a)
	if c.deferKeyColumns(tok, rec, keys) {
		c.pushNewGrid(c.res.unknownSchema())
	}
	return true
}

// sortColumns records that the columns sortBy sorts by must be sortable:
// the columns named, or every column when a name is known only at run
// time. The table checks the rest of sortBy.
func (c *coreChecker) sortColumns(tok Token) {
	n := len(c.stack)
	if n-c.floor < 2 {
		return
	}
	kind, rec, ok := c.gridOf(n - 2)
	if !ok || kind == TKGridRow {
		return
	}
	names, lit := c.litNames(c.stack[n-1])
	if name := c.stack[n-1].key(); name != NameNone {
		names, lit = []NameId{name}, true
	}
	if !lit {
		if t, ok := c.keyRead(rec); ok {
			c.deferKey(tok, t, ruleSortKey)
		} else {
			c.deferKey(tok, TidUnknown, ruleSortKey)
		}
		return
	}
	for _, name := range names {
		if t, status := c.labelRead(rec, name); status != FieldAbsent {
			c.deferKey(tok, t, ruleSortKey)
		}
	}
}

// deferKeyColumns checks the key columns a groupBy or pivot groups by: the
// columns named, when keys is a list of literal names, and otherwise every
// column of rec, since a name known only at run time may be any of them.
// It reports false after an error.
func (c *coreChecker) deferKeyColumns(tok Token, rec TypeId, keys coreSlot) bool {
	if names, lit := c.litNames(keys); lit {
		for _, name := range names {
			t, status := c.labelRead(rec, name)
			if status == FieldAbsent {
				c.noColumn(tok, rec, name)
				return false
			}
			c.deferKey(tok, t, ruleGroupKey)
		}
		return true
	}
	t, ok := c.anyColumn(rec, tok)
	if ok {
		c.deferKey(tok, t, ruleGroupKey)
	}
	return ok
}

// anyColumn is the type of a column of rec named only at run time: the
// join of every column's type (Get-Key), unknown when the schema is not
// known.
func (c *coreChecker) anyColumn(rec TypeId, tok Token) (TypeId, bool) {
	t, ok := c.keyRead(rec)
	if !ok {
		c.gridError(tok, "the columns of "+c.format(rec)+" have no common type, so a column named only at run time has none; use a literal list of names")
	}
	return t, ok
}

// ---------------------------------------------------------------------------
// Checks made when the unit is solved

// keyRule says which values the runtime takes at a position where it
// refuses containers (lists, dicts, grids, views and rows).
type keyRule uint8

const (
	// ruleResult: the result of an updateCol, pivot or groupBy quote,
	// checked at the top only (isContainerType).
	ruleResult keyRule = iota + 1
	// ruleGroupKey: a groupBy or pivot key cell, checked inside a Maybe too
	// (appendGridKeyPart).
	ruleGroupKey
	// ruleJoinKey: a join key; a Maybe is looked through, and a list of
	// values that are not lists is a compound key (appendJoinKey).
	ruleJoinKey
	// ruleJoinKeyItem: an element of a compound join key.
	ruleJoinKeyItem
	// ruleSortKey: a sortBy column, which the runtime orders only within
	// one kind of int, float, str, datetime or bool, with none last and a
	// Maybe looked through once (compareGridGenericCells).
	ruleSortKey
	// ruleCommandArg: an argument of a command that runs, whose type had
	// unsolved variables when it ran (commandLineable).
	ruleCommandArg
)

// deferKey records that a value of type t must be one the runtime takes
// under rule, checked when the unit is solved.
func (c *coreChecker) deferKey(tok Token, t TypeId, rule keyRule) {
	c.deferred = append(c.deferred, coreDeferred{tok: tok, t: t, rule: rule})
}

func (c *coreChecker) deferNotContainer(tok Token, t TypeId) { c.deferKey(tok, t, ruleResult) }
func (c *coreChecker) deferJoinKey(tok Token, t TypeId)      { c.deferKey(tok, t, ruleJoinKey) }

// keyAllowed reports whether every value of type t, as solved, is one the
// runtime takes under rule. A type not known is refused: it may be a
// container.
func (c *coreChecker) keyAllowed(t TypeId, rule keyRule) bool {
	if rule == ruleCommandArg {
		return c.commandLineable(t)
	}
	t = c.unfold(c.subst.Apply(c.arena, t))
	if rule == ruleSortKey {
		_, ok := c.sortKind(t, TidBottom, true)
		return ok
	}
	switch t {
	case TidUnknown, TidNothing:
		return false
	}
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKUnion:
		for _, m := range c.arena.unionMembers[n.Extra] {
			if !c.keyAllowed(m, rule) {
				return false
			}
		}
		return true
	case TKList:
		return rule == ruleJoinKey && c.keyAllowed(TypeId(n.A), ruleJoinKeyItem)
	case TKEnum:
		if n.A != EnumMaybe || rule == ruleResult {
			return true
		}
		inner := c.arena.enumArgs[n.Extra][0]
		if rule == ruleJoinKeyItem {
			// The item's Maybe is looked through like a whole key's.
			return c.keyAllowed(inner, ruleJoinKey)
		}
		return c.keyAllowed(inner, rule)
	case TKRecord, TKGrid, TKGridView, TKGridRow, TKCommand, TKVar, TKAbstract, TKRigid, TKAlias:
		return false
	}
	return true
}

// sortKind is the one kind sortBy compares the values of t at, given the
// kind seen so far (⊥ for none yet): one of int, float, str, datetime and
// bool, with a Maybe looked through once (maybe) and none, which sorts
// last, fitting any.
func (c *coreChecker) sortKind(t, seen TypeId, maybe bool) (TypeId, bool) {
	t = c.unfold(c.subst.Apply(c.arena, t))
	switch t {
	case TidBottom:
		return seen, true
	case TidInt, TidFloat, TidStr, TidDateTime, TidBool:
		return t, seen == TidBottom || seen == t
	}
	n := c.arena.nodes[t]
	switch {
	case n.Kind == TKEnum && n.A == EnumMaybe && maybe:
		return c.sortKind(c.arena.enumArgs[n.Extra][0], seen, false)
	case n.Kind == TKUnion:
		for _, m := range c.arena.unionMembers[n.Extra] {
			var ok bool
			if seen, ok = c.sortKind(m, seen, maybe); !ok {
				return seen, false
			}
		}
		return seen, true
	}
	return seen, false
}

// keyRuleText describes a rule in an error.
func keyRuleText(rule keyRule) string {
	switch rule {
	case ruleResult:
		return "a value that is not a list, dict or grid"
	case ruleGroupKey:
		return "a grouping key: not a list, dict or grid, also inside a Maybe"
	case ruleSortKey:
		return "a column it can sort: one of int, float, str, datetime or bool, or a Maybe of one"
	case ruleCommandArg:
		return "command arguments that are strings, paths, numbers or dates, or lists of them"
	}
	return "a join key: a value that is not a dict or grid, or a list of such values that are not lists"
}
