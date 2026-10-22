package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// FmBindings.go reads the members of the file manager's enums by their
// order in builtinDeclSource; the tables there must follow it.
func TestFmBindingEnumsMatch(t *testing.T) {
	enums := builtinEnums(t)
	check := func(name string, have []string) {
		t.Helper()
		d, ok := enums[name]
		if !ok {
			t.Fatalf("builtinDeclSource does not declare %s", name)
		}
		if !slices.Equal(d.Members, have) {
			t.Errorf("%s: the Go table is %v, the declaration %v", name, have, d.Members)
		}
	}
	check("FmOp", fmOpMembers[:])
	check("FmMode", fmModeMembers[:])
	check("FmPrompt", fmPromptMembers)
}

// Every default binding is in force: none conflicts with another, and
// every program runs where it stands.
func TestFmDefaultBindingsInForce(t *testing.T) {
	var ev EvalState
	ev.ensureFmDefaultBindings()
	for _, e := range ev.FmBindings {
		if e.inert != "" {
			t.Errorf("default binding %s is inert: %s", e.describe(), e.inert)
		}
	}
}

// Each reason a binding can never fire, and programs that cross modes.
func TestFmBindingInertReasons(t *testing.T) {
	tests := []struct {
		source string
		inert  string // a part of the reason, or "" for a binding in force
	}{
		{`{keys: [r'a'], action: [fmPromptSearch fmPrompt 'TODO' editInsert fmLineAccept] fmOps}`, ""},
		{`{keys: [r'a'], action: [fmHelp fmDown fmHelpClose fmUp] fmOps}`, ""},
		{`{keys: [r'a'], action: [fmPromptRename fmPrompt editLineStart fmLineCancel fmTop] fmOps}`, ""},
		{`{keys: [r'a'], action: [([ls];) fmRun fmQuit] fmOps, mode: fmModeHelp}`, ""},
		{`{keys: [r'a'], action: [fmDown editLeft] fmOps}`, "'editLeft' cannot run in fmModeNormal"},
		{`{keys: [r'a'], action: [editAccept] fmOps, mode: fmModeInsert}`, "'editAccept' cannot run in fmModeInsert"},
		{`{keys: [r'a'], action: [fmHelp fmSearchNext] fmOps}`, "'fmSearchNext' cannot run in fmModeHelp"},
		{`{keys: [r'a'], action: [fmHelp] fmOps, mode: fmModeInsert}`, "'fmHelp' cannot run in fmModeInsert"},
		{`{keys: [r'a'], action: [fmQuit fmDown] fmOps}`, "'fmQuit' must be its last op"},
		{`{keys: [r'a'], action: [0 fmRows fmDownBy] fmOps}`, "a distance must be more than 0"},
		{`{keys: [r'a'], action: [-5.0 fmPercent fmUpBy] fmOps}`, "a distance must be more than 0"},
		{`{keys: [], action: [fmDown] fmOps}`, "it has no keys"},
		{`{keys: [r'a'], action: [fmDown] fmOps, within: 0}`, "a time must be more than 0 ms"},
	}
	for _, tt := range tests {
		ev := &EvalState{}
		if r := evalForTest(t, ev, tt.source+" fmBindingAdd"); !r.Success {
			t.Fatalf("%s: fmBindingAdd failed", tt.source)
		}
		e := ev.FmBindings[len(ev.FmBindings)-1]
		if tt.inert == "" && e.inert != "" || !strings.Contains(e.inert, tt.inert) {
			t.Errorf("%s: inert %q, want %q", tt.source, e.inert, tt.inert)
		}
	}
}

