package main

// The file manager's keys (ai/fm-bindings.typ). Its input is decoded by
// the shell's terminal lexer, each key is resolved against the file
// manager's bindings (FmBindings.go) by the shared resolver
// (KeyResolver.go), and the program a binding gives runs here, along with
// the prompt line, pastes, questions, the prefix popup and the help screen.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

// maxFmLineBytes is the most the prompt line holds: the longest path Linux
// accepts (section 7.4).
const maxFmLineBytes = 4096

// fmLine is the prompt line: what it is for, its text, and its cursor.
type fmLine struct {
	prompt fmPrompt
	text   SourceText
	index  ByteOffset
	// scroll is where the part of the text shown starts, when the line is
	// wider than the terminal.
	scroll ByteOffset
}

// fmSnapshot is what a pure binding of the file manager changes: the
// cursor and scroll of the listing, the prompt line, and the scroll of the
// help (section 6).
type fmSnapshot struct {
	cursor, offset int
	text           SourceText
	index          ByteOffset
	helpOffset     int
}

// fmOpResult says how a program goes on after an op.
type fmOpResult uint8

const (
	fmNext fmOpResult = iota // the next op runs
	fmStop                   // the program stops
	fmEnd                    // the file manager ends
)

// isFmWordEnd reports whether a byte ends a word in the prompt line: a
// space or a '.', so a word op stops at a file name's extension.
func isFmWordEnd(b byte) bool { return b == ' ' || b == '.' }

// readTerminalToken reads one token with the shell's terminal lexer.
func readTerminalToken(r *StdinReaderState) (TerminalToken, error) {
	// The lexer logs through its TermState only, and a nil one logs
	// nothing.
	var lexer *TermState
	return lexer.InteractiveLexer(r)
}

// evalState is the evaluator whose bindings and definitions the file
// manager uses. A file manager made without one, as in tests, gets an
// empty one.
func (fm *FileManager) evalState() *EvalState {
	if fm.ev == nil {
		fm.ev = &EvalState{}
	}
	if fm.context.Variables == nil {
		fm.context.Variables = map[string]MShellObject{}
	}
	if fm.context.Pbm == nil {
		fm.context.Pbm = NewPathBinManager()
	}
	return fm.ev
}

// handleNextInput reads input and handles every token of one read, so the
// screen is drawn once for what arrived together. It reports whether the
// file manager should end.
func (fm *FileManager) handleNextInput() bool {
	for {
		token, err := readTerminalToken(fm.input)
		if err != nil || fm.handleToken(token) {
			return true
		}
		if fm.input.i >= fm.input.n {
			return false
		}
	}
}

// handleToken handles one token: a paste goes to the prompt line, and a
// key to the bindings. It reports whether the file manager should end.
func (fm *FileManager) handleToken(token TerminalToken) bool {
	fm.renderMu.Lock()
	defer fm.renderMu.Unlock()
	now := time.Now()
	switch t := token.(type) {
	case EofTerminalToken:
		return true
	case PasteToken:
		fm.statusMsg = ""
		fm.popup = false
		fm.keys.event(now)
		fm.paste(t.Text)
		return false
	}
	code, ok := keyOfToken(token)
	if !ok {
		// A late reply to a query, or a sequence that is no key.
		return false
	}
	fm.statusMsg = ""
	if t, ok := token.(KeyToken); ok && t.EscOf != 0 && !fm.keyUsed(code) {
		if fm.resolveKey(NamedEscape, now) {
			return true
		}
		code = t.EscOf
	}
	return fm.resolveKey(code, now)
}

// keyUsed reports whether a binding of the current mode starts with key,
// or continues the pending keys with it.
func (fm *FileManager) keyUsed(key KeyCode) bool {
	ev := fm.evalState()
	ev.ensureFmDefaultBindings()
	return fm.keys.used(&ev.fmBindingTries[fm.mode], key)
}

// resolveKey handles one key: it runs the longest binding the key
// completes, or does the key's own action, and keeps the keys that may
// still become part of a binding. It reports whether the file manager
// should end.
func (fm *FileManager) resolveKey(code KeyCode, now time.Time) bool {
	ev := fm.evalState()
	ev.ensureFmDefaultBindings()
	r := &fm.keys
	if fm.mode != r.mode {
		r.clear()
		r.mode = fm.mode
	}
	fm.popup = false
	w := r.push(code, now, fm.snapshot())
	trie := &ev.fmBindingTries[fm.mode]
	fired, end, _ := r.fireLongest(trie, w, fm.fireBinding)
	if fired {
		// A mode change ends a pending sequence.
		if fm.mode != r.mode {
			r.clear()
			r.mode = fm.mode
		}
		return end
	}
	_, typed := code.text()
	if typed && fm.mode == fmModeInsert {
		fm.replaceLine(string(code.baseRune()), fm.line.index, fm.line.index)
	}
	r.keep(trie, w)
	// Keys waiting for more, which show nothing yet, list what can follow.
	fm.popup = r.n > 0 && !(typed && fm.mode == fmModeInsert)
	return false
}

