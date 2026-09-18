//go:build linux || darwin

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

// Interactive command history storage permissions and safety.
func assertHistoryMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s: mode %o, want %o", path, info.Mode().Perm(), mode)
	}
}

func TestHistoryPrivacySaveAndRepair(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("XDG_DATA_HOME", parent)
	oldMask := syscall.Umask(0022)
	defer syscall.Umask(oldMask)
	oldPending := historyToSave
	defer func() { historyToSave = oldPending }()
	item := HistoryItem{UnixTimeUtc: 123, Command: "private command", Directory: "/private"}
	historyToSave = []HistoryItem{item}
	state := &TermState{}
	state.saveHistory()
	if len(historyToSave) != 0 {
		t.Fatal("history was not saved")
	}
	dir := filepath.Join(parent, "msh")
	assertHistoryMode(t, dir, 0700)
	files := []string{"msh_history", "msh_commands", "msh_dirs"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		assertHistoryMode(t, path, 0600)
		if err := os.Chmod(path, 0644); err != nil { t.Fatal(err) }
	}
	if err := os.Chmod(parent, 0755); err != nil { t.Fatal(err) }
	if err := os.Chmod(dir, 0755); err != nil { t.Fatal(err) }
	items, err := ReadHistory(dir)
	if err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(items, []HistoryItem{item}) {
		t.Fatalf("history changed: %#v", items)
	}
	assertHistoryMode(t, parent, 0755)
	assertHistoryMode(t, dir, 0700)
	for _, name := range files { assertHistoryMode(t, filepath.Join(dir, name), 0600) }
	if err := os.Chmod(dir, 0500); err != nil { t.Fatal(err) }
	if err := os.Chmod(filepath.Join(dir, "msh_commands"), 0400); err != nil { t.Fatal(err) }
	if _, err := ReadHistory(dir); err != nil { t.Fatal(err) }
	assertHistoryMode(t, dir, 0500)
	assertHistoryMode(t, filepath.Join(dir, "msh_commands"), 0400)
	if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
	if err := os.Chmod(filepath.Join(dir, "msh_commands"), 0644); err != nil { t.Fatal(err) }
	historyToSave = []HistoryItem{item}
	state.saveHistory()
	assertHistoryMode(t, filepath.Join(dir, "msh_commands"), 0600)
	items, err = ReadHistory(dir)
	if err != nil || len(items) != 2 { t.Fatalf("append failed: %v, %#v", err, items) }
}

func TestHistoryRejectsUnexpectedObjects(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "fifo", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "msh")
			if err := ensureHistoryDir(dir); err != nil { t.Fatal(err) }
			target := filepath.Join(parent, "unrelated")
			if err := os.WriteFile(target, []byte("untouched"), 0644); err != nil { t.Fatal(err) }
			path := filepath.Join(dir, "msh_commands")
			var err error
			switch kind {
			case "symlink": err = os.Symlink(target, path)
			case "hardlink": err = os.Link(target, path)
			case "directory": err = os.Mkdir(path, 0755)
			case "fifo": err = syscall.Mkfifo(path, 0644)
			case "foreign":
				if os.Geteuid() != 0 { t.Skip("changing ownership requires root") }
				err = os.WriteFile(path, []byte("foreign"), 0644)
				if err == nil { err = os.Chown(path, 65534, 65534) }
			}
			if err != nil { t.Fatal(err) }
			if err := prepareHistoryStorage(dir); err == nil { t.Fatal("unsafe history accepted") }
			if f, err := openHistoryFile(path, os.O_APPEND|os.O_WRONLY); err == nil {
				f.Close()
				t.Fatal("unsafe save accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "untouched" { t.Fatal("unrelated data modified") }
			assertHistoryMode(t, target, 0644)
		})
	}
}

func TestHistoryRejectsDirectorySymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "shared")
	if err := os.Mkdir(target, 0755); err != nil { t.Fatal(err) }
	dir := filepath.Join(parent, "msh")
	if err := os.Symlink(target, dir); err != nil { t.Fatal(err) }
	if err := ensureHistoryDir(dir); err == nil { t.Fatal("directory symlink accepted") }
	assertHistoryMode(t, target, 0755)
}

func TestHistoryRepairsRemainingFilesWhenHistoryMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "msh")
	if err := ensureHistoryDir(dir); err != nil { t.Fatal(err) }
	path := filepath.Join(dir, "msh_commands")
	if err := os.WriteFile(path, []byte("secret\n"), 0644); err != nil { t.Fatal(err) }
	if _, err := ReadHistory(dir); !os.IsNotExist(err) { t.Fatalf("want missing history: %v", err) }
	assertHistoryMode(t, path, 0600)
}

func TestHistoryPermissionErrorsReported(t *testing.T) {
	if os.Geteuid() == 0 { t.Skip("root bypasses Unix access checks") }
	parent := t.TempDir()
	t.Setenv("XDG_DATA_HOME", parent)
	dir := filepath.Join(parent, "msh")
	if err := ensureHistoryDir(dir); err != nil { t.Fatal(err) }
	path := filepath.Join(dir, "msh_commands")
	if err := os.WriteFile(path, []byte("secret\n"), 0000); err != nil { t.Fatal(err) }
	defer os.Chmod(path, 0600)
	if _, err := ReadHistory(dir); !os.IsPermission(err) {
		t.Fatalf("permission error not returned: %v", err)
	}
	oldPending := historyToSave
	defer func() { historyToSave = oldPending }()
	historyToSave = []HistoryItem{{Command: "pending", Directory: "/"}}
	stderr, err := os.CreateTemp(parent, "stderr")
	if err != nil { t.Fatal(err) }
	defer stderr.Close()
	originalStderr := os.Stderr
	func() {
		os.Stderr = stderr
		defer func() { os.Stderr = originalStderr }()
		(&TermState{}).saveHistory()
	}()
	data, err := os.ReadFile(stderr.Name())
	if err != nil || len(data) == 0 { t.Fatal("save permission error not reported") }
	if len(historyToSave) != 1 { t.Fatal("failed save discarded pending history") }
	assertHistoryMode(t, path, 0000)
}
