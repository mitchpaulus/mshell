# Audit: paths, files, operating system, dates and times

Line numbers are `mshell/Evaluator.go` unless stated otherwise.
"CastString" means the runtime calls `obj.CastString()`. That accepts `str`, `path` and `int` (an int is formatted with `strconv.Itoa`) and rejects every other type (MShellObject.go:2115-2149).
It also accepts `MShellLiteral`, a bare word, which has no core type.
Under rule 1, every CastString input is written `str | path | int`.
That widens most old sigs by `int`; see "Unsure" item 1.

All outputs of `str`, `path`, `int`, `float`, `bool`, `datetime`, `bytes` and `Maybe` of those are immutable, so they need no mark.
None of these builtins runs a quote, so none takes `child` or `newList`.
None of them is SPECIAL.

The go.txt has no entries for `readFile` or `exit`, because they are already in the seed and registering them again would panic.
Seed changes:

- `readFile`: replace the seed line with `b.reg("readFile", "(str | path | int -- str)")`.
- `exit`: keep the seed line as it is.

| name | runtime accepts (Evaluator.go:line) | old sig | new sig + marks | notes |
|---|---|---|---|---|
| toPath | CastString; pushes path (7075) | `(str \| path -- path)` | `(str \| path \| int -- path)` | +int (rule 1) |
| absPath | CastString; `filepath.Abs`, path (10751) | `(str \| path -- path)` | `(str \| path \| int -- path)` | +int (rule 1) |
| basename | CastString; path (7052) | `(str \| path -- path)` | `(str \| path \| int -- path)` | +int (rule 1) |
| dirname | CastString; path (7052) | `(str \| path -- path)` | `(str \| path \| int -- path)` | +int (rule 1) |
| stem | CastString; path (7052) | `(str \| path -- path)` | `(str \| path \| int -- path)` | +int (rule 1) |
| ext | CastString; str (7052) | `(str \| path -- str)` | `(str \| path \| int -- str)` | +int (rule 1) |
| removeWindowsVolumePrefix | CastString; str, unchanged except on Windows (10955) | none | `(str \| path \| int -- str)` | new entry; the old table had none. It returns str even when given a path. |
| nullDevice | nothing; path (11474) | `( -- path)` | `( -- path)` | unchanged |
| tempDir | nothing; path (7390) | `( -- path)` | `( -- path)` | unchanged |
| tempFile | nothing; path (7356) | `( -- path)` | `( -- path)` | unchanged |
| tempFileExt | CastString; path (7366) | `(str -- path)` | `(str \| path \| int -- path)` | +path, +int (rule 1) |
| pwd | nothing; pushes **str** (6898) | `( -- path)` | `( -- str)` | out path→str (rule 1). The docs say str (functions.inc.html:149, mshell.md:1287) |
| psub | `MShellString` or literal only, not path; pushes **str** (6904) | `(str -- path)` | `(str -- str)` | out path→str (rule 1) |
| readFile (seed) | CastString; str (6517) | `(str \| path -- str)` | `(str \| path \| int -- str)` | the seed lacks int (rule 1). Replace the seed line. |
| readFileBytes | CastString; bytes (6548) | `(str \| path -- bytes)` | `(str \| path \| int -- bytes)` | +int (rule 1) |
| writeFile | Pop2: top = file path (CastString); below = content, `bytes` or CastString (7557) | `(str \| bytes str \| path -- )` | `(str \| path \| int \| bytes str \| path \| int -- )` | content +path +int, path +int (rule 1) |
| appendFile | same case as writeFile (7557) | same | same | same |
| cp | Pop2: top = destination, below = source, both CastString (7618) | `(str \| path str \| path -- )` | `(str \| path \| int str \| path \| int -- )` | +int (rule 1) |
| mv | same case as cp (7618) | same | same | same |
| hardLink | top = new target, below = existing source, both CastString (7330) | same | same | same |
| rm | CastString (7598) | `(str \| path -- )` | `(str \| path \| int -- )` | +int |
| rmf | same case (7598) | same | same | +int |
| mkdir | CastString (6876) | `(str \| path -- )` | `(str \| path \| int -- )` | +int |
| mkdirp | same case (6876) | same | same | +int |
| cd | CastString (6565) | `(str \| path -- )` | `(str \| path \| int -- )` | +int |
| mshFileManager | CastString (6590) | `(str \| path -- )` | `(str \| path \| int -- )` | +int |
| clip | CastString (6534) | `(str \| path -- )` | `(str \| path \| int -- )` | +int |
| cdh | nothing (6580) | `( -- )` | `( -- )` | unchanged; interactive |
| cdp | nothing (6585) | `( -- )` | `( -- )` | unchanged |
| isDir | CastString; bool (6848) | `(str \| path -- bool)` | `(str \| path \| int -- bool)` | +int |
| isFile | same case (6848) | same | same | +int |
| fileExists | CastString; bool (11378) | `(path \| str -- bool)` | `(str \| path \| int -- bool)` | +int |
| isCmd | CastString; bool (10601) | `(str \| path -- bool)` | `(str \| path \| int -- bool)` | +int |
| fileSize | CastString; `Maybe[int]` (8135) | `(path \| str -- Maybe[int])` | `(str \| path \| int -- Maybe[int])` | +int |
| modTime | CastString; `Maybe[datetime]` (8153) | `(path \| str -- Maybe[datetime])` | `(str \| path \| int -- Maybe[datetime])` | +int |
| glob | CastString; a new list of paths (6053) | `(str \| path -- [path])` | `(str \| path \| int -- new [path])` | +int; output `new` (a new list of immutable paths) |
| lsDir | CastString; a new list of full paths (8172) | `(str \| path -- [path])` | `(str \| path \| int -- new [path])` | +int; `new` |
| files | nothing; a new list of paths (6826) | `( -- [path])` | `( -- new [path])` | `new` |
| dirs | same case (6826) | `( -- [path])` | `( -- new [path])` | `new` |
| args | nothing; a new list of str (6148) | `( -- [str])` | `( -- new [str])` | `new`; each call builds a new list |
| stdin | nothing; str (6075) | `( -- str)` | `( -- str)` | unchanged |
| stdinIsTerminal | bool (6094) | `( -- bool)` | `( -- bool)` | unchanged |
| stdoutIsTerminal | bool (6096) | `( -- bool)` | `( -- bool)` | unchanged |
| stderrIsTerminal | bool (6098) | `( -- bool)` | `( -- bool)` | unchanged |
| prompt | CastString; str (6100) | `(str \| path -- str)` | `(str \| path \| int -- str)` | +int |
| runtime | nothing; str (8195) | `( -- str)` | `( -- str)` | unchanged |
| hostname | nothing; str ("unknown" on error) (10936) | `( -- str)` | `( -- str)` | unchanged |
| setenv | top = value (CastString), below = name (CastString) (7087) | `(str str -- )` | `(str \| path \| int str \| path \| int -- )` | both +path +int (rule 1) |
| unsetenv | CastString (7117) | `(str -- )` | `(str \| path \| int -- )` | +path +int |
| envInspect | CastString; a new list of new dicts with keys dt, kind, source, changed (5974) | `(str -- [{dt: datetime, kind: str, source: str, changed: bool}])` | `(str \| path \| int -- new [{dt: datetime, kind: str, source: str, changed: bool}])` | +path +int; `new` (all the dicts and the list are allocated by the call) |
| binPaths | nothing; a new list of new 2-element lists `[name fullPath]` (5947; Pathbin_linux.go:119) | none | `( -- new [[str]])` | new entry; the old table had none. See bug 1. |
| sleep | `IsNumeric` (int or float); a negative value is ignored (11326) | `(int \| float -- )` | `(int \| float -- )` | unchanged |
| exit (seed) | `MShellInt` only (6679) | `(int -- never)` | `(int -- never)` | unchanged. Values outside 0-255 and use inside a format-string interpolation are checked runtime errors (the parser also rejects a literal `exit` there, Parser.go:673). |
| now | datetime (6937) | `( -- datetime)` | `( -- datetime)` | unchanged |
| toDt | str or literal: `Maybe[datetime]`; datetime: returns the same object (6798) | `(str -- Maybe[datetime])`, `(datetime -- datetime)` | same | unchanged. The overloads have distinct kinds (rule 6). Path and int are rejected, because there is no CastString. |
| date | datetime only; midnight of the same day (6941) | `(datetime -- datetime)` | same | unchanged |
| year | datetime; int (6980) | `(datetime -- int)` | same | unchanged |
| month | (6967) | same | same | unchanged |
| day | (6955) | same | same | unchanged |
| hour | (6992) | same | same | unchanged |
| minute | (7004) | same | same | unchanged |
| dow | datetime; int 0-6, Sunday = 0 (7467) | `(datetime -- int)` | same | unchanged |
| isWeekend | datetime; bool (7467) | `(datetime -- bool)` | same | unchanged |
| isWeekday | (7467) | same | same | unchanged |
| toUnixTime | datetime; int (7487) | `(datetime -- int)` | same | unchanged |
| toUnixTimeMilli / Micro / Nano | same case (7487) | same | same | unchanged |
| fromUnixTime | `MShellInt` only; a UTC datetime (7520) | `(int -- datetime)` | same | unchanged |
| fromUnixTimeMilli / Micro / Nano | same case (7520) | same | same | unchanged |
| toOleDate | datetime; float (7507) | `(datetime -- float)` | same | unchanged |
| fromOleDate | `IsNumeric` (int or float) (7544) | `(int \| float -- datetime)` | same | unchanged |
| addDays | Pop2: top = days (`IsNumeric`), below = datetime (10581) | `(datetime int \| float -- datetime)` | same | unchanged |
| utcToCst | datetime; a new datetime (9710) | `(datetime -- datetime)` | same | unchanged |
| cstToUtc | datetime; a new datetime (9730) | `(datetime -- datetime)` | same | unchanged |
| dateFmt | Pop: top = layout, `MShellString` only; below = datetime (7137) | `(datetime str -- str)` | same | unchanged. The layout is a Go time layout, not strftime. |

