package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	// editor_config: version and the config bytes; call_result: request and
	// status, with the rest in data.
	version uint32
	request uint32
	status  byte
}

type serverClient struct {
	t      *testing.T
	in     io.WriteCloser
	events chan serverEvent
	cmd    *exec.Cmd
	// The last editor config, and the version of each config seen in
	// order; the last prompt.
	config   serverEvent
	versions []uint32
	prompt   string
	title    string
	// The last prompt's styles: each one's offset and kind, and the
	// palette index or attribute it carries.
	promptStyles [][3]int
	// The directory blocks start in.
	cwd string
	// The last ops message's ops.
	ops []byte
}

// note keeps an editor config or a prompt.
func (c *serverClient) note(e serverEvent) bool {
	switch e.kind {
	case serverMsgEditorConfig:
		c.config = e
		c.versions = append(c.versions, e.version)
		return true
	case serverMsgPrompt:
		le := binary.LittleEndian
		b := e.data[1:]
		str := func() string {
			n := le.Uint32(b)
			v := string(b[4 : 4+n])
			b = b[4+n:]
			return v
		}
		c.title, c.prompt = str(), str()
		c.promptStyles = nil
		count := int(le.Uint16(b))
		b = b[2:]
		for range count {
			st := [3]int{int(le.Uint32(b)), int(b[4]), -1}
			b = b[5:]
			switch promptOpKind(st[1]) {
			case promptOpFgRgb, promptOpBgRgb:
				b = b[3:]
			case promptOpFgPalette, promptOpBgPalette, promptOpAttrOn, promptOpAttrOff:
				st[2] = int(b[0])
				b = b[1:]
			case promptOpLinkStart:
				str()
			}
			c.promptStyles = append(c.promptStyles, st)
		}
		return true
	}
	return false
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
	cmd.Env = append(cmd.Env, serverTestEnv(t)...)
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
	cwd, _ := os.Getwd()
	c := &serverClient{t: t, in: in, events: make(chan serverEvent, 1024), cmd: cmd, cwd: cwd}
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
		case serverMsgEditorConfig:
			e.version, e.data = le.Uint32(p), append([]byte(nil), p[4:]...)
		case serverMsgCallResult:
			e.request, e.status, e.data = le.Uint32(p), p[4], append([]byte(nil), p[5:]...)
		case serverMsgPrompt:
			e.data = append([]byte(nil), p...)
		case serverMsgOps:
			e.id, e.data = le.Uint64(p), append([]byte(nil), p[8:]...)
		case serverMsgFmView:
			e.data = append([]byte(nil), p...)
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
	for e = c.next(); (e.kind == serverMsgPtyOutput && e.id == 0) || c.note(e); e = c.next() {
	}
	if e.kind != serverMsgBlockStarted || e.id != id {
		c.t.Fatalf("got %+v, want block_started %d", e, id)
	}
	if string(e.data) != c.cwd || time.Since(time.Unix(0, e.unixNs)) > time.Minute {
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
		case c.note(e):
		case e.kind == serverMsgOps && e.id == id:
			c.ops = e.data
		default:
			c.t.Fatalf("inside block %d: %+v", id, e)
		}
	}
}

// The editor config as the test reads it.
type testConfig struct {
	bindings []testBinding
	abbrs    []testAbbr
	names    []string
}

type testBinding struct {
	keys  []KeyCode
	quote int // a bindingFunc's quote, or -1
	ops   int
	run   int // the quote of its first editRun or editRunOps, or -1
}

type testAbbr struct {
	regex bool
	word  string
	quote int // an abbrFunc's quote, or -1
	text  string
}

func decodeTestConfig(t *testing.T, b []byte) testConfig {
	t.Helper()
	le := binary.LittleEndian
	take := func(n int) []byte {
		if len(b) < n {
			t.Fatalf("config ends early")
		}
		v := b[:n]
		b = b[n:]
		return v
	}
	u8 := func() byte { return take(1)[0] }
	u16 := func() int { return int(le.Uint16(take(2))) }
	u32 := func() uint32 { return le.Uint32(take(4)) }
	str := func() string { return string(take(int(u32()))) }
	var c testConfig
	for range u32() {
		var tb testBinding
		u8()
		u32()
		u32()
		for range u8() {
			tb.keys = append(tb.keys, KeyCode(u32()))
		}
		tb.quote, tb.run = -1, -1
		if u8() == 1 {
			tb.quote = int(u32())
		} else {
			tb.ops = u16()
			for range tb.ops {
				switch editOpKind(u8()) {
				case opInsert, opMessage:
					str()
				case opSetLine:
					str()
					str()
				case opRun, opRunOps:
					if q := int(u32()); tb.run < 0 {
						tb.run = q
					}
				}
			}
		}
		c.bindings = append(c.bindings, tb)
	}
	for range u32() {
		ta := testAbbr{regex: u8() == 1, word: str(), quote: -1}
		if u8() == byte(abbrPosArgOf) {
			for range u16() {
				str()
			}
		}
		if u8() == 1 {
			ta.quote = int(u32())
		} else {
			ta.text = str()
		}
		str()
		c.abbrs = append(c.abbrs, ta)
	}
	for range u32() {
		c.names = append(c.names, str())
	}
	if len(b) != 0 {
		t.Fatalf("%d bytes after the config", len(b))
	}
	return c
}

// call sends a request and waits for its result, keeping configs.
func (c *serverClient) call(kind byte, request uint32, payload []byte) serverEvent {
	c.t.Helper()
	c.send(kind, append(binary.LittleEndian.AppendUint32(nil, request), payload...))
	for {
		e := c.next()
		if c.note(e) || (e.kind == serverMsgPtyOutput && e.id == 0) {
			continue
		}
		if e.kind != serverMsgCallResult || e.request != request {
			c.t.Fatalf("waiting for result %d: %+v", request, e)
		}
		return e
	}
}

func appendTestStr(b []byte, s string) []byte {
	return append(binary.LittleEndian.AppendUint32(b, uint32(len(s))), s...)
}

// A file manager view as the test reads it: shown, the entries' names and
// which has the cursor.
func decodeFmView(t *testing.T, b []byte) (bool, []string, int) {
	t.Helper()
	le := binary.LittleEndian
	shown := b[0]&1 != 0
	b = b[1:]
	str := func() string {
		n := le.Uint32(b)
		v := string(b[4 : 4+n])
		b = b[4+n:]
		return v
	}
	str()
	str()
	b = b[8:]
	n := int(le.Uint16(b))
	b = b[2:]
	var names []string
	cursor := -1
	for i := range n {
		flags := b[0]
		b = b[1:]
		names = append(names, str())
		str()
		str()
		if flags&2 != 0 {
			cursor = i
		}
	}
	return shown, names, cursor
}
