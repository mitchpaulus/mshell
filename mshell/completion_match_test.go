package main

import (
	"os"
	"strconv"
	"testing"
)

func TestTypedTextRank(t *testing.T) {
	cases := []struct {
		typed, candidate string
		want             matchRank
	}{
		{"", "", rankPrefix},
		{"", "alpha", rankPrefix},
		{"alpha", "alpha", rankPrefix},
		{"alp", "alpha", rankPrefix},
		// Lowercase typing ignores case at no cost in rank.
		{"alpha", "AlPhA", rankPrefix},
		{"alpha", "AlPhA!", rankPrefix},
		// Uppercase typing ranks a case difference lower.
		{"ALPHA", "alpha!", rankPrefixIgnoreCase},
		{"ALPHA!", "alPhA!", rankPrefixIgnoreCase},
		{"alPh", "ALPHA!", rankPrefixIgnoreCase},
		{"Alp", "Alpha", rankPrefix},
		{"LPH", "ALPHA!", rankSubstring},
		{"lph", "AlPhA!", rankSubstring},
		{"lPh", "ALPHA!", rankSubstringIgnoreCase},
		// A same-case place later in the candidate beats an earlier one
		// that differs in case.
		{"Ab", "xabxAb", rankSubstring},
		{"ha!", "ALPHA!", rankSubstring},
		// Letters spread out, or text longer than the candidate, do not fit.
		{"AA", "ALPHA!", rankNone},
		{"lh", "ALPHA!", rankNone},
		{"BB", "ALPHA!", rankNone},
		{"alphabet", "alpha", rankNone},
		// Options only match from the start.
		{"--h", "--help", rankPrefix},
		{"-v", "--verbose", rankNone},
		{"-h", "x-help", rankNone},
		// Non-ASCII.
		{"é", "CAFÉ", rankSubstring},
		{"É", "café", rankSubstringIgnoreCase},
		{"caf", "Café", rankPrefix},
		{"k", "\u212Aelvin", rankPrefix}, // Kelvin sign folds to k.
		{"lvin", "\u212Aelvin", rankSubstring},
		{"ü", "Müller", rankSubstring},
		{"üm", "Müller", rankNone},
	}
	for _, tc := range cases {
		typed := newTypedText(tc.typed)
		if got := typed.rank(tc.candidate, rankNone); got != tc.want {
			t.Errorf("rank(%q in %q) = %d, want %d", tc.typed, tc.candidate, got, tc.want)
		}
	}
}

func TestTypedTextRankCutoff(t *testing.T) {
	typed := newTypedText("al")
	if got := typed.rank("xalpha", rankPrefix); got != rankNone {
		t.Errorf("a substring passed a prefix cutoff: %d", got)
	}
	if got := typed.rank("alpha", rankPrefix); got != rankPrefix {
		t.Errorf("a prefix failed a prefix cutoff: %d", got)
	}
	upper := newTypedText("Al")
	if got := upper.rank("alpha", rankPrefix); got != rankNone {
		t.Errorf("a case difference passed a same-case cutoff: %d", got)
	}
}

func rankTestDeps() CompletionDeps {
	return CompletionDeps{
		FS: FakeCompletionFS{Cwd: "/w", Entries: map[string][]FakeDirEntry{
			".": {
				{EntryName: "Makefile"},
				{EntryName: "makepkg.conf"},
				{EntryName: "mylen.txt"},
				{EntryName: "README.md"},
				{EntryName: "src", EntryIsDir: true},
			},
			"src/": {
				{EntryName: "Main.go"},
				{EntryName: "domain.go"},
			},
		}},
		Env:         FakeCompletionEnv{Vars: []string{"HOME=/home/user", "XDG_HOME_DIR=/x", "=C:=C:\\"}},
		Binaries:    FakePathBinManager{Binaries: map[string]string{"make": "/bin/make", "cmake": "/bin/cmake"}},
		Variables:   map[string]struct{}{"homeDir": {}},
		BuiltIns:    map[string]struct{}{"len": {}, "split": {}},
		Definitions: []string{"lengthOf"},
	}
}

func argInput(prefix string) CompletionInput {
	return CompletionInput{Prefix: prefix, LastTokenType: LITERAL, PrevTokenType: WHITESPACE, NumTokens: 4}
}

