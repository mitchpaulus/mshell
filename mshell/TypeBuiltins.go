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
// Signatures are written as strings in the same `(inputs -- outputs)`
// notation users write in def signatures and parsed through the shared
// type-expression parser. Single lowercase letters are generics; any other
// unrecognized type name panics at construction time, so typos can't
// silently generalize. Re-registering a name also panics — a second
// assignment would silently overwrite the first overload set.
//
// Overload arms exist only where the arms' OUTPUTS differ. Arms that
// produce the same outputs and differ in input flavor are written as a
// single sig with union inputs (e.g. `(str | path -- str)` instead of two
// arms): a single candidate resolves deterministically, so the branching
// walker has fewer fan-out sites and quote inference fewer alternatives.
//
// Things the sig grammar cannot express stay hand-built in Go below:
// command/capture types (`*`, `^`, `?`, `;`, `!`), divergence (`exit`),
// and quote arms with their own locally-scoped generics (groupBy's agg).
//
// As the table grows, more programs become checkable under --check-types.
// Keep every sig in lockstep with the corresponding Evaluator.go switch —
// a permissive sig silently waves through programs that crash at runtime.

import (
	"slices"
	"sync"
)

// sigRegistry collects builtin signatures, parsing string sigs through the
// type-expression parser and rejecting duplicate registrations.
type sigRegistry struct {
	checker *Checker
	out     map[NameId][]QuoteSig
}

func newSigRegistry(arena *TypeArena, names *NameTable) *sigRegistry {
	return &sigRegistry{
		checker: &Checker{arena: arena, names: names},
		out:     make(map[NameId][]QuoteSig, 256),
	}
}

// sigASTCache memoizes the parsed AST per signature string. Sig strings
// are compile-time constants and the AST is arena-independent and
// read-only during resolution, so one parse serves every checker
// construction — table builds after the first (e.g. per LSP diagnostics
// pass) skip lexing/parsing entirely.
var sigASTCache sync.Map // string -> sigAST

type sigAST struct {
	inputs  []MShellParseItem
	outputs []MShellParseItem
}

// parseBuiltinSig parses one `(inputs -- outputs)` signature string into a
// QuoteSig. Generic names must be single lowercase letters; anything else
// unknown to the resolver is a typo and panics.
func parseBuiltinSig(c *Checker, src string) QuoteSig {
	var ast sigAST
	if cached, ok := sigASTCache.Load(src); ok {
		ast = cached.(sigAST)
	} else {
		lex := NewLexer(src, nil)
		parser := NewMShellParser(lex)
		parser.NextToken()
		inputs, outputs, err := parser.parseDefSignature()
		if err != nil {
			panic("builtin sig " + src + ": " + err.Error())
		}
		ast = sigAST{inputs: inputs, outputs: outputs}
		sigASTCache.Store(src, ast)
	}
	ctx := &typeResolveCtx{}
	errStart := len(c.errors)
	ins := c.resolveSigItems(ast.inputs, ctx)
	outs := c.resolveSigItems(ast.outputs, ctx)
	if len(c.errors) > errStart {
		panic("builtin sig " + src + ": " + c.errors[errStart].Format(c.arena, c.names))
	}
	gens := make([]TypeVarId, 0, len(ctx.generics))
	for name, v := range ctx.generics {
		if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
			panic("builtin sig " + src + ": unknown type '" + name + "' (generics must be single lowercase letters)")
		}
		gens = append(gens, v)
	}
	slices.Sort(gens)
	if len(gens) == 0 {
		gens = nil
	}
	return QuoteSig{Inputs: ins, Outputs: outs, Generics: gens}
}

// parseBuiltinSigs parses a list of signature strings, preserving order
// (overload candidate order is meaningful for inference tie-breaks).
func parseBuiltinSigs(c *Checker, srcs ...string) []QuoteSig {
	sigs := make([]QuoteSig, len(srcs))
	for i, src := range srcs {
		sigs[i] = parseBuiltinSig(c, src)
	}
	return sigs
}

// reg registers name's full overload set from signature strings.
// Registering the same name twice panics.
func (r *sigRegistry) reg(name string, sigs ...string) {
	id := r.checker.names.Intern(name)
	if _, dup := r.out[id]; dup {
		panic("duplicate builtin registration: " + name)
	}
	r.out[id] = parseBuiltinSigs(r.checker, sigs...)
}

// add appends overload arms to an already-registered name — used where one
// word spans categories (the dict/grid/Maybe arms of `map`, the grid arm
// of `join`). The name must already be registered; a typo here would
// otherwise silently mint a new builtin.
func (r *sigRegistry) add(name string, sigs ...string) {
	id := r.checker.names.Intern(name)
	if _, ok := r.out[id]; !ok {
		panic("add to unregistered builtin: " + name)
	}
	r.out[id] = append(r.out[id], parseBuiltinSigs(r.checker, sigs...)...)
}

// regGo registers hand-built sigs for shapes the grammar can't express.
func (r *sigRegistry) regGo(name string, sigs ...QuoteSig) {
	id := r.checker.names.Intern(name)
	if _, dup := r.out[id]; dup {
		panic("duplicate builtin registration: " + name)
	}
	r.out[id] = sigs
}

