//go:build !linux

package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

// openDir is empty: reads here do not keep the directory open.
type openDir struct{}

// Close releases the directory the last read left open, which here is none.
func (l *DirListing) Close() {}

// entryIsDir reports whether name in dir is a directory, following symbolic
// links.
func (l *DirListing) entryIsDir(dir string, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}

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
