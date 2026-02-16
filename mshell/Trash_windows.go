package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

const (
	foDelete          = 0x3
	fofAllowUndo      = 0x40
	fofNoConfirmation  = 0x10
	fofSilent          = 0x4
	fofNoErrorUI       = 0x400
)

// SHFILEOPSTRUCTW for 64-bit Windows
type shFileOpStructW struct {
	Hwnd                  uintptr
	WFunc                 uint32
	_                     [4]byte // padding for 64-bit alignment
	PFrom                 *uint16
	PTo                   *uint16
	FFlags                uint16
	FAnyOperationsAborted int32
	_                     [2]byte // padding
	HNameMappings         uintptr
	LpszProgressTitle     *uint16
}

func TrashFile(absPath string) error {
	// Convert path to UTF-16 with double-null termination
	pathUTF16, err := syscall.UTF16FromString(absPath)
	if err != nil {
		return err
	}
	// UTF16FromString already adds one null; add another for double-null
	pathUTF16 = append(pathUTF16, 0)

	op := shFileOpStructW{
		WFunc:  foDelete,
		PFrom:  &pathUTF16[0],
		FFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}

	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if ret != 0 {
		return fmt.Errorf("SHFileOperationW failed with code %d", ret)
	}
	return nil
}
