package main

// The builtin table of the core checker: Φ in ai/type-core-calculus.typ.
// Each entry is written in signature syntax. An output marked `new` is
// fresh: nothing else references it. newList marks a builtin whose output
// is a new list that is fresh when its elements are immutable (design doc,
// "One rule for new lists"). Every other output is shared.
//
// This is the seed of the table; the full table is ported and audited
// against Evaluator.go in stage 3 of ai/type-system-plan.md.

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

	// ----- Strings, regex, numbers, conversions, encoding -----

	// Words that read their argument as a string. The runtime also turns
	// an int into its digits there; the table does not, so `5 readFile`
	// is an error (decision pending, ai/type-system-plan.md).

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
	b.regTok(PLUS, append(arithmetic, "(str str -- str)", "([t] [t] -- [t])", "(path path -- path)",
		"(Grid | GridView Grid | GridView -- Grid)")...)
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
	b.reg("numFmt", "(int | float {decimals?: int, sigFigs?: int, sigfigs?: int, preserveInt?: bool, "+
		"decimalPoint?: str | path, thousandsSep?: str | path, grouping?: [int]} -- str)")
	b.reg("toJson", "(a -- str)")
	b.reg("typeof", "(a -- str)")

	// String below, delimiter on top.
	b.reg("split", "(str | path str | path -- new [str])")
	b.reg("wsplit", "(str | path -- new [str])")
	b.reg("lines", "(str -- new [str])")
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

	// A path, or a bare word from a list literal, names a file; a str is the text.
	b.reg("parseJson", "(str | path | bytes -- new Json)")
	// The document node: tag "" with the <html> element as its child.
	b.reg("parseHtml", "(str | path -- new HtmlNode)")
	b.reg("parseLinkHeader", "(str -- new [{url: str, rel: str, params: {str: str}}])")

	b.regTok(QUESTION, "(Maybe[a] -- a)")
	b.keeps(t.token(QUESTION))

	// ----- Lists -----

	b.reg("len", "([a] -- int)", "(str | path | {} | Grid | GridView | GridRow -- int)")
	// The value-below-list order (`x [xs] append`) is left to partialName:
	// when both are lists the runtime appends the upper list into the
	// lower one, so `(a [a] -- [a])` would be wrong whenever a is a list.
	b.reg("append", "([a] a -- [a])")
	// nth indexes whichever argument is not the int; when the top is an
	// int, the one below is indexed.
	b.reg("nth",
		"([a] int -- a)", "(int [a] -- a)",
		"(str int -- str)", "(int str -- str)",
		"(path int -- path)", "(int path -- path)",
		"(bytes int -- bytes)", "(int bytes -- bytes)",
		"(Grid | GridView int -- GridRow)", "(int Grid | GridView -- GridRow)",
	)
	// Words that change their receiver in place and give it back.
	b.reg("setAt", "([a] a int -- [a])")
	b.reg("insert", "([a] a int -- [a])")
	b.reg("del", "([a] int -- [a])", "(int [a] -- [a])")
	b.reg("extend", "([a] [a] -- [a])",
		"(Grid Grid | GridView -- Grid)", "(GridView Grid | GridView -- GridView)")
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
	b.reg("reverse", "([a] -- [a])", "(Grid | GridView -- Grid)")
	b.reg("take", "([a] int -- [a])", "(str int -- str)")
	b.reg("skip", "([a] int -- [a])", "(str int -- str)")
	// sort and sortV turn every element into a string, sort the strings,
	// and return them: `[10 9] sort` is `["10" "9"]`.
	for _, name := range []string{"sort", "sortV"} {
		b.reg(name, "([str] -- [str])", "([int] -- [str])", "([path] -- [str])")
	}
	b.reg("uniq", "([str] -- [str])", "([int] -- [int])", "([float] -- [float])",
		"([path] -- [path])", "([datetime] -- [datetime])")
	for _, name := range []string{"reverse", "take", "skip", "sort", "sortV", "uniq"} {
		b.newList(name)
	}
	b.reg("sortBy", "(Grid | GridView str | [str] -- Grid)")

	// Words that run a quote on a child stack, once per element (or per
	// comparison). sortByCmp sorts a list in place and gives it back; its
	// quote sees the elements, so the result is shared.
	b.reg("each", "([a] (a -- ) -- )", "(Grid | GridView (GridRow -- ) -- )")
	b.reg("filter", "([a] (a -- bool) -- [a])", "(Grid | GridView (GridRow -- bool) -- GridView)")
	b.newList("filter")
	b.reg("sortByCmp", "([a] (a a -- int) -- [a])", "(Grid | GridView (GridRow GridRow -- int) -- Grid)")
	for _, name := range []string{"each", "filter", "sortByCmp"} {
		b.child(name)
	}
	// map on a list or grid runs its quote on a child stack; on a Maybe,
	// on the current stack.
	b.reg("map",
		"([a] (a -- b) -- [b])",
		"(Maybe[a] (a -- b) -- Maybe[b])",
		"(Grid | GridView (GridRow -- GridRow | {}) -- Grid)",
	)
	mapSigs := t.name(res.names.Intern("map"))
	mapSigs[0].newListOut = 1
	mapSigs[0].child = true
	mapSigs[2].child = true

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
	b.reg("envInspect", "(str | path -- new [{dt: datetime, kind: str, source: str, changed: bool}])")
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
	packEntries := "[str | path | {path: str | path, archivePath?: str | path, mode?: int}]"
	tarDest := "str | path | {path: str | path, compress?: bool}"
	extractOpts := "{overwrite?: bool, skipExisting?: bool, preservePermissions?: bool, stripComponents?: int, pattern?: str | path, maxBytes?: int}"
	entryOpts := "{overwrite?: bool, skipExisting?: bool, preservePermissions?: bool, mkdirs?: bool, maxBytes?: int}"
	zipEntry := "{name: str, compressedSize: int, uncompressedSize: int, isDir: bool, perm: int, executable: bool, modified: datetime}"
	tarEntry := "{name: str, compressedSize: int, uncompressedSize: int, isDir: bool, perm: int, executable: bool, modified: datetime, 'type': str, linkTarget: str}"
	for _, name := range []string{"zipDirInc", "zipDirExc"} {
		b.reg(name, "(str | path str | path -- )")
	}
	for _, name := range []string{"tarDirInc", "tarDirExc"} {
		b.reg(name, "(str | path "+tarDest+" -- )")
	}
	b.reg("zipPack", "("+packEntries+" str | path -- )")
	b.reg("tarPack", "("+packEntries+" "+tarDest+" -- )")
	b.reg("zipList", "(str | path -- new ["+zipEntry+"])")
	b.reg("tarList", "(str | path -- new ["+tarEntry+"])")
	for _, name := range []string{"zipExtract", "tarExtract"} {
		b.reg(name, "(str | path str | path "+extractOpts+" -- )")
	}
	// archive, entry name, destination, options.
	for _, name := range []string{"zipExtractEntry", "tarExtractEntry"} {
		b.reg(name, "(str | path str | path str | path "+entryOpts+" -- )")
	}
	// archive, entry name; none when the entry does not exist.
	for _, name := range []string{"zipRead", "tarRead"} {
		b.reg(name, "(str | path str | path -- Maybe[bytes])")
	}

	// HTTP. The request is read by key. The jar is written in place: expired
	// cookies are removed, lastAccess is set to an int, and cookies are
	// replaced or appended; the response's cookieJar is that same list.
	cookie := "{name: str, value: str, domain: str, path: str, hostOnly: bool, secure: bool, httpOnly: bool, sameSite: str, expires: int | float | null, lastAccess: int | float, quoted: bool}"
	httpReq := "{url: str, timeout?: int, followRedirects?: bool, headers?: {str: str | int | path}, body?: str | int | path, cookieJar?: [" + cookie + "]}"
	httpResp := "Maybe[{status: int, reason: str, headers: {str: [str]}, body: bytes, cookieJar?: [" + cookie + "]}]"
	// The response is left shared: its cookie jar is the request's list.
	for _, name := range []string{"httpGet", "httpPost"} {
		b.reg(name, "("+httpReq+" -- "+httpResp+")")
	}

	// Uses of these words the table does not cover yet.
	t.partialToken = map[TokenType]string{
		LESSTHAN:    "redirects",
		GREATERTHAN: "redirects",
		ASTERISK:    "command captures",
		QUESTION:    "running commands",
	}
	t.partialName = map[NameId]string{
		res.names.Intern("append"):    "'append' with the value below the list",
		res.names.Intern("nth"):       "'nth' on a GridRow",
		res.names.Intern("map"):       "the dict form of 'map'",
		res.names.Intern("filter"):    "the dict form of 'filter'",
		res.names.Intern("sort"):      "'sort' on a list that mixes str, int and path",
		res.names.Intern("sortV"):     "'sortV' on a list that mixes str, int and path",
		res.names.Intern("uniq"):      "'uniq' on a list that mixes element types",
		res.names.Intern("urlEncode"): "the dict form of 'urlEncode'",
		res.names.Intern("join"):      "the grid form of 'join'",
	}
	return t
}
