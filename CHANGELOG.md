# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Functions
  - `sin`
  - `cos`
  - `tan`
  - `ln`
  - `ln2`
  - `ln10`
  - `random`
  - `randomFixed`

### Fixed

### Changed

## 0.9.0 - 2026-01-27

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

## 0.8.0 - 2025-12-29

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

## 0.7.0 - 2025-10-03

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


## 0.6.0 - 2025-07-17

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

## 0.5.0 - 2025-06-24

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

## 0.4.0 - 2025-05-26

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


## 0.1.0 through 0.3.0

- Initial releases of the project.
