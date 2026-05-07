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
	out[names.Intern("w")] = []QuoteSig{consumeAny()}      // write no newline
	out[names.Intern("we")] = []QuoteSig{consumeAny()}     // write to stderr no newline
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

	// ----- Higher-order list ops -----

	// map : ([T] (T -- U) -- [U])
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		fn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{t},
			Outputs: []TypeId{u},
		})
		out[names.Intern("map")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t), fn},
			Outputs:  []TypeId{arena.MakeList(u)},
			Generics: []TypeVarId{0, 1},
		}}
	}

	// filter : ([T] (T -- bool) -- [T])
	{
		t := arena.MakeVar(0)
		fn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{t},
			Outputs: []TypeId{TidBool},
		})
		out[names.Intern("filter")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t), fn},
			Outputs:  []TypeId{arena.MakeList(t)},
			Generics: []TypeVarId{0},
		}}
	}

	// each : ([T] (T -- ) -- )
	{
		t := arena.MakeVar(0)
		fn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{t},
			Outputs: nil,
		})
		out[names.Intern("each")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t), fn},
			Outputs:  nil,
			Generics: []TypeVarId{0},
		}}
	}

	// ----- Arithmetic LITERAL ops -----

	// `/` is LITERAL (no dedicated token). Overloaded int/float.
	out[names.Intern("/")] = []QuoteSig{
		{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat, TidFloat}, Outputs: []TypeId{TidFloat}},
	}
	// `mod` : (int int -- int) | (float float -- float)
	out[names.Intern("mod")] = []QuoteSig{
		{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat, TidFloat}, Outputs: []TypeId{TidFloat}},
	}

	// ----- Dict ops -----

	// keys : ({K: V} -- [K])
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("keys")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v)},
			Outputs:  []TypeId{arena.MakeList(k)},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// values : ({K: V} -- [V])
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("values")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v)},
			Outputs:  []TypeId{arena.MakeList(v)},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// set : ({K: V} K V -- {K: V})
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("set")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v), k, v},
			Outputs:  []TypeId{arena.MakeDict(k, v)},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// setd : ({K: V} K V -- )  — drop variant
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("setd")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v), k, v},
			Outputs:  nil,
			Generics: []TypeVarId{0, 1},
		}}
	}
	// get : ({K: V} K -- Maybe[V])
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("get")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v), k},
			Outputs:  []TypeId{arena.MakeMaybe(v)},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// getDef : ({K: V} K V -- V)  — get with default
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("getDef")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v), k, v},
			Outputs:  []TypeId{v},
			Generics: []TypeVarId{0, 1},
		}}
	}
	// keyValues : ({K: V} -- [[K, V]])  — list of [k, v] pairs.
	// Modeled here as `[Maybe[V]]` would lie; pairs in mshell are
	// represented as 2-element lists at runtime, and the list is
	// heterogeneous (K and V). We approximate as `[T]` with T fresh —
	// callers typically use `2unpack` / pattern-match to recover.
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		t := arena.MakeVar(2)
		out[names.Intern("keyValues")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeDict(k, v)},
			Outputs:  []TypeId{arena.MakeList(arena.MakeList(t))},
			Generics: []TypeVarId{0, 1, 2},
		}}
	}
	// in : (K {K: V} -- bool) | (T [T] -- bool) | (str str -- bool)
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("in")] = []QuoteSig{
			{
				Inputs:   []TypeId{k, arena.MakeDict(k, v)},
				Outputs:  []TypeId{TidBool},
				Generics: []TypeVarId{0, 1},
			},
			{
				Inputs:   []TypeId{arena.MakeVar(0), arena.MakeList(arena.MakeVar(0))},
				Outputs:  []TypeId{TidBool},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:  []TypeId{TidStr, TidStr},
				Outputs: []TypeId{TidBool},
			},
		}
	}

	// ----- List unpack -----

	// 2unpack : ([T] -- T T)
	{
		t := arena.MakeVar(0)
		out[names.Intern("2unpack")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  []TypeId{t, t},
			Generics: []TypeVarId{0},
		}}
	}

	// ----- String ops -----

	// join : ([str] str -- str)
	out[names.Intern("join")] = []QuoteSig{{
		Inputs:  []TypeId{arena.MakeList(TidStr), TidStr},
		Outputs: []TypeId{TidStr},
	}}
	// wsplit : (str -- [str])
	out[names.Intern("wsplit")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{arena.MakeList(TidStr)},
	}}
	// split : (str str -- [str])
	out[names.Intern("split")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr},
		Outputs: []TypeId{arena.MakeList(TidStr)},
	}}
	// lines : (str -- [str])
	out[names.Intern("lines")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{arena.MakeList(TidStr)},
	}}
	// unlines : ([str] -- str)
	out[names.Intern("unlines")] = []QuoteSig{{
		Inputs:  []TypeId{arena.MakeList(TidStr)},
		Outputs: []TypeId{TidStr},
	}}
	// trim : (str -- str), trimStart, trimEnd
	for _, name := range []string{"trim", "trimStart", "trimEnd", "upper", "lower"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidStr},
			Outputs: []TypeId{TidStr},
		}}
	}
	// chomp : (str -- str)
	out[names.Intern("chomp")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{TidStr},
	}}

	// ----- Maybe ops -----

	// Overload `map` to also handle Maybe[T] (T -- U) -> Maybe[U].
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		fn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{t},
			Outputs: []TypeId{u},
		})
		mapName := names.Intern("map")
		out[mapName] = append(out[mapName], QuoteSig{
			Inputs:   []TypeId{arena.MakeMaybe(t), fn},
			Outputs:  []TypeId{arena.MakeMaybe(u)},
			Generics: []TypeVarId{0, 1},
		})
	}
	// isJust : (Maybe[T] -- bool)
	{
		t := arena.MakeVar(0)
		out[names.Intern("isJust")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeMaybe(t)},
			Outputs:  []TypeId{TidBool},
			Generics: []TypeVarId{0},
		}}
	}
	// isNone : (Maybe[T] -- bool)
	{
		t := arena.MakeVar(0)
		out[names.Intern("isNone")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeMaybe(t)},
			Outputs:  []TypeId{TidBool},
			Generics: []TypeVarId{0},
		}}
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

