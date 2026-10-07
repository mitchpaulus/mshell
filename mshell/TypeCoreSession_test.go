package main

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sessionLine parses src as one REPL line and checks it in s.
func sessionLine(t *testing.T, s *CoreSession, src string) ([]string, bool) {
	t.Helper()
	file, err := NewMShellParser(NewLexer(src, nil)).ParseFile()
	if err != nil {
		t.Fatalf("parse error in %q: %v", src, err)
	}
	diags, ok := s.Check(file)
	var out []string
	for _, e := range diags {
		if e.Severity == SeverityError {
			out = append(out, e.Format(s.Arena(), s.Names()))
		}
	}
	return out, ok
}

// TestCoreSession runs REPL sessions line by line. A line is checked, then
// kept (as the REPL does before it runs it), or, when marked so, kept and
// then reported as stopped by a runtime error.
func TestCoreSession(t *testing.T) {
	base := NewCoreBase(nil, nil)
	type line struct {
		src     string
		ok      bool
		want    string // a part of the first error, when !ok
		fails   bool   // the line runs and stops with a runtime error
		depth   int    // the line's inputs, when ok (-1: not checked)
		stack   int    // the checker's stack after the line (-1: not checked)
	}
	L := func(src string) line { return line{src: src, ok: true, depth: -1, stack: -1} }
	Bad := func(src, want string) line { return line{src: src, want: want, depth: -1, stack: -1} }
	sessions := []struct {
		name  string
		lines []line
	}{
		{"a variable unsolved on one line is solved by a later one", []line{
			L(`[] l!`), L(`@l 1 append drop`), Bad(`@l "a" append drop`, "expected int"), L(`@l len wl`),
		}},
		{"none fixes nothing until a later store does", []line{
			L(`none r!`), L(`5 just r!`), Bad(`"a" just r!`, "variable 'r'"), L(`@r ? 1 + wl`),
		}},
		{"a refused line leaves nothing behind", []line{
			L(`5 x!`), Bad(`@x "a" +`, ""), Bad(`"s" x!`, "variable 'x'"), L(`@x 1 + wl`),
			Bad(`3 y! @y "a" +`, "no matching overload"), L(`"a" y! @y wl`),
		}},
		{"stack values carry over", []line{
			{src: `1 2`, ok: true, depth: 0, stack: 2},
			{src: `+`, ok: true, depth: 2, stack: 1},
			Bad(`"a" +`, "no matching overload"),
			{src: `wl`, ok: true, depth: 1, stack: 0},
			Bad(`wl`, "underflow"),
		}},
		{"the input depth is the fewest slots a line takes", []line{
			{src: `1 2 3 4 5`, ok: true, depth: 0, stack: 5},
			{src: `drop`, ok: true, depth: 1, stack: 4},
			{src: `swap`, ok: true, depth: 2, stack: 4},
			{src: `+ +`, ok: true, depth: 3, stack: 2},
			{src: `dup 0 > if 1 + end`, ok: true, depth: 1, stack: 2},
			{src: `[1] swap`, ok: true, depth: 1, stack: 3},
		}},
		{"definitions and declarations on one line are used on the next", []line{
			L(`def incr (int -- int) 1 + end`), L(`5 incr wl`), Bad(`"a" incr`, ""),
			Bad(`def incr (str -- str) end`, "already defined"),
			L(`enum Color = red | green end`), L(`red match red : 1, green : 2 end wl`),
			L(`def isRed (Color -- bool) match red : true, green : false end end`), L(`green isRed str wl`),
			L(`def mkGreen ( -- Color) green end`), Bad(`def Color ( -- int) 1 end`, "already declared"),
			L(`type Pt = {a: int, b: int}`), L(`{a: 1, b: 2} as Pt :a? wl`),
			Bad(`5 as Pt`, "int is not below Pt"),
		}},
		{"a refused line's definitions and declarations are taken back", []line{
			Bad(`def g (int -- int) 1 + end  "a" g`, ""), L(`def g (str -- str) end`), L(`"a" g wl`),
			Bad(`type T = int  "a" as T`, ""), L(`type T = str`), L(`"a" as T wl`),
			Bad(`type Z1 = {z: int}  5 as Z1`, "int is not below Z1"), L(`type Z2 = {z: int}`),
			Bad(`5 as Z2`, "int is not below Z2"),
			Bad(`enum E = e1 | e2 end  e1 1 +`, ""), L(`enum E = e1 | e3 end`), L(`e3 drop`),
		}},
		{"return at the top of a line ends the session, as it ends a script", []line{
			L(`def f (int -- int) return end`), L(`1 f wl`),
			L(`"a" false if return end wl`), Bad(`"a" true if return end 1 +`, "no matching overload"),
			L(`[1] "x" return`), // any stack: nothing runs after it
		}},
		{"a runtime error keeps shared and immutable inputs", []line{
			L(`[1 2] l!`),
			{src: `@l 5`, ok: true, depth: 0, stack: 2},
			// Takes both; the stored list and the int are kept.
			{src: `swap 0 nth drop drop`, ok: true, fails: true, depth: 2, stack: 2},
			L(`1 + drop 0 nth drop`),
		}},
		{"a runtime error drops a new list the line took", []line{
			{src: `"x" [1 2]`, ok: true, depth: 0, stack: 2},
			// The new list may have been widened before the error: dropped.
			{src: `as [int | str] "a" append drop`, ok: true, fails: true, depth: 1, stack: 1},
			L(`wl`),
			Bad(`wl`, "underflow"),
		}},
		{"a runtime error leaves slots below the line's inputs alone", []line{
			{src: `[1] [2] [3]`, ok: true, depth: 0, stack: 3},
			{src: `drop`, ok: true, fails: true, depth: 1, stack: 2},
			L(`as [int | str] "a" append drop as [int | str] drop`),
		}},
		{"an overload choice stays open for the lines after it", []line{
			L(`(len) q!`), Bad(`5 @q x wl`, "of an earlier line"), L(`"abc" @q x wl`), Bad(`[1] @q x`, "expected str"),
			L(`[] l!`), L(`@l sort drop`), L(`@l 5 append drop`), Bad(`@l "a" append drop`, "expected int"), L(`@l sort str wl`),
			L(`(+) p!`), L(`1 2 @p x wl`), Bad(`"a" "b" @p x`, "expected int"),
		}},
		{"a refused line leaves an open choice open", []line{
			L(`(len) q!`), Bad(`"abc" @q x "a" +`, "no matching overload"), L(`[1 2] @q x wl`), Bad(`"abc" @q x`, "expected [int]"),
		}},
		{"a quote is typed when its line ends", []line{
			L(`(1 +)`), Bad(`"a" swap x wl`, "expected int"), L(`5 swap x wl`),
			L(`(dup) q!`), L(`5 @q x + wl`), Bad(`"a" @q x + wl`, ""),
		}},
	}
	for _, sc := range sessions {
		t.Run(sc.name, func(t *testing.T) {
			s := base.NewSession(0)
			for i, ln := range sc.lines {
				errs, ok := sessionLine(t, s, ln.src)
				if ok != ln.ok {
					t.Fatalf("line %d %q: ok = %v, want %v; errors %q", i, ln.src, ok, ln.ok, errs)
				}
				if !ok {
					if len(errs) == 0 {
						t.Fatalf("line %d %q: refused with no error", i, ln.src)
					}
					if !strings.Contains(errs[0], ln.want) {
						t.Fatalf("line %d %q: error %q does not mention %q", i, ln.src, errs[0], ln.want)
					}
					continue
				}
				if ln.depth >= 0 && s.Depth() != ln.depth {
					t.Fatalf("line %d %q: depth %d, want %d", i, ln.src, s.Depth(), ln.depth)
				}
				s.Commit()
				if ln.fails {
					s.RuntimeError()
				}
				if ln.stack >= 0 && s.Len() != ln.stack {
					t.Fatalf("line %d %q: stack %d, want %d", i, ln.src, s.Len(), ln.stack)
				}
			}
		})
	}
}

