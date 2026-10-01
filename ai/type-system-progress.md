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

## Stage 3 planning (2026-09-30)

Proposal agreed; written into the plan (stage 3 structure, section 5a on performance).

- `Maybe` becomes the built-in enum declaration in the core checker at the start of stage 3; the old checker keeps `TKMaybe`.
- Stage 1 order: the `Maybe` equality fix, `...rest` and pipe slices, and `deepCopy` before stage 3.
- The builtin-table audit is split by category across parallel subagents and reviewed before it lands.
- Performance is a requirement at every stage (plan section 5a). Old checker baseline at `0fe830d`: corpus 453 ms, 117 MB, 1.12 M allocations; an empty check 0.61 ms, 1,980 allocations.

Runtime facts found while planning (none of stage 1 is done on this branch):

- `Maybe.Equals` asserts `Maybe` but the runtime holds `*Maybe`, so every `Maybe` comparison is false (`none none =` too).
- `...rest` caps the slice's capacity, so `append` on it is safe, but `setAt` and `del` still write the source. Pipe slices do not cap at all.
- List equality is not defined, and ordering does not compare containers.
- `parseJson` turns every number into a float.

## Stage 1, first part (2026-09-30)

Not committed (the plan says not to commit unless asked). Suites: test.sh 283 passed, typecheck 272 passed, `go test` ok.

- **`Maybe` equality.** `Maybe.Equals` accepts `*Maybe` as well as `Maybe`. Test: `tests/success/maybe_equality.msh`. Changelog: Fixed.
- **`...rest` and pipe slices are new lists** (item 1). `...rest` and `MShellPipe.Slice*` use `slices.Clone` (one allocation and a `memmove`, as `take`).
  `tests/success/match_rest_zero_copy.msh` is replaced by `match_rest_new_list.msh` (`setAt`, `del` and `append` on `rest`, and a spread in the middle) and `pipe_slice_new_list.msh` (`setAt` and `append` on all three slice forms).
  The zero-copy `...rest` was never released (its changelog entry was under Unreleased), so that entry is removed rather than reversed; pipe slices get a Fixed entry. Docs: one sentence on the spread binding being a new list, in `control-flow.inc.html` and `mshell.md`.
  No code in `lib/std.msh` uses `...rest`, so nothing in the repository becomes quadratic.
- **`deepCopy`** (item 2), `mshell/DeepCopy.go`. Per path; lists, dicts and grids tracked on the path, since every cycle passes through one of them; the path is searched linearly up to 32 entries, then also kept in a map. A cycle is an error that names it (`the list contains itself through index 1, then key "a"`).
  Lists, pipes and generic grid columns are cloned with one `memmove`, and only elements that can hold an object are visited; typed grid columns are cloned or gathered; a view or row becomes a view or row of a new grid with only its rows.
  Old checker: `(t -- t)`. Tests: `tests/success/deep_copy.msh`, `tests/fail/deep_copy_cycle.msh`, `mshell/DeepCopy_test.go` (metadata, dictionary-encoded columns, views, cycles through a `Maybe` and a grid cell, a path past the map threshold).
  `BenchmarkDeepCopyStrList` (1,000 strings): 20 µs, 3 allocations; the profile is the copy itself (`memmove`, GC write barriers).

## Stage 3, step 1: the core checker's skeleton (2026-09-30)

