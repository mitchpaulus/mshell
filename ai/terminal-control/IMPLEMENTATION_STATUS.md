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
- Foreground Windows commands inherit their resolved console and standard
  handles directly.  The surrounding console host remains responsible for
  keyboard input, output, escape sequences, Unicode, and resize events while
  mshell stops reading and waits.
- A nested foreground ConPTY was implemented and then removed because it made
  mshell an unnecessary terminal proxy.  ConPTY remains a possible future
  isolation mechanism for interactive background jobs, not the foreground
  handoff mechanism.

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
