package main

import (
	"syscall"
)

var cmdSysProcAttr = &syscall.SysProcAttr{
	Setpgid: true,
}

func SigIntHandler(processId int) error {
	return syscall.Kill(-processId, syscall.SIGINT)
}
