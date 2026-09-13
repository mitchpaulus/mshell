package main

import (
	"fmt"
	"io"
	"os"
)

type commandViewport struct {
	Start RowIndex
	Rows int
	CursorRow RowIndex
	CursorCol Cells
	HidePrompt bool
	ReserveScratch bool
}

// Move the window only when the cursor leaves it, or shrinking the command
// removes its last rows. Logical wrapping is independent of viewport movement.
// A tall command reserves one scratch row when the screen has at least two.
func commandViewportFor(layout LayoutResult, region ProbeRegion) commandViewport {
	cursorRow, cursorCol := layout.CursorRow, layout.CursorCol
	if cursorCol == region.Columns { cursorRow++; cursorCol = 0 }
	totalRows := max(len(layout.Rows), int(cursorRow)+1)
	capacity := max(1, region.ScreenRows-1)
	rows := min(totalRows, capacity)
	start := min(max(0, region.ViewportStart), RowIndex(totalRows-rows))
	if cursorRow < start { start = cursorRow }
	if cursorRow >= start+RowIndex(rows) { start = cursorRow-RowIndex(rows)+1 }
	return commandViewport{
		Start: start,
		Rows: rows,
		CursorRow: cursorRow-start,
		CursorCol: cursorCol,
		HidePrompt: region.PromptHidden || totalRows > capacity,
		ReserveScratch: totalRows > capacity && region.ScreenRows > 1,
	}
}

// Painting may occupy the whole screen. Probing additionally requires room
// for a scratch row, which ProbeRegion.validate checks separately.
func (region *ProbeRegion) validatePaintRegion() error {
	if region.Columns < 4 || region.Columns > Cells(maxTerminalCoordinate) || region.ScreenRows < 1 || region.ScreenRows > int(maxTerminalCoordinate) {
		return fmt.Errorf("command paint: unsupported terminal geometry")
	}
	if region.OriginRow < 1 || int(region.OriginRow) > region.ScreenRows || region.OriginCol < 1 || Cells(region.OriginCol) > region.Columns {
		return fmt.Errorf("command paint: invalid region origin")
	}
	if region.PaintedRows < 1 || region.PaintedRows > region.ScreenRows || region.CursorRow < 0 || int(region.CursorRow) >= region.PaintedRows || region.ViewportStart < 0 {
		return fmt.Errorf("command paint: invalid previous frame")
	}
	if region.PromptHidden && (region.OriginCol != 1 || region.CommandStartCol < 0 || region.CommandStartCol >= region.Columns) {
		return fmt.Errorf("command paint: invalid hidden prompt geometry")
	}
	ownedRows := region.PaintedRows
	if region.ScratchOwned { ownedRows++ }
	if int(region.OriginRow)+ownedRows-1 > region.ScreenRows {
		return fmt.Errorf("command paint: owned region extends beyond terminal")
	}
	return nil
}

