package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coreCheck parses src and checks it with the core checker.
func coreCheck(t *testing.T, base *CoreBase, src string) ([]string, bool) {
	t.Helper()
	file, err := NewMShellParser(NewLexer(src, nil)).ParseFile()
	if err != nil {
		t.Fatalf("parse error in %q: %v", src, err)
	}
	return base.Check(file)
}

// TestCoreChecker runs the acceptance rows of ai/type-system-plan.md
// section 7 that the core checker covers so far.
func TestCoreChecker(t *testing.T) {
	base := NewCoreBase(nil)
	cases := []struct {
		src  string
		ok   bool
		want string // a part of the first error, when !ok
	}{
		// Variables: one type per scope, fixed by the first store.
		{`false if 1 x! else "a" x! end @x 1 +`, false, "variable 'x' has type int"},
		{`"a\nb" text!  @text lines text!`, false, "use a new name, or widen the first store"},
		// A ⊥ in a store fixes nothing.
		{`none r!  5 just r!  none r!  @r ? 1 + wl`, true, ""},
		{`[none] l!  @l 5 just append drop`, true, ""},
		{`none r! @r drop`, true, ""},
		// never and divergence.
		{`def f ( -- never) 1 exit 1 + end`, true, ""},
		{`def f (str -- never) wl end`, false, "never returns, but the body can return"},
		{`def f ( -- int never) 1 end`, false, "'never' is allowed only"},
		{`def f ([never] -- ) drop end`, false, "'never' is allowed only"},
		{`def spin ( -- never) spin end`, true, ""},
		{`def f (bool -- never | str) drop "a" end`, false, "'never' is allowed only"},
		{`def die (str -- never) wl 1 exit end  true if 5 else "bad" die end 1 + wl`, true, ""},
		{`true if 1 else 1 exit end 1 + wl`, true, ""},
		{`1 return 2`, true, ""},
		{`def f (int -- int) return end`, true, ""},
		{`def f ( -- never) return end`, false, "'return' is not allowed"},
		// Joins (the design doc's join table).
		{`true if 1 else 2.5 end drop`, true, ""},
		{`true if none else 5 just end drop`, true, ""},
		{`true if "a" else null end drop`, true, ""},
		{`true if [1] else ["a"] end drop`, true, ""},
		{`true if {a: 1} else {a: 2, b: 3} end drop`, true, ""},
		{`[1] xs! ["a"] ys! true if @xs else @ys end drop`, false, "no common type"},
		{`[1] xs! ["a"] ys! true if @xs just else @ys just end drop`, false, "no common type"},
		{`true if [1] just else ["a"] just end drop`, true, ""},
		{`true if 5 else "x" parseJson end drop`, true, ""},
		{`true if [1] else "x" parseJson end drop`, true, ""},
		{`[1] xs! true if @xs else "x" parseJson end drop`, false, "no common type"},
		{`true if 1 else end`, false, "differing sizes"},
		// else if conditions run on the stack the earlier ones left.
		{`1 false if 2 else* 3 4 = *if 5 else 6 end + wl`, true, ""},
		{`false if 1 else* "a" *if 2 else 3 end drop`, false, "expected"},
		// Generics: a bare generic in two inputs is the join of the arguments.
		{`none 5 just = drop`, true, ""},
		{`[1] ["a"] = drop`, true, ""},
		// Quotes: checked against the word that takes them.
		{`[1 2] (1 + wl) each`, true, ""},
		{`[1 2] (1 +) map len wl`, true, ""},
		{`[1 2] ("a" +) map drop`, false, "no matching overload for '+'"},
		{`[1 2] map. 1 + end len wl`, true, ""},
		{`def apply ((int -- int) int -- int) swap x end (2 *) 3 apply wl`, true, ""},
		{`def apply ((int -- int) int -- int) swap x end ("a" +) 3 apply wl`, false, "no matching overload"},
		// x, iff and loop run a literal inline.
		{`(1 2 +) x wl`, true, ""},
		{`true (1) (2) iff wl`, true, ""},
		{`true (1 wl) iff`, true, ""},
		{`true (1) ("a") iff 1 + wl`, false, "no matching overload"},
		{`true if (1) else (2) end x wl`, true, ""},
		{`0 (dup 10 > if break end 1 +) loop wl`, true, ""},
		{`0 (dup 10 > if break end "a") loop`, false, "a loop body must leave the stack"},
		{`(1 wl) loop`, true, ""},
		// break and continue contexts.
		{`break`, false, "'break' is allowed only"},
		{`(break) q! [1] (drop @q x) each`, false, "'break' is allowed only"},
		{`0 ([1 2] (drop break) each) loop drop`, true, ""},
		{`[[1] [2]] (drop [1 2] (break) each) each`, false, "'break' is allowed only"},
		// P14: no renaming of a variable stored at a new type in a loop body.
		{`1 x!  [0 0] (drop @x 1 + drop "a" x!) each`, false, "variable 'x' has type int"},
		// A quote of unknown arity cannot be run.
		{`(1 wl) q! @q x`, true, ""},
		{`(1 +) drop`, false, "annotate"},
		{`("a" 1 +) drop`, false, "no matching overload"},

		// Pending overload choices: made once one candidate fits.
		{`(1 +) q! 5 @q x wl`, true, ""},
		{`(1 +) q! 2.5 @q x str wl`, true, ""},
		{`(1 +) q! "a" @q x drop`, false, "no matching overload for '+'"},
		{`(1 +) q!`, false, "annotate the quote"},
		{`(dup *) q! 3 @q x wl`, true, ""},

		// An overloaded word on a union is a match per member.
		{`true if 1 else 2.5 end toFloat 1.0 + str wl`, true, ""},
		{`true if 1 else "a" end 1 + drop`, false, "the stack has (str int)"},

		// match: kind patterns give the member of a union; arms are joined.
		{`def f (int | [str] -- int) match int n : @n , list xs : @xs len end end 1 f wl`, true, ""},
		{`def f (int | [str] -- int) match int n : @n end end`, false, "non-exhaustive"},
		{`def f (int | [str] -- ) match list xs : @xs "a" append drop , _ : end end`, true, ""},
		{`def f (int | null -- str) match int : "int" end end`, false, "non-exhaustive"},
		{`def f (int | null -- str) match int : "int" , null : "null" end end`, true, ""},
		{`true ([1]) (["x"]) iff match [value] : @value 1 + , _ : 0 end drop`, false, "the stack has (str int)"},
		{`5 just match just v : @v , none : 0 end wl`, true, ""},
		{`def k (int | str -- int | str) match int :> 1 + , str :> end end 1 k drop`, true, ""},
		{`{'a': 1, 'b': "x"} match {'a': v} : @v 1 + wl , _ : end`, true, ""},
		{`[1 2 3] match [a ...rest] : @rest len wl , _ : end`, true, ""},
		{`[1 2] => [a b] @a @b + wl`, true, ""},
		{`[1 2 3] match [] : , [a ...rest] : , end`, true, ""},
		{`[1 2 3] match [a ...rest] : , end`, false, "non-exhaustive"},
		{`{a: 1} => {b: n}`, false, "has no key 'b'"},
		// H8: a kind pattern on a type variable is unknown contents.
		{`def h (a -- int) match str s : @s 1 + , _ : drop 0 end end`, false, "no matching overload"},
		{`def h (a -- int) match str s : @s len , _ : 0 end end`, true, ""},
		// H11 and H2: no abstract binding; :> keeps the value on the stack.
		{`def g (a -- ) match list xs : @xs drop , _ : end end`, false, "keep the value on the stack with `:>`"},
		{`def g (a -- int) match list :> len , _ : 0 end end`, true, ""},
		{`[1 "s"] (match int n : (@n) q! , str n : @q x 1 + drop end) each`, false, "variable 'n' has type int"},
		{`[1 "s"] (match int n : , str s : end) each`, true, ""},
		{`def g (a -- ) [] acc! match list :> acc! , _ : end end`, false, "leaves the arm"},
		{`def g (a -- ) match list :> drop , _ : end end`, true, ""},

		// Unions of distinct kinds only.
		{`def f ([int] | [str] -- ) drop end`, false, "two members of the same kind"},
		{`def f (int | [str] -- ) drop end`, true, ""},
	}
	for _, tc := range cases {
		errs, ok := coreCheck(t, base, tc.src)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v; errors: %v", tc.src, ok, tc.ok, errs)
			continue
		}
		if !ok && !strings.Contains(errs[0], tc.want) {
			t.Errorf("%s: first error %q does not contain %q", tc.src, errs[0], tc.want)
		}
	}
}

