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
// Bracketed paste and the kitty keyboard protocol go with raw mode, so a
// child never inherits them.

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
	if err := state.setKittyKeys(true); err != nil {
		state.leaveRawMode()
		return err
	}
	return nil
}

// leaveRawMode restores the saved cooked state for command execution, the
// opaque prompt, and every exit path.
func (state *TermState) leaveRawMode() {
	state.setKittyKeys(false)
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

// kittyKeysFlags are the kitty keyboard protocol flags the editor asks for:
// 1, every key unambiguous (Escape, Alt chords, Ctrl-I apart from Tab), and
// 4, the key's place on a US layout, so Ctrl and Alt bindings follow it.
const kittyKeysFlags = "5"

// setKittyKeys pushes the kitty keyboard protocol's flags while the editor
// reads a line, and pops them before anything else gets the terminal.
func (state *TermState) setKittyKeys(enabled bool) error {
	if !state.kittyKeys || state.kittyKeysPushed == enabled {
		return nil
	}
	sequence := "\x1b[<u"
	if enabled {
		sequence = "\x1b[>" + kittyKeysFlags + "u"
	}
	if _, err := io.WriteString(os.Stdout, sequence); err != nil {
		return err
	}
	state.kittyKeysPushed = enabled
	return nil
}

// detectKittyKeys asks the terminal whether it has the kitty keyboard
// protocol, unless MSH_KITTY_KEYS is 0. It sends the protocol's query and
// then DA1, which every terminal answers, and reads until the DA1 reply:
// a terminal with the protocol answers the query first. Keys that arrive
// meanwhile are queued for the editor. The terminal must be raw.
func (state *TermState) detectKittyKeys() {
	if os.Getenv("MSH_KITTY_KEYS") == "0" {
		return
	}
	if _, err := io.WriteString(os.Stdout, "\x1b[?u\x1b[c"); err != nil {
		return
	}
	for {
		token, err := state.InteractiveLexer(state.stdInState)
		if err != nil {
			return
		}
		if _, ok := token.(EofTerminalToken); ok {
			return
		}
		if reply, ok := token.(CsiToken); ok && len(reply.Params) > 0 && reply.Params[0] == '?' {
			switch reply.FinalChar {
			case 'u':
				state.kittyKeys = true
				continue
			case 'c':
				return
			}
		}
		state.queuedInput = append(state.queuedInput, token)
	}
}
