package main

import (
	"strconv"
	"fmt"
	"strings"
	"unicode/utf8"
)

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

	out := make([]string, 0, len(checker.errors))
	ok = true
	for _, e := range checker.errors {
		// Info-severity diagnostics (e.g. `dbg` dumps) are written to
		// stderr directly by their source for the CLI path; skip them
		// here so Main.go doesn't print them a second time.
		if e.Severity != SeverityError {
			continue
		}
		out = append(out, e.Format(arena, names))
		ok = false
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, ok
}

// RegisterStdlibSigs resolves each stdlib def's signature AST into a
// QuoteSig and registers it under its name as a callable builtin.
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
		sig := c.ResolveDefSig(def.Inputs, def.Outputs)
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
			body := c.resolveTypeExpr(d.Body, nil)
			c.DeclareType(d.Name, body)
		}
	}
	// Pre-pass 2: register all `def` signatures so call sites (and
	// recursive self-calls inside def bodies) can resolve them.
	defSigs := make([]QuoteSig, len(file.Definitions))
	for i := range file.Definitions {
		def := &file.Definitions[i]
		sig := c.ResolveDefSig(def.Inputs, def.Outputs)
		defSigs[i] = sig
		nameId := c.names.Intern(def.Name)
		c.nameBuiltins[nameId] = append(c.nameBuiltins[nameId], sig)
	}
	// Pre-pass 3: type-check each def body against its declared sig.
	for i := range file.Definitions {
		c.checkDefBody(&file.Definitions[i], defSigs[i])
	}
	// Flow walk of top-level items via the branching driver. Initial
	// state is the live checker state captured into a single branch;
	// surviving branches at the end are the program's possible typings.
	// If exactly one survives, commit its substitution and stack.
	// Multiple surviving branches is TErrAmbiguousTyping (the program
	// has under-constrained typing). Zero surviving means a step in the
	// middle exhausted all alternatives, with the failing-step error
	// already recorded.
	initial := []quoteBranch{c.initialTopBranch(c.stack.items)}
	c.stack.items = c.stack.items[:0]
	branches := c.driveBranchesOverItems(initial, file.Items)
	c.reconcileTopLevelBranches(file, branches)
}

func (c *Checker) reconcileTopLevelBranches(file *MShellFile, branches []quoteBranch) {
	if len(branches) == 0 {
		return
	}
	// Diverged branches (return / exit / propagated fail) represent
	// paths that never reach end-of-program at runtime, so they
	// don't participate in convergence. If every branch diverged,
	// the program itself diverges and we just keep one for state.
	live := filterLiveBranches(branches)
	if len(live) == 0 {
		c.loadBranch(branches[0])
		return
	}
	if len(live) == 1 {
		c.loadBranch(live[0])
		return
	}
	if branchStacksConverge(c, live) {
		c.loadBranch(live[0])
		return
	}
	c.loadBranch(live[0])
	pos := lastBranchingTokenInFile(file)
	c.errors = append(c.errors, TypeError{
		Kind: TErrAmbiguousTyping,
		Pos:  pos,
		Hint: formatBranchStacks(c, live),
	})
}

// filterLiveBranches returns the non-diverged subset. Used at
// convergence points (end-of-program, end-of-def) where diverged
// paths don't represent realizable runtime continuations.
func filterLiveBranches(branches []quoteBranch) []quoteBranch {
	live := make([]quoteBranch, 0, len(branches))
	for _, b := range branches {
		if !b.diverged {
			live = append(live, b)
		}
	}
	return live
}

// branchStacksConverge reports whether all branches end with the same
// stack shape after substitution. Used to silently accept ambiguity
// that the surrounding context has fully resolved.
func branchStacksConverge(c *Checker, branches []quoteBranch) bool {
	if len(branches) <= 1 {
		return true
	}
	// Canonicalize each branch's stack as a slice of post-substitution
	// TypeIds. Compare to the first.
	canon := func(b quoteBranch) []TypeId {
		c.loadBranch(b)
		out := make([]TypeId, len(c.stack.items))
		for i, t := range c.stack.items {
			out[i] = c.subst.Apply(c.arena, t)
		}
		return out
	}
	first := canon(branches[0])
	for _, b := range branches[1:] {
		cur := canon(b)
		if len(cur) != len(first) {
			return false
		}
		for i := range first {
			if cur[i] != first[i] {
				return false
			}
		}
	}
	return true
}

// formatBranchStacks renders one line per surviving branch's final
// stack, suitable for an ambiguity-diagnostic hint. The branches are
// loaded in turn so their substitutions resolve.
func formatBranchStacks(c *Checker, branches []quoteBranch) string {
	var sb strings.Builder
	sb.WriteString("surviving typings (top of stack first):")
	for i, b := range branches {
		c.loadBranch(b)
		sb.WriteString("\n  branch ")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(":")
		if c.stack.Len() == 0 {
			sb.WriteString(" <empty>")
			continue
		}
		for j := c.stack.Len() - 1; j >= 0; j-- {
			sb.WriteString("\n    ")
			sb.WriteString(FormatType(c.arena, c.names, c.subst.Apply(c.arena, c.stack.items[j])))
		}
	}
	return sb.String()
}

