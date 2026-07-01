package main

import "fmt"

// Core token-level checking: the simulated type stack, the variable
// environment, per-token dispatch (checkOne), signature application
// (applySig), and structural unification (unify). The parse-tree walk
// lives in TypeCheckProgram.go and the branching driver in TypeQuote.go;
// see ai/type_checker.md for the overall design.

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

// VarEnv tracks bound variables per scope. Variables fall into two
// states:
//
//   - bound: definitely set on every path that leads here. `@name`
//     reads the stored TypeId.
//
//   - maybeBound: set on some paths but not others. Branch
//     reconciliation lifts names that aren't bound in every live arm
//     into this map (keyed by name, value is the unioned type from
//     whichever arms did bind it). Reading `@name` while it's in this
//     map is reported as an error — the user should restructure so
//     the binding is unconditional, since mshell has no probe
//     operator.
//
// A subsequent unconditional VARSTORE for the same name removes the
// maybeBound entry (the store makes the binding definite again).
type VarEnv struct {
	bound      map[NameId]TypeId
	maybeBound map[NameId]TypeId
}

// NewVarEnv constructs an empty environment.
func NewVarEnv() VarEnv {
	return VarEnv{
		bound:      make(map[NameId]TypeId, 8),
		maybeBound: make(map[NameId]TypeId, 0),
	}
}

// FnContext is the per-function checking context: the declared signature
// a def body is checked against, and whether the body used `return`.
type FnContext struct {
	Sig       QuoteSig
	SawReturn bool
}

// Checker is the top-level type-checking session. It owns the arena, name
// table, and accumulated errors. Programs enter through CheckProgram
// (parse-tree walk) or CheckTokens (flat token stream, used by tests).
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

	// branchSpawn is populated by multi-dispatch sites (resolveAndApply
	// with multiple viable candidates, prefix-quote handlers, INTERPRET
	// on an overloaded quote). Each entry is one alternative outcome of
	// the current step. tryBranchStep clears this before invoking the
	// step and reads it after: a non-empty result fans out the current
	// branch into all the spawned alternatives instead of capturing a
	// single deterministic continuation.
	branchSpawn []quoteBranch

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

// CheckTokens runs the checker over a flat token stream through the
// branching driver, starting from the checker's current state. Surviving
// branches are joined back into a single live state. Used by lower-level
// tests; the program path enters through CheckProgram.
func (c *Checker) CheckTokens(tokens []Token) {
	items := make([]MShellParseItem, len(tokens))
	for i, tok := range tokens {
		items[i] = tok
	}
	c.walkJoined(items)
}

