package main

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// FakeDirEntry implements fs.DirEntry for testing.
type FakeDirEntry struct {
	EntryName   string
	EntryIsDir  bool
	EntryIsLink bool
}

func (e FakeDirEntry) Name() string { return e.EntryName }
func (e FakeDirEntry) IsDir() bool  { return e.EntryIsDir }
func (e FakeDirEntry) Type() fs.FileMode {
	if e.EntryIsLink {
		return fs.ModeSymlink
	}
	if e.EntryIsDir {
		return fs.ModeDir
	}
	return 0
}
func (e FakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

// fakeFileInfo is what FakeCompletionFS.Stat reports for a link target.
type fakeFileInfo struct {
	name  string
	isDir bool
}

func (i fakeFileInfo) Name() string       { return i.name }
func (i fakeFileInfo) Size() int64        { return 0 }
func (i fakeFileInfo) Mode() fs.FileMode  { if i.isDir { return fs.ModeDir }; return 0 }
func (i fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (i fakeFileInfo) IsDir() bool        { return i.isDir }
func (i fakeFileInfo) Sys() any           { return nil }

// FakeCompletionFS implements CompletionFS for testing.
type FakeCompletionFS struct {
	Cwd      string
	Entries  map[string][]FakeDirEntry // dir path -> entries
	LinkDirs map[string]bool           // link path -> whether its target is a directory
}

func (f FakeCompletionFS) Stat(path string) (fs.FileInfo, error) {
	isDir, ok := f.LinkDirs[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return fakeFileInfo{name: filepath.Base(path), isDir: isDir}, nil
}

func (f FakeCompletionFS) Getwd() (string, error) {
	return f.Cwd, nil
}

func (f FakeCompletionFS) ReadDir(dir string) (*DirListing, error) {
	entries, ok := f.Entries[dir]
	if !ok {
		return nil, fs.ErrNotExist
	}
	listing := &DirListing{}
	for _, e := range entries {
		kind := dirKindFile
		if e.EntryIsLink {
			kind = dirKindLink
		} else if e.EntryIsDir {
			kind = dirKindDir
		}
		listing.add(e.EntryName, kind)
	}
	return listing, nil
}

// FakeCompletionEnv implements CompletionEnv for testing.
type FakeCompletionEnv struct {
	Vars []string
}

func (f FakeCompletionEnv) Environ() []string {
	return f.Vars
}

// FakePathBinManager implements IPathBinManager for testing.
type FakePathBinManager struct {
	Binaries map[string]string // name -> path
}

func (f FakePathBinManager) Matches(search string) []string {
	var matches []string
	for name := range f.Binaries {
		if strings.HasPrefix(name, search) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

func (f FakePathBinManager) Lookup(binName string) (string, bool) {
	path, ok := f.Binaries[binName]
	return path, ok
}

func (f FakePathBinManager) ExecuteArgs(execPath string) ([]string, error) {
	return nil, nil
}

func (f FakePathBinManager) DebugList() *MShellList {
	return nil
}

func (f FakePathBinManager) IsExecutableFile(path string) bool {
	return false
}

func (f FakePathBinManager) SetupCommand(allArgs []string) *exec.Cmd {
	return nil
}

func (f FakePathBinManager) Update() {}

// Helper to extract match strings of a specific type.
func filterMatchesByType(matches []TabMatch, matchType TabMatchType) []string {
	var result []string
	for _, m := range matches {
		if m.TabMatchType == matchType {
			result = append(result, m.Match)
		}
	}
	sort.Strings(result)
	return result
}

func TestEnvVarCompletion(t *testing.T) {
	deps := CompletionDeps{
		FS:  FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env: FakeCompletionEnv{Vars: []string{"HOME=/home/user", "HOSTNAME=myhost", "PATH=/usr/bin"}},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "$HO",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	envMatches := filterMatchesByType(matches, TABMATCHENVVAR)

	if len(envMatches) != 2 {
		t.Errorf("expected 2 env matches, got %d: %v", len(envMatches), envMatches)
	}

	if envMatches[0] != "$HOME" || envMatches[1] != "$HOSTNAME" {
		t.Errorf("expected $HOME and $HOSTNAME, got %v", envMatches)
	}
}

func TestEnvVarCompletionNoMatch(t *testing.T) {
	deps := CompletionDeps{
		FS:          FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env:         FakeCompletionEnv{Vars: []string{"HOME=/home/user", "PATH=/usr/bin"}},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "$XYZ",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	envMatches := filterMatchesByType(matches, TABMATCHENVVAR)

	if len(envMatches) != 0 {
		t.Errorf("expected 0 env matches, got %d: %v", len(envMatches), envMatches)
	}
}

func TestMShellVariableCompletion(t *testing.T) {
	deps := CompletionDeps{
		FS:       FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env:      FakeCompletionEnv{},
		Binaries: FakePathBinManager{},
		Variables: map[string]struct{}{
			"myvar":    {},
			"mylist":   {},
			"othervar": {},
		},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "@my",
		LastTokenType: VARRETRIEVE,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	varMatches := filterMatchesByType(matches, TABMATCHVAR)

	if len(varMatches) != 2 {
		t.Errorf("expected 2 var matches, got %d: %v", len(varMatches), varMatches)
	}

	if varMatches[0] != "@mylist" || varMatches[1] != "@myvar" {
		t.Errorf("expected @mylist and @myvar, got %v", varMatches)
	}
}

func TestMShellVariableBangCompletion(t *testing.T) {
	deps := CompletionDeps{
		FS:       FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env:      FakeCompletionEnv{},
		Binaries: FakePathBinManager{},
		Variables: map[string]struct{}{
			"myvar":    {},
			"mylist":   {},
			"othervar": {},
		},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "my",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	varMatches := filterMatchesByType(matches, TABMATCHVAR)

	if len(varMatches) != 2 {
		t.Errorf("expected 2 var matches, got %d: %v", len(varMatches), varMatches)
	}

	if varMatches[0] != "mylist!" || varMatches[1] != "myvar!" {
		t.Errorf("expected mylist! and myvar!, got %v", varMatches)
	}
}

func TestBinaryCompletion(t *testing.T) {
	deps := CompletionDeps{
		FS:  FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env: FakeCompletionEnv{},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{
				"git":   "/usr/bin/git",
				"grep":  "/usr/bin/grep",
				"go":    "/usr/bin/go",
				"ls":    "/usr/bin/ls",
			},
		},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "g",
		LastTokenType: LITERAL,
		NumTokens:     2, // First token + EOF = binary position
	}

	matches := GenerateCompletions(input, deps)
	binMatches := filterMatchesByType(matches, TABMATCHCMD)

	if len(binMatches) != 3 {
		t.Errorf("expected 3 binary matches, got %d: %v", len(binMatches), binMatches)
	}

	if binMatches[0] != "git" || binMatches[1] != "go" || binMatches[2] != "grep" {
		t.Errorf("expected git, go, grep, got %v", binMatches)
	}
}

func TestBinaryCompletionAfterPipe(t *testing.T) {
	deps := CompletionDeps{
		FS:  FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env: FakeCompletionEnv{},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{
				"git":  "/usr/bin/git",
				"grep": "/usr/bin/grep",
				"go":   "/usr/bin/go",
				"ls":   "/usr/bin/ls",
			},
		},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "g",
		LastTokenType: LITERAL,
		PrevTokenType: PIPE,
		NumTokens:     4,
	}

	matches := GenerateCompletions(input, deps)
	binMatches := filterMatchesByType(matches, TABMATCHCMD)

	if len(binMatches) != 3 {
		t.Errorf("expected 3 binary matches, got %d: %v", len(binMatches), binMatches)
	}

	if binMatches[0] != "git" || binMatches[1] != "go" || binMatches[2] != "grep" {
		t.Errorf("expected git, go, grep, got %v", binMatches)
	}
}

func TestBinaryCompletionAfterListStart(t *testing.T) {
	deps := CompletionDeps{
		FS:  FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env: FakeCompletionEnv{},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{
				"git":  "/usr/bin/git",
				"grep": "/usr/bin/grep",
				"go":   "/usr/bin/go",
				"ls":   "/usr/bin/ls",
			},
		},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "g",
		LastTokenType: LITERAL,
		PrevTokenType: LEFT_SQUARE_BRACKET,
		NumTokens:     3,
	}

	matches := GenerateCompletions(input, deps)
	binMatches := filterMatchesByType(matches, TABMATCHCMD)

	if len(binMatches) != 3 {
		t.Errorf("expected 3 binary matches, got %d: %v", len(binMatches), binMatches)
	}

	if binMatches[0] != "git" || binMatches[1] != "go" || binMatches[2] != "grep" {
		t.Errorf("expected git, go, grep, got %v", binMatches)
	}
}

func TestBinaryCompletionNotFirstToken(t *testing.T) {
	deps := CompletionDeps{
		FS:  FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env: FakeCompletionEnv{},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{
				"git":  "/usr/bin/git",
				"grep": "/usr/bin/grep",
			},
		},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	// NumTokens = 3 means we're past the first token
	input := CompletionInput{
		Prefix:        "g",
		LastTokenType: LITERAL,
		NumTokens:     3,
	}

	matches := GenerateCompletions(input, deps)
	binMatches := filterMatchesByType(matches, TABMATCHCMD)

	if len(binMatches) != 0 {
		t.Errorf("expected 0 binary matches (not first token), got %d: %v", len(binMatches), binMatches)
	}
}

func TestBuiltInCompletion(t *testing.T) {
	deps := CompletionDeps{
		FS:          FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env:         FakeCompletionEnv{},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{"swap": {}, "split": {}, "sort": {}, "dup": {}},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "sw",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	builtinMatches := filterMatchesByType(matches, TABMATCHBUILTIN)

	if len(builtinMatches) != 1 {
		t.Errorf("expected 1 builtin match, got %d: %v", len(builtinMatches), builtinMatches)
	}

	if builtinMatches[0] != "swap" {
		t.Errorf("expected swap, got %v", builtinMatches)
	}
}

func TestDefinitionCompletion(t *testing.T) {
	deps := CompletionDeps{
		FS:          FakeCompletionFS{Cwd: "/home/user", Entries: map[string][]FakeDirEntry{}},
		Env:         FakeCompletionEnv{},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{"git-add", "git-commit", "git-push", "other-def"},
	}

	input := CompletionInput{
		Prefix:        "git",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	defMatches := filterMatchesByType(matches, TABMATCHDEF)

	if len(defMatches) != 3 {
		t.Errorf("expected 3 definition matches, got %d: %v", len(defMatches), defMatches)
	}
}

func TestNonFileCompletionSuppressedInBinaryMode(t *testing.T) {
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				".": {
					{EntryName: "docs", EntryIsDir: true},
					{EntryName: "data.txt", EntryIsDir: false},
				},
			},
		},
		Env:      FakeCompletionEnv{},
		Binaries: FakePathBinManager{},
		Variables: map[string]struct{}{
			"gitvar": {},
		},
		BuiltIns:    map[string]struct{}{"git-help": {}},
		Definitions: []string{"git-add", "git-commit", "git-push"},
	}

	input := CompletionInput{
		Prefix:        "d",
		LastTokenType: LITERAL,
		NumTokens:     3,
		InBinaryMode:  true,
	}

	matches := GenerateCompletions(input, deps)
	defMatches := filterMatchesByType(matches, TABMATCHDEF)
	builtinMatches := filterMatchesByType(matches, TABMATCHBUILTIN)
	varMatches := filterMatchesByType(matches, TABMATCHVAR)
	fileMatches := filterMatchesByType(matches, TABMATCHFILE)

	if len(defMatches) != 0 {
		t.Errorf("expected 0 definition matches in binary mode, got %d: %v", len(defMatches), defMatches)
	}

	if len(builtinMatches) != 0 {
		t.Errorf("expected 0 builtin matches in binary mode, got %v", builtinMatches)
	}

	if len(varMatches) != 0 {
		t.Errorf("expected 0 variable bang matches in binary mode, got %v", varMatches)
	}

	if len(fileMatches) != 2 || fileMatches[0] != "data.txt" || fileMatches[1] != "docs/" {
		t.Errorf("expected file matches to still work in binary mode, got %v", fileMatches)
	}
}

func TestFileCompletionEmptyPrefix(t *testing.T) {
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				"/home/user": {
					{EntryName: "file1.txt", EntryIsDir: false},
					{EntryName: "file2.go", EntryIsDir: false},
					{EntryName: "docs", EntryIsDir: true},
				},
			},
		},
		Env:         FakeCompletionEnv{},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "",
		LastTokenType: EOF,
		NumTokens:     1,
	}

	matches := GenerateCompletions(input, deps)
	fileMatches := filterMatchesByType(matches, TABMATCHFILE)

	if len(fileMatches) != 3 {
		t.Errorf("expected 3 file matches, got %d: %v", len(fileMatches), fileMatches)
	}

	// Check that directory has trailing slash
	hasDocsDir := false
	for _, m := range fileMatches {
		if m == "docs/" {
			hasDocsDir = true
			break
		}
	}
	if !hasDocsDir {
		t.Errorf("expected docs/ with trailing slash, got %v", fileMatches)
	}
}

func TestFileCompletionWithPrefix(t *testing.T) {
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				".": {
					{EntryName: "file1.txt", EntryIsDir: false},
					{EntryName: "file2.go", EntryIsDir: false},
					{EntryName: "foo.txt", EntryIsDir: false},
					{EntryName: "docs", EntryIsDir: true},
				},
			},
		},
		Env:         FakeCompletionEnv{},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "fi",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	fileMatches := filterMatchesByType(matches, TABMATCHFILE)

	if len(fileMatches) != 2 {
		t.Errorf("expected 2 file matches, got %d: %v", len(fileMatches), fileMatches)
	}
}

