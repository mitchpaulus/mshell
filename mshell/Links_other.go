//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// fileLinkCount returns the number of hard links to the file at path,
// following symlinks.
func fileLinkCount(path string) (int, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("link count is not available on this system")
	}
	return int(stat.Nlink), nil
}

// fileHardLinks lists every name of the file at path. Unix has no way to look
// up the names of a file from its inode, so this is only available on Windows.
func fileHardLinks(path string) ([]string, error) {
	return nil, errors.New("hardLinks is only supported on Windows. On other systems, compare linkCount with a directory search using sameFile")
}
