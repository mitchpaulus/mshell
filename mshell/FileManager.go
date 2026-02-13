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

	ttyOut *os.File // where TUI output goes (may differ from stdout)
}

// RunFileManager runs as a standalone subcommand (msh fm).
// TUI goes to /dev/tty, final directory is printed to stdout.
func RunFileManager() int {
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

	fm.currentDir, err = os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting working directory: %s\n", err)
		return 1
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
// Returns the directory the user was in when they quit.
func RunFileManagerInteractive(stdInFd int, oldState *term.State) string {
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

	fm.currentDir, _ = os.Getwd()
	fm.loadDirectory()

	// Terminal is already in raw mode from the interactive session.
	// Just switch to alternate buffer and hide cursor.
	fm.ttyOut.WriteString("\033[?1049h\033[?25l")

	fm.mainLoop()

	fm.ttyOut.WriteString("\033[?25h\033[?1049l")

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
}

func (fm *FileManager) visibleRows() int {
	return fm.rows - 1
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

func (fm *FileManager) render() {
	var buf bytes.Buffer

	// Move cursor to top-left, clear screen
	buf.WriteString("\033[H\033[2J")

	leftW := fm.leftPaneWidth()
	rightW := fm.cols - leftW - 1 // 1 for separator
	if rightW < 0 {
		rightW = 0
	}

	// Header row
	header := fmt.Sprintf(" %s@%s: %s", fm.username, fm.hostname, fm.currentDir)
	headerRunes := utf8.RuneCountInString(header)
	if headerRunes > fm.cols {
		header = string([]rune(header)[:fm.cols])
	} else {
		header += strings.Repeat(" ", fm.cols-headerRunes)
	}
	buf.WriteString("\033[7m") // reverse video
	buf.WriteString(header)
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
			nameRunes := utf8.RuneCountInString(name)

			if idx == fm.cursor {
				buf.WriteString("\033[7m") // reverse video for selected
			}

			if entry.IsDir() {
				buf.WriteString("\033[34m") // blue for directories
			}

			if nameRunes > leftW {
				name = string([]rune(name)[:leftW])
				nameRunes = leftW
			}
			buf.WriteString(" ")
			buf.WriteString(name)
			// Pad to leftW
			pad := leftW - nameRunes - 1
			if pad > 0 {
				buf.WriteString(strings.Repeat(" ", pad))
			}

			if idx == fm.cursor {
				buf.WriteString("\033[0m")
			}
		} else {
			buf.WriteString(strings.Repeat(" ", leftW))
		}

		// Separator
		buf.WriteString("\033[90m\u2502\033[0m")

		// Right pane
		if row < len(previewLines) {
			line := previewLines[row]
			lineRunes := utf8.RuneCountInString(line)
			if lineRunes > rightW {
				line = string([]rune(line)[:rightW])
			}
			buf.WriteString(line)
		}
	}

	fm.ttyOut.Write(buf.Bytes())
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

	key := buf[0]

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
	}

	if key != 'g' {
		fm.lastKey = 0
	}

	return false
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
