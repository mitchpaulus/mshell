package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// deepCopy (ai/type-core-calculus.typ, "Explicit copies: deepCopy") gives
// every list, dict and grid reachable from a value a new object, once per
// path, so the copy is a tree that nothing else references. A value reached
// along two paths is copied twice. Immutable values and quotes are shared.
//
// A cycle has no finite copy, so it is an error that names the cycle. Every
// cycle passes through a list, dict or grid, since only those can be changed
// to point back at something that contains them, so only those are tracked
// on the path.

// copyPathShortMax is the path length up to which the path is searched
// linearly; past it, the path's containers are also kept in a map.
const copyPathShortMax = 32

// deepCopier holds the path from the copied value to the object being copied.
type deepCopier struct {
	// path holds the lists, dicts and grids being copied, outermost first.
	path []copyPathEntry
	// onPath maps each container on the path to its index in path. Nil
	// until the path is longer than copyPathShortMax.
	onPath map[any]int
	// edges is how each object on the way down was reached from its parent,
	// for the error message.
	edges []copyEdge
}

type copyPathEntry struct {
	obj   any // *MShellList, *MShellDict or *MShellGrid
	edges int // len(edges) when obj was entered
}

type copyEdgeKind uint8

const (
	copyEdgeIndex copyEdgeKind = iota
	copyEdgeKey
	copyEdgeJust
	copyEdgeCommand
	copyEdgeCell
	copyEdgeGridMeta
	copyEdgeColumnMeta
)

type copyEdge struct {
	kind  copyEdgeKind
	index int
	key   string
}

// DeepCopy returns a copy of v that shares no list, dict or grid with v.
func DeepCopy(v MShellObject) (MShellObject, error) {
	var c deepCopier
	return c.copy(v)
}

// holdsObjects reports whether copy would give v, or something inside it, a
// new object. Everything else is immutable, or a quote, and is shared.
func holdsObjects(v MShellObject) bool {
	switch o := v.(type) {
	case *MShellList, *MShellDict, *MShellPipe, *MShellGrid, *MShellGridView, *MShellGridRow:
		return true
	case *Maybe:
		return o.obj != nil
	case Maybe:
		return o.obj != nil
	}
	return false
}

func (c *deepCopier) copy(v MShellObject) (MShellObject, error) {
	switch o := v.(type) {
	case *MShellList:
		if err := c.enter(o); err != nil {
			return nil, err
		}
		nl := *o
		nl.Items = slices.Clone(o.Items)
		for i, item := range nl.Items {
			if !holdsObjects(item) {
				continue
			}
			copied, err := c.child(copyEdge{kind: copyEdgeIndex, index: i}, item)
			if err != nil {
				return nil, err
			}
			nl.Items[i] = copied
		}
		c.leave()
		return &nl, nil
	case *MShellDict:
		return c.copyDict(o)
	case *Maybe:
		if o.obj == nil {
			return o, nil
		}
		inner, err := c.child(copyEdge{kind: copyEdgeJust}, o.obj)
		if err != nil {
			return nil, err
		}
		return &Maybe{obj: inner}, nil
	case Maybe:
		if o.obj == nil {
			return o, nil
		}
		inner, err := c.child(copyEdge{kind: copyEdgeJust}, o.obj)
		if err != nil {
			return nil, err
		}
		return Maybe{obj: inner}, nil
	case *MShellPipe:
		np := &MShellPipe{List: o.List, StdoutBehavior: o.StdoutBehavior, StderrBehavior: o.StderrBehavior}
		np.List.Items = slices.Clone(o.List.Items)
		for i, item := range np.List.Items {
			if !holdsObjects(item) {
				continue
			}
			copied, err := c.child(copyEdge{kind: copyEdgeCommand, index: i}, item)
			if err != nil {
				return nil, err
			}
			np.List.Items[i] = copied
		}
		return np, nil
	case *MShellGrid:
		return c.copyGrid(o, nil)
	case *MShellGridView:
		// The selected rows become a new grid, and the copy is a view of
		// all of it.
		g, err := c.copyGrid(o.Source, o.Indices)
		if err != nil {
			return nil, err
		}
		indices := make([]int, len(o.Indices))
		for i := range indices {
			indices[i] = i
		}
		return &MShellGridView{Source: g, Indices: indices}, nil
	case *MShellGridRow:
		g, err := c.copyGrid(o.Grid, []int{o.RowIndex})
		if err != nil {
			return nil, err
		}
		return &MShellGridRow{Grid: g, RowIndex: 0}, nil
	}
	// Immutable values and quotes. A quote's captured scope is shared: it
	// is by reference, and freshness stops at quotes.
	return v, nil
}

// child copies v, reached from the object being copied along e.
func (c *deepCopier) child(e copyEdge, v MShellObject) (MShellObject, error) {
	c.edges = append(c.edges, e)
	copied, err := c.copy(v)
	c.edges = c.edges[:len(c.edges)-1]
	return copied, err
}

func (c *deepCopier) copyDict(o *MShellDict) (*MShellDict, error) {
	if err := c.enter(o); err != nil {
		return nil, err
	}
	nd := &MShellDict{Items: make(map[string]MShellObject, len(o.Items))}
	for k, item := range o.Items {
		if !holdsObjects(item) {
			nd.Items[k] = item
			continue
		}
		copied, err := c.child(copyEdge{kind: copyEdgeKey, key: k}, item)
		if err != nil {
			return nil, err
		}
		nd.Items[k] = copied
	}
	c.leave()
	return nd, nil
}

