package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCommandDisplayCompletesBothPaths(t *testing.T) {
	state := TermState{}
	for _, source := range []SourceText{"echo hello", "a\x1b\t世\n", "", "e\u0301", "ASCII again"} {
		state.currentCommand = source
		state.index = state.commandEnd()
		ready, err := state.prepareCommandDisplay(3, 8, nil)
		if !ready || err != nil || len(state.displayLayout.Rows) == 0 { t.Fatalf("%q: no layout: %v", source, err) }
		if state.displaySource != source || state.currentCommand != source { t.Fatal("source changed") }
		if isAllPrintableAscii(source) {
			if len(state.displayAtoms) != 0 || len(state.widthMisses) != 0 { t.Fatal("ASCII did not bypass atoms") }
			continue
		}
		var painted strings.Builder
		for _, atom := range state.displayAtoms {
			if atom.Width < 0 || atom.RequiredCells > 8 { t.Fatalf("unresolved atom: %+v", atom) }
			painted.WriteString(atom.displayText(source))
		}
		if strings.ContainsAny(painted.String(), "\x1b\t\n") { t.Fatalf("unsafe paint: %q", painted.String()) }
		if source == "a\x1b\t世\n" && painted.String() != "a^[??" { t.Fatalf("fallback paint: %q", painted.String()) }
	}
}

func TestMeasuredCommandDisplayGeometry(t *testing.T) {
	state := TermState{currentCommand: "👨‍👩‍👧‍👦"}
	state.index = state.commandEnd()
	region := ProbeRegion{OriginRow: 4, OriginCol: 13, PaintedRows: 1, ScreenRows: 4, Columns: 15}
	terminal := &probeTerminal{t: t, row: 4, col: 13, rows: 4, columns: 15, widths: map[string]int{string(state.currentCommand): 8}}
	read := func() (TerminalToken, error) {
		if len(terminal.replies) == 0 { t.Fatal("unexpected read") }
		token := terminal.replies[0]
		terminal.replies = terminal.replies[1:]
		return token, nil
	}
	ready, err := state.prepareMeasuredCommandDisplay(terminal, read, &region)
	if !ready || err != nil { t.Fatalf("prepare: %v", err) }
	if region.OriginRow != 3 || terminal.scrolls != 1 || terminal.scratchDirty { t.Fatal("scratch scrolling/cleanup lost") }
	if len(state.displayLayout.Rows) != 2 || state.displayLayout.Rows[0].EndType != RowEndForcedHardWrap || state.displayLayout.CursorRow != 1 || state.displayLayout.CursorCol != 8 { t.Fatalf("wrong measured layout: %+v", state.displayLayout) }
	if state.displayAtoms[0].RequiredCells != 15 { t.Fatal("lost conservative bound") }
	for _, columns := range []Cells{14, 15} {
		region.Columns = columns
		ready, err = state.prepareMeasuredCommandDisplay(terminal, read, &region)
		if !ready || err != nil { t.Fatalf("resize: %v", err) }
		wantKind := AtomGrapheme
		if columns == 14 { wantKind = AtomPlaceholder }
		if state.displayAtoms[0].Kind != wantKind || state.widthCache.Entries[string(state.currentCommand)] != 8 { t.Fatal("resize did not retain/reuse width") }
	}
	if len(terminal.writes) != 2 { t.Fatal("cached resize issued probes") }
}

func TestCommandDisplayDiscardsQueuedFrame(t *testing.T) {
	state := TermState{currentCommand: "é"}
	state.index = state.commandEnd()
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 20}
	terminal := &probeTerminal{t: t, row: 1, col: 1, rows: 5, columns: 20, widths: map[string]int{"é": 1}}
	inputs := []TerminalToken{AsciiToken{Char: 'a'}, AsciiToken{Char: 'b'}}
	read := func() (TerminalToken, error) {
		if len(inputs) > 0 {
			token := inputs[0]
			inputs = inputs[1:]
			return token, nil
		}
		if len(terminal.replies) == 0 { t.Fatal("unexpected extra query") }
		token := terminal.replies[0]
		terminal.replies = terminal.replies[1:]
		return token, nil
	}
	ready, err := state.prepareMeasuredCommandDisplay(terminal, read, &region)
	if ready || err != nil || len(state.displayLayout.Rows) != 0 || state.widthCache.Entries["é"] != 1 { t.Fatalf("stale frame exposed or measurement lost: %v", err) }
	for i := 0; i < 2; i++ {
		token, err := state.readInputToken()
		if err != nil { t.Fatal(err) }
		state.PushChars([]rune{rune(token.(AsciiToken).Char)})
		ready, err = state.prepareMeasuredCommandDisplay(terminal, read, &region)
		if err != nil || ready != (i == 1) { t.Fatalf("queue not drained before layout: %v", err) }
	}
	if state.displaySource != "éab" || state.displayLayout.CursorCol != 3 || len(terminal.writes) != 2 { t.Fatal("latest frame did not reuse measurements") }
}

