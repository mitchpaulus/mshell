//go:build linux || darwin

package main

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// Exercise the real input lexer and PTY transport through measurement, queued
// editing and repaint. The terminal side checks every byte, including the
// forced break before a measured cluster and the final cell-based cursor.
func TestCommandRefreshPTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil { t.Fatal(err) }
	defer master.Close()
	defer slave.Close()
	oldMode, err := term.MakeRaw(int(slave.Fd()))
	if err != nil { t.Fatal(err) }
	defer term.Restore(int(slave.Fd()), oldMode)
	oldStdin := os.Stdin
	os.Stdin = slave
	defer func() { os.Stdin = oldStdin }()

	const burst = "\r\r\n\033[2K\r\033[2K世\033[6n\r\033[2K"
	const cleanup = "\r\033[2K\033[1A\033[3G"
	const paint = "\033[0m\r\033[3G\033[K\r\033[1B\033[2K\033[1A\033[3G\r\n世x\033[0m\r\033[4G"
	done := make(chan error, 1)
	go func() {
		defer master.Close()
		check := func(want string) error {
			buf := make([]byte, len(want))
			if _, err := io.ReadFull(master, buf); err != nil { return err }
			if string(buf) != want { return fmt.Errorf("PTY output %q, want %q", buf, want) }
			return nil
		}
		if err := check(burst); err != nil { done <- err; return }
		// A key arrives before a fragmented cursor report, making this frame
		// obsolete. No paint should appear before the queued edit is applied.
		for _, part := range []string{"x\033[", "5;", "3R"} {
			if _, err := io.WriteString(master, part); err != nil { done <- err; return }
		}
		if err := check(cleanup); err != nil { done <- err; return }
		done <- check(paint)
	}()

	state := TermState{currentCommand: "世", index: 3, stdInState: &StdinReaderState{array: make([]byte, 1)}}
	region := ProbeRegion{OriginRow: 5, OriginCol: 3, PaintedRows: 1, ScreenRows: 5, Columns: 4}
	read := func() (TerminalToken, error) { return state.InteractiveLexer(state.stdInState) }
	ready, err := state.refreshCommandDisplay(slave, read, &region)
	if ready || err != nil || state.widthCache.Entries["世"] != 2 { t.Fatalf("queued frame: ready %v, error %v", ready, err) }
	token, err := state.readInputToken()
	if err != nil || token != (AsciiToken{Char: 'x'}) { t.Fatalf("queued key: %v, %v", token, err) }
	state.PushChars([]rune{'x'})
	ready, err = state.refreshCommandDisplay(slave, read, &region)
	if !ready || err != nil { t.Fatalf("repaint: %v", err) }
	if err := <-done; err != nil { t.Fatal(err) }
	if region.OriginRow != 4 || region.CursorRow != 1 || region.PaintedRows != 2 || region.ScratchOwned { t.Fatalf("PTY region bookkeeping: %+v", region) }
}

// A garbled cursor report is consumed, the prompt moves to a fresh line, and
// one further request anchors there. Keys typed meanwhile stay queued in order.
func TestAnchorPromptRetriesAfterMalformedReportPTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil { t.Fatal(err) }
	defer master.Close()
	defer slave.Close()
	oldMode, err := term.MakeRaw(int(slave.Fd()))
	if err != nil { t.Fatal(err) }
	defer term.Restore(int(slave.Fd()), oldMode)
	oldStdin := os.Stdin
	os.Stdin = slave
	defer func() { os.Stdin = oldStdin }()

	done := make(chan error, 1)
	go func() {
		check := func(want string) error {
			buf := make([]byte, len(want))
			if _, err := io.ReadFull(master, buf); err != nil { return err }
			if string(buf) != want { return fmt.Errorf("PTY output %q, want %q", buf, want) }
			return nil
		}
		if err := check("\x1b[6n"); err != nil { done <- err; return }
		if _, err := io.WriteString(master, "k\x1b[7;R"); err != nil { done <- err; return }
		if err := check("\r\n\x1b[6n"); err != nil { done <- err; return }
		if _, err := io.WriteString(master, "\x1b[8;1R"); err != nil { done <- err; return }
		done <- nil
	}()

	state := TermState{stdInState: &StdinReaderState{array: make([]byte, 1)}, numRows: 24, numCols: 80}
	if err := state.anchorPrompt(slave); err != nil { t.Fatalf("anchor: %v", err) }
	if err := <-done; err != nil { t.Fatal(err) }
	if state.promptRow != 8 || state.promptLength != 0 || state.commandRegion.OriginRow != 8 || state.commandRegion.OriginCol != 1 { t.Fatalf("anchor state: row %d length %d region %+v", state.promptRow, state.promptLength, state.commandRegion) }
	token, err := state.readInputToken()
	if err != nil || token != (AsciiToken{Char: 'k'}) { t.Fatalf("queued key: %v, %v", token, err) }
}
