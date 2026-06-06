# Grid Vertical Concatenation

Design for stacking two grids vertically (row-wise concat) in `msh`.
Companion to `grids_data_frames.md`.

## Goals

Match the priorities from `grids_data_frames.md`:
expressiveness first,
consistency with the rest of the language second,
performance third.

No `union` keyword.
The SQL meaning of `UNION` (dedupe) is not what users want most of the time,
and naming the row-stack `union` would mislead anyone coming from SQL.
The operation is exposed only via `+` and `extend`,
mirroring how lists already compose.

## Signatures

```
+      (Grid|GridView Grid|GridView -- Grid)         # new grid, deep copy
extend (Grid|GridView Grid|GridView -- Grid)         # mutate the lower operand in place
```

Stack order matches the list operators:

- `g1 g2 +` produces a new grid with `g1`'s rows first, then `g2`'s rows.
- `g1 g2 extend` mutates `g1` in place to include `g2`'s rows after its own,
  and leaves `g1` on the stack.

A `GridView` is accepted in either position of both `+` and `extend`.

When the `extend` receiver (`g1`) is a `GridView`,
the underlying source grid is mutated:
new rows are appended to the source's columns,
and the view's `Indices` slice grows to include those new row indices.
This means other handles to the same source grid —
including other views —
will observe the new rows in the source.
Other views' `Indices` are not modified,
so they will not include the new rows in their own iteration,
but reads against the source itself will see them.
This is the unavoidable consequence of `extend` being in place
combined with views being windows over a shared source.

Variadic concat is a left fold:

```
[g1 g2 g3] (+) reduce
```

The fold seed is the first element of the list,
not an empty grid —
see "No empty-grid identity" below.

## Column matching

### Match by name, strict on names

Both grids must have the same set of column names.
If they differ, `+` and `extend` both error.

Column order in the result follows the **left** grid (`g1`).
Columns in `g2` are looked up by name and reordered to match.

### Type handling is dynamic

Strict matching applies to **names only**.
Per-column types resolve dynamically:

- Same type on both sides → that type.
- Either side is `COL_GENERIC` → `COL_GENERIC`.
- Types differ → `COL_GENERIC`.

There is no numeric promotion.
`COL_INT` + `COL_FLOAT` does **not** become `COL_FLOAT`;
it becomes `COL_GENERIC`.
`COL_DATETIME` follows the same rule —
no special handling, no coercion to or from string.

Empty (zero-row) columns are not special-cased.
A `COL_INT` column with zero rows concatenated with a `COL_FLOAT` column produces `COL_GENERIC`,
the same as if both sides had data.
Predictable beats clever.

### `extend` may widen the receiver

When types differ, `extend` rewrites the receiver column's storage in place
to `COL_GENERIC`,
unboxing prior typed values into `MShellObject`s.
The `*GridColumn` pointer is preserved,
so any `GridView` already pointing into that grid continues to read correctly
(reads dispatch on `ColType`).
Callers holding the previous `ColType` value will observe the change.

When the receiver is a `GridView`,
widening applies to the underlying source grid's columns,
not just the rows visible through the view.

## Metadata

`MShellGrid.Meta` and `GridColumn.Meta` are both merged on concat.
The merge is dict-style, **left wins on key conflict**.
Meta and column type are independent:
a column going to `COL_GENERIC` does not drop or alter its meta.

This applies identically to `+` and `extend`.
For `extend`, the receiver's `Meta` is mutated to the merged result.

## No empty-grid identity

There is no implicit identity for `+`.
A zero-column grid does not silently adopt another grid's schema.
The fold pattern uses the first element as the seed,
not an `emptyGrid`.

If a user genuinely needs "empty grid with the same schema as `g`,"
a helper can be added (e.g., `emptyLike`),
but it is out of scope for this spec.
The reasoning:
silently adopting a schema would make schema-mismatch bugs harder to catch,
and the fold-from-first-element idiom is straightforward.

## Mutation and sharing

`+` follows the list `+` rule:
the result is a new grid,
columns are freshly allocated,
no storage is shared with either input.
A subsequent `extend` of the result will not be visible in the inputs.

`extend` mutates the lower operand in place
and returns that same grid on the stack.
Source rows are copied into the receiver's columns;
the source is unaffected.

## Error messages

Schema-mismatch errors use a diff format,
not "left has X, right has Y" prose:

```
Cannot concat grids: column sets differ.
  Missing in right: age, dob
  Extra in right:   city
```

If either side lists more than 10 missing or extra columns,
truncate with a count:

```
  Missing in right: age, dob, ... (12 more)
```

The threshold is implementation-detail;
the principle is that pathological mismatches do not flood the terminal.

## Out of scope for v1

- Outer / lenient concat (fill missing columns with null).
  Grids do not have a null story yet.
- Inner / intersection concat.
  Users can `select` down to common columns explicitly.
- Auto source-id columns (R's `bind_rows(.id=)`, data.table's `idcol=`).
  Users can `derive` a source column before concat.
- Lazy / chained columns.
  Both `+` and `extend` materialize.
  Revisit only if profiling on real ingestion shows it matters.

## Summary table

| Question                        | Decision                                                |
| ------------------------------- | ------------------------------------------------------- |
| Operators                       | `+` (new), `extend` (in place)                          |
| Variadic                        | Left fold over a list, seed = first element             |
| Column match                    | By name, strict                                         |
| Output column order             | Left grid wins                                          |
| Type promotion                  | None; mismatch → `COL_GENERIC`                          |
| `int` + `float`                 | `COL_GENERIC`                                           |
| `datetime` + anything else      | `COL_GENERIC`                                           |
| Empty (zero-row) typed column   | Not special-cased; same lattice                         |
| Empty (zero-column) grid        | Not an identity; no implicit schema adoption            |
| `GridView` operands             | Allowed in either position of `+` and `extend`; view receiver mutates the underlying source grid |
| Grid `Meta` merge               | Dict merge, left wins                                   |
| Column `Meta` merge             | Dict merge, left wins                                   |
| Meta vs column type             | Independent                                             |
| `+` sharing                     | Deep copy, like list `+`                                |
| `extend` widening               | Yes; rewrites receiver column to `COL_GENERIC` if needed |
| Schema-mismatch error           | Diff-style, truncated past a threshold                  |
