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

var procSHCreateItemFromParsingName = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")

// IID_IShellItem2
var iidIShellItem2 = windows.GUID{Data1: 0x7e9fb0d3, Data2: 0x919f, Data3: 0x4307, Data4: [8]byte{0xab, 0x2e, 0x9b, 0x18, 0x60, 0x31, 0x0c, 0x93}}

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// PKEY_StorageProviderState, the property behind Explorer's sync status icon.
var pkeyStorageProviderState = propertyKey{
	fmtid: windows.GUID{Data1: 0xe77e90df, Data2: 0x6271, Data3: 0x4f5b, Data4: [8]byte{0x83, 0x4f, 0x2d, 0xd1, 0xf2, 0x45, 0xdd, 0xa4}},
	pid:   3,
}

// Method positions in the IShellItem2 vtable.
const (
	shellItemRelease   = 2
	shellItemGetUInt32 = 18
)

// newStorageProviderStateLookup returns a function that reads the shell's
// System.StorageProviderState for a path, and a function that releases COM.
// Both must be called on the OS thread that called this function. It returns
// nil if COM cannot be initialized.
func newStorageProviderStateLookup() (func(path string) (uint32, bool), func()) {
	if err := procSHCreateItemFromParsingName.Find(); err != nil {
		return nil, nil
	}
	// S_FALSE (1) means COM was already initialized on this thread; it still
	// needs a matching CoUninitialize.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && err != syscall.Errno(1) {
		return nil, nil
	}

	lookup := func(path string) (uint32, bool) {
		pathPtr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return 0, false
		}
		var item unsafe.Pointer
		hr, _, _ := procSHCreateItemFromParsingName.Call(
			uintptr(unsafe.Pointer(pathPtr)),
			0,
			uintptr(unsafe.Pointer(&iidIShellItem2)),
			uintptr(unsafe.Pointer(&item)),
		)
		if hr != 0 || item == nil {
			return 0, false
		}
		vtable := (*[shellItemGetUInt32 + 1]uintptr)(*(*unsafe.Pointer)(item))
		defer syscall.SyscallN(vtable[shellItemRelease], uintptr(item))

		var value uint32
		hr, _, _ = syscall.SyscallN(vtable[shellItemGetUInt32], uintptr(item),
			uintptr(unsafe.Pointer(&pkeyStorageProviderState)),
			uintptr(unsafe.Pointer(&value)))
		if hr != 0 {
			return 0, false
		}
		return value, true
	}
	return lookup, windows.CoUninitialize
}
