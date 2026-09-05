package main

import (
	"github.com/rivo/uniseg"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"math/rand"
	"strings"
	"slices"
)

func TestHistory(t *testing.T) {
	path := "test.mshell_history"
	_ = WriteToHistory(os.Getenv("HOME"), "echo hello", path)
}

func TestAllMatchesAreFiles(t *testing.T) {
	if allMatchesAreFiles(nil) {
		t.Fatalf("expected false for empty matches")
	}

	if !allMatchesAreFiles([]TabMatch{
		{TabMatchType: TABMATCHFILE, Match: "Floor 0/"},
		{TabMatchType: TABMATCHFILE, Match: "Floor 1/"},
	}) {
		t.Fatalf("expected true when all matches are files")
	}

	if allMatchesAreFiles([]TabMatch{
		{TabMatchType: TABMATCHFILE, Match: "Floor 0/"},
		{TabMatchType: TABMATCHDEF, Match: "FloorHelper"},
	}) {
		t.Fatalf("expected false for mixed match types")
	}
}

func TestBuildCompletionInsertPrefersPathQuoteForFileMatches(t *testing.T) {
	state := TermState{l: NewLexer("", nil)}

	got := state.buildCompletionInsert("Floor 0/", LITERAL, true)
	want := "`Floor 0/"
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildCompletionInsert("Floor 0.txt", LITERAL, true)
	want = "`Floor 0.txt` "
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildCompletionInsert("Floor 0/", LITERAL, false)
	want = "'Floor 0/'"
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}
}