// TestCoreSessionRuntimeErrorKeep checks which of a line's inputs survive a
// runtime error, slot by slot.
func TestCoreSessionRuntimeErrorKeep(t *testing.T) {
	base := NewCoreBase(nil, nil)
	s := base.NewSession(0)
	for _, src := range []string{`[1] l!`, `@l [2] 3 "s" {a: 1} [@l]`} {
		if errs, ok := sessionLine(t, s, src); !ok {
			t.Fatalf("%q: %q", src, errs)
		}
		s.Commit()
	}
	// The stack: stored [int], new [int], int, str, new {a: int}, partly new [[int]].
	if errs, ok := sessionLine(t, s, `drop drop drop drop drop drop`); !ok {
		t.Fatalf("%q", errs)
	}
	s.Commit()
	d, keep := s.RuntimeError()
	if d != 6 || !slices.Equal(keep, []bool{true, false, true, true, false, false}) {
		t.Fatalf("depth %d, keep %v", d, keep)
	}
	if s.Len() != 3 {
		t.Fatalf("stack %d", s.Len())
	}
	// What is kept has its type: a stored list, an int, a str.
	if errs, ok := sessionLine(t, s, `"t" + wl 1 + wl 0 nth wl`); !ok {
		t.Fatalf("%q", errs)
	}
}

