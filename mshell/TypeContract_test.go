package main

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Builtin contract tests (ai/type-system-plan.md, stage 7; design doc
// "The builtin contract"). For each candidate signature in the core
// checker's builtin table, the test makes random inputs of the declared
// types, runs the builtin, and checks the contract:
//
//   - the runtime never reports a type mismatch (a checked failure is fine);
//   - every output validates against its declared type;
//   - every input passed shared (through a variable) still validates
//     against its declared type afterwards;
//   - an output the checker would mark fresh is a tree of new objects:
//     no list, dict or grid in it is reached twice, from another output,
//     or from any variable.
//
// MSH_CONTRACT_TRIALS sets the number of trials per candidate (default 12).

// contractSkip are builtins the test does not run: they change the file
// system, the process or its environment, wait for input, or leave.
var contractSkip = map[string]bool{
	"writeFile": true, "appendFile": true, "cp": true, "mv": true, "hardLink": true,
	"rm": true, "rmf": true, "mkdir": true, "mkdirp": true, "cd": true, "cdh": true, "cdp": true,
	"mshFileManager": true, "clip": true, "sleep": true, "stdin": true, "prompt": true,
	"setenv": true, "unsetenv": true, "exit": true, "psub": true, "tempFile": true, "tempFileExt": true,
	"httpGet": true, "httpPost": true, "binPaths": true, "glob": true,
	"zipDirInc": true, "zipDirExc": true, "tarDirInc": true, "tarDirExc": true, "zipPack": true, "tarPack": true,
	"zipExtract": true, "tarExtract": true, "zipExtractEntry": true, "tarExtractEntry": true,
}

// contractTokens are the builtins registered by token type, with their text.
var contractTokens = map[TokenType]string{
	PLUS: "+", MINUS: "-", ASTERISK: "*", LESSTHAN: "<", GREATERTHAN: ">",
	LESSTHANOREQUAL: "<=", GREATERTHANOREQUAL: ">=", EQUALS: "=", NOTEQUAL: "!=",
	STR: "str", NOT: "not", QUESTION: "?", STOP_ON_ERROR: "soe",
}

type contractGen struct {
	rng  *rand.Rand
	c    *coreChecker
	ar   *TypeArena
	pool []TypeId // types a generic may be instantiated with
	// schemas are the exact records a grid schema generic may be.
	schemas []TypeId
}

var contractInts = []string{"0", "1", "2", "3", "-1", "7", "36", "255", "1000", "-13"}
var contractFloats = []string{"0.0", "1.5", "-2.25", "3.0", "100.125", "0.001"}
var contractStrs = []string{"", "a", "hello world", "12", "-7", "3.5", "1e3", "true", "x,y,z", "a\nb\n",
	"  pad  ", "é☃", `{"a": 1}`, "[1, 2]", "2024-01-02", "a.b/c.txt", "(", ".*", "ff", "zz", "null", "a\tb"}
var contractPaths = []string{"`a.txt`", "`dir/b.csv`", "`/no/such/file`", "`.`", "`x y.tar.gz`"}
var contractDates = []string{"2024-01-02T03:04:05", "2020-02-29", "1999-12-31T23:59", "2001-07-04T12:00:00"}

// mshStr writes s as an mshell string literal.
func mshStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (g *contractGen) pick(xs []string) string { return xs[g.rng.Intn(len(xs))] }

// value writes the source of a new value of type t to b, and reports
// whether t has values the test can write.
func (g *contractGen) value(b *strings.Builder, t TypeId, depth int) bool {
	ar := g.ar
	switch ar.Kind(t) {
	case TKPrim:
		switch t {
		case TidInt:
			b.WriteString(g.pick(contractInts))
		case TidFloat:
			b.WriteString(g.pick(contractFloats))
		case TidStr:
			b.WriteString(mshStr(g.pick(contractStrs)))
		case TidBool:
			b.WriteString(g.pick([]string{"true", "false"}))
		case TidPath:
			b.WriteString(g.pick(contractPaths))
		case TidDateTime:
			b.WriteString(g.pick(contractDates))
		case TidBytes:
			b.WriteString(mshStr(g.pick(contractStrs)) + " utf8Bytes")
		case TidNull:
			b.WriteString("null")
		case TidUnknown:
			return g.value(b, g.pool[g.rng.Intn(len(g.pool))], depth)
		default:
			return false
		}
		return true
	case TKList:
		elem := TypeId(ar.Node(t).A)
		n := g.rng.Intn(4)
		if depth <= 0 || elem == TidBottom {
			n = 0
		}
		b.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			if !g.value(b, elem, depth-1) {
				return false
			}
		}
		b.WriteByte(']')
		return true
	case TKRecord:
		return g.record(b, ar.Record(t), depth)
	case TKUnion:
		members := ar.UnionMembers(t)
		if depth <= 0 {
			// Prefer a member with small values, so recursive types end.
			var small []TypeId
			for _, m := range members {
				if k := ar.Kind(m); k == TKPrim || k == TKEnum {
					small = append(small, m)
				}
			}
			if len(small) > 0 {
				members = small
			}
		}
		return g.value(b, members[g.rng.Intn(len(members))], depth)
	case TKAlias:
		return g.value(b, ar.AliasBody(t), depth)
	case TKEnum:
		n := ar.Node(t)
		decl := ar.EnumDecl(n.A)
		args := ar.EnumArgs(t)
		ctors := decl.Ctors
		if depth <= 0 {
			// A constructor with no payload ends a recursive value.
			for _, ct := range decl.Ctors {
				if len(ct.Payload) == 0 {
					ctors = []EnumCtor{ct}
					break
				}
			}
		}
		for tries := 0; tries < 4; tries++ {
			ct := ctors[g.rng.Intn(len(ctors))]
			var sub strings.Builder
			ok := true
			for _, p := range ct.Payload {
				if !g.value(&sub, g.c.rel.SubstParams(p, args), depth-1) {
					ok = false
					break
				}
				sub.WriteByte(' ')
			}
			if ok {
				b.WriteString(sub.String())
				b.WriteString(g.c.names.Name(ct.Name))
				return true
			}
		}
		return false
	case TKQuote:
		// A quote that drops its inputs and pushes values of its outputs.
		sig := ar.QuoteSig(t)
		if sig.Diverges {
			return false
		}
		b.WriteByte('(')
		for range sig.Inputs {
			b.WriteString("drop ")
		}
		for _, o := range sig.Outputs {
			if !g.value(b, o, depth-1) {
				return false
			}
			b.WriteByte(' ')
		}
		b.WriteByte(')')
		return true
	case TKGrid, TKGridView, TKGridRow:
		return g.grid(b, ar.Kind(t), ar.Record(ar.GridRecord(t)), depth)
	}
	return false
}

