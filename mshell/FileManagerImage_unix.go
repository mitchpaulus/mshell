//go:build linux || darwin

package main

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitForInput reports whether fd has input to read within timeout.
func waitForInput(fd int, timeout time.Duration) bool {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, int(timeout.Milliseconds()))
		if err == unix.EINTR {
			continue
		}
		return err == nil && n > 0
	}
}

// windowPixelCellSize returns the cell size from the pixel size the terminal
// gives the kernel, or zeros if it gives none.
func windowPixelCellSize(fd int, cols int, rows int) (int, int) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 || cols <= 0 || rows <= 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / cols, int(ws.Ypixel) / rows
}
