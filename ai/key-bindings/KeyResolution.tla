--------------------------- MODULE KeyResolution ---------------------------
(***************************************************************************)
(* How the interactive editor resolves key sequences into bindings         *)
(* (ai/cli_bindings.typ, section 6). In the code: resolveKey in            *)
(* mshell/Bindings.go.                                                     *)
(*                                                                         *)
(* The editor reads one key at a time. A key that may begin or continue a  *)
(* longer binding still does its own action at once (a text key types      *)
(* itself), and is kept as pending with the line as it was before it (a    *)
(* snapshot). When a binding completes, the line goes back to the snapshot *)
(* before the binding's first key and the binding runs on it.              *)
(*                                                                         *)
(* The model checks this incremental algorithm against the declarative     *)
(* definition of section 6.1, over every input of up to MaxLen events:     *)
(* keys and pastes, each with the time since the event before it.          *)
(*                                                                         *)
(*   StepMatches        after every event, the algorithm's line, effects   *)
(*                      and bindings are those of the definition: no key   *)
(*                      waits to be shown (no lag), and a binding runs on  *)
(*                      exactly the line before its first key.             *)
(*   EffectsNeverUndone an effect that ran is never replaced by a longer   *)
(*                      binding, so nothing has to be taken back.          *)
(*   PasteInert         no binding's keys span a paste.                   *)
(*   Bounded            at most MaxPending keys are pending.               *)
(*                                                                         *)
(* Variant "conflicts" is the design: an effect binding that ends a proper *)
(* prefix of another binding conflicts with it, and the older is inert.    *)
(* Variant "noconflicts" makes every binding active, and must fail         *)
(* EffectsNeverUndone: without the rule, a longer binding can complete     *)
(* over an effect that has already run. Variant "nosnapshot" runs a        *)
(* binding on the line as it is, with its first keys still typed, and must *)
(* fail StepMatches.                                                       *)
(*                                                                         *)
(* Time is given, not measured: each event carries its gap. Every timing    *)
(* rule looks only at gaps before the key being handled, so the editor     *)
(* decides on each key as it is read, and never waits for time to pass.   *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    MaxLen,      \* longest input
    Gaps,        \* the gaps an event may have after the one before it
    MaxPending,  \* the editor's pending array
    Variant      \* "conflicts", "noconflicts" or "nosnapshot"

Keys == {"a", "b", "X"}         \* a and b type themselves; X types nothing (Ctrl-X)
Kinds == Keys \cup {"P"}        \* P is a paste: text that is not a key
Events == [k : Kinds, g : Gaps]

(* The bindings, in the order they were added. kind "pure" replaces the    *)
(* keys with text; "effect" runs eff: accept submits the line and clears   *)
(* it, run runs something and keeps the line. within and ib (idleBefore)   *)
(* are 0 when not given.                                                   *)
Table == <<
    [keys |-> <<"a", "b">>, kind |-> "effect", eff |-> "accept", text |-> "", within |-> 0, ib |-> 0],
    [keys |-> <<"X", "b">>, kind |-> "effect", eff |-> "run", text |-> "", within |-> 0, ib |-> 0],
    [keys |-> <<"a", "a">>, kind |-> "pure", eff |-> "", text |-> "Z", within |-> 1, ib |-> 0],
    [keys |-> <<"b">>, kind |-> "effect", eff |-> "run", text |-> "", within |-> 0, ib |-> 0],
    [keys |-> <<"b", "a", "a">>, kind |-> "pure", eff |-> "", text |-> "W", within |-> 0, ib |-> 1],
    [keys |-> <<"a", "a", "b">>, kind |-> "pure", eff |-> "", text |-> "V", within |-> 0, ib |-> 0]
>>

NB == Len(Table)

(* s is a proper prefix of t, or a suffix of one. *)
ProperPrefixes(t) == {SubSeq(t, 1, n) : n \in 0..Len(t) - 1}
IsSuffix(s, t) == Len(s) <= Len(t) /\ SubSeq(t, Len(t) - Len(s) + 1, Len(t)) = s
EndsAPrefix(s, t) == \E p \in ProperPrefixes(t) : IsSuffix(s, p)

(* Section 6.3: two bindings conflict when they have the same keys, or     *)
(* when one is an effect whose keys end a proper prefix of the other's.    *)
Conflicts(i, j) ==
    \/ Table[i].keys = Table[j].keys
    \/ Table[i].kind = "effect" /\ EndsAPrefix(Table[i].keys, Table[j].keys)
    \/ Table[j].kind = "effect" /\ EndsAPrefix(Table[j].keys, Table[i].keys)

(* Adding binding n makes every active binding it conflicts with inert. *)
RECURSIVE ActiveAfter(_)
ActiveAfter(n) ==
    IF n = 0 THEN {}
    ELSE {i \in ActiveAfter(n - 1) : ~Conflicts(i, n)} \cup {n}

Active == IF Variant = "noconflicts" THEN 1..NB ELSE ActiveAfter(NB)

VARIABLES
    input,    \* the events
    i,        \* how many have been read
    line,     \* the line, as a sequence of characters
    log,      \* the effects that ran, each with the line it ran on
    pend,     \* the pending keys: [k, s (event index), snap (line before it)]
    used      \* the bindings in force: [b, s, e], keys s..e

vars == <<input, i, line, log, pend, used>>

Max(S) == CHOOSE x \in S : \A y \in S : x >= y

(***************************************************************************)
(* What a key, a binding and an effect do to the line.                     *)
(***************************************************************************)
Own(k, l) == IF k = "X" THEN l ELSE l \o <<IF k = "P" THEN "p" ELSE k>>

\* A binding u applied to the line l that was there before its first key.
Apply(u, l, lg) ==
    LET t == Table[u.b] IN
    IF t.kind = "pure" THEN [line |-> l \o <<t.text>>, log |-> lg]
    ELSE IF t.eff = "accept" THEN [line |-> <<>>, log |-> Append(lg, <<t.eff, l>>)]
    ELSE [line |-> l, log |-> Append(lg, <<t.eff, l>>)]

(***************************************************************************)
(* The timing conditions of a binding b whose keys are events s..e.        *)
(***************************************************************************)
Timed(b, s, e) ==
    LET t == Table[b] IN
    /\ t.within = 0 \/ \A j \in s + 1..e : input[j].g <= t.within
    /\ t.ib = 0 \/ s = 1 \/ input[s].g >= t.ib

\* A new binding may not start inside one in force: before it, or at or
\* before its first key (then it replaces it).
Fits(s, U) == \A u \in U : s > u.e \/ s <= u.s

(***************************************************************************)
(* The definition (section 6.1): at each key, the longest binding that     *)
(* ends there, on keys with no paste among them, whose times fit, and that *)
(* does not start inside a binding in force. It replaces the bindings in   *)
(* force that start at or after its first key.                            *)
(***************************************************************************)
Cands(U, n) ==
    {[b |-> b, s |-> n - Len(Table[b].keys) + 1, e |-> n] :
        b \in {c \in Active :
            LET s == n - Len(Table[c].keys) + 1 IN
            /\ s >= 1
            /\ \A j \in s..n : input[j].k = Table[c].keys[j - s + 1]
            /\ Timed(c, s, n)
            /\ Fits(s, U)}}

Longest(C) == CHOOSE c \in C : \A d \in C : c.e - c.s >= d.e - d.s

DefStep(U, n) ==
    IF input[n].k = "P" THEN U
    ELSE LET C == Cands(U, n) IN
         IF C = {} THEN U
         ELSE LET c == Longest(C) IN {u \in U : u.s < c.s} \cup {c}

RECURSIVE DefUsed(_)
DefUsed(n) == IF n = 0 THEN {} ELSE DefStep(DefUsed(n - 1), n)

\* The line and effects after events 1..n, with the bindings U in force:
\* every key a binding covers does the binding, at its last key, on the
\* line before its first; every other event does its own action.
RECURSIVE DefFold(_, _)
DefFold(U, n) ==
    IF n = 0 THEN [line |-> <<>>, log |-> <<>>]
    ELSE LET prev == DefFold(U, n - 1) IN
         IF \E u \in U : u.e = n THEN
             Apply(CHOOSE u \in U : u.e = n, prev.line, prev.log)
         ELSE IF \E u \in U : u.s <= n /\ n < u.e THEN prev
         ELSE [line |-> Own(input[n].k, prev.line), log |-> prev.log]

(***************************************************************************)
(* The algorithm (section 6.5). The window is the pending keys and the new *)
(* key; the trie walk finds the bindings that end it and the ones it can  *)
(* still grow into.                                                        *)
(***************************************************************************)
Window(n) == Append(pend, [k |-> input[n].k, s |-> n, snap |-> line])

\* A binding c ends the window W at n, its keys all in W.
EndsWindow(c, W, n) ==
    LET s == n - Len(Table[c].keys) + 1 IN
    /\ s >= W[1].s
    /\ \A j \in s..n : W[j - W[1].s + 1].k = Table[c].keys[j - s + 1]
    /\ Timed(c, s, n)
    /\ Fits(s, used)

\* The keys of W from event p on are a proper prefix of an active binding,
\* with its times so far.
Grows(W, p, U) ==
    \E c \in Active :
        LET t == Table[c]
            m == Len(W) - (p - W[1].s)
        IN
        /\ m < Len(t.keys)
        /\ \A x \in 1..m : W[p - W[1].s + x].k = t.keys[x]
        /\ t.within = 0 \/ \A j \in p + 1..W[Len(W)].s : input[j].g <= t.within
        /\ t.ib = 0 \/ p = 1 \/ input[p].g >= t.ib
        /\ Fits(p, U)

\* Keep the window from its first key that can still grow into a binding.
Trim(W, U) ==
    LET ps == {p \in W[1].s..W[Len(W)].s : Grows(W, p, U)} IN
    IF ps = {} THEN <<>>
    ELSE SubSeq(W, (CHOOSE p \in ps : \A q \in ps : p <= q) - W[1].s + 1, Len(W))

Paste ==
    /\ input[i + 1].k = "P"
    /\ line' = Own("P", line)
    /\ pend' = <<>>
    /\ UNCHANGED <<log, used>>

Key ==
    LET n == i + 1
        W == Window(n)
        C == {[b |-> c, s |-> n - Len(Table[c].keys) + 1, e |-> n] : c \in {c \in Active : EndsWindow(c, W, n)}}
    IN
    /\ input[n].k # "P"
    /\ IF C = {} THEN
           /\ line' = Own(input[n].k, line)
           /\ pend' = Trim(W, used)
           /\ UNCHANGED <<log, used>>
       ELSE
           LET c == Longest(C)
               snap == IF Variant = "nosnapshot" THEN line ELSE W[c.s - W[1].s + 1].snap
               r == Apply(c, snap, log)
               U == {u \in used : u.s < c.s} \cup {c}
           IN
           /\ line' = r.line
           /\ log' = r.log
           /\ used' = U
           /\ pend' = IF Table[c.b].kind = "effect" THEN <<>> ELSE Trim(W, U)

Init ==
    /\ input \in UNION {[1..n -> Events] : n \in 0..MaxLen}
    /\ i = 0
    /\ line = <<>>
    /\ log = <<>>
    /\ pend = <<>>
    /\ used = {}

Next ==
    /\ i < Len(input)
    /\ (Paste \/ Key)
    /\ i' = i + 1
    /\ UNCHANGED input

Spec == Init /\ [][Next]_vars

(***************************************************************************)
(* Properties                                                              *)
(***************************************************************************)
TypeOK ==
    /\ i \in 0..Len(input)
    /\ Len(pend) <= MaxLen

StepMatches ==
    /\ used = DefUsed(i)
    /\ LET d == DefFold(used, i) IN line = d.line /\ log = d.log

EffectsNeverUndone ==
    \A n \in 1..i : \A u \in DefUsed(n - 1) :
        Table[u.b].kind = "effect" => u \in DefUsed(n)

PasteInert ==
    \A u \in used : \A j \in u.s..u.e : input[j].k # "P"

Bounded == Len(pend) <= MaxPending

=============================================================================
