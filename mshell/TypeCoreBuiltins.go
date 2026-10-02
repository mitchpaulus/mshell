package main

import (
	"strings"
	"sync"
)

// The builtin table of the core checker: Φ in ai/type-core-calculus.typ.
// Each entry is written in signature syntax. An output marked `new` is
// fresh: nothing else references it. newList marks a builtin whose output
// is a new list that is fresh when its elements are immutable (design doc,
// "One rule for new lists"). Every other output is shared.
//
// This is the seed of the table; the full table is ported and audited
// against Evaluator.go in stage 3 of ai/type-system-plan.md.

// sigASTCache memoizes the parsed AST per signature string. Signature
// strings are constants and the AST is read-only during resolution, so a
// base built again (the LSP's hover index, tests) skips the parse.
var sigASTCache sync.Map // string -> sigAST

type sigAST struct {
	inputs  []MShellParseItem
	outputs []MShellParseItem
}

// builtinSigAST parses a `(inputs -- outputs)` signature string, once per
// string. A string that does not parse is a programmer error and panics.
func builtinSigAST(src string) sigAST {
	if cached, ok := sigASTCache.Load(src); ok {
		return cached.(sigAST)
	}
	lex := NewLexer(src, nil)
	parser := NewMShellParser(lex)
	parser.NextToken()
	inputs, outputs, err := parser.parseDefSignature()
	if err != nil {
		panic("builtin sig " + src + ": " + err.Error())
	}
	ast := sigAST{inputs: inputs, outputs: outputs}
	sigASTCache.Store(src, ast)
	return ast
}

// coreTableBuilder fills a coreTable from signature strings.
type coreTableBuilder struct {
	res *coreResolver
	t   *coreTable
}

func (b *coreTableBuilder) sig(src string) coreSig {
	ast := builtinSigAST(src)
	parts := b.res.resolveSig(ast.inputs, ast.outputs)
	if len(b.res.errs) > 0 {
		panic("core builtin sig " + src + ": " + b.res.errs[0].Format(b.res.arena, b.res.names))
	}
	for _, g := range parts.gens {
		name := b.res.names.Name(g)
		if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
			panic("core builtin sig " + src + ": unknown type '" + name + "' (generics must be single lowercase letters)")
		}
	}
	return newCoreSig(b.res.arena, parts)
}

func (b *coreTableBuilder) sigs(srcs []string) []coreSig {
	out := make([]coreSig, len(srcs))
	for i, src := range srcs {
		out[i] = b.sig(src)
	}
	return out
}

// alias declares a built-in type alias, written in type syntax: the
// record types builtins take and give have names, so a program can type a
// stored value where it is made (`{url: "x"} as HttpRequest req!`) and
// errors print the name.
func (b *coreTableBuilder) alias(name, body string) {
	t := b.typ(body)
	id := b.res.names.Intern(name)
	idx := b.res.arena.DeclareAlias(id)
	b.res.arena.SetAliasBody(idx, t)
	b.res.aliases[id] = b.res.arena.MakeAliasRef(idx)
}

// builtinAliases declares the names of the record types the builtins take
// and give. Lists are invariant, so a stored list of these is typed with the
// name where it is made.
func (b *coreTableBuilder) builtinAliases() {
	b.alias("NumFmtOptions", "{decimals?: int, sigFigs?: int, sigfigs?: int, preserveInt?: bool, "+
		"decimalPoint?: str | path, thousandsSep?: str | path, grouping?: [int]}")
	b.alias("Link", "{url: str, rel: str, params: {str: str}}")
	b.alias("EnvEvent", "{dt: datetime, kind: str, source: str, changed: bool}")
	b.alias("PackEntry", "str | path | {path: str | path, archivePath?: str | path, mode?: int}")
	b.alias("TarDest", "str | path | {path: str | path, compress?: bool}")
	b.alias("ExtractOptions", "{overwrite?: bool, skipExisting?: bool, preservePermissions?: bool, "+
		"stripComponents?: int, pattern?: str | path, maxBytes?: int}")
	b.alias("ExtractEntryOptions", "{overwrite?: bool, skipExisting?: bool, preservePermissions?: bool, "+
		"mkdirs?: bool, maxBytes?: int}")
	b.alias("ZipEntryInfo", "{name: str, compressedSize: int, uncompressedSize: int, isDir: bool, perm: int, "+
		"executable: bool, modified: datetime}")
	b.alias("TarEntryInfo", "{name: str, compressedSize: int, uncompressedSize: int, isDir: bool, perm: int, "+
		"executable: bool, modified: datetime, 'type': str, linkTarget: str}")
	b.alias("Cookie", "{name: str, value: str, domain: str, path: str, hostOnly: bool, secure: bool, "+
		"httpOnly: bool, sameSite: str, expires: int | float | null, lastAccess: int | float, quoted: bool}")
	b.alias("HttpRequest", "{url: str, timeout?: int, followRedirects?: bool, headers?: {str: str | int | path}, "+
		"body?: str | int | path, cookieJar?: [Cookie]}")
	b.alias("CompletionResult", "[str] | {values?: [str], preferredFiles?: str | [str], files?: str | [str], "+
		"dirs?: bool, binaries?: bool}")
	b.alias("HttpResponse", "{status: int, reason: str, headers: {str: [str]}, body: bytes, cookieJar?: [Cookie]}")
}

