//go:build !linux

package main

import (
	"io/fs"
	"os"
)

// readDirListing falls back to os.ReadDir where there is no raw reader yet.
func readDirListing(dir string, l *DirListing) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	l.reset()
	for _, entry := range entries {
		kind := dirKindFile
		switch t := entry.Type(); {
		case t.IsDir():
			kind = dirKindDir
		case t&fs.ModeSymlink != 0:
			kind = dirKindLink
		}
		l.add(entry.Name(), kind)
	}
	return nil
}
