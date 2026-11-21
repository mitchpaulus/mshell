package main

// msh server: the engine of the GUI frontend (ai/gui-terminal.typ, "Architecture").
//
// One engine runs one session on one terminal, the pane's: a PTY on Unix
// (Server_unix.go), a pseudoconsole on Windows (Server_windows.go). At
// startup it keeps its stdin and stdout for the protocol, then makes the
// terminal its stdin, stdout and stderr (and on Unix its controlling
// terminal). The evaluator, job control and every child process then see
// exactly what interactive msh sees on a terminal, and nothing in the
// evaluator changes.
//
// The protocol, both directions: frames of
//
//	u32 length (of what follows), u8 type, payload
//
// little-endian, with any variable-length bytes last in the payload, running
// to the end of the frame.
//
// Frontend to engine:
//
//	0x01 run        u64 block id (non-zero), source
//	0x02 input      bytes for the PTY: keys for the running program; Ctrl-C is just 0x03.
//	                Dropped when no block is running or asked to run, and
//	                past 1 MB the program hasn't read; what a block's
//	                programs leave unread is discarded when it ends.
//	0x03 resize     u16 cols, u16 rows, u16 width px, u16 height px
//
// Engine to frontend:
//
//	0x81 hello          u32 protocol version, u32 pid
//	0x82 block_started  u64 block id, i64 start (unix ns), cwd
//	0x83 pty_output     u64 block id (0: outside any block), bytes
//	0x84 block_finished u64 block id, i32 exit code, u64 duration ns
//	                    (the code as msh reports it: the last process's
//	                    status, -(128 + N) if signal N killed it, 1 if the
//	                    line failed without one)
//	0x85 exit           i32 exit code; the engine exits after it
//
// Blocks run one at a time, in the order their run messages arrive.
// block_started, the block's pty_output and block_finished arrive in that
// order: the engine writes a private marker into the PTY before and after
// each block, and its PTY reader turns each marker into the event at that
// exact point in the stream. Marking the stream itself is what makes this
// exact: the kernel hands PTY output to the reader asynchronously, so "the
// line finished" can't be matched to "the bytes so far" any other way.
// Output that arrives outside a block (startup messages, background jobs)
// has block id 0.
//
// When the frontend closes the protocol, the engine exits; the PTY is hung
// up, and the foreground job gets SIGHUP, as when a terminal window closes
// (on Windows, closing the pseudoconsole closes the programs attached to it).

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const serverProtocolVersion = 1

const (
	serverMsgRun    = 0x01
	serverMsgInput  = 0x02
	serverMsgResize = 0x03

	serverMsgHello         = 0x81
	serverMsgBlockStarted  = 0x82
	serverMsgPtyOutput     = 0x83
	serverMsgBlockFinished = 0x84
	serverMsgExit          = 0x85
)

// serverMaxFrame bounds a frame from the frontend; a larger length means the
// stream is not this protocol.
const serverMaxFrame = 64 << 20

// serverTerminal is the pane's terminal as the engine drives it: a PTY's
// master on Unix, a pseudoconsole's pipes on Windows.
type serverTerminal interface {
	// Read gives the terminal's output: everything the engine and its
	// programs wrote to it.
	io.Reader
	// Write is input for the terminal's programs.
	io.Writer
	resize(cols, rows, widthPx, heightPx uint16) error
	// flushInput discards input the terminal has received and nothing has
	// read yet.
	flushInput()
}

// serverFrameWriter serializes frames from the PTY reader and the main loop.
type serverFrameWriter struct {
	mu  sync.Mutex
	w   io.Writer
	buf []byte
	// closed: the engine is exiting, and sends nothing more.
	closed bool
}

func (fw *serverFrameWriter) send(kind byte, fixed []byte, tail []byte) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.closed {
		return nil
	}
	fw.buf = binary.LittleEndian.AppendUint32(fw.buf[:0], uint32(1+len(fixed)+len(tail)))
	fw.buf = append(fw.buf, kind)
	fw.buf = append(fw.buf, fixed...)
	fw.buf = append(fw.buf, tail...)
	_, err := fw.w.Write(fw.buf)
	return err
}

