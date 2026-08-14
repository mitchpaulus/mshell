# Implementation status

Last updated: 2026-08-13.

## Implemented

### 2026-08-13 parallel-shell hardening

Parallel `redo` builds exposed a cross-process race the 2026-08-09 milestone
missed: with several mshells sharing one PTY, each shell's recorded "previous
foreground process group" could be a sibling's transient child group, dead by
release time, so the restoring `tcsetpgrp` failed with ESRCH and the command
was failed even though its child exited 0.  Changes:

- Acquisition is gated on `tcgetpgrp(tty) == getpgrp()` (the bash/fish gate).
  A shell that is not the terminal's current foreground owner skips the
  foreground transaction entirely; its child runs as an ordinary background
  process group.  On Windows the gate is always open because the foreground
  state is an in-process Ctrl-C marker, not shared kernel ownership.
- A failed hand-back to the recorded previous group falls back to restoring
  the shell's own process group (the group bash and fish restore).  Only if
  both targets fail does `Release` report an error.
- Reclaim errors are warnings on stderr, never command failures; the child's
  exit status always stands.  This supersedes the earlier "restore the exact
  previous foreground marker or fail" behavior.
- Policy change: after a double restore failure the shell input gate is now
  released rather than held.  Both targets failing means the terminal itself
  is unusable (for example a closed PTY); holding the gate wedged every later
  command behind a terminal that no longer exists.  Mode-restore failure with
  a live terminal still blocks shell input as before.

### 2026-08-13 errno audit

ERRNO_AUDIT.md traces every errno of every syscall the controller makes to an
explicit policy or a finding.  Two further gaps found and fixed:

- `term.Restore` (`tcsetattr`) ran without SIGTTOU protection and could stop
  the shell when the terminal had been handed back to a live foreign group;
  it is now wrapped in `IgnoreSignalsForJobControl` like the tcsetpgrp calls.
- A fast pipeline whose stages were reaped concurrently could have its whole
  process group vanish before acquisition; the resulting ESRCH killed and
  failed a job whose children exited 0.  Acquisition-time ESRCH now means
  "already finished": roll back any partial handoff and run without a lease.

R1 has since been applied (closeTerminal failure is a warning, never a
pipeline failure) and R3 modeled (reaped-vs-zombie states in
POSIXTerminalControl.tla).  R2 remains open pending a maintainer decision;
the trade-off is recorded in ERRNO_AUDIT.md — degrading like bash is only
hang-safe once stopped children are recoverable (`jobs`/`fg`) or when the
errno proves the terminal is dead.

### 2026-08-09 milestone

- Resolved stdin/stdout/stderr metadata remains attached to the `exec.Cmd`
  streams after redirects and merges are applied.
- Terminal selection distinguishes an arbitrary TTY from the session's
  controlling terminal and prefers resolved stdin, then stdout and stderr.
- One serialized foreground transaction spans acquisition, `SIGCONT`, wait, and
  reclamation, corresponding to the model's `controlJob` reservation.
- Terminal/console modes are captured before transfer and restored only after
  ownership is reclaimed.  Shell input remains blocked if either restoration
  step fails.
- The shell input gate rejects a foreground acquisition while an input read is
  outstanding and rejects shell reads while a foreground job owns input.
- POSIX single commands and pipelines use the resolved controlling-terminal
  descriptor rather than `os.Stdin`.
- A process group that may have stopped on `SIGTTIN` between `Start` and
  `tcsetpgrp` receives `SIGCONT` only after it is foreground.
- Pipeline stages retain a duplicated terminal handle through reclamation.  The
  full test suite exposed the original borrowed-handle version closing too early;
  the retained-handle implementation fixes that counterexample.
- Acquisition failure kills and reaps the immediate child or pipeline processes
  instead of waiting forever on a stopped terminal reader.
- Windows direct-console control-handler installation errors are reported, and
  the same serialized input gate prevents mshell from competing with a
  foreground child for the shared console queue.
- Windows and POSIX restore the exact previous foreground marker/process group
  recorded during acquisition.
- Foreground Windows commands inherit their resolved console and standard
  handles directly.  The surrounding console host remains responsible for
  keyboard input, output, escape sequences, Unicode, and resize events while
  mshell stops reading and waits.
- A nested foreground ConPTY was implemented and then removed because it made
  mshell an unnecessary terminal proxy.  ConPTY remains a possible future
  isolation mechanism for interactive background jobs, not the foreground
  handoff mechanism.

Verification added with the hardening:

- A PTY integration test starts six shells sharing one PTY, each running a
  staggered short external command, and requires all six to succeed.  Before
  the fix it reproduced the production failure exactly (three to four of six
  failing with "no such process" on reclaim).
- Controller unit tests cover the skipped acquisition for a non-owner shell,
  the fallback restore order (dead previous group, then own group), and the
  combined error when both hand-back targets fail.
- `POSIXTerminalControl.tla` now models the foreign owner, the steal window,
  the dying previous-owner group, the ownership gate, and the fallback; all
  six bounded TLC configurations pass, including the new shared-terminal
  profile.  See MODEL_RESULTS.md.

## Verification currently passing

- Controller unit tests inject mode capture, acquisition, continue, rollback,
  ownership restoration, and mode restoration failures and check operation order
  and input-gate state.
- A deadline-protected PTY test runs msh logic with piped shell stdin while the
  child reads `/dev/tty`.  It kills the entire test session on timeout.
- A second PTY test covers an interactive pipeline stage and terminal-handle
  lifetime through pipeline completion.
- Linux package tests pass.
- Windows amd64 and macOS amd64 test binaries cross-compile.
- All five bounded TLC configurations pass after the implementation changes.
  `StreamLifecycle` was strengthened with the retained-terminal-handle invariant
  discovered during implementation.

## Not yet implemented

- Durable job objects and user-facing `jobs`, `fg`, and `bg` operations.
- Stopped/continued process aggregation and terminal-mode snapshots per stopped
  job.
- Asynchronous reaping for background jobs.
- Per-job Windows ConPTY isolation, relay workers, resizing, and Job Object
  cleanup.  These are future background-job facilities; foreground jobs should
  continue to use direct console inheritance.
- Mechanical trace replay or refinement checking between Go and TLA+.
- Conditional liveness checks and unbounded TLAPS proofs.

These remaining items are the future full-job-control program.  The current
milestone fixes synchronous foreground handoff without pretending that the full
program is complete.
