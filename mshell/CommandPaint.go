package main

import (
	"errors"
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
	if (region.OriginRow < 1 && !(region.RelativeOrigin && region.OriginRow == 0)) || int(region.OriginRow) > region.ScreenRows || region.OriginCol < 1 || Cells(region.OriginCol) > region.Columns {
		return fmt.Errorf("command paint: invalid region origin")
	}
	if region.RelativeOrigin && region.OriginRow != 0 { return fmt.Errorf("command paint: relative origin has an absolute row") }
	if region.PaintedRows < 1 || region.PaintedRows > region.ScreenRows || region.CursorRow < 0 || int(region.CursorRow) >= region.PaintedRows || region.ViewportStart < 0 || region.TrailerRows < 0 {
		return fmt.Errorf("command paint: invalid previous frame")
	}
	if region.TrailerRows > 0 && !region.ScratchOwned {
		return fmt.Errorf("command paint: trailer rows must own scratch")
	}
	if region.PromptHidden && (region.OriginCol != 1 || region.CommandStartCol < 0 || region.CommandStartCol >= region.Columns) {
		return fmt.Errorf("command paint: invalid hidden prompt geometry")
	}
	if region.ownedRows() > region.ScreenRows || int(region.OriginRow)+region.ownedRows()-1 > region.ScreenRows {
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
	if len(layout.Rows) == 0 || state.queuedInputIndex < len(state.queuedInput) || !state.displayFrameCurrent() || state.displayCursor != state.index {
		return dst, region, fmt.Errorf("command paint: no current prepared frame")
	}
	if state.displayColumns != region.Columns || state.displayStartCol != region.commandStartCol() {
		return dst, region, fmt.Errorf("command paint: geometry changed since preparation")
	}
	if layout.CursorRow < 0 || int(layout.CursorRow) >= len(layout.Rows) || layout.CursorCol < 0 || layout.CursorCol > region.Columns {
		return dst, region, fmt.Errorf("command paint: invalid layout cursor")
	}

	view := commandViewportFor(layout, region)

	// Completion rows fill whatever the command leaves, one row per line, and
	// never appear once a tall command owns the screen.
	trailerLines, trailerHighlights := state.completionTrailerLines(region.ScreenRows-view.Rows, view.HidePrompt, region.Columns)
	trailers := len(trailerLines)

	previousOwned := region.ownedRows()
	wantedRows := view.Rows + trailers
	if view.ReserveScratch && trailers == 0 { wantedRows++ }
	ownedRows := max(previousOwned, wantedRows)
	next := region
	if !region.RelativeOrigin {
		next.OriginRow -= OneBasedTerminalCoord(max(0, int(region.OriginRow)+ownedRows-1-region.ScreenRows))
	}
	next.CursorRow = view.CursorRow
	next.PaintedRows = view.Rows
	next.TrailerRows = trailers
	next.ViewportStart = view.Start
	if view.HidePrompt {
		next.CommandStartCol = region.commandStartCol()
		next.PromptHidden = true
		next.OriginCol = 1
	}
	// A cleared leftover row can be reused as scratch immediately below the
	// new contents. Any further cleared rows are relinquished.
	next.ScratchOwned = trailers > 0 || ownedRows > view.Rows

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
	// Printable-ASCII rows tile the source, so byte offsets accumulate; rows
	// above the viewport still advance the offset without painting.
	styles := stylePainter{spans: state.displayStyles}
	var offset ByteOffset
	for i := 0; i < int(view.Start) && len(state.displayAtoms) == 0; i++ { offset += ByteOffset(len(layout.Rows[i].Text)) }
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
			dst = styles.appendText(dst, offset, row.Text)
			offset += ByteOffset(len(row.Text))
		} else {
			for j := row.AtomStart; j < row.AtomEnd; j++ {
				atom := state.displayAtoms[j]
				dst = styles.appendText(dst, atom.SourceStart, atom.displayText(state.displaySource))
			}
		}
	}
	// A full final row can put the cursor in a blank logical row after the
	// command. It may be the entire viewport on a one-row terminal.
	paintedLayoutRows := visibleEnd-int(view.Start)
	if view.Rows > paintedLayoutRows && paintedLayoutRows > 0 { dst = append(dst, '\r', '\n') }
	// Each trailer row is truncated to atoms that fit one row, so painting it
	// can never wrap. CRLF first cancels any pending wrap from the row above.
	for i, line := range trailerLines {
		dst = append(dst, "\033[0m\r\n"...)
		dst = state.appendTrailerRow(dst, SourceText(line), trailerHighlights[i], region.Columns)
	}
	// CR cancels pending wrap without advancing a row. Cursor movement cannot
	// accidentally trigger a second wrap after an exact fill or forced CRLF.
	dst = append(dst, "\033[0m\r"...)
	if distance := view.Rows+trailers-1-int(view.CursorRow); distance > 0 { dst = appendProbeCursorControl(dst, distance, 'A') }
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
	state.displaySuggestion = 0
	if suffix := state.suggestionSuffix(); len(suffix) > 0 {
		state.displaySource += suffix
		state.displaySuggestion = len(suffix)
	}
	state.displayStyles = state.commandStyleSpansInto(state.displayStyles[:0], state.currentCommand)
	if state.displaySuggestion > 0 {
		state.displayStyles = append(state.displayStyles, styleSpan{Start: ByteOffset(len(state.currentCommand)), End: ByteOffset(len(state.displaySource)), SGR: "\033[90m"})
	}
	state.displayCursor = state.index
	state.displayStartCol = startCol
	state.displayColumns = columns

	if state.queuedInputIndex < len(state.queuedInput) || columns < 4 || columns > Cells(maxTerminalCoordinate) || startCol < 0 || startCol >= columns {
		return false, nil
	}

	if isAllPrintableAscii(state.displaySource) {
		state.widthMisses = state.appendCompletionMisses(state.widthMisses, columns)
		if measure != nil && !state.widthProbesBlocked && len(state.widthMisses) > 0 {
			if err := measure(state.widthMisses); err != nil {
				return false, err
			}
			if state.queuedInputIndex < len(state.queuedInput) {
				return false, nil
			}
		}
		state.displayLayout = layoutPrintableAsciiInto(state.displayLayout.Rows, state.displaySource, state.index, startCol, columns)
		return true, nil
	}

	state.displayAtoms = segmentAtomsInto(state.displayAtoms, state.displaySource)
	state.widthMisses = resolveCachedWidths(state.widthMisses, state.displaySource, state.displayAtoms, &state.widthCache, &state.eligibilityCache, columns)
	state.widthMisses = state.appendCompletionMisses(state.widthMisses, columns)
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