// readServerFrame reads one frame into buf (grown as needed).
func readServerFrame(r *bufio.Reader, buf []byte) (byte, []byte, []byte, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, buf, err
	}
	n := binary.LittleEndian.Uint32(head[:])
	if n == 0 || n > serverMaxFrame {
		return 0, nil, buf, fmt.Errorf("frame length %d", n)
	}
	if cap(buf) < int(n) {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, buf, err
	}
	return buf[0], buf[1:], buf, nil
}

// serverBlock is what the main loop knows about a block and the PTY reader
// reports when it reaches the block's markers.
type serverBlock struct {
	start    time.Time
	cwd      string
	exitCode int
	duration time.Duration
}

// serverMarks finds the engine's block markers in the PTY output:
//
//	ESC ] 6973 ; <nonce> ; S|E <block id> BEL
//
// The nonce is random per engine, so no program can forge a marker. It is
// upper-case hex, like the rest, so even a terminal set to map lower case
// to upper (OLCUC) passes it through unchanged.
type serverMarks struct {
	prefix  []byte
	pending []byte
}

func newServerMarks() (*serverMarks, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	prefix := "\x1b]6973;" + strings.ToUpper(hex.EncodeToString(nonce[:])) + ";"
	return &serverMarks{prefix: []byte(prefix)}, nil
}

func (m *serverMarks) marker(kind byte, id uint64) []byte {
	b := append([]byte(nil), m.prefix...)
	b = append(b, kind)
	b = strconv.AppendUint(b, id, 10)
	return append(b, '\a')
}

// feed scans PTY output: bytes before a marker go to emit, each marker to
// mark. A tail that could be the start of a marker is held until more output
// arrives.
func (m *serverMarks) feed(data []byte, emit func([]byte) error, mark func(kind byte, id uint64) error) error {
	m.pending = append(m.pending, data...)
	for {
		i := bytes.Index(m.pending, m.prefix)
		if i < 0 {
			keep := partialPrefix(m.pending, m.prefix)
			if out := m.pending[:len(m.pending)-keep]; len(out) > 0 {
				if err := emit(out); err != nil {
					return err
				}
			}
			m.pending = append(m.pending[:0], m.pending[len(m.pending)-keep:]...)
			return nil
		}
		if i > 0 {
			if err := emit(m.pending[:i]); err != nil {
				return err
			}
		}
		body := m.pending[i+len(m.prefix):]
		end := bytes.IndexByte(body, '\a')
		if end < 0 {
			// The marker continues in the next read.
			m.pending = append(m.pending[:0], m.pending[i:]...)
			return nil
		}
		if end < 2 || (body[0] != 'S' && body[0] != 'E') {
			return fmt.Errorf("malformed block marker %q", body[:end])
		}
		id, err := strconv.ParseUint(string(body[1:end]), 10, 64)
		if err != nil {
			return fmt.Errorf("malformed block marker %q", body[:end])
		}
		if err := mark(body[0], id); err != nil {
			return err
		}
		m.pending = append(m.pending[:0], body[end+1:]...)
	}
}

// partialPrefix is the length of the longest tail of b that is a proper
// prefix of prefix: output that may be the start of a marker.
func partialPrefix(b []byte, prefix []byte) int {
	start := max(0, len(b)-len(prefix)+1)
	esc := bytes.LastIndexByte(b[start:], 0x1b)
	if esc < 0 {
		return 0
	}
	for k := len(b) - start - esc; k > 0; k-- {
		if bytes.HasPrefix(prefix, b[len(b)-k:]) {
			return k
		}
	}
	return 0
}

// serverInput writes what the frontend sends the program (keys, pastes,
// the terminal's replies) to the PTY on a goroutine of its own, so the
// protocol reader never waits on the PTY. A program that doesn't read its
// input (a hung raw-mode program, or one that exited with input left
// unread) fills the PTY's input queue, and a write then waits till
// something reads; were the protocol reader the one waiting, the frontend
// would wait on it in turn. Past serverInputMax bytes not yet written, input
// is dropped: the program isn't reading it.
//
// Input is for a block's programs. Outside a block, nothing reads the PTY,
// so input then (replies the frontend's terminal made to output it was
// still drawing, after the block ended) is dropped, and what a block's
// programs leave unread is discarded when it ends.
type serverInput struct {
	mu    sync.Mutex
	queue [][]byte
	bytes int
	// Blocks asked to run and not yet finished.
	blocks int
	// Counts blockDone calls: a write begun before one is stale.
	gen uint64
	// The writer is writing (or flushing after a stale write).
	writing bool
	ready chan struct{}
	// The terminal whose unread input blockDone flushes; nil for none.
	term serverTerminal
}

