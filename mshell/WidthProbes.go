package main

import (
	"fmt"
	"io"
	"strings"
	"strconv"
	"unicode/utf8"
)

// ProbeRegion describes a fully visible region owned by the replacement
// renderer. Physical rows are relative to OriginRow except terminal coordinates;
// ViewportStart identifies the first visible row in the full command layout.
// The caller must establish a valid anchor and serialize queries and resize
// handling before probing. The command viewport reserves a scratch row on
// screens with at least two rows; a full-screen legacy region must shrink first.
type ProbeRegion struct {
	OriginRow OneBasedTerminalCoord
	OriginCol OneBasedTerminalCoord
	CursorRow RowIndex
	PaintedRows int // Rows occupied by the previous paint, including suggestions and completions; excludes scratch.
	ScreenRows int // Terminal height in rows.
	// Total terminal width in cells, including columns occupied by the prompt.
	// Probes start at column one and must leave a guard cell within this width.
	Columns Cells
	// True once a scratch row has been reserved immediately after PaintedRows,
	// at terminal row OriginRow + PaintedRows. The renderer owns this row and
	// may clear/reuse it without overwriting editing contents. It remains owned
	// after probing clears it and returns the cursor to the region origin.
	// The caller must invalidate ownership if repaint or resize changes the
	// region so that this row is no longer available as scratch space.
	ScratchOwned bool
	// Completion rows painted immediately below PaintedRows. They are owned
	// rows repainted with every frame; the first one doubles as scratch.
	TrailerRows int
	// Logical row at the top of the visible command window. CursorRow and
	// PaintedRows remain physical, region-relative coordinates for probing.
	ViewportStart RowIndex
	// Once a tall command takes over the screen, the opaque prompt is gone.
	// Keep its original command offset so moving the viewport does not reflow
	// logical rows. Returning to row zero leaves that prefix blank.
	PromptHidden bool
	CommandStartCol Cells
}

// ownedRows counts every screen row the renderer must clear and repaint.
func (region *ProbeRegion) ownedRows() int {
	owned := region.PaintedRows + region.TrailerRows
	if region.ScratchOwned && region.TrailerRows == 0 { owned++ }
	return owned
}

func (region *ProbeRegion) commandStartCol() Cells {
	if region.PromptHidden { return region.CommandStartCol }
	return Cells(region.OriginCol)-1
}

func (region *ProbeRegion) validate() error {
	if region.Columns < 4 || region.Columns > Cells(maxTerminalCoordinate) {
		return fmt.Errorf("width probe: terminal columns %d outside supported range 4..%d", region.Columns, maxTerminalCoordinate)
	}
	if region.ScreenRows < 2 || region.ScreenRows > int(maxTerminalCoordinate) {
		return fmt.Errorf("width probe: terminal rows %d outside supported range 2..%d", region.ScreenRows, maxTerminalCoordinate)
	}

	// Keep at least one screen row available for scratch space.
	if region.PaintedRows < 1 || region.PaintedRows >= region.ScreenRows {
		return fmt.Errorf("width probe: painted row count %d must be between 1 and %d", region.PaintedRows, region.ScreenRows-1)
	}
	if region.OriginRow < 1 {
		return fmt.Errorf("width probe: origin row %d must be positive", region.OriginRow)
	}
	if region.OriginCol < 1 || Cells(region.OriginCol) > region.Columns {
		return fmt.Errorf("width probe: origin column %d outside terminal columns 1..%d", region.OriginCol, region.Columns)
	}
	if region.CursorRow < 0 || int(region.CursorRow) >= region.PaintedRows {
		return fmt.Errorf("width probe: cursor row %d outside region rows 0..%d", region.CursorRow, region.PaintedRows-1)
	}

	lastPaintedRow := int(region.OriginRow) + region.PaintedRows - 1
	if lastPaintedRow > region.ScreenRows {
		return fmt.Errorf("width probe: region ends at row %d beyond terminal row %d", lastPaintedRow, region.ScreenRows)
	}
	// A new scratch row may scroll the region up. An already-owned row must
	// still be on screen immediately below the visible contents.
	scratchRow := lastPaintedRow + 1
	if region.ScratchOwned && scratchRow > region.ScreenRows {
		return fmt.Errorf("width probe: owned scratch row outside terminal")
	}
	return nil
}

// enterScratch clears pending wrap before moving. Only the final CRLF may
// scroll, and bookkeeping shifts the owned region by exactly that one row.
func (region *ProbeRegion) enterScratch(dst []byte) ([]byte, OneBasedTerminalCoord) {
	dst = append(dst, '\r')
	if region.ScratchOwned {
		distance := region.PaintedRows - int(region.CursorRow)
		dst = appendProbeCursorControl(dst, distance, 'B')
	} else {
		distance := region.PaintedRows - 1 - int(region.CursorRow)
		if distance > 0 {
			dst = appendProbeCursorControl(dst, distance, 'B')
		}
		dst = append(dst, '\r', '\n')
		if int(region.OriginRow)+region.PaintedRows > region.ScreenRows {
			region.OriginRow--
		}
		region.ScratchOwned = true
	}
	dst = append(dst, "\033[2K"...)
	return dst, region.OriginRow + OneBasedTerminalCoord(region.PaintedRows)
}

func safeWidthCandidate(candidate string) bool {
	if len(candidate) == 0 || len(candidate) > maxWidthCandidateBytes || !utf8.ValidString(candidate) {
		return false
	}
	nonASCII := false
	for _, r := range candidate {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
		nonASCII = nonASCII || r > 0x7f
	}
	return nonASCII
}

