package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

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

func TestReadModalKeyUsesSharedInputChannel(t *testing.T) {
	previewPath := filepath.Join(t.TempDir(), "example.txt")

	fm := &FileManager{
		inputChan:      make(chan inputEvent),
		previewChan:    make(chan previewResult, 1),
		quitChan:       make(chan struct{}, 1),
		previewCache:   make(map[string][]string),
		previewLoading: previewPath,
	}

	fm.previewChan <- previewResult{
		path:  previewPath,
		lines: []string{" preview"},
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		fm.inputChan <- inputEvent{
			buf: []byte{'o'},
			n:   1,
		}
	}()

	key, ok := fm.readModalKey()
	if !ok {
		t.Fatal("readModalKey() returned ok=false")
	}
	if key != 'o' {
		t.Fatalf("readModalKey() = %q, want %q", key, 'o')
	}
	if fm.modalActive.Load() {
		t.Fatal("readModalKey() left modalActive enabled")
	}

	wantPreview := []string{" preview"}
	gotPreview := fm.previewCache[previewPath]
	if !reflect.DeepEqual(gotPreview, wantPreview) {
		t.Fatalf("preview cache = %v, want %v", gotPreview, wantPreview)
	}
	if fm.previewLoading != "" {
		t.Fatalf("previewLoading = %q, want empty string", fm.previewLoading)
	}
}
