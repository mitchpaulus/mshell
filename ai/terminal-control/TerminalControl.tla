-------------------------- MODULE TerminalControl --------------------------
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS Jobs, NoJob

ASSUME /\ Jobs # {}
       /\ NoJob \notin Jobs

JobStates == {"idle", "preparing", "running", "stopped", "exited", "failed"}
Places == {"none", "foreground", "background"}
Phases == {"idle", "resolving", "ready", "spawning", "activating",
           "active", "reclaiming", "resuming", "done"}
Owners == Jobs \cup {"shell"}
Modes == {"shellRaw", "shellCooked", "jobMode"}

VARIABLES jobState, place, phase, terminalOwner, foregroundJob,
          controlJob, shellReader, terminalMode

vars == <<jobState, place, phase, terminalOwner, foregroundJob, controlJob,
          shellReader, terminalMode>>

Init ==
    /\ jobState = [j \in Jobs |-> "idle"]
    /\ place = [j \in Jobs |-> "none"]
    /\ phase = [j \in Jobs |-> "idle"]
    /\ terminalOwner = "shell"
    /\ foregroundJob = NoJob
    /\ controlJob = NoJob
    /\ shellReader = "active"
    /\ terminalMode = "shellRaw"

BeginForeground(j) ==
    /\ jobState[j] = "idle"
    /\ terminalOwner = "shell"
    /\ shellReader = "active"
    /\ foregroundJob = NoJob
    /\ controlJob = NoJob
    /\ jobState' = [jobState EXCEPT ![j] = "preparing"]
    /\ place' = [place EXCEPT ![j] = "foreground"]
    /\ phase' = [phase EXCEPT ![j] = "resolving"]
    /\ controlJob' = j
    /\ UNCHANGED <<terminalOwner, foregroundJob, shellReader, terminalMode>>

ResolveForeground(j) ==
    /\ jobState[j] = "preparing"
    /\ place[j] = "foreground"
    /\ phase[j] = "resolving"
    /\ phase' = [phase EXCEPT ![j] = "ready"]
    /\ UNCHANGED <<jobState, place, terminalOwner, foregroundJob, controlJob,
                    shellReader, terminalMode>>

PauseAndPrepare(j) ==
    /\ phase[j] = "ready"
    /\ place[j] = "foreground"
    /\ controlJob = j
    /\ terminalOwner = "shell"
    /\ foregroundJob = NoJob
    /\ shellReader = "active"
    /\ shellReader' = "paused"
    /\ terminalMode' = "shellCooked"
    /\ phase' = [phase EXCEPT ![j] =
         IF jobState[j] = "preparing" THEN "spawning" ELSE "activating"]
    /\ UNCHANGED <<jobState, place, terminalOwner, foregroundJob, controlJob>>

Spawn(j) ==
    /\ jobState[j] = "preparing"
    /\ phase[j] = "spawning"
    /\ shellReader = "paused"
    /\ jobState' = [jobState EXCEPT ![j] = "running"]
    /\ phase' = [phase EXCEPT ![j] = "activating"]
    /\ UNCHANGED <<place, terminalOwner, foregroundJob, controlJob,
                    shellReader, terminalMode>>

Activate(j) ==
    /\ jobState[j] \in {"running", "stopped"}
    /\ place[j] = "foreground"
    /\ phase[j] = "activating"
    /\ controlJob = j
    /\ terminalOwner = "shell"
    /\ foregroundJob = NoJob
    /\ shellReader = "paused"
    /\ jobState' = [jobState EXCEPT ![j] = "running"]
    /\ phase' = [phase EXCEPT ![j] = "active"]
    /\ terminalOwner' = j
    /\ foregroundJob' = j
    /\ terminalMode' = "jobMode"
    /\ UNCHANGED <<place, controlJob, shellReader>>

ForegroundStops(j) ==
    /\ terminalOwner = j
    /\ foregroundJob = j
    /\ jobState[j] = "running"
    /\ phase[j] = "active"
    /\ jobState' = [jobState EXCEPT ![j] = "stopped"]
    /\ phase' = [phase EXCEPT ![j] = "reclaiming"]
    /\ UNCHANGED <<place, terminalOwner, foregroundJob, controlJob,
                    shellReader, terminalMode>>

ForegroundExits(j) ==
    /\ terminalOwner = j
    /\ foregroundJob = j
    /\ jobState[j] = "running"
    /\ phase[j] = "active"
    /\ jobState' = [jobState EXCEPT ![j] = "exited"]
    /\ phase' = [phase EXCEPT ![j] = "reclaiming"]
    /\ UNCHANGED <<place, terminalOwner, foregroundJob, controlJob,
                    shellReader, terminalMode>>

FailLaunch(j) ==
    /\ jobState[j] = "preparing"
    /\ controlJob = j
    /\ phase[j] \in {"resolving", "ready", "spawning", "activating"}
    /\ jobState' = [jobState EXCEPT ![j] = "failed"]
    /\ phase' = [phase EXCEPT ![j] =
         IF shellReader = "paused" THEN "reclaiming" ELSE "done"]
    /\ controlJob' = IF shellReader = "paused" THEN j ELSE NoJob
    /\ UNCHANGED <<place, terminalOwner, foregroundJob, shellReader, terminalMode>>

