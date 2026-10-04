package main

import (
	"os"
	"testing"
)

// psub with a bad input must fail without creating a temp file.
func TestPsubBadInputLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)

	state := EvalState{CallStack: make([]CallStackItem, 0, 10)}
	if result := evalForTest(t, &state, `5 psub`); result.Success {
		t.Fatalf("expected 'psub' on an int to fail")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no temp files, found %d", len(entries))
	}
}