// lastBranchingTokenInFile is a best-effort anchor for ambiguity
// diagnostics: the last token in the program. The branching driver
// doesn't track which step introduced the ambiguity, so we point at
// the end of the program. A future refinement could thread the
// branching-event token through driveBranches.
func lastBranchingTokenInFile(file *MShellFile) Token {
	var best Token
	for _, item := range file.Items {
		if tok, ok := item.(Token); ok {
			best = tok
		}
	}
	return best
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
//
// The body is walked through the branching driver
// (driveBranchesOverItems). Each multi-overload call inside the body
// fans the in-flight branches out; downstream constraints prune them.
// The declared output signature is the convergence point: any
// surviving branch whose final stack unifies with the declared
// outputs is accepted. Branches that don't match are simply dropped
// — equivalent for the purposes of the declared sig, even if their
// intermediate types differed. If no branch matches, the closest
// mismatch is surfaced via TErrDefBodyMismatch.
func (c *Checker) checkDefBody(def *MShellDefinition, sig QuoteSig) {
	// Save outer state.
	outerStack := c.stack.items
	outerVars := c.vars.bound
	outerMaybeVars := c.vars.maybeBound
	outerDiverged := c.diverged
	outerInferring := c.inferring
	outerInferInputs := c.inferInputs
	prevFn := c.currentFn
	cp := c.subst.Checkpoint()

	c.stack.items = nil
	c.vars.bound = make(map[NameId]TypeId)
	c.vars.maybeBound = make(map[NameId]TypeId)
	c.diverged = false
	c.inferring = false
	c.inferInputs = nil

	// Fresh-rename generics for this body check.
	instSig := c.Instantiate(sig)
	fnCtx := &FnContext{Sig: instSig}
	c.currentFn = fnCtx

	// Build the initial branch from a stack pre-loaded with declared inputs.
	for _, in := range instSig.Inputs {
		c.stack.Push(in)
	}
	initial := []quoteBranch{c.initialTopBranch(c.stack.items)}
	c.stack.items = c.stack.items[:0]

	branches := c.driveBranchesOverItems(initial, def.Items)

	expected := instSig.Outputs
	if len(branches) == 0 {
		// All branches died; the failing-step error is already in
		// c.errors. Nothing more to report here.
	} else {
		c.reconcileDefBodyBranches(def, fnCtx, expected, branches)
	}

	// Restore outer state.
	c.currentFn = prevFn
	c.subst.Rollback(cp)
	c.stack.items = outerStack
	c.vars.bound = outerVars
	c.vars.maybeBound = outerMaybeVars
	c.diverged = outerDiverged
	c.inferring = outerInferring
	c.inferInputs = outerInferInputs
}

// reconcileDefBodyBranches accepts the def body iff at least one
// surviving branch agrees with the declared output sig. Branches that
// diverged (return / exit / propagated fail) are accepted regardless
// of residual stack. The first non-matching branch's mismatch is held
// in reserve and only surfaced when no branch matches at all.
func (c *Checker) reconcileDefBodyBranches(def *MShellDefinition, fnCtx *FnContext, expected []TypeId, branches []quoteBranch) {
	var firstMismatch *TypeError
	for _, b := range branches {
		c.loadBranch(b)
		if c.diverged || (fnCtx.SawReturn && c.stack.Len() == 0) {
			return
		}
		if c.stack.Len() != len(expected) {
			if firstMismatch == nil {
				err := TypeError{
					Kind: TErrDefBodyMismatch,
					Pos:  def.NameToken,
					Name: def.Name,
					Hint: defBodyArityHint(c, def.Name, expected, c.stack.items),
				}
				firstMismatch = &err
			}
			continue
		}
		cp := c.subst.Checkpoint()
		matched := true
		for i, want := range expected {
			got := c.stack.items[i]
			if !c.unify(got, want) {
				matched = false
				if firstMismatch == nil {
					err := TypeError{
						Kind:     TErrDefBodyMismatch,
						Pos:      def.NameToken,
						Name:     def.Name,
						Expected: want,
						Actual:   got,
						ArgIndex: i,
						Hint: "output position " + strconv.Itoa(i) +
							" — declared " + FormatType(c.arena, c.names, want) +
							", body produced " + FormatType(c.arena, c.names, got),
					}
					firstMismatch = &err
				}
				break
			}
		}
		if matched {
			return
		}
		c.subst.Rollback(cp)
	}
	if firstMismatch != nil {
		c.errors = append(c.errors, *firstMismatch)
	}
}

func defBodyArityHint(c *Checker, _ string, declared, produced []TypeId) string {
	return "declared " + strconv.Itoa(len(declared)) + " output(s) " + formatTypeList(c, declared) +
		", body produced " + strconv.Itoa(len(produced)) + " " + formatTypeList(c, produced)
}

// formatTypeList renders a stack/outputs slice as a parenthesized
// type list ordered as written in a def signature (left-to-right,
// bottom of stack to top). Empty renders as "( )".
func formatTypeList(c *Checker, items []TypeId) string {
	if len(items) == 0 {
		return "( )"
	}
	var sb strings.Builder
	sb.WriteByte('(')
	for i, t := range items {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(FormatType(c.arena, c.names, t))
	}
	sb.WriteByte(')')
	return sb.String()
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
		target := c.resolveTypeExpr(it.Target, nil)
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
		// The body is driven through the branching driver rather than a
		// plain loop, so an overloaded op inside the literal (e.g. an
		// indexer on a still-generic element type) fans out and each
		// surviving resolution is reconciled. A flat loop would leave
		// branchSpawn dangling and corrupt the collected item stack.
		//
		// listDepth is bumped so that bare LITERAL tokens inside the
		// list (shell-style argv words) get typed as `str` instead
		// of being flagged as unknown identifiers — see the
		// matching branch in `checkOne`.
		listScope := c.snapshotStack()
		c.listDepth++
		branches := c.driveBranchesOverItems([]quoteBranch{c.captureBranch()}, it.Items)
		c.listDepth--
		if len(branches) == 0 {
			// The body failed; the representative error is already on
			// c.errors. Restore the stack so the branch dies cleanly.
			c.restoreStack(listScope)
			return
		}
		// Common case: the body didn't fan out. Push the list directly so
		// callers that invoke checkParseItem outside the branching driver
		// (e.g. dict-literal values) still see it on the stack. Only a
		// genuine multi-branch body spawns — and that only arises while
		// driven by the branching walker, which consumes branchSpawn.
		if len(branches) == 1 {
			c.loadBranch(branches[0])
			items := append([]TypeId(nil), c.stack.items[listScope.length:]...)
			c.restoreStack(listScope)
			c.stack.Push(c.listTypeFromItems(items))
			return
		}
		for _, b := range branches {
			c.loadBranch(b)
			items := append([]TypeId(nil), c.stack.items[listScope.length:]...)
			c.restoreStack(listScope)
			c.stack.Push(c.listTypeFromItems(items))
			c.branchSpawn = append(c.branchSpawn, c.captureBranch())
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
			c.walkJoined(kv.Value)
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
		// Branching inference walks the body considering every
		// viable overload at each step. The result is the set of
		// surviving sigs — one collapses to TKQuote, multiple to
		// TKOverloadedQuote so the consumption site can resolve.
		sigs := c.InferQuoteSigItems(it.Items)
		c.stack.Push(c.arena.MakeOverloadedQuote(sigs))
		return

	case *MShellParsePrefixQuote:
		funcName := it.StartToken.Lexeme
		if len(funcName) > 0 && funcName[len(funcName)-1] == '.' {
			funcName = funcName[:len(funcName)-1]
		}
		// `fn.` is pure syntax sugar for `(...) fn`, so infer the block
		// body's signature exactly as a plain quote literal would — no
		// seeding from the receiver element. Seeding the body input with
		// the receiver's element type tangled the receiver's free type
		// vars into the inferred quote sig; after the inference rolls its
		// substitution back, the consumption site (`each`/`map`/...) then
		// saw the quote and the receiver carrying decoupled vars and
		// failed to unify (e.g. iterating `parseExcel`/`enumerate` output
		// whose element is a shape with a free var).
		sigs := c.InferQuoteSigItems(it.Items)
		c.stack.Push(c.arena.MakeOverloadedQuote(sigs))
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
				c.walkJoined(row[ci : ci+1])
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
		elementIndex := isSingleElementIndexer(it)

		// Empty stack: the receiver is the indexer's input.
		if c.stack.Len() == 0 {
			if c.inferring {
				// We're characterizing a quote whose body starts with the
				// indexer (e.g. `(2:)`). Dispatch the overload arms so the
				// quote captures the input→output link and fans out into an
				// overloaded quote that consumers like `map` can resolve.
				c.resolveAndApply(c.indexerCandidates(elementIndex), it.GetStartToken())
			} else {
				// Nothing to index; stay lenient rather than reporting
				// underflow (matches the getter case).
				c.stack.Push(c.subst.FreshVar(c.arena))
			}
			return
		}

		recv := c.subst.Apply(c.arena, c.stack.items[c.stack.Len()-1])
		switch n := c.arena.Node(recv); n.Kind {
		case TKGrid, TKGridView:
			// GridRow result carries the receiver's specific schema, which
			// can't be expressed as a generic candidate signature.
			c.stack.items[c.stack.Len()-1] = c.arena.MakeGridRow(n.Extra)
			return
		case TKBrand:
			c.stack.items[c.stack.Len()-1] = recv
			return
		case TKVar:
			// The receiver is a real stack value whose type isn't known yet —
			// commonly a quote-bound variable (`row!` then `@row :0:`) that an
			// outer `map`/apply will pin later. Fanning out here would bind
			// that shared variable to one container per overload arm and
			// pollute inference, so defer: yield a fresh result and let the
			// receiver be refined by the constraints that follow.
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			c.stack.Push(c.subst.FreshVar(c.arena))
			return
		}

		// Concrete container: dispatch through the shared overload machinery,
		// which resolves a single arm with full element/value precision.
		c.resolveAndApply(c.indexerCandidates(elementIndex), it.GetStartToken())
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
					storeNameId := c.names.Intern(storeName)
					c.vars.bound[storeNameId] = fresh
					delete(c.vars.maybeBound, storeNameId)
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
			storeNameId := c.names.Intern(storeName)
			c.vars.bound[storeNameId] = top
			delete(c.vars.maybeBound, storeNameId)
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
		applied := c.subst.Apply(c.arena, top)
		if !c.inferring && c.getterReceiverInvalid(applied) {
			c.errors = append(c.errors, TypeError{
				Kind:     TErrTypeMismatch,
				Pos:      it.Token,
				Expected: TidNothing,
				Hint: fmt.Sprintf("':' getter expects a dict, GridRow, Grid, or GridView, got %s",
					FormatType(c.arena, c.names, applied)),
			})
		}
		c.stack.Push(c.arena.MakeMaybe(c.lookupGetterValueType(applied, nameId)))
		return
	}
}

// listTypeFromItems collapses the item types collected from a list
// literal body into the list's static type. An empty body yields
// `[fresh]`. A homogeneous body (every slot the same TypeId) stays
// `[T]`; a heterogeneous one collapses to `[T1 | T2 | ...]`, matching
// the structural-union direction.
func (c *Checker) listTypeFromItems(items []TypeId) TypeId {
	if len(items) == 0 {
		return c.arena.MakeList(c.subst.FreshVar(c.arena))
	}
	homogeneous := true
	for i := 1; i < len(items); i++ {
		if items[i] != items[0] {
			homogeneous = false
			break
		}
	}
	if homogeneous {
		return c.arena.MakeList(items[0])
	}
	return c.arena.MakeList(c.arena.MakeUnion(items, 0))
}

// checkIfBlock drives an if/else-if/else chain through the branching
// walker. The condition for the main `if` is already on the stack at
// entry — the runtime pops it before executing the body; we mirror
// that here. Each arm body is walked through driveBranchesOverItems
// so multi-overload dispatch inside an arm fans out the same way it
// does anywhere else; the surviving branches from every arm become
// the candidate post-if states.
//
// An else-less if implicitly contributes a "did nothing" arm equal to
// the entry snapshot, since at runtime the if block may simply not
// fire. Surviving arm-branches are emitted into branchSpawn so the
// outer walker propagates them; if exactly one survives, the live
// state is loaded directly.
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

	entry := c.captureBranch()
	var armBranches []quoteBranch
	var armLabels []string

	walkArm := func(body []MShellParseItem, label string) {
		c.loadBranch(entry)
		c.diverged = false
		spawned := c.driveBranchesOverItems([]quoteBranch{c.captureBranch()}, body)
		for _, b := range spawned {
			armBranches = append(armBranches, b)
			armLabels = append(armLabels, label)
		}
	}

	walkArm(ifBlock.IfBody, "if")

	for i, elseIf := range ifBlock.ElseIfs {
		c.loadBranch(entry)
		c.diverged = false
		c.walkJoined(elseIf.Condition)
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
		label := fmt.Sprintf("else if #%d", i+1)
		spawned := c.driveBranchesOverItems([]quoteBranch{c.captureBranch()}, elseIf.Body)
		for _, b := range spawned {
			armBranches = append(armBranches, b)
			armLabels = append(armLabels, label)
		}
	}

	if len(ifBlock.ElseBody) > 0 {
		walkArm(ifBlock.ElseBody, "else")
	} else {
		// Implicit do-nothing arm: the if block may not fire at all.
		armBranches = append(armBranches, entry)
		armLabels = append(armLabels, "no-arm")
	}

	c.reconcileArmBranches(armBranches, armLabels, entry, startTok)
}

