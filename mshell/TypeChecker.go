package main

// Phase 2 of the type checker: stack simulation and applySig over a small
// primitive-only token stream.
//
// This is a deliberately narrow vertical slice. The Checker consumes a
// sequence of Tokens directly (no parse-tree pass yet), recognizes integer
// and boolean literals, and dispatches arithmetic/comparison operators
// through builtinSigsByToken. Composite types, generics, branching,
// overloading, and Main.go integration land in later phases per
// ai/type_checker.md.

// TypeStack is the checker's simulated runtime stack. The top of the stack
// is items[len(items)-1]; push/pop are the slice-tail operations. The same
// TypeStack is reused across a checking session — Reset() clears it
// between top-level items.
type TypeStack struct {
	items []TypeId
}

// Push adds id to the top of the stack.
func (s *TypeStack) Push(id TypeId) {
	s.items = append(s.items, id)
}

// Len returns the number of items currently on the stack.
func (s *TypeStack) Len() int {
	return len(s.items)
}

// Top returns the top of the stack without popping. Caller must check Len()
// first; calling Top on an empty stack panics.
func (s *TypeStack) Top() TypeId {
	return s.items[len(s.items)-1]
}

// Reset truncates the stack to zero length, retaining the backing array.
func (s *TypeStack) Reset() {
	s.items = s.items[:0]
}

// Snapshot returns a copy of the current stack contents. Used for branch
// reconciliation in later phases; defined here so tests can inspect state.
func (s *TypeStack) Snapshot() []TypeId {
	out := make([]TypeId, len(s.items))
	copy(out, s.items)
	return out
}

// VarEnv is a per-scope mapping from bound variable name to its current type.
// Phase 2 keeps this here as a placeholder; bindings are not yet exercised.
type VarEnv struct {
	bound map[NameId]TypeId
}

// NewVarEnv constructs an empty environment.
func NewVarEnv() VarEnv {
	return VarEnv{bound: make(map[NameId]TypeId, 8)}
}

// FnContext is the per-function checking context. In Phase 2 only the
// declared signature matters; Phase 2 of the deferred effect work adds
// declared/inferred fail and pure tracking.
type FnContext struct {
	Sig       QuoteSig
	SawReturn bool
}

// Checker is the top-level type-checking session. It owns the arena, name
// table, and accumulated errors. Tokens are fed in via Check / CheckTokens.
type Checker struct {
	arena *TypeArena
	names *NameTable

	stack  TypeStack
	vars   VarEnv
	subst  Substitution
	errors []TypeError
	diverged bool

	builtins     map[TokenType][]QuoteSig
	nameBuiltins map[NameId][]QuoteSig

	// typeEnv holds named type declarations (Phase 5). Built-in / reserved
	// type names are NOT stored here — they are recognized directly.
	typeEnv map[NameId]TypeId

	// Quote-body inference state (Phase 7). When inferring is true,
	// applySig responds to stack underflow by synthesizing fresh type
	// variables instead of reporting an error; those vars accumulate
	// into inferInputs in caller-stack order (deepest first).
	inferring   bool
	inferInputs []TypeId

	// listDepth tracks how deeply nested we are inside list literals
	// (`[...]`). Inside lists, mshell allows bare literals as strings
	// — that's how shell pipelines like `[sh -c "echo hi" ;]` stay
	// readable without quoting every argv token. A LITERAL that
	// resolves to no builtin/def/var is taken as `str` instead of
	// being reported as an unknown identifier when listDepth > 0.
	listDepth int

	currentFn *FnContext
}

// NewChecker constructs a fresh checker with the given arena and name table.
// The builtin sig table is built once here.
func NewChecker(arena *TypeArena, names *NameTable) *Checker {
	return &Checker{
		arena:        arena,
		names:        names,
		vars:         NewVarEnv(),
		builtins:     builtinSigsByToken(arena, names),
		nameBuiltins: builtinSigsByName(arena, names),
	}
}

// Errors returns the accumulated errors, in the order they were reported.
func (c *Checker) Errors() []TypeError {
	return c.errors
}

