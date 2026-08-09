---------------------- MODULE WindowsTerminalControl ----------------------
EXTENDS TLC

VARIABLES phase, shellRead, relayRead, inputOwner, consoleMode,
          conpty, job, child, outputDrain, failure

vars == <<phase, shellRead, relayRead, inputOwner, consoleMode,
          conpty, job, child, outputDrain, failure>>

Init ==
    /\ phase = "idle"
    /\ shellRead = "outstanding"
    /\ relayRead = "none"
    /\ inputOwner = "shell"
    /\ consoleMode = "shellRaw"
    /\ conpty = "none"
    /\ job = "none"
    /\ child = "none"
    /\ outputDrain = "none"
    /\ failure = "none"

BeginHandoff ==
    /\ phase = "idle"
    /\ shellRead = "outstanding"
    /\ shellRead' = "cancelPending"
    /\ phase' = "quiescing"
    /\ UNCHANGED <<relayRead, inputOwner, consoleMode, conpty, job,
                   child, outputDrain, failure>>

ShellReadQuiesces ==
    /\ phase = "quiescing"
    /\ shellRead = "cancelPending"
    /\ shellRead' = "none"
    /\ phase' = "ready"
    /\ UNCHANGED <<relayRead, inputOwner, consoleMode, conpty, job,
                   child, outputDrain, failure>>

QuiesceFails ==
    /\ phase = "quiescing"
    /\ shellRead = "cancelPending"
    /\ shellRead' = "outstanding"
    /\ phase' = "failed"
    /\ failure' = "quiesce"
    /\ UNCHANGED <<relayRead, inputOwner, consoleMode, conpty, job,
                   child, outputDrain>>

CreateConPTY ==
    /\ phase = "ready"
    /\ conpty' = "open"
    /\ outputDrain' = "active"
    /\ phase' = "conptyCreated"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, job,
                   child, failure>>

CreateJob ==
    /\ phase = "conptyCreated"
    /\ job' = "open"
    /\ phase' = "jobCreated"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, conpty,
                   child, outputDrain, failure>>

CreateSuspendedChild ==
    /\ phase = "jobCreated"
    /\ child' = "suspendedUncontained"
    /\ phase' = "childCreated"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, conpty,
                   job, outputDrain, failure>>

AssignChildToJob ==
    /\ phase = "childCreated"
    /\ child = "suspendedUncontained"
    /\ child' = "suspendedContained"
    /\ phase' = "contained"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, conpty,
                   job, outputDrain, failure>>

ActivateRelays ==
    /\ phase = "contained"
    /\ shellRead = "none"
    /\ child = "suspendedContained"
    /\ relayRead' = "outstanding"
    /\ inputOwner' = "job"
    /\ consoleMode' = "relayVT"
    /\ phase' = "relaying"
    /\ UNCHANGED <<shellRead, conpty, job, child, outputDrain, failure>>

ResumeChild ==
    /\ phase = "relaying"
    /\ child = "suspendedContained"
    /\ child' = "runningContained"
    /\ phase' = "foreground"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, conpty,
                   job, outputDrain, failure>>

RelayReadCompletes ==
    /\ phase = "foreground"
    /\ relayRead = "outstanding"
    /\ relayRead' = "none"
    /\ UNCHANGED <<phase, shellRead, inputOwner, consoleMode, conpty, job,
                   child, outputDrain, failure>>

RelayStartsAnotherRead ==
    /\ phase = "foreground"
    /\ relayRead = "none"
    /\ relayRead' = "outstanding"
    /\ UNCHANGED <<phase, shellRead, inputOwner, consoleMode, conpty, job,
                   child, outputDrain, failure>>

ChildExits ==
    /\ phase = "foreground"
    /\ child' = "exited"
    /\ relayRead' = IF relayRead = "outstanding" THEN "cancelPending" ELSE "none"
    /\ phase' = "stoppingRelays"
    /\ UNCHANGED <<shellRead, inputOwner, consoleMode, conpty, job,
                   outputDrain, failure>>

