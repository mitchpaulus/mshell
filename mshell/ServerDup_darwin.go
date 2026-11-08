package main

import "golang.org/x/sys/unix"

// dupTo makes newFd refer to what oldFd does.
func dupTo(oldFd int, newFd int) error {
	return unix.Dup2(oldFd, newFd)
}
