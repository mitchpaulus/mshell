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

## Stage 3, step 3: the builtin table (2026-09-30, four of five categories)

The audit was split across five subagents, each checking `Evaluator.go` against the spec in `ai/builtin-audit/SPEC.md`; their audit tables are in `ai/builtin-audit/` (lists, strings, os, misc; dicts and grids to come). Merged into `TypeCoreBuiltins.go` (147 registrations), with these choices:

- **Strings and paths read through `CastString` take `str | path`, not `int`**, though the runtime also turns an int into its digits. Narrower than the runtime, so sound. Decision pending with Mitchell.
- **No mixed int/float arithmetic**: the runtime rejects `1 2.5 +`, so `(int float -- float)` and `(float int -- float)` are gone (they were in the seed and the old table).
- **`=` / `!=`**: per-type forms for the eight scalars; anything else must join to an equatable type (scalars, `Maybe` of one, dicts whose labels hold unions of them); lists, quotes and grids have no runtime equality. `(a a -- bool)` was unsound.
- `reverse` has no string form; `sort`/`sortV` return `[str]`; `pop` returns `Maybe` of the element; `sortBy` is grid-only; `wln` and `sortVu` are not runtime builtins.
- Freshness marks: `keeps` on `append`, `setAt`, `insert`, `del`, `pop`, `maybe`, `just`, `?`. Not on `extend` or `httpGet`/`httpPost`: they would be fresh only by the dead-object argument `Slice.v` proves for `take`, not proved for them; shared is always safe.
- `HtmlNode` is a built-in recursive alias now (`parseHtml` returns `new HtmlNode`); the runtime's key is `attr`.
- Inference: a `⊥` in an argument is opened at every unifying checking position, not only at stores (`none 5 maybe`); an overload choice whose remaining candidates all give the same outputs takes the first when the unit is solved (`[] sortV`). Both in the design doc, §Typing rules, "Checking positions".

Core script: 160 passed, 0 unexpected, 112 not checked yet. Expected rejections: `unpack_union_bindings.msh` (mixed int/float `+`), `null.msh` (raw `Json` to `int | null`, needs `tryAs`).

Runtime bugs the audit found (not fixed): `5 (true) and` panics (unchecked `.(MShellBool)`, `Evaluator.go:11502`, `11528`); `mod` on a non-number pops two and pushes nothing; `parseHtml` panics on a non-string; `null 1 =` fails while `1 null =` is false; `toFixed` with negative places prints `%!(BADPREC)`; `toJson` of NaN/Inf is empty; `leftPad` counts bytes; `binPaths` pushes `*MShellString`; `psub` leaks a file on a type error; `e`/`ec`/`es` do not check for an existing stderr destination; `httpPost` cannot send bytes; `completionDefs` quotes leak variables into the caller; `~` (home directory), `and`, `or` are missing from `BuiltInList.go`; bare list words are rejected by `lines`, `toInt`, `toFloat`, `md5`, `base64decode`, `utf8Bytes`, `parseLinkHeader` though the checker types them `str`.

### The fifth category: dicts and grids

Merged from `ai/builtin-audit/dictgrid.md`: `keys`, `in`, `set`/`setd` on `{str: a}`, grid metadata (read-only `{}`), `gridRows`, `gridCols`, `gridCompact`, `select`, `exclude`, `toGrid`, `parseCsv`, `derive`, `pivot`, `leftJoin`, `outerJoin`, the grid form of `join`, the list form of `groupBy`, `parseExcel`. Left for steps 5 and 6 (each described in the audit): `get`, `getDef`, `values`, `keyValues` and getters (the Get-Key type), literal-key `set`/`setd`, `gridCol`, `gridValues`, `toDict`, `updateCol`, `gridSetCell`, `gridAddCol`, `gridRemoveCol`, `gridRenameCol`, grid `groupBy`.

Every unknown grid schema is one type (`Grid{0}`) for now. That is safe only while no grid cell is read at a type; step 6 gives each unknown schema its own abstract type, as the design says, before getters on rows are typed. The audit also asks for literal names on stack slots (`set`, `setd`, column names below the top), a "not a container" check on quote outputs (`pivot`, `updateCol`), and fresh grids.

A dict pattern on a key the subject's type says is absent is an error only after `=>`; in a match it is a dead arm (`tests/success/match.msh` relies on that).

Core script: 164 passed, 0 unexpected, 108 not checked yet. More runtime bugs: `gridSetCell` drops a value whose kind does not match the column's storage; a column mixing ints and floats reads ints back as floats; grid `map` takes columns from the first row only; `parseCsv` panics on a non-string; `extend` on a GridView appends rows to the shared grid beneath it; `innerJoin` and `rightJoin` in the old table do not exist.

## Decisions (2026-10-01)

1. String and path arguments do not take `int`, though the runtime turns an int into its digits there. The table stays `str | path`.
2. Read-only builtin parameters: not needed. The cookie jar is fixed by freshness per object, and zipPack/tarPack by one signature per stored list type plus built-in aliases (both below).
3. `del` on dicts added to the runtime (`dict key del`; an absent key is a no-op); the core checker types it on `{str: T}` only.
4. The runtime bugs the audit found are being fixed now, in a separate worktree, one commit per fix.

## Stage 3, step 5: dicts by key (2026-10-01)

- `TypeCoreDict.go`. A string literal keeps its text in its slot (`coreSlot.lit`, a slot is 16 bytes now), so a key below the top is still literal. `get` and `:name` give `Maybe` of the label's type (absent: `⊥`, open: `unknown`); with a runtime key, `Maybe` of the join of every label (Get-Key); `getDef` joins with the default; `values` and `keyValues` read at Get-Key, new lists fresh when the values are immutable; `set`/`setd` with a literal key write the label's type (Set), or give the label the value's type on a fresh dict with a fresh value (Set-Fresh), so `{} "a" 1 set` builds a dict. Grids and rows with the unknown schema read `unknown`.
- P1, P2, P3, P4 and H1 are rejected.
- Inference: at a checking position where unification fails and a variable is involved, records are matched label by label and covariant enum arguments recursively, then checked in full when the unit is solved (design doc, "Checking positions"). `each_def_quote_free_var_shape.msh` needed it.
- Expected rejections added: `dicts.msh` (one variable stored as three shapes), `tar_pack.msh`, `zip_pack.msh` (the last two since removed; see "Built-in aliases" below).
- Core script: 177 passed, 0 unexpected, 96 not checked yet.

## Stage 3: indexing and commands (2026-10-01)

- Indexing goes through two signature sets (`:n:`, and slices or lists of indexers), so unions are checked per member; a pipe indexes to one of its commands and slices to a new list of them.
- Commands (`TypeCoreCommand.go`): a command's type is its argument list and a state per stream (the old `TKCommand`, with a pipe flag in the stdout state, since a pipe and a list are different runtime objects). Redirects and captures change that state in place, so the list must be fresh (P7: `@c *` is an error suggesting deepCopy); `<` and `&` change nothing the type says; a redirect on a quote keeps a literal waiting (`(...) @f > loop`). Running checks the arguments are strings, paths, numbers, dates, or lists of them (the runtime flattens lists), and pushes the captures (`*`, `*b`, `^`, `^b`, `e`, `es`, `ec`) and `?`'s exit code. The old checker's stream-conflict messages are kept.
- Two commands over the same arguments join stream by stream; a stream whose states differ becomes "varied" (above every state), so a list literal like `[[make] 2>&1 [grep x]]` has a type. A pipe of such commands runs (a pipeline uses only its own captures); running or redirecting one varied command alone is an error. Commands are Go-only kinds the oracle does not generate.
- Core script: 251 passed, 0 unexpected, 22 not checked yet.

## Freshness per object (2026-10-01)