// TestCoreSessionAbortRestores feeds the corpus to sessions one top-level
// item per line, and checks every line twice in one of them, aborting the
// first check, with a refused line in between: the two sessions must agree
// on every line. A rollback that left anything behind (a variable, a
// binding, a definition, a declaration, a check) shows up as a difference.
func TestCoreSessionAbortRestores(t *testing.T) {
	var paths []string
	for _, dir := range []string{"../tests/success", "../tests/typecheck_fail"} {
		ps, err := filepath.Glob(filepath.Join(dir, "*.msh"))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, ps...)
	}
	base := NewCoreBase(benchStdlib(t), nil)
	garbage, _ := NewMShellParser(NewLexer(`1 "a" + zz!`, nil)).ParseFile()
	rng := rand.New(rand.NewSource(2))
	lines := 0
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		file, err := NewMShellParser(NewLexer(string(src), nil)).ParseFile()
		if err != nil {
			continue
		}
		a, b := base.NewSession(0), base.NewSession(0)
		for _, ln := range splitLines(file, rng) {
			lines++
			da, oka := a.Check(ln)
			if oka {
				a.Commit()
			}
			if _, ok := b.Check(ln); ok {
				b.Abort()
			}
			b.Check(garbage)
			db, okb := b.Check(ln)
			if okb {
				b.Commit()
			}
			// Type variables are numbered by when they are made, and the
			// aborted check made some, so messages are compared by kind
			// and position.
			fa, fb := diagKeys(da), diagKeys(db)
			if oka != okb || !slices.Equal(fa, fb) {
				t.Fatalf("%s: a line checks %v %q, and %v %q after an aborted check of it", p, oka, fa, okb, fb)
			}
			// Now and then, as if the line had stopped at run time.
			if oka && rng.Intn(4) == 0 {
				da, ka := a.RuntimeError()
				db, kb := b.RuntimeError()
				if da != db || !slices.Equal(ka, kb) {
					t.Fatalf("%s: runtime error keeps %d %v and %d %v", p, da, ka, db, kb)
				}
			}
		}
	}
	if lines < 1000 {
		t.Fatalf("only %d lines", lines)
	}
}

// splitLines makes REPL lines of a file: its definitions on one line, then
// its top-level items in runs of one to three.
func splitLines(file *MShellFile, rng *rand.Rand) []*MShellFile {
	var out []*MShellFile
	if len(file.Definitions) > 0 {
		out = append(out, &MShellFile{Definitions: file.Definitions})
	}
	items := file.Items
	for len(items) > 0 {
		n := min(len(items), 1+rng.Intn(3))
		out = append(out, &MShellFile{Items: items[:n]})
		items = items[n:]
	}
	return out
}

func diagKeys(diags []TypeError) []string {
	var out []string
	for _, e := range diags {
		out = append(out, fmt.Sprintf("%d:%d:%d", e.Kind, e.Pos.Line, e.Pos.Column))
	}
	return out
}

