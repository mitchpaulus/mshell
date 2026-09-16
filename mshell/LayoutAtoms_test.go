package main

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// Check the output's invariants rather than repeating the placement algorithm.
func checkAtomLayout(t *testing.T, atoms []DisplayAtom, cursor ByteOffset, startCol Cells, columns Cells, result LayoutResult) {
	t.Helper()
	rows := result.Rows
	if len(rows) == 0 {
		t.Fatal("layout has no rows")
	}

	next := AtomIndex(0)
	for i, row := range rows {
		if row.AtomStart != next || row.AtomEnd < next || row.AtomEnd > AtomIndex(len(atoms)) {
			t.Fatalf("row %d has invalid coverage: %+v; expected start %d", i, row, next)
		}
		width := Cells(0)
		for j := row.AtomStart; j < row.AtomEnd; j++ {
			if atoms[j].Kind == AtomHardBreak {
				t.Fatalf("row %d includes hard-break atom %d", i, j)
			}
			origin := Cells(0)
			if i == 0 {
				origin = startCol
			}
			if atoms[j].RequiredCells > columns-origin-width {
				t.Fatalf("row %d atom %d lacks required space", i, j)
			}
			width += atoms[j].Width
		}
		origin := Cells(0)
		if i == 0 {
			origin = startCol
		}
		if row.Width != width || width > columns-origin {
			t.Fatalf("row %d: recorded width=%d, sum=%d, available=%d", i, row.Width, width, columns-origin)
		}
		if (row.EndType == RowEndFinal) != (i == len(rows)-1) {
			t.Fatalf("row %d: only the last row must end Final", i)
		}

		next = row.AtomEnd
		switch row.EndType {
		case RowEndHard:
			if next == AtomIndex(len(atoms)) || atoms[next].Kind != AtomHardBreak {
				t.Fatalf("row %d ends hard without a newline atom", i)
			}
			next++
		case RowEndSoftExact, RowEndForcedHardWrap:
			if next == AtomIndex(len(atoms)) || atoms[next].Kind == AtomHardBreak {
				t.Fatalf("row %d soft-wraps without a following printable atom", i)
			}
			if origin+width+atoms[next].RequiredCells <= columns {
				t.Fatalf("row %d wrapped despite room for its next atom", i)
			}
			if (row.EndType == RowEndSoftExact) != (origin+width == columns && atoms[next].RequiredCells == 1) {
				t.Fatalf("row %d has incorrect soft-wrap kind: %+v", i, row)
			}
		case RowEndFinal:
		default:
			t.Fatalf("row %d has unknown ending %d", i, row.EndType)
		}
	}
	if next != AtomIndex(len(atoms)) {
		t.Fatalf("rows cover %d atoms, want %d", next, len(atoms))
	}

	// Invert the returned cursor: its cell gap must identify the requested byte gap.
	if result.CursorRow < 0 || result.CursorRow >= RowIndex(len(rows)) {
		t.Fatalf("cursor row %d outside layout", result.CursorRow)
	}
	row := rows[result.CursorRow]
	col := Cells(0)
	if result.CursorRow == 0 {
		col = startCol
	}
	found := false
	for j := row.AtomStart; j < row.AtomEnd; j++ {
		if col == result.CursorCol && atoms[j].SourceStart == cursor {
			found = true
		}
		col += atoms[j].Width
	}
	if result.CursorCol == col {
		if row.EndType == RowEndHard {
			found = cursor == atoms[row.AtomEnd].SourceStart
		} else if row.EndType == RowEndFinal {
			sourceEnd := ByteOffset(0)
			if len(atoms) > 0 {
				sourceEnd = atoms[len(atoms)-1].SourceEnd
			}
			found = cursor == sourceEnd
		}
	}
	if !found {
		t.Fatalf("cursor byte %d does not match returned gap (%d, %d)", cursor, result.CursorRow, result.CursorCol)
	}
	lastCol := rows[len(rows)-1].Width
	if len(rows) == 1 {
		lastCol += startCol
	}
	if result.PendingWrap != (lastCol == columns) {
		t.Fatalf("PendingWrap=%v with final end column %d of %d", result.PendingWrap, lastCol, columns)
	}
}