// typ resolves a type written in type syntax, with no generics.
func (b *coreTableBuilder) typ(src string) TypeId {
	ast := builtinSigAST("(" + src + " -- )")
	parts := b.res.resolveSig(ast.inputs, ast.outputs)
	if len(b.res.errs) > 0 || len(parts.gens) > 0 || len(parts.ins) != 1 {
		panic("core builtin type: " + src)
	}
	return parts.ins[0]
}

// gridForms writes one signature for each way of reading every `G_x` in
// src as `Grid_x` or `GridView_x`: a grid argument with schema x that may
// be a Grid or a view. They are separate candidates because unification
// never enters a union, and a schema with a variable in it must unify.
func gridForms(srcs ...string) []string {
	var out []string
	for _, src := range srcs {
		forms := []string{src}
		for _, letter := range "abcdefghijklmnopqrstuvwxyz" {
			g := "G_" + string(letter)
			if !strings.Contains(src, g) {
				continue
			}
			var next []string
			for _, f := range forms {
				next = append(next, strings.ReplaceAll(f, g, "Grid_"+string(letter)),
					strings.ReplaceAll(f, g, "GridView_"+string(letter)))
			}
			forms = next
		}
		out = append(out, forms...)
	}
	return out
}

// reg registers a builtin that arrives as a LITERAL word.
func (b *coreTableBuilder) reg(name string, srcs ...string) {
	id := b.res.names.Intern(name)
	if int(id) < len(b.t.byName) && b.t.byName[id] != nil {
		panic("duplicate core builtin: " + name)
	}
	b.t.setName(id, b.sigs(srcs))
}

// regTok registers a builtin that arrives as its own token type.
func (b *coreTableBuilder) regTok(tt TokenType, srcs ...string) {
	b.t.setToken(tt, b.sigs(srcs))
}

// keeps marks every candidate of a registered builtin as giving a fresh
// output when every input is fresh or immutable.
func (b *coreTableBuilder) keeps(sigs []coreSig) {
	for i := range sigs {
		sigs[i].keepOut = 1
	}
}

// child marks every candidate of a registered builtin as running its quote
// arguments on a child stack.
func (b *coreTableBuilder) child(name string) {
	id, _ := b.res.names.Lookup(name)
	for i := range b.t.byName[id] {
		b.t.byName[id][i].child = true
	}
}

// newList marks every candidate of a registered builtin as returning a new
// list, fresh when its elements are immutable.
func (b *coreTableBuilder) newList(name string) {
	id, _ := b.res.names.Lookup(name)
	for i := range b.t.byName[id] {
		b.t.byName[id][i].newListOut = 1
	}
}

