package main

// The file manager in the GUI frontend (gui/msh-gui): under msh server, the
// file manager's logic runs in the engine as always (its keymap, ops,
// quotes, previews), but it draws to the frontend as a view, and reads its
// keys from it, instead of using the terminal. The frontend shows the view
// as a dialog over the pane.
//
// Frontend to engine:
//
//	0x0A fm_input   u8 kind, then by kind: 0 key: u32 key, u32 the key the
//	                legacy encoding sent with ESC (0: none); 1 paste: the
//	                text (rest); 2 resize: u16 cols, u16 rows of the dialog;
//	                3 close.
//
// Engine to frontend:
//
//	0x8A fm_view    u8 flags (1 shown: 0 when the file manager ends or lends
//	                the pane to a program; 2 the help screen; 4 the preview
//	                is a directory's listing: " name", " name/" for a
//	                directory), str header,
//	                str clipboard status, u32 entry count, u32 the first
//	                entry shown, u16 entries shown, each: u8 flags (1 dir,
//	                2 cursor, 4 selected, 8 cut, 16 copied), str name, str
//	                size, str sync marker; u16 preview lines (the help lines
//	                with flag 2), strs, which may hold SGR colours; u8 status (0 none, 1 summary, 2
//	                message), str; u8 prompt line, str label, str text,
//	                u32 cursor (bytes); u16 overlay lines, strs (the key
//	                popup, or a question).

import (
	"encoding/binary"
	"strings"
	"time"
)

const (
	serverMsgFmInput = 0x0A
	serverMsgFmView  = 0x8A
)

const (
	fmInputKey    = 0
	fmInputPaste  = 1
	fmInputResize = 2
	fmInputClose  = 3
)

type fmGUIInput struct {
	kind       byte
	code       KeyCode
	escOf      KeyCode
	text       string
	cols, rows int
}

// fmGUI connects file managers to msh server's frontend.
type fmGUI struct {
	input chan fmGUIInput
	send  func(view []byte)
}

// fileManagerGUI is set by msh server: then mshFileManager draws to the
// frontend.
var fileManagerGUI *fmGUI

// startGUI drops input meant for an earlier file manager.
func (g *fmGUI) start() {
	for {
		select {
		case <-g.input:
		default:
			return
		}
	}
}

// guiNextInput handles the next input from the frontend. It reports whether
// the file manager should end.
func (fm *FileManager) guiNextInput() bool {
	in, ok := <-fm.gui.input
	if !ok {
		return true
	}
	fm.renderMu.Lock()
	defer fm.renderMu.Unlock()
	now := time.Now()
	switch in.kind {
	case fmInputKey:
		fm.statusMsg = ""
		code := in.code
		if in.escOf != 0 && !fm.keyUsed(code) {
			if fm.resolveKey(NamedEscape, now) {
				return true
			}
			code = in.escOf
		}
		return fm.resolveKey(code, now)
	case fmInputPaste:
		fm.statusMsg = ""
		fm.popup = false
		fm.keys.event(now)
		fm.paste(in.text)
	case fmInputResize:
		fm.cols, fm.rows = max(in.cols, 20), max(in.rows, 5)
		fm.clampCursor()
		fm.adjustScroll()
	case fmInputClose:
		return true
	}
	return false
}

// guiQuestionKey reads the key that answers a question from the frontend.
func (fm *FileManager) guiQuestionKey() (KeyCode, bool) {
	fm.keys.clear()
	fm.popup = false
	for in := range fm.gui.input {
		switch in.kind {
		case fmInputKey:
			if in.escOf != 0 {
				return NamedEscape, true
			}
			return in.code, true
		case fmInputResize:
			fm.cols, fm.rows = max(in.cols, 20), max(in.rows, 5)
			fm.render()
		case fmInputPaste:
			return 0, false
		case fmInputClose:
			return 0, false
		}
	}
	return 0, false
}

