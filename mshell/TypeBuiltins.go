package main

// Static table of builtin signatures consumed by the type checker.
//
// Two indices share this table:
//
//   - builtinSigsByToken: keyed by TokenType, used for ops that have a
//     dedicated lexer token (PLUS, STR, TYPEINT, etc.).
//   - builtinSigsByName: keyed by NameId, used for ops that arrive as
//     LITERAL tokens (dup, swap, just, none, wl, ...).
//
// As the table grows, more programs become checkable under
// --check-types. Adding a builtin requires both an accurate Inputs/
// Outputs slice and (when generic) a Generics list with TypeVarId(0)
// allocated as plain TKVar — Checker.Instantiate fresh-renames at
// every call site so collisions across canonical sigs are impossible.

// builtinSigsByName registers builtins that arrive as LITERAL tokens.
func builtinSigsByName(arena *TypeArena, names *NameTable) map[NameId][]QuoteSig {
	out := make(map[NameId][]QuoteSig, 32)

	// ----- Maybe constructors -----

	// just : (T -- Maybe[T])
	{
		t := arena.MakeVar(0)
		out[names.Intern("just")] = []QuoteSig{{
			Inputs:   []TypeId{t},
			Outputs:  []TypeId{arena.MakeMaybe(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// none : ( -- Maybe[T])
	{
		t := arena.MakeVar(0)
		out[names.Intern("none")] = []QuoteSig{{
			Inputs:   nil,
			Outputs:  []TypeId{arena.MakeMaybe(t)},
			Generics: []TypeVarId{0},
		}}
	}

	// ----- Stack manipulation (polymorphic) -----

	// dup : (T -- T T)
	{
		t := arena.MakeVar(0)
		out[names.Intern("dup")] = []QuoteSig{{
			Inputs:   []TypeId{t},
			Outputs:  []TypeId{t, t},
			Generics: []TypeVarId{0},
		}}
	}
	// drop : (T -- )
	{
		t := arena.MakeVar(0)
		out[names.Intern("drop")] = []QuoteSig{{
			Inputs:   []TypeId{t},
			Outputs:  nil,
			Generics: []TypeVarId{0},
		}}
	}
	// swap : (T U -- U T)
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		out[names.Intern("swap")] = []QuoteSig{{
			Inputs:   []TypeId{t, u},
			Outputs:  []TypeId{u, t},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// over : (T U -- T U T)
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		out[names.Intern("over")] = []QuoteSig{{
			Inputs:   []TypeId{t, u},
			Outputs:  []TypeId{t, u, t},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// rot : (T U V -- U V T)
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		v := arena.MakeVar(2)
		out[names.Intern("rot")] = []QuoteSig{{
			Inputs:   []TypeId{t, u, v},
			Outputs:  []TypeId{u, v, t},
			Generics: []TypeVarId{0, 1, 2},
		}}
	}
	// nip : (T U -- U)
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		out[names.Intern("nip")] = []QuoteSig{{
			Inputs:   []TypeId{t, u},
			Outputs:  []TypeId{u},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// tuck : (T U -- U T U)
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		out[names.Intern("tuck")] = []QuoteSig{{
			Inputs:   []TypeId{t, u},
			Outputs:  []TypeId{u, t, u},
			Generics: []TypeVarId{0, 1},
		}}
	}

	// ----- I/O (consume one of anything, no output) -----

	consumeAny := func() QuoteSig {
		t := arena.MakeVar(0)
		return QuoteSig{
			Inputs:   []TypeId{t},
			Outputs:  nil,
			Generics: []TypeVarId{0},
		}
	}
	out[names.Intern("wl")] = []QuoteSig{consumeAny()}     // write line
	out[names.Intern("wle")] = []QuoteSig{consumeAny()}    // write line stderr
	out[names.Intern("print")] = []QuoteSig{consumeAny()}  // write no newline
	out[names.Intern("printe")] = []QuoteSig{consumeAny()} // write to stderr no newline

	// wln : ( -- )  write just a newline
	out[names.Intern("wln")] = []QuoteSig{{}}

	// ----- Boolean ops -----
	// `not` lexes as NOT (token type), not LITERAL — see byToken table.

	out[names.Intern("and")] = []QuoteSig{{
		Inputs:  []TypeId{TidBool, TidBool},
		Outputs: []TypeId{TidBool},
	}}
	out[names.Intern("or")] = []QuoteSig{{
		Inputs:  []TypeId{TidBool, TidBool},
		Outputs: []TypeId{TidBool},
	}}

	// ----- Arithmetic helpers -----

	out[names.Intern("abs")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidFloat}},
	}
	out[names.Intern("neg")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidFloat}},
	}

	// ----- Numeric conversions -----

	// toFloat : (int -- float) | (float -- float)
	out[names.Intern("toFloat")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidFloat}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidFloat}},
	}
	// toInt : (float -- int) | (str -- int)
	out[names.Intern("toInt")] = []QuoteSig{
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
	}

	// ----- List ops -----

	// len : ([T] -- int) | (str -- int) | ({K: V} -- int)
	{
		t := arena.MakeVar(0)
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("len")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t)},
				Outputs:  []TypeId{TidInt},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:  []TypeId{TidStr},
				Outputs: []TypeId{TidInt},
			},
			{
				Inputs:   []TypeId{arena.MakeDict(k, v)},
				Outputs:  []TypeId{TidInt},
				Generics: []TypeVarId{0, 1},
			},
		}
	}

	// append : ([T] T -- [T])
	{
		t := arena.MakeVar(0)
		out[names.Intern("append")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t), t},
			Outputs:  []TypeId{arena.MakeList(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// push : ([T] T -- [T])  (alias of append in mshell)
	{
		t := arena.MakeVar(0)
		out[names.Intern("push")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t), t},
			Outputs:  []TypeId{arena.MakeList(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// reverse : ([T] -- [T]) | (str -- str)
	{
		t := arena.MakeVar(0)
		out[names.Intern("reverse")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t)},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:  []TypeId{TidStr},
				Outputs: []TypeId{TidStr},
			},
		}
	}

	// ----- Type introspection -----

	// typeof : (T -- str)
	{
		t := arena.MakeVar(0)
		out[names.Intern("typeof")] = []QuoteSig{{
			Inputs:   []TypeId{t},
			Outputs:  []TypeId{TidStr},
			Generics: []TypeVarId{0},
		}}
	}

	return out
}