// typeSrc writes t in type syntax, when it has one: grids, unknown and
// exact shapes do not (an exact shape is written open, which a new
// literal may be retyped to).
func (g *contractGen) typeSrc(t TypeId) (string, bool) {
	ar := g.ar
	switch ar.Kind(t) {
	case TKPrim:
		switch t {
		case TidInt, TidFloat, TidStr, TidBool, TidPath, TidDateTime, TidBytes, TidNull:
			return FormatType(ar, g.c.names, t), true
		}
		return "", false
	case TKList:
		e, ok := g.typeSrc(TypeId(ar.Node(t).A))
		return "[" + e + "]", ok
	case TKAlias:
		return FormatType(ar, g.c.names, t), true
	case TKUnion:
		var parts []string
		for _, m := range ar.UnionMembers(t) {
			s, ok := g.typeSrc(m)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " | "), true
	case TKEnum:
		decl := ar.EnumDecl(ar.Node(t).A)
		s := g.c.names.Name(decl.Name)
		args := ar.EnumArgs(t)
		if len(args) == 0 {
			return s, true
		}
		var parts []string
		for _, a := range args {
			if a == TidBottom {
				return "", false
			}
			x, ok := g.typeSrc(a)
			if !ok {
				return "", false
			}
			parts = append(parts, x)
		}
		return s + "[" + strings.Join(parts, " ") + "]", true
	case TKQuote:
		sig := ar.QuoteSig(t)
		var b strings.Builder
		b.WriteByte('(')
		for _, x := range sig.Inputs {
			s, ok := g.typeSrc(x)
			if !ok {
				return "", false
			}
			b.WriteString(s + " ")
		}
		b.WriteString("--")
		if sig.Diverges {
			b.WriteString(" never")
		}
		for _, x := range sig.Outputs {
			s, ok := g.typeSrc(x)
			if !ok {
				return "", false
			}
			b.WriteString(" " + s)
		}
		b.WriteByte(')')
		return b.String(), true
	case TKRecord:
		rec := ar.Record(t)
		var parts []string
		for _, f := range rec.Fields {
			s, ok := "", true
			switch f.Status {
			case FieldRequired:
				s, ok = g.typeSrc(f.Type)
				s = mshStr(g.c.names.Name(f.Name)) + ": " + s
			case FieldOptional:
				s, ok = g.typeSrc(f.Type)
				s = mshStr(g.c.names.Name(f.Name)) + "?: " + s
			default:
				return "", false
			}
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		switch rec.Rest.Status {
		case FieldOpen, FieldAbsent:
		case FieldOptional:
			s, ok := g.typeSrc(rec.Rest.Type)
			if !ok {
				return "", false
			}
			parts = append(parts, "*: "+s)
		case FieldDeletable:
			if len(parts) > 0 {
				return "", false
			}
			s, ok := g.typeSrc(rec.Rest.Type)
			return "{str: " + s + "}", ok
		}
		return "{" + strings.Join(parts, ", ") + "}", true
	}
	return "", false
}

// key writes a dict key: every key is quoted, since some names (`x`) are
// words.
func (g *contractGen) key(b *strings.Builder, name string) {
	b.WriteString(mshStr(name))
	b.WriteString(": ")
}

func (g *contractGen) record(b *strings.Builder, rec RecordType, depth int) bool {
	b.WriteByte('{')
	first := true
	sep := func() {
		if !first {
			b.WriteString(", ")
		}
		first = false
	}
	used := map[string]bool{}
	for _, f := range rec.Fields {
		name := g.c.names.Name(f.Name)
		used[name] = true
		switch f.Status {
		case FieldRequired:
		case FieldOptional, FieldDeletable:
			if depth <= 0 || g.rng.Intn(2) == 0 {
				continue
			}
		default:
			continue
		}
		sep()
		g.key(b, name)
		if !g.value(b, f.Type, depth-1) {
			return false
		}
	}
	extra := 0
	if depth > 0 {
		extra = g.rng.Intn(3)
	}
	for i := 0; i < extra; i++ {
		name := "k" + strconv.Itoa(i)
		if used[name] {
			continue
		}
		var t TypeId
		switch rec.Rest.Status {
		case FieldOptional, FieldDeletable:
			t = rec.Rest.Type
		case FieldOpen:
			t = g.pool[g.rng.Intn(len(g.pool))]
		default:
			continue
		}
		sep()
		g.key(b, name)
		if !g.value(b, t, depth-1) {
			return false
		}
	}
	b.WriteByte('}')
	return true
}

// grid writes a grid literal with the schema's required columns (and, for
// an open schema, a random column), then makes a view or a row of it.
func (g *contractGen) grid(b *strings.Builder, kind TypeKind, rec RecordType, depth int) bool {
	type col struct {
		name string
		t    TypeId
	}
	var cols []col
	for _, f := range rec.Fields {
		if f.Status == FieldRequired {
			cols = append(cols, col{g.c.names.Name(f.Name), f.Type})
		}
	}
	if rec.Rest.Status == FieldOpen || rec.Rest.Status == FieldOptional {
		t := TidStr
		if rec.Rest.Status == FieldOptional {
			t = rec.Rest.Type
		} else if g.rng.Intn(2) == 0 {
			t = TidInt
		}
		cols = append(cols, col{"c" + strconv.Itoa(g.rng.Intn(3)), t})
	}
	if len(cols) == 0 {
		return false
	}
	rows := 1 + g.rng.Intn(3)
	b.WriteString("[| ")
	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(c.name)
	}
	for r := 0; r < rows; r++ {
		b.WriteString("; ")
		for i, c := range cols {
			if i > 0 {
				b.WriteString(", ")
			}
			if !g.value(b, c.t, min(depth-1, 1)) {
				return false
			}
		}
	}
	b.WriteString(" |]")
	switch kind {
	case TKGridView:
		b.WriteString(" (drop true) filter")
	case TKGridRow:
		b.WriteString(" :0:")
	}
	return true
}

