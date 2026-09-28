//go:build windows

package main

import (
	"time"

	"golang.org/x/sys/windows"
)

// waitForInput reports whether the console has input to read within timeout.
func waitForInput(fd int, timeout time.Duration) bool {
	event, err := windows.WaitForSingleObject(windows.Handle(fd), uint32(timeout.Milliseconds()))
	return err == nil && event == windows.WAIT_OBJECT_0
}

// windowPixelCellSize returns zeros: the Windows console does not report a
// pixel size outside of the terminal's own replies.
func windowPixelCellSize(fd int, cols int, rows int) (int, int) {
	return 0, 0
}
