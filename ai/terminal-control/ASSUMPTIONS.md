# Trusted platform contracts

Checked against primary documentation on 2026-08-09.  These are assumptions at
the formal model boundary and must be revisited when supported Go or operating
system versions change.

## POSIX Issue 8

- A controlling terminal records one foreground process-group ID.  Terminal
  access by background process groups is governed by the terminal driver and can
  stop readers with `SIGTTIN` (and writers with `SIGTTOU` when `TOSTOP` applies).
- `tcsetpgrp()` requires a terminal associated with the caller's session and a
  process group in that session.  It can fail, including when the caller is a
  background group and does not appropriately block or ignore `SIGTTOU`.
- Each job is placed in its own process group.  The POSIX rationale recommends
  calling `setpgid()` in both child and parent to close the fork/exec race.
- A shell foregrounds a job with `tcsetpgrp()`, observes stopped children with
  `waitpid(..., WUNTRACED)`, reclaims the terminal, and foregrounds a stopped job
  before sending `SIGCONT`.

Sources:

- https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html#tag_19_11
- https://pubs.opengroup.org/onlinepubs/9699919799/functions/tcsetpgrp.html
- https://pubs.opengroup.org/onlinepubs/009604599/xrat/xbd_chap03.html
- https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap03.html

## Windows console

- Any number of processes can share one console and its queued input buffer.
  Windows has no POSIX-equivalent kernel foreground process group that gates
  reads.  Therefore mshell must make its own reader quiescent before allowing a
  foreground child to read the shared queue.
- `CREATE_NEW_PROCESS_GROUP` creates a control-event group, not an input-owner
  group.  It disables Ctrl+C in the new group.  `CTRL_BREAK_EVENT` can target the
  group; `CTRL_C_EVENT` cannot be limited to a nonzero group.
- the SetConsoleCtrlHandler Ctrl+C-ignore attribute is inherited.  Handler tables
  are reset by `AttachConsole`, `AllocConsole`, and `FreeConsole`.
- Console modes belong to console buffers, so processes sharing a buffer observe
  changes to that shared state.  Mode changes need owner-scoped save/restore.
- A ConPTY uses synchronous input and output channels.  Microsoft recommends a
  separate servicing thread for each direction to avoid deadlock.  The host must
  close its copies of handles given to the pseudoconsole after child creation so
  broken-channel/EOF detection works.  Teardown output must continue to be
  drained while closing the pseudoconsole.
- Windows handle inheritance requires both an inheritable handle and inheritance
  at `CreateProcess`; `PROC_THREAD_ATTRIBUTE_HANDLE_LIST` restricts the exact
  inherited set.  Inherited handles refer to the same underlying objects.

Sources:

- https://learn.microsoft.com/en-us/windows/console/consoles
- https://learn.microsoft.com/en-us/windows/console/console-input-buffer
- https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags
- https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent
- https://learn.microsoft.com/en-us/windows/console/setconsolectrlhandler
- https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session
- https://learn.microsoft.com/en-us/windows/win32/procthread/inheritance

## Go process creation

The current development toolchain reports Go 1.26.5.  Its local source is the
authoritative implementation reference for this checkout.

- `exec.Cmd` copies non-`*os.File` stdin/stdout/stderr through goroutines; `Wait`
  also waits for those copy goroutines under the documented rules.
- On Unix, `syscall.SysProcAttr` exposes `Setpgid`, `Pgid`, and `Foreground`.
  `Foreground` performs foreground placement in the child-side creation path and
  requires a parent controlling-terminal descriptor in `Ctty`.
- On Windows, `syscall.SysProcAttr` exposes `CreationFlags`,
  `AdditionalInheritedHandles`, and `NoInheritHandles`; Go uses an extended
  startup-info handle list for the selected inherited handles.
- `os.Process.Signal(os.Interrupt)` is not implemented on Windows.  Windows
  control events require platform-specific code and the limitations above.

Sources:

- https://pkg.go.dev/os/exec
- https://pkg.go.dev/syscall#SysProcAttr
- `/usr/lib/go/src/os/exec/exec.go`
- `/usr/lib/go/src/syscall/exec_linux.go`
- `/usr/lib/go/src/syscall/exec_windows.go`
