package main

import (
	"slices"
	"strings"
)

// Declarations in the core checker: `type` aliases and enums
// (ai/type-core-calculus.typ, "Aliases and recursive types", "Enums",
// "Names"; ai/type-system-plan.md, stage 4). They are read in three passes,
// so a declaration may mention one written after it:
//
//  1. every name is reserved;
//  2. alias bodies and enum payloads are resolved;
//  3. every cycle of alias references must pass a type constructor: a list,
//     dict, shape, quote or enum instance. Without that the assumption rule
//     that compares recursive types is unsound, not only slow (H13,
//     `unguarded_*` in formal-ver/Recursive.v). Then the unions written in
//     the declarations are checked for two members of one kind, each alias
//     that does not refer to itself is replaced by its body (eraseAliases),
//     each enum parameter's variance and the enums' other properties are
//     computed, and each member gets its constructor.

// coreCtor is an enum member: its enum, its position in the declaration, and
// its constructor's signature, payloads to the enum.
type coreCtor struct {
	enum uint32
	idx  int
	sig  coreSig
}

// reservedTypeNames are names the language already gives a type, or
// nothing a declaration may take.
var reservedTypeNames = map[string]bool{
	"binary": true, "null": true, "path": true, "datetime": true, "Grid": true, "GridView": true,
	"GridRow": true, "Maybe": true, "never": true, "none": true, "new": true,
}