// builtinSigsByToken returns sigs for ops that have dedicated lexer
// tokens. The map values are slices so overload dispatch (Phase 9)
// drives token-typed builtins the same way it drives LITERAL ones —
// arithmetic now has int and float overloads, string concatenation
// can be added later via additional `+` arms, etc.
//
// STR is the conversion form (T -- str); the lexer emits STR for the
// bare `str` keyword in expression position. The TypeExpr parser
// handles STR in type position separately and never consults this
// table.
func builtinSigsByToken(arena *TypeArena) map[TokenType][]QuoteSig {
	intIntInt := QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}}
	floatFloatFloat := QuoteSig{Inputs: []TypeId{TidFloat, TidFloat}, Outputs: []TypeId{TidFloat}}
	intIntBool := QuoteSig{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidBool}}
	floatFloatBool := QuoteSig{Inputs: []TypeId{TidFloat, TidFloat}, Outputs: []TypeId{TidBool}}

	arithmetic := []QuoteSig{intIntInt, floatFloatFloat}
	comparison := []QuoteSig{intIntBool, floatFloatBool}

	// STR : (T -- str) — generic conversion to string.
	t := arena.MakeVar(0)
	strConv := QuoteSig{
		Inputs:   []TypeId{t},
		Outputs:  []TypeId{TidStr},
		Generics: []TypeVarId{0},
	}
	// Polymorphic equality: both operands must unify.
	eqSig := QuoteSig{
		Inputs:   []TypeId{arena.MakeVar(0), arena.MakeVar(0)},
		Outputs:  []TypeId{TidBool},
		Generics: []TypeVarId{0},
	}

	// QUESTION (`?`): unwrap a Maybe[T] to T (None aborts at runtime).
	// Process-execute forms (list `?` for exit code) are not handled
	// here yet — list/process types live outside the v1 type surface.
	questionSig := func() QuoteSig {
		t := arena.MakeVar(0)
		return QuoteSig{
			Inputs:   []TypeId{arena.MakeMaybe(t)},
			Outputs:  []TypeId{t},
			Generics: []TypeVarId{0},
		}
	}

	// IFF: structural conditional with one or two quote arms.
	//   bool [-- ]       iff  → no-effect, single arm
	//   bool [-- ] [-- ] iff  → no-effect, two arms
	//   bool [-- T] [-- T] iff → both arms push one T (common case
	//                            in tests: `bool ("a") ("b") iff`)
	iffSigs := func() []QuoteSig {
		emptyQuote := arena.MakeQuote(QuoteSig{})
		t := arena.MakeVar(0)
		oneOutQuote := arena.MakeQuote(QuoteSig{Outputs: []TypeId{t}})
		return []QuoteSig{
			{Inputs: []TypeId{TidBool, emptyQuote}, Outputs: nil},
			{Inputs: []TypeId{TidBool, emptyQuote, emptyQuote}, Outputs: nil},
			{
				Inputs:   []TypeId{TidBool, oneOutQuote, oneOutQuote},
				Outputs:  []TypeId{t},
				Generics: []TypeVarId{0},
			},
		}
	}

	// LOOP: pop a quote with no net stack effect.
	loopSig := QuoteSig{
		Inputs: []TypeId{arena.MakeQuote(QuoteSig{})},
	}

	// BREAK / CONTINUE: no stack effect on the surrounding scope.
	noOp := QuoteSig{}

	return map[TokenType][]QuoteSig{
		PLUS:               arithmetic,
		MINUS:              arithmetic,
		ASTERISK:           arithmetic,
		ASTERISKBINARY:     arithmetic,
		LESSTHAN:           comparison,
		GREATERTHAN:        comparison,
		LESSTHANOREQUAL:    comparison,
		GREATERTHANOREQUAL: comparison,
		STR:                {strConv},
		NOT:                {{Inputs: []TypeId{TidBool}, Outputs: []TypeId{TidBool}}},
		EQUALS:             {eqSig},
		NOTEQUAL:           {eqSig},
		QUESTION:           {questionSig()},
		IFF:                iffSigs(),
		LOOP:               {loopSig},
		BREAK:              {noOp},
		CONTINUE:           {noOp},
	}
}