// prepare freezes only safe, distinct, unknown candidates. Input comes from
// the bounded resolver miss list; the cap also bounds this emission boundary.
func (batch *WidthProbeBatch) prepare(candidates []string, columns Cells, cache *WidthCache, eligibility *CandidateEligibilityCache) {
	batch.Clear()
	if columns < 4 {
		return
	}
	limit := min(maxWidthProbes, max(0, maxWidthCacheEntries-len(cache.Entries)))
	defer func() { clear(batch.seen) }()
	for _, candidate := range candidates {
		if len(batch.Candidates) >= limit {
			break
		}
		if _, cached := cache.Entries[candidate]; cached || batch.seen[candidate] {
			continue
		}
		if !safeWidthCandidate(candidate) || !eligibility.allows(candidate) || candidateWidthBound(candidate) >= columns {
			continue
		}
		if batch.seen == nil {
			batch.seen = make(map[string]bool)
		}
		batch.seen[candidate] = true
		batch.Candidates = append(batch.Candidates, strings.Clone(candidate))
	}
}

// commit validates the entire staged batch before touching session entries.
// prepare already cloned each candidate; transfer those owned strings into the
// cache without copying again or retaining slices of the command source.
func (batch *WidthProbeBatch) commit(cache *WidthCache) error {
	if batch.Failure != nil {
		return batch.Failure
	}
	if batch.RepliesReceived != len(batch.Candidates) || len(batch.Widths) != len(batch.Candidates) {
		return fmt.Errorf("width probe: incomplete batch")
	}
	additional := 0
	for i, candidate := range batch.Candidates {
		width := batch.Widths[i]
		if width < 1 || width > candidateWidthBound(candidate) {
			return fmt.Errorf("width probe: invalid staged width")
		}
		if previous, ok := cache.Entries[candidate]; ok {
			if previous != width {
				return fmt.Errorf("width probe: conflicting cached width")
			}
		} else {
			additional++
		}
	}
	if len(cache.Entries)+additional > maxWidthCacheEntries {
		return fmt.Errorf("width probe: cache capacity exceeded")
	}
	if cache.Entries == nil {
		cache.Entries = make(map[string]Cells)
	}
	for i, candidate := range batch.Candidates {
		cache.Entries[candidate] = batch.Widths[i]
	}
	return nil
}

func writeProbeOutput(writer io.Writer, output []byte) error {
	n, err := writer.Write(output)
	if err == nil && n != len(output) {
		return io.ErrShortWrite
	}
	return err
}

// measureWidths is a serialized transaction for the replacement renderer.
// readTerminal must read the terminal directly, never the editor's queued keys.
// All probes are written before the first read. Invalid observations drain the
// batch and block probes for this command; I/O failures return for editor exit.
// On successful I/O the scratch row is empty and the cursor is at the region
// origin, ready for repaint. Keyboard input remains queued in arrival order.
func (state *TermState) measureWidths(writer io.Writer, readTerminal func() (TerminalToken, error), region *ProbeRegion, batch *WidthProbeBatch, candidates []string) (err error) {
	if len(batch.Candidates) > batch.RepliesReceived {
		return fmt.Errorf("width probe: previous batch has outstanding replies")
	}
	if state.widthProbesBlocked || region.Columns < 4 {
		return nil
	}
	batch.prepare(candidates, region.Columns, &state.widthCache, &state.eligibilityCache)
	if len(batch.Candidates) == 0 {
		return nil
	}
	if err = region.validate(); err != nil {
		batch.Clear() // Nothing has been sent; there are no replies to drain.
		return err
	}

	output, row := region.enterScratch(batch.output[:0])
	batch.ScratchRow = row
	for _, candidate := range batch.Candidates {
		output = append(output, "\r\033[2K"...)
		output = append(output, candidate...)
		output = append(output, "\033[6n\r\033[2K"...)
	}

	batch.output = output

	// Clear scratch and return to the origin on every exit, including EOF and
	// write failure. After an I/O error terminal position is untrusted and the
	// caller must exit; this recovery write is best effort in that case.
	defer func() {
		cleanup := append(batch.output[:0], "\r\033[2K"...)
		cleanup = appendProbeCursorControl(cleanup, region.PaintedRows, 'A')
		cleanup = appendProbeCursorControl(cleanup, int(region.OriginCol), 'G')
		batch.output = cleanup
		cleanupErr := writeProbeOutput(writer, cleanup)
		if err == nil {
			err = cleanupErr
		}
		if err != nil {
			batch.Failure = err
		} else {
			region.CursorRow = 0
			batch.Failure = batch.commit(&state.widthCache)
		}
		if batch.Failure != nil {
			state.widthProbesBlocked = true
		}
	}()

	if err = writeProbeOutput(writer, output); err != nil {
		return err
	}
	for batch.RepliesReceived < len(batch.Candidates) {
		var token TerminalToken
		token, err = readTerminal()
		if err != nil {
			return err
		}
		if _, eof := token.(EofTerminalToken); eof {
			return io.EOF
		}
		if report, ok := token.(CsiToken); ok && report.FinalChar == 'R' {
			batch.acceptReply(report)
		} else {
			state.queuedInput = append(state.queuedInput, token)
		}
	}
	return nil
}

// Avoid fmt's variadic argument boxing when terminal coordinates exceed the
// runtime's small-integer cache. The destination belongs to the probe batch.
func appendProbeCursorControl(dst []byte, coordinate int, final byte) []byte {
	dst = append(dst, '\x1b', '[')
	dst = strconv.AppendInt(dst, int64(coordinate), 10)
	return append(dst, final)
}
