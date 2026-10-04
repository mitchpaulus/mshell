package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Printing, JSON and equality of nested values, each walked with one explicit
// work stack instead of method recursion, so a value of any depth cannot
// overflow the Go stack, and a value that contains itself cannot hang.
// Ported from the enum branch (debe03e, 0d78c14, 3413d03 and the fixes after
// them). A checked program can build a cyclic value through a recursive alias
// (`[] as [Json] j!  @j @j append`; design doc, "Cyclic values").

// isRefKind reports whether obj's dynamic type is a pointer: a value with heap
// identity. Only these can be shared or sit on a cycle, and only these are safe
// as map keys (a value kind may wrap a []byte, which panics when hashed).
func isRefKind(obj MShellObject) bool {
	return obj != nil && reflect.TypeOf(obj).Kind() == reflect.Pointer
}

func asMaybe(obj MShellObject) (Maybe, bool) {
	switch v := obj.(type) {
	case Maybe:
		return v, true
	case *Maybe:
		return *v, true
	}
	return Maybe{}, false
}

func sortedDictKeys(m map[string]MShellObject) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dagGuard keeps a comparison of values with shared parts from taking
// exponential time, and a comparison of two cyclic values from running forever.
// It counts the pairs the walk expands; past a threshold it remembers every
// pair of objects already expanded and skips a pair met again.
//
// Skipping is sound in a last-in-first-out walk: a pair's whole expansion is
// done before a later copy of it (lower on the stack) is popped, and any
// mismatch returns at once, so a copy that is popped at all compared equal.
// A pair met again while it is still being expanded is a cycle; skipping it
// assumes the pair is equal, which is equality of the infinite trees the two
// values unfold to, the same assumption validation makes (design doc §tryAs).
//
// Below the threshold the guard is one counter, so ordinary comparisons do not
// allocate. Past it the memo is not bounded: a bounded memo loses to a working
// set larger than its bound and goes exponential again.
type dagGuard struct {
	steps int
	memo  map[refPair]bool
}

type refPair struct{ a, b MShellObject }

const dagStepThreshold = 1 << 19

// skip reports whether this pair was already expanded earlier in the walk.
// Call it once per expanded pair.
func (g *dagGuard) skip(a, b MShellObject) bool {
	g.steps++
	if g.steps < dagStepThreshold {
		return false
	}
	if !isRefKind(a) || !isRefKind(b) {
		return false
	}
	key := refPair{a, b}
	if g.memo == nil {
		g.memo = make(map[refPair]bool, 1024)
	}
	if g.memo[key] {
		return true
	}
	g.memo[key] = true
	return false
}

// renderFlavor selects which text form of a value to make: flavorStr is
// ToString (what `str` gives), flavorDebug is DebugString (stack dumps, error
// messages, list items), flavorJson is ToJson. A container picks its items'
// flavor as the per-type methods always did: a list shows its items as
// DebugString, a dict's `str` form is its JSON form, and Maybe keeps its own.
type renderFlavor uint8

const (
	flavorStr renderFlavor = iota
	flavorDebug
	flavorJson
)

// gridRowKey identifies one row of a grid on the current path. Rows are made
// on demand, so the row object itself has no lasting identity.
type gridRowKey struct {
	grid *MShellGrid
	row  int
}

// renderValue renders a value in one flavor. It always finishes: a cyclic value
// shows `<cycle>` where it refers back to itself, so error messages and stack
// dumps cannot hang. `str` and `toJson` call renderValueDetect instead and
// report a cyclic value as an error.
func renderValue(root MShellObject, flavor renderFlavor) string {
	s, _ := renderValueDetect(root, flavor)
	return s
}

