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
	errors []TypeError

	builtins map[TokenType]QuoteSig

	currentFn *FnContext
}

// NewChecker constructs a fresh checker with the given arena and name table.
// The builtin sig table is built once here.
func NewChecker(arena *TypeArena, names *NameTable) *Checker {
	return &Checker{
		arena:    arena,
		names:    names,
		vars:     NewVarEnv(),
		builtins: builtinSigsByToken(),
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
	}

	if sig, ok := c.builtins[tok.Type]; ok {
		c.applySig(sig, tok)
		return
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
	if len(c.stack.items) < len(sig.Inputs) {
		c.errors = append(c.errors, TypeError{
			Kind: TErrStackUnderflow,
			Pos:  callSite,
		})
		return
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
		c.stack.items = append(c.stack.items, out)
	}
}

// unify is the Phase-2 "unifier": primitive-only structural equality.
// Generics, unions, brands, and the rest land in Phase 6 when Substitution
// is introduced. TidBottom unifies with anything — it is the divergent
// type and cannot occur as a literal yet, but the rule is encoded here so
// future divergent operations (exit, infinite loop) work transparently.
func (c *Checker) unify(got, want TypeId) bool {
	if got == want {
		return true
	}
	if got == TidBottom || want == TidBottom {
		return true
	}
	return false
}
