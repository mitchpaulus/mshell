package main

import (
	"golang.org/x/term"
)

// The interactive editor owns two terminal modes. Raw is defined explicitly
// per platform in setRawTerminalMode so the flags the renderer and lexer rely
// on cannot drift with a library's idea of raw. Cooked is always the exact
// state saved when interactive mode began: children expect what the user had.

// saveTerminalState records the cooked state once, at interactive start.
func (state *TermState) saveTerminalState() error {
	saved, err := term.GetState(state.stdInFd)
	if err != nil {
		return err
	}
	state.oldState = *saved
	return nil
}

// enterRawMode applies the explicit raw definition. It never changes the
// saved cooked state, so it can be called after every child command.
func (state *TermState) enterRawMode() error {
	return setRawTerminalMode(state.stdInFd)
}

// leaveRawMode restores the saved cooked state for command execution, the
// opaque prompt, and every exit path.
func (state *TermState) leaveRawMode() {
	term.Restore(state.stdInFd, &state.oldState)
}