func TestCommandDisplayFailedBatchAndRetry(t *testing.T) {
	state := TermState{currentCommand: "é世"}
	state.widthCache.remember("é", 1)
	state.index = state.commandEnd()
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 20}
	reads := 0
	read := func() (TerminalToken, error) {
		reads++
		return CsiToken{FinalChar: 'R', Params: []byte("2;1")}, nil
	}
	for i := 0; i < 2; i++ {
		ready, err := state.prepareMeasuredCommandDisplay(io.Discard, read, &region)
		if !ready || err != nil || !state.widthProbesBlocked { t.Fatalf("failure did not finish fallback: %v", err) }
		if state.displayAtoms[0].Kind != AtomGrapheme || state.displayAtoms[1].Kind != AtomPlaceholder || state.displayLayout.CursorCol != 2 { t.Fatal("cached/fallback layout wrong") }
	}
	if reads != 1 { t.Fatal("failed command retried") }
	// Command submission resets this flag; neither cache is cleared.
	state.widthProbesBlocked = false
	ready, err := state.prepareMeasuredCommandDisplay(io.Discard, func() (TerminalToken, error) {
		return CsiToken{FinalChar: 'R', Params: []byte("2;3")}, nil
	}, &region)
	if !ready || err != nil || state.displayLayout.CursorCol != 3 || state.widthCache.Entries["世"] != 2 { t.Fatalf("retry did not resolve: %v", err) }
}

func TestCommandDisplayBoundsOneBatch(t *testing.T) {
	var source strings.Builder
	for i := 0; i < maxWidthProbes+1; i++ { source.WriteRune(rune(0x4e00+i)) }
	state := TermState{currentCommand: SourceText(source.String())}
	state.index = state.commandEnd()
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 80}
	reads := 0
	ready, err := state.prepareMeasuredCommandDisplay(io.Discard, func() (TerminalToken, error) {
		reads++
		return CsiToken{FinalChar: 'R', Params: []byte("2;3")}, nil
	}, &region)
	if !ready || err != nil || reads != maxWidthProbes { t.Fatalf("unbounded preparation: %d reads, %v", reads, err) }
	if state.displayAtoms[maxWidthProbes].Kind != AtomPlaceholder || state.widthProbesBlocked { t.Fatal("overflow did not degrade without blocking") }
	for _, atom := range state.displayAtoms[:maxWidthProbes] {
		if atom.Kind != AtomGrapheme || atom.Width != 2 { t.Fatal("measured batch did not reach layout") }
	}
}

func BenchmarkCommandDisplayPreparation(b *testing.B) {
	for _, source := range []SourceText{"echo hello", "echo e\u0301 世", SourceText(strings.Repeat("x", 4096))} {
		name := "ASCII"
		if len(source) > 100 { name = "Paste" } else if !isAllPrintableAscii(source) { name = "CachedUnicode" }
		b.Run(name, func(b *testing.B) {
			state := TermState{currentCommand: source, index: ByteOffset(len(source))}
			state.widthCache.remember("e\u0301", 1)
			state.widthCache.remember("世", 2)
			state.prepareCommandDisplay(2, 80, nil)
			b.ReportAllocs()
			for b.Loop() { state.prepareCommandDisplay(2, 80, nil) }
		})
	}
}

func TestCommandDisplayNoLayoutOnIOErrorOrInvalidGeometry(t *testing.T) {
	state := TermState{currentCommand: "é", index: 2}
	region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 20}
	state.prepareCommandDisplay(0, 20, nil)
	ready, err := state.prepareMeasuredCommandDisplay(io.Discard, func() (TerminalToken, error) { return nil, io.EOF }, &region)
	if ready || !errors.Is(err, io.EOF) || len(state.displayLayout.Rows) != 0 { t.Fatalf("I/O failure exposed frame: %v", err) }
	for _, geometry := range [][2]Cells{{0, 3}, {-1, 20}, {20, 20}, {0, 10000}} {
		state.prepareCommandDisplay(0, 20, nil)
		ready, err = state.prepareCommandDisplay(geometry[0], geometry[1], func([]string) error { t.Fatal("probe with invalid geometry"); return nil })
		if ready || err != nil || len(state.displayLayout.Rows) != 0 || len(state.displayAtoms) != 0 { t.Fatalf("invalid geometry exposed stale frame: %v", geometry) }
	}
}
