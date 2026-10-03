package main

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
)

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
	// bodyGens are the generics of the def whose body is being checked:
	// outside its signature, a type written in the body (`as`) may name
	// them, as the rigid types the body sees.
	bodyGens []NameId
	// anon counts the generics `dict` and `list` made in this signature,
	// and badNames are the unknown names it has reported.
	anon     int
	badNames []NameId
	errs     []TypeError
	jsonName NameId
	// builtin is set while the builtin table is built: there `Grid_s`,
	// `GridView_s` and `GridRow_s` are a grid whose schema is the generic s.
	builtin bool

	// enums maps a declared enum's name to its declaration (TypeCoreDecl.go).
	enums map[NameId]uint32
	// While an enum's payloads are resolved, params are its parameters
	// (TKParam i is params[i]) and self is its declaration, a reference to
	// which must pass the parameters in order; self is -1 otherwise.
	params []NameId
	self   int
	// While declarations are read, a union's kinds are checked once every
	// alias it may mention is resolved: deferUnions is set, and the unions
	// wait in unions.
	deferUnions bool
	unions      []coreUnionCheck
	// ids holds the inputs and outputs of the signatures resolved, which
	// live as long as the table or the check that holds them. Copies of a
	// resolver share it.
	ids *idChunks
}

// idChunks hands out slices of TypeIds carved from large chunks, so that
// signatures share a few allocations instead of making two each. A slice
// handed out is never handed out again, and its capacity is its length.
type idChunks struct {
	chunk []TypeId
}

// take returns an empty slice with room for n ids.
func (p *idChunks) take(n int) []TypeId {
	if n == 0 {
		return nil
	}
	if cap(p.chunk)-len(p.chunk) < n {
		p.chunk = make([]TypeId, 0, max(512, n))
	}
	i := len(p.chunk)
	p.chunk = p.chunk[:i+n]
	return p.chunk[i:i:i+n]
}

// coreUnionCheck is a union whose kinds are checked later, and where it
// was written.
type coreUnionCheck struct {
	u   TypeId
	tok Token
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
	})
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

