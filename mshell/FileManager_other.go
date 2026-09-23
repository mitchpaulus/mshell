//go:build !windows

package main

import "os"

func mountedWindowsVolumes() []os.DirEntry {
	return nil
}

func isCloudSyncRoot(dir string) bool {
	return false
}

func fileAttributes(entry os.DirEntry) uint32 {
	return 0
}

func newStorageProviderStateLookup() (func(dir string, names []string, stop func() bool, found func(name string, value uint32)), func()) {
	return nil, nil
}
