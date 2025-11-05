# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Functions
  - `2each`
  - `2tuple`
  - `floatCmp`
  - `leftPad`
  - `now`
  - `date`
  - `nullDevice`
  - `enumerate`
  - `enumerateN`
- LSP completion suggestions for `@` variable references
- LSP rename support for variables scoped to definitions and globals

### Changed

- Breaking change: renamed the builtin that returns the current datetime from `date` to `now`; `date` now truncates a datetime to its date-only component.
- `parseJson` now accepts binary input and decodes it as UTF-8 before parsing.
- `fileExists` now uses golang [os.Lstat](https://pkg.go.dev/os#Lstat) instead of [os.Stat](https://pkg.go.dev/os#Stat), meaning if you have a broken symlink in Linux, `fileExists` will now return `true` instead of `false`.

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