// Stack returns the current TypeStack pointer for inspection (mostly tests).
func (c *Checker) Stack() *TypeStack {
	return &c.stack
}

// CheckTokens runs the Phase-2 checker over a flat token stream. Returns
// after consuming all tokens regardless of errors — every error is
// collected for batch reporting.
func (c *Checker) CheckTokens(tokens []Token) {
	for _, tok := range tokens {
		c.checkOne(tok)
	}
}

// checkOne dispatches a single token. Literals push a primitive; tokens
// in the builtin table apply the corresponding sig. Anything else is
// reported as an unknown identifier (Phase 2 surface is intentionally tiny).
func (c *Checker) checkOne(tok Token) {
	if c.diverged {
		return
	}
	switch tok.Type {
	case INTEGER:
		c.stack.Push(TidInt)
		return
	case FLOAT:
		c.stack.Push(TidFloat)
		return
	case STRING, SINGLEQUOTESTRING:
		c.stack.Push(TidStr)
		return
	case TRUE, FALSE:
		c.stack.Push(TidBool)
		return
	case FORMATSTRING:
		// `$"...{expr}..."` evaluates each `{...}` block as a tiny
		// program and concatenates the result. Type-check each
		// block on a fresh sub-stack that inherits the current
		// VarEnv, requiring the block to produce exactly one
		// value. The format string itself always pushes `str`.
		c.checkFormatStringInterpolations(tok)
		c.stack.Push(TidStr)
		return
	case PATH:
		c.stack.Push(TidPath)
		return
	case DATETIME:
		c.stack.Push(TidDateTime)
		return
	case INTERPRET:
		// Single-char `x`: pops a quote and applies it to the
		// stack. Row-polymorphic — its effect depends on the
		// quote's sig, not a fixed shape — so we resolve the
		// quote's inputs/outputs at this call site instead of
		// going through the builtin table.
		if c.stack.Len() == 0 {
			c.errors = append(c.errors, TypeError{
				Kind: TErrStackUnderflow,
				Pos:  tok,
				Hint: "interpret needs a quote on top",
			})
			c.stack.Push(c.subst.FreshVar(c.arena))
			return
		}
		top := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
		topNode := c.arena.Node(top)
		if topNode.Kind == TKOverloadedQuote {
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			c.resolveAndApply(c.arena.overloadedQuoteSigs[topNode.Extra], tok)
			return
		}
		if topNode.Kind != TKQuote {
			c.errors = append(c.errors, TypeError{
				Kind:     TErrTypeMismatch,
				Pos:      tok,
				Actual:   top,
				Expected: c.arena.MakeQuote(QuoteSig{}),
				Hint:     "interpret expected a quote on top",
			})
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			return
		}
		c.stack.items = c.stack.items[:c.stack.Len()-1]
		sig := c.arena.QuoteSig(top)
		c.applySig(sig, tok)
		if sig.Diverges {
			c.diverged = true
		}
		return
	case ENVRETREIVE:
		// `$VAR`: read an environment variable. Always pushes str
		// (runtime errors if unset; we don't model that effect).
		c.stack.Push(TidStr)
		return
	case ENVCHECK:
		// `$VAR?`: check whether an env var is set, push bool.
		c.stack.Push(TidBool)
		return
	case ENVSTORE:
		// `$VAR!`: pops a string-castable value and exports it.
		if c.stack.Len() == 0 {
			c.errors = append(c.errors, TypeError{
				Kind: TErrStackUnderflow,
				Pos:  tok,
				Hint: "env-store needs a value",
			})
			return
		}
		c.stack.items = c.stack.items[:c.stack.Len()-1]
		return
	case VARRETRIEVE:
		// `@name`: push the bound variable's type. Unknown name
		// is reported (a `@name` reference assumes prior storage
		// via `name!`). On miss, push a fresh var so the rest of
		// the walk has something coherent to operate on.
		name := tok.Lexeme
		if len(name) > 0 && name[0] == '@' {
			name = name[1:]
		}
		nameId := c.names.Intern(name)
		if t, ok := c.vars.bound[nameId]; ok {
			c.stack.Push(t)
		} else {
			c.errors = append(c.errors, TypeError{
				Kind: TErrUnknownIdentifier,
				Pos:  tok,
				Name: tok.Lexeme,
			})
			c.stack.Push(c.subst.FreshVar(c.arena))
		}
		return
	}

	if sigs, ok := c.builtins[tok.Type]; ok {
		switch tok.Type {
		case IFF:
			if c.tryIff(tok) {
				return
			}
		case BREAK, CONTINUE:
			c.diverged = true
			return
		}
		if tok.Type == LOOP && c.tryLoop(tok) {
			return
		}
		if tok.Type == PIPE && c.stack.Len() > 0 {
			top := c.subst.Apply(c.arena, c.stack.Top())
			if c.arena.Kind(top) == TKBrand {
				return
			}
		}
		if c.tryQuoteRedirect(tok) {
			return
		}
		c.resolveAndApply(sigs, tok)
		return
	}

	if tok.Type == LITERAL {
		if tok.Lexeme == "return" && c.tryReturn(tok) {
			return
		}
		if tok.Lexeme == "append" && c.tryAppend() {
			return
		}
		if tok.Lexeme == "get" && c.tryGet(tok) {
			return
		}
		if tok.Lexeme == "zipPack" {
			need := 2
			if c.stack.Len() < need {
				need = c.stack.Len()
			}
			c.stack.items = c.stack.items[:c.stack.Len()-need]
			return
		}
		if tok.Lexeme == "foldl" {
			if c.stack.Len() < 3 {
				c.errors = append(c.errors, TypeError{Kind: TErrStackUnderflow, Pos: tok, Hint: "foldl"})
				return
			}
			acc := c.stack.items[c.stack.Len()-2]
			c.stack.items = c.stack.items[:c.stack.Len()-3]
			c.stack.Push(acc)
			return
		}
		if tok.Lexeme == "join" && c.tryGridJoin(tok) {
			return
		}
		nameId := c.names.Intern(tok.Lexeme)
		if sigs, ok := c.nameBuiltins[nameId]; ok {
			c.resolveAndApply(sigs, tok)
			return
		}
	}

	// Inside a list literal, an unrecognized LITERAL is the
	// shell-style "argv token" — it pushes itself as a string.
	// This is the only place mshell allows bare literals as
	// values, and it's load-bearing for `[cmd args] ;`-style
	// pipelines where forcing the user to quote every word
	// would defeat the point.
	if c.listDepth > 0 && tok.Type == LITERAL {
		c.stack.Push(TidStr)
		return
	}

	c.errors = append(c.errors, TypeError{
		Kind: TErrUnknownIdentifier,
		Pos:  tok,
		Name: tok.Lexeme,
	})
	// Push a fresh var so downstream walks see *something* in
	// place of the unknown identifier's effect. This mirrors the
	// resilience strategy elsewhere (VARRETRIEVE on miss,
	// no-overload-recovery in resolveAndApply): one error per
	// site, no cascading "stack underflow" / "no matching
	// overload" diagnostics that would only restate the original
	// gap.
	c.stack.Push(c.subst.FreshVar(c.arena))
}

