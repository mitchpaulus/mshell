package main

import (
	"net/url"
	"unicode/utf8"
)

// terminalSafeText returns text with every byte a terminal could act on
// replaced by a visible stand-in, so data from the filesystem or from user
// input can be written to the terminal without becoming a control sequence.
//
//	C0 controls and DEL   caret notation, ^[ for ESC
//	C1 controls           '?'; UTF-8 terminals honour U+009B as CSI and U+009D as OSC
//	invalid UTF-8 bytes   '?'
//
// Newline and tab pass through when keepLayout is true, for multi-line error
// messages. The prompt passes false: a newline inside a directory name would
// otherwise change the prompt's row count.
func terminalSafeText(text string, keepLayout bool) string {
	var array [256]byte
	return string(appendTerminalSafeText(array[:0], text, keepLayout))
}

// appendTerminalSafeText appends what terminalSafeText returns for text.
func appendTerminalSafeText(dst []byte, text string, keepLayout bool) []byte {
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			dst = append(dst, '?')
		case keepLayout && (r == '\n' || r == '\t'):
			dst = append(dst, byte(r))
		case r < 0x20 || r == 0x7f:
			dst = append(dst, controlCaretText(byte(r))...)
		case r >= 0x80 && r <= 0x9f:
			dst = append(dst, '?')
		default:
			dst = append(dst, text[i:i+size]...)
		}
		i += size
	}
	return dst
}

// containsTerminalControl reports whether terminalSafeText would change text.
func containsTerminalControl(text string) bool {
	return terminalSafeText(text, false) != text
}

// directoryFileURL builds the file:// URL for an OSC 7 working-directory
// report. Percent-encoding keeps control bytes, spaces, and the ESC \ string
// terminator out of the escape sequence.
func directoryFileURL(hostname, path string) string {
	u := url.URL{Scheme: "file", Host: hostname, Path: path}
	return u.String()
}
