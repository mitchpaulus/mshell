package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

type FileManager struct {
	rows, cols int
	stdInFd    int
	oldState   term.State

	currentDir string
	entries    []os.DirEntry
	cursor     int
	offset     int

	hostname string
	username string

	lastKey byte // for gg detection

	previewCache map[string][]string // cached preview lines per file path

	// Search state
	searching    bool   // true when typing a search query
	searchQuery  []rune // current search input
	searchActive bool   // true when a search has been committed
	searchTerm   string // lowercase committed search term
	searchMatches []int // indices into entries that match

	ttyOut *os.File // where TUI output goes (may differ from stdout)

	// Bookmark state
	bookmarks       map[byte]string
	pendingMark     bool // waiting for mark character after 'm'
	showingBookmarks bool // showing bookmark selection after ';'

	// Rename state
	renaming       bool
	renameInput    []rune
	renameCursor   int // cursor position within renameInput

	// Status message (shown once at bottom, cleared on first keypress)
	statusMsg string
}

// RunFileManager runs as a standalone subcommand (msh fm).
// TUI goes to /dev/tty, final directory is printed to stdout.
// If startDir is non-empty and is a valid directory, it is used instead of cwd.
func RunFileManager(startDir string) int {
	fm := &FileManager{}
	fm.stdInFd = int(os.Stdin.Fd())

	// Open /dev/tty for TUI output so stdout stays clean for the directory result.
	if runtime.GOOS != "windows" {
		tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			fm.ttyOut = os.Stdout
		} else {
			fm.ttyOut = tty
			defer tty.Close()
		}
	} else {
		fm.ttyOut = os.Stdout
	}

	cols, rows, err := term.GetSize(int(fm.ttyOut.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting terminal size: %s\n", err)
		return 1
	}
	fm.rows = rows
	fm.cols = cols

	fm.initUserInfo()
	fm.bookmarks = loadBookmarks()

	if startDir != "" {
		if info, err := os.Stat(startDir); err == nil && info.IsDir() {
			fm.currentDir, _ = filepath.Abs(startDir)
		} else {
			fm.statusMsg = fmt.Sprintf("Directory not found: %s", startDir)
			fm.currentDir, err = os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting working directory: %s\n", err)
				return 1
			}
		}
	} else {
		fm.currentDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting working directory: %s\n", err)
			return 1
		}
	}

	fm.loadDirectory()

	oldState, err := term.MakeRaw(fm.stdInFd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error entering raw mode: %s\n", err)
		return 1
	}
	fm.oldState = *oldState

	fm.ttyOut.WriteString("\033[?1049h\033[?25l")

	defer func() {
		fm.ttyOut.WriteString("\033[?25h\033[?1049l")
		term.Restore(fm.stdInFd, &fm.oldState)
	}()

	fm.mainLoop()

	// Print the final directory to stdout so callers can cd to it.
	fmt.Fprintln(os.Stdout, fm.currentDir)

	return 0
}

// RunFileManagerInteractive runs from within the mshell interactive session.
// The terminal is already in raw mode from the interactive session.
// If startDir is non-empty and is a valid directory, it is used instead of cwd.
// Returns the directory the user was in when they quit.
func RunFileManagerInteractive(stdInFd int, oldState *term.State, startDir string) string {
	fm := &FileManager{}
	fm.stdInFd = stdInFd
	fm.oldState = *oldState
	fm.ttyOut = os.Stdout

	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return ""
	}
	fm.rows = rows
	fm.cols = cols

	fm.initUserInfo()
	fm.bookmarks = loadBookmarks()

	if startDir != "" {
		if info, err := os.Stat(startDir); err == nil && info.IsDir() {
			fm.currentDir, _ = filepath.Abs(startDir)
		} else {
			fm.statusMsg = fmt.Sprintf("Directory not found: %s", startDir)
			fm.currentDir, _ = os.Getwd()
		}
	} else {
		fm.currentDir, _ = os.Getwd()
	}
	fm.loadDirectory()

	// Terminal is already in raw mode from the interactive session.
	// Just switch to alternate buffer and hide cursor.
	fm.ttyOut.WriteString("\033[?1049h\033[?25l")

	fm.mainLoop()

	fm.ttyOut.WriteString("\033[?25h\033[?1049l")

	return fm.currentDir
}

// RunFileManagerBuiltin runs from a builtin call during evaluation.
// The terminal is in cooked mode, so this handles MakeRaw/Restore itself.
// If startDir is non-empty and is a valid directory, it is used instead of cwd.
// Returns the directory the user was in when they quit.
func RunFileManagerBuiltin(startDir string) string {
	fm := &FileManager{}
	fm.stdInFd = int(os.Stdin.Fd())
	fm.ttyOut = os.Stdout

	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return ""
	}
	fm.rows = rows
	fm.cols = cols

	fm.initUserInfo()
	fm.bookmarks = loadBookmarks()

	if startDir != "" {
		if info, err := os.Stat(startDir); err == nil && info.IsDir() {
			fm.currentDir, _ = filepath.Abs(startDir)
		} else {
			fm.statusMsg = fmt.Sprintf("Directory not found: %s", startDir)
			fm.currentDir, _ = os.Getwd()
		}
	} else {
		fm.currentDir, _ = os.Getwd()
	}
	fm.loadDirectory()

	oldState, err := term.MakeRaw(fm.stdInFd)
	if err != nil {
		return ""
	}
	fm.oldState = *oldState

	fm.ttyOut.WriteString("\033[?1049h\033[?25l")

	defer func() {
		fm.ttyOut.WriteString("\033[?25h\033[?1049l")
		term.Restore(fm.stdInFd, &fm.oldState)
	}()

	fm.mainLoop()

	return fm.currentDir
}

