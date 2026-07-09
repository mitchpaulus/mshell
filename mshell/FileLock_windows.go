//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileLock is an exclusive lock backed by LockFileEx. The lock is tied to the
// open handle, so the OS releases it automatically if the process dies while
// holding it; no stale-lock cleanup is ever needed.
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
	// Lock one byte at offset 0 (the offset lives in the Overlapped struct).
	// Without LOCKFILE_FAIL_IMMEDIATELY this blocks until the lock is free.
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &fileLock{f: f}, nil
}

func (l *fileLock) release() {
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, ol)
	l.f.Close()
}