// paintPrompt lays out the known prompt text with the same atoms, measured
// widths, placeholders and wrap rules as command text. The caller is in raw
// mode. Starting at column one and ending at the layout's cursor establishes
// the command origin without asking the terminal for its position.
func (state *TermState) paintPrompt(writer io.Writer, readTerminal func() (TerminalToken, error), source SourceText) error {
	columns := Cells(state.numCols)
	if columns < 4 || columns > Cells(maxTerminalCoordinate) || state.numRows < 1 || state.numRows > int(maxTerminalCoordinate) {
		return fmt.Errorf("prompt: unsupported terminal geometry")
	}
	if err := writeProbeOutput(writer, []byte("\r\033[0m")); err != nil { return err }
	region := ProbeRegion{RelativeOrigin: true, OriginCol: 1, PaintedRows: 1, ScreenRows: state.numRows, Columns: columns}
	atoms := segmentAtomsInto(nil, source)
	misses := resolveCachedWidths(nil, source, atoms, &state.widthCache, &state.eligibilityCache, columns)
	if len(misses) > 0 && state.numRows > 1 {
		if err := state.measureWidths(writer, readTerminal, &region, &state.widthBatch, misses); err != nil { return err }
	}
	finishWidthResolution(source, atoms, &state.widthCache)
	layout := layoutAtomsInto(nil, atoms, ByteOffset(len(source)), 0, columns)
	output := []byte("\033[35m")
	for i, row := range layout.Rows {
		if i > 0 && layout.Rows[i-1].EndType != RowEndSoftExact {
			output = append(output, '\r', '\n')
		}
		for j := row.AtomStart; j < row.AtomEnd; j++ {
			output = append(output, atoms[j].displayText(source)...)
		}
	}
	row, col := int(layout.CursorRow), int(layout.CursorCol)
	if layout.PendingWrap {
		output = append(output, '\r', '\n')
		row++
		col = 0
	}
	output = append(output, "\033[0m"...)
	if err := writeProbeOutput(writer, output); err != nil { return err }
	state.numPromptLines = row+1
	origin := 0
	if !region.RelativeOrigin { origin = min(state.numRows, int(region.OriginRow)+row) }
	state.anchorCommandRegion(origin, col+1)
	state.commandRegion.RelativeOrigin = region.RelativeOrigin
	state.promptRow = origin
	state.promptMaxWidth = reflowSafeWidth(layout.Rows, 0)
	if layout.PendingWrap { state.promptMaxWidth = UnresolvedWidth }
	return nil
}

