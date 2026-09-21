package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadPromptLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "newline terminated", input: "hello\n", want: "hello"},
		{name: "crlf terminated", input: "hello\r\n", want: "hello"},
		{name: "eof without newline", input: "hello", want: "hello"},
		{name: "empty eof", input: "", want: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readPromptLine(strings.NewReader(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error mismatch: got %v wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("line mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestReadPromptFromTTYNoTTY(t *testing.T) {
	originalOpenPromptTTY := openPromptTTYFunc
	openPromptTTYFunc = func() (*promptTTYIO, error) {
		return nil, errors.New("no tty")
	}
	t.Cleanup(func() {
		openPromptTTYFunc = originalOpenPromptTTY
	})

	_, err := readPromptFromTTY("Enter value: ")
	if err == nil {
		t.Fatalf("expected readPromptFromTTY to fail when no tty is available")
	}
}

func TestReadPromptFromTTYWritesPromptAndReadsLine(t *testing.T) {
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create input pipe: %v", err)
	}

	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create output pipe: %v", err)
	}

	_, err = inputWriter.WriteString("typed response\n")
	if err != nil {
		t.Fatalf("failed to write input data: %v", err)
	}
	_ = inputWriter.Close()

	originalOpenPromptTTY := openPromptTTYFunc
	openPromptTTYFunc = func() (*promptTTYIO, error) {
		return &promptTTYIO{
			input:  inputReader,
			output: outputWriter,
		}, nil
	}
	t.Cleanup(func() {
		openPromptTTYFunc = originalOpenPromptTTY
		_ = inputReader.Close()
		_ = outputReader.Close()
	})

	line, err := readPromptFromTTY("Enter value: ")
	if err != nil {
		t.Fatalf("readPromptFromTTY returned unexpected error: %v", err)
	}
	if line != "typed response" {
		t.Fatalf("line mismatch: got %q want %q", line, "typed response")
	}

	promptBytes, err := io.ReadAll(outputReader)
	if err != nil {
		t.Fatalf("failed reading prompt output: %v", err)
	}
	if string(promptBytes) != "Enter value: " {
		t.Fatalf("prompt mismatch: got %q want %q", string(promptBytes), "Enter value: ")
	}
}

func TestStreamIsTerminalReturnsFalseForAbstractStreams(t *testing.T) {
	if streamIsTerminal(&bytes.Buffer{}, os.Stdout) {
		t.Fatal("an in-memory byte stream must not be reported as a terminal")
	}
}

func TestStreamIsTerminalReturnsFalseForPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer reader.Close()
	defer writer.Close()

	if streamIsTerminal(reader, os.Stdin) {
		t.Fatal("a pipe reader must not be reported as a terminal")
	}
	if streamIsTerminal(writer, os.Stdout) {
		t.Fatal("a pipe writer must not be reported as a terminal")
	}
}

func openTestTerminal(t *testing.T) *os.File {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("/dev/ptmx test is Linux-specific")
	}

	terminal, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() {
		_ = terminal.Close()
	})
	return terminal
}

func TestStreamIsTerminalReturnsTrueForPTY(t *testing.T) {
	terminal := openTestTerminal(t)
	if !streamIsTerminal(terminal, os.Stdout) {
		t.Fatal("a PTY must be reported as a terminal")
	}
}

func TestStreamIsTerminalFollowsSymlinkTarget(t *testing.T) {
	terminal := openTestTerminal(t)
	tempDir := t.TempDir()

	terminalLink := filepath.Join(tempDir, "terminal-link")
	if err := os.Symlink(terminal.Name(), terminalLink); err != nil {
		t.Fatalf("failed to create terminal symlink: %v", err)
	}
	terminalFile, err := os.OpenFile(terminalLink, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open terminal symlink: %v", err)
	}
	defer terminalFile.Close()

	if !streamIsTerminal(terminalFile, os.Stdout) {
		t.Fatal("a symlink to a PTY must be reported as a terminal")
	}

	regularPath := filepath.Join(tempDir, "regular-file")
	if err := os.WriteFile(regularPath, nil, 0600); err != nil {
		t.Fatalf("failed to create regular file: %v", err)
	}
	regularLink := filepath.Join(tempDir, "regular-link")
	if err := os.Symlink(regularPath, regularLink); err != nil {
		t.Fatalf("failed to create regular-file symlink: %v", err)
	}
	regularFile, err := os.OpenFile(regularLink, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open regular-file symlink: %v", err)
	}
	defer regularFile.Close()

	if streamIsTerminal(regularFile, os.Stdout) {
		t.Fatal("a symlink to a regular file must not be reported as a terminal")
	}
}

