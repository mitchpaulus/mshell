package main

import (
	"errors"
	"path/filepath"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modKernel32Links       = windows.NewLazySystemDLL("kernel32.dll")
	procFindFirstFileNameW = modKernel32Links.NewProc("FindFirstFileNameW")
	procFindNextFileNameW  = modKernel32Links.NewProc("FindNextFileNameW")
)

// fileLinkCount returns the number of hard links to the file at path,
// following symlinks.
func fileLinkCount(path string) (int, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	// FILE_FLAG_BACKUP_SEMANTICS is required to open a directory.
	handle, err := windows.CreateFile(pathPtr, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return 0, err
	}
	return int(info.NumberOfLinks), nil
}

// fileHardLinks lists every name of the file at path, following symlinks.
// The names are absolute and sorted.
func fileHardLinks(path string) ([]string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	pathPtr, err := windows.UTF16PtrFromString(absPath)
	if err != nil {
		return nil, err
	}

	// The names come back without the volume, like \Users\me\file.txt.
	volume := filepath.VolumeName(absPath)
	buf := make([]uint16, windows.MAX_LONG_PATH)

	length := uint32(len(buf))
	r, _, callErr := procFindFirstFileNameW.Call(uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buf[0])))
	handle := windows.Handle(r)
	if handle == windows.InvalidHandle {
		return nil, callErr
	}
	defer windows.FindClose(handle)

	names := []string{volume + windows.UTF16ToString(buf)}
	for {
		length = uint32(len(buf))
		r, _, callErr = procFindNextFileNameW.Call(uintptr(handle), uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buf[0])))
		if r == 0 {
			if errors.Is(callErr, windows.ERROR_HANDLE_EOF) {
				break
			}
			return nil, callErr
		}
		names = append(names, volume+windows.UTF16ToString(buf))
	}

	sort.Strings(names)
	return names, nil
}
