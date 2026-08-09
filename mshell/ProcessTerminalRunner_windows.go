package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const procThreadAttributePseudoConsole = 0x00020016
const windowsUTF8CodePage = 65001

var procCancelSynchronousIo = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

// windowsTerminalProcess owns every kernel object in one foreground ConPTY
// transaction.  The process starts suspended so it cannot create a descendant
// before assignment to the kill-on-close Job Object.
type windowsTerminalProcess struct {
	console      windows.Handle
	job          windows.Handle
	process      windows.Handle
	thread       windows.Handle
	input        *os.File
	output       *os.File
	outputDone   chan error
	inputReady   chan windows.Handle
	inputDone    chan struct{}
	inputStop    chan struct{}
	inputThread  windows.Handle
	resizeDone   chan struct{}
	resizeWait   sync.WaitGroup
	foreground   *ForegroundLease
}

func runIsolatedTerminalCommand(cmd *exec.Cmd, stdio ResolvedProcessStdio) (bool, int, error) {
	if !stdio.HasTerminalStdio() {
		return false, 0, nil
	}
	if _, ok := stdio.Stdin.(*os.File); !ok {
		return false, 0, nil
	}
	if _, ok := stdio.Stdout.(*os.File); !ok {
		return false, 0, nil
	}
	if _, ok := stdio.Stderr.(*os.File); !ok {
		return false, 0, nil
	}

	terminalProcess, err := startWindowsTerminalProcess(cmd, stdio)
	if err != nil {
		return true, classifyStartError(err), err
	}
	exitCode, waitErr := terminalProcess.wait()
	return true, exitCode, waitErr
}

func startWindowsTerminalProcess(cmd *exec.Cmd, stdio ResolvedProcessStdio) (_ *windowsTerminalProcess, returnErr error) {
	stdin, stdinOK := stdio.Stdin.(*os.File)
	stdout, stdoutOK := stdio.Stdout.(*os.File)
	if !stdinOK || !stdoutOK {
		return nil, fmt.Errorf("terminal streams are not files")
	}

	ptyInput, hostInput, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create pseudoconsole input pipe: %w", err)
	}
	defer func() {
		if returnErr != nil {
			ptyInput.Close()
			hostInput.Close()
		}
	}()

	hostOutput, ptyOutput, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create pseudoconsole output pipe: %w", err)
	}
	defer func() {
		if returnErr != nil {
			hostOutput.Close()
			ptyOutput.Close()
		}
	}()

	size := windowsConsoleSize(windows.Handle(stdout.Fd()))
	var console windows.Handle
	if err := windows.CreatePseudoConsole(size, windows.Handle(ptyInput.Fd()), windows.Handle(ptyOutput.Fd()), 0, &console); err != nil {
		return nil, fmt.Errorf("create pseudoconsole: %w", err)
	}
	consoleOpen := true
	defer func() {
		if returnErr != nil && consoleOpen {
			windows.ClosePseudoConsole(console)
		}
	}()

	// ConPTY owns duplicates of these two ends after successful creation.
	// Keeping host copies open prevents EOF and can deadlock shutdown.
	if err := ptyInput.Close(); err != nil {
		return nil, fmt.Errorf("close host pseudoconsole input end: %w", err)
	}
	if err := ptyOutput.Close(); err != nil {
		return nil, fmt.Errorf("close host pseudoconsole output end: %w", err)
	}

	job, err := newWindowsProcessJob()
	if err != nil {
		return nil, err
	}
	jobOpen := true
	defer func() {
		if returnErr != nil && jobOpen {
			windows.CloseHandle(job)
		}
	}()

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("create process attribute list: %w", err)
	}
	defer attrs.Delete()
	if err := attrs.Update(procThreadAttributePseudoConsole, pseudoConsoleAttributeValue(console), unsafe.Sizeof(console)); err != nil {
		return nil, fmt.Errorf("attach pseudoconsole process attribute: %w", err)
	}

	pi, err := createSuspendedWindowsProcess(cmd, attrs)
	if err != nil {
		return nil, err
	}
	processOpen := true
	threadOpen := true
	defer func() {
		if returnErr != nil {
			if processOpen {
				windows.TerminateProcess(pi.Process, 1)
				windows.CloseHandle(pi.Process)
			}
			if threadOpen {
				windows.CloseHandle(pi.Thread)
			}
		}
	}()

	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		return nil, fmt.Errorf("assign suspended process to job object: %w", err)
	}

	foreground, err := acquireForeground(stdio.ControlTerminal(), int(pi.ProcessId))
	if err != nil {
		return nil, fmt.Errorf("reserve terminal for pseudoconsole: %w", err)
	}
	foregroundOpen := true
	defer func() {
		if returnErr != nil && foregroundOpen {
			foreground.Release()
		}
	}()

	if err := prepareWindowsRelayConsole(stdin, stdout); err != nil {
		return nil, err
	}

	terminalProcess := &windowsTerminalProcess{
		console:    console,
		job:        job,
		process:    pi.Process,
		thread:     pi.Thread,
		input:      hostInput,
		output:     hostOutput,
		outputDone: make(chan error, 1),
		inputReady: make(chan windows.Handle, 1),
		inputDone:  make(chan struct{}),
		inputStop:  make(chan struct{}),
		resizeDone: make(chan struct{}),
		foreground: foreground,
	}

	// From this point terminalProcess owns rollback as well as normal teardown.
	consoleOpen = false
	jobOpen = false
	processOpen = false
	threadOpen = false
	foregroundOpen = false
	go func() {
		_, copyErr := io.Copy(stdout, hostOutput)
		terminalProcess.outputDone <- copyErr
	}()
	go terminalProcess.relayInput(stdin)
	terminalProcess.inputThread = <-terminalProcess.inputReady
	terminalProcess.resizeWait.Add(1)
	go terminalProcess.relayResize(windows.Handle(stdout.Fd()))

	if terminalProcess.inputThread == 0 {
		windows.TerminateJobObject(job, 1)
		_, cleanupErr := terminalProcess.wait()
		return nil, errors.Join(fmt.Errorf("duplicate input-relay thread handle"), cleanupErr)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		windows.TerminateJobObject(job, 1)
		_, cleanupErr := terminalProcess.wait()
		return nil, errors.Join(fmt.Errorf("resume pseudoconsole process: %w", err), cleanupErr)
	}
	if err := windows.CloseHandle(pi.Thread); err == nil {
		terminalProcess.thread = 0
	}
	return terminalProcess, nil
}

