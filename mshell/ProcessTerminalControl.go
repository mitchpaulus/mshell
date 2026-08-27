package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

// TerminalEndpoint is a resolved terminal used by a child process.  The file
// descriptor/handle belongs to the already-resolved standard stream; it is not
// inferred from os.Stdin after redirection has been applied.
type TerminalEndpoint struct {
	fd                 int
	controlsForeground bool
	owned              bool
}

func duplicateTerminalEndpoint(endpoint *TerminalEndpoint) (*TerminalEndpoint, error) {
	if endpoint == nil {
		return nil, nil
	}
	fd, err := DuplicateTerminalHandle(endpoint.fd)
	if err != nil {
		return nil, err
	}
	return &TerminalEndpoint{
		fd:                 fd,
		controlsForeground: endpoint.controlsForeground,
		owned:              true,
	}, nil
}

func (endpoint *TerminalEndpoint) Close() error {
	if endpoint == nil || !endpoint.owned {
		return nil
	}
	endpoint.owned = false
	return CloseTerminalHandle(endpoint.fd)
}

// ResolvedProcessStdio records the final streams passed to exec.Cmd together
// with any terminal identity they preserve.  Keeping this metadata beside the
// streams prevents process control from re-resolving a different default.
type ResolvedProcessStdio struct {
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	StdinTerminal  *TerminalEndpoint
	StdoutTerminal *TerminalEndpoint
	StderrTerminal *TerminalEndpoint
}

// resolveControlTerminalEndpoint checks whether a resolved stream can control
// this process's foreground job.  The platform probe also establishes terminal
// identity, so a separate IsTerminal call would duplicate the same OS work.
func resolveControlTerminalEndpoint(stream any, fallback *os.File) *TerminalEndpoint {
	if stream == nil {
		stream = fallback
	}
	if stream == nil {
		return nil
	}

	fdProvider, ok := stream.(fileDescriptorProvider)
	if !ok {
		return nil
	}

	fd := int(fdProvider.Fd())
	if !CanControlTerminal(fd) {
		return nil
	}

	return &TerminalEndpoint{
		fd:                 fd,
		controlsForeground: true,
	}
}

func resolveProcessStdio(stdin io.Reader, stdout, stderr io.Writer) ResolvedProcessStdio {
	stdio := ResolvedProcessStdio{
		Stdin:          stdin,
		Stdout:         stdout,
		Stderr:         stderr,
	}

	// Terminal selection is ordered, so stop probing as soon as the governing
	// endpoint is known.  This avoids repeated ioctls/GetConsoleMode calls for
	// the usual case where all three streams share one terminal.
	stdio.StdinTerminal = resolveControlTerminalEndpoint(stdin, os.Stdin)
	if stdio.StdinTerminal != nil {
		return stdio
	}
	stdio.StdoutTerminal = resolveControlTerminalEndpoint(stdout, os.Stdout)
	if stdio.StdoutTerminal != nil {
		return stdio
	}
	stdio.StderrTerminal = resolveControlTerminalEndpoint(stderr, os.Stderr)
	return stdio
}

// ControlTerminal returns the terminal governing the job.  Stdin is preferred
// because it is the endpoint whose read access is gated by foreground control.
// A synchronous job with redirected stdin can still need foreground signal and
// output semantics, so terminal stdout/stderr are valid fallbacks.
func (stdio ResolvedProcessStdio) ControlTerminal() *TerminalEndpoint {
	if stdio.StdinTerminal != nil && stdio.StdinTerminal.controlsForeground {
		return stdio.StdinTerminal
	}
	if stdio.StdoutTerminal != nil && stdio.StdoutTerminal.controlsForeground {
		return stdio.StdoutTerminal
	}
	if stdio.StderrTerminal != nil && stdio.StderrTerminal.controlsForeground {
		return stdio.StderrTerminal
	}
	return nil
}

// TerminalModeSnapshot is platform-specific saved state for every console/TTY
// mode affected by a foreground job.
type TerminalModeSnapshot interface {
	Restore() error
}

type terminalControlBackend interface {
	captureMode(terminalFd int) (TerminalModeSnapshot, error)
	setForeground(terminalFd, pgid int) (int, error)
	restoreForeground(terminalFd, pgid int) error
	continueProcessGroup(pgid int) error
	shellOwnsTerminal(terminalFd int) bool
	shellProcessGroup() int
}