// addGo appends hand-built sigs to an already-registered name.
func (r *sigRegistry) addGo(name string, sigs ...QuoteSig) {
	id := r.checker.names.Intern(name)
	if _, ok := r.out[id]; !ok {
		panic("addGo to unregistered builtin: " + name)
	}
	r.out[id] = append(r.out[id], sigs...)
}

// builtinSigsByName registers builtins that arrive as LITERAL tokens.
func builtinSigsByName(arena *TypeArena, names *NameTable) map[NameId][]QuoteSig {
	r := newSigRegistry(arena, names)

	// ----- Maybe constructors -----
	r.reg("just", "(t -- Maybe[t])")
	// `none` is always Nothing, so its payload is uninhabited: Maybe[bottom].
	// This stays compatible with any Maybe[T] context (bottom unifies with
	// anything, and a declared Maybe[T] boundary launders it back to T), but
	// lets `?` recognize that unwrapping a bare `none` always fails.
	// `bottom` has no sig-string spelling, so build the sig in Go.
	r.regGo("none", QuoteSig{Outputs: []TypeId{arena.MakeMaybe(TidBottom)}})

	// ----- JSON null -----
	r.reg("null", "( -- null)")

	// ----- Stack manipulation (polymorphic) -----
	r.reg("dup", "(t -- t t)")
	r.reg("drop", "(t -- )")
	r.reg("swap", "(t u -- u t)")
	r.reg("over", "(t u -- t u t)")
	r.reg("rot", "(t u v -- u v t)")
	r.reg("nip", "(t u -- u)")
	r.reg("tuck", "(t u -- u t u)")

	// ----- I/O (consume one writable value, no output) -----
	//
	// Runtime (Evaluator.go ~line 5759) only stringifies str, int, and
	// binary; everything else fails with "Cannot write a X". Binary is
	// further restricted to the no-newline variants because writing a
	// trailing '\n' after raw bytes is rarely what callers want. Keep
	// these sigs in lockstep with that switch — otherwise the checker
	// silently waves through programs that crash at runtime (e.g.
	// `2026-01-01 wl` on a datetime). Use `str` to coerce other types
	// first (`1.5 str wl`).
	r.reg("wl", "(str | int -- )")  // write line
	r.reg("wle", "(str | int -- )") // write line stderr
	r.reg("w", "(str | int | bytes -- )")
	r.reg("we", "(str | int | bytes -- )")
	r.reg("wln", "( -- )") // write just a newline
	r.reg("stack", "( -- )")
	r.reg("defs", "( -- )")
	r.reg("env", "( -- )")
	r.reg("completionDefs", "( -- {[( -- t)]})")
	// ----- Boolean ops -----
	// `not` lexes as NOT (token type), not LITERAL — see byToken table.

	// `and`/`or` take a bool plus either a bool or, for the
	// short-circuit form, a quote that yields a bool.
	r.reg("and", "(bool bool | ( -- bool) -- bool)")
	r.reg("or", "(bool bool | ( -- bool) -- bool)")

	// ----- Arithmetic helpers -----
	r.reg("abs", "(int -- int)", "(float -- float)")
	// inc : increment an integer (int only at runtime)
	r.reg("inc", "(int -- int)")
	// sleep : sleep N seconds
	r.reg("sleep", "(int | float -- )")
	// Trig / log / sqrt — runtime requires float input (mshell rejects
	// implicit int->float coercion). std.msh defines `cos`, `tan`,
	// `ln2`, `ln10` on top of these primitives.
	for _, name := range []string{"sin", "arctan", "ln", "sqrt"} {
		r.reg(name, "(float -- float)")
	}
	// pow : base, exponent.
	r.reg("pow", "(float float -- float)")
	for _, name := range []string{"floor", "ceil", "round"} {
		r.reg(name, "(int | float -- int)")
	}
	for _, name := range []string{"random", "randomFixed"} {
		r.reg(name, "( -- float)")
	}
	r.reg("sum", "([float] -- float)", "([int] -- int)")
	for _, name := range []string{"max", "min"} {
		r.reg(name, "([float] -- float)", "([int] -- int)", "([datetime] -- datetime)")
	}
	// Mixed int/float pairs are rejected at runtime; no cross overloads.
	for _, name := range []string{"max2", "min2"} {
		r.reg(name, "(int int -- int)", "(float float -- float)")
	}

	// ----- Numeric conversions -----

	// The str overload is listed first so that under inferring-mode
	// overload resolution (when the input type is unknown, e.g. inside
	// `(toFloat?)`), the str-with-Maybe variant wins the tie. That
	// matches the typical use of `toFloat` paired with `?`.
	r.reg("toFloat", "(str -- Maybe[float])", "(int -- float)", "(float -- float)")
	// toFixed : format value with N decimals
	r.reg("toFixed", "(int int -- str)", "(float int -- str)")
	// str-first ordering for the same reason as toFloat above.
	r.reg("toInt", "(str -- Maybe[int])", "(float -- int)", "(int -- int)")
	// Format an int in an arbitrary base (2-36) as bare digits; parse a string
	// in a given base back to Maybe[int]. The int carries no base of its own.
	r.reg("toBase", "(int int -- str)")
	r.reg("fromBase", "(str int -- Maybe[int])")

	// ----- Path / DateTime / File ops -----

	r.reg("toPath", "(str | path -- path)")
	r.reg("toDt", "(str -- Maybe[datetime])", "(datetime -- datetime)")
	r.reg("now", "( -- datetime)")
	r.reg("stdin", "( -- str)")
	r.reg("runtime", "( -- str)")
	r.reg("nullDevice", "( -- path)")

	// date : strip time-of-day to midnight
	r.reg("date", "(datetime -- datetime)")
	for _, name := range []string{"day", "month", "year", "hour", "minute", "second", "weekday", "dow"} {
		r.reg(name, "(datetime -- int)")
	}
	for _, name := range []string{"isWeekend", "isWeekday"} {
		r.reg(name, "(datetime -- bool)")
	}
	for _, name := range []string{"toUnixTime", "toUnixTimeMilli", "toUnixTimeMicro", "toUnixTimeNano"} {
		r.reg(name, "(datetime -- int)")
	}
	r.reg("dateFmt", "(datetime str -- str)")

	r.reg("readFile", "(str | path -- str)")
	r.reg("glob", "(str | path -- [path])")
	r.reg("prompt", "(str | path -- str)")
	r.reg("read", "( -- str bool)")
	// exit : (int -- Bottom)  — divergent; Bottom has no sig syntax.
	r.regGo("exit", QuoteSig{Inputs: []TypeId{TidInt}, Outputs: []TypeId{TidBottom}})
	r.reg("readFileBytes", "(str | path -- bytes)")
	for _, name := range []string{"files", "dirs"} {
		r.reg(name, "( -- [path])")
	}
	r.reg("lsDir", "(str | path -- [path])")
	for _, name := range []string{"isDir", "isFile"} {
		r.reg(name, "(str | path -- bool)")
	}
	for _, name := range []string{"basename", "dirname", "stem"} {
		r.reg(name, "(str | path -- path)")
	}
	r.reg("ext", "(str | path -- str)")
	r.reg("absPath", "(str | path -- path)")
	for _, name := range []string{"cp", "mv", "hardLink"} {
		r.reg(name, "(str | path str | path -- )")
	}

	// ----- List ops -----

	// Grid/GridView return rows; GridRow returns columns.
	r.reg("len",
		"([t] -- int)",
		"({v} -- int)",
		"(str | path | Grid | GridView | GridRow -- int)",
	)
	r.reg("append",
		"([t] t -- [t])",
		"([t] u -- [t | u])",
		"(t [t] -- [t])",
		"(t [u] -- [t | u])",
	)
	r.reg("nth",
		"([t] int -- t)",
		"(int [t] -- t)",
		"(str int -- str)",
		"(int str -- str)",
	)
	r.reg("foldl", "((a t -- a) a [t] -- a)")
	r.reg("reverse",
		"([t] -- [t])",
		"(str -- str)",
		"(Grid | GridView -- GridView)",
	)

	// ----- Higher-order list ops -----

	r.reg("map", "([t] (t -- u) -- [u])")
	// any / all : list-of-T with a predicate. Matches the std.msh sig
	// `([T] (T -- bool) -- bool)`. For a bool list, pass `(id)` as the
	// predicate rather than an empty quote.
	for _, name := range []string{"any", "all"} {
		r.reg(name, "([t] (t -- bool) -- bool)")
	}
	// The Grid|GridView predicate uses `:col?`-style getters against the
	// implicit row.
	r.reg("filter",
		"([t] (t -- bool) -- [t])",
		"({v} (v -- bool) -- {v})",
		"(Grid | GridView (GridRow -- bool) -- GridView)",
	)
	r.reg("each", "([t] (t -- ) -- )")
	// The key-extractor must produce a str since dict keys are always str.
	r.reg("listToDict", "([t] (t -- str) (t -- v) -- {v})")

	// ----- Arithmetic LITERAL ops -----

	// `/` is LITERAL (no dedicated token): arithmetic division on
	// int/float, filepath.Join on paths.
	r.reg("/",
		"(int int -- int)",
		"(float float -- float)",
		"(path path -- path)",
	)
	r.reg("mod", "(int int -- int)", "(float float -- float)")

	// ----- Dict ops -----

	r.reg("keys", "({v} -- [str])")
	r.reg("values", "({v} -- [v])")
	r.reg("set", "({v} str v -- {v})")
	r.reg("setd", "({v} str v -- )") // drop variant
	r.reg("get",
		"({v} str -- Maybe[v])",
		"(GridRow str -- Maybe[t])",
		"(Grid | GridView str -- Maybe[[t]])",
	)
	r.reg("getDef", "({v} str v -- v)") // get with default
	r.add("map", "({v} (v -- u) -- {u})")
	// keyValues: each pair is a shape with fixed fields 'k' (the key) and
	// 'v' (the value), so the heterogeneous key/value types survive the
	// trip through the list. Callers use `:k` and `:v` getters to recover
	// the two halves.
	r.reg("keyValues", "({v} -- [{k: str, v: v}])")
	// Stack order matches the runtime: the haystack (dict or string) is
	// below, the needle (key or substring) on top.
	r.reg("in",
		"({v} str -- bool)",
		"(str str -- bool)",
	)

	// ----- String ops -----

	r.reg("join", "([str] str -- str)")
	r.reg("wsplit", "(str -- [str])")
	r.reg("split", "(str str -- [str])")
	r.reg("lines", "(str -- [str])")
	for _, name := range []string{"trim", "trimStart", "trimEnd", "upper", "lower", "title"} {
		r.reg(name, "(str -- str)")
	}

	// ----- Numeric / formatting ops -----

	// A value typed `int | float` (e.g. the joined result of a match/if
	// whose arms produce different numeric types) is accepted via the
	// overload-resolution union distribution in resolveAndApply, which
	// checks both the int and float members against these overloads.
	// numFmt options are all optional; an empty `{}` is valid. Listing them
	// as optional fields type-checks each when present (e.g. rejects
	// `{ 'decimals': "two" }`) while width subtyping still tolerates unknown
	// keys the runtime ignores.
	// The runtime accepts both `sigFigs` and the lowercase `sigfigs` alias.
	numFmtOpts := "{decimals?: int, sigFigs?: int, sigfigs?: int, preserveInt?: bool, decimalPoint?: str, thousandsSep?: str, grouping?: [int]}"
	r.reg("numFmt", "(int "+numFmtOpts+" -- str)", "(float "+numFmtOpts+" -- str)")
	r.reg("countSubStr", "(str str -- int)")
	r.reg("toJson", "(t -- str)") // generic conversion to JSON

	// ----- Grid ops -----
	//
	// In V1 we don't track grid schemas through these operations, so
	// every grid sig uses the unknown-schema variants. Element types
	// extracted from columns are modeled as fresh generics — overload
	// dispatch and the downstream walk treat the result as polymorphic.

	r.reg("gridRows", "(Grid | GridView -- int)")
	r.reg("gridCols", "(Grid | GridView -- [str])")
	r.reg("gridMeta", "(Grid | GridView -- Maybe[{v}])")
	r.reg("gridColMeta", "(Grid | GridView str -- Maybe[{v}])")
	r.reg("gridCol", "(Grid | GridView str -- [t])")
	r.reg("select", "(Grid | GridView [str] -- Grid)")
	r.reg("exclude", "(Grid | GridView [str] -- Grid)")
	r.reg("gridRenameCol", "(Grid str str -- Grid)")
	r.reg("gridCompact", "(Grid | GridView -- Grid)")
	r.reg("derive", "(Grid | GridView str {v} (GridRow -- t) -- Grid)")
	r.reg("gridValues", "(Grid | GridView -- [[t]])")
	r.reg("toGrid", "([t] -- Grid)")
	r.reg("toDict", "(GridRow -- {v})")
	r.add("map", "(Grid | GridView (GridRow -- {v}) -- Grid)")
	r.add("each", "(Grid | GridView (GridRow -- ) -- )")
	r.reg("sortBy", "(Grid | GridView str | [str] -- GridView)")
	r.reg("gridSetCell", "(Grid str int t -- Grid)")
	r.reg("gridAddCol", "(Grid str [t] -- Grid)", "(Grid str t -- Grid)")
	r.reg("gridRemoveCol", "(Grid str -- Grid)")
	r.reg("updateCol", "(Grid | GridView str (t -- u) -- Grid)")
	r.reg("sortByCmp",
		"([t] (t t -- int) -- [t])",
		"(Grid (GridRow GridRow -- int) -- Grid)",
		"(GridView (GridRow GridRow -- int) -- GridView)",
	)
	r.reg("parseCsv", "(str | path -- [[str]])")
	r.reg("parseJson", "(str | path | bytes -- t)")
	// parseExcel: a cell is a string, a float (numbers and dates), a
	// bool, or a None Maybe (error cells like #DIV/0!). The Maybe carries
	// a free inner type because an error cell is always None, mirroring
	// how `none` itself is typed.
	r.reg("parseExcel",
		"(path | bytes -- [{name: str, data: [[str | float | bool | Maybe[v]]], hidden: bool, visibility: str}])",
	)
	for _, name := range []string{"mkdir", "mkdirp"} {
		r.reg(name, "(str | path -- )")
	}
	r.reg("cd", "(str | path -- )")
	// unsetenv : remove an environment variable by name
	r.reg("unsetenv", "(str -- )")
	// cdh / cdp : interactive directory history / pop navigation
	for _, name := range []string{"cdh", "cdp"} {
		r.reg(name, "( -- )")
	}
	for _, name := range []string{"tempFile", "tempDir"} {
		r.reg(name, "( -- path)")
	}
	r.reg("tempFileExt", "(str -- path)")
	r.reg("strEscape", "(str -- str)")
	// writeFile / appendFile : stack order is content (str|bytes) below,
	// path (str|path) on top. Runtime pops top → path, then content.
	for _, name := range []string{"writeFile", "appendFile"} {
		r.reg(name, "(str | bytes str | path -- )")
	}
	for _, name := range []string{"endsWith", "startsWith"} {
		r.reg(name, "(str str -- bool)")
	}
	r.reg("uniq", "([t] -- [t])")
	// pop : destructive pop, empty list -> none
	r.reg("pop", "([t] -- Maybe[t])")
	r.reg("utf8Str", "(bytes -- str)")
	r.reg("utf8Bytes", "(str -- bytes)")
	// return : divergent at runtime, but stack-shape-wise a no-op.
	r.reg("return", "( -- )")

	// ----- More date/time helpers -----

	r.reg("cstToUtc", "(datetime -- datetime)")
	r.reg("fromOleDate", "(int | float -- datetime)")
	r.reg("toOleDate", "(datetime -- float)")
	// Stack order matches the runtime: datetime below, count on top.
	r.reg("addDays", "(datetime int | float -- datetime)")
	r.reg("reSplit", "(str str -- [str])")
	for _, name := range []string{"rm", "rmf"} {
		r.reg(name, "(str | path -- )")
	}
	r.reg("isCmd", "(str | path -- bool)")
	r.reg("map2", "(Maybe[a] Maybe[b] (a b -- c) -- Maybe[c])")
	// bind : monadic bind
	r.reg("bind", "(Maybe[a] (a -- Maybe[b]) -- Maybe[b])")
	r.reg("maybe", "(Maybe[t] u -- t | u)")

	// ----- Slicing / list / regex / string helpers -----

	// Stack order matches the runtime: list/string below, count on top.
	for _, name := range []string{"take", "skip"} {
		r.reg(name, "([t] int -- [t])", "(str int -- str)")
	}
	for _, name := range []string{"sort", "sortV", "sortVu"} {
		r.reg(name, "([t] -- [t])")
	}
	r.reg("extend",
		"([t] [t] -- [t])",
		"(Grid | GridView Grid | GridView -- Grid)",
	)
	r.reg("del", "([t] int -- [t])", "(int [t] -- [t])")
	r.reg("reReplace", "(str str str -- str)")
	r.reg("reMatch", "(str str -- bool)")
	r.reg("reFindAll", "(str str -- [[str]])")
	// findReplace : literal find/replace
	r.reg("findReplace", "(str str str -- str)")
	// leftPad / rightPad : input pad totalLen
	for _, name := range []string{"leftPad", "rightPad"} {
		r.reg(name, "(str str int -- str)")
	}
	for _, name := range []string{"index", "lastIndexOf"} {
		r.reg(name, "(str str -- int)")
	}
	r.reg("hostname", "( -- str)")
	r.reg("uuid", "( -- str)")
	r.reg("uuid7", "( -- str)")
	r.reg("pwd", "( -- path)")
	r.reg("args", "( -- [str])")
	r.reg("md5", "(str | path | bytes -- str)")
	r.reg("sha256sum", "(path -- str)")
	r.reg("fileSize", "(path | str -- Maybe[int])")
	r.reg("modTime", "(path | str -- Maybe[datetime])")
	r.reg("fileExists", "(path | str -- bool)")
	// seconds (milli/micro/nano) since epoch
	for _, name := range []string{"fromUnixTime", "fromUnixTimeMilli", "fromUnixTimeMicro", "fromUnixTimeNano"} {
		r.reg(name, "(int -- datetime)")
	}
	r.reg("utcToCst", "(datetime -- datetime)")
	// reFindAllIndex : match → [start, end] pairs
	r.reg("reFindAllIndex", "(str str -- [[int]])")
	r.reg("parseLinkHeader", "(str -- [{v}])")
	r.reg("parseHtml", "(str | path -- {v})")
	// httpGet / httpPost: the request dict requires a stringable `url`
	// plus optional `timeout` (int), `followRedirects` (bool), `headers`
	// ({str: str}), and `body`
	// (stringable, used by httpPost). Optional shape fields let the checker
	// require `url` and type-check the rest when present; width subtyping
	// still tolerates extra keys the runtime ignores.
	//
	// Output is precise: on a successful request the runtime always builds
	// a 4-field response dict. Encoding it as a shape lets `:status?` /
	// `:body?` etc. resolve their value types without fresh vars.
	// `url` is a required string. `body` and header values are passed through
	// CastString at runtime, which succeeds for str/int/path ("stringable");
	// `timeout` must be a plain int and `followRedirects` a plain bool.
	// Everything but `url` is optional.
	httpReq := "{url: str, timeout?: int, followRedirects?: bool, headers?: {str: str | int | path}, body?: str | int | path}"
	for _, name := range []string{"httpGet", "httpPost"} {
		r.reg(name, "("+httpReq+" -- Maybe[{status: int, reason: str, headers: {[str]}, body: bytes}])")
	}
	r.reg("psub", "(str -- path)")
	for _, name := range []string{"strCmp", "versionSortCmp"} {
		r.reg(name, "(str str -- int)")
	}
	r.reg("floatCmp", "(float float -- int)")
	r.reg("intCmp", "(int int -- int)")
	r.reg("dateTimeCmp", "(datetime datetime -- int)")
	r.reg("base64encode", "(bytes -- str)")
	r.reg("base64decode", "(str -- bytes)")
	// urlEncode: dict values must be a scalar the runtime can CastString,
	// or a list of such scalars (lists become repeated `k=v` pairs). The
	// runtime's CastString succeeds only on str/int/path/literal, so
	// float, bool, bytes, datetime, maybe, nested list, and nested dict
	// all crash — exclude them from the signature.
	r.reg("urlEncode", "(str | {str | int | path | [str] | [int] | [path]} -- str)")
	// setAt / insert : positional set / insert on lists
	r.reg("setAt", "([t] t int -- [t])")
	r.reg("insert", "([t] t int -- [t])")

	// ----- Grid joins -----
	for _, name := range []string{"outerJoin", "leftJoin", "innerJoin", "rightJoin"} {
		r.reg(name, "(Grid Grid (GridRow -- k) (GridRow -- k) -- Grid)")
	}
	// `join` already has a `[str] str -- str` overload; add the grid form
	// so `g1 g2 (:k?) (:k?) join` type-checks.
	r.add("join", "(Grid Grid (GridRow -- k) (GridRow -- k) -- Grid)")

	// ----- Zip ops -----
	// All zip ops accept str|path for path-like args.

	r.reg("zipRead", "(str | path str | path -- Maybe[bytes])")
	for _, name := range []string{"zipDirInc", "zipDirExc"} {
		r.reg(name, "(str | path str | path -- )")
	}
	// zipPack entries are either a bare source path (str | path) or a dict
	// with required `path` plus optional `archivePath` (str | path) and
	// `mode` (int). Stack order: entries below, zip path on top (runtime
	// Pop2 takes the path first).
	r.reg("zipPack", "([str | path | {path: str | path, archivePath?: str | path, mode?: int}] str | path -- )")
	// zipExtract / zipExtractEntry options are all optional; the dict is
	// required positionally but may be empty.
	zipExtractOpts := "{overwrite?: bool, skipExisting?: bool, stripComponents?: int, pattern?: str, preservePermissions?: bool, maxBytes?: int}"
	zipEntryOpts := "{overwrite?: bool, skipExisting?: bool, preservePermissions?: bool, mkdirs?: bool, maxBytes?: int}"
	r.reg("zipExtract", "(str | path str | path "+zipExtractOpts+" -- )")
	r.reg("zipExtractEntry",
		"(str str str "+zipEntryOpts+" -- )",
		"(path str path "+zipEntryOpts+" -- )",
	)
	// zipList: runtime metadata includes non-string fields, but the
	// existing zip-list tests immediately render/join these values as
	// text. Until the checker tracks literal dictionary keys, model this
	// as string-valued metadata so `name get?` and keyed row rendering
	// type-check without forcing every call site to add `str`.
	r.reg("zipList", "(str | path -- [{str: str}])")

	// Tar ops mirror the zip surface exactly (same argument order and option
	// dicts). Compression is chosen from the destination extension on write
	// (.tar.gz / .tgz -> gzip) and sniffed from the gzip magic bytes on read.
	// The write destination also accepts a dict form {path, compress?} that
	// overrides the extension inference (for extensionless targets like redo's
	// $3 temp files).
	tarDest := "str | path | {path: str | path, compress?: bool}"
	r.reg("tarRead", "(str | path str | path -- Maybe[bytes])")
	for _, name := range []string{"tarDirInc", "tarDirExc"} {
		r.reg(name, "(str | path "+tarDest+" -- )")
	}
	r.reg("tarPack", "([str | path | {path: str | path, archivePath?: str | path, mode?: int}] "+tarDest+" -- )")
	r.reg("tarExtract", "(str | path str | path "+zipExtractOpts+" -- )")
	r.reg("tarExtractEntry",
		"(str str str "+zipEntryOpts+" -- )",
		"(path str path "+zipEntryOpts+" -- )",
	)
	// tarList: same widened string-valued metadata modeling as zipList.
	r.reg("tarList", "(str | path -- [{str: str}])")

	// groupBy list form: bucket by a str key.
	r.reg("groupBy", "([t] (t -- str) -- {[t]})")
	// groupBy grid form:
	//   (Grid|GridView [str] [{agg: (GridView -- V)}] -- Grid)
	//
	// The spec element is a shape with a required `agg` quotation that
	// consumes the per-group GridView and produces the value placed in
	// the new column. Width subtyping permits callers to add the optional
	// `name` (str) and `meta` ({str: ...}) fields without listing them
	// here; the runtime validates their types.
	//
	// `V` is declared as a generic on the inner `agg` quote rather than
	// on the outer sig — a shape the sig grammar can't express — so each
	// `unifyQuote` against an agg field instantiates a fresh variable.
	// This lets one list mix specs whose quotes return different types
	// (e.g. an `int` `sumInt` alongside a `float` average), matching the
	// runtime, which makes no cross-spec coherence demand.
	{
		gridU := arena.MakeGrid(0)
		gridViewU := arena.MakeGridView(0)
		aggQuote := arena.MakeQuote(QuoteSig{
			Inputs:   []TypeId{gridViewU},
			Outputs:  []TypeId{arena.MakeVar(0)},
			Generics: []TypeVarId{0},
		})
		aggSpec := arena.MakeShape([]ShapeField{
			{Name: names.Intern("agg"), Type: aggQuote},
			// `name` is always a string when present; `meta` (an arbitrary
			// metadata dict) is left to width subtyping rather than pinned
			// to a value type here.
			{Name: names.Intern("name"), Type: TidStr, Optional: true},
		})
		r.addGo("groupBy", QuoteSig{
			Inputs: []TypeId{
				arena.MakeUnion([]TypeId{gridU, gridViewU}, 0),
				arena.MakeList(TidStr),
				arena.MakeList(aggSpec),
			},
			Outputs: []TypeId{gridU},
		})
	}

	// pivot : rowKeys, colKey, per-cell aggregation.
	r.reg("pivot", "(Grid | GridView [str] str (GridView -- t) -- Grid)")

	// ----- Maybe ops -----

	r.add("map", "(Maybe[t] (t -- u) -- Maybe[u])")
	r.reg("isJust", "(Maybe[t] -- bool)")
	r.reg("isNone", "(Maybe[t] -- bool)")

	// ----- Type introspection -----
	r.reg("typeof", "(t -- str)")

	return r.out
}