func pseudoConsoleAttributeValue(console windows.Handle) unsafe.Pointer {
	// Unlike most UpdateProcThreadAttribute values, Microsoft specifies HPCON
	// itself as lpValue, not the address of an HPCON variable.
	return *(*unsafe.Pointer)(unsafe.Pointer(&console))
}

func createSuspendedWindowsProcess(cmd *exec.Cmd, attrs *windows.ProcThreadAttributeListContainer) (*windows.ProcessInformation, error) {
	path, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return nil, fmt.Errorf("encode executable path: %w", err)
	}

	commandLine := windows.ComposeCommandLine(cmd.Args)
	var sys *syscall.SysProcAttr
	if cmd.SysProcAttr != nil {
		sys = cmd.SysProcAttr
		if sys.CmdLine != "" {
			commandLine = sys.CmdLine
		}
	}
	commandLinePointer, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return nil, fmt.Errorf("encode command line: %w", err)
	}

	var directory *uint16
	if cmd.Dir != "" {
		directory, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return nil, fmt.Errorf("encode working directory: %w", err)
		}
	}

	environment, err := windowsEnvironmentBlock(cmd.Env)
	if err != nil {
		return nil, err
	}

	startup := new(windows.StartupInfoEx)
	startup.Cb = uint32(unsafe.Sizeof(*startup))
	startup.ProcThreadAttributeList = attrs.List()
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_SUSPENDED)
	if sys != nil {
		flags |= sys.CreationFlags
	}

	pi := new(windows.ProcessInformation)
	if sys != nil && sys.Token != 0 {
		err = windows.CreateProcessAsUser(windows.Token(sys.Token), path, commandLinePointer, nil, nil, false, flags, &environment[0], directory, &startup.StartupInfo, pi)
	} else {
		err = windows.CreateProcess(path, commandLinePointer, nil, nil, false, flags, &environment[0], directory, &startup.StartupInfo, pi)
	}
	if err != nil {
		return nil, fmt.Errorf("create suspended pseudoconsole process: %w", err)
	}
	return pi, nil
}

func windowsEnvironmentBlock(environment []string) ([]uint16, error) {
	if environment == nil {
		environment = os.Environ()
	}
	environment = append([]string(nil), environment...)
	sort.SliceStable(environment, func(i, j int) bool {
		return strings.ToUpper(environment[i]) < strings.ToUpper(environment[j])
	})

	block := make([]uint16, 0)
	for _, entry := range environment {
		if strings.IndexByte(entry, 0) >= 0 {
			return nil, fmt.Errorf("environment entry contains NUL")
		}
		block = append(block, utf16.Encode([]rune(entry))...)
		block = append(block, 0)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return block, nil
}

func newWindowsProcessJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create process job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("set kill-on-close job limit: %w", err)
	}
	return job, nil
}

