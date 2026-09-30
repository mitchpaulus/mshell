package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// The examples of formal-ver/Decide.v, printed from the Rocq terms by the
// oracle (formal-ver/oracle/examples.txt): each line is a query, a tab, and
// the answer the proof gives.
func TestRelationsOracleExamples(t *testing.T) {
	f, err := os.Open("../formal-ver/oracle/examples.txt")
	if err != nil {
		t.Skipf("no oracle examples: %v", err)
	}
	defer f.Close()
	o := newOracleTypes()
	r := NewRelations(o.arena)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		query, want, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("no answer on line %q", line)
		}
		ok, got, err := o.agrees(r, query, want)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if !ok {
			t.Errorf("%s\n  want %s\n  got  %s", query, want, got)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no examples read")
	}
}

// relCase is one question about two types written in oracle syntax.
type relCase struct {
	name string
	a, b string
	want bool
}

func runSubCases(t *testing.T, retype bool, cases []relCase) {
	t.Helper()
	o := newOracleTypes()
	r := NewRelations(o.arena)
	for _, c := range cases {
		a, b := o.mustParse(t, c.a), o.mustParse(t, c.b)
		var got bool
		if retype {
			got = r.Retype(a, b)
		} else {
			got = r.Sub(a, b)
		}
		if got != c.want {
			t.Errorf("%s: %s %s: got %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

const (
	jsonT   = "(mu (union bool (union int (union str (union (list (rv 0)) (dict (rv 0)))))))"
	boxInv  = "(enum Box mut (inv/fresh) (%s))" // enum Box[a] = box [a] | empty end
	quoteF  = "(enum F imm (inv) (%s))"         // enum F[a] = f (a -- a) end
	pairCo  = "(enum Pair imm (co/fresh) (%s))" // enum Pair[a] = pair a a end
	intStr  = "(union int str)"
	listInt = "(list int)"
)

func sprintf1(format, arg string) string { return strings.Replace(format, "%s", arg, 1) }

// The subtyping rules of the design document (§Subtyping, S1-S4 and the
// plan's acceptance tests).
func TestRelationsSub(t *testing.T) {
	runSubCases(t, false, []relCase{
		{"invariant list", listInt, "(list (union int str))", false},
		{"union injection", "int", intStr, true},
		{"maybe covariant", "(maybe int)", "(maybe (union int str))", true},
		{"maybe of invariant list", "(maybe (list int))", "(maybe (list (union int str)))", false},
		{"none below every maybe", "(maybe bot)", "(maybe (list int))", true},
		// P1 and S1: a required field is invariant.
		{"S1 field invariant", "(rec ((a (req int))) open)", "(rec ((a (req (union int str)))) open)", false},
		{"width", "(rec ((a (req int)) (b (req str))) abs)", "(rec ((a (req int))) open)", true},
		// P2, P4: a shape is never a {str: T}.
		{"P2 shape is not a dict", "(rec ((a (req int))) open)", "(dict int)", false},
		{"exact shape is not a dict", "(rec ((a (req int))) abs)", "(dict int)", false},
		// P3: a dict is not a shape with a required field.
		{"P3 dict is not required", "(dict int)", "(rec ((a (req int))) open)", false},
		// S2 and the plan's acceptance rows.
		{"*: T covers optional", "(rec ((url (req str))) (opt int))", "(rec ((url (req str)) (timeout (opt int))) open)", true},
		{"dict is all-optional shape", "(dict int)", "(rec ((timeout (opt int))) (opt int))", true},
		{"dict is not required shape", "(dict int)", "(rec ((timeout (req int))) open)", false},
		{"exact lacks optional", "(rec ((url (req str))) abs)", "(rec ((url (req str)) (timeout (opt int))) open)", false},
		{"open lacks optional", "(rec ((url (req str))) open)", "(rec ((url (req str)) (timeout (opt int))) open)", false},
		// S3 and S4.
		{"S3 undeclared into *: T", "(rec ((a (req int))) (opt int))", "(rec () (opt int))", true},
		{"S4 exact is not *: T", "(rec () abs)", "(rec () (opt int))", false},
		{"S4 open accepts anything", "(dict int)", "(rec () open)", true},
		{"optional is not deletable", "(rec ((a (opt int))) (dict int))", "(dict int)", false},
		// Quotes.
		{"never below every output", "(quote (str) never)", "(quote (str) (int))", true},
		{"never is not below", "(quote (str) (int))", "(quote (str) never)", false},
		{"quote contravariant", "(quote (" + intStr + ") (int))", "(quote (int) (" + intStr + "))", true},
		{"quote arity", "(quote (int) (int))", "(quote (int int) (int))", false},
		// Enums.
		{"invariant enum", sprintf1(boxInv, "int"), sprintf1(boxInv, intStr), false},
		{"covariant enum", sprintf1(pairCo, "int"), sprintf1(pairCo, intStr), true},
		// Recursive aliases.
		{"int below Json", "int", jsonT, true},
		{"[Json] below Json", "(list " + jsonT + ")", jsonT, true},
		{"stored [int] is not Json", listInt, jsonT, false},
		{"unknown is top", jsonT, "top", true},
		{"top is not below", "top", jsonT, false},
		// H13: type V = int | V is unguarded; str is not below it.
		{"H13", "str", "(mu (union int (rv 0)))", false},
		// Type variables and abstract types equal only themselves.
		{"rigid only itself", "(var 1)", "(union (var 1) int)", true},
		{"rigid is not int", "(var 1)", "int", false},
	})
}

// Fresh retyping (§Freshness).
func TestRelationsRetype(t *testing.T) {
	runSubCases(t, true, []relCase{
		{"fresh list widens", listInt, "(list (union int str))", true},
		{"fresh nested list widens", "(list (list int))", "(list (list (union int str)))", true},
		{"fresh [int] becomes Json", listInt, jsonT, true},
		{"absent becomes optional", "(rec ((url (req str))) abs)", "(rec ((url (req str)) (timeout (opt int))) open)", true},
		{"fresh shape becomes dict", "(rec ((a (req int)) (b (req str))) abs)", "(dict (union int str))", true},
		{"required stays required", "(rec ((a (opt int))) abs)", "(rec ((a (req int))) abs)", false},
		// H4: a fresh box widens; H5: an argument under a quote does not.
		{"H4 fresh box", sprintf1(boxInv, listInt), sprintf1(boxInv, "(list (union int str))"), true},
		{"H5 quote argument", sprintf1(quoteF, "int"), sprintf1(quoteF, intStr), false},
		// Quotes are retyped only by <=.
		{"quote not widened", "(quote (int) (int))", "(quote (" + intStr + ") (" + intStr + "))", false},
		// H12: one assumption set per relation.
		{"H12",
			"(mu (rec ((x (req (list int))) (f (req (quote () ((rv 0)))))) abs))",
			"(mu (rec ((x (req (list (union int str)))) (f (req (quote () ((rv 0)))))) abs))", false},
		{"below is also retype", "int", intStr, true},
	})
}

// The join table of the design document (§Joins) and the cases of Join.v.
func TestRelationsJoin(t *testing.T) {
	cases := []struct {
		name string
		p, q string // slots
		want string // slot, or "none"
	}{
		{"int float", "(shared int)", "(shared str)", "(shared (union int str))"},
		{"none and just", "(shared (maybe bot))", "(shared (maybe int))", "(shared (maybe int))"},
		{"fresh lists", "(fresh (list int))", "(fresh (list str))", "(fresh (list (union int str)))"},
		{"shared lists", "(shared (list int))", "(shared (list str))", "none"},
		{"fresh and shared lists", "(fresh (list int))", "(shared (list str))", "none"},
		{"fresh shapes", "(fresh (rec ((a (req int))) abs))", "(fresh (rec ((a (req int)) (b (req int))) abs))",
			"(fresh (rec ((a (req int)) (b (opt int))) abs))"},
		{"shared shapes", "(shared (rec ((a (req int))) abs))", "(shared (rec ((a (req int)) (b (req int))) abs))", "none"},
		{"int and Json", "(shared int)", "(fresh " + jsonT + ")", "(shared " + jsonT + ")"},
		{"fresh [int] and Json", "(fresh (list int))", "(fresh " + jsonT + ")", "(fresh " + jsonT + ")"},
		{"stored [int] and Json", "(shared (list int))", "(fresh " + jsonT + ")", "none"},
		{"shared maybe lists", "(shared (maybe (list int)))", "(shared (maybe (list str)))", "none"},
		{"fresh maybe lists", "(fresh (maybe (list int)))", "(fresh (maybe (list str)))", "(fresh (maybe (list (union int str))))"},
		{"unrelated quotes", "(fresh (quote () (int)))", "(fresh (quote () (str)))", "none"},
		{"never quote", "(shared (quote (str) never))", "(shared (quote (str) (str)))", "(shared (quote (str) (str)))"},
		{"two enums", "(shared (enum Shape imm () ()))", "(shared (enum LoadError imm () ()))",
			"(shared (union (enum Shape imm () ()) (enum LoadError imm () ())))"},
		{"union below union", "(shared (union int str))", "(shared (union int (union str bool)))", "(shared (union int (union str bool)))"},
		{"fresh boxes", "(fresh " + sprintf1(boxInv, "int") + ")", "(fresh " + sprintf1(boxInv, "str") + ")",
			"(fresh " + sprintf1(boxInv, intStr) + ")"},
		{"join into union", "(fresh (union int (list int)))", "(fresh (list str))", "(fresh (union int (list (union int str))))"},
		{"add to union", "(shared (union int str))", "(shared bool)", "(shared (union int (union str bool)))"},
		{"two unrelated unions", "(shared (union int str))", "(shared (union int bool))", "none"},
		{"shared shape below", "(shared (rec ((a (req int)) (b (req int))) abs))", "(shared (rec ((a (req int))) open))",
			"(shared (rec ((a (req int))) open))"},
		{"two recursive types",
			"(fresh (mu (rec ((x (req (list (rv 0)))) (y (req int))) open)))",
			"(fresh (mu (rec ((x (req (list (rv 0)))) (y (req str))) open)))", "none"},
	}
	o := newOracleTypes()
	r := NewRelations(o.arena)
	for _, c := range cases {
		line := "join 30 " + c.p + " " + c.q
		ok, got, err := o.agrees(r, line, c.want)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !ok {
			t.Errorf("%s: %s\n  want %s\n  got  %s", c.name, line, c.want, got)
		}
	}
}

func TestRelationsImmutableCheckable(t *testing.T) {
	o := newOracleTypes()
	r := NewRelations(o.arena)
	cases := []struct {
		ty                  string
		immutable, checkable bool
	}{
		{"int", true, true},
		{"(maybe int)", true, true},
		{"(maybe (list int))", false, true},
		{"(quote (int) (int))", true, false},
		{"(var 1)", false, false},
		{"top", false, true},
		{jsonT, false, true},
		{"(mu (union int (maybe (rv 0))))", true, true},
		{sprintf1(boxInv, "int"), false, true},
		{sprintf1(pairCo, "int"), true, true},
		{sprintf1(pairCo, listInt), false, true},
		{"(rec ((f (req (quote () ())))) open)", false, false},
	}
	for _, c := range cases {
		id := o.mustParse(t, c.ty)
		if got := r.Immutable(id); got != c.immutable {
			t.Errorf("Immutable(%s) = %v, want %v", c.ty, got, c.immutable)
		}
		if got := r.Checkable(id); got != c.checkable {
			t.Errorf("Checkable(%s) = %v, want %v", c.ty, got, c.checkable)
		}
	}
	if k := o.arena.MakeAbstract(); r.Immutable(k) || r.Checkable(k) {
		t.Errorf("an abstract type is neither immutable nor checkable")
	}
}

func TestRelationsKinds(t *testing.T) {
	o := newOracleTypes()
	r := NewRelations(o.arena)
	json := o.mustParse(t, jsonT)
	ks, ok := r.Kinds(json)
	if !ok || len(ks) != 5 {
		t.Fatalf("Kinds(Json) = %v, %v; want 5 kinds", ks, ok)
	}
	withList := o.arena.MakeUnion([]TypeId{json, o.mustParse(t, listInt)}, NameNone)
	ks2, _ := r.Kinds(withList)
	if kindsDisjoint(ks, ks2[len(ks2)-1:]) {
		t.Errorf("Json | [int] should repeat the list kind")
	}
	if _, ok := r.Kinds(o.mustParse(t, "(mu (union int (rv 0)))")); ok {
		t.Errorf("an unguarded alias has no kinds")
	}
}
