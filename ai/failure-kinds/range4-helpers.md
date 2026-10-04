# Range 4 (Evaluator.go 9213-10471): helpers and notes

## E sites

### 10094 `toGrid`: `buildGridFromStringRows` (mshell/Evaluator.go:3185)

The checker's signature is `([[str]] -- Grid{*: str})`.
Its error returns:

| Line | Condition | Kind |
|------|-----------|------|
| 3187 | no rows at all (no header row) | C (list length is data) |
| 3192 | header row is not a list | M (`[[str]]`) |
| 3200 | a header cell is not castable to a string | M (`str`) |
| 3203 | duplicate header name | C (header values are data) |
| 3218 | a data row is not a list | M |
| 3221 | a data row's length differs from the header's | C (data) |
| 3227 | a cell is not castable to a string | M |

So the checked returns to mark are 3187, 3203 and 3221.

## Shared note: `EvaluateQuote`'s `err` (C)

Sites 9375, 9408, 9546, 9590, 9705 and 9856 report the `err` of `state.EvaluateQuote` (Evaluator.go:1153).
That error comes only from `MShellQuotation.BuildExecutionContext` (MShellObject.go:592): a redirect file that fails to open (OS), or stdout and stderr redirected to the same file with different append modes.
That last one compares path values, which the checker does not track, so it is a checked failure too.
Failures inside the quote come back in the `EvalResult`, not in `err`.
So all of these are C.
(Other ranges with `EvaluateQuote` sites should agree.)

## Possible checker holes

1. **`groupBy`, table form: aggregation result and key cells are unconstrained.**
   When the spec list is not a literal list of dict literals (for example a variable, `@specs`), `groupBySpecs` (TypeCoreGrid.go:1019) returns false and the table signature
   `(G_s [str] [{agg: (GridView_s -- a), name?: str, meta?: {}}] -- Grid)` applies.
   It has no `deferNotContainer(a)`, so an agg quote that returns a list passes the checker and fails at 9716 ("cannot return a container type").
   It also has no `deferKey(..., ruleGroupKey)` on the key columns, so grouping by a column that holds lists passes the checker and fails at 9672 (via `groupGridRows` -> `appendGridKeyPart`), even with literal key names on a known schema.
   I marked both M.
2. **`groupBy`, literal specs: key cells unchecked when names are not all literal.**
   In `groupBySpecs`, `deferKey` on the key columns runs only when the key names are literal *and* every agg `name` is literal (`!lit || !known` returns first, TypeCoreGrid.go:1111).
   A non-literal agg name with literal key names skips the check on key columns that the checker could otherwise check.
   With run-time key names the cell types are unknown; the checker could still require every column to be a valid key (as `keyRead` does for `gridValues`), or refuse.
3. **`pivot`: row-key cells and the column-key column.**
   `gridPivot` (TypeCoreGrid.go:960) runs `deferKey` on row-key columns only when the row-key list is literal, the result joins with `none`, and the agg result type has no variables left (`!lit || !ok || c.hasVars(at)` returns first).
   It runs `deferCheck(str)` on the column-key column only when the column-key name is literal.
   So 9790 (container row-key cell) and 9805 (non-string column-key cell) are reachable in checked programs today.
   I marked both M (unsure): they are wrong-kind failures, and reachable only because the walker skips the check.
   If the intent is "column names known only at run time read as anything, and that is a checked failure", these become C instead; that should be decided once for 9672, 9790 and 9805 together.

## Smaller notes

- `pivot` (9775): the column-key also being a row-key is never checked by the walker, even with literal names. C, but the checker could catch the literal case.
- `pivot` (9765): duplicate row-key names are not checked by the walker, even when literal (the `select` walker does check). C.
- `parseCsv` (10044): a value that is neither path, literal nor string leaves `reader` nil, and `reader.FieldsPerRecord` panics instead of failing. Unreachable in checked programs (`str | path`), but it is a runtime panic for unchecked ones.
- `groupBy` spec `name` (inside 9656): the runtime accepts only `MShellString`, not a path or literal, matching `name?: str`.
