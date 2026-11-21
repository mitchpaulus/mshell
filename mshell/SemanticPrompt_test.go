package main

import (
	"io"
	"testing"
)

func TestLineExitStatus(t *testing.T) {
	for _, tt := range []struct {
		name     string
		ok       bool
		exitCode int
		exitSet  bool
		want     int
	}{
		{"msh line, no process", true, 0, false, 0},
		{"process succeeded", true, 0, true, 0},
		{"process failed, line went on", true, 2, true, 2},
		{"line failed, no process", false, 0, false, 1},
		{"line failed after a process succeeded", false, 0, true, 1},
		{"line failed on a process's status", false, 127, true, 127},
	} {
		if got := lineExitStatus(tt.ok, tt.exitCode, tt.exitSet); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}
	if got := osc133CommandEnd(127); got != "\033]133;D;127\033\\" {
		t.Errorf("command end %q", got)
	}
}

// The prompt is marked when it is painted and again when a resize repaints
// it in place, so the terminal's marks follow the prompt's new rows.
func TestPromptMarksAroundPromptAndOnResize(t *testing.T) {
	read := func() (TerminalToken, error) { t.Fatal("ASCII prompt read input"); return nil, io.EOF }
	screen := newCommandScreen(t, 5, 40)
	screen.Write([]byte("earlier output\r\n"))
	state := &TermState{numRows: 5, numCols: 40, resizeReflow: true}
	if err := state.paintPrompt(screen, read, "~ (0)>\n:: "); err != nil { t.Fatal(err) }
	marks := []string{"133;A;redraw=0", "133;B"}
	if len(screen.oscs) != 2 || screen.oscs[0] != marks[0] || screen.oscs[1] != marks[1] {
		t.Fatalf("marks %q, want %q", screen.oscs, marks)
	}
	// The marks draw nothing: the prompt is where it was without them.
	if screen.line(1)[:6] != "~ (0)>" || screen.row != 2 || screen.col != 3 {
		t.Fatalf("prompt at %d,%d: %q", screen.row, screen.col, screen.line(1))
	}
	if err := state.redrawPromptAfterResize(screen, read, 30, 5); err != nil { t.Fatal(err) }
	if len(screen.oscs) != 4 || screen.oscs[2] != marks[0] || screen.oscs[3] != marks[1] {
		t.Fatalf("marks after resize %q", screen.oscs)
	}
}
