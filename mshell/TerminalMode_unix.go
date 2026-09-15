//go:build linux || darwin

package main

import (
	"golang.org/x/sys/unix"
)

// setRawTerminalMode mirrors cfmakeraw, spelled out because each flag carries
// an editor contract:
//
//	ICANON, ECHO off   keys arrive one byte at a time and only the painter echoes
//	ISIG off           Ctrl-C, Ctrl-Z, Ctrl-\ are keys, so there is no suspend/resume state
//	ICRNL off          Enter arrives as carriage return (13), which the handler checks
//	IXON off           Ctrl-S and Ctrl-Q are keys rather than flow control
//	IEXTEN off         Ctrl-V is a key
//	OPOST off          newline moves down one row only; the painter writes CRLF itself
//	CS8, no parity     UTF-8 bytes pass through untouched
//	VMIN 1, VTIME 0    reads block for at least one byte and never time out
func setRawTerminalMode(fd int) error {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return err
	}
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	termios.Oflag &^= unix.OPOST
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(fd, ioctlWriteTermios, termios)
}
