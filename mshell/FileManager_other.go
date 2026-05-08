//go:build !windows

package main

import "os"

func mountedWindowsVolumes() []os.DirEntry {
	return nil
}
