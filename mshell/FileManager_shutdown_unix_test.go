//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// When the default opener finds no program for a file, the file manager
// must say so. The opener is also told not to fall back on text browsers,
// which show nothing without a terminal and exit with success.
func TestOpenFileUnixReportsOpenerFailure(t *testing.T) {
	binDir := t.TempDir()
	browserFile := filepath.Join(t.TempDir(), "browser")
	script := "#!/bin/sh\nprintf '%s' \"$BROWSER\" > '" + browserFile + "'\necho 'no method available' >&2\nexit 3\n"
	for _, name := range []string{"xdg-open", "open"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mimeScript := "#!/bin/sh\necho 'application/pdf; charset=binary'\n"
	if err := os.WriteFile(filepath.Join(binDir, "xdg-mime"), []byte(mimeScript), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EDITOR", "")
	t.Setenv("BROWSER", "")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.pdf"), []byte("%PDF-1.4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(t.TempDir(), "screen"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	fm := &FileManager{rows: 10, cols: 80, currentDir: dir, ttyOut: out}
	fm.loadDirectory()

	fm.renderMu.Lock()
	fm.enterSelected()
	fm.renderMu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	status := ""
	for status == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		fm.renderMu.Lock()
		status = fm.statusMsg
		fm.renderMu.Unlock()
	}
	if status == "" {
		t.Fatal("statusMsg is empty, want the reason the file could not be opened")
	}
	if runtime.GOOS == "linux" {
		if want := "No program opens application/pdf. Set one: xdg-mime default APP.desktop application/pdf"; status != want {
			t.Fatalf("statusMsg = %q, want %q", status, want)
		}
		browser, err := os.ReadFile(browserFile)
		if err != nil {
			t.Fatal(err)
		}
		if string(browser) != xdgOpenGraphicalBrowsers {
			t.Fatalf("BROWSER = %q, want %q", browser, xdgOpenGraphicalBrowsers)
		}
	}
}

// An opener that is not installed must be reported too.
func TestOpenFileUnixReportsMissingOpener(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("EDITOR", "")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.pdf"), []byte("%PDF-1.4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fm := &FileManager{rows: 10, cols: 80, currentDir: dir}
	fm.loadDirectory()
	fm.enterSelected()

	if !strings.Contains(fm.statusMsg, "was not found on PATH") {
		t.Fatalf("statusMsg = %q, want it to say the opener was not found on PATH", fm.statusMsg)
	}
}
