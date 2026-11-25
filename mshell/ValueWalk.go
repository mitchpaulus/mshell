package main

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
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

// The rest of this file is `toJsonFractured`: it lays out the text `toJson`
// gives over several lines, in the style of the FracturedJson library.
// Each list or dictionary is written the first way that fits, in this order:
//
//  1. On one line, if it nests at most fjMaxNesting levels and fits the line.
//  2. A list of short items, several items per line.
//  3. A table: one item per line, with the fields of each item lined up.
//  4. One item per line, each item laid out by these same rules.
//
// `toJsonLong`, which always writes one item per line, is at the end of the file.

const (
	fjLineWidth = 120 // Longest line to write, counting the indent.
	fjIndent    = 4   // Spaces per level.
	// A list or dictionary goes on one line, or is a table row, only if it
	// holds at most this many levels of lists and dictionaries, counting itself.
	fjMaxNesting = 2
	// A list written several items per line needs at least this many per line.
	fjMinItemsPerLine = 3
	// Most levels of lists and dictionaries a value can have, counting the
	// outermost. A deeper value is an error. Each level is one call deeper in
	// the parser and the writer, and 4 more spaces at the start of its lines,
	// so without a limit the output grows with the square of the depth:
	// 100,000 levels would need about 40 GB.
	fjMaxDepth = 1000
	// Most fields a table row can have. A field takes at least 3 characters,
	// one for its value and two for the comma and space or the brackets, so
	// more fields than this never fit the line. Rows with more are not
	// counted field by field: a list of dictionaries that all have different
	// keys would otherwise get a column for every key before the table is
	// found to be too wide.
	fjMaxTableFields = fjLineWidth / 3
)

// fjNode is one JSON value read back from the text `toJson` gives.
// All the values are kept in one slice, each right before its first item, so
// a value's first item is the next node, and the item after that is at `next`.
// A node holds positions in the text instead of strings, so the slice has no
// pointers for the garbage collector to follow.
type fjNode struct {
	start, end       int32 // Where the value is in the text. An array or object starts at its opening bracket.
	keyStart, keyEnd int32 // Where the value's key is in the text, with its quotes, when it is in an object.
	next             int32 // The next item of the array or object this value is in.
	count            int32 // Number of items of an array or object.
	width            int32 // Characters the value takes on one line. Stops at the largest int32.
	kind             byte  // 's' string, 'n' number, 'l' true/false/null, 'a' array, 'o' object.
	nesting          uint8 // 0 for a string, number, literal or empty container, else 1 + the largest of its items. Stops at 255.
}

type fjParser struct {
	s       string
	pos     int
	nodes   []fjNode
	depth   int  // Arrays and objects the parser is inside of.
	tooDeep bool // The text nests more than fjMaxDepth levels.
}

func (p *fjParser) skipSpace() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

// str reads a quoted string and reports whether there was one. The string,
// with its quotes, is the text from where it started to p.pos.
func (p *fjParser) str() bool {
	if p.pos >= len(p.s) || p.s[p.pos] != '"' {
		return false
	}
	for p.pos++; p.pos < len(p.s); p.pos++ {
		switch p.s[p.pos] {
		case '\\':
			p.pos++
		case '"':
			p.pos++
			return true
		}
	}
	return false
}