// renderValueDetect renders a value in one flavor and reports whether it met a
// cycle. Lists, pipes, dicts, Maybes, enum values, grid rows and the JSON of
// grids are expanded here, one frame per container on an explicit stack;
// other kinds render through their own methods, which do not look at other
// values.
//
// The containers with a frame are the current path. Reaching one of them again
// is a cycle: `<cycle>` is written there instead of going in. An object reached
// twice along different paths (a DAG) is not on the path the second time, and
// renders twice.
func renderValueDetect(root MShellObject, flavor renderFlavor) (string, bool) {
	var r renderer
	var buf [8]renderFrame
	r.frames = buf[:0]
	r.open(root, flavor)
	for len(r.frames) > 0 {
		f := &r.frames[len(r.frames)-1]
		if f.i == f.n {
			r.sb.WriteString(renderTexts[f.close])
			r.pop()
			continue
		}
		if f.i > 0 {
			r.sb.WriteString(renderTexts[f.sep])
		}
		v := r.item(f)
		f.i++
		r.open(v, f.flavor)
	}
	return r.sb.String(), r.cycled
}

// renderPathShortMax is the number of containers on the path up to which the
// path is searched linearly; past it, they are also kept in a map.
const renderPathShortMax = 32

type frameKind uint8

const (
	frameItems frameKind = iota // the values in items
	frameOne                    // the one value in one
	frameDict                   // the values of ext.dict under ext.keys
	frameRow                    // the cells of one grid row: ext.cols at ext.row, named by ext.keys
	frameGrid                   // the rows of ext.grid at ext.indices (all rows when nil), as JSON
)

// keyStyle is what is written before each item of a frame.
type keyStyle uint8

const (
	keyNone  keyStyle = iota
	keyDebug          // k:
	keyJson           // "k":
)

// renderText is the text between a frame's items, or after the last one.
type renderText uint8

const (
	textNone renderText = iota
	textSpace
	textComma
	textPipe
	textBracket
	textParen
	textBrace
	textDebugDictEnd
	textBracketBrace
)

var renderTexts = [...]string{"", " ", ", ", " | ", "]", ")", "}", ", }", "]}"}

// renderFrame is one container being rendered: its items, how far it has got,
// and the text between and after the items. It is kept small, since a deep
// value has one frame per level; dicts, rows and grids keep the rest in ext.
type renderFrame struct {
	items  []MShellObject
	one    MShellObject
	ext    *renderExt
	path   any // the container's key on the path, or nil
	i, n   int32
	kind   frameKind
	flavor renderFlavor // the flavor of the items
	style  keyStyle
	sep    renderText
	close  renderText
}

type renderExt struct {
	keys    []string
	dict    map[string]MShellObject
	cols    []*GridColumn
	row     int
	grid    *MShellGrid
	indices []int
}

type renderer struct {
	sb     strings.Builder
	frames []renderFrame
	// onPath holds the path keys of the frames once more than
	// renderPathShortMax of them have one. Nil until then.
	onPath   map[any]bool
	pathKeys int
	cycled   bool
}

// pathKeyOf is the key a container is known by on the path, or nil for a
// value that cannot be on a cycle by itself. Rows are made on demand, so a
// row is known by its grid and index.
func pathKeyOf(obj MShellObject) any {
	switch v := obj.(type) {
	case *MShellList, *MShellPipe, *MShellDict, *MShellGrid:
		return obj
	case *MShellGridRow:
		return gridRowKey{v.Grid, v.RowIndex}
	}
	return nil
}

func (r *renderer) onPathHas(key any) bool {
	if r.onPath != nil {
		return r.onPath[key]
	}
	for i := range r.frames {
		if r.frames[i].path == key {
			return true
		}
	}
	return false
}

// push opens a frame; its path key, if any, goes on the path.
func (r *renderer) push(f renderFrame) {
	if f.path != nil {
		r.pathKeys++
		if r.onPath != nil {
			r.onPath[f.path] = true
		} else if r.pathKeys > renderPathShortMax {
			r.onPath = make(map[any]bool, 2*r.pathKeys)
			for i := range r.frames {
				if r.frames[i].path != nil {
					r.onPath[r.frames[i].path] = true
				}
			}
			r.onPath[f.path] = true
		}
	}
	r.frames = append(r.frames, f)
}

func (r *renderer) pop() {
	last := len(r.frames) - 1
	if key := r.frames[last].path; key != nil {
		r.pathKeys--
		if r.onPath != nil {
			delete(r.onPath, key)
		}
	}
	r.frames[last] = renderFrame{}
	r.frames = r.frames[:last]
}