// declareAll reads the declarations among items. defNames are the file's
// definitions, which a member's name may not take.
func (c *coreChecker) declareAll(items []MShellParseItem, defNames map[string]Token) {
	type decl struct {
		alias *MShellTypeDecl
		enum  *MShellEnumDecl
		idx   uint32
	}
	if len(declarationItems(items)) == 0 {
		return
	}
	var decls []decl
	var enumIdxs []uint32
	if c.res.enums == nil {
		c.res.enums = map[NameId]uint32{}
	}
	if c.ctors == nil {
		c.ctors = map[NameId]*coreCtor{}
	}
	if c.declared == nil {
		c.declared = map[NameId]Token{}
	}
	taken := c.declared
	declErr := func(tok Token, hint string) {
		c.errs = append(c.errs, TypeError{Kind: TErrDeclaration, Pos: tok, Hint: hint})
	}
	// reserve takes a type or member name, or reports why it cannot.
	reserve := func(tok Token, member bool) (NameId, bool) {
		lex := tok.Lexeme
		name := c.names.Intern(lex)
		what := "a type"
		if member {
			what = "an enum member"
		}
		_, builtinAlias := c.res.aliases[name]
		def, isDef := defNames[lex]
		switch prev, ok := taken[name]; {
		case ok && prev.TokenFile == builtinDeclFile:
			declErr(tok, "'"+lex+"' is a built-in name, so "+what+" cannot have that name")
		case ok:
			declErr(tok, "'"+lex+"' is already declared at "+tokenPosStr(prev))
		case reservedTypeNames[lex] || builtinAlias:
			declErr(tok, "'"+lex+"' is a built-in type, so "+what+" cannot have that name")
		case isPatternWord(lex):
			declErr(tok, "'"+lex+"' has a meaning of its own in match patterns, so "+what+" cannot have that name")
		case c.hasStartupDef(lex):
			prev, _ := c.startupDef(lex)
			declErr(tok, "'"+lex+"' is the name of the definition at "+tokenPosStr(prev)+", so "+what+" cannot have that name")
		case c.isBuiltinName(lex):
			declErr(tok, "'"+lex+"' is the name of a builtin, so "+what+" cannot have that name")
		case isDef:
			declErr(tok, "'"+lex+"' is the name of the definition at "+tokenPosStr(def)+", so "+what+" cannot have that name")
		default:
			taken[name] = tok
			return name, true
		}
		return name, false
	}

	// Pass 1: reserve every name.
	for _, item := range items {
		switch d := item.(type) {
		case *MShellTypeDecl:
			name, ok := reserve(withFile(d.NameToken, d.File), false)
			if !ok {
				continue
			}
			idx := c.arena.DeclareAlias(name)
			c.res.aliases[name] = c.arena.MakeAliasRef(idx)
			decls = append(decls, decl{alias: d, idx: idx})
		case *MShellEnumDecl:
			name, ok := reserve(withFile(d.NameToken, d.File), false)
			if !ok {
				continue
			}
			ed := EnumDecl{Name: name}
			for i, p := range d.Params {
				pn := c.names.Intern(p.Lexeme)
				for _, q := range d.Params[:i] {
					if q.Lexeme == p.Lexeme {
						declErr(withFile(p, d.File), "the parameter '"+p.Lexeme+"' is written twice")
						ok = false
					}
				}
				ed.Params = append(ed.Params, EnumParam{Name: pn})
			}
			for _, m := range d.MemberToks {
				mn, mok := reserve(withFile(m, d.File), true)
				ok = ok && mok
				ed.Ctors = append(ed.Ctors, EnumCtor{Name: mn})
			}
			if !ok {
				continue
			}
			idx := c.arena.DeclareEnum(ed)
			c.res.enums[name] = idx
			enumIdxs = append(enumIdxs, idx)
			decls = append(decls, decl{enum: d, idx: idx})
		}
	}

	// Pass 2: resolve the bodies. Errors here, and the unions checked
	// later, carry the declaration's file.
	c.res.deferUnions = true
	for _, d := range decls {
		errMark, unionMark := len(c.res.errs), len(c.res.unions)
		var file *TokenFile
		if d.alias != nil {
			file = d.alias.File
			c.arena.SetAliasBody(d.idx, c.res.resolveType(d.alias.Body))
		} else {
			file = d.enum.File
			c.res.params = c.res.params[:0]
			for _, p := range d.enum.Params {
				c.res.params = append(c.res.params, c.names.Intern(p.Lexeme))
			}
			c.res.self = int(d.idx)
			ctors := c.arena.EnumDecl(d.idx).Ctors
			for j, payload := range d.enum.MemberPayloads {
				for _, item := range payload {
					ctors[j].Payload = append(ctors[j].Payload, c.res.resolveType(item))
				}
			}
			c.res.params, c.res.self = c.res.params[:0], -1
		}
		for i := errMark; i < len(c.res.errs); i++ {
			c.res.errs[i].Pos.TokenFile = file
		}
		for i := unionMark; i < len(c.res.unions); i++ {
			c.res.unions[i].tok.TokenFile = file
		}
	}
	c.res.deferUnions = false
	c.takeResolveErrors()

	// Pass 3: guarded recursion, unions, and what each enum is.
	var aliases []coreAliasDecl
	for _, d := range decls {
		if d.alias != nil {
			aliases = append(aliases, coreAliasDecl{idx: d.idx, tok: withFile(d.alias.NameToken, d.alias.File)})
		}
	}
	guarded := c.checkGuarded(aliases)
	for _, u := range c.res.unions {
		if !guarded {
			break
		}
		if msg := c.declUnionError(u.u); msg != "" {
			declErr(u.tok, msg)
		}
	}
	c.res.unions = c.res.unions[:0]
	c.eraseAliases(aliases, enumIdxs)
	if len(enumIdxs) > 0 {
		c.rel.AnalyzeEnums(enumIdxs)
	}
	for _, d := range decls {
		if d.enum == nil {
			continue
		}
		if !c.rel.WellFormedEnum(d.idx) {
			c.errs = append(c.errs, TypeError{Kind: TErrCoreInternal, Pos: withFile(d.enum.NameToken, d.enum.File),
				Hint: "the variances computed for enum '" + d.enum.Name + "' do not pass its own declaration check"})
		}
		c.declareCtors(d.idx)
	}
}