// checkOne dispatches a single token. Literals push a primitive; tokens
// in the builtin table apply the corresponding sig. Anything else is
// reported as an unknown identifier (Phase 2 surface is intentionally tiny).
// flagAlwaysFailUnwrap emits a non-fatal hint when `?` is applied to a value
// that can only be None — a Maybe whose payload is bottom (uninhabited), which
// a getter for an undeclared concrete-shape field produces. The hint is placed
// on the `?` token, where the runtime failure occurs, and works regardless of
// how the value reached the top of stack (it is a property of the operand's
// type, so it survives variable bindings). Never affects typing.
func (c *Checker) flagAlwaysFailUnwrap(tok Token) {
	if c.inferring || c.stack.Len() == 0 {
		return
	}
	top := c.subst.Apply(c.arena, c.stack.Top())
	n := c.arena.Node(top)
	if n.Kind != TKMaybe {
		return
	}
	if c.subst.Apply(c.arena, TypeId(n.A)) != TidBottom {
		return
	}
	c.errors = append(c.errors, TypeError{
		Severity: SeverityInfo,
		Kind:     TErrUnwrapAlwaysFails,
		Pos:      tok,
		Hint:     "'?' unwraps a value that can only be None (e.g. a getter for a field the shape does not declare); this will fail at runtime",
	})
}

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
		// Carry the known value as a `str` refinement so a later `get` (or the
		// `:name` getter) can resolve a shape field by key. It behaves as `str`
		// everywhere else — unify and every container constructor widen it.
		if v, ok := c.stringLiteralValue(tok); ok {
			c.stack.Push(c.arena.MakeStrLit(c.names.Intern(v)))
		} else {
			c.stack.Push(TidStr)
		}
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
		// `@name`: push the bound variable's type. Three cases:
		//   - bound on every path leading here: push the stored
		//     TypeId.
		//   - maybeBound (set on some paths, not others): report
		//     TErrMaybeUnset and push the stored TypeId so
		//     downstream checking still has a coherent stack.
		//   - unknown: report TErrUnknownIdentifier and push a
		//     fresh var.
		name := tok.Lexeme
		if len(name) > 0 && name[0] == '@' {
			name = name[1:]
		}
		nameId := c.names.Intern(name)
		if t, ok := c.vars.bound[nameId]; ok {
			c.stack.Push(t)
		} else if t, ok := c.vars.maybeBound[nameId]; ok {
			c.errors = append(c.errors, TypeError{
				Kind: TErrMaybeUnset,
				Pos:  tok,
				Name: tok.Lexeme,
			})
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
		case QUESTION:
			// `?` is fromJust. If its operand is a Maybe whose payload is
			// uninhabited (Maybe[bottom] — e.g. a getter for a field a
			// concrete shape does not declare), the value can only be None,
			// so this unwrap is statically guaranteed to fail at runtime.
			// Flag it at the `?` (where the failure happens) as a non-fatal
			// hint, then fall through to normal `?` typing. The bottom
			// payload flows through bindings, so this fires even when the
			// getter and the `?` are far apart.
			c.flagAlwaysFailUnwrap(tok)
		}
		if tok.Type == LOOP && c.tryLoop(tok) {
			return
		}
		if tok.Type == PIPE && c.stack.Len() > 0 {
			top := c.subst.Apply(c.arena, c.stack.Top())
			if c.arena.Kind(top) == TKCommand {
				return
			}
		}
		if c.tryRedirect(tok) {
			return
		}
		c.resolveAndApply(sigs, tok)
		return
	}

	if tok.Type == LITERAL {
		if tok.Lexeme == "dbg" {
			c.emitDebugDump(tok)
			return
		}
		if tok.Lexeme == "return" && c.tryReturn(tok) {
			return
		}
		if tok.Lexeme == "join" && c.tryGridJoin() {
			return
		}
		if tok.Lexeme == "get" && c.tryGetLiteralKey() {
			return
		}
		if tok.Lexeme == "pivot" && c.tryPivot(tok) {
			return
		}
		if c.tryRejectPathWrite(tok) {
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

func (c *Checker) tryRejectPathWrite(tok Token) bool {
	switch tok.Lexeme {
	case "wl", "wle", "w", "we":
	default:
		return false
	}
	if c.stack.Len() == 0 {
		return false
	}
	top := c.subst.Apply(c.arena, c.stack.Top())
	if top != TidPath {
		return false
	}
	c.errors = append(c.errors, TypeError{
		Kind:     TErrTypeMismatch,
		Pos:      tok,
		Expected: TidStr,
		Actual:   top,
		Hint:     "write does not accept path; use str first",
	})
	c.stack.items = c.stack.items[:c.stack.Len()-1]
	return true
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
	// An overloaded quote in the true-branch position needs per-arm
	// dispatch; treating it as the one-arm form's condition would
	// misread the program. Defer to the table's iff sigs, where
	// overloaded-quote operand expansion explores the arms and the
	// surviving choices fan out through the branching driver. (The
	// table arms only cover thunk-shaped branches; folding full arm
	// dispatch into tryIff is the upgrade path if that bites.)
	if secondNode.Kind == TKOverloadedQuote {
		return false
	}

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
	allowedBindings := c.commonIffBindings(trueQuote, falseQuote, hasFalse)

	entry := c.captureBranch()
	var armBranches []quoteBranch
	var armLabels []string
	runArm := func(quote TypeId, label string, apply bool) {
		c.loadBranch(entry)
		if apply {
			c.applyQuoteArm(quote, tok, allowedBindings)
		}
		armBranches = append(armBranches, c.captureBranch())
		armLabels = append(armLabels, label)
	}

	runArm(trueQuote, "true", true)
	if hasFalse {
		runArm(falseQuote, "false", true)
	} else {
		// Implicit do-nothing arm: with one quote, the false case
		// leaves the entry state untouched.
		runArm(TidNothing, "no-arm", false)
	}

	c.reconcileArmBranches(armBranches, armLabels, entry, tok)
	return true
}

func (c *Checker) applyQuoteArm(quote TypeId, tok Token, allowedBindings map[NameId]struct{}) {
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
	for name, t := range sig.Bindings {
		if _, ok := allowedBindings[name]; ok {
			c.vars.bound[name] = c.subst.Apply(c.arena, t)
		}
	}
	if sig.Diverges {
		c.diverged = true
	}
}

func (c *Checker) commonIffBindings(trueQuote, falseQuote TypeId, hasFalse bool) map[NameId]struct{} {
	out := make(map[NameId]struct{})
	if !hasFalse || c.arena.Kind(trueQuote) != TKQuote || c.arena.Kind(falseQuote) != TKQuote {
		return out
	}
	trueBindings := c.arena.QuoteSig(trueQuote).Bindings
	falseBindings := c.arena.QuoteSig(falseQuote).Bindings
	for name := range trueBindings {
		if _, ok := falseBindings[name]; ok {
			out[name] = struct{}{}
		}
	}
	return out
}

// instantiatedQuoteCandidates returns the Instantiate-d candidate
// sigs for a stack-resident quote value. Single-sig TKQuote yields
// one entry; TKOverloadedQuote yields one per stored sig. Other
// kinds return nil so try* helpers know they shouldn't engage.
//
// This is the protocol every consumer of a stored quote sig must
// obey: Instantiate before unification or substitution lookups,
// because the stored sig's TypeVarIds in Generics are symbolic and
// don't address the live substitution.
func (c *Checker) instantiatedQuoteCandidates(t TypeId) []QuoteSig {
	switch c.arena.Kind(t) {
	case TKQuote:
		return []QuoteSig{c.Instantiate(c.arena.QuoteSig(t))}
	case TKOverloadedQuote:
		raw := c.arena.overloadedQuoteSigs[c.arena.Node(t).Extra]
		out := make([]QuoteSig, len(raw))
		for i, s := range raw {
			out[i] = c.Instantiate(s)
		}
		return out
	}
	return nil
}

// tryLoop accepts a stack-resident quote whose net stack effect is
// identity (inputs unify position-wise with outputs). It engages for
// both single-sig and overloaded quotes — for the latter, it picks
// the first candidate whose shape satisfies the identity check.
func (c *Checker) tryLoop(tok Token) bool {
	if c.stack.Len() == 0 {
		return false
	}
	top := c.subst.Apply(c.arena, c.stack.Top())
	candidates := c.instantiatedQuoteCandidates(top)
	if len(candidates) == 0 {
		return false
	}
	// Trial each candidate; the first one whose inputs unify with
	// outputs wins. Substitution is checkpointed so a failed trial
	// doesn't leak bindings.
	cp := c.subst.Checkpoint()
	for _, sig := range candidates {
		c.subst.Rollback(cp)
		if len(sig.Inputs) != len(sig.Outputs) {
			continue
		}
		ok := true
		for i := range sig.Inputs {
			if !c.unify(sig.Inputs[i], sig.Outputs[i]) {
				ok = false
				break
			}
		}
		if ok {
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			return true
		}
	}
	// None of the candidates produce an identity stack effect.
	// Report against the first single-sig shape if available, else
	// surface a generic mismatch.
	c.subst.Rollback(cp)
	first := candidates[0]
	if len(first.Inputs) != len(first.Outputs) {
		c.errors = append(c.errors, TypeError{
			Kind:     TErrTypeMismatch,
			Pos:      tok,
			Expected: c.arena.MakeQuote(QuoteSig{}),
			Actual:   top,
			Hint:     "loop quote must have matching input and output arity",
		})
		c.stack.items = c.stack.items[:c.stack.Len()-1]
		return true
	}
	for i := range first.Inputs {
		if !c.unify(first.Inputs[i], first.Outputs[i]) {
			c.errors = append(c.errors, TypeError{
				Kind:     TErrTypeMismatch,
				Pos:      tok,
				Expected: first.Inputs[i],
				Actual:   first.Outputs[i],
				ArgIndex: i,
				Hint:     "loop quote must preserve stack types",
			})
			return true
		}
	}
	c.stack.items = c.stack.items[:c.stack.Len()-1]
	return true
}

func (c *Checker) tryGridJoin() bool {
	if c.stack.Len() < 4 {
		return false
	}
	top := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
	second := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
	left := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-4])
	right := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-3])
	if !isQuoteKind(c.arena.Kind(top)) || !isQuoteKind(c.arena.Kind(second)) {
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

// isQuoteKind reports whether a TypeKind is a quote (single-sig or
// overloaded). Used by the try* helpers that test for a quote on the
// stack without caring about its arity.
func isQuoteKind(k TypeKind) bool {
	return k == TKQuote || k == TKOverloadedQuote
}

// isContainerKind reports whether a TypeKind denotes a runtime
// container value (list, dict, shape/struct, or any grid family
// member). Used by ops like `pivot` whose result cells must be
// scalars.
func isContainerKind(k TypeKind) bool {
	switch k {
	case TKList, TKDict, TKShape, TKGrid, TKGridView, TKGridRow:
		return true
	}
	return false
}

// tryPivot runs `pivot`'s normal overload resolution and then verifies
// that the supplied aggregation quote's output type is a scalar. The
// runtime rejects container cells at evaluation; surfacing the same
// constraint here turns the runtime crash into a typed error pointing
// at the call site.
//
// Caveat: when the quote's output resolves to an unconstrained type
// variable (e.g. `(:val?)`, where the getter on an unconstrained input
// yields a fresh var), there's nothing concrete to test, and this
// check stays quiet. Catching those needs bidirectional inference
// (re-walking the quote body with `GridView` pre-bound as the input).
func (c *Checker) tryPivot(tok Token) bool {
	sigs, ok := c.nameBuiltins[c.names.Intern("pivot")]
	if !ok {
		return false
	}
	var quoteOutputs []TypeId
	if c.stack.Len() >= 1 {
		top := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
		switch c.arena.Kind(top) {
		case TKQuote:
			sig := c.arena.QuoteSig(top)
			quoteOutputs = append(quoteOutputs, sig.Outputs...)
		case TKOverloadedQuote:
			n := c.arena.Node(top)
			for _, sig := range c.arena.overloadedQuoteSigs[n.Extra] {
				quoteOutputs = append(quoteOutputs, sig.Outputs...)
			}
		}
	}
	c.resolveAndApply(sigs, tok)
	for _, out := range quoteOutputs {
		resolved := c.subst.Apply(c.arena, out)
		if isContainerKind(c.arena.Kind(resolved)) {
			c.errors = append(c.errors, TypeError{
				Kind:   TErrTypeMismatch,
				Pos:    tok,
				Actual: resolved,
				Hint:   fmt.Sprintf("pivot aggregation must return a scalar; this quote returns %s", FormatType(c.arena, c.names, resolved)),
			})
			break
		}
	}
	return true
}

// tryRedirect handles a redirect op (`>`, `<`, `>e`, ...) applied to a
// `<operand> <target>` stack, where operand is a quote (single or overloaded)
// or a command-like value (list/command) and target is a writable path
// (str/path/bytes). It pops the target, leaving the operand on top. Returns
// false (no effect) when the op or stack shape doesn't match, so the caller
// falls through to ordinary builtin dispatch.
func (c *Checker) tryRedirect(tok Token) bool {
	switch tok.Type {
	case LESSTHAN, GREATERTHAN, STDERRREDIRECT, STDERRAPPEND,
		STDOUTANDSTDERRREDIRECT, STDOUTANDSTDERRAPPEND, INPLACEREDIRECT, STDAPPEND:
	default:
		return false
	}
	if c.stack.Len() < 2 {
		return false
	}
	target := c.arena.WidenStrLit(c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1]))
	if target != TidStr && target != TidPath && target != TidBytes {
		return false
	}
	operand := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-2])
	if !isQuoteKind(c.arena.Kind(operand)) && !c.isCommandLike(operand) {
		return false
	}
	c.stack.items = c.stack.items[:c.stack.Len()-1]
	return true
}