// reflowSafeWidth returns the widest row of a layout whose rows a terminal
// reflow cannot rejoin or split: every row ends at a hard break or is the last
// one. Any soft wrap returns UnresolvedWidth. startCol is the first row's
// offset, since it shares the row with the prompt.
func reflowSafeWidth(rows []LayoutRow, startCol Cells) Cells {
	widest := Cells(0)
	for i, row := range rows {
		if row.EndType != RowEndHard && row.EndType != RowEndFinal { return UnresolvedWidth }
		width := row.Width
		if i == 0 { width += startCol }
		widest = max(widest, width)
	}
	return widest
}

// adoptResizedGeometry keeps the painted prompt and command in place after a
// resize when the terminal cannot have reflowed them: no row was soft-wrapped
// and every row is narrower than both the old and the new width. Shrinking the
// height keeps the cursor visible, so rows below it may be gone; only a cursor
// on the last painted row is trusted then. The absolute origin is dropped
// because rows above may have scrolled; painting only needs relative rows.
// Returns false when a fresh prompt is required instead.
func (state *TermState) adoptResizedGeometry(columns Cells, rows int) bool {
	region := &state.commandRegion
	if columns < 4 || rows < 1 || region.PromptHidden || region.TrailerRows > 0 || region.ViewportStart != 0 {
		return false
	}
	if state.promptMaxWidth == UnresolvedWidth || state.promptMaxWidth >= min(columns, region.Columns) {
		return false
	}
	layout := state.displayLayout
	if len(layout.Rows) > 0 {
		if state.displayColumns != region.Columns || len(layout.Rows) != region.PaintedRows { return false }
		width := reflowSafeWidth(layout.Rows, region.commandStartCol())
		if width == UnresolvedWidth || width >= min(columns, region.Columns) { return false }
	} else if region.PaintedRows != 1 {
		return false
	}
	if rows < region.ScreenRows && int(region.CursorRow) < region.PaintedRows-1 { return false }
	if region.PaintedRows > rows { return false }

	region.Columns = columns
	region.ScreenRows = rows
	region.OriginRow = 0
	region.RelativeOrigin = true
	region.ScratchOwned = false
	return region.validatePaintRegion() == nil
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
	state.promptMaxWidth = UnresolvedWidth
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

// refreshInteractiveDisplay paints the editor after a token is handled. It
// measures widths through the anchored region and leaves a frame unpainted
// only while keys queued during measurement still wait for the editor loop.
func (state *TermState) refreshInteractiveDisplay(renderHistory bool) error {
	state.showSuggestion = renderHistory
	if renderHistory {
		state.updateHistoryCompletion()
	}

	// A resize reflows rows the region no longer describes. Re-anchor from a
	// fresh prompt rather than trusting the terminal's reflow, unless nothing
	// painted could have reflowed. A new pane often reports its parent's size
	// until the first key arrives, and that must not draw a second prompt.
	previousCols, previousRows := state.numCols, state.numRows
	state.UpdateSize()
	if Cells(state.numCols) != state.commandRegion.Columns || state.numRows != state.commandRegion.ScreenRows {
		if state.adoptResizedGeometry(Cells(state.numCols), state.numRows) {
			state.Logf("Terminal resized %dx%d -> %dx%d; kept prompt in place\n", previousCols, previousRows, state.numCols, state.numRows)
		} else {
			state.Logf("Terminal resized %dx%d -> %dx%d; re-anchoring prompt\n", previousCols, previousRows, state.numCols, state.numRows)
			if _, err := os.Stdout.WriteString("\r\n"); err != nil { return err }
			if err := state.printPrompt(); err != nil { return err }
		}
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

// A style span covers source bytes [Start, End) with one SGR sequence. Spans
// are sorted, non-overlapping, and never enter layout or width arithmetic.
type styleSpan struct {
	Start ByteOffset
	End   ByteOffset
	SGR   string
}

// stylePainter walks spans in source order while painting. It resets before
// switching styles so each span stands alone; the painter resets again at end.
type stylePainter struct {
	spans   []styleSpan
	next    int
	current string
}

func (p *stylePainter) styleAt(offset ByteOffset) string {
	for p.next < len(p.spans) && p.spans[p.next].End <= offset { p.next++ }
	if p.next < len(p.spans) && p.spans[p.next].Start <= offset { return p.spans[p.next].SGR }
	return ""
}

func (p *stylePainter) switchTo(dst []byte, sgr string) []byte {
	if sgr == p.current { return dst }
	if p.current != "" { dst = append(dst, "\033[0m"...) }
	dst = append(dst, sgr...)
	p.current = sgr
	return dst
}

// appendText paints text whose first byte sits at offset in the display
// source. Atoms take the style at their first byte; ASCII rows may split.
func (p *stylePainter) appendText(dst []byte, offset ByteOffset, text string) []byte {
	for len(text) > 0 {
		sgr := p.styleAt(offset)
		end := len(text)
		if p.next < len(p.spans) {
			if sgr == "" {
				end = min(end, int(p.spans[p.next].Start-offset))
			} else {
				end = min(end, int(p.spans[p.next].End-offset))
			}
		}
		if end <= 0 { end = len(text) }
		dst = p.switchTo(dst, sgr)
		dst = append(dst, text[:end]...)
		offset += ByteOffset(end)
		text = text[end:]
	}
	return dst
}

// suggestionSuffix is the history ghost text after the current command, or
// empty when suggestions are hidden or the completion no longer matches.
func (state *TermState) suggestionSuffix() SourceText {
	if !state.showSuggestion || len(state.historyComplete) <= len(state.currentCommand) || state.historyComplete[:len(state.currentCommand)] != state.currentCommand {
		return ""
	}
	return state.historyComplete[len(state.currentCommand):]
}

func (state *TermState) displayFrameCurrent() bool {
	suffix := state.suggestionSuffix()
	return len(state.displaySource) == len(state.currentCommand)+len(suffix) &&
		state.displaySource[:len(state.currentCommand)] == state.currentCommand &&
		state.displaySource[len(state.currentCommand):] == suffix
}

// commandStyleSpansInto tokenizes the command for syntax highlighting. Tokens
// tile the input when whitespace and comments are emitted, so byte offsets
// accumulate from lexeme lengths. A lex error leaves the command unstyled.
func (s *TermState) commandStyleSpansInto(dst []styleSpan, command SourceText) []styleSpan {
	if len(command) == 0 { return dst }
	if s.l == nil { s.l = NewLexer("", &TokenFile{"REPL"}) }
	s.l.allowUnterminatedString = true
	s.l.emitWhitespace = true
	s.l.emitComments = true
	s.l.resetInput(string(command))
	defer func() {
		s.l.allowUnterminatedString = false
		s.l.emitWhitespace = false
		s.l.emitComments = false
	}()
	tokens, err := s.l.Tokenize()
	if err != nil { return dst }

	commandLiteralIndex := -1
	firstTokenIsBinary := false
	if s.context.Pbm != nil {
		commandLiteralIndex = s.commandLiteralTokenIndex(tokens)
		_, firstTokenIsBinary = s.isFirstTokenBinary(tokens)
	}
	var offset ByteOffset
	for i, t := range tokens {
		var sgr string
		switch t.Type {
		case STRING, SINGLEQUOTESTRING, FORMATSTRING: sgr = "\033[31m"
		case UNFINISHEDSTRING, UNFINISHEDSINGLEQUOTESTRING: sgr = "\033[91m"
		case UNFINISHEDPATH: sgr = "\033[95m"
		case PATH: sgr = "\033[35m"
		case DATETIME: sgr = "\033[36m"
		case TRUE, FALSE: sgr = "\033[34m"
		case VARSTORE, ENVSTORE: sgr = "\033[32m"
		case VARRETRIEVE, ENVRETREIVE, ENVCHECK: sgr = "\033[33m"
		case LITERAL:
			underline := false
			if firstTokenIsBinary {
				if _, ok := BuiltInList[t.Lexeme]; ok || IsDefinitionDefined(t.Lexeme, s.stdLibDefs) { underline = true }
			}
			if i == commandLiteralIndex {
				sgr = "\033[4;34m"
			} else if underline {
				sgr = "\033[4m"
			}
		default:
			if i == commandLiteralIndex { sgr = "\033[4;34m" }
		}
		end := offset + ByteOffset(len(t.Lexeme))
		if sgr != "" { dst = append(dst, styleSpan{Start: offset, End: end, SGR: sgr}) }
		offset = end
	}
	return dst
}

// currentCompletions returns the matches to show below the command and the
// index highlighted while cycling, mirroring the legacy renderer's choice.
func (state *TermState) currentCompletions() ([]string, int) {
	matches := state.tabCompletions0
	if state.currentTabComplete != 0 { matches = state.tabCompletions1 }
	highlight := -1
	if state.tabCycleActive { highlight = state.tabCycleIndex }
	return matches, highlight
}

// appendCompletionMisses adds unknown cluster widths from completion matches
// to the frame's width batch so trailer rows can be painted exactly. Matches
// are joined with hard breaks, which resolve to nothing.
func (state *TermState) appendCompletionMisses(misses []string, columns Cells) []string {
	matches, _ := state.currentCompletions()
	if len(matches) == 0 { return misses }
	state.trailerSource = state.trailerSource[:0]
	for _, match := range matches {
		state.trailerSource = append(state.trailerSource, match...)
		state.trailerSource = append(state.trailerSource, '\n')
	}
	source := SourceText(state.trailerSource)
	if isAllPrintableAscii(source) { return misses }
	state.trailerAtoms = segmentAtomsInto(state.trailerAtoms[:0], source)
	state.trailerMisses = resolveCachedWidths(state.trailerMisses, source, state.trailerAtoms, &state.widthCache, &state.eligibilityCache, columns)
	return append(misses, state.trailerMisses...)
}

func (state *TermState) completionTrailerLines(availableRows int, hidden bool, columns Cells) ([]string, []highlightRange) {
	matches, highlight := state.currentCompletions()
	if hidden || availableRows <= 0 || len(matches) == 0 { return nil, nil }
	return completionDisplayRowsPlain(matches, highlight, min(tabCompletionColumnLimit, availableRows), availableRows, int(columns))
}

// appendTrailerRow paints the atoms of line that fit in one row from column
// one. Widths come from the session cache or placeholders; nothing is probed.
func (state *TermState) appendTrailerRow(dst []byte, line SourceText, highlight highlightRange, columns Cells) []byte {
	state.trailerAtoms = segmentAtomsInto(state.trailerAtoms[:0], line)
	state.trailerMisses = resolveCachedWidths(state.trailerMisses, line, state.trailerAtoms, &state.widthCache, &state.eligibilityCache, columns)
	finishWidthResolution(line, state.trailerAtoms, &state.widthCache)
	state.trailerLayout = layoutAtomRowsInto(state.trailerLayout[:0], state.trailerAtoms, 0, columns)
	if len(state.trailerLayout) == 0 { return dst }
	styles := stylePainter{}
	if highlight.End > highlight.Start {
		styles.spans = []styleSpan{{Start: ByteOffset(highlight.Start), End: ByteOffset(highlight.End), SGR: "\033[7m"}}
	}
	row := state.trailerLayout[0]
	for j := row.AtomStart; j < row.AtomEnd; j++ {
		atom := state.trailerAtoms[j]
		if atom.Kind == AtomHardBreak { break }
		dst = styles.appendText(dst, atom.SourceStart, atom.displayText(line))
	}
	return dst
}

// anchorPrompt establishes the editing origin after the opaque prompt. A
// garbled reply cannot anchor anything, so move to a fresh line at column one
// and ask once more; a second bad reply means the terminal is unsupported.
// Silence is never an error here: the query blocks until a reply arrives.
func (state *TermState) anchorPrompt(writer io.Writer) error {
	row, col, err := state.queryCursorPosition(writer)
	if errors.Is(err, errMalformedCursorReport) {
		state.Logf("Prompt anchor: %s; re-anchoring on a fresh line\n", err)
		if _, err = io.WriteString(writer, "\r\n"); err != nil { return err }
		row, col, err = state.queryCursorPosition(writer)
	}
	if err != nil { return err }
	state.promptRow = row
	state.anchorCommandRegion(row, col)
	return nil
}