// resolveSig resolves a signature. Unknown names of one letter, optionally
// followed by digits, are its generics.
func (r *coreResolver) resolveSig(ins, outs []MShellParseItem) coreSigParts {
	r.gens, r.inSig, r.anon, r.badNames = r.gens[:0], true, 0, r.badNames[:0]
	var p coreSigParts
	// A signature's inputs and outputs are tracked in 64-bit masks.
	for _, side := range [][]MShellParseItem{ins, outs} {
		if len(side) > 64 {
			r.errorf(side[64].GetStartToken(), "a signature takes at most 64 inputs and gives at most 64 outputs")
		}
	}
	if r.ids == nil {
		r.ids = &idChunks{}
	}
	p.ins = r.ids.take(len(ins))
	for _, it := range ins {
		p.ins = append(p.ins, r.resolve(it))
	}
	if isNeverOutput(outs) {
		p.diverges = true
	} else {
		p.outs = r.ids.take(len(outs))
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
		u := ar.MakeUnion(arms)
		if r.deferUnions {
			r.unions = append(r.unions, coreUnionCheck{u: u, tok: n.StartTok})
			return u
		}
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
		return ar.MakeGridOf(TKGrid, r.unknownSchema())
	case "GridView":
		return ar.MakeGridOf(TKGridView, r.unknownSchema())
	case "GridRow":
		return ar.MakeGridOf(TKGridRow, r.unknownSchema())
	case "Maybe":
		if len(n.Args) != 1 {
			return TidNothing
		}
		return ar.MakeMaybeEnum(r.resolve(n.Args[0]))
	case "dict", "list":
		// Short for `{str: T}` and `[T]`, with a new generic T for each
		// occurrence (decided 2026-10-01). Outside a signature there is no
		// generic to stand for T.
		if len(n.Args) > 0 {
			return r.errorf(n.Tok, "'"+n.Name+"' takes no arguments in brackets")
		}
		if !r.inSig {
			if n.Name == "dict" {
				return r.errorf(n.Tok, "'dict' needs its value type here: write `{str: T}`")
			}
			return r.errorf(n.Tok, "'list' needs its element type here: write `[T]`")
		}
		r.anon++
		g := r.generic(r.names.Intern("_" + strconv.Itoa(r.anon)))
		if n.Name == "dict" {
			return ar.MakeStrDict(g)
		}
		return ar.MakeList(g)
	case "none":
		return r.errorf(n.Tok, "'none' is not a type; it is the empty constructor of Maybe. Use 'Maybe[T]' for an optional value, or 'null' for the JSON null type")
	case "never":
		return r.errorf(n.Tok, "'never' is allowed only as the whole output side of a signature, as in (str -- never)")
	}
	if kind, letter, ok := schemaGeneric(n.Name); ok && r.builtin && r.inSig {
		return ar.MakeGridOf(kind, r.generic(r.names.Intern(letter)))
	}
	name := r.names.Intern(n.Name)
	for i, p := range r.params {
		if p == name {
			if len(n.Args) > 0 {
				return r.errorf(n.Tok, "'"+n.Name+"' is a parameter of the enum, and takes no arguments")
			}
			return ar.MakeParam(i)
		}
	}
	if idx, ok := r.enums[name]; ok {
		return r.enumType(n, idx)
	}
	if len(n.Args) > 0 {
		return r.errorf(n.Tok, "'"+n.Name+"' is not a generic enum, so it takes no arguments in brackets")
	}
	if t, ok := r.aliases[name]; ok {
		return t
	}
	if !r.inSig {
		if slices.Contains(r.bodyGens, name) {
			return ar.MakeRigid(name)
		}
		return r.errorf(n.Tok, "unknown type '"+n.Name+"'")
	}
	if n.Name == "new" {
		// Read as a mark only before a def's output type.
		return r.errorf(n.Tok, "'new' marks a def output as a new value, so it goes only before an output type: (int -- new [int])")
	}
	if !r.builtin && !isGenericName(n.Name) {
		// A generic is introduced by writing it, so a misspelled or
		// imagined type would silently become one (plan question 17).
		// Each name is reported once per signature.
		if slices.Contains(r.badNames, name) {
			return TidNothing
		}
		r.badNames = append(r.badNames, name)
		if hint, ok := notATypeHints[n.Name]; ok {
			return r.errorf(n.Tok, "'"+n.Name+"' is not a type: "+hint)
		}
		return r.errorf(n.Tok, "unknown type '"+n.Name+"'; a generic is a single letter, optionally followed by digits, such as `a` or `T1`")
	}
	return r.generic(name)
}

