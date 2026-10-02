package main

import (
	"fmt"
	"slices"
)

// Commands in the core checker (ai/type-core-calculus.typ, "Commands").
//
// A command is a list with a destination for each of its streams; the
// destinations are part of its type (TKCommand: the argument list, stdout's
// and stderr's state). A plain list is a command with no destinations. A
// pipe is a command over a list of commands.
//
// A redirect or capture sets a stream's destination on the list object in
// place, which changes its type, so the list must be fresh: `[cmd] *` is
// fine, `@c *` is an error that suggests deepCopy (P7). An input redirect
// (`<`) and `&` change nothing the type says. A redirect on a quote changes
// no type either, and a quote literal stays a literal: `(...) @f > loop`.
//
// Running a command (`;`, `!`, `?`) pushes what its captures give, and `?`
// the exit code.

// commandParts reads a list or command type: its argument list and stream
// states.
func (c *coreChecker) commandParts(t TypeId) (argv TypeId, out, errs CommandCaptureMode, ok bool) {
	n := c.arena.nodes[t]
	switch n.Kind {
	case TKList:
		return t, CommandCaptureNone, CommandCaptureNone, true
	case TKCommand:
		return TypeId(n.A), CommandCaptureMode(n.B) &^ CommandPipe, CommandCaptureMode(n.Extra), true
	}
	return 0, 0, 0, false
}

// isPipe reports whether t is a pipe.
func (c *coreChecker) isPipe(t TypeId) bool {
	n := c.arena.nodes[t]
	return n.Kind == TKCommand && CommandCaptureMode(n.B)&CommandPipe != 0
}

// makeCommand builds a list's or pipe's type from its parts.
func (c *coreChecker) makeCommand(pipe bool, argv TypeId, out, errs CommandCaptureMode) TypeId {
	if pipe {
		out |= CommandPipe
	} else if out == CommandCaptureNone && errs == CommandCaptureNone {
		return argv
	}
	return c.arena.MakeCommand(argv, out, errs)
}

// commandWord checks the command and redirect tokens. It reports false
// when the operands are not a command or a quote, for the table (`*` and
// `<` on numbers, `?` on a Maybe).
func (c *coreChecker) commandWord(tok Token) bool {
	switch tok.Type {
	case GREATERTHAN, LESSTHAN, STDAPPEND, STDERRREDIRECT, STDERRAPPEND,
		STDOUTANDSTDERRREDIRECT, STDOUTANDSTDERRAPPEND, INPLACEREDIRECT:
		return c.redirect(tok)
	case ASTERISK, ASTERISKBINARY, CARET, CARETBINARY:
		return c.capture(tok)
	case STDERRTOSTDOUT, STDOUTTOSTDERR:
		return c.merge(tok)
	case EXECUTE, BANG, QUESTION:
		return c.run(tok)
	case PIPE:
		return c.pipe(tok)
	case AMPERSAND:
		return c.background(tok)
	case LITERAL:
		switch tok.Lexeme {
		case "e", "es", "ec":
			return c.capture(tok)
		}
	}
	return false
}

// isCommandToken reports whether commandWord handles tokens of type tt.
func isCommandToken(tt TokenType) bool {
	switch tt {
	case GREATERTHAN, LESSTHAN, STDAPPEND, STDERRREDIRECT, STDERRAPPEND,
		STDOUTANDSTDERRREDIRECT, STDOUTANDSTDERRAPPEND, INPLACEREDIRECT,
		ASTERISK, ASTERISKBINARY, CARET, CARETBINARY, STDERRTOSTDOUT, STDOUTTOSTDERR,
		EXECUTE, BANG, QUESTION, PIPE, AMPERSAND:
		return true
	}
	return false
}

// operand reads the slot at i as a command; quote is true for a quote,
// waiting or typed.
func (c *coreChecker) operand(i int) (t TypeId, quote, ok bool) {
	s := c.stack[i]
	if c.waiting(s) != nil {
		return TidNothing, true, true
	}
	// An alias is its body (`type Cmd = [str]`).
	t = c.unfold(c.subst.Apply(c.arena, s.t))
	if c.arena.nodes[t].Kind == TKQuote {
		return t, true, true
	}
	_, _, _, ok = c.commandParts(t)
	return t, false, ok
}

