# Range 1 (Evaluator.go lines 1-6842): helpers and surprises

No `S` sites in this range.

## E helpers

### `validateValue` (mshell/Validate.go:149), sites 1725 and 1788

Returns `(bool, string)`, not an error, so it has to be changed to return an error before the sites can call `failErr`.
Its two messages:

- `cache.err` (Validate.go:153-155), set by `resolveTarget` (Validate.go:131-143): the target type does not resolve. **M**: the checker resolves the same target expression and rejects the program.
  Unsure only in that the runtime type environment is built separately (`declareRuntimeTypes`, or an empty `newRuntimeTypes()` when none was made), so a mismatch between the two environments would show up here.
- `err.Error()` from `env.validate` (Validate.go:156-159), always `errValidateBudget` (Validate.go:194-199, returned at Validate.go:217): the validation ran out of steps. **C**.

### `MShellObject.Index` (mshell/MShellObject.go), sites 2314, 2333, 6663, 6671

Checked (C), index out of range:

- `IndexCheck` (MShellObject.go:1335-1341), used by `MShellLiteral.Index` (1352), `*MShellQuotation.Index` (1367), `*MShellList.Index` (1377), `MShellString.Index` (1388), `MShellPath.Index` (1399), `*MShellPipe.Index` (1410).
- `MShellBinary.Index` (94-96; negative indexes always fail here, still C).
- `*MShellGrid.Index` (2334-2336), `*MShellGridView.Index` (2486-2488), `*MShellGridRow.Index` (2605-2607).

Mismatch (M), the kind has no index: `Maybe.Index` (204), `*MShellEnum.Index` (281), `MShellNull.Index` (346), `*MShellDateTime.Index` (425), `*MShellDict.Index` (513), `MShellBool.Index` (1363), `MShellInt.Index` (1423), `MShellFloat.Index` (1427), `*MShellSimple.Index` (1431).
The checker's index and nth signatures admit only list, str, path, bytes, Grid, GridView, GridRow and pipe.

### `SliceStart` / `SliceEnd` / `Slice` (mshell/MShellObject.go), sites 2381 and 2405

Checked (C), index or range out of bounds:

- `IndexCheckExc` (1343-1349) and `SliceIndexCheck` (1611-1625), used by the Literal, Quotation, List, String, Path and Pipe versions (SliceStart from 1436, SliceEnd from 1525, Slice from 1627).
- `MShellBinary.SliceStart/SliceEnd/Slice` (102-120). Note: `SliceStart` rejects `start == len(b)`, unlike the other kinds, which allow an empty tail.
- `*MShellGrid` (2340-2380) and `*MShellGridView` (2492-2520) range errors.

Mismatch (M), the kind cannot be sliced: Maybe (208-216), Enum (285-293), Null (350-358), DateTime (429-437), Dict (517-523), Bool, Int, Float and Simple (in the 1436-1740 blocks), and `*MShellGridRow` (2611-2619; the checker's slice forms are list/str/path/bytes/pipe/Grid/GridView, so not a row).

## Surprises: checker holes and runtime bugs (all confirmed with `msh --type-check-only` plus a run, built binary of today)

1. **Multi-indexer on a grid or a pipe** (sites 2356, 2389, 2413, and a type hole beyond them).
   The checker types any indexer list with more than one item with the slice table: `(Grid_s -- GridView_s)`, `(pipe -- [cmd])`.
   At run time the parts are joined with `Concat`:
   - `grid :0:,:1:` makes GridRows, and `GridRow.Concat` always fails ("Cannot concatenate a GridRow").
   - `grid 0:1,2:3` makes GridViews, and `GridView.Concat` always fails ("Use compact first").
   - `pipe :0:,1:` fails with "Cannot concatenate a Pipe with a List".
   - `pipe :0:,:1:` **succeeds** and gives a `Pipe`, where the checker says `[cmd]`: `[[echo a] [cat]] | :0:,:1: len` passes the checker and fails at run time with "Cannot get length of a Pipe". That is a real runtime-type mismatch in a checked program (the `len` site is outside this range).
   I classed the Concat sites M so the soundness tests flag them. The fix belongs in the runtime (make multi-indexers on grids/pipes build what the checker says) or in the checker (reject them).
2. **`break` / `continue` in an `else*` condition** (sites 1690, 1693).
   `(false if 1 wl else* break true *if 2 wl end) loop` passes the checker, and the run fails with "Encountered break within else-if condition".
   `ifBlock` in TypeCore.go walks `ei.Condition` with the enclosing loop's break context. It should clear `c.brk` / `c.cont` around the condition, as `formatString` does (or the parser should reject it, like `checkInterpolationControlFlow`). Classed M.
3. **Program literals that do not parse** (sites 1957, 1969, 2309, 2328, 2370, 2400).
   An integer that overflows (`99999999999999999999`) or a float with ~400 digits lexes as a number with a nil `Value`, and nothing static rejects it.
   `5 match 99999999999999999999 : ..., _ : ..., end` and `[1 2 3] :99999999999999999999:` pass the checker and fail at run time.
   This is program text, not data, so I classed these M; the lexer or parser should reject the literal. The plain INTEGER token site in a later range (Evaluator.go:12861, "Error parsing integer") has the same problem.
4. **Runtime bug: nil-pointer panic at Evaluator.go:2398-2400.**
   For a `a:b` slice where only `b` fails to parse, `err` is nil and `err2` is not, and the message calls `err.Error()`.
   `[1 2 3] 1:99999999999999999999 len wl` panics with SIGSEGV.
5. **Empty loop body** (site 2208, classed C).
   `() loop` and `() q! @q loop` pass the checker (it treats the loop as diverging) and fail at run time with "Loop quotation needs a minimum of one token".
   For a stored quote the emptiness is not in the type, so the checker cannot rule it out. Either keep it as a checked failure, or have the runtime loop forever as the type says.
6. **Empty commands** (sites 3983, 4013, 4554, classed C): `[] ;` passes the checker.
   Emptiness is a value, so C is right, but it is a checked failure the types do not mention.
7. Line 4198 is a commented-out `FailWithMessage` call. It is listed (C) only because the grep matches it.
