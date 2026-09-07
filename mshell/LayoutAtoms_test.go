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
		case RowEndSoftExact, RowEndSoftEarly:
			if next == AtomIndex(len(atoms)) || atoms[next].Kind == AtomHardBreak {
				t.Fatalf("row %d soft-wraps without a following printable atom", i)
			}
			if origin+width+atoms[next].Width <= columns {
				t.Fatalf("row %d wrapped despite room for its next atom", i)
			}
			if (row.EndType == RowEndSoftExact) != (origin+width == columns) {
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
			atoms := segmentAtomsInto(nil, command)
			widths := make(map[string]Cells)
			for i := range atoms {
				if atoms[i].Width == UnresolvedWidth {
					text := atoms[i].displayText(command)
					width, ok := widths[text]
					if !ok {
						width = Cells(1 + (len(widths)+model)%2)
						widths[text] = width
					}
					atoms[i].Width = width
				}
			}
			original := slices.Clone(atoms)
			boundaries := []ByteOffset{0}
			for _, atom := range atoms {
				boundaries = append(boundaries, atom.SourceEnd)
			}
			for _, columns := range []Cells{2, 3, 5, 10} {
				for startCol := Cells(0); startCol < columns; startCol++ {
					t.Run(fmt.Sprintf("sample%d/widths%d/columns%d/start%d", sample, model, columns, startCol), func(t *testing.T) {
						for _, cursor := range boundaries {
							got := layoutAtomsInto(dst, atoms, cursor, startCol, columns)
							dst = got.Rows
							checkAtomLayout(t, atoms, cursor, startCol, columns, got)
						}
					})
				}
			}
			if !slices.Equal(atoms, original) {
				t.Fatalf("layout mutated atoms for source %q", command)
			}
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
