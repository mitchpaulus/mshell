//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestTarPackRejectsFifoWithoutHanging verifies that packing a directory
// containing a FIFO fails promptly with an "unsupported file type" error
// instead of blocking forever on os.Open of the pipe (which has no writer).
func TestTarPackRejectsFifoWithoutHanging(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}

	archive := filepath.Join(dir, "out.tar")
	done := make(chan error, 1)
	go func() {
		done <- buildTarFromEntries([]zipPackItem{{SourcePath: src, PreserveRoot: true}}, archive)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error packing a directory containing a FIFO")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("buildTarFromEntries hung on a FIFO source entry")
	}
}
