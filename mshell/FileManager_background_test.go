package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newCloudTestFileManager returns a file manager showing one folder in a
// cloud sync folder, rendering to a temporary file.
func newCloudTestFileManager(t *testing.T) *FileManager {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Data"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(t.TempDir(), "screen"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	fm := &FileManager{rows: 10, cols: 80, currentDir: dir, ttyOut: out}
	fm.loadDirectory()
	fm.inCloudSyncRoot = true
	return fm
}

func withStorageProviderLookup(t *testing.T, lookup func(string, []string, func() bool, func(string, uint32)), closed chan struct{}) {
	t.Helper()
	saved := storageProviderStateLookupFactory
	storageProviderStateLookupFactory = func() (func(string, []string, func() bool, func(string, uint32)), func()) {
		return lookup, func() { close(closed) }
	}
	t.Cleanup(func() { storageProviderStateLookupFactory = saved })
}

func screenSize(t *testing.T, fm *FileManager) int64 {
	t.Helper()
	info, err := fm.ttyOut.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// A folder status lookup that hangs must not block leaving the file manager,
// and must not draw on the terminal when it finally returns.
func TestFolderStateWorkerHungLookupDoesNotBlockShutdown(t *testing.T) {
	fm := newCloudTestFileManager(t)
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan struct{})
	closed := make(chan struct{})
	withStorageProviderLookup(t, func(dir string, names []string, stop func() bool, found func(string, uint32)) {
		close(started)
		<-release
		found(names[0], 1)
		close(delivered)
	}, closed)

	fm.startPreviewLoop()
	fm.startFolderStateWorker()
	fm.renderMu.Lock()
	fm.scheduleFolderStates()
	fm.renderMu.Unlock()
	<-started

	stopped := make(chan struct{})
	go func() {
		fm.stopPreviewLoop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopPreviewLoop waited for a hung folder status lookup")
	}

	before := screenSize(t, fm)
	close(release)
	<-delivered
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not release COM after the hung lookup returned")
	}
	if after := screenSize(t, fm); after != before {
		t.Fatalf("worker drew %d bytes after shutdown", after-before)
	}
	if len(fm.folderCloudStates) != 0 {
		t.Fatalf("worker recorded a state after shutdown: %v", fm.folderCloudStates)
	}
}

// A panic in the folder status lookup turns folder markers off instead of
// crashing, and still releases COM.
func TestFolderStateWorkerRecoversFromPanic(t *testing.T) {
	fm := newCloudTestFileManager(t)
	closed := make(chan struct{})
	withStorageProviderLookup(t, func(string, []string, func() bool, func(string, uint32)) {
		panic("lookup failed")
	}, closed)

	fm.startPreviewLoop()
	defer fm.stopPreviewLoop()
	fm.startFolderStateWorker()
	fm.renderMu.Lock()
	fm.scheduleFolderStates()
	fm.renderMu.Unlock()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not release COM after a panic")
	}
	// Scheduling again must not block even though the worker is gone.
	fm.renderMu.Lock()
	fm.folderStatesRequested = false
	fm.scheduleFolderStates()
	fm.folderStatesRequested = false
	fm.scheduleFolderStates()
	fm.renderMu.Unlock()
}

// Without a lookup (not Windows, or COM unavailable), scheduling is harmless.
func TestFolderStateWorkerWithoutLookup(t *testing.T) {
	fm := newCloudTestFileManager(t)
	saved := storageProviderStateLookupFactory
	storageProviderStateLookupFactory = func() (func(string, []string, func() bool, func(string, uint32)), func()) {
		return nil, nil
	}
	defer func() { storageProviderStateLookupFactory = saved }()

	fm.startPreviewLoop()
	defer fm.stopPreviewLoop()
	fm.startFolderStateWorker()
	for i := 0; i < 3; i++ {
		fm.renderMu.Lock()
		fm.folderStatesRequested = false
		fm.scheduleFolderStates()
		fm.renderMu.Unlock()
	}
}
