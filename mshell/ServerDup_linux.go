package main

import "golang.org/x/sys/unix"

// dupTo makes newFd refer to what oldFd does (dup2; Linux has dup3 on every
// architecture, dup2 not on all).
func dupTo(oldFd int, newFd int) error {
	return unix.Dup3(oldFd, newFd, 0)
}
