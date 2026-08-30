//go:build darwin

package main

import (
	"sync"

	"golang.org/x/sys/unix"
)

// shutdownIdent identifies the EVFILT_USER event used to wake the read loop
// during Close.
const shutdownIdent = 1

// dirWatcher delivers coarse change notifications for a single directory via
// kqueue. Directory-level vnode events fire on entry create/delete/rename but
// carry no entry names, so every event is sent as an empty name and consumers
// must re-check the actual state on disk.
type dirWatcher struct {
	events    chan string
	kq        int
	dirFd     int
	done      chan struct{} // closed when readLoop exits
	closeOnce sync.Once
}

func watchDirectory(path string) (*dirWatcher, error) {
	// O_EVTONLY watches without holding a real open reference, so the watch
	// does not prevent the volume from unmounting.
	dirFd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	kq, err := unix.Kqueue()
	if err != nil {
		unix.Close(dirFd)
		return nil, err
	}

	// Register the directory vnode events plus a user event used only to
	// wake the read loop for shutdown.
	var dirEvent, userEvent unix.Kevent_t
	unix.SetKevent(&dirEvent, dirFd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
	dirEvent.Fflags = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_ATTRIB | unix.NOTE_LINK | unix.NOTE_DELETE | unix.NOTE_RENAME
	unix.SetKevent(&userEvent, shutdownIdent, unix.EVFILT_USER, unix.EV_ADD|unix.EV_CLEAR)
	if _, err := unix.Kevent(kq, []unix.Kevent_t{dirEvent, userEvent}, nil, nil); err != nil {
		unix.Close(kq)
		unix.Close(dirFd)
		return nil, err
	}

	w := &dirWatcher{
		events: make(chan string, 16),
		kq:     kq,
		dirFd:  dirFd,
		done:   make(chan struct{}),
	}
	go w.readLoop()
	return w, nil
}

func (w *dirWatcher) Events() <-chan string {
	return w.events
}

// Close wakes the read loop via the user event, waits for it to exit, and
// only then closes the descriptors, so a blocked Kevent call can never race
// with descriptor reuse.
func (w *dirWatcher) Close() error {
	w.closeOnce.Do(func() {
		var trigger unix.Kevent_t
		unix.SetKevent(&trigger, shutdownIdent, unix.EVFILT_USER, 0)
		trigger.Fflags = unix.NOTE_TRIGGER
		unix.Kevent(w.kq, []unix.Kevent_t{trigger}, nil, nil) // EBADF etc. only if readLoop already exited
		<-w.done
		unix.Close(w.kq)
		unix.Close(w.dirFd)
	})
	return nil
}

func (w *dirWatcher) readLoop() {
	defer close(w.done)
	defer close(w.events)
	received := make([]unix.Kevent_t, 4)
	for {
		n, err := unix.Kevent(w.kq, nil, received, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return
		}
		for i := 0; i < n; i++ {
			if received[i].Filter == unix.EVFILT_USER {
				return
			}
			if received[i].Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME) != 0 {
				// The watched directory itself was deleted or renamed; the
				// watch is dead. Send a final hint and exit so the consumer
				// can fall back to polling.
				w.send("")
				return
			}
			w.send("")
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