CancelInputRelay ==
    /\ phase = "stoppingRelays"
    /\ relayRead \in {"none", "cancelPending"}
    /\ relayRead' = "joined"
    /\ phase' = "closingConPTY"
    /\ UNCHANGED <<shellRead, inputOwner, consoleMode, conpty, job,
                   child, outputDrain, failure>>

DrainOutputAndCloseConPTY ==
    /\ phase = "closingConPTY"
    /\ relayRead = "joined"
    /\ outputDrain = "active"
    /\ outputDrain' = "joined"
    /\ conpty' = "closed"
    /\ phase' = "closingJob"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, job,
                   child, failure>>

CloseJob ==
    /\ phase = "closingJob"
    /\ child = "exited"
    /\ job' = "closed"
    /\ child' = "none"
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellRead, relayRead, inputOwner, consoleMode, conpty,
                   outputDrain, failure>>

Reclaim ==
    /\ phase = "reclaiming"
    /\ relayRead = "joined"
    /\ conpty = "closed"
    /\ job = "closed"
    /\ child = "none"
    /\ inputOwner' = "shell"
    /\ consoleMode' = "shellRaw"
    /\ shellRead' = "outstanding"
    /\ phase' = "done"
    /\ UNCHANGED <<relayRead, conpty, job, child, outputDrain, failure>>

CreateFails ==
    /\ phase \in {"ready", "conptyCreated", "jobCreated", "childCreated",
                    "contained", "relaying"}
    /\ failure' = phase
    /\ child' = "none"
    /\ relayRead' = "joined"
    /\ conpty' = "closed"
    /\ job' = "closed"
    /\ outputDrain' = "joined"
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellRead, inputOwner, consoleMode>>

Next == BeginHandoff \/ ShellReadQuiesces \/ QuiesceFails \/
        CreateConPTY \/ CreateJob \/ CreateSuspendedChild \/
        AssignChildToJob \/ ActivateRelays \/ ResumeChild \/
        RelayReadCompletes \/ RelayStartsAnotherRead \/ ChildExits \/
        CancelInputRelay \/ DrainOutputAndCloseConPTY \/ CloseJob \/
        Reclaim \/ CreateFails

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"idle", "quiescing", "ready", "conptyCreated",
                   "jobCreated", "childCreated", "contained", "relaying",
                   "foreground", "stoppingRelays", "closingConPTY",
                   "closingJob", "reclaiming", "failed", "done"}
    /\ shellRead \in {"none", "outstanding", "cancelPending"}
    /\ relayRead \in {"none", "outstanding", "cancelPending", "joined"}
    /\ inputOwner \in {"shell", "job"}
    /\ consoleMode \in {"shellRaw", "relayVT"}
    /\ conpty \in {"none", "open", "closed"}
    /\ job \in {"none", "open", "closed"}
    /\ child \in {"none", "suspendedUncontained", "suspendedContained",
                    "runningContained", "exited"}
    /\ outputDrain \in {"none", "active", "joined"}
    /\ failure \in {"none", "ready", "conptyCreated", "jobCreated",
                     "childCreated", "contained", "relaying", "quiesce"}

NoCompetingReads ==
    ~(shellRead = "outstanding" /\ relayRead \in {"outstanding", "cancelPending"})

ShellReadRequiresOwnership ==
    shellRead = "outstanding" => inputOwner = "shell"

ChildReadRequiresOwnership ==
    relayRead \in {"outstanding", "cancelPending"} => inputOwner = "job"

ChildActivationRequiresQuiescence ==
    inputOwner = "job" => shellRead = "none"

CtrlGroupIsNotInputOwnership ==
    job = "open" /\ phase = "jobCreated" => inputOwner = "shell"

ResumeRequiresContainment ==
    child = "runningContained" => job = "open"

RelayRequiresConPTY ==
    relayRead \in {"outstanding", "cancelPending"} => conpty = "open"

ReclaimRequiresJoinedRelays ==
    phase \in {"reclaiming", "done"} => relayRead = "joined"

JobCloseRequiresChildExit ==
    job = "closed" => child = "none"

=============================================================================