// fireBinding runs a binding for the keys of w, from the snapshot before
// them. It reports false when the binding's quote declined.
func (fm *FileManager) fireBinding(e *fmBindingEntry, w []pendingKey[fmSnapshot]) (bool, bool, error) {
	program, ok := fm.bindingProgram(e, w)
	if !ok {
		return false, false, nil
	}
	fm.restore(w[0].snap)
	return true, fm.runProgram(program, w[len(w)-1].code), nil
}

func (fm *FileManager) snapshot() fmSnapshot {
	return fmSnapshot{cursor: fm.cursor, offset: fm.offset, text: fm.line.text, index: fm.line.index, helpOffset: fm.helpOffset}
}

func (fm *FileManager) restore(s fmSnapshot) {
	fm.cursor, fm.offset, fm.helpOffset = s.cursor, s.offset, s.helpOffset
	if fm.mode == fmModeInsert && (fm.line.text != s.text || fm.line.index != s.index) {
		fm.line.text, fm.line.index = s.text, s.index
		fm.lineChanged()
	}
}

// bindingProgram is the program a binding gives for the keys of w: its
// ops, or what its quote returns. It reports false when the quote declined
// or failed.
func (fm *FileManager) bindingProgram(e *fmBindingEntry, w []pendingKey[fmSnapshot]) ([]fmOp, bool) {
	if e.fn == nil {
		return e.ops, true
	}
	keys := make([]KeyCode, len(w))
	for i, k := range w {
		keys[i] = k.code
	}
	stack := MShellStack{fm.stateValue(w[0].snap, keys)}
	return fm.runLimitedQuote(e.fn, &stack, "The quote of binding "+e.describe())
}

// runLimitedQuote runs the quote of a fmFunc or a fmPromptAsk as an
// abbreviation's quote runs: on its own stack, stopped after a second,
// with its output captured (section 7.2). It gives the program the quote
// returned, or false when the quote declined or failed. A failure is shown
// at the bottom. what names the quote, starting a sentence.
func (fm *FileManager) runLimitedQuote(quote *MShellQuotation, stack *MShellStack, what string) ([]fmOp, bool) {
	ev := fm.evalState()
	var out bytes.Buffer
	context := fm.context
	context.StandardOutput = &out
	context.StandardError = &out
	run := ev.runUserQuote(quote, stack, context, fm.definitions, abbrTimeout, newEvalCancel())
	var problem string
	switch {
	case run.err != nil:
		problem = run.err.Error()
	case run.result.ExitCalled:
		problem = what + " called 'exit'; that does not end the file manager."
	case run.result.Success:
		m, ok := singleMaybe(*stack)
		if !ok {
			problem = what + " must leave one Maybe[FmProgram] on the stack."
			break
		}
		if m.IsNone() {
			return nil, false
		}
		program, err := parseFmOps(m.obj)
		if err == nil {
			return program, true
		}
		problem = what + ": " + err.Error() + "."
	default:
		problem = run.failure(what, abbrTimeout)
	}
	fm.statusMsg = firstLine(problem)
	return nil, false
}

// firstLine is the first line of a message that is not empty.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// stateValue is the file manager as a binding sees it: an FmState, with
// the cursor and line of a snapshot.
func (fm *FileManager) stateValue(snap fmSnapshot, keys []KeyCode) *MShellDict {
	ev := fm.evalState()
	info := NewDict()
	info.Items["dir"] = MShellString{fm.currentDir}
	current := &Maybe{}
	if snap.cursor < len(fm.entries) {
		current = &Maybe{obj: MShellString{fm.selectedEntryPath(fm.entries[snap.cursor])}}
	}
	info.Items["current"] = current
	selected := NewList(len(fm.selected))
	for i, p := range fm.selected {
		selected.Items[i] = MShellString{p}
	}
	info.Items["selected"] = selected
	info.Items["mode"] = ev.builtinEnumValue(fmModeMembers[fm.mode])
	line := &Maybe{}
	if fm.mode == fmModeInsert {
		l := NewDict()
		l.Items["prompt"] = ev.fmPromptValue(fm.line.prompt)
		l.Items["before"] = MShellString{string(snap.text[:snap.index])}
		l.Items["after"] = MShellString{string(snap.text[snap.index:])}
		line = &Maybe{obj: l}
	}
	info.Items["line"] = line
	keyList := NewList(len(keys))
	for i, k := range keys {
		keyList.Items[i] = ev.keyValue(k)
	}
	info.Items["keys"] = keyList
	return info
}