Old-table entries the runtime does not have: `second` and `weekday` (TypeBuiltins.go:271).
There is no `case "second"` or `case "weekday"` in Evaluator.go, and neither name is in BuiltInList.go or lib/std.msh.
Do not port them.

I checked the go.txt entries in a scratch copy of the package: `buildCoreTable` built the table without panics, and small programs using these words passed or failed as expected.

## Things in the runtime that look like bugs

1. **binPaths pushes pointer strings.** `DebugList` builds its elements as `&MShellString{...}` (Pathbin_linux.go:131-132, Pathbin_darwin.go:81-82, Pathbin_windows.go:229-230).
   Everywhere else, strings are the value type `MShellString`.
   A `*MShellString` gets CastString through the method set, but it fails every `case MShellString:` type switch and `.(MShellString)` assertion, for example `dateFmt`'s layout, `psub` and `toDt`.
   It probably also fails `str` checks in `is`/`tryAs`.
   So `binPaths` does not really return `[[str]]` at runtime.
   The sig `new [[str]]` is only right once those pointers are values.
2. **psub leaks an open file.** It creates and registers the temp file, then on a wrong-typed input returns at 6904ff. without `tmpfile.Close()`.
3. **psub and pwd return str where the path words return path.**
   `tempFile`, `absPath` and `nullDevice` return path, while `pwd` and `psub` return str.
   This matches the docs for `pwd`, so it may be intended.
   `psub` also rejects a path input.
