# mshell type system: progress log

Plan: `ai/type-system-plan.md`. Specification: `ai/type-core-calculus.typ`.

## Stage 0: Baseline and measurements (2026-09-30)

Branch `type-checker-enhancements` at `56a08d2` (`main` at `b511d9b` already merged in). Nothing committed.

### Baseline

| Check | Result |
|---|---|
| `tests/test.sh` | 279 passed, 0 failed |
| `tests/typecheck_test.sh` | 269 passed (207 `success`, 62 `typecheck_fail`), 0 failed |
| `go test` (in `mshell/`) | ok |
| `make check` (in `formal-ver/`) | ok; every theorem printed `Closed under the global context` |
| `make -C formal-ver/oracle test` | 19 examples agree |
| `typst compile ai/type-core-calculus.typ` | ok |

Tools: Go 1.25.5, Typst 0.15.1, Rocq 9.1.1 (opam switch `rocq-mshell`).

The current checker on the other corpora (Stage 6 baseline):

- `tests/msh-scripts`: 73 of 118 pass `--type-check-only`. The first error in each of the 45 that fail:
  no matching overload (15), unknown identifier (14), stack underflow (4), one each of other mismatches.
- `lib/std.msh` checked as a file (with an empty `MSHSTDLIB`): fails, 11 errors
  (`-rot` unknown, `x` on an unknown quote in `2each`, `@out` unknown, `dateFmt` given a `date`, ...).
  The checker only reads std's signatures when checking other files, never its bodies.

### Measurements

How they were taken: `ai/type-system-stage0-counters.patch` adds temporary counters to the current checker (only active when `MSH_TC_STATS` is set). To reproduce, apply it, rebuild, and run `ai/type-system-stage0-run.sh`. The patch is not applied in the tree.
Counts are distinct source positions. Corpora: `tests/success` (207 files), `tests/msh-scripts` (118), `lib/std.msh`.
Positions inside std's bodies are only seen by checking std.msh itself, which fails partway through, so std counts are lower bounds.

| Measurement | success | msh-scripts | std.msh |
|---|---|---|---|
| Quote literals with more than one signature | 37 | 19 | 12 |
| Overload fan-outs (several candidates still viable) | 73 | 33 | 59 |
| Union distributions | 8 | 0 | 0 |
| Shape converted to `{str: T}` | 26 | 5 | 6 |
| `{str: T}` converted to a shape | 1 | 0 | 0 |
| Shape width or depth subtyping | 59 | 2 | 0 |
| `x` sites | 36 | 0 | 14 |
| `break`/`continue` sites | 22 | 9 | 19 |
| Variable stored at a different type (stores and match bindings) | 11 | 4 | 4 |
| Variable bound at different types in different arms, then joined | 6 | 4 | 9 |
| `as` sites | 8 | 0 | 6 |

**Quotes with several signatures (the risk named in plan section 5): the plan's approach covers all of them.**
All 68 are consumed by a word that takes a quote:
a builtin (`filter` 12, `map` 11, `any` 10, `sortByCmp` 5, `zip` 3, `map2` 3, `listToDict` 3, prefix forms 4, `each`, `2each`, `2apply`) or a def with a quote parameter (`testType` 12 in `dates.msh`).
In 66 the consumer comes right after the quote (or after more quote literals).
In 2 other arguments are pushed in between: `(+) @fg1 [@fg2 @fg3] foldl` (`grid_concat.msh:60`), `(extend) [] @l foldl` (`std.msh:405`).
Those need the "quote not yet checked" slot to survive pushes above it, as the plan describes.
None is stored, duplicated, returned or put in a list.
Decision: no change to the quote plan.

**Overload fan-outs.** 127 of the 165 are inside quote-body inference and go away when a quote is checked against its consumer's parameter.
The other 38 are in ordinary code:
- 18 are `any` in std.msh, and 3 are `foldl`/`sortByCmp`, given a literal quote (the case above);
- the rest are overloaded words (`wl`, `get`, `toInt`, `toFixed`, `gridAddCol`, `sum`, `classify`) whose operand type is still a variable.
  Many of those come from builtins that return a free type variable (the P10 pattern):
  `parseLinkHeader (str -- [{v}])`, `parseHtml (str | path -- {v})`, `parseJson`. The new builtin table fixes those.

