package main

import (
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
			for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' { end++ }
			if end == len(rest) { screen.t.Fatal("incomplete CSI") }
			n, _ := strconv.Atoi(rest[2:end])
			switch rest[end] {
			case 'A': screen.row = max(0, screen.row-max(1, n)); screen.pending = false
			case 'B': screen.row = min(len(screen.cells)-1, screen.row+max(1, n)); screen.pending = false
			case 'G': screen.col = min(columns-1, max(1, n)-1); screen.pending = false
			case 'K':
				start := screen.col
				if n == 2 { start = 0 } else if n != 0 { screen.t.Fatal("unsupported erase") }
				for col := start; col < columns; col++ { screen.cells[screen.row][col] = " " }
			case 'm':
				if n != 0 { screen.t.Fatal("unexpected style") }
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

func TestCommandPaintRefusesStaleOrOversizeFrames(t *testing.T) {
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
	state.queuedInput = nil
	state.currentCommand, state.index, region = "12345678", 8, before
	state.prepareCommandDisplay(0, 4, nil)
	writer := &failingProbeWriter{failAt: 1}
	if err := state.paintCommandDisplay(writer, &region); !errors.Is(err, errCommandViewportRequired) || writer.calls != 0 || region != before { t.Fatalf("oversize frame wrote or changed geometry: %v", err) }
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
		state := TermState{currentCommand: "long command", index: 12}
		state.prepareCommandDisplay(0, 8, nil)
		region := ProbeRegion{OriginRow: 4, OriginCol: 1, PaintedRows: 1, ScreenRows: 4, Columns: 8}
		before := region
		writer := &failingProbeWriter{failAt: 1, short: short}
		err := state.paintCommandDisplay(writer, &region)
		if err == nil || region != before || writer.calls != 1 { t.Fatalf("partial write published geometry: %v", err) }
		if short && !errors.Is(err, io.ErrShortWrite) { t.Fatal("short write not detected") }
	}
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
	for _, source := range []SourceText{"echo hello", "echo e\u0301 世", SourceText(strings.Repeat("x", 4096))} {
		name := "ASCII"
		if len(source) > 100 { name = "Paste" } else if !isAllPrintableAscii(source) { name = "CachedUnicode" }
		b.Run(name, func(b *testing.B) {
			state := TermState{currentCommand: source, index: ByteOffset(len(source))}
			state.widthCache.remember("e\u0301", 1)
			state.widthCache.remember("世", 2)
			region := ProbeRegion{OriginRow: 1, OriginCol: 3, PaintedRows: 1, ScreenRows: 100, Columns: 80}
			read := func() (TerminalToken, error) { b.Fatal("cached repaint issued a query"); return nil, io.EOF }
			if ready, err := state.refreshCommandDisplay(io.Discard, read, &region); !ready || err != nil { b.Fatal(err) }
			b.ReportAllocs()
			for b.Loop() {
				if ready, err := state.refreshCommandDisplay(io.Discard, read, &region); !ready || err != nil { b.Fatal(err) }
			}
		})
	}
}
