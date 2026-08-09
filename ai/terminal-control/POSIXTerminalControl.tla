----------------------- MODULE POSIXTerminalControl -----------------------
EXTENDS FiniteSets, TLC

CONSTANTS Procs, ShellPgrp, JobPgrp, ShellStdinIsTTY, ChildStdinIsTTY

ASSUME /\ Procs # {}
       /\ ShellPgrp # JobPgrp
       /\ ShellStdinIsTTY \in BOOLEAN
       /\ ChildStdinIsTTY \in BOOLEAN

VARIABLES phase, shellReader, ttyForeground, procState, grouped,
          terminalMode, failure

vars == <<phase, shellReader, ttyForeground, procState, grouped,
          terminalMode, failure>>

ProcStates == {"idle", "running", "stopped", "exited", "startFailed"}

Init ==
    /\ phase = "idle"
    /\ shellReader = "active"
    /\ ttyForeground = ShellPgrp
    /\ procState = [p \in Procs |-> "idle"]
    /\ grouped = [p \in Procs |-> FALSE]
    /\ terminalMode = "shellRaw"
    /\ failure = "none"

ResolveTerminalEndpoint ==
    /\ phase = "idle"
    /\ ChildStdinIsTTY
    /\ phase' = "resolved"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure>>

ResolveNonTerminalEndpoint ==
    /\ phase = "idle"
    /\ ~ChildStdinIsTTY
    /\ phase' = "done"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure>>

PauseShell ==
    /\ phase = "resolved"
    /\ shellReader = "active"
    /\ shellReader' = "paused"
    /\ terminalMode' = "shellCooked"
    /\ phase' = "launching"
    /\ UNCHANGED <<ttyForeground, procState, grouped, failure>>

StartProc(p) ==
    /\ phase = "launching"
    /\ procState[p] = "idle"
    \* Go's child-side Setpgid contract makes membership effective before exec.
    /\ procState' = [procState EXCEPT ![p] = "running"]
    /\ grouped' = [grouped EXCEPT ![p] = TRUE]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, terminalMode, failure>>

StartProcFails(p) ==
    /\ phase = "launching"
    /\ procState[p] = "idle"
    /\ procState' = [procState EXCEPT ![p] = "startFailed"]
    /\ grouped' = [grouped EXCEPT ![p] = FALSE]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, terminalMode, failure>>

LaunchComplete ==
    /\ phase = "launching"
    /\ \A p \in Procs: procState[p] # "idle"
    /\ \E p \in Procs: procState[p] = "running"
    /\ phase' = "groupReady"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure>>

AllStartFailed ==
    /\ phase = "launching"
    /\ \A p \in Procs: procState[p] = "startFailed"
    /\ phase' = "reclaiming"
    /\ failure' = "start"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped, terminalMode>>

GiveTerminal ==
    /\ phase = "groupReady"
    /\ shellReader = "paused"
    /\ ttyForeground = ShellPgrp
    /\ \A p \in Procs: procState[p] = "running" => grouped[p]
    /\ ttyForeground' = JobPgrp
    /\ terminalMode' = "jobMode"
    /\ phase' = "foreground"
    /\ UNCHANGED <<shellReader, procState, grouped, failure>>

TcsetpgrpFails ==
    /\ phase = "groupReady"
    /\ phase' = "reclaiming"
    /\ failure' = "tcsetpgrp"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped, terminalMode>>

ProcExits(p) ==
    /\ phase = "foreground"
    /\ procState[p] = "running"
    /\ procState' = [procState EXCEPT ![p] = "exited"]
    /\ UNCHANGED <<phase, shellReader, ttyForeground, grouped,
                    terminalMode, failure>>

JobStops ==
    /\ phase = "foreground"
    /\ \E p \in Procs: procState[p] = "running"
    /\ procState' = [p \in Procs |->
         IF procState[p] = "running" THEN "stopped" ELSE procState[p]]
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellReader, ttyForeground, grouped, terminalMode, failure>>

JobExited ==
    /\ phase = "foreground"
    /\ \A p \in Procs: procState[p] \in {"exited", "startFailed"}
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellReader, ttyForeground, procState, grouped,
                    terminalMode, failure>>

ReclaimTerminal ==
    /\ phase = "reclaiming"
    /\ shellReader = "paused"
    /\ ttyForeground \in {ShellPgrp, JobPgrp}
    /\ ttyForeground' = ShellPgrp
    /\ terminalMode' = "shellCooked"
    /\ phase' = "resuming"
    /\ UNCHANGED <<shellReader, procState, grouped, failure>>

ResumeShell ==
    /\ phase = "resuming"
    /\ ttyForeground = ShellPgrp
    /\ shellReader' = "active"
    /\ terminalMode' = "shellRaw"
    /\ phase' = "done"
    /\ UNCHANGED <<ttyForeground, procState, grouped, failure>>

Next ==
    ResolveTerminalEndpoint \/ ResolveNonTerminalEndpoint \/ PauseShell \/
    (\E p \in Procs: StartProc(p) \/ StartProcFails(p) \/ ProcExits(p)) \/
    LaunchComplete \/ AllStartFailed \/ GiveTerminal \/ TcsetpgrpFails \/
    JobStops \/ JobExited \/ ReclaimTerminal \/ ResumeShell

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"idle", "resolved", "launching", "groupReady",
                   "foreground", "reclaiming", "resuming", "done"}
    /\ shellReader \in {"active", "paused"}
    /\ ttyForeground \in {ShellPgrp, JobPgrp}
    /\ procState \in [Procs -> ProcStates]
    /\ grouped \in [Procs -> BOOLEAN]
    /\ terminalMode \in {"shellRaw", "shellCooked", "jobMode"}
    /\ failure \in {"none", "start", "tcsetpgrp"}

ShellReaderHasForeground ==
    shellReader = "active" => ttyForeground = ShellPgrp

JobForegroundPausesShell ==
    ttyForeground = JobPgrp => shellReader = "paused"

RunningProcessesAreGroupedBeforeForeground ==
    ttyForeground = JobPgrp =>
        \A p \in Procs: procState[p] = "running" => grouped[p]

PromptModeIsRestored ==
    shellReader = "active" => terminalMode = "shellRaw"

ResolvedChildEndpointControlsHandoff ==
    ~ChildStdinIsTTY => phase \notin {"resolved", "launching", "groupReady",
                                     "foreground", "reclaiming", "resuming"}

=============================================================================