func buildCoreTable(res *coreResolver) *coreTable {
	t := &coreTable{}
	b := &coreTableBuilder{res: res, t: t}
	ar := res.arena
	b.builtinAliases()
	t.completion = b.typ("([str] -- CompletionResult)")

	// ----- Strings, regex, numbers, conversions, encoding -----

	// Words that read their argument as a string. The runtime also turns
	// an int into its digits there; the table does not, so `5 readFile`
	// is an error (decided 2026-10-01).

	// No implicit numeric coercion: int with float is a runtime error.
	arithmetic := []string{
		"(int int -- int)",
		"(float float -- float)",
	}
	comparison := []string{
		"(int int -- bool)",
		"(float float -- bool)",
		"(datetime datetime -- bool)",
	}
	// `+` on two grids is in TypeCoreGrid.go.
	b.regTok(PLUS, append(arithmetic, "(str str -- str)", "([t] [t] -- [t])", "(path path -- path)")...)
	for i := range t.byToken[PLUS] {
		if s := &t.byToken[PLUS][i]; len(s.ins) == 2 && ar.Kind(s.ins[0]) == TKList {
			s.newListOut = 1
		}
	}
	b.regTok(MINUS, append(arithmetic, "(datetime datetime -- float)")...)
	b.regTok(ASTERISK, arithmetic...)
	for _, tt := range []TokenType{LESSTHAN, GREATERTHAN, LESSTHANOREQUAL, GREATERTHANOREQUAL} {
		b.regTok(tt, comparison...)
	}
	// Equality fails at runtime across kinds (except null, datetime, Maybe
	// and dict on top), on lists, quotes and grids, and between str and path.
	equality := []string{
		"(int int -- bool)",
		"(float float -- bool)",
		"(str str -- bool)",
		"(bool bool -- bool)",
		"(bytes bytes -- bool)",
		"(path path -- bool)",
		"(datetime datetime -- bool)",
		"(null null -- bool)",
	}
	b.regTok(EQUALS, equality...)
	b.regTok(NOTEQUAL, equality...)
	b.regTok(STR, "(a -- str)")
	b.regTok(NOT, "(bool | int -- bool)")

	b.reg("/", "(int int -- int)", "(float float -- float)", "(path path -- path)")
	b.reg("mod", "(int int -- int)", "(float float -- float)")
	b.reg("abs", "(int -- int)", "(float -- float)")
	b.reg("inc", "(int -- int)")
	for _, name := range []string{"floor", "ceil", "round"} {
		b.reg(name, "(int | float -- int)")
	}
	for _, name := range []string{"sin", "arctan", "ln", "sqrt"} {
		b.reg(name, "(float -- float)")
	}
	// Base below, exponent on top.
	b.reg("pow", "(float float -- float)")
	b.reg("random", "( -- float)")
	b.reg("randomFixed", "( -- float)")

	b.reg("toInt", "(str -- Maybe[int])", "(float -- int)", "(int -- int)")
	b.reg("toFloat", "(str -- Maybe[float])", "(int -- float)", "(float -- float)")
	// Value below, base (2 to 36) on top.
	b.reg("toBase", "(int int -- str)")
	b.reg("fromBase", "(str int -- Maybe[int])")
	// Number below, decimal places on top.
	b.reg("toFixed", "(int | float int -- str)")
	b.reg("numFmt", "(int | float NumFmtOptions -- str)")
	b.reg("toJson", "(a -- str)")
	b.reg("typeof", "(a -- str)")

	// String below, delimiter on top.
	b.reg("split", "(str | path str | path -- new [str])")
	b.reg("wsplit", "(str | path -- new [str])")
	b.reg("lines", "(str -- new [str])")
	// The grid form (two grids and a key quote for each) is in
	// TypeCoreGrid.go.
	b.reg("join", "([str] str -- str)")
	b.reg("unlines", "([str] -- str)")
	b.reg("unlinesCrLf", "([str] -- str)")
	for _, name := range []string{"trim", "trimStart", "trimEnd", "strEscape"} {
		b.reg(name, "(str | path -- str)")
	}
	for _, name := range []string{"upper", "lower", "title"} {
		b.reg(name, "(str -- str)", "(path -- path)")
	}
	// The searched string below, the prefix, suffix or substring on top.
	b.reg("startsWith", "(str | path str | path -- bool)")
	b.reg("endsWith", "(str | path str | path -- bool)")
	b.reg("countSubStr", "(str | path str | path -- int)")
	// Original, find, replacement.
	b.reg("findReplace", "(str | path str | path str | path -- str)")
	// Input, pad string, total length in bytes.
	b.reg("leftPad", "(str | path str | path int -- str)")

	// The string below, the regex on top.
	b.reg("reMatch", "(str | path str | path -- bool)")
	b.reg("reSplit", "(str | path str | path -- new [str])")
	b.reg("reFindAll", "(str | path str | path -- new [[str]])")
	b.reg("reFindAllIndex", "(str | path str | path -- new [[int]])")
	// String, regex, replacement.
	b.reg("reReplace", "(str | path str | path str | path -- str)")

	b.reg("base64encode", "(bytes -- str)")
	b.reg("base64decode", "(str -- bytes)")
	b.reg("utf8Str", "(bytes -- str)")
	b.reg("utf8Bytes", "(str -- bytes)")
	// A str or bytes is hashed as its content; a path names a file.
	b.reg("md5", "(str | path | bytes -- str)")
	// Always names a file.
	b.reg("sha256sum", "(str | path -- str)")
	b.reg("uuid", "( -- str)")
	b.reg("uuid7", "( -- str)")
	b.reg("urlEncode", "(str -- str)")
	// The dict form writes each value as a string: a str, int or path, or a
	// list of them, which gives the key once per element. Lists are
	// invariant, so each stored list type has its own form (TypeCoreDict.go).
	for _, src := range []string{"[str]", "[int]", "[path]", "[str | int | path]"} {
		t.urlEncodeLists = append(t.urlEncodeLists, b.typ(src))
	}

	// A path names a file; a str (a bare word in a list literal too) is the text.
	b.reg("parseJson", "(str | path | bytes -- new Json)")
	// The document node: tag "" with the <html> element as its child.
	b.reg("parseHtml", "(str | path -- new HtmlNode)")
	b.reg("parseLinkHeader", "(str -- new [Link])")

	b.regTok(POSITIONAL, "( -- str)")
	// A line of stdin, and whether one was read.
	b.regTok(READ, "( -- str bool)")
	b.regTok(QUESTION, "(Maybe[a] -- a)")
	b.keeps(t.token(QUESTION))

	// ----- Lists -----

	b.reg("len", append([]string{"([a] -- int)", "(str | path | {} -- int)", "(GridRow_s -- int)"},
		gridForms("(G_s -- int)")...)...)
	// The value-below-list order (`x [xs] append`) is left to partialName:
	// when both are lists the runtime appends the upper list into the
	// lower one, so `(a [a] -- [a])` would be wrong whenever a is a list.
	b.reg("append", "([a] a -- [a])")
	// nth indexes whichever argument is not the int; when the top is an
	// int, the one below is indexed.
	b.reg("nth", append([]string{
		"([a] int -- a)", "(int [a] -- a)",
		"(str int -- str)", "(int str -- str)",
		"(path int -- path)", "(int path -- path)",
		"(bytes int -- bytes)", "(int bytes -- bytes)",
	}, gridForms("(G_s int -- GridRow_s)", "(int G_s -- GridRow_s)")...)...)
	// Words that change their receiver in place and give it back.
	b.reg("setAt", "([a] a int -- [a])")
	b.reg("insert", "([a] a int -- [a])")
	// On a dict, only a `{str: a}` may lose a key: a shape never does.
	b.reg("del", "([a] int -- [a])", "(int [a] -- [a])", "({str: a} str | path -- {str: a})")
	// The grid form is in TypeCoreGrid.go.
	b.reg("extend", "([a] [a] -- [a])")
	// pop removes the last element and gives it back; the list is not pushed.
	b.reg("pop", "([a] -- Maybe[a])")
	// extend is left shared: its result would be fresh only by the argument
	// Slice.v makes for take, that the consumed list is dead, which is not
	// proved for extend.
	for _, name := range []string{"append", "setAt", "insert", "del", "pop"} {
		b.keeps(t.name(res.names.Intern(name)))
	}

	// Words that return a new list. newList has no effect on a candidate
	// whose output is not a list (str, Grid).
	b.reg("seq", "(int -- new [int])")
	b.reg("reverse", append([]string{"([a] -- [a])"}, gridForms("(G_s -- Grid_s)")...)...)
	b.reg("take", "([a] int -- [a])", "(str int -- str)")
	b.reg("skip", "([a] int -- [a])", "(str int -- str)")
	// sort and sortV turn every element into a string, sort the strings,
	// and return them: `[10 9] sort` is `["10" "9"]`.
	// Lists are invariant, so a list of one element type has its own form,
	// and a mixed list the widest one (design doc, "Checking positions").
	for _, name := range []string{"sort", "sortV"} {
		b.reg(name, "([str] -- [str])", "([int] -- [str])", "([path] -- [str])", "([str | int | path] -- [str])")
	}
	b.reg("uniq", "([str] -- [str])", "([int] -- [int])", "([float] -- [float])",
		"([path] -- [path])", "([datetime] -- [datetime])",
		"([str | path | int | float | datetime] -- [str | path | int | float | datetime])")
	for _, name := range []string{"reverse", "take", "skip", "sort", "sortV", "uniq"} {
		b.newList(name)
	}
	b.reg("sortBy", gridForms("(G_s str | [str] -- Grid_s)")...)
	b.newList("sortBy")

	// Words that run a quote on a child stack, once per element (or per
	// comparison). sortByCmp sorts a list in place and gives it back; its
	// quote sees the elements, so the result is shared.
	// A grid's quote gets rows of its schema. filter on a grid gives a view
	// of the same grid, which is shared (newList leaves a view shared); the
	// grid form of sortByCmp gives a new grid.
	b.reg("each", append([]string{"([a] (a -- ) -- )"}, gridForms("(G_s (GridRow_s -- ) -- )")...)...)
	b.reg("filter", append([]string{"([a] (a -- bool) -- [a])"}, gridForms("(G_s (GridRow_s -- bool) -- GridView_s)")...)...)
	b.newList("filter")
	b.reg("sortByCmp", append([]string{"([a] (a a -- int) -- [a])"}, gridForms("(G_s (GridRow_s GridRow_s -- int) -- Grid_s)")...)...)
	for cmp, i := t.name(res.names.Intern("sortByCmp")), 1; i < len(cmp); i++ {
		cmp[i].newListOut = 1
	}
	for _, name := range []string{"each", "filter", "sortByCmp"} {
		b.child(name)
	}
	// map on a list runs its quote on a child stack; on a Maybe, on the
	// current stack. The dict and grid forms are in TypeCoreDict.go and
	// TypeCoreGrid.go.
	b.reg("map",
		"([a] (a -- b) -- [b])",
		"(Maybe[a] (a -- b) -- Maybe[b])",
	)
	mapSigs := t.name(res.names.Intern("map"))
	mapSigs[0].newListOut = 1
	mapSigs[0].child = true

	// ----- Maybe -----

	b.reg("isNone", "(Maybe[a] -- bool)")
	b.reg("maybe", "(Maybe[a] a -- a)")
	b.keeps(t.name(res.names.Intern("maybe")))
	// These run their quote on the current stack.
	b.reg("bind", "(Maybe[a] (a -- Maybe[b]) -- Maybe[b])")
	b.reg("map2", "(Maybe[a] Maybe[b] (a b -- c) -- Maybe[c])")

	// ----- Comparisons and aggregates -----

	// strCmp, versionSortCmp, index and lastIndexOf read their arguments
	// as strings.
	for _, name := range []string{"strCmp", "versionSortCmp"} {
		b.reg(name, "(str | path str | path -- int)")
	}
	for _, name := range []string{"index", "lastIndexOf"} {
		b.reg(name, "(str | path str | path -- Maybe[int])")
	}
	b.reg("intCmp", "(int int -- int)")
	b.reg("floatCmp", "(float float -- int)")
	b.reg("dateTimeCmp", "(datetime datetime -- int)")
	b.reg("sum", "([int] -- int)", "([float] -- float)")
	for _, name := range []string{"max", "min"} {
		b.reg(name, "([int] -- int)", "([float] -- float)", "([datetime] -- datetime)")
	}
	// Mixed int and float pairs are rejected at runtime.
	for _, name := range []string{"max2", "min2"} {
		b.reg(name, "(int int -- int)", "(float float -- float)")
	}
	// With a quote, the quote runs on the current stack only when the bool
	// below does not decide the result.
	for _, name := range []string{"and", "or"} {
		b.reg(name, "(bool bool -- bool)", "(bool ( -- bool) -- bool)")
	}

	// Paths. A path input is a str or a path.
	b.reg("toPath", "(str | path -- path)")
	b.reg("absPath", "(str | path -- path)")
	for _, name := range []string{"basename", "dirname", "stem"} {
		b.reg(name, "(str | path -- path)")
	}
	b.reg("ext", "(str | path -- str)")
	b.reg("removeWindowsVolumePrefix", "(str | path -- str)")
	b.reg("nullDevice", "( -- path)")
	b.reg("tempDir", "( -- path)")
	b.reg("tempFile", "( -- path)")
	b.reg("tempFileExt", "(str | path -- path)")
	b.reg("pwd", "( -- str)")
	b.reg("psub", "(str -- str)")

	// Files. Lists read from the file system are new lists of paths.
	b.reg("readFile", "(str | path -- str)")
	b.reg("readFileBytes", "(str | path -- bytes)")
	// Content below, file path on top.
	for _, name := range []string{"writeFile", "appendFile"} {
		b.reg(name, "(str | path | bytes str | path -- )")
	}
	// Source below, destination on top.
	for _, name := range []string{"cp", "mv", "hardLink"} {
		b.reg(name, "(str | path str | path -- )")
	}
	for _, name := range []string{"rm", "rmf", "mkdir", "mkdirp", "cd", "mshFileManager", "clip"} {
		b.reg(name, "(str | path -- )")
	}
	for _, name := range []string{"isDir", "isFile", "fileExists", "isCmd"} {
		b.reg(name, "(str | path -- bool)")
	}
	b.reg("fileSize", "(str | path -- Maybe[int])")
	b.reg("modTime", "(str | path -- Maybe[datetime])")
	b.reg("glob", "(str | path -- new [path])")
	b.reg("lsDir", "(str | path -- new [path])")
	b.reg("files", "( -- new [path])")
	b.reg("dirs", "( -- new [path])")
	b.reg("cdh", "( -- )")
	b.reg("cdp", "( -- )")

	// The process and its environment.
	b.reg("args", "( -- new [str])")
	b.reg("stdin", "( -- str)")
	for _, name := range []string{"stdinIsTerminal", "stdoutIsTerminal", "stderrIsTerminal"} {
		b.reg(name, "( -- bool)")
	}
	b.reg("prompt", "(str | path -- str)")
	b.reg("runtime", "( -- str)")
	b.reg("hostname", "( -- str)")
	// Name below, value on top.
	b.reg("setenv", "(str | path str | path -- )")
	b.reg("unsetenv", "(str | path -- )")
	b.reg("envInspect", "(str | path -- new [EnvEvent])")
	// A quote per completion definition, built from its body; each def's
	// signature is checked below the quote type (checkCompletionSig).
	// soe: stop the script at the first failed command.
	b.regTok(STOP_ON_ERROR, "( -- )")
	b.reg("completionDefs", "( -- new {str: [([str] -- CompletionResult)]})")
	// Each element is [name fullPath].
	b.reg("binPaths", "( -- new [[str]])")
	b.reg("sleep", "(int | float -- )")

	// Dates and times.
	b.reg("now", "( -- datetime)")
	b.reg("toDt", "(str -- Maybe[datetime])", "(datetime -- datetime)")
	for _, name := range []string{"date", "utcToCst", "cstToUtc"} {
		b.reg(name, "(datetime -- datetime)")
	}
	for _, name := range []string{"year", "month", "day", "hour", "minute", "dow",
		"toUnixTime", "toUnixTimeMilli", "toUnixTimeMicro", "toUnixTimeNano"} {
		b.reg(name, "(datetime -- int)")
	}
	for _, name := range []string{"isWeekend", "isWeekday"} {
		b.reg(name, "(datetime -- bool)")
	}
	for _, name := range []string{"fromUnixTime", "fromUnixTimeMilli", "fromUnixTimeMicro", "fromUnixTimeNano"} {
		b.reg(name, "(int -- datetime)")
	}
	b.reg("toOleDate", "(datetime -- float)")
	b.reg("fromOleDate", "(int | float -- datetime)")
	// Date below, day count on top.
	b.reg("addDays", "(datetime int | float -- datetime)")
	// Date below, Go layout string on top.
	b.reg("dateFmt", "(datetime str -- str)")

	b.reg("just", "(a -- Maybe[a])")
	b.keeps(t.name(res.names.Intern("just")))
	t.setName(res.names.Intern("none"), []coreSig{{outs: []TypeId{ar.MakeMaybeEnum(TidBottom)}}})
	b.reg("null", "( -- null)")
	// Exit codes outside 0-255, and exit inside a format string
	// interpolation, are checked errors.
	b.reg("exit", "(int -- never)")
	// A cycle is a checked error.
	b.reg("deepCopy", "(a -- new a)")

	// Writes to stdout (w, wl) or stderr (we, wle). Only str and int are
	// printed; wl and wle add a newline and refuse bytes.
	b.reg("w", "(str | int | bytes -- )")
	b.reg("we", "(str | int | bytes -- )")
	b.reg("wl", "(str | int -- )")
	b.reg("wle", "(str | int -- )")

	// ---- New entries ----

	// Print the stack, the definitions or the environment to stderr.
	b.reg("stack", "( -- )")
	b.reg("defs", "( -- )")
	b.reg("env", "( -- )")
	// A hint for the checker; the runtime does nothing.
	b.reg("dbg", "( -- )")

	// Archives. Paths are str or path. Stack order: source below
	// destination, options on top. The option dicts are read by key only.
	// Lists are invariant, so the entry list has one form per stored list
	// type a program has: plain strings or paths (from ls, lines, glob), a
	// mix of the two, or the built-in PackEntry alias. Every form gives the
	// same outputs, so a new literal that fits several takes the first.
	packEntries := []string{"[str]", "[path]", "[str | path]", "[PackEntry]"}
	packSigs := func(dest string) []string {
		sigs := make([]string, len(packEntries))
		for i, e := range packEntries {
			sigs[i] = "(" + e + " " + dest + " -- )"
		}
		return sigs
	}
	for _, name := range []string{"zipDirInc", "zipDirExc"} {
		b.reg(name, "(str | path str | path -- )")
	}
	for _, name := range []string{"tarDirInc", "tarDirExc"} {
		b.reg(name, "(str | path TarDest -- )")
	}
	b.reg("zipPack", packSigs("str | path")...)
	b.reg("tarPack", packSigs("TarDest")...)
	b.reg("zipList", "(str | path -- new [ZipEntryInfo])")
	b.reg("tarList", "(str | path -- new [TarEntryInfo])")
	for _, name := range []string{"zipExtract", "tarExtract"} {
		b.reg(name, "(str | path str | path ExtractOptions -- )")
	}
	// archive, entry name, destination, options.
	for _, name := range []string{"zipExtractEntry", "tarExtractEntry"} {
		b.reg(name, "(str | path str | path str | path ExtractEntryOptions -- )")
	}
	// archive, entry name; none when the entry does not exist.
	for _, name := range []string{"zipRead", "tarRead"} {
		b.reg(name, "(str | path str | path -- Maybe[bytes])")
	}

	// HTTP. The request is read by key. The jar is written in place: expired
	// cookies are removed, lastAccess is set to an int, and cookies are
	// replaced or appended; the response's cookieJar is that same list.
	// The response is left shared: its cookie jar is the request's list.
	for _, name := range []string{"httpGet", "httpPost"} {
		b.reg(name, "(HttpRequest -- Maybe[HttpResponse])")
	}

	// ----- Dicts -----

	// A dict key or a column name is read as a string.
	key := "str | path"

	// Words that read no values take every dict-kinded value.
	b.reg("keys", "({} -- new [str])")
	b.reg("in", "({} "+key+" -- bool)", "("+key+" "+key+" -- bool)")
	// Writes with a runtime key need every key deletable. A literal key on
	// a shape, or on a fresh dict at a new type, is not covered yet.
	b.reg("set", "({str: a} "+key+" a -- {str: a})")
	b.keeps(t.name(res.names.Intern("set")))
	b.reg("setd", "({str: a} "+key+" a -- )")
	// Not in the table: get, getDef, values, keyValues and the `:name`
	// getter. Their result is a label's type or the join of every label's
	// type, which no signature can say.

	// ----- Grids -----
	//
	// Every Grid, GridView and GridRow below has the unknown schema. A word
	// whose result depends on the schema is either left out or gives the
	// unknown schema.

	b.reg("gridRows", gridForms("(G_s -- int)")...)
	b.reg("gridCols", gridForms("(G_s -- new [str])")...)
	// Metadata dicts are shared with the dicts they were made from and
	// between grids, so they are read only.
	b.reg("gridMeta", gridForms("(G_s -- Maybe[{}])")...)
	b.reg("gridColMeta", gridForms("(G_s "+key+" -- Maybe[{}])")...)
	// A Grid is returned as is; a GridView is copied into a new grid.
	b.reg("gridCompact", "(Grid_s -- Grid_s)", "(GridView_s -- Grid_s)")
	compact := t.name(res.names.Intern("gridCompact"))
	compact[0].keepOut = 1
	compact[1].newListOut = 1
	// Every column of a grid from toGrid holds strings; their names are data.
	t.setName(res.names.Intern("toGrid"), []coreSig{{
		ins:    []TypeId{ar.MakeList(ar.MakeList(TidStr))},
		outs:   []TypeId{ar.MakeGridOf(TKGrid, ar.MakeRecord(nil, RecordField{Status: FieldOptional, Type: TidStr}))},
		newOut: 1,
	}})
	b.reg("parseCsv", "(str | path -- new [[str]])")

	// The list form of groupBy. The grid form takes a list of aggregation
	// specs; written at the call, each quote gives its own type and the
	// result's columns are known (TypeCoreGrid.go). Otherwise, here, every
	// quote gives one type and the result's columns are not known.
	b.reg("groupBy", append([]string{"([a] (a -- "+key+") -- {str: [a]})"},
		gridForms("(G_s [str] [{agg: (GridView_s -- a), name?: str, meta?: {}}] -- Grid)")...)...)
	b.child("groupBy")
	// A spec is exact: the runtime refuses a key other than agg, name and
	// meta. Type syntax has no exact shape, so the grid forms' spec records
	// are made exact here.
	for i, sig := range t.name(res.names.Intern("groupBy")) {
		if i == 0 {
			continue
		}
		specs := sig.ins[2]
		rec := ar.Record(TypeId(ar.Node(specs).A))
		sig.ins[2] = ar.MakeList(ar.MakeRecord(rec.Fields, RecordField{Status: FieldAbsent}))
	}
	// In TypeCoreGrid.go, since their results depend on the schema: select,
	// exclude, derive, pivot, the grid forms of join, leftJoin, outerJoin,
	// map, extend and `+`, updateCol, gridSetCell, gridAddCol,
	// gridRemoveCol, gridRenameCol, gridCol, gridValues, toDict.

	// parseExcel: one record per sheet. An error cell is none.
	{
		cell := ar.MakeUnion([]TypeId{TidStr, TidFloat, TidBool, ar.MakeMaybeEnum(TidBottom)})
		field := func(name string, ty TypeId) RecordField {
			return RecordField{Name: res.names.Intern(name), Status: FieldRequired, Type: ty}
		}
		sheet := ar.MakeRecord([]RecordField{
			field("name", TidStr),
			field("data", ar.MakeList(ar.MakeList(cell))),
			field("hidden", TidBool),
			field("visibility", TidStr),
		}, RecordField{Status: FieldAbsent})
		t.setName(res.names.Intern("parseExcel"), []coreSig{{
			ins:    []TypeId{ar.MakeUnion([]TypeId{TidPath, TidBytes})},
			outs:   []TypeId{ar.MakeList(sheet)},
			newOut: 1,
		}})
	}

	// `x [xs] append`: with the list on top, a value below it that is not a
	// list is appended (two lists append the upper into the lower, the
	// table's form).
	t.appendBelow = b.sig("(a [a] -- [a])")
	t.appendBelow.keepOut = 1

	// Indexing. `:n:` gives an element (a row of a grid, a cell of a row);
	// slices give a new list, or a view of the same grid.
	t.index = b.sigs(append([]string{"([a] -- a)", "(str -- str)", "(path -- path)", "(bytes -- bytes)"},
		gridForms("(G_s -- GridRow_s)")...))
	// A row's cell by position: the column is not known.
	anyRow := ar.MakeGridOf(TKGridRow, res.unknownSchema())
	t.index = append(t.index, coreSig{ins: []TypeId{anyRow}, outs: []TypeId{TidUnknown}})
	// nth on a row is the same read, with the index on either side.
	nthRow := []coreSig{{ins: []TypeId{anyRow, TidInt}, outs: []TypeId{TidUnknown}}, {ins: []TypeId{TidInt, anyRow}, outs: []TypeId{TidUnknown}}}
	t.setName(res.names.Intern("nth"), append(t.name(res.names.Intern("nth")), nthRow...))
	t.slice = b.sigs(append([]string{"([a] -- [a])", "(str -- str)", "(path -- path)", "(bytes -- bytes)"},
		gridForms("(G_s -- GridView_s)")...))
	t.slice[0].newListOut = 1
	t.multi = b.sigs([]string{"([a] -- [a])", "(str -- str)", "(path -- path)", "(bytes -- bytes)"})
	t.multi[0].newListOut = 1
	// A pipe: an element is one of its commands; a slice is a new list of
	// them.
	{
		cmd := ar.MakeParam(0)
		pipe := ar.MakeCommand(ar.MakeList(cmd), CommandPipe, CommandCaptureNone)
		gens := []NameId{res.names.Intern("a")}
		t.index = append(t.index, coreSig{ins: []TypeId{pipe}, outs: []TypeId{cmd}, gens: gens, genIn: 1, genOut: 1})
		t.slice = append(t.slice, coreSig{ins: []TypeId{pipe}, outs: []TypeId{ar.MakeList(cmd)}, gens: gens, genIn: 1, genOut: 1})
		t.multiIndex = append(t.multi[:len(t.multi):len(t.multi)], coreSig{ins: []TypeId{pipe}, outs: []TypeId{pipe}, gens: gens, genIn: 1, genOut: 1})
	}

	return t
}

