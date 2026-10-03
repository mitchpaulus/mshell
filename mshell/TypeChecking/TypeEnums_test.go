package main

import (
	"fmt"
	"strings"
	"testing"
)

// declareTestEnum declares an enum with n parameters and constructors whose
// payload types are written in oracle syntax, with (param i) for the i'th
// parameter and (enum SELF ...) for the enum itself, then analyzes it.
func declareTestEnum(t *testing.T, o *oracleTypes, r *Relations, name string, n int, payloads ...[]string) uint32 {
	t.Helper()
	decl := EnumDecl{Name: o.names.Intern(name)}
	for i := 0; i < n; i++ {
		decl.Params = append(decl.Params, EnumParam{Name: o.names.Intern(fmt.Sprintf("p%d", i))})
	}
	idx := o.arena.DeclareEnum(decl)
	self := func(args ...TypeId) TypeId { return o.arena.MakeEnum(idx, args) }
	for ci, pts := range payloads {
		ctor := EnumCtor{Name: o.names.Intern(fmt.Sprintf("%s_c%d", name, ci))}
		for _, pt := range pts {
			// SELF is the enum at its own parameters: written as parameter 9,
			// then substituted.
			selfArgs := make([]TypeId, n)
			subst := make([]TypeId, 10)
			for i := range subst {
				subst[i] = o.arena.MakeParam(i)
				if i < n {
					selfArgs[i] = o.arena.MakeParam(i)
				}
			}
			subst[9] = self(selfArgs...)
			ty := r.SubstParams(o.mustParse(t, strings.ReplaceAll(pt, "SELF", "(param 9)")), subst)
			ctor.Payload = append(ctor.Payload, ty)
		}
		o.arena.enumDecls[idx].Ctors = append(o.arena.enumDecls[idx].Ctors, ctor)
	}
	r.AnalyzeEnums([]uint32{idx})
	if !r.WellFormedEnum(idx) {
		t.Errorf("%s: the analysis gave a declaration that is not well formed", name)
	}
	return idx
}

func TestAnalyzeEnums(t *testing.T) {
	o := newOracleTypes()
	r := NewRelations(o.arena)
	type want struct {
		vars      []Variance
		fresh     []bool
		imm, chk  bool
	}
	cases := []struct {
		name     string
		n        int
		payloads [][]string
		want     want
	}{
		// enum Maybe[a] = just a | none end
		{"Maybe", 1, [][]string{{"(param 0)"}, {}}, want{[]Variance{VarCo}, []bool{true}, true, true}},
		// enum Box[a] = box [a] | empty end: invariant, fresh-covariant, not immutable
		{"Box", 1, [][]string{{"(list (param 0))"}, {}}, want{[]Variance{VarInv}, []bool{true}, false, true}},
		// enum F[a] = f (a -- a) end: invariant, not fresh-covariant, not checkable
		{"F", 1, [][]string{{"(quote ((param 0)) ((param 0)))"}}, want{[]Variance{VarInv}, []bool{false}, true, false}},
		// enum Pair[a] = pair a a end
		{"Pair", 1, [][]string{{"(param 0)", "(param 0)"}}, want{[]Variance{VarCo}, []bool{true}, true, true}},
		// enum List[a] = cons a List[a] | nil end
		{"List", 1, [][]string{{"(param 0)", "SELF"}, {}}, want{[]Variance{VarCo}, []bool{true}, true, true}},
		// enum Sink[a] = sink (a -- ) Sink[a] end: contravariant, found only from "unused"
		{"Sink", 1, [][]string{{"(quote ((param 0)) ())", "SELF"}}, want{[]Variance{VarContra}, []bool{false}, true, false}},
		// enum Tree = leaf int | node Tree Tree end: immutable (greatest fixed point)
		{"Tree", 0, [][]string{{"int"}, {"SELF", "SELF"}}, want{nil, nil, true, true}},
		// an unused parameter is covariant and fresh
		{"Phantom", 1, [][]string{{"int"}}, want{[]Variance{VarCo}, []bool{true}, true, true}},
		// enum R[a] = r [R[a]] end: a occurs only through R itself, inside a
		// list, so once it counts as covariant the list makes it invariant
		{"Self", 1, [][]string{{"(list SELF)"}}, want{[]Variance{VarInv}, []bool{true}, false, true}},
		// a parameter under a dict value is invariant
		{"Bag", 1, [][]string{{"(dict (param 0))"}}, want{[]Variance{VarInv}, []bool{true}, false, true}},
	}
	for _, c := range cases {
		idx := declareTestEnum(t, o, r, c.name, c.n, c.payloads...)
		d := o.arena.enumDecls[idx]
		for i, p := range d.Params {
			if p.Variance != c.want.vars[i] || p.Fresh != c.want.fresh[i] {
				t.Errorf("%s: param %d is (%v, fresh %v), want (%v, fresh %v)", c.name, i, p.Variance, p.Fresh, c.want.vars[i], c.want.fresh[i])
			}
		}
		if d.Immutable != c.want.imm || d.Checkable != c.want.chk {
			t.Errorf("%s: immutable %v checkable %v, want %v %v", c.name, d.Immutable, d.Checkable, c.want.imm, c.want.chk)
		}
	}
}

