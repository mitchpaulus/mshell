//go:build linux || darwin

package main

import (
	"bytes"
	"slices"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serverTestEnv is what the engine under test needs beyond the harness's
// environment: nothing on Unix.
func serverTestEnv(t *testing.T) []string { return nil }

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

func TestServerEditorConfigAndCalls(t *testing.T) {
	c := startServer(t)
	le := binary.LittleEndian

	// The first config arrives before any block: the default keymap.
	c.run(1, "echo first")
	if out, code := c.block(1); out != "first\r\n" || code != 0 {
		t.Fatalf("first block: %q, %d", out, code)
	}
	if len(c.versions) != 1 || c.versions[0] != 1 {
		t.Fatalf("configs before any change: %v", c.versions)
	}
	defaults := decodeTestConfig(t, c.config.data)
	if len(defaults.bindings) < 30 {
		t.Fatalf("%d bindings in the default config", len(defaults.bindings))
	}

	// A line that adds a binding and abbreviations sends the new config
	// before its block_finished (block() fails on anything after it).
	add := `{keys: [r'x' keyCtrl], action: (drop ['hi' editInsert] just) bindingFunc} bindingAdd ` +
		`{abbr: 'gs' abbrWord, expansion: (drop 'git status' just) abbrFunc} abbrAdd ` +
		`{abbr: '\.\.+' abbrRegex, expansion: 'cd ..' abbrText} abbrAdd`
	c.run(2, add)
	if out, code := c.block(2); out != "" || code != 0 {
		t.Fatalf("adding: %q, %d", out, code)
	}
	if len(c.versions) != 2 || c.versions[1] != 2 {
		t.Fatalf("configs after adding: %v", c.versions)
	}
	config := decodeTestConfig(t, c.config.data)
	var binding testBinding
	for _, b := range config.bindings {
		if len(b.keys) == 1 && b.keys[0] == 'x'|KeyModCtrl {
			binding = b
		}
	}
	if binding.quote < 0 || len(config.abbrs) != 2 || config.abbrs[0].quote < 0 || !config.abbrs[1].regex {
		t.Fatalf("config after adding: %+v", config)
	}
	version := c.config.version

	// binding_call runs the quote on the EditState and returns its ops.
	callBinding := func(request, version uint32) serverEvent {
		p := le.AppendUint32(nil, version)
		p = le.AppendUint32(p, uint32(binding.quote))
		p = append(p, 0, 1)
		p = le.AppendUint32(p, uint32('x'|KeyModCtrl))
		p = appendTestStr(p, "echo ")
		return c.call(serverMsgBindingCall, request, append(p, "after"...))
	}
	r := callBinding(10, version)
	want := append(le.AppendUint16(nil, 1), byte(opInsert))
	want = appendTestStr(want, "hi")
	if r.status != callYes || !bytes.Equal(r.data, want) {
		t.Fatalf("binding_call: status %d, %q", r.status, r.data)
	}
	// An old version is refused: the frontend resolves the keys again.
	if r := callBinding(11, version-1); r.status != callStale {
		t.Fatalf("stale binding_call: status %d", r.status)
	}

	// abbr_call runs an abbrFunc on the word.
	p := le.AppendUint32(nil, version)
	p = le.AppendUint32(p, uint32(config.abbrs[0].quote))
	p = append(p, 0)
	p = appendTestStr(p, "gs")
	p = appendTestStr(p, "")
	if r := c.call(serverMsgAbbrCall, 12, append(p, "gs"...)); r.status != callYes || string(r.data) != "git status" {
		t.Fatalf("abbr_call: status %d, %q", r.status, r.data)
	}

	// regex_match uses Go's regexp; is_program the shell's rule.
	for i, tc := range []struct {
		word string
		want byte
	}{{"...", callYes}, {"x", callNo}} {
		p := le.AppendUint32(nil, version)
		p = le.AppendUint32(p, 1)
		if r := c.call(serverMsgRegexMatch, uint32(20+i), append(p, tc.word...)); r.status != tc.want {
			t.Fatalf("regex_match %q: status %d", tc.word, r.status)
		}
	}
	for i, tc := range []struct {
		word string
		want byte
	}{{"sh", callYes}, {"no-such-program-here", callNo}} {
		if r := c.call(serverMsgIsProgram, uint32(30+i), []byte(tc.word)); r.status != tc.want {
			t.Fatalf("is_program %q: status %d", tc.word, r.status)
		}
	}
	// line_kind: how the line runs, by runLine's rule.
	for i, tc := range []struct {
		line string
		want byte
	}{{"ls -la | wc", callYes}, {"  sh -c 'x", callYes}, {"1 2 +", callNo}, {"[ls]", callNo}, {"", callNo}, {"no-such-program-here x", callNo}} {
		if r := c.call(serverMsgLineKind, uint32(40+i), []byte(tc.line)); r.status != tc.want {
			t.Fatalf("line_kind %q: status %d", tc.line, r.status)
		}
	}
}

func TestServerPromptAndCompletion(t *testing.T) {
	c := startServer(t)
	le := binary.LittleEndian
	dir := t.TempDir()
	for _, f := range []string{"alpha.txt", "beta-one.txt", "beta-two.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The built-in prompt arrives at startup; setPrompt's after the line
	// that sets it, before its block_finished.
	c.run(1, "'"+dir+"' cd")
	if _, code := c.block(1); code != 0 || !strings.Contains(c.prompt, filepath.Base(dir)) {
		t.Fatalf("built-in prompt %q, exit %d", c.prompt, code)
	}
	c.cwd = dir
	c.run(2, `(drop ['title' setWindowTitle baseRed baseNormal setFgColorBase16 'my prompt> ' promptText resetStyle]) setPrompt`)
	out, code := c.block(2)
	if code != 0 {
		t.Fatalf("setPrompt: %q, exit %d", out, code)
	}
	// The prompt comes as text and typed styles, not escape sequences:
	// red (palette 1) from its start, reset at its end.
	want := [][3]int{{0, int(promptOpFgPalette), 1}, {len("my prompt> "), int(promptOpReset), -1}}
	if c.prompt != "my prompt> " || !slices.Equal(c.promptStyles, want) || c.title != "title" {
		t.Fatalf("prompt after setPrompt: %q, styles %v, title %q", c.prompt, c.promptStyles, c.title)
	}

	edit := func(request uint32, op editOpKind, line string, cursor int) (string, int, bool, []string) {
		t.Helper()
		p := append([]byte{byte(op)}, le.AppendUint32(nil, uint32(cursor))...)
		r := c.call(serverMsgEdit, request, append(p, line...))
		if r.status != callYes {
			t.Fatalf("edit %d: status %d, %q", op, r.status, r.data)
		}
		b := r.data
		cur, cycling := int(le.Uint32(b)), b[4] == 1
		n := int(le.Uint16(b[10:]))
		b = b[12:]
		var matches []string
		for range n {
			l := int(le.Uint32(b))
			matches = append(matches, string(b[4:4+l]))
			b = b[4+l:]
		}
		return string(b), cur, cycling, matches
	}
	// A unique prefix completes.
	if line, cur, cycling, _ := edit(10, opComplete, "cat al", 6); line != "cat alpha.txt" || cur != len(line) || cycling {
		t.Fatalf("complete al: %q at %d, cycling %v", line, cur, cycling)
	}
	// An ambiguous one inserts what they share and starts a cycle, which
	// Tab then steps through.
	line, cur, cycling, matches := edit(11, opComplete, "cat be", 6)
	if line != "cat beta-" || !cycling || len(matches) != 2 {
		t.Fatalf("complete be: %q at %d, cycling %v, %q", line, cur, cycling, matches)
	}
	if line, _, _, _ = edit(12, opComplete, line, cur); line != "cat beta-one.txt" {
		t.Fatalf("cycle: %q", line)
	}
	if _, _, cycling, _ = edit(13, opCompleteAccept, line, len(line)); cycling {
		t.Fatal("accept left the cycle on")
	}
}

func TestServerRunQuote(t *testing.T) {
	c := startServer(t)
	le := binary.LittleEndian
	c.run(1, `{keys: [r'x' keyAlt], action: [('ran' wl) editRun] bindingOps} bindingAdd `+
		`{keys: [r'y' keyAlt], action: [(drop ['done' editInsert] just) editRunOps] bindingOps} bindingAdd`)
	if out, code := c.block(1); code != 0 {
		t.Fatalf("adding: %q, %d", out, code)
	}
	config := decodeTestConfig(t, c.config.data)
	quoteOf := func(k KeyCode) int {
		for _, b := range config.bindings {
			if len(b.keys) == 1 && b.keys[0] == k {
				return b.run
			}
		}
		t.Fatalf("no binding of %v", k)
		return 0
	}
	runQuote := func(id uint64, quote int, withState bool) {
		p := le.AppendUint64(nil, id)
		p = le.AppendUint32(p, c.config.version)
		p = le.AppendUint32(p, uint32(quote))
		state := byte(0)
		if withState {
			state = 1
		}
		p = append(p, state)
		p = appendTestStr(p, "line ")
		c.send(serverMsgRunQuote, append(p, "after"...))
	}
	// editRun: the quote runs as a block, with the terminal.
	runQuote(2, quoteOf('x'|KeyModAlt), false)
	if out, code := c.block(2); out != "ran\r\n" || code != 0 {
		t.Fatalf("editRun: %q, %d", out, code)
	}
	// editRunOps: its ops come back before block_finished.
	runQuote(3, quoteOf('y'|KeyModAlt), true)
	if out, code := c.block(3); out != "" || code != 0 {
		t.Fatalf("editRunOps: %q, %d", out, code)
	}
	want := append(le.AppendUint16(nil, 1), byte(opInsert))
	if !bytes.Equal(c.ops, appendTestStr(want, "done")) {
		t.Fatalf("editRunOps ops: %q", c.ops)
	}
}

func TestServerFileManagerGUI(t *testing.T) {
	c := startServer(t)
	le := binary.LittleEndian
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	view := func() (bool, []string, int) {
		t.Helper()
		for {
			e := c.next()
			if e.kind == serverMsgFmView {
				return decodeFmView(t, e.data)
			}
			if e.kind == serverMsgBlockFinished {
				t.Fatalf("block finished before a view")
			}
		}
	}
	key := func(k KeyCode) {
		c.send(serverMsgFmInput, append([]byte{fmInputKey}, append(le.AppendUint32(nil, uint32(k)), 0, 0, 0, 0)...))
	}
	c.run(1, "'"+dir+"' mshFileManager")
	c.started(1)
	// The file manager draws to the GUI, not the PTY.
	shown, names, cursor := view()
	if !shown || len(names) != 2 || names[0] != "a.txt" || cursor != 0 {
		t.Fatalf("first view: shown %v, %q, cursor %d", shown, names, cursor)
	}
	c.send(serverMsgFmInput, append([]byte{fmInputResize}, append(le.AppendUint16(nil, 80), le.AppendUint16(nil, 20)...)...))
	view()
	// Its keymap runs in the engine: j moves down, q quits.
	key('j')
	for {
		if shown, _, cursor = view(); cursor == 1 {
			break
		}
	}
	key('q')
	for shown {
		shown, _, _ = view()
	}
	if _, code := c.rest(1); code != 0 {
		t.Fatalf("file manager block: exit %d", code)
	}
}

// The mouse in the file manager: entries are pointed at, opened, toggled
// and scrolled by what the view showed, and stale ones do nothing.
func TestServerFileManagerGUIMouse(t *testing.T) {
	c := startServer(t)
	le := binary.LittleEndian
	dir := t.TempDir()
	for _, d := range []string{"sub/inner", "sub/x"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 30 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	type view struct {
		names  []string
		cursor int
	}
	next := func() view {
		t.Helper()
		for {
			e := c.next()
			if e.kind == serverMsgFmView {
				_, names, cursor := decodeFmView(t, e.data)
				return view{names, cursor}
			}
			if e.kind == serverMsgBlockFinished {
				t.Fatalf("block finished before a view")
			}
		}
	}
	// The views until one is as wanted: previews loading send more.
	until := func(what string, ok func(view) bool) {
		t.Helper()
		for v := next(); !ok(v); v = next() {
			t.Logf("%s: %q, cursor %d", what, v.names, v.cursor)
		}
	}
	mouse := func(action byte, n int, name, child string) {
		p := append([]byte{fmInputMouse, action}, le.AppendUint32(nil, uint32(int32(n)))...)
		p = le.AppendUint32(p, uint32(len(name)))
		c.send(serverMsgFmInput, append(append(p, name...), child...))
	}
	c.run(1, "'"+dir+"' mshFileManager")
	c.started(1)
	next()
	// 10 rows: a header, 8 entries, the summary.
	c.send(serverMsgFmInput, append([]byte{fmInputResize}, append(le.AppendUint16(nil, 80), le.AppendUint16(nil, 10)...)...))
	until("resize", func(v view) bool { return len(v.names) == 8 && v.names[0] == "sub" && v.cursor == 0 })
	mouse(fmMousePoint, 3, "f02", "")
	until("point", func(v view) bool { return v.cursor == 3 })
	// A name that is not the entry's: the view was stale, and the cursor
	// stays, so j takes it to 4.
	mouse(fmMousePoint, 5, "f02", "")
	c.send(serverMsgFmInput, append([]byte{fmInputKey}, append(le.AppendUint32(nil, uint32('j')), 0, 0, 0, 0)...))
	until("stale point, j", func(v view) bool { return v.cursor == 4 || v.cursor == 6 })
	// Scrolling keeps the cursor in view; past the end it stops.
	mouse(fmMouseScroll, 6, "", "")
	until("scroll", func(v view) bool { return v.names[0] == "f05" && v.cursor == 0 })
	mouse(fmMouseScroll, 100, "", "")
	until("scroll to the end", func(v view) bool { return v.names[0] == "f22" && v.names[7] == "f29" && v.cursor == 0 })
	mouse(fmMouseScroll, -100, "", "")
	until("scroll to the top", func(v view) bool { return v.names[0] == "sub" && v.cursor == 7 })
	// Into: the cursor's directory opens, at the child clicked in its
	// preview; only for the entry under the cursor.
	mouse(fmMousePoint, 0, "sub", "")
	until("point at sub", func(v view) bool { return v.cursor == 0 })
	mouse(fmMouseInto, 1, "f00", "x")
	mouse(fmMouseInto, 0, "sub", "x")
	until("into", func(v view) bool { return len(v.names) == 2 && v.names[0] == "inner" && v.cursor == 1 })
	// Parent, and open, which enters a directory.
	mouse(fmMouseParent, 0, "", "")
	until("parent", func(v view) bool { return v.names[0] == "sub" && v.cursor == 0 })
	mouse(fmMouseOpen, 0, "sub", "")
	until("open", func(v view) bool { return len(v.names) == 2 && v.names[0] == "inner" && v.cursor == 0 })
	// Toggle selects the entry, under the cursor: the clipboard status
	// counts it.
	mouse(fmMouseToggle, 1, "x", "")
	for {
		e := c.next()
		if e.kind != serverMsgFmView {
			continue
		}
		b := e.data[1:]
		b = b[4+le.Uint32(b):]
		clip := string(b[4 : 4+le.Uint32(b)])
		if _, _, cursor := decodeFmView(t, e.data); cursor == 1 && strings.Contains(clip, "SELECTED 1") {
			break
		}
	}
	c.send(serverMsgFmInput, []byte{fmInputClose})
	for {
		e := c.next()
		if e.kind == serverMsgFmView && e.data[0]&1 == 0 {
			break
		}
	}
	if _, code := c.rest(1); code != 0 {
		t.Fatalf("file manager block: exit %d", code)
	}
}

func TestServerHistory(t *testing.T) {
	c := startServer(t)
	le := binary.LittleEndian
	c.run(1, "echo one-two")
	if out, code := c.block(1); out != "one-two\r\n" || code != 0 {
		t.Fatalf("echo: %q, %d", out, code)
	}
	// The names highlighting underlines: builtins and definitions.
	config := decodeTestConfig(t, c.config.data)
	if !slices.Contains(config.names, "wl") || !slices.Contains(config.names, "map") {
		t.Fatalf("config names: %d, without wl or map", len(config.names))
	}
	// The line is history now: suggested for its start.
	r := c.call(serverMsgLineKind, 1, []byte("echo o"))
	if r.status != callYes || string(r.data) != "ne-two" {
		t.Fatalf("line_info: status %d, suggestion %q", r.status, r.data)
	}
	edit := func(request uint32, op editOpKind, line string, cursor int) (string, int) {
		t.Helper()
		p := append([]byte{byte(op)}, le.AppendUint32(nil, uint32(cursor))...)
		r := c.call(serverMsgEdit, request, append(p, line...))
		if r.status != callYes {
			t.Fatalf("edit %d: status %d, %q", op, r.status, r.data)
		}
		b := r.data
		n := int(le.Uint16(b[10:]))
		b = b[12:]
		for range n {
			b = b[4+le.Uint32(b):]
		}
		return string(b), int(le.Uint32(r.data))
	}
	// Up brings it back; Ctrl-F at the end of a line takes the suggestion.
	if line, cur := edit(2, opHistoryPrev, "", 0); line != "echo one-two" || cur != len(line) {
		t.Fatalf("history prev: %q at %d", line, cur)
	}
	if line, _ := edit(3, opForward, "echo o", 6); line != "echo one-two" {
		t.Fatalf("forward: %q", line)
	}
}

// Input is kept only for a block, up to serverInputMax not yet written, and
// what is queued when the block ends is discarded.
func TestServerInputBounds(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := newServerInput(nil)
	in.add([]byte("outside a block"))
	in.blockQueued()
	in.add(bytes.Repeat([]byte("a"), serverInputMax-4))
	in.add([]byte("over the bound"))
	in.add([]byte("fits"))
	// Not running yet: the queue is full, and the block ends.
	queuedThenDone := newServerInput(nil)
	queuedThenDone.blockQueued()
	queuedThenDone.add([]byte("left unread"))
	queuedThenDone.blockDone()
	queuedThenDone.add([]byte("after the block"))
	if queuedThenDone.bytes != 0 || len(queuedThenDone.queue) != 0 {
		t.Fatalf("after the block: %d bytes queued", queuedThenDone.bytes)
	}

	go in.run(w)
	got := make([]byte, serverInputMax)
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:serverInputMax-4], bytes.Repeat([]byte("a"), serverInputMax-4)) || string(got[serverInputMax-4:]) != "fits" {
		t.Fatalf("written: ... %q", got[serverInputMax-20:])
	}
	r.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, err := r.Read(make([]byte, 64)); n != 0 || !os.IsTimeout(err) {
		t.Fatalf("more written: %d bytes, %v", n, err)
	}
}
