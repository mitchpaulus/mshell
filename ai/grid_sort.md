# Grid Sort

Design for sorting rows of a grid in `msh`.
Companion to `grids_data_frames.md`.

## Goals

Match the priorities from `grids_data_frames.md`:
expressiveness first,
consistency with the rest of the language second,
performance third.

The surface is intentionally narrow.
We ship a column-name sort,
extend the existing list `sortByCmp` and `reverse` to grids,
and rely on `derive` to bridge to "sort by computed value."
We do not ship a dedicated key-extractor form;
the `derive` + `sortBy` pipeline covers it explicitly with no loss of power.

## Signatures

```
sortBy     (Grid|GridView str|[str] -- Grid)
sortByCmp  (Grid|GridView (GridRow GridRow -- int) -- Grid)
reverse    (Grid|GridView -- Grid)
```

`sortByCmp` and `reverse` are polymorphic extensions of the existing list built-ins.
The list signatures remain unchanged.

Stack order, top to bottom on input:

- `sortBy`: spec (str or [str]), then grid.
- `sortByCmp`: comparator quotation, then grid.
- `reverse`: grid.

## `sortBy` semantics

The spec is either:

- a bare `str` — single column name, ascending.
- a `[str]` — multiple column names, lex priority left-to-right, all ascending.

Bare-string and one-element-list forms are equivalent;
the bare string is a convenience.

### Ordering by column type

- `int`, `float` — numeric.
- `string` — lexicographic, byte-wise (Go `<`).
- `datetime` — chronological.
- `bool` — `false` before `true`.
- `none` — last.

`COL_GENERIC` columns sort fine as long as the values present in the rows being
sorted are mutually comparable. Mixing string and int values within the same
column errors at sort time, matching the discipline of `groupBy` / `join` keys.

### Stability

Stable. This is what makes the sequenced-sort idiom (below) correct
and matches the existing list `sortByCmp` (merge sort).

### Direction

`sortBy` only sorts ascending. Descending is composed with `reverse`.
Multi-key with mixed direction is composed via sequenced stable sorts —
see "Mixed direction" below.

### Errors

- Spec is not `str` or `[str]` — error.
- Spec list contains a non-string — error.
- Named column does not exist — error, naming the missing column.
- Cross-type comparison during sort — error, naming the column and the two
  type names involved.

## `sortByCmp` semantics

Polymorphic with the existing list form:

```
[a]                (a a -- int)         -- [a]               # existing
Grid|GridView      (GridRow GridRow -- int) -- Grid          # new
```

The comparator returns `-1`, `0`, or `1`.
Same merge-sort implementation as the list form;
stability is preserved.

The user's comparator owns all of:

- Null/`none` handling — there is no implicit `none`-last for `sortByCmp`.
- Cross-type comparisons — the user defines what those mean (or fails).
- Compound key shape — the user reads whatever fields they need from each row.

This is the escape hatch. We do not constrain it further.

### Errors

- Top of stack is not a quotation — error.
- Comparator leaves zero or more than one value — error.
- Comparator's value is not an `int` — error.
- Comparator propagates a `ShouldPassResultUpStack` result — forwarded as in
  `groupBy` / `join`.

## `reverse` semantics

Currently a list-only built-in. Extended to:

```
Grid|GridView -- Grid
```

Returns a new `Grid` whose rows are in reverse order of the input.
Materializes; storage is freshly allocated, no sharing with the input.

The list signature is unchanged.

## Mixed direction via sequencing

There is no per-key direction in `sortBy`. Mixed-direction multi-key
sorts are expressed by sequencing single-key sorts in
**reverse priority order** (lowest priority first), using `reverse`
where a key needs to be descending.

```
# Want: region asc, age desc, name asc

@grid "name"   sortBy           # lowest priority
       "age"   sortBy reverse    # age desc
       "region" sortBy           # highest priority; stable preserves the rest
```

This works because the sort is stable. It reads slightly unusual
(priority is bottom-up in source order) but each step is one direction,
one key. A `sortByCmp` call can collapse this into one operation when
the user prefers.

## Why no key-extractor form

A `(GridRow -- key)` key extractor form was considered and rejected.

The space of orderings reachable via key extractor + default ordering is
the same space reachable via `derive` + `sortBy` on the derived column,
or via `sortByCmp` directly. The key extractor adds a third spelling for
the same capability without new expressive power.

`derive` + `sortBy` is the explicit form. It produces an inspectable
intermediate column — useful when debugging or when the computed value
is interesting in its own right. If the column is not wanted in the
output, `exclude` it after.

`sortByCmp` is the implicit form. It avoids materializing a column when
the comparison is genuinely just an ordering rule.