const (
	serverInputMax = 1 << 20
	// Written at most this much at a time, so little more than this is
	// written after discard.
	serverInputChunk = 4096
)

func newServerInput(term serverTerminal) *serverInput {
	return &serverInput{ready: make(chan struct{}, 1), term: term}
}

func (in *serverInput) flush() {
	if in.term != nil {
		in.term.flushInput()
	}
}

// blockQueued notes a block asked to run: input is for it from now on. The
// protocol reader calls it as it reads the request, so keys sent after the
// request are kept.
func (in *serverInput) blockQueued() {
	in.mu.Lock()
	in.blocks++
	in.mu.Unlock()
}

// blockDone discards the input a block's programs left unread, queued or
// in the PTY. The flush also frees the writer, should it wait on a full
// queue; the rest of what it was writing it flushes itself, which blockDone
// waits for (a moment at most), so the terminal is still in the program's
// mode, likely not echoing, as it does.
func (in *serverInput) blockDone() {
	in.mu.Lock()
	in.blocks = max(0, in.blocks-1)
	in.queue, in.bytes = nil, 0
	in.gen++
	in.mu.Unlock()
	in.flush()
	for range 50 {
		in.mu.Lock()
		writing := in.writing
		in.mu.Unlock()
		if !writing {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// add queues input for the writer, or drops it: outside a block, or when
// the queue is full.
func (in *serverInput) add(b []byte) {
	in.mu.Lock()
	if in.blocks == 0 || in.bytes+len(b) > serverInputMax {
		in.mu.Unlock()
		return
	}
	in.queue = append(in.queue, append([]byte(nil), b...))
	in.bytes += len(b)
	in.mu.Unlock()
	select {
	case in.ready <- struct{}{}:
	default:
	}
}

// run writes the queued input to the terminal, in order.
func (in *serverInput) run(master io.Writer) {
	for range in.ready {
		for {
			in.mu.Lock()
			if len(in.queue) == 0 {
				in.mu.Unlock()
				break
			}
			b := in.queue[0]
			chunk := b[:min(len(b), serverInputChunk)]
			if len(chunk) == len(b) {
				in.queue[0] = nil
				in.queue = in.queue[1:]
			} else {
				in.queue[0] = b[len(chunk):]
			}
			in.bytes -= len(chunk)
			gen := in.gen
			in.writing = true
			in.mu.Unlock()
			if _, err := master.Write(chunk); err != nil {
				fmt.Fprintf(os.Stderr, "msh server: writing input: %v\n", err)
			}
			in.mu.Lock()
			if in.gen != gen {
				in.flush()
			}
			in.writing = false
			in.mu.Unlock()
		}
	}
}

type server struct {
	out    serverFrameWriter
	master serverTerminal
	marks  *serverMarks
	input  *serverInput
	// beforeExit, when set, runs just before the engine exits.
	beforeExit func()

	mu     sync.Mutex
	blocks map[uint64]*serverBlock

	// started and finished receive each block id once its block_started or
	// block_finished is sent.
	started  chan uint64
	finished chan uint64

	editor serverEditor
}

// readPTY forwards PTY output until the PTY closes, turning markers into
// block events.
func (s *server) readPTY() {
	current := uint64(0)
	var head [8]byte
	emit := func(b []byte) error {
		binary.LittleEndian.PutUint64(head[:], current)
		return s.out.send(serverMsgPtyOutput, head[:], b)
	}
	mark := func(kind byte, id uint64) error {
		s.mu.Lock()
		b := s.blocks[id]
		if kind == 'E' {
			delete(s.blocks, id)
		}
		s.mu.Unlock()
		if b == nil {
			return fmt.Errorf("marker for unknown block %d", id)
		}
		if kind == 'S' {
			current = id
			fixed := binary.LittleEndian.AppendUint64(nil, id)
			fixed = binary.LittleEndian.AppendUint64(fixed, uint64(b.start.UnixNano()))
			err := s.out.send(serverMsgBlockStarted, fixed, []byte(b.cwd))
			s.started <- id
			return err
		}
		current = 0
		fixed := binary.LittleEndian.AppendUint64(nil, id)
		fixed = binary.LittleEndian.AppendUint32(fixed, uint32(int32(b.exitCode)))
		fixed = binary.LittleEndian.AppendUint64(fixed, uint64(b.duration))
		err := s.out.send(serverMsgBlockFinished, fixed, nil)
		s.finished <- id
		return err
	}
	buf := make([]byte, 64<<10)
	for {
		n, err := s.master.Read(buf)
		if n > 0 {
			if ferr := s.marks.feed(buf[:n], emit, mark); ferr != nil {
				fmt.Fprintf(os.Stderr, "msh server: %v\n", ferr)
				s.exit(1)
			}
		}
		if err != nil {
			// EIO: every slave descriptor is closed. The engine is exiting.
			return
		}
	}
}

// readProtocol handles input and resize at once, even while a block runs,
// and queues runs for the main loop. It exits the engine when the frontend
// closes the protocol.
func (s *server) readProtocol(in io.Reader, runs chan<- any) {
	r := bufio.NewReaderSize(in, 64<<10)
	var buf []byte
	for {
		kind, payload, b, err := readServerFrame(r, buf)
		buf = b
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(os.Stderr, "msh server: reading the protocol: %v\n", err)
			}
			s.exit(0)
		}
		switch kind {
		case serverMsgRun:
			if len(payload) < 8 {
				s.protocolError("short run message")
			}
			id := binary.LittleEndian.Uint64(payload)
			if id == 0 {
				s.protocolError("run with block id 0")
			}
			s.input.blockQueued()
			runs <- serverRun{id: id, source: string(payload[8:])}
		case serverMsgBindingCall, serverMsgAbbrCall, serverMsgRegexMatch, serverMsgIsProgram, serverMsgEdit, serverMsgRunQuote, serverMsgLineKind:
			// The editor's requests run in order with the blocks, on the
			// main loop, which owns the evaluator.
			if kind == serverMsgRunQuote {
				s.input.blockQueued()
			}
			runs <- serverCall{kind: kind, payload: append([]byte(nil), payload...)}
		case serverMsgFmInput:
			s.fmInput(payload)
		case serverMsgInput:
			s.input.add(payload)
		case serverMsgResize:
			if len(payload) < 8 {
				s.protocolError("short resize message")
			}
			le := binary.LittleEndian
			if err := s.master.resize(le.Uint16(payload), le.Uint16(payload[2:]), le.Uint16(payload[4:]), le.Uint16(payload[6:])); err != nil {
				fmt.Fprintf(os.Stderr, "msh server: resizing the terminal: %v\n", err)
			}
		default:
			s.protocolError(fmt.Sprintf("unknown message type 0x%02x", kind))
		}
	}
}

func (s *server) protocolError(msg string) {
	fmt.Fprintf(os.Stderr, "msh server: protocol error: %s\n", msg)
	s.exit(1)
}

// exit ends the engine. Nothing is sent after: on Windows, closing the
// pseudoconsole makes it write its own last bytes, which mustn't follow the
// exit message.
func (s *server) exit(code int) {
	s.out.mu.Lock()
	s.out.closed = true
	s.out.mu.Unlock()
	if s.beforeExit != nil {
		s.beforeExit()
	}
	os.Exit(code)
}

type serverRun struct {
	id     uint64
	source string
	// An editRun or editRunOps quote to run instead of source, with the
	// EditState it is given (editRunOps).
	quote          *MShellQuotation
	withState      bool
	before, after  string
	staleQuote     bool
}

// newServer is the engine for a pane's terminal, with the protocol's output
// on protoOut.
func newServer(protoOut io.Writer, term serverTerminal) (*server, error) {
	marks, err := newServerMarks()
	if err != nil {
		return nil, err
	}
	return &server{
		out:      serverFrameWriter{w: protoOut},
		master:   term,
		marks:    marks,
		input:    newServerInput(term),
		blocks:   map[uint64]*serverBlock{},
		started:  make(chan uint64, 1),
		finished: make(chan uint64, 1),
	}, nil
}

// sendHello is the engine's first message.
func (s *server) sendHello() error {
	hello := binary.LittleEndian.AppendUint32(nil, serverProtocolVersion)
	hello = binary.LittleEndian.AppendUint32(hello, uint32(os.Getpid()))
	return s.out.send(serverMsgHello, hello, nil)
}

// serve runs the engine once the platform's runServer has made the terminal
// its stdin, stdout and stderr: the protocol from protoIn, the evaluator,
// and a block at a time. ttyFd is the terminal's input as TermState takes
// it. It returns only on a setup error.
func (s *server) serve(protoIn io.Reader, ttyFd int) int {
	// mshFileManager draws to the GUI, as a dialog (FileManagerGUI.go).
	fileManagerGUI = &fmGUI{
		input: make(chan fmGUIInput, 256),
		send:  func(view []byte) { s.out.send(serverMsgFmView, view, nil) },
	}
	go s.readPTY()
	go s.input.run(s.master)
	runs := make(chan any, 64)
	go s.readProtocol(protoIn, runs)

	state := newSessionState(ttyFd, 80, 24, nil)
	// The cooked state each line starts from and returns to, as in
	// interactive mode: a program that leaves the terminal raw doesn't
	// leave it raw for the next block.
	if err := state.saveTerminalState(); err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	state.l = NewLexer("", &TokenFile{"REPL"})
	state.p = &MShellParser{lexer: state.l}

	PrebuildCoreBuiltins()
	stdLibDefs, err := stdLibDefinitions(&state.stack, state.context, &state.evalState)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading standard library: %s\n", err)
	}
	state.stdLibDefs = stdLibDefs
	state.checker = &replChecker{session: state.evalState.StartupSession}
	state.loadHistory()
	state.printStartupErrors()
	state.evalState.printAbbrWarnings()
	// The frontend's editor waits for the first config: the bindings
	// init.msh added.
	s.sendConfigIfChanged(&state)
	s.sendPrompt(&state)

	for request := range runs {
		switch request := request.(type) {
		case serverRun:
			code, exit := s.runBlock(&state, request)
			if exit {
				s.out.send(serverMsgExit, binary.LittleEndian.AppendUint32(nil, uint32(int32(code))), nil)
				s.exit(code)
			}
		case serverCall:
			if request.kind == serverMsgRunQuote {
				code, exit := s.runBlock(&state, s.quoteRun(request))
				if exit {
					s.out.send(serverMsgExit, binary.LittleEndian.AppendUint32(nil, uint32(int32(code))), nil)
					s.exit(code)
				}
				continue
			}
			s.handleCall(&state, request)
		}
	}
	return 0
}