// A newer binding that an older one is a prefix of makes the older inert,
// and erasing first keeps it from being reported.
func TestFmBindingConflicts(t *testing.T) {
	ev := &EvalState{}
	evalForTest(t, ev, `{keys: [r'd' r'd'], action: [fmCut] fmOps} fmBindingAdd`)
	want := "Binding [r'd'] never fires: it conflicts with the newer binding [r'd' r'd']."
	if !slices.Contains(ev.AbbrWarnings, want) {
		t.Errorf("warnings %q, want %q", ev.AbbrWarnings, want)
	}
	ev = &EvalState{}
	evalForTest(t, ev, `[r'd'] fmBindingErase {keys: [r'd' r'd'], action: [fmCut] fmOps} fmBindingAdd`)
	if len(ev.AbbrWarnings) != 0 {
		t.Errorf("warnings after erasing first: %q", ev.AbbrWarnings)
	}
}

// fmTestManager makes a file manager over a new directory holding the
// entries given (a name ending in / is a directory), with the bindings
// source adds, as init.msh would.
func fmTestManager(t *testing.T, source string, entries ...string) *FileManager {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	// A file opened by mistake must not wait for an editor.
	t.Setenv("EDITOR", "true")
	dir := t.TempDir()
	for _, name := range entries {
		path := filepath.Join(dir, name)
		var err error
		if strings.HasSuffix(name, "/") {
			err = os.Mkdir(path, 0755)
		} else {
			err = os.WriteFile(path, []byte(name), 0644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	ev := &EvalState{CallStack: make([]CallStackItem, 0, 10)}
	file, err := parseMShellInput(source, &TokenFile{"test"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r := evalForTest(t, ev, source); !r.Success {
		t.Fatal("the bindings failed")
	}
	fm := &FileManager{ev: ev, definitions: file.Definitions, rows: 20, cols: 80, currentDir: dir, startedIn: dir}
	fm.loadDirectory()
	return fm
}

// names are the entries of the listing.
func names(fm *FileManager) []string {
	var out []string
	for _, e := range fm.entries {
		out = append(out, e.Name())
	}
	return out
}

// The default keys move the cursor as they did before bindings.
func TestFmDefaultMoves(t *testing.T) {
	fm := fmTestManager(t, "", "a/", "b/", "c.txt", "d.txt", "e.md")
	steps := []struct {
		input  string
		cursor int
	}{
		{"j", 1}, {"k", 0}, {"G", 4}, {"gg", 0}, {"\x1b[F", 4}, {"\x1b[H", 0},
		{"\x1b[B", 1}, {"\x1b[A", 0}, {"\x04", 4}, {"\x15", 0}, {"\x1b[6~", 4}, {"\x1b[5~", 0},
		// y and a key that does not continue it: the key acts alone.
		{"yj", 1},
		// Escape and a key in one read, with Alt and the key unbound.
		{"\x1bj", 2},
	}
	for _, s := range steps {
		feedInput(fm, s.input)
		if fm.cursor != s.cursor || fm.mode != fmModeNormal {
			t.Fatalf("after %q: cursor %d, mode %d; want %d", s.input, fm.cursor, fm.mode, s.cursor)
		}
	}

	feedInput(fm, "ggl")
	if filepath.Base(fm.currentDir) != "a" {
		t.Fatalf("l did not enter a: %s", fm.currentDir)
	}
	feedInput(fm, "h")
	if fm.currentName() != "a" {
		t.Fatalf("h left the cursor on %q", fm.currentName())
	}
}

// A pending y lists what can follow it, from the table.
func TestFmPrefixPopup(t *testing.T) {
	fm := fmTestManager(t, `{keys: [r'y' r'n'], action: ['hi' fmMessage] fmOps, description: 'say hi'} fmBindingAdd`, "a")
	feedInput(fm, "y")
	if !fm.popup {
		t.Fatal("no popup after y")
	}
	lines := strings.Join(fm.popupLines(), "\n")
	for _, want := range []string{" y ", "y  copy entry to file-manager clipboard", "g  git-relative path", "n  say hi"} {
		if !strings.Contains(lines, want) {
			t.Errorf("popup %q does not hold %q", lines, want)
		}
	}
	feedInput(fm, "n")
	if fm.popup || fm.statusMsg != "hi" {
		t.Errorf("after y n: popup %v, message %q", fm.popup, fm.statusMsg)
	}
}

// Search: the line follows the matches, Enter jumps, n and N move, and
// keys that are not text no longer close the line.
func TestFmSearch(t *testing.T) {
	fm := fmTestManager(t, "", "a/", "café.txt", "c.txt", "d.md")
	feedInput(fm, "/t\r")
	if fm.mode != fmModeNormal || fm.currentName() != "c.txt" {
		t.Fatalf("after /t Enter: mode %d, on %q", fm.mode, fm.currentName())
	}
	feedInput(fm, "n")
	if fm.currentName() != "café.txt" {
		t.Fatalf("n went to %q", fm.currentName())
	}
	feedInput(fm, "N")
	if fm.currentName() != "c.txt" {
		t.Fatalf("N went to %q", fm.currentName())
	}

	feedInput(fm, "gg/é")
	feedInput(fm, "\x1b[A")
	if fm.mode != fmModeInsert {
		t.Fatal("Up closed the search line")
	}
	feedInput(fm, "\r")
	if fm.currentName() != "café.txt" {
		t.Fatalf("searching for é went to %q", fm.currentName())
	}

	feedInput(fm, "/x")
	feedInput(fm, "\x1b")
	if fm.mode != fmModeNormal {
		t.Fatal("Escape did not close the search line")
	}
}

// Rename: the line starts with the name, the cursor before the extension,
// and edits with the shell's line edits.
func TestFmRename(t *testing.T) {
	fm := fmTestManager(t, "", "c.txt", "report.final.pdf")
	feedInput(fm, "rX\r")
	if !slices.Contains(names(fm), "cX.txt") || fm.currentName() != "cX.txt" {
		t.Fatalf("after r X Enter: %v, on %q", names(fm), fm.currentName())
	}

	feedInput(fm, "r\x1b[H\x1b[3~")
	if string(fm.line.text) != "X.txt" {
		t.Fatalf("Delete at the start left %q", fm.line.text)
	}
	feedInput(fm, "\x03")
	if fm.mode != fmModeNormal || !slices.Contains(names(fm), "cX.txt") {
		t.Fatalf("Ctrl-C did not cancel: mode %d, %v", fm.mode, names(fm))
	}

	feedInput(fm, "j")
	feedInput(fm, "r\x17")
	if string(fm.line.text) != "report..pdf" {
		t.Fatalf("Ctrl-W left %q, want report..pdf", fm.line.text)
	}
	feedInput(fm, "\x1bb")
	if fm.line.index != 0 {
		t.Fatalf("Alt-B left the cursor at %d", fm.line.index)
	}
}

// Space selects, v inverts, u clears, and cut takes every selected entry,
// across directories, then clears the selection.
func TestFmSelection(t *testing.T) {
	fm := fmTestManager(t, "", "a/", "b.txt", "c.txt")
	if err := os.WriteFile(filepath.Join(fm.currentDir, "a", "inner"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	feedInput(fm, "j  ")
	if len(fm.selected) != 2 || fm.cursor != 2 {
		t.Fatalf("after j space space: %v, cursor %d", fm.selected, fm.cursor)
	}
	feedInput(fm, "v")
	if len(fm.selected) != 1 || filepath.Base(fm.selected[0]) != "a" {
		t.Fatalf("v gave %v", fm.selected)
	}
	feedInput(fm, "u")
	if len(fm.selected) != 0 {
		t.Fatalf("u left %v", fm.selected)
	}

	feedInput(fm, "j ")
	feedInput(fm, "gg")
	feedInput(fm, "l ")
	if len(fm.selected) != 2 {
		t.Fatalf("selected across directories: %v", fm.selected)
	}
	feedInput(fm, "d")
	cut, _ := loadClipboard()
	if len(cut) != 2 || len(fm.selected) != 0 {
		t.Fatalf("cut %v, still selected %v", cut, fm.selected)
	}

	// A selected entry removed before the op is left out, and counted.
	feedInput(fm, " ")
	os.Remove(fm.selected[0])
	feedInput(fm, "yy")
	if !strings.Contains(fm.statusMsg, "1 selected entry no longer exists") {
		t.Fatalf("message %q", fm.statusMsg)
	}
}

// Pastes never run keys: outside a prompt they are refused out loud; in
// one they go in whole or not at all.
func TestFmPastes(t *testing.T) {
	fm := fmTestManager(t, "", "a", "b")
	if feedInput(fm, "\x1b[200~qj\x1b[201~") {
		t.Fatal("a pasted q quit")
	}
	if fm.cursor != 0 || !strings.HasPrefix(fm.statusMsg, "Paste ignored (2 bytes)") {
		t.Fatalf("cursor %d, message %q", fm.cursor, fm.statusMsg)
	}

	feedInput(fm, "/", "\x1b[200~abc\r\x1b[201~")
	if string(fm.line.text) != "abc" {
		t.Fatalf("line %q after a paste with a line break", fm.line.text)
	}
	feedInput(fm, "\x1b[200~a\tb\x1b[201~")
	if string(fm.line.text) != "abc" || !strings.Contains(fm.statusMsg, "control character") {
		t.Fatalf("a tab: line %q, message %q", fm.line.text, fm.statusMsg)
	}
	feedInput(fm, "\x1b[200~"+strings.Repeat("x", maxFmLineBytes)+"\x1b[201~")
	if string(fm.line.text) != "abc" || !strings.Contains(fm.statusMsg, "at most") {
		t.Fatalf("a long paste: line %d bytes, message %q", len(fm.line.text), fm.statusMsg)
	}

	// Typing at the limit.
	fm.line.text = SourceText(strings.Repeat("x", maxFmLineBytes))
	fm.line.index = ByteOffset(maxFmLineBytes)
	feedInput(fm, "y")
	if len(fm.line.text) != maxFmLineBytes || fm.statusMsg == "" {
		t.Fatalf("typing at the limit: %d bytes, message %q", len(fm.line.text), fm.statusMsg)
	}
}

// The help screen lists bindings, and the inert ones with the reason; its
// moves scroll it, and closing it leaves the listing as it was.
func TestFmHelp(t *testing.T) {
	fm := fmTestManager(t, `
{keys: [r'g' r'h'], action: [~ fmCd] fmOps, description: 'home'} fmBindingAdd
{keys: [r'Z'], action: [fmDown editLeft] fmOps} fmBindingAdd
`, "a", "b", "c")
	feedInput(fm, "j?")
	if fm.mode != fmModeHelp {
		t.Fatal("? did not open the help")
	}
	lines := strings.Join(fm.helpLines(), "\n")
	for _, want := range []string{" Browsing", "g h", "home", " Not in force", "[r'Z']: 'editLeft' cannot run in fmModeNormal"} {
		if !strings.Contains(lines, want) {
			t.Errorf("help does not hold %q", want)
		}
	}
	feedInput(fm, "jjk")
	if fm.helpOffset != 1 || fm.cursor != 1 {
		t.Fatalf("help offset %d, cursor %d", fm.helpOffset, fm.cursor)
	}
	feedInput(fm, "G")
	if fm.helpOffset != len(fm.helpLines())-fm.helpRows() {
		t.Fatalf("G scrolled to %d", fm.helpOffset)
	}
	feedInput(fm, "q")
	if fm.mode != fmModeNormal || fm.cursor != 1 || fm.quit {
		t.Fatalf("q in help: mode %d, cursor %d, quit %v", fm.mode, fm.cursor, fm.quit)
	}
}

// With j j bound, j moves at once, and the second j takes it back and runs
// the binding.
func TestFmRollback(t *testing.T) {
	fm := fmTestManager(t, `{keys: [r'j' r'j'], action: [fmBottom] fmOps} fmBindingAdd`, "a", "b", "c", "d")
	feedInput(fm, "j")
	if fm.cursor != 1 {
		t.Fatalf("j moved to %d", fm.cursor)
	}
	feedInput(fm, "j")
	if fm.cursor != 3 {
		t.Fatalf("j j went to %d", fm.cursor)
	}
}

// A prompt of a binding's own runs its quote with the line, and then the
// program it returns. A quote that declines does nothing; one that fails
// says so.
func TestFmPromptAsk(t *testing.T) {
	fm := fmTestManager(t, `
def goTo (str FmState -- Maybe[FmProgram])
    drop name!
    [@name fmCursorTo] just
end
{keys: [r'a'], action: ['Go to: ' '' (goTo) fmPromptAsk fmPrompt] fmOps} fmBindingAdd
{keys: [r'b'], action: ['Nothing: ' 'x' (drop drop none) fmPromptAsk fmPrompt] fmOps} fmBindingAdd
{keys: [r'z'], action: ['Fails: ' '' (drop drop drop) fmPromptAsk fmPrompt] fmOps} fmBindingAdd
{keys: [r'B'], action: (drop [fmBottom] just) fmFunc} fmBindingAdd
`, "a", "b", "c")
	feedInput(fm, "a")
	if fm.mode != fmModeInsert || fm.lineLabel() != "Go to: " {
		t.Fatalf("a: mode %d, label %q", fm.mode, fm.lineLabel())
	}
	feedInput(fm, "c\r")
	if fm.mode != fmModeNormal || fm.currentName() != "c" {
		t.Fatalf("after Enter: mode %d, on %q, message %q", fm.mode, fm.currentName(), fm.statusMsg)
	}
	feedInput(fm, "gg", "b\r")
	if fm.cursor != 0 || fm.statusMsg != "" {
		t.Fatalf("a declining quote: cursor %d, message %q", fm.cursor, fm.statusMsg)
	}
	feedInput(fm, "z\r")
	if fm.statusMsg == "" {
		t.Fatal("a failing quote said nothing")
	}
	feedInput(fm, "B")
	if fm.cursor != 2 {
		t.Fatalf("a fmFunc binding: cursor %d", fm.cursor)
	}
}

// Q quits without changing directory; q quits in the directory shown.
func TestFmQuit(t *testing.T) {
	fm := fmTestManager(t, "", "a/")
	if !feedInput(fm, "lQ") || !fm.quitNoCd {
		t.Fatalf("Q: quit %v, no cd %v", fm.quit, fm.quitNoCd)
	}
	fm = fmTestManager(t, "", "a/")
	if !feedInput(fm, "lq") || fm.quitNoCd || filepath.Base(fm.currentDir) != "a" {
		t.Fatalf("q: quit %v, no cd %v, in %s", fm.quit, fm.quitNoCd, fm.currentDir)
	}
}

// A question reads one key, which reaches no binding: x and then a key
// that is not y keeps the file, and x y trashes it.
func TestFmTrashQuestion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("trashes with the freedesktop trash")
	}
	fm := fmTestManager(t, "", "a", "b")
	feedInput(fm, "xj")
	if len(fm.entries) != 2 || fm.cursor != 0 {
		t.Fatalf("x j: %v, cursor %d", names(fm), fm.cursor)
	}
	feedInput(fm, "xy")
	if slices.Contains(names(fm), "a") {
		t.Fatalf("x y kept a: %v", names(fm))
	}
}

// TestFmResolveMatchesDefinition runs the file manager's resolver on random
// keymaps of its prompt line, against the definition the shell's resolver
// is checked against (TestResolveMatchesDefinition): a pure binding
// inserts its text, an effect shows a message, and a paste inserts "p".
func TestFmResolveMatchesDefinition(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	alphabet := []KeyCode{'a', 'b', 'x' | KeyModCtrl}
	gaps := []time.Duration{0, 2 * time.Millisecond}
	limits := []time.Duration{0, 0, time.Millisecond}
	for trial := range 4000 {
		ev := &EvalState{fmDefaultBindingsAdded: true}
		for i := range 1 + rng.Intn(6) {
			keys := make([]KeyCode, 1+rng.Intn(3))
			for j := range keys {
				keys[j] = alphabet[rng.Intn(len(alphabet))]
			}
			e := &fmBindingEntry{keys: keys, mode: fmModeInsert,
				within: limits[rng.Intn(len(limits))], idleBefore: limits[rng.Intn(len(limits))]}
			text := fmt.Sprintf("<%d>", i)
			if rng.Intn(2) == 0 {
				e.ops = []fmOp{{kind: fmOpEdit, edit: editOp{kind: opInsert, a: text}}}
			} else {
				e.ops = []fmOp{{kind: fmOpMessage, a: "E" + text}}
			}
			ev.FmBindings = slices.DeleteFunc(ev.FmBindings, func(o *fmBindingEntry) bool { return slices.Equal(o.keys, e.keys) })
			ev.FmBindings = append(ev.FmBindings, e)
		}
		ev.recomputeFmBindings()
		var bs []oracleBinding
		var active []bool
		for _, e := range ev.FmBindings {
			ob := oracleBinding{keys: e.keys, pure: e.pure(), within: e.within, ib: e.idleBefore}
			if ob.pure {
				ob.text = e.ops[0].edit.a
			} else {
				ob.text = strings.TrimPrefix(e.ops[0].a, "E")
			}
			bs = append(bs, ob)
			active = append(active, e.inert == "")
		}

		events := make([]oracleEvent, rng.Intn(9))
		for i := range events {
			events[i].gap = gaps[rng.Intn(len(gaps))]
			if rng.Intn(8) == 0 {
				events[i].paste = true
			} else {
				events[i].key = alphabet[rng.Intn(len(alphabet))]
			}
		}

		fm := &FileManager{ev: ev, rows: 20, cols: 80}
		fm.openPrompt(fmPrompt{kind: fmLineAsk})
		now := time.Unix(1_000_000, 0)
		var log []string
		for i, e := range events {
			now = now.Add(e.gap)
			fm.statusMsg = ""
			if e.paste {
				fm.keys.event(now)
				fm.replaceLine("p", fm.line.index, fm.line.index)
			} else {
				fm.resolveKey(e.key, now)
			}
			if m := fm.statusMsg; strings.HasPrefix(m, "E") {
				log = append(log, strings.TrimPrefix(m, "E")+"@"+string(fm.line.text))
			}
			want := definition(bs, active, events, i+1)
			if string(fm.line.text) != want.line || !slices.Equal(log, want.log) {
				t.Fatalf("trial %d, after event %d of %s\nkeymap %s\nline %q, effects %v\nwant %q, %v",
					trial, i, describeEvents(events), describeKeymap(bs, active), fm.line.text, log, want.line, want.log)
			}
		}
	}
}

// The default keys that are not moves do what they did before bindings:
// c clears the clipboard, y y and p copy, F5 reads the directory again,
// and m and ; set and go to a bookmark.
func TestFmDefaultEffects(t *testing.T) {
	fm := fmTestManager(t, "", "a/", "b.txt")
	feedInput(fm, "jyy")
	if _, copied := loadClipboard(); len(copied) != 1 {
		t.Fatalf("y y copied %v", copied)
	}
	feedInput(fm, "ggl", "p")
	if !slices.Contains(names(fm), "b.txt") {
		t.Fatalf("p left %v", names(fm))
	}
	feedInput(fm, "c")
	if cut, copied := loadClipboard(); len(cut)+len(copied) != 0 {
		t.Fatalf("c left %v %v", cut, copied)
	}
	if err := os.WriteFile(filepath.Join(fm.currentDir, "new"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	feedInput(fm, "\x1b[15~")
	if !slices.Contains(names(fm), "new") {
		t.Fatalf("F5 left %v", names(fm))
	}
	inner := fm.currentDir
	feedInput(fm, "ma", "h", ";a")
	if fm.currentDir != inner {
		t.Fatalf("; a went to %s, want %s", fm.currentDir, inner)
	}
}