func TestFileCompletionSubdirectory(t *testing.T) {
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				"docs/": {
					{EntryName: "readme.md", EntryIsDir: false},
					{EntryName: "guide.md", EntryIsDir: false},
					{EntryName: "images", EntryIsDir: true},
				},
			},
		},
		Env:         FakeCompletionEnv{},
		Binaries:    FakePathBinManager{},
		Variables:   map[string]struct{}{},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}

	input := CompletionInput{
		Prefix:        "docs/re",
		LastTokenType: LITERAL,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)
	fileMatches := filterMatchesByType(matches, TABMATCHFILE)

	if len(fileMatches) != 1 {
		t.Errorf("expected 1 file match, got %d: %v", len(fileMatches), fileMatches)
	}

	if fileMatches[0] != "docs/readme.md" {
		t.Errorf("expected docs/readme.md, got %v", fileMatches)
	}
}

func TestUnfinishedStringOnlyReturnsFiles(t *testing.T) {
	// When inside an unfinished double-quoted string, only file completions should be returned.
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				".": {
					{EntryName: "file1.txt", EntryIsDir: false},
					{EntryName: "file2.txt", EntryIsDir: false},
					{EntryName: "other.go", EntryIsDir: false},
				},
			},
		},
		Env: FakeCompletionEnv{Vars: []string{"HOME=/home/user", "PATH=/usr/bin"}},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{
				"git": "/usr/bin/git",
				"ls":  "/usr/bin/ls",
			},
		},
		Variables:   map[string]struct{}{"myvar": {}, "mylist": {}},
		BuiltIns:    map[string]struct{}{"swap": {}, "dup": {}, "drop": {}},
		Definitions: []string{"my-def", "other-def"},
	}

	input := CompletionInput{
		Prefix:        "fi",
		LastTokenType: UNFINISHEDSTRING,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)

	// Should only have file matches (2 files starting with "fi")
	if len(matches) != 2 {
		t.Errorf("expected 2 total matches, got %d: %v", len(matches), matches)
	}

	for _, m := range matches {
		if m.TabMatchType != TABMATCHFILE {
			t.Errorf("expected all matches to be TABMATCHFILE, got %v", m)
		}
	}
}

