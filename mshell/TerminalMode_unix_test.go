//go:build linux || darwin

package main

import (
	"os"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// The explicit raw definition is the editor's contract with the kernel.
// Restoring afterwards yields the exact saved cooked state.
func TestRawTerminalModeFlagsPTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil { t.Fatal(err) }
	defer master.Close()
	defer slave.Close()
	fd := int(slave.Fd())
	before, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil { t.Fatal(err) }

	state := TermState{stdInFd: fd}
	output, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil { t.Fatal(err) }
	defer output.Close()
	oldStdout := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = oldStdout }()
	if err := state.saveTerminalState(); err != nil { t.Fatal(err) }
	if err := state.enterRawMode(); err != nil { t.Fatal(err) }
	defer state.leaveRawMode()
	raw, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil { t.Fatal(err) }
	if raw.Lflag&(unix.ICANON|unix.ECHO|unix.ECHONL|unix.ISIG|unix.IEXTEN) != 0 { t.Fatalf("local flags still set: %#x", raw.Lflag) }
	if raw.Iflag&(unix.ICRNL|unix.IXON|unix.INLCR|unix.IGNCR|unix.ISTRIP|unix.BRKINT|unix.IGNBRK|unix.PARMRK) != 0 { t.Fatalf("input flags still set: %#x", raw.Iflag) }
	if raw.Oflag&unix.OPOST != 0 { t.Fatal("OPOST still set") }
	if raw.Cflag&unix.CSIZE != unix.CS8 || raw.Cflag&unix.PARENB != 0 { t.Fatalf("control flags: %#x", raw.Cflag) }
	if raw.Cc[unix.VMIN] != 1 || raw.Cc[unix.VTIME] != 0 { t.Fatalf("VMIN %d VTIME %d", raw.Cc[unix.VMIN], raw.Cc[unix.VTIME]) }

	state.leaveRawMode()
	after, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil { t.Fatal(err) }
	if *after != *before { t.Fatalf("cooked state not restored: %+v vs %+v", after, before) }
	if term.IsTerminal(fd) != true { t.Fatal("slave is not a terminal") }
	// Returning from a child enables paste again; a repeated raw-mode call
	// does not change ownership or send a duplicate enable.
	if err := state.enterRawMode(); err != nil { t.Fatal(err) }
	if err := state.enterRawMode(); err != nil { t.Fatal(err) }
	state.leaveRawMode()
	got, err := os.ReadFile(output.Name())
	if err != nil { t.Fatal(err) }
	const want = "\x1b[?2004h\x1b[?2004l\x1b[?2004h\x1b[?2004l"
	if string(got) != want { t.Fatalf("paste mode lifecycle = %q, want %q", got, want) }
}
