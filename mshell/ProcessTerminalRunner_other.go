//go:build !windows

package main

import "os/exec"

// runIsolatedTerminalCommand is implemented by the ConPTY launcher on Windows.
// POSIX systems isolate foreground access with process groups and tcsetpgrp.
func runIsolatedTerminalCommand(cmd *exec.Cmd, stdio ResolvedProcessStdio) (bool, int, error) {
	return false, 0, nil
}
