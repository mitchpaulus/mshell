package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func makeCompletionTree(t testing.TB, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < n; i++ {
		name := "entry" + strconv.Itoa(i) + strings.Repeat("x", i%40)
		path := filepath.Join(root, name)
		switch i % 4 {
		case 0:
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		default:
			if err := os.WriteFile(path+".typ", nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Symlink(filepath.Join(root, "entry0"), filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "linkbroken")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReadDirListingMatchesOSReadDir(t *testing.T) {
	// Enough entries to need several reads and a growing buffer.
	root := makeCompletionTree(t, 1500)
	var listing DirListing
	for round := 0; round < 2; round++ { // The second round reuses the buffers.
		if err := readDirListing(root, &listing); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]string, 0, len(entries))
		for _, e := range entries {
			kind := "f"
			if e.IsDir() {
				kind = "d"
			} else if e.Type()&os.ModeSymlink != 0 {
				kind = "l"
			}
			want = append(want, e.Name()+":"+kind)
		}
		got := make([]string, 0, listing.Len())
		for i := 0; i < listing.Len(); i++ {
			kind := map[dirEntryKind]string{dirKindFile: "f", dirKindDir: "d", dirKindLink: "l", dirKindUnknown: "?"}[listing.entries[i].kind]
			got = append(got, listing.name(i)+":"+kind)
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("round %d: listing differs from os.ReadDir (%d vs %d entries)", round, len(got), len(want))
		}
	}
}

func TestReadDirListingMissingDir(t *testing.T) {
	var listing DirListing
	if err := readDirListing(filepath.Join(t.TempDir(), "nope"), &listing); err == nil {
		t.Fatal("expected an error")
	}
}

func TestOSFileCompletionSortedWithLinks(t *testing.T) {
	root := makeCompletionTree(t, 12)
	fsys := OSCompletionFS{Listing: &DirListing{}}
	spec := &CompletionSpec{Dirs: true}
	matches := appendFileCompletions(nil, specInput(root+"/", spec), fsys)
	texts := GetMatchTexts(matches)
	if !sort.StringsAreSorted(texts) {
		t.Errorf("matches not sorted: %v", texts)
	}
	sep := string(os.PathSeparator)
	want := []string{"entry0", "entry4", "entry8", "linkdir"}
	if len(texts) != len(want) {
		t.Fatalf("got %v", texts)
	}
	for i, w := range want {
		if !strings.HasPrefix(texts[i], root+"/"+w) || !strings.HasSuffix(texts[i], sep) {
			t.Errorf("match %d = %q, want %s%s...%s", i, texts[i], root+"/", w, sep)
		}
	}
}

func TestOSFileCompletionSteadyStateAllocs(t *testing.T) {
	root := makeCompletionTree(t, 400)
	fsys := OSCompletionFS{Listing: &DirListing{}}
	g, _ := CompileCompletionGlob("*.typ")
	input := specInput(root+"/entry1", &CompletionSpec{PreferredFiles: []CompletionGlob{g}})
	matches := make([]TabMatch, 0, 512)
	appendFileCompletions(matches[:0], input, fsys) // Warm the reused buffers.
	allocs := testing.AllocsPerRun(20, func() {
		appendFileCompletions(matches[:0], input, fsys)
	})
	// The output string buffer, plus what opening the directory costs.
	if allocs > 3 {
		t.Errorf("file completion allocated %v times per Tab, want <= 3", allocs)
	}
}

func BenchmarkRealDirCompletion(b *testing.B) {
	root := makeCompletionTree(b, 2000)
	g, _ := CompileCompletionGlob("*.typ")
	input := specInput(root+"/entry1", &CompletionSpec{PreferredFiles: []CompletionGlob{g}})
	b.Run("listing", func(b *testing.B) {
		fsys := OSCompletionFS{Listing: &DirListing{}}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			appendFileCompletions(nil, input, fsys)
		}
	})
	b.Run("os.ReadDir", func(b *testing.B) {
		// What the old path paid: os.ReadDir plus a string per match.
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entries, _ := os.ReadDir(root)
			var out []TabMatch
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "entry1") && (e.IsDir() || strings.HasSuffix(e.Name(), ".typ")) {
					out = append(out, TabMatch{TABMATCHFILE, root + "/" + e.Name()})
				}
			}
		}
	})
}

func TestDirListingKeepsLongNames(t *testing.T) {
	// 255 UTF-16 units of a 3-byte character: legal on Windows and macOS.
	long := strings.Repeat("語", 255)
	var listing DirListing
	listing.add(long, dirKindFile)
	listing.add("short", dirKindDir)
	if listing.Len() != 2 || listing.name(0) != long || listing.name(1) != "short" {
		t.Fatalf("long name lost: %d entries", listing.Len())
	}
	if size := unsafe.Sizeof(dirEntryRef{}); size != 8 {
		t.Errorf("dirEntryRef is %d bytes, want 8", size)
	}
}