func TestBuildSharedCompletionInsertUsesBacktickForFilePrefixes(t *testing.T) {
	state := TermState{l: NewLexer("", nil)}

	got := state.buildSharedCompletionInsert("Floor ", LITERAL, true)
	want := "`Floor "
	if got != want {
		t.Fatalf("buildSharedCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildSharedCompletionInsert("Floor ", LITERAL, false)
	want = "'Floor "
	if got != want {
		t.Fatalf("buildSharedCompletionInsert() = %q, want %q", got, want)
	}
}

func TestDefaultAppCommandFallsBackToPlatformDefault(t *testing.T) {
	tests := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{goos: "linux", wantName: "xdg-open", wantArgs: []string{"/tmp/init.msh"}},
		{goos: "darwin", wantName: "open", wantArgs: []string{"/tmp/init.msh"}},
		{goos: "windows", wantName: "powershell.exe", wantArgs: []string{"-NoProfile", "-Command", "Start-Process -FilePath '/tmp/init.msh'"}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			command, args, err := defaultAppCommand("/tmp/init.msh", tt.goos)
			if err != nil {
				t.Fatalf("defaultAppCommand() error = %v", err)
			}

			if command != tt.wantName {
				t.Fatalf("command = %q, want %q", command, tt.wantName)
			}

			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestOpenPathInEditorOrDefaultAppUsesEditorStringDirectly(t *testing.T) {
	t.Setenv("EDITOR", `C:\Program Files\Neovim\bin\nvim.exe`)

	oldRunAttachedCommand := runAttachedCommand
	defer func() {
		runAttachedCommand = oldRunAttachedCommand
	}()

	var gotName string
	var gotArgs []string
	runAttachedCommand = func(name string, args []string) error {
		gotName = name
		gotArgs = append([]string{}, args...)
		return nil
	}

	if err := openPathInEditorOrDefaultApp(`C:\Users\me\init.msh`); err != nil {
		t.Fatalf("openPathInEditorOrDefaultApp() error = %v", err)
	}

	if gotName != `C:\Program Files\Neovim\bin\nvim.exe` {
		t.Fatalf("command = %q, want %q", gotName, `C:\Program Files\Neovim\bin\nvim.exe`)
	}

	wantArgs := []string{`C:\Users\me\init.msh`}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestRunEditCommandOpensVersionedInitFilePath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("EDITOR", "my-editor")
	t.Setenv("MSHINIT", "")

	oldRunAttachedCommand := runAttachedCommand
	defer func() {
		runAttachedCommand = oldRunAttachedCommand
	}()

	var gotName string
	var gotArgs []string
	runAttachedCommand = func(name string, args []string) error {
		gotName = name
		gotArgs = append([]string{}, args...)
		return nil
	}

	exitCode := runEditCommand([]string{"init"})
	if exitCode != 0 {
		t.Fatalf("runEditCommand() = %d, want 0", exitCode)
	}

	expectedPath := filepath.Join(configHome, "msh", mshellVersion, "init.msh")
	if gotName != "my-editor" {
		t.Fatalf("command = %q, want %q", gotName, "my-editor")
	}

	wantArgs := []string{expectedPath}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestRunEditCommandUsesMSHINITOverride(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), "custom", "init.msh")
	t.Setenv("MSHINIT", overridePath)
	t.Setenv("EDITOR", "my-editor")

	oldRunAttachedCommand := runAttachedCommand
	defer func() {
		runAttachedCommand = oldRunAttachedCommand
	}()

	var gotArgs []string
	runAttachedCommand = func(name string, args []string) error {
		gotArgs = append([]string{}, args...)
		return nil
	}

	exitCode := runEditCommand([]string{"init"})
	if exitCode != 0 {
		t.Fatalf("runEditCommand() = %d, want 0", exitCode)
	}

	wantArgs := []string{overridePath}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestClusterSegmentation(t *testing.T) {
	// Two flags must segment as two 2-rune clusters, not one 4-rune blob.
	var got []string
	s := "🇺🇸🇺🇸"
	state := -1
	var cl string
	for len(s) > 0 {
		cl, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		got = append(got, cl)
	}
	if len(got) != 2 || got[0] != "🇺🇸" || got[1] != "🇺🇸" {
		t.Errorf("flag clusters = %q, want two flags", got)
	}
}

func TestAsciiAtomsInto(t *testing.T) {
	command := SourceText("a\t\x1a\n")
	got, allAscii := asciiAtomsInto(nil, command)
	if !allAscii {
		t.Fatal("seven-bit source should use the Ascii atomizer")
	}

	want := []DisplayAtom{
		{SourceStart: 0, SourceEnd: 1, Width: 1, Kind: AtomAscii},
		{SourceStart: 1, SourceEnd: 2, Width: UnresolvedWidth, Kind: AtomControl},
		{SourceStart: 2, SourceEnd: 3, Width: 2, Kind: AtomControl},
		{SourceStart: 3, SourceEnd: 4, Width: 0, Kind: AtomHardBreak},
	}

	if !slices.Equal(got, want) {
		t.Errorf("atoms = %+v, want %+v", got, want)
	}

	wantText := []string{"a", "\u25B8", "^Z", ""}
	for i, atom := range got {
		if s := atom.displayText(command); s != wantText[i] {
			t.Errorf("atom %d displayText = %q, want %q", i, s, wantText[i])
		}
	}

	got, allAscii = asciiAtomsInto(got, SourceText("a\r\nb"))
	wantCRLF := []DisplayAtom{
		{SourceStart: 0, SourceEnd: 1, Width: 1, Kind: AtomAscii},
		{SourceStart: 1, SourceEnd: 3, Width: 0, Kind: AtomHardBreak},
		{SourceStart: 3, SourceEnd: 4, Width: 1, Kind: AtomAscii},
	}
	if !allAscii || !slices.Equal(got, wantCRLF) {
		t.Errorf("crlf atoms = %+v, want %+v", got, wantCRLF)
	}

	got, allAscii = asciiAtomsInto(got, SourceText("a\u00e9"))
	if allAscii || len(got) != 0 {
		t.Errorf("non-ASCII input returned atoms=%+v, ok=%v", got, allAscii)
	}
}

func testWidthLookup(widths map[string]int) func(string) int {
	return func(cluster string) int {
		if len(cluster) == 1 && cluster[0] >= 0x20 && cluster[0] <= 0x7e {
			return 1
		}

		width, ok := widths[cluster]
		if !ok {
			panic("missing test width for cluster: " + cluster)
		}
		return width
	}
}

func sumTestWidths(text string, widthOf func(string) int) int {
	total := 0
	state := -1

	for len(text) > 0 {
		cluster, rest, _, nextState := uniseg.FirstGraphemeClusterInString(text, state)
		total += widthOf(cluster)
		text = rest
		state = nextState
	}

	return total
}

func TestLayout(t *testing.T) {
	widthOf := testWidthLookup(map[string]int{"世": 2,})

	// Exact fill: "abcdef" at width 3 → two full rows, pending wrap.
	r := layoutInto(nil, "abcdef", 6, 3, widthOf)
	if len(r.Rows) != 2 || r.Rows[0].Text != "abc" || r.Rows[1].Text != "def" {
		t.Fatalf("rows = %+v", r.Rows)
	}
	if r.Rows[0].EndType != RowEndSoftExact || !r.PendingWrap {
		t.Errorf("want SoftExact + PendingWrap, got %+v", r)
	}
	if r.CursorRow != 1 || r.CursorCol != 3 {
		t.Errorf("cursor at end = (%d,%d), want (1,3)", r.CursorRow, r.CursorCol)
	}

	// Early wrap: 世 (2 cells) doesn't fit after "ab" at width 3.
	r = layoutInto(nil, "ab世", 0, 3, widthOf)
	if len(r.Rows) != 2 || r.Rows[0].Text != "ab" || r.Rows[0].EndType != RowEndSoftEarly {
		t.Fatalf("early wrap rows = %+v", r.Rows)
	}
	if r.Rows[1].Text != "世" || r.Rows[1].Width != 2 || r.PendingWrap {
		t.Errorf("second row = %+v", r.Rows[1])
	}

	// Cursor on the wrap boundary belongs to the new row.
	r = layoutInto(nil, "abcd", 3, 3, widthOf)
	if r.CursorRow != 1 || r.CursorCol != 0 {
		t.Errorf("boundary cursor = (%d,%d), want (1,0)", r.CursorRow, r.CursorCol)
	}

	// Empty text: one empty row, cursor at origin.
	r = layoutInto(nil, "", 0, 80, widthOf)
	if len(r.Rows) != 1 || r.CursorRow != 0 || r.CursorCol != 0 || r.PendingWrap {
		t.Errorf("empty = %+v", r)
	}

	// Reuse contract: second call must not grow a new backing array.
	first := layoutInto(nil, "abcdef", 0, 3, widthOf)
	second := layoutInto(first.Rows, "xyzuvw", 0, 3, widthOf)
	if &first.Rows[0] != &second.Rows[0] {
		t.Error("dst backing array was not reused")
	}
}

func TestLayoutHardNewline(t *testing.T) {
	widthOf := testWidthLookup(nil)

	// Basic split; newline is in neither row's text nor width.
	r := layoutInto(nil, "ab\ncd", 5, 80, widthOf)
	if len(r.Rows) != 2 || r.Rows[0].Text != "ab" || r.Rows[1].Text != "cd" {
		t.Fatalf("rows = %+v", r.Rows)
	}
	if r.Rows[0].EndType != RowEndHard || r.Rows[0].Width != 2 {
		t.Errorf("first row = %+v", r.Rows[0])
	}
	if r.CursorRow != 1 || r.CursorCol != 2 {
		t.Errorf("cursor at end = (%d,%d), want (1,2)", r.CursorRow, r.CursorCol)
	}

	// Cursor on the newline = end of first row; just after it = start of second.
	if r := layoutInto(nil, "ab\ncd", 2, 80, widthOf); r.CursorRow != 0 || r.CursorCol != 2 {
		t.Errorf("cursor on \\n = (%d,%d), want (0,2)", r.CursorRow, r.CursorCol)
	}
	if r := layoutInto(nil, "ab\ncd", 3, 80, widthOf); r.CursorRow != 1 || r.CursorCol != 0 {
		t.Errorf("cursor after \\n = (%d,%d), want (1,0)", r.CursorRow, r.CursorCol)
	}

	// Trailing newline yields an empty final row, cursor lands on it.
	r = layoutInto(nil, "ab\n", 3, 80, widthOf)
	if len(r.Rows) != 2 || r.Rows[1].Text != "" || r.CursorRow != 1 || r.CursorCol != 0 {
		t.Errorf("trailing newline = %+v", r)
	}

	// Consecutive newlines produce an empty middle row.
	r = layoutInto(nil, "a\n\nb", 0, 80, widthOf)
	if len(r.Rows) != 3 || r.Rows[1].Text != "" || r.Rows[1].EndType != RowEndHard {
		t.Errorf("blank line = %+v", r.Rows)
	}

	// Hard break composes with soft wrapping.
	r = layoutInto(nil, "abcd\nef", 0, 3, widthOf)
	if len(r.Rows) != 3 || r.Rows[0].EndType != RowEndSoftExact ||
		r.Rows[1].Text != "d" || r.Rows[1].EndType != RowEndHard || r.Rows[2].Text != "ef" {
		t.Errorf("wrap+hard = %+v", r.Rows)
	}
}

func getClusterBoundaries(text string) []int {
	s := text
	state := -1
	offset := 0
	var cl string
	boundaries := make([]int, 0, len(text))
	for len(s) > 0 {
		boundaries = append(boundaries, offset)
		cl, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		offset += len(cl)
	}
	boundaries = append(boundaries, len(text))

	return boundaries
}

func TestLayoutProperties(t *testing.T) {
	pieces := []string{
		"a", "Z", " ", // printable ASCII
		"\n", "\r\n", // hard breaks
		"世", "界", // wide CJK
		"é", "e\u0301", // precomposed vs combining
		"👨‍👩‍👧‍👦", "❤️", "☂\uFE0F", "☂\uFE0E", // ZWJ family, VS16, VS15
		"🇺🇸", "±", // flag pair, East Asian ambiguous
	}
	widths := []int{2, 3, 5, 10, 80}

	widthMaps := []map[string]int{
		{
			"世": 2, "界": 2,
			"é": 1, "e\u0301": 1,
			"👨‍👩‍👧‍👦": 2, "❤️": 2,
			"☂\uFE0F": 2, "☂\uFE0E": 1,
			"🇺🇸": 2, "±": 1,
		},
		{
			"世": 1, "界": 1,
			"é": 2, "e\u0301": 2,
			"👨‍👩‍👧‍👦": 1, "❤️": 1,
			"☂\uFE0F": 1, "☂\uFE0E": 2,
			"🇺🇸": 1, "±": 2,
		},
	}

	rng := rand.New(rand.NewSource(1))

	var dst []LayoutRow

	for i := 0; i < 300; i++ {
		var sb strings.Builder
		for n := rng.Intn(31); n > 0; n-- {
			sb.WriteString(pieces[rng.Intn(len(pieces))])
		}
		text := sb.String()

		// Every cluster boundary is valid cursor position
		boundaries := getClusterBoundaries(text)

		for _, width := range widths {
			for widthMapIndex, widthMap := range widthMaps {
				widthOf := testWidthLookup(widthMap)
				for _, cursor := range boundaries {
					r := layoutInto(dst, text, cursor, width, widthOf)
					dst = r.Rows
					checkLayoutProperties(t, text, cursor, width, widthOf, r)
					if t.Failed() {
						t.Fatalf("input %q cursor=%d width=%d widthMap=%d", text, cursor, width, widthMapIndex)
					}
				}
			}
		}
	}
}

func checkLayoutProperties(t *testing.T, text string, cursor int, terminalWidth int, widthOf func(string) int, r LayoutResult) {
	t.Helper()

	rows := r.Rows
	if len(rows) == 0 {
		t.Error("no rows")
		return
	}

	// 1. Rows tile the input; a RowEndHard row skips exactly one newline cluster
	offset := 0
	for i, row := range rows {
		if offset+len(row.Text) > len(text) || text[offset:offset+len(row.Text)] != row.Text {
			t.Errorf("row %d text %q does not match input at offset %d", i, row.Text, offset)
			return
		}
		offset += len(row.Text)

		// The '\r\n' or '\n' is not part of the *Rows* text, so we need to handle moving the offset in that case.
		if row.EndType == RowEndHard {
			rest := text[offset:]
			if strings.HasPrefix(rest, "\r\n") {
				offset += 2
			} else if strings.HasPrefix(rest, "\n") {
				offset += 1
			} else {
				t.Errorf("row %d is RowEndHard but input at offset %d is not a newline", i, offset)
				return
			}
		}
	}

	if offset != len(text) {
		t.Errorf("rows cover %d bytes, but input is %d bytes", offset, len(text))
	}

	checkWidth := func(i int, row LayoutRow) {
		if got := sumTestWidths(row.Text, widthOf); row.Width != got {
			t.Errorf("row %d Width=%d but summed widths=%d", i, row.Width, got)
		}
		if row.Width > terminalWidth {
			cl, rest, _, _ := uniseg.FirstGraphemeClusterInString(row.Text, -1)
			if cl != row.Text || rest != "" {
				t.Errorf("row %d Width=%d exceeds terminal width %d on a multi-cluster row %q", i, row.Width, terminalWidth, row.Text)
			}
		}
	}

	// 2 + 3 for every row but the last: never Final, soft rows justified by the next row.
	for i, row := range rows[:len(rows)-1] {
		checkWidth(i, row)
		switch row.EndType {
		case RowEndFinal:
			t.Errorf("row %d of %d is RowEndFinal but not last", i, len(rows))
		case RowEndSoftExact:
			if row.Width != terminalWidth {
				t.Errorf("row %d SoftExact but Width=%d, terminal width %d", i, row.Width, terminalWidth)
			}
		case RowEndSoftEarly:
			if row.Width >= terminalWidth {
				t.Errorf("row %d SoftEarly but Width=%d, terminal width %d", i, row.Width, terminalWidth)
			}
		}
		if row.EndType == RowEndSoftExact || row.EndType == RowEndSoftEarly {
			if row.Text == "" {
				t.Errorf("row %d soft-wrapped but empty", i)
			} else {
				next, _, _, _ := uniseg.FirstGraphemeClusterInString(rows[i+1].Text, -1)
				if w := widthOf(next); row.Width+w <= terminalWidth {
					t.Errorf("row %d ended soft at width %d but next cluster %q (width %d) would have fit in %d",
						i, row.Width, next, w, terminalWidth)
				}
			}
		}
	}

	// The last row: same width rules, and it must be the one Final row.
	lastRow := rows[len(rows)-1]
	checkWidth(len(rows)-1, lastRow)
	if lastRow.EndType != RowEndFinal {
		t.Errorf("last row EndType=%d, want RowEndFinal", lastRow.EndType)
	}

	// 4. Cursor Checks
	if r.CursorRow < 0 || r.CursorRow >= len(rows) {
		t.Errorf("cursor row %d out of bounds [0,%d]", r.CursorRow, len(rows))
	} else {
		if r.CursorCol < 0 || r.CursorCol > rows[r.CursorRow].Width {
			t.Errorf("cursor col %d out of bounds [0,%d] for row %d", r.CursorCol, rows[r.CursorRow].Width, r.CursorRow)
		}

		if r.CursorCol == terminalWidth && rows[r.CursorRow].Width != terminalWidth {
			t.Errorf("cursor col %d at terminal width but row width is %d", r.CursorCol, rows[r.CursorRow].Width)
		}
	}

	// 5. PendingWrap tracks the final row exactly filling the terminal
	if r.PendingWrap != (rows[len(rows)-1].Width == terminalWidth) {
		t.Errorf("PendingWrap = %v, but last row width = %d, terminal width = %d", r.PendingWrap, rows[len(rows)-1].Width, terminalWidth)
	}
}