// replRun runs lines as the REPL does, with the REPL's replChecker: each is
// checked, run only if it checks, and after a runtime error the stack is
// restored. It returns why the run is a soundness failure, or "": a type
// mismatch, a panic, or the checker's stack out of step with the runtime's.
// report, if not nil, is told what happened to each line.
func replRun(s *CoreSession, lines []*MShellFile, report func(i int, what string)) (why string, ran, failed int) {
	r := &replChecker{session: s}
	state := EvalState{CallStack: make([]CallStackItem, 0, 10)}
	stack := MShellStack{}
	context := ExecuteContext{Variables: map[string]MShellObject{}, Pbm: testPbm(), StandardOutput: io.Discard, StandardError: io.Discard}
	callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
	var defs []MShellDefinition
	say := func(i int, what string) {
		if report != nil {
			report(i, what)
		}
	}
	for i, ln := range lines {
		if err := state.CheckDefinitionNames(defs, ln.Definitions); err != nil {
			say(i, "refused by the name check: "+err.Error())
			continue
		}
		msgs, ok := r.check(ln)
		if !ok {
			say(i, "refused: "+strings.Join(msgs, "; "))
			continue
		}
		if err := state.RegisterDeclarations(ln.Items, append(slices.Clone(defs), ln.Definitions...)); err != nil {
			r.abort()
			say(i, "refused by the runtime's declarations: "+err.Error())
			continue
		}
		diverges := s.Diverges()
		r.commit(stack)
		defs = append(defs, ln.Definitions...)
		if diverges {
			// It never returns normally (a loop with no break, say): the
			// REPL would see it again only if it stopped with an error.
			ran++
			failed++
			dropped := r.failed(&stack)
			say(i, fmt.Sprintf("diverges: not run, taken as stopped with a checked error; %d value(s) dropped", dropped))
			continue
		}
		var result EvalResult
		panicked := ""
		func() {
			defer func() {
				if p := recover(); p != nil {
					panicked = fmt.Sprint(p)
				}
			}()
			result = state.Evaluate(ln.Items, &stack, context, defs, callStackItem)
		}()
		ran++
		switch {
		case panicked != "":
			say(i, "PANIC: "+panicked)
			return "panic: " + panicked, ran, failed
		case result.ExitCalled:
			say(i, "exit")
			return "", ran, failed
		case !result.Success && state.FailureKind == TypeMismatch:
			say(i, "TYPE MISMATCH")
			return "type mismatch", ran, failed
		case !result.Success:
			failed++
			dropped := r.failed(&stack)
			say(i, fmt.Sprintf("stopped with a checked error; %d value(s) dropped, stack %d", dropped, len(stack)))
		default:
			say(i, fmt.Sprintf("ran, stack %d", len(stack)))
		}
		if !r.inSync(stack) {
			say(i, "OUT OF SYNC")
			return fmt.Sprintf("the checker has %d values on the stack, the runtime %d", s.Len(), len(stack)), ran, failed
		}
	}
	return "", ran, failed
}

// TestSessionScript runs MSH_SESSION_FILE as a REPL session, one line of
// the file per REPL line (blank lines and lines starting with # skipped),
// and prints what happened to each: refused, ran, or stopped with a runtime
// error. It fails on a type mismatch at run time. For reviewing the REPL's
// checking by hand.
func TestSessionScript(t *testing.T) {
	path := os.Getenv("MSH_SESSION_FILE")
	if path == "" {
		t.Skip("MSH_SESSION_FILE is not set")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []*MShellFile
	var texts []string
	for _, text := range strings.Split(string(src), "\n") {
		if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "#") {
			continue
		}
		f, err := NewMShellParser(NewLexer(text, nil)).ParseFile()
		if err != nil {
			t.Logf("%-40s  does not parse: %v", text, err)
			continue
		}
		lines = append(lines, f)
		texts = append(texts, text)
	}
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	saved := os.Stderr
	os.Stderr = devnull
	why, _, _ := replRun(NewCoreBase(nil, nil).NewSession(0), lines, func(i int, what string) {
		os.Stderr = saved
		t.Logf("%-40s  %s", texts[i], what)
		os.Stderr = devnull
	})
	os.Stderr = saved
	if why != "" {
		t.Errorf("%s", why)
	}
}

