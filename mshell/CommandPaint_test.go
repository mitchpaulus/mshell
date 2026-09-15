package main

import (
	"slices"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// A cell screen independent of LayoutResult. It implements delayed autowrap,
// scrolling, erasure and cursor controls from the emitted byte stream. Cluster
// advances come from a test terminal oracle, never from the layout under test.
type commandScreen struct {
	t *testing.T
	cells [][]string
	row, col int
	pending bool
	scrolls, naturalWraps int
	widths map[string]int
	writes []string
	replies []TerminalToken
	styles []string
}

func newCommandScreen(t *testing.T, rows, columns int) *commandScreen {
	screen := &commandScreen{t: t, cells: make([][]string, rows), widths: map[string]int{"世": 2, "é": 1, "e\u0301": 1, tabGlyph: 1, "👨‍👩‍👧‍👦": 8}}
	for i := range screen.cells { screen.cells[i] = strings.Split(strings.Repeat(" ", columns), "") }
	return screen
}

func (screen *commandScreen) lineFeed() {
	screen.pending = false
	if screen.row+1 < len(screen.cells) { screen.row++; return }
	copy(screen.cells, screen.cells[1:])
	screen.cells[len(screen.cells)-1] = strings.Split(strings.Repeat(" ", len(screen.cells[0])), "")
	screen.scrolls++
}

func (screen *commandScreen) Write(output []byte) (int, error) {
	screen.writes = append(screen.writes, string(output))
	columns := len(screen.cells[0])
	for rest := string(output); len(rest) > 0; {
		switch rest[0] {
		case '\r':
			screen.pending, screen.col = false, 0
			rest = rest[1:]
		case '\n':
			screen.lineFeed()
			rest = rest[1:]
		case '\x1b':
			if !strings.HasPrefix(rest, "\x1b[") { screen.t.Fatalf("non-CSI output: %q", rest) }
			end := 2
			for end < len(rest) && (rest[end] >= '0' && rest[end] <= '9' || rest[end] == ';') { end++ }
			if end == len(rest) { screen.t.Fatal("incomplete CSI") }
			n, _ := strconv.Atoi(rest[2:end])
			if rest[end] == 'm' { screen.styles = append(screen.styles, rest[2:end]) }
			switch rest[end] {
			case 'A': screen.row = max(0, screen.row-max(1, n)); screen.pending = false
			case 'B': screen.row = min(len(screen.cells)-1, screen.row+max(1, n)); screen.pending = false
			case 'G': screen.col = min(columns-1, max(1, n)-1); screen.pending = false
			case 'K':
				start := screen.col
				if n == 2 { start = 0 } else if n != 0 { screen.t.Fatal("unsupported erase") }
				for col := start; col < columns; col++ { screen.cells[screen.row][col] = " " }
			case 'm':
			case 'n':
				if n != 6 || screen.pending { screen.t.Fatal("invalid query or probe touched margin") }
				screen.replies = append(screen.replies, CsiToken{FinalChar: 'R', Params: []byte(fmt.Sprintf("%d;%d", screen.row+1, screen.col+1))})
			default: screen.t.Fatalf("unexpected CSI: %q", rest[:end+1])
			}
			rest = rest[end+1:]
		default:
			cluster, remaining, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
			width := 1
			if len(cluster) != 1 || cluster[0] > 127 {
				var ok bool
				width, ok = screen.widths[cluster]
				if !ok { screen.t.Fatalf("unmeasured text emitted: %q", cluster) }
				// Simulate a wider intermediate prefix even if the final advance
				// contracts. No measured cluster may reach the margin at any point.
				if screen.pending || columns-screen.col <= 2*utf8.RuneCountInString(cluster) { screen.t.Fatalf("unsafe cluster placement: %q at column %d", cluster, screen.col) }
			} else if cluster[0] < 32 || cluster[0] == 127 {
				screen.t.Fatalf("raw source control: %q", cluster)
			}
			if screen.pending { screen.col = 0; screen.lineFeed(); screen.naturalWraps++ }
			if screen.col+width > columns { screen.t.Fatal("text wrapped internally") }
			screen.cells[screen.row][screen.col] = cluster
			for i := 1; i < width; i++ { screen.cells[screen.row][screen.col+i] = "·" }
			screen.col += width
			if screen.col == columns { screen.col--; screen.pending = true }
			rest = remaining
		}
	}
	return len(output), nil
}

func (screen *commandScreen) read() (TerminalToken, error) {
	if len(screen.replies) == 0 { screen.t.Fatal("read without an outstanding report") }
	token := screen.replies[0]
	screen.replies = screen.replies[1:]
	return token, nil
}

func (screen *commandScreen) line(row int) string { return strings.Join(screen.cells[row], "") }

func TestCommandPaintWrapsAndCursor(t *testing.T) {
	for _, tt := range []struct {
		source SourceText
		columns int
		lines []string
		wraps int
	}{
		{"abcdef", 4, []string{">abc", "def "}, 1},
		{"abc", 4, []string{">abc", "    "}, 0},
		{"abc\n", 4, []string{">abc", "    "}, 0},
		{"abc世", 4, []string{">abc", "世·  "}, 0},
		{"ab\x1b", 4, []string{">ab ", "^[  "}, 0},
		{"abc\x1b", 4, []string{">abc", "^[  "}, 0},
		{"abc\té", 4, []string{">abc", tabGlyph+"é  "}, 0},
		{"e\u0301", 5, []string{">    ", "e\u0301    "}, 0},
		{"\x1b[2J\u0085", 8, []string{">^[[2J? "}, 0},
		{"👨‍👩‍👧‍👦", 15, []string{">              ", "👨‍👩‍👧‍👦·······       "}, 0},
	} {
		t.Run(string(tt.source), func(t *testing.T) {
			for cursor := ByteOffset(0); ; cursor = graphemeNext(tt.source, cursor) {
				state := TermState{currentCommand: tt.source, index: cursor}
				screen := newCommandScreen(t, 6, tt.columns)
				screen.cells[2][0] = ">"
				screen.row, screen.col = 2, 1
				for text, width := range screen.widths { state.widthCache.remember(text, Cells(width)) }
				region := ProbeRegion{OriginRow: 3, OriginCol: 2, PaintedRows: 1, ScreenRows: 6, Columns: Cells(tt.columns)}
				ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
				if !ready || err != nil { t.Fatalf("paint: %v", err) }
				for i, want := range tt.lines {
					if got := screen.line(2+i); got != want { t.Fatalf("cursor %d, line %d: %q, want %q", cursor, i, got, want) }
				}
				cursorRow, cursorCol := state.displayLayout.CursorRow, state.displayLayout.CursorCol
				if cursorCol == Cells(tt.columns) { cursorRow++; cursorCol = 0 }
				if screen.row != 2+int(cursorRow) || screen.col != int(cursorCol) || screen.pending || screen.naturalWraps != tt.wraps || screen.scrolls != 0 { t.Fatalf("cursor/wrap mismatch: screen %+v, layout %+v", screen, state.displayLayout) }
				if len(screen.writes) != 1 { t.Fatal("cached frame was not one write") }
				if cursor == ByteOffset(len(tt.source)) { break }
			}
		})
	}
}

func TestCommandPaintGrowShrinkAndScratch(t *testing.T) {
	screen := newCommandScreen(t, 5, 8)
	screen.cells[4][0], screen.cells[4][1] = ">", " "
	screen.cells[0][0] = "H"
	screen.row, screen.col = 4, 2
	region := ProbeRegion{OriginRow: 5, OriginCol: 3, PaintedRows: 1, ScreenRows: 5, Columns: 8}
	state := TermState{currentCommand: "abcdefghijklmn", index: 14}
	ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil { t.Fatal(err) }
	if screen.scrolls != 2 || region.OriginRow != 3 || region.PaintedRows != 3 || screen.line(2) != "> abcdef" || screen.line(3) != "ghijklmn" || screen.line(4) != "        " || screen.line(0) != "        " { t.Fatalf("growth bookkeeping: %+v, screen %+v", region, screen.cells) }

	// Shrink, then measure a miss using the cleared row as scratch. Neither
	// operation may scroll or erase the preserved prompt prefix.
	state.currentCommand, state.index = "x", 1
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || region.PaintedRows != 1 || !region.ScratchOwned { t.Fatalf("shrink: %+v, %v", region, err) }
	for row := 3; row < 5; row++ { if screen.line(row) != "        " { t.Fatal("stale text after shrink") } }
	state.currentCommand, state.index = "世", 3
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || state.widthCache.Entries["世"] != 2 || screen.line(2) != "> 世·    " || screen.scrolls != 2 || !region.ScratchOwned { t.Fatalf("probe/repaint: %+v, %v", region, err) }
	if len(screen.writes) != 5 { t.Fatalf("expected two paints, probe/cleanup and final paint, got %d writes", len(screen.writes)) }
}

func TestCommandRefreshQueuesKeysBeforePaint(t *testing.T) {
	screen := newCommandScreen(t, 4, 8)
	screen.row, screen.col = 3, 2
	screen.cells[3][0] = ">"
	state := TermState{currentCommand: "世", index: 3}
	region := ProbeRegion{OriginRow: 4, OriginCol: 3, PaintedRows: 1, ScreenRows: 4, Columns: 8}
	queued := false
	ready, err := state.refreshCommandDisplay(screen, func() (TerminalToken, error) {
		if !queued { queued = true; return AsciiToken{Char: 'x'}, nil }
		return screen.read()
	}, &region)
	if ready || err != nil || len(screen.writes) != 2 || state.widthCache.Entries["世"] != 2 { t.Fatalf("obsolete frame painted: %v", err) }
	if screen.line(2) != ">       " || screen.line(3) != "        " { t.Fatal("probe damaged visible frame") }
	token, err := state.readInputToken()
	if err != nil { t.Fatal(err) }
	state.PushChars([]rune{rune(token.(AsciiToken).Char)})
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || screen.line(2) != "> 世·x   " || len(screen.writes) != 3 { t.Fatalf("latest frame: %v", err) }
}

func TestCommandPaintRefusesStaleFrames(t *testing.T) {
	state := TermState{currentCommand: "hello", index: 5}
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 2, Columns: 4}
	state.prepareCommandDisplay(0, 4, nil)
	before := region
	for _, change := range []func(){
		func() { state.currentCommand = "other" },
		func() { state.index = 0 },
		func() { region.Columns = 5 },
		func() { region.OriginCol = 2 },
		func() { state.queuedInput = []TerminalToken{AsciiToken{Char: 'a'}} },
	} {
		state.currentCommand, state.index, state.queuedInput, region = "hello", 5, nil, before
		change()
		writer := &failingProbeWriter{failAt: 1}
		if err := state.paintCommandDisplay(writer, &region); err == nil || writer.calls != 0 { t.Fatalf("stale frame reached terminal: %v", err) }
	}
}

