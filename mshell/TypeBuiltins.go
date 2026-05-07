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

	// `and`/`or` overload: plain (bool bool -- bool) and a
	// short-circuit form taking a quote that yields a bool —
	//   (bool [-- bool] -- bool)
	{
		boolQuote := arena.MakeQuote(QuoteSig{Outputs: []TypeId{TidBool}})
		out[names.Intern("and")] = []QuoteSig{
			{Inputs: []TypeId{TidBool, TidBool}, Outputs: []TypeId{TidBool}},
			{Inputs: []TypeId{TidBool, boolQuote}, Outputs: []TypeId{TidBool}},
		}
		out[names.Intern("or")] = []QuoteSig{
			{Inputs: []TypeId{TidBool, TidBool}, Outputs: []TypeId{TidBool}},
			{Inputs: []TypeId{TidBool, boolQuote}, Outputs: []TypeId{TidBool}},
		}
	}

	// ----- Arithmetic helpers -----

	out[names.Intern("abs")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidFloat}},
	}
	out[names.Intern("neg")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidFloat}},
	}
	// Trig / log / sqrt — runtime requires float input (mshell
	// rejects implicit int->float coercion). std.msh defines
	// `cos`, `tan`, `ln2`, `ln10` on top of these primitives.
	for _, name := range []string{"sin", "arctan", "ln", "sqrt"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidFloat},
			Outputs: []TypeId{TidFloat},
		}}
	}
	// pow : (float float -- float)  base, exponent.
	out[names.Intern("pow")] = []QuoteSig{{
		Inputs:  []TypeId{TidFloat, TidFloat},
		Outputs: []TypeId{TidFloat},
	}}
	// floor / ceil / round : (int -- int) | (float -- int)
	for _, name := range []string{"floor", "ceil", "round"} {
		out[names.Intern(name)] = []QuoteSig{
			{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
			{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidInt}},
		}
	}
	// random / randomFixed : ( -- float )
	for _, name := range []string{"random", "randomFixed"} {
		out[names.Intern(name)] = []QuoteSig{{Outputs: []TypeId{TidFloat}}}
	}

	// ----- Numeric conversions -----

	// toFloat : (int -- float) | (float -- float) | (str -- Maybe[float])
	out[names.Intern("toFloat")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidFloat}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidFloat}},
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{arena.MakeMaybe(TidFloat)}},
	}
	// toFixed : (float|int int -- str)  format value with N decimals
	out[names.Intern("toFixed")] = []QuoteSig{
		{Inputs: []TypeId{TidFloat, TidInt}, Outputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidStr}},
	}
	// toInt : (float -- int) | (str -- int)
	out[names.Intern("toInt")] = []QuoteSig{
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidInt}},
	}

	// ----- Path / DateTime / File ops -----

	// String-castable: str, path, literal — model as str|path overloads.
	// (Literals don't have a TypeId equivalent in the value system.)

	// toPath : (str -- path) | (path -- path)
	out[names.Intern("toPath")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidPath}},
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidPath}},
	}
	// toDt : (str -- Maybe[datetime]) | (datetime -- datetime)
	out[names.Intern("toDt")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{arena.MakeMaybe(TidDateTime)}},
		{Inputs: []TypeId{TidDateTime}, Outputs: []TypeId{TidDateTime}},
	}
	// now : ( -- datetime )
	out[names.Intern("now")] = []QuoteSig{{Outputs: []TypeId{TidDateTime}}}

	// date : (datetime -- datetime)  — strip time-of-day to midnight
	out[names.Intern("date")] = []QuoteSig{{
		Inputs:  []TypeId{TidDateTime},
		Outputs: []TypeId{TidDateTime},
	}}
	// day/month/year/hour/minute/second : (datetime -- int)
	for _, name := range []string{"day", "month", "year", "hour", "minute", "second", "weekday"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidDateTime},
			Outputs: []TypeId{TidInt},
		}}
	}
	// toUnixTime / toUnixTimeMilli / toUnixTimeMicro / toUnixTimeNano :
	//   (datetime -- int)
	for _, name := range []string{"toUnixTime", "toUnixTimeMilli", "toUnixTimeMicro", "toUnixTimeNano"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidDateTime},
			Outputs: []TypeId{TidInt},
		}}
	}
	// dateFmt : (datetime str -- str)
	out[names.Intern("dateFmt")] = []QuoteSig{{
		Inputs:  []TypeId{TidDateTime, TidStr},
		Outputs: []TypeId{TidStr},
	}}

	// readFile : (str -- str) | (path -- str)
	out[names.Intern("readFile")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidStr}},
	}
	// readFileBytes : (str -- bytes) | (path -- bytes)
	out[names.Intern("readFileBytes")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidBytes}},
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidBytes}},
	}
	// files / dirs : ( -- [path] )
	for _, name := range []string{"files", "dirs"} {
		out[names.Intern(name)] = []QuoteSig{{Outputs: []TypeId{arena.MakeList(TidPath)}}}
	}

	// ----- List ops -----

	// len : ([T] -- int) | (str -- int) | ({K: V} -- int)
	//     | (Grid -- int) | (GridView -- int) | (GridRow -- int)
	// Grid/GridView return rows; GridRow returns columns.
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
			{Inputs: []TypeId{arena.MakeGrid(0)}, Outputs: []TypeId{TidInt}},
			{Inputs: []TypeId{arena.MakeGridView(0)}, Outputs: []TypeId{TidInt}},
			{Inputs: []TypeId{arena.MakeGridRow(0)}, Outputs: []TypeId{TidInt}},
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
	// reverse : ([T] -- [T]) | (str -- str) | (Grid|GridView -- GridView)
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
			{
				Inputs:  []TypeId{arena.MakeGrid(0)},
				Outputs: []TypeId{arena.MakeGridView(0)},
			},
			{
				Inputs:  []TypeId{arena.MakeGridView(0)},
				Outputs: []TypeId{arena.MakeGridView(0)},
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
	//        | ({K: V} (V -- bool) -- {K: V})
	//        | (Grid|GridView ( -- bool) -- GridView)  — predicate
	//          uses `:col?`-style getters against the implicit row.
	{
		t := arena.MakeVar(0)
		fn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{t},
			Outputs: []TypeId{TidBool},
		})
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		dictFn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{v},
			Outputs: []TypeId{TidBool},
		})
		gridRowFn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{arena.MakeGridRow(0)},
			Outputs: []TypeId{TidBool},
		})
		out[names.Intern("filter")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), fn},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{arena.MakeDict(k, v), dictFn},
				Outputs:  []TypeId{arena.MakeDict(k, v)},
				Generics: []TypeVarId{0, 1},
			},
			{Inputs: []TypeId{arena.MakeGrid(0), gridRowFn}, Outputs: []TypeId{arena.MakeGridView(0)}},
			{Inputs: []TypeId{arena.MakeGridView(0), gridRowFn}, Outputs: []TypeId{arena.MakeGridView(0)}},
		}
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

	// `/` is LITERAL (no dedicated token). Overloaded:
	//   int int -- int       arithmetic division
	//   float float -- float arithmetic division
	//   path path -- path    filepath.Join
	out[names.Intern("/")] = []QuoteSig{
		{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidFloat, TidFloat}, Outputs: []TypeId{TidFloat}},
		{Inputs: []TypeId{TidPath, TidPath}, Outputs: []TypeId{TidPath}},
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
	// in : ({K: V} K -- bool) | (str str -- bool)
	// Stack order matches the runtime: the haystack (dict or
	// string) is below, the needle (key or substring) on top.
	{
		k := arena.MakeVar(0)
		v := arena.MakeVar(1)
		out[names.Intern("in")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeDict(k, v), k},
				Outputs:  []TypeId{TidBool},
				Generics: []TypeVarId{0, 1},
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
	// title (alongside upper/lower): (str -- str)
	// trim : (str -- str), trimStart, trimEnd
	for _, name := range []string{"trim", "trimStart", "trimEnd", "upper", "lower", "title"} {
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

	// ----- Numeric / formatting ops -----

	// toFixed : (int int -- str) | (float int -- str)
	out[names.Intern("toFixed")] = []QuoteSig{
		{Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidFloat, TidInt}, Outputs: []TypeId{TidStr}},
	}
	// numFmt : (int {str: V} -- str) | (float {str: V} -- str)
	{
		v := arena.MakeVar(0)
		dict := arena.MakeDict(TidStr, v)
		out[names.Intern("numFmt")] = []QuoteSig{
			{
				Inputs:   []TypeId{TidInt, dict},
				Outputs:  []TypeId{TidStr},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{TidFloat, arena.MakeDict(TidStr, arena.MakeVar(0))},
				Outputs:  []TypeId{TidStr},
				Generics: []TypeVarId{0},
			},
		}
	}
	// countSubStr : (str str -- int)
	out[names.Intern("countSubStr")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr},
		Outputs: []TypeId{TidInt},
	}}
	// toJson : (T -- str) — generic conversion to JSON.
	{
		t := arena.MakeVar(0)
		out[names.Intern("toJson")] = []QuoteSig{{
			Inputs:   []TypeId{t},
			Outputs:  []TypeId{TidStr},
			Generics: []TypeVarId{0},
		}}
	}

	// ----- Grid ops -----
	//
	// In V1 we don't track grid schemas through these operations,
	// so every grid sig uses the unknown-schema variants
	// (schemaIdx 0). Element types extracted from columns are
	// modeled as fresh generics — overload dispatch and the
	// downstream walk treat the result as polymorphic.

	gridU := arena.MakeGrid(0)
	gridViewU := arena.MakeGridView(0)
	gridRowU := arena.MakeGridRow(0)

	// gridRows / gridCols : (Grid|GridView -- int) / (-- [str])
	out[names.Intern("gridRows")] = []QuoteSig{
		{Inputs: []TypeId{gridU}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{gridViewU}, Outputs: []TypeId{TidInt}},
	}
	out[names.Intern("gridCols")] = []QuoteSig{
		{Inputs: []TypeId{gridU}, Outputs: []TypeId{arena.MakeList(TidStr)}},
		{Inputs: []TypeId{gridViewU}, Outputs: []TypeId{arena.MakeList(TidStr)}},
	}
	// gridMeta : (Grid|GridView -- Maybe[{str: V}])
	{
		v := arena.MakeVar(0)
		metaDict := arena.MakeDict(TidStr, v)
		out[names.Intern("gridMeta")] = []QuoteSig{
			{
				Inputs:   []TypeId{gridU},
				Outputs:  []TypeId{arena.MakeMaybe(metaDict)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{gridViewU},
				Outputs:  []TypeId{arena.MakeMaybe(arena.MakeDict(TidStr, arena.MakeVar(0)))},
				Generics: []TypeVarId{0},
			},
		}
	}
	// gridColMeta : (Grid|GridView str -- Maybe[{str: V}])
	{
		v := arena.MakeVar(0)
		metaDict := arena.MakeDict(TidStr, v)
		out[names.Intern("gridColMeta")] = []QuoteSig{
			{
				Inputs:   []TypeId{gridU, TidStr},
				Outputs:  []TypeId{arena.MakeMaybe(metaDict)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{gridViewU, TidStr},
				Outputs:  []TypeId{arena.MakeMaybe(arena.MakeDict(TidStr, arena.MakeVar(0)))},
				Generics: []TypeVarId{0},
			},
		}
	}
	// gridCol : (Grid|GridView str -- [T])
	{
		t := arena.MakeVar(0)
		out[names.Intern("gridCol")] = []QuoteSig{
			{
				Inputs:   []TypeId{gridU, TidStr},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{gridViewU, TidStr},
				Outputs:  []TypeId{arena.MakeList(arena.MakeVar(0))},
				Generics: []TypeVarId{0},
			},
		}
	}
	// gridValues : (Grid|GridView -- [[T]])
	{
		t := arena.MakeVar(0)
		out[names.Intern("gridValues")] = []QuoteSig{
			{
				Inputs:   []TypeId{gridU},
				Outputs:  []TypeId{arena.MakeList(arena.MakeList(t))},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{gridViewU},
				Outputs:  []TypeId{arena.MakeList(arena.MakeList(arena.MakeVar(0)))},
				Generics: []TypeVarId{0},
			},
		}
	}
	// toGrid : ([T] -- Grid)
	{
		t := arena.MakeVar(0)
		out[names.Intern("toGrid")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  []TypeId{gridU},
			Generics: []TypeVarId{0},
		}}
	}
	// toDict : (GridRow -- {str: V})
	{
		v := arena.MakeVar(0)
		out[names.Intern("toDict")] = []QuoteSig{{
			Inputs:   []TypeId{gridRowU},
			Outputs:  []TypeId{arena.MakeDict(TidStr, v)},
			Generics: []TypeVarId{0},
		}}
	}
	// sortBy : (Grid|GridView str -- GridView) | (Grid|GridView [str] -- GridView)
	out[names.Intern("sortBy")] = []QuoteSig{
		{Inputs: []TypeId{gridU, TidStr}, Outputs: []TypeId{gridViewU}},
		{Inputs: []TypeId{gridViewU, TidStr}, Outputs: []TypeId{gridViewU}},
		{Inputs: []TypeId{gridU, arena.MakeList(TidStr)}, Outputs: []TypeId{gridViewU}},
		{Inputs: []TypeId{gridViewU, arena.MakeList(TidStr)}, Outputs: []TypeId{gridViewU}},
	}
	// gridSetCell : (Grid str int T -- Grid)
	{
		t := arena.MakeVar(0)
		out[names.Intern("gridSetCell")] = []QuoteSig{{
			Inputs:   []TypeId{gridU, TidStr, TidInt, t},
			Outputs:  []TypeId{gridU},
			Generics: []TypeVarId{0},
		}}
	}
	// gridAddCol : (Grid str [T] -- Grid) | (Grid str T -- Grid)
	{
		t := arena.MakeVar(0)
		out[names.Intern("gridAddCol")] = []QuoteSig{
			{
				Inputs:   []TypeId{gridU, TidStr, arena.MakeList(t)},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{gridU, TidStr, arena.MakeVar(0)},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{0},
			},
		}
	}
	// gridRemoveCol : (Grid str -- Grid)
	out[names.Intern("gridRemoveCol")] = []QuoteSig{{
		Inputs:  []TypeId{gridU, TidStr},
		Outputs: []TypeId{gridU},
	}}
	// updateCol : (Grid|GridView str (T -- U) -- Grid)
	{
		t := arena.MakeVar(0)
		u := arena.MakeVar(1)
		fn := arena.MakeQuote(QuoteSig{Inputs: []TypeId{t}, Outputs: []TypeId{u}})
		out[names.Intern("updateCol")] = []QuoteSig{
			{
				Inputs:   []TypeId{gridU, TidStr, fn},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{0, 1},
			},
			{
				Inputs: []TypeId{gridViewU, TidStr, arena.MakeQuote(QuoteSig{
					Inputs:  []TypeId{arena.MakeVar(0)},
					Outputs: []TypeId{arena.MakeVar(1)},
				})},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{0, 1},
			},
		}
	}
	// sortByCmp : ([T] (T T -- int) -- [T]) | (Grid (GridRow GridRow -- int) -- Grid)
	{
		t := arena.MakeVar(0)
		listCmp := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{t, t},
			Outputs: []TypeId{TidInt},
		})
		rowCmp := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{gridRowU, gridRowU},
			Outputs: []TypeId{TidInt},
		})
		out[names.Intern("sortByCmp")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), listCmp},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{Inputs: []TypeId{gridU, rowCmp}, Outputs: []TypeId{gridU}},
			{Inputs: []TypeId{gridViewU, rowCmp}, Outputs: []TypeId{gridViewU}},
		}
	}
	// parseCsv : (str -- [[str]]) | (path -- [[str]])
	out[names.Intern("parseCsv")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{arena.MakeList(arena.MakeList(TidStr))}},
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{arena.MakeList(arena.MakeList(TidStr))}},
	}
	// parseJson : (str -- T) | (path -- T) | (bytes -- T)
	{
		t := arena.MakeVar(0)
		out[names.Intern("parseJson")] = []QuoteSig{
			{Inputs: []TypeId{TidStr}, Outputs: []TypeId{t}, Generics: []TypeVarId{0}},
			{Inputs: []TypeId{TidPath}, Outputs: []TypeId{arena.MakeVar(0)}, Generics: []TypeVarId{0}},
			{Inputs: []TypeId{TidBytes}, Outputs: []TypeId{arena.MakeVar(0)}, Generics: []TypeVarId{0}},
		}
	}
	// mkdir / mkdirp : (str -- ) | (path -- )
	for _, name := range []string{"mkdir", "mkdirp"} {
		out[names.Intern(name)] = []QuoteSig{
			{Inputs: []TypeId{TidStr}},
			{Inputs: []TypeId{TidPath}},
		}
	}
	// cd : (str -- ) | (path -- )
	out[names.Intern("cd")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidPath}},
	}
	// tempFile, tempDir : ( -- path )
	for _, name := range []string{"tempFile", "tempDir"} {
		out[names.Intern(name)] = []QuoteSig{{Outputs: []TypeId{TidPath}}}
	}
	// tempFileExt : (str -- path)
	out[names.Intern("tempFileExt")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{TidPath},
	}}
	// strEscape : (str -- str)
	out[names.Intern("strEscape")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{TidStr},
	}}
	// writeFile / appendFile : (content path -- )
	// Stack order: content (str|bytes) below, path (str|path) on top.
	// Runtime pops top → path, then content (Pop2).
	for _, name := range []string{"writeFile", "appendFile"} {
		out[names.Intern(name)] = []QuoteSig{
			{Inputs: []TypeId{TidStr, TidStr}},
			{Inputs: []TypeId{TidStr, TidPath}},
			{Inputs: []TypeId{TidBytes, TidStr}},
			{Inputs: []TypeId{TidBytes, TidPath}},
		}
	}
	// endsWith / startsWith : (str str -- bool)
	for _, name := range []string{"endsWith", "startsWith"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidStr, TidStr},
			Outputs: []TypeId{TidBool},
		}}
	}
	// uniq : ([T] -- [T])
	{
		t := arena.MakeVar(0)
		out[names.Intern("uniq")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  []TypeId{arena.MakeList(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// pop : ([T] -- Maybe[T])  — destructive pop, empty list -> none
	{
		t := arena.MakeVar(0)
		out[names.Intern("pop")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  []TypeId{arena.MakeMaybe(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// utf8Str : (bytes -- str)
	out[names.Intern("utf8Str")] = []QuoteSig{{
		Inputs:  []TypeId{TidBytes},
		Outputs: []TypeId{TidStr},
	}}
	// utf8Bytes : (str -- bytes)
	out[names.Intern("utf8Bytes")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{TidBytes},
	}}
	// return : ( -- )  divergent at runtime, but stack-shape-wise a no-op.
	out[names.Intern("return")] = []QuoteSig{{}}

	// ----- More date/time helpers -----

	// cstToUtc : (datetime -- datetime)
	out[names.Intern("cstToUtc")] = []QuoteSig{{
		Inputs:  []TypeId{TidDateTime},
		Outputs: []TypeId{TidDateTime},
	}}
	// fromOleDate : (int -- datetime) | (float -- datetime)
	out[names.Intern("fromOleDate")] = []QuoteSig{
		{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidDateTime}},
		{Inputs: []TypeId{TidFloat}, Outputs: []TypeId{TidDateTime}},
	}
	// toOleDate : (datetime -- float)
	out[names.Intern("toOleDate")] = []QuoteSig{{
		Inputs:  []TypeId{TidDateTime},
		Outputs: []TypeId{TidFloat},
	}}
	// addDays : (datetime int -- datetime) | (datetime float -- datetime)
	// Stack order matches the runtime: datetime below, count on top.
	out[names.Intern("addDays")] = []QuoteSig{
		{Inputs: []TypeId{TidDateTime, TidInt}, Outputs: []TypeId{TidDateTime}},
		{Inputs: []TypeId{TidDateTime, TidFloat}, Outputs: []TypeId{TidDateTime}},
	}
	// reSplit : (str str -- [str])
	out[names.Intern("reSplit")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr},
		Outputs: []TypeId{arena.MakeList(TidStr)},
	}}
	// rm / rmf : (str -- ) | (path -- )
	for _, name := range []string{"rm", "rmf"} {
		out[names.Intern(name)] = []QuoteSig{
			{Inputs: []TypeId{TidStr}},
			{Inputs: []TypeId{TidPath}},
		}
	}
	// os : ( -- str )  — host OS identifier
	out[names.Intern("os")] = []QuoteSig{{Outputs: []TypeId{TidStr}}}
	// isCmd : (str -- bool) | (path -- bool)
	out[names.Intern("isCmd")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidBool}},
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidBool}},
	}
	// map2 : (Maybe[A] Maybe[B] (A B -- C) -- Maybe[C])
	{
		a := arena.MakeVar(0)
		b := arena.MakeVar(1)
		c2 := arena.MakeVar(2)
		fn := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{a, b},
			Outputs: []TypeId{c2},
		})
		out[names.Intern("map2")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeMaybe(a), arena.MakeMaybe(b), fn},
			Outputs:  []TypeId{arena.MakeMaybe(c2)},
			Generics: []TypeVarId{0, 1, 2},
		}}
	}

	// ----- Slicing / list / regex / string helpers -----

	// take / skip : ([T] int -- [T]) | (str int -- str)
	// Stack order matches the runtime: list/string below, count on top.
	for _, name := range []string{"take", "skip"} {
		t := arena.MakeVar(0)
		out[names.Intern(name)] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), TidInt},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:  []TypeId{TidStr, TidInt},
				Outputs: []TypeId{TidStr},
			},
		}
	}
	// sort / sortV / sortVu : ([T] -- [T])
	for _, name := range []string{"sort", "sortV", "sortVu"} {
		t := arena.MakeVar(0)
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  []TypeId{arena.MakeList(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// extend : ([T] [T] -- [T]) | (Grid Grid -- Grid) | (Grid GridView -- Grid)
	{
		t := arena.MakeVar(0)
		out[names.Intern("extend")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), arena.MakeList(t)},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{Inputs: []TypeId{gridU, gridU}, Outputs: []TypeId{gridU}},
			{Inputs: []TypeId{gridU, gridViewU}, Outputs: []TypeId{gridU}},
			{Inputs: []TypeId{gridViewU, gridU}, Outputs: []TypeId{gridU}},
			{Inputs: []TypeId{gridViewU, gridViewU}, Outputs: []TypeId{gridU}},
		}
	}
	// del : ([T] int -- [T])  — destructive; other forms exist but
	// the list+int shape (list below, index on top) is by far the
	// most common.
	{
		t := arena.MakeVar(0)
		out[names.Intern("del")] = []QuoteSig{{
			Inputs:   []TypeId{arena.MakeList(t), TidInt},
			Outputs:  []TypeId{arena.MakeList(t)},
			Generics: []TypeVarId{0},
		}}
	}
	// reReplace : (str str str -- str)
	out[names.Intern("reReplace")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr, TidStr},
		Outputs: []TypeId{TidStr},
	}}
	// reMatch : (str str -- bool)
	out[names.Intern("reMatch")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr},
		Outputs: []TypeId{TidBool},
	}}
	// reFindAll : (str str -- [[str]])
	out[names.Intern("reFindAll")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr},
		Outputs: []TypeId{arena.MakeList(arena.MakeList(TidStr))},
	}}
	// findReplace : (str str str -- str)  literal find/replace
	out[names.Intern("findReplace")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr, TidStr},
		Outputs: []TypeId{TidStr},
	}}
	// leftPad / rightPad : (str str int -- str)  input pad totalLen
	for _, name := range []string{"leftPad", "rightPad"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidStr, TidStr, TidInt},
			Outputs: []TypeId{TidStr},
		}}
	}
	// index / lastIndexOf : (str str -- int)
	for _, name := range []string{"index", "lastIndexOf"} {
		out[names.Intern(name)] = []QuoteSig{{
			Inputs:  []TypeId{TidStr, TidStr},
			Outputs: []TypeId{TidInt},
		}}
	}
	// hostname : ( -- str )
	out[names.Intern("hostname")] = []QuoteSig{{Outputs: []TypeId{TidStr}}}
	// pwd : ( -- path )
	out[names.Intern("pwd")] = []QuoteSig{{Outputs: []TypeId{TidPath}}}
	// args : ( -- [str] )
	out[names.Intern("args")] = []QuoteSig{{Outputs: []TypeId{arena.MakeList(TidStr)}}}
	// md5 : (str -- str) | (bytes -- str)
	out[names.Intern("md5")] = []QuoteSig{
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidBytes}, Outputs: []TypeId{TidStr}},
	}
	// fileSize : (path|str -- int)
	out[names.Intern("fileSize")] = []QuoteSig{
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidInt}},
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidInt}},
	}
	// fileExists : (path|str -- bool)
	out[names.Intern("fileExists")] = []QuoteSig{
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidBool}},
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidBool}},
	}
	// fromUnixTime : (int -- datetime)  seconds since epoch
	out[names.Intern("fromUnixTime")] = []QuoteSig{{
		Inputs:  []TypeId{TidInt},
		Outputs: []TypeId{TidDateTime},
	}}
	// utcToCst : (datetime -- datetime)
	out[names.Intern("utcToCst")] = []QuoteSig{{
		Inputs:  []TypeId{TidDateTime},
		Outputs: []TypeId{TidDateTime},
	}}
	// reFindAllIndex : (str str -- [[int]])  match → [start, end] pairs
	out[names.Intern("reFindAllIndex")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr, TidStr},
		Outputs: []TypeId{arena.MakeList(arena.MakeList(TidInt))},
	}}
	// parseLinkHeader : (str -- [{str: str}])
	out[names.Intern("parseLinkHeader")] = []QuoteSig{{
		Inputs:  []TypeId{TidStr},
		Outputs: []TypeId{arena.MakeList(arena.MakeDict(TidStr, TidStr))},
	}}
	// setAt : ([T] int T -- [T]) | (str int str -- str)  positional set
	{
		t := arena.MakeVar(0)
		out[names.Intern("setAt")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), TidInt, t},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:  []TypeId{TidStr, TidInt, TidStr},
				Outputs: []TypeId{TidStr},
			},
		}
	}
	// insert : ([T] int T -- [T]) | (str int str -- str)  positional insert
	{
		t := arena.MakeVar(0)
		out[names.Intern("insert")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), TidInt, t},
				Outputs:  []TypeId{arena.MakeList(t)},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:  []TypeId{TidStr, TidInt, TidStr},
				Outputs: []TypeId{TidStr},
			},
		}
	}

	// ----- Grid joins -----
	// outerJoin / leftJoin / innerJoin / rightJoin :
	//   (Grid|GridView Grid|GridView (GridRow -- T) (GridRow -- T) -- Grid)
	{
		k := arena.MakeVar(0)
		keyQuote := arena.MakeQuote(QuoteSig{
			Inputs:  []TypeId{gridRowU},
			Outputs: []TypeId{k},
		})
		joinSig := func() QuoteSig {
			return QuoteSig{
				Inputs:   []TypeId{gridU, gridU, keyQuote, keyQuote},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{0},
			}
		}
		for _, name := range []string{"outerJoin", "leftJoin", "innerJoin", "rightJoin"} {
			out[names.Intern(name)] = []QuoteSig{joinSig()}
		}
		// `join` already has a `[str] str -- str` overload; add the
		// grid form so `g1 g2 (:k?) (:k?) join` type-checks.
		out[names.Intern("join")] = append(out[names.Intern("join")], joinSig())
	}

	// ----- Zip ops -----
	// All zip ops accept str|path for path-like args. Modeled with
	// explicit overloads.

	// zipRead : (path|str path|str -- Maybe[bytes])
	out[names.Intern("zipRead")] = []QuoteSig{
		{Inputs: []TypeId{TidStr, TidStr}, Outputs: []TypeId{arena.MakeMaybe(TidBytes)}},
		{Inputs: []TypeId{TidPath, TidStr}, Outputs: []TypeId{arena.MakeMaybe(TidBytes)}},
		{Inputs: []TypeId{TidStr, TidPath}, Outputs: []TypeId{arena.MakeMaybe(TidBytes)}},
		{Inputs: []TypeId{TidPath, TidPath}, Outputs: []TypeId{arena.MakeMaybe(TidBytes)}},
	}
	// zipDirInc / zipDirExc : (path|str path|str -- )
	for _, name := range []string{"zipDirInc", "zipDirExc"} {
		out[names.Intern(name)] = []QuoteSig{
			{Inputs: []TypeId{TidStr, TidStr}},
			{Inputs: []TypeId{TidPath, TidStr}},
			{Inputs: []TypeId{TidStr, TidPath}},
			{Inputs: []TypeId{TidPath, TidPath}},
		}
	}
	// zipPack : (path|str [{str: V}] -- )
	{
		v := arena.MakeVar(0)
		entries := arena.MakeList(arena.MakeDict(TidStr, v))
		out[names.Intern("zipPack")] = []QuoteSig{
			{
				Inputs:   []TypeId{TidStr, entries},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{TidPath, arena.MakeList(arena.MakeDict(TidStr, arena.MakeVar(0)))},
				Generics: []TypeVarId{0},
			},
		}
	}
	// zipExtract : (path|str path|str {str: V} -- )
	{
		mk := func(a, b TypeId) QuoteSig {
			return QuoteSig{
				Inputs:   []TypeId{a, b, arena.MakeDict(TidStr, arena.MakeVar(0))},
				Generics: []TypeVarId{0},
			}
		}
		out[names.Intern("zipExtract")] = []QuoteSig{
			mk(TidStr, TidStr),
			mk(TidStr, TidPath),
			mk(TidPath, TidStr),
			mk(TidPath, TidPath),
		}
	}
	// zipExtractEntry : (path|str str path|str {str: V} -- )
	{
		v := arena.MakeVar(0)
		options := arena.MakeDict(TidStr, v)
		out[names.Intern("zipExtractEntry")] = []QuoteSig{
			{
				Inputs:   []TypeId{TidStr, TidStr, TidStr, options},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{TidPath, TidStr, TidPath, arena.MakeDict(TidStr, arena.MakeVar(0))},
				Generics: []TypeVarId{0},
			},
		}
	}
	// zipList : (path|str -- [{str: V}])
	{
		v := arena.MakeVar(0)
		out[names.Intern("zipList")] = []QuoteSig{
			{
				Inputs:   []TypeId{TidStr},
				Outputs:  []TypeId{arena.MakeList(arena.MakeDict(TidStr, v))},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{TidPath},
				Outputs:  []TypeId{arena.MakeList(arena.MakeDict(TidStr, arena.MakeVar(0)))},
				Generics: []TypeVarId{0},
			},
		}
	}

	// groupBy :
	//   ([T] (T -- str) -- {str: [T]})            list form
	//   (Grid|GridView [str] [{str: V}] -- Grid)  grid form
	{
		t := arena.MakeVar(0)
		fn := arena.MakeQuote(QuoteSig{Inputs: []TypeId{t}, Outputs: []TypeId{TidStr}})
		v := arena.MakeVar(1)
		aggList := arena.MakeList(arena.MakeDict(TidStr, v))
		out[names.Intern("groupBy")] = []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeList(t), fn},
				Outputs:  []TypeId{arena.MakeDict(TidStr, arena.MakeList(t))},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{gridU, arena.MakeList(TidStr), aggList},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{1},
			},
			{
				Inputs:   []TypeId{gridViewU, arena.MakeList(TidStr), aggList},
				Outputs:  []TypeId{gridU},
				Generics: []TypeVarId{1},
			},
		}
	}

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

	// Capture markers `*` / `*b` / `^` / `^b` are postfix list/pipe
	// modifiers. `*` doubles as multiplication when the top isn't a
	// list. We model this with overloads: arithmetic forms first,
	// then a list-passthrough form for the capture-marker role.
	captureT := arena.MakeVar(0)
	captureList := arena.MakeList(captureT)
	starOverloads := []QuoteSig{
		intIntInt,
		floatFloatFloat,
		{
			Inputs:   []TypeId{captureList},
			Outputs:  []TypeId{captureList},
			Generics: []TypeVarId{0},
		},
	}
	listCaptureOnly := []QuoteSig{
		{
			Inputs:   []TypeId{arena.MakeList(arena.MakeVar(0))},
			Outputs:  []TypeId{arena.MakeList(arena.MakeVar(0))},
			Generics: []TypeVarId{0},
		},
	}

	// `<` / `>` double as redirection markers when applied to a
	// list or quote: `[cmd] "file" >` sets stdout, `[cmd] "in" <`
	// pipes input. The output is the same list/quote (with
	// redirection metadata set).
	//
	// Quote-on-bottom (e.g. `(...) "f" 2>`) is not yet covered.
	// Doing so would need either a "wildcard quote" type or a
	// special-case dispatch — both touch the type machinery in
	// non-trivial ways, so it stays a known gap for now.
	redirT := arena.MakeVar(0)
	redirSigs := []QuoteSig{
		intIntBool,
		floatFloatBool,
		{
			Inputs:   []TypeId{arena.MakeList(redirT), TidStr},
			Outputs:  []TypeId{arena.MakeList(redirT)},
			Generics: []TypeVarId{0},
		},
		{
			Inputs:   []TypeId{arena.MakeList(arena.MakeVar(0)), TidPath},
			Outputs:  []TypeId{arena.MakeList(arena.MakeVar(0))},
			Generics: []TypeVarId{0},
		},
	}

	// `+` has additional non-arithmetic overloads. Strings concat;
	// lists concat; paths concat (as strings, not filepath.Join);
	// grids concat (row union). Built separately so the arithmetic
	// `+` keeps its int/float-only invariant for `-` / `*` / `**`.
	plusListT := arena.MakeVar(0)
	plusOverloads := []QuoteSig{
		intIntInt,
		floatFloatFloat,
		{Inputs: []TypeId{TidStr, TidStr}, Outputs: []TypeId{TidStr}},
		{
			Inputs:   []TypeId{arena.MakeList(plusListT), arena.MakeList(plusListT)},
			Outputs:  []TypeId{arena.MakeList(plusListT)},
			Generics: []TypeVarId{0},
		},
		{Inputs: []TypeId{TidPath, TidPath}, Outputs: []TypeId{TidPath}},
		{Inputs: []TypeId{arena.MakeGrid(0), arena.MakeGrid(0)}, Outputs: []TypeId{arena.MakeGrid(0)}},
		{Inputs: []TypeId{arena.MakeGridView(0), arena.MakeGridView(0)}, Outputs: []TypeId{arena.MakeGrid(0)}},
		{Inputs: []TypeId{arena.MakeGrid(0), arena.MakeGridView(0)}, Outputs: []TypeId{arena.MakeGrid(0)}},
		{Inputs: []TypeId{arena.MakeGridView(0), arena.MakeGrid(0)}, Outputs: []TypeId{arena.MakeGrid(0)}},
	}

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

	// QUESTION (`?`): three roles depending on what's on the stack.
	//   Maybe[T] -- T   : unwrap (None aborts at runtime)
	//   [T] -- int      : run process list, push exit code
	// EXECUTE/BANG share the process-list role but produce no output
	// (BANG additionally aborts on nonzero exit at runtime).
	questionSigs := func() []QuoteSig {
		t := arena.MakeVar(0)
		listT := arena.MakeList(arena.MakeVar(0))
		return []QuoteSig{
			{
				Inputs:   []TypeId{arena.MakeMaybe(t)},
				Outputs:  []TypeId{t},
				Generics: []TypeVarId{0},
			},
			{
				Inputs:   []TypeId{listT},
				Outputs:  []TypeId{TidInt},
				Generics: []TypeVarId{0},
			},
		}
	}
	// EXECUTE / BANG: run a list as a subprocess. Output behavior
	// depends on stdout/stderr capture markers we don't yet model;
	// the no-capture form produces nothing, which is the most
	// common case (`[cmd args] ;`).
	execSig := func() QuoteSig {
		t := arena.MakeVar(0)
		return QuoteSig{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  nil,
			Generics: []TypeVarId{0},
		}
	}
	// PIPE (`|`): the runtime converts a list into a pipeline value.
	// We don't model `Pipe` distinctly from a list yet — typing PIPE
	// as a list-to-list passthrough lets the trailing `; / ! / ?`
	// see something it accepts.
	pipeSig := func() QuoteSig {
		t := arena.MakeVar(0)
		return QuoteSig{
			Inputs:   []TypeId{arena.MakeList(t)},
			Outputs:  []TypeId{arena.MakeList(t)},
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
		PLUS:               plusOverloads,
		MINUS:              arithmetic,
		ASTERISK:           starOverloads,
		ASTERISKBINARY:     starOverloads,
		CARET:              listCaptureOnly,
		CARETBINARY:        listCaptureOnly,
		STDOUTLINES:        listCaptureOnly,
		STDOUTSTRIPPED:     listCaptureOnly,
		STDOUTCOMPLETE:     listCaptureOnly,
		LESSTHAN:           redirSigs,
		GREATERTHAN:        redirSigs,
		STDERRREDIRECT:     redirSigs,
		STDERRAPPEND:       redirSigs,
		STDOUTANDSTDERRREDIRECT: redirSigs,
		STDOUTANDSTDERRAPPEND:   redirSigs,
		INPLACEREDIRECT:    redirSigs,
		STDAPPEND:          redirSigs,
		LESSTHANOREQUAL:    comparison,
		GREATERTHANOREQUAL: comparison,
		STR:                {strConv},
		NOT:                {{Inputs: []TypeId{TidBool}, Outputs: []TypeId{TidBool}}},
		EQUALS:             {eqSig},
		NOTEQUAL:           {eqSig},
		QUESTION:           questionSigs(),
		IFF:                iffSigs(),
		LOOP:               {loopSig},
		BREAK:              {noOp},
		CONTINUE:           {noOp},
		EXECUTE:            {execSig()},
		BANG:               {execSig()},
		PIPE:               {pipeSig()},
	}
}
