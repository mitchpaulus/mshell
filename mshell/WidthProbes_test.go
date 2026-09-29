package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Model cursor motion and the furthest prefix advance independently of layout.
// At CPR a cluster can shrink to its observed final advance, as real terminals
// can do when a later codepoint combines with an earlier one.
type probeTerminal struct {
	t *testing.T
	row, col, rows, columns int
	scrolls int
	widths map[string]int
	replies []TerminalToken
	writes []string
	text string
	scratchDirty bool
}

func (term *probeTerminal) Write(output []byte) (int, error) {
	term.writes = append(term.writes, string(output))
	for rest := string(output); len(rest) > 0; {
		switch rest[0] {
		case '\r':
			term.col = 1
			term.text = ""
			rest = rest[1:]
		case '\n':
			term.row++
			if term.row > term.rows {
				term.row = term.rows
				term.scrolls++
			}
			rest = rest[1:]
		case '\x1b':
			end := 2
			for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
				end++
			}
			if len(rest) < 3 || rest[1] != '[' || end == len(rest) {
				term.t.Fatalf("invalid output escape %q", rest)
			}
			n, _ := strconv.Atoi(rest[2:end])
			switch rest[end] {
			case 'A': term.row -= n
			case 'B': term.row += n
			case 'G': term.col = n
			case 'K': term.scratchDirty = false
			case 'n':
				width, ok := term.widths[term.text]
				if !ok || n != 6 {
					term.t.Fatalf("unexpected probe %q / CSI %dn", term.text, n)
				}
				term.col = width + 1
				term.replies = append(term.replies, CsiToken{FinalChar: 'R', Params: []byte(fmt.Sprintf("%d;%d", term.row, term.col))})
			default: term.t.Fatalf("unexpected CSI %q", rest[:end+1])
			}
			rest = rest[end+1:]
		default:
			r, size := utf8.DecodeRuneInString(rest)
			advance := 1
			if r > 127 { advance = 2 }
			term.col += advance
			if term.col > term.columns {
				term.t.Fatalf("probe prefix filled the margin: %q", string(output))
			}
			term.text += rest[:size]
			term.scratchDirty = true
			rest = rest[size:]
		}
		if term.row < 1 || term.row > term.rows {
			term.t.Fatalf("cursor outside screen at row %d", term.row)
		}
	}
	return len(output), nil
}

func TestProbeTransactionGeometryAndReuse(t *testing.T) {
	for _, origin := range []OneBasedTerminalCoord{2, 4} {
		state := TermState{}
		region := ProbeRegion{OriginRow: origin, OriginCol: 7, CursorRow: 1, PaintedRows: 2, ScreenRows: 5, Columns: 15}
		terminal := &probeTerminal{t: t, row: int(origin)+1, col: 15, rows: 5, columns: 15, widths: map[string]int{"👨‍👩‍👧‍👦": 8, "é": 1, "世": 2}}
		batch := WidthProbeBatch{}
		reads := 0
		read := func() (TerminalToken, error) {
			if len(terminal.replies) != 2-reads { t.Fatal("read began before all probes were written") }
			token := terminal.replies[0]
			terminal.replies = terminal.replies[1:]
			reads++
			return token, nil
		}
		err := state.measureWidths(terminal, read, &region, &batch, []string{"👨‍👩‍👧‍👦", "é", "é"})
		if err != nil || batch.Failure != nil || reads != 2 || len(terminal.writes) != 2 { t.Fatalf("transaction failed: %v, %+v", err, batch) }
		if state.widthCache.Entries["👨‍👩‍👧‍👦"] != 8 || state.widthCache.Entries["é"] != 1 { t.Fatal("measurements not cached") }
		wantOrigin := origin
		wantScroll := 0
		if origin == 4 { wantOrigin--; wantScroll = 1 }
		if region.OriginRow != wantOrigin || region.CursorRow != 0 || terminal.row != int(wantOrigin) || terminal.col != 7 || terminal.scrolls != wantScroll || terminal.scratchDirty {
			t.Fatalf("incorrect recovery: region %+v, terminal %+v", region, terminal)
		}
		// A later batch reuses the cleared row, without another newline or scroll.
		read = func() (TerminalToken, error) { return terminal.replies[0], nil }
		if err := state.measureWidths(terminal, read, &region, &batch, []string{"世"}); err != nil { t.Fatal(err) }
		if strings.Contains(terminal.writes[2], "\n") || terminal.scrolls != wantScroll { t.Fatal("scratch reuse scrolled") }
	}
}

