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
	base := NewCoreBase(nil, nil)
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
		{`[1] ["a"] = drop`, false, "no matching overload for '='"},
		{`{a: 1} {a: 2} = drop`, true, ""},
		{`1 "a" = drop`, false, "no matching overload for '='"},
		// A ⊥ in an argument fixes no generic.
		{`none 5 maybe wl`, true, ""},
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
		{`(dup +) drop`, false, "annotate"},
		{`("a" 1 +) drop`, false, "no matching overload"},

		// Pending overload choices: made once one candidate fits.
		{`(dup +) q! 5 @q x wl`, true, ""},
		{`(dup +) q! 2.5 @q x str wl`, true, ""},
		{`(dup +) q! true @q x drop`, false, "no matching overload for '+'"},
		{`(dup +) q!`, false, "annotate the quote"},
		{`(1 +) q! 5 @q x wl`, true, ""},
		{`(dup *) q! 3 @q x wl`, true, ""},

		// An overloaded word on a union is a match per member.
		{`true if 1 else 2.5 end toFloat 1.0 + str wl`, true, ""},
		{`true if 1 else 2.5 end 1 + drop`, false, "the stack has (float int)"},
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

		// as: evidence, or fresh retyping.
		{`[1 2] l! @l as [int | str] drop`, false, "make a new one first with deepCopy"},
		{`[1 2] as [int | str] drop`, true, ""},
		{`[1 2] l! @l deepCopy as [int | str] drop`, true, ""},
		{`"x" parseJson as {a: int} drop`, false, "use tryAs"},              // P13
		{`[1] xs! {a: @xs} as {a: [int | str]} drop`, false, "needs evidence"}, // H3
		{`{url: "x"} as {url: str, timeout?: int} drop`, true, ""},
		{`[] as [str] drop`, true, ""},
		{`1 as int | str x!  "a" x!`, true, ""},
		{`[1] as [int | str] xs!  ["a"] xs!`, true, ""},
		{`["a"] ys! [1] as [int | str] xs!  @ys xs!`, false, "variable 'xs' has type [int | str]"},
		// Format strings.
		{`5 n! $"n is {@n}" wl`, true, ""},
		{`[1] l! $"l is {@l}" wl`, false, "expected int | str | path"},

		// del: only a {str: T} loses keys; a stored shape never does.
		{`{a: 1} s! @s "a" del drop`, false, "no matching overload for 'del'"},
		{`{a: 1} as {str: int} d! @d "a" del drop`, true, ""},

		// Dicts: literal keys read a label; runtime keys read above every label.
		{`{a: 1, b: "x"} d! @d :a? 1 + wl`, true, ""},
		{`{a: 1, b: "x"} d! @d "b" get ? len wl`, true, ""},
		{`{a: 1, b: "x"} d! @d :b? 1 + wl`, false, "the stack has (str int)"},
		{`{a: 1} "a" 0 getDef 1 + wl`, true, ""},
		{`{a: 1, b: 2} values len wl`, true, ""},
		{`{a: 1} as {a: int, *: str} k! "a" key! @k @key get ? "x" + drop`, false, "the stack has (int str)"}, // H1
		{`{a: 1} as {a: int, *: str} k! @k "b" get ? "x" + drop`, true, ""},
		{`{} "a" 1 set "b" "x" set d! @d :a? 1 + wl @d :b? len wl`, true, ""},
		{`{a: 1} d! @d "a" "x" set drop`, false, "'set' expected int"},
		{`{a: 1} d! @d "b" 2 set drop`, false, "has no key 'b' that can be set"},
		// P1-P4.
		{`def setA ({a: int | str} -- ) "a" "x" set drop end {a: 1} dup setA :a? 1 + wl`, false, "'setA' expected"},
		{`def onlyA ({a: int} -- {a: int}) end def vals ({str: int} -- ) values (1 + wl) each end {a: 1, b: "x"} onlyA vals`, false, "'vals' expected"},
		{`def getA ({a: int} -- int) :a? end {} as {str: int} getA 1 + wl`, false, "'getA' expected"},
		{`def put ({str: int | str} -- ) "a" "x" set drop end {a: 1} dup put :a? 1 + wl`, false, "'put' expected"},
		// A width step against a generic parameter needs no guess.
		{`def showName ({name: str, val: Maybe[t]} -- ) :name? str wl end [{"name": "alice", "val": none}] (showName) each`, true, ""},

		// Indexing.
		{`[1 2 3] :0: 1 + wl`, true, ""},
		{`"abc" :1: wl`, true, ""},
		{`[1 2 3] :0: len wl`, false, "no matching overload for 'len'"},
		{`[[1]] xs! @xs 1: as [[int | str]] drop`, false, "needs evidence"},
		{`"a\nb" lines 1: as [str | int] drop`, true, ""},

		// Commands: a redirect changes the type, so the list must be fresh (P7).
		{`[echo hi] * ; wl`, true, ""},
		{`[echo hi] c! @c * ; drop`, false, "the list must be new"},
		{`[echo hi] c! @c deepCopy * ; wl`, true, ""},
		{`[echo hi] * * ;`, false, "already has a capture"},
		{`[[echo a] [cat]] | * ; wl`, true, ""},
		{`[[make] 2>&1 [grep y]] | ;`, true, ""},
		{`[[make] 2>&1 [grep y]] 0 nth ;`, false, "different redirects"},
		{`[echo hi] e ; len wl`, true, ""},
		{`(1 wl) "out.txt" > x`, true, ""},
		{`[[1]] ;`, true, ""},
		{`[{a: 1}] ;`, false, "a command's arguments"},

		// append widens a fresh list; a shared list keeps its type.
		{`[] 1 append "a" append len wl`, true, ""},
		{`"a" [1 2] append len wl`, true, ""},
		{`[1 2] xs! @xs "a" append drop`, false, "'append' expected int"},

		// A new dict or list around stored values: its own type may change,
		// the stored values keep theirs (formal-ver/Partial.v).
		{`[1 2] xs! {a: @xs} as {a: [int], b?: int} d!`, true, ""},
		{`[1 2] xs! {a: @xs} as {a: [int | str]} d!`, false, "'as' needs evidence"},
		{`[1] xs! {a: @xs} dup as {a: [int], b?: int} d! e!`, false, "'as' needs evidence"},
		{`[1] xs! {} "a" @xs set as {a: [int], b?: int} d!`, true, ""},
		{`[1] xs! {a: @xs, h: {k: "v"}} as {a: [int], h: {str: str}, b?: int} d!`, true, ""},
		{`[1] xs! {a: {b: @xs}} as {a: {b: [int], c?: int}} d!`, true, ""},
		{`[1] xs! {a: {b: @xs}} as {a: {b: [int | str]}} d!`, false, "'as' needs evidence"},
		{`[1] xs! [@xs] as [[int] | str] l!`, true, ""},
		{`[1] xs! [@xs] as [[int | str]] l!`, false, "'as' needs evidence"},
		{`def f ({a?: [int], b?: int} -- ) drop end [] j! {a: @j} f`, true, ""},
		// zipPack and tarPack: one form per stored list type, and the
		// built-in PackEntry alias. A new literal fits several forms with
		// the same outputs, and takes the first.
		{`(["a" "b"] "x.zip" zipPack) drop`, true, ""},
		{"([\"a\" `b` {path: `c`, mode: 420}] \"x.zip\" zipPack) drop", true, ""},
		{`("a\nb" lines files! @files "x.zip" zipPack) drop`, true, ""},
		{"([`a`] as [path] ps! @ps \"x.tar\" tarPack) drop", true, ""},
		{"([\"a\" `b`] ps! @ps \"x.zip\" zipPack) drop", true, ""},
		{"([{path: `a`, archivePath: \"x\", mode: 420} \"b\"] as [PackEntry] es! @es {path: \"x.tgz\", compress: true} tarPack) drop", true, ""},
		{"([{path: `a`, mode: 420}] es! @es \"x.zip\" zipPack) drop", false, "where it is made"},
		{`([] "x.zip" zipPack) drop`, true, ""},
		{`([1 2] "x.zip" zipPack) drop`, false, "no matching overload for 'zipPack'"},
		{`def pack ([PackEntry] -- ) "x.zip" zipPack end ([] as [PackEntry] pack) drop`, true, ""},
		// Built-in aliases name the record types builtins take and give.
		{`([] jar! {url: "x", cookieJar: @jar} httpGet drop) drop`, true, ""},
		{`([] as [Cookie] jar! {url: "x", cookieJar: @jar} as HttpRequest r! @r httpGet drop @r httpPost drop) drop`, true, ""},
		{`({url: "x", timeout: 5} as HttpRequest r! @r httpGet ? :status? drop) drop`, true, ""},
		{`({url: "x"} r! @r httpGet drop) drop`, false, "give it the type the word takes where it is made"},
		{`({url: "x"} httpGet ? resp! @resp :body? drop @resp :cookieJar? drop) drop`, true, ""},
		{`{decimals: 2} as NumFmtOptions o! 3.14159 @o numFmt wl`, true, ""},
		{`{overwrite: true} as ExtractOptions o! ("a.zip" "d" @o zipExtract) drop`, true, ""},
		{`{path: "x.tgz", compress: true} as TarDest d! (["a"] @d tarPack) drop`, true, ""},
		{`("a.zip" zipList (:name? wl) each) drop`, true, ""},
		{`"HOME" envInspect (:kind? wl) each`, true, ""},
		{`"<a>; rel=next" parseLinkHeader (:url? wl) each`, true, ""},
		// A label whose type mentions a variable solved by an earlier label.
		{`{a: []} as {a?: [int]} drop`, true, ""},

		// Unions of distinct kinds only.
		{`def f ([int] | [str] -- ) drop end`, false, "two members of the same kind"},
		{`def f (int | [str] -- ) drop end`, true, ""},

		// The dict forms of map, filter and urlEncode read values at the
		// type of Get-Key; the new dict is fresh only over immutable values.
		{`{a: 1, b: 2} (1 +) map as {str: int | str} drop`, true, ""},
		{`{a: 1, b: "x"} (1 +) map drop`, false, "no matching overload for '+'"},
		{`{a: [1]} (drop true) filter as {str: [int | str]} drop`, false, "'as' needs evidence"},
		{`{a: 1} (drop true) filter as {str: int | str} drop`, true, ""},
		{`[1 2] xs! {q: "a", n: 2, l: @xs} urlEncode wl`, true, ""},
		{`{a: 1.5} urlEncode wl`, false, "a str, int or path, or a list of them"},

		// Grids: a schema is a record of columns; a literal's is exact.
		{`[| a, b; 1, "x"; 2, "y" |] "a" gridCol sum wl`, true, ""},
		{`[| a; 1; "x" |] "a" gridCol sum wl`, false, "no matching overload for 'sum'"},
		{`[| a; 1 |] "b" gridCol drop`, false, "the grid has no column 'b'"},
		{`[| a; 1 |] :0: :a? 1 + wl`, true, ""},
		{`[| a; 1 |] (:a? 1 >) filter "a" gridCol sum wl`, true, ""},
		{`[| a; drop |] drop`, false, "stack underflow"},
		// P6: a column's type changes in place only on a new grid.
		{`[| a; 1 |] g!  @g "a" (str) updateCol drop`, false, "make a new one first with deepCopy"},
		{`[| a; 1 |] g!  @g deepCopy "a" (str) updateCol "a" gridCol (str) map drop`, true, ""},
		{`[| a; 1 |] "a" (str) updateCol "a" gridCol (str) map drop`, true, ""},
		{`[| a; 1 |] g!  @g "a" (1 +) updateCol "a" gridCol sum wl`, true, ""},
		{`[| a; 1 |] g!  @g gridCompact "a" (str) updateCol drop`, false, "make a new one first with deepCopy"},
		{`[| a; 1 |] g!  @g (:a? 1 >) filter "a" (str) updateCol "a" gridCol (str) map drop`, true, ""},
		{`[| a; 1 |] "a" (drop [1]) updateCol drop`, false, "not a list, dict or grid"},
		// extend writes at the receiver's column types, or widens a new grid.
		{`[| a; 1 |] g!  @g [| a; 2 |] extend drop`, true, ""},
		{`[| a; 1 |] g!  @g [| a; "s" |] extend drop`, false, "changes their type in place"},
		{`[| a; 1 |] [| a; "s" |] extend "a" gridCol drop`, true, ""},
		{`[| a; 1 |] g!  @g (:a? 1 >) filter [| a; "s" |] extend drop`, false, "changes their type in place"},
		// Adding, removing or renaming a column needs a new grid.
		{`[| a; 1 |] "b" [2] gridAddCol "b" gridCol sum wl`, true, ""},
		{`[| a; 1 |] g!  @g "b" [2] gridAddCol drop`, false, "changes the grid's columns in place"},
		{`[| a, b; 1, 2 |] "b" gridRemoveCol "b" gridCol drop`, false, "the grid has no column 'b'"},
		{`[| a; 1 |] "a" "c" gridRenameCol "c" gridCol sum wl`, true, ""},
		// gridSetCell writes at the column's type, even on a new grid.
		{`[| a; 1 |] "a" 0 5 gridSetCell drop`, true, ""},
		{`[| a; 1 |] "a" 0 "s" gridSetCell drop`, false, "writes a cell at its column's type"},
		// The unknown schema is read only; toGrid's columns are strings.
		{`def f (Grid -- int) gridRows end  [| a; 1 |] f wl`, true, ""},
		{`def f (Grid -- ) "a" 0 1 gridSetCell drop end`, false, "the grid's columns are not known"},
		{`def f (Grid Grid -- ) extend drop end`, false, "changes their type in place"},
		{`"a,b\n1,2" parseCsv toGrid :0: :a? 1 + wl`, false, "no matching overload for '+'"},
		{`"a,b\n1,2" parseCsv toGrid g!  @g "a" 0 "x" gridSetCell drop`, true, ""},
		{`"a,b\n1,2" parseCsv toGrid "a" (toInt?) updateCol g!  @g "a" 0 "x" gridSetCell drop`, false, "writes a cell"},
		// select, exclude, derive, map, +, joins, pivot and groupBy compute
		// the result's columns.
		{`[| a, b; 1, "x" |] ["b"] select "b" gridCol (str) map drop`, true, ""},
		{`[| a; 1 |] ["b"] select drop`, false, "the grid has no column 'b'"},
		{`[| a, b; 1, "x" |] ["b"] exclude "b" gridCol drop`, false, "the grid has no column 'b'"},
		{`[| a; 1 |] "d" {} (:a? 2 *) derive "d" gridCol sum wl`, true, ""},
		{`[| a; 1 |] (toDict d! {"b": @d :a? str}) map "b" gridCol (str) map drop`, true, ""},
		{`[| a; 1 |] (drop 5) map drop`, false, "a dict or a GridRow"},
		{`[| a; 1 |] [| a; "s" |] + "a" gridCol drop`, true, ""},
		{`[| a; [1] |] [| a; ["s"] |] + drop`, false, "no common type"},
		{`[| a, b; 1, 2 |] [| a; 1 |] + drop`, false, "the same columns"},
		{`[| a; 1 |] [| b; 2 |] (:a?) (:b?) join "b" gridCol sum wl`, true, ""},
		{`[| a; 1 |] [| b; 2 |] (:a?) (:b?) leftJoin "b" gridCol sum wl`, false, "no matching overload for 'sum'"},
		{`[| a; 1 |] [| a; 2 |] (:a?) (:a?) join drop`, false, "both grids have a column 'a'"},
		{`[| a; 1 |] [| b; 2 |] (toDict) (:b?) join drop`, false, "a join key"},
		{`[| r, m, v; "e", "j", 1 |] ["r"] "m" ("v" gridCol sum) pivot "r" gridCol drop`, true, ""},
		{`[| r, m, v; "e", "j", 1 |] ["r"] "m" ("v" gridCol) pivot drop`, false, "not a list, dict or grid"},
		{`[| r, v; "e", 1 |] ["r"] [{"name": "n", "agg": (gridRows)} {"agg": (drop "x")}] groupBy` +
			` dup "n" gridCol sum wl "AggCol2" gridCol (wl) each`, true, ""},
		{`[| r, v; "e", 1 |] ["r"] [{"agg": ("v" gridCol)}] groupBy drop`, false, "not a list, dict or grid"},

		// new on def outputs: exactly what the body does.
		{`def f ( -- new Json) "c.json" readFile parseJson end  f drop`, true, ""},
		{`def f ( -- Json) "c.json" readFile parseJson end`, false, "mark it `new`"},
		{`def f ( -- new Json) "c.json" readFile parseJson j! @j end`, false, "is marked `new`, but"},
		{`def f ( -- new int) 1 end`, false, "`new` on it means nothing"},
		{`def f (int -- new [int]) dup 0 = if drop [] else 1 - f end end`, true, ""},
		{`def f (int -- [int]) dup 0 = if drop [] else 1 - f end end`, false, "mark it `new`"},
		{`def f (a -- new a) deepCopy end`, true, ""},
		{`def f (bool -- [int]) if [1] return end [2] l! @l end`, true, ""},
		{`def f (bool -- new [int]) if [1] l! @l return end [2] end`, false, "at the `return` on line 1"},
		{`def f ( -- new [int]) [1] end  f as [int | str] drop`, true, ""},

		// Definite assignment: every path to a read stores the variable.
		{`true if 1 x! end @x wl`, false, "a path reaches this read without setting it"},
		{`true if 1 x! else 2 x! end @x wl`, true, ""},
		{`true if 1 x! else 1 exit end @x wl`, true, ""},
		{`[1 2] (x!) each @x wl`, false, "a path reaches this read"},
		{`(@x wl) q! 5 x! @q x`, true, ""},
		{`0 (drop 5 x! 1 break) loop drop @x wl`, true, ""},
		{`(5 x!) x @x wl`, true, ""},
		{`true (5 x!) (6 x!) iff @x wl`, true, ""},
		{`true (5 x!) iff @x wl`, false, "a path reaches this read"},
		{`5 just match just v : @v wl , none : end @v wl`, false, "a path reaches this read"},
		{`@y wl`, false, "unknown identifier"},

		// Several candidates with different outputs: the one the arguments fit
		// as they are (plan question 2, decided 2026-10-01).
		{`[3 1 2] uniq (1 +) map drop`, true, ""},
		{`[1 "a" 2025-07-07 0.1] uniq len wl`, true, ""},
		{`[1 "a"] xs! @xs uniq drop`, false, "a stored value keeps its type"},
		{`[1 "a"] as [str | path | int | float | datetime] xs! @xs uniq drop`, true, ""},
		{`[1 "a" ` + "`p`" + `] sort (len) map drop`, true, ""},
		{`[1.5 "a"] sort drop`, false, "no matching overload for 'sort'"},

		// ⊥ joins to the other side; it does not solve a variable there.
		{`[] x! {a: @x} (len) map drop  @x 1 append drop`, true, ""},
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
	base := NewCoreBase(benchStdlib(b), nil)
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
	base := NewCoreBase(benchStdlib(b), nil)
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
		NewCoreBase(std, nil)
	}
}

