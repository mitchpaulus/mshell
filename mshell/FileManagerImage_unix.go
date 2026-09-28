//go:build linux || darwin

package main

import (
	"time"

	"golang.org/x/sys/unix"
)

// readTerminalInput waits at most wait for input on fd and returns what can
// be read without blocking. It returns nothing, and true, if the wait timed
// out or a signal interrupted it; the caller works out the time left and
// waits again. It returns false if the terminal hung up or failed.
//
// It reads fd directly, the same descriptor poll watched, so input poll
// reports is there to read and the read returns at once. That holds while
// nothing else in the process reads the terminal at the same time, which is
// the case while the file manager starts.
func readTerminalInput(fd int, wait time.Duration) ([]byte, bool) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, waitMilliseconds(wait))
	if err == unix.EINTR {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	if n == 0 {
		return nil, true // timed out
	}
	if fds[0].Revents&unix.POLLIN == 0 {
		return nil, false // hung up, failed, or not open
	}

	buf := make([]byte, 256)
	n, err = unix.Read(fd, buf)
	if err == unix.EINTR || err == unix.EAGAIN {
		return nil, true
	}
	if err != nil || n <= 0 {
		return nil, false
	}
	return buf[:n], true
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
