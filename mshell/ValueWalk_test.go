package main

import (
	"strings"
	"testing"
)

// deepChain builds a value n levels deep whose containers alternate: a list
// holding a dict holding a Maybe holding the next list.
func deepChain(n int, leaf MShellObject) MShellObject {
	v := leaf
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			l := NewList(1)
			l.Items[0] = v
			v = l
		case 1:
			d := NewDict()
			d.Items["k"] = v
			v = d
		default:
			v = &Maybe{obj: v}
		}
	}
	return v
}

// deepDictChain is n dicts, each holding a Maybe of the next: a value that
// has equality all the way down.
func deepDictChain(n int, leaf MShellObject) MShellObject {
	v := leaf
	for i := 0; i < n; i++ {
		d := NewDict()
		d.Items["k"] = &Maybe{obj: v}
		v = d
	}
	return v
}

func TestRenderDeepValue(t *testing.T) {
	const n = 1_000_000
	v := deepChain(n, MShellInt{Value: 7})
	for _, flavor := range []renderFlavor{flavorStr, flavorDebug, flavorJson} {
		s, cycled := renderValueDetect(v, flavor)
		if cycled {
			t.Fatalf("flavor %d: a deep value reported as a cycle", flavor)
		}
		if !strings.Contains(s, "7") {
			t.Fatalf("flavor %d: the leaf is missing", flavor)
		}
	}
}

func TestEqualsDeepValue(t *testing.T) {
	const n = 1_000_000
	a := deepDictChain(n, MShellInt{Value: 1})
	b := deepDictChain(n, MShellInt{Value: 1})
	c := deepDictChain(n, MShellInt{Value: 2})
	if eq, err := a.Equals(b); err != nil || !eq {
		t.Fatalf("equal deep values: got %v, %v", eq, err)
	}
	if eq, err := a.Equals(c); err != nil || eq {
		t.Fatalf("deep values that differ at the bottom: got %v, %v", eq, err)
	}
}

// Two values built separately, each sharing one dict under both keys at every
// level: 2^60 paths, 60 objects. Without the guard this never finishes.
func TestEqualsSharedParts(t *testing.T) {
	build := func(leaf int) *MShellDict {
		d := NewDict()
		d.Items["v"] = MShellInt{Value: leaf}
		for i := 0; i < 60; i++ {
			p := NewDict()
			p.Items["a"] = d
			p.Items["b"] = &Maybe{obj: d}
			d = p
		}
		return d
	}
	if eq, err := build(1).Equals(build(1)); err != nil || !eq {
		t.Fatalf("got %v, %v", eq, err)
	}
	if eq, err := build(1).Equals(build(2)); err != nil || eq {
		t.Fatalf("got %v, %v", eq, err)
	}
}

func TestEqualsCycles(t *testing.T) {
	cyc := func(leaf int) *MShellDict {
		d := NewDict()
		d.Items["self"] = d
		d.Items["v"] = MShellInt{Value: leaf}
		return d
	}
	if eq, err := cyc(1).Equals(cyc(1)); err != nil || !eq {
		t.Fatalf("two dicts that contain themselves: got %v, %v", eq, err)
	}
	if eq, err := cyc(1).Equals(cyc(2)); err != nil || eq {
		t.Fatalf("two cyclic dicts that differ: got %v, %v", eq, err)
	}
}

func TestEqualsKeepsItsRules(t *testing.T) {
	dict := func(k string, v MShellObject) *MShellDict {
		d := NewDict()
		d.Items[k] = v
		return d
	}
	// Values of different kinds in a dict are unequal, not an error.
	if eq, err := dict("a", MShellInt{Value: 1}).Equals(dict("a", MShellString{Content: "x"})); err != nil || eq {
		t.Fatalf("got %v, %v", eq, err)
	}
	// Lists have no equality, inside a dict or a Maybe too, even the same list.
	l := intList(1)
	if _, err := dict("a", l).Equals(dict("a", l)); err == nil {
		t.Fatal("a list inside a dict compared without an error")
	}
	if _, err := (Maybe{obj: l}).Equals(&Maybe{obj: l}); err == nil {
		t.Fatal("a list inside a Maybe compared without an error")
	}
	withList := dict("a", l)
	if _, err := withList.Equals(withList); err == nil {
		t.Fatal("a dict holding a list compared with itself without an error")
	}
	// Values are compared in sorted key order, so the list under "a" is met
	// before the difference under "b" every time.
	x := NewDict()
	x.Items["a"] = intList(1)
	x.Items["b"] = MShellInt{Value: 1}
	y := NewDict()
	y.Items["a"] = intList(1)
	y.Items["b"] = MShellInt{Value: 2}
	for i := 0; i < 20; i++ {
		if _, err := x.Equals(y); err == nil {
			t.Fatal("compared b before a")
		}
	}
	// A Maybe is unequal to anything that is not a Maybe.
	if eq, err := (Maybe{}).Equals(MShellInt{Value: 1}); err != nil || eq {
		t.Fatalf("got %v, %v", eq, err)
	}
}

