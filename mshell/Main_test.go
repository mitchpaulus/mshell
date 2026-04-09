package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHistory(t *testing.T) {
	path := "test.mshell_history"
	_ = WriteToHistory(os.Getenv("HOME"), "echo hello", path)
}

func TestAllMatchesAreFiles(t *testing.T) {
	if allMatchesAreFiles(nil) {
		t.Fatalf("expected false for empty matches")
	}

	if !allMatchesAreFiles([]TabMatch{
		{TabMatchType: TABMATCHFILE, Match: "Floor 0/"},
		{TabMatchType: TABMATCHFILE, Match: "Floor 1/"},
	}) {
		t.Fatalf("expected true when all matches are files")
	}

	if allMatchesAreFiles([]TabMatch{
		{TabMatchType: TABMATCHFILE, Match: "Floor 0/"},
		{TabMatchType: TABMATCHDEF, Match: "FloorHelper"},
	}) {
		t.Fatalf("expected false for mixed match types")
	}
}

func TestBuildCompletionInsertPrefersPathQuoteForFileMatches(t *testing.T) {
	state := TermState{l: NewLexer("", nil)}

	got := state.buildCompletionInsert("Floor 0/", LITERAL, true)
	want := "`Floor 0/"
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildCompletionInsert("Floor 0.txt", LITERAL, true)
	want = "`Floor 0.txt` "
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildCompletionInsert("Floor 0/", LITERAL, false)
	want = "'Floor 0/'"
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}
}

func TestBuildSharedCompletionInsertUsesBacktickForFilePrefixes(t *testing.T) {
	state := TermState{l: NewLexer("", nil)}

	got := state.buildSharedCompletionInsert("Floor ", LITERAL, true)
	want := "`Floor "
	if got != want {
		t.Fatalf("buildSharedCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildSharedCompletionInsert("Floor ", LITERAL, false)
	want = "'Floor "
	if got != want {
		t.Fatalf("buildSharedCompletionInsert() = %q, want %q", got, want)
	}
}

func TestDefaultAppCommandFallsBackToPlatformDefault(t *testing.T) {
	tests := []struct {
		goos       string
		wantName   string
		wantArgs   []string
	}{
		{goos: "linux", wantName: "xdg-open", wantArgs: []string{"/tmp/init.msh"}},
		{goos: "darwin", wantName: "open", wantArgs: []string{"/tmp/init.msh"}},
		{goos: "windows", wantName: "powershell.exe", wantArgs: []string{"-NoProfile", "-Command", "Start-Process -FilePath '/tmp/init.msh'"}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			command, args, err := defaultAppCommand("/tmp/init.msh", tt.goos)
			if err != nil {
				t.Fatalf("defaultAppCommand() error = %v", err)
			}

			if command != tt.wantName {
				t.Fatalf("command = %q, want %q", command, tt.wantName)
			}

			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestOpenPathInEditorOrDefaultAppUsesEditorStringDirectly(t *testing.T) {
	t.Setenv("EDITOR", `C:\Program Files\Neovim\bin\nvim.exe`)

	oldRunAttachedCommand := runAttachedCommand
	defer func() {
		runAttachedCommand = oldRunAttachedCommand
	}()

	var gotName string
	var gotArgs []string
	runAttachedCommand = func(name string, args []string) error {
		gotName = name
		gotArgs = append([]string{}, args...)
		return nil
	}

	if err := openPathInEditorOrDefaultApp(`C:\Users\me\init.msh`); err != nil {
		t.Fatalf("openPathInEditorOrDefaultApp() error = %v", err)
	}

	if gotName != `C:\Program Files\Neovim\bin\nvim.exe` {
		t.Fatalf("command = %q, want %q", gotName, `C:\Program Files\Neovim\bin\nvim.exe`)
	}

	wantArgs := []string{`C:\Users\me\init.msh`}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestRunEditCommandOpensVersionedInitFilePath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("EDITOR", "my-editor")
	t.Setenv("MSHINIT", "")

	oldRunAttachedCommand := runAttachedCommand
	defer func() {
		runAttachedCommand = oldRunAttachedCommand
	}()

	var gotName string
	var gotArgs []string
	runAttachedCommand = func(name string, args []string) error {
		gotName = name
		gotArgs = append([]string{}, args...)
		return nil
	}

	exitCode := runEditCommand([]string{"init"})
	if exitCode != 0 {
		t.Fatalf("runEditCommand() = %d, want 0", exitCode)
	}

	expectedPath := filepath.Join(configHome, "msh", mshellVersion, "init.msh")
	if gotName != "my-editor" {
		t.Fatalf("command = %q, want %q", gotName, "my-editor")
	}

	wantArgs := []string{expectedPath}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestRunEditCommandUsesMSHINITOverride(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), "custom", "init.msh")
	t.Setenv("MSHINIT", overridePath)
	t.Setenv("EDITOR", "my-editor")

	oldRunAttachedCommand := runAttachedCommand
	defer func() {
		runAttachedCommand = oldRunAttachedCommand
	}()

	var gotArgs []string
	runAttachedCommand = func(name string, args []string) error {
		gotArgs = append([]string{}, args...)
		return nil
	}

	exitCode := runEditCommand([]string{"init"})
	if exitCode != 0 {
		t.Fatalf("runEditCommand() = %d, want 0", exitCode)
	}

	wantArgs := []string{overridePath}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}