func (c *Checker) tryReturn(tok Token) bool {
	if c.currentFn == nil {
		return false
	}
	c.currentFn.SawReturn = true
	expected := c.currentFn.Sig.Outputs
	if c.stack.Len() < len(expected) {
		c.errors = append(c.errors, TypeError{
			Kind: TErrStackUnderflow,
			Pos:  tok,
			Hint: "return",
		})
		c.diverged = true
		return true
	}
	base := c.stack.Len() - len(expected)
	for i, want := range expected {
		got := c.stack.items[base+i]
		if !c.unify(got, want) {
			c.errors = append(c.errors, TypeError{
				Kind:     TErrTypeMismatch,
				Pos:      tok,
				Expected: want,
				Actual:   got,
				ArgIndex: i,
			})
		}
	}
	c.stack.items = c.stack.items[:base]
	c.diverged = true
	return true
}

func (c *Checker) tryAppend() bool {
	if c.stack.Len() >= 2 {
		top := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
		second := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
		if c.arena.Kind(second) == TKList {
			elem := TypeId(c.arena.Node(second).A)
			c.stack.items = c.stack.items[:c.stack.Len()-2]
			c.stack.Push(c.arena.MakeList(c.arena.MakeUnion([]TypeId{elem, top}, 0)))
			return true
		}
		if c.arena.Kind(top) == TKList {
			elem := TypeId(c.arena.Node(top).A)
			c.stack.items = c.stack.items[:c.stack.Len()-2]
			c.stack.Push(c.arena.MakeList(c.arena.MakeUnion([]TypeId{elem, second}, 0)))
			return true
		}
	}

	if c.inferring && c.stack.Len() == 1 {
		list := c.subst.Apply(c.arena, c.stack.Top())
		node := c.arena.Node(list)
		if node.Kind != TKList {
			return false
		}
		item := c.subst.FreshVar(c.arena)
		c.inferInputs = append([]TypeId{item}, c.inferInputs...)
		elem := TypeId(node.A)
		c.stack.items = c.stack.items[:0]
		c.stack.Push(c.arena.MakeList(c.arena.MakeUnion([]TypeId{elem, item}, 0)))
		return true
	}

	return false
}

