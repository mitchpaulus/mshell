package main

import (
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"
)

// keyPathState is an editor as the interactive shell has it before the
// first key, with the abbreviations source adds and history synthetic
// history entries.
func keyPathState(tb testing.TB, source string, history int) *TermState {
	tb.Helper()
	state := &TermState{stdInFd: -1}
	file, err := parseMShellInput(source, &TokenFile{"test"})
	if err != nil {
		tb.Fatal(err)
	}
	state.stdLibDefs = file.Definitions
	state.context = ExecuteContext{Variables: map[string]MShellObject{}, Pbm: NewPathBinManager()}
	stack := MShellStack{}
	if r := state.evalState.Evaluate(file.Items, &stack, state.context, file.Definitions, CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}); !r.Success {
		tb.Fatalf("evaluating %q failed", source)
	}
	rng := rand.New(rand.NewSource(1))
	heads := []string{"git status", "git log --oneline", "cd ", "ls -la ", "nvim ", "make ", "go test ./...", "echo ", "cat ", "grep -rn "}
	for i := range history {
		cmd := heads[rng.Intn(len(heads))] + fmt.Sprintf("%x", rng.Int63())
		state.previousHistory = append(state.previousHistory, HistoryItem{UnixTimeUtc: int64(i), Directory: "/home/u/repo", Command: cmd})
	}
	return state
}

const keyPathAbbrs = `
{abbr: 'gl' abbrWord, expansion: 'git log' abbrText} abbrAdd
{abbr: 's' abbrWord, expansion: 'git status -u' abbrText} abbrAdd
{abbr: 'co' abbrWord, expansion: 'checkout' abbrText, position: ['git'] abbrArgOf} abbrAdd
{abbr: '\.\.+' abbrRegex, expansion: "'..' cd" abbrText} abbrAdd
`

// keyPathMedium and keyPathLong are lines already typed, of 93 and 1,850
// bytes, that a case types more onto.
const keyPathMedium = "[git log --oneline -n 20] | [grep -i 'fix' ] | [head -5] ; echo done && ls -la /usr/local/bin"

var keyPathLong = strings.Repeat("[git log --oneline -n 20] | [grep -i 'fix'] ; ", 40) + "echo final"

// keyPathCases are keys typed onto a line a key at a time: a plain line,
// one whose first word expands, both with a history suggestion painted,
// and words typed at the end of longer lines.
var keyPathCases = []struct {
	name    string
	source  string
	history int
	prefix  string
	keys    string
}{
	{"plain", "", 0, "", "git log --oneline | grep fix"},
	{"abbrs", keyPathAbbrs, 0, "", "gl --oneline | grep fix"},
	{"history", "", 2000, "", "git log --oneline | grep fix"},
	{"abbrs+history", keyPathAbbrs, 2000, "", "gl --oneline | grep fix"},
	{"medium", keyPathAbbrs, 0, keyPathMedium, " nomatch"},
	{"long", keyPathAbbrs, 0, keyPathLong, " nomatch"},
}

// startLine puts prefix on the line and paints it, as typing it would
// have.
func startLine(tb testing.TB, state *TermState, in *StdinReaderState, region *ProbeRegion, read func() (TerminalToken, error), prefix string) {
	state.currentCommand, state.index = SourceText(prefix), ByteOffset(len(prefix))
	if ready, err := state.refreshCommandDisplay(io.Discard, read, region); !ready || err != nil {
		tb.Fatal(err)
	}
	in.i = 0
}

// typeLine types the keys onto the line through the editor's path for
// each key: read the key, handle it, find the history suggestion, and
// repaint into io.Discard.
func typeLine(tb testing.TB, state *TermState, in *StdinReaderState, region *ProbeRegion, read func() (TerminalToken, error), suggest bool) {
	for range in.n {
		token, err := state.readInputToken()
		if err != nil {
			tb.Fatal(err)
		}
		state.HandleToken(token)
		state.showSuggestion = suggest
		if suggest {
			state.updateHistoryCompletion()
		}
		if ready, err := state.refreshCommandDisplay(io.Discard, read, region); !ready || err != nil {
			tb.Fatal(err)
		}
	}
}

func keyPathSetup(tb testing.TB, source string, history int, keys string) (*TermState, *StdinReaderState, *ProbeRegion, func() (TerminalToken, error)) {
	state := keyPathState(tb, source, history)
	in := &StdinReaderState{array: []byte(keys), n: len(keys)}
	state.stdInState = in
	region := &ProbeRegion{OriginRow: 1, OriginCol: 3, PaintedRows: 1, ScreenRows: 100, Columns: 80}
	read := func() (TerminalToken, error) { tb.Fatal("an ASCII repaint asked the terminal"); return nil, io.EOF }
	return state, in, region, read
}

// TestKeyPathAllocations keeps typing allocation-free: the line's text and
// tokens, the suggestion and the abbreviation lookup all reuse the
// editor's buffers. A new arena chunk every few thousand edits rounds to
// zero per line.
func TestKeyPathAllocations(t *testing.T) {
	for _, c := range keyPathCases {
		state, in, region, read := keyPathSetup(t, c.source, c.history, c.keys)
		allocs := testing.AllocsPerRun(50, func() {
			startLine(t, state, in, region, read, c.prefix)
			typeLine(t, state, in, region, read, c.history > 0)
		})
		if allocs != 0 {
			t.Errorf("%s: typing %q allocates %v times", c.name, c.keys, allocs)
		}
		if c.source != "" && c.prefix == "" && string(state.currentCommand) != "git log --oneline | grep fix" {
			t.Errorf("%s: typed %q", c.name, state.currentCommand)
		}
	}
}

// BenchmarkKeyPath reports the cost of a key on the editor's path, per key.
func BenchmarkKeyPath(b *testing.B) {
	for _, c := range keyPathCases {
		b.Run(c.name, func(b *testing.B) {
			state, in, region, read := keyPathSetup(b, c.source, c.history, c.keys)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				startLine(b, state, in, region, read, c.prefix)
				b.StartTimer()
				typeLine(b, state, in, region, read, c.history > 0)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(c.keys)), "ns/key")
		})
	}
}