// ----- Running ops (section 7.1) -----

// runProgram runs the ops of a program in order. key is the last key of
// the sequence that ran it, which editSelfInsert types. An op that cannot
// run in the mode it finds stops the program. It reports whether the file
// manager should end.
func (fm *FileManager) runProgram(program []fmOp, key KeyCode) bool {
	for _, op := range program {
		if !op.runsIn(fm.mode) {
			fm.statusMsg = fmt.Sprintf("'%s' cannot run in %s; the rest of the binding did not run.", op.name(), fmModeNames[fm.mode])
			return false
		}
		switch fm.runOp(op, key) {
		case fmEnd:
			return true
		case fmStop:
			return false
		}
	}
	return false
}

func (fm *FileManager) runOp(op fmOp, key KeyCode) fmOpResult {
	if fm.mode == fmModeHelp && op.isMove() {
		fm.helpMove(op)
		return fmNext
	}
	switch op.kind {
	case fmOpEdit:
		if !fm.editLine(op.edit, key) {
			return fmStop
		}
	case fmOpDown:
		fm.moveCursor(1)
	case fmOpUp:
		fm.moveCursor(-1)
	case fmOpTop:
		fm.cursor, fm.offset = 0, 0
	case fmOpBottom:
		fm.cursor = len(fm.entries) - 1
		fm.clampCursor()
		fm.adjustScroll()
	case fmOpDownBy:
		fm.moveCursor(fm.distanceRows(op.dist))
	case fmOpUpBy:
		fm.moveCursor(-fm.distanceRows(op.dist))
	case fmOpSearchNext:
		fm.searchNext()
	case fmOpSearchPrev:
		fm.searchPrev()
	case fmOpCursorTo:
		fm.cursorTo(op.a)
	case fmOpOpen:
		fm.enterSelected()
	case fmOpParent:
		fm.goParent()
	case fmOpCd:
		fm.cd(op.a)
	case fmOpReload:
		fm.reload()
	case fmOpOpenEditor:
		if fm.requireRealDirectory() {
			fm.openEditor()
		}
	case fmOpToggle:
		fm.toggleCurrent()
	case fmOpSelectionInvert:
		for _, entry := range fm.entries {
			fm.toggleSelected(fm.selectedEntryPath(entry))
		}
	case fmOpSelectionClear:
		fm.clearSelection()
	case fmOpPrompt:
		fm.openPrompt(op.prompt)
	case fmOpCut:
		if fm.requireRealDirectory() {
			fm.clipboardAdd("cut")
		}
	case fmOpCopy:
		if fm.requireRealDirectory() {
			fm.clipboardAdd("copy")
		}
	case fmOpPaste:
		if fm.requireRealDirectory() {
			fm.clipboardPaste()
		}
	case fmOpClipboardClear:
		clearClipboard()
	case fmOpYankName:
		fm.yankSystemClipboard(yankFileName)
	case fmOpYankPath:
		fm.yankSystemClipboard(yankFullPath)
	case fmOpYankGitPath:
		fm.yankSystemClipboard(yankGitPath)
	case fmOpTrash:
		if fm.requireRealDirectory() {
			fm.deleteEntry()
		}
	case fmOpBookmarkSet:
		fm.askBookmark(true)
	case fmOpBookmarkGo:
		fm.askBookmark(false)
	case fmOpHelp:
		fm.mode = fmModeHelp
		fm.helpOffset = 0
	case fmOpLineAccept:
		return fm.acceptLine()
	case fmOpLineCancel:
		fm.closeLine()
	case fmOpHelpClose:
		fm.mode = fmModeNormal
	case fmOpRun, fmOpRunWait, fmOpRunOps:
		return fm.runQuote(op)
	case fmOpMessage:
		fm.statusMsg = op.a
	case fmOpQuit:
		fm.quit = true
		return fmEnd
	case fmOpQuitNoCd:
		fm.quit, fm.quitNoCd = true, true
		return fmEnd
	}
	return fmNext
}

// moveCursor moves the cursor n entries down, or up when n is negative.
func (fm *FileManager) moveCursor(n int) {
	fm.cursor += n
	fm.clampCursor()
	fm.adjustScroll()
}

// distanceRows is how many entries, or lines of the help, a distance is: a
// percentage of the rows shown, rounded down and at least one. A distance
// of zero or less is none.
func (fm *FileManager) distanceRows(d fmDistance) int {
	switch {
	case !d.positive():
		return 0
	case d.isPercent:
		return max(1, int(float64(fm.visibleRows())*d.percent/100))
	}
	return d.rows
}