func TestPromptNewlineScreen(t *testing.T) {
	for _, columns := range []int{2, 4, 8, 80} {
		for _, startRow := range []int{1, 4} {
			for count := 0; count <= columns; count++ {
				t.Run(fmt.Sprintf("width%d/row%d/output%d", columns, startRow, count), func(t *testing.T) {
					screen := newCommandScreen(t, 5, columns)
					screen.row = startRow
					io.WriteString(screen, strings.Repeat("x", count))
					// Model the existing one-cell marker as ASCII so the screen's
					// Unicode width-probe safety checks don't forbid margin placement.
					sequence := strings.ReplaceAll(promptNewlineSequence(columns), "⏎", "%")
					io.WriteString(screen, sequence)
					wantRow, wantScrolls := startRow, 0
					if count > 0 {
						wantRow++
						if wantRow == 5 { wantRow--; wantScrolls++ }
						wantOutput := strings.Repeat("x", count)
						if count < columns { wantOutput += "%" }
						wantOutput += strings.Repeat(" ", columns-len(wantOutput))
						if got := screen.line(wantRow-1); got != wantOutput {
							t.Fatalf("output %q, want %q", got, wantOutput)
						}
					}
					if screen.row != wantRow || screen.col != 0 || screen.pending || screen.scrolls != wantScrolls {
						t.Fatalf("cursor (%d,%d), pending %v, scrolls %d; want (%d,0), no pending wrap, scrolls %d", screen.row, screen.col, screen.pending, screen.scrolls, wantRow, wantScrolls)
					}
					if got := screen.line(wantRow); got != strings.Repeat(" ", columns) {
						t.Fatalf("prompt line not cleared: %q", got)
					}
					if len(screen.replies) != 0 { t.Fatal("newline handling requested terminal input") }
					// A second prompt preparation at column one must not add a line.
					io.WriteString(screen, sequence)
					if screen.row != wantRow || screen.col != 0 || screen.pending || screen.scrolls != wantScrolls {
						t.Fatal("repeated preparation moved the prompt")
					}
				})
			}
		}
	}
}

func TestPromptNewlineUnusableWidth(t *testing.T) {
	for _, columns := range []int{-1, 0, 1, int(maxTerminalCoordinate)+1} {
		if got := promptNewlineSequence(columns); got != "\r\n" {
			t.Fatalf("width %d: got %q, want unconditional newline", columns, got)
		}
	}
}

func TestPromptLayoutWithoutCursorQuery(t *testing.T) {
	for _, columns := range []int{4, 8, 20} {
		for _, startRow := range []int{0, 2, 4} {
			for _, prompt := range []SourceText{"~ (0)> \n:: ", "/a/very/long/path (3)> \n:: ", "世/é (0)> \n:: ", "??? >", "abcd"} {
				t.Run(fmt.Sprintf("%dx5/row%d/%s", columns, startRow, prompt), func(t *testing.T) {
					screen := newCommandScreen(t, 5, columns)
					screen.row = startRow
					state := TermState{numRows: 5, numCols: columns, currentCommand: "abc", index: 3}
					state.widthCache.remember("世", 2)
					state.widthCache.remember("é", 1)
					read := func() (TerminalToken, error) { t.Fatal("known prompt requested input"); return nil, io.EOF }
					if err := state.paintPrompt(screen, read, prompt); err != nil { t.Fatal(err) }
					if len(screen.replies) != 0 || !state.commandRegion.RelativeOrigin || state.commandRegion.OriginRow != 0 {
						t.Fatal("prompt obtained an absolute row")
					}
					if screen.pending || int(state.commandRegion.OriginCol) != screen.col+1 {
						t.Fatalf("incorrect origin: region %+v, screen column %d", state.commandRegion, screen.col)
					}
					if state.numPromptLines != screen.row+screen.scrolls-startRow+1 {
						t.Fatalf("prompt height %d does not match rendered rows", state.numPromptLines)
					}
					if state.currentCommand != "abc" || state.index != 3 { t.Fatal("prompt changed editor text") }
					// Repainting and scrolling work without an absolute origin.
					for _, command := range []SourceText{"abc", SourceText(strings.Repeat("x", columns*6)), "a"} {
						state.currentCommand, state.index = command, ByteOffset(len(command))
						ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion)
						if !ready || err != nil { t.Fatalf("repaint: ready %v, error %v", ready, err) }
						view := commandViewportFor(state.displayLayout, state.commandRegion)
						if screen.col != int(view.CursorCol) || screen.pending { t.Fatal("repaint misplaced cursor") }
					}
				})
			}
		}
	}
}