// TestCoreDeclarations covers `type` and `enum` declarations, constructors,
// member patterns and the names rule (ai/type-system-plan.md, stage 4).
func TestCoreDeclarations(t *testing.T) {
	base := NewCoreBase(nil, nil)
	cases := []struct {
		src  string
		ok   bool
		want string // a part of the first error, when !ok
	}{
		// Names do not shadow each other.
		{`def f ( -- ) end  def f ( -- ) end`, false, "'f' is already defined at 1:5"},
		{`def sum ( -- ) end`, false, "'sum' is the name of a builtin"},
		{`enum E = a | b end  def a ( -- ) end`, false, "'a' is the name of the definition at"},
		{`enum E = a | len end`, false, "'len' is the name of a builtin"},
		{`enum E = a end  enum F = a end`, false, "'a' is already declared at 1:10"},
		{`type T = int  enum T = a end`, false, "'T' is already declared"},
		{`type Json = int`, false, "'Json' is a built-in type"},
		{`enum E = a | just end`, false, "meaning of its own in match patterns"},
		{`enum E[a a] = c a end`, false, "the parameter 'a' is written twice"},
		// Generic enums: arguments, recursion, unions.
		{`enum Box[a] = box [a] end  def f (Box -- ) drop end`, false, "takes 1 argument(s)"},
		{`enum Box[a] = box [a] end  def f (Box[int str] -- ) drop end`, false, "takes 1 argument(s)"},
		{`enum E = a end  def f (E[int] -- ) drop end`, false, "has no parameters"},
		{`enum E[a] = c [a | int] end`, false, "an enum parameter cannot be a member of a union"},
		{`type Id = int  enum E = c Id end  5 c drop`, true, ""},
		{`enum E = c Later end  type Later = [E]  [] c drop`, true, ""},
		// Constructors.
		{`enum Opt[a] = some a | nothing end  nothing o!  5 some o!  @o drop`, true, ""},
		{`enum Opt[a] = some a | nothing end  def f (Opt[int] -- ) drop end  nothing f`, true, ""},
		{`enum F = f (int -- int) end  (1 +) f drop`, true, ""},
		{`enum F = f (int -- int) end  ("a" ++) f drop`, false, ""},
		{`enum P = p int str end  "a" 1 p drop`, false, "'p' expected"},
		// Member patterns and coverage.
		{`enum E = c int end  1 c match c : end`, false, "has 1 payload value(s)"},
		{`enum E = c int | d end  def f (E -- int) match c n : @n, d : 0 end end`, true, ""},
		{`enum E = c int | d end  def f (E -- int) match E e : 1 end end`, true, ""},
		{`enum E = c int | d end  def f (E -- int) match c n : @n end end`, false, "non-exhaustive"},
		{`enum E = c int | d end  def f (E | str -- int) match c n : @n, d : 0, str s : 1 end end`, true, ""},
		{`enum Box[a] = box [a] | empty end  def g (a -- ) match Box :> drop, _ : end end`, true, ""},
		// Equality: payloads must be equatable.
		{`enum E = a | b end  a b = drop`, true, ""},
		{`enum P = p int end  1 p 2 p = drop`, true, ""},
		{`enum B = box [int] end  [1] box [1] box = drop`, false, ""},
		{`enum T = leaf int | node T T end  1 leaf 2 leaf = drop`, true, ""},
		// Enums whose arguments grow as they refer to each other: = is
		// refused, and the check ends.
		{`enum A[t] = a B[[t]] | an t end  enum B[t] = b A[t] | bn end  1 an x1!  @x1 @x1 = drop`, false, "no matching overload for '='"},
	}
	for _, tc := range cases {
		errs, ok := coreCheck(t, base, tc.src)
		if ok != tc.ok {
			t.Errorf("%q: ok = %v, want %v; errors: %v", tc.src, ok, tc.ok, errs)
			continue
		}
		if !ok && tc.want != "" && !strings.Contains(errs[0], tc.want) {
			t.Errorf("%q: first error %q does not contain %q", tc.src, errs[0], tc.want)
		}
	}
}

