package main

import (
	"strings"
	"testing"
)

func TestEnumNullaryDeclAndConstruct(t *testing.T) {
	errs, ok := parseAndCheck(t, "enum Color = red | green | blue\ndef describe (Color -- str) c! \"x\" end\nred describe")
	if !ok || len(errs) != 0 {
		t.Fatalf("nullary enum decl + construct should pass; errs=%v ok=%v", errs, ok)
	}
}

func TestEnumPayloadConstructorSignature(t *testing.T) {
	// A payload constructor has signature (payload... -- Enum).
	errs, ok := parseAndCheck(t, "enum R = ok(str) | failed(int str) | none2\ndef use (R -- str) c! \"x\" end\n404 \"nf\" failed use")
	if !ok || len(errs) != 0 {
		t.Fatalf("payload constructor should type-check; errs=%v ok=%v", errs, ok)
	}
}

func TestEnumPayloadWrongType(t *testing.T) {
	errs, ok := parseAndCheck(t, "enum R = ok(int)\n\"x\" ok")
	if ok {
		t.Fatalf("wrong payload type should fail; errs=%v", errs)
	}
}

func TestEnumDistinctNominal(t *testing.T) {
	// Two enums with parallel members do not unify.
	src := "enum A = a1 | a2\nenum B = b1 | b2\ndef takesA (A -- str) c! \"x\" end\nb1 takesA"
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("feeding enum B where A is expected should fail; errs=%v", errs)
	}
}

func TestEnumDuplicateMember(t *testing.T) {
	errs, ok := parseAndCheck(t, "enum E = a | b | a")
	if ok {
		t.Fatalf("duplicate enum member should fail; errs=%v", errs)
	}
	if !strings.Contains(strings.Join(errs, "\n"), "duplicate enum member") {
		t.Fatalf("expected duplicate-member error; errs=%v", errs)
	}
}

func TestEnumCrossEnumMemberCollision(t *testing.T) {
	errs, ok := parseAndCheck(t, "enum E = x1 | shared\nenum F = shared | y1")
	if ok {
		t.Fatalf("member name reused across enums should fail; errs=%v", errs)
	}
}

func TestEnumReservedName(t *testing.T) {
	errs, ok := parseAndCheck(t, "enum Maybe = a | b")
	if ok {
		t.Fatalf("enum named with a reserved type name should fail; errs=%v", errs)
	}
}

func TestEnumMatchExhaustive(t *testing.T) {
	src := "enum Color = red | green | blue\ngreen match\n red : \"r\" wl,\n green : \"g\" wl,\n blue : \"b\" wl,\nend"
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("exhaustive enum match should pass; errs=%v ok=%v", errs, ok)
	}
}

func TestEnumMatchNonExhaustive(t *testing.T) {
	src := "enum Color = red | green | blue\ngreen match\n red : \"r\" wl,\n blue : \"b\" wl,\nend"
	errs, ok := parseAndCheck(t, src)
	if ok {
		t.Fatalf("non-exhaustive enum match should fail; errs=%v", errs)
	}
	if !strings.Contains(strings.Join(errs, "\n"), "missing: green") {
		t.Fatalf("expected missing-member hint naming 'green'; errs=%v", errs)
	}
}

func TestEnumMatchWildcardExhaustive(t *testing.T) {
	src := "enum Color = red | green | blue\nred match\n red : \"r\" wl,\n _ : \"o\" wl,\nend"
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("wildcard should make enum match exhaustive; errs=%v ok=%v", errs, ok)
	}
}

func TestEnumMatchPayloadBinding(t *testing.T) {
	src := "enum R = ok(str) | failed(int str) | quit\n404 \"nf\" failed match\n ok s : @s wl,\n failed c e : @e wl,\n quit : \"q\" wl,\nend"
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("payload-binding enum match should pass; errs=%v ok=%v", errs, ok)
	}
}

func TestEnumRecursivePayload(t *testing.T) {
	// A member may carry a payload that references the enum itself.
	src := "enum Tree = leaf(int) | node(Tree Tree)\n3 leaf 4 leaf node"
	errs, ok := parseAndCheck(t, src)
	if !ok || len(errs) != 0 {
		t.Fatalf("self-referential enum payload should type-check; errs=%v ok=%v", errs, ok)
	}
}