func TestCompletionKeepsBestRankAcrossSources(t *testing.T) {
	deps := rankTestDeps()

	// Prefix matches from builtins and definitions drop the file that
	// only contains the text.
	matches, rank := generateRankedCompletions(argInput("len"), deps)
	expectMatches(t, "prefix wins", matches, "len", "lengthOf")
	if rank != rankPrefix {
		t.Errorf("rank = %d, want prefix", rank)
	}

	// With nothing starting with it, text in the middle matches.
	matches, rank = generateRankedCompletions(argInput("ylen"), deps)
	expectMatches(t, "substring", matches, "mylen.txt")
	if rank != rankSubstring {
		t.Errorf("rank = %d, want substring", rank)
	}

	// Lowercase typing ignores case.
	expectMatches(t, "smart case", GenerateCompletions(argInput("mak"), deps), "Makefile", "makepkg.conf")
	expectMatches(t, "smart case substring", GenerateCompletions(argInput("adme"), deps), "README.md")

	// Uppercase typing prefers the same case.
	expectMatches(t, "same case wins", GenerateCompletions(argInput("Mak"), deps), "Makefile")
	expectMatches(t, "case falls back", GenerateCompletions(argInput("MAKEP"), deps), "makepkg.conf")
}

func TestCompletionMatchesFileNameNotDirectory(t *testing.T) {
	deps := rankTestDeps()
	expectMatches(t, "in dir prefix", GenerateCompletions(argInput("src/main"), deps), "src/Main.go")
	expectMatches(t, "in dir substring", GenerateCompletions(argInput("src/ain"), deps), "src/Main.go", "src/domain.go")
	expectMatches(t, "dir not searched", GenerateCompletions(argInput("rc/"), deps))
}

func TestCompletionSigilsAndBinaries(t *testing.T) {
	deps := rankTestDeps()
	expectMatches(t, "env prefix", GenerateCompletions(argInput("$HO"), deps), "$HOME")
	expectMatches(t, "env substring", GenerateCompletions(argInput("$_HOME"), deps), "$XDG_HOME_DIR")
	expectMatches(t, "env no empty name", GenerateCompletions(argInput("$C"), deps))
	expectMatches(t, "var", GenerateCompletions(argInput("@dir"), deps), "@homeDir")

	cmd := CompletionInput{Prefix: "mak", LastTokenType: LITERAL, NumTokens: 2}
	expectMatches(t, "binary prefix", GenerateCompletions(cmd, deps), "Makefile", "make", "makepkg.conf")
	cmd.Prefix = "cma"
	expectMatches(t, "binary prefix drops substrings", GenerateCompletions(cmd, deps), "cmake")
	cmd.Prefix = "ake"
	expectMatches(t, "binary substring", GenerateCompletions(cmd, deps), "Makefile", "cmake", "make", "makepkg.conf")
}

func TestCompletionOptionsOnlyMatchFromStart(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{Values: []string{"--help", "--verbose", "compile"}}
	expectMatches(t, "option prefix", GenerateCompletions(specInput("--h", spec), deps), "--help")
	expectMatches(t, "option substring", GenerateCompletions(specInput("-v", spec), deps))
	expectMatches(t, "value substring", GenerateCompletions(specInput("pil", spec), deps), "compile")
}

// preferredTestDeps lists one directory for a 'preferredFiles: *.typ'
// definition.
func preferredTestDeps(files []string, dirs []string) CompletionDeps {
	var entries []FakeDirEntry
	for _, f := range files {
		entries = append(entries, FakeDirEntry{EntryName: f})
	}
	for _, d := range dirs {
		entries = append(entries, FakeDirEntry{EntryName: d, EntryIsDir: true})
	}
	return CompletionDeps{
		FS:       FakeCompletionFS{Cwd: "/w", Entries: map[string][]FakeDirEntry{".": entries}},
		Env:      FakeCompletionEnv{},
		Binaries: FakePathBinManager{},
	}
}

func TestPreferredFilesLevels(t *testing.T) {
	g, _ := CompileCompletionGlob("*.typ")
	cases := []struct {
		name   string
		typed  string
		values []string
		files  []string
		dirs   []string
		want   []string
	}{
		// A preferred file containing the text beats a fallback file
		// starting with it, and a directory that only contains it.
		{"preferred substring", "rep", nil, []string{"report.pdf", "my-report.typ"}, []string{"prep"}, []string{"my-report.typ"}},
		// A directory starting with the text beats a preferred file that
		// only contains it.
		{"dir prefix", "sr", nil, []string{"user-srs.typ"}, []string{"src"}, []string{"src/"}},
		{"preferred and dir prefix", "ma", nil, []string{"main.typ", "manual.pdf"}, []string{"math"}, []string{"main.typ", "math/"}},
		{"preferred case differs", "Rep", nil, []string{"report.typ", "Report.pdf"}, nil, []string{"report.typ"}},
		{"dir prefix hides fallback", "no", nil, []string{"notes.pdf"}, []string{"notes"}, []string{"notes/"}},
		// With no preferred file or prefix directory, the fallback offers
		// files and directories that contain the text.
		{"fallback substring", "rc", nil, []string{"arc.pdf"}, []string{"src"}, []string{"arc.pdf", "src/"}},
		{"preferred beats dir substring", "rc", nil, []string{"orc.typ"}, []string{"src"}, []string{"orc.typ"}},
		{"preferred substring alone", "co", nil, []string{"deco.typ", "config.toml"}, nil, []string{"deco.typ"}},
		// A value starting with the text beats a preferred file that only
		// contains it, without letting the fallback files in.
		{"value beats preferred substring", "co", []string{"compile"}, []string{"deco.typ", "config.toml"}, nil, []string{"compile"}},
		{"value and preferred prefix", "co", []string{"compile"}, []string{"cover.typ", "config.toml"}, nil, []string{"compile", "cover.typ"}},
	}
	for _, tc := range cases {
		deps := preferredTestDeps(tc.files, tc.dirs)
		spec := &CompletionSpec{Values: tc.values, PreferredFiles: []CompletionGlob{g}}
		expectMatches(t, tc.name, GenerateCompletions(specInput(tc.typed, spec), deps), tc.want...)
	}
}

