package main

import (
	"math/rand"
	"testing"

	"github.com/rivo/uniseg"
)

func checkEditingCursor(t *testing.T, state *TermState) {
	t.Helper()
	if state.index == 0 {
		return
	}
	graphemes := uniseg.NewGraphemes(string(state.currentCommand))
	for graphemes.Next() {
		_, end := graphemes.Positions()
		if state.index == ByteOffset(end) {
			return
		}
	}
	t.Fatalf("cursor %d is not a grapheme boundary in %q", state.index, state.currentCommand)
}

func TestReplaceTextGraphemeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name string
		source SourceText
		start, end ByteOffset
		insert string
		want SourceText
		cursor ByteOffset
	}{
		{"insert accent", "ex", 1, 1, "\u0301", "e\u0301x", 3},
		{"insert base before accent", "\u0301x", 0, 0, "e", "e\u0301x", 3},
		{"insert joiner", "👩💻x", 4, 4, "\u200d", "👩‍💻x", 11},
		{"insert before flag", "🇧🇨x", 0, 0, "🇦", "🇦🇧🇨x", 8},
		{"insert inside codepoint", "世x", 1, 1, "a", "世ax", 4},
		{"partial selection", "ae\u0301b", 2, 3, "世", "a世b", 4},
		{"join after replacement", "a\nx", 1, 2, "\u0301", "a\u0301x", 3},
		{"join after deletion", "🇦x🇧", 4, 5, "", "🇦🇧", 8},
		{"clamp insertion", "abc", 99, 99, "x", "abcx", 4},
		{"clamp selection", "abc", -1, 99, "x", "x", 1},
		{"invalid bytes preserved", "\xffx\xfe", 1, 2, "世", "\xff世\xfe", 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := TermState{currentCommand: tt.source, historySearchActive: true}
			state.replaceText(tt.insert, tt.start, tt.end)
			if state.currentCommand != tt.want || state.index != tt.cursor || state.historySearchActive {
				t.Fatalf("got %q at %d, history active %v; want %q at %d", state.currentCommand, state.index, state.historySearchActive, tt.want, tt.cursor)
			}
			checkEditingCursor(t, &state)
		})
	}
}

func TestWordEditingGraphemeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		source SourceText
		cursor, left, right ByteOffset
	}{
		{"abc def", 0, 0, 3},
		{"abc def", 4, 0, 7},
		{"a \u0301b", 1, 0, 5},
		{"a \u0301b", 4, 0, 5},
		{"a \u0301b", 5, 4, 5},
		{"世 👩‍💻 x", 15, 4, 17},
		{"  世", 2, 0, 5},
		{"a\tb c", 4, 0, 5}, // Preserve the existing space-only word convention.
	} {
		for _, token := range []TerminalToken{KEY_ALT_B, KEY_ALT_F, AsciiToken{Char: 23}, AsciiToken{Char: 8}} {
			state := TermState{currentCommand: tt.source, index: tt.cursor}
			end, err := state.HandleToken(token)
			if end || err != nil {
				t.Fatalf("HandleToken: %v, %v", end, err)
			}
			want := tt.source
			cursor := tt.left
			if token == KEY_ALT_F {
				cursor = tt.right
			} else if _, deleting := token.(AsciiToken); deleting {
				want = tt.source[:tt.left] + tt.source[tt.cursor:]
			}
			if state.currentCommand != want || state.index != cursor {
				t.Fatalf("%v in %q at %d: got %q at %d, want %q at %d", token, tt.source, tt.cursor, state.currentCommand, state.index, want, cursor)
			}
			checkEditingCursor(t, &state)
		}
	}
}