// reconcileArmBranches takes the surviving branches from all arms of
// an if- or match-block and chooses a post-state for the surrounding
// walker. `labels` runs parallel to armBranches and gives each
// branch a short source-snippet (the match arm's pattern, or the
// if-block's arm role) used purely in the TErrBranchStackSize
// diagnostic.
//
//   - 0 surviving branches → all arms errored; fall back to the
//     pre-construct state so downstream code stays coherent.
//   - 1 surviving branch → load it directly; the construct has a
//     fully-determined effect.
//   - multiple live (non-diverged) branches that disagree on stack
//     SIZE → emit TErrBranchStackSize anchored at the construct's
//     start token. This is a structural defect of the source, not
//     an ambiguity downstream code can resolve, so we surface it
//     here rather than letting the branches survive into
//     end-of-program TErrAmbiguousTyping with worse signal.
//   - multiple branches that agree on size → fan out via
//     branchSpawn so per-slot type differences propagate; downstream
//     constraints may narrow them.
func (c *Checker) reconcileArmBranches(armBranches []quoteBranch, labels []string, entry quoteBranch, startTok Token) {
	if len(armBranches) == 0 {
		c.loadBranch(entry)
		return
	}
	if len(armBranches) == 1 {
		c.loadBranch(armBranches[0])
		return
	}
	liveBranches, liveLabels := filterLiveBranchesWithLabels(armBranches, labels)
	if len(liveBranches) <= 1 {
		if len(liveBranches) == 1 {
			c.loadBranch(liveBranches[0])
		} else {
			c.loadBranch(armBranches[0])
		}
		return
	}
	wantSize := len(liveBranches[0].stack)
	sizesAgree := true
	for _, b := range liveBranches[1:] {
		if len(b.stack) != wantSize {
			sizesAgree = false
			break
		}
	}
	if !sizesAgree {
		c.errors = append(c.errors, TypeError{
			Kind: TErrBranchStackSize,
			Pos:  startTok,
			Hint: c.formatArmBranchSizes(liveBranches, liveLabels),
		})
		c.loadBranch(liveBranches[0])
		return
	}
	// Join the live arms into a single post-state by unioning per-slot
	// types and merging var bindings, rather than fanning them out as
	// alternative typings. Match/if arms are control-flow paths: every
	// arm is a real runtime possibility, so the post-state must reflect
	// *all* of them (a union), and a downstream op that rejects any
	// member is a genuine error.
	//
	// Fanning the arms out as separate branchSpawn continuations was
	// unsound: the branching driver keeps a program alive as long as
	// *any* branch survives, so one arm could mask a type error that a
	// sibling arm provokes downstream. E.g. `match []: 0.0, _ :> sum`
	// leaves `int | float`; `@b @t toFloat /` is valid only for the
	// `float` arm, yet the fan-out accepted the whole program because
	// that arm survived — while the `int` arm (the actual runtime path
	// for a non-empty list) crashes with "Cannot divide an int by a
	// float." This mirrors the quote-form `iff` join in ReconcileArms.
	c.joinArmBranches(liveBranches)
}

