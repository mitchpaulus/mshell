//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// fileLock is an exclusive advisory lock backed by flock(2). The lock is tied
// to the open file description, so the OS releases it automatically if the
// process dies while holding it; no stale-lock cleanup is ever needed.
type fileLock struct {
	f *os.File
}

// acquireFileLock blocks until an exclusive lock on path is acquired,
// creating the lock file if needed.
func acquireFileLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return &fileLock{f: f}, nil
}

func (l *fileLock) release() {
	// Closing the descriptor releases the flock.
	l.f.Close()
}