func (c *Checker) tryGet(tok Token) bool {
	if c.stack.Len() < 2 {
		return false
	}
	key := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
	receiver := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
	if !c.unify(key, TidStr) {
		return false
	}

	var value TypeId
	node := c.arena.Node(receiver)
	switch node.Kind {
	case TKGridRow, TKShape:
		value = c.lookupGetterValueType(receiver, c.names.Intern(""))
	case TKDict:
		value = TypeId(node.B)
	case TKVar:
		value = c.subst.FreshVar(c.arena)
		if !c.unify(receiver, c.arena.MakeDict(TidStr, value)) {
			return false
		}
	default:
		return false
	}

	c.stack.items = c.stack.items[:c.stack.Len()-2]
	c.stack.Push(c.arena.MakeMaybe(value))
	return true
}

func (c *Checker) tryIff(tok Token) bool {
	if c.stack.Len() < 2 {
		return false
	}
	falseQuote := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
	falseNode := c.arena.Node(falseQuote)
	if falseNode.Kind != TKQuote {
		return false
	}
	second := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
	secondNode := c.arena.Node(second)

	var trueQuote TypeId
	var hasFalse bool
	var cond TypeId
	var baseLen int
	if secondNode.Kind == TKQuote {
		if c.stack.Len() < 3 {
			return false
		}
		hasFalse = true
		trueQuote = second
		cond = c.stack.items[c.stack.Len()-3]
		baseLen = c.stack.Len() - 3
	} else {
		trueQuote = falseQuote
		cond = c.stack.items[c.stack.Len()-2]
		baseLen = c.stack.Len() - 2
	}

	if !c.isBoolOrInt(cond) {
		c.errors = append(c.errors, TypeError{
			Kind:     TErrTypeMismatch,
			Pos:      tok,
			Expected: TidBool,
			Actual:   cond,
			ArgIndex: 0,
		})
	}

	c.stack.items = c.stack.items[:baseLen]
	snap := c.Snapshot()

	c.applyQuoteArm(trueQuote, tok)
	arms := []BranchArm{c.CaptureArm(c.diverged)}

	if hasFalse {
		c.Fork(snap)
		c.applyQuoteArm(falseQuote, tok)
		arms = append(arms, c.CaptureArm(c.diverged))
	} else {
		c.Fork(snap)
		arms = append(arms, c.CaptureArm(false))
	}

	c.ReconcileArms(arms, tok)
	return true
}

func (c *Checker) applyQuoteArm(quote TypeId, tok Token) {
	if c.arena.Kind(quote) != TKQuote {
		c.errors = append(c.errors, TypeError{
			Kind:     TErrTypeMismatch,
			Pos:      tok,
			Expected: c.arena.MakeQuote(QuoteSig{}),
			Actual:   quote,
			Hint:     "iff branch",
		})
		return
	}
	sig := c.arena.QuoteSig(quote)
	c.applySig(sig, tok)
	if sig.Diverges {
		c.diverged = true
	}
}

