package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// useTempHistoryDir points GetHistoryDir at a per-test temp directory so
// clipboard tests never touch the user's real shared clipboard.
func useTempHistoryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", dir)
	} else {
		t.Setenv("XDG_DATA_HOME", dir)
	}
	got, err := GetHistoryDir()
	if err != nil {
		t.Fatalf("GetHistoryDir: %v", err)
	}
	if !strings.HasPrefix(got, dir) {
		t.Fatalf("GetHistoryDir %q not under temp dir %q; aborting to protect real data", got, dir)
	}
	return got
}

func TestClipboardConcurrentAdds(t *testing.T) {
	useTempHistoryDir(t)

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op := "copy"
			if i%2 == 0 {
				op = "cut"
			}
			if err := addClipboardPath(op, fmt.Sprintf("/some/path/file%d", i)); err != nil {
				t.Errorf("addClipboardPath %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	cutPaths, copyPaths := loadClipboard()
	if len(cutPaths)+len(copyPaths) != n {
		t.Fatalf("expected %d clipboard entries after concurrent adds, got %d cut + %d copy",
			n, len(cutPaths), len(copyPaths))
	}
}

func TestClipboardLoadIsReadOnly(t *testing.T) {
	useTempHistoryDir(t)
	path, err := clipboardFilePath()
	if err != nil {
		t.Fatal(err)
	}

	// Valid entries mixed with malformed lines (no tab, empty path, unknown
	// op, blank line).
	content := "cut\t/a/b\nbogus line\ncopy\t\nteleport\t/x\n\ncopy\t/c/d\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cutPaths, copyPaths := loadClipboard()
	if len(cutPaths) != 1 || cutPaths[0] != "/a/b" {
		t.Errorf("cut paths = %v, want [/a/b]", cutPaths)
	}
	if len(copyPaths) != 1 || copyPaths[0] != "/c/d" {
		t.Errorf("copy paths = %v, want [/c/d]", copyPaths)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Errorf("loadClipboard modified the file: %q -> %q", content, string(after))
	}
}

func TestClipboardSaveRoundtripAndClear(t *testing.T) {
	useTempHistoryDir(t)

	err := withClipboardLock(func() error {
		return saveClipboard([]string{"/one", "/two"}, []string{"/three"})
	})
	if err != nil {
		t.Fatal(err)
	}
	cutPaths, copyPaths := loadClipboard()
	if len(cutPaths) != 2 || len(copyPaths) != 1 {
		t.Fatalf("roundtrip mismatch: cut %v copy %v", cutPaths, copyPaths)
	}

	// No stray temp files left behind.
	path, _ := clipboardFilePath()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %q", e.Name())
		}
	}

	// Saving empty removes the file; clearing an already-clear clipboard
	// is not an error.
	if err := withClipboardLock(func() error { return saveClipboard(nil, nil) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("clipboard file should be removed after empty save")
	}
	if err := clearClipboard(); err != nil {
		t.Errorf("clearClipboard on missing file: %v", err)
	}
}

func TestRefreshClipboard(t *testing.T) {
	useTempHistoryDir(t)
	fm := &FileManager{}

	if !fm.refreshClipboard(false) {
		t.Fatal("first refresh should load")
	}
	if len(fm.clipCut) != 0 || len(fm.clipCopy) != 0 {
		t.Fatalf("expected empty clipboard, got cut %v copy %v", fm.clipCut, fm.clipCopy)
	}
	if fm.refreshClipboard(false) {
		t.Error("refresh with no change should not reload")
	}

	if err := addClipboardPath("cut", "/from/elsewhere"); err != nil {
		t.Fatal(err)
	}
	if !fm.refreshClipboard(false) {
		t.Fatal("refresh should detect the new clipboard file")
	}
	if len(fm.clipCut) != 1 || fm.clipCut[0] != "/from/elsewhere" {
		t.Fatalf("cut cache = %v, want [/from/elsewhere]", fm.clipCut)
	}

	if !fm.refreshClipboard(true) {
		t.Error("forced refresh should always reload")
	}
}

// TestClipboardWatchPropagates checks the full push pipeline: a mutation by
// another instance reaches an idle FileManager (no keystrokes) through the
// directory watch — via an event, or via the one-time reconcile that runs
// right after the watch is established (which covers writes racing startup).
func TestClipboardWatchPropagates(t *testing.T) {
	useTempHistoryDir(t)

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()

	fm := &FileManager{rows: 24, cols: 80, ttyOut: devnull}
	fm.startClipboardWatch()
	defer fm.stopClipboardWatch()

	// Initial load, as the first render would do.
	fm.renderMu.Lock()
	fm.refreshClipboard(false)
	fm.renderMu.Unlock()

	// Another instance cuts a file.
	if err := addClipboardPath("cut", "/other/instance/file"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fm.renderMu.Lock()
		got := len(fm.clipCut)
		fm.renderMu.Unlock()
		if got == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("clipboard change never propagated to the idle instance")
}

func TestClipboardPasteMergesConcurrentEntries(t *testing.T) {
	useTempHistoryDir(t)

	srcDir := t.TempDir()
	destDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "moveme.txt")
	if err := os.WriteFile(srcFile, []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := addClipboardPath("cut", srcFile); err != nil {
		t.Fatal(err)
	}
	// Simulate another instance adding an entry before this one pastes.
	if err := addClipboardPath("copy", "/other/instance/entry"); err != nil {
		t.Fatal(err)
	}

	fm := &FileManager{currentDir: destDir}
	fm.clipboardPaste()

	if _, err := os.Stat(filepath.Join(destDir, "moveme.txt")); err != nil {
		t.Errorf("pasted file missing: %v", err)
	}
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Errorf("cut source should be gone after paste")
	}

	cutPaths, copyPaths := loadClipboard()
	if len(cutPaths) != 0 {
		t.Errorf("cut entries should be consumed by paste, got %v", cutPaths)
	}
	if len(copyPaths) != 1 || copyPaths[0] != "/other/instance/entry" {
		t.Errorf("concurrent copy entry lost: %v", copyPaths)
	}
}

// TestClipboardLockWaitIsBounded holds the clipboard lock and checks that a
// second acquire fails after its timeout instead of hanging: a stuck or
// hostile lock holder must never freeze another instance's keystroke.
func TestClipboardLockWaitIsBounded(t *testing.T) {
	useTempHistoryDir(t)
	dir, err := GetHistoryDir()
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, clipboardLockFileName)

	held, err := acquireFileLock(lockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()

	start := time.Now()
	if _, err := acquireFileLock(lockPath, 150*time.Millisecond); err == nil {
		t.Fatal("second acquire should time out while the lock is held")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timed-out acquire took %v; the wait is not bounded", elapsed)
	}
}

// TestClipboardLoadEntryCap writes more entries than the cap and checks the
// load degrades to exactly the cap instead of doing unbounded work.
func TestClipboardLoadEntryCap(t *testing.T) {
	useTempHistoryDir(t)
	path, err := clipboardFilePath()
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	for i := 0; i < maxClipboardEntries+100; i++ {
		fmt.Fprintf(&sb, "copy\t/bulk/file%05d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	cutPaths, copyPaths := loadClipboard()
	if got := len(cutPaths) + len(copyPaths); got != maxClipboardEntries {
		t.Fatalf("loaded %d entries, want cap of %d", got, maxClipboardEntries)
	}
}

// TestClipboardLoadByteCap writes a file past the byte cap (using paths long
// enough that the entry cap is not hit first) and checks that the load is
// partial rather than empty, and that the line straddling the cap is dropped
// whole — a truncated fragment must never surface as a real path.
func TestClipboardLoadByteCap(t *testing.T) {
	useTempHistoryDir(t)
	path, err := clipboardFilePath()
	if err != nil {
		t.Fatal(err)
	}

	longSeg := strings.Repeat("x", 4096)
	written := make(map[string]bool)
	var sb strings.Builder
	for i := 0; sb.Len() <= maxClipboardBytes+8192; i++ {
		p := fmt.Sprintf("/bulk/%s/%05d", longSeg, i)
		written[p] = true
		sb.WriteString("copy\t" + p + "\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	_, copyPaths := loadClipboard()
	if len(copyPaths) == 0 {
		t.Fatal("byte cap should degrade to a partial load, not an empty one")
	}
	if len(copyPaths) >= len(written) {
		t.Fatalf("loaded all %d entries; the byte cap did not bound the read", len(written))
	}
	for _, p := range copyPaths {
		if !written[p] {
			t.Fatalf("loaded a path that was never written (truncated fragment?): %.80q", p)
		}
	}
}

// TestClipboardSaveSweepsStaleTemps checks that a save removes temp files
// orphaned by a crashed writer (old mtime) while leaving recent ones alone,
// so orphans cannot accumulate forever.
func TestClipboardSaveSweepsStaleTemps(t *testing.T) {
	useTempHistoryDir(t)
	path, err := clipboardFilePath()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)

	stale := filepath.Join(dir, clipboardFileName+".tmp-stale")
	fresh := filepath.Join(dir, clipboardFileName+".tmp-fresh")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("orphan"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	err = withClipboardLock(func() error {
		return saveClipboard([]string{"/a"}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale temp file should have been swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("recent temp file should have been left alone: %v", err)
	}
}

// TestClipboardReadSingleFlight simulates a hung read by occupying the read
// gate and checks that further reads fail fast — and, critically, that a
// mutation aborts instead of saving an empty clipboard over real entries.
func TestClipboardReadSingleFlight(t *testing.T) {
	useTempHistoryDir(t)

	if err := addClipboardPath("cut", "/existing/entry"); err != nil {
		t.Fatal(err)
	}

	clipboardReadGate <- struct{}{}
	defer func() { <-clipboardReadGate }()

	start := time.Now()
	if _, _, ok := readClipboard(); ok {
		t.Fatal("read should fail while another read is in flight")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("gated read took %v, want fail-fast", elapsed)
	}

	if err := addClipboardPath("copy", "/new/entry"); err == nil {
		t.Fatal("mutation must abort when the clipboard cannot be read")
	}

	// The existing entry survived the aborted mutation.
	<-clipboardReadGate
	cutPaths, _ := loadClipboard()
	clipboardReadGate <- struct{}{}
	if len(cutPaths) != 1 || cutPaths[0] != "/existing/entry" {
		t.Fatalf("existing entry lost after aborted mutation: %v", cutPaths)
	}
}
