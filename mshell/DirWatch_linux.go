//go:build linux

package main

import (
	"bytes"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// dirWatcher delivers coarse change notifications for a single directory via
// inotify. Each event carries the name of the changed entry. Events are hints
// only: the channel drops events when full and a kernel queue overflow is
// reported as an empty name, so consumers must re-read the actual state from
// disk rather than trust the event stream to be complete.
type dirWatcher struct {
	events chan string
	file   *os.File // wraps the inotify descriptor; Close unblocks Read
}

func watchDirectory(path string) (*dirWatcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	mask := uint32(unix.IN_CREATE | unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_MOVED_FROM | unix.IN_DELETE | unix.IN_ONLYDIR)
	if _, err := unix.InotifyAddWatch(fd, path, mask); err != nil {
		unix.Close(fd)
		return nil, err
	}
	// os.NewFile registers the non-blocking descriptor with the runtime
	// poller, so Read blocks cooperatively and Close unblocks a pending Read.
	w := &dirWatcher{
		events: make(chan string, 16),
		file:   os.NewFile(uintptr(fd), "inotify"),
	}
	go w.readLoop()
	return w, nil
}

func (w *dirWatcher) Events() <-chan string {
	return w.events
}

func (w *dirWatcher) Close() error {
	return w.file.Close()
}

func (w *dirWatcher) readLoop() {
	defer close(w.events)
	// Room for many events; a single event is at most
	// unix.SizeofInotifyEvent + NAME_MAX + 1 bytes.
	buf := make([]byte, 4096)
	for {
		n, err := w.file.Read(buf)
		if err != nil {
			return // closed or unrecoverable
		}
		for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
			event := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))
			if event.Mask&unix.IN_IGNORED != 0 {
				// The watch itself is gone (directory deleted or moved).
				// Exit so the consumer can fall back to polling.
				return
			}
			nameLen := int(event.Len)
			if offset+unix.SizeofInotifyEvent+nameLen > n {
				// A record claiming to extend past the read is malformed;
				// the kernel never splits events, so stop parsing rather
				// than slice past the data (which would panic the process).
				break
			}
			name := ""
			if nameLen > 0 {
				nameBytes := buf[offset+unix.SizeofInotifyEvent : offset+unix.SizeofInotifyEvent+nameLen]
				name = string(bytes.TrimRight(nameBytes, "\x00"))
			}
			// IN_Q_OVERFLOW arrives with no name: the empty name tells the
			// consumer events were lost and it must re-check state.
			w.send(name)
			offset += unix.SizeofInotifyEvent + nameLen
		}
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
