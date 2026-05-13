package main

// Phase 10 step 3 (gate) + step 4 (program-flow walker).
//
// TypeCheckProgram is the entry point invoked from Main.go's
// `--check-types` gate and `--type-check-only` mode. It does two passes:
//
//   1. Pre-pass: collect all MShellTypeDecl items and register each
//      via Checker.DeclareType. Forward references across decls
//      work in declaration order. Reserved-type-name and
//      duplicate-name errors surface here.
//
//   2. Flow walk: drive the Checker's TypeStack through every parse
//      item. Tokens dispatch through the existing builtin table
//      (arithmetic, comparison, just/none). MShellAsCast resolves
//      its target and calls Checker.Cast against the live stack.
//      MShellTypeDecl is skipped (already registered).
//
// Composite parse items (lists, dicts, quotes, if/match blocks,
// grids, indexers, varstore, getters) currently push or pop
// placeholder types so the stack stays roughly consistent and
// nested casts still get walked. Real per-node semantics for these
// land as the builtin table and parse-tree walker grow. Until then,
// programs that lean on unregistered word builtins (most existing
// mshell programs do) will surface unknown-identifier errors when
// run under --check-types or --type-check-only — that's the signal
// for what to register next.

// TypeCheckProgram runs the new Checker against the given file.
// Returns formatted error strings (one per error) and an
// exit-friendly bool: ok == true means no errors were found.
//
// stdlibDefs is the slice of definitions loaded from `lib/std.msh`
// (and any other startup files). Their sigs are pre-registered as
// builtins so call sites resolve, but their bodies are not
// type-checked here — std.msh exercises features (process lists,
// format strings, dynamic exec) the v1 checker does not yet model,
// and we trust the runtime tests catch breakage there.
func TypeCheckProgram(file *MShellFile, stdlibDefs []MShellDefinition) (errors []string, ok bool) {
	arena := NewTypeArena()
	names := NewNameTable()
	checker := NewChecker(arena, names)

	checker.RegisterStdlibSigs(stdlibDefs)
	checker.CheckProgram(file)

	if len(checker.errors) == 0 {
		return nil, true
	}
	out := make([]string, 0, len(checker.errors))
	for _, e := range checker.errors {
		out = append(out, e.Format(arena, names))
	}
	return out, false
}

// RegisterStdlibSigs translates each stdlib def's TypeDefinition
// into a QuoteSig and registers it under its name as a callable
// builtin. Defs without a usable sig (translator errors) are
// skipped silently — the call site will surface as
// `unknown identifier`, which is the same behavior as before
// integration.
//
// If a name is already in nameBuiltins (i.e. a real builtin with
// the same identifier already exists), the stdlib def is skipped:
// we trust the table-driven sig over the std.msh re-declaration to
// avoid spurious "ambiguous overload" diagnostics for operations
// like `2unpack` that exist in both places.
func (c *Checker) RegisterStdlibSigs(defs []MShellDefinition) {
	for i := range defs {
		def := &defs[i]
		nameId := c.names.Intern(def.Name)
		if _, exists := c.nameBuiltins[nameId]; exists {
			continue
		}
		sig, errs := TranslateTypeDef(c.arena, c.names, &def.TypeDef)
		if len(errs) != 0 {
			continue
		}
		c.nameBuiltins[nameId] = append(c.nameBuiltins[nameId], sig)
	}
}

// CheckProgram is the file-level type-check pass. It registers all
// type declarations and user-defined function sigs, then walks the
// parse tree driving the type stack. Error accumulation lives on the
// Checker.
func (c *Checker) CheckProgram(file *MShellFile) {
	// Pre-pass 1: register all `type` declarations.
	for _, item := range file.Items {
		if d, ok := item.(*MShellTypeDecl); ok {
			body := ResolveTypeExprAST(c, d.Body)
			c.DeclareType(d.Name, body)
		}
	}
	// Pre-pass 2: register all `def` signatures so call sites (and
	// recursive self-calls inside def bodies) can resolve them.
	defSigs := make([]QuoteSig, len(file.Definitions))
	for i := range file.Definitions {
		def := &file.Definitions[i]
		sig, _ := TranslateTypeDef(c.arena, c.names, &def.TypeDef)
		defSigs[i] = sig
		nameId := c.names.Intern(def.Name)
		c.nameBuiltins[nameId] = append(c.nameBuiltins[nameId], sig)
	}
	// Pre-pass 3: type-check each def body against its declared sig.
	for i := range file.Definitions {
		c.checkDefBody(&file.Definitions[i], defSigs[i])
	}
	// Flow walk of top-level items.
	for _, item := range file.Items {
		c.checkParseItem(item)
	}
}