func TestAtomLayoutMixedProperties(t *testing.T) {
	pieces := []string{"a", " ", "\n", "\r\n", "\r", "\t", "\x00", "\x1b", "\x7f", "\u0085", "\xff", "世", "é", "e\u0301", "\u0301", "👨‍👩‍👧‍👦", "❤️", "🇺🇸", "🇨", "±"}
	rng := rand.New(rand.NewSource(17))
	var dst []LayoutRow
	for sample := 0; sample < 120; sample++ {
		var text strings.Builder
		for n := rng.Intn(25); n > 0; n-- {
			text.WriteString(pieces[rng.Intn(len(pieces))])
		}
		command := SourceText(text.String())
		for model := 0; model < 2; model++ {
			for _, columns := range []Cells{2, 3, 4, 5, 10, 15, 20} {
				atoms := segmentAtomsInto(nil, command)
				cache := &WidthCache{}
				for _, atom := range atoms {
					if atom.Width == UnresolvedWidth {
						candidate := atom.displayText(command)
						width := Cells(1)
						if model == 1 {
							width = candidateWidthBound(candidate)
						}
						cache.remember(candidate, width)
					}
				}
				resolveCachedWidths(nil, command, atoms, cache, &CandidateEligibilityCache{}, columns)
				finishWidthResolution(command, atoms, cache)
				original := slices.Clone(atoms)
				boundaries := []ByteOffset{0}
				for _, atom := range atoms {
					boundaries = append(boundaries, atom.SourceEnd)
				}
				for startCol := Cells(0); startCol < columns; startCol++ {
					t.Run(fmt.Sprintf("sample%d/widths%d/columns%d/start%d", sample, model, columns, startCol), func(t *testing.T) {
						for _, cursor := range boundaries {
							got := layoutAtomsInto(dst, atoms, cursor, startCol, columns)
							dst = got.Rows
							checkAtomLayout(t, atoms, cursor, startCol, columns, got)
						}
					})
				}
				if !slices.Equal(atoms, original) {
					t.Fatalf("layout mutated atoms for source %q", command)
				}
			}
		}
	}
}

func TestConservativeWidthResolution(t *testing.T) {
	const family = "👨‍👩‍👧‍👦" // Seven codepoints: bound 14, required space 15.
	for _, candidate := range []string{family, "e\u0301", tabGlyph} {
		bound := candidateWidthBound(candidate)
		for width := Cells(1); width <= bound; width++ {
			cache := &WidthCache{}
			if !cache.remember(candidate, width) {
				t.Fatalf("could not cache %q at width %d", candidate, width)
			}
			eligibility := &CandidateEligibilityCache{}
			command := SourceText(candidate)
			if candidate == tabGlyph {
				command = "\t"
			}
			// Resize must gate cache hits without losing either cache entry.
			for _, columns := range []Cells{20, bound, bound + 1, 3, 20} {
				atoms := segmentAtomsInto(nil, command)
				misses := resolveCachedWidths(nil, command, atoms, cache, eligibility, columns)
				finishWidthResolution(command, atoms, cache)
				atom := atoms[0]
				if len(misses) != 0 || atom.SourceStart != 0 || atom.SourceEnd != ByteOffset(len(command)) {
					t.Fatalf("cache hit changed source or produced misses: %+v, %q", atom, misses)
				}
				if columns < 4 || columns <= bound {
					if atom.Kind != AtomPlaceholder || atom.Width != 1 || atom.RequiredCells != 1 {
						t.Fatalf("unsafe geometry retained measured text: %+v", atom)
					}
				} else if atom.Width != width || atom.RequiredCells != bound+1 || atom.displayText(command) != candidate {
					t.Fatalf("safe geometry did not restore cached text: %+v", atom)
				}
			}
		}
		for _, width := range []Cells{-1, 0, bound + 1} {
			if (&WidthCache{}).remember(candidate, width) {
				t.Fatalf("accepted invalid width %d for %q", width, candidate)
			}
		}
	}

	cache := &WidthCache{}
	eligibility := &CandidateEligibilityCache{}
	for _, columns := range []Cells{14, 15} {
		atoms := segmentAtomsInto(nil, family)
		misses := resolveCachedWidths(nil, family, atoms, cache, eligibility, columns)
		if (len(misses) == 1) != (columns == 15) || !eligibility.Entries[family] {
			t.Fatalf("geometry skip poisoned eligibility or miss list: %q, %+v", misses, eligibility)
		}
		finishWidthResolution(family, atoms, cache)
		if atoms[0].Kind != AtomPlaceholder || atoms[0].RequiredCells != 1 {
			t.Fatalf("unknown width did not fall back safely: %+v", atoms)
		}
	}
}