// cursorTo puts the cursor on the entry with this name, if there is one.
func (fm *FileManager) cursorTo(name string) {
	for i, e := range fm.entries {
		if e.Name() == name {
			fm.cursor = i
			fm.adjustScroll()
			return
		}
	}
}

// currentName is the name of the entry under the cursor, or "".
func (fm *FileManager) currentName() string {
	if fm.cursor < len(fm.entries) {
		return fm.entries[fm.cursor].Name()
	}
	return ""
}

// cd shows a directory: one under the directory shown, or an absolute one.
func (fm *FileManager) cd(dir string) {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(fm.currentDir, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fm.statusMsg = "Not a directory: " + dir
		return
	}
	fm.currentDir = filepath.Clean(dir)
	fm.cursor, fm.offset = 0, 0
	fm.loadDirectory()
}

// reload reads the directory shown again.
func (fm *FileManager) reload() {
	if fm.showingWindowsVolumes {
		fm.showWindowsVolumes()
	} else {
		fm.loadDirectory()
	}
	fm.clampCursor()
	fm.adjustScroll()
}

// reloadKeepingCursor reads the directory again, and puts the cursor back
// on the entry it was on.
func (fm *FileManager) reloadKeepingCursor(name string) {
	fm.reload()
	fm.cursorTo(name)
}

// ----- The selection (section 7.1) -----

func (fm *FileManager) toggleCurrent() {
	if fm.cursor < len(fm.entries) {
		fm.toggleSelected(fm.selectedEntryPath(fm.entries[fm.cursor]))
	}
}

// toggleSelected adds a path to the selection, or takes it out.
func (fm *FileManager) toggleSelected(path string) {
	if fm.selectedSet[path] {
		delete(fm.selectedSet, path)
		fm.selected = slices.DeleteFunc(fm.selected, func(p string) bool { return p == path })
		return
	}
	if fm.selectedSet == nil {
		fm.selectedSet = make(map[string]bool)
	}
	fm.selectedSet[path] = true
	fm.selected = append(fm.selected, path)
}

func (fm *FileManager) clearSelection() {
	fm.selected = nil
	fm.selectedSet = nil
}

// targets are what an op that acts on the selection acts on: the selected
// entries that still exist, in the order they were selected, or else the
// current entry. Using the selection clears it. missing is how many
// selected entries no longer exist.
func (fm *FileManager) targets() (paths []string, missing int) {
	if len(fm.selected) > 0 {
		for _, p := range fm.selected {
			if _, err := os.Lstat(p); err == nil {
				paths = append(paths, p)
			} else {
				missing++
			}
		}
		fm.clearSelection()
		return paths, missing
	}
	if fm.cursor < len(fm.entries) {
		paths = append(paths, fm.selectedEntryPath(fm.entries[fm.cursor]))
	}
	return paths, 0
}

// noteMissing adds to the message how many selected entries were left out
// because they no longer exist.
func (fm *FileManager) noteMissing(missing int) {
	if missing == 0 {
		return
	}
	note := fmt.Sprintf("%d selected entries no longer exist and were left out.", missing)
	if missing == 1 {
		note = "1 selected entry no longer exists and was left out."
	}
	fm.statusMsg = strings.TrimSpace(fm.statusMsg + " " + note)
}

// clipboardAdd adds the targets to the file-manager clipboard, to be cut
// or copied.
func (fm *FileManager) clipboardAdd(op string) {
	paths, missing := fm.targets()
	for _, p := range paths {
		_ = addClipboardPath(op, p)
	}
	fm.noteMissing(missing)
}

// ----- The prompt line (section 7.4) -----

// openPrompt opens the prompt line, in insert mode. A rename needs an
// entry in a real directory, and an fmPromptAsk a text that fits.
func (fm *FileManager) openPrompt(p fmPrompt) {
	line := fmLine{prompt: p}
	switch p.kind {
	case fmLineRename:
		if !fm.requireRealDirectory() || fm.cursor >= len(fm.entries) {
			return
		}
		name := fm.entries[fm.cursor].Name()
		line.text = SourceText(name)
		line.index = ByteOffset(len(name))
		// The cursor goes before the extension.
		if ext := filepath.Ext(name); len(ext) > 0 && len(ext) < len(name) {
			line.index -= ByteOffset(len(ext))
		}
	case fmLineAsk:
		if len(p.text) > maxFmLineBytes {
			fm.statusMsg = fmt.Sprintf("The prompt did not open: its text is longer than %d bytes.", maxFmLineBytes)
			return
		}
		line.text = SourceText(p.text)
		line.index = ByteOffset(len(p.text))
	}
	fm.line = line
	fm.mode = fmModeInsert
}

// closeLine closes the prompt line, and goes back to normal mode.
func (fm *FileManager) closeLine() {
	fm.line = fmLine{}
	fm.mode = fmModeNormal
}