func (c *coreChecker) cmdError(tok Token, hint string) {
	c.errs = append(c.errs, TypeError{Kind: TErrTypeMismatch, Pos: tok, Hint: hint})
}

// setStates replaces a command operand's type after a change of its stream
// states; the list must be fresh.
func (c *coreChecker) setStates(i int, tok Token, argv TypeId, out, errs CommandCaptureMode) {
	if !c.stack[i].fresh {
		c.cmdError(tok, "'"+tok.Lexeme+"' changes where a command's output goes, which is part of its type, "+
			"so the list must be new, and this one may be shared (stored or duplicated); "+
			"write the redirect right after the list literal, or deepCopy the list first")
	}
	c.stack[i].t = c.makeCommand(c.isPipe(c.subst.Apply(c.arena, c.stack[i].t)), argv, out, errs)
}

// claimed reports a stream that already has a destination.
func (c *coreChecker) claimed(tok Token, stream string, mode CommandCaptureMode, stdout bool) bool {
	if desc := streamStateDesc(mode, stdout); desc != "" {
		c.cmdError(tok, fmt.Sprintf("Cannot apply '%s': %s already has %s. Each stream has exactly one destination.", tok.Lexeme, stream, desc))
		return true
	}
	return false
}

func (c *coreChecker) redirect(tok Token) bool {
	if len(c.stack)-c.floor < 2 {
		return false
	}
	n := len(c.stack)
	c.force(n - 1)
	target := c.subst.Apply(c.arena, c.stack[n-1].t)
	ot, quote, ok := c.operand(n - 2)
	if !ok {
		return false
	}
	switch target {
	case TidStr, TidPath:
	case TidBytes:
		if tok.Type != LESSTHAN {
			c.cmdError(tok, "only '<' takes bytes; '"+tok.Lexeme+"' needs a file name (str or path)")
		}
	default:
		if quote || c.arena.nodes[target].Kind != TKPrim {
			c.mismatch(tok, 1, c.arena.MakeUnion([]TypeId{TidStr, TidPath}), target)
		} else {
			return false
		}
	}
	c.stack = c.stack[:n-1]
	if quote {
		if tok.Type == INPLACEREDIRECT {
			c.cmdError(tok, "In-place redirect (<>) requires a List, found a quotation.")
		}
		return true
	}
	if c.isPipe(ot) {
		c.cmdError(tok, "Cannot redirect a Pipe. Add the redirection to the final item in the pipeline.")
		return true
	}
	argv, out, errs, _ := c.commandParts(ot)
	switch tok.Type {
	case LESSTHAN:
		// Input: nothing the type says changes.
		return true
	case GREATERTHAN, STDAPPEND:
		if !c.claimed(tok, "stdout", out, true) {
			out = CommandDestFile
		}
	case STDERRREDIRECT, STDERRAPPEND:
		if !c.claimed(tok, "stderr", errs, false) {
			errs = CommandDestFile
		}
	case STDOUTANDSTDERRREDIRECT, STDOUTANDSTDERRAPPEND:
		if !c.claimed(tok, "stdout", out, true) && !c.claimed(tok, "stderr", errs, false) {
			out, errs = CommandDestFile, CommandDestFile
		}
	case INPLACEREDIRECT:
		if target != TidPath {
			c.cmdError(tok, "In-place redirect (<>) requires a Path target (`...`).")
		}
		if errs == CommandDestMerged {
			c.cmdError(tok, fmt.Sprintf("Cannot apply '%s' with '2>&1': stderr would be written back into the edited file.", tok.Lexeme))
		} else if !c.claimed(tok, "stdout", out, true) {
			out = CommandDestInPlace
		}
	}
	c.setStates(n-2, tok, argv, out, errs)
	return true
}

