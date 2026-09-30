package main

// Random types over the model's type language, for comparing the Go
// relations with the proved ones (formal-ver/oracle) and for property tests.
//
// Generated types follow the rules the checker enforces on real types:
// union members have distinct kinds (looking through aliases), and every
// cycle of alias references passes a type constructor. bot and unknown are
// never union members.

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

type typeGen struct {
	rng     *rand.Rand
	o       *oracleTypes
	r       *Relations
	enums   []uint32
	aliases []TypeId
}

func newTypeGen(seed int64) *typeGen {
	g := &typeGen{rng: rand.New(rand.NewSource(seed)), o: newOracleTypes()}
	g.r = NewRelations(g.o.arena)
	a := g.o.arena
	params := []EnumParam{
		{Variance: VarCo, Fresh: true}, {Variance: VarCo}, {Variance: VarInv, Fresh: true},
		{Variance: VarInv}, {Variance: VarContra},
	}
	for i := 0; i < 5; i++ {
		decl := EnumDecl{Name: g.o.names.Intern(fmt.Sprintf("E%d", i)), Immutable: g.rng.Intn(2) == 0, Checkable: true}
		for j := 0; j <= g.rng.Intn(2); j++ {
			p := params[g.rng.Intn(len(params))]
			p.Name = g.o.names.Intern(fmt.Sprintf("p%d", j))
			decl.Params = append(decl.Params, p)
		}
		g.enums = append(g.enums, a.DeclareEnum(decl))
	}
	// Aliases whose bodies refer to aliases only under a constructor, so
	// every cycle is guarded. Each body is made after all names exist, so
	// aliases can refer to each other.
	n := 2 + g.rng.Intn(3)
	idxs := make([]uint32, n)
	for i := range idxs {
		idxs[i] = a.DeclareAlias(g.o.names.Intern(fmt.Sprintf("A%d", i)))
		g.aliases = append(g.aliases, a.MakeAliasRef(idxs[i]))
	}
	for i, idx := range idxs {
		var body TypeId
		for {
			body = g.ty(3, false, true)
			if _, ok := g.r.Kinds(body); ok || g.rng.Intn(4) == 0 {
				break
			}
		}
		a.SetAliasBody(idx, body)
		_ = i
	}
	return g
}

// ty makes a random type. underCtor says a type constructor encloses it
// (so an alias reference here is guarded); inAlias says it is part of an
// alias body (where only guarded references are allowed).
func (g *typeGen) ty(depth int, underCtor, inAlias bool) TypeId {
	a := g.o.arena
	if depth <= 0 {
		return g.leaf(underCtor, inAlias)
	}
	switch g.rng.Intn(14) {
	case 0, 1:
		return g.leaf(underCtor, inAlias)
	case 2:
		return a.MakeMaybe(g.ty(depth-1, true, inAlias))
	case 3, 4:
		return a.MakeList(g.ty(depth-1, true, inAlias))
	case 5:
		return a.MakeStrDict(g.ty(depth-1, true, inAlias))
	case 6, 7:
		return g.record(depth, inAlias)
	case 8, 9:
		return g.union(depth, underCtor, inAlias)
	case 10:
		return g.quote(depth, inAlias)
	case 11:
		idx := g.enums[g.rng.Intn(len(g.enums))]
		args := make([]TypeId, len(a.enumDecls[idx].Params))
		for i := range args {
			args[i] = g.ty(depth-1, true, inAlias)
		}
		return a.MakeEnum(idx, args)
	}
	return g.leaf(underCtor, inAlias)
}

func (g *typeGen) leaf(underCtor, inAlias bool) TypeId {
	switch g.rng.Intn(12) {
	case 0, 1, 2:
		return TidInt
	case 3, 4:
		return TidStr
	case 5:
		return TidBool
	case 6:
		return TidBottom
	case 7:
		return TidUnknown
	case 8:
		// An alias is not generic: its body mentions no type variable.
		if !inAlias {
			return g.o.arena.MakeRigid(g.o.names.Intern(fmt.Sprintf("v%d", g.rng.Intn(2))))
		}
	}
	if len(g.aliases) > 0 && (underCtor || !inAlias) {
		return g.aliases[g.rng.Intn(len(g.aliases))]
	}
	return TidInt
}

var fieldNames = []string{"a", "b", "c"}

func (g *typeGen) status(depth int, inAlias bool, allowNoType bool) RecordField {
	for {
		s := FieldStatus(g.rng.Intn(5))
		if !allowNoType && (s == FieldAbsent || s == FieldOpen) {
			continue
		}
		f := RecordField{Status: s}
		if s != FieldAbsent && s != FieldOpen {
			f.Type = g.ty(depth-1, true, inAlias)
		}
		return f
	}
}

