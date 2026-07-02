package main

// Enum support: registering `enum Name = a | b(T..) | ...` declarations.
//
// An enum is a generative tagged sum type. Registration happens in two passes
// so member payloads may reference any enum regardless of order (including the
// enum itself):
//
//   - predeclareEnum interns the name and registers a placeholder TKEnum in the
//     type environment, so the name resolves in any later type position.
//   - defineEnum resolves each member's payload types, finalizes the variant
//     list, and registers each member as a constructor word whose signature
//     consumes the payload and produces the enum (`(T.. -- Enum)`).
//
// Members share the global word namespace: a member name that collides with an
// existing builtin / def / another enum member is rejected.

// predeclareEnum registers the enum's name with a placeholder type. It returns
// true when the name was newly registered (so defineEnum should finish it), and
// false for a reserved or duplicate name (an error is recorded).
func (c *Checker) predeclareEnum(d *MShellEnumDecl) bool {
	if IsReservedTypeName(d.Name) {
		c.errors = append(c.errors, TypeError{Kind: TErrReservedTypeName, Pos: d.NameToken, Name: d.Name})
		return false
	}
	if c.typeEnv == nil {
		c.typeEnv = make(map[NameId]TypeId, 8)
	}
	nameId := c.names.Intern(d.Name)
	if _, exists := c.typeEnv[nameId]; exists {
		c.errors = append(c.errors, TypeError{Kind: TErrDuplicateTypeName, Pos: d.NameToken, Name: d.Name})
		return false
	}
	c.typeEnv[nameId] = c.arena.MakeEnum(nameId, nil)
	return true
}

// defineEnum resolves payload types, finalizes the variant list, and registers
// constructor words. It must run after predeclareEnum has registered the name.
func (c *Checker) defineEnum(d *MShellEnumDecl) {
	nameId := c.names.Intern(d.Name)
	enumType := c.typeEnv[nameId]

	type member struct {
		name     string
		tok      Token
		payloads []TypeId
	}
	uniq := make([]member, 0, len(d.Members))
	variants := make([]EnumVariant, 0, len(d.Members))
	seen := make(map[string]bool, len(d.Members))
	for i, m := range d.Members {
		tok := d.MemberToks[i]
		if seen[m] {
			c.errors = append(c.errors, TypeError{
				Kind: TErrTypeParse, Pos: tok,
				Hint: "duplicate enum member '" + m + "' in '" + d.Name + "'",
			})
			continue
		}
		seen[m] = true

		var payloads []TypeId
		for _, p := range d.MemberPayloads[i] {
			payloads = append(payloads, c.resolveTypeExpr(p, nil))
		}
		uniq = append(uniq, member{name: m, tok: tok, payloads: payloads})
		variants = append(variants, EnumVariant{Name: c.names.Intern(m), Payload: payloads})
	}

	c.arena.SetEnumVariants(enumType, variants)

	for _, u := range uniq {
		mid := c.names.Intern(u.name)
		if _, exists := c.nameBuiltins[mid]; exists {
			c.errors = append(c.errors, TypeError{
				Kind: TErrTypeParse, Pos: u.tok,
				Hint: "enum member '" + u.name + "' conflicts with an existing definition or builtin of the same name",
			})
			continue
		}
		c.nameBuiltins[mid] = append(c.nameBuiltins[mid], QuoteSig{Inputs: u.payloads, Outputs: []TypeId{enumType}})
		if c.enumMemberToks == nil {
			c.enumMemberToks = make(map[NameId]Token, len(uniq))
		}
		c.enumMemberToks[mid] = u.tok
	}
}
