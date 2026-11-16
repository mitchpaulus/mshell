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

func TestSanitizeNameComponent(t *testing.T) {
	cases := map[string]string{
		"plain.txt":   "plain.txt",
		"a:b?.txt":    "a_b_.txt",
		`<>:"|?*`:     "_______",
		"tab\there":   "tab_here",
		"trailing. .": "trailing___",
		"NUL":         "_NUL",
		"nul.txt":     "_nul.txt",
		"com1.tar.gz": "_com1.tar.gz",
		"COM10":       "COM10",
		"null":        "null",
		"..":          "..",
		".hidden":     ".hidden",
		"ünï:code":    "ünï_code",
		"caf\xe9.txt":  "caf_.txt",
		"\xff\xfe":     "__",
		"bad\xc3":      "bad_",
		"real\uFFFD":   "real\uFFFD",
	}
	for in, want := range cases {
		if got := sanitizeNameComponent(in); got != want {
			t.Errorf("sanitizeNameComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func addZipFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

func TestZipExtractSanitize(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "s.zip")
	writeZipArchive(t, archive, func(zw *zip.Writer) {
		addZipFile(t, zw, "dir:1/a?b.txt", "one")
		addZipFile(t, zw, "dir:1/aux.log", "two")
		addZipFile(t, zw, "plain.txt", "three")
	})

	dest := filepath.Join(dir, "dest")
	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{preservePermissions: true, sanitize: true}}
	if err := extractZipArchive(archive, dest, opts); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for name, want := range map[string]string{"dir_1/a_b.txt": "one", "dir_1/_aux.log": "two", "plain.txt": "three"} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, want)
		}
	}

	// A file already on disk under the sanitized name follows the usual rules.
	err := extractZipArchive(archive, dest, opts)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' on second extraction, got %v", err)
	}
}

// Two entries that sanitize to one name fail before anything is written, even
// with overwrite set.
func TestZipExtractSanitizeCollision(t *testing.T) {
	collisions := [][2]string{
		{"a:b.txt", "a?b.txt"},
		{"a:b.txt", "a_b.txt"},
		{"d:/x.txt", "d_/y.txt"},
		{"Readme", "README"},
		{"caf\xe9", "caf\xe8"},
	}
	for _, pair := range collisions {
		dir := t.TempDir()
		archive := filepath.Join(dir, "c.zip")
		writeZipArchive(t, archive, func(zw *zip.Writer) {
			addZipFile(t, zw, "first.txt", "first")
			addZipFile(t, zw, pair[0], "1")
			addZipFile(t, zw, pair[1], "2")
		})
		dest := filepath.Join(dir, "dest")
		opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true, sanitize: true}}
		err := extractZipArchive(archive, dest, opts)
		if err == nil || !strings.Contains(err.Error(), "would both be extracted") {
			t.Errorf("%v: expected a collision error, got %v", pair, err)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 0 {
			t.Errorf("%v: expected nothing written, found %d entries", pair, len(entries))
		}
	}
}