// value reads one value and everything in it, and returns its place in p.nodes.
func (p *fjParser) value() (int32, bool) {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return 0, false
	}
	at := int32(len(p.nodes))
	start := p.pos
	c := p.s[start]
	switch c {
	case '"':
		if !p.str() {
			return 0, false
		}
		width := utf8.RuneCountInString(p.s[start:p.pos])
		p.nodes = append(p.nodes, fjNode{kind: 's', start: int32(start), end: int32(p.pos), width: int32(width)})
		return at, true
	case '[', '{':
		kind, closer := byte('a'), byte(']')
		if c == '{' {
			kind, closer = 'o', '}'
		}
		if p.depth >= fjMaxDepth {
			p.tooDeep = true
			return 0, false
		}
		p.depth++
		p.nodes = append(p.nodes, fjNode{})
		p.pos++
		width, count, nesting := 2, 0, 0
		last := int32(0)
		for {
			p.skipSpace()
			if p.pos >= len(p.s) {
				return 0, false
			}
			if p.s[p.pos] == closer {
				p.pos++
				break
			}
			if count > 0 {
				if p.s[p.pos] != ',' {
					return 0, false
				}
				p.pos++
				p.skipSpace()
			}
			keyStart, keyEnd := p.pos, p.pos
			if kind == 'o' {
				ok := p.str()
				keyEnd = p.pos
				p.skipSpace()
				if !ok || p.pos >= len(p.s) || p.s[p.pos] != ':' {
					return 0, false
				}
				p.pos++
				width += utf8.RuneCountInString(p.s[keyStart:keyEnd]) + 2
			}
			item, ok := p.value()
			if !ok {
				return 0, false
			}
			n := &p.nodes[item]
			n.keyStart, n.keyEnd = int32(keyStart), int32(keyEnd)
			if count > 0 {
				p.nodes[last].next = item
			}
			last = item
			count++
			width += int(n.width)
			nesting = max(nesting, int(n.nesting)+1)
		}
		if count > 0 {
			width += 2 * (count - 1)
			if nesting >= 2 {
				width += 2
			}
		}
		p.depth--
		p.nodes[at] = fjNode{kind: kind, start: int32(start), end: int32(p.pos), count: int32(count),
			width: int32(min(width, math.MaxInt32)), nesting: uint8(min(nesting, 255))}
		return at, true
	}
scan:
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ',', ':', ']', '}', ' ', '\t', '\r', '\n':
			break scan
		}
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	kind := byte('l')
	if c == '-' || (c >= '0' && c <= '9') {
		kind = 'n'
	}
	p.nodes = append(p.nodes, fjNode{kind: kind, start: int32(start), end: int32(p.pos),
		width: int32(utf8.RuneCountInString(p.s[start:p.pos]))})
	return at, true
}

// fjCol says how to write one column of a table, so that the same field of
// every row starts and ends at the same place. It is filled by giving it each
// value of the column with add, and then calling layout.
type fjCol struct {
	// 'n': every value is a number, written with the decimal points lined up.
	// 'a', 'o': every value is an array or object, written with one column per
	// item or key. 0: values are written as they are, padded on the right.
	kind     byte
	width    int
	intW     int            // 'n': most characters before the decimal point.
	exponent bool           // 'n': some number has an exponent, so the numbers are right-aligned instead.
	children []*fjCol       // 'a', 'o': one per item or key.
	keys     []string       // 'o': the key of each child, in order of first use.
	index    map[string]int // 'o': key to its place in children.
	pad      bool           // 'a', 'o': put a space inside the brackets.

	// Some value has more than fjMaxTableFields items or keys, so the values
	// cannot be written with one column per item. children is not complete.
	tooManyFields bool

	// What add has seen so far.
	firstKind byte // Kind of the first value.
	mixed     bool // Some value is of another kind than the first.
	maxWidth  int  // Widest value.
	maxLen    int  // Numbers: longest text.
	dotIntW   int  // Numbers: most characters before the decimal point, or in all for a number without one.
	dotFracW  int  // Numbers: most characters from the decimal point on.
}

// fjIntLen gives how many characters of a number come before its decimal point.
func fjIntLen(text string, exponent bool) int {
	if !exponent {
		if i := strings.IndexByte(text, '.'); i >= 0 {
			return i
		}
	}
	return len(text)
}

// keyAt gives the place in col.children of an object key, adding it when new.
// guess is the key's place in its own object, which is its place in the
// column whenever the objects have the same keys in the same order.
func (col *fjCol) keyAt(key string, guess int) int {
	if guess < len(col.keys) && col.keys[guess] == key {
		return guess
	}
	at, seen := col.index[key]
	if !seen {
		if col.index == nil {
			col.index = map[string]int{}
		}
		at = len(col.keys)
		col.index[key] = at
		col.keys = append(col.keys, key)
	}
	return at
}