// item writes the key before item f.i, if the frame has keys, and returns it.
func (r *renderer) item(f *renderFrame) MShellObject {
	switch f.style {
	case keyDebug:
		r.sb.WriteString(f.ext.keys[f.i])
		r.sb.WriteString(": ")
	case keyJson:
		writeJsonString(&r.sb, f.ext.keys[f.i])
		r.sb.WriteString(": ")
	}
	switch f.kind {
	case frameItems:
		return f.items[f.i]
	case frameDict:
		return f.ext.dict[f.ext.keys[f.i]]
	case frameRow:
		return f.ext.cols[f.i].Get(f.ext.row)
	case frameGrid:
		idx := int(f.i)
		if f.ext.indices != nil {
			idx = f.ext.indices[f.i]
		}
		return &MShellGridRow{Grid: f.ext.grid, RowIndex: idx}
	default:
		return f.one
	}
}

// itemsFrame is a frame over a list of values.
func itemsFrame(items []MShellObject, flavor renderFlavor, sep, close renderText, path any) renderFrame {
	return renderFrame{items: items, n: int32(len(items)), flavor: flavor, sep: sep, close: close, path: path}
}

// oneFrame is a frame around one value.
func oneFrame(v MShellObject, flavor renderFlavor, close renderText) renderFrame {
	return renderFrame{kind: frameOne, one: v, n: 1, flavor: flavor, close: close}
}

