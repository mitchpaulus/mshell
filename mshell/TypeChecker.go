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
	Sig QuoteSig
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

	currentFn *FnContext
}

// NewChecker constructs a fresh checker with the given arena and name table.
// The builtin sig table is built once here.
func NewChecker(arena *TypeArena, names *NameTable) *Checker {
	return &Checker{
		arena:        arena,
		names:        names,
		vars:         NewVarEnv(),
		builtins:     builtinSigsByToken(arena),
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
		// `$"...{expr}..."` evaluates interpolations and yields a
		// string. Interpolation contents reference variables
		// (`@name`), bare names (`name`), and arbitrary expressions
		// — checking them properly requires re-lexing the inside,
		// which is left for a follow-up. For now, accept the
		// literal as `str`.
		c.stack.Push(TidStr)
		return
	case PATH:
		c.stack.Push(TidPath)
		return
	case DATETIME:
		c.stack.Push(TidDateTime)
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
		c.resolveAndApply(sigs, tok)
		return
	}

	if tok.Type == LITERAL {
		nameId := c.names.Intern(tok.Lexeme)
		if sigs, ok := c.nameBuiltins[nameId]; ok {
			c.resolveAndApply(sigs, tok)
			return
		}
	}

	c.errors = append(c.errors, TypeError{
		Kind: TErrUnknownIdentifier,
		Pos:  tok,
		Name: tok.Lexeme,
	})
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
		// Nominal: brand ids must match. Underlying types must coincide too;
		// hashconsing guarantees that if brand id and underlying agree we'd
		// already have gotten id equality up top — so reaching here with
		// matching brand ids means underlyings differ, which is a programmer
		// error we treat as a mismatch.
		return gn.A == wn.A && gn.B == wn.B
	case TKQuote:
		return c.unifyQuote(gn, wn)
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
	if gs.Fail != ws.Fail || gs.Pure != ws.Pure {
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