type platformTerminalControlBackend struct{}

func (platformTerminalControlBackend) captureMode(terminalFd int) (TerminalModeSnapshot, error) {
	return CaptureTerminalMode(terminalFd)
}

func (platformTerminalControlBackend) setForeground(terminalFd, pgid int) (int, error) {
	restoreSignals := IgnoreSignalsForJobControl()
	previousPgid, err := SetForegroundProcessGroup(terminalFd, pgid)
	restoreSignals()
	return previousPgid, err
}

func (platformTerminalControlBackend) restoreForeground(terminalFd, pgid int) error {
	restoreSignals := IgnoreSignalsForJobControl()
	err := RestoreForegroundProcessGroup(terminalFd, pgid)
	restoreSignals()
	return err
}

func (platformTerminalControlBackend) continueProcessGroup(pgid int) error {
	return ContinueProcessGroup(pgid)
}

func (platformTerminalControlBackend) shellOwnsTerminal(terminalFd int) bool {
	return ShellOwnsTerminal(terminalFd)
}

func (platformTerminalControlBackend) shellProcessGroup() int {
	return ShellProcessGroup()
}

// shellInputGate makes the no-competing-reads invariant executable.  Today the
// interactive reader is synchronous, but keeping this gate at the actual Read
// boundary preserves the invariant if input later moves to a worker goroutine.
type shellInputGate struct {
	mu               sync.Mutex
	readInProgress   bool
	foregroundActive bool
}

func (gate *shellInputGate) beginRead() error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.foregroundActive {
		return fmt.Errorf("shell input read attempted while a foreground job owns the terminal")
	}
	if gate.readInProgress {
		return fmt.Errorf("concurrent shell input reads are not allowed")
	}
	gate.readInProgress = true
	return nil
}

func (gate *shellInputGate) endRead() {
	gate.mu.Lock()
	gate.readInProgress = false
	gate.mu.Unlock()
}

func (gate *shellInputGate) beginForeground() error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.readInProgress {
		return fmt.Errorf("cannot foreground a job while a shell input read is outstanding")
	}
	if gate.foregroundActive {
		return fmt.Errorf("another foreground job already owns shell input")
	}
	gate.foregroundActive = true
	return nil
}

func (gate *shellInputGate) endForeground() {
	gate.mu.Lock()
	gate.foregroundActive = false
	gate.mu.Unlock()
}

var processShellInputGate = &shellInputGate{}

// foregroundController implements the single controlJob reservation in the
// formal model.  Its mutex remains held from acquisition through reclamation.
type foregroundController struct {
	mu        sync.Mutex
	backend   terminalControlBackend
	inputGate *shellInputGate
}

var processForegroundController = foregroundController{
	backend: platformTerminalControlBackend{},
	inputGate: processShellInputGate,
}

type ForegroundLease struct {
	controller   *foregroundController
	terminal     TerminalEndpoint
	previousPgid int
	modeSnapshot TerminalModeSnapshot
	released     bool
	releaseErr   error
}

func acquireForeground(endpoint *TerminalEndpoint, pgid int) (*ForegroundLease, error) {
	return processForegroundController.acquire(endpoint, pgid)
}