// add takes one more value into the column. levels is how many levels of
// arrays and objects are also taken into columns of their own.
func (f *fjWriter) add(col *fjCol, at int32, levels int) {
	n := &f.nodes[at]
	if col.firstKind == 0 {
		col.firstKind = n.kind
	} else if n.kind != col.firstKind {
		col.mixed = true
	}
	col.maxWidth = max(col.maxWidth, int(n.width))
	if col.mixed {
		return
	}
	switch n.kind {
	case 'n':
		text := f.s[n.start:n.end]
		dot := len(text)
		for i := 0; i < len(text); i++ {
			switch text[i] {
			case '.':
				if dot == len(text) {
					dot = i
				}
			case 'e', 'E':
				col.exponent = true
			}
		}
		col.maxLen = max(col.maxLen, len(text))
		col.dotIntW = max(col.dotIntW, dot)
		col.dotFracW = max(col.dotFracW, len(text)-dot)
	case 'a', 'o':
		if levels <= 0 || col.tooManyFields {
			return
		}
		if n.count > fjMaxTableFields {
			col.tooManyFields = true
			return
		}
		if n.nesting >= 2 {
			col.pad = true
		}
		item := at + 1
		for i := 0; i < int(n.count); i++ {
			child := &f.nodes[item]
			place := i
			if n.kind == 'o' {
				place = col.keyAt(f.s[child.keyStart:child.keyEnd], i)
				if place >= fjMaxTableFields {
					col.tooManyFields = true
					return
				}
			}
			for place >= len(col.children) {
				col.children = append(col.children, f.newCol())
			}
			f.add(col.children[place], item, levels-1)
			item = child.next
		}
	}
}

// layout sets how the column is written, from the values add was given.
// levels is how many levels of arrays and objects are split into their own
// columns. It is at most the levels the values were added with.
func (col *fjCol) layout(levels int) {
	col.kind = 0
	col.width = col.maxWidth
	if col.mixed {
		return
	}
	switch col.firstKind {
	case 'n':
		col.kind = 'n'
		col.intW = col.dotIntW
		col.width = col.dotIntW + col.dotFracW
		if col.exponent {
			col.intW = col.maxLen
			col.width = col.maxLen
		}
	case 'a', 'o':
		if levels <= 0 {
			return
		}
		if col.tooManyFields {
			col.kind = col.firstKind
			col.width = fjLineWidth + 1 // Too wide for any line.
			return
		}
		if len(col.children) == 0 {
			return
		}
		col.kind = col.firstKind
		col.width = 2 + 2*(len(col.children)-1)
		if col.pad {
			col.width += 2
		}
		for i, child := range col.children {
			child.layout(levels - 1)
			col.width += child.width
			if col.kind == 'o' {
				col.width += utf8.RuneCountInString(col.keys[i]) + 2
			}
		}
	}
}

type fjWriter struct {
	s     string
	nodes []fjNode
	buf   []byte
	// The values of the object rows being written, by column. Each row uses
	// the end of it while it is written.
	rowValues []int32
	// Columns are handed out from blocks, one allocation per block.
	colBlock []fjCol
}

func (f *fjWriter) newCol() *fjCol {
	if len(f.colBlock) == 0 {
		f.colBlock = make([]fjCol, 16)
	}
	col := &f.colBlock[0]
	f.colBlock = f.colBlock[1:]
	return col
}

func (f *fjWriter) spaces(n int) {
	for ; n > 0; n-- {
		f.buf = append(f.buf, ' ')
	}
}

// newline starts a new line indented to the given level. Spaces left at the
// end of the current line by column padding are removed.
func (f *fjWriter) newline(level int) {
	for len(f.buf) > 0 && f.buf[len(f.buf)-1] == ' ' {
		f.buf = f.buf[:len(f.buf)-1]
	}
	f.buf = append(f.buf, '\n')
	f.spaces(level * fjIndent)
}

// key writes the key of a value in an object, and the colon after it.
func (f *fjWriter) key(n *fjNode) {
	f.buf = append(f.buf, f.s[n.keyStart:n.keyEnd]...)
	f.buf = append(f.buf, ": "...)
}

