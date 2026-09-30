# Mechanized core of the mshell type system

A Rocq development of the core calculus in `ai/type-core-calculus.typ`, with a machine-checked
type soundness theorem.

```
make          # build (needs Rocq 9.1; defaults to ~/.opam/rocq-mshell/bin, override with ROCQBIN=...)
make check    # build, then print the assumptions of the main theorem
```

To install Rocq into the switch the Makefile expects (opam 2.x, no `sudo`):

```
opam init --bare -y --disable-shell-hook
opam switch create rocq-mshell ocaml-base-compiler.4.14.2
opam install --switch rocq-mshell -y rocq-prover rocq-core.9.1.1
```

`make check` ends with `Closed under the global context`.
The theorem uses no axioms and no admitted lemmas.
A clean build takes about 20 seconds.

## The theorem

`Soundness.v`:

```coq
Theorem soundness : forall sigs defs,              (* sigs: def signatures and enum declarations *)
  def_ok sigs defs ->                              (* every def body checks at every instance of its signature *)
  forall G R e s, T sigs G LNone LNone R e [] s -> (* the program checks from an empty stack *)
  forall n, eval defs n [OScope []] 0 [] e <> RStuck.
```

`Soundness.v` proves it for the interpreter with any validator for `tryAs` whose `just` is right for
a fresh value and for a target with no list or dict (`soundness_v`); `soundness` is the instance for
the model's `validate`, and `Cycles.v` gives another, a validator that answers `just` when an
(object, type) pair repeats on its path (`soundness_cycles`).

`Generic.v` removes the "every instance" assumption: a body checked once, at its declared signature
with its type variables rigid, is enough.

```coq
Theorem soundness_generic : forall sigs gs defs,
  (forall f ins outs, g_sigs sigs f ins outs <-> instances gs f ins outs) ->  (* signatures are declared *)
  (forall E c pts, g_ctors sigs E c = Some pts -> wf_payload E pts) ->       (* enum declarations are well formed *)
  gdefs_ok sigs gs defs ->                           (* every body checks once, generically *)
  forall G R e s, T sigs G LNone LNone R e [] s ->
  forall n, eval defs n [OScope []] 0 [] e <> RStuck.
```

`eval` is a fuel-bounded definitional interpreter (`Interp.v`).
It returns `RStuck` exactly where the Go runtime reports a type mismatch: `+` on a string, a
missing *required* key, a non-quote given to `x`, stack underflow, and so on.
It returns `RErr` for checked errors (`?` on none, index out of range, reading an unset variable,
`copy` of a cyclic value) and `RExit` for `exit`.
"For all fuel" means every finite prefix of every run is free of type errors, including runs
that never terminate.

## What you have to trust

The proofs are checked by Rocq.
What you have to read is whether the *definitions* say what the design means:

