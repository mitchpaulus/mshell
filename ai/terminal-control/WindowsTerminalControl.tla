---------------------- MODULE WindowsTerminalControl ----------------------
EXTENDS TLC

VARIABLES phase, shellRead, childRead, inputOwner, consoleMode,
          ctrlGroup, failure

vars == <<phase, shellRead, childRead, inputOwner, consoleMode,
          ctrlGroup, failure>>

Init ==
    /\ phase = "idle"
    /\ shellRead = "outstanding"
    /\ childRead = "none"
    /\ inputOwner = "shell"
    /\ consoleMode = "shellRaw"
    /\ ctrlGroup = "none"
    /\ failure = "none"

BeginHandoff ==
    /\ phase = "idle"
    /\ shellRead = "outstanding"
    /\ shellRead' = "cancelPending"
    /\ phase' = "quiescing"
    /\ UNCHANGED <<childRead, inputOwner, consoleMode, ctrlGroup, failure>>

ShellReadQuiesces ==
    /\ phase = "quiescing"
    /\ shellRead = "cancelPending"
    /\ shellRead' = "none"
    /\ consoleMode' = "shellCooked"
    /\ phase' = "ready"
    /\ UNCHANGED <<childRead, inputOwner, ctrlGroup, failure>>

QuiesceFails ==
    /\ phase = "quiescing"
    /\ shellRead = "cancelPending"
    /\ shellRead' = "outstanding"
    /\ phase' = "failed"
    /\ failure' = "quiesce"
    /\ UNCHANGED <<childRead, inputOwner, consoleMode, ctrlGroup>>

CreateChildGroup ==
    /\ phase = "ready"
    /\ shellRead = "none"
    /\ ctrlGroup' = "job"
    /\ phase' = "created"
    /\ UNCHANGED <<shellRead, childRead, inputOwner, consoleMode, failure>>

CreateFails ==
    /\ phase = "ready"
    /\ phase' = "reclaiming"
    /\ failure' = "create"
    /\ UNCHANGED <<shellRead, childRead, inputOwner, consoleMode, ctrlGroup>>

ActivateChild ==
    /\ phase = "created"
    /\ shellRead = "none"
    /\ inputOwner = "shell"
    /\ inputOwner' = "job"
    /\ childRead' = "outstanding"
    /\ consoleMode' = "jobMode"
    /\ phase' = "foreground"
    /\ UNCHANGED <<shellRead, ctrlGroup, failure>>

ChildReadCompletes ==
    /\ phase = "foreground"
    /\ childRead = "outstanding"
    /\ childRead' = "none"
    /\ UNCHANGED <<phase, shellRead, inputOwner, consoleMode, ctrlGroup, failure>>

ChildStartsAnotherRead ==
    /\ phase = "foreground"
    /\ childRead = "none"
    /\ childRead' = "outstanding"
    /\ UNCHANGED <<phase, shellRead, inputOwner, consoleMode, ctrlGroup, failure>>

ChildStopsOrExits ==
    /\ phase = "foreground"
    /\ childRead \in {"none", "outstanding"}
    /\ childRead' = "none"
    /\ phase' = "reclaiming"
    /\ UNCHANGED <<shellRead, inputOwner, consoleMode, ctrlGroup, failure>>

Reclaim ==
    /\ phase = "reclaiming"
    /\ shellRead = "none"
    /\ childRead = "none"
    /\ inputOwner' = "shell"
    /\ consoleMode' = "shellRaw"
    /\ ctrlGroup' = "none"
    /\ shellRead' = "outstanding"
    /\ phase' = "done"
    /\ UNCHANGED <<childRead, failure>>

Next == BeginHandoff \/ ShellReadQuiesces \/ QuiesceFails \/
        CreateChildGroup \/ CreateFails \/ ActivateChild \/
        ChildReadCompletes \/ ChildStartsAnotherRead \/
        ChildStopsOrExits \/ Reclaim

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"idle", "quiescing", "ready", "created", "foreground",
                   "reclaiming", "failed", "done"}
    /\ shellRead \in {"none", "outstanding", "cancelPending"}
    /\ childRead \in {"none", "outstanding"}
    /\ inputOwner \in {"shell", "job"}
    /\ consoleMode \in {"shellRaw", "shellCooked", "jobMode"}
    /\ ctrlGroup \in {"none", "job"}
    /\ failure \in {"none", "quiesce", "create"}

NoCompetingReads ==
    ~(shellRead = "outstanding" /\ childRead = "outstanding")

ShellReadRequiresOwnership ==
    shellRead = "outstanding" => inputOwner = "shell"

ChildReadRequiresOwnership ==
    childRead = "outstanding" => inputOwner = "job"

ChildActivationRequiresQuiescence ==
    inputOwner = "job" => shellRead = "none"

CtrlGroupIsNotInputOwnership ==
    ctrlGroup = "job" /\ phase = "created" => inputOwner = "shell"

=============================================================================