func (fm *FileManager) initUserInfo() {
	fm.hostname, _ = os.Hostname()
	if u, err := user.Current(); err == nil {
		fm.username = u.Username
	} else {
		fm.username = os.Getenv("USER")
	}
}

func (fm *FileManager) mainLoop() {
	for {
		fm.render()
		quit := fm.handleInput()
		if quit {
			break
		}
	}
}

func (fm *FileManager) loadDirectory() {
	entries, err := os.ReadDir(fm.currentDir)
	if err != nil {
		fm.entries = nil
		return
	}

	sort.SliceStable(entries, func(i, j int) bool {
		iDir := entries[i].IsDir()
		jDir := entries[j].IsDir()
		if iDir != jDir {
			return iDir
		}
		return VersionSortComparer(strings.ToLower(entries[i].Name()), strings.ToLower(entries[j].Name())) < 0
	})

	fm.entries = entries
	fm.previewCache = make(map[string][]string)
	fm.searchActive = false
	fm.searchMatches = fm.searchMatches[:0]
}

func (fm *FileManager) visibleRows() int {
	if fm.searching || fm.renaming || fm.statusMsg != "" {
		return fm.rows - 2 // header + bottom bar
	}
	return fm.rows - 1 // header only
}

func (fm *FileManager) clampCursor() {
	if len(fm.entries) == 0 {
		fm.cursor = 0
		return
	}
	if fm.cursor < 0 {
		fm.cursor = 0
	}
	if fm.cursor >= len(fm.entries) {
		fm.cursor = len(fm.entries) - 1
	}
}

func (fm *FileManager) adjustScroll() {
	visible := fm.visibleRows()
	if visible <= 0 {
		return
	}
	if fm.cursor < fm.offset {
		fm.offset = fm.cursor
	}
	if fm.cursor >= fm.offset+visible {
		fm.offset = fm.cursor - visible + 1
	}
}

func (fm *FileManager) leftPaneWidth() int {
	_, clipPaths := loadClipboard()
	clipSet := make(map[string]bool)
	for _, p := range clipPaths {
		clipSet[p] = true
	}

	maxLen := 0
	visible := fm.visibleRows()
	end := fm.offset + visible
	if end > len(fm.entries) {
		end = len(fm.entries)
	}
	for i := fm.offset; i < end; i++ {
		name := fm.entries[i].Name()
		nameLen := utf8.RuneCountInString(name)
		if fm.entries[i].IsDir() {
			nameLen++ // for '/'
		}
		entryPath := filepath.Join(fm.currentDir, fm.entries[i].Name())
		if clipSet[entryPath] {
			nameLen += 2 // indent for clipboard entries
		}
		if nameLen > maxLen {
			maxLen = nameLen
		}
	}
	maxLen += 1 // padding
	maxWidth := fm.cols / 2
	if maxLen > maxWidth {
		maxLen = maxWidth
	}
	if maxLen < 10 {
		maxLen = 10
	}
	return maxLen
}

// truncateMiddle truncates s to maxRunes by replacing the middle with "..".
func truncateMiddle(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 2 {
		return string(runes[:maxRunes])
	}
	// Split available space: more on the left
	leftLen := (maxRunes - 2 + 1) / 2
	rightLen := maxRunes - 2 - leftLen
	return string(runes[:leftLen]) + ".." + string(runes[len(runes)-rightLen:])
}

func (fm *FileManager) writeHighlightedName(buf *bytes.Buffer, name string, isDir bool, isCursor bool) {
	if !fm.searchActive || len(fm.searchTerm) == 0 {
		buf.WriteString(name)
		return
	}

	nameLower := strings.ToLower(name)
	matchStart := strings.Index(nameLower, fm.searchTerm)
	if matchStart < 0 {
		buf.WriteString(name)
		return
	}

	runes := []rune(name)
	termRunes := utf8.RuneCountInString(fm.searchTerm)

	// Find rune index of match start
	runeIdx := 0
	byteIdx := 0
	for runeIdx < len(runes) && byteIdx < matchStart {
		byteIdx += utf8.RuneLen(runes[runeIdx])
		runeIdx++
	}
	matchRuneStart := runeIdx
	matchRuneEnd := matchRuneStart + termRunes
	if matchRuneEnd > len(runes) {
		matchRuneEnd = len(runes)
	}

	// Write pre-match
	buf.WriteString(string(runes[:matchRuneStart]))
	// Highlight: yellow background
	buf.WriteString("\033[33;1m")
	buf.WriteString(string(runes[matchRuneStart:matchRuneEnd]))
	// Restore previous style
	buf.WriteString("\033[22;39m")
	if isDir {
		buf.WriteString("\033[34m")
	}
	if isCursor {
		buf.WriteString("\033[7m")
	}
	// Write post-match
	buf.WriteString(string(runes[matchRuneEnd:]))
}

