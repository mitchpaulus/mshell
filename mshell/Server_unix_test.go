//go:build linux || darwin

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const serverHelperEnv = "MSH_TEST_SERVER_HELPER"

// TestServerHelper is the engine under test: the test binary run again as
// msh server.
func TestServerHelper(t *testing.T) {
	if os.Getenv(serverHelperEnv) == "" {
		t.Skip("run by TestServer* as the engine")
	}
	os.Exit(runServer())
}

type serverEvent struct {
	kind   byte
	id     uint64
	data   []byte // pty_output bytes, block_started cwd
	code   int32
	pid    uint32
	ver    uint32
	durNs  uint64
	unixNs int64
}

type serverClient struct {
	t      *testing.T
	in     io.WriteCloser
	events chan serverEvent
	cmd    *exec.Cmd
}

func startServer(t *testing.T) *serverClient {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	initFile := filepath.Join(t.TempDir(), "init.msh")
	if err := os.WriteFile(initFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestServerHelper$")
	cmd.Env = append(os.Environ(), serverHelperEnv+"=1",
		"MSHSTDLIB="+filepath.Join(root, "lib", "std.msh"), "MSHINIT="+initFile,
		"XDG_DATA_HOME="+t.TempDir(), "TERM=xterm-256color")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &serverClient{t: t, in: in, events: make(chan serverEvent, 1024), cmd: cmd}
	go c.read(out)
	t.Cleanup(func() {
		in.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
	})
	hello := c.next()
	if hello.kind != serverMsgHello || hello.ver != serverProtocolVersion || hello.pid == 0 {
		t.Fatalf("first message %+v, want hello", hello)
	}
	return c
}

func (c *serverClient) read(out io.Reader) {
	defer close(c.events)
	r := bufio.NewReader(out)
	var buf []byte
	for {
		kind, p, b, err := readServerFrame(r, buf)
		buf = b
		if err != nil {
			return
		}
		e := serverEvent{kind: kind}
		le := binary.LittleEndian
		switch kind {
		case serverMsgHello:
			e.ver, e.pid = le.Uint32(p), le.Uint32(p[4:])
		case serverMsgBlockStarted:
			e.id, e.unixNs, e.data = le.Uint64(p), int64(le.Uint64(p[8:])), append([]byte(nil), p[16:]...)
		case serverMsgPtyOutput:
			e.id, e.data = le.Uint64(p), append([]byte(nil), p[8:]...)
		case serverMsgBlockFinished:
			e.id, e.code, e.durNs = le.Uint64(p), int32(le.Uint32(p[8:])), le.Uint64(p[12:])
		case serverMsgExit:
			e.code = int32(le.Uint32(p))
		}
		c.events <- e
	}
}

func (c *serverClient) next() serverEvent {
	c.t.Helper()
	select {
	case e, ok := <-c.events:
		if !ok {
			c.t.Fatal("engine closed the protocol")
		}
		return e
	case <-time.After(20 * time.Second):
		c.t.Fatal("no message from the engine")
	}
	return serverEvent{}
}

func (c *serverClient) send(kind byte, payload []byte) {
	c.t.Helper()
	frame := binary.LittleEndian.AppendUint32(nil, uint32(1+len(payload)))
	frame = append(frame, kind)
	frame = append(frame, payload...)
	if _, err := c.in.Write(frame); err != nil {
		c.t.Fatal(err)
	}
}

func (c *serverClient) run(id uint64, source string) {
	c.send(serverMsgRun, append(binary.LittleEndian.AppendUint64(nil, id), source...))
}

func (c *serverClient) input(b string) { c.send(serverMsgInput, []byte(b)) }

func (c *serverClient) resize(cols, rows uint16) {
	p := binary.LittleEndian.AppendUint16(nil, cols)
	p = binary.LittleEndian.AppendUint16(p, rows)
	p = binary.LittleEndian.AppendUint16(p, cols*8)
	c.send(serverMsgResize, binary.LittleEndian.AppendUint16(p, rows*16))
}

// block reads a whole block: output outside it comes before block_started
// (startup messages, id 0); everything between is tagged with the block.
func (c *serverClient) block(id uint64) (output string, code int32) {
	c.t.Helper()
	c.started(id)
	return c.rest(id)
}

// started waits for the block to start, skipping output outside any block.
func (c *serverClient) started(id uint64) {
	c.t.Helper()
	var e serverEvent
	for e = c.next(); e.kind == serverMsgPtyOutput && e.id == 0; e = c.next() {
	}
	if e.kind != serverMsgBlockStarted || e.id != id {
		c.t.Fatalf("got %+v, want block_started %d", e, id)
	}
	cwd, _ := os.Getwd()
	if string(e.data) != cwd || time.Since(time.Unix(0, e.unixNs)) > time.Minute {
		c.t.Fatalf("block_started cwd %q at %d", e.data, e.unixNs)
	}
}

// rest collects a started block's output until it finishes.
func (c *serverClient) rest(id uint64) (output string, code int32) {
	c.t.Helper()
	var e serverEvent
	var out bytes.Buffer
	for {
		e = c.next()
		switch {
		case e.kind == serverMsgPtyOutput && e.id == id:
			out.Write(e.data)
		case e.kind == serverMsgBlockFinished && e.id == id:
			return out.String(), e.code
		default:
			c.t.Fatalf("inside block %d: %+v", id, e)
		}
	}
}

func TestServerRunsBlocksOnItsPTY(t *testing.T) {
	c := startServer(t)
	c.resize(100, 30)

	c.run(1, "echo hi")
	if out, code := c.block(1); out != "hi\r\n" || code != 0 {
		t.Fatalf("echo: %q, exit %d", out, code)
	}

	// The PTY is the program's terminal, at the size the frontend set.
	c.run(2, "stty size")
	if out, code := c.block(2); out != "30 100\r\n" || code != 0 {
		t.Fatalf("stty size: %q, exit %d", out, code)
	}

	c.run(3, "sh -c 'exit 3'")
	if out, code := c.block(3); out != "" || code != 3 {
		t.Fatalf("exit 3: %q, exit %d", out, code)
	}

	c.run(4, "[")
	if out, code := c.block(4); !strings.Contains(out, "Error parsing input") || code != 1 {
		t.Fatalf("parse error: %q, exit %d", out, code)
	}

	// mshell values persist between blocks, as in one interactive session.
	c.run(5, "1 2 + x!")
	if out, code := c.block(5); out != "" || code != 0 {
		t.Fatalf("assignment: %q, exit %d", out, code)
	}
	c.run(6, "@x wl")
	if out, code := c.block(6); out != "3\r\n" || code != 0 {
		t.Fatalf("variable: %q, exit %d", out, code)
	}

	// An empty line is a block with no output.
	c.run(7, "")
	if out, code := c.block(7); out != "" || code != 0 {
		t.Fatalf("empty: %q, exit %d", out, code)
	}
}

func TestServerInputReachesTheProgram(t *testing.T) {
	c := startServer(t)

	// Keys go to the PTY: the line discipline echoes them, and the program
	// reads the line. A frontend sends keys once the block has started;
	// keys sent earlier are echoed outside it.
	c.run(1, "head -n1")
	c.started(1)
	c.input("typed\r")
	if out, code := c.rest(1); out != "typed\r\ntyped\r\n" || code != 0 {
		t.Fatalf("head: %q, exit %d", out, code)
	}

	// Ctrl-C is just a byte: the kernel signals the foreground job, not the
	// engine, which goes on to the next block.
	c.run(2, "sleep 30")
	c.started(2)
	// sleep must be running and in the foreground before the key.
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	c.input("\x03")
	out, code := c.rest(2)
	// Killed by SIGINT, which msh reports as -(128 + 2).
	if code != -130 || time.Since(start) > 10*time.Second {
		t.Fatalf("Ctrl-C: %q, exit %d after %v", out, code, time.Since(start))
	}
	c.run(3, "echo after")
	if out, code := c.block(3); out != "after\r\n" || code != 0 {
		t.Fatalf("after Ctrl-C: %q, exit %d", out, code)
	}
}

// Output that looks like the start of a marker, and a run split across reads,
// stay in the block's output.
func TestServerMarksIgnoreLookalikes(t *testing.T) {
	m, err := newServerMarks()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	var marks []string
	emit := func(b []byte) error { out.Write(b); return nil }
	mark := func(kind byte, id uint64) error { marks = append(marks, fmt.Sprintf("%c%d|%s", kind, id, out.String())); return nil }
	stream := []byte("a\x1b]6973;other;S1\abc")
	stream = append(stream, m.marker('S', 7)...)
	stream = append(stream, "out\x1b]0;title\a"...)
	stream = append(stream, m.marker('E', 7)...)
	stream = append(stream, "tail\x1b"...)
	// Every split point, one byte at a time.
	for i := range stream {
		if err := m.feed(stream[i:i+1], emit, mark); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"S7|a\x1b]6973;other;S1\abc", "E7|a\x1b]6973;other;S1\abcout\x1b]0;title\a"}
	if len(marks) != 2 || marks[0] != want[0] || marks[1] != want[1] {
		t.Fatalf("marks %q, want %q", marks, want)
	}
	// A lone ESC at the end could start a marker, so it waits for more.
	if out.String() != "a\x1b]6973;other;S1\abcout\x1b]0;title\atail" {
		t.Fatalf("output %q", out.String())
	}
	if err := m.feed([]byte("x"), emit, mark); err != nil || !strings.HasSuffix(out.String(), "tail\x1bx") {
		t.Fatalf("held ESC: %q, %v", out.String(), err)
	}
}

func TestServerExit(t *testing.T) {
	c := startServer(t)
	c.run(1, "7 exit")
	if out, code := c.block(1); out != "" || code != 7 {
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