// copyGrid copies g, or only the given rows of it when rows is not nil.
func (c *deepCopier) copyGrid(g *MShellGrid, rows []int) (*MShellGrid, error) {
	if err := c.enter(g); err != nil {
		return nil, err
	}
	n := g.RowCount
	if rows != nil {
		n = len(rows)
	}
	ng := &MShellGrid{
		Columns:  make([]*GridColumn, len(g.Columns)),
		ColIndex: maps.Clone(g.ColIndex),
		RowCount: n,
	}
	if g.Meta != nil {
		c.edges = append(c.edges, copyEdge{kind: copyEdgeGridMeta})
		meta, err := c.copyDict(g.Meta)
		c.edges = c.edges[:len(c.edges)-1]
		if err != nil {
			return nil, err
		}
		ng.Meta = meta
	}
	for i, col := range g.Columns {
		nc, err := c.copyColumn(col, rows)
		if err != nil {
			return nil, err
		}
		ng.Columns[i] = nc
	}
	c.leave()
	return ng, nil
}

func (c *deepCopier) copyColumn(col *GridColumn, rows []int) (*GridColumn, error) {
	nc := &GridColumn{Name: col.Name, ColType: col.ColType}
	if col.Meta != nil {
		c.edges = append(c.edges, copyEdge{kind: copyEdgeColumnMeta, key: col.Name})
		meta, err := c.copyDict(col.Meta)
		c.edges = c.edges[:len(c.edges)-1]
		if err != nil {
			return nil, err
		}
		nc.Meta = meta
	}
	switch col.ColType {
	case COL_INT:
		nc.IntData = pickRows(col.IntData, rows)
	case COL_FLOAT:
		nc.FloatData = pickRows(col.FloatData, rows)
	case COL_STRING:
		nc.StringData = pickRows(col.StringData, rows)
	case COL_DATETIME:
		nc.DateTimeData = pickRows(col.DateTimeData, rows)
	case COL_DICT_STRING:
		nc.DictCodes = pickRows(col.DictCodes, rows)
		// Cloned, not shared: either column may append to its own.
		nc.DictValues = slices.Clone(col.DictValues)
	default:
		nc.GenericData = pickRows(col.GenericData, rows)
		for i, cell := range nc.GenericData {
			if !holdsObjects(cell) {
				continue
			}
			src := i
			if rows != nil {
				src = rows[i]
			}
			copied, err := c.child(copyEdge{kind: copyEdgeCell, index: src, key: col.Name}, cell)
			if err != nil {
				return nil, err
			}
			nc.GenericData[i] = copied
		}
	}
	return nc, nil
}

// pickRows copies data, or only the given rows of it when rows is not nil.
func pickRows[T any](data []T, rows []int) []T {
	if rows == nil {
		return slices.Clone(data)
	}
	out := make([]T, len(rows))
	for i, r := range rows {
		out[i] = data[r]
	}
	return out
}

// enter puts obj on the path, or reports the cycle if it is already there.
func (c *deepCopier) enter(obj any) error {
	if i := c.find(obj); i >= 0 {
		return c.cycleError(i)
	}
	c.path = append(c.path, copyPathEntry{obj: obj, edges: len(c.edges)})
	if c.onPath != nil {
		c.onPath[obj] = len(c.path) - 1
	} else if len(c.path) > copyPathShortMax {
		c.onPath = make(map[any]int, 2*len(c.path))
		for i, e := range c.path {
			c.onPath[e.obj] = i
		}
	}
	return nil
}

func (c *deepCopier) leave() {
	last := len(c.path) - 1
	if c.onPath != nil {
		delete(c.onPath, c.path[last].obj)
	}
	c.path = c.path[:last]
}

func (c *deepCopier) find(obj any) int {
	if c.onPath != nil {
		if i, ok := c.onPath[obj]; ok {
			return i
		}
		return -1
	}
	for i := range c.path {
		if c.path[i].obj == obj {
			return i
		}
	}
	return -1
}

// cycleError describes the cycle from path[i] back to itself.
func (c *deepCopier) cycleError(i int) error {
	var kind string
	switch c.path[i].obj.(type) {
	case *MShellList:
		kind = "list"
	case *MShellDict:
		kind = "dict"
	default:
		kind = "grid"
	}
	start := c.path[i].edges
	where := "the " + kind
	if start > 0 {
		where += " at " + formatCopyEdges(c.edges[:start])
	}
	return fmt.Errorf("deepCopy cannot copy a value that contains itself: %s contains itself through %s",
		where, formatCopyEdges(c.edges[start:]))
}

func formatCopyEdges(edges []copyEdge) string {
	var sb strings.Builder
	for i, e := range edges {
		if i > 0 {
			sb.WriteString(", then ")
		}
		switch e.kind {
		case copyEdgeIndex:
			sb.WriteString("index " + strconv.Itoa(e.index))
		case copyEdgeKey:
			sb.WriteString("key " + strconv.Quote(e.key))
		case copyEdgeJust:
			sb.WriteString("the just")
		case copyEdgeCommand:
			sb.WriteString("command " + strconv.Itoa(e.index) + " of a pipe")
		case copyEdgeCell:
			sb.WriteString("column " + strconv.Quote(e.key) + " row " + strconv.Itoa(e.index))
		case copyEdgeGridMeta:
			sb.WriteString("the grid metadata")
		case copyEdgeColumnMeta:
			sb.WriteString("the metadata of column " + strconv.Quote(e.key))
		}
	}
	return sb.String()
}
