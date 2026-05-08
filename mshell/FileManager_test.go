package main

import (
	"io/fs"
	"os"
	"reflect"
	"testing"
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