func TestCommandRefreshFullScreenAndNarrow(t *testing.T) {
	screen := newCommandScreen(t, 2, 4)
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 2, ScreenRows: 2, Columns: 4}
	state := TermState{currentCommand: "世", index: 3}
	ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || screen.line(0) != "?   " || state.widthProbesBlocked || !region.ScratchOwned { t.Fatalf("full screen did not free scratch with a placeholder: %v", err) }
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || screen.line(0) != "世·  " { t.Fatalf("freed scratch not usable: %v", err) }
	region.Columns = 3
	writes := len(screen.writes)
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if ready || err != nil || len(screen.writes) != writes || len(state.displayLayout.Rows) != 0 { t.Fatal("narrow screen emitted output") }
}

func TestCommandPaintWriteFailureDoesNotPublishRegion(t *testing.T) {
	for _, short := range []bool{false, true} {
		state := TermState{currentCommand: SourceText(strings.Repeat("x", 80)), index: 80}
		state.prepareCommandDisplay(0, 8, nil)
		region := ProbeRegion{OriginRow: 4, OriginCol: 1, PaintedRows: 1, ScreenRows: 4, Columns: 8}
		before := region
		writer := &failingProbeWriter{failAt: 1, short: short}
		err := state.paintCommandDisplay(writer, &region)
		if err == nil || region != before || writer.calls != 1 { t.Fatalf("partial write published geometry: %v", err) }
		if short && !errors.Is(err, io.ErrShortWrite) { t.Fatal("short write not detected") }
	}
}

