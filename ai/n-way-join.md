# One join for all the arms

## Problem

`joinArms` folds the arms pairwise: arm 1 with arm 2, that with arm 3, and so on.
The join is not a least upper bound, so the fold is not associative,
and whether a `match` checks can depend on the order of its arms.
On `main`, 24 of the 364 three-arm combinations drawn from a pool of 14 arms
(new and stored dicts and lists, partly new dicts, ints, Maybes) check in some orders and not others.
On `partial-join` it is 54, though no ordering `main` accepts is rejected.

The cause is the step that lets a new arm take a shared arm's type (`join_slot2`).
It gives up the new arm's freshness early, depending on which arm it meets first.
`{a: 1}`, `@t`, `["a"]` checks (the dict takes `t`'s type, then joins the list as a union);
`{a: 1}`, `["a"]`, `@t` does not (the new union cannot take `t`'s type).

## Design

One join takes every arm's slot at a position at once.
Each slot has a mark: new (fresh, or of an immutable type), stored, or partly new.

**Kinds.** When every arm's type has one kind and there are two or more kinds,
the arms of each kind are joined on their own (below), and the join is the union of those,
new when every kind's join is new.
If a kind has no join, or some arm's type has no single kind (a union), all the arms are joined together.
This is the n-way form of `tjoin` making a union of two kinds,
and it is what lets `{a: 1}`, `{b: 1}`, `@xs` join in any order.

**One kind**, or arms not grouped (`joinG`), tries three things in order.

1. **All new.** Fold the fresh join (`tjoin` with `fr = true`) over the types. The result is new.
2. **Partly new.** When some arm is partly new and none is stored:
   merge all the marks at once (`fmergeN`): for each position, the marks of the arms that hold a value there must merge,
   a new list or dict counting as partly new with every position new.
   If the merged mark is partly new, fold `mjoin` under it. The result is partly new.
3. **Shared.** Pick a target, let new and partly new arms take it where they can, and join in the rest.
   The targets, tried in order:
   - with stored arms, the shared join of the stored arms;
   - with none, the partly new join of the partly new arms together with every new arm whose mark agrees with theirs
     (merging it with them succeeds; a per-arm test, so the set does not depend on order),
     then of the partly new arms alone, then each partly new arm's own type.
     The arms a joint target was made from already reach it.

   For a target `T0`, each partly new arm that retypes to it (`rt`, the checker's `markBelow`) takes it;
   the others are committed and joined in: `T1 = T0 ⊔ ...`.
   Then each new arm that retypes to `T1` takes it; the others are committed and joined in: `T = T1 ⊔ ...`.
   The result is `T`, shared.

Every `⊔` above is the existing binary `tjoin` (Go: `Relations.JoinSlot`), folded,
so the relations compared with the OCaml oracle do not change.

For two arms this accepts what `join_slot2` and `join_slot3` accept:
new and stored (the new arm takes the stored type, or both join shared),
partly new and stored (the same), new and partly new (the new arm takes the partly new arm's type),
two partly new (one takes the other's type, or both commit).

### What stays order-dependent

Which arms take a target is decided against `T0` and `T1`, and which new arms join a joint target by a per-arm test,
none of which depend on arm order; the merge in step 2 fails on a conflict in any order.
What can still depend on order is inside the folded `tjoin`, on unions with members of one kind.
In Go, unions are canonical (`MakeUnion` sorts them), so member order does not show.

**Correction (2026-11-25).** The last paragraph was wrong: whether the join succeeded did depend on order.
Every `⊔` above was the binary `tjoin` folded over the types, and `tjoin` is not a least upper bound, so the fold is not associative.
Two types of one kind that cannot be widened inside (stored lists and dicts, quotes, aliases) join only when one is below the other,
so `a` and `b` may have no join while both are below a third arm `c`: `a`, `c`, `b` joined and `a`, `b`, `c` did not.
The first case found was a list, `["a" true @x]` with `x : str | null`:
an arm that is a union has no single kind, so the arms were not grouped, the fold made `bool | str`, and two different unions had no join.
`TestJoinOrderIndependentRandom` (`TypeRelationsGen_test.go`) then joined random arms in every order:
6143 of 20000 sets joined in some orders only before two unions could join, and 68 after.

## All the types at once (2026-11-25)

`tjoin` is replaced by `tjoinN` (`Join.v`; Go: `Relations.JoinAll`), the join of any number of types, and the join of two is `tjoinN` of two.
Nothing is folded: every `⊔` above is now one call with all its types.

1. `⊥` and repeated types are dropped.
2. If one of the types is above all the others, it is the join.
3. Otherwise the members of the types (a union's members, or the type itself) are grouped:
   two members are in one group when their kinds overlap, directly or through other members.
   Each group is joined, and the join is the union of those.
4. A group of one member is that member.
   A group of lists, of dicts, or of instances of one enum is widened inside, its parts joined all at once (lists and dicts only when fresh).
   Otherwise, or if that fails, the join is the member all the others are below.
   An alias is never widened inside, but the rest of its group may be, and that join is the group's when the alias is below it.

Under a partly new mark the types are joined position by position the same way (`mjoinN`).

Three steps were added to the shared step so that nothing a fold accepted in some order is lost:

- The new arms that do not take the target are joined with each other as new values first, and that join is committed (`commit_new`).
  Two new `Box[int]` and `Box[str]` widen to `Box[int | str]`; committed first, neither is below the other.
- Failing that, all the new arms are joined as new values (`join_new_first`). That join takes the target if it can, is committed and joined in if not,
  and if that has no join either, the target is given the members of kinds it lacks and the new arms' join takes that:
  a new `int | [str]` beside a stored `[str | null]` gives `int | [str | null]`.
- Last, every arm is committed and the types joined as stored values (`commit_all`): another arm may be above stored arms that have no join of their own.

Where the model takes the first type that fits, Go takes the one with the smallest `TypeId`, so the result is the same type for every order.

### What is proved and what is tested

Proved (`tjoinN_ub`, `mjoinN_ub`, `joinN_ub`): the join is above every arm.
Each member reaches the join directly, so the proofs need no transitivity of the fresh retype, which the model does not have.

Not proved: that the join is the same for every order, or that it exists whenever an upper bound does. These are tested:

| Test | What it checks | Result |
|---|---|---|
| `TestJoinOrderIndependentRandom` | random arms (new or stored, no partly new ones) joined in every order | none of 1,000,000 sets depends on order; each gives one type |
| `TestJoinOrderIndependent` | a pool of new, stored and partly new arms, now with two union-typed ones, in every order | no combination depends on order |
| `TestJoinAllAgreesWithOracle` | Go `JoinAll` against the extracted `tjoinN` on random types | agree on 100,000 questions |
| `TestRelationsAgreeWithOracle` | the join of two against the extracted `join_slot` | agree on 100,000 questions |

Known gap: about 10 sets in a million have no join all at once, yet join when the join of two is folded over them in some order.
A fold can commit some new arms before it meets others, which no single rule for all the arms does.
`TestJoinOrderIndependentRandom` counts these and fails if one appears in its default 20000 sets.

Compared with `main` on the pool of `TestJoinOrderIndependent`, the same 220 three-arm combinations check.
The checker benchmarks are within noise of `main` (corpus: 18,663 allocations per run before, 18,736 after).

### Unification variables

When some slot's type has an unsolved variable, the solved slots are joined at once as above,
and the others are folded in one at a time (unify, then match), which the model does not cover today either.
Which slots are solved is decided before any join, since joining solves variables.

## Proof

In `formal-ver/PartialJoin.v`, replacing `join_slot3`:

- `fmergeN`, with `fmergeN_ok`: each arm's mark changes to the merged one where it holds values (`mcompat`).
- `foldj_ub`: for a binary join that is an upper bound for a relation `R`,
  the fold's result is reached from the start and from every element by `R`, repeated (`steps`).
- `joinG`, and `joinN` with kinds; `joinN_ub`: every arm reaches the join by `slot_sub`, repeated.
- `joinN_stack` (whole stacks), `joinN_stack_ub`; `T_steps`: a judgment stays derivable after repeated subsumption of its output.
- `arm_joinN`, the per-arm form any n-arm rule (`tw_case`, kind patterns) uses, and `if_joinN`.
- `if_joinN_alg` and `arm_joinN_alg` in `Decide.v` with the proved `<=` procedure,
  and `alg_joinN_any_order`: `{a: [2]}`, `{a: @xs}`, `{b: @xs}` join in all six orders.
  `rt` stays an assumption, as in `join_slot2`.

Repeated subsumption instead of a fixed number of steps avoids needing `rsub` to be transitive, which is not proved.
The merge and the joins are fuel-bounded in the model; the checker recurses on the marks and types, which the fuel bounds.

`join_slot`, `join_slot2` and their theorems stay: `join_slot`'s `tjoin` is the building block,
and the pairwise join is still what the checker uses with unification variables.

## Go

- `joinN`, `joinKinds`, `joinG`, `pjoinN`, `shjoin`, `layer`, `fmergeN` in `TypeCorePartial.go`, mirroring the model.
  Every `⊔` is one fold, `foldJoin(m, ...)` over `mjoin` under a mark.
- `joinArms`, list literals and repeated generic arguments join all their slots at once; `joinSlot(a, b)` is `joinN` of two.
  The pairwise `joinTypes` only unifies or matches a slot with variables, and joins two solved slots by `joinN`.
- A step that fails, or keeps only a type (the kinds' union, the shared step), drops the parts it recorded,
  so trying targets does not use up the unit's partly new marks.
- `mfield` shares `Relations.fieldJoin`'s status table (`fieldJoinShape`).
- Error messages list every type the arms leave.

## Results

The pool of 14 arms, every three-arm combination in every order (`TestJoinOrderIndependent`, plus four-arm combinations of 7):

| | order-dependent combinations | accepted in some order |
| --- | --- | --- |
| `main` | 24 of 364 | 189 |
| `partial-join`, pairwise | 54 | 220 |
| this join | 0 | 220, and every combination either of the others accepts in some order |

Allocations in the checker benchmarks rose about 1% (scripts) and 6% (corpus) at first;
with the joins on reused buffers and the record keys on the stack, the corpus is 17,416 per run (18,364 with the first version)
and the scripts 2,677 (2,715). Time is within noise.
