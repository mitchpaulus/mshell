# Requirements and invariants

## Vocabulary

A **job** is one command or one pipeline and is the unit of foreground,
background, stop, continue, signal, wait, and terminal ownership state.

An **endpoint** is the fully resolved source or destination of a standard stream:
terminal, pipe, file, capture buffer, inherited abstract reader/writer, or null.
Terminal-control decisions use resolved endpoints and the controlling-terminal
handle; they must never be inferred solely from `os.Stdin`.

Terminal **ownership** is permission to consume terminal input.  It is distinct
from having an inherited handle, being a Windows console process group, and being
the target of a control event.

## Safety requirements

1. At most one job, or the shell, may be authorized to consume terminal input.
2. The shell input reader is quiescent before a foreground job can consume input.
3. The shell cannot display an interactive prompt unless it owns the terminal,
   has no foreground job, and has restored its input mode.
4. A background job never owns terminal input.
5. All processes in a POSIX pipeline belong to the job's process group before the
   group is relied on for terminal access or signaling.
6. Every foreground transition is transactional: failure either leaves the shell
   in its prior usable state or enters an explicit recovery state.  Errors from
   ownership, mode, process-group, wait, or restoration operations are not lost.
7. The shell reclaims the terminal after a foreground job exits, stops, or fails
   to launch, before resuming its reader.
8. Terminal modes are saved and restored by owner.  A stopped foreground job's
   modes are retained for a later `fg`; the shell's modes are restored for the
   prompt.
9. Standard handles are resolved before process creation.  Only intended handles
   are inherited, and every parent copy of a child-only pipe end is closed on all
   success and failure paths so EOF remains observable.
10. Output bytes and terminal responses produced while a child owns the terminal
    are not interpreted as shell keystrokes.  The terminal emulator, not mshell's
    input lexer, interprets child output escape sequences.
11. Stop, continue, exit, and signal state is tracked at job and process level;
    partial pipeline completion does not destroy the job prematurely.
12. Shutdown and cancellation terminate or detach jobs according to an explicit
    policy and do not leak goroutines, handles, processes, or terminal modes.
13. The shell transfers terminal ownership only while its own process group is
    the terminal's current foreground process group (`tcgetpgrp == getpgrp`),
    the same gate bash and fish apply.  A shell that shares a terminal it does
    not own — one of several parallel shells under `redo`/`make -j`, or a
    backgrounded script — runs its children without a foreground transaction.
14. The recorded previous foreground process group is a snapshot of a pgid, not
    a stable handle: it may belong to a sibling's transient child and be dead by
    hand-back time.  A failed hand-back (ESRCH) is bookkeeping, not a command
    failure; the shell falls back to restoring its own process group, and a
    child's successful exit status is never overridden by reclaim errors.
    (Both added 2026-08-13 after parallel `redo` builds hit the reclaim race.)

## Progress requirements

Under explicit fairness assumptions that the operating system eventually
returns from non-hung calls and that a child eventually stops or exits after the
required external event:

- every foreground job eventually stops, exits, or is reported as unrecoverable;
- every completed process is eventually reaped;
- every terminal-recovery state eventually returns ownership to the shell or
  reports a terminal-loss condition; and
- pipe readers eventually observe data or EOF after all writers close.

Progress properties are conditional.  An arbitrary child can intentionally run
forever, ignore events, or retain a pipe handle; the shell cannot prove otherwise.

## Required support profiles

- POSIX interactive shell with a controlling terminal.
- POSIX non-interactive execution with no controlling terminal.
- POSIX piped mshell stdin plus a child endpoint opened from the controlling
  terminal (the original editor case).
- Windows classic attached console, whose input buffer is shared by processes.
- Windows redirected standard handles while a console remains attached.
- Windows ConPTY hosting, with independently serviced synchronous channels.
- Nested shells and shells started outside the foreground process group.