func (fm *FileManager) render() {
	var buf bytes.Buffer

	// Move cursor to top-left, clear screen
	buf.WriteString("\033[H\033[2J")

	leftW := fm.leftPaneWidth()
	rightW := fm.cols - leftW - 3 // 3 for " │ " separator
	if rightW < 0 {
		rightW = 0
	}

	// Load clipboard for display
	clipOp, clipPaths := loadClipboard()
	clipSet := make(map[string]bool)
	for _, p := range clipPaths {
		clipSet[p] = true
	}

	// Header row
	header := fmt.Sprintf(" %s@%s: %s", fm.username, fm.hostname, fm.currentDir)

	// Clipboard status suffix
	clipStatus := ""
	if clipOp != "" && len(clipPaths) > 0 {
		noun := "file"
		if len(clipPaths) != 1 {
			noun = "files"
		}
		clipStatus = fmt.Sprintf(" %s %d %s", strings.ToUpper(clipOp), len(clipPaths), noun)
	}

	headerRunes := utf8.RuneCountInString(header)
	clipStatusRunes := utf8.RuneCountInString(clipStatus)

	buf.WriteString("\033[7m") // reverse video
	if headerRunes+clipStatusRunes > fm.cols {
		header = truncateMiddle(header, fm.cols-clipStatusRunes)
		headerRunes = fm.cols - clipStatusRunes
	}
	buf.WriteString(header)
	// Pad between header and clip status
	pad := fm.cols - headerRunes - clipStatusRunes
	if pad > 0 {
		buf.WriteString(strings.Repeat(" ", pad))
	}
	if clipStatus != "" {
		if clipOp == "cut" {
			buf.WriteString("\033[31m") // red
		} else {
			buf.WriteString("\033[32m") // green
		}
		buf.WriteString(clipStatus)
		buf.WriteString("\033[39m") // reset fg, keep reverse
	}
	buf.WriteString("\033[0m")

	// Preview content
	previewLines := fm.getPreview()

	visible := fm.visibleRows()
	for row := 0; row < visible; row++ {
		buf.WriteString("\r\n")
		idx := fm.offset + row

		// Left pane entry
		if idx < len(fm.entries) {
			entry := fm.entries[idx]
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}

			entryPath := filepath.Join(fm.currentDir, entry.Name())
			inClip := clipSet[entryPath]
			indent := 0
			if inClip {
				indent = 2
			}

			nameRunes := utf8.RuneCountInString(name)

			if idx == fm.cursor {
				buf.WriteString("\033[7m") // reverse video for selected
			}

			if inClip {
				if clipOp == "cut" {
					buf.WriteString("\033[31m") // red
				} else {
					buf.WriteString("\033[32m") // green
				}
			} else if entry.IsDir() {
				buf.WriteString("\033[34m") // blue for directories
			}

			availW := leftW - 1 - indent // 1 for leading space
			if nameRunes > availW {
				name = truncateMiddle(name, availW)
				nameRunes = availW
			}
			buf.WriteString(" ")
			if indent > 0 {
				buf.WriteString(strings.Repeat(" ", indent))
			}
			fm.writeHighlightedName(&buf, name, entry.IsDir(), idx == fm.cursor)
			// Pad to leftW
			padLeft := availW - nameRunes
			if padLeft > 0 {
				buf.WriteString(strings.Repeat(" ", padLeft))
			}

			if idx == fm.cursor || inClip {
				buf.WriteString("\033[0m")
			}
		} else {
			buf.WriteString(strings.Repeat(" ", leftW))
		}

		// Separator
		buf.WriteString(" \033[90m\u2502\033[0m ")

		// Right pane
		if row < len(previewLines) {
			line := previewLines[row]
			lineRunes := utf8.RuneCountInString(line)
			if lineRunes > rightW {
				line = truncateMiddle(line, rightW)
			}
			buf.WriteString(line)
		}
	}

	// Search bar at the bottom
	if fm.searching {
		buf.WriteString("\r\n")
		searchLine := "/" + string(fm.searchQuery)
		searchRunes := utf8.RuneCountInString(searchLine)
		if searchRunes > fm.cols {
			searchLine = string([]rune(searchLine)[:fm.cols])
		}
		buf.WriteString(searchLine)
		// Show cursor at end of search input
		buf.WriteString("\033[?25h")
	}

	// Rename bar at the bottom
	if fm.renaming {
		buf.WriteString("\r\n")
		prefix := "rename: "
		renameLine := prefix + string(fm.renameInput)
		renameRunes := utf8.RuneCountInString(renameLine)
		if renameRunes > fm.cols {
			renameLine = string([]rune(renameLine)[:fm.cols])
		}
		buf.WriteString(renameLine)
		// Position cursor
		cursorCol := utf8.RuneCountInString(prefix) + fm.renameCursor + 1
		buf.WriteString(fmt.Sprintf("\033[%d;%dH", fm.rows, cursorCol))
		buf.WriteString("\033[?25h")
	}

	// Status message at the bottom
	if fm.statusMsg != "" && !fm.searching && !fm.renaming {
		buf.WriteString("\r\n")
		msg := fm.statusMsg
		msgRunes := utf8.RuneCountInString(msg)
		if msgRunes > fm.cols {
			msg = string([]rune(msg)[:fm.cols])
		}
		buf.WriteString("\033[33m") // yellow
		buf.WriteString(msg)
		buf.WriteString("\033[0m")
	}

	// Bookmark overlay
	if fm.pendingMark || fm.showingBookmarks {
		fm.renderBookmarkOverlay(&buf)
	}

	fm.ttyOut.Write(buf.Bytes())
}

