# msh server on Windows: ConPTY experiments

Goal: find out whether the Unix design of `msh server` (`mshell/Server.go`) carries over to a Windows pseudoconsole (ConPTY).
The Unix design depends on two things:

- The engine's own stdout and every child's output go to one PTY.
- Private OSC markers (`ESC ] 6973 ; nonce ; S|E id BEL`) written into that PTY come out of the master side
  exactly where they were written, between the block's output bytes.

Machine: Windows 11 build 26300.9711, x64. Go 1.24 with `golang.org/x/sys` v0.34.0 (already in `mshell/go.mod`).
The experiment is a throwaway program in `%TEMP%\msh-conpty` (`main.go`), not part of the repository.

## E1: do markers pass through ConPTY in order?

A child started in a pseudoconsole writes text and markers in a known order:
text, `S1`, text, `E1`, text without a newline, `S2`, text, a screen clear, `E2`,
then the alternate screen, 40 lines of scrolling output, and back-to-back markers.

### The ConPTY built into Windows (kernel32 `CreatePseudoConsole`)

- Markers come through intact, byte for byte.
- **They are not in order.**
  Conhost passes an escape sequence it doesn't know through at once,
  but text goes into its screen buffer and is drawn later, on its own schedule.
  `S1`, `E1` and `S2` all came out before `inside-1`, the text written between `S1` and `E1`.
- **Conhost redraws its own screen.**
  Output is not the program's bytes: lines get `ESC [ K` appended, clears are expanded,
  and a later block's stream repaints earlier blocks' text (`after-clear`, from block 2, was redrawn inside block 3).
  The GUI gives each block a terminal of its own, so cards would show other blocks' text.
- Flag `0x8` changed nothing: this build ignores it.

So the built-in ConPTY can't carry the Unix design.

### Microsoft's ConPTY package (`conpty.dll` + `OpenConsole.exe`)

`Microsoft.Windows.Console.ConPTY` on NuGet, version 1.25.260930003 (2026-10-02, stable), MIT, from microsoft/terminal.
The same API as kernel32's, prefixed `Conpty`; `conpty.dll` starts the `OpenConsole.exe` beside it.
Its header has no pass-through flag (`0x8` is now a glyph-width flag),
consistent with this ConPTY passing programs' escape sequences through rather than redrawing.
Also of interest: `ConptyClearPseudoConsole`.

Result (run by Mitchell, `%TEMP%\msh-conpty\run-new.msh`): **markers come out exactly in order, and output is the program's own bytes.**
No redraws, no added `ESC [ K`; clears, the alternate screen and scrolling pass through as written.
Every marker sits exactly between the text written before and after it, including back-to-back markers and one after text with no newline.

At startup it writes `ESC [ 1 t`, a primary device attributes query (`ESC [ c`),
and turns on focus reporting (`?1004h`) and its Win32 input mode (`?9001h`), turning the last two off when it closes.
Nothing answered the query in the experiment and it still worked;
the engine should answer it itself, so startup never waits on a timeout.

## E2: can the engine join the pseudoconsole it creates?

A process can only be attached to a pseudoconsole when it is started, but the engine is started by the GUI.
The test: the engine creates the pseudoconsole, starts a holder process in it, then `FreeConsole` and `AttachConsole(holder pid)`,
opens `CONOUT$`, and makes it its stdout and stderr (`SetStdHandle` and `os.Stdout`).

- Works. `AttachConsole` fails with `ERROR_GEN_FAILURE` until the holder has connected to the console; retrying every 10 ms succeeds within 1 or 2 tries.
- `CONOUT$` comes up in mode `0x7` (processed output, wrap at end of line, VT processing).
- The engine's own writes and a child it starts (`cmd /c echo`, inheriting the console) both come out of the pseudoconsole's output pipe.
- The engine must `FreeConsole` before closing the pseudoconsole: closing waits for every attached process to leave.
- With the newer ConPTY the order is exact here too:
  `engine-before`, `S1`, `engine-inside`, `from-cmd-grandchild`, `E1`, `engine-after`.
  With the built-in one, `E1` came out before the grandchild's output.

## Gotchas found

- A process started into a pseudoconsole inherits the parent's standard handles when those are redirected (pipes, as the engine's are),
  so its output bypasses the pseudoconsole entirely.
  Set `STARTF_USESTDHANDLES` with null handles.
- Close the pseudoconsole only after detaching from it (E2).

## Conclusion

The Unix design carries over to Windows on Microsoft's newer ConPTY (`conpty.dll` + `OpenConsole.exe`), and not on the one built into Windows:
in-stream markers, and the engine attached to the pseudoconsole through a holder process, so the evaluator doesn't change.

## In msh server (`mshell/Server_windows.go`)

Running the real engine (`mshell/Server_windows_test.go`) taught two more things:

- The pseudoconsole writes `ESC [ 1 t` when it is created,
  but the rest of its handshake (the device attributes query, `?1004h`, `?9001h`) only once the first process connects.
  The engine waits for it after attaching, answers the query, and drops the handshake.
  Waiting before attaching ran into its 2 s timeout every time.
- Before each program it starts, the pseudoconsole writes `ESC [ ? 7 l ESC [ ? 7 h ESC [ 6 n`: a cursor position query.
  Programs run without an answer, but msh-gui's terminals answer it, as they do every query.
- `cmd.exe` takes a variable longer than 8191 characters as empty, so a long `PATH` breaks its program search.
  An `init.msh` that prepends to `PATH` in each nested msh session can get there (8660 characters seen); it isn't the engine's doing.
