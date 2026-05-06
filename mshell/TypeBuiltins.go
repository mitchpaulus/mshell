package main

// Static table of builtin signatures. Phase 2 carries a deliberately small
// slice — primitive-typed arithmetic and comparison only — so the checker
// can be exercised end-to-end against `2 3 +` style inputs before the
// composite kinds and overloading land in later phases.
//
// The table is keyed by TokenType for token-driven dispatch (analogous to
// the runtime's switch in Evaluator.go). When a builtin grows past one
// signature (overloading), the value type becomes a slice and dispatch
// gains a specificity step (Phase 9).

// builtinSigsByName registers builtins that arrive as LITERAL tokens
// (i.e. ordinary identifiers in source). Phase 4 introduces this path
// for `just` and `none`; later phases will fill in `len`, `map`, and
// the rest as the relevant kinds and overloading land.
//
// Type variables in canonical sigs are allocated as plain TKVar nodes
// without going through Substitution.FreshVar — they live in the sig's
// Generics list and are fresh-renamed at every call site by
// Checker.Instantiate. Two separate canonical sigs may both use
// TypeVarId(0); they don't collide because renameVars produces fresh
// per-call variables before unification ever touches them.
func builtinSigsByName(arena *TypeArena, names *NameTable) map[NameId]QuoteSig {
	out := make(map[NameId]QuoteSig, 4)

	// just : (T -- Maybe[T])
	{
		t := arena.MakeVar(0)
		out[names.Intern("just")] = QuoteSig{
			Inputs:   []TypeId{t},
			Outputs:  []TypeId{arena.MakeMaybe(t)},
			Generics: []TypeVarId{0},
		}
	}

	// none : ( -- Maybe[T])
	// The free T is not constrained by inputs; if the surrounding code
	// never pins it, the result type is "Maybe[?]" and a later phase
	// will diagnose. For now we just push the unbound var.
	{
		t := arena.MakeVar(0)
		out[names.Intern("none")] = QuoteSig{
			Inputs:   nil,
			Outputs:  []TypeId{arena.MakeMaybe(t)},
			Generics: []TypeVarId{0},
		}
	}

	return out
}

// builtinSigsByToken returns the Phase-2 builtin sigs. Each call rebuilds
// the map; the table is small and the cost is paid once per checker
// session at startup. A future phase can promote it to a package-level
// var indexed by TokenType for faster lookup.
func builtinSigsByToken() map[TokenType]QuoteSig {
	intIntInt := QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}}
	intIntBool := QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidBool}}
	return map[TokenType]QuoteSig{
		PLUS:               intIntInt,
		MINUS:              intIntInt,
		ASTERISK:           intIntInt,
		ASTERISKBINARY:     intIntInt,
		LESSTHAN:           intIntBool,
		GREATERTHAN:        intIntBool,
		LESSTHANOREQUAL:    intIntBool,
		GREATERTHANOREQUAL: intIntBool,
	}
}
