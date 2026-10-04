//go:build linux || darwin

package main

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

// Lookup reads PATH one name at a time; it must agree with the full scan
// that completion and binPaths use, and msh_bins.txt wins over both.
func TestLookupAgreesWithScan(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "msh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "msh", binMapFileName), []byte("mapped\t/opt/mapped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, d := range []string{a, b, filepath.Join(a, "subdir")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(a, "both"), 0o755)
	write(filepath.Join(b, "both"), 0o755)
	write(filepath.Join(a, "plain"), 0o644) // not executable: b's wins
	write(filepath.Join(b, "plain"), 0o755)
	write(filepath.Join(b, "onlyB"), 0o755)
	write(filepath.Join(a, "mapped"), 0o755) // msh_bins.txt wins
	if err := os.Symlink(filepath.Join(b, "onlyB"), filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", a+":"+filepath.Join(root, "missing")+":"+b)

	names := []string{"both", "plain", "onlyB", "mapped", "link", "subdir", "nothere", "", "a/both"}
	lazy := NewPathBinManager()
	got := map[string]string{}
	for _, n := range names {
		if p, ok := lazy.Lookup(n); ok {
			got[n] = p
		}
	}
	// The full scan, then the same lookups answered from it.
	scanned := NewPathBinManager()
	scanned.DebugList()
	for _, n := range names {
		p, ok := scanned.Lookup(n)
		if q, had := got[n]; ok != had || p != q {
			t.Errorf("%q: lookup gives %q %v, the scan %q %v", n, q, had, p, ok)
		}
	}
	want := map[string]string{
		"both": filepath.Join(a, "both"), "plain": filepath.Join(b, "plain"), "onlyB": filepath.Join(b, "onlyB"),
		"mapped": "/opt/mapped", "link": filepath.Join(a, "link"),
	}
	if !maps.Equal(got, want) {
		t.Errorf("lookups %v, want %v", got, want)
	}
	// An answer is kept until Update, which forgets it.
	if _, ok := lazy.Lookup("later"); ok {
		t.Fatal("found 'later' before it exists")
	}
	write(filepath.Join(a, "later"), 0o755)
	if _, ok := lazy.Lookup("later"); ok {
		t.Error("a name not found was looked up again")
	}
	lazy.Update()
	if _, ok := lazy.Lookup("later"); !ok {
		t.Error("Update did not forget a name not found")
	}
}