// joinArmBranches collapses size-agreeing arm branches into a single
// post-state. Each stack slot becomes the union of that slot across
// arms; a var bound in every arm stays bound (as a union of its arm
// types) while one bound in only some arms lifts to maybeBound. The
// branches carry independent substitution checkpoints, so each branch's
// types are resolved under its own substitution before unioning. The
// merged state is installed on the first branch's substitution so the
// surrounding walker continues from a coherent checkpoint.
func (c *Checker) joinArmBranches(branches []quoteBranch) {
	n := len(branches[0].stack)
	resolvedStacks := make([][]TypeId, len(branches))
	resolvedVars := make([]map[NameId]TypeId, len(branches))
	resolvedMaybe := make([]map[NameId]TypeId, len(branches))
	for bi, b := range branches {
		c.loadBranch(b)
		rs := make([]TypeId, n)
		for i := 0; i < n; i++ {
			rs[i] = c.subst.Apply(c.arena, b.stack[i])
		}
		rv := make(map[NameId]TypeId, len(c.vars.bound))
		for k, v := range c.vars.bound {
			rv[k] = c.subst.Apply(c.arena, v)
		}
		rmv := make(map[NameId]TypeId, len(c.vars.maybeBound))
		for k, v := range c.vars.maybeBound {
			rmv[k] = c.subst.Apply(c.arena, v)
		}
		resolvedStacks[bi] = rs
		resolvedVars[bi] = rv
		resolvedMaybe[bi] = rmv
	}

	mergedStack := make([]TypeId, n)
	scratch := make([]TypeId, 0, len(branches))
	for slot := 0; slot < n; slot++ {
		scratch = scratch[:0]
		for bi := range branches {
			scratch = append(scratch, resolvedStacks[bi][slot])
		}
		mergedStack[slot] = c.arena.MakeUnion(scratch, 0)
	}

	allNames := make(map[NameId]struct{})
	for bi := range branches {
		for nm := range resolvedVars[bi] {
			allNames[nm] = struct{}{}
		}
		for nm := range resolvedMaybe[bi] {
			allNames[nm] = struct{}{}
		}
	}
	mergedBound := make(map[NameId]TypeId)
	mergedMaybe := make(map[NameId]TypeId)
	for nm := range allNames {
		boundEverywhere := true
		scratch = scratch[:0]
		for bi := range branches {
			if t, ok := resolvedVars[bi][nm]; ok {
				scratch = append(scratch, t)
				continue
			}
			boundEverywhere = false
			if t, ok := resolvedMaybe[bi][nm]; ok {
				scratch = append(scratch, t)
			}
		}
		merged := c.arena.MakeUnion(scratch, 0)
		if boundEverywhere {
			mergedBound[nm] = merged
		} else {
			mergedMaybe[nm] = merged
		}
	}

	// Install the merged state on the first branch's substitution. The
	// unioned types are already fully resolved, so they remain valid;
	// inferInputs / inferring carry over from a representative arm.
	c.loadBranch(branches[0])
	c.stack.items = append(c.stack.items[:0], mergedStack...)
	c.vars.bound = mergedBound
	c.vars.maybeBound = mergedMaybe
	c.diverged = false
}

