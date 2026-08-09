------------------------- MODULE StreamLifecycle -------------------------
EXTENDS FiniteSets, TLC

CONSTANTS Procs, Handles, TerminalUsed

ASSUME /\ Procs # {}
       /\ Handles # {}
       /\ TerminalUsed \in BOOLEAN

VARIABLES phase, resolved, desired, inherited, parentOpen, childOpen,
          terminalRetained, procState, eofObserved, failure

vars == <<phase, resolved, desired, inherited, parentOpen, childOpen,
          terminalRetained, procState, eofObserved, failure>>

Init ==
    /\ phase = "idle"
    /\ resolved = FALSE
    /\ desired = [p \in Procs |-> {}]
    /\ inherited = [p \in Procs |-> {}]
    /\ parentOpen = Handles
    /\ childOpen = [p \in Procs |-> {}]
    /\ terminalRetained = FALSE
    /\ procState = [p \in Procs |-> "idle"]
    /\ eofObserved = FALSE
    /\ failure = "none"

Resolve ==
    /\ phase = "idle"
    \* The bounded model gives every process exactly its intended standard/pipe handles.
    /\ desired' = [p \in Procs |-> Handles]
    /\ resolved' = TRUE
    /\ terminalRetained' = TerminalUsed
    /\ phase' = "resolved"
    /\ UNCHANGED <<inherited, parentOpen, childOpen, procState,
                    eofObserved, failure>>

Start(p) ==
    /\ phase \in {"resolved", "starting"}
    /\ resolved
    /\ procState[p] = "idle"
    /\ inherited' = [inherited EXCEPT ![p] = desired[p]]
    /\ childOpen' = [childOpen EXCEPT ![p] = desired[p]]
    /\ procState' = [procState EXCEPT ![p] = "running"]
    /\ phase' = "starting"
    /\ UNCHANGED <<resolved, desired, parentOpen, terminalRetained,
                    eofObserved, failure>>

StartFails(p) ==
    /\ phase \in {"resolved", "starting"}
    /\ procState[p] = "idle"
    /\ procState' = [procState EXCEPT ![p] = "failed"]
    /\ phase' = "starting"
    /\ UNCHANGED <<resolved, desired, inherited, parentOpen, childOpen,
                    terminalRetained,
                    eofObserved, failure>>

AllStartsReported ==
    /\ phase = "starting"
    /\ \A p \in Procs: procState[p] # "idle"
    /\ phase' = "closeParentCopies"
    /\ UNCHANGED <<resolved, desired, inherited, parentOpen, childOpen,
                    terminalRetained,
                    procState, eofObserved, failure>>

CloseParentCopies ==
    /\ phase = "closeParentCopies"
    /\ parentOpen' = {}
    /\ phase' = "running"
    /\ UNCHANGED <<resolved, desired, inherited, childOpen, terminalRetained, procState,
                    eofObserved, failure>>

AbortBeforeRunning ==
    /\ phase \in {"resolved", "starting", "closeParentCopies"}
    /\ parentOpen' = {}
    /\ childOpen' = [p \in Procs |-> {}]
    /\ terminalRetained' = FALSE
    /\ procState' = [p \in Procs |->
         IF procState[p] = "running" THEN "failed" ELSE procState[p]]
    /\ phase' = "failed"
    /\ failure' = "launch"
    /\ UNCHANGED <<resolved, desired, inherited, eofObserved>>

Exit(p) ==
    /\ phase = "running"
    /\ procState[p] = "running"
    /\ childOpen' = [childOpen EXCEPT ![p] = {}]
    /\ procState' = [procState EXCEPT ![p] = "exited"]
    /\ UNCHANGED <<phase, resolved, desired, inherited, parentOpen,
                    terminalRetained,
                    eofObserved, failure>>

ObserveEOF ==
    /\ phase = "running"
    /\ parentOpen = {}
    /\ \A p \in Procs: childOpen[p] = {}
    /\ eofObserved' = TRUE
    /\ terminalRetained' = FALSE
    /\ phase' = "done"
    /\ UNCHANGED <<resolved, desired, inherited, parentOpen, childOpen,
                    procState, failure>>

Next == Resolve \/ (\E p \in Procs: Start(p) \/ StartFails(p) \/ Exit(p)) \/
        AllStartsReported \/ CloseParentCopies \/ AbortBeforeRunning \/ ObserveEOF

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"idle", "resolved", "starting", "closeParentCopies",
                   "running", "failed", "done"}
    /\ resolved \in BOOLEAN
    /\ desired \in [Procs -> SUBSET Handles]
    /\ inherited \in [Procs -> SUBSET Handles]
    /\ parentOpen \subseteq Handles
    /\ childOpen \in [Procs -> SUBSET Handles]
    /\ terminalRetained \in BOOLEAN
    /\ procState \in [Procs -> {"idle", "running", "exited", "failed"}]
    /\ eofObserved \in BOOLEAN
    /\ failure \in {"none", "launch"}

ExactInheritance ==
    \A p \in Procs: procState[p] = "running" => inherited[p] = desired[p]

NoSpawnBeforeResolution ==
    (\E p \in Procs: procState[p] = "running") => resolved

EOFIsSound ==
    eofObserved =>
        /\ parentOpen = {}
        /\ \A p \in Procs: childOpen[p] = {}

TerminalStatesLeakNoHandles ==
    phase \in {"failed", "done"} =>
        /\ parentOpen = {}
        /\ \A p \in Procs: childOpen[p] = {}

TerminalHandleLifetime ==
    TerminalUsed /\ phase \in {"resolved", "starting", "closeParentCopies", "running"}
        => terminalRetained

TerminalHandleReleasedAtEnd ==
    phase \in {"failed", "done"} => ~terminalRetained

=============================================================================
