# Errno audit of the terminal/job-control syscall surface

Date: 2026-08-13.
Sources: Linux man-pages on the development machine (man-pages 6.x), POSIX
Issue 8, observed production behavior, and kernel behavior where the man pages
are incomplete.  Method: enumerate every syscall the controller makes, list
every documented (and observed-undocumented) errno, and trace each to an
explicit policy in code and, where applicable, a transition in the TLA+
models.  A row with no policy is a finding.

Verdicts: **OK** (explicit, correct policy), **FIXED** (gap found by this
audit, fixed 2026-08-13), **REC** (works, but a better policy is recommended
below), **N/A** (cannot occur at this call site, with reasoning).

## POSIX backend

### tcsetpgrp — `TIOCSPGRP` (`SetForegroundProcessGroup`, `RestoreForegroundProcessGroup`)

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| `ESRCH` (undocumented) | Target pgid fully reaped.  Linux's `TIOCSPGRP` returns this; the tcsetpgrp man page does not list it.  Observed in production 2026-08-13. | Acquire: skip the handoff (nothing left to foreground).  Release: fall back to own group, warn only. | FIXED / OK |
| `EPERM` | Target pgid valid but not in caller's session. | Acquire: kill child, fail command.  Release: fallback to own group (own group cannot be `EPERM`). | REC (R2) / OK |
| `ENOTTY` | fd no longer the caller's controlling terminal (hangup or dissociation after the resolve-time `CanControlTerminal` probe). | Acquire: kill child, fail command.  Release: fallback also fails → warn, unblock input gate; later commands re-probe and skip. | REC (R2) / OK |
| `EBADF`, `EINVAL` | Bad fd / bad pgid value.  Program error; fd is held via a duplicated handle in pipelines. | Same as `ENOTTY` paths. | OK |
| SIGTTOU (not an errno) | Caller is in a background group and does not ignore/block SIGTTOU: default action stops the shell. | Both call sites wrapped in `IgnoreSignalsForJobControl`. | OK |

### tcgetpgrp — `TIOCGPGRP` (`CanControlTerminal`, `ShellOwnsTerminal`, first step of `SetForegroundProcessGroup`)

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| `ENOTTY`, `EBADF` | Not the controlling terminal / bad fd. | Resolve probe: endpoint is nil, no transaction.  Ownership gate: reports non-owner, transaction skipped.  Inside acquire: acquire fails (see tcsetpgrp rows). | OK |

Read-only; does not raise SIGTTOU.

### kill(-pgid, SIGCONT) — `ContinueProcessGroup`

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| `ESRCH` | Group fully reaped between `tcsetpgrp` and `SIGCONT`.  A group whose members are zombies still exists (man kill: an existing process may be a zombie), so the single-command path — which reaps only after acquisition — cannot hit this; pipelines reap concurrently in per-stage goroutines and can. | Roll back the handoff, then skip: return no lease, no error.  The job already finished. | FIXED |
| `EPERM` | Cannot signal any member.  POSIX exempts `SIGCONT` within the sender's session, and the child group is always in our session. | Falls into the generic continue-failure path (rollback + fail). | N/A in practice, policy exists |
| `EINVAL` | Bad signal number. | Impossible (constant `SIGCONT`). | N/A |

### kill(-pgid, SIGKILL) — `KillProcessGroup`

Called only on already-failed launch/acquisition paths; the return value is
deliberately ignored at every call site (best-effort cleanup; `ESRCH` here
means the work is already done).  **OK** — now documented as intentional.

### tcgetattr — `TCGETS` (`CaptureTerminalMode`)

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| `ENOTTY`, `EBADF` | Terminal hung up between resolve and acquire. | Acquire fails before any state is touched; rollback is trivial.  Evaluator kills the child and fails the command. | REC (R2) |

Read-only; does not raise SIGTTOU.

### tcsetattr — `TCSETSF` via `term.Restore` (`posixTerminalModeSnapshot.Restore`)

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| SIGTTOU (not an errno) | POSIX: `tcsetattr` from a background process group raises SIGTTOU; default action stops the process.  Reachable whenever the restore runs while another group owns the terminal (e.g. Release handed the terminal back to a live foreign group recorded during the steal window). | Restore is now wrapped in `IgnoreSignalsForJobControl`, like the tcsetpgrp calls. | FIXED |
| `ENOTTY`, `EBADF`, `EIO`, `EINTR`, `EINVAL` | Terminal gone or hung up. | Release: warn; input gate stays blocked only when the terminal is otherwise alive (mode restored wrong is a real input hazard); on total terminal loss the gate is released. | OK |
| Partial success (returns 0) | man termios: `tcsetattr` "returns success if any of the requested changes could be carried out." | No policy possible without a verify-readback; accepted contract gap, recorded here. | OK (accepted) |