func (fm *FileManager) renderBookmarkOverlay(buf *bytes.Buffer) {
	// Draw a centered overlay box
	var lines []string
	if fm.pendingMark {
		lines = append(lines, " Set bookmark (0-9, a-z): ")
	} else {
		lines = append(lines, " Go to bookmark: ")
	}
	// List existing bookmarks
	for c := byte('0'); c <= '9'; c++ {
		if dir, ok := fm.bookmarks[c]; ok {
			lines = append(lines, fmt.Sprintf("  %c  %s", c, dir))
		}
	}
	for c := byte('a'); c <= 'z'; c++ {
		if dir, ok := fm.bookmarks[c]; ok {
			lines = append(lines, fmt.Sprintf("  %c  %s", c, dir))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "  (no bookmarks)")
	}

	// Find box width
	boxW := 0
	for _, l := range lines {
		runes := utf8.RuneCountInString(l)
		if runes > boxW {
			boxW = runes
		}
	}
	boxW += 2 // padding
	if boxW > fm.cols-4 {
		boxW = fm.cols - 4
	}

	// Center position
	startCol := (fm.cols - boxW) / 2
	if startCol < 1 {
		startCol = 1
	}
	startRow := (fm.rows-len(lines))/2 + 1
	if startRow < 2 {
		startRow = 2
	}

	for i, line := range lines {
		row := startRow + i
		if row > fm.rows {
			break
		}
		buf.WriteString(fmt.Sprintf("\033[%d;%dH", row, startCol))
		buf.WriteString("\033[7m") // reverse video
		lineRunes := utf8.RuneCountInString(line)
		if lineRunes > boxW {
			line = truncateMiddle(line, boxW)
			lineRunes = boxW
		}
		buf.WriteString(line)
		pad := boxW - lineRunes
		if pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
		buf.WriteString("\033[0m")
	}
}

func (fm *FileManager) getPreview() []string {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return nil
	}

	entry := fm.entries[fm.cursor]
	path := filepath.Join(fm.currentDir, entry.Name())

	if cached, ok := fm.previewCache[path]; ok {
		return cached
	}

	lines := fm.computePreview(entry, path)
	fm.previewCache[path] = lines
	return lines
}

func (fm *FileManager) computePreview(entry os.DirEntry, path string) []string {
	maxLines := fm.visibleRows()

	if entry.IsDir() {
		subEntries, err := os.ReadDir(path)
		if err != nil {
			return []string{" (cannot read)"}
		}
		sort.SliceStable(subEntries, func(i, j int) bool {
			iDir := subEntries[i].IsDir()
			jDir := subEntries[j].IsDir()
			if iDir != jDir {
				return iDir
			}
			return VersionSortComparer(strings.ToLower(subEntries[i].Name()), strings.ToLower(subEntries[j].Name())) < 0
		})
		var lines []string
		for i, e := range subEntries {
			if i >= maxLines {
				break
			}
			name := e.Name()
			if e.IsDir() {
				lines = append(lines, " \033[34m"+name+"/\033[0m")
			} else {
				lines = append(lines, " "+name)
			}
		}
		return lines
	}

	// PDFs don't always have a null byte, so treat by extension
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		return []string{" (binary file)"}
	}

	// Check for binary before reading full file
	f, err := os.Open(path)
	if err != nil {
		return []string{" (cannot read)"}
	}
	defer f.Close()

	probe := make([]byte, 512)
	n, _ := f.Read(probe)
	if n > 0 && bytes.ContainsRune(probe[:n], 0) {
		return []string{" (binary file)"}
	}

	// Read full file for text preview
	f.Seek(0, 0)
	data, err := io.ReadAll(f)
	if err != nil {
		return []string{" (cannot read)"}
	}

	allLines := strings.Split(string(data), "\n")
	var lines []string
	for i, line := range allLines {
		if i >= maxLines {
			break
		}
		// Replace tabs with spaces for display
		line = strings.ReplaceAll(line, "\t", "    ")
		lines = append(lines, " "+line)
	}
	return lines
}