// acceptLine does what the prompt does on Enter, after closing the line.
func (fm *FileManager) acceptLine() fmOpResult {
	line := fm.line
	fm.closeLine()
	switch line.prompt.kind {
	case fmLineSearch:
		fm.commitSearch(string(line.text))
	case fmLineRename:
		fm.commitRename(string(line.text))
	case fmLineAsk:
		stack := MShellStack{MShellString{string(line.text)}, fm.stateValue(fm.snapshot(), nil)}
		program, ok := fm.runLimitedQuote(line.prompt.quote, &stack, "The quote of the prompt "+strconv.Quote(line.prompt.label))
		if ok && fm.runProgram(program, noKey) {
			return fmEnd
		}
	}
	return fmNext
}

// lineChanged follows a change to the prompt line: in search, the matches
// follow the line.
func (fm *FileManager) lineChanged() {
	if fm.line.prompt.kind == fmLineSearch {
		fm.updateSearchLive(string(fm.line.text))
	}
}

// replaceLine replaces the bytes start to end of the prompt line with
// text. An edit that would make the line longer than maxFmLineBytes is
// not done, and a message says so; it reports false then.
func (fm *FileManager) replaceLine(text string, start, end ByteOffset) bool {
	l := &fm.line
	n := len(l.text) - int(end-start) + len(text)
	if n > maxFmLineBytes && n > len(l.text) {
		fm.statusMsg = fmt.Sprintf("The line holds at most %d bytes.", maxFmLineBytes)
		return false
	}
	l.text = l.text[:start] + SourceText(text) + l.text[end:]
	l.index = graphemeCeil(l.text, start+ByteOffset(len(text)))
	fm.lineChanged()
	return true
}

// editLine runs a line edit on the prompt line. key is the last key of the
// sequence, which editSelfInsert types. It reports false when the edit did
// not fit in the line.
func (fm *FileManager) editLine(op editOp, key KeyCode) bool {
	l := &fm.line
	end := ByteOffset(len(l.text))
	switch op.kind {
	case opInsert:
		return fm.replaceLine(op.a, l.index, l.index)
	case opSelfInsert:
		if r, ok := key.text(); ok {
			return fm.replaceLine(string(r), l.index, l.index)
		}
	case opSetLine:
		if len(op.a)+len(op.b) > maxFmLineBytes {
			fm.statusMsg = fmt.Sprintf("The line holds at most %d bytes.", maxFmLineBytes)
			return false
		}
		l.text = SourceText(op.a + op.b)
		l.index = graphemeCeil(l.text, ByteOffset(len(op.a)))
		fm.lineChanged()
	case opLeft:
		l.index = graphemePrevious(l.text, l.index)
	case opRight:
		l.index = graphemeNext(l.text, l.index)
	case opWordLeft:
		l.index = wordLeftBy(l.text, l.index, isFmWordEnd)
	case opWordRight:
		l.index = wordRightBy(l.text, l.index, isFmWordEnd)
	case opLineStart:
		l.index = 0
	case opLineEnd:
		l.index = end
	case opBackspace:
		if l.index > 0 {
			fm.replaceLine("", graphemePrevious(l.text, l.index), l.index)
		}
	case opDelete:
		if l.index < end {
			fm.replaceLine("", graphemeFloor(l.text, l.index), graphemeNext(l.text, l.index))
		}
	case opDeleteWordLeft:
		if l.index > 0 {
			fm.replaceLine("", wordLeftBy(l.text, l.index, isFmWordEnd), l.index)
		}
	case opDeleteToStart:
		fm.replaceLine("", 0, l.index)
	case opDeleteToEnd:
		fm.replaceLine("", l.index, end)
	}
	return true
}

// paste puts a paste into the prompt line, as one edit, once line breaks
// at its end are dropped. Outside a prompt line, and when it holds another
// control character or does not fit, it is refused whole (section 8.1).
func (fm *FileManager) paste(text string) {
	if fm.mode != fmModeInsert {
		fm.statusMsg = fmt.Sprintf("Paste ignored (%d bytes): paste into a prompt line, such as / or r", len(text))
		return
	}
	text = strings.TrimRight(text, "\n")
	for _, r := range text {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			fm.statusMsg = "Paste ignored: it holds a control character, such as a tab or a line break."
			return
		}
	}
	if len(fm.line.text)+len(text) > maxFmLineBytes {
		fm.statusMsg = fmt.Sprintf("Paste ignored (%d bytes): the line holds at most %d bytes.", len(text), maxFmLineBytes)
		return
	}
	fm.replaceLine(text, fm.line.index, fm.line.index)
}

