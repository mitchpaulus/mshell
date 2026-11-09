package main

import "golang.org/x/sys/unix"

// dupTo makes newFd refer to what oldFd does (dup2; Linux has dup3 on every
// architecture, dup2 not on all).
func dupTo(oldFd int, newFd int) error {
	return unix.Dup3(oldFd, newFd, 0)
}

// flushInput discards the input a terminal has received and not yet been
// read.
func flushInput(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
