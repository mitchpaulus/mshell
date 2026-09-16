package main

import (
	"io"
	"os"

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
	if err := setRawTerminalMode(state.stdInFd); err != nil {
		return err
	}
	if err := state.setBracketedPaste(true); err != nil {
		state.leaveRawMode()
		return err
	}
	return nil
}

// leaveRawMode restores the saved cooked state for command execution, the
// opaque prompt, and every exit path.
func (state *TermState) leaveRawMode() {
	state.setBracketedPaste(false)
	term.Restore(state.stdInFd, &state.oldState)
}

// Bracketed paste belongs to the command editor, not child programs or the
// file manager. Keep its lifetime paired with the editor's terminal ownership.
func (state *TermState) setBracketedPaste(enabled bool) error {
	if state.bracketedPasteEnabled == enabled {
		return nil
	}
	sequence := "\x1b[?2004l"
	if enabled {
		sequence = "\x1b[?2004h"
	}
	if _, err := io.WriteString(os.Stdout, sequence); err != nil {
		return err
	}
	state.bracketedPasteEnabled = enabled
	return nil
}
