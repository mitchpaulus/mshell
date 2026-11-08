//go:build linux || darwin

package main

// msh server: the engine of the GUI frontend (ai/gui-terminal.typ, "Architecture").
//
// One engine runs one session on one PTY, the pane's. At startup it keeps
// private copies of its stdin and stdout for the protocol, then makes the
// PTY's slave side its stdin, stdout, stderr and controlling terminal. The
// evaluator, job control and every child process then see exactly what
// interactive msh sees on a terminal, and nothing in the evaluator changes.
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
//	0x02 input      bytes for the PTY: keys for the running program; Ctrl-C is just 0x03
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
// up, and the foreground job gets SIGHUP, as when a terminal window closes.

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
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
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

// serverSetsidEnv marks the copy of the engine started in its own session.
const serverSetsidEnv = "MSH_SERVER_SETSID"

// serverFrameWriter serializes frames from the PTY reader and the main loop.
type serverFrameWriter struct {
	mu  sync.Mutex
	w   io.Writer
	buf []byte
}

func (fw *serverFrameWriter) send(kind byte, fixed []byte, tail []byte) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
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

type server struct {
	out    serverFrameWriter
	master *os.File
	marks  *serverMarks

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
				os.Exit(1)
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
			os.Exit(0)
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
			runs <- serverRun{id: id, source: string(payload[8:])}
		case serverMsgBindingCall, serverMsgAbbrCall, serverMsgRegexMatch, serverMsgIsProgram, serverMsgEdit, serverMsgRunQuote:
			// The editor's requests run in order with the blocks, on the
			// main loop, which owns the evaluator.
			runs <- serverCall{kind: kind, payload: append([]byte(nil), payload...)}
		case serverMsgFmInput:
			s.fmInput(payload)
		case serverMsgInput:
			if _, err := s.master.Write(payload); err != nil {
				fmt.Fprintf(os.Stderr, "msh server: writing input: %v\n", err)
			}
		case serverMsgResize:
			if len(payload) < 8 {
				s.protocolError("short resize message")
			}
			ws := pty.Winsize{
				Cols: binary.LittleEndian.Uint16(payload),
				Rows: binary.LittleEndian.Uint16(payload[2:]),
				X:    binary.LittleEndian.Uint16(payload[4:]),
				Y:    binary.LittleEndian.Uint16(payload[6:]),
			}
			if err := pty.Setsize(s.master, &ws); err != nil {
				fmt.Fprintf(os.Stderr, "msh server: resizing the PTY: %v\n", err)
			}
		default:
			s.protocolError(fmt.Sprintf("unknown message type 0x%02x", kind))
		}
	}
}

func (s *server) protocolError(msg string) {
	fmt.Fprintf(os.Stderr, "msh server: protocol error: %s\n", msg)
	os.Exit(1)
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

// runServer is msh server. It returns only on a setup error.
func runServer() int {
	if code, handled := serverOwnSession(); handled {
		return code
	}

	// The protocol keeps the original stdin and stdout, at descriptors no
	// child inherits.
	protoIn, err := unix.FcntlInt(0, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	protoOut, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}

	master, slave, err := pty.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: opening a PTY: %v\n", err)
		return 1
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	// The PTY becomes this session's controlling terminal, with the engine
	// in the foreground, as a terminal's shell is.
	if err := unix.IoctlSetInt(int(slave.Fd()), unix.TIOCSCTTY, 0); err != nil {
		fmt.Fprintf(os.Stderr, "msh server: taking the PTY as controlling terminal: %v\n", err)
		return 1
	}
	marks, err := newServerMarks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	s := &server{
		out:      serverFrameWriter{w: os.NewFile(uintptr(protoOut), "protocol out")},
		master:   master,
		marks:    marks,
		blocks:   map[uint64]*serverBlock{},
		started:  make(chan uint64, 1),
		finished: make(chan uint64, 1),
	}

	hello := binary.LittleEndian.AppendUint32(nil, serverProtocolVersion)
	hello = binary.LittleEndian.AppendUint32(hello, uint32(os.Getpid()))
	if err := s.out.send(serverMsgHello, hello, nil); err != nil {
		return 1
	}

	for fd := 0; fd <= 2; fd++ {
		if err := dupTo(int(slave.Fd()), fd); err != nil {
			fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
			return 1
		}
	}
	slave.Close()

	// Keys typed while the engine itself is in the foreground would signal
	// it: Ctrl-C, Ctrl-\ and Ctrl-Z mean nothing to the engine between
	// blocks. Caught rather than ignored, so children get the default
	// action back when they exec.
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	go func() {
		for range signals {
		}
	}()

	// mshFileManager draws to the GUI, as a dialog (FileManagerGUI.go).
	fileManagerGUI = &fmGUI{
		input: make(chan fmGUIInput, 256),
		send:  func(view []byte) { s.out.send(serverMsgFmView, view, nil) },
	}
	go s.readPTY()
	runs := make(chan any, 64)
	go s.readProtocol(os.NewFile(uintptr(protoIn), "protocol in"), runs)

	state := newSessionState(0, 80, 24, nil)
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
				os.Exit(code)
			}
		case serverCall:
			if request.kind == serverMsgRunQuote {
				code, exit := s.runBlock(&state, s.quoteRun(request))
				if exit {
					s.out.send(serverMsgExit, binary.LittleEndian.AppendUint32(nil, uint32(int32(code))), nil)
					os.Exit(code)
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

// serverOwnSession makes the engine a session leader, so the PTY can become
// its controlling terminal. A process that leads its process group (as a
// shell starts it) can't call setsid, so then the engine runs a copy of
// itself in a new session and passes its exit code on.
func serverOwnSession() (int, bool) {
	copied := os.Getenv(serverSetsidEnv) != ""
	// Programs the engine runs must not inherit the marker.
	os.Unsetenv(serverSetsidEnv)
	if _, err := syscall.Setsid(); err == nil {
		return 0, false
	}
	if copied {
		fmt.Fprintln(os.Stderr, "msh server: can't start a new session")
		return 1, true
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1, true
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), serverSetsidEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), true
		}
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1, true
	}
	return 0, true
}
