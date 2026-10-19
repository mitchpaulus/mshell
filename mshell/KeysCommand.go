package main

// msh keys (ai/cli_bindings.typ, section 10.1): reads keys with the
// editor's decoder and protocol, and prints each as a binding writes it,
// with the bytes that came and the time since the key before.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

func runKeysCommand(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "Usage: msh keys")
		fmt.Fprintln(os.Stderr, "Shows each key pressed as a key binding writes it. Ctrl-C twice, or Ctrl-D, ends.")
		return 2
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "msh keys reads keys from a terminal.")
		return 1
	}
	state := &TermState{stdInFd: fd, stdInState: &StdinReaderState{array: make([]byte, 1024)}}
	if err := state.saveTerminalState(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading the terminal state: %s\n", err)
		return 1
	}
	if err := state.enterRawMode(); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting the terminal to raw mode: %s\n", err)
		return 1
	}
	defer state.leaveRawMode()
	state.detectKittyKeys()
	if err := state.setKittyKeys(true); err != nil {
		fmt.Fprintf(os.Stderr, "Error enabling the kitty keyboard protocol: %s\r\n", err)
		return 1
	}
	protocol := "legacy encoding"
	if state.kittyKeys {
		protocol = "kitty keyboard protocol"
	}
	fmt.Printf("Reading keys (%s). Ctrl-C twice, or Ctrl-D, ends.\r\n", protocol)

	var record []byte
	state.stdInState.record = &record
	var last time.Time
	ctrlC := false
	for {
		record = record[:0]
		token, err := state.readInputToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading the terminal: %s\r\n", err)
			return 1
		}
		now := time.Now()
		gap := ""
		if !last.IsZero() {
			gap = fmt.Sprintf("+%d ms", now.Sub(last).Milliseconds())
		}
		last = now

		var what string
		code, isKey := keyOfToken(token)
		switch t := token.(type) {
		case EofTerminalToken:
			return 0
		case PasteToken:
			what = fmt.Sprintf("a paste of %d bytes", len(t.Text))
		case KeyToken:
			what = keySource(code)
			if t.EscOf != 0 {
				what += "  (or " + keySource(t.EscOf) + " alone, when that is not bound)"
			}
		default:
			if isKey {
				what = keySource(code)
			} else {
				what = "not a key"
			}
		}
		fmt.Printf("%-40s %-24s %s\r\n", what, hexBytes(record), gap)

		if isKey && code == 'd'|KeyModCtrl {
			return 0
		}
		if isKey && code == 'c'|KeyModCtrl {
			if ctrlC {
				return 0
			}
			ctrlC = true
		} else {
			ctrlC = false
		}
	}
}

// hexBytes writes bytes as hex pairs: 1b 5b 41.
func hexBytes(b []byte) string {
	if len(b) > 12 {
		return fmt.Sprintf("% x ...", b[:12])
	}
	return strings.TrimSpace(fmt.Sprintf("% x", b))
}
