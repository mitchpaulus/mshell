//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const fileLockRetryInterval = 50 * time.Millisecond

// openNonBlock is ORed into opens of the shared clipboard file so that a FIFO
// planted at that path cannot block open(2) forever waiting for a writer. It
// has no effect on regular files.
const openNonBlock = unix.O_NONBLOCK

// fileLock is an exclusive advisory lock backed by flock(2). The lock is tied
// to the open file description, so the OS releases it automatically if the
// process dies while holding it; no stale-lock cleanup is ever needed.
type fileLock struct {
	f *os.File
}

// acquireFileLock acquires an exclusive lock on path, creating the lock file
// if needed. The wait is bounded: it retries a non-blocking lock until
// timeout, then fails, so a stuck or hostile lock holder can never hang the
// caller indefinitely.
func acquireFileLock(path string, timeout time.Duration) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return &fileLock{f: f}, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN && err != unix.EINTR {
			f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("timed out waiting for lock on %s", filepath.Base(path))
		}
		time.Sleep(fileLockRetryInterval)
	}
}

func (l *fileLock) release() {
	// Closing the descriptor releases the flock.
	l.f.Close()
}
