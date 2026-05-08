package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func mountedWindowsVolumes() []os.DirEntry {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}

	entries := make([]os.DirEntry, 0, 26)
	for drive := 0; drive < 26; drive++ {
		if mask&(1<<uint(drive)) == 0 {
			continue
		}
		name := string(rune('A'+drive)) + ":"
		entries = append(entries, fileManagerVolumeEntry{name: name})
	}
	return entries
}
