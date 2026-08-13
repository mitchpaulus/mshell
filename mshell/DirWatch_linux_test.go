//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForEvent reads events until one matches want ("" accepts any) or the
// timeout expires.
func waitForEvent(t *testing.T, w *dirWatcher, want string) bool {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case name, ok := <-w.Events():
			if !ok {
				t.Fatal("event channel closed unexpectedly")
			}
			if want == "" || name == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestWatchDirectoryRename(t *testing.T) {
	dir := t.TempDir()
	w, err := watchDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Mimic the atomic clipboard write: temp file, then rename into place.
	tmp := filepath.Join(dir, "fm_clipboard.tmp-1")
	if err := os.WriteFile(tmp, []byte("cut\t/a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "fm_clipboard")
	if err := os.Rename(tmp, final); err != nil {
		t.Fatal(err)
	}

	if !waitForEvent(t, w, "fm_clipboard") {
		t.Fatal("no event for renamed clipboard file")
	}

	// Deletion is also observed.
	if err := os.Remove(final); err != nil {
		t.Fatal(err)
	}
	if !waitForEvent(t, w, "fm_clipboard") {
		t.Fatal("no event for deleted clipboard file")
	}
}

func TestWatchDirectoryClose(t *testing.T) {
	dir := t.TempDir()
	w, err := watchDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-w.Events():
		if ok {
			// A buffered event from setup is fine; the channel must still
			// close afterwards.
			if _, ok := <-w.Events(); ok {
				t.Fatal("event channel should close after Close")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event channel did not close after Close")
	}
}