// checkDefName reports a definition whose name is taken by an earlier one in
// the file, a builtin or a standard library definition (design doc,
// "Names"), and records it in defNames.
func (c *coreChecker) checkDefName(def MShellDefinition, defNames map[string]Token) {
	hint := ""
	if prev, ok := defNames[def.Name]; ok {
		hint = "'" + def.Name + "' is already defined at " + tokenPosStr(prev)
	} else if _, ok := BuiltInList[def.Name]; ok {
		hint = "'" + def.Name + "' is the name of a builtin"
	} else if prev, ok := c.startupDef(def.Name); ok {
		hint = "'" + def.Name + "' is already defined at " + tokenPosStr(prev)
	} else if c.isBuiltinName(def.Name) {
		hint = "'" + def.Name + "' is already defined in the standard library"
	} else if ct := c.ctorNamed(def.Name); ct != nil {
		hint = "'" + def.Name + "' is a member of enum '" + c.names.Name(c.arena.EnumDecl(ct.enum).Name) + "'"
	} else {
		defNames[def.Name] = withFile(def.NameToken, def.File)
		return
	}
	c.errs = append(c.errs, TypeError{Kind: TErrDeclaration, Pos: def.NameToken, Hint: hint})
}

// startupDef returns the name token, with its file, of the startup
// files' definition named lex.
func (c *coreChecker) startupDef(lex string) (Token, bool) {
	id, ok := c.names.Lookup(lex)
	if !ok {
		return Token{}, false
	}
	tok, ok := c.table.startupDefs[id]
	return tok, ok
}

func (c *coreChecker) hasStartupDef(lex string) bool {
	_, ok := c.startupDef(lex)
	return ok
}

// isBuiltinName reports whether a word is a builtin or a standard library
// definition.
func (c *coreChecker) isBuiltinName(lex string) bool {
	if _, ok := BuiltInList[lex]; ok {
		return true
	}
	if id, ok := c.names.Lookup(lex); ok {
		return c.table.name(id) != nil
	}
	return false
}

// coreAliasDecl is a declared alias and its name's token.
type coreAliasDecl struct {
	idx uint32
	tok Token
}

// checkGuarded reports an alias that reaches itself through unions and other
// aliases alone, and returns whether there was none. Aliases are visited in
// declaration order; each one on a cycle is reported once.
func (c *coreChecker) checkGuarded(aliases []coreAliasDecl) bool {
	ok := true
	toks := map[uint32]Token{}
	var order []uint32
	for _, a := range aliases {
		toks[a.idx] = a.tok
		order = append(order, a.idx)
	}
	const (
		unseen = iota
		onPath
		done
	)
	state := map[uint32]int{}
	reported := map[uint32]bool{}
	var path []uint32
	var visit func(idx uint32)
	visit = func(idx uint32) {
		state[idx] = onPath
		path = append(path, idx)
		for _, next := range c.unguardedRefs(c.arena.aliases[idx].Body, nil) {
			switch state[next] {
			case unseen:
				visit(next)
			case onPath:
				// The cycle is the path from next to here.
				start := len(path) - 1
				for path[start] != next {
					start--
				}
				names := make([]string, 0, len(path)-start+1)
				for _, a := range path[start:] {
					names = append(names, c.names.Name(c.arena.aliases[a].Name))
				}
				names = append(names, c.names.Name(c.arena.aliases[next].Name))
				for _, a := range path[start:] {
					if reported[a] {
						continue
					}
					reported[a] = true
					ok = false
					tok, has := toks[a]
					if !has {
						continue
					}
					c.errs = append(c.errs, TypeError{Kind: TErrDeclaration, Pos: tok,
						Hint: "'" + tok.Lexeme + "' refers to itself with nothing in between that holds a value (" +
							strings.Join(names, " -> ") + "); a recursive type must pass a list, dict, shape, quote or enum, as in `type T = int | [T]`"})
				}
			}
		}
		path = path[:len(path)-1]
		state[idx] = done
	}
	for _, idx := range order {
		if state[idx] == unseen {
			visit(idx)
		}
	}
	return ok
}