// filterLiveBranchesWithLabels mirrors filterLiveBranches but also
// keeps a parallel labels slice in sync, dropping the label of any
// diverged branch.
func filterLiveBranchesWithLabels(branches []quoteBranch, labels []string) ([]quoteBranch, []string) {
	live := make([]quoteBranch, 0, len(branches))
	liveLabels := make([]string, 0, len(branches))
	for i, b := range branches {
		if b.diverged {
			continue
		}
		live = append(live, b)
		var lbl string
		if i < len(labels) {
			lbl = labels[i]
		}
		liveLabels = append(liveLabels, lbl)
	}
	return live, liveLabels
}

// formatArmBranchSizes renders one line per surviving arm-branch with
// its source-snippet label and tail stack as a `(top -- ... --
// bottom)` signature. Used as the hint for TErrBranchStackSize.
// Branches are loaded in turn so each line reflects the branch's own
// substitution.
func (c *Checker) formatArmBranchSizes(branches []quoteBranch, labels []string) string {
	const lead = "all arms must produce the same number of stack items"
	saved := c.captureBranch()
	defer c.loadBranch(saved)
	var sb strings.Builder
	sb.WriteString(lead)
	for i, b := range branches {
		c.loadBranch(b)
		resolved := make([]TypeId, len(c.stack.items))
		for j, t := range c.stack.items {
			resolved[j] = c.subst.Apply(c.arena, t)
		}
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		if label == "" {
			fmt.Fprintf(&sb, "\n  Branch %d:  %s", i+1, c.formatArmStack(resolved))
		} else {
			fmt.Fprintf(&sb, "\n  Branch %d (%s):  %s", i+1, label, c.formatArmStack(resolved))
		}
	}
	return sb.String()
}

// truncatePatternSnippet shortens a pattern snippet for use inline
// in the TErrBranchStackSize hint. Snippets up to 20 visible chars
// (covers the common list-pattern idioms like `[header ...rows]`)
// pass through unchanged; longer ones get a head/tail split joined
// by a single-char Unicode ellipsis so rest-binds (`...name`) stay
// readable next to the truncation marker.
func truncatePatternSnippet(s string) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	const cap = 20
	if utf8.RuneCountInString(collapsed) <= cap {
		return collapsed
	}
	runes := []rune(collapsed)
	return string(runes[:9]) + "…" + string(runes[len(runes)-9:])
}

// formatPatternSnippet renders a match-arm pattern as a short string,
// recursing into list / dict / quote literals so patterns like
// `[a ...rest]` display their contents rather than collapsing to
// `[...]` the way formatItemsSnippet does for arbitrary composites.
func formatPatternSnippet(items []MShellParseItem) string {
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(formatPatternItem(it))
	}
	return sb.String()
}