func (c *Checker) isCommandLike(t TypeId) bool {
	n := c.arena.Node(t)
	switch n.Kind {
	case TKList:
		return true
	case TKCommand:
		return true
	default:
		return false
	}
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
	for _, i := range c.inputUnifyOrder(sig.Inputs, base) {
		want := sig.Inputs[i]
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
		resolved := c.subst.Apply(c.arena, out)
		if resolved == TidBottom {
			c.diverged = true
		}
		c.stack.items = append(c.stack.items, resolved)
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

	// A string literal is a subtype of `str`; widen both sides so a literal
	// unifies exactly as `str` (including binding a free variable to `str`,
	// not the narrower literal). Reusing the nodes loaded just above, the
	// common case — neither side a literal — costs only two kind checks.
	if gn.Kind == TKStrLit {
		got = TidStr
		if got == want {
			return true
		}
		gn = c.arena.Node(TidStr)
	}
	if wn.Kind == TKStrLit {
		want = TidStr
		if got == want {
			return true
		}
		wn = c.arena.Node(TidStr)
	}

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
	case TKCommand:
		return gn.B == wn.B && gn.Extra == wn.Extra && c.unify(TypeId(gn.A), TypeId(wn.A))
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

// unifyShape implements width subtyping with optional fields: every
// required field of `want` must appear in `got` with a unifiable type, and a
// `want` field marked optional may be absent in `got`. Extra fields in `got`
// are allowed. A required `want` field cannot be satisfied by an optional
// `got` field (the value might be absent at runtime); when a field is present
// in both, its value type is checked regardless of optionality. Both field
// lists are pre-sorted by NameId (see normalizeShapeFields), so the merge is
// linear.
func (c *Checker) unifyShape(gn, wn TypeNode) bool {
	gFields := c.arena.shapeFields[gn.Extra]
	wFields := c.arena.shapeFields[wn.Extra]
	gi := 0
	for _, wf := range wFields {
		// Advance gi to the first field with name >= wf.Name.
		for gi < len(gFields) && gFields[gi].Name < wf.Name {
			gi++
		}
		present := gi < len(gFields) && gFields[gi].Name == wf.Name
		if !present {
			// Absent in `got` is only acceptable if `want` allows it.
			if wf.Optional {
				continue
			}
			return false
		}
		// A required `want` field is not satisfied by an optional `got`
		// field — presence is not guaranteed even though the name matches.
		if !wf.Optional && gFields[gi].Optional {
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

// unifyQuote unifies two quote signatures. Inputs are contravariant
// (the actual quote may accept a wider input type than the expected
// context supplies), while outputs are covariant.
// unifyQuote unifies two TKQuote sigs. Each side may carry a
// Generics list whose TypeVarIds are symbolic — they refer to
// nothing in the current substitution and must be renamed to fresh
// substitution-allocated vars before structural unification. We
// Instantiate both sides; for a sig with empty Generics this is a
// no-op, matching the protocol that hand-written and inferred sigs
// alike obey.
func (c *Checker) unifyQuote(gn, wn TypeNode) bool {
	gs := c.Instantiate(c.arena.quoteSigs[gn.Extra])
	ws := c.Instantiate(c.arena.quoteSigs[wn.Extra])
	if len(gs.Inputs) != len(ws.Inputs) || len(gs.Outputs) != len(ws.Outputs) {
		return false
	}
	if gs.Diverges != ws.Diverges {
		return false
	}
	for i := range gs.Inputs {
		if !c.unify(ws.Inputs[i], gs.Inputs[i]) {
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
