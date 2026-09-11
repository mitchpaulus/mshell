package main

// Phase 5: type environment and the `as` cast.
//
// Phase 3 already implemented union/brand unification at the unify() level.
// Phase 5 adds two pieces:
//
//   1. A type environment on the Checker: a NameId -> TypeId map populated
//      by DeclareType. This is what `type X = A | B` declarations register
//      against, and what later phases will look up when a parser encounters
//      a type name in a sig or cast position.
//   2. A Cast(target, callSite) entry point that re-tags the top of the
//      stack as `target`, with compatibility checking.
//
// Per ai/type_checker.md:1287-1298 ("option 1, pure-checker minimal"), this
// phase deliberately does NOT touch the parser or the lexer. The user-
// visible `type X = ...` form and postfix `as` operator land together with
// the rest of the parser integration in Phase 10. Phase 5's tests drive the
// Checker through the API directly.

// DeclareType registers a named type declaration:
//
//	type Name = body
//
// `body` is the structural form the user wrote on the right-hand side; the
// returned id is a NEW nominal type that wraps it. Two distinct declarations
// with the same body produce distinct types — this is the "brand-equality
// semantics for unions" rule from the design (ai/type_checker.md:796-799).
//
// Wrapping rule:
//   - If body is already a branded union, that's a programmer error in V1
//     (re-branding an already-branded type). Reported as a hint; the
//     existing brand stands and `name` is bound to body unchanged.
//   - If body is an unbranded TKUnion, return a new TKUnion with the same
//     arms tagged with brand id = NameId(name).
//   - Otherwise (primitives, lists, dicts, shapes, etc.), wrap with
//     TKBrand. This is the newtype-style case.
//
// Reserved type names (int, float, str, ..., Maybe, Grid, ...) cannot be
// redefined; attempting to do so returns NameNone-binding TidNothing and a
// false ok flag, with no environment change.
func (c *Checker) DeclareType(name string, body TypeId) (TypeId, bool) {
	if IsReservedTypeName(name) {
		c.errors = append(c.errors, TypeError{
			Kind: TErrReservedTypeName,
			Name: name,
		})
		return TidNothing, false
	}
	if c.typeEnv == nil {
		c.typeEnv = make(map[NameId]TypeId, 8)
	}
	nameId := c.names.Intern(name)
	if _, exists := c.typeEnv[nameId]; exists {
		c.errors = append(c.errors, TypeError{
			Kind: TErrDuplicateTypeName,
			Name: name,
		})
		return TidNothing, false
	}

	branded := c.brandify(nameId, body)
	c.arena.markNamed(branded)
	c.typeEnv[nameId] = branded
	return branded, true
}