// builtinSigsByToken returns sigs for ops that have dedicated lexer
// tokens. The map values are slices so overload dispatch drives
// token-typed builtins the same way it drives LITERAL ones.
//
// STR is the conversion form (T -- str); the lexer emits STR for the
// bare `str` keyword in expression position. The TypeExpr parser
// handles STR in type position separately and never consults this
// table.
func builtinSigsByToken(arena *TypeArena, names *NameTable) map[TokenType][]QuoteSig {
	c := &Checker{arena: arena, names: names}
	sigs := func(srcs ...string) []QuoteSig { return parseBuiltinSigs(c, srcs...) }

	arithmetic := sigs(
		"(int int -- int)",
		"(float float -- float)",
		"(int float -- float)",
		"(float int -- float)",
	)
	// `-` also subtracts two datetimes into a float number of days.
	minusOverloads := append(append([]QuoteSig{}, arithmetic...),
		parseBuiltinSig(c, "(datetime datetime -- float)"))
	comparison := sigs(
		"(int int -- bool)",
		"(float float -- bool)",
		"(datetime datetime -- bool)",
	)

	// Capture markers `*` / `*b` / `^` / `^b` are postfix command
	// modifiers. Model them as a command type over the argv list plus
	// structured stdout/stderr capture modes so redirection can preserve
	// the exact command without enumerating brand combinations. Command
	// types have no sig syntax, so this whole family is hand-built.
	captureT := arena.MakeVar(0)
	captureList := arena.MakeList(captureT)
	cmd := func(stdout, stderr CommandCaptureMode) TypeId {
		return arena.MakeCommand(captureList, stdout, stderr)
	}
	stdoutLinesCmd := cmd(CommandCaptureLines, CommandCaptureNone)
	stdoutStrCmd := cmd(CommandCaptureStr, CommandCaptureNone)
	stdoutBytesCmd := cmd(CommandCaptureBytes, CommandCaptureNone)
	stderrStrCmd := cmd(CommandCaptureNone, CommandCaptureStr)
	stderrBytesCmd := cmd(CommandCaptureNone, CommandCaptureBytes)
	stdoutStrStderrStrCmd := cmd(CommandCaptureStr, CommandCaptureStr)
	stdoutStrStderrBytesCmd := cmd(CommandCaptureStr, CommandCaptureBytes)
	stdoutBytesStderrStrCmd := cmd(CommandCaptureBytes, CommandCaptureStr)
	stdoutBytesStderrBytesCmd := cmd(CommandCaptureBytes, CommandCaptureBytes)

	starOverloads := append(sigs("(int int -- int)", "(float float -- float)"),
		QuoteSig{Inputs: []TypeId{captureList}, Outputs: []TypeId{stdoutStrCmd}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stderrStrCmd}, Outputs: []TypeId{stdoutStrStderrStrCmd}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stderrBytesCmd}, Outputs: []TypeId{stdoutStrStderrBytesCmd}, Generics: []TypeVarId{0}},
	)
	starBytesOverloads := []QuoteSig{
		{Inputs: []TypeId{captureList}, Outputs: []TypeId{stdoutBytesCmd}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stderrStrCmd}, Outputs: []TypeId{stdoutBytesStderrStrCmd}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stderrBytesCmd}, Outputs: []TypeId{stdoutBytesStderrBytesCmd}, Generics: []TypeVarId{0}},
	}
	caretOverloads := []QuoteSig{
		{Inputs: []TypeId{captureList}, Outputs: []TypeId{stderrStrCmd}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutStrCmd}, Outputs: []TypeId{stdoutStrStderrStrCmd}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutBytesCmd}, Outputs: []TypeId{stdoutBytesStderrStrCmd}, Generics: []TypeVarId{0}},
	}
	caretBytesOverloads := []QuoteSig{
		{Inputs: []TypeId{captureList}, Outputs: []TypeId{stderrBytesCmd}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutStrCmd}, Outputs: []TypeId{stdoutStrStderrBytesCmd}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutBytesCmd}, Outputs: []TypeId{stdoutBytesStderrBytesCmd}, Generics: []TypeVarId{0}},
	}

	// `<` / `>` double as redirection markers when applied to a list:
	// `[cmd] "file" >` sets stdout, `[cmd] "in" <` pipes input. The
	// output is the same list (with redirection metadata set).
	//
	// Quote-on-bottom (e.g. `(...) "f" 2>`) is not yet covered. Doing so
	// would need either a "wildcard quote" type or a special-case
	// dispatch — both touch the type machinery in non-trivial ways, so
	// it stays a known gap for now.
	redirSigs := append(append([]QuoteSig{}, comparison...),
		parseBuiltinSig(c, "([t] str | path | bytes -- [t])"))

	// `+` has additional non-arithmetic overloads. Strings concat; lists
	// concat; paths concat (as strings, not filepath.Join); grids concat
	// (row union). Built separately so the arithmetic `+` keeps its
	// int/float-only invariant for `-` / `*` / `**`.
	plusOverloads := sigs(
		"(int int -- int)",
		"(float float -- float)",
		"(str str -- str)",
		"([t] [t] -- [t])",
		"(path path -- path)",
		"(Grid | GridView Grid | GridView -- Grid)",
	)

	// Polymorphic equality: both operands must unify, with the str/path
	// cross-comparisons the runtime also accepts.
	eqSigs := sigs(
		"(a a -- bool)",
		"(path str -- bool)",
		"(str path -- bool)",
	)

	// QUESTION (`?`): three roles depending on what's on the stack.
	//   Maybe[T] -- T   : unwrap (None aborts at runtime)
	//   [T] -- int      : run process list, push exit code
	//   captured command -- captured output(s) int
	// EXECUTE/BANG share the process-list role but produce no output
	// (BANG additionally aborts on nonzero exit at runtime).
	questionSigs := append(sigs(
		"(Maybe[t] -- t)",
		"([t] -- int)",
	),
		QuoteSig{Inputs: []TypeId{stdoutStrCmd}, Outputs: []TypeId{TidStr, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stdoutLinesCmd}, Outputs: []TypeId{arena.MakeList(TidStr), TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stdoutBytesCmd}, Outputs: []TypeId{TidBytes, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stderrStrCmd}, Outputs: []TypeId{TidStr, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stderrBytesCmd}, Outputs: []TypeId{TidBytes, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stdoutStrStderrStrCmd}, Outputs: []TypeId{TidStr, TidStr, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stdoutStrStderrBytesCmd}, Outputs: []TypeId{TidStr, TidBytes, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stdoutBytesStderrStrCmd}, Outputs: []TypeId{TidBytes, TidStr, TidInt}, Generics: []TypeVarId{0}},
		QuoteSig{Inputs: []TypeId{stdoutBytesStderrBytesCmd}, Outputs: []TypeId{TidBytes, TidBytes, TidInt}, Generics: []TypeVarId{0}},
	)
	// EXECUTE / BANG: run a list as a subprocess. Unbranded command
	// lists have no stack output; capture-branded command lists push
	// the captured stream values. When both streams are captured, the
	// runtime pushes stdout first, then stderr.
	execSigs := []QuoteSig{
		{Inputs: []TypeId{captureList}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutLinesCmd}, Outputs: []TypeId{arena.MakeList(TidStr)}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutStrCmd}, Outputs: []TypeId{TidStr}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutBytesCmd}, Outputs: []TypeId{TidBytes}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stderrStrCmd}, Outputs: []TypeId{TidStr}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stderrBytesCmd}, Outputs: []TypeId{TidBytes}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutStrStderrStrCmd}, Outputs: []TypeId{TidStr, TidStr}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutStrStderrBytesCmd}, Outputs: []TypeId{TidStr, TidBytes}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutBytesStderrStrCmd}, Outputs: []TypeId{TidBytes, TidStr}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{stdoutBytesStderrBytesCmd}, Outputs: []TypeId{TidBytes, TidBytes}, Generics: []TypeVarId{0}},
	}

	// IFF: structural conditional with one or two quote arms. The
	// condition may be bool or int, matching the runtime.
	iffSigs := sigs(
		"(bool | int ( -- ) -- )",
		"(bool | int ( -- ) ( -- ) -- )",
		"(bool | int ( -- t) ( -- t) -- t)",
	)

	return map[TokenType][]QuoteSig{
		PLUS:                    plusOverloads,
		MINUS:                   minusOverloads,
		ASTERISK:                starOverloads,
		ASTERISKBINARY:          starBytesOverloads,
		CARET:                   caretOverloads,
		CARETBINARY:             caretBytesOverloads,
		LESSTHAN:                redirSigs,
		GREATERTHAN:             redirSigs,
		STDERRREDIRECT:          redirSigs,
		STDERRAPPEND:            redirSigs,
		STDOUTANDSTDERRREDIRECT: redirSigs,
		STDOUTANDSTDERRAPPEND:   redirSigs,
		INPLACEREDIRECT:         redirSigs,
		STDAPPEND:               redirSigs,
		LESSTHANOREQUAL:         comparison,
		GREATERTHANOREQUAL:      comparison,
		POSITIONAL:              sigs("( -- str)"),
		READ:                    sigs("( -- str bool)"),
		STOP_ON_ERROR:           sigs("( -- )"),
		STR:                     sigs("(a -- str)"),
		NOT:                     sigs("(bool | int -- bool)"),
		EQUALS:                  eqSigs,
		NOTEQUAL:                eqSigs,
		QUESTION:                questionSigs,
		IFF:                     iffSigs,
		// LOOP: pop a quote with no net stack effect.
		LOOP: sigs("(( -- ) -- )"),
		// BREAK / CONTINUE: no stack effect on the surrounding scope.
		BREAK:     sigs("( -- )"),
		CONTINUE:  sigs("( -- )"),
		EXECUTE:   execSigs,
		BANG:      execSigs,
		// PIPE (`|`): the runtime converts a list into a pipeline value.
		// We don't model `Pipe` distinctly from a list yet — typing PIPE
		// as a list-to-list passthrough lets the trailing `; / ! / ?`
		// see something it accepts.
		PIPE: sigs("([a] -- [a])"),
		// AMPERSAND (`&`): marks a command list to run in the background.
		// The runtime flips RunInBackground on the popped list and pushes
		// it back; the actual subprocess start happens at the trailing
		// `;`/`!`. Type effect mirrors PIPE: list-to-list passthrough.
		AMPERSAND: sigs("([a] -- [a])"),
	}
}
