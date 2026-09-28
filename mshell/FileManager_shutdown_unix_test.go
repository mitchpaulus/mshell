//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A preview stuck reading a file (a hung network drive, say) must not block
// leaving the file manager. Previews skip named pipes and devices, so the
// stuck read is simulated.
func TestStopPreviewLoopDoesNotWaitForBlockedPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.txt")
	if err := os.WriteFile(path, []byte("text"), 0644); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	original := makePreview
	makePreview = func(req previewRequest) ([]string, imagePreview) {
		close(started)
		<-release
		return nil, imagePreview{}
	}
	defer func() { makePreview = original }()
	// Release the blocked preview so the worker can exit.
	defer close(release)

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
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("preview never started")
	}

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
}
