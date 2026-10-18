package main

import (
	"strings"
	"testing"
	"time"
)

func testIsProgram(word string) bool {
	switch word {
	case "git", "grep", "echo", "sudo":
		return true
	}
	return false
}

func TestFindAbbrSite(t *testing.T) {
	// A '|' in a line marks the cursor; without one it is at the end.
	cases := []struct {
		line      string
		toWordEnd bool
		ok        bool
		word      string
		commandAt bool
		command   string // "" for none
	}{
		{line: "s", ok: true, word: "s", commandAt: true},
		{line: "  s", ok: true, word: "s", commandAt: true},
		{line: "[s", ok: true, word: "s", commandAt: true},
		{line: "[[s", ok: true, word: "s", commandAt: true},
		{line: "[git co] [s", ok: true, word: "s", commandAt: true},
		{line: "[git [s", ok: true, word: "s", commandAt: true},
		{line: "git status ¦ s", ok: true, word: "s", commandAt: true},
		{line: "1 2 ¦ s", ok: true, word: "s"},
		{line: "echo s", ok: true, word: "s", command: "echo"},
		{line: "[git co", ok: true, word: "co", command: "git"},
		{line: "[git -v co", ok: true, word: "co", command: "git"},
		{line: "[git (co", ok: true, word: "co"},
		{line: "{abbr: co", ok: true, word: "co"},
		{line: "[git co] co", ok: true, word: "co"},
		{line: "git log ¦ grep co", ok: true, word: "co", command: "grep"},
		{line: "git log > co", ok: true, word: "co"},
		{line: "git log < co", ok: true, word: "co"},
		{line: "sudo s", ok: true, word: "s", command: "sudo"},
		{line: "/usr/bin/git co", ok: true, word: "co"},
		{line: "git x\ns", ok: true, word: "s", command: "git"},
		{line: "echo 'x' s", ok: true, word: "s", command: "echo"},
		{line: `$"a {s`, ok: true, word: "s"},
		{line: "!!", ok: true, word: "!!", commandAt: true},
		{line: "..", ok: true, word: "..", commandAt: true},
		{line: "echo s", ok: true, word: "s", command: "echo"},
		{line: "s| rest", ok: true, word: "s", commandAt: true},
		{line: "st|atus rest", toWordEnd: true, ok: true, word: "status", commandAt: true},
		{line: "st|atus rest", ok: true, word: "st", commandAt: true},
		{line: "s |", toWordEnd: true, ok: false},
		{line: "echo ", ok: false},
		{line: "", ok: false},
		{line: `"a s`, ok: false},
		{line: `"a" "b s`, ok: false},
		{line: "`a s", ok: false},
		{line: "# s", ok: false},
		{line: "echo 'gco'", ok: false},
		// A combining mark after '[' belongs to the bracket's grapheme.
		{line: "[́s", ok: false},
	}
	for _, c := range cases {
		text, cursor := c.line, -1
		if i := strings.Index(text, "|"); i >= 0 {
			text = text[:i] + text[i+1:]
			cursor = len(strings.ReplaceAll(text[:i], "¦", "|"))
		}
		line := strings.ReplaceAll(text, "¦", "|")
		if cursor < 0 {
			cursor = len(line)
		}
		site, ok := findAbbrSite(SourceText(line), ByteOffset(cursor), c.toWordEnd, testIsProgram)
		if ok != c.ok {
			t.Errorf("%q: ok = %v, want %v", c.line, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if word := line[site.Start:site.End]; word != c.word {
			t.Errorf("%q: word %q, want %q", c.line, word, c.word)
		}
		if site.CommandPosition != c.commandAt {
			t.Errorf("%q: command position %v, want %v", c.line, site.CommandPosition, c.commandAt)
		}
		command := ""
		if site.HasCommand {
			command = site.Command
		}
		if command != c.command {
			t.Errorf("%q: command %q, want %q", c.line, command, c.command)
		}
	}
}

// abbrTestState is an editor holding the abbreviations source adds. Its
// terminal is no file, so running a quote changes no terminal mode.
func abbrTestState(t *testing.T, source string) *TermState {
	t.Helper()
	state := &TermState{stdInFd: -1}
	file, err := parseMShellInput(source, &TokenFile{"test"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	state.stdLibDefs = file.Definitions
	state.context = ExecuteContext{Variables: map[string]MShellObject{}, Pbm: NewPathBinManager()}
	callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
	stack := MShellStack{}
	if result := state.evalState.Evaluate(file.Items, &stack, state.context, file.Definitions, callStackItem); !result.Success {
		t.Fatalf("evaluating %q failed", source)
	}
	return state
}

// typeKeys types text into the editor, a key at a time; '\r' is Enter,
// which expands but does not run the line.
func typeKeys(t *testing.T, state *TermState, text string) {
	t.Helper()
	for _, r := range text {
		if r == '\r' {
			state.expandAbbrOnEnter()
			continue
		}
		// ';' starts a key chord, which reads the next key from the
		// terminal.
		if r == ';' {
			state.typeAbbrTrigger(';')
			continue
		}
		var token TerminalToken = MutliByteToken{Char: r}
		if r < 128 {
			token = AsciiToken{Char: byte(r)}
		}
		if _, err := state.HandleToken(token); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAbbrExpansion(t *testing.T) {
	source := `
def dotCount (AbbrInfo -- Maybe[str])
    :word? len str just
end
{abbr: 's' abbrWord, expansion: 'git status -u' abbrText} abbrAdd
{abbr: 'u' abbrWord, expansion: "'..' cd" abbrText} abbrAdd
{abbr: 'co' abbrWord, expansion: 'checkout' abbrText, position: ['git'] abbrArgOf} abbrAdd
{abbr: 'gcm' abbrWord, expansion: "git commit -m '%'" abbrText, cursorMarker: '%'} abbrAdd
{abbr: 'dt' abbrWord, expansion: 'date +%Y <c>' abbrText, cursorMarker: '<c>'} abbrAdd
{abbr: '\.\.+' abbrRegex, expansion: (dotCount) abbrFunc} abbrAdd
{abbr: 'any' abbrWord, expansion: 'ANY' abbrText, position: abbrAnywhere} abbrAdd
{abbr: 'dec' abbrWord, expansion: 'old' abbrText} abbrAdd
{abbr: 'dec' abbrWord, expansion: (drop none) abbrFunc, position: abbrAnywhere} abbrAdd
{abbr: 'fail' abbrWord, expansion: 'unused' abbrText, position: abbrAnywhere} abbrAdd
{abbr: 'fail' abbrWord, expansion: (drop 1 exit) abbrFunc} abbrAdd
{abbr: 'a b' abbrWord, expansion: 'never' abbrText} abbrAdd
{abbr: '(' abbrRegex, expansion: 'never' abbrText} abbrAdd
`
	cases := []struct {
		keys   string
		line   string
		cursor int // -1 for the end
	}{
		{"s ", "git status -u ", -1},
		{"s\r", "git status -u", -1},
		{"echo s ", "echo s ", -1},
		{"[s]", "[git status -u]", -1},
		{"[s;", "[git status -u;", -1},
		{"s|", "git status -u|", -1},
		{"git co ", "git checkout ", -1},
		{"[git co]", "[git checkout]", -1},
		{"echo co ", "echo co ", -1},
		{"co ", "co ", -1},
		{"gcm ", "git commit -m ''", len("git commit -m '")},
		{"[gcm]", "[git commit -m '']", len("[git commit -m '")},
		{"gcm\r", "git commit -m ''", len("git commit -m '")},
		{"dt ", "date +%Y ", len("date +%Y ")},
		{"... ", "3 ", -1},
		{"echo ... ", "echo ... ", -1},
		{"echo any ", "echo ANY ", -1},
		{`"any `, `"any `, -1},
		{"# any ", "# any ", -1},
		{"echo 'any' ", "echo 'any' ", -1},
		// The newest declines, so the older one is tried.
		{"dec ", "old ", -1},
		// The newest fails: the word stays, and no other is tried.
		{"fail ", "fail ", -1},
		{"( ", "( ", -1},
		{"u ", "'..' cd ", -1},
	}
	for _, c := range cases {
		state := abbrTestState(t, source)
		typeKeys(t, state, c.keys)
		if string(state.currentCommand) != c.line {
			t.Errorf("%q: line %q, want %q", c.keys, state.currentCommand, c.line)
			continue
		}
		cursor := c.cursor
		if cursor < 0 {
			cursor = len(c.line)
		}
		if int(state.index) != cursor {
			t.Errorf("%q: cursor %d, want %d", c.keys, state.index, cursor)
		}
	}
}

func TestAbbrFailureMessageShownOnce(t *testing.T) {
	state := abbrTestState(t, `{abbr: 'f' abbrWord, expansion: (drop 1 exit) abbrFunc} abbrAdd`)
	typeKeys(t, state, "f ")
	if !strings.Contains(state.abbrMessage, "'f'") {
		t.Fatalf("message %q does not name the abbreviation", state.abbrMessage)
	}
	state.abbrMessage = ""
	state.currentCommand, state.index = "", 0
	typeKeys(t, state, "f ")
	if state.abbrMessage != "" {
		t.Fatalf("the same failure was shown again: %q", state.abbrMessage)
	}
}

func TestAbbrBuiltins(t *testing.T) {
	state := &EvalState{}
	source := `
{abbr: 'l' abbrWord, expansion: 'ls -alh' abbrText} abbrAdd
{abbr: 'l' abbrWord, expansion: 'log' abbrText, position: ['jj'] abbrArgOf} abbrAdd
{abbr: 'l' abbrWord, expansion: 'ls' abbrText} abbrAdd
{abbr: 'l' abbrRegex, expansion: 'regex' abbrText} abbrAdd
{abbr: 'k' abbrWord, expansion: 'kept' abbrText, position: ['jj'] abbrArgOf} abbrAdd
{abbr: 'bad word' abbrWord, expansion: 'x' abbrText} abbrAdd
{abbr: '(' abbrRegex, expansion: 'x' abbrText} abbrAdd
{abbr: '' abbrRegex, expansion: 'x' abbrText} abbrAdd
{abbr: '#x' abbrWord, expansion: 'x' abbrText} abbrAdd
{abbr: 'm' abbrWord, expansion: 'x' abbrText, position: [] abbrArgOf} abbrAdd
{abbr: 'n' abbrWord, expansion: 'x' abbrText, position: ['a b'] abbrArgOf} abbrAdd
{abbr: 'o' abbrWord, expansion: 'x' abbrText, cursorMarker: ''} abbrAdd
'l' abbrWord 'k' abbrWord abbrRename
'missing' abbrWord abbrErase
'missing' abbrWord 'other' abbrWord abbrRename
`
	if r := evalForTest(t, state, source); !r.Success {
		t.Fatal("the builtins failed")
	}
	type want struct {
		word, text string
		inert      bool
	}
	wants := []want{
		{"l", "regex", false},
		{"bad word", "x", true},
		{"(", "x", true},
		{"", "x", true},
		{"#x", "x", true},
		{"m", "x", true},
		{"n", "x", true},
		{"o", "x", false},
		// Renamed in order: the first k replaces the old k at ['jj'].
		{"k", "log", false},
		{"k", "ls", false},
	}
	if len(state.Abbrs) != len(wants) {
		t.Fatalf("%d abbreviations, want %d", len(state.Abbrs), len(wants))
	}
	for i, w := range wants {
		e := state.Abbrs[i]
		if e.word != w.word || e.text != w.text || (e.inert != "") != w.inert {
			t.Errorf("entry %d: %q -> %q inert %q, want %q -> %q inert %v", i, e.word, e.text, e.inert, w.word, w.text, w.inert)
		}
	}
	if len(state.AbbrWarnings) != 6 {
		t.Errorf("%d warnings, want 6: %q", len(state.AbbrWarnings), state.AbbrWarnings)
	}
	if _, ok := state.Abbrs[0].record.Items["position"]; !ok {
		t.Error("a position that was not given is not filled in")
	}
}

func TestAbbrFuncTimeout(t *testing.T) {
	state := abbrTestState(t, `{abbr: 'slow' abbrWord, expansion: (drop 5 sleep none) abbrFunc} abbrAdd`)
	began := time.Now()
	typeKeys(t, state, "slow ")
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("the quote ran %s; it should stop after %s", took, abbrTimeout)
	}
	if state.currentCommand != "slow " {
		t.Fatalf("line %q, want the word left as it was", state.currentCommand)
	}
	if !strings.Contains(state.abbrMessage, "longer than 1 second") {
		t.Fatalf("message %q", state.abbrMessage)
	}
}