func (g *typeGen) record(depth int, inAlias bool) TypeId {
	var fields []RecordField
	for _, name := range fieldNames {
		if g.rng.Intn(2) == 0 {
			f := g.status(depth, inAlias, true)
			f.Name = g.o.names.Intern(name)
			fields = append(fields, f)
		}
	}
	return g.o.arena.MakeRecord(fields, g.status(depth, inAlias, true))
}

// union makes a union whose members have distinct kinds.
func (g *typeGen) union(depth int, underCtor, inAlias bool) TypeId {
	var members []TypeId
	var seen []valueKind
	for tries := 0; tries < 6 && len(members) < 3; tries++ {
		m := g.ty(depth-1, underCtor, inAlias)
		if m == TidBottom || m == TidUnknown {
			continue
		}
		ks, ok := g.r.Kinds(m)
		if !ok || len(ks) == 0 || !kindsDisjoint(ks, seen) {
			continue
		}
		members = append(members, m)
		seen = append(seen, ks...)
	}
	if len(members) == 0 {
		return TidInt
	}
	return g.o.arena.MakeUnion(members, NameNone)
}

func (g *typeGen) quote(depth int, inAlias bool) TypeId {
	var sig QuoteSig
	for i := g.rng.Intn(3); i > 0; i-- {
		sig.Inputs = append(sig.Inputs, g.ty(depth-1, true, inAlias))
	}
	if g.rng.Intn(5) == 0 {
		sig.Diverges = true
	} else {
		for i := g.rng.Intn(3); i > 0; i-- {
			sig.Outputs = append(sig.Outputs, g.ty(depth-1, true, inAlias))
		}
	}
	return g.o.arena.MakeQuote(sig)
}

// near makes a type related to t: mostly t with some parts changed in ways
// that often keep it above or below t, so that many questions answer yes.
func (g *typeGen) near(t TypeId, depth int) TypeId {
	a := g.o.arena
	if g.rng.Intn(4) == 0 {
		return t
	}
	switch g.rng.Intn(10) {
	case 0:
		return TidUnknown
	case 1:
		ks, ok := g.r.Kinds(t)
		if !ok {
			return t
		}
		extra := g.ty(1, false, false)
		ks2, ok2 := g.r.Kinds(extra)
		if ok2 && len(ks2) > 0 && extra != TidBottom && kindsDisjoint(ks, ks2) && len(ks) > 0 {
			return a.MakeUnion([]TypeId{t, extra}, NameNone)
		}
		return t
	case 2:
		return g.ty(depth, false, false)
	}
	n := a.Node(t)
	switch n.Kind {
	case TKMaybe:
		return a.MakeMaybe(g.near(TypeId(n.A), depth-1))
	case TKList:
		return a.MakeList(g.near(TypeId(n.A), depth-1))
	case TKRecord:
		rec := a.records[n.Extra]
		fields := make([]RecordField, 0, len(rec.Fields))
		for _, f := range rec.Fields {
			f2 := f
			if f.Type != TidNothing && g.rng.Intn(2) == 0 {
				f2.Type = g.near(f.Type, depth-1)
			}
			if g.rng.Intn(4) == 0 {
				f2.Status = FieldStatus(g.rng.Intn(5))
				if (f2.Status != FieldAbsent && f2.Status != FieldOpen) && f2.Type == TidNothing {
					f2.Type = g.ty(1, true, false)
				}
			}
			fields = append(fields, f2)
		}
		rest := rec.Rest
		if g.rng.Intn(4) == 0 {
			rest = g.status(depth, false, true)
		}
		return a.MakeRecord(fields, rest)
	case TKUnion:
		members := append([]TypeId(nil), a.unionMembers[n.Extra]...)
		i := g.rng.Intn(len(members))
		if g.rng.Intn(2) == 0 && len(members) > 1 {
			members = append(members[:i], members[i+1:]...)
		} else {
			m := g.near(members[i], depth-1)
			ks, ok := g.r.Kinds(m)
			old, _ := g.r.Kinds(members[i])
			if ok && len(ks) == len(old) && m != TidBottom && m != TidUnknown {
				members[i] = m
				rest := append(append([]TypeId(nil), members[:i]...), members[i+1:]...)
				others, _ := g.r.Kinds(a.MakeUnion(rest, NameNone))
				if len(rest) > 0 && !kindsDisjoint(ks, others) {
					return t
				}
			}
		}
		return a.MakeUnion(members, NameNone)
	case TKQuote:
		sig := a.quoteSigs[n.Extra]
		sig2 := QuoteSig{Diverges: sig.Diverges}
		for _, x := range sig.Inputs {
			sig2.Inputs = append(sig2.Inputs, g.near(x, depth-1))
		}
		for _, x := range sig.Outputs {
			sig2.Outputs = append(sig2.Outputs, g.near(x, depth-1))
		}
		if g.rng.Intn(6) == 0 {
			sig2.Diverges = !sig2.Diverges
			if !sig2.Diverges {
				sig2.Outputs = nil
			}
		}
		if sig2.Diverges {
			sig2.Outputs = nil
		}
		return a.MakeQuote(sig2)
	case TKEnum:
		args := append([]TypeId(nil), a.enumArgs[n.Extra]...)
		for i := range args {
			if g.rng.Intn(2) == 0 {
				args[i] = g.near(args[i], depth-1)
			}
		}
		return a.MakeEnum(n.A, args)
	case TKAlias:
		if g.rng.Intn(2) == 0 {
			return a.aliases[n.A].Body
		}
	}
	return t
}