// eraseAliases makes each declared alias that does not refer to itself its
// body: its name resolves to the body, and every reference to it, in the
// other aliases' bodies and the enums' payloads, is replaced by the body.
// Only recursive aliases are left as alias types, as in the model, where
// such an alias is the recursive type it denotes; so nothing in the checker
// sees an alias it would have to look through to find a record, a list or a
// quote that is not recursive. The body keeps the name for messages.
func (c *coreChecker) eraseAliases(aliases []coreAliasDecl, enumIdxs []uint32) {
	ar := c.arena
	plain := map[uint32]bool{}
	for _, a := range aliases {
		if ar.aliases[a.idx].Body != TidNothing && !c.aliasRecursive(a.idx) {
			plain[a.idx] = true
		}
	}
	if len(plain) == 0 {
		return
	}
	done := map[TypeId]TypeId{}
	var erase func(t TypeId) TypeId
	erase = func(t TypeId) TypeId {
		if t == TidNothing {
			return t
		}
		if e, ok := done[t]; ok {
			return e
		}
		e := t
		n := ar.nodes[t]
		switch n.Kind {
		case TKAlias:
			if plain[n.A] {
				e = erase(ar.aliases[n.A].Body)
			}
		case TKList:
			e = ar.MakeList(erase(TypeId(n.A)))
		case TKCommand:
			e = ar.MakeCommand(erase(TypeId(n.A)), CommandCaptureMode(n.B), CommandCaptureMode(n.Extra))
		case TKRecord:
			rec := ar.records[n.Extra]
			fields := make([]RecordField, len(rec.Fields))
			for i, f := range rec.Fields {
				fields[i] = f
				fields[i].Type = erase(f.Type)
			}
			rest := rec.Rest
			rest.Type = erase(rest.Type)
			e = ar.MakeRecord(fields, rest)
		case TKUnion:
			members := slices.Clone(ar.unionMembers[n.Extra])
			for i, m := range members {
				members[i] = erase(m)
			}
			e = ar.MakeUnion(members)
		case TKQuote:
			sig := ar.quoteSigs[n.Extra]
			out := QuoteSig{Diverges: sig.Diverges}
			for _, x := range sig.Inputs {
				out.Inputs = append(out.Inputs, erase(x))
			}
			for _, x := range sig.Outputs {
				out.Outputs = append(out.Outputs, erase(x))
			}
			e = ar.MakeQuote(out)
		case TKEnum:
			args := slices.Clone(ar.enumArgs[n.Extra])
			for i, x := range args {
				args[i] = erase(x)
			}
			e = ar.MakeEnum(n.A, args)
		case TKGrid, TKGridView, TKGridRow:
			if n.A != 0 {
				e = ar.MakeGridOf(n.Kind, erase(TypeId(n.A)))
			}
		}
		done[t] = e
		return e
	}
	for _, a := range aliases {
		decl := &ar.aliases[a.idx]
		body := erase(decl.Body)
		if plain[a.idx] {
			c.res.aliases[decl.Name] = body
			ar.NameType(body, decl.Name)
		} else {
			ar.SetAliasBody(a.idx, body)
		}
	}
	for _, idx := range enumIdxs {
		ctors := ar.EnumDecl(idx).Ctors
		for j := range ctors {
			for k, t := range ctors[j].Payload {
				ctors[j].Payload[k] = erase(t)
			}
		}
	}
}