// appendCommandPaint assembles a full repaint without terminal I/O. The region
// describes the previous physical cursor and owned rows, not the new layout.
// Reserve additional rows before painting so natural wraps cannot scroll away
// the origin. The prompt prefix is preserved until a tall command takes over
// the screen. After that, every cell in the first physical row is owned.
func (state *TermState) appendCommandPaint(dst []byte, region ProbeRegion) ([]byte, ProbeRegion, error) {
	if err := region.validatePaintRegion(); err != nil { return dst, region, err }
	layout := state.displayLayout
	if len(layout.Rows) == 0 || state.queuedInputIndex < len(state.queuedInput) || state.displaySource != state.currentCommand || state.displayCursor != state.index {
		return dst, region, fmt.Errorf("command paint: no current prepared frame")
	}
	if state.displayColumns != region.Columns || state.displayStartCol != region.commandStartCol() {
		return dst, region, fmt.Errorf("command paint: geometry changed since preparation")
	}
	if layout.CursorRow < 0 || int(layout.CursorRow) >= len(layout.Rows) || layout.CursorCol < 0 || layout.CursorCol > region.Columns {
		return dst, region, fmt.Errorf("command paint: invalid layout cursor")
	}

	view := commandViewportFor(layout, region)

	previousOwned := region.PaintedRows
	if region.ScratchOwned { previousOwned++ }
	wantedRows := view.Rows
	if view.ReserveScratch { wantedRows++ }
	ownedRows := max(previousOwned, wantedRows)
	next := region
	next.OriginRow -= OneBasedTerminalCoord(max(0, int(region.OriginRow)+ownedRows-1-region.ScreenRows))
	next.CursorRow = view.CursorRow
	next.PaintedRows = view.Rows
	next.ViewportStart = view.Start
	if view.HidePrompt {
		next.CommandStartCol = region.commandStartCol()
		next.PromptHidden = true
		next.OriginCol = 1
	}
	// A cleared leftover row can be reused as scratch immediately below the
	// new contents. Any further cleared rows are relinquished.
	next.ScratchOwned = ownedRows > view.Rows

	dst = append(dst, "\033[0m\r"...)
	if region.CursorRow > 0 { dst = appendProbeCursorControl(dst, int(region.CursorRow), 'A') }
	dst = appendProbeCursorControl(dst, int(next.OriginCol), 'G')
	if ownedRows > previousOwned {
		if previousOwned > 1 { dst = appendProbeCursorControl(dst, previousOwned-1, 'B') }
		for i := previousOwned; i < ownedRows; i++ { dst = append(dst, '\r', '\n') }
		if ownedRows > 1 { dst = appendProbeCursorControl(dst, ownedRows-1, 'A') }
		dst = appendProbeCursorControl(dst, int(next.OriginCol), 'G')
	}

	// Clear only owned cells, including old scratch and rows left by a longer
	// command. Clearing before paint preserves the pending wrap between rows
	// joined by one-cell natural autowrap.
	dst = append(dst, "\033[K"...)
	for i := 1; i < ownedRows; i++ { dst = append(dst, "\r\033[1B\033[2K"...) }
	if ownedRows > 1 {
		dst = appendProbeCursorControl(dst, ownedRows-1, 'A')
		dst = appendProbeCursorControl(dst, int(next.OriginCol), 'G')
	}

	if view.Start == 0 && next.PromptHidden {
		dst = appendProbeCursorControl(dst, int(next.CommandStartCol)+1, 'G')
	}
	visibleEnd := min(len(layout.Rows), int(view.Start)+view.Rows)
	for i := int(view.Start); i < visibleEnd; i++ {
		row := layout.Rows[i]
		if i > int(view.Start) {
			switch layout.Rows[i-1].EndType {
			case RowEndSoftExact: // The next one-cell atom triggers autowrap.
			case RowEndHard, RowEndForcedHardWrap:
				dst = append(dst, '\r', '\n')
			default:
				return dst, region, fmt.Errorf("command paint: unsupported row transition")
			}
		}
		if len(state.displayAtoms) == 0 {
			dst = append(dst, row.Text...)
		} else {
			for j := row.AtomStart; j < row.AtomEnd; j++ {
				dst = append(dst, state.displayAtoms[j].displayText(state.displaySource)...)
			}
		}
	}
	// A full final row can put the cursor in a blank logical row after the
	// command. It may be the entire viewport on a one-row terminal.
	paintedLayoutRows := visibleEnd-int(view.Start)
	if view.Rows > paintedLayoutRows && paintedLayoutRows > 0 { dst = append(dst, '\r', '\n') }
	// CR cancels pending wrap without advancing a row. Cursor movement cannot
	// accidentally trigger a second wrap after an exact fill or forced CRLF.
	dst = append(dst, "\033[0m\r"...)
	if distance := view.Rows-1-int(view.CursorRow); distance > 0 { dst = appendProbeCursorControl(dst, distance, 'A') }
	dst = appendProbeCursorControl(dst, int(view.CursorCol)+1, 'G')
	return dst, next, nil
}

// On a write error the physical cursor is untrusted: the caller must leave the
// editor through terminal cleanup. Never publish bookkeeping for a partial
// write. A successful repaint is one write from the reusable render buffer.
func (state *TermState) paintCommandDisplay(writer io.Writer, region *ProbeRegion) error {
	output, next, err := state.appendCommandPaint(state.renderBuffer[:0], *region)
	state.renderBuffer = output
	if err != nil { return err }
	if err = writeProbeOutput(writer, output); err != nil { return err }
	*region = next
	return nil
}