// widen makes a type that is often above t: below it by <= when fresh is
// false, and by fresh retyping when it is true. It is a heuristic; the
// tests check what the relations say, not what widen meant.
func (g *typeGen) widen(t TypeId, depth int, fresh bool) TypeId {
	a := g.o.arena
	if depth <= 0 || g.rng.Intn(5) == 0 {
		return t
	}
	if t == TidBottom {
		return g.ty(2, false, false)
	}
	if g.rng.Intn(12) == 0 {
		return TidUnknown
	}
	if g.rng.Intn(4) == 0 {
		if ks, ok := g.r.Kinds(t); ok && len(ks) > 0 {
			extra := g.ty(1, false, false)
			ks2, ok2 := g.r.Kinds(extra)
			if ok2 && len(ks2) > 0 && extra != TidBottom && kindsDisjoint(ks, ks2) {
				return a.MakeUnion([]TypeId{t, extra}, NameNone)
			}
		}
	}
	n := a.Node(t)
	switch n.Kind {
	case TKMaybe:
		return a.MakeMaybe(g.widen(TypeId(n.A), depth-1, fresh))
	case TKList:
		if fresh {
			return a.MakeList(g.widen(TypeId(n.A), depth-1, true))
		}
	case TKRecord:
		rec := a.records[n.Extra]
		var fields []RecordField
		for _, f := range rec.Fields {
			switch {
			case g.rng.Intn(4) == 0:
				f = RecordField{Name: f.Name, Status: FieldOpen}
			case fresh && f.Type != TidNothing:
				f.Type = g.widen(f.Type, depth-1, true)
				if f.Status == FieldRequired && g.rng.Intn(3) == 0 {
					f.Status = FieldOptional
				}
			case fresh && f.Status == FieldAbsent && g.rng.Intn(2) == 0:
				f = RecordField{Name: f.Name, Status: FieldOptional, Type: g.ty(1, true, false)}
			}
			fields = append(fields, f)
		}
		rest := rec.Rest
		if g.rng.Intn(4) == 0 {
			rest = RecordField{Status: FieldOpen}
		} else if fresh && rest.Type != TidNothing {
			rest.Type = g.widen(rest.Type, depth-1, true)
		}
		return a.MakeRecord(fields, rest)
	case TKUnion:
		members := slices.Clone(a.unionMembers[n.Extra])
		i := g.rng.Intn(len(members))
		m := g.widen(members[i], depth-1, fresh)
		old, _ := g.r.Kinds(members[i])
		rest := append(slices.Clone(members[:i]), members[i+1:]...)
		ks, ok := g.r.Kinds(m)
		others, _ := g.r.Kinds(a.MakeUnion(rest, NameNone))
		if ok && len(ks) >= len(old) && m != TidUnknown && (len(rest) == 0 || kindsDisjoint(ks, others)) {
			members[i] = m
		}
		return a.MakeUnion(members, NameNone)
	case TKQuote:
		sig := a.quoteSigs[n.Extra]
		if sig.Diverges {
			out := QuoteSig{Inputs: sig.Inputs}
			for i := g.rng.Intn(3); i > 0; i-- {
				out.Outputs = append(out.Outputs, g.ty(1, true, false))
			}
			return a.MakeQuote(out)
		}
		out := QuoteSig{Inputs: sig.Inputs}
		for _, x := range sig.Outputs {
			out.Outputs = append(out.Outputs, g.widen(x, depth-1, false))
		}
		return a.MakeQuote(out)
	case TKEnum:
		params := a.enumDecls[n.A].Params
		args := slices.Clone(a.enumArgs[n.Extra])
		for i, p := range params {
			if (fresh && p.Fresh) || p.Variance == VarCo {
				args[i] = g.widen(args[i], depth-1, fresh && p.Fresh)
			}
		}
		return a.MakeEnum(n.A, args)
	case TKAlias:
		return g.widen(a.aliases[n.A].Body, depth-1, fresh)
	}
	return t
}

