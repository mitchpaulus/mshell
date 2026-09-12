package main

import (
	"fmt"
	"slices"
	"testing"
)

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