func TestPromptMeasuresUnknownWidths(t *testing.T) {
	for _, startRow := range []int{0, 4} {
		screen := newCommandScreen(t, 5, 20)
		screen.row = startRow
		state := TermState{numRows: 5, numCols: 20}
		if err := state.paintPrompt(screen, screen.read, "~/世 (0)> \n:: "); err != nil { t.Fatal(err) }
		if state.widthCache.Entries["世"] != 2 || state.widthBatch.RepliesReceived != 1 {
			t.Fatal("prompt did not use the width measurement pipeline")
		}
		if state.commandRegion.RelativeOrigin || int(state.commandRegion.OriginRow) != screen.row+1 || state.commandRegion.OriginCol != 4 {
			t.Fatalf("measured prompt origin does not match screen: %+v", state.commandRegion)
		}
		// A later command can use the measured width without another query.
		state.currentCommand, state.index = "世", ByteOffset(len("世"))
		read := func() (TerminalToken, error) { t.Fatal("cached command requested input"); return nil, io.EOF }
		if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil { t.Fatalf("repaint: %v", err) }
		if screen.col != 5 { t.Fatalf("command cursor column %d, want 5", screen.col) }
	}
}

func TestPromptPreservesQueuedInput(t *testing.T) {
	state := TermState{numRows: 5, numCols: 20, queuedInput: []TerminalToken{AsciiToken{Char: 'a'}}}
	var output bytes.Buffer
	read := func() (TerminalToken, error) { t.Fatal("ASCII prompt read input"); return nil, io.EOF }
	if err := state.paintPrompt(&output, read, "~ (0)> \n:: "); err != nil { t.Fatal(err) }
	if strings.Contains(output.String(), "\033[6n") { t.Fatal("prompt requested cursor position") }
	if len(state.queuedInput) != 1 || state.queuedInputIndex != 0 { t.Fatal("prompt consumed queued input") }
}

func TestCommandMeasuresAfterRelativePrompt(t *testing.T) {
	for _, startRow := range []int{0, 4} {
		screen := newCommandScreen(t, 5, 20)
		screen.row = startRow
		state := TermState{numRows: 5, numCols: 20}
		if err := state.paintPrompt(screen, screen.read, "~ (0)> \n:: "); err != nil { t.Fatal(err) }
		// Grow the command before the first probe so scratch is relative to a
		// multi-row region, including when growth has already scrolled it.
		state.currentCommand = SourceText(strings.Repeat("a", 30))
		state.index = state.commandEnd()
		if ready, err := state.refreshCommandDisplay(screen, screen.read, &state.commandRegion); !ready || err != nil { t.Fatalf("ASCII repaint: %v", err) }
		state.currentCommand += "世é"
		state.index = state.commandEnd()
		keySent := false
		read := func() (TerminalToken, error) {
			if !keySent { keySent = true; return AsciiToken{Char: 'k'}, nil }
			return screen.read()
		}
		if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); ready || err != nil { t.Fatalf("queued frame: ready %v, error %v", ready, err) }
		if state.commandRegion.RelativeOrigin || int(state.commandRegion.OriginRow) != screen.row+1 || state.widthCache.Entries["世"] != 2 || state.widthCache.Entries["é"] != 1 {
			t.Fatalf("relative probe did not learn screen geometry and widths: %+v", state.commandRegion)
		}
		if token, err := state.readInputToken(); err != nil || token != (AsciiToken{Char: 'k'}) { t.Fatalf("queued key: %v, %v", token, err) }
		if ready, err := state.refreshCommandDisplay(screen, screen.read, &state.commandRegion); !ready || err != nil { t.Fatalf("measured repaint: %v", err) }
		if int(state.commandRegion.OriginRow)+int(state.commandRegion.CursorRow) != screen.row+1 { t.Fatal("cursor row lost after measuring") }
	}
}

func TestPromptUnknownWidthOnOneRow(t *testing.T) {
	screen := newCommandScreen(t, 1, 20)
	state := TermState{numRows: 1, numCols: 20}
	read := func() (TerminalToken, error) { t.Fatal("one-row prompt attempted a probe"); return nil, io.EOF }
	if err := state.paintPrompt(screen, read, "世\n:: "); err != nil { t.Fatal(err) }
	if !state.commandRegion.RelativeOrigin || screen.col != 3 || screen.line(0) != "::                  " { t.Fatal("one-row prompt misplaced") }
}

