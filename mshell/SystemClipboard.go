package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func writeSystemClipboard(text string) error {
	candidates := systemClipboardCommands()
	if len(candidates) == 0 {
		return fmt.Errorf("no clipboard tool available for this platform")
	}

	var lastErr error
	for _, c := range candidates {
		if _, err := exec.LookPath(c.cmd); err != nil {
			lastErr = err
			continue
		}
		proc := exec.Command(c.cmd, c.args...)
		proc.Stdin = strings.NewReader(text)
		if err := proc.Run(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		return fmt.Errorf("no clipboard tool found in PATH")
	}
	return lastErr
}

type clipboardCmd struct {
	cmd  string
	args []string
}

func systemClipboardCommands() []clipboardCmd {
	switch runtime.GOOS {
	case "darwin":
		return []clipboardCmd{{cmd: "pbcopy"}}
	case "windows":
		return []clipboardCmd{{cmd: "clip"}}
	default:
		return []clipboardCmd{
			{cmd: "wl-copy"},
			{cmd: "xclip", args: []string{"-selection", "clipboard"}},
			{cmd: "xsel", args: []string{"--clipboard", "--input"}},
		}
	}
}