// contractObjects adds every list, dict and grid reachable from v to seen,
// counting each meeting. Quotes are not entered (freshness stops at
// quotes), nor are grid metadata dicts, which are read only.
func contractObjects(v MShellObject, seen map[any]int) {
	work := []MShellObject{v}
	for len(work) > 0 {
		o := work[len(work)-1]
		work = work[:len(work)-1]
		switch x := o.(type) {
		case *MShellList:
			seen[x]++
			if seen[x] == 1 {
				work = append(work, x.Items...)
			}
		case *MShellDict:
			seen[x]++
			if seen[x] == 1 {
				for _, item := range x.Items {
					work = append(work, item)
				}
			}
		case *MShellPipe:
			seen[x]++
			if seen[x] == 1 {
				work = append(work, &x.List)
			}
		case *MShellGrid:
			seen[x]++
			if seen[x] == 1 {
				for _, c := range x.Columns {
					work = append(work, c.GenericData...)
				}
			}
		case *MShellGridView:
			work = append(work, x.Source)
		case *MShellGridRow:
			work = append(work, x.Grid)
		case *Maybe:
			if x.obj != nil {
				work = append(work, x.obj)
			}
		case Maybe:
			if x.obj != nil {
				work = append(work, x.obj)
			}
		case *MShellEnum:
			work = append(work, x.Payload...)
		}
	}
}