func TestUnfinishedSingleQuoteStringOnlyReturnsFiles(t *testing.T) {
	// When inside an unfinished single-quoted string, only file completions should be returned.
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				"/home/user": {
					{EntryName: "file1.txt", EntryIsDir: false},
					{EntryName: "docs", EntryIsDir: true},
				},
			},
		},
		Env: FakeCompletionEnv{Vars: []string{"HOME=/home/user"}},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{"git": "/usr/bin/git"},
		},
		Variables:   map[string]struct{}{"myvar": {}},
		BuiltIns:    map[string]struct{}{"swap": {}, "dup": {}},
		Definitions: []string{"my-def"},
	}

	input := CompletionInput{
		Prefix:        "",
		LastTokenType: UNFINISHEDSINGLEQUOTESTRING,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)

	// Should only have file matches (2 entries in current directory)
	if len(matches) != 2 {
		t.Errorf("expected 2 total matches, got %d: %v", len(matches), matches)
	}

	for _, m := range matches {
		if m.TabMatchType != TABMATCHFILE {
			t.Errorf("expected all matches to be TABMATCHFILE, got %v", m)
		}
	}
}

func TestGetLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{[]string{}, ""},
		{[]string{"foo"}, "foo"},
		{[]string{"foo", "foobar"}, "foo"},
		{[]string{"foobar", "foo"}, "foo"},
		{[]string{"foo", "bar"}, ""},
		{[]string{"prefix_a", "prefix_b", "prefix_c"}, "prefix_"},
		{[]string{"abc", "ab", "a"}, "a"},
		{[]string{"", "foo"}, ""},
		{[]string{"foo", ""}, ""},
	}

	for _, tc := range tests {
		result := getLongestCommonPrefix(tc.input)
		if result != tc.expected {
			t.Errorf("getLongestCommonPrefix(%v) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestUnfinishedPathOnlyReturnsFiles(t *testing.T) {
	// When input is just "`" (unfinished path), only file completions should be returned.
	// All other completion sources should be ignored.
	deps := CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/home/user",
			Entries: map[string][]FakeDirEntry{
				"/home/user": {
					{EntryName: "file1.txt", EntryIsDir: false},
					{EntryName: "docs", EntryIsDir: true},
				},
			},
		},
		Env: FakeCompletionEnv{Vars: []string{"HOME=/home/user", "PATH=/usr/bin"}},
		Binaries: FakePathBinManager{
			Binaries: map[string]string{
				"git": "/usr/bin/git",
				"ls":  "/usr/bin/ls",
			},
		},
		Variables:   map[string]struct{}{"myvar": {}, "mylist": {}},
		BuiltIns:    map[string]struct{}{"swap": {}, "dup": {}, "drop": {}},
		Definitions: []string{"my-def", "other-def"},
	}

	// Empty prefix with UNFINISHEDPATH (simulates just "`" typed)
	input := CompletionInput{
		Prefix:        "",
		LastTokenType: UNFINISHEDPATH,
		NumTokens:     2,
	}

	matches := GenerateCompletions(input, deps)

	// Should only have file matches (2 files in the fake filesystem)
	if len(matches) != 2 {
		t.Errorf("expected 2 total matches, got %d: %v", len(matches), matches)
	}

	for _, m := range matches {
		if m.TabMatchType != TABMATCHFILE {
			t.Errorf("expected all matches to be TABMATCHFILE, got %v", m)
		}
	}
}

