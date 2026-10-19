# Key resolution model

The interactive editor resolves key sequences into bindings (`ai/cli_bindings.typ`, section 6).
A key that may begin a longer binding still does its own action at once, and is kept as pending with a snapshot of the line before it.
When a binding completes, the line goes back to the snapshot before its first key and the binding runs on it.
`KeyResolution.tla` checks that algorithm against the declarative definition of section 6.1.
In the code, the algorithm is `resolveKey` in `mshell/Bindings.go`, and `TestResolveMatchesDefinition` runs the same comparison on random keymaps.

Run `./check.sh`.
It uses `$TLA2TOOLS_JAR`, or `~/.local/share/tlaplus/tla2tools.jar`.

## What is modeled

A table of six bindings over the keys `a`, `b` (which type themselves) and `X` (which types nothing, like Ctrl-X):
an effect with `idleAfter`, an effect after a key that types nothing, a pure binding with `within`,
a one-key effect made inert by a later binding it conflicts with, a pure binding with `idleBefore`, and a pure binding longer than another.
The input is every sequence of up to five events, keys and pastes, each 0 or 2 ticks after the one before.
Time is given, not measured: the wait for `idleAfter` is the algorithm looking at the next event's gap, which is what a read with a deadline finds out.

## Properties

| Property | Meaning |
|---|---|
| `StepMatches` | After every event, the line, the effects that ran and the bindings in force are the definition's: no key waits to be shown, and a binding runs on the line before its first key. |
| `EffectsNeverUndone` | An effect in force is never replaced by a longer binding. |
| `PasteInert` | No binding's keys span a paste. |
| `Bounded` | At most 8 keys are pending. |

## Results

| Config | Design | Result |
|---|---|---|
| `Conflicts.cfg` | the design | passes, 219,345 distinct states |
| `NoConflicts.cfg` | every binding active | fails `EffectsNeverUndone`: the effect `b` runs, then `b a a` completes over it |
| `NoSnapshot.cfg` | a binding runs on the line as it is | fails `StepMatches` |

`check.sh` requires all three: the design passing, and both broken variants still failing.

The `NoConflicts` failure is why the conflict rule covers a sequence inside a longer one (`; r` against `x ; r q`), not only a prefix.
An earlier draft of the design held such an effect until the longer binding completed or failed; the definition says the longer one replaces it, which an effect that has run cannot be.