| File | Lines | What to check |
|---|---|---|
| `Syntax.v` | ~400 | types (including recursive types `TMu`, their closedness and unfolding), enum identities, substitution, words, values, heap objects |
| `Interp.v` | ~460 | the interpreter matches `Evaluator.go` where it matters (see below); `eval` is `evalv validate` |
| `Typing.v` | ~270 | each typing rule matches the doc; `genv` holds def signatures and enum declarations |
| `Subtyping.v` (definitions only) | ~200 | the one-level relations `subF` and `rsubF` (with `fsubR`, `subsR`, `osubR`, `vsubsR`, `frsubR`, `vrsubsR`), `sub` and `rsub` as their greatest fixed points, `immutable`, and the enum declaration checks `occ_sub`, `occ_fresh`, `wf_payload` |

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
| `tryAs` (in place, never copies) | `tw_try_dp` / `tw_try_sub` / `tw_try_imm`; `validate` in `Interp.v` |
| `deepCopy` (explicit, result fresh; `WCopy` in the model) | `tw_copy`; `dcopy` in `Interp.v`; `dcopy_fresh` in `Copy.v`; `inv_alloc_region` in `InvOps.v` |
| Type-changing updates of fresh values | `tw_setk_dp`, `tw_del_dp` |
| `never` | `TQuote ins None`; `tw_exec_never`, `tw_call_never`, `tw_loop_forever` |
| Store typing, freshness, commit | `vtyped`, `dtyped`, `inv` in `Invariant.v`; `commit_all` in `Commit.v` |
| Generic enums: variance, fresh-covariance, immutability | `s_enum`/`vsubs`, `rs_enum`/`vrsubs`, `immutable`; declaration check `wf_payload`; soundness of the check `payload_sub`, `payload_rsub`, `payload_imm` in `Variance.v` |
| Constructors, constructor match, enum kind pattern | `tw_con_sh`, `tw_con_dp`, `tw_case`, `tw_kind_enum` |
| Validation with a work budget | `validate` in `Interp.v` (running out is `RErr`) |
| Type variables; checking a def once | `TVar`, `tsub`; `T_subst`, `generic_def_ok`, `soundness_generic` in `Generic.v` |
| Checking a quote body once (frame lemma) | `T_frame`, `quote_once` in `Frame.v` |
| "A diverging effect absorbs what follows" | `t_div`; `diverges` and the `div_*` lemmas in `Frame.v` |
| Branch joins | `join_slot`, `join_slot_ub`, `if_join` in `Join.v`; the join is given the checker's decision procedure `le` for `<=` and fresh retyping, and the proofs assume only that it is right when it says yes (`le_ok`) |
| Joins that meet a recursive alias (never widened inside) | `ajoin` in `Join.v`; `join_*` in `Recursive.v` |
| `map` with a literal body | `WMap`; `tw_map`, `tw_map_imm` (fresh only when the results are immutable) |
| `return` in top-level code | return context `RAny`, `tw_return_any` |
| Match bindings | stores into the scope (`WStore`); `list :>` is `WKindIf` with the value left on the stack |
| Checkable targets; `is T` exhaustiveness | `chk`, `validate_complete`, `validate_complete_fresh` in `Checkable.v` |
| Counterexamples | `Examples.v` |
| Recursive aliases `type X = T` | `TMu t` (`TRV 0` in `t` is the type itself), closed (`mu_ok`); one step of unfolding is `tunfold` |
| Recursive aliases compared as infinite trees | `sub`, `rsub` as greatest fixed points; `sub_trans`, `sub_tsub`, `rsub_tsub`; `json_teq` in `Recursive.v` |
| Guardedness; one assumption set per relation | `unguarded_*`, `mixed_accepts`, `rsub_rejects`, `hole_mixed_*` in `Recursive.v` |
| Immutable, checkable for aliases (greatest fixed points) | `immutable`, `chk` on `TMu`; `immutable_tunfold`, `chk_tunfold` |
| Kind pattern on an alias | `kind_then k (TMu _) = kind_top k` (unknown contents); unfold first with `t_sub` for the member |
| What the proof needs of the validator; the cycle rule | `soundness_v` (`vd_fresh`, `vd_imm`); `cvalidate`, `soundness_cycles`, `cval_complete` in `Cycles.v` |

## Modeling choices

- **Curry style.** Words carry no types except `tryAs`, whose runtime needs the target.
  `as T` is the subsumption rule, not a word. No word's runtime behavior depends on a
  static fact: `tryAs` is always in place, and `copy` always copies.
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
- **`copy` has a depth bound.** `dcopy` is given the heap size as its bound on object nesting, so it
  runs out only on a cyclic value, which is a checked error (`copy_cycle_err`). The proof does not
  depend on the bound. The result's region is exactly the new locations, and their store types are
  placeholders until the value is committed.
