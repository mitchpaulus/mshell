package main

import (
	"time"
)

// Clipboard change propagation.
//
// Every FileManager instance watches the history directory (where the shared
// fm_clipboard file lives) so cut/copy state from other mshell instances is
// reflected immediately, even while this instance is idle. The platform
// dirWatcher implementations (DirWatch_linux.go, DirWatch_darwin.go,
// DirWatch_windows.go) deliver hint events only; the actual clipboard state
// is always re-read from disk (see refreshClipboard in FileManager.go).
//
// Degradation ladder:
//  1. Platform directory watch (instant updates).
//  2. If the watch cannot be established or dies: stat poll every 3 seconds,
//     bounded to 1 minute, then the goroutine goes dormant.
//  3. While dormant, the next keystroke restarts the ladder from step 1
//     (restartClipboardWatchIfDormant), and the stat check at the top of
//     every render still keeps the display fresh per keystroke regardless.

const clipboardWatchDebounce = 50 * time.Millisecond
const clipboardPollInterval = 3 * time.Second
const clipboardPollMaxDuration = time.Minute

func (fm *FileManager) startClipboardWatch() {
	dir, err := GetHistoryDir()
	if err != nil {
		return
	}
	fm.watchDir = dir
	fm.watchDone = make(chan struct{})
	fm.watchWG.Add(1)
	go func() {
		defer fm.watchWG.Done()
		fm.clipboardWatchLoop()
	}()
}

func (fm *FileManager) stopClipboardWatch() {
	if fm.watchDone == nil {
		return
	}
	close(fm.watchDone)
	fm.watchWG.Wait()
}

// restartClipboardWatchIfDormant restarts the watch goroutine after a bounded
// poll window expired. Called on every keystroke; the atomic check makes the
// common case (watch alive) free. Only ever called from the input loop, so it
// cannot race with stopClipboardWatch.
func (fm *FileManager) restartClipboardWatchIfDormant() {
	if fm.watchDone == nil || !fm.watchDormant.CompareAndSwap(true, false) {
		return
	}
	fm.watchWG.Add(1)
	go func() {
		defer fm.watchWG.Done()
		fm.clipboardWatchLoop()
	}()
}

func (fm *FileManager) clipboardWatchLoop() {
	watcher, err := watchDirectory(fm.watchDir)
	if err != nil {
		fm.clipboardPollLoop()
		return
	}
	defer watcher.Close()

	for {
		select {
		case <-fm.watchDone:
			return
		case name, ok := <-watcher.Events():
			if !ok {
				// The watcher died (e.g. the directory was removed);
				// degrade to bounded polling.
				fm.clipboardPollLoop()
				return
			}
			// An empty name means the backend does not know which entry
			// changed (kqueue, or an event-queue overflow); treat it as
			// potentially the clipboard file.
			if name != "" && name != clipboardFileName {
				continue
			}
			// Debounce: writers create a temp file and rename it into
			// place, which produces short bursts of events.
			timer := time.NewTimer(clipboardWatchDebounce)
			select {
			case <-fm.watchDone:
				timer.Stop()
				return
			case <-timer.C:
			}
			drainEvents(watcher.Events())
			fm.applyClipboardChange(true)
		}
	}
}

// drainEvents empties any queued events without blocking so a burst results
// in a single reload.
func drainEvents(events <-chan string) {
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// clipboardPollLoop stat-polls the clipboard for up to a minute, then marks
// the watch dormant and exits. The next keystroke restarts the watch.
func (fm *FileManager) clipboardPollLoop() {
	ticker := time.NewTicker(clipboardPollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(clipboardPollMaxDuration)
	defer deadline.Stop()
	for {
		select {
		case <-fm.watchDone:
			return
		case <-deadline.C:
			fm.watchDormant.Store(true)
			return
		case <-ticker.C:
			fm.applyClipboardChange(false)
		}
	}
}

// applyClipboardChange re-reads the shared clipboard and re-renders if it
// changed. force skips the stat comparison (used when the watcher reported an
// event for the clipboard file itself). renderMu keeps this from painting
// over modal prompts or an editor session: handleInput holds the mutex for
// its full duration, so this blocks until input handling finishes.
func (fm *FileManager) applyClipboardChange(force bool) {
	fm.renderMu.Lock()
	if fm.refreshClipboard(force) {
		fm.render()
	}
	fm.renderMu.Unlock()
}