func TestRenderCycles(t *testing.T) {
	l := intList(1)
	l.Items = append(l.Items, l)
	s, cycled := renderValueDetect(l, flavorDebug)
	if !cycled || s != "[1 <cycle>]" {
		t.Fatalf("got %q, %v", s, cycled)
	}
	if _, err := renderOrError(l, flavorJson); err == nil {
		t.Fatal("JSON of a cyclic list did not fail")
	}

	// One list reached along two paths is not a cycle.
	shared := intList(2)
	outer := NewList(2)
	outer.Items[0] = shared
	outer.Items[1] = &Maybe{obj: shared}
	s, cycled = renderValueDetect(outer, flavorJson)
	if cycled || s != "[[2], [2]]" {
		t.Fatalf("got %q, %v", s, cycled)
	}

	// A grid cell that holds its own grid.
	g := &MShellGrid{RowCount: 1}
	cell := NewList(1)
	cell.Items[0] = g
	g.Columns = []*GridColumn{{Name: "c", GenericData: []MShellObject{cell}}}
	if _, cycled := renderValueDetect(g, flavorJson); !cycled {
		t.Fatal("a grid that contains itself was not reported")
	}
	// Two rows of one grid, one inside the other, are not a cycle.
	g2 := &MShellGrid{RowCount: 2}
	g2.Columns = []*GridColumn{{Name: "c", GenericData: []MShellObject{MShellInt{Value: 1}, nil}}}
	g2.Columns[0].GenericData[1] = &MShellGridRow{Grid: g2, RowIndex: 0}
	if s, cycled := renderValueDetect(&MShellGridRow{Grid: g2, RowIndex: 1}, flavorJson); cycled || s != `{"c": {"c": 1}}` {
		t.Fatalf("got %q, %v", s, cycled)
	}
}

// enumChain is n nested enum values, alternating with Maybe:
// node(Just(node(Just(... leaf ...)))).
func enumChain(n int, leaf int) MShellObject {
	var v MShellObject = &MShellEnum{EnumName: "T", Member: "leaf", Payload: []MShellObject{MShellInt{Value: leaf}}}
	for i := 0; i < n; i++ {
		v = &MShellEnum{EnumName: "T", Member: "node", MemberIndex: 1, Payload: []MShellObject{&Maybe{obj: v}}}
	}
	return v
}

func TestEnumDeepValue(t *testing.T) {
	const n = 300_000
	a, b, c := enumChain(n, 1), enumChain(n, 1), enumChain(n, 2)
	for _, flavor := range []renderFlavor{flavorStr, flavorDebug, flavorJson} {
		if s, cycled := renderValueDetect(a, flavor); cycled || !strings.Contains(s, "leaf") {
			t.Fatalf("flavor %d: cycled=%v", flavor, cycled)
		}
	}
	if eq, err := a.Equals(b); err != nil || !eq {
		t.Fatalf("equal chains: %v, %v", eq, err)
	}
	if eq, err := a.Equals(c); err != nil || eq {
		t.Fatalf("chains that differ at the bottom: %v, %v", eq, err)
	}
}

// Two trees built separately, each reusing one subtree for both children at
// every level: 2^60 paths through 60 values.
func TestEnumSharedSubtrees(t *testing.T) {
	build := func(leaf int) MShellObject {
		var v MShellObject = &MShellEnum{EnumName: "T", Member: "leaf", Payload: []MShellObject{MShellInt{Value: leaf}}}
		for i := 0; i < 60; i++ {
			v = &MShellEnum{EnumName: "T", Member: "node", MemberIndex: 1, Payload: []MShellObject{v, v}}
		}
		return v
	}
	if eq, err := build(1).Equals(build(1)); err != nil || !eq {
		t.Fatalf("got %v, %v", eq, err)
	}
	if eq, err := build(1).Equals(build(2)); err != nil || eq {
		t.Fatalf("got %v, %v", eq, err)
	}
}

func TestEnumRender(t *testing.T) {
	leaf := func(n int) *MShellEnum {
		return &MShellEnum{EnumName: "T", Member: "leaf", Payload: []MShellObject{MShellInt{Value: n}}}
	}
	node := &MShellEnum{EnumName: "T", Member: "node", MemberIndex: 1, Payload: []MShellObject{leaf(1), MShellString{Content: "a b"}}}
	dot := &MShellEnum{EnumName: "T", Member: "dot", MemberIndex: 2}
	cases := []struct {
		v      MShellObject
		flavor renderFlavor
		want   string
	}{
		{node, flavorStr, "node(leaf(1) a b)"},
		{node, flavorDebug, `node(leaf(1) "a b")`},
		{node, flavorJson, `{"node": [{"leaf": 1}, "a b"]}`},
		{dot, flavorStr, "dot"},
		{dot, flavorJson, `"dot"`},
		{leaf(5), flavorJson, `{"leaf": 5}`},
	}
	for _, tc := range cases {
		if got := renderValue(tc.v, tc.flavor); got != tc.want {
			t.Errorf("flavor %d: got %q, want %q", tc.flavor, got, tc.want)
		}
	}
	// Different members or enums are unequal; a payload of another kind is
	// unequal, not an error.
	other := &MShellEnum{EnumName: "U", Member: "leaf", Payload: []MShellObject{MShellInt{Value: 1}}}
	if eq, err := leaf(1).Equals(other); err != nil || eq {
		t.Fatalf("different enums: %v, %v", eq, err)
	}
	str := &MShellEnum{EnumName: "T", Member: "leaf", Payload: []MShellObject{MShellString{Content: "1"}}}
	if eq, err := leaf(1).Equals(str); err != nil || eq {
		t.Fatalf("payloads of different kinds: %v, %v", eq, err)
	}
}