// lineLabel is what the prompt line shows before its text.
func (fm *FileManager) lineLabel() string {
	switch fm.line.prompt.kind {
	case fmLineSearch:
		return "/"
	case fmLineRename:
		return "rename: "
	}
	return terminalSafeText(fm.line.prompt.label, false)
}

// textWidth is the number of columns text takes once it is safe to show.
func textWidth(text SourceText) int {
	return uniseg.StringWidth(terminalSafeText(string(text), false))
}

// writeLine writes the prompt line, scrolled sideways to keep its cursor
// on screen, and gives the column of the cursor, from 1.
func (fm *FileManager) writeLine(buf *bytes.Buffer) int {
	l := &fm.line
	label := fm.lineLabel()
	labelW := uniseg.StringWidth(label)
	if labelW >= fm.cols-1 {
		label, labelW = "", 0
	}
	// Room for the text, with a column for the cursor after it.
	room := max(1, fm.cols-labelW-1)
	if l.scroll > l.index || int(l.scroll) > len(l.text) {
		l.scroll = graphemeFloor(l.text, l.index)
	}
	for l.scroll < l.index && textWidth(l.text[l.scroll:l.index]) > room {
		l.scroll = graphemeNext(l.text, l.scroll)
	}
	buf.WriteString(label)
	used := 0
	rest, state := string(l.text[l.scroll:]), -1
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		safe := terminalSafeText(cluster, false)
		w := uniseg.StringWidth(safe)
		if used+w > room {
			break
		}
		buf.WriteString(safe)
		used += w
	}
	return 1 + labelW + textWidth(l.text[l.scroll:l.index])
}

// ----- The help screen (section 7.5) -----

// fmModeHeadings head the help screen's list of each mode's bindings.
var fmModeHeadings = [fmModeCount]string{"Browsing", "The prompt line", "The help screen"}

// helpLines are the lines of the help screen: every binding in force, by
// mode, and then every binding that is not, with the reason.
func (fm *FileManager) helpLines() []string {
	ev := fm.evalState()
	ev.ensureFmDefaultBindings()
	width := 0
	for _, e := range ev.FmBindings {
		if e.inert == "" {
			width = max(width, uniseg.StringWidth(keysShort(e.keys)))
		}
	}
	width = min(width, 24)
	var lines []string
	for m := range fmModeCount {
		lines = append(lines, " "+fmModeHeadings[m])
		for _, e := range ev.FmBindings {
			if e.inert == "" && e.mode == m {
				keys := keysShort(e.keys)
				pad := strings.Repeat(" ", max(0, width-uniseg.StringWidth(keys)))
				lines = append(lines, "   "+keys+pad+"  "+terminalSafeText(e.what(), false))
			}
		}
		lines = append(lines, "")
	}
	var inert []string
	for _, e := range ev.FmBindings {
		if e.inert != "" {
			inert = append(inert, "   "+terminalSafeText(e.describe()+": "+e.inert, false))
		}
	}
	if len(inert) > 0 {
		lines = append(lines, " Not in force")
		lines = append(lines, inert...)
	}
	return lines
}

// helpRows is how many lines of the help are shown.
func (fm *FileManager) helpRows() int {
	return max(1, fm.rows-2)
}

// helpMove scrolls the help as a move would move the cursor.
func (fm *FileManager) helpMove(op fmOp) {
	last := max(0, len(fm.helpLines())-fm.helpRows())
	switch op.kind {
	case fmOpDown:
		fm.helpOffset++
	case fmOpUp:
		fm.helpOffset--
	case fmOpTop:
		fm.helpOffset = 0
	case fmOpBottom:
		fm.helpOffset = last
	case fmOpDownBy:
		fm.helpOffset += fm.distanceRows(op.dist)
	case fmOpUpBy:
		fm.helpOffset -= fm.distanceRows(op.dist)
	}
	fm.helpOffset = min(max(fm.helpOffset, 0), last)
}

// renderHelp draws the help screen below the header.
func (fm *FileManager) renderHelp(buf *bytes.Buffer) {
	lines := fm.helpLines()
	rows := fm.helpRows()
	fm.helpOffset = min(fm.helpOffset, max(0, len(lines)-rows))
	for row := range rows {
		buf.WriteString("\r\n")
		if i := fm.helpOffset + row; i < len(lines) {
			line := lines[i]
			if uniseg.StringWidth(line) > fm.cols {
				line = truncateMiddle(line, fm.cols)
			}
			buf.WriteString(line)
		}
	}
	buf.WriteString("\r\n")
	if fm.statusMsg != "" {
		fm.writeStatus(buf)
		return
	}
	last := min(len(lines), fm.helpOffset+rows)
	bottom := fmt.Sprintf(" Help: lines %d-%d of %d", min(fm.helpOffset+1, last), last, len(lines))
	buf.WriteString("\033[90m" + bottom + "\033[0m")
}