// contractRun is the outcome of one run of a program.
type contractRun struct {
	ok       bool
	kind     FailureKind
	panicked string
	stack    MShellStack
	vars     map[string]MShellObject
	stderr   string
}

// testPbm is one path-bin manager for every program a test runs: making
// one scans PATH.
var testPbm = sync.OnceValue(func() IPathBinManager { return NewPathBinManager() })

// runProgram runs file with no type check, its output discarded, and its
// error messages read from errFile.
func runProgram(file *MShellFile, errFile *os.File) (run contractRun, err error) {
	state := EvalState{CallStack: make([]CallStackItem, 0, 10)}
	if err := state.RegisterDeclarations(file.Items, file.Definitions); err != nil {
		return run, err
	}
	stack := MShellStack{}
	vars := map[string]MShellObject{}
	context := ExecuteContext{Variables: vars, Pbm: testPbm(), StandardOutput: io.Discard, StandardError: io.Discard}
	callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
	errFile.Truncate(0)
	errFile.Seek(0, 0)
	func() {
		defer func() {
			if r := recover(); r != nil {
				run.panicked = fmt.Sprint(r)
			}
		}()
		result := state.Evaluate(file.Items, &stack, context, file.Definitions, callStackItem)
		run.ok = result.Success
		if !result.Success && !result.ExitCalled {
			run.kind = state.FailureKind
		}
	}()
	run.stack, run.vars = stack, vars
	if !run.ok || run.panicked != "" {
		errFile.Seek(0, 0)
		msg, _ := io.ReadAll(errFile)
		run.stderr = string(msg)
	}
	return run, nil
}