func (c *coreChecker) capture(tok Token) bool {
	if len(c.stack)-c.floor < 1 {
		return false
	}
	n := len(c.stack)
	ot, quote, ok := c.operand(n - 1)
	if !ok {
		return false
	}
	if quote {
		c.cmdError(tok, fmt.Sprintf("'%s' capture is not supported on quotations; it would change the quotation's stack effect. Capture the individual command lists inside instead.", tok.Lexeme))
		return true
	}
	argv, out, errs, _ := c.commandParts(ot)
	switch tok.Type {
	case ASTERISK, ASTERISKBINARY:
		mode := CommandCaptureStr
		if tok.Type == ASTERISKBINARY {
			mode = CommandCaptureBytes
		}
		if !c.claimed(tok, "stdout", out, true) {
			out = mode
		}
	default:
		mode := CommandCaptureStr
		switch {
		case tok.Type == CARETBINARY:
			mode = CommandCaptureBytes
		case tok.Lexeme == "e":
			mode = CommandCaptureLines
		}
		if !c.claimed(tok, "stderr", errs, false) {
			errs = mode
		}
	}
	c.setStates(n-1, tok, argv, out, errs)
	return true
}

func (c *coreChecker) merge(tok Token) bool {
	if !c.need(1, tok) {
		return true
	}
	n := len(c.stack)
	ot, quote, ok := c.operand(n - 1)
	if quote {
		return true
	}
	if !ok {
		c.cmdError(tok, fmt.Sprintf("Cannot apply '%s' to a %s; expected a list or quotation.", tok.Lexeme, c.format(ot)))
		return true
	}
	if c.isPipe(ot) {
		c.cmdError(tok, fmt.Sprintf("Cannot apply '%s' to a Pipe. Add it to a command in the pipeline.", tok.Lexeme))
		return true
	}
	argv, out, errs, _ := c.commandParts(ot)
	if tok.Type == STDERRTOSTDOUT {
		switch {
		case out == CommandDestMerged:
			c.cmdError(tok, fmt.Sprintf("Cannot apply '%s': the other stream is already merged with '1>&2'; merging both streams into each other is circular.", tok.Lexeme))
		case out == CommandDestInPlace:
			c.cmdError(tok, fmt.Sprintf("Cannot apply '%s' with an in-place redirect ('<>'): stderr would be written back into the edited file.", tok.Lexeme))
		case !c.claimed(tok, "stderr", errs, false):
			errs = CommandDestMerged
		}
	} else {
		switch {
		case errs == CommandDestMerged:
			c.cmdError(tok, fmt.Sprintf("Cannot apply '%s': the other stream is already merged with '2>&1'; merging both streams into each other is circular.", tok.Lexeme))
		case !c.claimed(tok, "stdout", out, true):
			out = CommandDestMerged
		}
	}
	c.setStates(n-1, tok, argv, out, errs)
	return true
}

// commandLineable reports whether every value of t can be a command-line
// argument: a string, path, number or date.
// A list of such values is flattened into the command line.
func (c *coreChecker) commandLineable(t TypeId) bool {
	return c.commandLineableIn(t, nil)
}