// aliasRecursive reports whether the alias at idx refers to itself,
// directly or through other aliases.
func (c *coreChecker) aliasRecursive(idx uint32) bool {
	seen := map[uint32]bool{}
	var walk func(t TypeId) bool
	walk = func(t TypeId) bool {
		if t == TidNothing {
			return false
		}
		ar := c.arena
		n := ar.nodes[t]
		switch n.Kind {
		case TKAlias:
			if n.A == idx {
				return true
			}
			if seen[n.A] {
				return false
			}
			seen[n.A] = true
			return walk(ar.aliases[n.A].Body)
		case TKList, TKCommand:
			return walk(TypeId(n.A))
		case TKRecord:
			rec := ar.records[n.Extra]
			for _, f := range rec.Fields {
				if walk(f.Type) {
					return true
				}
			}
			return walk(rec.Rest.Type)
		case TKUnion:
			for _, m := range ar.unionMembers[n.Extra] {
				if walk(m) {
					return true
				}
			}
		case TKQuote:
			sig := ar.quoteSigs[n.Extra]
			for _, x := range append(append([]TypeId(nil), sig.Inputs...), sig.Outputs...) {
				if walk(x) {
					return true
				}
			}
		case TKEnum:
			for _, x := range ar.enumArgs[n.Extra] {
				if walk(x) {
					return true
				}
			}
		case TKGrid, TKGridView, TKGridRow:
			return walk(TypeId(n.A))
		}
		return false
	}
	return walk(c.arena.aliases[idx].Body)
}

// unguardedRefs appends the aliases t refers to without passing a type
// constructor: at its top, or as members of its unions.
func (c *coreChecker) unguardedRefs(t TypeId, out []uint32) []uint32 {
	if t == TidNothing {
		return out
	}
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKAlias:
		return append(out, n.A)
	case TKUnion:
		for _, m := range c.arena.unionMembers[n.Extra] {
			out = c.unguardedRefs(m, out)
		}
	}
	return out
}

// declUnionError checks a union written in a declaration, once every alias
// is resolved: its members have distinct kinds, and none is an enum
// parameter, which has no kind.
func (c *coreChecker) declUnionError(u TypeId) string {
	if c.arena.nodes[u].Kind != TKUnion {
		return ""
	}
	for _, m := range c.arena.unionMembers[c.arena.nodes[u].Extra] {
		if c.arena.nodes[m].Kind == TKParam {
			return "an enum parameter cannot be a member of a union, since nothing says what kind of value it is; " +
				"give the member its own constructor instead"
		}
	}
	return c.res.unionKindsError(u)
}

// declareCtors gives each member of the enum at idx its constructor: a
// signature from the payload types to the enum at its parameters. A
// parameter no payload of the member mentions is ⊥ when it is covariant,
// so `none` is a `Maybe[⊥]`, and a new variable otherwise, as `[]` is a
// list of one (design doc, "Enums").
func (c *coreChecker) declareCtors(idx uint32) {
	ar := c.arena
	decl := ar.EnumDecl(idx)
	gens := make([]NameId, len(decl.Params))
	for i, p := range decl.Params {
		gens[i] = p.Name
	}
	for j, ctor := range decl.Ctors {
		args := make([]TypeId, len(decl.Params))
		for i, p := range decl.Params {
			mentioned := false
			for _, t := range ctor.Payload {
				mentioned = mentioned || mentionsParam(ar, t, i)
			}
			if !mentioned && p.Variance == VarCo {
				args[i] = TidBottom
			} else {
				args[i] = ar.MakeParam(i)
			}
		}
		parts := coreSigParts{ins: ctor.Payload, outs: []TypeId{ar.MakeEnum(idx, args)}}
		if len(gens) > 0 {
			parts.gens = gens
		}
		sig := newCoreSig(ar, parts)
		// A constructed value is fresh when every payload is fresh or
		// immutable (tw_con_dp, tw_con_sh).
		sig.keepOut = 1
		c.ctors[ctor.Name] = &coreCtor{enum: idx, idx: j, sig: sig}
	}
}

// ctorNamed is the enum member called lex, or nil.
func (c *coreChecker) ctorNamed(lex string) *coreCtor {
	if len(c.ctors) == 0 {
		return nil
	}
	if id, ok := c.names.Lookup(lex); ok {
		return c.ctors[id]
	}
	return nil
}
