package main

import (
	"strings"
	"testing"
)

// validateFor validates value against the type typ, with the declarations
// decls, as `tryAs typ` would.
func validateFor(t *testing.T, decls string, value MShellObject, typ string) (bool, string) {
	t.Helper()
	file, err := parseMShellInput(decls+"\n0 tryAs "+typ, &TokenFile{"test"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var node *MShellTryAs
	for _, item := range file.Items {
		if n, ok := item.(*MShellTryAs); ok {
			node = n
		}
	}
	state := &EvalState{}
	if err := state.RegisterDeclarations(file.Items, file.Definitions); err != nil {
		return false, err.Error()
	}
	return validateMsg(state, value, node.Target, &node.resolved)
}

func mustValidate(t *testing.T, decls string, value MShellObject, typ string, want bool) {
	t.Helper()
	got, msg := validateFor(t, decls, value, typ)
	if msg != "" {
		t.Fatalf("%s: %s", typ, msg)
	}
	if got != want {
		t.Fatalf("%s: got %v, want %v", typ, got, want)
	}
}

func strList(xs ...string) *MShellList {
	l := NewList(len(xs))
	for i, x := range xs {
		l.Items[i] = MShellString{Content: x}
	}
	return l
}

// tryAs gives `ok` with the value itself: validation never copies.
func TestTryAsKeepsTheValue(t *testing.T) {
	file, err := parseMShellInput(`"[[1], [2, 3]]" parseJson tryAs [[int]]`, &TokenFile{"test"})
	if err != nil {
		t.Fatal(err)
	}
	state := &EvalState{CallStack: make([]CallStackItem, 0, 10)}
	stack := MShellStack{}
	value := NewList(0)
	stack.Push(value)
	node := file.Items[len(file.Items)-1].(*MShellTryAs)
	if r := state.processTryAs(node, &stack); r != nil {
		t.Fatalf("tryAs failed")
	}
	top, _ := stack.Pop()
	r, ok := top.(*MShellEnum)
	if !ok || r.EnumName != "Result" || r.Member != "ok" || r.Payload[0] != MShellObject(value) {
		t.Fatalf("tryAs did not give ok with the same list: %#v", top)
	}
}

// explainFor validates value against typ, with the declarations decls, and
// returns why it does not conform.
func explainFor(t *testing.T, decls string, value MShellObject, typ string) string {
	t.Helper()
	file, err := parseMShellInput(decls+"\n0 tryAs "+typ, &TokenFile{"test"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	node := file.Items[len(file.Items)-1].(*MShellTryAs)
	state := &EvalState{}
	if err := state.RegisterDeclarations(file.Items, file.Definitions); err != nil {
		t.Fatal(err)
	}
	ok, why, err := state.tryValidate(value, node.Target, &node.resolved)
	if err != nil || ok {
		t.Fatalf("%s: ok %v, err %v", typ, ok, err)
	}
	return why
}

// A failed tryAs says where the first failing part is, also in a value that
// contains itself or shares parts, and names the same part every time.
func TestTryAsExplains(t *testing.T) {
	self := NewList(0)
	self.Items = []MShellObject{self, MShellInt{Value: 1}, MShellString{Content: "x"}}
	if got, want := explainFor(t, "type T = [T | int]", self, "T"),
		`not of type T: at [2], expected int | T, found str "x"`; got != want {
		t.Fatalf("cycle:\n got %s\nwant %s", got, want)
	}

	// Two bad keys: the first in key order is named.
	d := NewDict()
	for _, k := range []string{"c", "a", "b"} {
		d.Items[k] = MShellString{Content: k}
	}
	d.Items["b"] = MShellInt{Value: 1}
	shared := NewList(0)
	shared.Items = []MShellObject{d, d}
	for i := 0; i < 20; i++ {
		if got, want := explainFor(t, "", shared, "[{str: str}]"),
			`not of type [{str: str}]: at [0].b, expected str, found int 1`; got != want {
			t.Fatalf("keys:\n got %s\nwant %s", got, want)
		}
	}
}

// Items, keys and payloads are checked in order: the first bad one is named.
func TestTryAsExplainsInOrder(t *testing.T) {
	l := NewList(0)
	l.Items = []MShellObject{MShellInt{Value: 1}, MShellString{Content: "a"}, MShellString{Content: "b"}}
	if got, want := explainFor(t, "", l, "[int]"),
		`not of type [int]: at [1], expected int, found str "a"`; got != want {
		t.Fatalf("list:\n got %s\nwant %s", got, want)
	}

	d := NewDict()
	for _, k := range []string{"d", "c", "b", "a"} {
		d.Items[k] = MShellInt{Value: 1}
	}
	d.Items["b"], d.Items["c"] = MShellString{Content: "b"}, MShellString{Content: "c"}
	for range 20 {
		if got, want := explainFor(t, "", d, "{str: int}"),
			`not of type {str: int}: at .b, expected int, found str "b"`; got != want {
			t.Fatalf("keys:\n got %s\nwant %s", got, want)
		}
	}

	e := &MShellEnum{EnumName: "P", Member: "pair", Payload: []MShellObject{MShellString{Content: "x"}, MShellString{Content: "y"}}}
	if got, want := explainFor(t, "enum P = pair int int end", e, "P"),
		`not of type P: at (pair 0), expected int, found str "x"`; got != want {
		t.Fatalf("payload:\n got %s\nwant %s", got, want)
	}
}

// A deep value names the ends of its path, not every step.
func TestTryAsExplainsDeepValue(t *testing.T) {
	var x MShellObject = MShellString{Content: "bad"}
	for range 100_000 {
		l := NewList(1)
		l.Items[0] = x
		x = l
	}
	ends := strings.Repeat("[0]", pathEnds)
	if got, want := explainFor(t, "type T = [T] | int", x, "T"),
		`not of type T: at `+ends+`...`+ends+`, expected T (int | [T]), found str "bad"`; got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	// A path of exactly 2*pathEnds steps is written in full.
	x = MShellString{Content: "bad"}
	for range 2 * pathEnds {
		l := NewList(1)
		l.Items[0] = x
		x = l
	}
	if got, want := explainFor(t, "type T = [T] | int", x, "T"),
		`not of type T: at `+ends+ends+`, expected T (int | [T]), found str "bad"`; got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// Explaining a failure takes dict keys in another order than validating
// did, so it may run out of steps where validating did not: the value still
// does not conform, and the reason only names the type.
func TestTryAsExplainOutOfSteps(t *testing.T) {
	saved := validateBudget
	validateBudget = 100
	defer func() { validateBudget = saved }()
	file, err := parseMShellInput("0 tryAs [[int]]", &TokenFile{"test"})
	if err != nil {
		t.Fatal(err)
	}
	node := file.Items[len(file.Items)-1].(*MShellTryAs)
	state := &EvalState{}
	env := state.runtimeTypeEnv()
	defer env.mu.Unlock()
	env.resolveTarget(node.Target, &node.resolved)
	l := NewList(0)
	l.Items = []MShellObject{intList(make([]int, 200)...), strList("x")}
	if got, want := env.explainFailure(l, node.resolved.t), "not of type [[int]]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestValidateBasics(t *testing.T) {
	decls := `
type Person = {name: str, age: int, friends: [Person]}
type Config = {url: str, timeout?: float}
enum Shape = circle float | rect float float end
enum Box[a] = box [a] | empty end
`
	d := NewDict()
	d.Items["url"] = MShellString{Content: "x"}
	mustValidate(t, decls, d, "Config", true)
	d.Items["timeout"] = MShellInt{Value: 1}
	mustValidate(t, decls, d, "Config", false)
	d.Items["timeout"] = MShellFloat{Value: 1}
	mustValidate(t, decls, d, "Config", true)
	d.Items["other"] = MShellInt{Value: 1}
	mustValidate(t, decls, d, "Config", true) // a written shape is open
	mustValidate(t, decls, d, "{str: str | int | float}", true)
	mustValidate(t, decls, d, "{str: str}", false)

	mustValidate(t, decls, intList(1, 2), "[int]", true)
	mustValidate(t, decls, intList(1, 2), "[str]", false)
	mustValidate(t, decls, intList(), "[str]", true)
	mustValidate(t, decls, intList(1), "Json", true)
	mustValidate(t, decls, intList(1), "Toml", true)
	// TOML has no null.
	nulls := NewList(0)
	nulls.Items = append(nulls.Items, MShellNull{})
	mustValidate(t, decls, nulls, "Json", true)
	mustValidate(t, decls, nulls, "Toml", false)
	mustValidate(t, decls, strList("a"), "[int] | str", false)

	// A list whose stdout or stderr goes somewhere is a command, not a
	// list; one with `<` or `&` is still a list, as the checker types it.
	cmd := strList("ls")
	cmd.StdoutBehavior = STDOUT_COMPLETE
	mustValidate(t, decls, cmd, "[str]", false)
	in := strList("cat")
	in.StdinBehavior, in.StandardInputContents, in.RunInBackground = STDIN_CONTENT, "x", true
	mustValidate(t, decls, in, "[str]", true)

	circle := &MShellEnum{EnumName: "Shape", Member: "circle", MemberIndex: 0, Payload: []MShellObject{MShellFloat{Value: 1}}}
	mustValidate(t, decls, circle, "Shape", true)
	mustValidate(t, decls, circle, "Box[int]", false)
	bad := &MShellEnum{EnumName: "Shape", Member: "circle", MemberIndex: 0, Payload: []MShellObject{MShellInt{Value: 1}}}
	mustValidate(t, decls, bad, "Shape", false)
	box := &MShellEnum{EnumName: "Box", Member: "box", MemberIndex: 0, Payload: []MShellObject{intList(1)}}
	mustValidate(t, decls, box, "Box[int]", true)
	mustValidate(t, decls, box, "Box[int | str]", true)
	mustValidate(t, decls, box, "Box[str]", false)

	mustValidate(t, decls, &Maybe{}, "Maybe[[int]]", true)
	mustValidate(t, decls, &Maybe{obj: intList(1)}, "Maybe[[str]]", false)
	mustValidate(t, decls, &MShellQuotation{}, "int", false)
}

// A cycle validates against a type it has (the pair is met again), but not
// against one it does not have: `j = [j]` against [[int]] meets j again at
// [int], a different question.
func TestValidateCycles(t *testing.T) {
	j := NewList(0)
	j.Items = append(j.Items, j)
	mustValidate(t, "type L = [L]", j, "[Json]", true)
	mustValidate(t, "type L = [L]", j, "L", true)
	mustValidate(t, "type L = [L]", j, "[[int]]", false)

	d := NewDict()
	d.Items["self"] = d
	mustValidate(t, "type D = {self: D}", d, "D", true)
	mustValidate(t, "type D = {self: D}", d, "{self: {self: int}}", false)
}

// A value 300,000 levels deep is walked without the Go stack.
func TestValidateDeepValue(t *testing.T) {
	var v MShellObject = MShellInt{Value: 1}
	for range 300_000 {
		l := NewList(1)
		l.Items[0] = v
		v = l
	}
	mustValidate(t, "", v, "Json", true)
	var m MShellObject = MShellInt{Value: 1}
	for range 300_000 {
		m = &Maybe{obj: m}
	}
	mustValidate(t, "type M = Maybe[M] | int", m, "M", true)
}

// 2^60 paths through 60 shared values are walked once per value.
func TestValidateSharedParts(t *testing.T) {
	var l MShellObject = intList(1)
	for range 60 {
		next := NewList(2)
		next.Items[0], next.Items[1] = l, l
		l = next
	}
	mustValidate(t, "type T = int | [T]", l, "T", true)
	var e MShellObject = &MShellEnum{EnumName: "Tree", Member: "leaf", Payload: []MShellObject{MShellInt{Value: 1}}}
	for range 60 {
		e = &MShellEnum{EnumName: "Tree", Member: "node", MemberIndex: 1, Payload: []MShellObject{e, e}}
	}
	mustValidate(t, "enum Tree = leaf int | node Tree Tree end", e, "Tree", true)
}

// One list referenced many times from one parent is walked once: 70,000
// references to a list of 1,000 ints, and three levels of such sharing.
func TestValidateRepeatedReferences(t *testing.T) {
	x := intList(make([]int, 1000)...)
	p := NewList(70_000)
	for i := range p.Items {
		p.Items[i] = x
	}
	mustValidate(t, "", p, "[[int]]", true)
	p2 := NewList(1000)
	for i := range p2.Items {
		p2.Items[i] = x
	}
	q := NewList(100)
	for i := range q.Items {
		q.Items[i] = p2
	}
	mustValidate(t, "", q, "[[[int]]]", true)
	// A small dict holding a big list, shared by many slots.
	d := NewDict()
	d.Items["xs"] = x
	r := NewList(70_000)
	for i := range r.Items {
		r.Items[i] = d
	}
	mustValidate(t, "", r, "[{xs: [int]}]", true)
}

// Running out of steps is an error, not none.
func TestValidateBudget(t *testing.T) {
	saved := validateBudget
	validateBudget = 100
	defer func() { validateBudget = saved }()
	ok, msg := validateFor(t, "", intList(make([]int, 200)...), "[int]")
	if ok || !strings.Contains(msg, "took more than 100 steps") {
		t.Fatalf("got %v, %q", ok, msg)
	}
	if ok, msg := validateFor(t, "", intList(1, 2), "[int]"); !ok || msg != "" {
		t.Fatalf("a small value after a stopped one: %v, %q", ok, msg)
	}
}

// As REPL lines: a line whose declarations are refused adds none of them,
// so later lines can declare the names properly; a tryAs that named a type
// before it was declared finds it once it is.
func TestValidateDeclarationLines(t *testing.T) {
	state := &EvalState{}
	line := func(src string) error {
		t.Helper()
		file, err := parseMShellInput(src, &TokenFile{"repl"})
		if err != nil {
			t.Fatal(err)
		}
		return state.RegisterDeclarations(file.Items, file.Definitions)
	}
	tryAs := func(src string) *MShellTryAs {
		t.Helper()
		file, err := parseMShellInput("0 tryAs "+src, &TokenFile{"repl"})
		if err != nil {
			t.Fatal(err)
		}
		return file.Items[1].(*MShellTryAs)
	}
	early := tryAs("U")
	if _, msg := validateMsg(state, intList(1), early.Target, &early.resolved); !strings.Contains(msg, "unknown type 'U'") {
		t.Fatalf("before U: %q", msg)
	}
	if err := line("type U = [foo]"); err == nil || !strings.Contains(err.Error(), "unknown type 'foo'") {
		t.Fatalf("a bad body: %v", err)
	}
	if ok, msg := validateMsg(state, MShellInt{Value: 5}, tryAs("int").Target, &tryAs("int").resolved); !ok || msg != "" {
		t.Fatalf("after a refused line: %v %q", ok, msg)
	}
	if err := line("type U = [int]"); err != nil {
		t.Fatalf("declaring U again: %v", err)
	}
	if ok, msg := validateMsg(state, intList(1), early.Target, &early.resolved); !ok || msg != "" {
		t.Fatalf("the early tryAs after U: %v %q", ok, msg)
	}
	if err := line("type U = [str]"); err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("U twice: %v", err)
	}
}

func TestValidateDeclarationErrors(t *testing.T) {
	if _, msg := validateFor(t, "type A = A", MShellInt{Value: 1}, "int"); !strings.Contains(msg, "refers to itself") {
		t.Fatalf("got %q", msg)
	}
	if _, msg := validateFor(t, "", MShellInt{Value: 1}, "Nope"); !strings.Contains(msg, "unknown type 'Nope'") {
		t.Fatalf("got %q", msg)
	}
	// Two members of one kind are refused, in a declaration or a target.
	if _, msg := validateFor(t, "type A = [int] | [str]", NewList(0), "A"); !strings.Contains(msg, "two members of the same kind") {
		t.Fatalf("got %q", msg)
	}
	if _, msg := validateFor(t, "", NewList(0), "[int] | [str]"); !strings.Contains(msg, "two members of the same kind") {
		t.Fatalf("got %q", msg)
	}
}

// 10,000 records of a name, an age and a list of tags, against
// [{name: str, age: int, tags: [str]}].
func BenchmarkValidateRecords(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := range 10_000 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"name": "person", "age": 30, "tags": ["a", "b", "c"]}`)
	}
	sb.WriteString("]")
	decoded, err := decodeJson([]byte(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	value := ParseJsonObjToMshell(decoded)
	file, err := parseMShellInput("type P = {name: str, age: int, tags: [str]}\n0 tryAs [P]", &TokenFile{"bench"})
	if err != nil {
		b.Fatal(err)
	}
	node := file.Items[len(file.Items)-1].(*MShellTryAs)
	state := &EvalState{}
	if err := state.RegisterDeclarations(file.Items, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if ok, msg := validateMsg(state, value, node.Target, &node.resolved); !ok || msg != "" {
			b.Fatalf("%v %q", ok, msg)
		}
	}
}

func BenchmarkValidateJson(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := range 10_000 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"name": "person", "age": 30, "tags": ["a", "b", "c"]}`)
	}
	sb.WriteString("]")
	decoded, err := decodeJson([]byte(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	value := ParseJsonObjToMshell(decoded)
	file, err := parseMShellInput("0 tryAs Json", &TokenFile{"bench"})
	if err != nil {
		b.Fatal(err)
	}
	node := file.Items[len(file.Items)-1].(*MShellTryAs)
	state := &EvalState{}
	b.ReportAllocs()
	for b.Loop() {
		if ok, msg := validateMsg(state, value, node.Target, &node.resolved); !ok || msg != "" {
			b.Fatalf("%v %q", ok, msg)
		}
	}
}

func BenchmarkRuntimeTypesNew(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		newRuntimeTypes()
	}
}

// validateMsg is validateValue with its error as a message, "" for none.
func validateMsg(state *EvalState, value MShellObject, target MShellParseItem, cache *runtimeTarget) (bool, string) {
	ok, err := state.validateValue(value, target, cache)
	if err != nil {
		return ok, err.Error()
	}
	return ok, ""
}
