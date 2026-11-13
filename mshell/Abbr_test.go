package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
		var lt lineTokens
		site, ok := abbrSiteAt(&lt, SourceText(line), ByteOffset(cursor), c.toWordEnd)
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

// abbrSiteAt finds the word at the cursor and its position, as expandAbbr
// does.
func abbrSiteAt(lt *lineTokens, line SourceText, cursor ByteOffset, toWordEnd bool) (abbrSite, bool) {
	start, end, ok := abbrWordAt(line, cursor, toWordEnd)
	if !ok {
		return abbrSite{}, false
	}
	return findAbbrSite(lt, line, start, end, testIsProgram)
}

// findAbbrSiteByPrefix is the lookup as it was first written: it lexes the
// line only up to the word's end. It is the reference for
// TestAbbrSiteFromLineTokens.
func findAbbrSiteByPrefix(line SourceText, cursor ByteOffset, toWordEnd bool) (abbrSite, bool) {
	start, end, ok := abbrWordAt(line, cursor, toWordEnd)
	if !ok {
		return abbrSite{}, false
	}
	l := NewLexer(string(line[:end]), &TokenFile{"REPL"})
	l.allowUnterminatedString = true
	l.emitWhitespace = true
	l.emitComments = true
	tokens, err := l.Tokenize()
	if err != nil {
		return abbrSite{}, false
	}
	var before []Token
	var open []int
	lastPipe := -1
	found := false
	var offset ByteOffset
	for _, t := range tokens {
		if t.Type == EOF {
			break
		}
		tokenStart := offset
		offset += ByteOffset(len(t.Lexeme))
		if tokenStart >= start {
			found = tokenStart == start && !isAbbrTextToken(t.Type)
			break
		}
		if offset > start {
			return abbrSite{}, false
		}
		switch t.Type {
		case WHITESPACE, LINECOMMENT:
			continue
		case LEFT_SQUARE_BRACKET, LEFT_PAREN, LEFT_CURLY, GRID_OPEN:
			open = append(open, len(before))
		case RIGHT_SQUARE_BRACKET, RIGHT_PAREN, RIGHT_CURLY, GRID_CLOSE:
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		case PIPE:
			if len(open) == 0 {
				lastPipe = len(before)
			}
		}
		before = append(before, t)
	}
	if !found {
		return abbrSite{}, false
	}
	site := abbrSite{Start: start, End: end}
	if len(before) == 0 {
		site.CommandPosition = true
		return site, true
	}
	commandLine := before[0].Type == LITERAL && testIsProgram(before[0].Lexeme)
	prev := before[len(before)-1].Type
	if prev == LEFT_SQUARE_BRACKET || (prev == PIPE && commandLine) {
		site.CommandPosition = true
		return site, true
	}
	head := -1
	switch {
	case len(open) > 0:
		if i := open[len(open)-1]; before[i].Type == LEFT_SQUARE_BRACKET {
			head = i + 1
		}
	case commandLine && prev != LESSTHAN && prev != GREATERTHAN:
		head = lastPipe + 1
	}
	if head >= 0 && head < len(before) && before[head].Type == LITERAL {
		site.Command, site.HasCommand = before[head].Lexeme, true
	}
	return site, true
}

// TestAbbrSiteFromLineTokens checks the lookup on the line's tokens
// against lexing the line up to the word, at every cursor of random lines.
// They may differ only where the word ends inside a longer token of the
// whole line: then the lookup judges the token as it is painted.
func TestAbbrSiteFromLineTokens(t *testing.T) {
	pieces := []string{
		"s", "co", "git", "echo", "grep", "sudo", "x", "1", "99999999999999999999999", "-", "-5", ".", "..", "/usr/bin/git",
		" ", " ", " ", "\n", "\t", "[", "]", "(", ")", "{", "}", "|", ";", "<", ">", "#", "'", "\"", "`", "$\"", "{", "}",
		"\\", "é", "\u0301", "世", "?", "!", "@", ":", ",", "[|", "|]", "2024-01-02", "true", "abbr:", "@x", "x!", "%",
	}
	rng := rand.New(rand.NewSource(1))
	var lt lineTokens
	cases, cut := 0, 0
	for n := 0; n < 10000; n++ {
		var b strings.Builder
		for k := rng.Intn(12) + 1; k > 0; k-- {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		line := SourceText(b.String())
		for cursor := 0; cursor <= len(line); cursor++ {
			if cursor < len(line) && !utf8.RuneStart(line[cursor]) {
				continue
			}
			for _, toWordEnd := range []bool{false, true} {
				cases++
				want, wantOk := findAbbrSiteByPrefix(line, ByteOffset(cursor), toWordEnd)
				got, gotOk := abbrSiteAt(&lt, line, ByteOffset(cursor), toWordEnd)
				if got == want && gotOk == wantOk {
					continue
				}
				if wordCutsToken(&lt, line, ByteOffset(cursor), toWordEnd) {
					cut++
					continue
				}
				t.Fatalf("%q cursor %d toWordEnd %v: %+v %v, lexing up to the word gives %+v %v", line, cursor, toWordEnd, got, gotOk, want, wantOk)
			}
		}
	}
	t.Logf("%d cases, %d differ where the word ends inside a token", cases, cut)
}

// wordCutsToken reports whether the word ends inside a token of the
// whole line: the line's token at the word's last byte runs past it, or a
// lex error starts in the word (a number too large to read that the cursor
// is in). Lexing only up to the word's end sees that token cut short.
func wordCutsToken(lt *lineTokens, line SourceText, cursor ByteOffset, toWordEnd bool) bool {
	start, end, ok := abbrWordAt(line, cursor, toWordEnd)
	if !ok {
		return false
	}
	lt.get(line)
	if lt.errAt >= start && lt.errAt < end {
		return true
	}
	var offset ByteOffset
	for _, t := range lt.tokens {
		tokenStart := offset
		offset += ByteOffset(len(t.Lexeme))
		if tokenStart < end && offset >= end {
			return offset > end
		}
	}
	return false
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