func windowsConsoleSize(output windows.Handle) windows.Coord {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(output, &info); err != nil {
		return windows.Coord{X: 80, Y: 25}
	}
	width := info.Window.Right - info.Window.Left + 1
	height := info.Window.Bottom - info.Window.Top + 1
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 25
	}
	return windows.Coord{X: width, Y: height}
}

func prepareWindowsRelayConsole(input, output *os.File) error {
	if err := windows.SetConsoleCP(windowsUTF8CodePage); err != nil {
		return fmt.Errorf("set UTF-8 console input code page for pseudoconsole relay: %w", err)
	}
	if err := windows.SetConsoleOutputCP(windowsUTF8CodePage); err != nil {
		return fmt.Errorf("set UTF-8 console output code page for pseudoconsole relay: %w", err)
	}
	inputHandle := windows.Handle(input.Fd())
	var inputMode uint32
	if err := windows.GetConsoleMode(inputHandle, &inputMode); err != nil {
		return fmt.Errorf("read console input mode for pseudoconsole relay: %w", err)
	}
	inputMode &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT
	inputMode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(inputHandle, inputMode); err != nil {
		return fmt.Errorf("set console input mode for pseudoconsole relay: %w", err)
	}

	outputHandle := windows.Handle(output.Fd())
	var outputMode uint32
	if err := windows.GetConsoleMode(outputHandle, &outputMode); err != nil {
		return fmt.Errorf("read console output mode for pseudoconsole relay: %w", err)
	}
	outputMode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(outputHandle, outputMode); err != nil {
		return fmt.Errorf("set console output mode for pseudoconsole relay: %w", err)
	}
	return nil
}

func (process *windowsTerminalProcess) relayInput(input *os.File) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(process.inputDone)

	currentProcess := windows.CurrentProcess()
	var thread windows.Handle
	err := windows.DuplicateHandle(currentProcess, windows.CurrentThread(), currentProcess, &thread, 0, false, windows.DUPLICATE_SAME_ACCESS)
	if err != nil {
		process.inputReady <- 0
		return
	}
	process.inputReady <- thread

	inputHandle := windows.Handle(input.Fd())
	buffer := make([]byte, 4096)
	for {
		select {
		case <-process.inputStop:
			return
		default:
		}
		var count uint32
		if err := windows.ReadFile(inputHandle, buffer, &count, nil); err != nil || count == 0 {
			return
		}
		if _, err := process.input.Write(buffer[:count]); err != nil {
			return
		}
	}
}

func (process *windowsTerminalProcess) relayResize(output windows.Handle) {
	defer process.resizeWait.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastSize := windowsConsoleSize(output)
	for {
		select {
		case <-process.resizeDone:
			return
		case <-ticker.C:
			size := windowsConsoleSize(output)
			if size != lastSize {
				windows.ResizePseudoConsole(process.console, size)
				lastSize = size
			}
		}
	}
}

func (process *windowsTerminalProcess) wait() (exitCode int, returnErr error) {
	defer func() {
		close(process.resizeDone)
		process.resizeWait.Wait()
		close(process.inputStop)
		process.stopInputRelay()
		process.input.Close()
		if process.inputThread != 0 {
			windows.CloseHandle(process.inputThread)
		}

		// The output reader must remain active while ClosePseudoConsole runs;
		// older Windows versions can block here until pending output drains.
		windows.ClosePseudoConsole(process.console)
		outputErr := <-process.outputDone
		process.output.Close()

		windows.CloseHandle(process.process)
		if process.thread != 0 {
			windows.CloseHandle(process.thread)
		}
		windows.CloseHandle(process.job)
		foregroundErr := process.foreground.Release()
		returnErr = errors.Join(returnErr, outputErr, foregroundErr)
	}()

	result, err := windows.WaitForSingleObject(process.process, windows.INFINITE)
	if err != nil {
		return ExitStartUnknown, fmt.Errorf("wait for pseudoconsole process: %w", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		return ExitStartUnknown, fmt.Errorf("unexpected pseudoconsole wait result %d", result)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(process.process, &code); err != nil {
		return ExitStartUnknown, fmt.Errorf("read pseudoconsole process exit code: %w", err)
	}
	return int(code), nil
}

func (process *windowsTerminalProcess) stopInputRelay() {
	if process.inputThread == 0 {
		<-process.inputDone
		return
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		cancelSynchronousWindowsIO(process.inputThread)
		select {
		case <-process.inputDone:
			return
		case <-ticker.C:
		}
	}
}

func cancelSynchronousWindowsIO(thread windows.Handle) error {
	result, _, callErr := procCancelSynchronousIo.Call(uintptr(thread))
	if result != 0 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return callErr
	}
	return nil
}
