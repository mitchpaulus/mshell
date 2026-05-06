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
