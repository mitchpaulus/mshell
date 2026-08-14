//go:build linux || darwin

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

const terminalHandoffHelperEnv = "MSHELL_TERMINAL_HANDOFF_HELPER"

// TestTerminalHandoffHelper runs only in the subprocess placed inside a fresh
// session and controlling PTY by TestPipedShellStdinCanForegroundTTYChild.
func TestTerminalHandoffHelper(t *testing.T) {
	if os.Getenv(terminalHandoffHelperEnv) != "1" {
		t.Skip("terminal handoff helper")
	}

	list := NewList(3)
	list.Items[0] = MShellString{Content: "sh"}
	list.Items[1] = MShellString{Content: "-c"}
	list.Items[2] = MShellString{Content: "printf 'READY\\n'; IFS= read -r value; printf 'GOT:%s\\n' \"$value\""}
	list.StdinBehavior = STDIN_FILE
	list.StandardInputFile = "/dev/tty"

	pbm := NewPathBinManager()
	state := &EvalState{}
	context := ExecuteContext{
		StandardOutput: os.Stdout,
		StandardError:  os.Stderr,
		Pbm:            pbm,
	}

	result, exitCode, _, _ := RunProcess(*list, context, state)
	if !result.Success || exitCode != 0 {
		t.Fatalf("RunProcess result.Success = %v, exitCode = %d", result.Success, exitCode)
	}
	fmt.Fprintln(os.Stdout, "HELPER_DONE")
}

func TestPipelineTerminalHandoffHelper(t *testing.T) {
	if os.Getenv(terminalHandoffHelperEnv) != "1" {
		t.Skip("pipeline terminal handoff helper")
	}

	producer := NewList(3)
	producer.Items[0] = MShellString{Content: "sh"}
	producer.Items[1] = MShellString{Content: "-c"}
	producer.Items[2] = MShellString{Content: "printf 'unused pipe data\\n'"}

	consumer := NewList(3)
	consumer.Items[0] = MShellString{Content: "sh"}
	consumer.Items[1] = MShellString{Content: "-c"}
	consumer.Items[2] = MShellString{Content: "printf 'PIPE_READY\\n'; IFS= read -r value; printf 'PIPE_GOT:%s\\n' \"$value\""}
	consumer.StdinBehavior = STDIN_FILE
	consumer.StandardInputFile = "/dev/tty"

	pipeline := MShellPipe{
		List: MShellList{Items: []MShellObject{producer, consumer}},
	}
	pbm := NewPathBinManager()
	state := &EvalState{}
	stack := MShellStack{}
	context := ExecuteContext{
		StandardOutput: os.Stdout,
		StandardError:  os.Stderr,
		Variables:      make(map[string]MShellObject),
		Pbm:            pbm,
	}

	result, exitCode, _, _ := state.RunPipeline(pipeline, context, &stack)
	if !result.Success || exitCode != 0 {
		t.Fatalf("RunPipeline result.Success = %v, exitCode = %d", result.Success, exitCode)
	}
	fmt.Fprintln(os.Stdout, "PIPE_HELPER_DONE")
}

func TestPipedShellStdinCanForegroundTTYChild(t *testing.T) {
	if testing.Short() {
		t.Skip("PTY integration test")
	}
	runPipedStdinPTYHelper(t, "TestTerminalHandoffHelper", "hello from tty\n", "GOT:hello from tty", "HELPER_DONE")
}

func TestPipelineCanForegroundTTYStage(t *testing.T) {
	if testing.Short() {
		t.Skip("PTY integration test")
	}
	runPipedStdinPTYHelper(t, "TestPipelineTerminalHandoffHelper", "hello pipeline\n", "PIPE_GOT:hello pipeline", "PIPE_HELPER_DONE")
}

func runPipedStdinPTYHelper(t *testing.T, helperName, terminalInput string, expectedOutput ...string) {
	t.Helper()

	// The wrapper owns a fresh controlling PTY but gives the Go helper a pipe as
	// stdin.  RunProcess then gives its child /dev/tty explicitly.  Testing
	// os.Stdin would skip tcsetpgrp and the child would stop forever on SIGTTIN.
	command := exec.Command("sh", "-c", "printf 'piped shell input\\n' | \"$1\" -test.run \"^$2$\"", "sh", os.Args[0], helperName)
	command.Env = append(os.Environ(), terminalHandoffHelperEnv+"=1")
	ptmx, err := pty.Start(command)
	if err != nil {
		t.Fatalf("start helper in PTY: %v", err)
	}

	readDone := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(ptmx)
		readDone <- output
	}()

	if _, err := ptmx.Write([]byte(terminalInput)); err != nil {
		terminatePTYProcess(t, command, ptmx)
		t.Fatalf("write terminal input: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- command.Wait()
	}()

	select {
	case err := <-waitDone:
		ptmx.Close()
		output := <-readDone
		if err != nil {
			t.Fatalf("PTY helper failed: %v\noutput:\n%s", err, output)
		}
		for _, expected := range expectedOutput {
			if !bytes.Contains(output, []byte(expected)) {
				t.Fatalf("PTY output does not contain %q; output:\n%s", expected, output)
			}
		}
	case <-time.After(5 * time.Second):
		terminatePTYProcess(t, command, ptmx)
		output := <-readDone
		t.Fatalf("terminal handoff hung; killed helper process group\noutput:\n%s", output)
	}
}

