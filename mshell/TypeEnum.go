package main

// Phase 1 enum support: registering `enum Name = a | b | c` declarations.
//
// An enum is a generative tagged sum type; v1 members are all nullary.
// Declaring an enum (1) registers a nominal TKEnum type in the type
// environment under its name (so the name resolves in type positions like
// def signatures), and (2) registers each member as a nullary constructor
// word `( -- Enum)` in the name-builtin table, so a bare member reference
// type-checks as construction through the ordinary resolveAndApply path.
//
// Members share the global word namespace: a member name that collides with
// an existing builtin / def / another enum member is rejected. See
// design/literal_or_enum_typing.html and ai/enum_implementation_plan.md.

// DeclareEnum registers the type and constructors for one `enum` declaration.
func (c *Checker) DeclareEnum(d *MShellEnumDecl) {
	if IsReservedTypeName(d.Name) {
		c.errors = append(c.errors, TypeError{Kind: TErrReservedTypeName, Pos: d.NameToken, Name: d.Name})
		return
	}
	if c.typeEnv == nil {
		c.typeEnv = make(map[NameId]TypeId, 8)
	}
	nameId := c.names.Intern(d.Name)
	if _, exists := c.typeEnv[nameId]; exists {
		c.errors = append(c.errors, TypeError{Kind: TErrDuplicateTypeName, Pos: d.NameToken, Name: d.Name})
		return
	}

	type member struct {
		name string
		tok  Token
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
		uniq = append(uniq, member{name: m, tok: tok})
		variants = append(variants, EnumVariant{Name: c.names.Intern(m)})
	}

	enumType := c.arena.MakeEnum(nameId, variants)
	c.typeEnv[nameId] = enumType

	// Register each (unique) member as a nullary constructor word.
	for _, u := range uniq {
		mid := c.names.Intern(u.name)
		if _, exists := c.nameBuiltins[mid]; exists {
			c.errors = append(c.errors, TypeError{
				Kind: TErrTypeParse, Pos: u.tok,
				Hint: "enum member '" + u.name + "' conflicts with an existing definition or builtin of the same name",
			})
			continue
		}
		c.nameBuiltins[mid] = append(c.nameBuiltins[mid], QuoteSig{Outputs: []TypeId{enumType}})
	}
}