// commandLineableIn is commandLineable, with the list types being looked
// inside. A list type met again is a recursive one: refused, since the
// runtime flattens a command's lists without looking for cycles, and a
// value of that type may contain itself.
func (c *coreChecker) commandLineableIn(t TypeId, visiting []TypeId) bool {
	var members []TypeId
	if !c.members(c.subst.Apply(c.arena, t), &members) {
		return false
	}
	for _, m := range members {
		switch m {
		case TidStr, TidPath, TidInt, TidFloat, TidDateTime, TidBottom:
		default:
			switch c.arena.nodes[m].Kind {
			case TKVar:
			case TKList:
				if slices.Contains(visiting, m) || !c.commandLineableIn(c.listElem(m), append(visiting, m)) {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

// run checks `;`, `!` and `?` on a command or pipe.
func (c *coreChecker) run(tok Token) bool {
	if len(c.stack)-c.floor < 1 {
		return false
	}
	n := len(c.stack)
	ot, quote, ok := c.operand(n - 1)
	if !ok || quote {
		return false
	}
	argv, out, errs, _ := c.commandParts(ot)
	if out == CommandDestVaried || errs == CommandDestVaried {
		c.cmdError(tok, "this command came from commands with different redirects, so what running it gives is not known here")
	}
	elem := c.listElem(argv)
	if c.isPipe(ot) {
		for _, m := range c.listMembers(elem) {
			a, _, _, _ := c.commandParts(m)
			if !c.commandLineable(c.listElem(a)) {
				c.cmdError(tok, "a command's arguments must be strings, paths, numbers or dates; this pipeline has "+c.format(m))
			}
		}
	} else if !c.commandLineable(elem) {
		c.cmdError(tok, "a command's arguments must be strings, paths, numbers or dates, not "+c.format(elem))
	}
	c.stack = c.stack[:n-1]
	push := func(mode CommandCaptureMode) {
		switch mode {
		case CommandCaptureStr:
			c.push(TidStr, true)
		case CommandCaptureBytes:
			c.push(TidBytes, true)
		case CommandCaptureLines:
			c.push(c.arena.MakeList(TidStr), true)
		}
	}
	push(out)
	push(errs)
	if tok.Type == QUESTION {
		c.push(TidInt, true)
	}
	return true
}

// listMembers is the members of a union, or the type itself.
func (c *coreChecker) listMembers(t TypeId) []TypeId {
	var ms []TypeId
	if !c.members(c.subst.Apply(c.arena, t), &ms) {
		return []TypeId{t}
	}
	return ms
}

// pipe checks `|`: a list of commands becomes a pipe, fresh when the list
// was.
func (c *coreChecker) pipe(tok Token) bool {
	if !c.need(1, tok) {
		return true
	}
	n := len(c.stack)
	c.force(n - 1)
	t := c.subst.Apply(c.arena, c.stack[n-1].t)
	// The list's own captures become the pipe's.
	argv, out, errs, ok := c.commandParts(t)
	if !ok || c.isPipe(t) {
		c.cmdError(tok, "'|' needs a list of commands, got "+c.format(t))
		return true
	}
	t = argv
	for _, m := range c.listMembers(c.listElem(t)) {
		if _, _, _, ok := c.commandParts(m); !ok {
			c.cmdError(tok, "'|' needs a list of commands, but an element is "+c.format(m))
			return true
		}
	}
	c.stack[n-1].t = c.makeCommand(true, t, out, errs)
	return true
}

// background checks `&`: the command runs in the background when it is
// run; its type is unchanged.
func (c *coreChecker) background(tok Token) bool {
	if !c.need(1, tok) {
		return true
	}
	if ot, quote, ok := c.operand(len(c.stack) - 1); !ok || quote {
		c.cmdError(tok, "'&' needs a command")
	} else if c.isPipe(ot) {
		c.cmdError(tok, "'&' runs one command in the background, not a Pipe")
	}
	return true
}

// streamStateDesc mirrors the runtime's StdoutDestinationDesc /
// StderrDestinationDesc strings so static and runtime conflict errors read
// identically. Returns "" for an unclaimed stream.
func streamStateDesc(mode CommandCaptureMode, isStdout bool) string {
	switch mode {
	case CommandCaptureStr, CommandCaptureBytes, CommandCaptureLines:
		if isStdout {
			return "a capture ('*')"
		}
		return "a capture ('^')"
	case CommandDestFile:
		if isStdout {
			return "a file redirect ('>')"
		}
		return "a file redirect ('2>')"
	case CommandDestInPlace:
		return "an in-place redirect ('<>')"
	case CommandDestMerged:
		if isStdout {
			return "a merge to stderr ('1>&2')"
		}
		return "a merge to stdout ('2>&1')"
	case CommandDestVaried:
		return "a destination that differs between the commands it came from"
	}
	return ""
}
