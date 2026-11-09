//go:build windows

package main

// msh server's terminal on Windows: a pseudoconsole (Server.go).
//
// The pseudoconsole comes from Microsoft's ConPTY package (conpty.dll, which
// starts the OpenConsole.exe beside it), not the one built into Windows:
// that one keeps text in its own screen buffer and draws it later, but
// passes escape sequences it doesn't know through at once, so the block
// markers would come out ahead of the block's output, and it redraws its
// whole screen, earlier blocks' text included. The newer one passes the
// programs' bytes through in order (ai/windows-server-experiments.md).
// msh-gui ships the two files and names conpty.dll in MSH_CONPTY; msh alone
// never needs them.
//
// A process can only be attached to a pseudoconsole when it is started, and
// the frontend starts the engine. So the engine starts a holder process
// inside the pseudoconsole it created, attaches to the holder's console, and
// ends the holder. The engine's own output then goes to the pseudoconsole,
// and every program it runs inherits it, as on Unix.

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// serverConptyEnv names conpty.dll; msh-gui sets it.
	serverConptyEnv = "MSH_CONPTY"
	// serverHolderEnv marks the holder: the copy of msh started in the
	// pseudoconsole so the engine can attach to it.
	serverHolderEnv = "MSH_SERVER_HOLDER"
)

// stillActive is the exit code GetExitCodeProcess gives a running process.
const stillActive = 259

var (
	procAttachConsole           = kernel32.NewProc("AttachConsole")
	procFreeConsole             = kernel32.NewProc("FreeConsole")
	procFlushConsoleInputBuffer = kernel32.NewProc("FlushConsoleInputBuffer")
	procUpdateProcThreadAttribute = kernel32.NewProc("UpdateProcThreadAttribute")
)

// conpty is the ConPTY package's API: kernel32's pseudoconsole functions,
// prefixed Conpty.
type conpty struct {
	create, resize, close *windows.Proc
}