// checkDefBody verifies that the def's body, when run with the
// declared inputs on the stack, produces a stack matching the
// declared outputs.
//
// Isolation:
//   - Stack and VarEnv are saved + reset before the check; the
//     def's body sees only its inputs and its own variable scope.
//   - The substitution is checkpointed and rolled back afterwards
//     so per-body bindings don't leak globally.
//   - Generics are fresh-renamed via Instantiate so the body
//     check operates on substitution-local TypeVarIds; recursive
//     self-calls go through nameBuiltins as usual and get their
//     own fresh-rename.
func (c *Checker) checkDefBody(def *MShellDefinition, sig QuoteSig) {
	// Save outer state.
	outerStack := c.stack.items
	outerVars := c.vars.bound
	outerDiverged := c.diverged
	prevFn := c.currentFn
	cp := c.subst.Checkpoint()

	c.stack.items = nil
	c.vars.bound = make(map[NameId]TypeId)
	c.diverged = false

	// Fresh-rename generics for this body check.
	instSig := c.Instantiate(sig)
	fnCtx := &FnContext{Sig: instSig}
	c.currentFn = fnCtx

	// Push declared inputs.
	for _, in := range instSig.Inputs {
		c.stack.Push(in)
	}

	// Walk the body.
	for _, item := range def.Items {
		c.checkParseItem(item)
	}

	// Verify the resulting stack matches declared outputs.
	expected := instSig.Outputs
	if c.diverged || (fnCtx.SawReturn && c.stack.Len() == 0) {
		// A path that has already returned is checked at the return
		// site. During V1 we also accept a body with return sites and
		// no residual fallthrough stack; exhaustive-return analysis is
		// a later control-flow refinement.
	} else if c.stack.Len() != len(expected) {
		c.errors = append(c.errors, TypeError{
			Kind: TErrTypeMismatch,
			Pos:  def.NameToken,
			Hint: defBodyArityHint(def.Name, len(expected), c.stack.Len()),
		})
	} else {
		for i, want := range expected {
			got := c.stack.items[i]
			if !c.unify(got, want) {
				c.errors = append(c.errors, TypeError{
					Kind:     TErrTypeMismatch,
					Pos:      def.NameToken,
					Expected: want,
					Actual:   got,
					ArgIndex: i,
					Hint:     "def body output position " + intToStr(i) + " of '" + def.Name + "'",
				})
			}
		}
	}

	// Restore outer state.
	c.currentFn = prevFn
	c.subst.Rollback(cp)
	c.stack.items = outerStack
	c.vars.bound = outerVars
	c.diverged = outerDiverged
}