// pair makes two types to ask about.
func (g *typeGen) pair() (TypeId, TypeId) {
	x := g.ty(4, false, false)
	switch g.rng.Intn(6) {
	case 0:
		return x, g.ty(4, false, false)
	case 1:
		return g.near(x, 4), x
	case 2, 3:
		return x, g.widen(x, 4, g.rng.Intn(2) == 0)
	case 4:
		return g.widen(x, 4, g.rng.Intn(2) == 0), x
	}
	return x, g.near(x, 4)
}

// oracleBinary is the extracted oracle, or "" when it is not built.
func oracleBinary() string {
	p := "../formal-ver/oracle/oracle"
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// runOracle answers queries, one per line, with the extracted oracle.
func runOracle(t *testing.T, bin string, queries []string) []string {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(strings.Join(queries, "\n") + "\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("oracle: %v", err)
	}
	var answers []string
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		answers = append(answers, sc.Text())
	}
	if len(answers) != len(queries) {
		t.Fatalf("oracle gave %d answers to %d queries", len(answers), len(queries))
	}
	return answers
}

// The fuel given to the oracle, and the larger fuel used to recheck a
// question it answers no that Go answers yes.
const (
	oracleFuel      = 200
	oracleRecheckFuel = 2000
)

// TestRelationsAgreeWithOracle asks the Go relations and the extracted,
// proved ones the same questions about random types. A yes from Go that the
// oracle does not give, even with more fuel, is a possible soundness bug; a
// no from Go where the oracle says yes, or a different join, is a bug in the
// port. MSH_ORACLE_QUERIES sets how many questions of each kind to ask.
func TestRelationsAgreeWithOracle(t *testing.T) {
	bin := oracleBinary()
	if bin == "" {
		t.Skip("formal-ver/oracle/oracle is not built (make -C formal-ver/oracle)")
	}
	perKind := 1000
	if s := os.Getenv("MSH_ORACLE_QUERIES"); s != "" {
		fmt.Sscan(s, &perKind)
	}
	var queries []string
	for seed := int64(1); len(queries) < 3*perKind; seed++ {
		g := newTypeGen(seed)
		for i := 0; i < 50 && len(queries) < 3*perKind; i++ {
			a, b := g.pair()
			sa, sb := g.o.print(a), g.o.print(b)
			fa, fb := g.rng.Intn(2) == 0, g.rng.Intn(3) != 0
			queries = append(queries,
				fmt.Sprintf("sub %d %s %s", oracleFuel, sa, sb),
				fmt.Sprintf("rsub %d %s %s", oracleFuel, sa, sb),
				fmt.Sprintf("join %d %s %s", oracleFuel, g.o.printSlot(Slot{a, fa}), g.o.printSlot(Slot{b, fb})))
		}
	}
	answers := runOracle(t, bin, queries)

	p := newOracleTypes()
	r := NewRelations(p.arena)
	var unsound, incomplete, joins []string
	yes := map[string]int{}
	var recheck []int
	for i, q := range queries {
		ok, got, err := p.agrees(r, q, answers[i])
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		kind := q[:strings.IndexByte(q, ' ')]
		if answers[i] == "yes" || (kind == "join" && answers[i] != "none") {
			yes[kind]++
		}
		if ok {
			continue
		}
		switch {
		case kind == "join":
			joins = append(joins, fmt.Sprintf("%s\n  oracle %s\n  go     %s", q, answers[i], got))
		case got == "yes":
			recheck = append(recheck, i)
		default:
			incomplete = append(incomplete, q)
		}
	}
	if len(recheck) > 0 {
		more := make([]string, len(recheck))
		for j, i := range recheck {
			more[j] = strings.Replace(queries[i], fmt.Sprintf(" %d ", oracleFuel), fmt.Sprintf(" %d ", oracleRecheckFuel), 1)
		}
		for j, a := range runOracle(t, bin, more) {
			if a != "yes" {
				unsound = append(unsound, queries[recheck[j]])
			}
		}
	}
	t.Logf("%d questions; oracle yes: sub %d, rsub %d, join %d", len(queries), yes["sub"], yes["rsub"], yes["join"])
	report := func(what string, xs []string) {
		if len(xs) == 0 {
			return
		}
		t.Errorf("%d %s", len(xs), what)
		for _, x := range xs[:min(len(xs), 8)] {
			t.Errorf("  %s", x)
		}
	}
	report("Go yes, oracle no (possible soundness bug)", unsound)
	report("Go no, oracle yes (port less complete)", incomplete)
	report("joins differ", joins)
}