func (c *Checker) tryLoop(tok Token) bool {
	if c.stack.Len() == 0 {
		return false
	}
	top := c.subst.Apply(c.arena, c.stack.Top())
	if c.arena.Kind(top) != TKQuote {
		return false
	}
	sig := c.arena.QuoteSig(top)
	if len(sig.Inputs) != len(sig.Outputs) {
		return false
	}
	for i := range sig.Inputs {
		if !c.unify(sig.Inputs[i], sig.Outputs[i]) {
			c.errors = append(c.errors, TypeError{
				Kind:     TErrTypeMismatch,
				Pos:      tok,
				Expected: sig.Inputs[i],
				Actual:   sig.Outputs[i],
				ArgIndex: i,
				Hint:     "loop quote must preserve stack types",
			})
			return true
		}
	}
	c.stack.items = c.stack.items[:c.stack.Len()-1]
	return true
}

func (c *Checker) tryGridJoin(tok Token) bool {
	if c.stack.Len() < 4 {
		return false
	}
	top := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
	second := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
	left := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-4])
	right := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-3])
	if c.arena.Kind(top) != TKQuote || c.arena.Kind(second) != TKQuote {
		return false
	}
	leftKind := c.arena.Kind(left)
	rightKind := c.arena.Kind(right)
	if (leftKind != TKGrid && leftKind != TKGridView) || (rightKind != TKGrid && rightKind != TKGridView) {
		return false
	}
	c.stack.items = c.stack.items[:c.stack.Len()-4]
	c.stack.Push(c.arena.MakeGrid(0))
	return true
}

func (c *Checker) tryQuoteRedirect(tok Token) bool {
	switch tok.Type {
	case LESSTHAN, GREATERTHAN, STDERRREDIRECT, STDERRAPPEND,
		STDOUTANDSTDERRREDIRECT, STDOUTANDSTDERRAPPEND, INPLACEREDIRECT, STDAPPEND:
	default:
		return false
	}
	if c.stack.Len() < 2 {
		return false
	}
	target := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
	if target != TidStr && target != TidPath && target != TidBytes {
		return false
	}
	quote := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
	if c.arena.Kind(quote) != TKQuote {
		return false
	}
	c.stack.items = c.stack.items[:c.stack.Len()-1]
	return true
}

// applySig is the hot path. It validates arity, unifies each input against
// the corresponding stack slot, pops the inputs, and pushes the outputs.
//
// On stack underflow, no inputs are popped and no outputs pushed — the
// stack is left untouched so subsequent checks have something coherent to
// continue against. On a type mismatch, the error is recorded but the sig
// is still applied (inputs popped, outputs pushed) so cascading errors are
// reduced.
func (c *Checker) applySig(sig QuoteSig, callSite Token) {
	sig = c.Instantiate(sig)
	if len(c.stack.items) < len(sig.Inputs) {
		if c.inferring {
			// Synthesize fresh vars at the bottom of the stack to satisfy
			// the demand. Each synthesized var also lands at the front of
			// inferInputs because the deepest-needed var is the first
			// item the quote's caller must supply (bottom of caller stack).
			need := len(sig.Inputs) - len(c.stack.items)
			extra := make([]TypeId, need)
			for i := 0; i < need; i++ {
				extra[i] = c.subst.FreshVar(c.arena)
			}
			c.inferInputs = append(append([]TypeId(nil), extra...), c.inferInputs...)
			c.stack.items = append(append([]TypeId(nil), extra...), c.stack.items...)
		} else {
			c.errors = append(c.errors, TypeError{
				Kind: TErrStackUnderflow,
				Pos:  callSite,
			})
			return
		}
	}
	base := len(c.stack.items) - len(sig.Inputs)
	for i, want := range sig.Inputs {
		got := c.stack.items[base+i]
		if !c.unify(got, want) {
			c.errors = append(c.errors, TypeError{
				Kind:     TErrTypeMismatch,
				Pos:      callSite,
				Expected: want,
				Actual:   got,
				ArgIndex: i,
			})
		}
	}
	c.stack.items = c.stack.items[:base]
	for _, out := range sig.Outputs {
		c.stack.items = append(c.stack.items, c.subst.Apply(c.arena, out))
	}
}

