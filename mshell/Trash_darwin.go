package main

import (
	"os/exec"
)

func TrashFile(absPath string) error {
	return exec.Command("trash", absPath).Run()
}