### setpgid — child-side via `SysProcAttr` (leader `Pgid=0`, followers join leader)

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| `EPERM` (target group does not exist) | Follower joins after the leader's group died. | Prevented structurally: the leader waits for every stage to launch before it can be reaped (launch barrier).  Residual failure surfaces as that stage's `cmd.Start` error → per-stage exit code. | OK |
| `EACCES`, `ESRCH`, `EINVAL` | Post-exec / wrong pid / negative pgid. | Not reachable from the child-side pre-exec call with Go's contract. | N/A |

### waitpid — via `os/exec.Cmd.Wait`

`EINTR` is retried inside the Go runtime; `ECHILD` cannot occur because Go
owns reaping for started commands.  Non-`ExitError` failures map to
`ExitStartUnknown` with a printed diagnostic.  **OK**.

### dup / close — `DuplicateTerminalHandle`, `CloseTerminalHandle` (pipeline terminal retention)

| Errno | Trigger | Policy | Verdict |
| --- | --- | --- | --- |
| dup: `EMFILE`, `ENOMEM`, `EBADF` | fd exhaustion / bad fd. | `registerTerminal` fails before the stage starts; command fails fast, nothing orphaned. | OK |
| close: `EBADF`, `EINTR`, `EIO` | On Linux the fd is closed even when `close` reports `EINTR`/`EIO`; the code correctly does not retry. | `closeTerminal` error is a stderr warning; the pipeline's own result stands. | FIXED (was R1) |

### SIGTTOU/SIGTTIN protection — `IgnoreSignalsForJobControl`

`signal.Ignore`/`signal.Reset` are process-wide and not reentrant: a nested
wrap would drop protection at the inner `Reset`.  All current uses are
sequential and disjoint (verified).  Recorded as a constraint for future code.

## Windows backend

The direct-console backend has a much smaller failure surface: the
"foreground" is an in-process marker, so `RestoreForegroundProcessGroup` is
infallible and `ESRCH`/SIGTTOU have no analogue.

- `SetConsoleCtrlHandler` (inside `SetForegroundProcessGroup`): failure fails
  acquisition; `KillProcessGroup` is a no-op, so only the immediate child is
  killed — already documented in the code as awaiting Job Object support.
- `GetConsoleMode` capture skips per-handle failures and errors only when no
  handle yields a mode; `SetConsoleMode` restore applies every saved mode and
  reports the first failure.  Reasonable; console modes are shared state that
  other processes can also change (see the closed-world note in
  MODEL_RESULTS.md).

## Findings summary

- **F1 (documented):** Linux `TIOCSPGRP` returns `ESRCH` for a reaped group;
  the man page omits it.  Recorded in ASSUMPTIONS.md.
- **F2 (fixed):** `term.Restore` (`tcsetattr`) ran without SIGTTOU protection;
  a release that handed the terminal to a live foreign group could stop the
  shell mid-`Release`.  Now wrapped like the tcsetpgrp calls.
- **F3 (fixed):** a fast pipeline whose stages were reaped concurrently could
  be fully gone before acquisition; the resulting `ESRCH` from
  `tcsetpgrp`/`SIGCONT` killed and failed a job whose children exited 0.
  `ESRCH` during acquisition now means "already finished, run without a
  handoff" (with rollback where the handoff partially happened).

Resolutions (maintainer decisions 2026-08-13):

- **R1 (applied):** `closeTerminal` failure is now a warning; a successful
  pipeline is never failed over it.
- **R2 (open, trade-off analysis delivered):** non-`ESRCH` acquisition
  failures (`ENOTTY`/`EIO` after a hangup in the resolve→acquire window,
  `EPERM`) kill a healthy child and fail the command.  bash degrades to
  running the job without foreground ownership — but bash can afford to: it
  has a job table and `WUNTRACED` waits, so a child that later stops on
  `SIGTTIN` becomes a recoverable stopped job.  mshell's synchronous
  `cmd.Wait` would hang forever on such a child.  Degrading is only clearly
  safe for errnos proving the terminal is dead (`ENOTTY`/`EBADF`/`EIO`, where
  reads return `EIO` instead of raising `SIGTTIN`).  Options: keep kill
  (never hangs, predictable), degrade only on dead-terminal errnos (bash-like
  where provably hang-free), or full degrade (needs stopped-job recovery
  first — the future `jobs`/`fg` work).
- **R3 (applied, modeled):** `POSIXTerminalControl.tla` now has a `reaped`
  state distinct from zombie `exited`, early `ProcExits`, concurrent
  `ReapProc`, the `~GroupDead` kernel contract on `GiveTerminal`, and the
  `GiveTerminalTargetGone` skip transition; invariant
  `UnsupervisedJobNeverOwnsTerminal` verified non-vacuous by mutation.  See
  MODEL_RESULTS.md.