// runBlock runs one line between its markers and waits until the PTY
// reader has reported it finished.
func (s *server) runBlock(state *TermState, run serverRun) (int, bool) {
	cwd, _ := os.Getwd()
	b := &serverBlock{start: time.Now(), cwd: cwd}
	s.mu.Lock()
	s.blocks[run.id] = b
	s.mu.Unlock()

	state.leaveRawMode()
	os.Stdout.Write(s.marks.marker('S', run.id))
	// What the engine sends itself during the block (a config, a prompt,
	// ops) must come after block_started, which the reader sends when it
	// reaches the marker.
	for id := range s.started {
		if id == run.id {
			break
		}
	}
	var exit bool
	var code int
	if run.quote != nil || run.staleQuote {
		var program []editOp
		exit, code, program = s.runQuote(state, run)
		if program != nil {
			s.sendOps(run.id, program)
		}
	} else {
		source := strings.TrimSpace(run.source)
		state.lineRan = source != ""
		if source != "" {
			s.recordHistory(state, source)
		}
		exit, code = state.runLine(source)
		if state.lineRan {
			state.recordLine()
		}
		if !exit {
			code = 0
			if source != "" {
				code = lineExitStatus(state.lastLineOk, state.evalState.lastExitCode, state.evalState.ranProcess)
			}
		}
	}
	// A line can add bindings or abbreviations: the new config goes before
	// block_finished, which the end marker becomes, as does the prompt for
	// the next line (what the prompt's quotation prints is part of the
	// block).
	s.sendConfigIfChanged(state)
	if !exit {
		s.sendPrompt(state)
	}
	// Input the block's programs left unread isn't for the next block;
	// discarded in the mode they left the terminal in, which is likely not
	// to echo it.
	s.input.blockDone()
	// Back to the cooked state before the end marker, so the marker passes
	// through whatever the program left the terminal in.
	state.leaveRawMode()
	s.mu.Lock()
	b.exitCode, b.duration = code, time.Since(b.start)
	s.mu.Unlock()
	os.Stdout.Write(s.marks.marker('E', run.id))
	for id := range s.finished {
		if id == run.id {
			break
		}
	}
	return code, exit
}
