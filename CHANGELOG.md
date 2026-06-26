# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Functions
  - `clip`: Copy a string to the system clipboard. Cross-platform, using
    `pbcopy` on macOS, `clip` on Windows, and `wl-copy`/`xclip`/`xsel` on Linux.
    `(str -- )`
  - `uuid`: Generate a random (version 4) UUID per RFC 9562 as a canonical
    lowercase hyphenated string. `( -- str)`
  - `uuid7`: Generate a time-ordered (version 7) UUID per RFC 9562, whose leading
    bits encode a Unix millisecond timestamp so values sort chronologically.
    `( -- str)`
  - `intCmp`: Compare two ints and return -1, 0, or 1. Useful with `sortByCmp`.
    `(int int -- int)`
  - `dateTimeCmp`: Compare two datetimes and return -1, 0, or 1. Useful with
    `sortByCmp`. `(datetime datetime -- int)`
- `unsetenv`: Remove an environment variable by name. Unsetting a variable that
  does not exist is not an error. `(str -- )`
- `modTime`: Return a file's last modification time as a `datetime`, the one file
  timestamp portable across operating systems and filesystems. Returns a `Maybe`
  (`None` when the file is missing or cannot be stat'd). `(str|path -- Maybe[datetime])`
- The language server now offers completion on `$` environment variables, drawing
  from the actual process environment as well as any environment variables already
  referenced in the current file.
- `match` arms can now bind the matched value when matching on a type keyword by
  following it with a name, e.g. `str s : @s len` (mirroring `just v`). Works for
  every type keyword (`int`, `float`, `str`, `bool`, `list`, `dict`, `path`,
  `date`, `quotation`, `maybe`, `binary`).

### Fixed

- Interactive programs now work as a stage of a pipeline. A command that drives
  the terminal (e.g. `... | nvim -`, `... | less`, `... | fzf`) is no longer
  stopped on startup: every external stage of a pipeline is now placed in one
  shared process group that becomes the terminal's foreground group, instead of
  each stage getting its own group with only one (whichever started first)
  receiving the terminal.
- The file manager preview now times out instead of hanging when a file is slow
  to read. Cloud-backed files (e.g. OneDrive "files on demand") could block the
  preview worker indefinitely while hydrating, freezing previews for every other
  entry; a slow preview now gives up after a few seconds and shows a placeholder.
- Match arms that are not a recognized pattern form now produce a clear error
  listing the legal forms, instead of silently failing to bind (and later
  reporting a confusing "unknown identifier" in the arm body).
- Type-checker diagnostics for unknown identifiers inside a `$"...{ }"` format
  string interpolation now point at the interpolation's actual source location
  rather than line 1, column 1.

- The type checker now joins the arms of a `match` or `if`/`else` block into a
  single union post-state instead of treating each arm as an independent
  alternative typing. Previously, arms that left different types on the stack
  (e.g. `match []: 0.0, _ :> sum end`, which yields `int | float`) fanned out, and
  a later operation that was valid for only one arm let the whole program pass —
  hiding a real type error that would crash at runtime. Such usage is now reported.
- Overloaded built-ins now accept a union operand (such as the `int | float` a
  `match`/`if` join produces) when every member of the union is handled. The
  checker resolves the call for each member and yields the union of the results,
  so `int | float toFloat` gives `float` and `int | float { … } numFmt` formats.
  An unsafe combination is still rejected — e.g. `int | float` divided by a
  `float` fails, because the `int` case has no matching overload.
- In the interactive `::` CLI shorthand, bare literals in argument position are no
  longer turned into strings, so operators work again (e.g. `:: numargs '*' glob`
  now runs `glob` as the wildcard operator instead of passing the word `glob`).
  The leading command name is still treated as a command, so an executable continues
  to win over a builtin of the same name (e.g. `date`, `sort`).

### Changed

- A command that cannot start (not found, permission denied, bad format, ...) run
  with `?` or `;` no longer aborts the script.
  Instead, `?` leaves a negative exit code carrying the exact reason: `-(256+errno)`
  for POSIX start failures, `-(1024+winerror)` on Windows, `-(128+signal)` for a
  process killed by a signal (replacing the old flat `-1`), `-255` for a command
  not found on `PATH`, and `-256` when the OS error cannot be read.
  Negative codes never collide with a real exit status (`0`-`255`).
  `!` still stops on these, exiting `msh` itself with the conventional 127/126/128+N.

### Removed

- The `pick` stack operator was removed.
  Its stack effect depends on a runtime integer, so it could not be expressed in the static type checker, and it saw no real use.

### Added

- Type checking v1!
  - Quotes built from overloaded builtins whose arms all produce the same
    output (e.g. the `str|path` file ops like `cd`, `toPath`, `readFile`)
    now infer as a single union-input quote instead of an overloaded one,
    so they can be used directly as `iff`/`loop` branch quotes
    (e.g. `… (drop) (cd) iff`).
- File manager yank bindings that copy to the system clipboard via `wl-copy`/`xclip`/`xsel`/`pbcopy`/`clip`:
  - `yf` — copy the selected entry's file name
  - `yp` — copy the selected entry's absolute path
  - `yg` — copy the selected entry's path relative to the enclosing `.git` directory
- File manager popup that lists available follow-up keys whenever a multi-key prefix (`y`, `g`) is pending
- Grid (data frame) type with columnar storage for high-performance tabular data
  - Literal syntax: `[| col1, col2; val1, val2; val3, val4 |]`
  - Optional grid and column metadata
  - Typed column storage (int, float, string, datetime) with automatic optimization
  - `GridView` for filtered views without data copying
  - `GridRow` for lazy row access without allocation
- Extended `map` to work with Grid and GridView (transforms rows using quotation returning dict)
- Extended `len` to work with Grid, GridView, and GridRow
- Extended `get` and `:` getter to work with GridRow
- Functions
  - `gridRows` - get row count
  - `gridCols` - get list of column names
  - `gridMeta` - get grid-level metadata
  - `gridColMeta` - get column metadata
  - `gridCol` - extract a column as a list
  - `gridAddCol` - add a column
  - `gridRemoveCol` - remove a column
  - `gridRenameCol` - rename a column
  - `gridSetCell` - set a single cell value
  - `gridValues` - extract grid values as row-major lists without headers
  - `gridCompact` - materialize a GridView to a Grid
  - `select` - project a grid to a specific ordered set of columns
  - `exclude` - drop a set of columns from a grid
  - `derive` - append a derived column to a grid
  - `groupBy` - group grids by key columns with multiple aggregation specs, and preserve existing list grouping behavior
  - `pivot` - reshape a Grid or GridView into a pivot table; rows are grouped by `rowKeys`, distinct `colKey` values become new columns ordered by version-sort, and each cell aggregates matching source rows (empty cells fill with `none`)
  - `updateCol` - mutate a grid column by applying a quotation to each cell
  - `toGrid` - build a grid from `[[str]]` with headers on the first row
  - `join` (grid form) - inner equi-join of two grids via key-extractor quotations; polymorphic with the existing string `join`
  - `leftJoin` - left outer equi-join of two grids
  - `outerJoin` - full outer equi-join of two grids
  - `filter` - now a built-in that works on both Lists and Grids/GridViews
  - `each` - now a built-in that works on both Lists and Grids/GridViews
  - `toDict` - convert a GridRow to a dictionary
  - `+` and `extend` for vertical concatenation of Grids/GridViews. Strict matching by name (left-grid order wins); type mismatch produces a generic column with no numeric promotion; meta merges left-wins. `+` deep-copies; `extend` mutates the receiver in place, widening to generic when needed, and accepts a GridView in either position (the underlying source grid grows and the view's indices extend to include the new rows).
- CLI
  - `msh edit init` to open the current init file path using `$EDITOR`, with fallback to the platform default opener when `$EDITOR` is unavailable
  - `--type-check-only` to run static type checking and exit without evaluating the script
- Functions
  - `toCsvCell`
  - `toCsv`
  - `linearSearchIndex`
  - `id` / `2id` / `3id` - identity quotes useful as no-op value selectors for `listToDict` and similar
  - `parseExcel` - parse an `.xlsx` workbook into a list of sheets in workbook (tab) order; each sheet is a dict with a `name` key, a `data` key holding a rectangular list of rows, a `hidden` key (bool), and a `visibility` key (`"visible"`/`"hidden"`/`"veryHidden"`)
  - `sortBy` - stable ascending sort of a Grid or GridView by one or more columns; bare-string and list-of-strings forms; `none` cells sort last; cross-type values in a generic column error
  - `sortByCmp` extended to accept Grid or GridView; the comparator receives two `GridRow`s
  - `reverse` is now a built-in and accepts list, Grid, or GridView (the prior std lib `reverse` definition is removed; behavior on lists is unchanged)
- LSP completion at a literal outside of `[ ... ]` argv lists now offers in-file definitions, standard library definitions, typed builtins, and remaining `BuiltInList` names, with each item's signature (when known) shown as the completion detail. PATH-binary completion at the first position inside `[ ... ]` is unchanged.

### Changed

- **Breaking:** `keyValues` now returns a list of `{k, v}` dictionaries instead of a list of two-element lists.
  Each pair has a `k` field holding the key and a `v` field holding the value, so the key and value types stay distinct (previously they were collapsed into a single shared type, which forced overload-resolution ambiguity downstream).
  Update existing callers from `2unpack key!, value!` to `pair! @pair :k? key!, @pair :v? value!` (or use `:k?`/`:v?` directly).
- History, bin map, and interactive log storage now use the `$LOCALAPPDATA\msh` directory on Windows instead of `$LOCALAPPDATA\mshell`, and `XDG_DATA_HOME` history/bin map storage now uses the required `msh/` subdirectory on Linux/macOS.
  You should be able to simply move the previous files over with no problems.
- `msh bin edit` now falls back to the platform default opener when `$EDITOR` is unavailable
- File manager preview now short-circuits many more common binary extensions and shows detailed archive listings with human-readable sizes and `h:mm AM/PM` times for `.zip` and `.tar.gz` archives
- File manager now hides OneDrive's hidden `.849C9593-D756-4E56-8D6E-42412F2A707B` metadata file from listings and directory previews
- On Windows, pressing `h` at the root of a drive in the file manager now shows mounted drive letters so you can switch volumes.
- `match` arm separators now control subject consumption explicitly: `:` consumes the matched subject and `:>` preserves it, independent of pattern kind or bindings
- `updateCol` now accepts `GridView`, materializes a new `Grid` from the viewed rows, retypes the result columns, and leaves the backing `Grid` unchanged.
- `skip` and `take` now work on strings using the same indexing logic as string slicing.
- Completely removed the concept of `o`, `oc`, and `os`.
- `abs`, `max`, `min`, `max2`, `min2`, and `sum` are now runtime builtins with proper `(int -- int) | (float -- float)` overloads (and `([int] -- int) | ([float] -- float)` for the list-folding variants). They were previously stdlib defs whose float-only bodies crashed on int operands when called via the int overload. `sumInt` is kept as a stdlib alias for backwards compatibility. Mixed int+float overloads on `max2`/`min2` have been removed since the runtime `<`/`>` operators reject mixed numeric types.
- `max` and `min` now also work on a list of `DateTime`, returning the latest/earliest element (`([DateTime] -- DateTime)`).
- `len` runtime now accepts dictionaries (returns key count); the sig already permitted this.
- `md5` runtime now accepts `bytes` input directly, matching the listed overload.
- Dict type expressions now require an implicit (or `str`) key. `{V}` and `{str: V}` are accepted; anything else (`{int: V}`, `{path: V}`, etc.) is a parse error. Dict keys are always `str` at runtime, and the type system no longer pretends otherwise. Every dict-related builtin signature (`keys`, `values`, `get`, `set`, `setd`, `getDef`, `map`, `filter`, `in`, `len`, `keyValues`, `listToDict`) drops the `K` generic accordingly.
- `Error loading startup files:` now includes the script path, whether a version was pinned, the full MSHSTDLIB/MSHINIT and standard-location lookup order, and concrete resolution steps
- Tightened the grid form of `groupBy`: the aggregation-spec list is typed as `[{agg: (GridView -- V)}]` instead of `[{str: V}]`, so the required `agg` field and its quotation shape are now enforced statically. The agg quote's output type is generic per element, so a single list may mix specs whose quotations return different scalar types. Width subtyping still allows the optional `name` and `meta` fields.
- `[head ...rest]` (and any other spread in a `match` list pattern) now binds the rest as a zero-copy sub-slice of the source list. Appending to the rest still allocates a fresh backing array because cap equals len, so the source is never overwritten. The behavioral difference is that `setAt` on the rest list now mutates the shared backing — historically rest was an independent copy. This makes recursive list-walking idioms (e.g. `def f [head ...rest] : ... @rest f`) run in linear time instead of O(N²); the previous copy was the dominant cost on large lists.
- Extended the `:name` getter (and `get` built-in) to accept `Grid` and `GridView`. On a grid the getter returns the named column as `Maybe[[T]]` — the materialized column when present, `none` when the column is absent — making `g :n?` a shorthand for `g "n" gridCol`. On a `GridView` the values are projected through the view's row indices. The type checker now resolves the element type from the grid's schema when known. The runtime error message for `:` on an unsupported type now lists `Grid` and `GridView` alongside `dict` and `GridRow`.
- The type checker now rejects `pivot` aggregation quotations whose return type resolves to a container (`[T]`, `{V}`, shape, `Grid`, `GridView`, `GridRow`), mirroring the runtime constraint that pivoted cells must be scalars. The check fires only when the quote's output is concretely a container after substitution; if the output stays as an unconstrained type variable (e.g. `(:foo?)` quotes that infer through a synthesized fresh input), the call still type-checks and the runtime still catches it.
- Tightened the `w` / `wl` / `we` / `wle` write-builtin type signatures to match the runtime: `wl` / `wle` are now `(str -- ) | (int -- )`, and `w` / `we` are now `(str -- ) | (int -- ) | (bytes -- )`. Previously these were typed as `(T -- )` and silently accepted floats, bools, datetimes, lists, etc. — all of which crash at runtime. Convert with `str` first (`1.5 str wl`) for other types.


## v0.13.0 - 2026-04-07

### Added

- `match ... end` pattern matching syntax with value matching, type matching, `_` wildcard, maybe destructuring (`just v`/`none`), list destructuring (`[a b ...rest]`), and dict destructuring (`{ 'key': v }`)
- `map` on dictionaries (maps over values, preserving keys)
- Functions
  - `filter` builtin now supports dictionaries, filtering by value while preserving keys
  - `cdh`
  - `cdp`
  - `fromUnixTimeMicro`
  - `fromUnixTimeMilli`
  - `fromUnixTimeNano`
  - `prompt`
  - `toUnixTimeMicro`
  - `toUnixTimeMilli`
  - `toUnixTimeNano`
  - `toSvgPathStr`
  - `unlinesCrLf`
  - `scaleLinear`
- Explicit version syntax (example: `VER "v0.13.0"`)  and execution. You can now specify the exact version a script should run with and this will force the execution to use that interpreter and corresponding standard library.
- Multiple cut/copy selections in file manager.

### Fixed

- CLI interactive command execution now switches to a fresh output line before parsing/evaluation, so lexer/parser errors do not render on the prompt line.
- Lexer `ERROR` tokens now stop parsing immediately (including simple CLI parsing), preventing fall through to evaluation errors like unimplemented `ERROR` token handling.

### Changed

- Startup now loads both `std.msh` and `init.msh` from version directories (`msh/<version>/...`), keeps `init.msh` optional for implicit current-version startup unless `MSHINIT` is set, requires it for `VER` scripts, re-execs `VER` scripts with `msh-<version>` when needed, and ignores `MSHSTDLIB`/`MSHINIT` for `VER` scripts.
- `cartesian` type signature changed. Now is `[[a]] [a] -- [[a]]`. This make it easy to chain more than one Cartesian product. Usually start the chain off with empty `[[]]` as an identity element.

## v0.12.0 - 2026-02-19

### Changed

- File manager `l` on a file now opens it: text files open in `$EDITOR`, binary/unreadable files open with the platform default (`Start-Process` on Windows, `xdg-open` on Linux, `open` on macOS)

## v0.11.0 - 2026-02-18

### Added

- `completionDefs` builtin: pushes a dictionary of completion definitions, keyed by command name with quotation values
- `mshFileManager` builtin: pops a starting directory from the stack, opens the file manager, and cds to the final directory on exit
- `msh fm` now accepts an optional starting directory argument
- Built-in file manager via `msh fm` subcommand and Ctrl-O in interactive mode
  - Dual-pane layout with directory listing and file/directory preview
  - Vim-style navigation (`j`/`k`, `h`/`l`, `gg`/`G`, Ctrl-u/Ctrl-d)
  - Search with `/`, case-insensitive match highlighting, `n`/`N` to cycle matches
  - Rename with `r`, cursor positioned before extension, Ctrl-W word delete
  - Bookmarks with `m` + char to set, `;` + char to jump
  - Editor integration with `e` (uses `$EDITOR`)
  - Directory change on quit (Ctrl-O returns to shell in new directory)
  - Version-sorted entries, directories first and colored blue
  - Binary file detection for preview
  - Preview caching for fast scrolling
  - Cut/copy/paste buffer (`d` cut, `yy` copy, `p` paste, `c` clear) shared across instances
  - Delete to trash (`x`) with confirmation, using platform-native trash
  - `msh fm` prints final directory to stdout for `cd "$(msh fm)"` usage

## v0.10.0 - 2026-02-13

### Added

- Tail-call optimization (TCO) for recursive definitions in tail position.
- Functions
  - `sin`
  - `cos`
  - `tan`
  - `arctan`
  - `ln`
  - `ln2`
  - `ln10`
  - `pow`
  - `random`
  - `randomFixed`
  - `randomNorm`
  - `sqrt`
  - `randomTri`
  - `tempFileExt`
- CLI Alt-D inserts the current date as `YYYY-MM-DD`

### Fixed

- Windows CMD.EXE /C quoting now handles quoted commands with extra arguments (e.g., npm.cmd paths with spaces).

### Changed

- Builds/releases now are pure Go, built with `CGO_ENABLED=0`.

## v0.9.0 - 2026-01-27

### Added

- Prefix quote syntax (`functionName. ... end`) as an alternative to `(...) functionName`
- `<>` operator for in-place file modification. Reads file to stdin, writes stdout back on success.
  Example: `` [sort -u] `file.txt` <> ! ``
- Functions
  - `chomp`
  - `cstToUtc`
  - `fromOleDate`
  - `toOleDate`
  - `__gitCompletion`
  - `__sshCompletion`
  - `strCmp`
  - `strEscape`
  - `reSplit`
  - `linearSearch`
- Function definition metadata dictionaries in `def` signatures
- Definition-based CLI completions via the `complete` metadata key
- CLI completions for:
  - `msh`
  - `git`
  - `fd`
  - `rg`
  - `ssh`
- CLI history prefix search on Ctrl-N/Ctrl-P (case-insensitive)
- Alt-. to cycle last argument from history in the CLI
- Bin map file and `msh bin` CLI commands for binary overrides
- `msh completions` subcommand for bash, fish, nushell, and elvish
- CLI syntax highlighting for environment variables
- GitHub Action for installing mshell in CI workflows
- Append stderr redirection with `2>>`
- Combined stdout/stderr redirection with `&>` (truncate) and `&>>` (append)
- Same-path detection when using `>` and `2>` with identical paths (shares single file descriptor)
- Full stderr redirection support for quotations (`2>`, `2>>`, `&>`, `&>>`)
- Null byte validation for redirection file paths and `cp`/`mv` commands

### Fixed

- CLI binary mode now converts literal redirect targets (e.g., `cmd > file.txt` converts `file.txt` to a string for stdout, path for stdin)
- CTRL-C now only kills the running subprocess instead of both the subprocess and the shell

### Changed

- Breaking change: `@name` now only reads mshell variables and no longer falls back to environment variables; use `$NAME` for environment access.
- `w`/`we` now accept binary input and write raw bytes to stdout/stderr.
- Renamed `.s` to `stack`, `.def` to `defs`, `.env` to `env`
- Removed `.b` (use `binPaths` instead)

## v0.8.0 - 2025-12-29

### Added

- Functions
  - `2each`
  - `2tuple`
  - `floatCmp`
  - `ceil`
  - `floor`
  - `leftPad`
  - `lastIndexOf`
  - `numFmt`
  - `preserveInt` option for `numFmt`
  - `now`
  - `date`
  - `nullDevice`
  - `enumerate`
  - `enumerateN`
  - `takeWhile`
  - `dropWhile`
  - `2unpack`
  - `2apply`
  - `title`
  - `zipDirInc`
  - `zipDirExc`
  - `zipDir`
  - `zipPack`
  - `zipList`
  - `zipExtract`
  - `zipExtractEntry`
  - `zipRead`
  - `chunk`
  - `repeat`
  - `return`
  - `toJson`
  - `base64encode`
  - `base64decode`
  - `:` shorthand for `get`
- `timeout` option for `httpGet` and `httpPost`
- Support for comma-separated variable stores (e.g. `a!, b!, c!`)
- LSP completion suggestions for `@` variable references
- LSP rename support for variables scoped to definitions and globals
- Default <kbd>CTRL</kbd>-<kbd>F</kbd> binding matching fish shell behavior
- Execution operators for capturing stdout/stderr as strings or binary (`*b`, `^`, `^b`)

### Changed

- Breaking change: renamed the builtin that returns the current datetime from `date` to `now`; `date` now truncates a datetime to its date-only component.
- `parseJson` now accepts binary input and decodes it as UTF-8 before parsing.
- `fileExists` now uses golang [os.Lstat](https://pkg.go.dev/os#Lstat) instead of [os.Stat](https://pkg.go.dev/os#Stat), meaning if you have a broken symlink in Linux, `fileExists` will now return `true` instead of `false`.
- Slice semantics are slightly different. You now get a new backing array guaranteed for slice. This would come up if you did a partial slice (`0:n`), and then extended that in a loop or map. You could then be "extending" into the same backing array, causing previous items in the loop to be overwritten.
- `mv` will now allow moving a file path into a directory path. Previously had to be file to file.
- `skip` and `take` no longer throw exception when `n` is greater than the length of the list.
- Input redirection can now accept binary data directly and stream it to stdin without string conversion.

### Fixed

- Fixed infinite loop in `versionSortCmp` when non-digit after digit.

## v0.7.0 - 2025-10-03

### Added

- Basic tab completion for the CLI
- Basic HTML parsing
- Start of VS code extension
- `httpGet` and `httpPost` for making web requests.
- Functions
  - `isNone`
  - `parseHtml`
  - `absPath`
  - `bind`
  - `findByTag`
  - `concat`
  - `cartesian`
  - `groupBy`
  - `reFind`
  - `reFindAllIndex`
  - `md5`
  - `eachWhile`
  - `rmf`
  - `take`
  - `skip`


### Fixed

- Handling of `.cmd` and `.bat`
- Now immediately close file when appending
- `mv` made more robust
- Fixed broken line/columns in lexing
- Better handling of UTF-8 input

### Changed

- Now return Maybes for conversions
- Removed `canParseDt`
- `get` returns Maybe
- No escaping in path literals
- In JSON mappings, null now goes to `none`, not 0.
- `fileSize` now returns Maybe


## v0.6.0 - 2025-07-17

### Added

- Basic CLI history
- Command completion in CLI
- `countSubStr`, `uniq`, `canParseDt`, `fromUnixTime`, `toUnixTime`
- Built-in Maybe type, `?` operator for unwrapping

### Fixed

- Dict literal parsing

### Changed

- Sorted output for `keys` and `values`
- `map` now a built in function

## v0.5.0 - 2025-06-24

### Added

- `!` operator for executing external command, stopping on non-zero exit code
- `seq`, `toFixed`, `round``
- JSON handling

### Changed

- `filesIn` -> `lsDir`
- `tempFile` pushes a path, not a string

### Fixed

- Bug in `unlines`
- Bad printing in certain cases for `.s` and others.

## v0.4.0 - 2025-05-26

### Added

- `startsWith` and `endsWith`
- `tempDir`
- `hardLink`
- `isWeekend`, `isWeekday`, `dow`, `unixTime`
- `writeFile`, `appendFile`
- `rm`, `mv`, `cp`
- `skip`
- `e`, `ec`, `es`
- `filesIn`, `runtime`
- `sort`, `sortu`
- `sha256sum`
- `continue` keyword
- `zip`
- `reMatch`, `reReplace`
- dictionaries
- `PATH` searching


### Changed

- Allow standard output redirection to any string-like item.


## v0.1.0 through 0.3.0

- Initial releases of the project.