func mustGlob(t *testing.T, pattern string) CompletionGlob {
	t.Helper()
	g, err := CompileCompletionGlob(pattern)
	if err != nil {
		t.Fatalf("CompileCompletionGlob(%q): %v", pattern, err)
	}
	return g
}

func TestCompileCompletionGlob(t *testing.T) {
	tests := []struct {
		pattern string
		kind    completionGlobKind
		match   []string
		noMatch []string
	}{
		{"*", globAny, []string{"a", ".hidden", "x.typ"}, nil},
		{"*.typ", globSuffix, []string{"main.typ", ".typ"}, []string{"main.typst", "typ"}},
		{"Makefile*", globPrefix, []string{"Makefile", "Makefile.am"}, []string{"makefile"}},
		{"Makefile", globExact, []string{"Makefile"}, []string{"Makefile.am"}},
		{"test_*.py", globGeneral, []string{"test_a.py"}, []string{"a_test.py"}},
		{"*.[ch]", globGeneral, []string{"a.c", "a.h"}, []string{"a.o"}},
	}
	for _, tt := range tests {
		g := mustGlob(t, tt.pattern)
		if g.Kind != tt.kind {
			t.Errorf("%q: kind %d, want %d", tt.pattern, g.Kind, tt.kind)
		}
		for _, name := range tt.match {
			if !g.Matches(name) {
				t.Errorf("%q should match %q", tt.pattern, name)
			}
		}
		for _, name := range tt.noMatch {
			if g.Matches(name) {
				t.Errorf("%q should not match %q", tt.pattern, name)
			}
		}
	}
	if _, err := CompileCompletionGlob("[a-"); err == nil {
		t.Errorf("expected an error for a malformed pattern")
	}
}