Adding `sortByKey` would split usage between three near-identical patterns
and require us to define key-shape rules (scalar vs flat list, `none`
behavior, cross-type behavior) that mirror but slightly differ from
`join` and `groupBy`. The cost is not paid for by the gain.

## Why `none` last for `sortBy` but not `sortByCmp`

`sortBy` has no place for the user to express null intent, so we pick a
default. `none` last matches pandas / Polars default and matches what
most users expect.

`sortByCmp` is user-defined. The user's comparator returns whatever it
returns when one or both arguments are `none`; we don't intercept.

There is no per-key `nulls first` / `nulls last` switch in `sortBy`.
Users who need it use `sortByCmp`, or `derive` a sort key that maps
`none` to a sentinel.

## Algorithm

`sortBy`:

1. Validate inputs: top of stack is `str` or `[str]` of strings;
   below is a `Grid` or `GridView`.
2. Resolve column names against the source grid; error on unknowns.
3. Resolve source indices via `getGridSourceAndIndices`.
4. Build a permutation of those indices using a stable sort with a
   composite comparator that walks the column list left-to-right and
   uses the per-type default order.
5. Project the source columns through the permuted indices into a new
   `Grid`. Reuse `projectGridColumns` or an analog so column meta is
   preserved and `optimizeColumnStorage` runs on each output column.

`sortByCmp` (grid form):

1. Validate inputs: top is a quotation; below is `Grid` or `GridView`.
2. Resolve source indices.
3. Stable merge sort the indices, calling the comparator with two
   `MShellGridRow`s constructed against the source grid.
4. Project as in `sortBy`.

`reverse` (grid form):

1. Resolve source indices.
2. Reverse the index slice.
3. Project as above.

Complexity:

- `sortBy`: `O(n log n)` comparisons, each `O(k)` for `k` keys.
- `sortByCmp`: `O(n log n)` user comparator invocations.
- `reverse`: `O(n)` plus the projection cost.

All three pay the projection cost (one materialized `Grid`).

## Non-dependencies

`sortBy` does **not** require default ordering to be defined on lists.
Compound sorts decompose into sequenced single-key sorts;
no tuple key is ever produced.

`sortBy` does **not** require numeric promotion. `int` and `float`
columns sort as their own type; mixing `int` and `float` cells in a
generic column errors during the sort.

## What this design does not give you, in v1

- Per-key direction in `sortBy`. Use sequencing or `sortByCmp`.
- Per-key null position in `sortBy`. Use `sortByCmp` or `derive` a sentinel.
- A view-returning sort. Output is always a materialized `Grid`.
  Revisit if profiling on real workloads shows projection cost matters.
- An in-place `sortByExtend` analog. The `+` / `extend` split is about
  adding rows; sort just reorders. Revisit on demand.
- A locale- or version-aware default in `sortBy`. Use `sortByCmp` with
  `versionSortCmp` (or future helpers).

## Documentation and code touchpoints

When implementing:

- Built-ins live in `mshell/Evaluator.go` near `groupBy` / `derive` /
  `join`. Reuse `getGridSourceAndIndices`, `projectGridColumns` (or
  factor out a `projectGridColumnsByPermutation` if helpful), and
  `optimizeColumnStorage`.
- Add `sortBy` to `mshell/BuiltInList.go`. `sortByCmp` and `reverse`
  are already listed.
- Update `doc/grid.inc.html`: add the three rows to the Grid Operations
  table and add a Sorting subsection with the mixed-direction example.
- Update `doc/mshell.md` correspondingly.
- `CHANGELOG.md`: under `## Unreleased` → `### Added`, group as a
  `- Functions` bullet. Note that `sortByCmp` and `reverse` are extended
  to `Grid|GridView`.
- Tests in `tests/` covering:
  single bare-string sort,
  list-of-strings sort,
  multi-column lex priority,
  stability under repeated sorts (the mixed-direction example),
  `none`-last placement,
  cross-type error,
  unknown column error,
  `sortByCmp` on a `GridView`,
  `reverse` on a `GridView`,
  `sortByCmp` propagating its quotation's errors.
- Go-level tests for the projection-by-permutation helper if factored out.

## Future work, in priority order

1. Per-key direction in `sortBy` if the sequencing idiom proves to be
   a frequent friction point.
2. View-returning sort (`sortByView` or a flag) if profiling shows the
   materialization cost matters in pipelines.
3. Helpers that produce sortable keys for common non-trivial orderings
   (e.g., a `versionParse` returning a comparable representation),
   so `derive` + `sortBy` covers more cases without `sortByCmp`.