Decided with Mitchell: freshness is a property of each object, not of a whole value. A literal around a stored value is new at the top; the stored value keeps its type. This fixes the cookie jar (`{url: "x", cookieJar: @jar} httpGet`, which failed because the request's missing optional keys can be added only to a new dict) without read-only parameters. Design doc: §Freshness per object (new), §Shapes, §deepCopy, §Soundness, the H3 row, "What changes for users".

Rocq (`formal-ver/`, `make check`: every theorem closed under the global context; `make -C oracle test`: 23 examples agree):

- Marks are trees: `Sh`, `Dp`, `MList m`, `MRec (label -> mark)` (`Typing.v`). `msub` retypes a partly new value position by position: `sub` at a stored position, `rsub` at a new one, the per-label rule of `rsub` for a new dict's own type, and never `open` over a partly new label. New rules `ss_m`, `ss_m_refl`, `ss_m_forget`, `tw_nil_m`, `tw_push_m`, `tw_setk_m`.
- `mtyped` (`Invariant.v`): a stored value inside a new one is typed through the store and adds nothing to the region. `Partial.v` proves its structure, stability under store changes that keep live entries, retyping, and commit; `PartialOps.v` proves `set` on a new dict and push on a new list.
- A region's locations now have store type `HDead` until committed (they had a placeholder `HList TBot`). That is what keeps stored values from pointing into a region, with no new invariant clause, and lets an overwritten value in a new dict be forgotten without a commit.
- `tw_kind` takes a stored or new value only (`partial m = false`): under a substitution the arm's type is only `sub`-above the member. `tw_slice`'s result is `Sh` or `Dp`. The other mark-generic rules (`tw_kind_list`, `tw_kind_enum`, `tw_case`) are proved for partly new values as they are.
- Examples: `partly_new_typed` (types and runs `{a: @xs}` retyped to `{a: [int], b?: int}` and a write through it), `hole_literal_no_typing` (H3 has no typing). About 12,800 lines in all.

Core checker (`TypeCorePartial.go`):

- A slot has `part uint16`, 1 + an index into the unit's partly new marks (`corePart`: a new list with an element mark, or a new dict with label marks and a rest mark); the slot stays 16 bytes. List and dict literals with stored values, and `set` with a literal key on a new dict, make them. A checking position retypes by `msub` or commits (`Sub`); `as` keeps the mark when retyped and makes the slot shared when committed. Everything else treats a partly new slot as shared (`dup` and `over` clear the mark through `share()`).
- Fixed a bug found while testing: `matchSub` decided whether a type still had unsolved variables on its solved form, then compared the unsolved form, so `{a: []} as {a?: [int]}` and `{url: "x", cookieJar: []} httpGet` were rejected. It now applies the substitution first.
- `tests/success/http_cookiejar_types.msh` passes unchanged and is off the expected-rejections list. New: `tests/success/partly_new_dict.msh`, `tests/typecheck_fail/h3_literal_around_shared.msh`, and core checker unit tests (nested literals, `set`, list literals, H3, a `dup`ed literal).
- Performance (same corpus including `tests/msh-scripts`, 10 interleaved runs each): corpus check median 13.3 ms against 14.7 ms before, fastest 13.15 against 13.29; 27.89 MB and 57,980 allocations against 27.57 MB and 57,520 (the corpus now also has the two new test files, and the cookie-jar test checks to the end).

## Built-in aliases and the archive words (2026-10-01)

Decided with Mitchell: every record type a builtin takes or gives has a built-in alias, and a builtin that only reads a list has one signature per list type programs store (lists are invariant).

- `coreTableBuilder.alias` (`TypeCoreBuiltins.go`) declares a built-in alias from type text; `builtinAliases` declares `NumFmtOptions`, `Link`, `EnvEvent`, `PackEntry`, `TarDest`, `ExtractOptions`, `ExtractEntryOptions`, `ZipEntryInfo`, `TarEntryInfo`, `Cookie`, `HttpRequest`, `HttpResponse`. The signatures of `numFmt`, `parseLinkHeader`, `envInspect`, the archive words and `httpGet`/`httpPost` (`(HttpRequest -- Maybe[HttpResponse])`) use them. `Json` and `HtmlNode` are declared in `TypeCoreResolve.go` as before, being recursive. Design doc: the alias table under "Checking positions".
- `zipPack` and `tarPack` take `[str]`, `[path]`, `[str | path]` or `[PackEntry]`, so a stored list from `ls` or `lines` needs no `as`.
- `choose` (`TypeCoreChoice.go`) takes the first candidate when the arguments are known, several fit, and all give the same outputs (`sameEffect`); before, only a choice still open at the end of the unit did. A new list literal fits several archive forms.
- `msub` and `matchSub` look through an alias that is not recursive (`plainAlias`, `aliasRecursive` in `TypeCorePartial.go`); without that the cookie-jar literal was rejected against `HttpRequest`.
- Errors: "no matching overload" (`fitsIfNew`) and a type mismatch (`mismatch`) add `storedHint` ("a stored value keeps its type, so give it the type the word takes where it is made, or make a new one with deepCopy") when the argument would fit if it were new. `TypeError.Format` now prints a mismatch's hint after the expected/got text, for the old checker too.
- `tests/success/zip_pack.msh` and `tar_pack.msh` give their stored entry list the entry type written out, since the old checker does not know `PackEntry`; switch them to `as [PackEntry]` when the old checker is deleted (stage 6). Both are off the expected-rejections list. Unit tests in `TestCoreChecker` cover the four forms, each alias in use, `PackEntry` in a def signature, `[]`, and the rejected cases with the hint.
- A user `type` with one of these names silently replaces the built-in one; name collisions are stage 4. User docs for the names wait for stage 8.

## Stage 3, step 6: grids, and the dict forms of map, filter, urlEncode (2026-10-01, second session)

- **Dict forms** (`TypeCoreDict.go`): `map` and `filter` on a dict run the quote per value read at the type of Get-Key and give a new `{str: T}`, fresh when `T` is immutable. `urlEncode` on a dict reads its values at Get-Key; they must be str, int or path, or a list of them, with one form per stored list type (`[str]`, `[int]`, `[path]`, `[str | int | path]`; `coreTable.urlEncodeLists`).
- **Grid types.** A core grid, view or row type's schema is a record type, in the node's `A` (`TypeArena.MakeGridOf`, `GridRecord`); the old checker's grids keep `Extra` as before, with `A = 0`. So the per-label rule relates grids (`Sub`, `Retype` compare the schema records), two new grids join column by column (`joinCore`), and unification, substitution, `SubstParams`, the printer (`Grid{a: int, b: str}`), `Checkable` and the enum walkers look inside the schema. A schema not known statically is the read-only `{| open}` (written `Grid`); `toGrid` gives `{*: str}`. Open question 1 in the plan.
- **Builtin table.** `Grid_s`, `GridView_s`, `GridRow_s` in builtin signatures are a grid whose schema is the generic `s` (`schemaGeneric`; only while the table is built), and `gridForms` writes one candidate per Grid/GridView reading of `G_s`, since unification never enters a union. `filter`, `each`, `sortBy`, `sortByCmp`, `reverse`, `gridRows`, `gridCols`, `gridMeta`, `gridColMeta`, `gridCompact`, `nth`, `len`, indexing and slices keep the schema this way. `newListOut` now also covers a new grid (`newOverImmutable`).
- **`TypeCoreGrid.go`** has the words whose result schema a signature cannot say: grid literals (cells and metadata on their own stacks, columns joined, fresh when every cell is), `gridCol`, `gridValues`, `toDict`, `get` and the getter, `select`/`exclude`/`derive` (literal names), `updateCol` (in place at the column's type on any grid; a type change only on a new Grid; a new grid from a view), `gridSetCell` (always at the column's type: the runtime drops a value of another kind), `gridAddCol`/`gridRemoveCol`/`gridRenameCol` (new Grid only), grid `map`, `+`, `extend` (plain writes on any receiver, widening only a new Grid), `join`/`leftJoin`/`outerJoin` (left then right columns, `none` joined into a side that may be missing), `pivot` (row keys, then `*: a | Maybe[⊥]`), and a grid `groupBy` whose spec list is written at the call (`walk` looks ahead: each `agg` quote is checked against `(GridView -- t)` with its own `t`).
- **Runtime refusals** are checked when the unit is solved (`coreDeferred.rule`): quote results the runtime refuses when they are a container (`updateCol`, `pivot`, `groupBy`), grouping keys (also inside a `Maybe`) and join keys (a `Maybe` looked through, one level of list allowed), each matching `isContainerType`, `appendGridKeyPart` and `appendJoinKey`.
- A list literal of string literals keeps its names in its slot (`litListTag`, `litNames`), as a string literal keeps its text.
- **Expected rejections added** (each rewrite checked in a scratch copy and run: same output): `grid.msh` (`toDict dup ... "y" swap set` adds a key to a dict `dup` made shared), `grid_concat.msh` (`extend` widens a stored grid's int column; `dropped` stored as a Grid and a GridView), `grid_dict_strings.msh` (`r` stored as five grid types; `extend` widens a stored grid's column), `grid_group_keys.msh` (`r` stored as two row types in one scope).
- Tests: grid and dict-form rows in `TestCoreChecker`.
- Suites: test.sh 285 passed; typecheck_test.sh 275 passed; core script 274 passed, 0 unexpected, 1 not checked yet (`uniq.msh`, open question 2); `go test` ok; `typst compile ai/type-core-calculus.typ` ok (Typst 0.15.1, the real file). Rocq not rebuilt (no `formal-ver/` change).
- Performance, same machine, 6 runs: core checker on the corpus 14-17 ms, 29.5 MB, 67,460 allocations (13 more programs now check to the end); the old checker 89-129 ms, 103 MB, 1,007,600 allocations.

Notes for later:

- The grid docs' example `toDict dup "score" get? 70 >= "passed" set` (`doc/grid.inc.html`, `doc/mshell.md`) is rejected by the core rules: `dup` makes the dict shared, and adding a key needs a new dict. `"passed" {} (:score? 70 >=) derive` says the same thing; change the docs at the switch-over (stage 8).
- There is no syntax for a grid schema type. A stored grid whose column must widen later is given the wide type where it is made: `"x" (as int | float) updateCol`.

## Stage 3, step 7: `new` on def outputs, the LSP (2026-10-01, second session)

- `TypeCoreNew.go`. Each exit of a def body (its end and each `return`) records, per output, whether the value there is new (`exit`). Then: `new` written but some exit shared is an error naming that exit; `new` missing but every exit new is an error; `new` on an output whose type holds no list, dict or grid is an error. A def that calls itself and leaves a shared value at an unmarked output is checked once more assuming the mark (`checkBody` again, errors discarded); if that is consistent and every exit is then new, the error is "mark it `new`" (the largest consistent mark). Mutual recursion is not covered: two defs that call each other without marks are accepted with shared outputs (only the "missing new" error is lost; soundness does not depend on it).
- Each of those errors carries a `TypeFix` (`TypeError.Fix`: insert `new `, or delete it). The LSP uses the core checker when `MSH_CHECKER=core` (diagnostics and fixes; `CoreBase.Errors`), offers each fix as a quick fix on its diagnostic, and all of them as one `source.fixAll` action. Test: `TestCodeActionFixesNewMarks`.
- Expected rejection added: `dict_types.msh` (eight defs return new dict literals without `new`; with `new` added it checks under both checkers and prints the same).
- Suites: test.sh 285; typecheck_test.sh 275; core script 274 passed, 0 unexpected, 1 not checked yet; `go test` ok.

## Stage 3, step 8: the corpus (2026-10-01, second session)

`tests/msh-scripts` under the core checker: 73 of 118 pass, the same count as the old checker's baseline (stage 0), but not the same scripts. The 45 that fail:

- 17 use words that are not in the language any more (`o`, `oc`, `os`, `soe`), and 3 do not parse; 3 more are not mshell (`open --type-check-only` from their shebang). They fail under both checkers.
- Real bugs the runtime would hit, which the old checker accepted: `average` (float `/` int), `f2k` and `set_progress` (`toFloat`/`toInt` give a `Maybe`), `filename` (`wl` of a path), `ep_floor_areas` (a `map` quote that leaves nothing; it wants `each`), `antlrc`/`antlrj` (`*.g4` in a list is multiplication), `clean_antlr` (`rm` in a list literal runs the builtin on an empty stack), `binlink` (an `if` without `else` leaves a value on one path only), `refresh-repos-msh` (`absPath` in a def with no inputs), `docx2pdf` (`tmpfile` is never set).
- `dict` in a def signature (`github_repos_msh`, `setdiff2way.msh`, `fg_fpt`, and std's HTML helpers) is not a type, so it is a generic, and a generic can only be passed along. The scripts mean a dict. Worth a hint when a generic is named like a type (`dict`, `list`, `string`), or an error: not done.
- `tt`: one variable stored as `[str]` and then `str` (one type per scope).
- `listToDict` in `lib/std.msh` is declared `-- c`, an output type no input fixes: the P10 pattern, through a std signature (std bodies are not checked, so std signatures are trusted like builtins). The core checker now refuses a call to such a std def (`coreSig.freeOut`, `outputOnlyGeneric`); `listToDict` is the only one. Plan question 3 proposes `-- {str: b}`, with which `tests/success/listToDict.msh` (now an expected rejection), `old_branches` and `copy_construction` check.

`lib/std.msh` checked as a file (its bodies; stage 6 work, recorded here, nothing changed): std defs that return new values without `new` (`sl`, `tsplit`, several completion option builders); `dateFmt`'s signature says `date`, which is a generic, not `datetime`; the HTML helpers use `dict` (stage 6 types them with `HtmlNode`); `2tuple (a b -- [a | b])` has a union of two generics, which the design does not allow (a generic has no kind, design doc §Unions), and its body cannot build one; `listToDict`'s body stores `{}` and sets runtime keys into it (plan question 4); `enumerate` returns `xs (... {index: @i, item: @x}) map`, a shared list of exact shapes, which no written type matches (a written shape is open), so the body needs `as` inside the quote; `completionDefs` is not in the core table yet.

The `enumerate` case points at something to consider: `map`'s result is fresh only when its elements are immutable, because a quote's results are shared values. When the quote's body leaves a value it just made (a literal) on every run, each result is a different new object, and the result list is a tree. Marking such quotes' outputs new would make `xs (... {k: v}) map` fresh, so it could be given a written type with no `as`. That needs the proof first (a `tw_map` whose body output is fresh); not done.

## Stage 3, step 8: acceptance tests (2026-10-01, second session)

The rows of plan section 7 that need nothing from stages 4 and 5 (enums, aliases, `tryAs`, `is`) are test files now: 41 in `tests/typecheck_fail` (P1-P4, P6, P7, P10, P13, P14, H1, H2, H6, H8, H10, H11, S1-S4 cases, `never`, `new`, joins, stores, `as`, `del`, unions, break contexts, `x` of unknown arity) and 17 in `tests/success` with expected output (the `deepCopy` rewrites of P6 and P7, fresh widening, `never` arms and quotes, `new` outputs, width cases). Each was run under both checkers and the runtime (with an empty `MSHINIT`) before it was added.

The old checker accepts 29 of the fail programs (its holes) and rejects 7 of the ok ones. `tests/typecheck_test.sh`, which runs the old checker, now skips the programs listed in `tests/old_checker_accepts.txt` and `tests/old_checker_rejects.txt`; the core script checks them all. Both lists go away at the switch-over.

Found while running them: a def in `~/.config/msh/init.msh` (`f`, `g`) silently wins over a script's own def of the same name at runtime, while the core checker uses the script's. Stage 4's "name collisions are errors" covers it.

Suites: test.sh 302 passed; typecheck_test.sh 297 passed, 0 failed; core script 332 passed, 0 unexpected, 1 not checked yet; `go test` ok.

## Stage 3: definite assignment (2026-10-01, second session)

`TypeCoreAssign.go`, inside the walker rather than a separate pass: each variable has a "set on every path so far" flag with an undo log. `if`, `iff` and `match` keep what every arm that goes on set (an arm that diverges does not count); `loop` keeps what every `break` that leaves it had set; stores in a quote that may run zero times (a literal given to `each`, `map`, a def) or later (a stored quote) do not count; reads in a quote typed on its own are not checked, since it runs later. A read some path reaches unset is an error; a variable stored nowhere is still "unknown identifier". No test program is affected; in `tests/msh-scripts` one read is (`fg_fpt`: `xlsxFile` is set only when `-x` was given, and read under a separate flag). Unit tests in `TestCoreChecker`.

Benchmarks after it (the corpus now has 58 more files, +15%): corpus check 17-18 ms, 35.2 MB, 74,300 allocations; an empty check 3 µs, 25 allocations.

## Where things stand (end of 2026-10-01, second session)

- Committed on `type-checker-enhancements` (not pushed): `f86dc51` (Rocq: freshness per object), `7eff1b5` (std `listToDict`), `782877e` (the core checker, LSP, docs), `41d50c8` (acceptance tests and the old checker's skip lists).
- Suites: `test.sh` 302 passed; `typecheck_test.sh` 297 passed, 0 failed (old checker, with the skip lists); `tests/typecheck_core_test.sh` 333 passed, 0 unexpected, 0 not checked yet (after questions 1-3 were decided); `go test` ok; `typst compile ai/type-core-calculus.typ` ok (Typst 0.15.1, real file). `formal-ver/` unchanged this session, so `make check` was not rerun (Rocq is not installed on this machine).
- `gofmt` has not been run on the changed Go files (not permitted without asking).
- From the first session: one `go test` run failed once with LSP "publishDiagnostics write failed" messages, and 15 more passed; if it recurs, capture `go test -v` output. Not seen this session.
- Open questions in the plan: none. Questions 1-4 were decided this session (below).
- Decided with Mitchell (question 4): `{}` stays the exact empty shape until default parameters land, then becomes `{str: T}` (listed under "Later" in the plan; design doc §Joins).
- Decided with Mitchell (question 2): when several overload candidates fit with different outputs, take the one the arguments fit as they are (`fitAsIs` in `TypeCoreChoice.go`), if exactly one. `uniq` gained the form `[str | path | int | float | datetime]`, and `sort`/`sortV` the form `[str | int | path]` (same outputs, so no rule needed); their "not checked yet" marks are gone. A stored mixed list is given the widest type where it is made. Design doc: "Checking positions".
- Decided with Mitchell: a grid whose schema is not known statically has the read-only `{| open}` schema (question 1), as implemented; the design doc says so.
- Decided with Mitchell: `listToDict` in `lib/std.msh` is now `([a] (a -- str) (a -- b) -- {str: b})`. `listToDict.msh` checks under both checkers and is off the expected-rejections list; all suites unchanged (302 / 297 / 332).
- Fixed: `joinSlot` unified a side with an unsolved variable against `⊥` (setting it to `⊥`, or failing for `[T]`), so a runtime-key read of a dict whose value type was still a variable went wrong. `⊥` now joins to the other side first. Found while trying `{}` as `{str: T}` (plan question 4). Test in `TestCoreChecker`; suites unchanged.
- Core expected rejections: `unpack_union_bindings.msh`, `null.msh`, `dicts.msh`, `grid.msh`, `grid_concat.msh`, `grid_dict_strings.msh`, `grid_group_keys.msh`, `dict_types.msh` (reasons and rewrites in `tests/core_expected_rejections.txt`).
- Stage 3 is complete except: `new` marks across mutually recursive defs; the error that names the branch each member of a join's union came from; a hint when a def signature uses a word like `dict` that is a generic, not a type.
- Next, by the plan: stage 1 item 3 (iterative, cycle-safe walkers for `str`, `toJson`, equality, ordering, ported from the enum branch; needed before stage 4), then stage 4 (aliases and enums). Items 4 (JSON integral numbers as `int`) and 5 (runtime error classification) before stages 5 and 7.

## Stage 1, item 3: iterative walkers for printing, JSON and equality (2026-10-01, third session)

Not committed. Suites: test.sh 304 passed; typecheck_test.sh 297 passed; core script 333 passed, 0 unexpected; `go test` ok.

- `mshell/ValueWalk.go`, ported from the enum branch (`debe03e`, `0d78c14`, `3413d03` and the fixes after them) without its enum parts, and restructured for speed.
  `renderValueDetect` makes `ToString`, `DebugString` and `ToJson` of lists, pipes, dicts, `Maybe`s, grid rows and the JSON of grids and views with one frame per container on an explicit stack, so depth cannot overflow the Go stack.
  The containers with a frame are the current path (searched linearly up to 32, then also a map, as `deepCopy`); meeting one again writes `<cycle>`. Rows are known on the path by grid and index, since they are made on demand.
- `str` and `toJson` of a value that contains itself are an error ("Cannot convert a value that contains itself to a string."); `DebugString` (stack dumps, error messages) shows `<cycle>` and never fails. Changelog: Fixed.
- `equalsIter` is equality of dicts and `Maybe`s with an explicit stack of dict pairs, in sorted key order, depth first, as before, so which pair fails first does not vary. `dagGuard` (from the enum branch) memoizes dict pairs past 2^19 steps, so DAGs are not exponential and two dicts that contain themselves compare equal (equality of the infinite trees, the assumption validation makes).
  Not ported: the enum branch's "the same object is equal to itself" shortcut. It would make a dict holding a list equal to itself, where today it is an error (lists have no equality); the core checker's `=` table never accepts such a dict anyway.
- Also not ported: the enum branch's `sort` by a total order across kinds (`compareValues`). This branch's `sort` compares the items' strings, `<` does not look inside containers, and the core checker types `sort` that way; ordering of enums comes with stage 4.
- One intended output change: a dict's `DebugString` lists its keys sorted (it was Go's map order, which varies from run to run). Everything else is byte-identical with the previous binary on a sample of nested values (`str`, `toJson`, the `stack` dump).
- Tests: `tests/fail/cyclic_str.msh` (two self-containing dicts compare, then `str` of a self-containing list fails), `tests/fail/cyclic_json.msh`; `ValueWalk_test.go` (a million-level value alternating list, dict and `Maybe`; equality of million-level chains; 2^60 paths through 60 shared dicts; cycles; equality keeps its old rules; `<cycle>` in a list, a grid cell holding its grid, a row inside a row of the same grid).
- Performance, against the previous binary (`go test -bench`): JSON of a 1,000-string list 150 µs and 5,016 allocations before, 24 µs and 17 now; JSON of a 20-key dict 6.0 µs to 1.5 µs; `str` of the list about the same (57-65 µs); equality of two 20-key dicts 1.7 µs to 1.2 µs; two `Maybe(int)`s 2.7 ns to 4.5 ns, no allocations either way.

## Stage 4: aliases and enums (2026-10-01, third session)

Not committed. Suites: test.sh 316 passed; typecheck_test.sh 308 passed, 0 failed (old checker, with its skip lists); core script 357 passed, 0 unexpected, 0 not checked yet; `go test` ok; `typst compile` ok (Typst 0.15.1, real file). `formal-ver/` unchanged.

Parser and runtime:

- `enum Name = m1 | m2 T1 T2 | ... end`, with an optional leading `|` and parameters `enum Box[a b] = ...` (`ParseEnumDecl`, `MShellEnumDecl`); `enum` is a keyword. A type name with `[` written against it takes arguments (`Box[int]`, `Pair[int str]`); `Foo [int]` with a space is still a name and a list.
- `MShellEnum` (enum name, member, position, payload). `RegisterEnums` records the declarations of the startup files, the script and each REPL line; a member's name is a constructor word; patterns are a member and a name per payload (`circle r`, `rect w _`, `dot`) or the enum's name, alone or with a name (`Shape s`, `Shape :>`).
- `str`: `member` or `member(p0 p1)`, payloads in their `str` form; `toJson` externally tagged; `=` compares enum, member, payloads (payloads of different kinds are unequal, as dict values are). `deepCopy` copies payloads (path step "payload N of member" in cycle messages). Enum values are in the iterative walkers; frames there are now about 80 bytes (a million-level chain prints in 0.55 s, was 1.2 s).
- Names: a definition whose name is taken (twice, in std, init or the script; a builtin; an enum member) is an error (`CheckDefinitionNames`), at startup, for the script and per REPL line; so is an enum or member name already used, a builtin's, or a pattern word. Messages name the file of the earlier definition (`MShellDefinition.File`, `MShellEnumDecl.File`, `MShellTypeDecl.File`; tokens are unchanged, since the call-stack printer would start printing file names if they carried one).

Core checker (`TypeCoreDecl.go`):

- Declarations in three passes: reserve names (collisions with builtins, std defs, the file's defs, pattern words, built-in types, earlier declarations, startup declarations); resolve alias bodies and enum payloads (parameters as `TKParam`; a recursive reference must pass the parameters in order; unions checked afterwards); reject alias cycles that pass no constructor (H13), check the unions (an enum parameter cannot be a member), `AnalyzeEnums`, `WellFormedEnum` (an internal error if it fails), constructors.
- A constructor is a `coreSig` (payloads to `E[params]`, `keepOut`), so it goes through `apply`: generics, checking positions, fresh when every payload is fresh or immutable. A parameter no payload of the member mentions is `⊥` when covariant, else a new variable.
- Patterns: the enum's name is a kind (`patternKind`); a kind pattern on unknown contents gives `E[k1..kn]`, one abstract type per parameter, each with the escape check; member patterns bind payloads at the subject's arguments; coverage counts members. A binding refused for having an abstract type now stops the unit (no follow-on "unknown identifier").
- `matchSub` unfolds a recursive alias one step against a type that is not an alias (a literal with an unsolved `[]` inside, `as Person`).
- `=`: an enum is equatable when every payload, with its arguments, is (greatest fixed point through recursive enums and aliases).
- Startup files' declarations are declared in the frozen base (`NewCoreBase(stdlibDefs, decls)`); CLI checks report errors in them with their file. The LSP reads the init file too (`loadStartupForLSP`; an init file that does not parse is skipped).
- New error kind `TErrDeclaration`. `tests/typecheck_core_test.sh` uses an empty `MSHINIT`, as `typecheck_test.sh` does (the user's init defined `f` and `g`).

Tests: `tests/success/enum_basic.msh`, `enum_recursive.msh`, `enum_generic.msh`, `enum_deep_copy.msh`, `alias_recursive.msh`, `recursive_types.msh` (from the recursive-type branch); `tests/typecheck_fail/` enum coverage, unknown member, member/def collision, `type A = A`, `type A = int | A`, H13, invariant `Box`, H4, H5, H12, enum kind escape, `Nest[[a]]`, `Box[int] | Box[str]`, `Json | [int]`, duplicate def, and three from the recursive-type branch; `tests/fail/` duplicate def, def named like a builtin, member named like a builtin, member declared twice, pattern arity, cyclic `deepCopy` through a box; `TestCoreDeclarations`, `TestCoreStartupDeclarations`, `TestEnumDeepValue`, `TestEnumSharedSubtrees`, `TestEnumRender`. The old checker's skip lists gained the new success files and the fail files it accepts.

Docs: Enum in `doc/data-types.inc.html`, enum patterns in `control-flow.inc.html`, `mshell.md` (Enums, enum patterns, the names rule); keyword styling for `enum` and `type` in `base.html`; `enum`/`type` in the Sublime and Notepad++ keyword lists (the TextMate grammar has no keyword rule; stage 8). Changelog: enums (Added); names defined once, `enum` a keyword (Changed).

Found:

- `x` is the execute word, so it cannot be a binding name or an unquoted dict key, in literals or shape types (`{x: int}` does not parse; `{"x": int}` does). The plan's H12 row is written with `x`; the test uses `xs`.
- `=>` takes list, dict and `just` patterns only; a member pattern would need the parser to know arities. Recorded in the design doc.
- The type-declaring tests the plan says to migrate (`unpack_union_bindings.msh`, `optional_as_cast_missing_required.msh`, `optional_nested_wrong_type.msh`, `unpack_brand_*_binding_type.msh`) check correctly under the core checker as they are; only their comments mention brands. `TKBrand` goes with the old checker (stage 6).
- The old checker (still the default for `--check-types`) rejects every program that declares an enum.

Left open (plan section 2): question 5, ordering of enum values; question 6, `dict` in a def signature.

## Stage 1, item 4: JSON integral numbers are ints (2026-10-01, third session)

Not committed. Suites: test.sh 317; typecheck_test.sh 309; core script 358, 0 unexpected; `go test` ok.

- `decodeJson` decodes with `UseNumber`; `jsonNumber` gives an `int` for a number with no `.`, `e` or `E` that fits in an int, and a `float` otherwise (so `9223372036854775808` is a float). Invalid input reports `json.Unmarshal`'s message, as before.
- Test: `tests/success/json_numbers.msh` (passes both checkers). Docs: `parseJson` in `functions.inc.html` and `mshell.md`, and the cookie-jar note that said timestamps come back as floats. Changelog: Changed.

## Review of this session's work (2026-10-01, third session)

An independent review (a subagent, read-only) found these; all fixed, each with a test:

- Startup files' signatures were resolved before their declarations, so `def colorName (Color -- str)` in an init file read `Color` as a generic and `5 colorName` checked (then failed at runtime). `NewCoreBase` now declares first. `TestCoreStartupDeclarations`.
- Enums that refer to each other with growing arguments (`A[t] = a B[[t]]`, `B[t] = b A[t]`) hung `equatable`; it now answers no past depth 64 (safe: `=` is refused).
- `q. 1 + end` with a constructor `q`: the checker accepted it and the runtime's prefix-quote path did not construct. Fixed in the runtime. `enum_basic.msh`.
- `deepCopy` of an enum value with shared subtrees took exponential time asking whether it held a list; the answer is now remembered per enum value, and one that holds nothing to copy is shared. `TestDeepCopyEnumSharedSubtrees`.
- Errors in startup declarations (resolve errors, duplicate parameters, unions) now carry their file. `TestCoreStartupDeclarationErrors`.
- `parseJson` turned `1e400` into `+Inf`; it is an error again, with the old message. `tests/fail/json_number_range.msh`.
- REPL: a line whose definitions are refused no longer leaves its enums registered (definitions are checked first, everywhere).
- An enum's debug form (stack dumps, error messages) shows payloads in their debug form (`s("x y")`); `str` is unchanged.
- The runtime pattern check no longer changes the message for non-enum patterns once an enum is declared; the pattern-forms hint lists enum patterns.
- `enum` inside a definition is a parse error.

Not changed: the LSP does not show errors in the init file's declarations (they would appear in the open document at the wrong lines).
Found, not from this session: `[] as Json` (and `as` to any alias whose unfolding is a union with a list member) is rejected: matching a new `[T0]` against a union stops at the union. A kind-directed step (members have distinct kinds, so it is not a guess) would fix it.

## Where things stand (end of 2026-10-01, third session)

- Committed on `type-checker-enhancements` (not pushed): `4975a87` (code, tests, user docs), `983c1dc` (design doc, plan, progress log).
- Suites: `test.sh` 319 passed; `typecheck_test.sh` 0 failed (old checker, with skip lists); `tests/typecheck_core_test.sh` 358 passed, 0 unexpected, 0 not checked yet; `go test` ok; `typst compile ai/type-core-calculus.typ` ok. `formal-ver/` unchanged. `tests/msh-scripts` under the core checker: 75 of 118 pass (73 before).
- Done this session: stage 1 items 3 (walkers) and 4 (JSON ints); stage 4 (aliases and enums), except the two open questions.
- `gofmt` not run.
- Open questions in the plan: 5 (ordering of enum values), 6 (`dict` in a def signature).
- Next, by the plan: stage 5 (one runtime validator; `is T x` and `tryAs` in the parser, runtime and core checker; checkable targets; `deepCopy` as `(τ -- τ•)` in the core table, already so). The `try-as` branch has the `tryAs` token and parser node to read (not to cherry-pick; its validator accepts quotes by kind, which the design does not). Stage 1 item 5 (runtime error classification) before stage 7.

## Questions 5 and 6 answered (2026-10-01, third session)

- Enum ordering (Mitchell): no word orders enum values now; a later builtin may list an enum's members, in the order written. The order is already kept (`MShellEnum.MemberIndex`, the constructor's position). Design doc §Surface updated; question removed.
- `dict` in a def signature (Mitchell): meant as an easy way to write `{str: T}`. The core resolver reads `dict` as `{str: T}` and `list` as `[T]`, a new generic per occurrence (generics named `_1`, `_2`, ...), as the recursive-type branch did; outside a signature either is an error asking for the full form. `list` was not asked about; it follows the recursive-type branch, which treated both alike.
- So std's HTML helpers (`htmlDescendents`, `htmlDescendentsAcc`, `findByTag`), whose `(dict -- [dict])` would give an output generic no input fixes, are typed with `HtmlNode`, as stage 6 planned. The old checker reads `HtmlNode` there as a generic, as it read `dict`.
- Tests: `tests/typecheck_fail/sig_dict_keyword_rejects_int.msh` (from the recursive-type branch; the old checker accepts it, so it is on its skip list), `sig_list_keyword_rejects_int.msh`, `dict_keyword_outside_signature.msh`. Docs: `mshell.md` and `type_system.inc.html` describe the shorthand. No changelog entry until the core checker is the default.

## A freshness hole in `:>` arms (2026-10-01, fourth session)

Found while reading `TypeCoreMatch.go` for stage 5: a match arm written `:>` that binds a name kept the value's fresh mark, though the binding is a store of the value (or a part of it). `[1 2] match list xs :> as [int | str] "a" append drop end  @xs (1 +) map` checked under the core checker and failed at runtime. Now the kept value is shared unless every binding has an immutable type. Test: `tests/typecheck_fail/match_keep_binding_shared.msh` (the old checker rejects it too). Design doc §Freshness, "Which words keep freshness".

## Stage 5: validation (2026-10-01, fourth session)

Committed (not pushed): `c5c2ffc` (code, tests, user docs), `113d4b3` (design doc, plan, progress log), `27f7cc3` (these commit ids). Suites: `test.sh` 326 passed; `typecheck_test.sh` 320 passed, 0 failed (old checker, skip lists); `tests/typecheck_core_test.sh` 374 passed, 0 unexpected, 0 not checked yet; `go test` ok; `typst compile ai/type-core-calculus.typ` ok. `formal-ver/` unchanged.

Parser and runtime:

- `tryAs` is a keyword (`TRYAS`); `<value> tryAs T` is `MShellTryAs`. `is T name` at the start of a match arm is `MShellIsPattern` (`is` is not reserved elsewhere, but is now a pattern word, so no enum member can take it). The try-as branch's parser was read, not cherry-picked.
- `mshell/Validate.go`: one validator. The runtime resolves targets with the checker's resolver (`coreResolver`, `declareAll`) in its own arena; each target is resolved once and cached in its parse node. A mutex guards it (pipeline stages run quotations at the same time).
  Union members have distinct kinds, so the value's kind picks the member and validation is a conjunction: the first failure answers `none`. A set of (object, type) pairs is both the cycle rule and the memo; a container is entered when it has 16 or more elements or a child that is a big container or holds containers, which keeps the walk linear however values are shared. Explicit work stack; budget 2^26 steps, then an error. A list whose stdout or stderr is redirected or captured is not a list type (`<` and `&` are fine: the checker types them as lists). A quote target never validates; neither does a grid (no type names a grid's columns).
- `RegisterEnums` is now `RegisterDeclarations`. It also checks `type` names (twice, an enum's or member's name, a builtin's, a built-in type's, a pattern word), and declares the batch's bodies in new runtime types (`declareRuntimeTypes`, 15 µs to make), refusing the batch on an error (unknown type, unguarded alias, union of one kind). So a script with a bad declaration stops before it runs, with or without the checker, and a REPL line with one adds nothing; the next line can declare the name properly. New runtime types per batch also make each cached target resolve again, so a tryAs in an earlier def that named a type not yet declared finds it.
- Performance (`BenchmarkValidateRecords`, 10,000 JSON records `{name, age, tags: [str]}` against a declared shape): 0.87 ms, no allocations; against `Json` 1.5 ms. A first version that entered every record in the set and looked every key up twice took 2.8 ms and 1.3 MB.

Core checker (`TypeCoreValidate.go`):

- The target must resolve and be checkable; a def's generic gets its own message, as do quotes, enums that hold quotes and grids.
- The operand: fresh, any target, and the result `Maybe[T]` stays fresh (`tw_try_dp`); otherwise the target is immutable (`tw_try_imm`), or the type is below it (`tw_try_sub`; checked once the unit is solved if it has unsolved variables). A partly new value is committed first. Anything else is an error that says to validate where the value is made or to `deepCopy`.
- `is T x`: the same rule on the matched value; it binds `x : T`; with `:>` the value stays at type `T`, fresh only if it was and nothing is bound. Coverage: an `is T` arm covers a member equivalent to `T`, or the whole type.
- The old checker reports `tryAs` and `is` as checked only by the new checker; the four success programs are on `old_checker_rejects.txt`.

An independent review (a subagent) found four problems, all fixed with tests: lists with `<` or `&` failed validation against `[str]`, their type; a list referenced from many slots was walked once per slot (70,000 references to a 1,000-element list ran out of steps); a bad declaration on a REPL line made every later `tryAs` an error; a failed resolution stayed cached after the type was declared. It also improved three messages (an alias or enum whose declaration has an error, and a deferred check on a shared value).

Tests: `tests/success/tryas.msh` (the try-as branch's cases, with JSON integers and without the quote target), `tryas_types.msh` (recursive alias, `new Json` def output, `deepCopy` before a refinement, enums and generic enums, `is` coverage, `is T _ :>`), `tryas_cycle.msh`, `r6_refinements.msh` (R6 with `deepCopy`); `tests/typecheck_fail/` R6, `tryas_stored_dict`, `tryas_quote_enum`, `is_quote_enum`, `tryas_generic`, H7, `is_not_exhaustive`, `is_stored_list`; `tests/fail/` an unknown target, a declaration error (stops before running), `deepCopy` of a cyclic `[Json]`; `Validate_test.go` (same object back, shapes and remainders, commands, enums, `Maybe`, cycles including `j = [j]` against `[[int]]`, 300,000-deep values, 2^60 shared paths, repeated references, the budget, declaration errors, declarations line by line as in the REPL).

Docs: `type_system.inc.html` (Validating Data, and the boundary advice now says `tryAs`), `control-flow.inc.html` (typed patterns), `mshell.md`; `tryAs` in the Sublime and Notepad++ keyword lists and `base.html`; docs rebuilt. Changelog: Added (`tryAs`, `is`), Changed (`tryAs` is a keyword; a `type` name is declared once; a declaration body with an error stops the script before it runs).

Decided with Mitchell (question 7, `tryAs`/`is` on a shared union value): the rule stays as proved (the whole type below the target). When only members already below the target could pass, the error suggests the kind pattern instead of `deepCopy` (`kindPatternHint` in `TypeCoreValidate.go`; cases in `TestCoreChecker`). Design doc §Validation.

Not done, and why:

- Grids: no syntax names a grid's columns, so no grid type is a checkable target (`Relations.Checkable` says a grid with a known, closed schema would be, but none can be written), and the validator answers no for every grid. If a grid schema type is ever added, the validator must check each column's cells.
- `tests/core_expected_rejections.txt` still lists `null.msh`; its rewrite (`'null' parseJson tryAs int | null ? classify`) was checked to pass the core checker with the same output. Programs on that list change at the switch-over (stage 6).

Found: the design doc's H2 example uses `getAt`, which is not an mshell word (indexing is `:n:`).

## Where things stand (end of 2026-10-01, fourth session)

- Committed on `type-checker-enhancements`, 3 commits ahead of `origin` (not pushed): `c5c2ffc`, `113d4b3`, `27f7cc3`. Working tree clean before this note.
- Suites: `test.sh` 326 passed; `typecheck_test.sh` 320 passed, 0 failed (old checker, skipping `tests/old_checker_accepts.txt` and `tests/old_checker_rejects.txt`); `tests/typecheck_core_test.sh` 374 passed, 0 unexpected, 0 not checked yet; `go test ./...` ok (`go vet` has one old warning, `UnreadByte` in `Main.go`); `typst compile ai/type-core-calculus.typ` ok. Rocq is installed here (`~/.opam/rocq-mshell`): `make check` in `formal-ver/` closed under the global context, `make -C formal-ver/oracle test` 23 examples agree. `tests/msh-scripts` under the core checker: 75 of 118 pass (unchanged).
- `gofmt` has not been run on any of the new or changed Go files (not permitted without asking).
- Open questions in the plan: none.
- Stages done: 0, 2, 3 (with the gaps below), 4, 5; stage 1 items 1-4. Not done: stage 1 item 5, stages 6, 7, 8.
- Stage 3 gaps still open: `new` marks across mutually recursive defs (two defs that call each other without marks are accepted with shared outputs; only the "missing `new`" error is lost, soundness does not depend on it); the error that names the branch each member of a join's union came from (plan section 6, stage 3 work list).
- Next, by the plan: stage 6 (switch over: core checker the default for `--check-types`, `--type-check-only` and the LSP; rewrite the programs on `tests/core_expected_rejections.txt` and fold both checkers' test lists into `typecheck_test.sh`; make `lib/std.msh`'s read-only list functions generic and fix the std signatures listed under "Stage 3, step 8: the corpus" (stop and ask about any signature that looks wrong); run `tests/msh-scripts`; delete the old checker's code paths). Stage 1 item 5 (runtime error classification) is independent and must land before stage 7.
- Working notes:
  - Test runs: `MSHINIT=` (empty) still loads `~/.config/msh/.../init.msh`, whose `f` and `g` collide with test programs; point `MSHINIT` at an empty file. The test scripts already do.
  - `tests/typecheck_core_test.sh` runs `mshell/msh`, `tests/test.sh` runs `mshell/mshell`: build both (`cd mshell && ./build.sh`).
  - The design doc's H2 example uses `getAt`, which is not an mshell word (indexing is `:n:`); left as is.

## Stage 6: switch over and delete the old checker (2026-10-01, fifth session)

Committed in `92a84fa` and `f31afe8`. Suites: `test.sh` 325 passed, 1 failed (`fail/json_number_range.msh`, question 10: Go 1.27 changed `encoding/json`'s error text, which the test pins); `typecheck_test.sh` 374 passed, 0 failed (the one script now); `go test ./...` ok; `typst compile ai/type-core-calculus.typ` ok. `formal-ver/` unchanged.
Toolchain on this machine is now Go 1.27.0.

### Performance first

Benchmarks at the start were 4x slower than recorded; the cause is the machine or toolchain, not the code: the end-of-stage-3 commit (`41d50c8`) also takes 56-70 ms on its corpus now (17 ms then), and the current code on that same corpus allocates the same (26.1 MB, 54,400). The corpus has grown from 330 to 520 programs since. GC was 44% of the time, so allocation was the target:

- `vars` was indexed by `NameId` and grew one entry at a time to the size of the whole name table, 96 bytes an entry (a `Token` in each). Now 12-byte entries, sized once; first reads go in a per-unit list.
- `NameTable.Overlay` clipped the base's names, so the first local name copied all of them. An overlay now holds only its own names, numbered from the base's length.
- The overlays' hash-consing maps were made with room for 64 entries; now empty until used.
- Assumption sets were a map per `Sub`/`Retype` query (a third of all allocations). Now one buffer per `Relations`, used as a stack: a query's set is the tail from its start, searched linearly, with a map past 64 pairs; a query started inside another (`Retype` asks `Sub`) takes the buffer above it and gives it back. 150,000 oracle questions agree, with the threshold at 64 and at 1 (every set a map).
- Generics of an instantiation are on a stack with a release mark (no copy per call); `MakeVar` finds a variable's `TypeId` in a slice; `Apply` writes a path-compression entry only when it changes.

| Benchmark (FX-8350, Go 1.27) | Session start | Now |
|---|---|---|
| `BenchmarkCoreCheckCorpus` (520 programs + msh-scripts) | 117-122 ms, 45.4 MB, 96,800 allocs | 50-63 ms, 9.1 MB, 49,700 allocs |
| `BenchmarkCoreCheckEmpty` | 13.5 µs, 10.1 KB, 25 allocs | 4.1 µs, 2.2 KB, 15 allocs |
| `BenchmarkCoreBase` (once per process) | 1.15 ms | 1.23 ms (a few more entries) |
| `BenchmarkLSPDiagnostics/setdiff2way.msh` (parse + check, per edit) | old checker | 2.4 ms, 238 KB |

### `lib/std.msh` checks

Std's bodies are checked as a file (`MSHSTDLIB` empty, `--type-check-only lib/std.msh`); everything passes except questions 8 and 9. Changes:

- Bodies: `enumerate`/`enumerateN` say the type of the record they build (`as {"item": a, "index": int}`); `listToDict` starts from `{} as {str: b}`.
- Signatures: `new` on the outputs that are new on every path (`sl`, `tsplit`, and 15 completion helpers), as the design requires.
- The read-only list functions were already generic; the ones with a concrete element type call builtins that need it (`join` on `[str]`).

Checker changes this needed (design doc updated for each):

- `completionDefs` is typed `( -- new {str: [([str] -- CompletionResult)]})`, with the built-in alias `CompletionResult`; every def with `complete` metadata (startup files or script) must have a signature below `([str] -- CompletionResult)` (`completionSigError`).
- `getDef`'s default is checked against the stored values' type as an argument would be, before the join (`{} getDef` on a `{str: Json}`).
- A union is entered by kind at a checking position with unsolved variables (`matchSub`): `[] as Json` now checks (a known gap from the third session). Joins use the same when unification fails and the other side is solved.
- A join of a new (or partly new) arm and a shared arm takes the shared arm's type when the new value retypes to it (`ss_dp`/`ss_m`, then `ss_forget`/`ss_m_forget`); `Relations.JoinSlot` is unchanged, the checker tries this after it.
- A partly new value may be retyped to the union member of its kind and committed at the union (`markBelow`).
- A type written in a def body may name the def's generics (the rigid types; `coreResolver.bodyGens`). Question 11 asks Mitchell to confirm this and the join rule.

### The switch

- `--check-types`, `--type-check-only` and the LSP use the core checker; `MSH_CHECKER` is gone. The LSP's hover signatures come from the core table (`formatCoreSig`, with `new` marks) and, for words the walker types itself, `coreWalkerSigs`; in-file def signatures resolve with the file's own declarations.
- The 8 programs on the expected-rejections list were rewritten as listed there; each checks and prints the same output. `dicts.msh` also had a line relying on string-literal types through a variable (`"count" ckey! @rec @ckey get? 2 +`); it now matches `int`. `zip_pack.msh`, `tar_pack.msh` use `as [PackEntry]`.
- `tests/typecheck_test.sh` is the one script; `typecheck_core_test.sh` and the four list files are deleted.
- Gaps closed instead of "not checked yet": `soe` (`STOP_ON_ERROR`, `( -- )`), `nth` on a `GridRow` (`unknown`, as `:n:`), a grid `groupBy` whose spec list is not written at the call (`[{agg: (GridView_s -- a), name?: str, meta?: {}}]`, result `Grid` with unknown columns), a quotation pattern on unknown contents (an error that says why). The "unsupported" kind remains only for a construct with no rule, worded as a checker gap to report.
- Ported from the old checker: the info diagnostic for a `?` that can only fail (`Maybe[⊥]` once the unit is solved; the LSP shows info diagnostics, `CoreBase.Diagnostics`), and `dbg`'s snapshot of the stack and variables, with the final types (printed by `--type-check-only` too).
- CLI help text for the two flags no longer says "Phase 10 preview". Changelog: Changed, one grouped entry.

Tests changed by the switch: `TestHoverRequestForBuiltin` (`swap :: (a b -- b a)`, was `(T0 T1 -- T1 T0)`); `TestGetLiteralKeyThroughVariable` became `TestGetKeyThroughVariableIsRuntimeKey` (the design removes string-literal types); `OptionalDictKeys_test.go`, `TypeExpr_test.go`, `TypeUnify_test.go`, `Type_test.go` run against the core (old kinds replaced by records and the `Maybe` enum); new `TestNameTableOverlay`.

### Deleted

`TypeChecker.go`, `TypeCheckProgram.go`, `TypeOverload.go`, `TypeQuote.go`, `TypeBuiltins.go`, `TypeBranch.go`, `TypeCast.go` and their tests (6,553 lines of code and 3,132 of tests; 11,473 lines deleted in all, 1,104 added); the old resolver in `TypeExpr.go`; the kinds `TKMaybe`, `TKDict`, `TKShape`, `TKOverloadedQuote`, `TKBrand`, `TKStrLit`, union brands, the old grid schema tables, `QuoteSig.Bindings`/`Generics` and the rewriter's skip sets, and seven error kinds nothing produced. Helpers the runtime and parser use moved (`builtinSigAST` to `TypeCoreBuiltins.go`, `streamStateDesc` to `TypeCoreCommand.go`, pattern helpers to `Parser.go`). `deadcode` finds nothing left in the type files.

### `tests/msh-scripts`

135 of 150 pass. The 15 that fail, each a real problem or a rule of the design:

- run-time failures the checker catches: `[sort '-V']` and `[clip]` in list literals run the builtin on an empty stack (`pathbins`, `sch`); `unlines` on `[path]` (`eq_type_set_diff`; the runtime refuses paths there);
- raw `Json` used without a check (`gg`, `ph_tag_list`); `str | datetime` claimed `datetime` with `as` (`last_influx_data`);
- one variable stored as `str` and `path` (`docx2pdf`, `fixofficelensdates`); redirect on a stored list (`epdoc`, `oj`); a def returning a new list without `new` (`gitchanges`);
- `dict` in a signature read as `{str: T}` with a generic `T` the body then fixes (`fg_fpt`, `github_repos_msh`, `setdiff2way.msh`);
- `tsv2typ` does not parse.

### Left open

Questions 8-11 in the plan. Stage 3's two small gaps (`new` across mutual recursion; naming the branch each member of a join's union came from). `gofmt` not run. Next by the plan: stage 1 item 5 (runtime error classification), then stage 7.

## Questions 8-11 (2026-10-01, fifth session, continued)

Answered by Mitchell: 8 (`datetime`, the precise type), 10 (our own message), 11 (prove it formally). Suites after: `test.sh` 326 passed, 0 failed; `typecheck_test.sh` 375 passed, 0 failed; `go test ./...` ok; `make check` in `formal-ver/`: every main theorem closed under the global context, `if_join2_alg` added to the list; `typst compile` ok.

- 8: `isoDateFmt`, `isoDateTimeFmt` are `(datetime -- str)` in `lib/std.msh`, `doc/mshell.md` and `doc/functions.inc.html` (other `date` signatures in `functions.inc.html` describe builtins informally; not changed).
- 10: an out-of-range JSON number reports "the number 1e400 is out of range for a float" (`jsonNumberOutOfRange`); `tests/fail/json_number_range.msh.stderr` updated.
- 11, proved in Rocq:
  - the join: `join_slot2` (the proved `join_slot`, then a new or partly new arm taking a non-new arm's type through the checker's retype `rt`), `join_slot2_ub` (each arm reaches the result in two `slot_sub` steps), `if_join2`, and `if_join2_alg` in `Decide.v` with the proved `<=`/retype procedure. The only hypothesis is that `rt` is right when it says yes, stated in the model's relations (`rsub` for a new value; `msub` to a type below the target, or `sub`, for a partly new one).
  - a type in a def body naming the def's generics: `as` is `t_sub`, so `soundness_generic` covers it. `item_gdefs_ok` in `Examples.v` checks `def item (a int -- {item: a, index: int})` once, generically, with the body's `as` at a type naming `TVar 0` (`ss_m`, then `ss_m_forget`); `item_use_never_stuck` calls it at `a = [int]` and appends through the result; `item_use_runs` runs it. Also `maybe_ctors_wf`.
  - Go unit tests in `TestCoreChecker` for this session's rules (kind-directed union steps, the join, `getDef`'s default, body generics, completion definitions, `nth` on a row, `soe`).

Question 9 ("is there a fundamental reason `(a b -- [a | b])` isn't possible?") led to a hole, not from this session: the resolver checked distinct kinds in declarations but not in def signatures, so `def g (a b -- a | b) swap drop end  [1] ["x"] g match list l : @l 0 nth 1 + wl, _ : end` checked and then added 1 to `"x"` (the pattern took the first list member of `[int] | [str]`). Fixed: a generic (signature or rigid) cannot be a union member (`unionKindsError`, `genericName`); test `tests/typecheck_fail/union_generic_member.msh`. `2tuple` (used by `grid_join.msh` and `zip.msh`, which an earlier search missed) is `(a a -- [a])`, typing both uses as before (same output). Also: a startup file's signature that does not resolve is now reported with its file instead of silently becoming an empty type. Question 9 stays open in the plan with the options.
- Question 9 decided (Mitchell): `2tuple` stays `(a a -- [a])`, as long as `5 "a" 2tuple` works; it checks as `[int | str]`. Test `tests/success/two_tuple_mixed.msh`. No open questions.

## Where things stand (end of 2026-10-01, fifth session)

- Committed on `type-checker-enhancements` (not pushed): `92a84fa` (code, tests, user docs, Rocq), `f31afe8` (design doc, plan, progress log), and the commit that records these ids. Working tree clean after it.
- Suites: `tests/test.sh` 327 passed, 0 failed; `tests/typecheck_test.sh` 376 passed, 0 failed (the only type-check script now, one checker, no skip lists); `go test ./...` ok (`go vet` has one old warning, `UnreadByte` in `Main.go`); `make check` in `formal-ver/` closed under the global context for every listed theorem (now including `if_join2_alg`); `typst compile ai/type-core-calculus.typ` ok; docs rebuilt (`cd doc && msh build.msh`). `make -C formal-ver/oracle test` not rerun: `Decide.v`'s extracted functions did not change.
- Go is 1.27.0 on this machine. Benchmarks here are about 4x slower than the numbers recorded in earlier sessions for the same code; compare against a checkout of an older commit (a `git worktree` in the scratchpad) rather than against recorded numbers.
- `gofmt` has not been run on any changed Go file (not permitted without asking).
- Open questions in the plan: none.
- Stages done: 0, 2, 3 (two small gaps below), 4, 5, 6; stage 1 items 1-4. Not done: stage 1 item 5, stages 7, 8.
- Stage 3 gaps still open: `new` marks across mutually recursive defs (only the "missing `new`" error is lost); naming the branch each member of a join's union came from.
- Next, by the plan: stage 1 item 5 (runtime error classification: every `FailWithMessage` in `Evaluator.go` gets a kind, *type mismatch* or *checked error*, plus an option or environment variable that makes the runtime report the kind), then stage 7 (soundness oracle: run `tests/success` and `tests/msh-scripts` and fail on any type mismatch; generated programs; per-builtin contract tests). Stage 8 (docs: `doc/type_system.inc.html`, `doc/mshell.md`'s Type System section, editor grammars) can go alongside; the changelog already has the switch-over entry.
- Where to start reading the checker: `mshell/TypeCore.go` (walker, units, `joinSlot`, `check`/`matchSub`), `TypeCoreBuiltins.go` (the table, aliases, `coreWalkerSigs` for hover), `TypeCoreResolve.go` (type expressions, `unionKindsError`), `TypeRelations.go` (the proved relations; compared with `formal-ver/oracle` by `TestRelationsAgreeWithOracle`).
- Working notes: point `MSHINIT` at an empty file when running tests by hand (the user's init defines names that collide); `tests/test.sh` runs `mshell/mshell` and `typecheck_test.sh` runs `mshell/msh`, so build both with `cd mshell && ./build.sh`; `--type-check-only lib/std.msh` with an empty `MSHSTDLIB` checks std's bodies (it passes).

## Stage 1, item 5: runtime failure kinds (2026-10-01, sixth session)

- `Evaluator.go`: `FailWithMessage`/`failPtr` are gone. Every site calls `TypeMismatch` (a failure the checker must prevent; errors that mean the interpreter is wrong count here) or `CheckedFailure` (one a checked program may meet), or `failErr(err, msg)` for a helper that can fail either way: such a helper marks its checked errors with `checkedErrorf`/`asChecked`, and an unmarked error is a mismatch, so a wrong guess is a false alarm, never a hidden hole. The kind is kept in `EvalState.FailureKind`; with `MSH_ERROR_KIND` set the runtime prints `msh error kind: <kind>` after the message.
- The 939 sites were classified by six subagents against `ai/failure-kinds/SPEC.md` (the test: "can a program the checker accepts reach it?"); their tables and notes are in `ai/failure-kinds/`, applied by `apply.py`. I read every checked site. Result: 721 mismatch, 199 checked, 19 by helper (index/slice range errors, `validateValue`'s budget, the `numFmt` grouping, zip/tar extract options, `toGrid`, `extend`'s column check, cookie-jar values). A redirect that conflicts with one a quote already has is checked (`redirectConflictKind`): a quote's redirects are not part of its type.
- `tests/soundness_test.sh` (stage 7, first part): runs every program in `tests/success` and `tests/fail` that passes the checker, as `test_file.sh` does, and fails on a mismatch or a Go panic. Now: 266 run, 0 mismatches. The 19 `tests/fail` programs that pass the checker all stop with a checked failure or an exit. `tests/msh-scripts` are not run (plan question 13). `FailureKind_test.go` pins a sample of kinds, one per helper.
- Fixed on the way: a nil error dereference when a slice index did not parse (`[1 2 3] 1:99999999999999999999` crashed).

### The audit's runtime fixes were never merged

The twelve commits from decision 4 (2026-10-01, first session) sat on `worktree-agent-a062315c2b5cacf54`. Merged (`95ae869`): bare list words in `lines`, `toInt`, `toFloat`, `md5`, `base64decode`, `utf8Bytes`, `parseLinkHeader`; `and`/`or` with a quote; `mod`; `parseCsv`/`parseHtml` on bad input; `null` equality both ways; `toFixed` with negative places; `toJson` of NaN; `leftPad`; bare words in `>`/`<`; `binPaths`; `psub`; function names in messages. Two of their tests needed the core checker:
- `and`/`or` with a literal quote run it inline (`andOr` in `TypeCoreQuote.go`, the elaboration `b if q else false end`), so a `break` in it leaves the loop.
- A top-level union of one scalar kind and `null` is equatable (the runtime compares `null` with any scalar, both ways). `null_equality.msh` uses `int | null` instead of raw `Json`, which has no operations.

### Checker holes the classification found (each a `tests/typecheck_fail` file now)

- Dict reads (`get`, `getDef`, `values`) on a value of unknown contents read it as any dict (`dictArg`); it may not be a dict.
- `break`/`continue` in an `else*` condition (the runtime refuses them there).
- `$NAME!` took any type (the runtime exports str, path, int).
- `&`, `2>&1`, `1>&2` on a pipe.
- A list of indexers on a grid (rows and views do not concatenate), or with a slice on a pipe; a list of `:n:` alone on a pipe gives a pipe (`table.multi`, `table.multiIndex`).
- `groupBy` and `pivot` key columns named only at run time were not checked; now every column must be a valid key (`deferKeyColumns`, `anyColumn`). A `groupBy` whose spec list is not written at the call had no check on its aggregation result (`gridGroupBy`).
- `sortBy` on a column mixing kinds, or of paths (`ruleSortKey`, `sortKind`).
- A number token that does not fit (literal, index, slice, positional, including non-ASCII digits like `$٣`) is a lexing error (`numberLexemeError`). Changelog: Changed.

Not fixed, decision needed: bare words in list literals (plan question 12).
Found, not changed: `~/.config/msh/init.msh` fails the checker (a generic in a union, line 52), so `--type-check-only` without `MSHINIT` fails on every file; `pivot` does not check duplicate row-key names or a column key that is also a row key, even when literal (checked failures at run time); a literal quote redirected twice is not caught statically (a checked failure).

Performance: same corpus for both (the current tests), `go test -bench`, 8 runs: corpus check 73 ms against 74 ms at `c512c3e`, 9.42 MB, 51,180 allocations against 50,975 (the new deferred checks); parsing the corpus 40.5 ms either way (a temporary benchmark: the number check in the lexer costs nothing measurable).

Suites: `test.sh` 0 failed; `typecheck_test.sh` 393 passed, 0 failed; `soundness_test.sh` 0 mismatches; `go test ./...` ok; `typst compile` ok.
Commits: `69f89a0` (classification), `95ae869` (merge), `d8e6f7b` (holes), `f9931e5` (`sortBy`).

## Stage 7: builtin contracts and generated programs; stage 3's gaps; stage 8 docs (2026-10-01, seventh session)

### Builtin contract tests (`mshell/TypeContract_test.go`)

`TestBuiltinContracts` runs every candidate of the core table (333, tokens, indexers and slices included) on random inputs of its declared types: generics instantiated from a pool, grid schemas from exact records, values written as mshell literals and pinned at the instance with `as`, both new (literals) and shared (through variables). It runs only programs the checker accepts, since the walker adds rules to some entries, and fails on a type mismatch or a panic, an output that does not validate against its type, a shared input whose type changed, or an output the checker would mark fresh that is not a tree of new objects. Fixture files (text, JSON, CSV, xlsx, zip, tar) let the file-reading builtins run. Words that write files, change the process, or wait for input are skipped (`contractSkip`). `MSH_CONTRACT_TRIALS` (default 12; 300 gives 97,000 programs in 9 s).

Found: a `groupBy` spec was an open shape, so `[{name: "x", agg: (...), k0: 5}]` checked and the runtime refused the key with a type mismatch. Specs are exact now, in the table and in `gridGroupBy` (`tests/typecheck_fail/groupby_spec_unknown_key.msh`). Two false alarms on the way showed the test must compare against the instance the checker uses (`[]` is polymorphic) and must not test `appendBelow` with a list (the walker never uses it so).

### The generated-program oracle (`mshell/TypeSoundGen_test.go`)

`TestGeneratedProgramsSound` builds each program one statement at a time and keeps a statement only if the whole program still checks, so every finished program is one the checker accepts, with whatever risky statements got through. Families: variables, aliases, widening with `as` (risky on stored values), writes through every view, joins, partly new dicts, tryAs and `is`, defs and calls, each, loop, quotes stored and run, matches, grids (literals, column changes on new grids, updateCol, gridSetCell, extend, views, select, rows, toDict, gridCol, derive, +, sortBy, groupBy, joins, pivot, map, gridCompact, gridValues), dict words (runtime keys, values, keyValues, getDef, map, filter, set-built dicts), quotes with inputs (pending overload choices), `:>` arms with bindings, commands with redirects (P7), generic defs, loops carrying a value, shuffled quote literals, quote parameters, unknown contents, equality, format strings. Each program gives one family more weight. After a statement that gives a value a second, wider type it writes a value outside the first type through the new view and reads the original back (`exploit`), so an accepted hole becomes a type mismatch. Every variable is read back at its type at the end (`consume`). A failure is shrunk and printed. `MSH_GEN_PROGRAMS` (default 60), `MSH_GEN_SEED`, `MSH_GEN_WORKERS`, `MSH_GEN_DEBUG` (print each refused statement with its error), `MSH_GEN_SHOW=seed` (print one program).

Found: plan question 12 at once (`[echo "a b"]`: a `str` arm does not match the bare word); the generator writes command names as strings until it is decided. Nothing else: the final run, seeds 100000-109999 with 4 workers, was 10,000 programs, 1,001,679 lines, 923,352 checks, 28,954 risky statements accepted and 73,785 refused, 9,409 programs ran to the end, no type mismatch or panic, peak heap 9 MB, 18 minutes.

**Resource incident.** A run of 10,000 programs, alongside the full test suite, grew to 26 GB and was killed by the kernel: one program extended a list inside two nested `each` loops over that same list, doubling it every pass. Fixed two ways: every statement that grows a list or grid does it only while the list is shorter than 16 (`grow`), which holds through any number of names for the list; and a watchdog stops the test past `MSH_GEN_MEMCAP` bytes of heap (default 2 GB) or `MSH_GEN_TIMEOUT` seconds for one program (default 30), printing the seeds in progress (it found the culprit, seed 101172, at 2 GB). Normal runs peak at about 5 MB. Large runs: fewer workers, nothing else running.

### Checker changes

- **Overload pre-filter** (`mayFit`): a candidate whose input kinds cannot hold the arguments is skipped before it is instantiated, comparing type heads only, and answering "maybe" for generics, variables, unions, aliases and ⊥. Checked to never skip a candidate `argsFit` accepts (on the corpus, the typecheck suite and generated programs). Corpus 73 → 65 ms; generated programs 26 → 20 ms, 22% fewer bytes.
- **Errors name the arms of a join** (stage 3 gap): `coreSlot.origin` (the slot is 20 bytes now) and `coreVar.origin` record the join that made a union; `mismatchSlot`, "no matching overload", stores and conditions add "`int | str` comes from the `if` at line 1: the `if` branch (line 2) leaves int, the `else` branch (line 4) leaves str". Labels for if, else*, else, the missing else, match arms (pattern snippet), iff quotes.
- **`new` across mutual recursion** (stage 3 gap): `checkDefs` checks every body, then decides marks per strongly connected component of the call graph (`defGroups`), iterating the assumption to the largest consistent set. `tests/typecheck_fail/new_mutual_recursion.msh`, `tests/success/new_mutual_recursion.msh`.
- **⊥ in a grid schema** is opened at a store (`openBottom`), so `[| c; none |] g!` can take `5 just` later.
- **A walker word given a stack none of its forms fits** (`derive` with its arguments in the wrong order) reports the stack and the forms, not "the type checker has no rule ... please report this". The hover form of `derive` was wrong (`(str Grid ...)`); it is `(Grid str {} (GridRow -- T) -- new Grid)`.

### Benchmarks

`BenchmarkCoreCheckGenerated` and `BenchmarkParseGenerated` time six fixed generated programs (`mshell/testdata/generated`, 50 KB, denser in types and matches than the corpus). Checking is linear: about 400-540 ns per byte (FX-8350). Parsing costs about as much as checking on these files, with 37,000 allocations for 50 KB (the lexer makes a string per token); not a type-system change, noted for Mitchell.

### Docs (stage 8)

`doc/type_system.inc.html` rewritten for the current checker (new and stored values, `deepCopy`, changing a type in place, `as` needing evidence, unions of distinct kinds, aliases and the built-in aliases, `never`, `new` outputs, shapes and remainders and when one fits another, runtime keys, grids, quotations checked against their consumer, joins, one type per variable, definite assignment, unknown contents); every example was run through the checker. `doc/mshell.md`'s Type System section likewise, for agents. `doc/execution.inc.html`: a redirect needs a new list. Grammars: `match` and `as` added to the Sublime and Notepad++ keyword lists; the TextMate grammar (VS Code) gets keyword rules, which it had none of.

### Found, not changed

- `gridSetCell` silently drops a value whose kind does not match the column's storage (known from the audit). A join of two new grids makes it reachable with a value the static type allows (`true if [| a; 1 |] else [| a; "x" |] end g!  @g "a" 0 "y" gridSetCell` leaves `[1]`). Not unsound, but a write that does nothing.
- `mshell/mshell.test` (a test binary) is tracked in git since `749145c` (#320); `go test -cpuprofile` in `mshell/` overwrites it. Probably should be removed from the repository and ignored.