// Command-only replacement-renderer entry point. The caller owns anchoring,
// terminal modes and resize serialization. Suggestions and completions must
// join this region before replacing the legacy interactive renderer.
func (state *TermState) refreshCommandDisplay(writer io.Writer, readTerminal func() (TerminalToken, error), region *ProbeRegion) (bool, error) {
	if region.Columns < 4 {
		return state.prepareCommandDisplay(region.commandStartCol(), region.Columns, nil)
	}
	if err := region.validatePaintRegion(); err != nil { return false, err }
	var ready bool
	var err error
	if region.PaintedRows < region.ScreenRows {
		ready, err = state.prepareMeasuredCommandDisplay(writer, readTerminal, region)
	} else {
		// A full-screen frame has no scratch row. Keep cached measurements and
		// placeholders until editing frees room; this is not a failed batch.
		ready, err = state.prepareCommandDisplay(region.commandStartCol(), region.Columns, nil)
	}
	if !ready || err != nil { return false, err }
	if err = state.paintCommandDisplay(writer, region); err != nil { return false, err }
	return true, nil
}

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
	return state.prepareCommandDisplay(region.commandStartCol(), region.Columns, func(candidates []string) error {
		return state.measureWidths(writer, readTerminal, region, &state.widthBatch, candidates)
	})
}

// Region rendering is opt-in until suggestions, completions and styling join
// the anchored region. Set MSH_REGION_RENDER=1 to exercise it on a terminal.
func regionRenderEnabled() bool {
	v, ok := os.LookupEnv("MSH_REGION_RENDER")
	return ok && v != "" && v != "0"
}

// anchorCommandRegion starts a fresh editing region at the reported prompt
// cursor. The first frame owns exactly the prompt row; nothing is painted yet.
func (state *TermState) anchorCommandRegion(row int, col int) {
	state.commandRegion = ProbeRegion{
		OriginRow: OneBasedTerminalCoord(row),
		OriginCol: OneBasedTerminalCoord(col),
		PaintedRows: 1,
		ScreenRows: state.numRows,
		Columns: Cells(state.numCols),
	}
}

// updateHistoryCompletion refreshes the ghost suggestion for the current
// command and returns how many suffix bytes it adds, negative when none.
func (s *TermState) updateHistoryCompletion() int {
	s.historyComplete = SourceText(SearchHistory(string(s.currentCommand), historyToSave))
	numToAdd := len(s.historyComplete) - len(s.currentCommand)
	if numToAdd < 0 {
		s.historyComplete = SourceText(SearchHistory(string(s.currentCommand), s.previousHistory))
		numToAdd = len(s.historyComplete) - len(s.currentCommand)
	}
	return numToAdd
}

// refreshInteractiveDisplay paints the editor after a token is handled. The
// legacy renderer remains the default; the region renderer measures widths
// through the anchored region and leaves a frame unpainted only while keys
// queued during measurement still wait for the editor loop.
func (state *TermState) refreshInteractiveDisplay(renderHistory bool) error {
	if !state.regionRender {
		// Complete both layout paths while the legacy painter remains active.
		if _, err := state.prepareCommandDisplay(Cells(state.promptLength), Cells(state.numCols), nil); err != nil {
			return err
		}
		state.Render(renderHistory)
		return nil
	}
	if renderHistory {
		state.updateHistoryCompletion()
	}

	// A resize reflows rows the region no longer describes. Re-anchor from a
	// fresh prompt rather than trusting the terminal's reflow.
	previousCols, previousRows := state.numCols, state.numRows
	state.UpdateSize()
	if Cells(state.numCols) != state.commandRegion.Columns || state.numRows != state.commandRegion.ScreenRows {
		state.Logf("Terminal resized %dx%d -> %dx%d; re-anchoring prompt\n", previousCols, previousRows, state.numCols, state.numRows)
		if _, err := os.Stdout.WriteString("\r\n"); err != nil { return err }
		if err := state.printPrompt(); err != nil { return err }
	}

	read := func() (TerminalToken, error) { return state.InteractiveLexer(state.stdInState) }
	var ready bool
	var err error
	if renderHistory {
		ready, err = state.refreshCommandDisplay(os.Stdout, read, &state.commandRegion)
	} else {
		// Submission paints from cached widths only; no measurement can queue
		// keys past the command that is about to run.
		ready, err = state.prepareCommandDisplay(state.commandRegion.commandStartCol(), state.commandRegion.Columns, nil)
		if ready && err == nil { err = state.paintCommandDisplay(os.Stdout, &state.commandRegion) }
	}
	if err != nil { return err }
	if !ready && state.queuedInputIndex >= len(state.queuedInput) {
		state.Logf("Region renderer skipped frame: %dx%d\n", state.numCols, state.numRows)
	}
	return nil
}
