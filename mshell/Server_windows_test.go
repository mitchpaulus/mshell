//go:build windows

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// serverTestEnv names the ConPTY package's conpty.dll for the engine under
// test: MSH_TEST_CONPTY, else the copy msh-gui/fetch-conpty.msh unpacks.
// Without one the test is skipped.
func serverTestEnv(t *testing.T) []string {
	t.Helper()
	path := os.Getenv("MSH_TEST_CONPTY")
	if path == "" {
		path = filepath.Join("..", "gui", "third_party", "conpty-1.25.260930003", "x64", "conpty.dll")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("no conpty.dll at %s: run `msh msh-gui/fetch-conpty.msh` in gui/, or set MSH_TEST_CONPTY", abs)
	}
	return []string{serverConptyEnv + "=" + abs}
}

var testEscapes = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\a\x1b]*(\a|\x1b\\)|[@-Z\\-_])`)

// visible is a block's output without escape sequences and carriage
// returns: the console host adds mode changes of its own around programs.
func visible(out string) string {
	return strings.ReplaceAll(testEscapes.ReplaceAllString(out, ""), "\r", "")
}

func TestServerWindowsRunsBlocks(t *testing.T) {
	c := startServer(t)
	c.resize(100, 30)

	c.run(1, "cmd /c echo hi")
	if out, code := c.block(1); visible(out) != "hi\n" || code != 0 {
		t.Fatalf("echo: %q, exit %d", out, code)
	}

	// The pseudoconsole is the program's console, at the size the frontend
	// set. (mode.com by its full path: TestServerWindowsPath is for PATH.)
	c.run(2, `['cmd' '/c' '%SystemRoot%\System32\mode.com' 'con']!`)
	out, code := c.block(2)
	if !strings.Contains(visible(out), "Columns:        100") || !strings.Contains(visible(out), "Lines:          30") || code != 0 {
		t.Fatalf("mode con: %q, exit %d", out, code)
	}

	// A list, not a command line: `exit` is a builtin, so msh would take
	// "cmd /c exit 3" for mshell.
	c.run(3, `['cmd' '/c' 'exit 3'];`)
	if out, code := c.block(3); visible(out) != "" || code != 3 {
		t.Fatalf("exit 3: %q, exit %d", out, code)
	}

	c.run(4, "[")
	if out, code := c.block(4); !strings.Contains(out, "Error parsing input") || code != 1 {
		t.Fatalf("parse error: %q, exit %d", out, code)
	}

	// mshell values persist between blocks; the engine's own output goes
	// through the pseudoconsole, in order with its programs'.
	c.run(5, "1 2 + x!")
	if out, code := c.block(5); visible(out) != "" || code != 0 {
		t.Fatalf("assignment: %q, exit %d", out, code)
	}
	c.run(6, `"before" wl ['cmd' '/c' 'echo middle']! @x wl`)
	if out, code := c.block(6); visible(out) != "before\nmiddle\n3\n" || code != 0 {
		t.Fatalf("mixed: %q, exit %d", out, code)
	}

	c.run(7, "")
	if out, code := c.block(7); visible(out) != "" || code != 0 {
		t.Fatalf("empty: %q, exit %d", out, code)
	}
}

func TestServerWindowsInput(t *testing.T) {
	c := startServer(t)

	// Keys go to the console: it echoes the line, and the program reads it.
	c.run(1, `cmd /c "set /p v=? & call echo got %v%"`)
	c.started(1)
	c.input("typed\r")
	// The prompt is "? ": the space before & is part of it.
	if out, code := c.rest(1); !strings.Contains(visible(out), "? typed\ngot typed\n") || code != 0 {
		t.Fatalf("set /p: %q, exit %d", out, code)
	}

	// Ctrl-C is just a byte: the console sends CTRL_C_EVENT to the program,
	// and the engine goes on to the next block.
	c.run(2, "ping -n 30 127.0.0.1")
	c.started(2)
	time.Sleep(time.Second)
	start := time.Now()
	c.input("\x03")
	out, _ := c.rest(2)
	if time.Since(start) > 10*time.Second {
		t.Fatalf("Ctrl-C: %q after %v", out, time.Since(start))
	}
	c.run(3, `"after" wl`)
	if out, code := c.block(3); visible(out) != "after\n" || code != 0 {
		t.Fatalf("after Ctrl-C: %q, exit %d", out, code)
	}
}

func TestServerWindowsExit(t *testing.T) {
	c := startServer(t)
	c.run(1, "7 exit")
	if out, code := c.block(1); visible(out) != "" || code != 7 {
		t.Fatalf("exit: %q, exit %d", out, code)
	}
	if e := c.next(); e.kind != serverMsgExit || e.code != 7 {
		t.Fatalf("after exit: %+v", e)
	}
	select {
	case _, ok := <-c.events:
		if ok {
			t.Fatal("message after exit")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("engine still running after exit")
	}
}

// Programs the engine runs get its environment, as they do from msh.
func TestServerWindowsEnvironment(t *testing.T) {
	c := startServer(t)
	c.run(1, `"from the engine" $MSH_TEST_VAR!`)
	if out, code := c.block(1); visible(out) != "" || code != 0 {
		t.Fatalf("set: %q, exit %d", out, code)
	}
	c.run(2, `['cmd' '/c' 'echo %MSH_TEST_VAR%']!`)
	if out, code := c.block(2); visible(out) != "from the engine\n" || code != 0 {
		t.Fatalf("cmd's MSH_TEST_VAR: %q, exit %d", out, code)
	}
}

// The pseudoconsole's startup handshake is the engine's: nothing arrives as
// output outside a block, which msh-gui would show as a card of its own.
func TestServerWindowsNoStartupOutput(t *testing.T) {
	c := startServer(t)
	c.run(1, "")
	for e := c.next(); e.kind != serverMsgBlockStarted; e = c.next() {
		if e.kind == serverMsgPtyOutput {
			t.Fatalf("output outside any block before the first block: %q", e.data)
		}
		c.note(e)
	}
	if out, code := c.rest(1); visible(out) != "" || code != 0 {
		t.Fatalf("empty: %q, exit %d", out, code)
	}
}