// ----- The prefix popup (section 6.1) -----

// popupLines are the lines of the prefix popup: titled with the pending
// keys, the bindings in force that continue them, with the keys still to
// type.
func (fm *FileManager) popupLines() []string {
	ev := fm.evalState()
	pending := fm.keys.pendingCodes()
	lines := []string{" " + keysShort(pending) + " "}
	var rest []string
	var what []string
	width := 0
	for _, e := range ev.FmBindings {
		if e.inert != "" || e.mode != fm.mode || len(e.keys) <= len(pending) || !slices.Equal(e.keys[:len(pending)], pending) {
			continue
		}
		keys := keysShort(e.keys[len(pending):])
		width = max(width, uniseg.StringWidth(keys))
		rest = append(rest, keys)
		what = append(what, terminalSafeText(e.what(), false))
	}
	for i := range rest {
		pad := strings.Repeat(" ", width-uniseg.StringWidth(rest[i]))
		lines = append(lines, "  "+rest[i]+pad+"  "+what[i])
	}
	return lines
}

// ----- Questions (section 9) -----

// readQuestionKey reads the key that answers a question. It ends any
// pending sequence, and the key reaches no binding. A paste or the end of
// the input is no answer.
func (fm *FileManager) readQuestionKey() (KeyCode, bool) {
	fm.keys.clear()
	fm.popup = false
	for {
		token, err := readTerminalToken(fm.input)
		if err != nil {
			return 0, false
		}
		switch t := token.(type) {
		case EofTerminalToken, PasteToken:
			return 0, false
		case KeyToken:
			if t.EscOf != 0 {
				// Escape and then a key: Escape answers.
				return NamedEscape, true
			}
		}
		if code, ok := keyOfToken(token); ok {
			return code, true
		}
	}
}

// ask shows a question in a box over the screen and reads its answer.
func (fm *FileManager) ask(lines []string) (KeyCode, bool) {
	fm.asking = true
	defer func() { fm.asking = false }()
	fm.render()
	var buf bytes.Buffer
	fm.drawOverlayBox(&buf, lines)
	fm.ttyOut.Write(buf.Bytes())
	return fm.readQuestionKey()
}

// answerRune is the character an answer types, or 0.
func answerRune(code KeyCode) rune {
	if r, ok := code.text(); ok {
		return r
	}
	return 0
}

// askBookmark asks for a bookmark's key, and sets the bookmark to the
// directory shown, or goes to it.
func (fm *FileManager) askBookmark(set bool) {
	code, ok := fm.ask(fm.bookmarkLines(set))
	r := answerRune(code)
	if !ok || r > 127 || !isBookmarkChar(byte(r)) {
		return
	}
	c := byte(r)
	if set {
		if fm.bookmarks == nil {
			fm.bookmarks = make(map[byte]string)
		}
		fm.bookmarks[c] = fm.currentDir
		saveBookmarks(fm.bookmarks)
		return
	}
	if dir, ok := fm.bookmarks[c]; ok {
		fm.currentDir = dir
		fm.cursor, fm.offset = 0, 0
		fm.loadDirectory()
	}
}

// ----- The terminal (sections 7.3 and 10.3) -----

// setBracketedPaste and setKittyKeys turn bracketed paste and the kitty
// keyboard protocol on while the file manager has the terminal, and off
// before anything else gets it.
func (fm *FileManager) setBracketedPaste(enabled bool) {
	if fm.bracketedPaste == enabled {
		return
	}
	sequence := "\x1b[?2004l"
	if enabled {
		sequence = "\x1b[?2004h"
	}
	fm.ttyOut.WriteString(sequence)
	fm.bracketedPaste = enabled
}

func (fm *FileManager) setKittyKeys(enabled bool) {
	if !fm.kittyKeys || fm.kittyKeysPushed == enabled {
		return
	}
	sequence := "\x1b[<u"
	if enabled {
		sequence = "\x1b[>" + kittyKeysFlags + "u"
	}
	fm.ttyOut.WriteString(sequence)
	fm.kittyKeysPushed = enabled
}

// takeTerminal puts the terminal in the file manager's raw mode, on the
// alternate screen, with bracketed paste and the kitty protocol.
func (fm *FileManager) takeTerminal() error {
	oldState, err := term.MakeRaw(fm.stdInFd)
	if err != nil {
		return err
	}
	fm.oldState = *oldState
	fm.ttyOut.WriteString("\033[?1049h\033[?25l")
	fm.setBracketedPaste(true)
	fm.setKittyKeys(true)
	fm.hasTerminal = true
	if cols, rows, err := term.GetSize(int(fm.ttyOut.Fd())); err == nil {
		fm.rows, fm.cols = rows, cols
	}
	return nil
}