func TestCommandViewportFollowsCursorAndKeepsWraps(t *testing.T) {
	screen := newCommandScreen(t, 4, 8)
	screen.cells[3][0], screen.row, screen.col = ">", 3, 2
	region := ProbeRegion{OriginRow: 4, OriginCol: 3, PaintedRows: 1, ScreenRows: 4, Columns: 8}
	state := TermState{currentCommand: "aa\nbb\ncc\ndd\nee"}
	for _, tt := range []struct {
		cursor ByteOffset
		start RowIndex
		lines [3]string
		cursorRow, cursorCol int
	}{
		{14, 2, [3]string{"cc      ", "dd      ", "ee      "}, 2, 2},
		{9, 2, [3]string{"cc      ", "dd      ", "ee      "}, 1, 0},
		{3, 1, [3]string{"bb      ", "cc      ", "dd      "}, 0, 0},
		{0, 0, [3]string{"  aa    ", "bb      ", "cc      "}, 0, 2},
		{14, 2, [3]string{"cc      ", "dd      ", "ee      "}, 2, 2},
	} {
		state.index = tt.cursor
		ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
		if !ready || err != nil { t.Fatalf("cursor %d: %v", tt.cursor, err) }
		if region.ViewportStart != tt.start || region.OriginRow != 1 || region.OriginCol != 1 || region.CommandStartCol != 2 || !region.PromptHidden || !region.ScratchOwned || region.PaintedRows != 3 { t.Fatalf("cursor %d: viewport %+v", tt.cursor, region) }
		for row, want := range tt.lines { if screen.line(row) != want { t.Fatalf("cursor %d row %d: %q, want %q", tt.cursor, row, screen.line(row), want) } }
		if screen.line(3) != "        " || screen.scrolls != 3 || screen.row != tt.cursorRow || screen.col != tt.cursorCol || screen.pending { t.Fatalf("cursor %d: screen %+v", tt.cursor, screen) }
	}
	// A short replacement clears the old window and leaves the prompt hidden.
	state.currentCommand, state.index = "x", 1
	ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || region.ViewportStart != 0 || region.PaintedRows != 1 || !region.PromptHidden || screen.line(0) != "  x     " { t.Fatalf("shrink: %+v, %v", region, err) }
	for row := 1; row < 4; row++ { if screen.line(row) != "        " { t.Fatal("old viewport text survived shrink") } }
	if state.currentCommand != "x" || state.displayStartCol != 2 { t.Fatal("viewport changed source or logical wrapping") }
}