// builtinSigsByToken returns sigs for ops that have dedicated lexer tokens.
// STR is the conversion form (T -- str); the lexer emits STR for the bare
// `str` keyword in expression position. The TypeExpr parser handles STR
// in type position separately and never consults this table.
func builtinSigsByToken(arena *TypeArena) map[TokenType]QuoteSig {
	intIntInt := QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}}
	intIntBool := QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidBool}}

	// STR : (T -- str) — generic conversion to string.
	t := arena.MakeVar(0)
	strConv := QuoteSig{
		Inputs:   []TypeId{t},
		Outputs:  []TypeId{TidStr},
		Generics: []TypeVarId{0},
	}

	return map[TokenType]QuoteSig{
		PLUS:               intIntInt,
		MINUS:              intIntInt,
		ASTERISK:           intIntInt,
		ASTERISKBINARY:     intIntInt,
		LESSTHAN:           intIntBool,
		GREATERTHAN:        intIntBool,
		LESSTHANOREQUAL:    intIntBool,
		GREATERTHANOREQUAL: intIntBool,
		STR:                strConv,
		NOT:                {Inputs: []TypeId{TidBool}, Outputs: []TypeId{TidBool}},
		EQUALS: {
			Inputs:   []TypeId{arena.MakeVar(0), arena.MakeVar(0)},
			Outputs:  []TypeId{TidBool},
			Generics: []TypeVarId{0},
		},
		NOTEQUAL: {
			Inputs:   []TypeId{arena.MakeVar(0), arena.MakeVar(0)},
			Outputs:  []TypeId{TidBool},
			Generics: []TypeVarId{0},
		},
	}
}
