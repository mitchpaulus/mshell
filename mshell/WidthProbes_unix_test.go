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

// The PTY is the transport, not a width oracle. Its master emulates the terminal
// and withholds every report until it has received the entire probe burst.
func TestWidthProbesPTY(t *testing.T) {
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

	const burst = "\r\r\n\033[2K\r\033[2K👨‍👩‍👧‍👦\033[6n\r\033[2K\r\033[2Ké\033[6n\r\033[2K"
	const cleanup = "\r\033[2K\033[1A\033[3G"
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
		// Split CPR sequences and interleave keyboard tokens in their byte stream.
		for _, piece := range []string{"a\033[", "5;9", "Rb", "\033[5;", "2R"} {
			if _, err := io.WriteString(master, piece); err != nil { done <- err; return }
		}
		done <- check(cleanup)
	}()

	state := TermState{stdInState: &StdinReaderState{array: make([]byte, 1)}}
	region := ProbeRegion{OriginRow: 4, OriginCol: 3, PaintedRows: 1, ScreenRows: 5, Columns: 15}
	batch := WidthProbeBatch{}
	read := func() (TerminalToken, error) { return state.InteractiveLexer(state.stdInState) }
	if err := state.measureWidths(slave, read, &region, &batch, []string{"👨‍👩‍👧‍👦", "é"}); err != nil { t.Fatal(err) }
	if err := <-done; err != nil { t.Fatal(err) }
	if batch.Failure != nil || state.widthCache.Entries["👨‍👩‍👧‍👦"] != 8 || state.widthCache.Entries["é"] != 1 { t.Fatalf("PTY measurements: %+v", batch) }
	for _, char := range []byte{'a', 'b'} {
		token, err := state.readInputToken()
		if err != nil || token != (AsciiToken{Char: char}) { t.Fatalf("queued PTY input: %v, %v", token, err) }
	}
}
