package main

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func promptItem(member string, payload ...MShellObject) *MShellEnum {
	return &MShellEnum{EnumName: "PromptItem", Member: member, Payload: payload}
}

func enumValue(enum, member string) *MShellEnum {
	return &MShellEnum{EnumName: enum, Member: member}
}

func promptList(items ...*MShellEnum) *MShellList {
	list := NewList(0)
	for _, item := range items {
		list.Items = append(list.Items, item)
	}
	return list
}

func TestRenderPromptSanitizesText(t *testing.T) {
	r, err := renderPrompt(promptList(
		promptItem("promptText", MShellString{"a\nb\r\x1b[31m"}),
		promptItem("promptNewline"),
		promptItem("promptText", MShellString{":: "}),
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a^Jb^M^[[31m\n:: "; string(r.Text) != want {
		t.Fatalf("text %q, want %q", r.Text, want)
	}
}

func TestRenderPromptStyles(t *testing.T) {
	r, err := renderPrompt(promptList(
		promptItem("setFgColorBase16", enumValue("Base16Color", "baseRed"), enumValue("BaseBrightness", "baseBright")),
		promptItem("promptText", MShellString{"x"}),
		promptItem("setBgColorRgb", MShellInt{1}, MShellInt{2}, MShellInt{3}),
		promptItem("setFgColor256", MShellInt{200}),
		promptItem("setTextAttr", enumValue("TextAttribute", "attrReverse")),
		promptItem("clearTextAttr", enumValue("TextAttribute", "attrFaint")),
		promptItem("promptText", MShellString{"y"}),
		promptItem("setBgDefault"),
		promptItem("resetStyle"),
	), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []promptStyle{
		{0, "\033[91m", bgKeep},
		{1, "\033[48;2;1;2;3m", bgSet},
		{1, "\033[38;5;200m", bgKeep},
		{1, "\033[7m", bgKeep},
		{1, "\033[22m", bgKeep},
		{2, "\033[49m", bgClear},
		{2, "\033[0m", bgClear},
	}
	if len(r.Styles) != len(want) {
		t.Fatalf("styles %q, want %q", r.Styles, want)
	}
	for i := range want {
		if r.Styles[i] != want[i] {
			t.Fatalf("style %d is %q, want %q", i, r.Styles[i], want[i])
		}
	}
}

func TestRenderPromptRejects(t *testing.T) {
	cases := []struct {
		name string
		obj  MShellObject
		want string
	}{
		{"not a list", MShellString{"> "}, "list of PromptItem"},
		{"not a PromptItem", promptList(enumValue("CursorShape", "steadyBar")), "not a PromptItem"},
		{"rgb out of range", promptList(promptItem("setFgColorRgb", MShellInt{0}, MShellInt{256}, MShellInt{0})), "0 to 255"},
		{"palette out of range", promptList(promptItem("setBgColor256", MShellInt{-1})), "0 to 255"},
		{"url with a space", promptList(promptItem("promptHyperlink", MShellString{"a b"}, MShellString{"x"})), "percent-encoded"},
		{"url with ESC", promptList(promptItem("promptHyperlink", MShellString{"a\x1b\\b"}, MShellString{"x"})), "percent-encoded"},
	}
	for _, c := range cases {
		_, err := renderPrompt(c.obj, false)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one about %q", c.name, err, c.want)
		}
	}
}

func TestRenderPromptTitleAndHyperlink(t *testing.T) {
	r, err := renderPrompt(promptList(
		promptItem("setWindowTitle", MShellString{"t\x1b\\"}),
		promptItem("promptHyperlink", MShellString{"https://x.test/a%20b"}, MShellString{"link"}),
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasTitle || r.Title != "t^[\\" {
		t.Fatalf("title %q", r.Title)
	}
	if string(r.Text) != "link" || len(r.Styles) != 2 ||
		r.Styles[0] != (promptStyle{0, "\033]8;;https://x.test/a%20b\033\\", bgKeep}) ||
		r.Styles[1] != (promptStyle{4, "\033]8;;\033\\", bgKeep}) {
		t.Fatalf("hyperlink %q %q", r.Text, r.Styles)
	}
}

func TestPaintPromptWritesStylesInPlace(t *testing.T) {
	r, err := renderPrompt(promptList(
		promptItem("setFgColorBase16", enumValue("Base16Color", "baseGreen"), enumValue("BaseBrightness", "baseNormal")),
		promptItem("promptText", MShellString{"ab"}),
		promptItem("setBgColor256", MShellInt{4}),
		promptItem("promptText", MShellString{"c"}),
		promptItem("promptNewline"),
		promptItem("promptText", MShellString{"d"}),
		promptItem("resetStyle"),
		promptItem("promptText", MShellString{"e"}),
	), false)
	if err != nil {
		t.Fatal(err)
	}
	state := TermState{numRows: 5, numCols: 20, promptStyles: r.Styles}
	var output bytes.Buffer
	read := func() (TerminalToken, error) { t.Fatal("ASCII prompt read input"); return nil, io.EOF }
	if err := state.paintPrompt(&output, read, r.Text); err != nil {
		t.Fatal(err)
	}
	// The background is off for the line break and set again after it.
	want := "\r\033[0m" + "\033[32mab\033[48;5;4mc\033[49m\r\n\033[48;5;4md\033[0me\033[0m"
	if output.String() != want {
		t.Fatalf("output %q, want %q", output.String(), want)
	}
	if state.numPromptLines != 2 || state.commandRegion.OriginCol != 3 {
		t.Fatalf("prompt rows %d, command column %d", state.numPromptLines, state.commandRegion.OriginCol)
	}
}

func TestDefaultPromptIsMagenta(t *testing.T) {
	r := defaultPrompt("~/a\nb", true, 2)
	if string(r.Text) != "~/a^Jb (2)> \n:: " || len(r.Styles) != 1 || r.Styles[0].Seq != "\033[35m" {
		t.Fatalf("default prompt %q %q", r.Text, r.Styles)
	}
}

func TestRenderRightPromptIsOneLine(t *testing.T) {
	_, err := renderPrompt(promptList(promptItem("promptText", MShellString{"a"}), promptItem("promptNewline")), true)
	if err == nil || !strings.Contains(err.Error(), "item 1 of the right prompt, 'promptNewline': the right prompt is one line") {
		t.Fatalf("got %v", err)
	}
	// A newline in text is shown visibly, as in the prompt.
	r, err := renderPrompt(promptList(promptItem("promptText", MShellString{"a\nb"})), true)
	if err != nil || r.Text != "a^Jb" {
		t.Fatalf("text %q, error %v", r.Text, err)
	}
}

// rightPromptScreen paints the prompt ":: " and a right prompt on a 5 by 20
// screen, ready for a command.
func rightPromptScreen(t *testing.T, right promptRender) (*commandScreen, *TermState, func() (TerminalToken, error)) {
	screen := newCommandScreen(t, 5, 20)
	state := &TermState{numRows: 5, numCols: 20, rightPrompt: right}
	state.widthCache.remember("世", 2)
	read := func() (TerminalToken, error) { t.Fatal("known widths requested input"); return nil, io.EOF }
	if err := state.paintPrompt(screen, read, ":: "); err != nil {
		t.Fatal(err)
	}
	return screen, state, read
}

func (screen *commandScreen) rowText(row int) string { return strings.Join(screen.cells[row], "") }

func TestRightPromptSharesTheCommandRow(t *testing.T) {
	screen, state, read := rightPromptScreen(t, promptRender{Text: "12:30"})
	// 3 cells of prompt, 5 of right prompt and one blank leave 11 for the command.
	for _, c := range []struct{ command, row0, row1 string }{
		{"", ":: " + strings.Repeat(" ", 12) + "12:30", ""},
		{"abc", ":: abc" + strings.Repeat(" ", 9) + "12:30", ""},
		{strings.Repeat("x", 11), ":: " + strings.Repeat("x", 11) + " 12:30", ""},
		// The command reaches the right prompt, so it is not shown.
		{strings.Repeat("x", 12), ":: " + strings.Repeat("x", 12), ""},
		// Nor with a command of more than one row.
		{"a\nb", ":: a", "b"},
		{strings.Repeat("x", 30), ":: " + strings.Repeat("x", 17), strings.Repeat("x", 13)},
		// It returns when the command is short again.
		{"ab", ":: ab" + strings.Repeat(" ", 10) + "12:30", ""},
	} {
		state.currentCommand, state.index = SourceText(c.command), ByteOffset(len(c.command))
		if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil {
			t.Fatalf("%q: ready %v, error %v", c.command, ready, err)
		}
		if got := strings.TrimRight(screen.rowText(0), " "); got != c.row0 {
			t.Errorf("%q: first row %q, want %q", c.command, got, c.row0)
		}
		if got := strings.TrimRight(screen.rowText(1), " "); got != c.row1 {
			t.Errorf("%q: second row %q, want %q", c.command, got, c.row1)
		}
		view := commandViewportFor(state.displayLayout, state.commandRegion)
		if screen.row != int(view.CursorRow) || screen.col != int(view.CursorCol) || screen.pending {
			t.Errorf("%q: cursor at %d,%d", c.command, screen.row, screen.col)
		}
	}
}

func TestRightPromptCursorInsideCommand(t *testing.T) {
	screen, state, read := rightPromptScreen(t, promptRender{Text: "R"})
	state.currentCommand, state.index = "abcdef", 2
	if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil {
		t.Fatalf("ready %v, error %v", ready, err)
	}
	if got := screen.rowText(0); got != ":: abcdef" + strings.Repeat(" ", 10) + "R" {
		t.Fatalf("row %q", got)
	}
	if screen.row != 0 || screen.col != 5 || screen.pending {
		t.Fatalf("cursor at %d,%d pending %v", screen.row, screen.col, screen.pending)
	}
}

func TestRightPromptStylesAndWideText(t *testing.T) {
	r, err := renderPrompt(promptList(
		promptItem("setFgColor256", MShellInt{4}),
		promptItem("promptText", MShellString{"a世"}),
		promptItem("resetStyle"),
	), true)
	if err != nil {
		t.Fatal(err)
	}
	screen, state, read := rightPromptScreen(t, r)
	state.currentCommand, state.index = "x", 1
	if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil {
		t.Fatalf("ready %v, error %v", ready, err)
	}
	// A wide character is kept one cell from the edge, as in a command.
	if got := screen.rowText(0); got != ":: x" + strings.Repeat(" ", 12) + "a世· " {
		t.Fatalf("row %q", got)
	}
	if last := screen.writes[len(screen.writes)-1]; !strings.Contains(last, "\033[0m\033[17G\033[38;5;4ma世\033[0m\033[0m") {
		t.Fatalf("right prompt not styled in place: %q", last)
	}
}

func TestRightPromptWiderThanTerminalIsNotShown(t *testing.T) {
	screen, state, read := rightPromptScreen(t, promptRender{Text: SourceText(strings.Repeat("r", 30))})
	state.currentCommand, state.index = "", 0
	if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil {
		t.Fatalf("ready %v, error %v", ready, err)
	}
	if got := strings.TrimRight(screen.rowText(0), " "); got != "::" {
		t.Fatalf("row %q", got)
	}
}

func TestRightPromptRepaintedAfterResize(t *testing.T) {
	screen, state, read := rightPromptScreen(t, promptRender{Text: "12:30"})
	state.currentCommand, state.index = "ab", 2
	if ready, err := state.refreshCommandDisplay(screen, read, &state.commandRegion); !ready || err != nil {
		t.Fatalf("ready %v, error %v", ready, err)
	}
	wide := newCommandScreen(t, 5, 30)
	if err := state.redrawPromptAfterResize(wide, read, 30, 5); err != nil {
		t.Fatal(err)
	}
	if ready, err := state.refreshCommandDisplay(wide, read, &state.commandRegion); !ready || err != nil {
		t.Fatalf("ready %v, error %v", ready, err)
	}
	if got := wide.rowText(0); got != ":: ab" + strings.Repeat(" ", 20) + "12:30" {
		t.Fatalf("row %q", got)
	}
}

// A prompt rendered into the last prompt's memory is the same as one
// rendered fresh, whether or not it changed.
func TestRenderPromptReusingMatchesFresh(t *testing.T) {
	hyperlinks := promptList(
		promptItem("promptHyperlink", MShellString{"https://x.test/a"}, MShellString{"a"}),
		promptItem("promptHyperlink", MShellString{"https://x.test/b"}, MShellString{"b"}),
	)
	var last promptRender
	for i, list := range []*MShellList{benchRightPromptList(" 1.2s"), benchRightPromptList(" 1.2s"), benchRightPromptList(" 3.4s"), hyperlinks, promptList(), hyperlinks} {
		want, err := renderPrompt(list, true)
		if err != nil {
			t.Fatal(err)
		}
		if last, err = renderPromptReusing(list, true, last); err != nil {
			t.Fatal(err)
		}
		if last.Text != want.Text || !slices.Equal(last.Styles, want.Styles) {
			t.Fatalf("prompt %d: text %q styles %q, want %q %q", i, last.Text, last.Styles, want.Text, want.Styles)
		}
	}
}

// benchRightPromptList is a right prompt like a user's: colored text, an
// attribute, a wide character and a reset. duration is its last text.
func benchRightPromptList(duration string) *MShellList {
	return promptList(
		promptItem("setFgColorRgb", MShellInt{120}, MShellInt{200}, MShellInt{40}),
		promptItem("promptText", MShellString{"main "}),
		promptItem("setFgColor256", MShellInt{244}),
		promptItem("setTextAttr", enumValue("TextAttribute", "attrBold")),
		promptItem("promptText", MShellString{"世 12:30:45"}),
		promptItem("setFgColorBase16", enumValue("Base16Color", "baseRed"), enumValue("BaseBrightness", "baseBright")),
		promptItem("promptText", MShellString{duration}),
		promptItem("resetStyle"),
	)
}

func BenchmarkRightPrompt(b *testing.B) {
	list := benchRightPromptList(" 1.2s")
	read := func() (TerminalToken, error) { b.Fatal("known widths requested input"); return nil, io.EOF }
	b.Run("Render", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := renderPrompt(list, true); err != nil { b.Fatal(err) }
		}
	})
	right, err := renderPrompt(list, true)
	if err != nil { b.Fatal(err) }
	// Each prompt after the first renders into the styles of the one before.
	b.Run("RenderUnchanged", func(b *testing.B) {
		var last promptRender
		b.ReportAllocs()
		for b.Loop() {
			if last, err = renderPromptReusing(list, true, last); err != nil { b.Fatal(err) }
		}
	})
	// The text differs from the last prompt's, as a clock's does.
	b.Run("RenderChanged", func(b *testing.B) {
		lists := [2]*MShellList{list, benchRightPromptList(" 3.4s")}
		var last promptRender
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			if last, err = renderPromptReusing(lists[i&1], true, last); err != nil { b.Fatal(err) }
			i++
		}
	})
	for _, tt := range []struct { name string; right promptRender }{{"None", promptRender{}}, {"Styled", right}} {
		b.Run("PaintPrompt/"+tt.name, func(b *testing.B) {
			state := &TermState{numRows: 50, numCols: 120, rightPrompt: tt.right}
			state.widthCache.remember("世", 2)
			if err := state.paintPrompt(io.Discard, read, ":: "); err != nil { b.Fatal(err) }
			b.ReportAllocs()
			for b.Loop() {
				if err := state.paintPrompt(io.Discard, read, ":: "); err != nil { b.Fatal(err) }
			}
		})
		b.Run("Keystroke/"+tt.name, func(b *testing.B) {
			state := &TermState{numRows: 50, numCols: 120, rightPrompt: tt.right, currentCommand: "echo hello", index: 10}
			state.widthCache.remember("世", 2)
			if err := state.paintPrompt(io.Discard, read, ":: "); err != nil { b.Fatal(err) }
			if ready, err := state.refreshCommandDisplay(io.Discard, read, &state.commandRegion); !ready || err != nil { b.Fatal(err) }
			b.ReportAllocs()
			for b.Loop() {
				if ready, err := state.refreshCommandDisplay(io.Discard, read, &state.commandRegion); !ready || err != nil { b.Fatal(err) }
			}
			b.ReportMetric(float64(len(state.renderBuffer)), "paint-bytes/op")
		})
	}
}

// BenchmarkPromptQuotes runs a prompt quote and the documented right prompt
// quote and renders what they return, as each prompt does. PerQuote starts
// a Ctrl-C watcher for each quote, and Shared one for both.
func BenchmarkPromptQuotes(b *testing.B) {
	state := keyPathState(b, `
def myPrompt (PromptInfo -- new [PromptItem])
    info!
    [baseGreen baseNormal setFgColorBase16 $"({@info :stackDepth?})> " promptText resetStyle]
end
def myRightPrompt (PromptInfo -- new [PromptItem])
    info!
    [attrFaint setTextAttr $"{@info :lastDurationMs?} ms" promptText resetStyle]
end
(myPrompt) setPrompt
(myRightPrompt) setRightPrompt
`, 0)
	var running atomic.Pointer[evalCancel]
	if r, ok := state.userPrompt(&running); !ok || r.Text != "(0)> " { b.Fatalf("prompt %q", r.Text) }
	if r := state.userRightPrompt(&running); r.Text != "0 ms" { b.Fatalf("right prompt %q", r.Text) }
	b.Run("WatcherPerQuote", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			stop := watchInterrupts(&running)
			state.userPrompt(&running)
			stop()
			stop = watchInterrupts(&running)
			state.rightPrompt = state.userRightPrompt(&running)
			stop()
		}
	})
	b.Run("SharedWatcher", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			stop := watchInterrupts(&running)
			state.userPrompt(&running)
			state.rightPrompt = state.userRightPrompt(&running)
			stop()
		}
	})
	b.Run("NoWatcher", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			state.userPrompt(&running)
			state.rightPrompt = state.userRightPrompt(&running)
		}
	})
}
