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

	arithmetic := []string{
		"(int int -- int)",
		"(float float -- float)",
		"(int float -- float)",
		"(float int -- float)",
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
	b.regTok(ASTERISK, "(int int -- int)", "(float float -- float)")
	for _, tt := range []TokenType{LESSTHAN, GREATERTHAN, LESSTHANOREQUAL, GREATERTHANOREQUAL} {
		b.regTok(tt, comparison...)
	}
	b.regTok(EQUALS, "(a a -- bool)", "(path str -- bool)", "(str path -- bool)")
	b.regTok(NOTEQUAL, "(a a -- bool)", "(path str -- bool)", "(str path -- bool)")
	b.regTok(STR, "(a -- str)")
	b.regTok(NOT, "(bool | int -- bool)")
	b.regTok(QUESTION, "(Maybe[a] -- a)")
	b.keeps(t.token(QUESTION))

	b.reg("/", "(int int -- int)", "(float float -- float)", "(path path -- path)")
	b.reg("mod", "(int int -- int)", "(float float -- float)")
	b.reg("abs", "(int -- int)", "(float -- float)")
	b.reg("inc", "(int -- int)")
	b.reg("max2", "(int int -- int)", "(float float -- float)")
	b.reg("min2", "(int int -- int)", "(float float -- float)")

	b.reg("just", "(a -- Maybe[a])")
	b.keeps(t.name(res.names.Intern("just")))
	t.setName(res.names.Intern("none"), []coreSig{{outs: []TypeId{ar.MakeMaybeEnum(TidBottom)}}})
	b.reg("null", "( -- null)")
	b.reg("exit", "(int -- never)")
	b.reg("deepCopy", "(a -- new a)")

	b.reg("wl", "(str | int -- )")
	b.reg("wle", "(str | int -- )")
	b.reg("w", "(str | int | bytes -- )")
	b.reg("we", "(str | int | bytes -- )")
	b.reg("wln", "( -- )")

	b.reg("toInt", "(str -- Maybe[int])", "(float -- int)", "(int -- int)")
	b.reg("toFloat", "(str -- Maybe[float])", "(int -- float)", "(float -- float)")
	b.reg("len", "([a] -- int)", "(str | path -- int)")
	b.reg("append", "([a] a -- [a])")
	b.reg("reverse", "([a] -- [a])", "(str -- str)")
	b.newList("reverse")

	b.reg("lines", "(str -- new [str])")
	b.reg("split", "(str str -- new [str])")
	b.reg("join", "([str] str -- str)")
	for _, name := range []string{"trim", "trimStart", "trimEnd", "upper", "lower", "title"} {
		b.reg(name, "(str -- str)")
	}
	// Words that run a quote on a child stack, once per element.
	b.reg("each", "([a] (a -- ) -- )")
	b.reg("map", "([a] (a -- b) -- [b])")
	b.newList("map")
	b.reg("filter", "([a] (a -- bool) -- [a])")
	b.newList("filter")
	for _, name := range []string{"each", "map", "filter"} {
		b.child(name)
	}

	b.reg("and", "(bool bool -- bool)")
	b.reg("or", "(bool bool -- bool)")
	b.reg("readFile", "(str | path -- str)")

	// Uses of these words the table does not cover yet.
	t.partialToken = map[TokenType]string{
		LESSTHAN:    "redirects",
		GREATERTHAN: "redirects",
		ASTERISK:    "command captures",
		QUESTION:    "running commands",
	}
	t.partialName = map[NameId]string{
		res.names.Intern("and"): "'and' with a quote",
		res.names.Intern("or"):  "'or' with a quote",
		res.names.Intern("len"): "the grid forms of 'len'",
		res.names.Intern("map"): "the Maybe, dict and grid forms of 'map'",
		res.names.Intern("filter"): "the dict and grid forms of 'filter'",
		res.names.Intern("each"): "the dict and grid forms of 'each'",
	}
	b.reg("parseJson", "(str | path | bytes -- new Json)")
	return t
}