Reclaim(j) ==
    /\ phase[j] = "reclaiming"
    /\ place[j] = "foreground"
    /\ controlJob = j
    /\ shellReader = "paused"
    /\ terminalOwner \in {"shell", j}
    /\ foregroundJob \in {NoJob, j}
    /\ terminalOwner' = "shell"
    /\ foregroundJob' = NoJob
    /\ terminalMode' = "shellCooked"
    /\ phase' = [phase EXCEPT ![j] = "resuming"]
    /\ UNCHANGED <<jobState, place, controlJob, shellReader>>

ResumeShell(j) ==
    /\ phase[j] = "resuming"
    /\ controlJob = j
    /\ terminalOwner = "shell"
    /\ foregroundJob = NoJob
    /\ shellReader = "paused"
    /\ shellReader' = "active"
    /\ terminalMode' = "shellRaw"
    /\ phase' = [phase EXCEPT ![j] = "done"]
    /\ controlJob' = NoJob
    /\ UNCHANGED <<jobState, place, terminalOwner, foregroundJob>>

BeginBackground(j) ==
    /\ jobState[j] = "idle"
    /\ terminalOwner = "shell"
    /\ shellReader = "active"
    /\ jobState' = [jobState EXCEPT ![j] = "running"]
    /\ place' = [place EXCEPT ![j] = "background"]
    /\ phase' = [phase EXCEPT ![j] = "active"]
    /\ UNCHANGED <<terminalOwner, foregroundJob, controlJob, shellReader, terminalMode>>

BackgroundStops(j) ==
    /\ jobState[j] = "running"
    /\ place[j] = "background"
    /\ jobState' = [jobState EXCEPT ![j] = "stopped"]
    /\ UNCHANGED <<place, phase, terminalOwner, foregroundJob, controlJob,
                    shellReader, terminalMode>>

ContinueBackground(j) ==
    /\ jobState[j] = "stopped"
    /\ place[j] = "background"
    /\ jobState' = [jobState EXCEPT ![j] = "running"]
    /\ UNCHANGED <<place, phase, terminalOwner, foregroundJob, controlJob,
                    shellReader, terminalMode>>

RequestForeground(j) ==
    /\ jobState[j] \in {"running", "stopped"}
    /\ place[j] = "background"
    /\ terminalOwner = "shell"
    /\ shellReader = "active"
    /\ foregroundJob = NoJob
    /\ controlJob = NoJob
    /\ place' = [place EXCEPT ![j] = "foreground"]
    /\ phase' = [phase EXCEPT ![j] = "ready"]
    /\ controlJob' = j
    /\ UNCHANGED <<jobState, terminalOwner, foregroundJob,
                    shellReader, terminalMode>>

Finish(j) ==
    /\ phase[j] = "done"
    /\ jobState[j] \in {"exited", "failed"}
    /\ jobState' = [jobState EXCEPT ![j] = "idle"]
    /\ place' = [place EXCEPT ![j] = "none"]
    /\ phase' = [phase EXCEPT ![j] = "idle"]
    /\ UNCHANGED <<terminalOwner, foregroundJob, controlJob, shellReader, terminalMode>>

Next == \E j \in Jobs:
    BeginForeground(j) \/ ResolveForeground(j) \/ PauseAndPrepare(j) \/
    Spawn(j) \/ Activate(j) \/ ForegroundStops(j) \/ ForegroundExits(j) \/
    FailLaunch(j) \/ Reclaim(j) \/ ResumeShell(j) \/ BeginBackground(j) \/
    BackgroundStops(j) \/ ContinueBackground(j) \/ RequestForeground(j) \/
    Finish(j)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ jobState \in [Jobs -> JobStates]
    /\ place \in [Jobs -> Places]
    /\ phase \in [Jobs -> Phases]
    /\ terminalOwner \in Owners
    /\ foregroundJob \in Jobs \cup {NoJob}
    /\ controlJob \in Jobs \cup {NoJob}
    /\ shellReader \in {"active", "paused"}
    /\ terminalMode \in Modes

ShellReadRequiresOwnership ==
    shellReader = "active" => terminalOwner = "shell"

PromptIsSafe ==
    shellReader = "active" =>
        /\ foregroundJob = NoJob
        /\ terminalMode = "shellRaw"

OwnerMatchesForeground ==
    terminalOwner \in Jobs =>
        /\ foregroundJob = terminalOwner
        /\ place[terminalOwner] = "foreground"
        /\ shellReader = "paused"

NoBackgroundTerminalOwner ==
    \A j \in Jobs: place[j] = "background" => terminalOwner # j

SingleForegroundJob ==
    Cardinality({j \in Jobs: place[j] = "foreground" /\ phase[j] = "active"}) <= 1

ControlTransactionIsExclusive ==
    controlJob # NoJob =>
        /\ place[controlJob] = "foreground"
        /\ phase[controlJob] \in {"resolving", "ready", "spawning",
                                  "activating", "active", "reclaiming", "resuming"}

=============================================================================
