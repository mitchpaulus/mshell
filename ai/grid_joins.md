# Grid Joins

Design for joining two grids in `msh`.
Companion to `grids_data_frames.md`.

## Goals

Match the priorities from `grids_data_frames.md`:
expressiveness first,
consistency with the rest of the language second,
performance third.

The v1 surface is intentionally narrow.
We ship the equi-join class fast,
keep an explicit escape hatch for everything else,
and defer specialized variants until real workloads ask for them.

## Scope

### In v1

- `join` — inner equi-join.
- `leftJoin` — left outer equi-join.
- `outerJoin` — full outer equi-join.

### Out of v1, deferred

- `crossJoin` — expressible as a constant-key `join` (see Escape hatch below).
  Will revisit as a clarity/perf shortcut.
- `rightJoin` — users swap argument order.
- `asofJoin` — high-value for time-series workloads, distinct algorithm.
  Top of the v2 list.
- `joinOn` — predicate-based, O(n·m).
  Add only if the escape-hatch idiom proves common enough to deserve a name.
- `semiJoin` / `antiJoin` — composable from `filter` plus column lookups.

## Signatures

```
join      (Grid|GridView Grid|GridView (GridRow -- key) (GridRow -- key) -- Grid)
leftJoin  (Grid|GridView Grid|GridView (GridRow -- key) (GridRow -- key) -- Grid)
outerJoin (Grid|GridView Grid|GridView (GridRow -- key) (GridRow -- key) -- Grid)
```

Stack order, top to bottom on input:
right key extractor,
left key extractor,
right grid,
left grid.

So a user writes:

```
leftGrid rightGrid (leftRow -- key) (rightRow -- key) join
```

Pop order matches `groupBy` and `derive`:
the grid is deepest,
the higher-arity arguments are popped first.

## Key extractor semantics

The two quotations are evaluated once per row of their respective side,
each with a `GridRow` on the stack.
Each must leave exactly one value on the stack.

### Allowed key types

- Any non-container scalar:
  `int`, `float`, `string`, `datetime`, `bool`.
- A `list` of the above, treated as a tuple key
  (use this for compound keys).

### Disallowed

- Container types other than a flat `list` of scalars
  (`dict`, nested list, grid, etc.) — error.
- A quotation that leaves zero or more than one value — error.
- A quotation that returns `none` (a `Maybe` with no inner value) — does not match anything.
  Mirrors SQL `NULL ≠ NULL` semantics.
  Both sides observing `none` does not produce a match,
  even with the same source column.

### Cross-side type coercion

Keys must be equal under the existing equality rules used by `groupBy`'s `typedGridGroupKey`.
No implicit numeric coercion across types
(an `int` 1 does not match a `float` 1.0 unless both extractors normalize).
This matches `groupBy` and avoids surprise.

## Output

### Column order

1. All columns from the left grid, in their original order, with their original metadata.
2. All columns from the right grid, in their original order, with their original metadata.

Key extractors are opaque,
so we cannot identify "the join column" to deduplicate.
The user is responsible for shaping output with `select`/`exclude` afterward.

### Column collision policy

If any non-key column name appears in both grids, error before doing any work.
Message names the colliding column.
The user resolves with `select`, `exclude`, or `gridRenameCol` first.

This is strict by design.
Relaxing later (auto-suffix, opt-in suffix) is easy;
tightening later is breaking.

### Row order

- Inner / left: left-grid order is preserved.
  When a left row matches multiple right rows,
  the right matches appear in right-grid order.
- Outer: matched and left-orphan rows in left-grid order,
  then right-orphan rows in right-grid order.

### Grid metadata

The output grid carries the **left** grid's `Meta` dictionary.
Column metadata is preserved per-column from each source.

## Outer join null fills

Unmatched cells use `Maybe.None` (the existing `none`).
Any column that receives a `None` is forced to `COL_GENERIC` storage.

This is a tax on outer joins for affected columns.
A typed-storage-with-nullability bitmap is a future optimization,
not in v1.