func TestCommandViewportClipsSoftWrapsAndFinalGap(t *testing.T) {
	screen := newCommandScreen(t, 3, 4)
	screen.cells[1][0], screen.row, screen.col = ">", 1, 1
	region := ProbeRegion{OriginRow: 2, OriginCol: 2, PaintedRows: 1, ScreenRows: 3, Columns: 4}
	state := TermState{currentCommand: "abcdefghijklmnopqrs"}
	for _, tt := range []struct {
		cursor ByteOffset
		start RowIndex
		lines [2]string
		row, col int
	}{
		{19, 4, [2]string{"pqrs", "    "}, 1, 0},
		{18, 3, [2]string{"lmno", "pqrs"}, 1, 3},
		{3, 1, [2]string{"defg", "hijk"}, 0, 0},
		{0, 0, [2]string{" abc", "defg"}, 0, 1},
	} {
		state.index = tt.cursor
		ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
		if !ready || err != nil { t.Fatal(err) }
		if region.ViewportStart != tt.start || screen.row != tt.row || screen.col != tt.col || screen.pending { t.Fatalf("cursor %d: region %+v, screen %+v", tt.cursor, region, screen) }
		for row, want := range tt.lines { if screen.line(row) != want { t.Fatalf("cursor %d row %d: %q, want %q", tt.cursor, row, screen.line(row), want) } }
		if screen.scrolls != 1 || screen.line(2) != "    " { t.Fatal("viewport scrolled physical screen or damaged scratch") }
	}
}

func TestCommandViewportProbesWithHiddenPrompt(t *testing.T) {
	screen := newCommandScreen(t, 4, 8)
	screen.row, screen.col = 3, 2
	region := ProbeRegion{OriginRow: 4, OriginCol: 3, PaintedRows: 1, ScreenRows: 4, Columns: 8}
	state := TermState{currentCommand: "a\nb\nc\nd\ne", index: 9}
	if ready, err := state.refreshCommandDisplay(screen, screen.read, &region); !ready || err != nil { t.Fatal(err) }
	state.currentCommand = "a\nb\nc\n123456世\nz"
	state.index = state.commandEnd()
	ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || state.widthCache.Entries["世"] != 2 { t.Fatalf("viewport probe failed: %v", err) }
	for row, want := range []string{"123456  ", "世·      ", "z       ", "        "} {
		if screen.line(row) != want { t.Fatalf("row %d: %q, want %q", row, screen.line(row), want) }
	}
	if region.ViewportStart != 3 || region.CommandStartCol != 2 || state.displayStartCol != 2 || !region.ScratchOwned || screen.scrolls != 3 || len(screen.writes) != 4 { t.Fatalf("probe disturbed viewport/geometry: %+v", region) }
	// Going back to the beginning uses the original prompt offset, while
	// probe cleanup and physical cursor movement continue to use column one.
	state.index = 0
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || screen.line(0) != "  a     " || screen.col != 2 || region.ViewportStart != 0 || len(screen.writes) != 5 { t.Fatalf("return to hidden prompt: %v", err) }
}

