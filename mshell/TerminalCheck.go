package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// --check-terminal exercises the real region renderer and probe transaction in
// the terminal it runs in, then asks that terminal where the cursor landed.
// Layout predicts a cursor cell for every frame; a cluster that wraps or
// advances differently in context than in its probe moves the real cursor
// somewhere else. Frames stay on screen above the report for eyeballing.
// This is a maintainer diagnostic and is deliberately absent from --help.

type terminalCheckInput struct {
	name    string
	text    SourceText
	cluster string // Measured cluster whose cached width is reported, or empty.
}

func terminalCheckInputs(columns int) []terminalCheckInput {
	// The check prompt "> " leaves the command origin at column three.
	fill := strings.Repeat("a", max(0, columns-3-1))
	return []terminalCheckInput{
		{"ascii", "echo hello", ""},
		{"long paste", SourceText(strings.Repeat("abcdefghij ", (columns*5/2)/11+1)), ""},
		{"combining", "café x", "é"},
		{"cjk", "世界 x", "世"},
		{"vs15 text", "☃︎ x", "☃︎"},
		{"vs16 emoji", "❤️ x", "❤️"},
		{"flag", "🇺🇸 x", "🇺🇸"},
		{"skin tone", "👍🏽 x", "👍🏽"},
		{"keycap", "1️⃣ x", "1️⃣"},
		{"zwj family", "👨‍👩‍👧‍👦 x", "👨‍👩‍👧‍👦"},
		{"tab", "a\tb", tabGlyph},
		{"control", "a\x1bb", ""},
		{"forced wrap", SourceText(fill) + "世 x", "世"},
	}
}

func runTerminalCheck() int {
	out := os.Stdout
	stdInFd := int(os.Stdin.Fd())
	if !term.IsTerminal(stdInFd) || !term.IsTerminal(int(out.Fd())) {
		fmt.Fprintln(os.Stderr, "--check-terminal needs a terminal on stdin and stdout")
		return 2
	}
	state := TermState{
		stdInFd:      stdInFd,
		stdInState:   &StdinReaderState{array: make([]byte, 1024)},
		widthCache:   WidthCache{Entries: make(map[string]Cells)},
	}
	state.UpdateSize()
	if err := state.saveTerminalState(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := state.enterRawMode(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer state.leaveRawMode()

	fmt.Fprintf(out, "mshell %s check %dx%d %s\r\n", mshellVersion, state.numCols, state.numRows, os.Getenv("TERM"))
	if state.numCols < 4 || state.numRows < 2 {
		fmt.Fprint(out, "terminal too small: interactive use needs at least 4 columns and 2 rows\r\n")
		return 1
	}

	read := func() (TerminalToken, error) { return state.InteractiveLexer(state.stdInState) }
	failures := 0
	for _, input := range terminalCheckInputs(state.numCols) {
		if _, err := io.WriteString(out, "> "); err != nil {
			return 2
		}
		if err := state.anchorPrompt(out); err != nil {
			fmt.Fprintf(out, "\r\nanchor failed: %s\r\n", err)
			return 2
		}
		state.currentCommand = input.text
		state.index = state.commandEnd()
		region := &state.commandRegion
		ready, err := state.refreshCommandDisplay(out, read, region)
		if err == nil && !ready {
			// Keys pressed during the check are discarded; the widths are
			// already committed, so the second pass paints from the cache.
			state.queuedInput = state.queuedInput[:0]
			state.queuedInputIndex = 0
			ready, err = state.refreshCommandDisplay(out, read, region)
		}
		if err != nil || !ready {
			fmt.Fprintf(out, "\r\npaint failed: ready %v, %v\r\n", ready, err)
			return 2
		}

		layout := state.displayLayout
		cursorRow, cursorCol := layout.CursorRow, layout.CursorCol
		if cursorCol == Cells(state.numCols) {
			cursorRow++
			cursorCol = 0
		}
		wantRow := int(region.OriginRow) + int(cursorRow)
		wantCol := int(cursorCol) + 1
		gotRow, gotCol, err := state.queryCursorPosition(out)
		if err != nil {
			fmt.Fprintf(out, "\r\ncursor query failed: %s\r\n", err)
			return 2
		}
		state.queuedInput = state.queuedInput[:0]
		state.queuedInputIndex = 0

		// Leave the frame intact and report on the row below it.
		below := region.PaintedRows + region.TrailerRows - 1 - int(region.CursorRow)
		if _, err := io.WriteString(out, "\r"); err != nil {
			return 2
		}
		if below > 0 {
			fmt.Fprintf(out, "\033[%dB", below)
		}
		io.WriteString(out, "\r\n")

		verdict := "PASS"
		if gotRow != wantRow || gotCol != wantCol {
			verdict = "FAIL"
			failures++
		}
		width := "-"
		if input.cluster != "" {
			if cells, ok := state.widthCache.Entries[input.cluster]; ok {
				width = fmt.Sprint(cells)
			} else {
				width = "?" // placeholder: unmeasured, rejected, or too wide for this terminal
			}
		}
		fmt.Fprintf(out, "%s %-11s w=%s r=%d %d,%d/%d,%d\r\n", verdict, input.name, width, len(layout.Rows), wantRow, wantCol, gotRow, gotCol)
		if state.widthProbesBlocked {
			fmt.Fprint(out, "     width batch rejected\r\n")
			state.widthProbesBlocked = false
		}
	}
	fmt.Fprintf(out, "%d failures\r\n", failures)
	if failures > 0 {
		return 1
	}
	return 0
}
