# Terminal control formalization

This directory is the persistent design and formal-model workspace for mshell's
standard-I/O, terminal-control, and job-control implementation.

## Long-term scope

The target is **full job control**, even though mshell does not expose all of it
today.  Every design choice and implementation step must preserve a path to:

- foreground and background jobs;
- multi-process pipelines treated as one job;
- stop, continue, `fg`, and `bg` transitions;
- terminal-mode save and restore for both the shell and stopped jobs;
- correct signal or console-control-event routing;
- asynchronous reaping and durable job status; and
- interactive programs whose terminal endpoint differs from mshell's inherited
  standard input, including an explicit `/dev/tty`-style input while mshell is
  reading a pipe.

This requirement is intentional and must not be narrowed to the current
`brename`/editor failure when this work is resumed.

## What is proved

The TLA+ models describe the control protocol, not the implementations of the
operating-system calls.  TLC exhaustively checks the configured finite models.
A successful TLC run means that the stated invariants hold for every modeled
interleaving and failure in that finite state space, subject to the contracts in
[ASSUMPTIONS.md](ASSUMPTIONS.md).  It is not a proof that an arbitrary Go
implementation conforms to the model, nor a proof of the operating systems.

The model suite is deliberately split:

- `TerminalControl.tla` models platform-neutral job and terminal ownership.
- `POSIXTerminalControl.tla` models process groups and the controlling terminal.
- `WindowsTerminalControl.tla` models the shared console input queue and Ctrl
  event limitations.
- `StreamLifecycle.tla` models standard-handle resolution, inheritance, closure,
  and EOF.  This is kept separate to control state-space growth.

Run every bounded check with `./check.sh`.  Set `TLA2TOOLS_JAR` to use a jar in a
different location.

Production conformance progress is tracked in
[IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md).
The chosen Windows foreground architecture and the rejected nested-ConPTY
alternative are recorded in
[WINDOWS_DIRECT_HANDOFF.md](WINDOWS_DIRECT_HANDOFF.md).

## Proof roadmap

1. Keep the TLC models executable alongside implementation work and add every
   discovered race as a modeled transition.
2. Introduce a single Go terminal/job controller whose states and operations map
   directly to the abstract actions.
3. Add model-based and pseudo-terminal integration tests, including injected
   failures and deadlines that kill hung process trees.
4. Write refinement mappings from the POSIX and Windows models to the abstract
   model.
5. Use TLAPS for unbounded invariant proofs after the state machines stabilize.
6. Consider Gobra contracts for the critical Go controller.  System calls remain
   trusted contracts, so this supplements rather than replaces the TLA+ work.
