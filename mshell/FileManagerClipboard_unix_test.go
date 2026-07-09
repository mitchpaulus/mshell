//go:build !windows

package main

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestClipboardFifoDoesNotHang plants a FIFO at the clipboard path — which
// would block a naive open(2) forever waiting for a writer — and checks that
// a load returns promptly and empty, and that the next save heals the path by
// renaming a regular file over it.
func TestClipboardFifoDoesNotHang(t *testing.T) {
	useTempHistoryDir(t)
	path, err := clipboardFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0644); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		cutPaths, copyPaths := loadClipboard()
		if len(cutPaths)+len(copyPaths) != 0 {
			t.Errorf("FIFO should read as empty clipboard, got cut %v copy %v", cutPaths, copyPaths)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("loadClipboard hung on a FIFO at the clipboard path")
	}

	// The next mutation atomically renames a regular file over the FIFO.
	if err := addClipboardPath("cut", "/heal"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("clipboard path not healed to a regular file, mode %v", info.Mode())
	}
}