func TestProbeCandidateEmissionGates(t *testing.T) {
	for _, columns := range []Cells{3, 4, 5, 14, 15} {
		state := TermState{}
		region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: columns}
		batch := WidthProbeBatch{}
		var output bytes.Buffer
		candidates := []string{"ascii", "\x1b", "\n", "\t", "\x00", "\u0085", "\xff", "\u0301", strings.Repeat("é", 257), "é", "é", "e\u0301", "👨‍👩‍👧‍👦"}
		read := func() (TerminalToken, error) { return CsiToken{FinalChar: 'R', Params: []byte("2;2")}, nil }
		if err := state.measureWidths(&output, read, &region, &batch, candidates); err != nil { t.Fatal(err) }
		want := []string{}
		if columns >= 4 { want = append(want, "é") }
		if columns >= 5 { want = append(want, "e\u0301") }
		if columns >= 15 { want = append(want, "👨‍👩‍👧‍👦") }
		if !slices.Equal(batch.Candidates, want) { t.Fatalf("columns %d: candidates %q, want %q", columns, batch.Candidates, want) }
		if strings.Count(output.String(), "\033[6n") != len(want) { t.Fatal("wrong request count") }
		if columns < 4 && output.Len() != 0 { t.Fatal("painted below minimum width") }
	}
	batch := WidthProbeBatch{}
	cache := WidthCache{}
	candidates := make([]string, 300)
	for i := range candidates { candidates[i] = string(rune(0x4e00+i)) }
	batch.prepare(candidates, 80, &cache, &CandidateEligibilityCache{})
	if len(batch.Candidates) != maxWidthProbes { t.Fatal("probe budget not enforced") }
	cache.Entries = make(map[string]Cells)
	for i := 0; i < maxWidthCacheEntries-1; i++ { cache.Entries[fmt.Sprint(i)] = 1 }
	batch.prepare(candidates, 80, &cache, &CandidateEligibilityCache{})
	if len(batch.Candidates) != 1 { t.Fatal("cache capacity not reserved") }
}

func TestProbeRejectionDrainsAndBlocks(t *testing.T) {
	for _, bad := range []string{"bad", "3;2", "2;1", "2;4"} {
		state := TermState{widthCache: WidthCache{Entries: map[string]Cells{"old": 1}}, queuedInput: []TerminalToken{AsciiToken{Char: 'a'}}}
		region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 15}
		batch := WidthProbeBatch{}
		var output bytes.Buffer
		tokens := []TerminalToken{AsciiToken{Char: 'b'}, CsiToken{FinalChar: 'R', Params: []byte("2;2")}, CsiToken{FinalChar: 'R', Params: []byte(bad)}, AsciiToken{Char: 'c'}, CsiToken{FinalChar: 'R', Params: []byte("2;3")}}
		read := func() (TerminalToken, error) { token := tokens[0]; tokens = tokens[1:]; return token, nil }
		if err := state.measureWidths(&output, read, &region, &batch, []string{"é", "世", "界"}); err != nil { t.Fatal(err) }
		if len(tokens) != 0 || batch.RepliesReceived != 3 || batch.Failure == nil || !state.widthProbesBlocked || len(state.widthCache.Entries) != 1 { t.Fatalf("failed batch not drained/isolated: %+v", batch) }
		for _, char := range []byte{'a', 'b', 'c'} {
			token, err := state.readInputToken()
			if err != nil || token != (AsciiToken{Char: char}) { t.Fatalf("queued key order: %v, %v", token, err) }
		}
		count := output.Len()
		if err := state.measureWidths(&output, read, &region, &batch, []string{"é"}); err != nil || output.Len() != count { t.Fatal("retried blocked command") }
		// The next prompt can retry only after the old reports have drained.
		state.widthProbesBlocked = false
		if err := state.measureWidths(&output, func() (TerminalToken, error) { return CsiToken{FinalChar: 'R', Params: []byte("2;2")}, nil }, &region, &batch, []string{"é"}); err != nil || batch.Failure != nil { t.Fatal("next command could not retry") }
	}
}