func (fm *FileManager) handleInput() bool {
	buf := make([]byte, 16)
	n, err := os.Stdin.Read(buf)
	if err != nil || n == 0 {
		return false
	}

	fm.statusMsg = ""

	if fm.searching {
		return fm.handleSearchInput(buf, n)
	}

	if fm.renaming {
		return fm.handleRenameInput(buf, n)
	}

	key := buf[0]

	if fm.pendingMark {
		fm.pendingMark = false
		if isBookmarkChar(key) {
			fm.bookmarks[key] = fm.currentDir
			saveBookmarks(fm.bookmarks)
		}
		return false
	}

	if fm.showingBookmarks {
		fm.showingBookmarks = false
		if isBookmarkChar(key) {
			if dir, ok := fm.bookmarks[key]; ok {
				fm.currentDir = dir
				fm.cursor = 0
				fm.offset = 0
				fm.loadDirectory()
			}
		}
		return false
	}

	// Check for escape sequences
	if n >= 3 && buf[0] == 0x1b && buf[1] == '[' {
		switch buf[2] {
		case 'A': // Up arrow
			fm.cursor--
			fm.clampCursor()
			fm.adjustScroll()
			fm.lastKey = 0
			return false
		case 'B': // Down arrow
			fm.cursor++
			fm.clampCursor()
			fm.adjustScroll()
			fm.lastKey = 0
			return false
		case 'C': // Right arrow - enter directory
			fm.enterSelected()
			fm.lastKey = 0
			return false
		case 'D': // Left arrow - parent directory
			fm.goParent()
			fm.lastKey = 0
			return false
		}

		// F5 = \033[15~
		if n >= 4 && buf[2] == '1' && buf[3] == '5' {
			fm.loadDirectory()
			fm.clampCursor()
			fm.adjustScroll()
			fm.lastKey = 0
			return false
		}
	}

	switch key {
	case 'q':
		return true
	case 'j':
		fm.cursor++
		fm.clampCursor()
		fm.adjustScroll()
	case 'k':
		fm.cursor--
		fm.clampCursor()
		fm.adjustScroll()
	case 'h':
		fm.goParent()
	case 'l', 13: // l or Enter
		fm.enterSelected()
	case 'G':
		fm.cursor = len(fm.entries) - 1
		fm.clampCursor()
		fm.adjustScroll()
	case 'g':
		if fm.lastKey == 'g' {
			fm.cursor = 0
			fm.offset = 0
			fm.lastKey = 0
			return false
		}
		fm.lastKey = 'g'
		return false
	case 4: // Ctrl-d
		fm.cursor += 10
		fm.clampCursor()
		fm.adjustScroll()
	case 21: // Ctrl-u
		fm.cursor -= 10
		fm.clampCursor()
		fm.adjustScroll()
	case 'e':
		fm.openEditor()
	case 'r':
		fm.startRename()
		return false
	case '/':
		fm.searching = true
		fm.searchQuery = fm.searchQuery[:0]
		fm.ttyOut.WriteString("\033[?25h") // show cursor
		return false
	case 'n':
		fm.searchNext()
	case 'N':
		fm.searchPrev()
	case 'd':
		fm.clipboardCut()
	case 'y':
		if fm.lastKey == 'y' {
			fm.clipboardCopy()
			fm.lastKey = 0
			return false
		}
		fm.lastKey = 'y'
		return false
	case 'p':
		fm.clipboardPaste()
	case 'c':
		clearClipboard()
	case 'x':
		fm.deleteEntry()
	case 'm':
		fm.pendingMark = true
		return false
	case ';':
		fm.showingBookmarks = true
		return false
	}

	if key != 'g' && key != 'y' {
		fm.lastKey = 0
	}

	return false
}

func (fm *FileManager) handleSearchInput(buf []byte, _ int) bool {
	key := buf[0]

	// Escape cancels search
	if key == 0x1b {
		fm.searching = false
		fm.ttyOut.WriteString("\033[?25l")
		return false
	}

	// Enter commits search
	if key == 13 {
		fm.searching = false
		fm.ttyOut.WriteString("\033[?25l")
		fm.commitSearch()
		return false
	}

	// Backspace
	if key == 127 {
		if len(fm.searchQuery) > 0 {
			fm.searchQuery = fm.searchQuery[:len(fm.searchQuery)-1]
			fm.updateSearchLive()
		}
		return false
	}

	// Ctrl-U clears the search input
	if key == 21 {
		fm.searchQuery = fm.searchQuery[:0]
		fm.updateSearchLive()
		return false
	}

	// Ctrl-W deletes the last word
	if key == 23 {
		if len(fm.searchQuery) > 0 {
			// Skip trailing spaces
			i := len(fm.searchQuery) - 1
			for i >= 0 && fm.searchQuery[i] == ' ' {
				i--
			}
			// Delete back to previous space or start
			for i >= 0 && fm.searchQuery[i] != ' ' {
				i--
			}
			fm.searchQuery = fm.searchQuery[:i+1]
			fm.updateSearchLive()
		}
		return false
	}

	// Printable characters
	if key >= 32 && key < 127 {
		fm.searchQuery = append(fm.searchQuery, rune(key))
		fm.updateSearchLive()
	}

	return false
}

