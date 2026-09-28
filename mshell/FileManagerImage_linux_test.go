//go:build linux

package main

import (
	"bytes"
	"os"
	"os/signal"
	"runtime"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// openRawPty opens a pseudo terminal with its terminal side in raw mode, as
// the file manager's is. The first file is the terminal program's side.
func openRawPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	terminal, tty, err := pty.Open()
	if err != nil {
		t.Skip("cannot open a pseudo terminal:", err)
	}
	t.Cleanup(func() {
		terminal.Close()
		tty.Close()
	})
	if _, err := term.MakeRaw(int(tty.Fd())); err != nil {
		t.Fatal(err)
	}
	return terminal, tty
}

// A terminal that never answers, while signals keep interrupting the wait:
// the wait still ends at the deadline, not later.
func TestTerminalQueryDeadlineHoldsUnderSignals(t *testing.T) {
	_, tty := openRawPty(t)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, unix.SIGWINCH)
	defer signal.Stop(signals)

	const timeout = 300 * time.Millisecond
	type result struct {
		elapsed time.Duration
		waits   int
	}
	thread := make(chan int, 1)
	done := make(chan result, 1)
	go func() {
		// Keep the waits on one thread so the signals can be sent to it.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		thread <- unix.Gettid()
		start := time.Now()
		waits := 0
		collectTerminalReplies(timeout, func(wait time.Duration) ([]byte, bool) {
			waits++
			return readTerminalInput(int(tty.Fd()), wait)
		})
		done <- result{time.Since(start), waits}
	}()

	tid := <-thread
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				unix.Tgkill(os.Getpid(), tid, unix.SIGWINCH)
			}
		}
	}()

	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		close(stop)
		t.Fatal("waiting for the terminal did not end")
	}
	close(stop)

	if r.elapsed < timeout || r.elapsed > timeout+150*time.Millisecond {
		t.Fatalf("took %s, want about %s", r.elapsed, timeout)
	}
	// The signals did interrupt the waits; otherwise this proves nothing.
	if r.waits < 10 {
		t.Fatalf("only %d waits; the signals did not interrupt them", r.waits)
	}
}

// A terminal that goes away: reading stops at once instead of waiting out
// the deadline.
func TestTerminalQueryStopsWhenTerminalHangsUp(t *testing.T) {
	terminal, tty := openRawPty(t)
	terminal.Close()

	start := time.Now()
	reply := collectTerminalReplies(time.Second, func(wait time.Duration) ([]byte, bool) {
		return readTerminalInput(int(tty.Fd()), wait)
	})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("took %s after the terminal hung up", elapsed)
	}
	if reply != nil {
		t.Fatalf("read %q", reply)
	}
}

// A terminal that answers: detection finishes as soon as the replies arrive.
func TestDetectSixelReadsReplies(t *testing.T) {
	terminal, tty := openRawPty(t)
	go func() {
		// Answer once all the queries have arrived.
		var queries []byte
		buf := make([]byte, 64)
		for !bytes.Contains(queries, []byte("\x1b[c")) {
			n, err := terminal.Read(buf)
			if err != nil {
				return
			}
			queries = append(queries, buf[:n]...)
		}
		terminal.Write([]byte("\x1b[6;30;12t\x1b[4;600;960t\x1b[?62;4;22c"))
	}()

	start := time.Now()
	got := detectSixel(tty, int(tty.Fd()), 80, 20)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("took %s with a terminal that answers", elapsed)
	}
	if got != (sixelTerminal{supported: true, cellW: 12, cellH: 30}) {
		t.Fatalf("got %+v", got)
	}
}
