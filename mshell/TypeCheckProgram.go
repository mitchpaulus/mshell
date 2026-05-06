package main

// Phase 10 step 3 (gate) + step 4 (program-flow walker).
//
// TypeCheckProgram is the entry point invoked from Main.go's
// `--check-types` gate. It does two passes:
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
// run under --check-types — that's the signal for what to register
// next.

// TypeCheckProgram runs the new Checker against the given file.
// Returns formatted error strings (one per error) and an
// exit-friendly bool: ok == true means no errors were found.
func TypeCheckProgram(file *MShellFile) (errors []string, ok bool) {
	arena := NewTypeArena()
	names := NewNameTable()
	checker := NewChecker(arena, names)

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

// CheckProgram is the file-level type-check pass. It registers all
// type declarations, then walks the parse tree driving the type
// stack. Error accumulation lives on the Checker.
func (c *Checker) CheckProgram(file *MShellFile) {
	// Pre-pass: register all type declarations.
	for _, item := range file.Items {
		if d, ok := item.(*MShellTypeDecl); ok {
			body := ResolveTypeExprAST(c, d.Body)
			c.DeclareType(d.Name, body)
		}
	}
	// Flow walk.
	for _, item := range file.Items {
		c.checkParseItem(item)
	}
}

// checkParseItem dispatches a single parse-tree item, advancing the
// type stack as appropriate. Unknown / not-yet-implemented item
// kinds are handled with placeholder stack effects so the rest of
// the walk doesn't cascade into garbage; this is acceptable while
// the walker grows.
func (c *Checker) checkParseItem(item MShellParseItem) {
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
		// Recurse into elements so any inner casts get walked.
		// Each element pushes onto the stack inside the recursion;
		// we collapse to a single list-of-fresh-var TypeId so the
		// outer stack reflects "a list pushed". Real per-element
		// inference comes when the walker matures.
		listScope := c.snapshotStack()
		for _, sub := range it.Items {
			c.checkParseItem(sub)
		}
		c.restoreStack(listScope)
		elem := c.subst.FreshVar(c.arena)
		c.stack.Push(c.arena.MakeList(elem))
		return

	case *MShellParseDict:
		// Walk values for nested casts; push an empty shape.
		for _, kv := range it.Items {
			scope := c.snapshotStack()
			for _, sub := range kv.Value {
				c.checkParseItem(sub)
			}
			c.restoreStack(scope)
		}
		c.stack.Push(c.arena.MakeShape(nil))
		return

	case *MShellParseQuote:
		// Walk body for inner casts; push a fresh quote sig
		// placeholder. Real quote-body inference (Phase 7 lives
		// behind InferQuoteSig) plugs in when the walker is
		// pulling tokens off the same stream.
		scope := c.snapshotStack()
		for _, sub := range it.Items {
			c.checkParseItem(sub)
		}
		c.restoreStack(scope)
		c.stack.Push(c.arena.MakeQuote(QuoteSig{}))
		return

	case *MShellParsePrefixQuote:
		// Treated like a no-op stack-wise for now.
		return

	case *MShellParseIfBlock:
		c.checkIfBlock(it)
		return

	case *MShellParseMatchBlock:
		// Match arm dispatch is more involved (pattern semantics on
		// the matched type drive arm typing). Walk arm bodies for
		// nested casts but otherwise no-op the stack effect.
		for _, arm := range it.Arms {
			scope := c.snapshotStack()
			for _, sub := range arm.Body {
				c.checkParseItem(sub)
			}
			c.restoreStack(scope)
		}
		return

	case *MShellParseGrid:
		c.stack.Push(c.arena.MakeGrid(0))
		return

	case *MShellIndexerList:
		// Indexing reads from the stack and pushes a fresh element
		// var. Conservative placeholder.
		if c.stack.Len() > 0 {
			c.stack.items = c.stack.items[:c.stack.Len()-1]
		}
		c.stack.Push(c.subst.FreshVar(c.arena))
		return

	case MShellVarstoreList:
		// Pop one stack item per name. The bound variable's type
		// is captured into VarEnv so subsequent getters can
		// resolve it.
		for i := len(it.VarStores) - 1; i >= 0; i-- {
			tok := it.VarStores[i]
			if c.stack.Len() == 0 {
				c.errors = append(c.errors, TypeError{
					Kind: TErrStackUnderflow,
					Pos:  tok,
					Hint: "varstore",
				})
				continue
			}
			top := c.stack.items[c.stack.Len()-1]
			c.stack.items = c.stack.items[:c.stack.Len()-1]
			c.vars.bound[c.names.Intern(tok.Lexeme)] = top
		}
		return

	case *MShellGetter:
		// `:name` lookup: push the var's current type (or fresh
		// var if unknown).
		nameId := c.names.Intern(it.String)
		if t, ok := c.vars.bound[nameId]; ok {
			c.stack.Push(t)
		} else {
			c.stack.Push(c.subst.FreshVar(c.arena))
		}
		return
	}
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
	arms := []BranchArm{c.CaptureArm(false)}

	// else-if arms.
	for _, elseIf := range ifBlock.ElseIfs {
		c.Fork(snap)
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
		arms = append(arms, c.CaptureArm(false))
	}

	// else body, or the implicit "did nothing" arm if absent.
	if len(ifBlock.ElseBody) > 0 {
		c.Fork(snap)
		for _, sub := range ifBlock.ElseBody {
			c.checkParseItem(sub)
		}
		arms = append(arms, c.CaptureArm(false))
	} else {
		c.Fork(snap)
		arms = append(arms, c.CaptureArm(false))
	}

	c.ReconcileArms(arms, startTok)
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