func TestCommandViewportOneRowTerminal(t *testing.T) {
	screen := newCommandScreen(t, 1, 4)
	region := ProbeRegion{OriginRow: 1, OriginCol: 2, PaintedRows: 1, ScreenRows: 1, Columns: 4}
	state := TermState{}
	for _, tt := range []struct { source SourceText; cursor ByteOffset; text string; col int }{
		{"abcdef", 6, "def ", 3},
		{"abcdef", 0, " abc", 1},
		{"abc", 3, "    ", 0},
		{"世", 3, " ?  ", 2},
	} {
		state.currentCommand, state.index = tt.source, tt.cursor
		ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
		if !ready || err != nil || screen.line(0) != tt.text || screen.col != tt.col || screen.row != 0 || screen.pending || region.ScratchOwned || state.widthProbesBlocked { t.Fatalf("%q cursor %d: %q, region %+v, %v", tt.source, tt.cursor, screen.line(0), region, err) }
	}
	if screen.scrolls != 0 || len(screen.writes) != 4 { t.Fatal("one-row viewport probed or scrolled") }
}

func TestCommandPaintAssemblyDoesNotMutateRegion(t *testing.T) {
	state := TermState{currentCommand: "abcd", index: 4}
	state.prepareCommandDisplay(0, 4, nil)
	region := ProbeRegion{OriginRow: 3, OriginCol: 1, PaintedRows: 1, ScreenRows: 3, Columns: 4}
	a, nextA, err := state.appendCommandPaint(nil, region)
	if err != nil { t.Fatal(err) }
	b, nextB, err := state.appendCommandPaint(nil, region)
	if err != nil || string(a) != string(b) || !reflect.DeepEqual(nextA, nextB) || region.OriginRow != 3 { t.Fatal("assembly is not deterministic") }
}

func TestCommandRefreshRejectedBatchPaintsOnlyFallbacks(t *testing.T) {
	screen := newCommandScreen(t, 5, 8)
	state := TermState{currentCommand: "é世\x1b", index: 6}
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 8}
	reads := 0
	ready, err := state.refreshCommandDisplay(screen, func() (TerminalToken, error) {
		token, err := screen.read()
		reads++
		if reads == 2 { return CsiToken{FinalChar: 'R', Params: []byte("2;0")}, nil }
		return token, err
	}, &region)
	if !ready || err != nil || reads != 2 || !state.widthProbesBlocked || len(state.widthCache.Entries) != 0 || screen.line(0) != "??^[    " { t.Fatalf("rejected batch did not paint atomic fallback: %v", err) }
	if screen.line(1) != "        " || screen.pending { t.Fatal("scratch or pending wrap remained after fallback") }
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil || len(screen.writes) != 4 { t.Fatalf("blocked command emitted another probe: %v", err) }
}

func BenchmarkCommandRefresh(b *testing.B) {
	for _, tt := range []struct { name string; source SourceText; rows int }{
		{"ASCII", "echo hello", 100},
		{"CachedUnicode", "echo e\u0301 世", 100},
		{"Paste", SourceText(strings.Repeat("x", 4096)), 100},
		{"TallPaste", SourceText(strings.Repeat("x", 4096)), 8},
	} {
		b.Run(tt.name, func(b *testing.B) {
			state := TermState{currentCommand: tt.source, index: ByteOffset(len(tt.source))}
			state.widthCache.remember("e\u0301", 1)
			state.widthCache.remember("世", 2)
			region := ProbeRegion{OriginRow: 1, OriginCol: 3, PaintedRows: 1, ScreenRows: tt.rows, Columns: 80}
			read := func() (TerminalToken, error) { b.Fatal("cached repaint issued a query"); return nil, io.EOF }
			if ready, err := state.refreshCommandDisplay(io.Discard, read, &region); !ready || err != nil { b.Fatal(err) }
			b.ReportAllocs()
			for b.Loop() {
				if ready, err := state.refreshCommandDisplay(io.Discard, read, &region); !ready || err != nil { b.Fatal(err) }
			}
			b.ReportMetric(float64(len(state.renderBuffer)), "paint-bytes/op")
		})
	}
}

