# Implementation status

Last updated: 2026-08-09.

## Implemented

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
- A foreground Windows command with terminal-backed stdin, stdout, and stderr
  runs in a per-command ConPTY.  The host relays VT input/output without parsing
  it and propagates outer-console size changes.
- The Windows child is created suspended, assigned to a kill-on-close Job
  Object, and only then resumed.  This closes the process-tree escape race.
- The Windows input relay is pinned to one OS thread and cancelled with
  `CancelSynchronousIo` before shell input is released.  The output relay stays
  active during `ClosePseudoConsole` to avoid the documented shutdown deadlock.
- Windows commands with a redirected stream or pipeline endpoint retain normal
  `os/exec` handles.  This preserves separate stdout/stderr and pipeline bytes,
  which cannot be represented by ConPTY's one merged output channel.

## Verification currently passing

- Controller unit tests inject mode capture, acquisition, continue, rollback,
  ownership restoration, and mode restoration failures and check operation order
  and input-gate state.
- A deadline-protected PTY test runs msh logic with piped shell stdin while the
  child reads `/dev/tty`.  It kills the entire test session on timeout.
- A second PTY test covers an interactive pipeline stage and terminal-handle
  lifetime through pipeline completion.
- Linux package tests pass.
- Windows amd64 and arm64 test binaries cross-compile.
- The Windows test binary includes a native ConPTY lifecycle test whose helper
  process has a hard deadline.  It skips when no Windows console is attached.
- All five bounded TLC configurations pass after the implementation changes.
  `StreamLifecycle` was strengthened with the retained-terminal-handle invariant
  discovered during implementation.

## Not yet implemented

- Durable job objects and user-facing `jobs`, `fg`, and `bg` operations.
- Stopped/continued process aggregation and terminal-mode snapshots per stopped
  job.
- Asynchronous reaping for background jobs.
- Windows Job Objects for redirected commands, multi-process pipelines, and
  background jobs.  Those stream shapes currently use the direct-console/
  ordinary-handle compatibility profile.
- Native Windows validation of the ConPTY input relay with full-screen editors,
  Ctrl+C, resize, and forced process-tree teardown.
- Mechanical trace replay or refinement checking between Go and TLA+.
- Conditional liveness checks and unbounded TLAPS proofs.

These remaining items are the future full-job-control program.  The current
milestone fixes synchronous foreground handoff without pretending that the full
program is complete.
