package main

import (
	"io/fs"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// FakeDirEntry implements fs.DirEntry for testing.
type FakeDirEntry struct {
	EntryName  string
	EntryIsDir bool
}

func (e FakeDirEntry) Name() string               { return e.EntryName }
func (e FakeDirEntry) IsDir() bool                { return e.EntryIsDir }
func (e FakeDirEntry) Type() fs.FileMode          { if e.EntryIsDir { return fs.ModeDir }; return 0 }
func (e FakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

// FakeCompletionFS implements CompletionFS for testing.
type FakeCompletionFS struct {
	Cwd     string
	Entries map[string][]FakeDirEntry // dir path -> entries
}

func (f FakeCompletionFS) Getwd() (string, error) {
	return f.Cwd, nil
}

func (f FakeCompletionFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	entries, ok := f.Entries[dir]
	if !ok {
		return nil, fs.ErrNotExist
	}
	result := make([]fs.DirEntry, len(entries))
	for i := range entries {
		result[i] = entries[i]
	}
	return result, nil
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
