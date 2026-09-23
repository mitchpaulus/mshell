package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"reflect"
)

type testDirEntry struct {
	name  string
	isDir bool
}

func (e testDirEntry) Name() string               { return e.name }
func (e testDirEntry) IsDir() bool                { return e.isDir }
func (e testDirEntry) Type() fs.FileMode          { return 0 }
func (e testDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func TestAppendUniquePathAppendsWhenMissing(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b"}
	got := appendUniquePath(paths, "/tmp/c")
	want := []string{"/tmp/a", "/tmp/b", "/tmp/c"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendUniquePath() = %v, want %v", got, want)
	}
}

func TestAppendUniquePathSkipsDuplicate(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b"}
	got := appendUniquePath(paths, "/tmp/a")
	want := []string{"/tmp/a", "/tmp/b"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendUniquePath() = %v, want %v", got, want)
	}
}

func TestRemovePathRemovesExisting(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b", "/tmp/c"}
	got := removePath(paths, "/tmp/b")
	want := []string{"/tmp/a", "/tmp/c"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("removePath() = %v, want %v", got, want)
	}
}

func TestRemovePathNoOpWhenMissing(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b"}
	got := removePath(paths, "/tmp/c")
	want := []string{"/tmp/a", "/tmp/b"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("removePath() = %v, want %v", got, want)
	}
}

func TestHandleInputProcessesBufferedQuit(t *testing.T) {
	fm := &FileManager{
		entries: []os.DirEntry{
			testDirEntry{name: "a"},
			testDirEntry{name: "b"},
			testDirEntry{name: "c"},
		},
	}

	quit := fm.handleInput([]byte("jq"), 2)

	if !quit {
		t.Fatal("expected buffered q to quit")
	}
	if fm.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", fm.cursor)
	}
}

func TestComputePreviewReturnsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lines := computePreview(testDirEntry{name: "note.txt"}, path, 10, false)

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "hello" {
		t.Fatalf("preview = %v, want first line 'hello'", lines)
	}
}

func TestComputePreviewSkipsCloudOnlyFile(t *testing.T) {
	// The path does not exist, so any attempt to read it would report an error
	// instead of the cloud only placeholder.
	path := filepath.Join(t.TempDir(), "missing.txt")

	lines := computePreview(testDirEntry{name: "missing.txt"}, path, 10, true)

	if len(lines) != 1 || !strings.Contains(lines[0], "cloud only") {
		t.Fatalf("preview = %v, want cloud only placeholder", lines)
	}
}

func TestCloudFileStateFromAttributes(t *testing.T) {
	const archive = 0x20
	const directory = 0x10
	tests := []struct {
		name       string
		attrs      uint32
		isDir      bool
		inSyncRoot bool
		want       cloudFileState
	}{
		{"plain file outside sync root", archive, false, false, cloudFileNotManaged},
		{"downloaded file in sync root", archive, false, true, cloudFileLocal},
		{"cloud only file", archive | fileAttributeRecallOnDataAccess, false, true, cloudFileOnlineOnly},
		{"legacy offline file", archive | fileAttributeOffline, false, true, cloudFileOnlineOnly},
		{"pinned file", archive | fileAttributePinned, false, true, cloudFilePinned},
		{"pinned file still downloading", archive | fileAttributePinned | fileAttributeRecallOnDataAccess, false, true, cloudFileOnlineOnly},
		{"folder in sync root", directory, true, true, cloudFileNotManaged},
		{"pinned folder", directory | fileAttributePinned, true, true, cloudFilePinned},
	}
	for _, tt := range tests {
		if got := cloudFileStateFromAttributes(tt.attrs, tt.isDir, tt.inSyncRoot); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCloudFileStateFromStorageProvider(t *testing.T) {
	tests := map[uint32]cloudFileState{
		0:  cloudFileNotManaged,
		1:  cloudFileOnlineOnly,
		2:  cloudFileLocal,
		3:  cloudFilePinned,
		4:  cloudFileSyncing,
		5:  cloudFileSyncing,
		6:  cloudFileSyncing,
		7:  cloudFileError,
		8:  cloudFileError,
		9:  cloudFileNotManaged,
		10: cloudFileSyncing,
	}
	for value, want := range tests {
		if got := cloudFileStateFromStorageProvider(value); got != want {
			t.Errorf("state %d: got %v, want %v", value, got, want)
		}
	}
}

func TestLeftPaneWidthIncludesCloudMarker(t *testing.T) {
	fm := &FileManager{rows: 20, cols: 200, currentDir: t.TempDir()}
	fm.entries = []os.DirEntry{testDirEntry{name: "1 Project", isDir: true}}
	plain := fm.leftPaneWidth()
	fm.inCloudSyncRoot = true
	if got := fm.leftPaneWidth(); got != plain+cloudMarkerCols {
		t.Fatalf("leftPaneWidth() = %d, want %d", got, plain+cloudMarkerCols)
	}
}

func TestEnterSelectedWindowsVolumeSwitchesCurrentDirectory(t *testing.T) {
	fm := &FileManager{
		showingWindowsVolumes: true,
		entries: []os.DirEntry{
			fileManagerVolumeEntry{name: "C:"},
			fileManagerVolumeEntry{name: "F:"},
		},
		cursor: 1,
	}

	fm.enterSelected()

	if fm.currentDir != `F:\` {
		t.Fatalf("currentDir = %q, want %q", fm.currentDir, `F:\`)
	}
	if fm.showingWindowsVolumes {
		t.Fatal("expected volume list to close after selecting a volume")
	}
}
