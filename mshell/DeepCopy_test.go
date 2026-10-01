package main

import (
	"strings"
	"testing"
)

func intList(xs ...int) *MShellList {
	l := NewList(len(xs))
	for i, x := range xs {
		l.Items[i] = MShellInt{Value: x}
	}
	return l
}

func TestDeepCopyTwoPathsGiveTwoLists(t *testing.T) {
	xs := intList(1, 2)
	d := NewDict()
	d.Items["a"] = xs
	d.Items["b"] = xs
	out, err := DeepCopy(d)
	if err != nil {
		t.Fatal(err)
	}
	cd := out.(*MShellDict)
	a, b := cd.Items["a"].(*MShellList), cd.Items["b"].(*MShellList)
	if a == b || a == xs || b == xs {
		t.Fatalf("copies share a list: a=%p b=%p xs=%p", a, b, xs)
	}
	if &a.Items[0] == &xs.Items[0] {
		t.Fatal("copy shares the backing array")
	}
}

func TestDeepCopyKeepsCommandFields(t *testing.T) {
	l := intList(1)
	l.StdoutBehavior = STDOUT_LINES
	l.StandardOutputFile = "out.txt"
	out, err := DeepCopy(l)
	if err != nil {
		t.Fatal(err)
	}
	cl := out.(*MShellList)
	if cl.StdoutBehavior != STDOUT_LINES || cl.StandardOutputFile != "out.txt" {
		t.Fatalf("redirect fields lost: %+v", cl)
	}
}

func TestDeepCopyGrid(t *testing.T) {
	g := NewGrid()
	g.Meta = NewDict()
	g.Meta.Items["k"] = intList(1)
	names := NewGridColumn("name", 3)
	for i, s := range []string{"a", "b", "a"} {
		names.GenericData[i] = MShellString{Content: s}
	}
	dictEncodeStringColumn(names)
	names.Meta = NewDict()
	names.Meta.Items["unit"] = MShellString{Content: "none"}
	cells := NewGridColumn("cells", 3)
	for i := range 3 {
		cells.GenericData[i] = intList(i)
	}
	g.AddColumn(names)
	g.AddColumn(cells)
	g.RowCount = 3

	out, err := DeepCopy(g)
	if err != nil {
		t.Fatal(err)
	}
	cg := out.(*MShellGrid)
	if cg == g || cg.Meta == g.Meta || cg.Meta.Items["k"] == g.Meta.Items["k"] {
		t.Fatal("grid or its metadata is shared")
	}
	cn := cg.GetColumn("name")
	if cn == names || cn.Meta == names.Meta {
		t.Fatal("column or its metadata is shared")
	}
	if names.ColType == COL_DICT_STRING {
		cn.Set(0, MShellString{Content: "new"})
		if names.StringAt(0) != "a" || len(names.DictValues) != 2 {
			t.Fatalf("writing the copy changed the source column: %q %v", names.StringAt(0), names.DictValues)
		}
	}
	if cg.GetColumn("cells").GenericData[1] == cells.GenericData[1] {
		t.Fatal("a list in a generic cell is shared")
	}

	// A view copies only its rows.
	view := &MShellGridView{Source: g, Indices: []int{2, 0}}
	out, err = DeepCopy(view)
	if err != nil {
		t.Fatal(err)
	}
	cv := out.(*MShellGridView)
	if cv.Source == g || cv.Source.RowCount != 2 || len(cv.Indices) != 2 {
		t.Fatalf("view copy: %+v", cv)
	}
	if got := cv.Source.GetColumn("cells").GenericData[0].(*MShellList).Items[0].(MShellInt).Value; got != 2 {
		t.Fatalf("view copy row 0 = %d, want 2", got)
	}
}

func TestDeepCopyCycles(t *testing.T) {
	// A list that contains itself through a Maybe.
	l := intList(1)
	l.Items = append(l.Items, &Maybe{obj: l})
	_, err := DeepCopy(l)
	if err == nil || !strings.Contains(err.Error(), "index 1, then the just") {
		t.Fatalf("got %v", err)
	}

	// A grid that contains itself through a cell, below a dict.
	g := NewGrid()
	col := NewGridColumn("c", 1)
	col.GenericData[0] = g
	g.AddColumn(col)
	g.RowCount = 1
	d := NewDict()
	d.Items["g"] = g
	_, err = DeepCopy(d)
	if err == nil || !strings.Contains(err.Error(), `the grid at key "g" contains itself through column "c" row 0`) {
		t.Fatalf("got %v", err)
	}
}

func TestDeepCopyLongPath(t *testing.T) {
	// Deeper than copyPathShortMax, so the path is also kept in a map.
	depth := 3 * copyPathShortMax
	root := NewList(0)
	cur := root
	for range depth {
		next := NewList(0)
		cur.Items = append(cur.Items, next)
		cur = next
	}
	if _, err := DeepCopy(root); err != nil {
		t.Fatal(err)
	}
	// The same list reached twice at the bottom is two paths, not a cycle.
	shared := intList(7)
	cur.Items = append(cur.Items, shared, shared)
	if _, err := DeepCopy(root); err != nil {
		t.Fatal(err)
	}
	// Close the cycle at the bottom.
	cur.Items = append(cur.Items, root)
	_, err := DeepCopy(root)
	if err == nil || !strings.HasPrefix(err.Error(), "deepCopy cannot copy a value that contains itself: the list contains itself") {
		t.Fatalf("got %v", err)
	}
}

func BenchmarkDeepCopyStrList(b *testing.B) {
	l := NewList(1000)
	for i := range l.Items {
		l.Items[i] = MShellString{Content: "x"}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DeepCopy(l); err != nil {
			b.Fatal(err)
		}
	}
}