type failingProbeWriter struct { calls int; failAt int; short bool }
func (w *failingProbeWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		if w.short { return len(p)-1, nil }
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func TestProbeIOFailureCleanup(t *testing.T) {
	for _, failure := range []string{"write", "short write", "read", "eof", "cleanup"} {
		state := TermState{}
		region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 15}
		batch := WidthProbeBatch{}
		writer := &failingProbeWriter{}
		if failure == "write" || failure == "short write" { writer.failAt = 1; writer.short = failure == "short write" }
		if failure == "cleanup" { writer.failAt = 2 }
		read := func() (TerminalToken, error) {
			if failure == "write" || failure == "short write" { t.Fatal("read after failed write") }
			if failure == "read" { return nil, io.ErrUnexpectedEOF }
			if failure == "eof" { return EofTerminalToken{}, nil }
			return CsiToken{FinalChar: 'R', Params: []byte("2;2")}, nil
		}
		err := state.measureWidths(writer, read, &region, &batch, []string{"é"})
		if err == nil || batch.Failure == nil || writer.calls != 2 || len(state.widthCache.Entries) != 0 || !state.widthProbesBlocked { t.Fatalf("%s: no cleanup or committed failed batch: %v, %+v", failure, err, batch) }
		if failure == "short write" && !errors.Is(err, io.ErrShortWrite) { t.Fatalf("short write: %v", err) }
	}
}

func TestProbeCommitIsAtomic(t *testing.T) {
	cache := WidthCache{Entries: map[string]Cells{"世": 1}}
	batch := WidthProbeBatch{Candidates: []string{"é", "世"}, Widths: []Cells{1, 2}, RepliesReceived: 2}
	if batch.commit(&cache) == nil || len(cache.Entries) != 1 || cache.Entries["世"] != 1 { t.Fatal("conflicting batch partially committed") }
}

func TestProbeQueryFenceAndRegionValidation(t *testing.T) {
	valid := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 15}
	for _, region := range []ProbeRegion{
		{OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 15}, // No anchor.
		{OriginRow: 1, OriginCol: 1, PaintedRows: 5, ScreenRows: 5, Columns: 15}, // No room for a visible scratch row.
		{OriginRow: 5, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 15, ScratchOwned: true},
	} {
		state := TermState{}
		batch := WidthProbeBatch{}
		var output bytes.Buffer
		read := func() (TerminalToken, error) { t.Fatal("read without a usable region"); return nil, io.EOF }
		if state.measureWidths(&output, read, &region, &batch, []string{"é"}) == nil || output.Len() != 0 || len(batch.Candidates) != 0 {
			t.Fatal("invalid region emitted probes or left replies outstanding")
		}
	}
	state := TermState{}
	batch := WidthProbeBatch{Candidates: []string{"é", "世"}, RepliesReceived: 1, Failure: fmt.Errorf("rejected")}
	var output bytes.Buffer
	read := func() (TerminalToken, error) { t.Fatal("new query read before previous batch drained"); return nil, io.EOF }
	if state.measureWidths(&output, read, &valid, &batch, []string{"界"}) == nil || output.Len() != 0 || batch.RepliesReceived != 1 {
		t.Fatal("new query bypassed outstanding reply fence")
	}
	state.widthCache.remember("é", 1)
	batch.Clear()
	if err := state.measureWidths(&output, read, &valid, &batch, []string{"é"}); err != nil || output.Len() != 0 {
		t.Fatal("cached candidate generated terminal traffic")
	}
}