// withStderrFile points os.Stderr at a temporary file while f runs, since
// the evaluator writes failure messages there.
func withStderrFile(t *testing.T, f func(errFile *os.File)) {
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = errFile
	defer func() { os.Stderr = saved; errFile.Close() }()
	f(errFile)
}

func envTrials(name string, def int) int {
	if s := os.Getenv(name); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func newContractGen(seed int64) (*contractGen, *CoreBase) {
	base := NewCoreBase(nil, nil)
	c := base.newChecker()
	g := &contractGen{rng: rand.New(rand.NewSource(seed)), c: c, ar: c.arena}
	for _, src := range []string{"int", "str", "float", "bool", "path", "datetime", "[int]", "[str]", "{str: int}",
		"Maybe[int]", "int | str", "[[int]]", "{a: int, b?: str}", "Json", "Maybe[[str]]", "[int | str]"} {
		ast := builtinSigAST("(" + src + " -- )")
		parts := c.res.resolveSig(ast.inputs, ast.outputs)
		if len(c.res.errs) > 0 {
			panic("contract pool type " + src)
		}
		g.pool = append(g.pool, parts.ins[0])
	}
	field := func(name string, t TypeId) RecordField {
		return RecordField{Name: c.names.Intern(name), Status: FieldRequired, Type: t}
	}
	exact := RecordField{Status: FieldAbsent}
	g.schemas = []TypeId{
		c.arena.MakeRecord([]RecordField{field("a", TidInt)}, exact),
		c.arena.MakeRecord([]RecordField{field("a", TidInt), field("b", TidStr)}, exact),
		c.arena.MakeRecord([]RecordField{field("n", TidFloat), field("s", TidStr), field("d", TidDateTime)}, exact),
		c.arena.MakeRecord([]RecordField{field("k", TidStr), field("v", c.arena.MakeList(TidInt))}, exact),
	}
	return g, base
}

// contractCase is one candidate signature of one builtin. notList says
// the generic may not be a list: the walker uses appendBelow only when the
// value below is not one.
type contractCase struct {
	word    string
	sig     coreSig
	notList bool
}

func contractCases(g *contractGen) []contractCase {
	t := g.c.table
	var out []contractCase
	for id, sigs := range t.byName {
		name := g.c.names.Name(NameId(id))
		if contractSkip[name] {
			continue
		}
		for _, s := range sigs {
			out = append(out, contractCase{word: name, sig: s})
		}
	}
	for tt, sigs := range t.byToken {
		text, ok := contractTokens[TokenType(tt)]
		if !ok {
			continue
		}
		for _, s := range sigs {
			out = append(out, contractCase{word: text, sig: s})
		}
	}
	for _, s := range t.index {
		out = append(out, contractCase{word: ":0:", sig: s}, contractCase{word: ":-1:", sig: s})
	}
	for _, s := range t.slice {
		out = append(out, contractCase{word: "1:", sig: s}, contractCase{word: ":1", sig: s}, contractCase{word: "0:2", sig: s})
	}
	for _, s := range t.multi {
		out = append(out, contractCase{word: ":0:,1:", sig: s})
	}
	out = append(out, contractCase{word: "append", sig: t.appendBelow, notList: true})
	return out
}

// instantiate picks a type for each generic of sig: a schema for one that
// is a grid's schema, else one from the pool.
func (g *contractGen) instantiate(sig coreSig) []TypeId {
	args := make([]TypeId, len(sig.gens))
	schema := map[uint32]bool{}
	var find func(t TypeId)
	find = func(t TypeId) {
		switch g.ar.Kind(t) {
		case TKGrid, TKGridView, TKGridRow:
			if r := g.ar.GridRecord(t); g.ar.Kind(r) == TKParam {
				schema[g.ar.Node(r).A] = true
			}
		case TKList:
			find(TypeId(g.ar.Node(t).A))
		case TKQuote:
			q := g.ar.QuoteSig(t)
			for _, x := range q.Inputs {
				find(x)
			}
			for _, x := range q.Outputs {
				find(x)
			}
		case TKUnion:
			for _, m := range g.ar.UnionMembers(t) {
				find(m)
			}
		case TKRecord:
			rec := g.ar.Record(t)
			for _, f := range rec.Fields {
				if f.Type != TidNothing {
					find(f.Type)
				}
			}
		}
	}
	for _, x := range sig.ins {
		find(x)
	}
	for _, x := range sig.outs {
		find(x)
	}
	for i := range args {
		if schema[uint32(i)] {
			args[i] = g.schemas[g.rng.Intn(len(g.schemas))]
		} else {
			args[i] = g.pool[g.rng.Intn(len(g.pool))]
		}
	}
	return args
}

// validatable says whether the validator can check a value against t:
// it answers no for every quote and grid, and a command is not a type it
// knows.
func validatable(ar *TypeArena, t TypeId) bool {
	for _, k := range []TypeKind{TKQuote, TKGrid, TKGridView, TKGridRow, TKCommand, TKAbstract} {
		if typeMentions(ar, t, k) {
			return false
		}
	}
	return true
}

type contractFailure struct {
	word, sig, src, why string
}

func TestBuiltinContracts(t *testing.T) {
	trials := envTrials("MSH_CONTRACT_TRIALS", 12)
	g, base := newContractGen(1)
	env := &runtimeTypes{c: g.c}
	cases := contractCases(g)
	var failures []contractFailure
	runs, checked, rejected := 0, 0, 0
	perWord := map[string][2]int{} // runs, runs that succeeded
	withStderrFile(t, func(errFile *os.File) {
		for _, cc := range cases {
			sigText := FormatType(g.ar, g.c.names, g.ar.MakeQuote(QuoteSig{Inputs: cc.sig.ins, Outputs: cc.sig.outs, Diverges: cc.sig.diverges}))
			for trial := 0; trial < trials; trial++ {
				args := g.instantiate(cc.sig)
				if cc.notList && g.ar.Kind(args[0]) == TKList {
					continue
				}
				ins := make([]TypeId, len(cc.sig.ins))
				for i, x := range cc.sig.ins {
					ins[i] = g.c.rel.SubstParams(x, args)
				}
				outs := make([]TypeId, len(cc.sig.outs))
				for i, x := range cc.sig.outs {
					outs[i] = g.c.rel.SubstParams(x, args)
				}
				shared := trial%2 == 1
				var src strings.Builder
				ok := true
				for i, x := range ins {
					if !g.value(&src, x, 3) {
						ok = false
						break
					}
					// Pin the value at this instance, so the checker and
					// the test agree on its type (`[]` has many).
					if ts, ok := g.typeSrc(x); ok {
						src.WriteString(" as " + ts)
					}
					if shared {
						src.WriteString(" a" + strconv.Itoa(i) + "!\n")
					} else {
						src.WriteByte(' ')
					}
				}
				if !ok {
					continue
				}
				if shared {
					for i := range ins {
						src.WriteString("@a" + strconv.Itoa(i) + " ")
					}
				}
				src.WriteString(cc.word)
				text := src.String()
				file, err := parseMShellInput(text, &TokenFile{"contract.msh"})
				if err != nil {
					t.Fatalf("%s %s: the generated program does not parse: %v\n%s", cc.word, sigText, err, text)
				}
				// Only programs the checker accepts: the walker adds rules
				// to some table entries, and a program it rejects is not
				// one the contract covers.
				if _, ok := base.Check(file); !ok {
					rejected++
					continue
				}
				run, err := runProgram(file, errFile)
				if err != nil {
					t.Fatalf("%s: %v\n%s", cc.word, err, text)
				}
				runs++
				pw := perWord[cc.word]
				pw[0]++
				fail := func(why string) {
					failures = append(failures, contractFailure{cc.word, sigText, text, why})
				}
				switch {
				case run.panicked != "":
					fail("panic: " + run.panicked)
					continue
				case !run.ok && run.kind == TypeMismatch:
					fail("type mismatch: " + strings.TrimSpace(run.stderr))
					continue
				case !run.ok:
					perWord[cc.word] = pw
					continue
				}
				pw[1]++
				perWord[cc.word] = pw
				if cc.sig.diverges {
					continue
				}
				if len(run.stack) != len(outs) {
					fail(fmt.Sprintf("left %d values, the signature says %d", len(run.stack), len(outs)))
					continue
				}
				checked++
				for i, o := range outs {
					if !validatable(g.ar, o) {
						continue
					}
					if good, err := env.validate(run.stack[i], o); err != nil || !good {
						fail(fmt.Sprintf("output %d is %s, which is not a %s", i, run.stack[i].DebugString(), FormatType(g.ar, g.c.names, o)))
					}
				}
				if shared {
					for i, x := range ins {
						v := run.vars["a"+strconv.Itoa(i)]
						if v == nil || !validatable(g.ar, x) {
							continue
						}
						if good, err := env.validate(v, x); err != nil || !good {
							fail(fmt.Sprintf("shared input %d became %s, which is not a %s", i, v.DebugString(), FormatType(g.ar, g.c.names, x)))
						}
					}
				}
				// Freshness: what the checker would mark fresh here.
				reach := map[any]int{}
				for _, v := range run.vars {
					contractObjects(v, reach)
				}
				outSeen := map[any]int{}
				for i, o := range outs {
					fresh := cc.sig.newOut&(1<<i) != 0 || (cc.sig.keepOut&(1<<i) != 0 && !shared)
					if !fresh && cc.sig.newListOut&(1<<i) != 0 {
						fresh = g.c.newOverImmutable(o)
					}
					if !fresh || g.c.rel.Immutable(o) {
						continue
					}
					mine := map[any]int{}
					contractObjects(run.stack[i], mine)
					for obj, n := range mine {
						switch {
						case n > 1:
							fail(fmt.Sprintf("fresh output %d reaches a %T twice", i, obj))
						case reach[obj] > 0:
							fail(fmt.Sprintf("fresh output %d shares a %T with a variable", i, obj))
						case outSeen[obj] > 0:
							fail(fmt.Sprintf("fresh output %d shares a %T with another output", i, obj))
						}
					}
					for obj := range mine {
						outSeen[obj]++
					}
				}
			}
		}
	})
	words := make([]string, 0, len(perWord))
	for w := range perWord {
		words = append(words, w)
	}
	sort.Strings(words)
	var never []string
	for _, w := range words {
		if perWord[w][1] == 0 {
			never = append(never, w)
		}
	}
	t.Logf("%d candidates; %d programs the checker rejected; %d runs, %d ran to the end and were checked", len(cases), rejected, runs, checked)
	if len(never) > 0 {
		t.Logf("never ran to the end (every input made a checked failure): %s", strings.Join(never, " "))
	}
	seen := map[string]bool{}
	shown := 0
	for _, f := range failures {
		key := f.word + "|" + f.sig + "|" + f.why[:min(len(f.why), 40)]
		if seen[key] {
			continue
		}
		seen[key] = true
		if shown < 60 {
			t.Errorf("%s %s: %s\n--- program\n%s", f.word, f.sig, f.why, f.src)
		}
		shown++
	}
	if shown > 60 {
		t.Errorf("... and %d more", shown-60)
	}
}
