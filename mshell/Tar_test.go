package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTarArchive builds a tar (optionally gzip-compressed) at path using the
// supplied callback to add entries.
func writeTarArchive(t *testing.T, path string, gzipped bool, add func(tw *tar.Writer)) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()

	var tw *tar.Writer
	if gzipped {
		gz := gzip.NewWriter(f)
		tw = tar.NewWriter(gz)
		add(tw)
		if err := tw.Close(); err != nil {
			t.Fatalf("close tar: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}
		return
	}
	tw = tar.NewWriter(f)
	add(tw)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
}

func addTarFile(t *testing.T, tw *tar.Writer, name, content string) {
	t.Helper()
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header %s: %v", name, err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("write body %s: %v", name, err)
	}
}

func addTarSymlink(t *testing.T, tw *tar.Writer, name, target string) {
	t.Helper()
	hdr := &tar.Header{Name: name, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: target}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write symlink header %s: %v", name, err)
	}
}

func extractAll(tarPath, dest string) error {
	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true}}
	return extractTarArchive(tarPath, dest, opts)
}

func TestTarRoundTripAndGzipAutodetect(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"out.tar", "out.tar.gz"} {
		archive := filepath.Join(dir, name)
		if err := buildTarFromEntries([]zipPackItem{{SourcePath: src, PreserveRoot: true}}, archive, isGzipTarget(archive)); err != nil {
			t.Fatalf("%s pack: %v", name, err)
		}
		entries, err := collectTarMetadata(archive)
		if err != nil {
			t.Fatalf("%s list: %v", name, err)
		}
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Name] = true
		}
		for _, want := range []string{"src/a.txt", "src/sub/b.txt"} {
			if !found[want] {
				t.Errorf("%s: missing entry %s (got %v)", name, want, found)
			}
		}
		data, ok, err := readTarEntry(archive, "src/a.txt")
		if err != nil || !ok || string(data) != "hello" {
			t.Errorf("%s: readTarEntry a.txt = %q ok=%v err=%v", name, data, ok, err)
		}
	}

	// Auto-detect: a gzip tarball with a non-gzip name must still be read via
	// magic-byte sniffing.
	mystery := filepath.Join(dir, "mystery")
	if err := os.Rename(filepath.Join(dir, "out.tar.gz"), mystery); err != nil {
		t.Fatal(err)
	}
	if _, err := collectTarMetadata(mystery); err != nil {
		t.Errorf("auto-detect list of renamed gzip failed: %v", err)
	}
}

func TestTarExtractRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "trav.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "../escape.txt", "pwned")
	})
	dest := filepath.Join(dir, "dest")
	if err := extractAll(archive, dest); err == nil {
		t.Fatal("expected path-traversal extraction to be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("path traversal escaped the destination directory")
	}
}

func TestTarExtractRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	archive := filepath.Join(dir, "sym.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarSymlink(t, tw, "bad", outside)
	})
	dest := filepath.Join(dir, "dest")
	if err := extractAll(archive, dest); err == nil {
		t.Fatal("expected escaping symlink extraction to be refused")
	}
}

// TestTarExtractRejectsSymlinkTargets covers both flavors of an escaping
// symlink target: an absolute path and a relative "../" path.
func TestTarExtractRejectsSymlinkTargets(t *testing.T) {
	cases := map[string]string{
		"absolute": "/etc/passwd",
		"relative": "../../../../etc/passwd",
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "s.tar")
			writeTarArchive(t, archive, false, func(tw *tar.Writer) {
				addTarSymlink(t, tw, "link", target)
			})
			if err := extractAll(archive, filepath.Join(dir, "dest")); err == nil {
				t.Fatalf("expected symlink with escaping target %q to be refused", target)
			}
		})
	}
}