**Union distribution** (8, all in `tests/success`): `numFmt`, `toFloat`, `map` on `int | float` operands from match joins (`match_arm_union.msh`, `unpack_union_bindings.msh`, `overloaded_quote_iff.msh`).
The design elaborates these to a `match` per member.

**Shape to `{str: T}`** (37). These are still accepted:
- 20 fresh literals passed straight to a dict builtin or returned from a def (`{...} keys`, `{...} @k getDef`, `urlEncode`, `httpPost` headers), through fresh retyping;
- 8 `{}` literals, which get a type variable and are solved by unification;
- 1 from a `dict d` pattern on `Json` (`std.msh:1123`), which binds `{str: Json}`.

8 in `tests/success` read a shared shape through a dict builtin (`dicts.msh:46,61,64,65,80`, `filter.msh:13`, `match.msh:209,210`).
With read-only dict builtins typed over every dict-kinded type (decided 2026-09-30, design doc §Runtime keys), 7 are accepted.
The one still rejected is `dicts.msh:61`, `setd` on a stored shape literal, which writes; the fix is `as {str: int}` at the literal.

**Shape width/depth subtyping** (61). Almost all are option-dict literals given straight to a builtin (`numFmt` 20, `groupBy` 16, `zipExtract*` 7, `tarPack`/`tarDirExc`, `httpGet`/`httpPost`). These are fresh, so they are accepted.
Two are not fresh: `tar_pack.msh:19` and `zip_pack.msh:19` pass a stored `[path | str | {path: path, archivePath: str, mode: int}]`, but the builtin wants `{path: str | path, ...}` elements. That is rejected (lists are invariant).

**`x`.** Every `x` gets a quote of known arity, except `std.msh:75` (`2each`), which today's checker already rejects.

**`break`/`continue`** (48). All are inside a literal quote given to `loop`, `each` or `iff`, inside a def or the script.
Two have a redirect between the literal and `loop`: `(...) @f > loop` (`terminal_streams.msh:55`) and ``(...) `stdin_for_test.txt` < loop`` (`while_read.msh:3`).
Stage 3 must treat a literal quote followed by redirect words as a literal at the loop site.

**Variables stored at two types.** The following are real retypings that the one-type-per-scope rule rejects:
- `dicts.msh:22,45` (`d` stored as three different shapes);
- `grid_dict_strings.msh:43,57` (`r` as `Grid` and as `GridView`);
- `match.msh` (`v` and `n` bound as `int | str`, `str`, and unknowns across several matches in one scope; H11);
- `msh-scripts/tt:3` (`projs` as `[str]`, then `str`).

These are not real retypings:
- the std.msh and `msh-scripts` rows marked `str` → `str` are the old checker's string-literal type;
- `link_header.msh:5` unifies.

Found: `none linearSearch-result!` followed later by `@x just linearSearch-result!` (`std.msh:113,129`).
Read literally, the first-store rule fixes the variable's type at `Maybe[⊥]`, which holds only `none`, and rejects the second store.
Design changed (2026-09-30): a `⊥` in a store's type is replaced by a new type variable before unifying, so it fixes nothing (design doc, "A ⊥ in a store fixes nothing"). Both std sites then check unchanged.

**`as`** (14). 7 are `[] as [str]` (std.msh completion code) or fresh literal widening (`fresh_container_widen.msh`, `std_lib.msh`); all are still accepted.
2 are brand casts (`unpack_union_bindings.msh:31,35`), which Stage 4 migrates.
None claims a type without evidence.

### Programs these measurements expect Stage 3 to reject on purpose (preview, not complete)

`dicts.msh` (`setd` on a stored shape; `d` stored as three shapes), `match.msh` (binding names), `tar_pack.msh`, `zip_pack.msh`, `grid_dict_strings.msh`, `unpack_union_bindings.msh` (brands; Stage 4).
Programs that use raw `parseJson` results (3 in `tests/success`, 2 in `msh-scripts`) have not been checked by hand yet.

### Left open

Nothing.

## Stage 2: Types and relations (2026-09-30)

Nothing committed. New code, not wired into any checker; the old checker and all suites are unaffected
(test.sh 279/279, typecheck 269/269, `go test` ok).

### What was built