// Styles and the ghost suggestion never change geometry: a string token is
// painted red, the suggestion suffix gray, and both wrap like plain text.
func TestCommandPaintStylesAndSuggestion(t *testing.T) {
	state := TermState{currentCommand: `x "ab`, index: 5, historyComplete: `x "abc" d`, showSuggestion: true}
	screen := newCommandScreen(t, 6, 6)
	screen.cells[2][0] = ">"
	screen.row, screen.col = 2, 1
	region := ProbeRegion{OriginRow: 3, OriginCol: 2, PaintedRows: 1, ScreenRows: 6, Columns: 6}
	ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil { t.Fatalf("paint: %v", err) }
	if got := screen.line(2) + "|" + screen.line(3); got != `>x "ab|c" d  ` { t.Fatalf("lines %q", got) }
	if screen.row != 3 || screen.col != 0 { t.Fatalf("cursor after full row, got %d,%d", screen.row, screen.col) }
	want := `\x1b[0m\r\x1b[2G\r\n\x1b[1A\x1b[2G\x1b[K\r\x1b[1B\x1b[2K\x1b[1A\x1b[2Gx \x1b[91m\"ab\x1b[0m\x1b[90mc\" d\x1b[0m\r\x1b[1G`
	if got := fmt.Sprintf("%q", screen.writes[0]); got != `"`+want+`"` { t.Fatalf("styled paint %s", got) }

	state.showSuggestion = false
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil { t.Fatalf("repaint: %v", err) }
	if got := screen.line(2) + "|" + screen.line(3); got != `>x "ab|      ` { t.Fatalf("suggestion not cleared: %q", got) }
}

// Completion rows are owned trailer rows below the command: painted with
// every frame, truncated to one row each, erased when gone, and their first
// row doubles as the probe scratch row.
func TestCommandPaintCompletionTrailerRows(t *testing.T) {
	state := TermState{currentCommand: "ab", index: 2, tabCompletions0: []string{"alpha", "世x"}, tabCycleActive: true, tabCycleIndex: 1}
	screen := newCommandScreen(t, 6, 10)
	screen.cells[2][0] = ">"
	screen.row, screen.col = 2, 1
	region := ProbeRegion{OriginRow: 3, OriginCol: 2, PaintedRows: 1, ScreenRows: 6, Columns: 10}
	ready, err := state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil { t.Fatalf("paint: %v", err) }
	if got := screen.line(2) + "|" + screen.line(3) + "|" + screen.line(4); got != ">ab       |alpha     |世·x       " { t.Fatalf("lines %q", got) }
	if region.PaintedRows != 1 || region.TrailerRows != 2 || !region.ScratchOwned || screen.row != 2 || screen.col != 3 || screen.scrolls != 0 { t.Fatalf("region %+v cursor %d,%d", region, screen.row, screen.col) }
	if state.widthCache.Entries["世"] != 2 || !slices.Contains(screen.styles, "7") { t.Fatalf("highlight/probe: cache %v styles %v", state.widthCache.Entries, screen.styles) }

	// A new miss probes on the first trailer row, then the frame restores it.
	state.tabCompletions0 = []string{"é", "a-very-long-name-that-cannot-fit"}
	state.tabCycleActive = false
	writes := len(screen.writes)
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil { t.Fatalf("repaint: %v", err) }
	if got := screen.line(3) + "|" + screen.line(4) + "|" + screen.line(5); got != "é         |a-very-lon|          " { t.Fatalf("lines %q", got) }
	if len(screen.writes) != writes+3 || region.TrailerRows != 2 || screen.scrolls != 0 { t.Fatalf("probe via trailer row: writes %d region %+v", len(screen.writes)-writes, region) }

	state.tabCompletions0 = nil
	ready, err = state.refreshCommandDisplay(screen, screen.read, &region)
	if !ready || err != nil { t.Fatalf("clear: %v", err) }
	if got := screen.line(2) + "|" + screen.line(3) + "|" + screen.line(4); got != ">ab       |          |          " { t.Fatalf("lines %q", got) }
	if region.TrailerRows != 0 || !region.ScratchOwned || region.PaintedRows != 1 || screen.row != 2 || screen.col != 3 { t.Fatalf("region after clear %+v", region) }
}
