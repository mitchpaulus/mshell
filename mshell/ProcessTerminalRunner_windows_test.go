package main

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
	"unicode/utf16"
)

func decodeWindowsEnvironmentBlock(block []uint16) []string {
	entries := make([]string, 0)
	start := 0
	for index, value := range block {
		if value != 0 {
			continue
		}
		if index == start {
			break
		}
		entries = append(entries, string(utf16.Decode(block[start:index])))
		start = index + 1
	}
	return entries
}

func TestWindowsEnvironmentBlockIsSortedAndDoubleTerminated(t *testing.T) {
	block, err := windowsEnvironmentBlock([]string{"z=last", "A=first", "m=middle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(block) < 2 || block[len(block)-1] != 0 || block[len(block)-2] != 0 {
		t.Fatalf("environment block is not double-NUL terminated: %v", block)
	}
	want := []string{"A=first", "m=middle", "z=last"}
	if got := decodeWindowsEnvironmentBlock(block); !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded environment = %v, want %v", got, want)
	}
}

func TestWindowsEnvironmentBlockRejectsNUL(t *testing.T) {
	if _, err := windowsEnvironmentBlock([]string{"BAD=value\x00tail"}); err == nil {
		t.Fatal("environment entry containing NUL was accepted")
	}
}

// TestWindowsConPTYLifecycle runs the potentially blocking part in a helper
// process.  CommandContext kills that host on timeout; closing the host's Job
// Object handle then kills the entire nested process tree.
func TestWindowsConPTYLifecycle(t *testing.T) {
	if os.Getenv("MSHELL_CONPTY_TEST_HELPER") == "1" {
		cmd := exec.Command("cmd.exe", "/D", "/C", "exit 7")
		cmd.Env = os.Environ()
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		stdio := resolveProcessStdio(cmd.Stdin, cmd.Stdout, cmd.Stderr)
		handled, exitCode, err := runIsolatedTerminalCommand(cmd, stdio)
		if err != nil || !handled || exitCode != 7 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	if !IsTerminal(int(os.Stdin.Fd())) || !IsTerminal(int(os.Stdout.Fd())) || !IsTerminal(int(os.Stderr.Fd())) {
		t.Skip("native ConPTY lifecycle test requires an attached Windows console")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	helper := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsConPTYLifecycle$")
	helper.Env = append(os.Environ(), "MSHELL_CONPTY_TEST_HELPER=1")
	helper.Stdin = os.Stdin
	helper.Stdout = os.Stdout
	helper.Stderr = os.Stderr
	if err := helper.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("ConPTY helper exceeded hard deadline: %v", ctx.Err())
		}
		t.Fatalf("ConPTY helper failed: %v", err)
	}
}
