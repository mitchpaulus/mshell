//go:build windows

package main

import (
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetNumberOfConsoleInputEvents = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNumberOfConsoleInputEvents")
	procReadConsoleInputW             = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")
)

// consoleInputRecord is INPUT_RECORD with its KEY_EVENT_RECORD part. Other
// event types use the same 20 bytes differently; only key events are read.
type consoleInputRecord struct {
	eventType       uint16
	_               uint16
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

// INPUT_RECORD is 20 bytes; this fails to compile if the struct is not.
var _ [20]byte = [unsafe.Sizeof(consoleInputRecord{})]byte{}

const (
	consoleKeyEvent  = 0x0001 // KEY_EVENT
	waitTimedOut     = 0x0102 // WAIT_TIMEOUT
	maxConsoleEvents = 128    // events read at a time
)

// readTerminalInput waits at most wait for console input and returns the
// characters typed or sent by the terminal. It returns nothing, and true, if
// the wait timed out or only events that are not keys arrived (focus, mouse,
// window size); those events are dropped. It returns false if the console
// failed.
//
// Events are read only after the console reports some are queued, so the
// read returns at once. A plain read of standard input would instead wait
// for a key when the console was woken by an event that is not one. That
// holds while nothing else in the process reads the console at the same
// time, which is the case while the file manager starts.
func readTerminalInput(fd int, wait time.Duration) ([]byte, bool) {
	handle := windows.Handle(fd)
	event, err := windows.WaitForSingleObject(handle, uint32(waitMilliseconds(wait)))
	if err != nil {
		return nil, false
	}
	if event == waitTimedOut {
		return nil, true
	}
	if event != windows.WAIT_OBJECT_0 {
		return nil, false
	}

	var queued uint32
	if ok, _, _ := procGetNumberOfConsoleInputEvents.Call(uintptr(handle), uintptr(unsafe.Pointer(&queued))); ok == 0 {
		return nil, false
	}
	if queued == 0 {
		return nil, true
	}

	records := make([]consoleInputRecord, min(queued, maxConsoleEvents))
	var read uint32
	if ok, _, _ := procReadConsoleInputW.Call(uintptr(handle), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&read))); ok == 0 {
		return nil, false
	}

	var input []byte
	for _, record := range records[:min(read, uint32(len(records)))] {
		if record.eventType != consoleKeyEvent || record.keyDown == 0 || record.unicodeChar == 0 {
			continue
		}
		repeat := min(max(int(record.repeatCount), 1), maxTerminalReplyBytes)
		for i := 0; i < repeat; i++ {
			input = utf8.AppendRune(input, rune(record.unicodeChar))
		}
	}
	return input, true
}

// windowPixelCellSize returns zeros: the Windows console does not report a
// pixel size outside of the terminal's own replies.
func windowPixelCellSize(fd int, cols int, rows int) (int, int) {
	return 0, 0
}