// replLines splits a file into REPL lines at random item boundaries, so
// values cross from one line to the next, and makes some lines stop with a
// runtime error after they have taken their inputs.
func replLines(file *MShellFile, rng *rand.Rand) []*MShellFile {
	fail, _ := NewMShellParser(NewLexer(`[] 0 nth drop`, nil)).ParseFile()
	lines := splitLines(file, rng)
	for i, ln := range lines {
		if len(ln.Items) > 0 && rng.Intn(5) == 0 {
			lines[i] = &MShellFile{Items: append(slices.Clone(ln.Items), fail.Items...)}
		}
	}
	return lines
}

// TestSessionProgramsSound runs generated programs (TypeSoundGen_test.go)
// line by line through a session and the runtime, as the REPL would, and
// fails on a type mismatch at run time. MSH_SESSION_PROGRAMS sets how many
// (default 60) and MSH_GEN_SEED the first seed.
func TestSessionProgramsSound(t *testing.T) {
	programs := envTrials("MSH_SESSION_PROGRAMS", 60)
	seed0 := int64(envTrials("MSH_GEN_SEED", 1))
	base := NewCoreBase(nil, nil)
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	saved := os.Stderr
	os.Stderr = devnull
	defer func() { os.Stderr = saved }()
	slots := make([]genSlot, 1)
	stop := make(chan struct{})
	done := make(chan uint64)
	go genWatchdog(slots, saved, stop, done)
	defer func() { close(stop); <-done }()
	ran, failed, lines := 0, 0, 0
	for i := range programs {
		seed := seed0 + int64(i)
		p := &progGen{rng: rand.New(rand.NewSource(seed)), base: base}
		p.risk = p.rng.Float64() * 0.3
		slots[0].begin(seed, genGenerating)
		src := p.generate()
		file, err := parseMShellInput(src, &TokenFile{"gen.msh"})
		if err != nil {
			continue
		}
		ls := replLines(file, rand.New(rand.NewSource(seed)))
		lines += len(ls)
		slots[0].begin(seed, genRunning)
		why, r, f := replRun(base.NewSession(0), ls, nil)
		slots[0].begin(0, genIdle)
		ran, failed = ran+r, failed+f
		if why != "" {
			os.Stderr = saved
			t.Fatalf("seed %d: %s\n--- lines\n%s", seed, why, renderLines(ls))
		}
	}
	t.Logf("%d programs, %d lines, %d ran, %d stopped with a runtime error", programs, lines, ran, failed)
}