// inline writes a value on one line.
func (f *fjWriter) inline(at int32) {
	n := &f.nodes[at]
	if n.kind != 'a' && n.kind != 'o' {
		f.buf = append(f.buf, f.s[n.start:n.end]...)
		return
	}
	open := f.s[n.start]
	f.buf = append(f.buf, open)
	if n.nesting >= 2 {
		f.buf = append(f.buf, ' ')
	}
	item := at + 1
	for i := 0; i < int(n.count); i++ {
		if i > 0 {
			f.buf = append(f.buf, ", "...)
		}
		child := &f.nodes[item]
		if n.kind == 'o' {
			f.key(child)
		}
		f.inline(item)
		item = child.next
	}
	if n.nesting >= 2 {
		f.buf = append(f.buf, ' ')
	}
	f.buf = append(f.buf, open+2) // ']' and '}' are two after '[' and '{'.
}

// Whether a table cell is followed by a comma, or by a space where other
// rows have their comma, or by nothing.
const (
	fjNoComma = iota
	fjBlankComma
	fjComma
)

// cell writes one value of a table column, padded to the column's width.
// at is -1 when the row has no value for this column.
func (f *fjWriter) cell(col *fjCol, at int32, comma int) {
	total := col.width
	if comma != fjNoComma {
		total++
	}
	written := 0
	if at >= 0 {
		n := &f.nodes[at]
		switch {
		case col.kind == 'n':
			text := f.s[n.start:n.end]
			before := col.intW - fjIntLen(text, col.exponent)
			f.spaces(before)
			f.buf = append(f.buf, text...)
			written = before + len(text)
		case col.kind != 0:
			f.cellItems(col, at)
			written = col.width
		default:
			f.inline(at)
			written = int(n.width)
		}
		if comma == fjComma {
			f.buf = append(f.buf, ',')
			written++
		}
	}
	f.spaces(total - written)
}

// cellItems writes an array or object with each item in its own column.
func (f *fjWriter) cellItems(col *fjCol, at int32) {
	n := &f.nodes[at]
	// values[i] is the row's value for column i, or -1.
	base := len(f.rowValues)
	for range col.children {
		f.rowValues = append(f.rowValues, -1)
	}
	last := -1
	item := at + 1
	for i := 0; i < int(n.count); i++ {
		child := &f.nodes[item]
		place := i
		if col.kind == 'o' {
			place = col.keyAt(f.s[child.keyStart:child.keyEnd], i)
		}
		f.rowValues[base+place] = item
		last = max(last, place)
		item = child.next
	}
	open := f.s[n.start]
	f.buf = append(f.buf, open)
	if col.pad {
		f.buf = append(f.buf, ' ')
	}
	for i, child := range col.children {
		if i > 0 {
			f.buf = append(f.buf, ' ')
		}
		v := f.rowValues[base+i]
		if col.kind == 'o' {
			if v < 0 {
				f.spaces(utf8.RuneCountInString(col.keys[i]) + 2)
			} else {
				f.buf = append(f.buf, col.keys[i]...)
				f.buf = append(f.buf, ": "...)
			}
		}
		comma := fjNoComma
		if i < len(col.children)-1 {
			comma = fjBlankComma
			if i < last {
				comma = fjComma
			}
		}
		f.cell(child, v, comma)
	}
	f.rowValues = f.rowValues[:base]
	if col.pad {
		f.buf = append(f.buf, ' ')
	}
	f.buf = append(f.buf, open+2)
}

