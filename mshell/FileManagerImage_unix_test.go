//go:build linux || darwin

package main

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPreviewSkipsNamedPipes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"pipe.png", "pipe.txt"} {
		path := filepath.Join(dir, name)
		if err := unix.Mkfifo(path, 0o644); err != nil {
			t.Fatal(err)
		}
		done := make(chan []string, 1)
		go func() {
			if name == "pipe.png" {
				lines, _ := previewImage(path, 500, 500, 1)
				done <- lines
			} else {
				done <- computePreview(testDirEntry{name: name}, path, 10, false)
			}
		}()
		select {
		case lines := <-done:
			if len(lines) != 1 || lines[0] != " (not a regular file)" {
				t.Errorf("%s: lines = %q", name, lines)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: preview blocked on a named pipe", name)
		}
	}
}
