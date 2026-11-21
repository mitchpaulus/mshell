//go:build linux || darwin

package main

// msh server's terminal on Unix: a PTY, which becomes the engine's
// controlling terminal (Server.go).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// serverSetsidEnv marks the copy of the engine started in its own session.
const serverSetsidEnv = "MSH_SERVER_SETSID"

// unixServerTerminal is the PTY's master; flushInput discards what the
// slave's input queue holds.
type unixServerTerminal struct {
	*os.File
	slaveFd int
}

func (t unixServerTerminal) resize(cols, rows, widthPx, heightPx uint16) error {
	return pty.Setsize(t.File, &pty.Winsize{Cols: cols, Rows: rows, X: widthPx, Y: heightPx})
}

func (t unixServerTerminal) flushInput() {
	flushInput(t.slaveFd)
}

// runServer is msh server. It returns only on a setup error.
func runServer() int {
	if code, handled := serverOwnSession(); handled {
		return code
	}

	// The protocol keeps the original stdin and stdout, at descriptors no
	// child inherits.
	protoIn, err := unix.FcntlInt(0, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	protoOut, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}

	master, slave, err := pty.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: opening a PTY: %v\n", err)
		return 1
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	// The PTY becomes this session's controlling terminal, with the engine
	// in the foreground, as a terminal's shell is.
	if err := unix.IoctlSetInt(int(slave.Fd()), unix.TIOCSCTTY, 0); err != nil {
		fmt.Fprintf(os.Stderr, "msh server: taking the PTY as controlling terminal: %v\n", err)
		return 1
	}
	// The slave's input queue is flushed through stdin, which it becomes.
	s, err := newServer(os.NewFile(uintptr(protoOut), "protocol out"), unixServerTerminal{File: master, slaveFd: 0})
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	if err := s.sendHello(); err != nil {
		return 1
	}

	for fd := 0; fd <= 2; fd++ {
		if err := dupTo(int(slave.Fd()), fd); err != nil {
			fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
			return 1
		}
	}
	slave.Close()

	// Keys typed while the engine itself is in the foreground would signal
	// it: Ctrl-C, Ctrl-\ and Ctrl-Z mean nothing to the engine between
	// blocks. Caught rather than ignored, so children get the default
	// action back when they exec.
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	go func() {
		for range signals {
		}
	}()

	return s.serve(os.NewFile(uintptr(protoIn), "protocol in"), 0)
}

// serverOwnSession makes the engine a session leader, so the PTY can become
// its controlling terminal. A process that leads its process group (as a
// shell starts it) can't call setsid, so then the engine runs a copy of
// itself in a new session and passes its exit code on.
func serverOwnSession() (int, bool) {
	copied := os.Getenv(serverSetsidEnv) != ""
	// Programs the engine runs must not inherit the marker.
	os.Unsetenv(serverSetsidEnv)
	if _, err := syscall.Setsid(); err == nil {
		return 0, false
	}
	if copied {
		fmt.Fprintln(os.Stderr, "msh server: can't start a new session")
		return 1, true
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1, true
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), serverSetsidEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), true
		}
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1, true
	}
	return 0, true
}