// TestRejectNULInPath verifies the NUL-byte guard directly. (Go's own
// archive/tar refuses to encode a NUL in a name, so such an entry cannot be
// produced through its writer; the guard defends against a hand-crafted PAX
// record whose value survives decoding.)
func TestRejectNULInPath(t *testing.T) {
	if err := rejectNULInPath("ok/name.txt", "also/fine"); err != nil {
		t.Fatalf("clean paths should pass: %v", err)
	}
	if err := rejectNULInPath("bad\x00name.txt"); err == nil {
		t.Fatal("expected embedded NUL in name to be rejected")
	}
	if err := rejectNULInPath("fine", "link\x00target"); err == nil {
		t.Fatal("expected embedded NUL in link target to be rejected")
	}
}

func TestTarExtractRejectsHardlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "hl.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		hdr := &tar.Header{Name: "hl", Typeflag: tar.TypeLink, Linkname: "/etc/passwd", Mode: 0o644}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	})
	dest := filepath.Join(dir, "dest")
	if err := extractAll(archive, dest); err == nil {
		t.Fatal("expected hardlink entry to be rejected")
	}
}

func TestTarExtractRejectsWriteThroughPreexistingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing symlink inside the destination that points outside it.
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	archive := filepath.Join(dir, "wt.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "link/pwned.txt", "pwned")
	})
	if err := extractAll(archive, dest); err == nil {
		t.Fatal("expected write-through pre-existing symlink to be refused")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Fatal("write escaped through pre-existing symlink")
	}
}

func TestTarExtractAllowsInDestSymlink(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(filepath.Join(dest, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink that stays within the destination must remain usable.
	if err := os.Symlink("realdir", filepath.Join(dest, "s")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	archive := filepath.Join(dir, "ok.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "s/f.txt", "ok")
	})
	if err := extractAll(archive, dest); err != nil {
		t.Fatalf("in-dest symlink write should be allowed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "realdir", "f.txt"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("expected file written through in-dest symlink, got %q err=%v", got, err)
	}
}

func TestTarExtractStripComponentsAndPattern(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "s.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "top/keep.txt", "k")
		addTarFile(t, tw, "top/skip.log", "s")
	})

	dest := filepath.Join(dir, "dest")
	opts := zipExtractOptions{
		zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true},
		stripComponents: 1,
		pattern:         "top/*.txt",
	}
	if err := extractTarArchive(archive, dest, opts); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Errorf("expected keep.txt after strip+pattern: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skip.log")); !strings.Contains(errString(err), "no such") && err == nil {
		t.Errorf("skip.log should have been filtered out by pattern")
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestTarExtractOverwriteDoesNotFollowFinalSymlink checks that extracting with
// overwrite over a destination name that is already a symlink to an outside
// file replaces the symlink with a fresh file rather than writing through it.
func TestTarExtractOverwriteDoesNotFollowFinalSymlink(t *testing.T) {
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

	archive := filepath.Join(dir, "v.tar")
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "victim", "REPLACED")
	})
	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true}}
	if err := extractTarArchive(archive, dest, opts); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got, _ := os.ReadFile(outside); string(got) != "ORIGINAL" {
		t.Fatalf("wrote THROUGH the symlink: outside file is now %q", got)
	}
	fi, err := os.Lstat(filepath.Join(dest, "victim"))
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dest/victim should be a fresh regular file, got mode %v err %v", fi.Mode(), err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "victim")); string(got) != "REPLACED" {
		t.Fatalf("dest/victim content = %q, want REPLACED", got)
	}
}

func TestTarExtractMaxBytesCapsBomb(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "big.tar")
	big := strings.Repeat("A", 5_000_000)
	writeTarArchive(t, archive, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "big.bin", big)
	})
	dest := filepath.Join(dir, "dest")

	opts := zipExtractOptions{zipWriteOptions: zipWriteOptions{overwrite: true, preservePermissions: true, maxBytes: 1_000_000}}
	if err := extractTarArchive(archive, dest, opts); err == nil {
		t.Fatal("expected extraction to exceed maxBytes and fail")
	}

	// Under the cap it must succeed.
	small := filepath.Join(dir, "small.tar")
	writeTarArchive(t, small, false, func(tw *tar.Writer) {
		addTarFile(t, tw, "ok.txt", "hello")
	})
	if err := extractTarArchive(small, filepath.Join(dir, "d2"), opts); err != nil {
		t.Fatalf("small archive under cap should succeed: %v", err)
	}
}
