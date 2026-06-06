package main

import "testing"

// Phase 5 tests: type environment + Cast.
//
// These tests drive the Checker through DeclareType / LookupType / Cast
// directly. The parser-level surface (`type X = ...` declarations and the
// postfix `as` operator) lands in Phase 10; until then, this is the way
// to exercise the new behavior.

func newCheckerForCast(t *testing.T) *Checker {
	t.Helper()
	arena := NewTypeArena()
	names := NewNameTable()
	return NewChecker(arena, names)
}

func TestDeclareUnionType(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	id, ok := c.DeclareType("Result", body)
	if !ok {
		t.Fatalf("DeclareType returned !ok; errors: %+v", c.Errors())
	}
	if id == body {
		t.Fatalf("declared type should be branded, distinct from body")
	}
	n := c.arena.Node(id)
	if n.Kind != TKUnion {
		t.Fatalf("expected branded union, got kind %s", n.Kind)
	}
	if n.A == 0 {
		t.Fatalf("expected nonzero brand id on declared union")
	}
	if got := c.LookupType("Result"); got != id {
		t.Fatalf("LookupType returned %d; want %d", got, id)
	}
}

func TestDeclareNewtypeOverPrimitive(t *testing.T) {
	c := newCheckerForCast(t)
	id, ok := c.DeclareType("UserId", TidInt)
	if !ok {
		t.Fatalf("unexpected errors: %+v", c.Errors())
	}
	n := c.arena.Node(id)
	if n.Kind != TKBrand {
		t.Fatalf("expected TKBrand newtype over int, got %s", n.Kind)
	}
	if TypeId(n.B) != TidInt {
		t.Fatalf("brand should wrap int, wraps %d", n.B)
	}
}

func TestDeclareReservedTypeNameRejected(t *testing.T) {
	c := newCheckerForCast(t)
	_, ok := c.DeclareType("int", TidInt)
	if ok {
		t.Fatalf("expected DeclareType to reject reserved name 'int'")
	}
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrReservedTypeName {
		t.Fatalf("expected one TErrReservedTypeName, got %+v", errs)
	}
}

func TestDeclareDuplicateRejected(t *testing.T) {
	c := newCheckerForCast(t)
	if _, ok := c.DeclareType("X", TidInt); !ok {
		t.Fatalf("first declare should succeed")
	}
	if _, ok := c.DeclareType("X", TidStr); ok {
		t.Fatalf("second declare with same name should fail")
	}
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrDuplicateTypeName {
		t.Fatalf("expected TErrDuplicateTypeName, got %+v", errs)
	}
}

func TestTwoDeclarationsSameBodyAreDistinct(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	a, _ := c.DeclareType("A", body)
	b, _ := c.DeclareType("B", body)
	if a == b {
		t.Fatalf("declarations with distinct names must produce distinct types")
	}
	// Neither should unify with the other; they're nominally branded.
	if c.unify(a, b) {
		t.Fatalf("A and B should not unify")
	}
}

func TestCastIntoUnionBrand(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	result, _ := c.DeclareType("Result", body)

	// Top of stack is int. Cast to Result: int unifies with one arm of the
	// underlying int|str, so the cast is allowed.
	c.stack.Push(TidInt)
	c.Cast(result, fakeTok("as"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("expected no errors casting int -> Result, got %+v", errs)
	}
	if c.stack.Len() != 1 || c.stack.Top() != result {
		t.Fatalf("stack top should be Result; got %d, len=%d", c.stack.Top(), c.stack.Len())
	}
}

func TestCastUnionIntoUnionBrand(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	result, _ := c.DeclareType("Result", body)

	// int|str (unbranded) flows into Result via cast.
	src := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	c.stack.Push(src)
	c.Cast(result, fakeTok("as"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
	if c.stack.Top() != result {
		t.Fatalf("expected Result on top of stack")
	}
}

func TestCastIncompatibleRejected(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	result, _ := c.DeclareType("Result", body)

	// bool does not match int | str — invalid cast.
	c.stack.Push(TidBool)
	c.Cast(result, fakeTok("as"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrInvalidCast {
		t.Fatalf("expected TErrInvalidCast, got %+v", errs)
	}
	// Recovery: target still pushed.
	if c.stack.Top() != result {
		t.Fatalf("recovery should leave Result on stack")
	}
}

func TestCastBrandedToDifferentBrandRejected(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	a, _ := c.DeclareType("A", body)
	b, _ := c.DeclareType("B", body)

	// Top is A. Casting to B should fail — A's underlying is int|str, but
	// A as a value is branded; we don't allow brand-to-brand teleport.
	c.stack.Push(a)
	c.Cast(b, fakeTok("as"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrInvalidCast {
		t.Fatalf("expected TErrInvalidCast for A -> B, got %+v", errs)
	}
}

func TestCastBrandToUnderlyingAllowed(t *testing.T) {
	c := newCheckerForCast(t)
	body := c.arena.MakeUnion([]TypeId{TidInt, TidStr}, NameNone)
	a, _ := c.DeclareType("A", body)

	// Top is A. Cast back to underlying int|str is allowed: A's
	// underlying unifies with the target.
	c.stack.Push(a)
	c.Cast(body, fakeTok("as"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("expected cast A -> underlying to succeed, got %+v", errs)
	}
	if c.stack.Top() != body {
		t.Fatalf("expected underlying union on top of stack")
	}
}

func TestCastNewtypeFromUnderlying(t *testing.T) {
	c := newCheckerForCast(t)
	userId, _ := c.DeclareType("UserId", TidInt)
	c.stack.Push(TidInt)
	c.Cast(userId, fakeTok("as"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("expected int -> UserId cast to succeed, got %+v", errs)
	}
	if c.stack.Top() != userId {
		t.Fatalf("expected UserId on top of stack")
	}
}

func TestCastNewtypeWrongUnderlyingRejected(t *testing.T) {
	c := newCheckerForCast(t)
	userId, _ := c.DeclareType("UserId", TidInt)
	c.stack.Push(TidStr)
	c.Cast(userId, fakeTok("as"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrInvalidCast {
		t.Fatalf("expected TErrInvalidCast, got %+v", errs)
	}
}

func TestCastUnderflow(t *testing.T) {
	c := newCheckerForCast(t)
	userId, _ := c.DeclareType("UserId", TidInt)
	c.Cast(userId, fakeTok("as"))
	errs := c.Errors()
	if len(errs) != 1 || errs[0].Kind != TErrStackUnderflow {
		t.Fatalf("expected TErrStackUnderflow, got %+v", errs)
	}
	// Recovery: target pushed.
	if c.stack.Top() != userId {
		t.Fatalf("expected UserId on top of stack after underflow recovery")
	}
}

func TestCastIdentity(t *testing.T) {
	c := newCheckerForCast(t)
	c.stack.Push(TidInt)
	c.Cast(TidInt, fakeTok("as"))
	if errs := c.Errors(); len(errs) != 0 {
		t.Fatalf("identity cast should be a no-op, got %+v", errs)
	}
	if c.stack.Top() != TidInt {
		t.Fatalf("expected int still on top")
	}
}

func TestLookupTypeMissing(t *testing.T) {
	c := newCheckerForCast(t)
	if got := c.LookupType("Nope"); got != TidNothing {
		t.Fatalf("expected TidNothing for missing name, got %d", got)
	}
}

func fakeTok(lex string) Token {
	return Token{Lexeme: lex, Line: 1, Column: 1, Type: LITERAL}
}