// renderGUI sends the view.
func (fm *FileManager) renderGUI() {
	le := binary.LittleEndian
	var b []byte
	flags := byte(0)
	if !fm.guiHidden && !fm.guiDone {
		flags |= 1
	}
	if fm.mode == fmModeHelp {
		flags |= 2
	}
	b = append(b, flags)
	header := fm.currentDir
	if fm.showingWindowsVolumes {
		header = "Windows volumes"
	}
	b = appendStr(b, fm.username+"@"+fm.hostname+": "+header)

	cutPaths, copyPaths := loadClipboard()
	cut, copied := map[string]bool{}, map[string]bool{}
	for _, p := range cutPaths {
		cut[p] = true
	}
	for _, p := range copyPaths {
		copied[p] = true
	}
	var clip []byte
	if len(cutPaths) > 0 {
		clip = append(clip, " CUT "...)
		clip = appendInt(clip, len(cutPaths))
	}
	if len(copyPaths) > 0 {
		clip = append(clip, " COPY "...)
		clip = appendInt(clip, len(copyPaths))
	}
	if fm.selected.len() > 0 {
		clip = append(clip, " SELECTED "...)
		clip = appendInt(clip, fm.selected.len())
	}
	b = appendStr(b, string(clip))

	// The preview is a directory's listing, which the frontend draws as
	// it draws the entries.
	if fm.mode != fmModeHelp && fm.cursor < len(fm.entries) && fm.entries[fm.cursor].IsDir() {
		if lines := fm.getPreview(); len(lines) == 0 || !strings.HasPrefix(lines[0], " (") && lines[0] != " Loading..." {
			b[0] |= 4
		}
	}

	// The entries shown, as the terminal shows them.
	visible := 0
	if fm.mode != fmModeHelp {
		visible = max(0, min(fm.visibleRows(), len(fm.entries)-fm.offset))
	}
	b = le.AppendUint32(b, uint32(len(fm.entries)))
	b = le.AppendUint32(b, uint32(fm.offset))
	b = le.AppendUint16(b, uint16(visible))
	for row := range visible {
		idx := fm.offset + row
		entry := fm.entries[idx]
		path := fm.selectedEntryPath(entry)
		var ef byte
		if entry.IsDir() {
			ef |= 1
		}
		if idx == fm.cursor {
			ef |= 2
		}
		if fm.selected.has(path) {
			ef |= 4
		}
		if cut[path] {
			ef |= 8
		}
		if copied[path] {
			ef |= 16
		}
		b = append(b, ef)
		b = appendStr(b, terminalSafeText(entry.Name(), false))
		b = appendStr(b, fm.entrySize(entry))
		marker := ""
		if fm.inCloudSyncRoot && !fm.showingWindowsVolumes {
			marker, _ = cloudStateMarker(fm.cloudState(entry))
		}
		b = appendStr(b, marker)
	}

	// The preview, or the help screen's lines.
	var lines []string
	if fm.mode == fmModeHelp {
		help := fm.helpLines()
		rows := fm.helpRows()
		fm.helpOffset = min(fm.helpOffset, max(0, len(help)-rows))
		lines = help[fm.helpOffset:min(len(help), fm.helpOffset+rows)]
	} else {
		lines = fm.getPreview()
		if len(lines) > fm.visibleRows() {
			lines = lines[:fm.visibleRows()]
		}
	}
	b = le.AppendUint16(b, uint16(len(lines)))
	for _, l := range lines {
		b = appendStr(b, sgrSafeText(l))
	}

	switch summary := fm.directorySummary(); {
	case fm.statusMsg != "":
		b = append(b, 2)
		b = appendStr(b, terminalSafeText(fm.statusMsg, false))
	case summary != "" && fm.mode != fmModeHelp:
		b = append(b, 1)
		b = appendStr(b, summary)
	default:
		b = append(b, 0)
		b = appendStr(b, "")
	}

	if fm.mode == fmModeInsert {
		b = append(b, 1)
		b = appendStr(b, fm.lineLabel())
		b = appendStr(b, string(fm.line.text))
		b = le.AppendUint32(b, uint32(fm.line.index))
	} else {
		b = append(b, 0)
		b = appendStr(b, "")
		b = appendStr(b, "")
		b = le.AppendUint32(b, 0)
	}

	var overlay []string
	switch {
	case fm.asking:
		overlay = fm.askLines
	case fm.popup:
		overlay = fm.popupLines()
	}
	b = le.AppendUint16(b, uint16(len(overlay)))
	for _, l := range overlay {
		b = appendStr(b, l)
	}
	fm.gui.send(b)
}

// sgrSafeText is a preview line made safe to show, keeping its colours:
// SGR sequences (ESC [ digits and ; m) stay, and the text between them is
// terminalSafeText.
func sgrSafeText(s string) string {
	var out strings.Builder
	for len(s) > 0 {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			out.WriteString(terminalSafeText(s, false))
			break
		}
		out.WriteString(terminalSafeText(s[:i], false))
		j := i + 2
		for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';') {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			out.WriteString(s[i : j+1])
			s = s[j+1:]
		} else {
			out.WriteString(terminalSafeText(s[i:j], false))
			s = s[j:]
		}
	}
	return out.String()
}

func appendInt(b []byte, n int) []byte {
	return append(b, []byte(itoa(n))...)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [20]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}

func appendStr(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}