func renderLines(lines []*MShellFile) string {
	var sb strings.Builder
	for _, ln := range lines {
		for _, d := range ln.Definitions {
			sb.WriteString("def " + d.Name + " ...")
			sb.WriteString(" ")
		}
		for _, it := range ln.Items {
			sb.WriteString(it.DebugString())
			sb.WriteString(" ")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// sessionBenchLines are REPL lines of the kinds people type: values left on
// the stack and used on the next line, stores, a list built over lines.
var sessionBenchLines = []string{
	`[1 2 3] (1 +) map len n!`,
	`{a: 1, b: "x"} :a? @n + m!`,
	`"x y z" " " split len drop`,
	`[] acc!`,
	`@acc @m append drop`,
	`1 2`,
	`+ 3 *`,
	`str s!`,
	`@s "-" + wl`,
	`none r!  5 just r!`,
	`@r 0 maybe 1 + drop`,
	`[echo hi] c!`,
	`true if 1 else 2 end drop`,
	`[1 2] (x! @x 2 *) map (3 >) filter len drop`,
}

func parseLines(b *testing.B, srcs []string) []*MShellFile {
	out := make([]*MShellFile, len(srcs))
	for i, src := range srcs {
		f, err := NewMShellParser(NewLexer(src, nil)).ParseFile()
		if err != nil {
			b.Fatal(err)
		}
		out[i] = f
	}
	return out
}

// BenchmarkCoreSessionLine times checking and keeping one line, in a new
// session and after 10,000 earlier lines. The earlier lines include stores
// of `[]` never used again, which leave a variable unsolved for good.
func BenchmarkCoreSessionLine(b *testing.B) {
	base := NewCoreBase(nil, nil)
	lines := parseLines(b, sessionBenchLines)
	for _, warm := range []int{10, 10000} {
		b.Run(fmt.Sprintf("after%d", warm), func(b *testing.B) {
			s := base.NewSession(0)
			for i := range warm {
				f := lines[i%len(lines)]
				if i%7 == 6 {
					f = parseLines(b, []string{fmt.Sprintf("[] u%d!", i)})[0]
				}
				if _, ok := s.Check(f); !ok {
					b.Fatalf("warm-up line %d refused", i)
				}
				s.Commit()
			}
			for s.Len() > 0 {
				s.Check(parseLines(b, []string{"drop"})[0])
				s.Commit()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				if _, ok := s.Check(lines[i%len(lines)]); !ok {
					b.Fatalf("line %d refused", i%len(lines))
				}
				s.Commit()
			}
		})
	}
	// Every open choice is made again at the end of each line.
	b.Run("openChoices", func(b *testing.B) {
		s := base.NewSession(0)
		for i := range maxOpenChoices {
			if _, ok := s.Check(parseLines(b, []string{fmt.Sprintf("(len) q%d!", i)})[0]); !ok {
				b.Fatal("refused")
			}
			s.Commit()
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			if _, ok := s.Check(lines[i%len(lines)]); !ok {
				b.Fatalf("line %d refused", i%len(lines))
			}
			s.Commit()
		}
	})
	b.Run("refused", func(b *testing.B) {
		s := base.NewSession(0)
		bad := parseLines(b, []string{`5 x! @x "a" +`})[0]
		b.ReportAllocs()
		for b.Loop() {
			if _, ok := s.Check(bad); ok {
				b.Fatal("accepted")
			}
		}
	})
}

// TestCoreSessionStartupVariables: a variable a startup file set has a
// value of a type the checker does not know, so it is unknown; a line that
// stores it and then stops before the store must not let a later line read
// it at the stored type (found by review).
func TestCoreSessionStartupVariables(t *testing.T) {
	s := NewCoreBase(nil, nil).NewSession(0, "x")
	if errs, ok := sessionLine(t, s, `[] 0 nth drop 5 x!`); !ok {
		t.Fatalf("%q", errs)
	}
	s.Commit()
	s.RuntimeError()
	if _, ok := sessionLine(t, s, `@x 1 +`); ok {
		t.Fatal("@x read as an int, though a startup file set it to an unknown value")
	}
	if errs, ok := sessionLine(t, s, `@x match int n : @n 1 + wl, _ : end`); !ok {
		t.Fatalf("%q", errs)
	}
	// A read inside a quote is not checked for definite assignment, so a
	// store on a path that does not run must not type it either.
	s = NewCoreBase(nil, nil).NewSession(0, "v")
	if _, ok := sessionLine(t, s, `(@v 1 + wl) q!  false if 5 v! end`); ok {
		t.Fatal("a quote read a startup variable as an int")
	}
}

// TestCompletionDefsOwnVariables: a completion definition runs in a scope of
// its own, so pressing TAB cannot change the REPL's variables (found by
// review: it overwrote them, behind the checker's back).
func TestCompletionDefsOwnVariables(t *testing.T) {
	file, err := parseMShellInput(`def c ([str] -- [str]) drop ["a"] options! @options end`, &TokenFile{"t"})
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]MShellObject{"options": MShellInt{Value: 5}}
	state := &TermState{context: ExecuteContext{Variables: vars, Pbm: testPbm()},
		evalState: EvalState{CallStack: make([]CallStackItem, 0, 10)}}
	spec, ok := state.runCompletionDefinitions(file.Definitions, []string{"c", ""})
	if !ok || len(spec.Values) != 1 {
		t.Fatalf("completion gave %v %v", spec, ok)
	}
	if v, isInt := vars["options"].(MShellInt); !isInt || v.Value != 5 {
		t.Fatalf("options is now %v", vars["options"])
	}
}

// TestCoreSessionChoicesPastCap: a session keeps at most maxOpenChoices
// overload choices open; past that the oldest is made at the end of the
// line, as a file makes it when the unit is solved.
func TestCoreSessionChoicesPastCap(t *testing.T) {
	s := NewCoreBase(nil, nil).NewSession(0)
	run := func(src string) {
		t.Helper()
		if errs, ok := sessionLine(t, s, src); !ok {
			t.Fatalf("%.60q: %q", src, errs)
		}
		s.Commit()
	}
	n := maxOpenChoices + 4
	for i := range n {
		run(fmt.Sprintf(`(len) q%d!`, i))
	}
	if len(s.c.choices) != maxOpenChoices {
		t.Fatalf("%d choices open, want %d", len(s.c.choices), maxOpenChoices)
	}
	// The newest are still open either way; the oldest were made with the
	// first candidate that fits, a list.
	run(fmt.Sprintf(`"abc" @q%d x drop  [1] @q%d x drop`, n-1, n-2))
	run(`[1] @q0 x drop`)
	if _, ok := sessionLine(t, s, `"abc" @q1 x drop`); ok {
		t.Fatal("a choice made for good at the cap is still open")
	}
}

// TestCoreSessionRefsPastCap: join origins and literal name lists are
// dropped once nothing refers to them, and the ones a kept slot refers to
// still work after that.
func TestCoreSessionRefsPastCap(t *testing.T) {
	s := NewCoreBase(nil, nil).NewSession(0)
	run := func(src string) {
		t.Helper()
		if errs, ok := sessionLine(t, s, src); !ok {
			t.Fatalf("%.60q: %q", src, errs)
		}
		s.Commit()
	}
	run(`true if 1 else "a" end  ["y"]`) // a union from a join, and a literal name list
	churn := strings.Repeat(`true if 1 else "a" end drop  ["x" "z"] drop `, 100)
	for range 3 * compactAt / 100 {
		run(churn)
	}
	if n, m := len(s.c.origins), len(s.c.litLists); n > compactAt+200 || m > compactAt+200 {
		t.Fatalf("%d join origins and %d literal name lists kept", n, m)
	}
	run(`[| x, y; 1, "a" |] swap select "y" gridCol ("b" +) map drop`)
	errs, ok := sessionLine(t, s, `1 +`)
	if ok || len(errs) == 0 || !strings.Contains(errs[0], "comes from the `if`") {
		t.Fatalf("the join origin of a kept slot was lost: %q", errs)
	}
}

// TestCoreSessionPartsPastCap: partly new marks are numbered with 16 bits,
// and past maxParts a value is treated as shared. A session drops the marks
// nothing refers to, so a long one does not reach the cap (found by review),
// and a partly new value left on the stack keeps its mark through that.
func TestCoreSessionPartsPastCap(t *testing.T) {
	s := NewCoreBase(nil, nil).NewSession(0)
	run := func(src string) {
		t.Helper()
		if errs, ok := sessionLine(t, s, src); !ok {
			t.Fatalf("%.60q: %q", src, errs)
		}
		s.Commit()
	}
	run(`[1] xs!`)
	run(`{a: @xs}`) // left on the stack, partly new
	churn := strings.Repeat(`{a: @xs} drop `, 100)
	for range (maxParts + 2000) / 100 {
		run(churn)
	}
	if n := len(s.c.parts); n > compactAt+200 {
		t.Fatalf("%d partly new marks kept", n)
	}
	run(`{a: @xs} as {a: [int], b?: int} drop`)
	// The value left on the stack is still partly new: it may gain an
	// optional label, and its stored list keeps its type.
	run(`as {a: [int], b?: int}`)
	if _, ok := sessionLine(t, s, `as {a: [int | str]}`); ok {
		t.Fatal("a stored list widened through a partly new value")
	}
	// And after a runtime error in a line that took it, it is dropped.
	run(`drop`)
	if d, keep := s.RuntimeError(); d != 1 || keep[0] {
		t.Fatalf("depth %d keep %v", d, keep)
	}
}

// TestCoreSessionEarlierCheck: an earlier line's check that this line's
// types make fail says it is from an earlier line (found by review).
func TestCoreSessionEarlierCheck(t *testing.T) {
	s := NewCoreBase(nil, nil).NewSession(0)
	if errs, ok := sessionLine(t, s, `[none] dup a! b!`); !ok {
		t.Fatalf("%q", errs)
	}
	s.Commit()
	errs, ok := sessionLine(t, s, `@a 5 just append drop`)
	if ok {
		t.Fatal("accepted")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "of an earlier line") {
		t.Fatalf("%q", errs)
	}
}

// BenchmarkCoreSessionOpenChecks times a line after 8,000 lines that each
// leave a check open for good: `none rN!` stores a Maybe whose contents no
// later line fixes (found by review: every line made them all again).
func BenchmarkCoreSessionOpenChecks(b *testing.B) {
	base := NewCoreBase(nil, nil)
	s := base.NewSession(0)
	for i := range 8000 {
		if _, ok := s.Check(parseLines(b, []string{fmt.Sprintf("none r%d!", i)})[0]); !ok {
			b.Fatal("refused")
		}
		s.Commit()
	}
	lines := parseLines(b, sessionBenchLines)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, ok := s.Check(lines[i%len(lines)]); !ok {
			b.Fatalf("line %d refused", i%len(lines))
		}
		s.Commit()
	}
}

// TestCoreSessionOpenChecksCompact closes many open checks, so the session
// compacts them, and checks that the ones still open are still made: a
// check an early line left open fails after the compaction.
func TestCoreSessionOpenChecksCompact(t *testing.T) {
	s := NewCoreBase(nil, nil).NewSession(0)
	run := func(src string) {
		t.Helper()
		if errs, ok := sessionLine(t, s, src); !ok {
			t.Fatalf("%q: %q", src, errs)
		}
		s.Commit()
	}
	run(`[none] dup a! b!`)
	for i := range 300 {
		run(fmt.Sprintf("none r%d!", i))
	}
	open := s.openStores.kept()
	for i := range 300 {
		run(fmt.Sprintf("%d just r%d!", i, i))
	}
	if s.openStores.kept() >= open {
		t.Fatalf("open store checks: %d before closing, %d after", open, s.openStores.kept())
	}
	if _, ok := sessionLine(t, s, `"x" just r7!`); ok {
		t.Fatal("r7 took a str after an int")
	}
	errs, ok := sessionLine(t, s, `@a 5 just append drop`)
	if ok || !strings.Contains(strings.Join(errs, " "), "of an earlier line") {
		t.Fatalf("the early line's open check was lost: %v %q", ok, errs)
	}
	run(`@b len wl`)
}

// TestCoreSessionStartupBodies: a startup def whose body does not check is
// refused in a session, rather than trusted by its signature (which made
// the checker lose track of the stack); its other defs work.
func TestCoreSessionStartupBodies(t *testing.T) {
	init, err := NewMShellParser(NewLexer("def double (int -- int) 2 * end\ndef badBody (int -- int) \"oops\" end\n",
		&TokenFile{"init.msh"})).ParseFile()
	if err != nil {
		t.Fatal(err)
	}
	base := NewCoreBase(init.Definitions, nil)
	if errs := base.StartupErrors(); len(errs) != 1 || !strings.Contains(errs[0], "badBody") {
		t.Fatalf("startup errors: %q", errs)
	}
	s := base.NewSession(0)
	if errs, ok := sessionLine(t, s, `5 badBody wl`); ok || !strings.Contains(errs[0], "its body has a type error") {
		t.Fatalf("a call to a broken startup def: ok = %v, %q", ok, errs)
	}
	if errs, ok := sessionLine(t, s, `5 double wl`); !ok {
		t.Fatalf("a call to a good startup def: %q", errs)
	}
}
