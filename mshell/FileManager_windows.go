package main

import (
	"os"
	"syscall"
	"unsafe"

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

var procCfGetSyncRootInfoByPath = windows.NewLazySystemDLL("cldapi.dll").NewProc("CfGetSyncRootInfoByPath")

// isCloudSyncRoot reports whether dir is inside a folder managed by a cloud
// sync provider such as OneDrive. Windows hides the placeholder reparse point
// from ordinary programs, so a downloaded OneDrive file has the same attributes
// as a plain local file; this check is how the two are told apart.
func isCloudSyncRoot(dir string) bool {
	if procCfGetSyncRootInfoByPath.Find() != nil {
		return false
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return false
	}
	// CF_SYNC_ROOT_INFO_BASIC (0) fills a CF_SYNC_ROOT_BASIC_INFO, which is a
	// single 8 byte file id.
	var info [8]byte
	var returned uint32
	hr, _, _ := procCfGetSyncRootInfoByPath.Call(
		uintptr(unsafe.Pointer(path)),
		0,
		uintptr(unsafe.Pointer(&info[0])),
		uintptr(len(info)),
		uintptr(unsafe.Pointer(&returned)),
	)
	return hr == 0
}

// fileAttributes returns the Windows file attributes captured when the
// directory was listed. It does not open the file, so it never triggers a
// cloud download.
func fileAttributes(entry os.DirEntry) uint32 {
	info, err := entry.Info()
	if err != nil || info == nil {
		return 0
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0
	}
	return data.FileAttributes
}