func TestCompileCompletionGlobDoesNotAllocate(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		g, _ := CompileCompletionGlob("*.typ")
		_ = g.Matches("main.typ")
	})
	if allocs != 0 {
		t.Errorf("compile+match allocated %v times, want 0", allocs)
	}
}

func specTestDeps() CompletionDeps {
	return CompletionDeps{
		FS: FakeCompletionFS{
			Cwd: "/work",
			Entries: map[string][]FakeDirEntry{
				"/work": {
					{EntryName: "main.typ"},
					{EntryName: "notes.md"},
					{EntryName: "out.pdf"},
					{EntryName: "src", EntryIsDir: true},
					{EntryName: "linked", EntryIsLink: true},
					{EntryName: "broken", EntryIsLink: true},
				},
				".": {
					{EntryName: "main.typ"},
					{EntryName: "notes.md"},
					{EntryName: "out.pdf"},
					{EntryName: "src", EntryIsDir: true},
				},
			},
			LinkDirs: map[string]bool{"/work/linked": true},
		},
		Env:         FakeCompletionEnv{},
		Binaries:    FakePathBinManager{Binaries: map[string]string{"typst": "/bin/typst", "tar": "/bin/tar"}},
		Variables:   map[string]struct{}{"var": {}},
		BuiltIns:    map[string]struct{}{},
		Definitions: []string{},
	}
}