// value writes a value that starts `used` characters into a line at the given
// indent level. comma says a comma follows it on its last line.
func (f *fjWriter) value(at int32, level int, used int, comma bool) {
	n := &f.nodes[at]
	room := fjLineWidth - used
	if comma {
		room--
	}
	if n.count == 0 || (n.nesting <= fjMaxNesting && int(n.width) <= room) {
		f.inline(at)
		return
	}
	if f.severalPerLine(at, level) || f.table(at, level) {
		return
	}
	open := f.s[n.start]
	f.buf = append(f.buf, open)
	indent := (level + 1) * fjIndent
	item := at + 1
	for i := 0; i < int(n.count); i++ {
		child := &f.nodes[item]
		f.newline(level + 1)
		itemUsed := indent
		if n.kind == 'o' {
			f.key(child)
			itemUsed += utf8.RuneCountInString(f.s[child.keyStart:child.keyEnd]) + 2
		}
		more := i < int(n.count)-1
		f.value(item, level+1, itemUsed, more)
		if more {
			f.buf = append(f.buf, ',')
		}
		item = child.next
	}
	f.newline(level)
	f.buf = append(f.buf, open+2)
}

// severalPerLine writes an array of short items with as many items on each
// line as fit, and reports whether it did. Numbers are padded to one width so
// they form columns. It does nothing if the array holds an object or deeper
// nesting, if an item is too long for one line, or if some line would hold
// fewer than fjMinItemsPerLine items.
func (f *fjWriter) severalPerLine(at int32, level int) bool {
	n := &f.nodes[at]
	if n.kind != 'a' || n.nesting > fjMaxNesting {
		return false
	}
	var col fjCol
	item := at + 1
	for i := 0; i < int(n.count); i++ {
		child := &f.nodes[item]
		if child.kind == 'o' && child.count > 0 {
			return false
		}
		f.add(&col, item, 0)
		item = child.next
	}
	col.layout(0)
	indent := (level + 1) * fjIndent
	// The first pass only finds whether the lines work out. The second pass
	// writes them, breaking the lines at the same items.
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			f.buf = append(f.buf, '[')
		}
		used, onLine := indent, 0
		item = at + 1
		for i := 0; i < int(n.count); i++ {
			child := &f.nodes[item]
			more := i < int(n.count)-1
			need := int(child.width)
			if col.kind == 'n' {
				need = col.width
			}
			if more {
				need++
			}
			if indent+need > fjLineWidth {
				return false // Too long for a line of its own; it has to be split.
			}
			if onLine > 0 && used+1+need > fjLineWidth {
				if onLine < fjMinItemsPerLine {
					return false
				}
				used, onLine = indent, 0
			}
			if pass == 1 {
				if onLine == 0 {
					f.newline(level + 1)
				} else {
					f.buf = append(f.buf, ' ')
				}
				comma := fjNoComma
				if more {
					comma = fjComma
				}
				if col.kind == 'n' {
					f.cell(&col, item, comma)
				} else {
					f.inline(item)
					if more {
						f.buf = append(f.buf, ',')
					}
				}
			}
			if onLine > 0 {
				used++
			}
			used += need
			onLine++
			item = child.next
		}
	}
	f.newline(level)
	f.buf = append(f.buf, ']')
	return true
}

// table writes an array or object whose items are all arrays, or all objects,
// one item per line with the fields lined up, and reports whether it did.
// It does nothing if the lined up rows do not fit the line.
func (f *fjWriter) table(at int32, level int) bool {
	n := &f.nodes[at]
	if n.count < 2 {
		return false
	}
	kind := f.nodes[at+1].kind
	if kind != 'a' && kind != 'o' {
		return false
	}
	keyW := 0
	item := at + 1
	for i := 0; i < int(n.count); i++ {
		child := &f.nodes[item]
		if child.kind != kind || child.nesting > fjMaxNesting {
			return false
		}
		if n.kind == 'o' {
			keyW = max(keyW, utf8.RuneCountInString(f.s[child.keyStart:child.keyEnd]))
		}
		item = child.next
	}
	used := (level + 1) * fjIndent
	if n.kind == 'o' {
		used += keyW + 2
	}
	var col fjCol
	item = at + 1
	for i := 0; i < int(n.count); i++ {
		f.add(&col, item, fjMaxNesting)
		item = f.nodes[item].next
	}
	// Try with the rows' inner arrays and objects split into columns too,
	// then with only the rows' own fields as columns.
	for levels := fjMaxNesting; levels >= 1; levels-- {
		col.layout(levels)
		if col.kind == 0 {
			return false
		}
		if used+col.width+1 > fjLineWidth {
			continue
		}
		open := f.s[n.start]
		f.buf = append(f.buf, open)
		item = at + 1
		for i := 0; i < int(n.count); i++ {
			child := &f.nodes[item]
			f.newline(level + 1)
			if n.kind == 'o' {
				key := f.s[child.keyStart:child.keyEnd]
				f.buf = append(f.buf, key...)
				f.spaces(keyW - utf8.RuneCountInString(key))
				f.buf = append(f.buf, ": "...)
			}
			comma := fjNoComma
			if i < int(n.count)-1 {
				comma = fjComma
			}
			f.cell(&col, item, comma)
			item = child.next
		}
		f.newline(level)
		f.buf = append(f.buf, open+2)
		return true
	}
	return false
}

