# Mechanized core of the mshell type system

A Rocq development of the core calculus in `ai/type-core-calculus.typ`, with a machine-checked
type soundness theorem.

```
make          # build (needs Rocq 9.1; defaults to ~/.opam/rocq-mshell/bin, override with ROCQBIN=...)
make check    # build, then print the assumptions of the main theorem
```

`make check` ends with `Closed under the global context`.
The theorem uses no axioms and no admitted lemmas.
A clean build takes about 20 seconds.

## The theorem

`Soundness.v`:

```coq
Theorem soundness : forall sigs defs,
  def_ok sigs defs ->                              (* every def body checks at every instance of its signature *)
  forall G e s, T sigs G LNone LNone None e [] s -> (* the program checks from an empty stack *)
  forall n, eval defs n [OScope []] 0 [] e <> RStuck.
```

`eval` is a fuel-bounded definitional interpreter (`Interp.v`).
It returns `RStuck` exactly where the Go runtime reports a type mismatch: `+` on a string, a
missing *required* key, a non-quote given to `x`, stack underflow, and so on.
It returns `RErr` for checked errors (`?` on none, index out of range, reading an unset variable)
and `RExit` for `exit`.
"For all fuel" means every finite prefix of every run is free of type errors, including runs
that never terminate.

## What you have to trust

The proofs are checked by Rocq.
What you have to read is whether the *definitions* say what the design means:

| File | Lines | What to check |
|---|---|---|
| `Syntax.v` | ~130 | types, words, values, heap objects |
| `Interp.v` | ~360 | the interpreter matches `Evaluator.go` where it matters (see below) |
| `Typing.v` | ~230 | each typing rule matches the doc |
| `Subtyping.v` (definitions only) | ~80 | `sub`, `fsub`, `rsub`, `immutable` |

Everything else (`Invariant.v` onward) is proof and cannot make the theorem say something weaker.

## Map to the design document

| Doc | Rocq |
|---|---|
| Types, shapes, remainders, `{str: T}` | `ty`, `fstat` in `Syntax.v`. A dict-kinded type is `TRec fields remainder`; `{str: T}` is `TRec [] (FDict T)` |
| Subtyping, S1-S4 | `sub` and `fsub` (per label), `sub_trans` |
| Retype of fresh values | `rsub` / `frsub` |
| Stack slots `τ` / `τ•` | `mark` = `Sh` / `Dp`; `slot_sub` is the one subsumption rule |
| Typing rules, break/continue/return contexts | `TW` / `T` in `Typing.v` |
| Kind patterns, abstract types | `tw_kind` (`kind_then`/`kind_else`), `tw_kind_list` (arm checked for every element type) |
| `tryAs` | `tw_try_dp` / `tw_try_sub` / `tw_try_imm` / `tw_try_copy`; `validate`, `copy` in `Interp.v` |
| Type-changing updates of fresh values | `tw_setk_dp`, `tw_del_dp` |
| `never` | `TQuote ins None`; `tw_exec_never`, `tw_call_never`, `tw_loop_forever` |
| Store typing, freshness, commit | `vtyped`, `dtyped`, `inv` in `Invariant.v`; `commit_all` in `Commit.v` |
| Counterexamples | `Examples.v` |

## Modeling choices

- **Curry style.** Words carry no types except `tryAs`, whose runtime needs the target, and its
  in-place/copy flag, which the checker chooses. `as T` is the subsumption rule, not a word.
- **Closed types; polymorphism as instance sets.** `sigs f ins outs` lists the closed instances of
  `f`'s signature. The checker's rigid-variable check implies this by the usual substitution lemma,
  which is not mechanized here.
- **Abstract types are universally quantified arms.** `tw_kind_list` checks the arm for every element
  type. This is what rules out escape (see findings).
- **Unknown** is a type `TTop`. Every value has it, and no operation except kind patterns accepts it.
- **Frame polymorphism** is part of quote types: a quote body must check for every rest of the stack.
- **Fresh values are deep-typed off the heap.** A fresh slot's lists and dicts form a tree that nothing
  else references, and their store types are ignored until the value is committed. Retyping a fresh
  value therefore changes no state.
- **Child-stack builtins** (`each`) run the body on a one-element child stack, as the runtime does.
  `break`/`continue` inside the body discard the child stack and restore the outer one. The typing
  contexts are `LNone`, `LExact s` and `LChild` (the doc's `·`, `σ`, `⋆`).
- **Deviations from the Go runtime** that do not affect type safety: dict objects are association
  lists; `each` iterates over the list's elements as they are when it starts; stack shuffles other
  than `dup`/`drop`/`swap` and all other builtins are left out.

## Findings (now in the design document)

Holes in the previous draft. Each has a program in `Examples.v` that the interpreter runs to `RStuck`:

1. **Runtime-key `get` on a shape** was typed as `Maybe` of the remainder, but the key can name a declared
   field (`hole_dynget`). Fix: the result must be above every field type (`tw_getd`).
2. **Abstract types escaping their pattern arm.** A `k` fixed per pattern site can be unified with an
   outer type variable or reused across loop iterations (`hole_exists`). Fix: check the arm for every
   `k` (skolem escape check).
3. **Every shape literal marked fresh**, including `{a: @xs}` (`hole_literal`). Fix: a literal is fresh
   only around fresh or immutable contents.

Clarifications the proof forced:

- `dict d` on an unknown value must bind the read-only `{| open}`, not `{str: k}`.
- The validation copy must be per path (not memoized), and in-place validation needs a tree.
- A `never` def has no `return`.
- An optional field must never be viewable as a deletable entry: that would break transitivity. This is
  the exact reason shapes are never `Dict`s. It was found while proving `sub_trans`.

Rules that turned out to be usability choices, not soundness conditions:

- union members of distinct kinds;
- `checkable` targets for `tryAs`;
- S2's refusal of a source with a matching `*: T` remainder, and `{str: T}` never viewed as an
  all-optional shape.

## Not modeled

Recursive aliases (`Json`), enums, grids and commands as such, other builtins (they need the builtin
contract), the checker algorithm (unification, joins, frame and substitution lemmas, the escape check),
and definite assignment (an unset variable is a checked error, as at runtime).
See the "mechanized core" section of the design document for what each would need.
