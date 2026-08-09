# TLC results

Tool: TLA+ tools 1.7.4, TLC 2.19 (`5a47802`), Java 17.

Last complete run: 2026-08-09.

| Model | Configured size | Distinct states | Result |
| --- | ---: | ---: | --- |
| `TerminalControl` | 2 jobs | 204 | all configured invariants hold |
| `POSIXTerminalControl` | 2 processes; shell stdin non-TTY, child stdin TTY | 58 | all configured invariants hold |
| `POSIXTerminalControl` | 2 processes; child stdin non-TTY | 2 | all configured invariants hold |
| `WindowsTerminalControl` | one foreground job | 11 | all configured invariants hold |
| `StreamLifecycle` | 2 processes, 5 abstract handles | 36 | all configured invariants hold |

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

## Boundaries and unfinished proof work

- These are exhaustive finite-state safety checks, not unbounded TLAPS proofs.
- The current platform modules are contract models, not yet mechanically checked
  refinement mappings to `TerminalControl`.
- Progress requirements are documented but not checked yet.  Fairness must be
  stated carefully because children are allowed to run forever or retain handles.
- The Windows model covers reader quiescence in a shared console.  A separate,
  larger ConPTY relay model is required before claiming full Windows isolation.
- Terminal resize, nested shells, signal masks, per-process stopped/continued
  aggregation, mode-snapshot failure, and shutdown policy need additional state.
- The Go code has not yet been proven to implement these transitions.  A single
  controller plus trace-replay tests is the next conformance step.

Accordingly, the present result should be described as a checked formal design
foundation, not as “mshell terminal handling is formally verified.”
