package main

import "golang.org/x/sys/unix"

// dupTo makes newFd refer to what oldFd does.
func dupTo(oldFd int, newFd int) error {
	return unix.Dup2(oldFd, newFd)
}

// flushInput discards the input a terminal has received and not yet been
// read.
func flushInput(fd int) error {
	// FREAD (sys/fcntl.h): the input queue.
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, 1)
}
