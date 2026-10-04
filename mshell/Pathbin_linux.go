package main

import (
	"errors"
	"os"
	"os/signal"
	"os/exec"
	"fmt"
	"syscall"
	"golang.org/x/term"
	"golang.org/x/sys/unix"
)

const nullDevice = "/dev/null"

// classifyStartError maps a cmd.Start() failure to a negative exit code that
// carries the raw host errno verbatim: -(256 + errno). No lookup table — the
// number is the answer, so every errno (even ones we never enumerate) is exact.
func classifyStartError(err error) int {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return -(256 + int(errno))
	}
	return ExitStartUnknown
}

// signalExitCode encodes a signal death as -(128 + signal), mirroring the
// familiar POSIX 128+N. Returns false if the process was not killed by a signal.
func signalExitCode(ps *os.ProcessState) (int, bool) {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return -(signalBase + int(ws.Signal())), true
	}
	return 0, false
}

func (pbm *PathBinManager) SetupCommand(allArgs []string) (*exec.Cmd) {
	cmd := exec.Command(allArgs[0], allArgs[1:]...)
	// Put subprocess in its own process group so CTRL-C only affects it, not the shell
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	return cmd
}

// SetCommandPgid configures the process group a pipeline stage joins before it
// is started. pgid == 0 makes the new process the leader of its own process
// group; a positive pgid makes it join that existing group so the whole
// pipeline shares one process group and can be the terminal foreground together.
func SetCommandPgid(cmd *exec.Cmd, pgid int) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = pgid
}

// IgnoreSignalsForJobControl ignores SIGTTOU and SIGTTIN which would stop the shell
// when it manipulates the foreground process group. Returns a function to restore signals.
func IgnoreSignalsForJobControl() func() {
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
	return func() {
		signal.Reset(syscall.SIGTTOU, syscall.SIGTTIN)
	}
}

// SetForegroundProcessGroup makes the given process group the foreground process group
// of the terminal. Returns the previous foreground process group ID so it can be restored.
// IMPORTANT: Call IgnoreSignalsForJobControl() before this to avoid SIGTTOU stopping the shell.
func SetForegroundProcessGroup(ttyFd int, pgid int) (int, error) {
	// Get current foreground process group
	oldPgid, err := unix.IoctlGetInt(ttyFd, unix.TIOCGPGRP)
	if err != nil {
		return 0, err
	}

	// Set new foreground process group
	err = unix.IoctlSetPointerInt(ttyFd, unix.TIOCSPGRP, pgid)
	if err != nil {
		return oldPgid, err
	}

	return oldPgid, nil
}

// RestoreForegroundProcessGroup restores the previous process group as foreground
// IMPORTANT: Call IgnoreSignalsForJobControl() before this to avoid SIGTTOU stopping the shell.
func RestoreForegroundProcessGroup(ttyFd int, pgid int) error {
	return unix.IoctlSetPointerInt(ttyFd, unix.TIOCSPGRP, pgid)
}

// ContinueProcessGroup resumes a process group that may have stopped on an
// early terminal read before it became foreground.
func ContinueProcessGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGCONT)
}

// KillProcessGroup terminates every process in a failed foreground launch.
func KillProcessGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// IsTerminal returns true if the file descriptor is connected to a terminal
func IsTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

// CanControlTerminal reports whether fd names this session's controlling
// terminal, rather than merely some terminal device.
func CanControlTerminal(fd int) bool {
	_, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	return err == nil
}

// ShellOwnsTerminal reports whether this process's group is the terminal's
// current foreground process group.  This is the standard bash/fish gate: a
// shell that is not the foreground owner (a script under redo/make -j, a
// backgrounded script) must not hand the terminal to its children, because
// grabbing a terminal owned by someone else is exactly how parallel shells
// clobber each other's foreground state.
func ShellOwnsTerminal(ttyFd int) bool {
	pgid, err := unix.IoctlGetInt(ttyFd, unix.TIOCGPGRP)
	return err == nil && pgid == syscall.Getpgrp()
}

// ShellProcessGroup returns this shell's own process group, the fallback
// hand-back target when the recorded previous foreground group has exited.
func ShellProcessGroup() int {
	return syscall.Getpgrp()
}

func DuplicateTerminalHandle(fd int) (int, error) {
	return unix.Dup(fd)
}

func CloseTerminalHandle(fd int) error {
	return syscall.Close(fd)
}

type posixTerminalModeSnapshot struct {
	fd    int
	state *term.State
}

func CaptureTerminalMode(fd int) (TerminalModeSnapshot, error) {
	state, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}
	return &posixTerminalModeSnapshot{fd: fd, state: state}, nil
}

func (snapshot *posixTerminalModeSnapshot) Restore() error {
	// tcsetattr from a background process group raises SIGTTOU (default action:
	// stop).  At restore time the terminal may already belong to another group,
	// so the same protection used around tcsetpgrp applies here.
	restoreSignals := IgnoreSignalsForJobControl()
	defer restoreSignals()
	return term.Restore(snapshot.fd, snapshot.state)
}

func IsPathSeparator(c uint8) bool {
	return c == '/'
}

func (s *TermState) UpdateSize() {
	var err error
	s.numCols, s.numRows, err = term.GetSize(s.stdInFd)
	if err != nil {
		fmt.Fprintf(s.f, "Error getting terminal size for FD %d: %s\n", s.stdInFd, err)
	}
}