// releaseTerminal gives the terminal back as it was before the file
// manager took it.
func (fm *FileManager) releaseTerminal() {
	if !fm.hasTerminal {
		return
	}
	fm.setKittyKeys(false)
	fm.setBracketedPaste(false)
	fm.ttyOut.WriteString("\033[?25h\033[?1049l")
	term.Restore(fm.stdInFd, &fm.oldState)
	fm.hasTerminal = false
}

// runWithTerminal gives the terminal to run, takes it back, and reads the
// directory again, keeping the cursor on the same entry.
func (fm *FileManager) runWithTerminal(run func()) {
	fm.keys.clear()
	name := fm.currentName()
	fm.releaseTerminal()
	run()
	fm.takeTerminal()
	fm.reloadKeepingCursor(name)
}

// waitForEnter waits for Enter, with the terminal in its normal mode, so
// an error printed is read before the screen is drawn over it.
func waitForEnter() {
	fmt.Fprint(os.Stderr, "Press Enter to continue...")
	buf := make([]byte, 256)
	os.Stdin.Read(buf)
}

// waitForKey waits for a key, so what a program printed is read before the
// screen is drawn over it. The key is read as a question's is.
func (fm *FileManager) waitForKey() {
	fmt.Fprint(fm.ttyOut, "Press any key to return")
	raw, err := term.MakeRaw(fm.stdInFd)
	if err != nil {
		return
	}
	fm.readQuestionKey()
	term.Restore(fm.stdInFd, raw)
}

// runContext is the context a program the file manager runs gets: its
// standard output is the terminal, which under msh fm is not the standard
// output of msh.
func (fm *FileManager) runContext() ExecuteContext {
	context := fm.context
	if fm.ttyOut != nil && fm.ttyOut != os.Stdout {
		context.StandardOutput = fm.ttyOut
	}
	return context
}

// enterShownDirectory makes the directory shown the working directory, for
// a quote to run in. The function it returns changes back, and gives the
// directory the quote changed to, or "".
func (fm *FileManager) enterShownDirectory() func() string {
	orig, err := os.Getwd()
	if err != nil || fm.showingWindowsVolumes || os.Chdir(fm.currentDir) != nil {
		return func() string { return "" }
	}
	pwd := os.Getenv("PWD")
	return func() string {
		now, err := os.Getwd()
		os.Chdir(orig)
		if os.Getenv("PWD") != pwd {
			fm.evalState().EnvironmentHistory().Set("PWD", pwd, "mshFileManager")
		}
		if err == nil && now != fm.currentDir {
			return now
		}
		return ""
	}
}

// runQuote runs the quote of fmRun, fmRunWait or fmRunOps with the
// terminal, in the directory shown (section 7.3). The mode and the prompt
// line are kept. For fmRunOps the ops the quote returns then run.
func (fm *FileManager) runQuote(op fmOp) fmOpResult {
	ev := fm.evalState()
	fm.keys.clear()
	name := fm.currentName()
	stack := MShellStack{}
	if op.kind == fmOpRunOps {
		stack = append(stack, fm.stateValue(fm.snapshot(), nil))
	}
	fm.releaseTerminal()
	back := fm.enterShownDirectory()
	result, err := ev.EvaluateQuote(op.quote, &stack, fm.runContext(), fm.definitions)
	newDir := back()
	ev.printAbbrWarnings()

	var program []fmOp
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, terminalSafeText(err.Error(), true))
		waitForEnter()
	case result.ExitCalled:
		fm.exitResult = &result
		return fmEnd
	case !result.Success:
		// The evaluator printed the error.
		waitForEnter()
	case op.kind == fmOpRunWait:
		fm.waitForKey()
	case op.kind == fmOpRunOps:
		m, ok := singleMaybe(stack)
		if !ok {
			fmt.Fprintln(os.Stderr, "The quote of fmRunOps must leave one Maybe[FmProgram] on the stack.")
			waitForEnter()
		} else if !m.IsNone() {
			p, err := parseFmOps(m.obj)
			if err != nil {
				fmt.Fprintf(os.Stderr, "The quote of fmRunOps: %s.\n", err)
				waitForEnter()
			}
			program = p
		}
	}

	fm.takeTerminal()
	if newDir != "" {
		fm.currentDir = newDir
		fm.cursor, fm.offset = 0, 0
		fm.loadDirectory()
	} else {
		fm.reloadKeepingCursor(name)
	}
	if fm.mode == fmModeInsert {
		fm.lineChanged()
	}
	if len(program) > 0 && fm.runProgram(program, noKey) {
		return fmEnd
	}
	return fmNext
}