func TestCompletionByteRanges(t *testing.T) {
	for _, tt := range []struct {
		source SourceText
		prefix string
		start ByteOffset
	}{
		{"世 abc", "abc", 4},
		{"世\nabc", "abc", 4},
		{"世\n'abc", "abc", 4},
		{"世\n`abc", "abc", 4},
		{"世\nabc ", "", 8},
		{"", "", 0},
	} {
		lexer := NewLexer(string(tt.source), nil)
		lexer.allowUnterminatedString = true
		tokens, err := lexer.Tokenize()
		if err != nil {
			t.Fatal(err)
		}
		token := tokens[len(tokens)-1]
		if len(tokens) > 1 {
			token = tokens[len(tokens)-2]
		}
		prefix, start := completionTokenPrefix(tt.source, ByteOffset(len(tt.source)), token)
		if prefix != tt.prefix || start != tt.start {
			t.Fatalf("%q: prefix %q start %d; want %q, %d", tt.source, prefix, start, tt.prefix, tt.start)
		}
	}
	for _, tt := range []struct { matches []string; want string }{
		{[]string{"é", "ê"}, ""},
		{[]string{"e\u0301x", "e\u0300y"}, ""},
		{[]string{"echo e\u0301x", "echo e\u0301y"}, "echo e\u0301"},
		{[]string{"👩‍💻a", "👩‍🔬b"}, ""},
		{[]string{"abc", "abd"}, "ab"},
	} {
		if got := completionGraphemePrefix(tt.matches); got != tt.want {
			t.Fatalf("prefix of %q = %q, want %q", tt.matches, got, tt.want)
		}
	}
}

func TestCompletionCyclesPreserveJoinedSuffix(t *testing.T) {
	const original = "世 🇧"
	state := TermState{
		currentCommand: original, index: 4,
		tabCycleSource: original, tabCycleStart: 4, tabCycleEnd: 4,
		tabCycleMatches: []string{"🇦", "x"}, tabCycleIndex: -1,
		l: NewLexer("", nil),
	}
	state.cycleTabCompletion(1)
	if state.currentCommand != "世 🇦🇧" || state.index != 12 {
		t.Fatalf("first completion: %q at %d", state.currentCommand, state.index)
	}
	state.cycleTabCompletion(1)
	if state.currentCommand != "世 x🇧" || state.index != 5 {
		t.Fatalf("cycling consumed suffix: %q at %d", state.currentCommand, state.index)
	}
	state.selectTabCompletion(0)
	if state.currentCommand != "世 🇦🇧" || state.index != 12 {
		t.Fatalf("selection lost suffix: %q at %d", state.currentCommand, state.index)
	}

	oldHistory := history
	defer func() { history = oldHistory }()
	history = []string{"echo z", "echo 🇦"}
	state = TermState{currentCommand: original, index: 4}
	for _, want := range []SourceText{"世 🇦🇧", "世 z🇧"} {
		state.cycleLastArgument()
		if state.currentCommand != want {
			t.Fatalf("last-argument cycling: %q, want %q", state.currentCommand, want)
		}
		checkEditingCursor(t, &state)
	}
}

func TestAliasReplacementPreservesSuffix(t *testing.T) {
	oldAliases := aliases
	defer func() { aliases = oldAliases }()
	aliases = map[string]string{"x": "ééé"}
	state := TermState{currentCommand: "世 x suffix", index: 5}
	if _, err := state.HandleToken(AsciiToken{Char: ' '}); err != nil {
		t.Fatal(err)
	}
	if state.currentCommand != "世 ééé  suffix" || state.index != 11 {
		t.Fatalf("alias replacement: %q at %d", state.currentCommand, state.index)
	}
	checkEditingCursor(t, &state)
}

func TestEditingSequenceBoundaries(t *testing.T) {
	rng := rand.New(rand.NewSource(93))
	pieces := []string{"a", " ", "\u0301", "世", "👩", "\u200d", "💻", "🇦", "🇧", "\r", "\n", "\xff"}
	keys := []TerminalToken{KEY_LEFT, KEY_RIGHT, KEY_ALT_B, KEY_ALT_F, KEY_DELETE, AsciiToken{Char: 127}, AsciiToken{Char: 23}, AsciiToken{Char: 11}, AsciiToken{Char: 21}}
	state := TermState{}
	for i := 0; i < 600; i++ {
		if rng.Intn(2) == 0 {
			state.replaceText(pieces[rng.Intn(len(pieces))], state.index, state.index)
		} else if end, err := state.HandleToken(keys[rng.Intn(len(keys))]); end || err != nil {
			t.Fatalf("HandleToken: %v, %v", end, err)
		}
		checkEditingCursor(t, &state)
	}
}