func specInput(prefix string, spec *CompletionSpec) CompletionInput {
	numTokens := 3 // binary, whitespace, EOF
	if prefix != "" {
		numTokens = 4
	}
	return CompletionInput{Prefix: prefix, LastTokenType: LITERAL, PrevTokenType: WHITESPACE, NumTokens: numTokens, InBinaryMode: true, Spec: spec}
}

func matchTexts(matches []TabMatch) []string {
	texts := GetMatchTexts(matches)
	sort.Strings(texts)
	return texts
}

func expectMatches(t *testing.T, name string, got []TabMatch, want ...string) {
	t.Helper()
	texts := matchTexts(got)
	sort.Strings(want)
	if strings.Join(texts, ",") != strings.Join(want, ",") {
		t.Errorf("%s: got %v, want %v", name, texts, want)
	}
}

func TestCompletionSpecNilOffersAllFiles(t *testing.T) {
	deps := specTestDeps()
	expectMatches(t, "nil spec", GenerateCompletions(specInput("", nil), deps),
		"main.typ", "notes.md", "out.pdf", "src/", "linked/", "broken")
	anyFiles := &CompletionSpec{Files: []CompletionGlob{mustGlob(t, "*")}}
	expectMatches(t, "files '*'", GenerateCompletions(specInput("", anyFiles), deps),
		"main.typ", "notes.md", "out.pdf", "src/", "linked/", "broken")
}

func TestCompletionSpecFilesGlob(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{Files: []CompletionGlob{mustGlob(t, "*.typ"), mustGlob(t, "*.md")}}
	expectMatches(t, "files", GenerateCompletions(specInput("", spec), deps),
		"main.typ", "notes.md", "src/", "linked/")
}

func TestCompletionSpecDirsOnly(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{Dirs: true}
	expectMatches(t, "dirs", GenerateCompletions(specInput("", spec), deps), "src/", "linked/")
}

func TestCompletionSpecValuesOnlyOffersNoFiles(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{Values: []string{"compile", "watch", "--help"}}
	expectMatches(t, "values", GenerateCompletions(specInput("", spec), deps), "compile", "watch")
	expectMatches(t, "options", GenerateCompletions(specInput("--", spec), deps), "--help")
}

