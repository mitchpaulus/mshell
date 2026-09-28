--------------------------- MODULE TerminalQuery ---------------------------
(***************************************************************************)
(* The loop that reads the terminal's replies when the file manager        *)
(* starts, and everything around it that can go wrong. In the code:        *)
(* collectTerminalReplies (the loop) and readTerminalInput (the wait and   *)
(* read) in mshell/FileManagerImage*.go.                                   *)
(*                                                                         *)
(* The process:                                                            *)
(*   loop: stop if the reply is complete, MaxBytes were read, MaxIter      *)
(*         waits were made, or the deadline has passed. Otherwise wait     *)
(*         for input until the deadline.                                   *)
(*   wait: wakes when input is available, when the wait times out, when    *)
(*         a signal interrupts it (Unix), or when a console event that is  *)
(*         not a key arrives (Windows).                                    *)
(*                                                                         *)
(* The environment may, at any moment: send bytes (possibly the last one   *)
(* of the reply, or never), send console events that are not keys, and     *)
(* interrupt the wait with signals.                                        *)
(*                                                                         *)
(* Time: the process's own steps take no time. The clock only advances     *)
(* while the process is blocked: in a wait that has nothing to wake it     *)
(* and has not timed out, or in a blocking read with no bytes available.   *)
(* So any time that passes is time spent blocked.                          *)
(*                                                                         *)
(* Variant "old" is the code before this change:                           *)
(*   Unix: after a signal, poll is retried with the original timeout.      *)
(*   Windows: after the console handle is signaled, a blocking read        *)
(*   follows, which waits for a key even if the event was not a key.     *)
(* Variant "new" is the fixed design:                                      *)
(*   Unix: a signal ends the wait; the loop works out the time left.       *)
(*   Windows: events are read only when the console reports some are      *)
(*   queued, so the read cannot block; non-key events are dropped.         *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
    Deadline,     \* ticks until the deadline
    MaxBytes,     \* stop after this many bytes
    MaxIter,      \* stop after this many waits
    SendLimit,    \* most bytes the terminal sends (bounds the model)
    EventLimit,   \* most non-key events (Windows)
    SignalLimit,  \* most signals (Unix)
    Slack,        \* ticks past Deadline the clock may run in the model
    Platform,     \* "unix" or "windows"
    Variant       \* "old" or "new"

VARIABLES
    now,          \* the clock
    pc,           \* "loop", "waiting", "blockedRead", "done"
    waitUntil,    \* when the current wait times out
    origTimeout,  \* old Unix: the timeout the wait was started with
    avail,        \* bytes sent but not yet read
    finalSent,    \* the terminal has sent the last byte of the reply
    finalRead,    \* the process has read it
    events,       \* non-key console events queued (Windows)
    interrupted,  \* a signal is interrupting the current wait (Unix)
    sent, got, iter, signals, eventsSent

vars == <<now, pc, waitUntil, origTimeout, avail, finalSent, finalRead,
          events, interrupted, sent, got, iter, signals, eventsSent>>

Init ==
    /\ now = 0
    /\ pc = "loop"
    /\ waitUntil = 0
    /\ origTimeout = 0
    /\ avail = 0
    /\ finalSent = FALSE
    /\ finalRead = FALSE
    /\ events = 0
    /\ interrupted = FALSE
    /\ sent = 0
    /\ got = 0
    /\ iter = 0
    /\ signals = 0
    /\ eventsSent = 0

Min(a, b) == IF a < b THEN a ELSE b

-----------------------------------------------------------------------------
(* The environment *)

\* The terminal sends a byte; it may be the last byte of the reply.
SendByte ==
    /\ sent < SendLimit
    /\ ~finalSent
    /\ sent' = sent + 1
    /\ avail' = avail + 1
    /\ finalSent' \in BOOLEAN
    /\ UNCHANGED <<now, pc, waitUntil, origTimeout, finalRead, events,
                   interrupted, got, iter, signals, eventsSent>>

\* A console event that is not a key: focus change, window resize, mouse.
SendEvent ==
    /\ Platform = "windows"
    /\ eventsSent < EventLimit
    /\ eventsSent' = eventsSent + 1
    /\ events' = events + 1
    /\ UNCHANGED <<now, pc, waitUntil, origTimeout, avail, finalSent,
                   finalRead, interrupted, sent, got, iter, signals>>

\* A signal interrupts a wait in progress.
Signal ==
    /\ Platform = "unix"
    /\ pc = "waiting"
    /\ ~interrupted
    /\ signals < SignalLimit
    /\ signals' = signals + 1
    /\ interrupted' = TRUE
    /\ UNCHANGED <<now, pc, waitUntil, origTimeout, avail, finalSent,
                   finalRead, events, sent, got, iter, eventsSent>>

\* Something the wait wakes up for.
WakeReason ==
    \/ avail > 0
    \/ interrupted
    \/ (Platform = "windows" /\ events > 0)

Blocked ==
    \/ (pc = "waiting" /\ ~WakeReason /\ now < waitUntil)
    \/ (pc = "blockedRead" /\ avail = 0)

\* The clock advances only while the process is blocked.
Tick ==
    /\ Blocked
    /\ now < Deadline + Slack
    /\ now' = now + 1
    /\ UNCHANGED <<pc, waitUntil, origTimeout, avail, finalSent, finalRead,
                   events, interrupted, sent, got, iter, signals, eventsSent>>

Environment == SendByte \/ SendEvent \/ Signal \/ Tick

-----------------------------------------------------------------------------
(* The process *)

\* Read k of the available bytes (a read returns at least one).
ReadBytes ==
    \E k \in 1..avail :
        /\ avail' = avail - k
        /\ got' = got + k
        \* The last byte of the reply is the last one sent, so it is read
        \* once every sent byte has been read.
        /\ finalRead' = (finalSent /\ avail' = 0)

Stop ==
    \/ finalRead
    \/ got >= MaxBytes
    \/ now >= Deadline
    \/ (Variant = "new" /\ iter >= MaxIter)

LoopStep ==
    /\ pc = "loop"
    /\ IF Stop
         THEN /\ pc' = "done"
              /\ UNCHANGED <<waitUntil, origTimeout, iter>>
         ELSE /\ pc' = "waiting"
              /\ waitUntil' = Deadline          \* wait for the time left
              /\ origTimeout' = Deadline - now
              /\ iter' = iter + 1
    /\ UNCHANGED <<now, avail, finalSent, finalRead, events, interrupted,
                   sent, got, signals, eventsSent>>

\* Input is available: read it.
WakeForBytes ==
    /\ pc = "waiting"
    /\ avail > 0
    /\ ReadBytes
    /\ pc' = "loop"
    /\ interrupted' = FALSE
    /\ UNCHANGED <<now, waitUntil, origTimeout, finalSent, events, sent,
                   iter, signals, eventsSent>>

\* A signal interrupted the wait (Unix).
WakeForSignal ==
    /\ pc = "waiting"
    /\ avail = 0
    /\ interrupted
    /\ interrupted' = FALSE
    /\ IF Variant = "old"
         \* Retry poll with the original timeout.
         THEN /\ pc' = "waiting"
              /\ waitUntil' = now + origTimeout
         \* Back to the loop, which works out the time left.
         ELSE /\ pc' = "loop"
              /\ UNCHANGED waitUntil
    /\ UNCHANGED <<now, origTimeout, avail, finalSent, finalRead, events,
                   sent, got, iter, signals, eventsSent>>

\* The console handle was signaled by events that are not keys (Windows).
WakeForEvents ==
    /\ pc = "waiting"
    /\ avail = 0
    /\ events > 0
    /\ events' = 0
    /\ IF Variant = "old"
         \* os.Stdin.Read drops the events, then blocks until a key.
         THEN pc' = "blockedRead"
         \* The events are read and dropped; the read cannot block.
         ELSE pc' = "loop"
    /\ UNCHANGED <<now, waitUntil, origTimeout, avail, finalSent, finalRead,
                   interrupted, sent, got, iter, signals, eventsSent>>

\* The wait timed out.
WakeForTimeout ==
    /\ pc = "waiting"
    /\ ~WakeReason
    /\ now >= waitUntil
    /\ pc' = "loop"
    /\ UNCHANGED <<now, waitUntil, origTimeout, avail, finalSent, finalRead,
                   events, interrupted, sent, got, iter, signals, eventsSent>>

\* Old Windows: the blocking read returns once bytes arrive.
BlockedReadReturns ==
    /\ pc = "blockedRead"
    /\ avail > 0
    /\ ReadBytes
    /\ pc' = "loop"
    /\ UNCHANGED <<now, waitUntil, origTimeout, finalSent, events,
                   interrupted, sent, iter, signals, eventsSent>>

Process ==
    LoopStep \/ WakeForBytes \/ WakeForSignal \/ WakeForEvents
    \/ WakeForTimeout \/ BlockedReadReturns

Done == pc = "done" /\ UNCHANGED vars

Next == Process \/ Environment \/ Done

\* The process's steps are fast: if one is possible, it happens. And time
\* keeps passing: while the process is blocked, the clock advances.
Spec == Init /\ [][Next]_vars /\ WF_vars(Process) /\ WF_vars(Tick)

-----------------------------------------------------------------------------
(* Properties *)

TypeOK ==
    /\ now \in 0..(Deadline + Slack)
    /\ pc \in {"loop", "waiting", "blockedRead", "done"}
    /\ avail \in 0..SendLimit
    /\ got \in 0..SendLimit

\* The key property: the clock never passes the deadline while the
\* process is still reading.
NeverPastDeadline == now > Deadline => pc = "done"

\* The process always finishes.
Finishes == <>(pc = "done")

\* When it finishes, it has either the whole reply, or MaxBytes, or the
\* deadline passed, or it made MaxIter waits.
FinishesForAReason ==
    pc = "done" => (finalRead \/ got >= MaxBytes \/ now >= Deadline
                    \/ (Variant = "new" /\ iter >= MaxIter))

\* If the whole reply arrives in time and fits, it is read in full: the
\* loop does not give up on a reply that is there to read, except after
\* MaxIter waits.
ReadsCompleteReply ==
    (pc = "done" /\ finalSent /\ sent <= MaxBytes /\ now < Deadline
     /\ iter < MaxIter) => finalRead
=============================================================================
