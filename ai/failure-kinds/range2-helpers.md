# Range 2 (Evaluator.go 6843-8040): helpers and notes

## Helpers marked E

### `getGroupingOption` (mshell/Evaluator.go:393), used by `numFmt` at 7729

Returns three errors:

- `return nil, true, fmt.Errorf("... must be a list of ints, found a %s ...")` (value is not a list): M, `grouping?: [int]` in `NumFmtOptions`.
- `return nil, true, fmt.Errorf("... must be a list of ints, found a %s ... at index %d")` (item is not an int): M, same reason.
- `return nil, true, fmt.Errorf("... must contain positive integers, found %d at index %d")` (item <= 0): **checked**.
  Reached by a checked program: `{"grouping": [0]} 1000 swap numFmt wl` passes `--type-check-only` and fails here.

The other option helpers (`getIntOption` 289, `getStringOption` 299, `getBoolOption` 310) return only wrong-kind errors,
and every key they read is a declared optional field of `NumFmtOptions` (field types invariant; a `{str: T}` cannot fit it since its fields differ in type), so those sites are M.

## Split sites

None.

## Surprising

### Runtime rejects `MShellLiteral` where the checker says `str` (checker-accepted programs reach M sites)

A bare word in a list literal (`[abc]`) is typed `str` by the checker (TypeCore.go:769), but is an `MShellLiteral` at runtime.
Sites that switch on `MShellString` only, without `MShellLiteral` or `CastString`, fail on it.
Each of these passes `MSHINIT=<empty> msh --type-check-only` and fails at run time with the current binary:

| Line | Word | Program |
|------|------|---------|
| 6869 | `lines` | `[abc] (lines len wl) each` |
| 6991 | `del` (dict key) | `{} as {str: int} d! [foo] (@d swap del drop) each` |
| 7203 | `toFloat` | `[abc] (toFloat drop) each` |
| 7225 | `toInt` | `[abc] (toInt drop) each` |
| 7627 | `dateFmt` (format) | `[abc] (now swap dateFmt wl) each` |

I marked them M: the type is right, and the runtime is the one that's wrong.
The fix belongs in the runtime: accept `MShellLiteral` in those cases (`case MShellString, MShellLiteral` / `CastString`).
After that fix the sites can't be reached, so M stays the right kind.
Other sites in the range already accept literals (`psub`, `fromBase`, `toDt`, `upper`/`lower`/`title`, `joinStringItems`, everything that uses `CastString`).
The same pattern is probably in other ranges: look for any type switch on `MShellString` with no `MShellLiteral` arm.

### `mod` with a non-int/float divisor does nothing (7503)

The outer `switch obj1.(type)` in `mod` has no `default`.
If the top is neither int nor float, both operands are popped and nothing is pushed or reported.
The checker rules that input out (`(int int)` / `(float float)`), so a checked program can't reach it, but unchecked code gets a silently wrong stack.

### 7297 is a commented-out line

`// return state.FailWithMessage(... Error parsing date time ...)` in `toDt` matches the search pattern.
It's dead code (`toDt` now returns `Maybe`). I listed it as C so that every matched line appears once; it needs no change.

### Error text detail

`setenv`/`unsetenv` (7586, 7606) drop the underlying `os.Setenv`/`os.Unsetenv` error from the message.
They're C (an empty name, or one containing `=` or NUL, is rejected by the OS).
