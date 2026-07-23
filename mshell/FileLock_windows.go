//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

const fileLockRetryInterval = 50 * time.Millisecond

// openNonBlock mirrors the unix constant ORed into clipboard opens. Windows
// has no FIFO-in-the-filesystem equivalent whose open blocks, so it is a
// no-op here.
const openNonBlock = 0

// fileLock is an exclusive lock backed by LockFileEx. The lock is tied to the
// open handle, so the OS releases it automatically if the process dies while
// holding it; no stale-lock cleanup is ever needed.
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
		// Lock one byte at offset 0 (the offset lives in the Overlapped
		// struct). LOCKFILE_FAIL_IMMEDIATELY keeps each attempt non-blocking.
		ol := new(windows.Overlapped)
		err = windows.LockFileEx(windows.Handle(f.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
		if err == nil {
			return &fileLock{f: f}, nil
		}
		if err != windows.ERROR_LOCK_VIOLATION {
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
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, ol)
	l.f.Close()
}
