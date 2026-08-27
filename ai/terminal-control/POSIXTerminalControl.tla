----------------------- MODULE POSIXTerminalControl -----------------------
EXTENDS FiniteSets, TLC

(***************************************************************************)
(* The 2026-08-13 parallel-runner incident (redo -j spawning many mshells  *)
(* that share one PTY) showed the original model's universe was closed: it *)
(* assumed the shell starts as the terminal's foreground owner, that only  *)
(* the shell and its job ever own the terminal, and that the reclaiming    *)
(* tcsetpgrp cannot fail.  This revision adds the environment:             *)
(*                                                                         *)
(*   - OtherPgrp: a foreign process group in the same session (a parallel  *)
(*     runner, a sibling shell, or a sibling's transient child group);     *)
(*   - ShellStartsForeground = FALSE: the shell may not own the terminal;  *)
(*   - a steal window between the ownership check and tcsetpgrp, and       *)
(*     while the job runs (siblings ignore SIGTTOU, so their tcsetpgrp     *)
(*     always succeeds);                                                   *)
(*   - OtherGroupExits: the recorded previous owner is a snapshot of a     *)
(*     pgid, not a stable handle, and can be dead at hand-back time; and   *)
(*   - the two production fixes: a shell that does not own the terminal    *)
(*     skips the handoff entirely, and a hand-back that fails because the  *)
(*     recorded group died falls back to the shell's own group.            *)
(*                                                                         *)
(* Deliberate boundary: the steal window is limited to the phases where    *)
(* the discovered race lives ("ownedReady" and "foreground").  A fully     *)
(* adversarial environment that can steal at any time makes every reader-  *)
(* ownership invariant unsatisfiable; in reality the kernel answers that   *)
(* case with SIGTTIN/SIGTTOU, which this model does not simulate for the   *)
(* shell's own reads.                                                      *)
(*                                                                         *)
(* Revision 2 (errno audit finding F3): processes may exit as soon as they *)
(* are launched, and pipelines reap concurrently, so the job's own group   *)
(* can be fully reaped before the handoff.  "exited" is a zombie — it      *)
(* still holds its pgid, so tcsetpgrp to the group succeeds — while        *)
(* "reaped" does not, and tcsetpgrp to a fully reaped group fails with     *)
(* the (undocumented on Linux) ESRCH.  The production fix models as        *)
(* GiveTerminalTargetGone: an ESRCH handoff means the job already          *)
(* finished, so the shell runs it unsupervised instead of failing it.      *)
(***************************************************************************)

CONSTANTS Procs, ShellPgrp, JobPgrp, OtherPgrp,
          ShellStartsForeground, ShellStdinIsTTY, ChildStdinIsTTY

ASSUME /\ Procs # {}
       /\ ShellPgrp # JobPgrp
       /\ OtherPgrp \notin {ShellPgrp, JobPgrp}
       /\ ShellStartsForeground \in BOOLEAN
       /\ ShellStdinIsTTY \in BOOLEAN
       /\ ChildStdinIsTTY \in BOOLEAN

VARIABLES phase, shellReader, ttyForeground, procState, grouped,
          terminalMode, failure, otherAlive, savedPrev

vars == <<phase, shellReader, ttyForeground, procState, grouped,
          terminalMode, failure, otherAlive, savedPrev>>

ProcStates == {"idle", "running", "stopped", "exited", "reaped", "startFailed"}

NoSave == "none"

\* "exited" is a zombie: it still occupies its pgid, so the group remains a
\* valid tcsetpgrp/kill target.  Only when every started member is reaped does
\* the group cease to exist (kernel returns ESRCH).
GroupDead == \A p \in Procs: procState[p] \in {"reaped", "startFailed"}

Init ==
    /\ phase = "idle"
    /\ shellReader = "active"
    /\ ttyForeground = IF ShellStartsForeground THEN ShellPgrp ELSE OtherPgrp
    /\ procState = [p \in Procs |-> "idle"]
    /\ grouped = [p \in Procs |-> FALSE]
    /\ terminalMode = "shellRaw"
    /\ failure = "none"
    /\ otherAlive = TRUE
    /\ savedPrev = NoSave

ResolveTerminalEndpoint ==
    /\ phase = "idle"
    /\ ChildStdinIsTTY
    /\ phase' = "resolved"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

ResolveNonTerminalEndpoint ==
    /\ phase = "idle"
    /\ ~ChildStdinIsTTY
    /\ phase' = "done"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

PauseShell ==
    /\ phase = "resolved"
    /\ shellReader = "active"
    /\ shellReader' = "paused"
    /\ terminalMode' = "shellCooked"
    /\ phase' = "launching"
    /\ UNCHANGED <<ttyForeground, procState, grouped, failure,
                    otherAlive, savedPrev>>

StartProc(p) ==
    /\ phase = "launching"
    /\ procState[p] = "idle"
    \* Go's child-side Setpgid contract makes membership effective before exec.
    /\ procState' = [procState EXCEPT ![p] = "running"]
    /\ grouped' = [grouped EXCEPT ![p] = TRUE]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, terminalMode, failure,
                    otherAlive, savedPrev>>

StartProcFails(p) ==
    /\ phase = "launching"
    /\ procState[p] = "idle"
    /\ procState' = [procState EXCEPT ![p] = "startFailed"]
    /\ grouped' = [grouped EXCEPT ![p] = FALSE]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, terminalMode, failure,
                    otherAlive, savedPrev>>

LaunchComplete ==
    /\ phase = "launching"
    /\ \A p \in Procs: procState[p] # "idle"
    \* A member that already exited (or was even reaped) still counts as
    \* launched; the barrier counts launches, not survivors.
    /\ \E p \in Procs: procState[p] \in {"running", "exited", "reaped"}
    /\ phase' = "groupReady"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

AllStartFailed ==
    /\ phase = "launching"
    /\ \A p \in Procs: procState[p] = "startFailed"
    /\ phase' = "reclaiming"
    /\ failure' = "start"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, otherAlive, savedPrev>>

\* The production gate: the foreground transaction runs only when the shell's
\* own process group currently owns the terminal (tcgetpgrp == getpgrp).
CheckOwnershipPasses ==
    /\ phase = "groupReady"
    /\ ttyForeground = ShellPgrp
    /\ phase' = "ownedReady"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

\* A non-owner shell skips the handoff entirely: no tcsetpgrp, no mode save,
\* no reclamation.  Its child runs as an ordinary background process group.
CheckOwnershipFails ==
    /\ phase = "groupReady"
    /\ ttyForeground # ShellPgrp
    /\ phase' = "unsupervised"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

\* A sibling in the session hands the terminal to its own child.  Siblings
\* ignore SIGTTOU while doing so, so this succeeds regardless of the current
\* owner.  This is the TOCTOU window after our ownership check, and it can
\* also happen while our job is foreground.
ExternalTakesTerminal ==
    /\ phase \in {"ownedReady", "foreground"}
    /\ otherAlive
    /\ ttyForeground # OtherPgrp
    /\ ttyForeground' = OtherPgrp
    /\ UNCHANGED <<phase, shellReader, procState, grouped, terminalMode,
                    failure, otherAlive, savedPrev>>

\* A pgid recorded at acquisition is a snapshot, not a stable handle: the
\* foreign group can exit at any time, making a later hand-back fail (ESRCH).
OtherGroupExits ==
    /\ otherAlive
    /\ otherAlive' = FALSE
    /\ UNCHANGED <<phase, shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, savedPrev>>

\* tcsetpgrp succeeds even if a sibling stole the terminal inside the window;
\* the snapshot of the previous owner is whatever tcgetpgrp returned then.
\* The ~GroupDead guard is the kernel contract, not a shell decision: a group
\* kept alive by zombies is a valid target, a fully reaped one is ESRCH.
GiveTerminal ==
    /\ phase = "ownedReady"
    /\ shellReader = "paused"
    /\ ~GroupDead
    /\ \A p \in Procs: procState[p] = "running" => grouped[p]
    /\ savedPrev' = ttyForeground
    /\ ttyForeground' = JobPgrp
    /\ terminalMode' = "jobMode"
    /\ phase' = "foreground"
    /\ UNCHANGED <<shellReader, procState, grouped, failure, otherAlive>>

\* Errno-audit fix F3: the handoff tcsetpgrp fails with ESRCH because the
\* job's own group was fully reaped (fast pipeline, concurrent reaping).  The
\* job already finished; the shell runs it unsupervised instead of killing a
\* job whose children exited 0.  The terminal was not touched.
GiveTerminalTargetGone ==
    /\ phase = "ownedReady"
    /\ shellReader = "paused"
    /\ GroupDead
    /\ phase' = "unsupervised"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

TcsetpgrpFails ==
    /\ phase = "ownedReady"
    /\ phase' = "reclaiming"
    /\ failure' = "tcsetpgrp"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, otherAlive, savedPrev>>

\* A process may exit the instant it is launched, well before the shell
\* checks ownership or hands the terminal over (errno-audit finding F3).
ProcExits(p) ==
    /\ phase \in {"launching", "groupReady", "ownedReady",
                   "foreground", "unsupervised"}
    /\ procState[p] = "running"
    /\ procState' = [procState EXCEPT ![p] = "exited"]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

\* Pipelines reap concurrently (per-stage goroutines), so a zombie can turn
\* into a fully reaped process at any point, including mid-transaction.  The
\* single-command path reaps only after release; the model checks the worst
\* case.
ReapProc(p) ==
    /\ procState[p] = "exited"
    /\ procState' = [procState EXCEPT ![p] = "reaped"]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

JobStops ==
    /\ phase = "foreground"
    /\ \E p \in Procs: procState[p] = "running"
    /\ procState' = [p \in Procs |->
         IF procState[p] = "running" THEN "stopped" ELSE procState[p]]
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellReader, ttyForeground, grouped, terminalMode, failure,
                    otherAlive, savedPrev>>

JobExited ==
    /\ phase = "foreground"
    /\ \A p \in Procs: procState[p] \in {"exited", "reaped", "startFailed"}
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

\* An unsupervised (skipped-handoff) job completes without the shell touching
\* terminal ownership or modes.  A child that read the terminal would be
\* stopped by SIGTTIN, which this model leaves out of scope.
UnsupervisedComplete ==
    /\ phase = "unsupervised"
    /\ \A p \in Procs: procState[p] \in {"exited", "reaped", "startFailed"}
    /\ phase' = "resuming"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

\* No handoff happened (start failure or failed tcsetpgrp), so there is no
\* previous owner to restore.
ReclaimNoHandoff ==
    /\ phase = "reclaiming"
    /\ shellReader = "paused"
    /\ savedPrev = NoSave
    /\ terminalMode' = "shellCooked"
    /\ phase' = "resuming"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped, failure,
                    otherAlive, savedPrev>>

\* Hand the terminal back to the recorded previous owner while it is alive.
ReclaimRestoresPrev ==
    /\ phase = "reclaiming"
    /\ shellReader = "paused"
    /\ savedPrev # NoSave
    /\ (savedPrev = ShellPgrp \/ (savedPrev = OtherPgrp /\ otherAlive))
    /\ ttyForeground' = savedPrev
    /\ terminalMode' = "shellCooked"
    /\ phase' = "resuming"
    /\ UNCHANGED <<shellReader, procState, grouped, failure,
                    otherAlive, savedPrev>>

\* The recorded previous owner died: the hand-back tcsetpgrp fails with ESRCH.
\* This is bookkeeping, not a command failure.
ReclaimHandBackFails ==
    /\ phase = "reclaiming"
    /\ shellReader = "paused"
    /\ savedPrev = OtherPgrp
    /\ ~otherAlive
    /\ phase' = "reclaimFallback"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure, otherAlive, savedPrev>>

\* The production fallback: restore the shell's own group, which always
\* exists.  This is also the group bash and fish restore unconditionally.
ReclaimFallbackToOwnGroup ==
    /\ phase = "reclaimFallback"
    /\ ttyForeground' = ShellPgrp
    /\ terminalMode' = "shellCooked"
    /\ phase' = "resuming"
    /\ UNCHANGED <<shellReader, procState, grouped, failure,
                    otherAlive, savedPrev>>

\* A foreground-owner shell resumes its reader only once it owns the terminal
\* again.  If reclamation legitimately restored a live foreign owner, the
\* reader stays paused; resuming to read a terminal owned by someone else is
\* the SIGTTIN case outside this model.  A shell that never owned the terminal
\* resumes reading its (possibly non-terminal) stdin without touching modes.
ResumeShell ==
    /\ phase = "resuming"
    /\ ShellStartsForeground => ttyForeground = ShellPgrp
    /\ shellReader' = "active"
    /\ terminalMode' = IF ShellStartsForeground THEN "shellRaw" ELSE terminalMode
    /\ phase' = "done"
    /\ UNCHANGED <<ttyForeground, procState, grouped, failure,
                    otherAlive, savedPrev>>

Next ==
    ResolveTerminalEndpoint \/ ResolveNonTerminalEndpoint \/ PauseShell \/
    (\E p \in Procs: StartProc(p) \/ StartProcFails(p) \/ ProcExits(p)
                     \/ ReapProc(p)) \/
    LaunchComplete \/ AllStartFailed \/
    CheckOwnershipPasses \/ CheckOwnershipFails \/
    ExternalTakesTerminal \/ OtherGroupExits \/
    GiveTerminal \/ GiveTerminalTargetGone \/ TcsetpgrpFails \/
    JobStops \/ JobExited \/ UnsupervisedComplete \/
    ReclaimNoHandoff \/ ReclaimRestoresPrev \/ ReclaimHandBackFails \/
    ReclaimFallbackToOwnGroup \/ ResumeShell

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"idle", "resolved", "launching", "groupReady", "ownedReady",
                   "foreground", "unsupervised", "reclaiming",
                   "reclaimFallback", "resuming", "done"}
    /\ shellReader \in {"active", "paused"}
    /\ ttyForeground \in {ShellPgrp, JobPgrp, OtherPgrp}
    /\ procState \in [Procs -> ProcStates]
    /\ grouped \in [Procs -> BOOLEAN]
    /\ terminalMode \in {"shellRaw", "shellCooked", "jobMode"}
    /\ failure \in {"none", "start", "tcsetpgrp"}
    /\ otherAlive \in BOOLEAN
    /\ savedPrev \in {NoSave, ShellPgrp, OtherPgrp}

\* Meaningful only for a shell that owns its terminal; a shell sharing a
\* terminal it does not own cannot enforce anything about foreign ownership.
ShellReaderHasForeground ==
    ShellStartsForeground =>
        (shellReader = "active" => ttyForeground = ShellPgrp)

JobForegroundPausesShell ==
    ttyForeground = JobPgrp => shellReader = "paused"

RunningProcessesAreGroupedBeforeForeground ==
    ttyForeground = JobPgrp =>
        \A p \in Procs: procState[p] = "running" => grouped[p]

PromptModeIsRestored ==
    ShellStartsForeground =>
        (shellReader = "active" => terminalMode = "shellRaw")

ResolvedChildEndpointControlsHandoff ==
    ~ChildStdinIsTTY => phase \notin {"resolved", "launching", "groupReady",
                                     "ownedReady", "foreground",
                                     "unsupervised", "reclaiming",
                                     "reclaimFallback", "resuming"}

\* The gate's guarantee: a shell that never owned the terminal never
\* foregrounds a job, never applies job terminal modes, and never records a
\* previous owner it would later have to restore.
NonOwnerShellNeverForegrounds ==
    ~ShellStartsForeground =>
        /\ ttyForeground # JobPgrp
        /\ terminalMode # "jobMode"
        /\ savedPrev = NoSave

\* Both skip paths (non-owner shell, ESRCH on a reaped group) run the job
\* without ever pointing the terminal at it or applying job terminal modes.
UnsupervisedJobNeverOwnsTerminal ==
    phase = "unsupervised" =>
        /\ ttyForeground # JobPgrp
        /\ terminalMode # "jobMode"

=============================================================================