// A new pane may report its parent's size until the first key arrives. When
// nothing painted could have reflowed, the region adopts the new size and
// keeps painting in place; otherwise a fresh prompt is needed.
func TestResizeKeepsUnwrappedPromptInPlace(t *testing.T) {
	read := func() (TerminalToken, error) { t.Fatal("unexpected read"); return nil, io.EOF }
	setup := func(columns int, prompt SourceText, command SourceText, index int) (*TermState, *commandScreen) {
		screen := newCommandScreen(t, 5, columns)
		state := &TermState{numRows: 5, numCols: columns}
		if err := state.paintPrompt(screen, read, prompt); err != nil { t.Fatal(err) }
		state.currentCommand, state.index = command, ByteOffset(index)
		if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil { t.Fatalf("paint: %v", err) }
		return state, screen
	}

	// The reported case: a wide parent pane shrinks before the first key.
	state, _ := setup(312, "~ (0)> \n:: ", "", 0)
	state.displayLayout = LayoutResult{}
	if !state.adoptResizedGeometry(155, 5) { t.Fatal("empty command refused an in-place resize") }
	if state.commandRegion.Columns != 155 || !state.commandRegion.RelativeOrigin || state.commandRegion.OriginCol != 4 { t.Fatalf("region after resize: %+v", state.commandRegion) }
	// The kept region still paints correctly at the new width.
	screen := newCommandScreen(t, 5, 155)
	screen.row, screen.col = 1, 3
	state.currentCommand, state.index = "abc", 3
	if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil { t.Fatalf("repaint: %v", err) }
	if strings.TrimRight(screen.line(1), " ") != "   abc" || screen.col != 6 { t.Fatalf("repaint after resize: %q col %d", screen.line(1), screen.col) }

	state, _ = setup(40, "~ (0)> \n:: ", "abc", 3)
	if !state.adoptResizedGeometry(20, 5) { t.Fatal("short command refused a narrower width") }
	// The layout still describes the old width until the next paint, so a
	// second resize before painting is not trusted.
	if state.adoptResizedGeometry(60, 9) { t.Fatal("resize adopted against a stale layout") }
	state, _ = setup(40, "~ (0)> \n:: ", "abc", 3)
	if !state.adoptResizedGeometry(60, 9) { t.Fatal("short command refused a wider size") }
	if state.commandRegion.Columns != 60 || state.commandRegion.ScreenRows != 9 { t.Fatalf("region after resize: %+v", state.commandRegion) }

	// Rows that would not fit the new width, soft wraps, and wide prompts reflow.
	state, _ = setup(40, "~ (0)> \n:: ", SourceText(strings.Repeat("x", 30)), 30)
	if state.adoptResizedGeometry(20, 5) { t.Fatal("command wider than the new width kept in place") }
	state, _ = setup(40, "~ (0)> \n:: ", SourceText(strings.Repeat("x", 50)), 50)
	if state.adoptResizedGeometry(80, 5) { t.Fatal("soft-wrapped command kept in place") }
	state, _ = setup(40, "/a/very/long/path (3)> \n:: ", "abc", 3)
	if state.adoptResizedGeometry(20, 5) { t.Fatal("prompt wider than the new width kept in place") }
	state, _ = setup(40, SourceText(strings.Repeat("p", 40)), "abc", 3)
	if state.adoptResizedGeometry(80, 5) { t.Fatal("prompt filling the row kept in place") }

	// Shrinking the height may drop rows below the cursor.
	state, _ = setup(40, "~ (0)> \n:: ", "a\nb", 0)
	if len(state.displayLayout.Rows) != 2 { t.Fatalf("expected two rows: %+v", state.displayLayout) }
	if state.adoptResizedGeometry(40, 3) { t.Fatal("cursor above the last row kept in place while shrinking") }
	if !state.adoptResizedGeometry(40, 6) { t.Fatal("growing the height refused") }
	state, _ = setup(40, "~ (0)> \n:: ", "a\nb", 3)
	if !state.adoptResizedGeometry(40, 3) { t.Fatal("cursor on the last row refused while shrinking") }

	// An opaque prompt anchored by a cursor query has an unknown width.
	state = &TermState{numRows: 5, numCols: 40}
	state.anchorCommandRegion(2, 4)
	if state.adoptResizedGeometry(20, 5) { t.Fatal("opaque prompt kept in place") }
}
