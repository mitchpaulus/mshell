# Oracle for the checker's `<=` and fresh retyping

`Decide.v` proves the checker's decision procedures for `<=` (`subq`) and fresh retyping (`rsubq`)
right whenever they say yes, and `Join.v` proves the branch join an upper bound of its arms.
This directory extracts those functions to OCaml as a small program, `oracle`, that answers queries
one line at a time.
The Go checker's port of the same procedures (plan stage 2) is tested by comparing its answers with
the oracle's on generated types.

```
make          # extract from the proof and build ./oracle (needs the proof built: make -C ..)
make test     # check the oracle against examples.txt
```

`examples.txt` is the examples of `Decide.v`, printed from the Rocq terms themselves (`./oracle --examples`),
so no type in it was typed by hand. Each line is a query, a tab, and the expected answer.
The Go tests can load the same file.

## Queries

One query per line on stdin, one answer per line on stdout. Blank lines and lines starting with `#`
get no answer. A line that does not parse gets `error: ...`.

| Query | Answer |
|---|---|
| `sub N A B` | `yes` or `no`: does `subq` decide `A <= B` with fuel `N` |
| `rsub N A B` | `yes` or `no`: does `rsubq` decide that a fresh `A` may be retyped to `B` |
| `join N P Q` | the join of two stack slots (`join_slot` with the procedures above), or `none` |

A slot is `(shared T)` or `(fresh T)`.
The cache is empty in every query.

Fuel bounds the depth of the search. Running out answers `no`. The Go checker has no fuel: guarded
aliases make its search terminate. So compare with ample fuel (the depth needed grows with the
number of distinct pairs of subterms, so a few hundred is plenty for generated types of modest size),
and treat a `no` that turns into `yes` with more fuel as "fuel ran out".

## Types

| Syntax | Type |
|---|---|
| `int`, `str`, `bool` | base types (the model has only these three; others behave the same way) |
| `bot`, `top` | the empty type; unknown contents |
| `(maybe T)`, `(list T)` | `Maybe[T]`, `[T]` |
| `(dict T)` | `{str: T}` (the same as `(rec () (dict T))`) |
| `(rec ((LABEL STATUS) ...) STATUS)` | a dict-kinded type: declared labels, then the status of every other label |
| `(union T T ...)` | a union, nested to the right |
| `(quote (IN ...) (OUT ...))`, `(quote (IN ...) never)` | a quote type; stacks are top first |
| `(enum NAME imm\|mut (PARAM ...) (ARG ...))` | an enum instance |
| `(var N)` | a rigid type variable |
| `(mu T)`, `(rv N)` | a recursive type, and a de Bruijn reference to an enclosing `mu` (`(rv 0)` is the nearest) |
| `(param N)` | an enum declaration's parameter (appears only in payload types) |

Field statuses: `(req T)` present; `(opt T)` may be present, writable; `(dict T)` like `opt` and may
also be deleted; `abs` absent; `open` unknown, read-only.
So a shape literal `{a: 1}` is `(rec ((a (req int))) abs)`, a written shape type `{a: int}` has remainder
`open`, and `{a: int, *: str}` has remainder `(opt str)`.

An enum's identity in the model is its name together with, for each parameter, its variance
(`co`, `contra`, `inv`) and whether it is fresh-covariant (`co/fresh`), and whether the enum is
immutable (`imm`/`mut`). The Go checker computes these from the declaration; the oracle takes them
as given, so this comparison does not test that computation. The property tests for `payload_sub`
and `payload_rsub` (plan stage 2) do.

## Writing Go types for the oracle

- An alias reference node becomes `(mu ...)` of the alias's body, with each reference to an alias
  that is being expanded written as `(rv K)`, where `K` counts the `mu`s between the reference and
  the one it names. Aliases that refer to each other become nested `mu`s. This is the model's
  meaning of an alias (`formal-ver/README.md`, "Recursive types are closed μ-types").
- `Maybe[T]`, which the checker implements as the enum `Maybe[a] = just a | none`, is written
  `(maybe T)`. The model proves the two agree (`maybe_*` in `Examples.v`).
- Base types other than `int`, `str` and `bool` have no model counterpart. Generate types over the
  three.

## What a disagreement means

- **Go yes, oracle no** (with ample fuel): a possible soundness bug in the Go port. The oracle's yes
  answers are theorems; the Go checker's are not.
- **Go no, oracle yes**: the Go port is less complete than the proved procedure. Not unsound, but
  a program the design accepts would be rejected, so it is a bug in the port.
- **Joins**: the Go join must give the same slot, or `none` exactly when the oracle does.