func formatPatternItem(it MShellParseItem) string {
	switch v := it.(type) {
	case Token:
		return v.Lexeme
	case *MShellParseList:
		return "[" + formatPatternSnippet(v.Items) + "]"
	default:
		start := it.GetStartToken().Lexeme
		end := it.GetEndToken().Lexeme
		if end != "" && end != start {
			return start + "…" + end
		}
		return start
	}
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
// Exhaustiveness is enforced via classifyArmPattern + CheckMatchExhaustive:
// the matched value's static type must be fully covered by the arm patterns,
// or a wildcard `_` arm must be present. Pattern-driven type narrowing inside
// arms is still a future improvement.
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
	entry := c.captureBranch()

	if len(matchBlock.Arms) == 0 {
		// Empty match block: no arms could fire. Treat as a no-op.
		// The runtime would error at first use; the checker keeps
		// the subject on the stack.
		return
	}

	var armBranches []quoteBranch
	var armLabels []string
	tags := make([]MatchArmTag, 0, len(matchBlock.Arms))
	for _, arm := range matchBlock.Arms {
		if len(arm.Pattern) > 0 && !c.armPatternRecognized(arm.Pattern) {
			c.errors = append(c.errors, TypeError{
				Kind: TErrInvalidMatchPattern,
				Pos:  arm.Pattern[0].GetStartToken(),
				Hint: matchPatternFormsHint,
			})
		}
		c.loadBranch(entry)
		c.diverged = false
		// Apply per-arm subject handling.
		if arm.Consume {
			// Pop the subject — body sees the stack without it.
			c.stack.items = c.stack.items[:c.stack.Len()-1]
		} else if nt, ok := c.narrowMatchSubject(arm.Pattern); ok {
			// Preserve (`:>`): narrow the on-stack subject to the arm's
			// matched type, so a `float`/`int`/... arm body sees that
			// type rather than the full union it matched against.
			c.stack.items[c.stack.Len()-1] = nt
		}
		// Pattern-introduced bindings flow into the captured arm
		// like any other binding. Each surviving branch keeps its
		// own bindings, so per-arm name disagreements naturally
		// surface as branch differences rather than maybeBound
		// lifts.
		c.bindMatchPattern(subject, arm.Pattern)

		label := truncatePatternSnippet(formatPatternSnippet(arm.Pattern))
		spawned := c.driveBranchesOverItems([]quoteBranch{c.captureBranch()}, arm.Body)
		for _, b := range spawned {
			armBranches = append(armBranches, b)
			armLabels = append(armLabels, label)
		}
		tags = append(tags, c.classifyArmPattern(arm.Pattern))
	}
	c.CheckMatchExhaustive(subject, tags, startTok)
	c.reconcileArmBranches(armBranches, armLabels, entry, startTok)
}

// classifyArmPattern reduces a parsed match arm pattern to the
// MatchArmTag the exhaustiveness checker understands. Anything not
// recognized as wildcard / just / none / true / false / a known type
// name is returned as MatchArmType with TidNothing — it counts as a
// non-wildcard arm but credits no coverage.
func (c *Checker) classifyArmPattern(pattern []MShellParseItem) MatchArmTag {
	if len(pattern) == 1 {
		if list, ok := pattern[0].(*MShellParseList); ok {
			// `[]` covers empty lists. `[... ...rest]` (any pattern
			// element whose LITERAL lexeme starts with `...`) is a
			// rest-binding form that matches every non-empty list
			// (or every list of length >= prefix count).
			if len(list.Items) == 0 {
				return MatchArmTag{Kind: MatchArmEmptyList}
			}
			for _, item := range list.Items {
				if tok, ok := item.(Token); ok && tok.Type == LITERAL &&
					strings.HasPrefix(tok.Lexeme, "...") {
					return MatchArmTag{Kind: MatchArmListWithRest}
				}
			}
		}
		tok, ok := pattern[0].(Token)
		if ok {
			switch tok.Type {
			case TYPEINT:
				return MatchArmTag{Kind: MatchArmType, TypeArm: TidInt}
			case TYPEFLOAT:
				return MatchArmTag{Kind: MatchArmType, TypeArm: TidFloat}
			case TYPEBOOL:
				return MatchArmTag{Kind: MatchArmType, TypeArm: TidBool}
			case STR:
				return MatchArmTag{Kind: MatchArmType, TypeArm: TidStr}
			case TRUE:
				return MatchArmTag{Kind: MatchArmTrue}
			case FALSE:
				return MatchArmTag{Kind: MatchArmFalse}
			case LITERAL:
				switch tok.Lexeme {
				case "_":
					return MatchArmTag{Kind: MatchArmWildcard}
				case "none":
					return MatchArmTag{Kind: MatchArmNone}
				case "path":
					return MatchArmTag{Kind: MatchArmType, TypeArm: TidPath}
				case "date":
					return MatchArmTag{Kind: MatchArmType, TypeArm: TidDateTime}
				case "binary":
					return MatchArmTag{Kind: MatchArmType, TypeArm: TidBytes}
				}
				// User-declared named type (e.g. `type X = A | B`).
				if tid := c.LookupType(tok.Lexeme); tid != TidNothing {
					return MatchArmTag{Kind: MatchArmType, TypeArm: tid}
				}
			}
		}
	}
	if len(pattern) == 2 {
		t0, ok0 := pattern[0].(Token)
		t1, ok1 := pattern[1].(Token)
		if ok0 && ok1 && t1.Type == LITERAL {
			if t0.Type == LITERAL && t0.Lexeme == "just" {
				return MatchArmTag{Kind: MatchArmJust}
			}
			// `<typekeyword> name` binds the matched value; for
			// exhaustiveness it covers the same type as the bare keyword.
			if tid, ok := c.typeKeywordTokenType(t0); ok {
				return MatchArmTag{Kind: MatchArmType, TypeArm: tid}
			}
			if tid := c.LookupType(t0.Lexeme); t0.Type == LITERAL && tid != TidNothing {
				return MatchArmTag{Kind: MatchArmType, TypeArm: tid}
			}
		}
	}
	return MatchArmTag{Kind: MatchArmType, TypeArm: TidNothing}
}

// typeKeywordTokenType maps a match type-keyword token to the concrete
// TypeId it tests for. Container/other keywords (list, dict, quotation,
// maybe) have no single primitive id and return ok=false; callers that
// need a binding type fall back to a fresh var or the subject.
func (c *Checker) typeKeywordTokenType(tok Token) (TypeId, bool) {
	switch tok.Type {
	case TYPEINT:
		return TidInt, true
	case TYPEFLOAT:
		return TidFloat, true
	case TYPEBOOL:
		return TidBool, true
	case STR:
		return TidStr, true
	case LITERAL:
		switch tok.Lexeme {
		case "path":
			return TidPath, true
		case "date":
			return TidDateTime, true
		case "binary":
			return TidBytes, true
		}
	}
	return TidNothing, false
}

// isTypeKeywordToken reports whether tok is one of the match type-keyword
// patterns: int, float, str, bool, list, dict, path, date, quotation,
// maybe, binary.
func isTypeKeywordToken(tok Token) bool {
	switch tok.Type {
	case TYPEINT, TYPEFLOAT, STR, TYPEBOOL:
		return true
	case LITERAL:
		switch tok.Lexeme {
		case "list", "dict", "path", "date", "quotation", "maybe", "binary":
			return true
		}
	}
	return false
}

