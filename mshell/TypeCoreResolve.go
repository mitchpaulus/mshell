package main

// Type expressions resolved to the types of the core checker
// (ai/type-core-calculus.typ): shapes and dicts are records with a status
// per label and a remainder, Maybe is the built-in enum, `type` names are
// alias references, and `never` is the whole output side of a signature.
//
// Signatures keep their generics as enum-parameter types (TKParam 0..n-1).
// Instantiating a signature substitutes them, with new unification
// variables at a call and with rigid types for checking a def's body, so a
// generic can never be confused with a live unification variable.

// coreResolver resolves parsed type expressions.
type coreResolver struct {
	arena *TypeArena
	names *NameTable
	rel   *Relations
	// aliases maps a `type` name, and the built-in Json, to its alias type.
	aliases map[NameId]TypeId
	// gens names the generics of the signature being resolved, in order of
	// first appearance; gens[i] is the parameter TKParam i. Nil outside a
	// signature, where an unknown name is an error.
	gens     []NameId
	inSig    bool
	errs     []TypeError
	jsonName NameId
}

// coreSigParts is a resolved signature before it is stored.
type coreSigParts struct {
	ins, outs []TypeId
	gens      []NameId
	// newOut has bit i set when output i is marked `new`.
	newOut   uint64
	diverges bool
}

// declareJson adds the built-in recursive alias
// `type Json = null | bool | int | float | str | [Json] | {str: Json}`.
func (r *coreResolver) declareJson() {
	r.jsonName = r.names.Intern("Json")
	idx := r.arena.DeclareAlias(r.jsonName)
	ref := r.arena.MakeAliasRef(idx)
	body := r.arena.MakeUnion([]TypeId{
		TidNull, TidBool, TidInt, TidFloat, TidStr,
		r.arena.MakeList(ref), r.arena.MakeStrDict(ref),
	}, NameNone)
	r.arena.SetAliasBody(idx, body)
	r.aliases[r.jsonName] = ref
}

// declareHtmlNode adds the built-in recursive alias of the nodes parseHtml
// returns: `type HtmlNode = {tag: str, attr: {str: str}, children: [HtmlNode], text: str}`.
func (r *coreResolver) declareHtmlNode() {
	name := r.names.Intern("HtmlNode")
	idx := r.arena.DeclareAlias(name)
	ref := r.arena.MakeAliasRef(idx)
	field := func(label string, t TypeId) RecordField {
		return RecordField{Name: r.names.Intern(label), Status: FieldRequired, Type: t}
	}
	body := r.arena.MakeRecord([]RecordField{
		field("tag", TidStr),
		field("attr", r.arena.MakeStrDict(TidStr)),
		field("children", r.arena.MakeList(ref)),
		field("text", TidStr),
	}, RecordField{Status: FieldOpen})
	r.arena.SetAliasBody(idx, body)
	r.aliases[name] = ref
}

// resolveSig resolves a signature. Unknown names are its generics.
func (r *coreResolver) resolveSig(ins, outs []MShellParseItem) coreSigParts {
	r.gens, r.inSig = r.gens[:0], true
	var p coreSigParts
	p.ins = make([]TypeId, 0, len(ins))
	for _, it := range ins {
		p.ins = append(p.ins, r.resolve(it))
	}
	if isNeverOutput(outs) {
		p.diverges = true
	} else {
		p.outs = make([]TypeId, 0, len(outs))
		for i, it := range outs {
			if nw, ok := it.(*TypeNewExpr); ok {
				if i < 64 {
					p.newOut |= 1 << i
				}
				it = nw.Inner
			}
			p.outs = append(p.outs, r.resolve(it))
		}
	}
	if len(r.gens) > 0 {
		p.gens = append([]NameId(nil), r.gens...)
	}
	r.gens, r.inSig = r.gens[:0], false
	return p
}

// isNeverOutput reports whether an output side is exactly `never`.
func isNeverOutput(outs []MShellParseItem) bool {
	if len(outs) != 1 {
		return false
	}
	n, ok := outs[0].(*TypeNamed)
	return ok && n.Name == "never"
}

// resolveType resolves a type outside a signature, as for `as T`.
func (r *coreResolver) resolveType(item MShellParseItem) TypeId {
	r.inSig = false
	return r.resolve(item)
}

func (r *coreResolver) errorf(tok Token, hint string) TypeId {
	r.errs = append(r.errs, TypeError{Kind: TErrTypeParse, Pos: tok, Hint: hint})
	return TidNothing
}