// isGenericName reports whether a name in a signature may be a generic:
// one letter, optionally followed by digits (`a`, `T`, `T1`). A generic
// needs no declaration, so a longer unknown name is an error rather than
// a generic: it is almost always a misspelled or imagined type.
func isGenericName(s string) bool {
	for i, r := range s {
		if i == 0 && !unicode.IsLetter(r) || i > 0 && !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

// notATypeHints are the type names people reach for that mshell spells
// differently.
var notATypeHints = map[string]string{
	"numeric":   "write `int | float`",
	"number":    "write `int | float`",
	"string":    "write `str`",
	"integer":   "write `int`",
	"boolean":   "write `bool`",
	"double":    "write `float`",
	"date":      "write `datetime`",
	"binary":    "write `bytes`",
	"quote":     "write the quote's type, such as `(int -- int)`",
	"quotation": "write the quote's type, such as `(int -- int)`",
	"function":  "write the quote's type, such as `(int -- int)`",
	"any":       "no type holds every value; use a generic, such as `a`",
}

// enumType is the enum declared at idx, at the arguments written after its
// name.
func (r *coreResolver) enumType(n *TypeNamed, idx uint32) TypeId {
	ar := r.arena
	decl := ar.EnumDecl(idx)
	if len(n.Args) != len(decl.Params) {
		if len(decl.Params) == 0 {
			return r.errorf(n.Tok, "the enum '"+n.Name+"' has no parameters, so it takes no arguments in brackets")
		}
		return r.errorf(n.Tok, "the enum '"+n.Name+"' takes "+strconv.Itoa(len(decl.Params))+" argument(s), as in "+
			n.Name+"["+strings.TrimSpace(strings.Repeat("T ", len(decl.Params)))+"]")
	}
	args := make([]TypeId, len(n.Args))
	for i, a := range n.Args {
		if args[i] = r.resolve(a); args[i] == TidNothing {
			return TidNothing
		}
	}
	if r.self == int(idx) {
		// A recursive reference passes the parameters in order: a usability
		// rule, not a soundness one (design doc, "Generic enums").
		for i, a := range args {
			if a != ar.MakeParam(i) {
				return r.errorf(n.Tok, "inside its own declaration, '"+n.Name+"' must be used with its own parameters in order: "+
					n.Name+"["+strings.Join(r.paramNames(), " ")+"]")
			}
		}
	}
	return ar.MakeEnum(idx, args)
}

func (r *coreResolver) paramNames() []string {
	out := make([]string, len(r.params))
	for i, p := range r.params {
		out[i] = r.names.Name(p)
	}
	return out
}

// generic is the signature's generic called name, added if it is new.
func (r *coreResolver) generic(name NameId) TypeId {
	for i, g := range r.gens {
		if g == name {
			return r.arena.MakeParam(i)
		}
	}
	r.gens = append(r.gens, name)
	return r.arena.MakeParam(len(r.gens) - 1)
}

// unknownSchema is the schema of a grid whose columns are not known: the
// read-only `{| open}`, which every schema is below. Reading a column of
// it gives unknown, and nothing writes into it unless the grid is new.
func (r *coreResolver) unknownSchema() TypeId {
	return r.arena.MakeRecord(nil, RecordField{Status: FieldOpen})
}

// schemaGeneric reads a builtin signature's `Grid_s`, `GridView_s` or
// `GridRow_s`: a grid kind whose schema is the generic s.
func schemaGeneric(name string) (TypeKind, string, bool) {
	for _, k := range []struct {
		prefix string
		kind   TypeKind
	}{{"Grid_", TKGrid}, {"GridView_", TKGridView}, {"GridRow_", TKGridRow}} {
		if rest, ok := strings.CutPrefix(name, k.prefix); ok && len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' {
			return k.kind, rest, true
		}
	}
	return 0, "", false
}

// genericName names t when it is a generic: of the signature being read,
// or of the def whose body is checked.
func (r *coreResolver) genericName(t TypeId) (string, bool) {
	n := r.arena.Node(t)
	switch n.Kind {
	case TKParam:
		if r.inSig && int(n.A) < len(r.gens) {
			return r.names.Name(r.gens[n.A]), true
		}
		return "", false
	case TKRigid:
		return r.names.Name(NameId(n.A)), true
	}
	return "", false
}

// unionKindsError reports a union with two members of the same runtime
// kind, which the design does not allow (ai/type-core-calculus.typ,
// "Unions have distinct kinds"), or "" when the union is well formed.
// A generic has no kind, so it cannot be a member: at an instance two
// members could have one kind, and a kind pattern would pick the wrong one.
func (r *coreResolver) unionKindsError(u TypeId) string {
	if r.arena.Node(u).Kind != TKUnion {
		return ""
	}
	var buf [16]valueKind
	seen := buf[:0]
	for _, m := range r.arena.unionMembers[r.arena.Node(u).Extra] {
		n := len(seen)
		var ok bool
		if seen, ok = r.rel.AppendKinds(seen, m); !ok {
			seen = seen[:n]
			if name, generic := r.genericName(m); generic {
				return "'" + name + "' is a generic, so it cannot be a member of a union: it has no kind," +
					" and an instance could give the union two members of one kind; declare an enum instead"
			}
			continue
		}
		for i := n; i < len(seen); i++ {
			if slices.Contains(seen[:i], seen[i]) {
				return "a union cannot have two members of the same kind (" +
					FormatType(r.arena, r.names, u) + "); declare an enum instead"
			}
		}
	}
	return ""
}
