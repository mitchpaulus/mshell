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

var (
	procSHCreateItemFromParsingName  = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")
	procSHCreateItemFromRelativeName = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromRelativeName")
)

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

func shellItemMethod(item unsafe.Pointer, index int) uintptr {
	return (*[shellItemGetUInt32 + 1]uintptr)(*(*unsafe.Pointer)(item))[index]
}

func releaseShellItem(item unsafe.Pointer) {
	syscall.SyscallN(shellItemMethod(item, shellItemRelease), uintptr(item))
}

// newStorageProviderStateLookup returns a function that reads the shell's
// System.StorageProviderState for the named entries of dir, calling found for
// each one that has a value and returning early when stop reports true. The
// second function releases COM. Both must be called on the OS thread that
// called this function. It returns nil if COM cannot be initialized.
//
// Every failure (a missing DLL, COM already set up differently on the thread,
// a path the shell cannot parse, an entry the provider has no status for)
// just leaves that entry without a status.
func newStorageProviderStateLookup() (func(dir string, names []string, stop func() bool, found func(name string, value uint32)), func()) {
	if procSHCreateItemFromParsingName.Find() != nil || procSHCreateItemFromRelativeName.Find() != nil {
		return nil, nil
	}
	// Multithreaded mode, because this thread never processes window messages,
	// which single threaded mode expects. S_FALSE (1) means COM was already set
	// up in this mode on the thread and still needs a matching CoUninitialize.
	// Any other error, such as RPC_E_CHANGED_MODE, must not be paired with
	// CoUninitialize.
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED|windows.COINIT_DISABLE_OLE1DDE)
	if err != nil && err != syscall.Errno(1) {
		return nil, nil
	}

	lookup := func(dir string, names []string, stop func() bool, found func(name string, value uint32)) {
		// Parsing a full path costs several milliseconds per item, most of the
		// lookup time. Parsing the directory once and creating each entry
		// relative to it takes a fraction of a millisecond per entry.
		parent := shellItemFromPath(dir)
		if parent == nil {
			return
		}
		defer releaseShellItem(parent)

		for _, name := range names {
			if stop() {
				return
			}
			if value, ok := storageProviderState(parent, name); ok {
				found(name, value)
			}
		}
	}
	return lookup, windows.CoUninitialize
}

// shellItemFromPath returns an IShellItem2 for path, or nil. The caller must
// release it.
func shellItemFromPath(path string) unsafe.Pointer {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var item unsafe.Pointer
	hr, _, _ := procSHCreateItemFromParsingName.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&iidIShellItem2)),
		uintptr(unsafe.Pointer(&item)),
	)
	if hr != 0 && item != nil {
		// Not expected, but do not leak an object returned with an error.
		releaseShellItem(item)
		return nil
	}
	return item
}

// storageProviderState reads System.StorageProviderState for the entry name
// inside parent.
func storageProviderState(parent unsafe.Pointer, name string) (uint32, bool) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var item unsafe.Pointer
	hr, _, _ := procSHCreateItemFromRelativeName.Call(
		uintptr(parent),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(unsafe.Pointer(&iidIShellItem2)),
		uintptr(unsafe.Pointer(&item)),
	)
	if item == nil {
		return 0, false
	}
	defer releaseShellItem(item)
	if hr != 0 {
		return 0, false
	}

	var value uint32
	hr, _, _ = syscall.SyscallN(shellItemMethod(item, shellItemGetUInt32), uintptr(item),
		uintptr(unsafe.Pointer(&pkeyStorageProviderState)),
		uintptr(unsafe.Pointer(&value)))
	if hr != 0 {
		return 0, false
	}
	return value, true
}
