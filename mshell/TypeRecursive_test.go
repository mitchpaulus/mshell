package main

import (
	"strings"
	"testing"
)

// Recursive named types: a `type` body may refer to its own name and to
// names declared later in the file, as long as the reference sits inside a
// list, dict, shape field, Maybe, or quotation.

func expectTypeOk(t *testing.T, src string) {
	t.Helper()
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("expected program to type-check; errs=%v\nsource:\n%s", errs, src)
	}
}

func expectTypeErr(t *testing.T, src string, want string) []string {
	t.Helper()
	errs, ok := parseAndCheck(t, src)
	if ok || len(errs) == 0 {
		t.Fatalf("expected a type error containing %q; got none\nsource:\n%s", want, src)
	}
	for _, e := range errs {
		if strings.Contains(e, want) {
			return errs
		}
	}
	t.Fatalf("expected an error containing %q; got %v", want, errs)
	return nil
}

func TestRecursiveShapeDecl(t *testing.T) {
	expectTypeOk(t, `
type Node = {name: str, children: [Node]}
def childCount (Node -- int) :children? len end
def firstGrandchildName (Node -- str) :children? :0: :children? :0: :name? end
{"name": "root", "children": []} as Node childCount wl
{"name": "root", "children": []} as Node firstGrandchildName wl
`)
}

func TestMutuallyRecursiveDecls(t *testing.T) {
	// B is declared after A and A refers to it: forward references work.
	expectTypeOk(t, `
type A = {b: [B]}
type B = {a: Maybe[A]}
def count (A -- int) :b? len end
{"b": []} as A count wl
`)
}

func TestRecursiveUnionDeclWithKeywordNarrowing(t *testing.T) {
	// `list l` on a union subject binds only the list members, so the
	// recursive call sees `[Json]`, not the whole union.
	expectTypeOk(t, `
type Json = null | bool | int | float | str | [Json] | {str: Json}
def depth (Json -- int)
  match
    list l : @l (depth) map 0 append max 1 +,
    dict d : @d values (depth) map 0 append max 1 +,
    _ : 1,
  end
end
"[1,[2,[3]]]" parseJson as Json depth wl
`)
}

func TestGuardedMutualRecursiveUnionsOk(t *testing.T) {
	// A refers to B directly, B refers back to A through a list: the
	// cycle has a guard, so it is a legitimate type.
	expectTypeOk(t, `
type A = int | B
type B = str | [A]
1 as A drop
`)
}

func TestUnguardedRecursiveTypesRejected(t *testing.T) {
	cases := []string{
		"type T = int | T",
		"type A = B\ntype B = A",
		"type A = int | B\ntype B = str | A",
		"type A = A",
	}
	for _, src := range cases {
		expectTypeErr(t, src, "refers to itself with nothing in between")
	}
}

func TestRecursiveTypePrintsFinitely(t *testing.T) {
	// The cast error prints the declared type; the body is printed once
	// and the recursive reference prints the name alone.
	errs := expectTypeErr(t, `
type Node = {children: [Node]}
1 as Node drop
`, "invalid cast")
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "Node({children: [Node]})") {
		t.Fatalf("expected the recursive type to print as Node({children: [Node]}); got:\n%s", joined)
	}
}

func TestHtmlNodeBuiltinType(t *testing.T) {
	expectTypeOk(t, `
def childCount (HtmlNode -- int) :children? len end
"<p>x</p>" parseHtml childCount wl
"<p>x</p>" parseHtml :children? :0: :children? :0: :attr? keys len wl
"<p>x</p>" parseHtml :text? wl
`)
	expectTypeErr(t, "type HtmlNode = int", "")
	expectTypeErr(t, `"<p>x</p>" parseHtml as {str: int} drop`, "invalid cast")
}

func TestHtmlNodeGetWithLiteralKeyResolvesField(t *testing.T) {
	// The stdlib html helpers read fields through `get`; the brand
	// resolves the shape field the same way the getter does.
	expectTypeOk(t, `
def desc (HtmlNode [HtmlNode] -- [HtmlNode])
    node!, accum!
    @node "children" get? (c! @c @accum desc drop) each
    @accum @node append
end
"<p>x</p>" parseHtml [] desc len wl
`)
}

func TestDictAndListKeywordsInSignatures(t *testing.T) {
	expectTypeOk(t, "def f (dict -- int) len end\n{\"a\": 1} f wl")
	expectTypeOk(t, "def f (list -- int) len end\n[1 2] f wl")
	expectTypeOk(t, "def f (date -- str) str end\n2024-01-01 f wl")
	// A bare int must not flow into a `dict` parameter.
	expectTypeErr(t, "def f (dict -- int) drop 1 end\n1 f wl", "got int")
	// Without a generic scope the value type has nowhere to go.
	expectTypeErr(t, "1 as dict drop", "needs a value type here")
	expectTypeErr(t, "def f (quotation -- ) drop end", "'quotation' is not a type name")
	expectTypeErr(t, "def f (maybe -- ) drop end", "'maybe' is not a type name")
}

func TestCastLiteralIntoRecursiveTypeAtDepth(t *testing.T) {
	// A nested literal satisfies the brand's body at every depth inside a
	// cast; nominal checking still applies outside casts and a value that
	// already carries another brand never re-brands.
	expectTypeOk(t, `
type Tree = {name: str, kids: [Forest]}
type Forest = {trees: [Tree]}
{"trees": [{"name": "a", "kids": [{"trees": []}]}]} as Forest drop
`)
	expectTypeOk(t, `
type P = {n: int}
type T = {ms: [P]}
{"ms": [{"n": 1}]} as T drop
`)
	expectTypeErr(t, `
type P = {n: int}
def f (P -- int) :n? end
{"n": 1} f wl
`, "expected P")
	expectTypeErr(t, `
type A = {n: int}
type B = {n: int}
type T = {ms: [B]}
{"ms": [{"n": 1} as A]} as T drop
`, "invalid cast")
}
