package main

// Translation from the old TypeChecking-style TypeDefinition (built by
// the existing parser at `def` declarations) into the new Checker's
// QuoteSig. This is a bridge during V1 — Phase 11 deletes the old
// type-system parse tree and the translator with it. Until then, this
// is what lets every user-defined function get a sig in the new
// arena without touching the parser.
//
// Generics scoping: a `def` introduces its own scope of named generics.
// Every occurrence of `a` within one sig refers to the same TypeVarId;
// across two sigs the same name is unrelated. The translator maintains
// a per-def name map and lists every fresh TypeVarId in QuoteSig.Generics
// so Checker.Instantiate fresh-renames at every call site.

import "fmt"

// TranslateTypeDef produces a QuoteSig from an old-parser TypeDefinition.
// Anything the translator can't represent yields TidNothing for that
// slot and an error string in errs; the caller decides whether to drop
// the def or register it with placeholders.
func TranslateTypeDef(arena *TypeArena, names *NameTable, def *TypeDefinition) (QuoteSig, []string) {
	tx := &typeDefTranslator{
		arena:    arena,
		names:    names,
		generics: map[string]TypeVarId{},
	}
	inputs := make([]TypeId, 0, len(def.InputTypes))
	for _, in := range def.InputTypes {
		inputs = append(inputs, tx.translate(in))
	}
	outputs := make([]TypeId, 0, len(def.OutputTypes))
	for _, out := range def.OutputTypes {
		outputs = append(outputs, tx.translate(out))
	}
	gens := make([]TypeVarId, 0, len(tx.generics))
	for _, v := range tx.generics {
		gens = append(gens, v)
	}
	return QuoteSig{
		Inputs:   inputs,
		Outputs:  outputs,
		Generics: gens,
	}, tx.errs
}

type typeDefTranslator struct {
	arena    *TypeArena
	names    *NameTable
	generics map[string]TypeVarId
	nextVar  uint32
	errs     []string
}

// freshVar issues the next unused TypeVarId for this translation. They
// are local indices; Checker.Instantiate rewrites them to fresh
// substitution slots at each call site.
func (tx *typeDefTranslator) freshVar() TypeVarId {
	v := TypeVarId(tx.nextVar)
	tx.nextVar++
	return v
}

func (tx *typeDefTranslator) translate(t MShellType) TypeId {
	switch v := t.(type) {
	case TypeInt:
		return TidInt
	case TypeFloat:
		return TidFloat
	case TypeString:
		return TidStr
	case TypeBool:
		return TidBool
	case TypeBinary:
		return TidBytes
	case TypeGeneric:
		switch v.Name {
		case "path":
			return TidPath
		case "datetime":
			return TidDateTime
		}
		// Generics are name-scoped per def. First occurrence allocates
		// a fresh var; later occurrences of the same name reuse it.
		if id, ok := tx.generics[v.Name]; ok {
			return tx.arena.MakeVar(id)
		}
		id := tx.freshVar()
		tx.generics[v.Name] = id
		return tx.arena.MakeVar(id)
	case *TypeHomogeneousList:
		return tx.arena.MakeList(tx.translate(v.ListType))
	case *TypeQuote:
		ins := make([]TypeId, 0, len(v.InputTypes))
		for _, in := range v.InputTypes {
			ins = append(ins, tx.translate(in))
		}
		outs := make([]TypeId, 0, len(v.OutputTypes))
		for _, out := range v.OutputTypes {
			outs = append(outs, tx.translate(out))
		}
		return tx.arena.MakeQuote(QuoteSig{Inputs: ins, Outputs: outs})
	case *TypeDictionary:
		if v.WildCardType != nil {
			if len(v.TypeMap) == 0 {
				return tx.arena.MakeDict(TidStr, tx.translate(v.WildCardType))
			}
			// V1 has no "shape plus wildcard" type. Preserve the
			// concrete fields as a shape; width subtyping accepts
			// extra runtime keys.
			fields := make([]ShapeField, 0, len(v.TypeMap))
			for key, typ := range v.TypeMap {
				fields = append(fields, ShapeField{Name: tx.names.Intern(key), Type: tx.translate(typ)})
			}
			return tx.arena.MakeShape(fields)
		}
		fields := make([]ShapeField, 0, len(v.TypeMap))
		for key, typ := range v.TypeMap {
			fields = append(fields, ShapeField{Name: tx.names.Intern(key), Type: tx.translate(typ)})
		}
		return tx.arena.MakeShape(fields)
	case *TypeTuple:
		// V1 has no fixed-arity tuple type. Model `[a b ...]` from
		// the def syntax as a homogeneous list whose element type is
		// the union of the tuple's slot types. Arity is lost but
		// 2unpack / map / each users get a usable element type.
		elems := make([]TypeId, 0, len(v.Types))
		for _, et := range v.Types {
			elems = append(elems, tx.translate(et))
		}
		if len(elems) == 1 {
			return tx.arena.MakeList(elems[0])
		}
		return tx.arena.MakeList(tx.arena.MakeUnion(elems, NameNone))
	}
	tx.errs = append(tx.errs, fmt.Sprintf("unknown MShellType %T (skipped)", t))
	return TidNothing
}
