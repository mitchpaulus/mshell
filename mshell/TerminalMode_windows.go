//go:build windows

package main

import (
	"golang.org/x/term"
)

// Windows console modes have no termios equivalent; the library's raw mode
// disables line input, echo, and processed input, which is what the editor
// needs. The saved cooked state is still restored by leaveRawMode.
func setRawTerminalMode(fd int) error {
	_, err := term.MakeRaw(fd)
	return err
}
