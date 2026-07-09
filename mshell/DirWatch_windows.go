//go:build windows

package main

import (
	"encoding/binary"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// dirWatcher delivers change notifications for a single directory via
// overlapped ReadDirectoryChangesW. Each event carries the name of the
// changed entry; a kernel buffer overflow (lost events) is reported as an
// empty name, so consumers must re-read the actual state rather than trust
// the event stream to be complete.
type dirWatcher struct {
	events    chan string
	handle    windows.Handle
	olEvent   windows.Handle // auto-reset, signaled when an overlapped read completes
	stopEvent windows.Handle // auto-reset, signaled by Close to stop the read loop
	done      chan struct{}  // closed when readLoop exits
	closeOnce sync.Once
}

func watchDirectory(path string) (*dirWatcher, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, err
	}
	olEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	stopEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		windows.CloseHandle(olEvent)
		windows.CloseHandle(handle)
		return nil, err
	}

	w := &dirWatcher{
		events:    make(chan string, 16),
		handle:    handle,
		olEvent:   olEvent,
		stopEvent: stopEvent,
		done:      make(chan struct{}),
	}
	go w.readLoop()
	return w, nil
}

func (w *dirWatcher) Events() <-chan string {
	return w.events
}

// Close signals the read loop, waits for it to exit, and only then closes the
// handles, so an outstanding overlapped read can never race with handle reuse.
func (w *dirWatcher) Close() error {
	w.closeOnce.Do(func() {
		windows.SetEvent(w.stopEvent)
		<-w.done
		windows.CloseHandle(w.handle)
		windows.CloseHandle(w.olEvent)
		windows.CloseHandle(w.stopEvent)
	})
	return nil
}

func (w *dirWatcher) readLoop() {
	defer close(w.done)
	defer close(w.events)
	// The kernel writes FILE_NOTIFY_INFORMATION records into this buffer
	// while a read is outstanding, so it must stay allocated for the life of
	// the loop.
	buf := make([]byte, 8192)
	filter := uint32(windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_LAST_WRITE | windows.FILE_NOTIFY_CHANGE_SIZE)
	for {
		var ol windows.Overlapped
		ol.HEvent = w.olEvent
		err := windows.ReadDirectoryChanges(w.handle, &buf[0], uint32(len(buf)), false, filter, nil, &ol, 0)
		if err != nil && err != windows.ERROR_IO_PENDING {
			return
		}

		which, err := windows.WaitForMultipleObjects([]windows.Handle{w.stopEvent, w.olEvent}, false, windows.INFINITE)
		if err != nil {
			windows.CancelIoEx(w.handle, &ol)
			return
		}
		if which == windows.WAIT_OBJECT_0 {
			// Stop requested. Cancel the outstanding read and wait for it to
			// complete so the kernel is no longer writing into buf.
			windows.CancelIoEx(w.handle, &ol)
			var n uint32
			windows.GetOverlappedResult(w.handle, &ol, &n, true)
			return
		}

		var n uint32
		if err := windows.GetOverlappedResult(w.handle, &ol, &n, false); err != nil {
			if err == windows.ERROR_NOTIFY_ENUM_DIR {
				// Kernel buffer overflowed: events were lost; tell the
				// consumer to re-check state.
				w.send("")
				continue
			}
			return
		}
		if n == 0 {
			// Overflow can also surface as a successful zero-length read.
			w.send("")
			continue
		}
		w.parseAndSend(buf[:n])
	}
}

// parseAndSend walks the packed FILE_NOTIFY_INFORMATION records:
// NextEntryOffset uint32, Action uint32, FileNameLength uint32 (bytes),
// FileName []uint16 (not NUL-terminated).
func (w *dirWatcher) parseAndSend(buf []byte) {
	offset := uint32(0)
	for {
		if int(offset)+12 > len(buf) {
			return
		}
		next := binary.LittleEndian.Uint32(buf[offset:])
		nameLen := binary.LittleEndian.Uint32(buf[offset+8:])
		nameStart := offset + 12
		nameEnd := nameStart + nameLen
		if int(nameEnd) > len(buf) {
			return
		}
		nameBytes := buf[nameStart:nameEnd]
		codeUnits := make([]uint16, nameLen/2)
		for i := range codeUnits {
			codeUnits[i] = binary.LittleEndian.Uint16(nameBytes[i*2:])
		}
		w.send(string(utf16.Decode(codeUnits)))
		if next == 0 {
			return
		}
		offset += next
	}
}

// send never blocks: events are hints and the consumer re-reads the real
// state, so dropping an event behind an already-pending one loses nothing.
func (w *dirWatcher) send(name string) {
	select {
	case w.events <- name:
	default:
	}
}