// What makes the enum rules sound (Variance.v): for a well-formed enum,
// E[xs] <= E[ys] implies every payload type at xs is below the same payload
// type at ys (payload_sub), the same for fresh retyping (payload_rsub), and
// an immutable instance has immutable payloads (payload_imm). Checked on
// random declarations and arguments.
func TestEnumPayloadProperties(t *testing.T) {
	var subPairs, retypePairs int
	for seed := int64(1); seed <= 1000; seed++ {
		g := newTypeGen(seed)
		a := g.o.arena
		// A random enum with one or two parameters whose payloads mention
		// them and the enum itself.
		n := 1 + g.rng.Intn(2)
		decl := EnumDecl{Name: g.o.names.Intern("R")}
		for i := 0; i < n; i++ {
			decl.Params = append(decl.Params, EnumParam{Name: g.o.names.Intern(fmt.Sprintf("q%d", i))})
		}
		idx := a.DeclareEnum(decl)
		params := make([]TypeId, n)
		for i := range params {
			params[i] = a.MakeParam(i)
		}
		for c := 0; c < 1+g.rng.Intn(3); c++ {
			var payload []TypeId
			for k := 0; k < g.rng.Intn(3); k++ {
				payload = append(payload, g.payloadType(3, params, idx))
			}
			a.enumDecls[idx].Ctors = append(a.enumDecls[idx].Ctors, EnumCtor{Name: g.o.names.Intern(fmt.Sprintf("r%d", c)), Payload: payload})
		}
		g.r.AnalyzeEnums([]uint32{idx})
		if !g.r.WellFormedEnum(idx) {
			t.Fatalf("seed %d: analysis gave a declaration that is not well formed", seed)
		}
		for trial := 0; trial < 60; trial++ {
			xs := make([]TypeId, n)
			ys := make([]TypeId, n)
			fresh := g.rng.Intn(2) == 0
			for i := range xs {
				xs[i] = g.ty(3, false, false)
				ys[i] = g.widen(xs[i], 3, fresh)
				if g.rng.Intn(4) == 0 {
					xs[i], ys[i] = ys[i], xs[i]
				}
			}
			ex, ey := a.MakeEnum(idx, xs), a.MakeEnum(idx, ys)
			sub, retype := g.r.Sub(ex, ey), g.r.Retype(ex, ey)
			imm := g.r.Immutable(ex)
			for _, c := range a.enumDecls[idx].Ctors {
				for _, pt := range c.Payload {
					px, py := g.r.SubstParams(pt, xs), g.r.SubstParams(pt, ys)
					if sub && !g.r.Sub(px, py) {
						t.Errorf("payload_sub: %s <= %s but payload %s is not below %s",
							g.o.print(ex), g.o.print(ey), g.o.print(px), g.o.print(py))
					}
					if retype && !g.r.Retype(px, py) {
						t.Errorf("payload_rsub: %s retypes to %s but payload %s does not retype to %s",
							g.o.print(ex), g.o.print(ey), g.o.print(px), g.o.print(py))
					}
					if imm && !g.r.Immutable(px) {
						t.Errorf("payload_imm: %s is immutable but payload %s is not", g.o.print(ex), g.o.print(px))
					}
				}
			}
			if sub {
				subPairs++
			}
			if retype {
				retypePairs++
			}
		}
	}
	t.Logf("%d <= instances, %d retype instances", subPairs, retypePairs)
}
