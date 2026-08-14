# TLC results

Tool: TLA+ tools 1.7.4, TLC 2.19 (`5a47802`), Java 17.

Last complete run: 2026-08-13.

| Model | Configured size | Distinct states | Result |
| --- | ---: | ---: | --- |
| `TerminalControl` | 2 jobs | 204 | all configured invariants hold |
| `POSIXTerminalControl` | 2 processes; shell non-owner start allowed as below; shell stdin non-TTY, child stdin TTY | 284 | all configured invariants hold |
| `POSIXTerminalControl` | 2 processes; child stdin non-TTY | 4 | all configured invariants hold |
| `POSIXTerminalControl` (`POSIXSharedTerminal.cfg`) | 2 processes; shell does not own the terminal; child stdin TTY | 62 | all configured invariants hold |
| `WindowsTerminalControl` | one foreground job | 11 | all configured invariants hold |
| `StreamLifecycle` | 2 processes, 5 stream handles, retained terminal | 36 | all configured invariants hold |

`check.sh` uses `-deadlock` because terminal `done` and explicitly failed states
are expected to have no enabled action.  Invariant checking still explores their
complete reachable state graph.

## Counterexample found during modeling

The first `TerminalControl` version did not serialize acquisition transactions.
TLC found this trace:

1. job 1 begins a foreground launch;
2. job 2 begins in the background and requests `fg`;
3. job 2 pauses the shell;
4. job 1 fails and enters recovery;
5. job 2 acquires the terminal, stops, reclaims it, and resumes the shell; and
6. job 1 remains forever in a recovery state that can no longer satisfy its
   preconditions.

The model now has a single `controlJob` reservation spanning resolution through
recovery.  This is a design requirement for the future Go controller, not merely
a modeling convenience.

## Counterexample found in production (2026-08-13)

Parallel `redo` builds (several mshells sharing one PTY) hit a race the models
could not represent, because the modeled universe was closed:

- `Init` fixed `ttyForeground = ShellPgrp`: the shell was assumed to start as
  the terminal's foreground owner.
- `TypeOK` restricted `ttyForeground` to `{ShellPgrp, JobPgrp}`: no foreign
  process group existed, so no sibling could own or steal the terminal and no
  recorded previous owner could be anything but the shell itself.
- `ReclaimTerminal` could not fail: restoring `ShellPgrp` always succeeds, so
  the ESRCH hand-back failure had no modeled transition, and the implementation
  question "what severity is a failed restore?" was never posed.

Inside that universe, "restore the exact previous foreground process group" and
"restore your own process group" are indistinguishable — the previous owner is
always the shell.  The implementation chose the former; outside the model the
previous owner can be a sibling's transient child group that is dead by release
time.  REQUIREMENTS.md had listed "shells started outside the foreground
process group" as a required profile, and the boundaries section below listed
nested shells as missing state, but no configuration exercised either.

`POSIXTerminalControl.tla` now models the environment: a foreign process group
(`OtherPgrp`) that may own the terminal from the start
(`ShellStartsForeground = FALSE`), may steal it in the window between the
ownership check and `tcsetpgrp` and while the job is foreground, and may exit
at any time, making the recorded previous owner a dead pgid.  The two
production fixes are modeled as transitions: `CheckOwnershipFails` (a
non-owner shell skips the handoff) and
`ReclaimHandBackFails`/`ReclaimFallbackToOwnGroup` (an ESRCH hand-back falls
back to the shell's own group).  The new invariant
`NonOwnerShellNeverForegrounds` fails within seconds if the ownership gate is
removed from the model (verified by mutation).

## Boundaries and unfinished proof work

- These are exhaustive finite-state safety checks, not unbounded TLAPS proofs.
- The current platform modules are contract models, not yet mechanically checked
  refinement mappings to `TerminalControl`.
- The models cannot yet express early reaping: process exit is only enabled
  after the foreground/unsupervised split, and there is no zombie-vs-reaped
  distinction, so "the job's group vanishes before acquisition" (finding F3
  in ERRNO_AUDIT.md, handled in code by treating acquisition-time ESRCH as
  already-finished) has no modeled transition.  Needs a `reaped` process
  state and earlier `ProcExits` enabling.
- The external-steal window is deliberately limited to the `ownedReady` and
  `foreground` phases.  A fully adversarial environment that can take the
  terminal at any time makes every reader-ownership invariant unsatisfiable;
  the kernel answers that case with `SIGTTIN`/`SIGTTOU`, and the shell's own
  stopped-by-signal states are not modeled.  Reclaiming to a live foreign
  owner leaves the model in a `resuming` dead end for the same reason.
- Progress requirements are documented but not checked yet.  Fairness must be
  stated carefully because children are allowed to run forever or retain handles.
- The Windows model covers reader quiescence in a shared console.  A separate,
  larger ConPTY relay model is required before claiming full Windows isolation.
- Terminal resize, signal masks, per-process stopped/continued aggregation,
  mode-snapshot failure, and shutdown policy need additional state.
- The Go controller and failure-injection tests mirror the principal ownership
  transitions, but no refinement checker has mechanically proved that every Go
  execution implements the TLA+ specification.  Generated trace replay remains
  a next conformance step.

The stream model now also requires a duplicated controlling-terminal handle to
remain live from endpoint resolution through job completion and to be closed in
terminal states.  This invariant was added after the Go integration tests found
that retaining only a borrowed pipeline descriptor allowed a stage to close it
before terminal reclamation.

Accordingly, the present result should be described as a checked formal design
foundation, not as “mshell terminal handling is formally verified.”