- **Child-stack builtins** (`each`) run the body on a one-element child stack, as the runtime does.
  `break`/`continue` inside the body discard the child stack and restore the outer one. The typing
  contexts are `LNone`, `LExact s` and `LChild` (the doc's `·`, `σ`, `⋆`).
- **Recursive types are closed μ-types.** A recursive alias is written as the type it denotes, `TMu t`,
  with de Bruijn recursion variables (`TRV`); aliases that refer to each other become nested `TMu`s.
  Aliases are not generic, so a `TMu` used anywhere is closed (`mu_ok`: no type variable, no enum
  parameter), and substitution treats it as an atom. The unfolding rules require `mu_ok`, so a
  `TMu` that is not closed relates to nothing but itself and has no values.
- **Subtyping and fresh retyping are greatest fixed points.** Each is the largest relation closed under
  one level (`subF`, `rsubF`): union and unfolding steps are taken finitely often inside a level, and
  the children of a type constructor are compared by the relation being defined. `sub_fold` and
  `sub_unfold` move between the two views; `sub_coind` proves a pair by exhibiting a relation. Values
  are finite, so `vtyped` and `dtyped` stay inductive, with one rule for a recursive type (`vt_mu`,
  `dt_mu`: a value has it when it has the unfolding).
- **Enum identity.** An enum's identity in the model (`ename`) is its name together with each parameter's
  variance, whether the parameter is fresh-covariant, and whether the enum is immutable. The checker has
  one declaration per name, so this names the same enums, and it lets `sub`, `rsub` and `immutable` be
  defined without a declaration environment. Constructor payload types live in `g_ctors` and must pass
  `wf_payload`, which checks them against that identity. Recursive references need no special case.
- **Enum values carry their constructor's payload types**, as the runtime's pointer to the declaration.
  The validator reads them; `vtyped` requires them to be the declared ones.
- **Validation has a work budget** (the interpreter's remaining fuel). Running out is a checked error.
  This is what a cyclic value meets under a recursive type.
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

Holes in the enum rules as first written, found while adding generic enums (`Examples.v`):

4. **"Enums are immutable."** The freshness section listed enums among the immutable types. An enum with a
   list payload is not: a box around a shared list could be made fresh and retyped (`hole_box_stuck`).
   Fix: an enum type is immutable only when its payloads (with the arguments substituted) are
   (`en_imm`, `box_imm_rejected`).
5. **Fresh retyping of an argument used under a quote.** Retyping a fresh value is covariant everywhere
   *in its data*, but a quote is not data: a fresh `F[int]` for `enum F[a] = f (a -- a)` must not become
   `F[int | str]` (`hole_quote_arg_stuck`). Fix: a parameter is fresh-covariant only when every
   occurrence is in a data position (`occ_fresh`); otherwise it follows its variance.
6. **Joins that widen inside fresh quotes.** The join rule widened inside any two fresh values of one kind,
   quotes included; `(1 +)` and `("a" ++)` would join to `(int | str -- int | str)`
   (`hole_quote_join_stuck`). Fix: quotes join by subtyping only.

Found while checking definitions once (`Generic.v`):

7. **A type variable counted as immutable.** A shared argument of type `a` could be made fresh and
   validated in place; at the instance `a = [int]` a shared list becomes a `[str]` (`hole_tvar_imm_stuck`).
   Fix: a type variable is not immutable.
8. **A kind pattern on a type variable read as "no member of that kind".** The arm is then checked
   vacuously, and at the instance `a = str` it runs (`hole_tvar_kind_stuck`). Fix: a type variable is
   treated like unknown contents (`kind_then k (TVar x) = kind_top k`).

Also: a `tryAs` target must not mention a type variable (types are erased, so the runtime cannot
validate against one). That part of "checkable" is needed by the proof, not only for usability.

Model gaps found and closed:

- **Dead code after a diverging word.** The design's checker "absorbs" what follows `exit`; the model used
  to type it anyway, so `def f ( -- never) 1 exit 1 + end` was rejected. Rule `t_div` skips it. It has to
  hold at every frame (with loop and return stacks extended), or it would not survive the frame lemma.
- **`return` in top-level code** ends the script (`tests/success/return_top_level.msh`). The calculus gave
  top-level code no return context; it now has `RAny`.
- **Joins inside `Maybe`** of shared values are shared joins: `@xs just` and `@ys just` with `xs : [int]`,
  `ys : [str]` stored have no join (`hole_maybe_join_stuck`). The result of a join is fresh only when both
  arms are.

Found in the elaboration (`Examples.v`):

9. **Renaming a variable stored at a new type** changes what the program does when the variable is stored in a
   loop body or read after a branch. The renamed program type-checks and never gets stuck; the original,
   which is what runs, gets stuck (`rename_loop_*`, `rename_if_*`). The loop case is also accepted by the
   checker on `main` today.

Found while modeling match bindings and quote-taking list builtins (`Examples.v`):

10. **`filter` "fresh when the input is fresh".** The quote is given each element and may store it, so the
    result's elements can be shared even when the input list was fresh. A `map` whose body keeps its
    element (`filter` keeping everything) shows it (`hole_filter_fresh_stuck`). Fix: builtins whose quote
    sees the elements (`filter`, `sortBy`, `groupBy`, ...) give a fresh result only when the elements are
    immutable, like `map`.
11. **Match bindings typed per arm.** The runtime stores a match arm's bindings in the enclosing scope, like
    `x!`. Typing `int n` and `str n` in two arms separately is renaming, and a quote made in one arm reads
    the other arm's value (`arms_same_name_stuck`, rejected by the core in any context:
    `arms_same_name_rejected`). A binding of abstract type (`list xs` on unknown contents) fails even when
    kept to the arm: a call inside the arm can run the same pattern again and store another list under
    `xs` (`hole_bind_reentry_stuck`, `hole_bind_reentry_rejected`). Fix: a binding is a variable with one
    type per scope, and cannot have an abstract type; on unknown contents, `list :>` keeps the value on the
    stack, which is the core's `WKindIf` (`keep_on_stack_typed`).

Found while proving validation complete (`Checkable.v`):

- **"Checkable" must look into enum payloads.** The validator rejects every value against a quote type,
  including a quote in an enum payload, so a real `enum F = f (int -- int) end` value fails validation
  against `F` (`quote_enum_fails`). With that in the definition, validation never rejects a value against
  its own checkable type (`validate_complete`, `validate_complete_fresh`), which is what exhaustiveness
  of `is T` arms needs.
- **`tryAs` is a core word, not sugar for a match.** A match would store the value in a hidden variable,
  making the result shared.

Design additions the proof supports:

- **Fresh def inputs and outputs.** Signatures in `g_sigs` are stack slots, so an input or output can be
  marked fresh. The soundness proof needed no change (`mk_*`).
- **`map`'s result is fresh only when its elements are immutable**, never "when the input is fresh": the
  body's results are shared (`hole_map_fresh_stuck`, `map_widen_typed`).

R6 (two `tryAs` refinements of one shared dict) also runs to `RStuck` (`r6_stuck`), so by the theorem it
has no typing in any context (`r6_rejected`). With an explicit `copy` before the second `tryAs` it
type-checks and runs (`r6_copy_typed`, `r6_copy_runs`).

Clarifications the proof forced:

- `dict d` on an unknown value must bind the read-only `{| open}`, not `{str: k}`.
- The explicit copy must be per path, not memoized, so its result is a tree (`copy_two_paths_separate`);
  in-place validation of a fresh value needs a tree too.
- A `never` def has no `return`.
- An optional field must never be viewable as a deletable entry: that would break transitivity. This is
  the exact reason shapes are never `Dict`s. It was found while proving `sub_trans`.

Rules that turned out to be usability choices, not soundness conditions:

- union members of distinct kinds;
- `checkable` targets for `tryAs`;
- S2's refusal of a source with a matching `*: T` remainder, and `{str: T}` never viewed as an
  all-optional shape;
- a recursive enum reference using the same parameters (`nest_wf`: `Nest[[a]]` inside `Nest[a]` is sound).

Also forced by enums: an enum kind pattern on a value of unknown type checks its arm for every argument
list (`tw_kind_enum`), like `list xs`; `E[unknown]` would be wrong for an invariant parameter.

Found while adding recursive aliases (`Recursive.v`, `Cycles.v`). The rules held; the obvious way to
implement them did not:

12. **One assumption set for `<=` and fresh retyping.** Fresh retyping falls back to `<=` under a quote.
    A checker that keeps one set of assumed pairs answers `A <= B` under the quote from the retyping
    assumption, so a fresh `{x: [int], f: (-- A)}` becomes a `{x: [int | str], f: (-- B)}` and `f`
    hands out a shared `[int]` as `[int | str]` (`mixed_accepts`, `hole_mixed_stuck`,
    `hole_mixed_rejected`). The rule keeps them apart (`rsub_rejects`).
13. **The assumption rule without guardedness.** With `type V = int | V`, assuming a pair and then taking
    only union and unfolding steps proves `str <= V` and `V <= int` (`unguarded_str_below`,
    `unguarded_below_int`). The model's relation takes those steps inductively and has neither
    (`unguarded_model`); guardedness is what makes the algorithm agree with it.

Also found:

- **A checked program can build a cyclic value** (`cyc_try_typed`: `type L = [L]`, a list that contains
  itself). `deepCopy` of it and validating it are checked errors in the model (`cyc_copy_err`,
  `cyc_try_err`).
- **The cycle rule of validation is free** (the design chose `just`, 2026-09-30). The proof uses only two facts about the validator
  (`vd_fresh`, `vd_imm`). A cycle is always shared, and a shared operand is validated only when its type
  is already below the target, so answering `just` on a repeated (object, type) pair is as sound as an
  error (`soundness_cycles`) and still complete (`cval_complete`); it gives `just` for the cyclic list
  (`cyc_try_cycles`).
- **An enum instance guards a recursive alias**, as a list does (`type T = Box[T]`, `tb_typed`).
- **A join never widens inside a recursive alias** (decided 2026-09-30). Widening inside two different
  recursive types asks for their join again forever; the real answer is a recursive type nobody wrote.
  The join takes the other side when one is below the other, a union when the kinds do not overlap,
  and otherwise fails (`ajoin`, `join_two_recursive`); `join_slot_ub` still holds.
- **Immutability and checkability of an alias are greatest fixed points**; unfolding changes neither
  (`immutable_tunfold`, `chk_tunfold`).
- **A kind pattern on an alias** is typed like one on a type variable (unknown contents of that kind)
  unless the alias is unfolded first. `kind_then` must give some answer for a recursive type: the
  substitution lemma instantiates a type variable, which the pattern treats as unknown contents, with
  an alias.

## Not modeled

Generic aliases (`type Tree[a] = ...`: substitution would have to enter recursive types), the
assumption-set algorithm for recursive types (H12 and H13 show how it can go wrong), grids and commands as such, other builtins (they need the builtin
contract; every builtin that returns a new list follows `map`'s rule, fresh exactly when the elements are
immutable, `tw_map_imm`), the checker algorithm (unification, overload resolution; joins, frame and substitution
lemmas are now proved),
and definite assignment (an unset variable is a checked error, as at runtime).
See the "mechanized core" section of the design document for what each would need.