// declareTypes registers every top-level `type` declaration of a file so
// that bodies may refer to any declared name, including their own
// (recursive types) and names declared later in the file (mutual
// recursion). It replaces the resolve-then-DeclareType sequence for the
// program path; DeclareType stays for callers that already hold a
// resolved body.
//
// Steps:
//
//  1. Reserve a placeholder id per declaration and bind the name. A union
//     body gets a branded-union placeholder, anything else a brand
//     placeholder, so the final node kind matches what brandify would
//     have produced.
//  2. Resolve each body with every name visible and patch it into its
//     placeholder. The placeholder id is the type's id for good, so every
//     reference resolved during this step stays valid.
//  3. Reject unguarded cycles. A declaration may reach itself only through
//     a list, dict, shape field, Maybe, or quotation; `type T = int | T`
//     or `type A = B` with `type B = A` has no value that terminates and
//     would make the checker's one-layer unfolding loop. A cycle is
//     reported on each declaration in it and the offending bodies are
//     emptied so later checking stays finite.
func (c *Checker) declareTypes(decls []*MShellTypeDecl) {
	type pending struct {
		decl    *MShellTypeDecl
		nameId  NameId
		id      TypeId
		isUnion bool
	}
	if c.typeEnv == nil {
		c.typeEnv = make(map[NameId]TypeId, 8)
	}
	var pend []pending
	for _, d := range decls {
		if IsReservedTypeName(d.Name) {
			c.errors = append(c.errors, TypeError{Kind: TErrReservedTypeName, Pos: d.NameToken, Name: d.Name})
			continue
		}
		nameId := c.names.Intern(d.Name)
		if _, exists := c.typeEnv[nameId]; exists {
			c.errors = append(c.errors, TypeError{Kind: TErrDuplicateTypeName, Pos: d.NameToken, Name: d.Name})
			continue
		}
		_, isUnion := d.Body.(*TypeUnionExpr)
		var id TypeId
		if isUnion {
			id = c.arena.NewUnionPlaceholder(nameId)
		} else {
			id = c.arena.NewBrandPlaceholder(nameId)
		}
		c.typeEnv[nameId] = id
		pend = append(pend, pending{decl: d, nameId: nameId, id: id, isUnion: isUnion})
	}

	inBatch := make(map[TypeId]int, len(pend))
	for i, p := range pend {
		inBatch[p.id] = i
	}
	// direct[i] lists the batch indices that declaration i refers to with
	// nothing in between: its whole body, or one of its union arms.
	direct := make([][]int, len(pend))
	for i, p := range pend {
		if p.isUnion {
			union := p.decl.Body.(*TypeUnionExpr)
			arms := make([]TypeId, 0, len(union.Arms))
			for _, a := range union.Arms {
				arms = append(arms, c.resolveTypeExpr(a, nil))
			}
			c.arena.PatchUnion(p.id, arms)
			for _, a := range c.arena.UnionMembers(p.id) {
				if j, ok := inBatch[a]; ok {
					direct[i] = append(direct[i], j)
				}
			}
			continue
		}
		body := c.resolveTypeExpr(p.decl.Body, nil)
		c.arena.PatchBrand(p.id, body)
		if j, ok := inBatch[body]; ok {
			direct[i] = append(direct[i], j)
		}
	}

	// Cycle detection over the direct-reference graph (three-colour DFS).
	const (
		white = iota
		grey
		black
	)
	colour := make([]int, len(pend))
	onCycle := make([]bool, len(pend))
	var stack []int
	var visit func(i int)
	visit = func(i int) {
		colour[i] = grey
		stack = append(stack, i)
		for _, j := range direct[i] {
			switch colour[j] {
			case white:
				visit(j)
			case grey:
				for k := len(stack) - 1; k >= 0; k-- {
					onCycle[stack[k]] = true
					if stack[k] == j {
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		colour[i] = black
	}
	for i := range pend {
		if colour[i] == white {
			visit(i)
		}
	}
	for i, p := range pend {
		if !onCycle[i] {
			continue
		}
		c.errors = append(c.errors, TypeError{
			Kind: TErrTypeParse,
			Pos:  p.decl.NameToken,
			Hint: "type '" + p.decl.Name + "' refers to itself with nothing in between; a recursive reference must sit inside a list, dict, shape field, Maybe, or quotation",
		})
		if p.isUnion {
			c.arena.PatchUnion(p.id, nil)
		} else {
			c.arena.PatchBrand(p.id, TidNothing)
		}
	}
}

// LookupType returns the TypeId previously registered for `name` via
// DeclareType, or TidNothing if none is registered. Reserved/built-in names
// are NOT served from this table — those are recognized at parse / sig-build
// time and do not flow through the env.
func (c *Checker) LookupType(name string) TypeId {
	if c.typeEnv == nil {
		return TidNothing
	}
	id, ok := c.typeEnv[c.names.Intern(name)]
	if !ok {
		return TidNothing
	}
	return id
}

// brandify wraps body with a brand for nameId. See DeclareType for the rules.
func (c *Checker) brandify(nameId NameId, body TypeId) TypeId {
	n := c.arena.Node(body)
	switch n.Kind {
	case TKUnion:
		if n.A != 0 {
			// Already branded; cannot re-brand. Return unchanged.
			c.errors = append(c.errors, TypeError{
				Kind: TErrRebrand,
				Name: c.names.Name(nameId),
			})
			return body
		}
		arms := c.arena.UnionMembers(body)
		// MakeUnion will brand the existing arms with nameId.
		return c.arena.MakeUnion(arms, nameId)
	case TKBrand:
		// Wrapping an already-branded newtype with another brand is unusual.
		// Allow it: the new brand wins.
		return c.arena.MakeBrand(nameId, body)
	default:
		return c.arena.MakeBrand(nameId, body)
	}
}

// Cast implements `<value> as <target>`. It pops the top of the stack,
// checks that the value's static type can be re-tagged as `target`, and
// pushes `target` regardless (so downstream checking has a coherent stack
// even when the cast errors).
//
// Compatibility rule:
//
//   - If the source type already equals target, the cast is a no-op.
//   - Otherwise the source must unify with target's UNDERLYING structure
//     (the union arms for a branded union; the wrapped type for a TKBrand;
//     the type itself when target is unbranded). This matches the design's
//     "casting from int|str to A is required" example: int|str unifies
//     with the underlying of A even though it doesn't unify with A
//     directly.
//   - On any failure, an error is recorded and `target` is still pushed.
//
// This is intentionally generous in one direction (a value of the
// underlying structural form can be tagged into the brand) and strict in
// the other (the brand cannot be silently re-cast to a different brand).
// Going from a brand back to the underlying is also allowed by the same
// rule, since the underlying unifies with itself.
func (c *Checker) Cast(target TypeId, callSite Token) {
	if c.stack.Len() == 0 {
		c.errors = append(c.errors, TypeError{
			Kind: TErrStackUnderflow,
			Pos:  callSite,
			Hint: "cast",
		})
		c.stack.Push(target)
		return
	}
	top := c.stack.items[len(c.stack.items)-1]
	c.stack.items = c.stack.items[:len(c.stack.items)-1]

	if c.castOk(top, target) {
		c.stack.Push(target)
		return
	}
	err := TypeError{
		Kind:     TErrInvalidCast,
		Pos:      callSite,
		Expected: target,
		Actual:   top,
	}
	if c.arena.Kind(c.subst.Apply(c.arena, top)) == TKUnion {
		// The common mistake: trying to pick one arm of a union with `as`.
		// `as` is static only and never fails, so it cannot narrow; the
		// runtime check happens in a match.
		err.Hint = "'as' only widens or names a type, it cannot narrow a union; take the value apart with match (`dict d :`, `list l :`, `int n :`, ...)"
	}
	c.errors = append(c.errors, err)
	c.stack.Push(target)
}

// castOk reports whether `src` can be cast to `dst`.
//
// Trials, in order (each isolated by a substitution checkpoint):
//
//  1. Trivial id equality.
//  2. Direct unification src ~ dst.
//  3. Tag-in: src is acceptable where dst's UNDERLYING is expected. A
//     single primitive flowing into a branded union takes this path
//     because primitives don't unify with a union directly — we need the
//     "is src a member of the union arms" check.
//  4. Untag: dst is acceptable where src's UNDERLYING is. Lets a branded
//     value cast back to the structural form it wraps.
func (c *Checker) castOk(src, dst TypeId) bool {
	if src == dst {
		return true
	}
	// No brand-to-brand teleport: two names over the same body are
	// different types on purpose, and a cast between them is not a
	// widening, only a rename.
	if c.isBranded(src) && c.isBranded(dst) && c.underlying(src) == c.underlying(dst) {
		return false
	}
	c.casting++
	defer func() { c.casting-- }()
	cp := c.subst.Checkpoint()
	if c.unify(src, dst) {
		return true
	}
	c.subst.Rollback(cp)

	dstU := c.underlying(dst)
	if dstU != dst && c.acceptsAs(src, dstU) {
		return true
	}
	c.subst.Rollback(cp)

	srcU := c.underlying(src)
	if srcU != src && c.acceptsAs(srcU, dst) {
		return true
	}
	c.subst.Rollback(cp)

	return false
}

// acceptsAs is a cast-compatibility check: is `src` valid where `dst` is
// expected? Unlike unify, this allows a primitive (or any non-union type)
// to be a valid source for a union destination if it matches one of the
// arms. Unify itself is symmetric on kind; cast acceptance is asymmetric.
func (c *Checker) acceptsAs(src, dst TypeId) bool {
	src = c.subst.Apply(c.arena, src)
	dst = c.subst.Apply(c.arena, dst)
	if src == dst {
		return true
	}
	dn := c.arena.Node(dst)
	if dn.Kind == TKUnion {
		for _, arm := range c.arena.unionMembers[dn.Extra] {
			cp := c.subst.Checkpoint()
			if c.acceptsAs(src, arm) {
				return true
			}
			c.subst.Rollback(cp)
		}
		return false
	}
	return c.unify(src, dst)
}

// underlying returns the structural form of a (possibly-branded) type.
//
//   - Branded union -> same arms with brand id 0.
//   - TKBrand wrapper -> the wrapped underlying TypeId.
//   - Anything else -> the type itself.
//
// Not recursive past one layer: if the underlying is itself branded, that
// brand is preserved. Tests will tell us if we want to peel further.
func (c *Checker) underlying(id TypeId) TypeId {
	n := c.arena.Node(id)
	switch n.Kind {
	case TKUnion:
		if n.A != 0 {
			arms := c.arena.UnionMembers(id)
			return c.arena.MakeUnion(arms, NameNone)
		}
	case TKBrand:
		return TypeId(n.B)
	}
	return id
}

// isBranded reports whether id carries a declared name: a TKBrand or a
// branded union.
func (c *Checker) isBranded(id TypeId) bool {
	n := c.arena.Node(id)
	return n.Kind == TKBrand || (n.Kind == TKUnion && n.A != 0)
}