const parallelReclaimSleepEnv = "MSHELL_PARALLEL_RECLAIM_SLEEP"

// TestParallelTerminalReclaimHelper runs only inside the shared PTY session
// created by TestParallelShellsSharingPTYSucceed.  Each helper is one msh-like
// process running a single external command whose stdio is the shared terminal.
func TestParallelTerminalReclaimHelper(t *testing.T) {
	if os.Getenv(terminalHandoffHelperEnv) != "1" {
		t.Skip("parallel terminal reclaim helper")
	}
	duration := os.Getenv(parallelReclaimSleepEnv)
	if duration == "" {
		duration = "0.2"
	}

	list := NewList(2)
	list.Items[0] = MShellString{Content: "sleep"}
	list.Items[1] = MShellString{Content: duration}

	pbm := NewPathBinManager()
	state := &EvalState{}
	context := ExecuteContext{
		StandardOutput: os.Stdout,
		StandardError:  os.Stderr,
		Pbm:            pbm,
	}

	result, exitCode, _, _ := RunProcess(*list, context, state)
	if !result.Success || exitCode != 0 {
		t.Fatalf("RunProcess result.Success = %v, exitCode = %d", result.Success, exitCode)
	}
	fmt.Fprintln(os.Stdout, "PARALLEL_OK")
}

// TestParallelShellsSharingPTYSucceed reproduces the race hit under parallel
// runners such as redo or make -j: several shells share one terminal, each
// wraps its child in a foreground transaction, and the "previous foreground
// process group" each records is a sibling's transient child group.  By release
// time that group has exited, so the restoring tcsetpgrp fails with ESRCH.
// Every helper's child exits 0, so every helper must succeed.
func TestParallelShellsSharingPTYSucceed(t *testing.T) {
	if testing.Short() {
		t.Skip("PTY integration test")
	}

	// The staggered durations make earlier siblings' child groups reliably dead
	// by the time later helpers hand the terminal back to them.
	script := `for d in 0.15 0.3 0.45 0.6 0.75 0.9; do ` +
		parallelReclaimSleepEnv + `="$d" "$1" -test.run '^TestParallelTerminalReclaimHelper$' & ` +
		`done; wait; printf 'WRAPPER_DONE\n'`
	command := exec.Command("sh", "-c", script, "sh", os.Args[0])
	command.Env = append(os.Environ(), terminalHandoffHelperEnv+"=1")
	ptmx, err := pty.Start(command)
	if err != nil {
		t.Fatalf("start parallel helpers in PTY: %v", err)
	}

	readDone := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(ptmx)
		readDone <- output
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- command.Wait()
	}()

	select {
	case err := <-waitDone:
		ptmx.Close()
		output := <-readDone
		if err != nil {
			t.Fatalf("PTY wrapper failed: %v\noutput:\n%s", err, output)
		}
		if !bytes.Contains(output, []byte("WRAPPER_DONE")) {
			t.Fatalf("PTY output does not contain WRAPPER_DONE; output:\n%s", output)
		}
		if got := bytes.Count(output, []byte("PARALLEL_OK")); got != 6 {
			t.Fatalf("PARALLEL_OK count = %d, want 6; output:\n%s", got, output)
		}
	case <-time.After(30 * time.Second):
		terminatePTYProcess(t, command, ptmx)
		output := <-readDone
		t.Fatalf("parallel reclaim test hung; killed helper process group\noutput:\n%s", output)
	}
}

func terminatePTYProcess(t *testing.T, command *exec.Cmd, ptmx *os.File) {
	t.Helper()
	if command.Process != nil {
		// pty.Start creates a new session led by command.Process, so a negative
		// pid terminates the wrapper and every descendant instead of leaving a
		// stopped child behind after a failed test.
		killErr := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if killErr != nil && !strings.Contains(killErr.Error(), "no such process") {
			t.Logf("kill PTY process group: %v", killErr)
		}
		command.Process.Kill()
	}
	ptmx.Close()
}