// countingCompletionFS counts the stat calls a completion makes.
type countingCompletionFS struct {
	FakeCompletionFS
	stats *int
}

func (c countingCompletionFS) EntryIsDir(l *DirListing, dir string, name string) bool {
	*c.stats++
	return c.FakeCompletionFS.EntryIsDir(l, dir, name)
}

func TestCompletionStatsOnlyLinksThatMatter(t *testing.T) {
	// 50 links that only contain "a", one that starts with it, and a
	// preferred file that only contains it.
	var entries []FakeDirEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, FakeDirEntry{EntryName: "x-a-" + strconv.Itoa(i), EntryIsLink: true})
	}
	entries = append(entries,
		FakeDirEntry{EntryName: "alpha", EntryIsLink: true},
		FakeDirEntry{EntryName: "xa.sh"},
	)
	stats := 0
	fsys := countingCompletionFS{FakeCompletionFS{Cwd: "/w", Entries: map[string][]FakeDirEntry{".": entries}}, &stats}
	deps := CompletionDeps{FS: fsys, Env: FakeCompletionEnv{}, Binaries: FakePathBinManager{}}
	g, _ := CompileCompletionGlob("*.sh")

	cases := []struct {
		name      string
		spec      *CompletionSpec
		want      []string
		wantStats int
	}{
		// Every entry is offered whatever its type, so only the one shown
		// is stat'd, for its trailing separator.
		{"no spec", nil, []string{"alpha"}, 1},
		// alpha could only be offered as a directory, so it is stat'd. The
		// links that only contain the text could only be offered as
		// directories, and directories that only contain it are not
		// preferred, so they are skipped by name.
		{"preferred", &CompletionSpec{PreferredFiles: []CompletionGlob{g}}, []string{"xa.sh"}, 1},
	}
	for _, tc := range cases {
		stats = 0
		expectMatches(t, tc.name, GenerateCompletions(specInput("a", tc.spec), deps), tc.want...)
		if stats != tc.wantStats {
			t.Errorf("%s: %d stat calls, want %d", tc.name, stats, tc.wantStats)
		}
	}
}

func TestOSFileCompletionLinksAndClose(t *testing.T) {
	root := makeCompletionTree(t, 4)
	fsys := OSCompletionFS{Listing: &DirListing{}}
	c := newCompletionCollector()
	appendFileCompletions(&c, specInput(root+"/link", nil), fsys)
	sep := string(os.PathSeparator)
	expectMatches(t, "links", c.matches, root+"/linkbroken", root+"/linkdir"+sep)
	if fsys.Listing.open != (openDir{}) {
		t.Errorf("the directory was left open after completion")
	}
}

// pathLikeNames is a list shaped like the executables on a typical PATH.
func pathLikeNames(n int) []string {
	stems := []string{"git", "docker", "python", "systemctl", "gnome-shell", "x86_64-linux-gnu-gcc", "lsblk", "ffmpeg", "node", "kubectl"}
	names := make([]string, n)
	for i := range names {
		names[i] = stems[i%len(stems)] + "-" + strconv.Itoa(i)
	}
	return names
}

func BenchmarkTypedTextRank(b *testing.B) {
	names := pathLikeNames(3000)
	for _, typed := range []string{"gi", "Doc", "ctl", "zq"} {
		b.Run(typed, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c := newCompletionCollector()
				t := newTypedText(typed)
				for _, name := range names {
					c.add(TABMATCHCMD, name, t.rank(name, c.best))
				}
			}
		})
	}
}

func BenchmarkFileCompletionNoMatch(b *testing.B) {
	entries := make([]FakeDirEntry, 0, 2000)
	for i := 0; i < 2000; i++ {
		entries = append(entries, FakeDirEntry{EntryName: "report-" + strconv.Itoa(i) + ".typ"})
	}
	fsys := prebuiltCompletionFS{fake: FakeCompletionFS{Entries: map[string][]FakeDirEntry{".": entries}}}
	input := specInput("zz", nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := newCompletionCollector()
		appendFileCompletions(&c, input, &fsys)
	}
}
