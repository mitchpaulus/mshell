package main

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTerminalControlBackend struct {
	mu           sync.Mutex
	operations   []string
	previousPgid int
	captureErr    error
	setErr       error
	continueErr    error
	restoreErrs    []error // popped once per restoreForeground call; nil entries succeed
	restoreTargets []int
	modeRestoreErr error
	notOwner       bool
	shellPgid      int
}

type fakeTerminalModeSnapshot struct {
	backend *fakeTerminalControlBackend
	err     error
}

func (snapshot *fakeTerminalModeSnapshot) Restore() error {
	snapshot.backend.record("restoreMode")
	return snapshot.err
}

func (backend *fakeTerminalControlBackend) captureMode(terminalFd int) (TerminalModeSnapshot, error) {
	backend.record("capture")
	if backend.captureErr != nil {
		return nil, backend.captureErr
	}
	return &fakeTerminalModeSnapshot{backend: backend, err: backend.modeRestoreErr}, nil
}

func (backend *fakeTerminalControlBackend) record(operation string) {
	backend.mu.Lock()
	backend.operations = append(backend.operations, operation)
	backend.mu.Unlock()
}

func (backend *fakeTerminalControlBackend) setForeground(terminalFd, pgid int) (int, error) {
	backend.record("set")
	return backend.previousPgid, backend.setErr
}

func (backend *fakeTerminalControlBackend) restoreForeground(terminalFd, pgid int) error {
	backend.record("restore")
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.restoreTargets = append(backend.restoreTargets, pgid)
	if len(backend.restoreErrs) == 0 {
		return nil
	}
	err := backend.restoreErrs[0]
	backend.restoreErrs = backend.restoreErrs[1:]
	return err
}

func (backend *fakeTerminalControlBackend) shellOwnsTerminal(terminalFd int) bool {
	return !backend.notOwner
}

func (backend *fakeTerminalControlBackend) shellProcessGroup() int {
	return backend.shellPgid
}

func (backend *fakeTerminalControlBackend) recordedRestoreTargets() []int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]int(nil), backend.restoreTargets...)
}

func (backend *fakeTerminalControlBackend) continueProcessGroup(pgid int) error {
	backend.record("continue")
	return backend.continueErr
}

func (backend *fakeTerminalControlBackend) recordedOperations() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]string(nil), backend.operations...)
}

func TestForegroundControllerAcquireAndReleaseOrder(t *testing.T) {
	backend := &fakeTerminalControlBackend{previousPgid: 41}
	controller := foregroundController{backend: backend, inputGate: &shellInputGate{}}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 9}, 52)
	if err != nil {
		t.Fatalf("acquire returned error: %v", err)
	}
	if lease == nil {
		t.Fatal("acquire returned a nil lease")
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release returned error: %v", err)
	}

	want := []string{"capture", "set", "continue", "restore", "restoreMode"}
	if got := backend.recordedOperations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
}

func TestResolvedProcessStdioPrefersInputTerminal(t *testing.T) {
	stdin := &TerminalEndpoint{fd: 1, controlsForeground: true}
	stdout := &TerminalEndpoint{fd: 2, controlsForeground: true}
	stderr := &TerminalEndpoint{fd: 3, controlsForeground: true}
	stdio := ResolvedProcessStdio{
		StdinTerminal:  stdin,
		StdoutTerminal: stdout,
		StderrTerminal: stderr,
	}

	if got := stdio.ControlTerminal(); got != stdin {
		t.Fatalf("ControlTerminal() = %v, want stdin terminal", got)
	}
	stdio.StdinTerminal = nil
	if got := stdio.ControlTerminal(); got != stdout {
		t.Fatalf("ControlTerminal() = %v, want stdout terminal fallback", got)
	}
	stdio.StdoutTerminal = nil
	if got := stdio.ControlTerminal(); got != stderr {
		t.Fatalf("ControlTerminal() = %v, want stderr terminal fallback", got)
	}
	stderr.controlsForeground = false
	if got := stdio.ControlTerminal(); got != nil {
		t.Fatalf("ControlTerminal() = %v, want nil for a non-controlling TTY", got)
	}
}

func TestForegroundControllerRollsBackContinueFailure(t *testing.T) {
	backend := &fakeTerminalControlBackend{
		previousPgid: 7,
		continueErr: errors.New("continue failed"),
	}
	controller := foregroundController{backend: backend, inputGate: &shellInputGate{}}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 8)
	if lease != nil {
		t.Fatal("failed acquire returned a lease")
	}
	if err == nil || !strings.Contains(err.Error(), "continue failed") {
		t.Fatalf("acquire error = %v, want continue failure", err)
	}

	want := []string{"capture", "set", "continue", "restore", "restoreMode"}
	if got := backend.recordedOperations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want rollback sequence %v", got, want)
	}

	// A rollback must release the serialized control transaction.
	backend.continueErr = nil
	secondLease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 9)
	if err != nil {
		t.Fatalf("second acquire returned error: %v", err)
	}
	if err := secondLease.Release(); err != nil {
		t.Fatalf("second release returned error: %v", err)
	}
}

func TestForegroundControllerReleasesGateAfterModeCaptureFailure(t *testing.T) {
	backend := &fakeTerminalControlBackend{captureErr: errors.New("capture failed")}
	gate := &shellInputGate{}
	controller := foregroundController{backend: backend, inputGate: gate}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 8)
	if lease != nil {
		t.Fatal("failed mode capture returned a lease")
	}
	if err == nil || !strings.Contains(err.Error(), "capture failed") {
		t.Fatalf("acquire error = %v, want mode-capture failure", err)
	}
	if got := backend.recordedOperations(); !reflect.DeepEqual(got, []string{"capture"}) {
		t.Fatalf("operations = %v, want capture only", got)
	}
	if err := gate.beginRead(); err != nil {
		t.Fatalf("shell input remained blocked after capture failure: %v", err)
	}
	gate.endRead()
}