// matchPatternFormsHint lists the legal match-arm pattern forms, used in
// the diagnostic raised when an arm pattern is not recognized.
const matchPatternFormsHint = "expected one of: `_`; a type keyword " +
	"(int, float, str, bool, list, dict, path, date, quotation, maybe, binary), " +
	"optionally followed by a binding name; a value literal (42, 1.5, \"text\", true, false, PATH); " +
	"`none`; `just <name>`; a list pattern `[ ... ]`; or a dict pattern `{ ... }`"

// armPatternRecognized reports whether pattern is one of the legal
// match-arm forms. Unrecognized patterns get a TErrInvalidMatchPattern
// diagnostic rather than silently binding nothing.
func (c *Checker) armPatternRecognized(pattern []MShellParseItem) bool {
	switch len(pattern) {
	case 1:
		switch p := pattern[0].(type) {
		case *MShellParseList, *MShellParseDict:
			return true
		case Token:
			return c.tokenPatternRecognized(p)
		}
		return false
	case 2:
		t0, ok0 := pattern[0].(Token)
		t1, ok1 := pattern[1].(Token)
		if !ok0 || !ok1 || t1.Type != LITERAL {
			return false
		}
		if t0.Type == LITERAL && t0.Lexeme == "just" {
			return true
		}
		if isTypeKeywordToken(t0) {
			return true
		}
		if t0.Type == LITERAL && c.LookupType(t0.Lexeme) != TidNothing {
			return true
		}
		return false
	}
	return false
}

// tokenPatternRecognized reports whether a single-token pattern is legal:
// a type keyword, value literal, `_`, `none`, or a user-declared type name.
func (c *Checker) tokenPatternRecognized(tok Token) bool {
	switch tok.Type {
	case TYPEINT, TYPEFLOAT, STR, TYPEBOOL,
		INTEGER, FLOAT, STRING, SINGLEQUOTESTRING, PATH, TRUE, FALSE:
		return true
	case LITERAL:
		if tok.Lexeme == "_" || tok.Lexeme == "none" {
			return true
		}
		if isTypeKeywordToken(tok) {
			return true
		}
		if c.LookupType(tok.Lexeme) != TidNothing {
			return true
		}
	}
	return false
}

// narrowMatchSubject returns the concrete primitive type that a
// type-keyword arm pattern matches (int/float/str/path/bool/date/binary),
// so the arm body can treat a union subject as that single type. These
// primitives have no subtypes, so replacing the subject with the matched
// primitive is a sound refinement regardless of the subject's static
// type. Non-type-keyword patterns (wildcard, just/none, value literals,
// list/dict destructuring) and user-declared union types return
// ok=false and leave the subject unrefined — narrowing those is a
// follow-up.
func (c *Checker) narrowMatchSubject(pattern []MShellParseItem) (TypeId, bool) {
	tag := c.classifyArmPattern(pattern)
	if tag.Kind != MatchArmType {
		return TidNothing, false
	}
	switch tag.TypeArm {
	case TidInt, TidFloat, TidStr, TidPath, TidBool, TidDateTime, TidBytes:
		return tag.TypeArm, true
	}
	return TidNothing, false
}

func (c *Checker) bindPatternName(name string, typ TypeId) {
	if name == "_" || name == "" {
		return
	}
	c.vars.bound[c.names.Intern(name)] = typ
}

// bindMatchPattern mirrors runtime match destructuring enough for body
// type-checking:
//   - `just name` binds the Maybe payload.
//   - `[a b ...rest]` binds element names and spread rest.
//   - `{ 'key': name }` binds dictionary value names.
//
// Value/type/wildcard patterns do not introduce bindings.
func (c *Checker) bindMatchPattern(subject TypeId, pattern []MShellParseItem) {

	// `just v` and `<typekeyword> name`
	if len(pattern) != 2 {
		goto single
	}
	if first, ok1 := pattern[0].(Token); ok1 {
		if second, ok2 := pattern[1].(Token); ok2 && second.Type == LITERAL {
			if first.Type == LITERAL && first.Lexeme == "just" {
				if second.Lexeme != "_" {
					resolved := c.subst.Apply(c.arena, subject)
					n := c.arena.Node(resolved)
					if n.Kind == TKMaybe {
						c.bindPatternName(second.Lexeme, TypeId(n.A))
					}
				}
				return
			}
			// `<typekeyword> name` binds the matched value to the type
			// the keyword tests for. Container/other keywords have no
			// single primitive id; bind to the subject (the runtime
			// binds the value as-is) which is sound if wider.
			if isTypeKeywordToken(first) {
				bindType, ok := c.typeKeywordTokenType(first)
				if !ok {
					bindType = c.subst.Apply(c.arena, subject)
				}
				c.bindPatternName(second.Lexeme, bindType)
				return
			}
		}
	}

single:
	if len(pattern) != 1 {
		return
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
				c.bindPatternName(tok.Lexeme[3:], c.arena.MakeList(elem))
			} else {
				c.bindPatternName(tok.Lexeme, elem)
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
				c.bindPatternName(tok.Lexeme, value)
			}
		}
	}
	return
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
				// Map the block's first content rune back to its
				// position in the original source so inner diagnostics
				// point at the interpolation, not 1:1.
				baseLine, baseCol := runePosition(runes, startIdx, tok.Line, tok.Column)
				c.checkFormatBlock(inner, tok, baseLine, baseCol)
				mode = modeNormal
				startIdx = -1
			}
		}
	}
}

