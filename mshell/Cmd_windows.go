package main

import (
	"syscall"
)

const CREATE_NEW_PROCESS_GROUP = 0x00000200

var cmdSysProcAttr = &syscall.SysProcAttr{
	CreationFlags: CREATE_NEW_PROCESS_GROUP,
}

func SigIntHandler(processId int) error {
	return generateCtrlBreak(processId)
}

func generateCtrlBreak(pid int) error {
    // Windows-only: needs attached console
    kernel32 := syscall.MustLoadDLL("kernel32.dll")
    generateCtrlEvent := kernel32.MustFindProc("GenerateConsoleCtrlEvent")
    _, _, err := generateCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(pid))
    return err
}
