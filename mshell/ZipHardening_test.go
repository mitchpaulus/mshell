package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZipArchive builds a zip at path using the supplied callback.
func writeZipArchive(t *testing.T, path string, add func(zw *zip.Writer)) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	add(zw)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

// TestZipExtractRejectsWriteThroughPreexistingSymlink verifies the shared
// ensureRealParentWithinBase guard now protects the zip extractor too.
func TestZipExtractRejectsWriteThroughPreexistingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	archive := filepath.Join(dir, "wt.zip")
	writeZipArchive(t, archive, func(zw *zip.Writer) {
		w, err := zw.Create("link/pwned.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("pwned")); err != nil {
			t.Fatal(err)
		}
	})

	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true}}
	if err := extractZipArchive(archive, dest, opts); err == nil {
		t.Fatal("expected write-through pre-existing symlink to be refused")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Fatal("zip write escaped through pre-existing symlink")
	}
}

// TestZipExtractOverwriteDoesNotFollowFinalSymlink is the zip analog of the
// tar test: overwriting a destination name that is a symlink must not write
// through it.
func TestZipExtractOverwriteDoesNotFollowFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "victim")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	archive := filepath.Join(dir, "v.zip")
	writeZipArchive(t, archive, func(zw *zip.Writer) {
		w, err := zw.Create("victim")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("REPLACED")); err != nil {
			t.Fatal(err)
		}
	})
	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true}}
	if err := extractZipArchive(archive, dest, opts); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got, _ := os.ReadFile(outside); string(got) != "ORIGINAL" {
		t.Fatalf("zip wrote THROUGH the symlink: outside file is now %q", got)
	}
}

func TestZipExtractMaxBytesCapsBomb(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "big.zip")
	writeZipArchive(t, archive, func(zw *zip.Writer) {
		w, err := zw.Create("big.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(strings.Repeat("A", 5_000_000))); err != nil {
			t.Fatal(err)
		}
	})

	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true, maxBytes: 1_000_000}}
	if err := extractZipArchive(archive, filepath.Join(dir, "dest"), opts); err == nil {
		t.Fatal("expected zip extraction to exceed maxBytes and fail")
	}
}