// open writes a value that is not a container, or writes a container's
// opening text and pushes its frame.
func (r *renderer) open(v MShellObject, flavor renderFlavor) {
	// A Maybe's JSON is its contents' JSON, so a chain of them is one loop.
	for {
		m, ok := asMaybe(v)
		if !ok {
			break
		}
		switch {
		case m.IsNone() && flavor == flavorJson:
			r.sb.WriteString("null")
			return
		case m.IsNone():
			r.sb.WriteString("None")
			return
		case flavor == flavorJson:
			v = m.obj
			continue
		case flavor == flavorDebug:
			r.sb.WriteString("Maybe(")
		default:
			r.sb.WriteString("Just(")
		}
		r.push(oneFrame(m.obj, flavor, textParen))
		return
	}
	if v == nil {
		r.sb.WriteString("nil")
		return
	}
	key := pathKeyOf(v)
	if key != nil && r.onPathHas(key) {
		r.sb.WriteString("<cycle>")
		r.cycled = true
		return
	}
	switch x := v.(type) {
	case *MShellList:
		r.sb.WriteString("[")
		if flavor == flavorJson {
			r.push(itemsFrame(x.Items, flavorJson, textComma, textBracket, key))
		} else {
			r.push(itemsFrame(x.Items, flavorDebug, textSpace, textBracket, key))
		}
	case *MShellPipe:
		items := x.List.Items
		if flavor == flavorJson {
			r.sb.WriteString("[")
			r.push(itemsFrame(items, flavorJson, textComma, textBracket, key))
		} else {
			r.push(itemsFrame(items, flavorDebug, textPipe, textNone, key))
		}
	case *MShellDict:
		if flavor == flavorDebug {
			keys := sortedDictKeys(x.Items)
			r.sb.WriteString("Dictionary{")
			r.push(renderFrame{kind: frameDict, ext: &renderExt{dict: x.Items, keys: keys}, n: int32(len(keys)), flavor: flavorDebug,
				style: keyDebug, sep: textComma, close: debugDictClose(len(keys)), path: key})
			return
		}
		// The `str` form of a dict is its JSON form.
		if len(x.Items) == 0 {
			r.sb.WriteString("{}")
			return
		}
		keys := sortedDictKeys(x.Items)
		r.sb.WriteString("{")
		r.push(renderFrame{kind: frameDict, ext: &renderExt{dict: x.Items, keys: keys}, n: int32(len(keys)), flavor: flavorJson,
			style: keyJson, sep: textComma, close: textBrace, path: key})
	case *MShellGrid:
		if flavor != flavorJson {
			r.sb.WriteString(x.DebugString())
			return
		}
		r.sb.WriteString("[")
		r.push(renderFrame{kind: frameGrid, ext: &renderExt{grid: x}, n: int32(x.RowCount), flavor: flavorJson, sep: textComma, close: textBracket, path: key})
	case *MShellGridView:
		if flavor != flavorJson {
			r.sb.WriteString(x.DebugString())
			return
		}
		r.sb.WriteString("[")
		r.push(renderFrame{kind: frameGrid, ext: &renderExt{grid: x.Source, indices: x.Indices}, n: int32(len(x.Indices)), flavor: flavorJson, sep: textComma, close: textBracket})
	case *MShellGridRow:
		cols := x.Grid.Columns
		if flavor == flavorJson {
			// Columns in grid order.
			names := make([]string, len(cols))
			for i, col := range cols {
				names[i] = col.Name
			}
			r.sb.WriteString("{")
			r.push(renderFrame{kind: frameRow, ext: &renderExt{cols: cols, row: x.RowIndex, keys: names}, n: int32(len(cols)), flavor: flavorJson,
				style: keyJson, sep: textComma, close: textBrace, path: key})
			return
		}
		// `GridRow` followed by the row as a dict's DebugString: columns by name.
		sorted := slices.Clone(cols)
		slices.SortFunc(sorted, func(a, b *GridColumn) int { return strings.Compare(a.Name, b.Name) })
		names := make([]string, len(sorted))
		for i, col := range sorted {
			names[i] = col.Name
		}
		r.sb.WriteString("GridRowDictionary{")
		r.push(renderFrame{kind: frameRow, ext: &renderExt{cols: sorted, row: x.RowIndex, keys: names}, n: int32(len(sorted)), flavor: flavorDebug,
			style: keyDebug, sep: textComma, close: debugDictClose(len(sorted)), path: key})
	case *MShellEnum:
		// Enum values are never changed, so one cannot be on a cycle unless
		// a list or dict inside it is, and those are on the path.
		if flavor == flavorJson {
			switch len(x.Payload) {
			case 0:
				writeJsonString(&r.sb, x.Member)
			case 1:
				r.sb.WriteString("{")
				writeJsonString(&r.sb, x.Member)
				r.sb.WriteString(": ")
				r.push(oneFrame(x.Payload[0], flavorJson, textBrace))
			default:
				r.sb.WriteString("{")
				writeJsonString(&r.sb, x.Member)
				r.sb.WriteString(": [")
				r.push(itemsFrame(x.Payload, flavorJson, textComma, textBracketBrace, nil))
			}
			return
		}
		// `member` or `member(p0 p1)`, the payloads in their `str` form, or,
		// in a stack dump or error message, in their debug form (quoted,
		// cleaned and shortened like every other value there).
		r.sb.WriteString(x.Member)
		if len(x.Payload) > 0 {
			r.sb.WriteString("(")
			r.push(itemsFrame(x.Payload, flavor, textSpace, textParen, nil))
		}
	case MShellString:
		if flavor == flavorJson {
			writeJsonString(&r.sb, x.Content)
		} else if flavor == flavorDebug {
			r.sb.WriteString(x.DebugString())
		} else {
			r.sb.WriteString(x.Content)
		}
	case MShellInt:
		r.sb.WriteString(strconv.Itoa(x.Value))
	default:
		switch flavor {
		case flavorDebug:
			r.sb.WriteString(v.DebugString())
		case flavorJson:
			r.sb.WriteString(v.ToJson())
		default:
			r.sb.WriteString(v.ToString())
		}
	}
}

// debugDictClose ends a dict's DebugString, which puts ", " after every entry.
func debugDictClose(entries int) renderText {
	if entries == 0 {
		return textBrace
	}
	return textDebugDictEnd
}

// writeJsonString writes s as a JSON string, exactly as json.Marshal does.
// Printable ASCII that json.Marshal leaves alone is written directly.
func writeJsonString(sb *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			enc, _ := json.Marshal(s)
			sb.Write(enc)
			return
		}
	}
	sb.WriteByte('"')
	sb.WriteString(s)
	sb.WriteByte('"')
}