// Properties the proof establishes for the relations (sub_trans for <= and
// for fresh retyping, and rs_sub: <= implies retype), checked on random
// types. The decision procedures are not proved complete, so a failure
// here is either a port bug or a question the procedure cannot answer,
// and either is worth knowing. MSH_PROPERTY_TRIPLES sets the number of
// triples tried.
func TestRelationsProperties(t *testing.T) {
	n := 20000
	if s := os.Getenv("MSH_PROPERTY_TRIPLES"); s != "" {
		fmt.Sscan(s, &n)
	}
	var subChains, retypeChains, subPairs, distinct, retypeDistinct int
	var failures []string
	fail := func(format string, args ...any) {
		if len(failures) < 10 {
			failures = append(failures, fmt.Sprintf(format, args...))
		}
	}
	for seed, done := int64(1), 0; done < n; seed++ {
		g := newTypeGen(seed)
		r := g.r
		pr := g.o.print
		for i := 0; i < 50 && done < n; i, done = i+1, done+1 {
			var a, b, c TypeId
			switch fresh := g.rng.Intn(2) == 0; g.rng.Intn(3) {
			case 0:
				b = g.ty(4, false, false)
				a, c = g.near(b, 4), g.near(b, 4)
			default:
				a = g.ty(4, false, false)
				b = g.widen(a, 4, fresh)
				c = g.widen(b, 4, fresh)
			}
			if r.Sub(a, b) {
				subPairs++
				if !r.Retype(a, b) {
					fail("<= but not retype: %s %s", pr(a), pr(b))
				}
				if r.Sub(b, c) {
					subChains++
					if a != b && b != c && a != c {
						distinct++
					}
					if !r.Sub(a, c) {
						fail("<= not transitive:\n  %s\n  %s\n  %s", pr(a), pr(b), pr(c))
					}
				}
			}
			if r.Retype(a, b) && r.Retype(b, c) {
				retypeChains++
				if a != b && b != c && a != c {
					retypeDistinct++
				}
				if !r.Retype(a, c) {
					fail("retype not transitive:\n  %s\n  %s\n  %s", pr(a), pr(b), pr(c))
				}
			}
		}
	}
	t.Logf("%d triples: %d <= pairs, %d <= chains (%d of three distinct types), %d retype chains (%d distinct)",
		n, subPairs, subChains, distinct, retypeChains, retypeDistinct)
	for _, f := range failures {
		t.Error(f)
	}
}

// payloadType makes a random constructor payload type that mentions the
// parameters and the enum being declared (at its own parameters).
func (g *typeGen) payloadType(depth int, params []TypeId, self uint32) TypeId {
	a := g.o.arena
	if depth <= 0 || g.rng.Intn(4) == 0 {
		switch g.rng.Intn(4) {
		case 0, 1:
			return params[g.rng.Intn(len(params))]
		case 2:
			return a.MakeEnum(self, params)
		}
		return g.leaf(true, true)
	}
	switch g.rng.Intn(7) {
	case 0:
		return a.MakeMaybe(g.payloadType(depth-1, params, self))
	case 1:
		return a.MakeList(g.payloadType(depth-1, params, self))
	case 2:
		return a.MakeStrDict(g.payloadType(depth-1, params, self))
	case 3:
		sig := QuoteSig{}
		for i := g.rng.Intn(2); i >= 0; i-- {
			sig.Inputs = append(sig.Inputs, g.payloadType(depth-1, params, self))
		}
		sig.Outputs = append(sig.Outputs, g.payloadType(depth-1, params, self))
		return a.MakeQuote(sig)
	case 4:
		idx := g.enums[g.rng.Intn(len(g.enums))]
		args := make([]TypeId, len(a.enumDecls[idx].Params))
		for i := range args {
			args[i] = g.payloadType(depth-1, params, self)
		}
		return a.MakeEnum(idx, args)
	case 5:
		f := RecordField{Name: g.o.names.Intern("a"), Status: FieldRequired, Type: g.payloadType(depth-1, params, self)}
		return a.MakeRecord([]RecordField{f}, RecordField{Status: FieldOpen})
	}
	return params[g.rng.Intn(len(params))]
}