func (r *coreResolver) resolve(item MShellParseItem) TypeId {
	ar := r.arena
	switch n := item.(type) {
	case *TypePrim:
		return n.Tid
	case *TypeListExpr:
		return ar.MakeList(r.resolve(n.Elem))
	case *TypeDictExpr:
		return ar.MakeStrDict(r.resolve(n.Value))
	case *TypeShapeExpr:
		fields := make([]RecordField, 0, len(n.Fields))
		for _, f := range n.Fields {
			status := FieldRequired
			if f.Optional {
				status = FieldOptional
			}
			fields = append(fields, RecordField{Name: r.names.Intern(f.Name), Status: status, Type: r.resolve(f.Type)})
		}
		// A written shape type is open, or has a `*: T` remainder; shape
		// literals are the only exact shapes.
		rest := RecordField{Status: FieldOpen}
		if n.Wildcard != nil {
			rest = RecordField{Status: FieldOptional, Type: r.resolve(n.Wildcard)}
		}
		return ar.MakeRecord(fields, rest)
	case *TypeQuoteExpr:
		sig := QuoteSig{Inputs: make([]TypeId, 0, len(n.Inputs))}
		for _, in := range n.Inputs {
			sig.Inputs = append(sig.Inputs, r.resolve(in))
		}
		if isNeverOutput(n.Outputs) {
			sig.Diverges = true
		} else {
			sig.Outputs = make([]TypeId, 0, len(n.Outputs))
			for _, out := range n.Outputs {
				sig.Outputs = append(sig.Outputs, r.resolve(out))
			}
		}
		return ar.MakeQuote(sig)
	case *TypeUnionExpr:
		arms := make([]TypeId, 0, len(n.Arms))
		for _, a := range n.Arms {
			t := r.resolve(a)
			if t == TidNothing {
				return TidNothing
			}
			arms = append(arms, t)
		}
		u := ar.MakeUnion(arms, NameNone)
		if msg := r.unionKindsError(u); msg != "" {
			return r.errorf(n.StartTok, msg)
		}
		return u
	case *TypeNamed:
		return r.resolveNamed(n)
	case *TypeNewExpr:
		return r.errorf(n.Tok, "'new' is allowed only on the outputs of a def signature")
	}
	return TidNothing
}

func (r *coreResolver) resolveNamed(n *TypeNamed) TypeId {
	ar := r.arena
	switch n.Name {
	case "bytes":
		return TidBytes
	case "null":
		return TidNull
	case "path":
		return TidPath
	case "datetime":
		return TidDateTime
	case "Grid":
		return ar.MakeGrid(0)
	case "GridView":
		return ar.MakeGridView(0)
	case "GridRow":
		return ar.MakeGridRow(0)
	case "Maybe":
		if len(n.Args) != 1 {
			return TidNothing
		}
		return ar.MakeMaybeEnum(r.resolve(n.Args[0]))
	case "none":
		return r.errorf(n.Tok, "'none' is not a type; it is the empty constructor of Maybe. Use 'Maybe[T]' for an optional value, or 'null' for the JSON null type")
	case "never":
		return r.errorf(n.Tok, "'never' is allowed only as the whole output side of a signature, as in (str -- never)")
	}
	name := r.names.Intern(n.Name)
	if t, ok := r.aliases[name]; ok {
		return t
	}
	if !r.inSig {
		return r.errorf(n.Tok, "unknown type '"+n.Name+"'")
	}
	for i, g := range r.gens {
		if g == name {
			return ar.MakeParam(i)
		}
	}
	r.gens = append(r.gens, name)
	return ar.MakeParam(len(r.gens) - 1)
}

// unionKindsError reports a union with two members of the same runtime
// kind, which the design does not allow (ai/type-core-calculus.typ,
// "Unions have distinct kinds"), or "" when the union is well formed.
// Members with no kind (generics) are left to the checker.
func (r *coreResolver) unionKindsError(u TypeId) string {
	if r.arena.Node(u).Kind != TKUnion {
		return ""
	}
	var seen []valueKind
	for _, m := range r.arena.unionMembers[r.arena.Node(u).Extra] {
		ks, ok := r.rel.Kinds(m)
		if !ok {
			continue
		}
		for _, k := range ks {
			for _, s := range seen {
				if s == k {
					return "a union cannot have two members of the same kind (" +
						FormatType(r.arena, r.names, u) + "); declare an enum instead"
				}
			}
			seen = append(seen, k)
		}
	}
	return ""
}