func TestForegroundControllerSerializesTransactions(t *testing.T) {
	backend := &fakeTerminalControlBackend{previousPgid: 1}
	controller := foregroundController{backend: backend, inputGate: &shellInputGate{}}
	first, err := controller.acquire(&TerminalEndpoint{fd: 3}, 10)
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}

	type acquireResult struct {
		lease *ForegroundLease
		err   error
	}
	result := make(chan acquireResult, 1)
	go func() {
		lease, acquireErr := controller.acquire(&TerminalEndpoint{fd: 3}, 11)
		result <- acquireResult{lease: lease, err: acquireErr}
	}()

	select {
	case second := <-result:
		if second.lease != nil {
			second.lease.Release()
		}
		t.Fatal("second foreground transaction acquired before the first released")
	case <-time.After(50 * time.Millisecond):
	}

	if err := first.Release(); err != nil {
		t.Fatalf("first release returned error: %v", err)
	}

	select {
	case second := <-result:
		if second.err != nil {
			t.Fatalf("second acquire returned error: %v", second.err)
		}
		if err := second.lease.Release(); err != nil {
			t.Fatalf("second release returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second foreground transaction did not acquire after release")
	}
}

func TestForegroundControllerRejectsOutstandingShellRead(t *testing.T) {
	backend := &fakeTerminalControlBackend{}
	gate := &shellInputGate{}
	controller := foregroundController{backend: backend, inputGate: gate}
	if err := gate.beginRead(); err != nil {
		t.Fatalf("begin shell read: %v", err)
	}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 12)
	if lease != nil {
		t.Fatal("acquire with an outstanding read returned a lease")
	}
	if err == nil || !strings.Contains(err.Error(), "outstanding") {
		t.Fatalf("acquire error = %v, want outstanding-read error", err)
	}
	if got := backend.recordedOperations(); len(got) != 0 {
		t.Fatalf("backend operations = %v, want none before input quiescence", got)
	}

	gate.endRead()
	lease, err = controller.acquire(&TerminalEndpoint{fd: 3}, 12)
	if err != nil {
		t.Fatalf("acquire after read quiesced: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestAcquireSkipsWhenShellDoesNotOwnTerminal(t *testing.T) {
	backend := &fakeTerminalControlBackend{notOwner: true}
	gate := &shellInputGate{}
	controller := foregroundController{backend: backend, inputGate: gate}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 13)
	if err != nil {
		t.Fatalf("acquire while not terminal owner returned error: %v", err)
	}
	if lease != nil {
		t.Fatal("acquire while not terminal owner returned a lease")
	}
	if got := backend.recordedOperations(); len(got) != 0 {
		t.Fatalf("backend operations = %v, want none for a non-owner shell", got)
	}
	// The skipped transaction must leave shell input usable.
	if err := gate.beginRead(); err != nil {
		t.Fatalf("shell input blocked after skipped acquisition: %v", err)
	}
	gate.endRead()
}

func TestReleaseFallsBackToOwnGroupWhenPreviousGroupIsGone(t *testing.T) {
	backend := &fakeTerminalControlBackend{
		previousPgid: 41,
		shellPgid:    77,
		restoreErrs:  []error{errors.New("no such process")},
	}
	gate := &shellInputGate{}
	controller := foregroundController{backend: backend, inputGate: gate}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 13)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release with a dead previous group returned error: %v", err)
	}
	if got, want := backend.recordedRestoreTargets(), []int{41, 77}; !reflect.DeepEqual(got, want) {
		t.Fatalf("restore targets = %v, want dead previous group then own group %v", got, want)
	}
	want := []string{"capture", "set", "continue", "restore", "restore", "restoreMode"}
	if got := backend.recordedOperations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if err := gate.beginRead(); err != nil {
		t.Fatalf("shell input blocked after successful fallback restore: %v", err)
	}
	gate.endRead()
}

func TestReleaseReportsErrorWhenFallbackRestoreAlsoFails(t *testing.T) {
	backend := &fakeTerminalControlBackend{
		previousPgid: 41,
		shellPgid:    77,
		restoreErrs:  []error{errors.New("restore failed"), errors.New("terminal gone")},
	}
	gate := &shellInputGate{}
	controller := foregroundController{backend: backend, inputGate: gate}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 13)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	releaseErr := lease.Release()
	if releaseErr == nil || !strings.Contains(releaseErr.Error(), "own process group also failed") {
		t.Fatalf("release error = %v, want combined restore failure", releaseErr)
	}
	if err := lease.Release(); err == nil {
		t.Fatal("repeated release hid the prior restoration failure")
	}
	// Both hand-back targets failing means the terminal itself is unusable, so
	// shell input must not stay wedged behind it.
	if err := gate.beginRead(); err != nil {
		t.Fatalf("shell input blocked after unrecoverable terminal loss: %v", err)
	}
	gate.endRead()
}

func TestModeRestoreFailureKeepsShellInputBlocked(t *testing.T) {
	backend := &fakeTerminalControlBackend{modeRestoreErr: errors.New("mode restore failed")}
	gate := &shellInputGate{}
	controller := foregroundController{backend: backend, inputGate: gate}

	lease, err := controller.acquire(&TerminalEndpoint{fd: 3}, 14)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lease.Release(); err == nil || !strings.Contains(err.Error(), "mode restore failed") {
		t.Fatalf("release error = %v, want terminal-mode restoration failure", err)
	}
	if err := gate.beginRead(); err == nil {
		gate.endRead()
		t.Fatal("shell input was unblocked after terminal-mode restoration failed")
	}
}
