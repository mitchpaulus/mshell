package main

import (
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