func TestConservativeWrapRows(t *testing.T) {
	for _, tt := range []struct {
		name string
		command SourceText
		width Cells
		columns Cells
		start Cells
		firstEnd RowEnd
		rowCount int
		lastWidth Cells
	}{
		{"eight cells with three remaining", "👨‍👩‍👧‍👦x", 8, 15, 12, RowEndForcedHardWrap, 2, 9},
		{"bound and guard fit exactly", "👨‍👩‍👧‍👦x", 8, 15, 0, RowEndFinal, 1, 9},
		{"one cell still needs guard", "e\u0301x", 1, 5, 1, RowEndForcedHardWrap, 2, 2},
		{"measured after exact fill", "abc世", 2, 4, 1, RowEndForcedHardWrap, 2, 2},
		{"caret after exact fill", "abc\x1b", 0, 4, 1, RowEndForcedHardWrap, 2, 2},
		{"caret with one remaining", "ab\x1b", 0, 4, 1, RowEndForcedHardWrap, 2, 2},
		{"caret fills exactly", "a\x1b", 0, 4, 1, RowEndFinal, 1, 3},
		{"ascii after exact fill", "abcd", 0, 4, 1, RowEndSoftExact, 2, 1},
		{"newline after exact fill", "abc\n", 0, 4, 1, RowEndHard, 2, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			atoms := segmentAtomsInto(nil, tt.command)
			cache := &WidthCache{}
			for _, atom := range atoms {
				if atom.Width == UnresolvedWidth {
					cache.remember(atom.displayText(tt.command), tt.width)
				}
			}
			resolveCachedWidths(nil, tt.command, atoms, cache, &CandidateEligibilityCache{}, tt.columns)
			finishWidthResolution(tt.command, atoms, cache)
			for _, atom := range atoms {
				got := layoutAtomsInto(nil, atoms, atom.SourceStart, tt.start, tt.columns)
				checkAtomLayout(t, atoms, atom.SourceStart, tt.start, tt.columns, got)
				if len(got.Rows) != tt.rowCount || got.Rows[0].EndType != tt.firstEnd || got.Rows[len(got.Rows)-1].Width != tt.lastWidth {
					t.Fatalf("unexpected rows: %+v", got.Rows)
				}
			}
		})
	}
}

func TestWidthBatchCandidateBounds(t *testing.T) {
	batch := WidthProbeBatch{ScratchRow: 5, Candidates: []string{"👨‍👩‍👧‍👦", "é"}}
	if err := batch.acceptReply(CsiToken{FinalChar: 'R', Params: []byte("5;9")}); err != nil {
		t.Fatalf("eight-cell cluster rejected: %v", err)
	}
	if err := batch.acceptReply(CsiToken{FinalChar: 'R', Params: []byte("5;4")}); err == nil {
		t.Fatal("second candidate incorrectly reused first candidate's bound")
	}
	if batch.RepliesReceived != 2 || !slices.Equal(batch.Widths, []Cells{8}) {
		t.Fatalf("unexpected batch state: %+v", batch)
	}
	for bound := Cells(2); bound <= 14; bound += 2 {
		for width := Cells(0); width <= bound+1; width++ {
			t.Run(fmt.Sprintf("bound%d/width%d", bound, width), func(t *testing.T) {
				got, err := widthFromCursorReport(CursorReport{Row: 5, Column: OneBasedTerminalCoord(width+1)}, 5, bound)
				if width >= 1 && width <= bound {
					if err != nil || got != width {
						t.Fatalf("valid observation rejected: %d, %v", got, err)
					}
				} else if err == nil {
					t.Fatal("out-of-bound observation accepted")
				}
			})
		}
	}
}

func TestAtomLayoutConsecutiveHardBreaks(t *testing.T) {
	atoms := segmentAtomsInto(nil, "\r\n\n")
	want := []LayoutRow{
		{AtomStart: 0, AtomEnd: 0, EndType: RowEndHard},
		{AtomStart: 1, AtomEnd: 1, EndType: RowEndHard},
		{AtomStart: 2, AtomEnd: 2, EndType: RowEndFinal},
	}
	for i, cursor := range []ByteOffset{0, 2, 3} {
		got := layoutAtomsInto(nil, atoms, cursor, 4, 5)
		col := Cells(0)
		if i == 0 {
			col = 4
		}
		if !slices.Equal(got.Rows, want) || got.CursorRow != RowIndex(i) || got.CursorCol != col || got.PendingWrap {
			t.Fatalf("cursor %d: got %+v, want rows %+v and cursor (%d, %d)", cursor, got, want, i, col)
		}
	}
	requireLayoutPanic(t, func() { layoutAtomsInto(nil, atoms, 1, 4, 5) })
}

func requireLayoutPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected invalid layout input to panic")
		}
	}()
	f()
}

func TestAtomLayoutInvalidInputs(t *testing.T) {
	for _, geometry := range []struct{ start, columns Cells }{{0, -1}, {0, 0}, {0, 1}, {-1, 5}, {5, 5}, {6, 5}} {
		t.Run(fmt.Sprintf("geometry/%d/%d", geometry.start, geometry.columns), func(t *testing.T) {
			for _, text := range []SourceText{"", "a"} {
				atoms := segmentAtomsInto(nil, text)
				requireLayoutPanic(t, func() { layoutAtomsInto(nil, atoms, 0, geometry.start, geometry.columns) })
				requireLayoutPanic(t, func() { layoutAtomRowsInto(nil, atoms, geometry.start, geometry.columns) })
				requireLayoutPanic(t, func() { layoutPrintableAsciiInto(nil, text, 0, geometry.start, geometry.columns) })
			}
		})
	}
	for _, cursor := range []ByteOffset{-1, 1} {
		requireLayoutPanic(t, func() { layoutAtomsInto(nil, nil, cursor, 0, 5) })
	}
	for _, width := range []Cells{-2, UnresolvedWidth, 0, 3} {
		atoms := []DisplayAtom{{SourceEnd: 1, Width: width, Kind: AtomGrapheme}}
		requireLayoutPanic(t, func() { layoutAtomsInto(nil, atoms, 0, 0, 5) })
	}
	for _, required := range []Cells{UnresolvedWidth, 0, 1, 6} {
		atoms := []DisplayAtom{{SourceEnd: 1, Width: 2, RequiredCells: required, Kind: AtomGrapheme}}
		requireLayoutPanic(t, func() { layoutAtomsInto(nil, atoms, 0, 0, 5) })
	}
	atoms := []DisplayAtom{{SourceEnd: 1, RequiredCells: 1, Kind: AtomHardBreak}}
	requireLayoutPanic(t, func() { layoutAtomsInto(nil, atoms, 0, 0, 5) })
	for _, width := range []Cells{UnresolvedWidth, 1, 2} {
		atoms := []DisplayAtom{{SourceEnd: 1, Width: width, Kind: AtomHardBreak}}
		requireLayoutPanic(t, func() { layoutAtomsInto(nil, atoms, 0, 0, 5) })
	}
}

func TestLayoutRowStorageReuse(t *testing.T) {
	for _, general := range []bool{false, true} {
		t.Run(fmt.Sprintf("general=%v", general), func(t *testing.T) {
			dst := make([]LayoutRow, 0, 16)
			first := &dst[:cap(dst)][0]
			for _, text := range []SourceText{"abcdefghijk", "x", "", "abcdef"} {
				var got LayoutResult
				if general {
					got = layoutAtomsInto(dst, segmentAtomsInto(nil, text), ByteOffset(len(text)), 1, 3)
				} else {
					got = layoutPrintableAsciiInto(dst, text, ByteOffset(len(text)), 1, 3)
				}
				if &got.Rows[0] != first {
					t.Fatal("layout replaced sufficiently large destination storage")
				}
				if text == "" && (len(got.Rows) != 1 || got.Rows[0] != (LayoutRow{EndType: RowEndFinal})) {
					t.Fatalf("empty layout retained stale row data: %+v", got.Rows)
				}
				dst = got.Rows
			}
		})
	}
}

func TestPrintableAsciiClassifierAllBytes(t *testing.T) {
	if !isAllPrintableAscii("") {
		t.Fatal("empty input should use the direct path")
	}
	for b := 0; b < 256; b++ {
		for _, text := range []string{string([]byte{byte(b)}), "abc" + string([]byte{byte(b)}) + "xyz"} {
			if got := isAllPrintableAscii(SourceText(text)); got != (b >= 0x20 && b <= 0x7e) {
				t.Fatalf("classification of %q = %v", text, got)
			}
		}
	}
}
