# Terminal query model

When the file manager starts, it asks the terminal for sixel support and its cell size
(`CSI 16 t`, `CSI 14 t`, then DA1 `CSI c`) and reads the replies.
`TerminalQuery.tla` models that read loop and a hostile environment.
In the code, the loop is `collectTerminalReplies` in `mshell/FileManagerImage.go`,
and one wait and read is `readTerminalInput` in `mshell/FileManagerImage_unix.go` and `mshell/FileManagerImage_windows.go`.

Run `./check.sh`.
It uses `$TLA2TOOLS_JAR`, or `~/.local/share/java/tla2tools.jar`.

## What is modeled

The environment may, at any moment: send reply bytes (possibly never finishing the reply),
send console events that are not keys (Windows: focus, mouse, window size),
and interrupt the wait with signals (Unix).

The process's own steps take no time; the clock only advances while it is blocked.
So any time that passes is time spent waiting or reading, and the properties are about those.

## Properties

| Property | Meaning |
|---|---|
| `NeverPastDeadline` | The clock never passes the deadline while the loop is still reading. |
| `Finishes` | The loop always finishes. |
| `FinishesForAReason` | It finishes only with the whole reply, `MaxBytes` read, the deadline passed, or `MaxIter` waits made. |
| `ReadsCompleteReply` | A reply that arrives in full before the deadline, within the limits, is read in full. |

## Results

| Config | Design | Result |
|---|---|---|
| `Unix.cfg` | current | passes, 94,370 distinct states |
| `Windows.cfg` | current | passes, 287,775 distinct states |
| `UnixBefore.cfg` | before the fix | fails `NeverPastDeadline`: a signal restarted `poll` with the original timeout, moving the timeout past the deadline |
| `WindowsBefore.cfg` | before the fix | fails `NeverPastDeadline`: an event that is not a key woke the wait, then a plain read of standard input waited for a key |

`check.sh` requires both: the current design passing, and the old one still failing, so the model keeps catching those bugs.

## What the model assumes

TLC checks every ordering of events in the finite model; it does not check the system calls.
The model relies on these contracts, which the code comments also state:

- Unix: after `poll` reports `POLLIN` on a terminal in raw mode, `read` on the same descriptor returns at once.
  This holds while nothing else in the process reads the terminal, which is true while the file manager starts.
  The code reads the descriptor `poll` watched, not `os.Stdin`, so the two cannot differ.
- Windows: after `GetNumberOfConsoleInputEvents` reports events, `ReadConsoleInputW` returns at once, under the same condition.
- Waits end no later than their timeout, apart from scheduling delay.
  `waitMilliseconds` rounds up, so a wait can end up to 1 ms past the deadline.
- The clock is monotonic: Go's `time.Now` and `time.Until` use the monotonic clock, so changing the system time does not move the deadline.

## Tests that match the model

In `mshell/FileManagerImage_test.go`: the loop stops after `maxTerminalQueryWaits` instant empty wakeups,
after `maxTerminalReplyBytes`, when input fails, and at the DA1 reply,
and never asks to wait longer than the time left.

In `mshell/FileManagerImage_linux_test.go`, on a real pseudo terminal:
signals sent to the waiting thread every 2 ms do not push the wait past the deadline
(with the old retry, the wait did not end until the signals stopped),
a terminal that hangs up ends the wait at once,
and a terminal that answers ends it as soon as the replies arrive.

Not run here: the Windows path. It builds for amd64, 386, and arm64, and a compile-time check fixes the `INPUT_RECORD` layout at 20 bytes,
but it needs a run in Windows Terminal, including moving focus away and back while the file manager opens.
