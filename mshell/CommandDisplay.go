package main

import "io"

// prepareCommandDisplay completes one serialized editor frame. measure, when
// supplied, must finish/drain its transaction and queue input without applying
// edits. The caller applies queued tokens through the normal editor loop and
// prepares again before painting. No layout from an obsolete frame is exposed.
// A nil measure uses cached widths and placeholders without terminal I/O.
func (state *TermState) prepareCommandDisplay(startCol, columns Cells, measure func([]string) error) (bool, error) {
	clear(state.displayLayout.Rows)
	state.displayLayout = LayoutResult{Rows: state.displayLayout.Rows[:0]}
	state.displayAtoms = state.displayAtoms[:0]
	clear(state.widthMisses)
	state.widthMisses = state.widthMisses[:0]
	state.displaySource = state.currentCommand
	state.displayCursor = state.index
	state.displayStartCol = startCol
	state.displayColumns = columns

	if state.queuedInputIndex < len(state.queuedInput) || columns < 4 || columns > Cells(maxTerminalCoordinate) || startCol < 0 || startCol >= columns {
		return false, nil
	}

	if isAllPrintableAscii(state.displaySource) {
		state.displayLayout = layoutPrintableAsciiInto(state.displayLayout.Rows, state.displaySource, state.index, startCol, columns)
		return true, nil
	}

	state.displayAtoms = segmentAtomsInto(state.displayAtoms, state.displaySource)
	state.widthMisses = resolveCachedWidths(state.widthMisses, state.displaySource, state.displayAtoms, &state.widthCache, &state.eligibilityCache, columns)
	if measure != nil && !state.widthProbesBlocked && len(state.widthMisses) > 0 {
		if err := measure(state.widthMisses); err != nil {
			return false, err
		}
		// A valid batch still populates the session cache when keys arrived in
		// flight. Defer layout until those keys have been applied, including
		// submission and its prompt query, after all reports have drained.
		if state.queuedInputIndex < len(state.queuedInput) {
			return false, nil
		}
	}
	finishWidthResolution(state.displaySource, state.displayAtoms, &state.widthCache)
	state.displayLayout = layoutAtomsInto(state.displayLayout.Rows, state.displayAtoms, state.index, startCol, columns)
	return true, nil
}

// The replacement painter supplies its previously painted, anchored region.
// Keep this entry point separate from the legacy painter, which cannot yet
// guarantee scratch ownership. The batch storage belongs to the editor session.
func (state *TermState) prepareMeasuredCommandDisplay(writer io.Writer, readTerminal func() (TerminalToken, error), region *ProbeRegion) (bool, error) {
	return state.prepareCommandDisplay(Cells(region.OriginCol)-1, region.Columns, func(candidates []string) error {
		return state.measureWidths(writer, readTerminal, region, &state.widthBatch, candidates)
	})
}