// coreWalkerSigs are signatures, for hover, of the words the walker types
// itself instead of through the table, because their result depends on
// more than the argument types: a literal key or column name, a grid's
// schema, or freshness. `T` is the type the walker works out.
var coreWalkerSigs = map[string][]string{
	"dup":           {"(a -- a a)"},
	"drop":          {"(a -- )"},
	"swap":          {"(a b -- b a)"},
	"over":          {"(a b -- a b a)"},
	"rot":           {"(a b c -- b c a)"},
	"-rot":          {"(a b c -- c a b)"},
	"nip":           {"(a b -- b)"},
	"get":           {"(dict str -- Maybe[T])", "(Grid str -- [T])", "(GridRow str -- T)"},
	"getDef":        {"(dict str T -- T)"},
	"values":        {"(dict -- [T])"},
	"keyValues":     {"(dict -- [{k: str, v: T}])"},
	"toDict":        {"(GridRow -- new {...})"},
	"gridCol":       {"(Grid str -- [T])", "(GridView str -- [T])"},
	"gridValues":    {"(Grid -- [[T]])", "(GridView -- [[T]])"},
	"select":        {"(Grid [str] -- new Grid)", "(GridView [str] -- new Grid)"},
	"exclude":       {"(Grid [str] -- new Grid)", "(GridView [str] -- new Grid)"},
	"derive":        {"(Grid str {} (GridRow -- T) -- new Grid)", "(GridView str {} (GridRow -- T) -- new Grid)"},
	"updateCol":     {"(Grid str (T -- U) -- Grid)", "(GridView str (T -- U) -- new Grid)"},
	"gridSetCell":   {"(Grid str int T -- Grid)"},
	"gridAddCol":    {"(Grid str [T] -- Grid)"},
	"gridRemoveCol": {"(Grid str -- Grid)"},
	"gridRenameCol": {"(Grid str str -- Grid)"},
	"leftJoin":      {"(Grid Grid (GridRow -- K) (GridRow -- K) -- new Grid)"},
	"outerJoin":     {"(Grid Grid (GridRow -- K) (GridRow -- K) -- new Grid)"},
	"pivot":         {"(Grid [str] str (GridView -- T) -- new Grid)"},
}
