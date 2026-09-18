//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func assertHistoryMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s: mode %o, want %o", path, info.Mode().Perm(), mode)
	}
}

func TestHistoryPrivacySaveAndRepair(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("XDG_DATA_HOME", parent)
	oldMask := unix.Umask(0022)
	defer unix.Umask(oldMask)
	oldPending := historyToSave
	defer func() { historyToSave = oldPending }()
	item := HistoryItem{UnixTimeUtc: 123, Command: "private command", Directory: "/private"}
	historyToSave = []HistoryItem{item}
	state := &TermState{}
	state.saveHistory()
	if len(historyToSave) != 0 {
		t.Fatal("history was not saved")
	}
	dir := filepath.Join(parent, "msh")
	assertHistoryMode(t, dir, 0700)
	files := []string{"msh_history", "msh_commands", "msh_dirs"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		assertHistoryMode(t, path, 0600)
		if err := os.Chmod(path, 0644); err != nil { t.Fatal(err) }
	}
	if err := os.Chmod(parent, 0755); err != nil { t.Fatal(err) }
	if err := os.Chmod(dir, 0755); err != nil { t.Fatal(err) }
	items, err := ReadHistory(dir)
	if err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(items, []HistoryItem{item}) {
		t.Fatalf("history changed: %#v", items)
	}
	assertHistoryMode(t, parent, 0755)
	assertHistoryMode(t, dir, 0700)
	for _, name := range files { assertHistoryMode(t, filepath.Join(dir, name), 0600) }
	if err := os.Chmod(dir, 0500); err != nil { t.Fatal(err) }
	if err := os.Chmod(filepath.Join(dir, "msh_commands"), 0400); err != nil { t.Fatal(err) }
	if _, err := ReadHistory(dir); err != nil { t.Fatal(err) }
	assertHistoryMode(t, dir, 0500)
	assertHistoryMode(t, filepath.Join(dir, "msh_commands"), 0400)
	if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
	if err := os.Chmod(filepath.Join(dir, "msh_commands"), 0644); err != nil { t.Fatal(err) }
	historyToSave = []HistoryItem{item}
	state.saveHistory()
	assertHistoryMode(t, filepath.Join(dir, "msh_commands"), 0600)
	items, err = ReadHistory(dir)
	if err != nil || len(items) != 2 { t.Fatalf("append failed: %v, %#v", err, items) }
}

func TestHistoryRejectsUnexpectedObjects(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "fifo", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "msh")
			if err := ensureHistoryDir(dir); err != nil { t.Fatal(err) }
			target := filepath.Join(parent, "unrelated")
			if err := os.WriteFile(target, []byte("untouched"), 0644); err != nil { t.Fatal(err) }
			path := filepath.Join(dir, "msh_commands")
			var err error
			switch kind {
			case "symlink": err = os.Symlink(target, path)
			case "hardlink": err = os.Link(target, path)
			case "directory": err = os.Mkdir(path, 0755)
			case "fifo": err = unix.Mkfifo(path, 0644)
			case "foreign":
				if os.Geteuid() != 0 { t.Skip("changing ownership requires root") }
				err = os.WriteFile(path, []byte("foreign"), 0644)
				if err == nil { err = os.Chown(path, 65534, 65534) }
			}
			if err != nil { t.Fatal(err) }
			if err := prepareHistoryStorage(dir); err == nil { t.Fatal("unsafe history accepted") }
			if f, err := openHistoryFile(path, os.O_APPEND|os.O_WRONLY); err == nil {
				f.Close()
				t.Fatal("unsafe save accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "untouched" { t.Fatal("unrelated data modified") }
			assertHistoryMode(t, target, 0644)
		})
	}
}

func TestHistoryRejectsDirectorySymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "shared")
	if err := os.Mkdir(target, 0755); err != nil { t.Fatal(err) }
	dir := filepath.Join(parent, "msh")
	if err := os.Symlink(target, dir); err != nil { t.Fatal(err) }
	if err := ensureHistoryDir(dir); err == nil { t.Fatal("directory symlink accepted") }
	assertHistoryMode(t, target, 0755)
}

func TestHistoryRepairsRemainingFilesWhenHistoryMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "msh")
	if err := ensureHistoryDir(dir); err != nil { t.Fatal(err) }
	path := filepath.Join(dir, "msh_commands")
	if err := os.WriteFile(path, []byte("secret\n"), 0644); err != nil { t.Fatal(err) }
	if _, err := ReadHistory(dir); !os.IsNotExist(err) { t.Fatalf("want missing history: %v", err) }
	assertHistoryMode(t, path, 0600)
}

func TestHistoryPermissionErrorsReported(t *testing.T) {
	if os.Geteuid() == 0 { t.Skip("root bypasses Unix access checks") }
	parent := t.TempDir()
	t.Setenv("XDG_DATA_HOME", parent)
	dir := filepath.Join(parent, "msh")
	if err := ensureHistoryDir(dir); err != nil { t.Fatal(err) }
	path := filepath.Join(dir, "msh_commands")
	if err := os.WriteFile(path, []byte("secret\n"), 0000); err != nil { t.Fatal(err) }
	defer os.Chmod(path, 0600)
	if _, err := ReadHistory(dir); !os.IsPermission(err) {
		t.Fatalf("permission error not returned: %v", err)
	}
	oldPending := historyToSave
	defer func() { historyToSave = oldPending }()
	historyToSave = []HistoryItem{{Command: "pending", Directory: "/"}}
	stderr, err := os.CreateTemp(parent, "stderr")
	if err != nil { t.Fatal(err) }
	defer stderr.Close()
	originalStderr := os.Stderr
	func() {
		os.Stderr = stderr
		defer func() { os.Stderr = originalStderr }()
		(&TermState{}).saveHistory()
	}()
	data, err := os.ReadFile(stderr.Name())
	if err != nil || len(data) == 0 { t.Fatal("save permission error not reported") }
	if len(historyToSave) != 1 { t.Fatal("failed save discarded pending history") }
	assertHistoryMode(t, path, 0000)
}