// renderOrError is what `str` and `toJson` use: a cyclic value is an error.
func renderOrError(obj MShellObject, flavor renderFlavor) (string, error) {
	s, cycled := renderValueDetect(obj, flavor)
	if cycled {
		what := "a string"
		if flavor == flavorJson {
			what = "JSON"
		}
		return "", fmt.Errorf("Cannot convert a value that contains itself to %s.\n", what)
	}
	return s, nil
}

// eqFrame is a pair of containers being compared: two dicts' values under
// keys, or two enum values' payloads, from index i on.
type eqFrame struct {
	a, b   *MShellDict
	keys   []string
	pa, pb []MShellObject
	i      int
}

func (f *eqFrame) done() bool {
	if f.a != nil {
		return f.i == len(f.keys)
	}
	return f.i == len(f.pa)
}

// equalsIter is equality of dicts, Maybes and enum values, walked with an
// explicit stack of container pairs, so it cannot overflow the Go stack.
// Every other kind compares through its own Equals: lists have no equality,
// so a list inside is still an error, even when a dict is compared with
// itself. Dict values are compared in sorted key order, depth first, as the
// recursive version did, so which pair fails first (false, or an error) does
// not vary. Two enum values are equal when the enum, the member and the
// payloads are. Values of different kinds in a dict or a payload are unequal,
// where two values compared directly may be an error. dagGuard keeps shared
// and cyclic values from blowing up.
func equalsIter(a, b MShellObject) (bool, error) {
	switch a.(type) {
	case *MShellDict, *MShellEnum:
	default:
		if _, ok := asMaybe(a); !ok {
			return a.Equals(b)
		}
	}
	var guard dagGuard
	var buf [4]eqFrame
	frames := buf[:0]
	x, y, inner := a, b, false
	for {
		eq, err := true, error(nil)
		if inner && x.TypeName() != y.TypeName() {
			return false, nil
		}
		// A Maybe pair: compare the contents, in place.
		for {
			xm, ok := asMaybe(x)
			if !ok {
				break
			}
			ym, ok := asMaybe(y)
			if !ok || xm.IsNone() != ym.IsNone() {
				return false, nil
			}
			if xm.IsNone() {
				x = nil
				break
			}
			x, y = xm.obj, ym.obj
		}
		// Values of different kinds inside a Maybe are unequal, as in a
		// dict or a payload: Maybe(5) and Maybe(null) included.
		if x != nil && x.TypeName() != y.TypeName() {
			return false, nil
		}
		switch xv := x.(type) {
		case nil:
		case *MShellDict:
			yd, ok := y.(*MShellDict)
			if !ok || len(xv.Items) != len(yd.Items) {
				return false, nil
			}
			if !guard.skip(xv, yd) {
				for k := range xv.Items {
					if _, ok := yd.Items[k]; !ok {
						return false, nil
					}
				}
				frames = append(frames, eqFrame{a: xv, b: yd, keys: sortedDictKeys(xv.Items)})
			}
		case *MShellEnum:
			ye, ok := y.(*MShellEnum)
			if !ok || xv.EnumName != ye.EnumName || xv.Member != ye.Member || len(xv.Payload) != len(ye.Payload) {
				return false, nil
			}
			if len(xv.Payload) > 0 && !guard.skip(xv, ye) {
				frames = append(frames, eqFrame{pa: xv.Payload, pb: ye.Payload})
			}
		default:
			if eq, err = x.Equals(y); err != nil || !eq {
				return eq, err
			}
		}
		// The next pair: the next item of the innermost pair not done.
		for len(frames) > 0 && frames[len(frames)-1].done() {
			frames = frames[:len(frames)-1]
		}
		if len(frames) == 0 {
			return true, nil
		}
		f := &frames[len(frames)-1]
		if f.a != nil {
			k := f.keys[f.i]
			x, y = f.a.Items[k], f.b.Items[k]
		} else {
			x, y = f.pa[f.i], f.pb[f.i]
		}
		f.i++
		inner = true
	}
}
