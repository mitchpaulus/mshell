package main

import "strconv"

// Semantic prompt marks (OSC 133, from FinalTerm): they tell the terminal
// where each prompt, the command typed at it, and the command's output begin,
// and how the command ended. Terminals that know them (Ghostty, kitty,
// WezTerm, iTerm2, Windows Terminal, foot) can then jump between prompts and
// select a command's output; the rest ignore them like any unknown OSC.
//
// Spec: https://gitlab.freedesktop.org/Per_Bothner/specifications/blob/master/proposals/semantic-prompts.md

// osc133PromptStart starts a prompt. It is written at column one, after the
// omitted-newline marker has made sure of a fresh line.
//
// redraw=0 (kitty, Ghostty): the shell doesn't repaint its prompt when the
// window is resized, so the terminal must not clear it. The shell redraws it
// in place only when the next key arrives (redrawPromptAfterResize), from the
// rows it painted; a terminal that had already cleared them would leave the
// prompt missing until then.
const osc133PromptStart = "\033]133;A;redraw=0\033\\"

// osc133InputStart ends the prompt; what follows is the command being typed.
const osc133InputStart = "\033]133;B\033\\"

// osc133OutputStart ends the command line; what follows is its output.
const osc133OutputStart = "\033]133;C\033\\"

// osc133CommandEnd ends a command's output with its exit status.
func osc133CommandEnd(code int) string {
	return "\033]133;D;" + strconv.Itoa(code) + "\033\\"
}

// lineExitStatus is the status reported for the line that just ran: the
// exit code of the last process it ran, or 1 if the line failed without one.
func lineExitStatus(ok bool, exitCode int, exitSet bool) int {
	code := 0
	if exitSet {
		code = exitCode
	}
	if !ok && code == 0 {
		code = 1
	}
	return code
}