func (fm *FileManager) updateSearchLive() {
	if len(fm.searchQuery) == 0 {
		fm.searchMatches = fm.searchMatches[:0]
		fm.searchActive = false
		return
	}
	query := strings.ToLower(string(fm.searchQuery))
	fm.searchTerm = query
	fm.searchMatches = fm.searchMatches[:0]
	for i, e := range fm.entries {
		if strings.Contains(strings.ToLower(e.Name()), query) {
			fm.searchMatches = append(fm.searchMatches, i)
		}
	}
	fm.searchActive = true
}

func (fm *FileManager) commitSearch() {
	if len(fm.searchQuery) == 0 {
		fm.searchActive = false
		fm.searchMatches = fm.searchMatches[:0]
		return
	}
	fm.searchTerm = strings.ToLower(string(fm.searchQuery))
	fm.buildSearchMatches()
	if len(fm.searchMatches) > 0 {
		// Jump to first match at or after cursor
		for _, idx := range fm.searchMatches {
			if idx >= fm.cursor {
				fm.cursor = idx
				fm.adjustScroll()
				return
			}
		}
		fm.cursor = fm.searchMatches[0]
		fm.adjustScroll()
	}
}

func (fm *FileManager) buildSearchMatches() {
	fm.searchMatches = fm.searchMatches[:0]
	for i, e := range fm.entries {
		if strings.Contains(strings.ToLower(e.Name()), fm.searchTerm) {
			fm.searchMatches = append(fm.searchMatches, i)
		}
	}
}

func (fm *FileManager) searchNext() {
	if !fm.searchActive || len(fm.searchMatches) == 0 {
		return
	}
	for _, idx := range fm.searchMatches {
		if idx > fm.cursor {
			fm.cursor = idx
			fm.adjustScroll()
			return
		}
	}
	// Wrap around
	fm.cursor = fm.searchMatches[0]
	fm.adjustScroll()
}

func (fm *FileManager) searchPrev() {
	if !fm.searchActive || len(fm.searchMatches) == 0 {
		return
	}
	for i := len(fm.searchMatches) - 1; i >= 0; i-- {
		if fm.searchMatches[i] < fm.cursor {
			fm.cursor = fm.searchMatches[i]
			fm.adjustScroll()
			return
		}
	}
	// Wrap around
	fm.cursor = fm.searchMatches[len(fm.searchMatches)-1]
	fm.adjustScroll()
}

func (fm *FileManager) startRename() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	name := fm.entries[fm.cursor].Name()
	fm.renaming = true
	fm.renameInput = []rune(name)

	// Position cursor before the extension
	ext := filepath.Ext(name)
	if len(ext) > 0 && len(ext) < len(name) {
		fm.renameCursor = utf8.RuneCountInString(name) - utf8.RuneCountInString(ext)
	} else {
		fm.renameCursor = len(fm.renameInput)
	}
}

func (fm *FileManager) handleRenameInput(buf []byte, n int) bool {
	key := buf[0]

	// Escape cancels
	if key == 0x1b {
		// Check for arrow keys: ESC [ A/B/C/D
		if n >= 3 && buf[1] == '[' {
			switch buf[2] {
			case 'C': // Right
				if fm.renameCursor < len(fm.renameInput) {
					fm.renameCursor++
				}
			case 'D': // Left
				if fm.renameCursor > 0 {
					fm.renameCursor--
				}
			case 'H': // Home
				fm.renameCursor = 0
			case 'F': // End
				fm.renameCursor = len(fm.renameInput)
			}
			return false
		}
		fm.renaming = false
		fm.ttyOut.WriteString("\033[?25l")
		return false
	}

	// Enter commits rename
	if key == 13 {
		fm.renaming = false
		fm.ttyOut.WriteString("\033[?25l")
		fm.commitRename()
		return false
	}

	// Backspace
	if key == 127 {
		if fm.renameCursor > 0 {
			fm.renameInput = append(fm.renameInput[:fm.renameCursor-1], fm.renameInput[fm.renameCursor:]...)
			fm.renameCursor--
		}
		return false
	}

	// Ctrl-U clears to start
	if key == 21 {
		fm.renameInput = fm.renameInput[fm.renameCursor:]
		fm.renameCursor = 0
		return false
	}

	// Ctrl-W delete word backwards
	if key == 23 {
		if fm.renameCursor > 0 {
			i := fm.renameCursor - 1
			for i > 0 && fm.renameInput[i] == ' ' {
				i--
			}
			for i > 0 && fm.renameInput[i-1] != ' ' && fm.renameInput[i-1] != '.' {
				i--
			}
			fm.renameInput = append(fm.renameInput[:i], fm.renameInput[fm.renameCursor:]...)
			fm.renameCursor = i
		}
		return false
	}

	// Ctrl-A go to start
	if key == 1 {
		fm.renameCursor = 0
		return false
	}

	// Ctrl-E go to end
	if key == 5 {
		fm.renameCursor = len(fm.renameInput)
		return false
	}

	// Ctrl-K delete to end
	if key == 11 {
		fm.renameInput = fm.renameInput[:fm.renameCursor]
		return false
	}

	// Printable characters
	if key >= 32 && key < 127 {
		fm.renameInput = append(fm.renameInput[:fm.renameCursor], append([]rune{rune(key)}, fm.renameInput[fm.renameCursor:]...)...)
		fm.renameCursor++
	}

	return false
}