4. **Wrong operation names in error messages** (cosmetic):
   - `date` on an empty stack says 'day' (6941).
   - `isFile` says 'isDir'.
   - `dow`/`isWeekday` on a non-datetime say "is a weekend".
   - `utcToCst`/`cstToUtc` say 'toCst'/'toUtc'.
   - `toUnixTime*` says 'unixTime'.
5. **Overflow in `addDays` and `fromOleDate`.** `time.Duration(days * 24h)` overflows silently for spans over about 292 years.

## Unsure

1. **int on every path or string input.** CastString accepts int, so under rule 1 every input above is `str | path | int`.
   This affects the seed's `readFile`, and probably other categories (`wl` in the seed is `str | int`, without path).
   If the table should not accept `5 readFile`, it needs one table-wide decision.
   The narrower spelling is `str | path`.
2. **Bare-word literals.** `toDt`, `psub` and every CastString word also accept an `MShellLiteral` (a bare word) on the stack.
   That has no core type, so I left it out.
3. **writeFile/appendFile content.** It accepts `path` and `int` as content (written as their text), because the non-bytes branch uses CastString.
   I followed rule 1, but this may be unintended.
4. **envInspect's shape.** The output dicts have exactly the four keys, but the core syntax only writes open shapes.
   The open shape is safe.
