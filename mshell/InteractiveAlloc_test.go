package main

import (
	"io"
	"strings"
	"testing"
)

func BenchmarkInteractiveAllocations(b *testing.B) {
	for _, source := range []SourceText{"echo hello", "echo e\u0301 👩‍💻", SourceText(strings.Repeat("x", 4096))} {
		name := "ASCII"
		if len(source) > 100 { name = "Paste" } else if !isAllPrintableAscii(source) { name = "Unicode" }
		b.Run("Edit/"+name, func(b *testing.B) {
			state := TermState{}
			b.ReportAllocs()
			for b.Loop() {
				state.currentCommand = source
				state.index = state.commandEnd()
				state.PushChars([]rune{'a'})
			}
		})
		b.Run("Movement/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() { _ = wordLeft(source, graphemePrevious(source, ByteOffset(len(source)))) }
		})
	}
	b.Run("CompletionPrefix", func(b *testing.B) {
		matches := []string{"echo e\u0301abc", "echo e\u0301abd"}
		b.ReportAllocs()
		for b.Loop() { _ = completionGraphemePrefix(matches) }
	})
	for _, cached := range []bool{false, true} {
		name := "Misses"
		if cached { name = "Cached" }
		b.Run("Resolve/"+name, func(b *testing.B) {
			var text strings.Builder
			cache := &WidthCache{}
			eligibility := &CandidateEligibilityCache{}
			for i := 0; i < 256; i++ {
				candidate := string(rune(0x4e00+i))
				text.WriteString(candidate)
				eligibility.allows(candidate)
				if cached { cache.remember(candidate, 2) }
			}
			source := SourceText(text.String())
			atoms := segmentAtomsInto(nil, source)
			misses := resolveCachedWidths(nil, source, atoms, cache, eligibility, 80)
			b.ReportAllocs()
			for b.Loop() {
				atoms = segmentAtomsInto(atoms, source)
				misses = resolveCachedWidths(misses, source, atoms, cache, eligibility, 80)
			}
		})
	}
	b.Run("ProbeTransaction", func(b *testing.B) {
		state := TermState{}
		region := ProbeRegion{OriginRow: 1, OriginCol: 1, PaintedRows: 1, ScreenRows: 5, Columns: 80}
		batch := WidthProbeBatch{}
		candidates := []string{"é", "世", "👨‍👩‍👧‍👦"}
		var report TerminalToken = CsiToken{FinalChar: 'R', Params: []byte("2;2")}
		read := func() (TerminalToken, error) { return report, nil }
		if err := state.measureWidths(io.Discard, read, &region, &batch, candidates); err != nil { b.Fatal(err) }
		b.ReportAllocs()
		for b.Loop() {
			clear(state.widthCache.Entries)
			if err := state.measureWidths(io.Discard, read, &region, &batch, candidates); err != nil { b.Fatal(err) }
		}
	})
}