- `Maybe` is the built-in enum `Maybe[a] = just a | none end` in the core types (enum 0 of every arena, reserved name ids); the relations, enum analysis and unifier lost their `TKMaybe` cases. 150,000 oracle questions agree.
- `MSH_CHECKER=core` selects the core checker for `--check-types` and `--type-check-only`. Not documented for users.
- Files: `TypeCoreResolve.go` (type expressions to core types; generics are enum-parameter types, instantiated with new variables at a call and rigid types for a body), `TypeCoreStack.go` (8-byte slots, signatures, the table), `TypeCoreBuiltins.go` (the seed table), `TypeCore.go` (the walker).
- Covered: literals, list and dict literals (ShapeLit freshness, elements joined), stack shuffles that keep or drop fresh marks, variables (one type per scope, `⊥` opened at a store, every store checked again once solved), defs (rigid generics, outputs, `never`, `return`), top-level `return`, `if`/`else*`/`else` with joins, overloads chosen from the argument types, the end-of-unit checks (`Recheck`, stores, unset variables; unsolved variables become `⊥`).
- A generic that is several bare inputs (`(a a -- bool)`) is set to the join of the arguments first; the join is an upper bound (`join_slot_ub`), so it is a valid instantiation, and the result does not depend on argument order.
- `new T` parses in def outputs (`TypeNewExpr`); the old checker ignores it. The core checker uses it in the builtin table; on user defs it is "not checked yet" until step 7.
- Builtin outputs: `new` (always fresh), new lists (fresh when the elements are immutable), and outputs that keep freshness (`just`, `?`).
- Anything not covered yet stops the unit with "the core checker does not check X yet", and words whose table entries are partial say so instead of failing.
- Performance: the arena and name table are overlays of a frozen base (`TypeArena.Overlay`, `NameTable.Overlay`). `BenchmarkCoreCheckEmpty`: 11 µs and 23 allocations per check, against 1.2 ms and 1,991 for the old checker; building the base (`BenchmarkCoreBase`) is 0.34 ms, once per process. `BenchmarkCoreCheckCorpus` is not comparable yet: most units stop at an unchecked construct.
- `tests/typecheck_core_test.sh` (with `core_expected_rejections.txt` and `core_no_longer_errors.txt`, both empty): 38 passed, 0 unexpected, 234 not checked yet. `TestCoreChecker` holds the acceptance rows step 1 covers.

Found: `and` and `or` are runtime builtins missing from `BuiltInList.go`.

## Stage 3, step 2: quotes (2026-09-30)

- `TypeCoreQuote.go`. A quote literal waits in its slot. A word that takes a quote checks the body against its parameter type after its other arguments; `x`, `iff` and `loop` given a literal run it inline (the elaboration to `if` and `loop{...}`); anything else (`dup`, `drop`, a store, a literal, a join of different quotes, a def output, the end of the unit) types it on its own, with new variables for the inputs it reads. Shuffles that only move a slot leave it waiting. Every quote body is checked, even one that is never run.
- Break and continue contexts as in the design: none, the loop's stack (a loop body, and inline `iff`/`x` inside one), or a child stack (a literal given to a child-stack word, under the Each rule's condition). A loop without a reachable break diverges. A stored quote run by `loop` never breaks.
- A loop's stack is the stack at the loop with `⊥` opened to new variables and every slot shared; the entry stack is checked against it once the unit is solved (deferred checks).
- An overloaded word on a union argument is checked per member and the arms joined, the design's elaboration to a match per member (`toFloat` on `int | float`).
- Table: `each`, `map`, `filter` (child stack), `+` on grids. Partial entries say "not checked yet" only when a list or quote is among the arguments, so they do not hide real mismatches.
- Core script: 60 passed, 0 unexpected, 212 not checked yet.

Known gap for step 3: a quote typed on its own that uses an overloaded word on an unknown input is ambiguous (`(1 +) q!`); pending overload choices fix it.

## Stage 3: pending overload choices, match (2026-09-30)

- Pending overload choices (`TypeCoreChoice.go`), as planned: retried after each item while anything new was unified, made when one candidate fits; an ambiguous one left at the end of the unit asks for an annotation (`(1 +) q!` never used is an error; `(1 +) q! 5 @q x` checks).
- `match` (`TypeCoreMatch.go`), done ahead of the table (step 4 of the plan's order) while the table audit runs: kind patterns give the union member; on unknown contents (unknown, abstract, rigid) scalars give the base type, `dict` the read-only `{}`, `list` a list of a new abstract type with the escape check; abstract bindings are an error pointing at `:>`; bindings are stores; `just`/`none`, values, or-patterns, list patterns (with `...rest`), dict patterns, `=>`. Exhaustiveness: every member of the subject's type covered by a kind pattern, `just`+`none`, `true`+`false`, list patterns that cover every length, or a `_` arm. A dict pattern on a key the subject's type says is absent is an error.
- Found: in a pattern `x` is the interpret word, not a name, so `str x` (the plan's H8 row) is rejected by the runtime too; the H8 test uses `str s`.
- `tests/core_no_longer_errors.txt`: `unpack_union_maybe_binding_type.msh` (the arms join to `Maybe[int | float]` and `+` checks per member).
- Core script: 80 passed, 0 unexpected, 192 not checked yet.