func (fm *FileManager) commitRename() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	oldName := fm.entries[fm.cursor].Name()
	newName := string(fm.renameInput)
	if newName == "" || newName == oldName {
		return
	}

	oldPath := filepath.Join(fm.currentDir, oldName)
	newPath := filepath.Join(fm.currentDir, newName)
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return
	}

	fm.loadDirectory()
	// Try to select the renamed entry
	for i, e := range fm.entries {
		if e.Name() == newName {
			fm.cursor = i
			fm.adjustScroll()
			break
		}
	}
}

func (fm *FileManager) enterSelected() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	entry := fm.entries[fm.cursor]
	if !entry.IsDir() {
		return
	}
	newDir := filepath.Join(fm.currentDir, entry.Name())
	fm.currentDir = newDir
	fm.cursor = 0
	fm.offset = 0
	fm.loadDirectory()
}

func (fm *FileManager) goParent() {
	parent := filepath.Dir(fm.currentDir)
	if parent == fm.currentDir {
		return
	}
	oldName := filepath.Base(fm.currentDir)
	fm.currentDir = parent
	fm.cursor = 0
	fm.offset = 0
	fm.loadDirectory()

	// Try to restore cursor to previous directory
	for i, e := range fm.entries {
		if e.Name() == oldName {
			fm.cursor = i
			fm.adjustScroll()
			break
		}
	}
}

func (fm *FileManager) openEditor() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	entry := fm.entries[fm.cursor]
	if entry.IsDir() {
		return
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	filePath := filepath.Join(fm.currentDir, entry.Name())

	// Restore terminal
	fm.ttyOut.WriteString("\033[?25h\033[?1049l")
	term.Restore(fm.stdInFd, &fm.oldState)

	cmd := exec.Command(editor, filePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// Re-enter raw mode and alternate buffer
	newState, _ := term.MakeRaw(fm.stdInFd)
	if newState != nil {
		fm.oldState = *newState
	}
	fm.ttyOut.WriteString("\033[?1049h\033[?25l")

	// Refresh terminal size
	cols, rows, err := term.GetSize(int(fm.ttyOut.Fd()))
	if err == nil {
		fm.rows = rows
		fm.cols = cols
	}

	fm.loadDirectory()
	fm.clampCursor()
	fm.adjustScroll()
}

// Clipboard actions

func (fm *FileManager) clipboardCut() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	absPath := filepath.Join(fm.currentDir, fm.entries[fm.cursor].Name())
	saveClipboard("cut", []string{absPath})
}

func (fm *FileManager) clipboardCopy() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	absPath := filepath.Join(fm.currentDir, fm.entries[fm.cursor].Name())
	saveClipboard("copy", []string{absPath})
}

func (fm *FileManager) clipboardPaste() {
	op, paths := loadClipboard()
	if op == "" || len(paths) == 0 {
		return
	}
	for _, src := range paths {
		base := filepath.Base(src)
		dest := filepath.Join(fm.currentDir, base)
		if src == dest {
			if op == "cut" {
				continue
			}
			// Copy to same directory: go straight to versioned name
			dest = versionedPath(dest)
		} else if _, err := os.Lstat(dest); err == nil {
			// Destination exists but is a different file
			choice := fm.promptConflict(base)
			if choice == 0 {
				// Cancel
				return
			}
			if choice == 2 {
				// Version number
				dest = versionedPath(dest)
			}
			// choice == 1: overwrite, use dest as-is
		}

		if op == "cut" {
			if err := os.Rename(src, dest); err != nil {
				return
			}
		} else {
			if err := CopyFile(src, dest); err != nil {
				return
			}
		}
	}
	if op == "cut" {
		clearClipboard()
	}
	fm.loadDirectory()
	fm.clampCursor()
	fm.adjustScroll()
}

// promptConflict shows an overlay asking the user how to handle a name conflict.
// Returns 0=cancel, 1=overwrite, 2=version number.
func (fm *FileManager) promptConflict(name string) int {
	lines := []string{
		fmt.Sprintf(" \"%s\" already exists ", name),
		"",
		"  o  Overwrite",
		"  v  Rename with version number",
		"  Esc  Cancel",
	}

	// Find box width
	boxW := 0
	for _, l := range lines {
		runes := utf8.RuneCountInString(l)
		if runes > boxW {
			boxW = runes
		}
	}
	boxW += 2 // padding
	if boxW > fm.cols-4 {
		boxW = fm.cols - 4
	}

	startCol := (fm.cols - boxW) / 2
	if startCol < 1 {
		startCol = 1
	}
	startRow := (fm.rows-len(lines))/2 + 1
	if startRow < 2 {
		startRow = 2
	}

	var buf bytes.Buffer
	for i, line := range lines {
		row := startRow + i
		if row > fm.rows {
			break
		}
		buf.WriteString(fmt.Sprintf("\033[%d;%dH", row, startCol))
		buf.WriteString("\033[7m")
		lineRunes := utf8.RuneCountInString(line)
		if lineRunes > boxW {
			line = truncateMiddle(line, boxW)
			lineRunes = boxW
		}
		buf.WriteString(line)
		pad := boxW - lineRunes
		if pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
		buf.WriteString("\033[0m")
	}
	fm.ttyOut.Write(buf.Bytes())

	// Read one key
	keyBuf := make([]byte, 16)
	n, err := os.Stdin.Read(keyBuf)
	if err != nil || n == 0 {
		return 0
	}
	switch keyBuf[0] {
	case 'o':
		return 1
	case 'v':
		return 2
	default:
		return 0
	}
}