// benchCorpus parses every script the type-check benchmarks use.
func benchCorpus(b *testing.B) []*MShellFile {
	var files []*MShellFile
	for _, pattern := range []string{"../tests/success/*.msh", "../tests/typecheck_fail/*.msh", "../tests/msh-scripts/*"} {
		paths, _ := filepath.Glob(pattern)
		for _, p := range paths {
			src, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			file, err := NewMShellParser(NewLexer(string(src), nil)).ParseFile()
			if err != nil {
				continue
			}
			files = append(files, file)
		}
	}
	return files
}

// BenchmarkCoreCheckCorpus checks the corpus of BenchmarkTypeCheckCorpus
// with the core checker, from a base built once.
func BenchmarkCoreCheckCorpus(b *testing.B) {
	base := NewCoreBase(benchStdlib(b))
	files := benchCorpus(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, f := range files {
			base.Check(f)
		}
	}
}

// BenchmarkCoreCheckEmpty is the cost of starting one check.
func BenchmarkCoreCheckEmpty(b *testing.B) {
	base := NewCoreBase(benchStdlib(b))
	file := benchParse(b, "")
	b.ReportAllocs()
	for b.Loop() {
		base.Check(file)
	}
}

// BenchmarkCoreBase is the cost of building the base, once per process.
func BenchmarkCoreBase(b *testing.B) {
	std := benchStdlib(b)
	b.ReportAllocs()
	for b.Loop() {
		NewCoreBase(std)
	}
}