func TestCompletionSpecPreferredFiles(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{PreferredFiles: []CompletionGlob{mustGlob(t, "*.typ")}}
	expectMatches(t, "preferred", GenerateCompletions(specInput("", spec), deps),
		"main.typ", "src/", "linked/")

	// Nothing preferred fits 'no', so every file is offered.
	expectMatches(t, "fallback to all", GenerateCompletions(specInput("no", spec), deps), "notes.md")

	// With 'files' as well, the fallback is 'files' instead of every file.
	spec.Files = []CompletionGlob{mustGlob(t, "*.pdf")}
	expectMatches(t, "fallback to files", GenerateCompletions(specInput("no", spec), deps))
	expectMatches(t, "fallback to files", GenerateCompletions(specInput("o", spec), deps), "out.pdf")
}

func TestCompletionSpecBinaries(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{Binaries: true}
	expectMatches(t, "binaries", GenerateCompletions(specInput("t", spec), deps), "tar", "typst")
}

func TestCompletionSpecUnfinishedString(t *testing.T) {
	deps := specTestDeps()
	spec := &CompletionSpec{Values: []string{"main"}, Files: []CompletionGlob{mustGlob(t, "*.typ")}}
	input := specInput("ma", spec)
	input.LastTokenType = UNFINISHEDSINGLEQUOTESTRING
	expectMatches(t, "unfinished", GenerateCompletions(input, deps), "main", "main.typ")
}

func TestMergeCompletionDict(t *testing.T) {
	state := &TermState{}
	dict := NewDict()
	values := NewList(0)
	values.Items = append(values.Items, MShellString{"compile"})
	dict.Items["values"] = values
	dict.Items["preferredFiles"] = MShellString{"*.typ"}
	files := NewList(0)
	files.Items = append(files.Items, MShellString{"*.pdf"}, MShellString{"[bad"}, MShellBool{true})
	dict.Items["files"] = files
	dict.Items["dirs"] = MShellBool{true}
	dict.Items["binaries"] = MShellString{"yes"} // wrong type, ignored

	var spec CompletionSpec
	got := state.mergeCompletionDict("test", dict, &spec)
	if got != values {
		t.Errorf("values list not returned")
	}
	if len(spec.PreferredFiles) != 1 || spec.PreferredFiles[0].Text != ".typ" {
		t.Errorf("preferredFiles = %+v", spec.PreferredFiles)
	}
	if len(spec.Files) != 1 || spec.Files[0].Text != ".pdf" {
		t.Errorf("files = %+v", spec.Files)
	}
	if !spec.Dirs || spec.Binaries {
		t.Errorf("dirs=%v binaries=%v", spec.Dirs, spec.Binaries)
	}
}

func BenchmarkFileCompletionPreferred(b *testing.B) {
	entries := make([]FakeDirEntry, 0, 2000)
	for i := 0; i < 2000; i++ {
		name := "file" + strings.Repeat("x", i%7) + string(rune('a'+i%26))
		switch i % 4 {
		case 0:
			name += ".typ"
		case 1:
			name += ".pdf"
		case 2:
			entries = append(entries, FakeDirEntry{EntryName: name, EntryIsDir: true})
			continue
		}
		entries = append(entries, FakeDirEntry{EntryName: name})
	}
	fsys := prebuiltCompletionFS{fake: FakeCompletionFS{Entries: map[string][]FakeDirEntry{".": entries}}}
	g, _ := CompileCompletionGlob("*.typ")
	input := specInput("file", &CompletionSpec{PreferredFiles: []CompletionGlob{g}})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		appendFileCompletions(nil, input, &fsys)
	}
}

// prebuiltCompletionFS hands back one listing every time, so a benchmark
// measures completion and not building the fake.
type prebuiltCompletionFS struct {
	fake    FakeCompletionFS
	listing *DirListing
}

func (p *prebuiltCompletionFS) ReadDir(dir string) (*DirListing, error) {
	if p.listing == nil {
		p.listing, _ = p.fake.ReadDir(dir)
	}
	return p.listing, nil
}
func (p *prebuiltCompletionFS) Getwd() (string, error)           { return "/w", nil }
func (p *prebuiltCompletionFS) Stat(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist }
