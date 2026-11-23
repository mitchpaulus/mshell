//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(f *os.File, exclusive bool) error {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}
	for {
		err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB)
		switch err {
		case nil:
			return nil
		case unix.EINTR:
			continue
		case unix.EWOULDBLOCK:
			return errBusy
		case unix.ENOLCK, unix.ENOTSUP, unix.ENOSYS:
			return errNoLocks
		default:
			return err
		}
	}
}

func unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }

// sameFile reports whether the locked descriptor is still the file at path.
func sameFile(f *os.File, path string) bool {
	var a, b unix.Stat_t
	if unix.Fstat(int(f.Fd()), &a) != nil || unix.Stat(path, &b) != nil {
		return false
	}
	return a.Dev == b.Dev && a.Ino == b.Ino
}

func isEphemeral(err error) bool { return false }