// fracturedJson lays out compact JSON text over several lines.
// Text that is not JSON is returned as it is, and so is text too long for
// its positions to be kept in an int32.
// Text nested more than fjMaxDepth levels is an error.
func fracturedJson(compact string) (string, error) {
	if len(compact) > math.MaxInt32 {
		return compact, nil
	}
	// Every value but the first comes after a comma or an opening bracket, so
	// this is room for all of them, and the slice is never copied to grow.
	// Commas and brackets inside strings make it more room than needed.
	room := strings.Count(compact, ",") + strings.Count(compact, "[") + strings.Count(compact, "{") + 1
	p := fjParser{s: compact, nodes: make([]fjNode, 0, room)}
	root, ok := p.value()
	p.skipSpace()
	if p.tooDeep {
		return "", fmt.Errorf("Cannot lay out JSON nested more than %d levels deep.\n", fjMaxDepth)
	}
	if !ok || p.pos != len(compact) {
		return compact, nil
	}
	f := fjWriter{s: compact, nodes: p.nodes, buf: make([]byte, 0, len(compact)*2)}
	f.value(root, 0, 0, false)
	return string(f.buf), nil
}

// longJson lays out compact JSON text with every item of a list or dictionary
// on its own line, indented fjIndent spaces per level. This is `toJsonLong`.
// An empty list or dictionary stays as `[]` or `{}`.
// Text nested more than fjMaxDepth levels is an error, for the same reason as
// in fracturedJson: the indent makes the output grow with the square of the depth.
func longJson(compact string) (string, error) {
	buf := make([]byte, 0, len(compact)*2)
	depth := 0
	newline := func() {
		buf = append(buf, '\n')
		for n := depth * fjIndent; n > 0; n-- {
			buf = append(buf, ' ')
		}
	}
	// skipSpace returns the position of the first character at or after i
	// that is not white space.
	skipSpace := func(i int) int {
		for i < len(compact) && (compact[i] == ' ' || compact[i] == '\t' || compact[i] == '\r' || compact[i] == '\n') {
			i++
		}
		return i
	}
	for i := 0; i < len(compact); {
		c := compact[i]
		switch c {
		case '"':
			end := i + 1
			for end < len(compact) && compact[end] != '"' {
				if compact[end] == '\\' {
					end++
				}
				end++
			}
			end = min(end+1, len(compact))
			buf = append(buf, compact[i:end]...)
			i = end
		case '[', '{':
			buf = append(buf, c)
			i = skipSpace(i + 1)
			if i < len(compact) && compact[i] == c+2 { // ']' and '}' are two after '[' and '{'.
				buf = append(buf, c+2)
				i++
				break
			}
			depth++
			if depth > fjMaxDepth {
				return "", fmt.Errorf("Cannot lay out JSON nested more than %d levels deep.\n", fjMaxDepth)
			}
			newline()
		case ']', '}':
			if depth > 0 {
				depth--
			}
			newline()
			buf = append(buf, c)
			i++
		case ',':
			buf = append(buf, c)
			newline()
			i = skipSpace(i + 1)
		case ':':
			buf = append(buf, ": "...)
			i = skipSpace(i + 1)
		case ' ', '\t', '\r', '\n':
			i++
		default:
			buf = append(buf, c)
			i++
		}
	}
	return string(buf), nil
}