// unify checks whether `got` is acceptable where `want` is expected and
// records any TKVar bindings into the checker's substitution. Phase 6
// added the variable cases: each side is first resolved through the
// current substitution, and any remaining TKVar gets bound (with an
// occurs check) to the other side.
//
// Composite cases (Phase 3) recurse via c.unify so each inner unification
// also goes through Apply; this means a binding made deep inside a
// composite immediately affects later sibling slots in the same call.
//
// `got` is the actual stack type; `want` is the sig's declared input.
// Acceptance is asymmetric where the rules are asymmetric (shape width
// subtyping, union subset). Variable binding is symmetric — either side
// is willing to be bound to the other.
//
// TidBottom unifies with anything — divergent operations (`exit`,
// infinite loops; Phase-2-of-effects: propagated `fail`) produce it.
func (c *Checker) unify(got, want TypeId) bool {
	got = c.subst.Apply(c.arena, got)
	want = c.subst.Apply(c.arena, want)
	if got == want {
		return true
	}
	if got == TidBottom || want == TidBottom {
		return true
	}

	gn := c.arena.Node(got)
	wn := c.arena.Node(want)

	// Variable cases: bind whichever side is still a free variable. After
	// Apply above, a TKVar here is guaranteed unbound.
	if gn.Kind == TKVar {
		return c.subst.Bind(c.arena, TypeVarId(gn.A), want)
	}
	if wn.Kind == TKVar {
		return c.subst.Bind(c.arena, TypeVarId(wn.A), got)
	}

	if gn.Kind != wn.Kind {
		if gn.Kind == TKOverloadedQuote && wn.Kind == TKQuote {
			return c.unifyOverloadedQuoteToQuote(gn, want)
		}
		if gn.Kind == TKQuote && wn.Kind == TKOverloadedQuote {
			return c.unifyOverloadedQuoteToQuote(wn, got)
		}
		if gn.Kind == TKDict && wn.Kind == TKShape {
			return c.unifyDictToShape(gn, wn)
		}
		if gn.Kind == TKShape && wn.Kind == TKDict {
			return c.unifyShapeToDict(gn, wn)
		}
		if gn.Kind == TKUnion {
			return c.unifyUnionToType(gn, want)
		}
		if wn.Kind == TKUnion {
			return c.unifyTypeToUnion(got, wn)
		}
		return false
	}

	switch gn.Kind {
	case TKMaybe, TKList:
		return c.unify(TypeId(gn.A), TypeId(wn.A))
	case TKDict:
		return c.unify(TypeId(gn.A), TypeId(wn.A)) && c.unify(TypeId(gn.B), TypeId(wn.B))
	case TKShape:
		return c.unifyShape(gn, wn)
	case TKUnion:
		return c.unifyUnion(gn, wn)
	case TKBrand:
		// Nominal wrapper: brand ids must match, but the wrapped
		// type can still contain generics that need normal structural
		// unification at the call site.
		return gn.A == wn.A && c.unify(TypeId(gn.B), TypeId(wn.B))
	case TKQuote:
		return c.unifyQuote(gn, wn)
	case TKOverloadedQuote:
		return false
	case TKGrid, TKGridView, TKGridRow:
		// Phase-3 grids are opaque. Equality-by-id is the only way two grid
		// types match; if we got here with same kind but different ids,
		// they are different schemas (or one tracks schema and the other
		// doesn't). Phase-3 treats schema-unknown as compatible with any
		// schema of the same kind so opaque builtins still type-check.
		if gn.Extra == 0 || wn.Extra == 0 {
			return true
		}
		return false
	}
	return false
}

