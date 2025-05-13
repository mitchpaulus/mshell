package main

import (
	"syscall"
	"os"
)

var cmdSysProcAttr = &syscall.SysProcAttr{
	Setpgid: true,
	// See https://github.com/junegunn/fzf/issues/3646
	// Two items below recommended by LLM
	Ctty: int(os.Stdin.Fd()),
	Foreground: true,
}

func SigIntHandler(processId int) error {
	return syscall.Kill(-processId, syscall.SIGINT)
}