## Algorithm

Hash join.

1. Validate inputs:
   both arguments resolve to `Grid` or `GridView`,
   both quotations are quotations,
   no non-key column collisions.
2. Build phase.
   Iterate the **right** grid (or its view's indices).
   Evaluate the right key extractor on each row.
   Skip rows whose key is `none`.
   Insert into `map[typedKey][]int` keyed on the row's typed key.
3. Probe phase.
   Iterate the **left** grid (or its view's indices).
   Evaluate the left key extractor on each row.
   For inner / left joins on a `none` left key, no match;
   for outer, treat as a left orphan.
   Look up the bucket in the build map.
   - Match: emit one output row per right index in the bucket.
   - No match (left/outer): emit one output row with right side filled `none`.
4. Outer-only finalize.
   Track which right indices were matched during probe.
   Append a row per unmatched right index, left side filled `none`.
5. Optimize columns.
   Run `optimizeColumnStorage` on each output column,
   the same hook `groupBy` uses.

Complexity:
build O(m),
probe O(n + matches),
overall O(n + m + matches).

The "always hash, single bucket" idiom for cross product
(`(drop 1) (drop 1) join`) degrades to O(n·m) emits but the same algorithm,
no special case needed.

## Errors

All errors are reported with the standard `t.Line:t.Column` prefix used by other built-ins.

- Wrong types on the stack
  (non-grid input, non-quotation extractor).
- Extractor leaves zero or many values.
- Extractor returns a disallowed key type
  (container, nested list).
- Non-key column name collision between left and right grids.
- Quotation evaluation propagates a `ShouldPassResultUpStack` result —
  forwarded as in `groupBy`.

## Examples

```
# Equi-join on a shared "id" column.
# Output has both an "id" column from each side; user excludes one.
left right (:id) (:id) join ["id"] exclude

# Renamed key.
orders customers (:customer_id) (:id) leftJoin

# Compound key (date + region).
trades positions
  (:date :region [..] joinKey)
  (:date :region [..] joinKey) join

# Case-insensitive equi-join on derived value.
employees managers
  (:name toLower) (:name toLower) join

# Outer join: missing cells are `none`.
left right (:id) (:id) outerJoin

# Predicate escape hatch (cross product + filter).
left right
  (drop 1) (drop 1) join
  (:l_x :r_y_thresholded <) filter
```

## Documentation and code touchpoints

When implementing:

- New built-ins live in `mshell/Evaluator.go` next to `groupBy` / `derive`,
  reusing `getGridSourceAndIndices`, `typedGridGroupKey`, `optimizeColumnStorage`,
  and `isContainerType`.
- Add the lexemes to `mshell/BuiltInList.go`.
- Update `doc/grid.inc.html`:
  add the three rows to the Grid Operations table,
  add a Joins subsection with examples.
- Update `doc/mshell.md`.
- `CHANGELOG.md`:
  under `## Unreleased` → `### Added`, group as a `- Functions` bullet.
- Tests in `tests/` covering:
  inner match, no match, multi-match (cartesian within group),
  left orphans, outer orphans on both sides,
  compound list keys,
  `none` keys do not match,
  column collision error,
  empty left, empty right,
  view inputs (`Grid|GridView`),
  cross-product idiom yields n·m rows.
- Go-level tests in `mshell/` for the hash-join helper if it is factored out.

## Future work, in priority order

1. `asofJoin` — sorted-side merge walk, optional `groupKeys` partition,
   direction (backward default, forward optional),
   tolerance window.
   Most-asked-for non-equi join in the target domain.
2. `crossJoin` — thin wrapper for clarity and to avoid the constant-extractor idiom.
3. Nullable typed-column storage — eliminates the GENERIC-storage tax on outer joins.
4. `joinOn` (predicate, streaming nested loop) — only if real workloads
   show the escape-hatch idiom showing up frequently and confusingly.
5. `semiJoin` / `antiJoin` — only if user-space `filter` patterns are too verbose.