func (controller *foregroundController) acquire(endpoint *TerminalEndpoint, pgid int) (*ForegroundLease, error) {
	if endpoint == nil || pgid <= 0 {
		return nil, nil
	}

	controller.mu.Lock()
	// Only run the foreground transaction when this shell currently owns the
	// terminal, the same gate bash and fish apply.  A shell that is not the
	// foreground owner (one of several parallel shells sharing a terminal under
	// redo or make -j, or a backgrounded script) taking the terminal is exactly
	// how the cross-process reclaim race starts.  Its child simply runs without
	// a handoff; a child that reads the terminal is stopped by SIGTTIN, which is
	// standard background-job behavior.
	if !controller.backend.shellOwnsTerminal(endpoint.fd) {
		controller.mu.Unlock()
		return nil, nil
	}
	if err := controller.inputGate.beginForeground(); err != nil {
		controller.mu.Unlock()
		return nil, err
	}
	modeSnapshot, err := controller.backend.captureMode(endpoint.fd)
	if err != nil {
		controller.inputGate.endForeground()
		controller.mu.Unlock()
		return nil, fmt.Errorf("capture terminal fd %d mode: %w", endpoint.fd, err)
	}
	previousPgid, err := controller.backend.setForeground(endpoint.fd, pgid)
	if err != nil {
		controller.inputGate.endForeground()
		controller.mu.Unlock()
		// ESRCH: the child group is already fully reaped (a fast pipeline's
		// stages are waited concurrently, so this races with acquisition).
		// There is nothing left to foreground and the terminal was not touched;
		// run without a handoff instead of killing a job that already finished.
		if errors.Is(err, syscall.ESRCH) {
			return nil, nil
		}
		return nil, fmt.Errorf("give terminal fd %d to process group %d: %w", endpoint.fd, pgid, err)
	}

	// A process that attempted a terminal read between Start and tcsetpgrp may
	// already have been stopped by SIGTTIN.  Foreground it before continuing it.
	if err := controller.backend.continueProcessGroup(pgid); err != nil {
		restoreErr := controller.backend.restoreForeground(endpoint.fd, previousPgid)
		var modeRestoreErr error
		if restoreErr == nil {
			modeRestoreErr = modeSnapshot.Restore()
		}
		if restoreErr == nil && modeRestoreErr == nil {
			controller.inputGate.endForeground()
		}
		controller.mu.Unlock()
		if restoreErr != nil {
			return nil, fmt.Errorf("continue process group %d: %w; terminal rollback also failed: %v", pgid, err, restoreErr)
		}
		if modeRestoreErr != nil {
			return nil, fmt.Errorf("continue process group %d: %w; terminal-mode rollback also failed: %v", pgid, err, modeRestoreErr)
		}
		// ESRCH: the group vanished between tcsetpgrp and SIGCONT because its
		// members were reaped concurrently.  The rollback above already returned
		// the terminal; a job that has already finished needs no foregrounding.
		if errors.Is(err, syscall.ESRCH) {
			return nil, nil
		}
		return nil, fmt.Errorf("continue process group %d: %w", pgid, err)
	}

	return &ForegroundLease{
		controller:   controller,
		terminal:     *endpoint,
		previousPgid: previousPgid,
		modeSnapshot: modeSnapshot,
	}, nil
}

func (lease *ForegroundLease) Release() error {
	if lease == nil {
		return nil
	}
	if lease.released {
		return lease.releaseErr
	}
	lease.released = true

	backend := lease.controller.backend
	err := backend.restoreForeground(lease.terminal.fd, lease.previousPgid)
	if err != nil {
		// The recorded previous owner is a snapshot, not a stable handle: under
		// a parallel runner it can be a sibling shell's transient child group
		// that has already exited, so the hand-back fails with ESRCH.  That is
		// bookkeeping, not a command failure.  Hand the terminal to this shell's
		// own group instead, which is the group bash and fish restore.
		if fallbackErr := backend.restoreForeground(lease.terminal.fd, backend.shellProcessGroup()); fallbackErr == nil {
			err = nil
		} else {
			err = fmt.Errorf("restore terminal fd %d to process group %d: %w; restore to own process group also failed: %v", lease.terminal.fd, lease.previousPgid, err, fallbackErr)
		}
	}
	var modeRestoreErr error
	if err == nil {
		modeRestoreErr = lease.modeSnapshot.Restore()
		if modeRestoreErr == nil {
			lease.controller.inputGate.endForeground()
		}
	} else {
		// Both hand-back targets failed, so the terminal itself is unusable
		// (for example a closed PTY).  Keeping shell input blocked would wedge
		// every later command behind a gate protecting a terminal that no
		// longer exists; later commands re-probe the terminal themselves and
		// skip control when it is gone.
		lease.controller.inputGate.endForeground()
	}
	lease.controller.mu.Unlock()
	if err != nil {
		lease.releaseErr = err
	} else if modeRestoreErr != nil {
		lease.releaseErr = fmt.Errorf("restore terminal fd %d mode: %w", lease.terminal.fd, modeRestoreErr)
	}
	return lease.releaseErr
}