func (c *Checker) unifyOverloadedQuoteToQuote(overNode TypeNode, quote TypeId) bool {
	candidates := c.arena.overloadedQuoteSigs[overNode.Extra]
	cp := c.subst.Checkpoint()
	for _, cand := range candidates {
		c.subst.Rollback(cp)
		inst := c.Instantiate(cand)
		if c.unify(c.arena.MakeQuote(inst), quote) {
			return true
		}
	}
	c.subst.Rollback(cp)
	return false
}

func (c *Checker) unifyDictToShape(gn, wn TypeNode) bool {
	if !c.unify(TypeId(gn.A), TidStr) {
		return false
	}
	value := TypeId(gn.B)
	for _, field := range c.arena.shapeFields[wn.Extra] {
		if !c.unify(value, field.Type) {
			return false
		}
	}
	return true
}

func (c *Checker) unifyShapeToDict(gn, wn TypeNode) bool {
	if !c.unify(TidStr, TypeId(wn.A)) {
		return false
	}
	fields := c.arena.shapeFields[gn.Extra]
	if len(fields) == 0 {
		return true
	}
	values := make([]TypeId, 0, len(fields))
	for _, field := range fields {
		values = append(values, field.Type)
	}
	return c.unify(c.arena.MakeUnion(values, 0), TypeId(wn.B))
}

func (c *Checker) unifyUnionToType(gn TypeNode, want TypeId) bool {
	for _, arm := range c.arena.unionMembers[gn.Extra] {
		if !c.unify(arm, want) {
			return false
		}
	}
	return true
}

func (c *Checker) unifyTypeToUnion(got TypeId, wn TypeNode) bool {
	for _, arm := range c.arena.unionMembers[wn.Extra] {
		cp := c.subst.Checkpoint()
		if c.unify(got, arm) {
			return true
		}
		c.subst.Rollback(cp)
	}
	return false
}

// unifyShape implements width subtyping: every field of `want` must appear
// in `got` with a unifiable type. Extra fields in `got` are allowed.
// Both field lists are pre-sorted by NameId (see normalizeShapeFields), so
// the merge is linear.
func (c *Checker) unifyShape(gn, wn TypeNode) bool {
	gFields := c.arena.shapeFields[gn.Extra]
	wFields := c.arena.shapeFields[wn.Extra]
	gi := 0
	for _, wf := range wFields {
		// Advance gi to the first field with name >= wf.Name.
		for gi < len(gFields) && gFields[gi].Name < wf.Name {
			gi++
		}
		if gi == len(gFields) || gFields[gi].Name != wf.Name {
			return false
		}
		if !c.unify(gFields[gi].Type, wf.Type) {
			return false
		}
		gi++
	}
	return true
}

// unifyUnion implements the subset rule: every arm of `got` must unify
// with some arm of `want`. Both arm lists are sorted/deduped (see
// flattenAndCanonicalizeUnion). Brand ids must match — a branded union
// is nominal and never collapses with a different brand or with the
// unbranded form.
func (c *Checker) unifyUnion(gn, wn TypeNode) bool {
	if gn.A != wn.A {
		return false
	}
	gArms := c.arena.unionMembers[gn.Extra]
	wArms := c.arena.unionMembers[wn.Extra]
	for _, ga := range gArms {
		matched := false
		for _, wa := range wArms {
			if c.unify(ga, wa) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// unifyQuote unifies two quote signatures. In Phase 3 this is exact-arity,
// pairwise unification of inputs and outputs. Fail/Pure flags must match
// directly (always default in V1 — Phase-2-of-effects revisits this).
func (c *Checker) unifyQuote(gn, wn TypeNode) bool {
	gs := c.arena.quoteSigs[gn.Extra]
	ws := c.arena.quoteSigs[wn.Extra]
	if len(gs.Inputs) != len(ws.Inputs) || len(gs.Outputs) != len(ws.Outputs) {
		return false
	}
	if gs.Fail != ws.Fail || gs.Pure != ws.Pure || gs.Diverges != ws.Diverges {
		return false
	}
	for i := range gs.Inputs {
		if !c.unify(gs.Inputs[i], ws.Inputs[i]) {
			return false
		}
	}
	for i := range gs.Outputs {
		if !c.unify(gs.Outputs[i], ws.Outputs[i]) {
			return false
		}
	}
	return true
}