// versionedPath returns a path with a version number appended, e.g.
// "file.txt" -> "file (1).txt", "dir" -> "dir (1)".
func versionedPath(dest string) string {
	dir := filepath.Dir(dest)
	base := filepath.Base(dest)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

// Delete to trash

func (fm *FileManager) deleteEntry() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	entry := fm.entries[fm.cursor]
	name := entry.Name()
	absPath := filepath.Join(fm.currentDir, name)

	if !fm.promptDelete(entry) {
		return
	}

	if err := TrashFile(absPath); err != nil {
		return
	}

	fm.loadDirectory()
	fm.clampCursor()
	fm.adjustScroll()
}

// promptDelete shows a confirmation overlay. Returns true if the user confirms.
func (fm *FileManager) promptDelete(entry os.DirEntry) bool {
	name := entry.Name()
	lines := []string{
		fmt.Sprintf(" Delete \"%s\"? ", name),
	}

	// For non-empty directories, show entry count
	if entry.IsDir() {
		dirPath := filepath.Join(fm.currentDir, name)
		if subEntries, err := os.ReadDir(dirPath); err == nil && len(subEntries) > 0 {
			lines = append(lines, fmt.Sprintf(" (directory with %d entries) ", len(subEntries)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "  y  Confirm")
	lines = append(lines, "  Esc  Cancel")

	// Find box width
	boxW := 0
	for _, l := range lines {
		runes := utf8.RuneCountInString(l)
		if runes > boxW {
			boxW = runes
		}
	}
	boxW += 2
	if boxW > fm.cols-4 {
		boxW = fm.cols - 4
	}

	startCol := (fm.cols - boxW) / 2
	if startCol < 1 {
		startCol = 1
	}
	startRow := (fm.rows-len(lines))/2 + 1
	if startRow < 2 {
		startRow = 2
	}

	var buf bytes.Buffer
	for i, line := range lines {
		row := startRow + i
		if row > fm.rows {
			break
		}
		buf.WriteString(fmt.Sprintf("\033[%d;%dH", row, startCol))
		buf.WriteString("\033[7m")
		lineRunes := utf8.RuneCountInString(line)
		if lineRunes > boxW {
			line = truncateMiddle(line, boxW)
			lineRunes = boxW
		}
		buf.WriteString(line)
		pad := boxW - lineRunes
		if pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
		buf.WriteString("\033[0m")
	}
	fm.ttyOut.Write(buf.Bytes())

	// Read one key
	keyBuf := make([]byte, 16)
	n, err := os.Stdin.Read(keyBuf)
	if err != nil || n == 0 {
		return false
	}
	return keyBuf[0] == 'y'
}

// Bookmarks

const bookmarksFileName = "fm_bookmarks"

func bookmarksFilePath() (string, error) {
	dir, err := GetHistoryDir()
	if err != nil {
		return "", err
	}
	return dir + bookmarksFileName, nil
}

func loadBookmarks() map[byte]string {
	bookmarks := make(map[byte]string)
	path, err := bookmarksFilePath()
	if err != nil {
		return bookmarks
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return bookmarks
	}
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) >= 3 && line[1] == ' ' {
			bookmarks[line[0]] = line[2:]
		}
	}
	return bookmarks
}

func isBookmarkChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')
}

// Clipboard

const clipboardFileName = "fm_clipboard"

func clipboardFilePath() (string, error) {
	dir, err := GetHistoryDir()
	if err != nil {
		return "", err
	}
	return dir + clipboardFileName, nil
}

func loadClipboard() (string, []string) {
	path, err := clipboardFilePath()
	if err != nil {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) < 2 {
		return "", nil
	}
	op := lines[0]
	if op != "cut" && op != "copy" {
		return "", nil
	}
	return op, lines[1:]
}

func saveClipboard(op string, paths []string) error {
	path, err := clipboardFilePath()
	if err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString(op)
	sb.WriteByte('\n')
	for _, p := range paths {
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func clearClipboard() error {
	path, err := clipboardFilePath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func saveBookmarks(bookmarks map[byte]string) error {
	path, err := bookmarksFilePath()
	if err != nil {
		return err
	}
	var sb strings.Builder
	for c := byte('0'); c <= '9'; c++ {
		if dir, ok := bookmarks[c]; ok {
			sb.WriteByte(c)
			sb.WriteByte(' ')
			sb.WriteString(dir)
			sb.WriteByte('\n')
		}
	}
	for c := byte('a'); c <= 'z'; c++ {
		if dir, ok := bookmarks[c]; ok {
			sb.WriteByte(c)
			sb.WriteByte(' ')
			sb.WriteString(dir)
			sb.WriteByte('\n')
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}
