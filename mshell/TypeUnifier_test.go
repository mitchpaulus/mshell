package main

import "testing"

func TestUnifier(t *testing.T) {
	o := newOracleTypes()
	a := o.arena
	rel := NewRelations(a)
	var subst Substitution
	u := NewUnifier(a, &subst, rel)
	v := func() TypeId { return subst.FreshVar(a) }
	json := o.mustParse(t, jsonT)
	json2 := o.mustParse(t, "(mu (union (dict (rv 0)) (union (list (rv 0)) (union str (union int bool)))))")
	selfList := o.mustParse(t, "(mu (list (rv 0)))") // type L = [L]

	x := v()
	if !u.Unify(a.MakeList(x), a.MakeList(TidInt)) || subst.Apply(a, x) != TidInt {
		t.Errorf("[x] = [int] should bind x to int")
	}
	y := v()
	cp := u.Checkpoint()
	if u.Unify(y, a.MakeList(y)) {
		t.Errorf("the occurs check should refuse y = [y]")
	}
	u.Rollback(cp)
	if !u.Unify(json, json2) {
		t.Errorf("Json and its reordered spelling are equal")
	}
	z := v()
	if !u.Unify(a.MakeList(z), selfList) || subst.Apply(a, z) != selfList {
		t.Errorf("[z] = L (type L = [L]) should bind z to L")
	}
	w := v()
	if u.Unify(a.MakeList(w), json) {
		t.Errorf("[w] = Json needs a union step, which unification does not take")
	}
	m := v()
	if u.Unify(a.MakeUnion([]TypeId{TidInt, a.MakeList(m)}, NameNone), a.MakeUnion([]TypeId{TidInt, a.MakeList(TidStr)}, NameNone)) {
		t.Errorf("unification does not enter a union")
	}
	if u.Unify(TidInt, a.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)) {
		t.Errorf("int = int | str is subtyping, not equality")
	}
	r := v()
	shape := func(x TypeId) TypeId {
		return a.MakeRecord([]RecordField{{Name: o.names.Intern("a"), Status: FieldRequired, Type: x}}, RecordField{Status: FieldOpen})
	}
	if !u.Unify(shape(r), shape(TidStr)) || subst.Apply(a, r) != TidStr {
		t.Errorf("{a: r} = {a: str} should bind r")
	}
	if u.Unify(shape(v()), a.MakeRecord(nil, RecordField{Status: FieldOpen})) {
		t.Errorf("records with different labels are not equal")
	}
	q := v()
	if !u.Unify(a.MakeMaybe(q), a.MakeMaybe(json)) || subst.Apply(a, q) != json {
		t.Errorf("Maybe[q] = Maybe[Json] should bind q to Json")
	}
	if bad := u.Recheck(); len(bad) != 0 {
		for _, p := range bad {
			t.Errorf("recheck: %s is not %s", FormatType(a, o.names, p.A), FormatType(a, o.names, p.B))
		}
	}
	// A wrong unification shows up in Recheck.
	u.pairs = append(u.pairs, typePair{TidInt, TidStr})
	if len(u.Recheck()) != 1 {
		t.Errorf("recheck should report int = str")
	}
}