- **Type store** (`Type.go`): `unknown` (`TidUnknown`); one record kind (`TKRecord`) for shapes and dicts,
  with a status per declared label and a remainder (`FieldRequired`, `FieldOptional`, `FieldDeletable`,
  `FieldAbsent`, `FieldOpen`), canonical so records that agree on every label share an id;
  enums (`TKEnum` over `EnumDecl`s) with parameters (`TKParam`); alias references (`TKAlias`, unfolded only
  when a relation looks inside); abstract types (`TKAbstract`). A `never` quote is a `QuoteSig` with
  `Diverges` and no outputs. The printer and the substitution's walker and rewriter know the new kinds.
- **Relations** (`TypeRelations.go`): `Sub` (`subq`), `Retype` (`rsubq`) and `JoinSlot` (`join_slot`,
  `ajoin`), ported case by case in the model's order; assumption sets used only at constructor children,
  threaded through a query and rolled back on a failed union alternative, one set per relation; pairs from
  a query that said yes are kept for later queries. `Kinds`, `Immutable`, `Checkable` (greatest fixed
  points through aliases).
- **Enum declarations** (`TypeEnums.go`): `AnalyzeEnums` computes variance (least fixed point from
  "unused"), fresh-covariance, immutability and checkability (greatest fixed points) over a group of
  declarations; `WellFormedEnum` is `wf_pt`; `SubstParams` is `subst`.
- **Unification** (`TypeUnifier.go`): equality only, with the occurs check; never enters a union; aliases
  unfolded with an assumption set; records each successful pair, with checkpoints that also drop pairs;
  `Recheck` checks every pair again with the final substitution using the proved relations.

### Tests

- `formal-ver/oracle/examples.txt` (the examples of `Decide.v`): all agree.
- Unit tests for S1-S4, the plan's acceptance rows about subtyping, P1-P3, H4, H5, H12, H13, the join table,
  immutability, checkability, kinds, enum analysis (including `Sink[a] = sink (a -- ) Sink[a]`, contravariant,
  and `R[a] = r [R[a]]`, invariant), unification.
- **Against the oracle** (`TestRelationsAgreeWithOracle`, `MSH_ORACLE_QUERIES=50000`): 150,000 random
  questions, all agree (oracle yes: 34,137 `<=`, 35,421 retype; 39,317 joins with a result). Default run:
  3,000. Checked that the test fails when lists are made covariant in `Sub`, and when the assumption set is
  consulted on every step (H13).
- **Properties** (`MSH_PROPERTY_TRIPLES=1000000`): transitivity of `<=` on 64,850 chains of three distinct
  types and of retyping on 90,398; `<=` implies retype on 864,155 pairs. `payload_sub`, `payload_rsub`,
  `payload_imm` on random well-formed enums: 39,451 `<=` and 44,216 retype instances.

### Decisions made in the port

- `Maybe` stays the `TKMaybe` kind, treated by the relations exactly as the covariant, fresh-covariant,
  immutable enum `Maybe[a] = just a | none` (in the model `Maybe` is that enum, `EMaybe`). Revisit in
  stage 4 when constructor patterns are built.
- Go-only kinds: `float`, `bytes`, `path`, `datetime`, `null` are base kinds; a command has the list kind and
  is related only to itself; grids are related only to themselves; a grid is checkable when its schema is
  known and its column types are; a command is not checkable.
- A query has a work limit (2^20 steps); reaching it answers no, which is safe. Only unguarded aliases reach
  it, and stage 4 rejects those at the declaration.
- Generated aliases never mention a type variable: the model requires a recursive type to be closed
  (`mu_ok`), and so does the design (aliases are not generic).

### The join, widened (2026-09-30)

The model's join was narrower than the design doc's join table. Decided: two different enums join to their
union, and where types cannot be widened inside, the join is the other side if one side is below the other
(so a quote that never returns joins with one that does). The doc's example of joining two unrelated quotes,
`(int -- int)` / `(int -- str)`, was dropped: it needs fully known inputs on both quotes, which is contrived,
and the result is awkward to use.

- `Join.v`: `tjoin_core` (the old cases, with different enums giving their union), and `tjoin`, which falls
  back to one side being below the other. `join_slot_ub` proved again; `make check` closed under the global
  context. New proved examples in `Decide.v` (`alg_join_two_enums`, `alg_join_never_quote`,
  `alg_join_unrelated_quotes`, `alg_join_union_below`); the oracle re-extracted, 23 examples agree.
- Go port changed the same way. Against the new oracle: 150,000 questions agree; joins with a result went
  from 39,317 to 46,085.
- Added to stage 3: an error about a union made by a join names the branch each member came from.

### Left open

Nothing.