// TestCoreStartupDeclarations checks that the startup files' declarations
// are seen by every check, and keep their names.
func TestCoreStartupDeclarations(t *testing.T) {
	init, err := NewMShellParser(NewLexer("enum Color = red | green end\ntype Point = {px: int}\n"+
		"def colorName (Color -- str) match red : \"r\", green : \"g\" end end\n"+
		"def mkColor ( -- Color) red end\n", &TokenFile{"init.msh"})).ParseFile()
	if err != nil {
		t.Fatal(err)
	}
	// The startup files' signatures name their own declarations.
	base := NewCoreBase(init.Definitions, declarationItems(init.Items))
	cases := []struct {
		src  string
		ok   bool
		want string
	}{
		{`def name (Color -- str) match red : "r", green : "g" end end  green name wl`, true, ""},
		{`{px: 1} as Point drop`, true, ""},
		{`def red ( -- ) end`, false, "'red' is a member of enum 'Color'"},
		{`type Point = int`, false, "'Point' is already declared at init.msh:2:6"},
		{`mkColor colorName wl`, true, ""},
		{`5 colorName wl`, false, "'colorName' expected Color"},
	}
	for _, tc := range cases {
		errs, ok := coreCheck(t, base, tc.src)
		if ok != tc.ok {
			t.Errorf("%q: ok = %v, want %v; errors: %v", tc.src, ok, tc.ok, errs)
			continue
		}
		if !ok && tc.want != "" && !strings.Contains(errs[0], tc.want) {
			t.Errorf("%q: first error %q does not contain %q", tc.src, errs[0], tc.want)
		}
	}
}

// TestCoreStartupDeclarationErrors checks that an error in a startup file's
// declaration names that file.
func TestCoreStartupDeclarationErrors(t *testing.T) {
	init, err := NewMShellParser(NewLexer("enum E = a Foo | b end\nenum F[t t] = c t end", &TokenFile{"init.msh"})).ParseFile()
	if err != nil {
		t.Fatal(err)
	}
	base := NewCoreBase(nil, declarationItems(init.Items))
	errs, ok := coreCheck(t, base, `1 drop`)
	if ok || len(errs) < 2 {
		t.Fatalf("got %v", errs)
	}
	for _, e := range errs {
		if !strings.HasPrefix(e, "in init.msh: ") {
			t.Errorf("error without its file: %q", e)
		}
	}
}
