//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A preview stuck reading a file (here a FIFO with no writer, standing in for
// a hung network drive) must not block leaving the file manager.
func TestStopPreviewLoopDoesNotWaitForBlockedPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(path, 0644); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}

	fm := &FileManager{rows: 10, cols: 80, currentDir: dir}
	out, err := os.Create(filepath.Join(t.TempDir(), "screen"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	fm.ttyOut = out
	fm.loadDirectory()
	fm.startPreviewLoop()
	fm.renderMu.Lock()
	fm.schedulePreview()
	fm.renderMu.Unlock()
	time.Sleep(50 * time.Millisecond) // let the worker start the blocking open

	stopped := make(chan struct{})
	go func() {
		fm.stopPreviewLoop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopPreviewLoop waited for a blocked preview")
	}

	// Release the blocked open so the worker can exit.
	if w, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		w.Close()
	}
}