// runePosition returns the one-based (line, column) of runes[idx] given
// that runes[0] sits at (baseLine, baseCol). Newlines reset the column to
// 1; every other rune advances the column by one.
func runePosition(runes []rune, idx, baseLine, baseCol int) (int, int) {
	line, col := baseLine, baseCol
	for i := 0; i < idx && i < len(runes); i++ {
		if runes[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// remapBlockToken rewrites a token's position from format-block-local
// coordinates (where the block source starts at line 1, column 1) to the
// surrounding source, given that the block content begins at
// (baseLine, baseCol). Lines past the first already start at column 1 in
// both coordinate systems, so only their line number shifts.
func remapBlockToken(t Token, baseLine, baseCol int) Token {
	if t.Line == 1 {
		t.Column = baseCol + (t.Column - 1)
		t.Line = baseLine
	} else {
		t.Line = baseLine + (t.Line - 1)
	}
	return t
}

// checkFormatBlock lexes/parses one interpolation block and walks its
// items on a fresh sub-stack. The current VarEnv is shared so `@name`
// references resolve against the surrounding scope. Errors are
// reported against the format-string token's position — finer source
// mapping inside the block is left for a follow-up. The outer stack
// and substitution are restored before returning.
func (c *Checker) checkFormatBlock(src string, callSite Token, baseLine, baseCol int) {
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

	errStart := len(c.errors)
	c.walkJoined(file.Items)
	// Diagnostics raised while walking the block carry block-local
	// positions; shift them back onto the original source.
	for i := errStart; i < len(c.errors); i++ {
		c.errors[i].Pos = remapBlockToken(c.errors[i].Pos, baseLine, baseCol)
	}

	if c.stack.Len() != 1 {
		c.errors = append(c.errors, TypeError{
			Kind: TErrInterpolationArity,
			Pos:  callSite,
			Hint: "format-string interpolation `{" + src + "}` must produce exactly one value, got " + strconv.Itoa(c.stack.Len()),
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
	case TKShape:
		for _, field := range c.arena.shapeFields[n.Extra] {
			if field.Name == name {
				return field.Type
			}
		}
	case TKDict:
		return TypeId(n.B)
	case TKGrid, TKGridView:
		// `:name` on Grid/GridView returns the column as a list. The
		// element type comes from the schema when known; otherwise a
		// fresh var stands in.
		schemaIdx := n.Extra
		if schemaIdx != 0 {
			for _, col := range c.arena.gridSchemas[schemaIdx].Columns {
				if col.Name == name {
					return c.arena.MakeList(col.Type)
				}
			}
		}
		return c.arena.MakeList(c.subst.FreshVar(c.arena))
	case TKBrand:
		// Branded values are exactly their underlying at runtime, so the
		// getter resolves against the underlying type.
		return c.lookupGetterValueType(TypeId(n.B), name)
	}
	return c.subst.FreshVar(c.arena)
}

// getterReceiverInvalid reports whether a `:name` getter applied to a
// receiver of type t is guaranteed to fail at runtime. The runtime's
// processGetter accepts only Dict, GridRow, Grid, and GridView (TKShape is
// the checker's view of a dict literal); every other concrete value —
// lists, primitives, Maybe, quotes, commands — hits the default branch and
// crashes.
//
// The "absolutely fails" bar drives the recursion:
//   - TKVar: not yet pinned; could still become a valid receiver, so
//     permissive (the only kind that legitimately defers).
//   - TKBrand: brands have no runtime representation — a branded value is
//     exactly its underlying — so validity follows the underlying type.
//   - TKUnion: a union value is one of its members at runtime, so the
//     getter only certainly fails when *every* member is invalid; if any
//     member could be a valid receiver the runtime might succeed.
func (c *Checker) getterReceiverInvalid(t TypeId) bool {
	id := c.subst.Apply(c.arena, t)
	n := c.arena.Node(id)
	switch n.Kind {
	case TKDict, TKShape, TKGridRow, TKGrid, TKGridView:
		return false
	case TKVar:
		return false
	case TKBrand:
		return c.getterReceiverInvalid(TypeId(n.B))
	case TKUnion:
		for _, m := range c.arena.UnionMembers(id) {
			if !c.getterReceiverInvalid(m) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func isSingleElementIndexer(indexers *MShellIndexerList) bool {
	if len(indexers.Indexers) != 1 {
		return false
	}
	tok, ok := indexers.Indexers[0].(Token)
	return ok && tok.Type == INDEXER
}

// indexerCandidates returns the overload arms for an indexer/slice whose
// result type is a pure function of the receiver and is therefore expressible
// as a generic signature. Receivers whose result is computed from the specific
// receiver (grids/gridviews → schema-carrying GridRow, brands) are handled
// directly at the call site, not here.
//
// elementIndex selects between single-element extraction (`:i:`) and a
// sub-range (`i:`, `:j`, `i:j`, or a multi-indexer). Both forms narrow path to
// str, matching the runtime.
func (c *Checker) indexerCandidates(elementIndex bool) []QuoteSig {
	a := c.arena
	v := a.MakeVar(0)
	if elementIndex {
		return []QuoteSig{
			{Inputs: []TypeId{a.MakeList(v)}, Outputs: []TypeId{v}, Generics: []TypeVarId{0}},
			{Inputs: []TypeId{a.MakeDict(TidStr, v)}, Outputs: []TypeId{v}, Generics: []TypeVarId{0}},
			{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidStr}},
			{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidStr}},
			{Inputs: []TypeId{TidBytes}, Outputs: []TypeId{TidBytes}},
		}
	}
	return []QuoteSig{
		{Inputs: []TypeId{a.MakeList(v)}, Outputs: []TypeId{a.MakeList(v)}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{a.MakeDict(TidStr, v)}, Outputs: []TypeId{v}, Generics: []TypeVarId{0}},
		{Inputs: []TypeId{TidStr}, Outputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidPath}, Outputs: []TypeId{TidStr}},
		{Inputs: []TypeId{TidBytes}, Outputs: []TypeId{TidBytes}},
	}
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
