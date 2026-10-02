# Range 3 (Evaluator.go 8041-9212): helpers and notes

## E helpers

### `parseZipExtractOptions` (mshell/Evaluator.go:5528), used at 8282 (zipExtract) and 8497 (tarExtract)

- Checked (C):
  - 5552 `options 'overwrite' and 'skipExisting' are mutually exclusive`: both are `bool` in `ExtractOptions`, so the values decide.
  - 5565 `'stripComponents' must be >= 0`: a value of an `int`.
  - 5577 `parseMaxBytesOption`: pass its error through as it is; it marks its own checked error (below).
- Type mismatch (M): 5540, 5546, 5556 (`boolOption`), 5562 (`intOption`), 5571 (`stringOption`).
  `ExtractOptions` types every key these read, so a wrong kind cannot get here.

### `parseZipExtractEntryOptions` (mshell/Evaluator.go:5597), used at 8328 (zipExtractEntry) and 8543 (tarExtractEntry)

- Checked (C):
  - 5620 `mutually exclusive`, as above.
  - 5636 `parseMaxBytesOption`: pass its error through as it is.
- Type mismatch (M): 5608, 5614, 5624, 5630 (`boolOption`).

### `parseMaxBytesOption` (mshell/Evaluator.go:5585), called by both helpers above

- Checked (C): 5590 `option 'maxBytes' must be >= 0`.
- Type mismatch (M): 5587 (`intOption`, a non-int `maxBytes`).

`boolOption`, `intOption` and `stringOption` (5672-5706) return only kind errors (M).
`parseTarDestination` (5646) returns only kind errors too, so 8367 and 8387 are plain M.

## S sites

None.

## Surprising: failures a checked program reaches, but the site is a type mismatch

I confirmed each one with the existing `mshell/msh` binary.
`--type-check-only` accepts the program, and running it hits the site.

1. **Bare words in list literals: a `str` that is an `MShellLiteral` at runtime.**
   The checker types a bare word in a list literal as `str` (TypeCore.go, the `listDepth > 0` branch of `word`), but the runtime pushes `MShellLiteral`.
   Code that type-switches on `MShellString`/`MShellPath` instead of calling `CastString` rejects it:
   - 8194 `[a.txt b.txt] "x.zip" zipPack` gives "zipPack entry 0 is not a string, path, or dictionary. Found Literal."
   - 8407, the same in `tarPack`.
   - 8724 `[foo] 0 nth x! [| foo; 1 |] @x sortBy` gives "sortBy requires a column name (str) ... got Literal."

   These are runtime bugs, not checker holes: the runtime should accept `MShellLiteral` wherever it accepts `str`.
   This is likely to show up elsewhere in Evaluator.go: any `case MShellString:` switch on a value the checker calls `str`.
   I left these sites as M, so the soundness tests catch them until the runtime is fixed.

2. **Checker hole: reads accept a value of unknown type as "any dict".**
   `dictArg` (TypeCoreDict.go:81-83) treats `unknown` (and abstract/rigid types) as `{| open}`.
   An `unknown` is any value, though, for example a value read from a `{}` dict.
   The calculus (sec-unknown) only allows `{| open}` after a `dict d` pattern has checked the kind.
   - 8870 `{"a": 1} as {} "a" get ? "b" get` gives "The stack parameter for 'get' is not a dictionary ... Found a Integer".
   - 8893, the same with `getDef`.
   - 8933, the same with `values`. `keys` goes through the table and is rejected correctly.

   Writes (`set`/`setd` on an unknown) are rejected correctly.
   I left these sites as M; the checker should be fixed.

3. **`sortBy` on a column whose cells have different kinds (8761, marked C).**
   `compareGridGenericCells` (Evaluator.go:2675) fails on a generic column whose cells have different kinds (for example `int | str`), or whose kind it cannot order (lists, dicts, paths, Literal-vs-String).
   The `sortBy` signature (`G_s str | [str]`) accepts any schema, so `[| a; 1; "x" |] "a" sortBy` checks and then fails with "cannot compare String and Integer".
   I marked it C because checked programs reach it.
   By the soundness definition it is a wrong-runtime-kind failure, though.
   Either the checker should require sortable column types for `sortBy` (then the site is M), or the runtime should define a total order across kinds.
   Mitchell should decide.
   `sort` on lists does not have this problem: it orders string casts.

## Minor runtime oddities (not failure-kind issues)

- 8912: the "key not a string" message prints `keyStr`, which is empty at that point, and the dict's type name instead of the key's.
- 8592/8605: the message prints `t.Type` instead of `t.Lexeme`. Both sites are unreachable anyway.
- The `stripComponents` error says "zipExtract option" for `tarExtract` too.
