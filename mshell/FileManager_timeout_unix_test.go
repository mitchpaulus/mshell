//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestComputePreviewWithTimeoutFiresOnSlowRead verifies that a preview which
// blocks on opening a file (here a FIFO with no writer, standing in for a
// cloud file stuck hydrating) gives up after the timeout instead of hanging.
func TestComputePreviewWithTimeoutFiresOnSlowRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(path, 0644); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}

	start := time.Now()
	lines := computePreviewWithTimeout(testDirEntry{name: "pipe"}, path, 10, 100*time.Millisecond, nil)
	elapsed := time.Since(start)

	if len(lines) == 0 || !strings.Contains(lines[0], "timed out") {
		t.Fatalf("preview = %v, want a timeout placeholder", lines)
	}
	if elapsed > time.Second {
		t.Fatalf("timeout took %v, expected to fire near 100ms", elapsed)
	}

	// Release the goroutine still blocked opening the FIFO for reading so it
	// does not linger past the test.
	if w, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		w.Close()
	}
}
