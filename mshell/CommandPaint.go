package main

import (
	"errors"
	"fmt"
	"io"
)

var errCommandViewportRequired = errors.New("command paint: frame exceeds terminal height; viewport required")

// Painting may occupy the whole screen. Probing additionally requires room
// for a scratch row, which ProbeRegion.validate checks separately.
func (region *ProbeRegion) validatePaintRegion() error {
	if region.Columns < 4 || region.Columns > Cells(maxTerminalCoordinate) || region.ScreenRows < 1 || region.ScreenRows > int(maxTerminalCoordinate) {
		return fmt.Errorf("command paint: unsupported terminal geometry")
	}
	if region.OriginRow < 1 || region.OriginCol < 1 || Cells(region.OriginCol) > region.Columns {
		return fmt.Errorf("command paint: invalid region origin")
	}
	if region.PaintedRows < 1 || region.CursorRow < 0 || int(region.CursorRow) >= region.PaintedRows {
		return fmt.Errorf("command paint: invalid previous frame")
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
// the origin. Only the command suffix of the first row belongs to the painter.
func (state *TermState) appendCommandPaint(dst []byte, region ProbeRegion) ([]byte, ProbeRegion, error) {
	if err := region.validatePaintRegion(); err != nil { return dst, region, err }
	layout := state.displayLayout
	if len(layout.Rows) == 0 || state.queuedInputIndex < len(state.queuedInput) || state.displaySource != state.currentCommand || state.displayCursor != state.index {
		return dst, region, fmt.Errorf("command paint: no current prepared frame")
	}
	if state.displayColumns != region.Columns || state.displayStartCol != Cells(region.OriginCol)-1 {
		return dst, region, fmt.Errorf("command paint: geometry changed since preparation")
	}
	if layout.CursorRow < 0 || int(layout.CursorRow) >= len(layout.Rows) || layout.CursorCol < 0 || layout.CursorCol > region.Columns {
		return dst, region, fmt.Errorf("command paint: invalid layout cursor")
	}

	// The gap after a full row has no addressable terminal column. Materialize
	// it at column one of the next row, including a blank row at command end.
	// Every parked cursor is an ordinary cell position with pending wrap off.
	cursorRow, cursorCol := layout.CursorRow, layout.CursorCol
	if cursorCol == region.Columns { cursorRow++; cursorCol = 0 }
	paintedRows := max(len(layout.Rows), int(cursorRow)+1)
	if paintedRows > region.ScreenRows { return dst, region, errCommandViewportRequired }

	previousOwned := region.PaintedRows
	if region.ScratchOwned { previousOwned++ }
	ownedRows := max(previousOwned, paintedRows)
	next := region
	next.OriginRow -= OneBasedTerminalCoord(max(0, int(region.OriginRow)+ownedRows-1-region.ScreenRows))
	next.CursorRow = cursorRow
	next.PaintedRows = paintedRows
	// A cleared leftover row can be reused as scratch immediately below the
	// new contents. Any further cleared rows are relinquished.
	next.ScratchOwned = ownedRows > paintedRows

	dst = append(dst, "\033[0m\r"...)
	if region.CursorRow > 0 { dst = appendProbeCursorControl(dst, int(region.CursorRow), 'A') }
	dst = appendProbeCursorControl(dst, int(region.OriginCol), 'G')
	if ownedRows > previousOwned {
		if previousOwned > 1 { dst = appendProbeCursorControl(dst, previousOwned-1, 'B') }
		for i := previousOwned; i < ownedRows; i++ { dst = append(dst, '\r', '\n') }
		if ownedRows > 1 { dst = appendProbeCursorControl(dst, ownedRows-1, 'A') }
		dst = appendProbeCursorControl(dst, int(region.OriginCol), 'G')
	}

	// Clear only owned cells, including old scratch and rows left by a longer
	// command. Clearing before paint preserves the pending wrap between rows
	// joined by one-cell natural autowrap.
	dst = append(dst, "\033[K"...)
	for i := 1; i < ownedRows; i++ { dst = append(dst, "\r\033[1B\033[2K"...) }
	if ownedRows > 1 {
		dst = appendProbeCursorControl(dst, ownedRows-1, 'A')
		dst = appendProbeCursorControl(dst, int(region.OriginCol), 'G')
	}

	for i, row := range layout.Rows {
		if i > 0 {
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
	if paintedRows > len(layout.Rows) { dst = append(dst, '\r', '\n') }
	// CR cancels pending wrap without advancing a row. Cursor movement cannot
	// accidentally trigger a second wrap after an exact fill or forced CRLF.
	dst = append(dst, "\033[0m\r"...)
	if distance := paintedRows-1-int(cursorRow); distance > 0 { dst = appendProbeCursorControl(dst, distance, 'A') }
	dst = appendProbeCursorControl(dst, int(cursorCol)+1, 'G')
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
		return state.prepareCommandDisplay(Cells(region.OriginCol)-1, region.Columns, nil)
	}
	if err := region.validatePaintRegion(); err != nil { return false, err }
	var ready bool
	var err error
	if region.PaintedRows < region.ScreenRows {
		ready, err = state.prepareMeasuredCommandDisplay(writer, readTerminal, region)
	} else {
		// A full-screen frame has no scratch row. Keep cached measurements and
		// placeholders until editing frees room; this is not a failed batch.
		ready, err = state.prepareCommandDisplay(Cells(region.OriginCol)-1, region.Columns, nil)
	}
	if !ready || err != nil { return false, err }
	if err = state.paintCommandDisplay(writer, region); err != nil { return false, err }
	return true, nil
}
