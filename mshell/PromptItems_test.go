package main

import (
	"bytes"
	"io"
	"strings"
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
	))
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
	))
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
		_, err := renderPrompt(c.obj)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one about %q", c.name, err, c.want)
		}
	}
}

func TestRenderPromptTitleAndHyperlink(t *testing.T) {
	r, err := renderPrompt(promptList(
		promptItem("setWindowTitle", MShellString{"t\x1b\\"}),
		promptItem("promptHyperlink", MShellString{"https://x.test/a%20b"}, MShellString{"link"}),
	))
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
	))
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