func loadConpty(path string) (*conpty, error) {
	dll, err := windows.LoadDLL(path)
	if err != nil {
		return nil, err
	}
	var c conpty
	for name, p := range map[string]**windows.Proc{
		"ConptyCreatePseudoConsole": &c.create,
		"ConptyResizePseudoConsole": &c.resize,
		"ConptyClosePseudoConsole":  &c.close,
	} {
		if *p, err = dll.FindProc(name); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// coord packs a COORD (two int16s) into the one argument word it is passed in.
func coord(cols, rows uint16) uintptr {
	return uintptr(uint32(cols) | uint32(rows)<<16)
}

// windowsServerTerminal is the pseudoconsole: its output (read by a
// goroutine of its own, so the startup handshake can wait with a timeout),
// its input pipe, and the console input the engine is attached to.
type windowsServerTerminal struct {
	chunks  chan []byte
	readErr error // set before chunks is closed
	pending []byte
	in      *os.File
	conin   windows.Handle
	hpc     windows.Handle
	api     *conpty
}

// readOutput reads the pseudoconsole's output pipe until it closes.
func (t *windowsServerTerminal) readOutput(out *os.File) {
	for {
		buf := make([]byte, 64<<10)
		n, err := out.Read(buf)
		if n > 0 {
			t.chunks <- buf[:n]
		}
		if err != nil {
			t.readErr = err
			close(t.chunks)
			return
		}
	}
}

func (t *windowsServerTerminal) Read(b []byte) (int, error) {
	if len(t.pending) == 0 {
		chunk, ok := <-t.chunks
		if !ok {
			return 0, t.readErr
		}
		t.pending = chunk
	}
	n := copy(b, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

func (t *windowsServerTerminal) Write(b []byte) (int, error) { return t.in.Write(b) }

// The ConPTY handshake: when the pseudoconsole is created it writes
// ESC [ 1 t, and once the first process connects it asks the terminal for
// its primary device attributes and turns on focus reporting and its own
// Win32 input mode, the last of which ends the handshake. It is for the
// pseudoconsole and the engine, not for any block.
const (
	conptyAttributesQuery = "\x1b[c"
	conptyHandshakeEnd    = "\x1b[?9001h"
	// The attributes the engine answers with: those msh-gui's terminals
	// (libghostty-vt) report, a VT220 with colour and the clipboard.
	conptyAttributesReply = "\x1b[?62;22;52c"
)

// handshake answers the pseudoconsole's startup query and drops its startup
// output, so it neither waits on the query nor shows as output outside any
// block. Output after the handshake stays for Read. A pseudoconsole that
// says something else is given a moment, then left to Read.
func (t *windowsServerTerminal) handshake() {
	var seen []byte
	answered := false
	deadline := time.After(2 * time.Second)
	for {
		select {
		case chunk, ok := <-t.chunks:
			if !ok {
				t.pending = seen
				return
			}
			seen = append(seen, chunk...)
			if !answered && strings.Contains(string(seen), conptyAttributesQuery) {
				answered = true
				t.in.Write([]byte(conptyAttributesReply))
			}
			if i := strings.Index(string(seen), conptyHandshakeEnd); i >= 0 {
				t.pending = seen[i+len(conptyHandshakeEnd):]
				return
			}
		case <-deadline:
			t.pending = seen
			return
		}
	}
}

func (t *windowsServerTerminal) resize(cols, rows, widthPx, heightPx uint16) error {
	if r, _, _ := t.api.resize.Call(uintptr(t.hpc), coord(cols, rows)); r != 0 {
		return fmt.Errorf("ConptyResizePseudoConsole: 0x%x", r)
	}
	return nil
}

func (t *windowsServerTerminal) flushInput() {
	procFlushConsoleInputBuffer.Call(uintptr(t.conin))
}

// close leaves the pseudoconsole, then closes it, which closes the programs
// still attached to it. Closing waits for every attached process to leave,
// the engine included, and for OpenConsole to finish; it is given a moment.
func (t *windowsServerTerminal) close() {
	procFreeConsole.Call()
	done := make(chan struct{})
	go func() {
		t.api.close.Call(uintptr(t.hpc))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

// runServer is msh server. It returns only on a setup error.
func runServer() int {
	if os.Getenv(serverHolderEnv) != "" {
		// The holder: the engine attaches to this console, then ends it.
		time.Sleep(time.Minute)
		return 0
	}

	path := os.Getenv(serverConptyEnv)
	// Programs the engine runs don't need it.
	os.Unsetenv(serverConptyEnv)
	if path == "" {
		fmt.Fprintf(os.Stderr, "msh server: needs Microsoft's ConPTY (conpty.dll and OpenConsole.exe, shipped with msh-gui), named in %s\n", serverConptyEnv)
		return 1
	}
	api, err := loadConpty(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: loading %s: %v\n", path, err)
		return 1
	}

	// The protocol keeps the original stdin and stdout. Programs the engine
	// runs don't inherit them: Go's process start passes only the handles
	// it is given.
	protoIn, protoOut := os.Stdin, os.Stdout

	t, err := newPseudoconsole(api)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	s, err := newServer(protoOut, t)
	if err != nil {
		fmt.Fprintf(os.Stderr, "msh server: %v\n", err)
		return 1
	}
	s.beforeExit = t.close
	if err := s.sendHello(); err != nil {
		return 1
	}

	// Ctrl-C reaches every process attached to the console, the engine
	// among them, and means nothing to it between blocks. While a program
	// runs, msh's console handler already leaves Ctrl-C to it.
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, os.Interrupt)
	go func() {
		for range signals {
		}
	}()

	return s.serve(protoIn, int(t.conin))
}

// newPseudoconsole creates the pane's pseudoconsole and attaches the engine
// to it: its stdin, stdout and stderr become the console's.
func newPseudoconsole(api *conpty) (*windowsServerTerminal, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		return nil, err
	}
	var hpc windows.Handle
	r, _, _ := api.create.Call(coord(80, 24), uintptr(inR), uintptr(outW), 0, uintptr(unsafe.Pointer(&hpc)))
	// The pseudoconsole has its own copies of the ends it uses.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if r != 0 {
		return nil, fmt.Errorf("ConptyCreatePseudoConsole: 0x%x", r)
	}
	t := &windowsServerTerminal{
		chunks: make(chan []byte, 64),
		in:     os.NewFile(uintptr(inW), "pseudoconsole in"),
		hpc:    hpc,
		api:    api,
	}
	go t.readOutput(os.NewFile(uintptr(outR), "pseudoconsole out"))
	if err := attachPseudoconsole(hpc); err != nil {
		return nil, err
	}
	// The pseudoconsole finishes its handshake once a process has connected.
	t.handshake()

	conin, err := openConsole("CONIN$")
	if err != nil {
		return nil, err
	}
	conout, err := openConsole("CONOUT$")
	if err != nil {
		return nil, err
	}
	conerr, err := openConsole("CONOUT$")
	if err != nil {
		return nil, err
	}
	windows.SetStdHandle(windows.STD_INPUT_HANDLE, conin)
	windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, conout)
	windows.SetStdHandle(windows.STD_ERROR_HANDLE, conerr)
	os.Stdin = os.NewFile(uintptr(conin), "CONIN$")
	os.Stdout = os.NewFile(uintptr(conout), "CONOUT$")
	os.Stderr = os.NewFile(uintptr(conerr), "CONOUT$")
	t.conin = conin
	return t, nil
}

// attachPseudoconsole starts the holder in the pseudoconsole, moves the
// engine from its own console (if any) to the holder's, and ends the holder.
func attachPseudoconsole(hpc windows.Handle) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Started as the engine was (msh server, or a test binary's arguments),
	// so it reaches runServer and sees serverHolderEnv.
	holder, pid, err := startInPseudoconsole(hpc, exe, os.Args[1:], serverHolderEnv+"=1")
	if err != nil {
		return fmt.Errorf("starting the holder: %w", err)
	}
	defer windows.CloseHandle(holder)
	defer windows.TerminateProcess(holder, 0)

	procFreeConsole.Call()
	// The holder connects to the pseudoconsole as it starts; until then
	// AttachConsole fails (ERROR_GEN_FAILURE).
	var callErr error
	for range 500 {
		r, _, e := procAttachConsole.Call(uintptr(pid))
		if r != 0 {
			return nil
		}
		callErr = e
		var code uint32
		if windows.GetExitCodeProcess(holder, &code) == nil && code != stillActive {
			return fmt.Errorf("the holder exited (%d) before it could be attached to", code)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("attaching to the pseudoconsole: %v", callErr)
}

// startInPseudoconsole starts exe with args in the pseudoconsole, with the
// engine's environment and extra.
func startInPseudoconsole(hpc windows.Handle, exe string, args []string, extra ...string) (windows.Handle, uint32, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, 0, err
	}
	defer attrs.Delete()
	// The attribute's value is the handle itself, not a pointer to it, so
	// it is passed as a number rather than through Update's unsafe.Pointer.
	if r, _, e := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(attrs.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(hpc), unsafe.Sizeof(hpc), 0, 0); r == 0 {
		return 0, 0, fmt.Errorf("UpdateProcThreadAttribute: %v", e)
	}
	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = attrs.List()
	// Null standard handles, so the process takes the pseudoconsole's
	// rather than inheriting the engine's (the protocol's pipes).
	si.Flags = windows.STARTF_USESTDHANDLES

	line := []string{syscall.EscapeArg(exe)}
	for _, a := range args {
		line = append(line, syscall.EscapeArg(a))
	}
	cmdline, err := windows.UTF16PtrFromString(strings.Join(line, " "))
	if err != nil {
		return 0, 0, err
	}
	env := environmentBlock(append(os.Environ(), extra...))
	var pi windows.ProcessInformation
	err = windows.CreateProcess(nil, cmdline, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		&env[0], nil, &si.StartupInfo, &pi)
	if err != nil {
		return 0, 0, err
	}
	windows.CloseHandle(pi.Thread)
	return pi.Process, pi.ProcessId, nil
}

// environmentBlock is env as CreateProcess takes it: each NAME=value ended by
// a NUL, then one more NUL.
func environmentBlock(env []string) []uint16 {
	var b []uint16
	for _, kv := range env {
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	return append(b, 0)
}

func openConsole(name string) (windows.Handle, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(n, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", name, err)
	}
	return h, nil
}
