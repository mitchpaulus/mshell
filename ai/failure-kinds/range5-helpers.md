# Range 5 helpers (Evaluator.go 10472-11940)

## E sites

### 11194: `extendGrid` (mshell/Evaluator.go:3134)

- 3144 `extend receiver must be a Grid or GridView`: M (the caller already switched on Grid/GridView).
- 3149 `return nil, err` from `getGridSourceAndIndices` (Evaluator.go:2603): M (source is a Grid or GridView by the caller's switch; gridExtend requires one).
- 3152 `return nil, err` from `validateGridSchemaMatch` (Evaluator.go:2940, error built by `formatGridSchemaMismatch`): **C**.
  `sameColumns` (TypeCoreGrid.go:805) checks the column sets only when both schemas are exact;
  when either is not exact it returns `unknownSchema()`, and `gridExtend` then accepts it on a new receiver Grid.
  So a fresh grid of unknown schema (from `toGrid`, CSV, etc.) extended with a grid of different columns reaches this.
  Mark at 3152 (or inside `validateGridSchemaMatch`) with `asChecked`.

### 11654: `newHTTPListCookieJar` (mshell/HTTPCookieJar.go:30) and `validateHTTPCookieRecord` (HTTPCookieJar.go:85)

`newHTTPListCookieJar`:
- 33 not a list: M (`cookieJar?: [Cookie]`).
- 38 element not a dict: M.
- 41 wraps the error of `validateHTTPCookieRecord` with `%w`: keeps whatever kind the inner error has, so the wrap must preserve the checked marker.
- 44 duplicate domain/path/name: **C** (values).

`validateHTTPCookieRecord`:
- 88 field not a string, 93 field not a bool: M (the `Cookie` alias declares them `str` / `bool`, required).
- 97 `lastAccess` not a whole-number timestamp: **C** (`int | float`; a fractional, non-finite or overflowing float is a value).
- 101 `expires` not a whole-number timestamp or null: **C** (same, `int | float | null`).
- 107 `sameSite` not one of the allowed strings: **C**.
- 112 domain not normalized: **C**.
- 117 IP / public suffix without hostOnly: **C**.
- 125 path not starting with `/`: **C**.
- 128 `http.Cookie.Valid` failed: **C**.
- 131 `__Secure-` without secure: **C**.
- 134 `__Host-` rules: **C**.
- 139 `partitioned` present: M, unsure. The `Cookie` alias is an exact shape with no `partitioned` field,
  so a checked `[Cookie]` should never hold that key; this is right only if `tryAs` / validation of
  `parseJson` output into `[Cookie]` rejects undeclared keys on an exact shape.

## S sites

None.

## Surprising

- `toFixed` (just before 10503): the decimals int is passed to `fmt.Sprintf("%.*f", ...)` unchecked.
  A negative count is accepted by the checker and Go then prints `%!(BADPREC)` followed by the
  number instead of failing. Runtime bug, not a type hole: it should be a checked failure (value out of range) or clamp to 0.
- `parseHtml` (11095-11124): the switch has no default case; a value that is not str, path or literal
  leaves `reader` nil and `html.Parse(nil)` panics. Unreachable in a checked program (signature `str | path`),
  so it only matters for unchecked runs.
- 11370 / 11408 (`take` / `skip` on a str): the SliceEnd/SliceStart bounds errors cannot happen as written
  (count is non-negative and clamped to the length). Marked C because it is an index check; M would also be correct.
- 11488 (`sortByCmp` grid branch): `getGridSourceAndIndices` cannot fail there; dead check.
- 11606 (`Unknown HTTP method`): dead internal check.
- `EvaluateQuote` errors (filter/map/map2/each/bind) are all C: they come only from
  `BuildExecutionContext` (MShellObject.go:592), which opens redirect files and rejects stdout and stderr
  sent to the same runtime path with different append modes. That path comparison is on run-time values,
  so it is C, not a redirect-state conflict.
- `extend` on grids: `gridExtend` lets a column-set mismatch through when a schema is not exact (see 11194 above).
  That is a C by design, but worth knowing the checker does no column check at all in that case.
