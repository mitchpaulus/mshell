# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

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
  - `parseExcel`
  - `sortBy` - stable ascending sort of a Grid or GridView by one or more columns; bare-string and list-of-strings forms; `none` cells sort last; cross-type values in a generic column error
  - `sortByCmp` extended to accept Grid or GridView; the comparator receives two `GridRow`s
  - `reverse` is now a built-in and accepts list, Grid, or GridView (the prior std lib `reverse` definition is removed; behavior on lists is unchanged)

### Changed

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
- `2apply` standard library signature narrowed from `([a | b] (a|b a|b -- c) -- c)` to `([a] (a a -- c) -- c)`. Heterogeneous two-element lists are no longer accepted; cast the list to a homogeneous element type if you need the prior behavior.
- `abs`, `max`, `min`, `max2`, `min2`, and `sum` are now runtime builtins with proper `(int -- int) | (float -- float)` overloads (and `([int] -- int) | ([float] -- float)` for the list-folding variants). They were previously stdlib defs whose float-only bodies crashed on int operands when called via the int overload. `sumInt` is kept as a stdlib alias for backwards compatibility. Mixed int+float overloads on `max2`/`min2` have been removed since the runtime `<`/`>` operators reject mixed numeric types.
- `len` runtime now accepts dictionaries (returns key count); the sig already permitted this.
- `md5` runtime now accepts `bytes` input directly, matching the listed overload.
- `fileSize` static signature corrected from `(path|str -- int)` to `(path|str -- Maybe[int])` to match the runtime, which returns `Maybe[int]` (`None` on stat failure).
- `setAt` and `insert` static signatures no longer claim a `(str str int -- str)` overload that the runtime did not implement.
- `toFloat` / `toInt` static signatures no longer include a generic `(T -- Maybe[float|int])` fallback; only concrete `int`/`float`/`str` inputs are accepted, matching the runtime. The `str` overload is listed first so that under inferring overload resolution (e.g. inside `(toFloat?)`) it wins ties.
- Dict type expressions now require an implicit (or `str`) key. `{V}` and `{str: V}` are accepted; anything else (`{int: V}`, `{path: V}`, etc.) is a parse error. Dict keys are always `str` at runtime, and the type system no longer pretends otherwise. Every dict-related builtin signature (`keys`, `values`, `get`, `set`, `setd`, `getDef`, `map`, `filter`, `in`, `len`, `keyValues`, `listToDict`) drops the `K` generic accordingly.


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