func defBodyArityHint(name string, want, got int) string {
	return "def body of '" + name + "' produced " + intToStr(got) +
		" output(s); declared sig has " + intToStr(want)
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// checkParseItem dispatches a single parse-tree item, advancing the
// type stack as appropriate. Unknown / not-yet-implemented item
// kinds are handled with placeholder stack effects so the rest of
// the walk doesn't cascade into garbage; this is acceptable while
// the walker grows.
func (c *Checker) checkParseItem(item MShellParseItem) {
	if c.diverged {
		return
	}
	switch it := item.(type) {

	case *MShellTypeDecl:
		// Already registered in the pre-pass.
		return

	case *MShellAsCast:
		target := ResolveTypeExprAST(c, it.Target)
		if target != TidNothing {
			c.Cast(target, it.AsToken)
		}
		return

	case Token:
		c.checkOne(it)
		return

	case *MShellParseList:
		// Evaluate list contents on an isolated stack, mirroring the
		// runtime's FRAME_LIST behavior, then collapse the resulting
		// item stack into a homogeneous list element type. Heterogeneous
		// literals become list-of-union, which is the closest static
		// representation to mshell's runtime lists.
		//
		// listDepth is bumped so that bare LITERAL tokens inside the
		// list (shell-style argv words) get typed as `str` instead
		// of being flagged as unknown identifiers — see the
		// matching branch in `checkOne`.
		listScope := c.snapshotStack()
		c.listDepth++
		for _, sub := range it.Items {
			c.checkParseItem(sub)
		}
		c.listDepth--
		items := append([]TypeId(nil), c.stack.items[listScope.length:]...)
		c.restoreStack(listScope)
		if len(items) == 0 {
			c.stack.Push(c.arena.MakeList(c.subst.FreshVar(c.arena)))
			return
		}
		// Detect homogeneity: every slot the same TypeId. Homogeneous
		// literals stay as `[T]` so list-consuming code keeps working
		// as before. Heterogeneous literals become tuples so 2unpack /
		// pattern-match can recover positional types.
		homogeneous := true
		for i := 1; i < len(items); i++ {
			if items[i] != items[0] {
				homogeneous = false
				break
			}
		}
		if homogeneous {
			c.stack.Push(c.arena.MakeList(items[0]))
		} else {
			c.stack.Push(c.arena.MakeTuple(items))
		}
		return

	case *MShellParseDict:
		// Dict literal `{k: v, ...}`. Preserve concrete keys as a
		// shape so heterogeneous dictionaries can satisfy declared
		// dictionary shapes. Shape-to-dict unification below keeps
		// generic dictionary operations working.
		fields := make([]ShapeField, 0, len(it.Items))
		for _, kv := range it.Items {
			scope := c.snapshotStack()
			for _, sub := range kv.Value {
				c.checkParseItem(sub)
			}
			valueT := c.subst.FreshVar(c.arena)
			if c.stack.Len() > scope.length {
				valueT = c.stack.items[c.stack.Len()-1]
			}
			fields = append(fields, ShapeField{Name: c.names.Intern(kv.Key), Type: valueT})
			c.restoreStack(scope)
		}
		c.stack.Push(c.arena.MakeShape(fields))
		return

	case *MShellParseQuote:
		if candidates, ok := c.quoteOverloadCandidates(it.Items); ok {
			// Instantiate each candidate's generics to fresh vars at
			// the push site. Builtin overload templates share small
			// TypeVarIds (0, 1, ...) across entries, so storing the
			// raw candidates would let occurs checks conflate them
			// with the caller's freshly-allocated vars. Per-push
			// instantiation gives each quote literal a private set.
			fresh := make([]QuoteSig, len(candidates))
			for i, cand := range candidates {
				fresh[i] = c.Instantiate(cand)
			}
			c.stack.Push(c.arena.MakeOverloadedQuote(fresh))
			return
		}
		// Phase 7 inference: run the body against a fresh empty
		// stack with inferring mode on, accumulate underflow as
		// fresh-var inputs, take the residual stack as outputs.
		// The result is the quote literal's inferred sig.
		sig := c.InferQuoteSigItems(it.Items)
		c.stack.Push(c.arena.MakeQuote(sig))
		return

	case *MShellParsePrefixQuote:
		funcName := it.StartToken.Lexeme
		if len(funcName) > 0 && funcName[len(funcName)-1] == '.' {
			funcName = funcName[:len(funcName)-1]
		}
		sig := c.InferQuoteSigItemsWithInputs(it.Items, c.prefixQuoteInputs(funcName))
		c.stack.Push(c.arena.MakeQuote(sig))
		callTok := it.StartToken
		callTok.Type = LITERAL
		callTok.Lexeme = funcName
		c.checkOne(callTok)
		return

	case *MShellParseIfBlock:
		c.checkIfBlock(it)
		return

	case *MShellParseMatchBlock:
		c.checkMatchBlock(it)
		return

	case *MShellParseGrid:
		// Derive a column-typed schema by walking each cell expression
		// in isolation and unioning the per-column results. An empty
		// column (no rows) gets a fresh var so subsequent ops can
		// refine it.
		cols := make([]GridSchemaCol, len(it.Columns))
		for ci, col := range it.Columns {
			nameId := c.names.Intern(col.Name)
			var cellTypes []TypeId
			for _, row := range it.Rows {
				if ci >= len(row) {
					continue
				}
				scope := c.snapshotStack()
				c.checkParseItem(row[ci])
				if c.stack.Len() > scope.length {
					cellTypes = append(cellTypes, c.stack.items[c.stack.Len()-1])
				}
				c.restoreStack(scope)
			}
			var colType TypeId
			switch len(cellTypes) {
			case 0:
				colType = c.subst.FreshVar(c.arena)
			case 1:
				colType = cellTypes[0]
			default:
				colType = c.arena.MakeUnion(cellTypes, 0)
			}
			cols[ci] = GridSchemaCol{Name: nameId, Type: colType}
		}
		schemaIdx := c.arena.MakeGridSchemaIdx(cols)
		c.stack.Push(c.arena.MakeGrid(schemaIdx))
		return

	case *MShellIndexerList:
		var indexed TypeId
		elementIndex := isSingleElementIndexer(it)
		if c.stack.Len() > 0 {
			indexed = c.stack.items[c.stack.Len()-1]
			c.stack.items = c.stack.items[:c.stack.Len()-1]
		} else if c.inferring {
			indexed = c.subst.FreshVar(c.arena)
			c.inferInputs = append([]TypeId{indexed}, c.inferInputs...)
		}
		c.stack.Push(c.indexerResultType(indexed, elementIndex))
		return

	case MShellVarstoreList:
		// Pop one stack item per name. The bound variable's type
		// is captured into VarEnv so subsequent getters can
		// resolve it.
		for i := len(it.VarStores) - 1; i >= 0; i-- {
			tok := it.VarStores[i]
			if c.stack.Len() == 0 {
				if c.inferring {
					// Quote body that starts by binding its
					// input(s): synthesize a fresh var as the
					// quote's i'th caller-supplied input. This
					// mirrors applySig's underflow-as-fresh-var
					// behavior so patterns like
					//   (num! @num wl @num 3 <)
					// produce a sig of (num -- bool).
					fresh := c.subst.FreshVar(c.arena)
					c.inferInputs = append([]TypeId{fresh}, c.inferInputs...)
					storeName := tok.Lexeme
					if n := len(storeName); n > 0 && storeName[n-1] == '!' {
						storeName = storeName[:n-1]
					}
					c.vars.bound[c.names.Intern(storeName)] = fresh
					continue
				}
				c.errors = append(c.errors, TypeError{
					Kind: TErrStackUnderflow,
					Pos:  tok,
					Hint: "varstore",
				})
				continue
			}
			top := c.stack.items[c.stack.Len()-1]
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			// VARSTORE lexeme is `name!`; strip the trailing `!`
			// so it interns as the same name `@name` looks up.
			storeName := tok.Lexeme
			if n := len(storeName); n > 0 && storeName[n-1] == '!' {
				storeName = storeName[:n-1]
			}
			c.vars.bound[c.names.Intern(storeName)] = top
		}
		return

	case *MShellGetter:
		// `:name` pops a Dict or GridRow off the stack and pushes
		// Maybe[V]. The value type V is looked up from the popped
		// type's schema when known: TKGridRow with a tracked schema
		// resolves the column by name; TKDict[str, V] yields V; any
		// other case pushes Maybe[fresh].
		//
		// Underflow while inferring a quote signature synthesizes a
		// fresh var as the quote's input — the caller's expected
		// (GridRow -- T) sig will unify with (T_fresh -- Maybe[T])
		// and bind T_fresh to GridRow at apply time. This lets
		// extractor quotes like `(:id?)` infer cleanly.
		nameId := c.names.Intern(it.String)
		if c.stack.Len() == 0 {
			if c.inferring {
				fresh := c.subst.FreshVar(c.arena)
				c.inferInputs = append([]TypeId{fresh}, c.inferInputs...)
				c.stack.Push(c.arena.MakeMaybe(c.subst.FreshVar(c.arena)))
				return
			}
			c.errors = append(c.errors, TypeError{
				Kind: TErrStackUnderflow,
				Pos:  it.Token,
				Hint: "':' getter",
			})
			c.stack.Push(c.arena.MakeMaybe(c.subst.FreshVar(c.arena)))
			return
		}
		top := c.stack.items[c.stack.Len()-1]
		c.stack.items = c.stack.items[:c.stack.Len()-1]
		c.stack.Push(c.arena.MakeMaybe(c.lookupGetterValueType(top, nameId)))
		return
	}
}

func (c *Checker) prefixQuoteInputs(funcName string) []TypeId {
	switch funcName {
	case "each", "filter", "map", "any", "all":
	default:
		return nil
	}
	if c.stack.Len() == 0 {
		return nil
	}
	receiver := c.subst.Apply(c.arena, c.stack.Top())
	node := c.arena.Node(receiver)
	switch node.Kind {
	case TKList:
		return []TypeId{TypeId(node.A)}
	case TKDict:
		return []TypeId{TypeId(node.B)}
	}
	return nil
}

func (c *Checker) quoteOverloadCandidates(items []MShellParseItem) ([]QuoteSig, bool) {
	if len(items) != 1 {
		return nil, false
	}
	tok, ok := items[0].(Token)
	if !ok {
		return nil, false
	}
	if sigs, ok := c.builtins[tok.Type]; ok && len(sigs) > 1 {
		return sigs, true
	}
	if tok.Type == LITERAL {
		nameId := c.names.Intern(tok.Lexeme)
		if sigs, ok := c.nameBuiltins[nameId]; ok && len(sigs) > 1 {
			return sigs, true
		}
	}
	return nil, false
}

// checkIfBlock drives an if/else-if/else chain through the branch
// reconciliation infrastructure (TypeBranch.go). The condition for
// the main `if` is already on the stack at entry — the runtime pops
// it before executing the body; we mirror that here. Each else-if
// arm starts from the post-pop snapshot, walks its condition body
// (which is expected to push a bool/int), pops that, then walks the
// arm body. An else-less if implicitly contributes a "did nothing"
// arm equal to the entry snapshot, since at runtime the if block
// may simply not fire.
func (c *Checker) checkIfBlock(ifBlock *MShellParseIfBlock) {
	startTok := ifBlock.GetStartToken()

	if c.stack.Len() == 0 {
		c.errors = append(c.errors, TypeError{
			Kind: TErrStackUnderflow,
			Pos:  startTok,
			Hint: "if condition",
		})
		return
	}
	cond := c.stack.items[c.stack.Len()-1]
	c.stack.items = c.stack.items[:c.stack.Len()-1]
	if !c.isBoolOrInt(cond) {
		c.errors = append(c.errors, TypeError{
			Kind:     TErrTypeMismatch,
			Pos:      startTok,
			Expected: TidBool,
			Actual:   cond,
			Hint:     "if condition must be bool or int",
		})
	}

	snap := c.Snapshot()

	// If body.
	for _, sub := range ifBlock.IfBody {
		c.checkParseItem(sub)
	}
	arms := []BranchArm{c.CaptureArm(c.diverged)}

	// else-if arms.
	for _, elseIf := range ifBlock.ElseIfs {
		c.Fork(snap)
		c.diverged = false
		for _, sub := range elseIf.Condition {
			c.checkParseItem(sub)
		}
		// Pop the else-if condition.
		if c.stack.Len() == 0 {
			c.errors = append(c.errors, TypeError{
				Kind: TErrStackUnderflow,
				Pos:  startTok,
				Hint: "else-if condition",
			})
		} else {
			ec := c.stack.items[c.stack.Len()-1]
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			if !c.isBoolOrInt(ec) {
				c.errors = append(c.errors, TypeError{
					Kind:     TErrTypeMismatch,
					Pos:      startTok,
					Expected: TidBool,
					Actual:   ec,
					Hint:     "else-if condition must be bool or int",
				})
			}
		}
		for _, sub := range elseIf.Body {
			c.checkParseItem(sub)
		}
		arms = append(arms, c.CaptureArm(c.diverged))
	}

	// else body, or the implicit "did nothing" arm if absent.
	if len(ifBlock.ElseBody) > 0 {
		c.Fork(snap)
		c.diverged = false
		for _, sub := range ifBlock.ElseBody {
			c.checkParseItem(sub)
		}
		arms = append(arms, c.CaptureArm(c.diverged))
	} else {
		c.Fork(snap)
		c.diverged = false
		arms = append(arms, c.CaptureArm(false))
	}

	c.ReconcileArms(arms, startTok)
}

// checkMatchBlock drives a `<subject> match ... end` through branch
// reconciliation. The subject is on top of the stack at entry; the
// runtime peeks (not pops) it, then per-arm pops if `Consume` is true.
//
// This first cut handles:
//   - Per-arm Consume vs preserve: each arm forks to the entry
//     snapshot, then pops or keeps the subject before walking the body.
//   - `just <bindingName>` patterns: when the subject is `Maybe[T]`,
//     the body sees `bindingName` bound to T.
//   - Wildcard `_` patterns: any subject matches; no narrowing.
//   - Type / value literal patterns (int, str, bool, "x", 42, true):
//     no narrowing yet — the body sees the unrefined subject type.
//     Refinement (e.g. inside an `int : ...` arm the subject is known
//     to be int) is a future improvement.
//
// What's NOT yet done:
//   - Exhaustiveness checking. The Phase 6b `CheckMatchExhaustive`
//     helper is available but classifying the parser's pattern items
//     into the MatchArmKind enum needs another translation step.
//   - Pattern-driven type narrowing inside arms.
func (c *Checker) checkMatchBlock(matchBlock *MShellParseMatchBlock) {
	startTok := matchBlock.GetStartToken()
	if c.stack.Len() == 0 {
		c.errors = append(c.errors, TypeError{
			Kind: TErrStackUnderflow,
			Pos:  startTok,
			Hint: "match subject",
		})
		return
	}
	subject := c.stack.items[c.stack.Len()-1]
	snap := c.Snapshot()

	if len(matchBlock.Arms) == 0 {
		// Empty match block: no arms could fire. Treat as a no-op.
		// The runtime would error at first use; the checker keeps
		// the subject on the stack.
		return
	}

	arms := make([]BranchArm, 0, len(matchBlock.Arms))
	for _, arm := range matchBlock.Arms {
		c.Fork(snap)
		// Apply per-arm subject handling.
		if arm.Consume {
			// Pop the subject — body sees the stack without it.
			c.stack.items = c.stack.items[:c.stack.Len()-1]
		}
		// Pattern-introduced bindings are arm-local. Restore any
		// shadowed outer bindings before capturing so ReconcileArms
		// doesn't see them as var-set disagreements across arms.
		patternBindings := c.bindMatchPattern(subject, arm.Pattern)

		for _, sub := range arm.Body {
			c.checkParseItem(sub)
		}
		c.restorePatternBindings(patternBindings)
		arms = append(arms, c.CaptureArm(false))
	}
	c.ReconcileArms(arms, startTok)
}

type patternBinding struct {
	Name NameId
	Old  TypeId
	Had  bool
}

func (c *Checker) bindPatternName(name string, typ TypeId, bindings *[]patternBinding) {
	if name == "_" || name == "" {
		return
	}
	nameId := c.names.Intern(name)
	old, had := c.vars.bound[nameId]
	*bindings = append(*bindings, patternBinding{Name: nameId, Old: old, Had: had})
	c.vars.bound[nameId] = typ
}

func (c *Checker) restorePatternBindings(bindings []patternBinding) {
	for i := len(bindings) - 1; i >= 0; i-- {
		b := bindings[i]
		if b.Had {
			c.vars.bound[b.Name] = b.Old
		} else {
			delete(c.vars.bound, b.Name)
		}
	}
}

// bindMatchPattern mirrors runtime match destructuring enough for body
// type-checking:
//   - `just name` binds the Maybe payload.
//   - `[a b ...rest]` binds element names and spread rest.
//   - `{ 'key': name }` binds dictionary value names.
//
// Value/type/wildcard patterns do not introduce bindings.
func (c *Checker) bindMatchPattern(subject TypeId, pattern []MShellParseItem) []patternBinding {
	var bindings []patternBinding

	// `just v`
	if len(pattern) != 2 {
		goto single
	}
	if first, ok1 := pattern[0].(Token); ok1 {
		if second, ok2 := pattern[1].(Token); ok2 &&
			first.Type == LITERAL && first.Lexeme == "just" &&
			second.Type == LITERAL && second.Lexeme != "_" {
			resolved := c.subst.Apply(c.arena, subject)
			n := c.arena.Node(resolved)
			if n.Kind == TKMaybe {
				c.bindPatternName(second.Lexeme, TypeId(n.A), &bindings)
			}
			return bindings
		}
	}

single:
	if len(pattern) != 1 {
		return bindings
	}
	switch p := pattern[0].(type) {
	case *MShellParseList:
		elem := c.subst.FreshVar(c.arena)
		resolved := c.subst.Apply(c.arena, subject)
		n := c.arena.Node(resolved)
		if n.Kind == TKList {
			elem = TypeId(n.A)
		}
		for _, item := range p.Items {
			tok, ok := item.(Token)
			if !ok || tok.Type != LITERAL {
				continue
			}
			if len(tok.Lexeme) > 3 && tok.Lexeme[:3] == "..." {
				c.bindPatternName(tok.Lexeme[3:], c.arena.MakeList(elem), &bindings)
			} else {
				c.bindPatternName(tok.Lexeme, elem, &bindings)
			}
		}
	case *MShellParseDict:
		for _, kv := range p.Items {
			if len(kv.Value) != 1 {
				continue
			}
			tok, ok := kv.Value[0].(Token)
			if ok && tok.Type == LITERAL {
				value := c.subst.FreshVar(c.arena)
				c.bindPatternName(tok.Lexeme, value, &bindings)
			}
		}
	}
	return bindings
}

// checkFormatStringInterpolations walks each `{...}` block inside a
// FORMATSTRING token, type-checking it as a tiny program against a
// fresh sub-stack that inherits the current VarEnv. Each block must
// produce exactly one value (the runtime concatenates it into the
// output string). The outer stack and substitution are unaffected
// regardless of what the blocks contain — only diagnostics
// accumulate.
//
// The lexeme is the full `$"..."` token including the leading `$"`
// and trailing `"`. Escape handling mirrors EvaluateFormatString in
// the runtime: `\{` is a literal `{`, `\\` is a literal `\`, etc.
func (c *Checker) checkFormatStringInterpolations(tok Token) {
	runes := []rune(tok.Lexeme)
	if len(runes) < 3 {
		return
	}
	// Skip leading `$"`; stop before trailing `"`.
	const (
		modeNormal = iota
		modeEscape
		modeFormat
	)
	mode := modeNormal
	startIdx := -1
	for i := 2; i < len(runes)-1; i++ {
		ch := runes[i]
		switch mode {
		case modeEscape:
			mode = modeNormal
		case modeNormal:
			switch ch {
			case '\\':
				mode = modeEscape
			case '{':
				startIdx = i + 1
				mode = modeFormat
			}
		case modeFormat:
			if ch == '}' {
				inner := string(runes[startIdx:i])
				c.checkFormatBlock(inner, tok)
				mode = modeNormal
				startIdx = -1
			}
		}
	}
}

// checkFormatBlock lexes/parses one interpolation block and walks its
// items on a fresh sub-stack. The current VarEnv is shared so `@name`
// references resolve against the surrounding scope. Errors are
// reported against the format-string token's position — finer source
// mapping inside the block is left for a follow-up. The outer stack
// and substitution are restored before returning.
func (c *Checker) checkFormatBlock(src string, callSite Token) {
	lex := NewLexer(src, nil)
	parser := NewMShellParser(lex)
	file, err := parser.ParseFile()
	if err != nil {
		c.errors = append(c.errors, TypeError{
			Kind: TErrUnknownIdentifier,
			Pos:  callSite,
			Name: "format-string interpolation: " + src,
		})
		return
	}

	outerStack := c.stack.items
	cp := c.subst.Checkpoint()
	c.stack.items = nil

	for _, item := range file.Items {
		c.checkParseItem(item)
	}

	if c.stack.Len() != 1 {
		c.errors = append(c.errors, TypeError{
			Kind: TErrInterpolationArity,
			Pos:  callSite,
			Hint: "format-string interpolation `{" + src + "}` must produce exactly one value, got " + intToStr(c.stack.Len()),
		})
	}

	c.subst.Rollback(cp)
	c.stack.items = outerStack
}

// lookupGetterValueType returns the value type produced by a `:name`
// getter applied to a value of type t. The result is the inner V of the
// returned Maybe[V]; callers wrap it.
//
// Resolution order after applying the current substitution:
//   - TKGridRow with a tracked schema: look up the column by name.
//     Hit → its declared type. Miss → fresh var (the runtime would
//     return None at this site; we keep type-checking permissive).
//   - TKDict[str, V]: V (regardless of name; dict keys are dynamic).
//   - Anything else (TKVar, unknown-schema GridRow, union, ...):
//     fresh var.
func (c *Checker) lookupGetterValueType(t TypeId, name NameId) TypeId {
	r := c.subst.Apply(c.arena, t)
	n := c.arena.Node(r)
	switch n.Kind {
	case TKGridRow:
		schemaIdx := n.Extra
		if schemaIdx != 0 {
			for _, col := range c.arena.gridSchemas[schemaIdx].Columns {
				if col.Name == name {
					return col.Type
				}
			}
		}
	case TKDict:
		return TypeId(n.B)
	}
	return c.subst.FreshVar(c.arena)
}

func isSingleElementIndexer(indexers *MShellIndexerList) bool {
	if len(indexers.Indexers) != 1 {
		return false
	}
	tok, ok := indexers.Indexers[0].(Token)
	return ok && tok.Type == INDEXER
}

func (c *Checker) indexerResultType(t TypeId, elementIndex bool) TypeId {
	if t == TidNothing {
		return c.subst.FreshVar(c.arena)
	}
	r := c.subst.Apply(c.arena, t)
	n := c.arena.Node(r)
	switch n.Kind {
	case TKBrand:
		return r
	case TKList:
		if !elementIndex {
			return r
		}
		return TypeId(n.A)
	case TKGrid, TKGridView:
		return c.arena.MakeGridRow(n.Extra)
	case TKDict:
		return TypeId(n.B)
	case TKPrim:
		if r == TidStr || r == TidPath {
			return TidStr
		}
	}
	return c.subst.FreshVar(c.arena)
}

// isBoolOrInt reports whether t resolves to bool or int after applying
// the current substitution. mshell's runtime accepts either as an if
// condition.
func (c *Checker) isBoolOrInt(t TypeId) bool {
	r := c.subst.Apply(c.arena, t)
	return r == TidBool || r == TidInt
}

// snapshotStack / restoreStack capture and restore the stack
// length so a recursive walk can be sandboxed without leaving
// extra items behind. Variable bindings made inside the recursion
// persist (which is fine for now — real branch reconciliation will
// snapshot/restore VarEnv too).
type stackSnapshotMarker struct{ length int }

func (c *Checker) snapshotStack() stackSnapshotMarker {
	return stackSnapshotMarker{length: c.stack.Len()}
}

func (c *Checker) restoreStack(s stackSnapshotMarker) {
	if c.stack.Len() > s.length {
		c.stack.items = c.stack.items[:s.length]
	}
}
