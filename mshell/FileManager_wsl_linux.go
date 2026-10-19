//go:build linux

package main

import (
	"os"
	"strings"
	"sync"
	"syscall"
)

// v9fsMagic is the file system type WSL 2 reports for Windows drives (/mnt/c).
const v9fsMagic = 0x01021997

// maxNtfsResidentSize is the largest file NTFS may store inside its file table
// instead of in its own disk blocks. Such files report no blocks even though
// they are on this device, so they are never taken for cloud only.
const maxNtfsResidentSize = 4096

var isWSL = sync.OnceValue(func() bool {
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(release)), "microsoft")
})

// looksCloudOnly reports whether path is a file on a Windows drive under WSL
// whose data is only in the cloud, such as a OneDrive file that has not been
// downloaded. Such a file has its full size but takes no disk blocks. Reading
// it would download it, but a stat does not.
func looksCloudOnly(path string) bool {
	if !isWSL() {
		return false
	}
	var fsStat syscall.Statfs_t
	if err := syscall.Statfs(path, &fsStat); err != nil || fsStat.Type != v9fsMagic {
		return false
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return false
	}
	return stat.Mode&syscall.S_IFMT == syscall.S_IFREG && stat.Blocks == 0 && stat.Size > maxNtfsResidentSize
}
