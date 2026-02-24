package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCdhSelectionToIndex(t *testing.T) {
	tests := []struct {
		name      string
		selection string
		maxIndex  int
		want      int
		ok        bool
	}{
		{name: "number", selection: "2", maxIndex: 3, want: 2, ok: true},
		{name: "lower letter", selection: "a", maxIndex: 3, want: 1, ok: true},
		{name: "upper letter", selection: "B", maxIndex: 3, want: 2, ok: true},
		{name: "out of range number", selection: "4", maxIndex: 3, ok: false},
		{name: "out of range letter", selection: "d", maxIndex: 3, ok: false},
		{name: "empty", selection: "", maxIndex: 3, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cdhSelectionToIndex(tc.selection, tc.maxIndex)
			if ok != tc.ok {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("index mismatch: got %d want %d", got, tc.want)
			}
		})
	}
}

func TestAddPreviousDirectoryDedupesAndMovesToEnd(t *testing.T) {
	state := &EvalState{PreviousDirectories: []string{"/a", "/b", "/c"}}
	state.AddPreviousDirectory("/b")

	got := strings.Join(state.PreviousDirectories, ",")
	want := "/a,/c,/b"
	if got != want {
		t.Fatalf("history mismatch: got %s want %s", got, want)
	}
}

func TestRunCdhChangesToSelectedDirectory(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
	})

	baseDir := t.TempDir()
	dir1 := filepath.Join(baseDir, "dir1")
	dir2 := filepath.Join(baseDir, "dir2")
	currentDir := filepath.Join(baseDir, "current")
	for _, d := range []string{dir1, dir2, currentDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir failed for %s: %v", d, err)
		}
	}

	if err := os.Chdir(currentDir); err != nil {
		t.Fatalf("chdir failed for %s: %v", currentDir, err)
	}

	state := &EvalState{PreviousDirectories: []string{dir1, dir2}}
	var output bytes.Buffer
	context := ExecuteContext{
		StandardInput:  strings.NewReader("a\n"),
		StandardOutput: &output,
	}

	result, err := state.RunCdh(context)
	if err != nil {
		t.Fatalf("RunCdh returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("RunCdh returned failure")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed after cdh: %v", err)
	}
	if cwd != dir2 {
		t.Fatalf("cwd mismatch: got %s want %s", cwd, dir2)
	}

	menuOutput := output.String()
	if !strings.Contains(menuOutput, " b  2)") || !strings.Contains(menuOutput, " a  1)") {
		t.Fatalf("menu output missing expected entries: %q", menuOutput)
	}
}

func TestRunCdhCtrlCCancelsPrompt(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
	})

	baseDir := t.TempDir()
	currentDir := filepath.Join(baseDir, "current")
	previousDir := filepath.Join(baseDir, "previous")
	for _, d := range []string{currentDir, previousDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir failed for %s: %v", d, err)
		}
	}

	if err := os.Chdir(currentDir); err != nil {
		t.Fatalf("chdir failed for %s: %v", currentDir, err)
	}

	state := &EvalState{PreviousDirectories: []string{previousDir}}
	var output bytes.Buffer
	context := ExecuteContext{
		StandardInput:  strings.NewReader("\x03"),
		StandardOutput: &output,
	}

	result, err := state.RunCdh(context)
	if err != nil {
		t.Fatalf("RunCdh returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("RunCdh should succeed when cancelled")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed after cdh cancel: %v", err)
	}
	if cwd != currentDir {
		t.Fatalf("cwd should be unchanged on cancel: got %s want %s", cwd, currentDir)
	}
}

func TestRunCdhExcludesCurrentDirectoryFromMenu(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
	})

	baseDir := t.TempDir()
	currentDir := filepath.Join(baseDir, "current")
	previousDir := filepath.Join(baseDir, "previous")
	for _, d := range []string{currentDir, previousDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir failed for %s: %v", d, err)
		}
	}

	if err := os.Chdir(currentDir); err != nil {
		t.Fatalf("chdir failed for %s: %v", currentDir, err)
	}

	state := &EvalState{PreviousDirectories: []string{currentDir, previousDir}}
	var output bytes.Buffer
	context := ExecuteContext{
		StandardInput:  strings.NewReader("a\n"),
		StandardOutput: &output,
	}

	result, err := state.RunCdh(context)
	if err != nil {
		t.Fatalf("RunCdh returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("RunCdh returned failure")
	}

	menuOutput := output.String()
	if strings.Contains(menuOutput, currentDir) {
		t.Fatalf("menu output should not include current directory: %q", menuOutput)
	}
	if !strings.Contains(menuOutput, previousDir) {
		t.Fatalf("menu output should include previous directory: %q", menuOutput)
	}
}

func TestRunCdpChangesToMostRecentPreviousDirectory(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
	})

	baseDir := t.TempDir()
	currentDir := filepath.Join(baseDir, "current")
	previousDir := filepath.Join(baseDir, "previous")
	for _, d := range []string{currentDir, previousDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir failed for %s: %v", d, err)
		}
	}

	if err := os.Chdir(currentDir); err != nil {
		t.Fatalf("chdir failed for %s: %v", currentDir, err)
	}

	state := &EvalState{PreviousDirectories: []string{previousDir}}
	result, err := state.RunCdp()
	if err != nil {
		t.Fatalf("RunCdp returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("RunCdp returned failure")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed after cdp: %v", err)
	}
	if cwd != previousDir {
		t.Fatalf("cwd mismatch: got %s want %s", cwd, previousDir)
	}
}

func TestRunCdpFailsWithoutHistory(t *testing.T) {
	state := &EvalState{}
	result, err := state.RunCdp()
	if err != nil {
		t.Fatalf("expected no error when history is empty, got: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success when history is empty")
	}
}
