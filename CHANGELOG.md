# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- The interactive prompt is set in `init.msh` with `setPrompt`, from a quotation that returns a list of `PromptItem` values:
  text, line breaks, colors (16, 256 or RGB), text attributes, the window title and hyperlinks.
  It is given a `PromptInfo` with the stack's depth and types, and whether the last line succeeded, its exit code and how long it took.
  A prompt that fails, takes longer than 3 seconds, or is interrupted with Ctrl-C is stopped, and the built-in prompt is shown.
- `setCursorShape` sets the cursor's shape while the interactive shell reads a command.
- Enums: `enum Shape = circle float | rect float float | dot end` declares a type whose values are one of its members.
  A member's name makes a value from its payload (`2.0 circle`), and a `match` arm takes it apart (`circle r : ...`).
  The enum's name is a match pattern for any of its members (`Shape s : ...`).
  Enums may have parameters (`enum Box[a] = box [a] | empty end`) and may refer to themselves.
  `str` gives `circle(2)`, `toJson` gives `{"circle": 2}`, and `=` compares the member and its payloads.
- `tryAs T` checks at run time that a value conforms to the type `T`, and gives `just` the same value or `none`:
  `"people.json" parseJson tryAs [Person] ?`.
  Every element, dictionary value and enum payload is checked, in place, with no copy.
  The match pattern `is T name` does the same check in a match arm, and binds the value.
- `del` removes a key from a dictionary: `{a: 1, b: 2} "a" del`. Nothing happens when the key is absent.
- `getEnv` gets an environment variable by name, giving `none` when it is not set: `"EDITOR" getEnv "vim" maybe`.
- `deepCopy` copies a value, giving every list, dict and grid inside it a new object, so changing the copy never changes the original.
- The interactive shell type checks each line before it runs it, keeping types across lines.
  A line that does not check is not run and changes nothing.
  After a line that stops with an error, the stack goes back to what it was before the line, less the new lists, dictionaries and grids the line took.
- The file manager previews PNG, JPEG, and GIF images in terminals that support sixel graphics, such as Windows Terminal, WezTerm, foot, and xterm.
  Other terminals show the image format and size in pixels.
  If images look stretched, set `MSH_CELL_PIXELS` to the real size of a text cell in pixels, such as `9x20`.
  Windows Terminal always reports 10x20 cells whatever the font, so images there can be slightly squeezed without it.
- The file manager preview shows PDF details: version, page count, page size, encryption, and document info such as title and author.
  It reads only the parts of the file that hold this, so it stays fast on large PDFs.
- On Windows, the file manager shows OneDrive status in cloud sync folders:
  `☁` cloud only, `✓` on this device, `●` always keep on this device, `↻` syncing, `✗` sync error.
  Folders show the same status as in Explorer.
  Cloud only files are not previewed, so scrolling past them no longer downloads them.
- The file manager shows file sizes in a column next to the file names, in `ls -h` style (`556`, `5.4K`, `41K`, `1.2M`).
  Each unit has its own color: bytes dim, kilobytes plain, megabytes cyan, gigabytes yellow, and larger magenta.
  Directories have no size.
- Completion definitions can return a dictionary instead of a list, to choose what Tab offers:
  `values`, files matching glob patterns (`files`), preferred files with a fallback to the rest (`preferredFiles`), directories only (`dirs`), or executables on the path (`binaries`).
- `\{` and `\}` escapes in double quoted strings and format strings, for literal braces.

### Changed

- Every script is type-checked before it runs, and runs only if it checks: a file, `-c` code, or code read from standard input.
  There is no way to run a script unchecked; `--check-types` is still accepted and changes nothing.
  `--type-check-only` checks and exits, as before.
  A script that the checker refuses must be fixed before it runs again; `msh --type-check-only` on your scripts shows what needs changing.
- The type checker checks the standard library's and the startup file's definitions as well as the script's.
  Every definition in them is checked on every run, whether or not the script calls it.
  A script runs even when one of them has a type error, with a warning listing the errors; a call to such a definition is refused.
- The startup files' top-level code is type-checked before it runs, and runs only if it checks.
  A script, and the interactive shell, start from the stack and variables it leaves, with their types: a script can use a variable the startup file sets.
  When it does not check, its errors are printed and it is not run; a script then does not run either, and the interactive shell starts without it.
  `--type-check-only` checks it without running it.
  A definition there with a type error stays defined, but code that calls it is refused, with the error; code that does not call it is checked as before.
- The match pattern for a date/time is `datetime`, the same as the type name: `datetime d : ...`. Previously it was `date`.
- `return` at the top level of an interactive line exits the shell, as it ends a script. Previously it ended only the line.
- A bare word in a list literal is a string, exactly as if it were quoted: `[ls -l]` is `["ls" "-l"]`.
  `typeof` gives `String`, a `str` match arm matches it, and every word that takes a string takes it.
  `<`, `parseCsv`, `parseHtml` and `parseJson` read a string as the text itself, so a bare word given to them is text, not a file name: write a path (`` `data.csv` ``) to name a file.
  A printed list shows the word quoted.
- A number too large to read, in a literal, an index such as `:99999999999999999999:`, or a positional argument, is an error when the script is read, not when that code runs.
  A slice like `1:99999999999999999999` used to crash.
- The type checker (run on every script, by `--type-check-only`, and by the language server) is new.
  It is built so that a script it accepts never stops with a type mismatch at run time,
  and it rejects code that the old checker accepted and that then failed. In particular:
  - A stored value keeps the type it was made with: a list, dictionary or grid passed on, stored or duplicated is never seen at a second, wider type.
    A new value, such as a literal written where it is used, may be given any type it fits: `[1 2] as [int | str]`.
    `deepCopy` makes a new value from a stored one.
  - A variable has one type in each definition and in the script.
    Store values of a different type under a new name, or widen the first store with `as`.
  - `parseJson` gives `Json`, which must be checked before use, with `match` or `tryAs`.
  - `as` needs evidence: it widens a type, or retypes a new value. Use `tryAs` to check data from outside.
  - A redirect or a type-changing grid update needs a new list or grid: `[cmd] *`, not `@cmd *`.
  - A definition that returns a new list, dictionary or grid says so: `def load ( -- new Json)`.
  - A definition that never returns says so: `def die (str -- never)`.
  - `break` and `continue` work in a quotation written at the `loop`, `each`, `map`, or other word that runs it, not in a stored one.
  - A `?` that can only fail, such as on a key a dictionary's type says is absent, is pointed out by the language server.
- A name can be defined only once. A second definition of a name, in the script, the init file or the standard library,
  is an error, as is a definition with the name of a builtin or an enum member.
  Previously the first definition silently won, so a later one never ran.
- `enum` and `tryAs` are keywords.
- A `type` name can be declared only once, and not with the name of an enum, an enum member, a builtin or a built-in type.
  A `type` or `enum` declaration that names an unknown type, refers to itself with nothing in between (`type A = A`),
  or has a union of two lists or two dictionaries is an error before the script runs, with or without the type checker.
- `parseJson` gives an `int` for a JSON number with no fraction or exponent, such as `30`, and a `float` for any other, such as `30.0` or `3e1`.
  Previously every JSON number was a `float`.
- Startup is faster on Linux and macOS: `msh` no longer reads every directory on `PATH` when it starts.
  A command's path is looked up when it first runs, and the full list is read only for completion and `binPaths`.
  A one-line script went from about 25 ms to 7 ms on a machine with 6,000 files on `PATH`.
- Format string interpolations can hold any code: string literals, dictionaries, and nested format strings all work inside `{...}`.
  Interpolations are parsed once when the script is read, not each time the string is built,
  and errors inside them point at the right line and column.
  The type checker now also checks that each interpolation produces a `str`, `path`, or `int`.
- Tab completion of file names is faster, especially in large directories, and symbolic links to directories complete with a trailing separator.
- `sudo` completion offers executables from the path much faster.
- Grid `groupBy` and `pivot` are faster: `groupBy` is 2-6x faster on a 5.4 million row table.
  Grid string columns with many repeated values use about a third of the memory.

- `seq` is now built in and much faster: about 25x faster and a third of the memory for 10 million items.
- The file manager preview no longer times out after 3 seconds.
  The timeout existed for OneDrive files, which are now detected and skipped instead.
- `stdin` fails if the input exceeds `MSH_READ_LIMIT` bytes (default 104857600, `0` for no limit).
- Scripts run faster, especially calls to definitions and quotations run by functions like `each`, `map`, and `filter`.
  Calling a definition allocates less than half as much, and looking up a built-in function no longer slows down the later it appears in the list.
  String, number, and date/time literals are decoded once when the script is read, not each time they run.
- `join` is faster, and `unlines` and `unlinesCrLf` are now built in and much faster: about 80x faster on a list of a million strings.
- `return` is only allowed directly in a definition, or in the body of an `if` or `match` there.
  Anywhere else is now an error: inside a quotation (including ones run by `x`, `iff`, `loop`, or `each`),
  a list or dict literal, an else-if condition, or a grid cell.
  These are the places where the type checker can check what `return` leaves against the definition's outputs.
  Write `cond (value return) iff` as `cond if value return end`, and leave a loop early with `break`.
- The type checker rejects a `return` that leaves more values than the definition declares,
  such as `def name (-- str) 5 "a" return end`.
- `map` on a grid with no rows gives a grid with no columns. Previously it kept the input grid's columns.
- In a signature, a generic is a single letter, optionally followed by digits (`a`, `T`, `T1`).
  Any other name that is not a type is an error, with a hint for names such as `string` (`str`) and `numeric` (`int | float`).
  Previously a misspelled type silently became a generic.
- The type checker is about twice as fast, and checking a file again, as the language server does on every edit, allocates almost nothing.
  Deeply nested quotations no longer take quadratic time.
- Source code must be UTF-8: a byte that is not part of a valid UTF-8 character is an error, reported at its line and column.

### Security

- Terminal replies that are strings (OSC, DCS, APC, PM, SOS), such as colour or version reports,
  are consumed and dropped by the interactive editor instead of being typed into the command,
  where their text could trigger key chords.
- The prompt no longer writes the working directory to the terminal raw.
  Control bytes in a directory name display as caret notation, and the OSC 7 directory report
  percent-encodes the path, so a directory name can no longer inject terminal queries.
- Error messages escape control bytes from quoted input and file names.

### Fixed

- Pressing TAB to complete a command's arguments no longer changes the interactive shell's variables: completion definitions run in a scope of their own.
- Running or type checking a startup file itself (the init file, or the standard library through `MSHSTDLIB`) no longer also loads it as a startup file first, which defined everything in it twice.

- `map` on a grid, when a row the quotation gives lacks a column the first row has, is an error.
  Previously the cell was left empty, which crashed later or read as zero.
- `updateCol` whose quotation adds rows to the grid it is updating is an error. Previously it crashed.
- The language server:
  - publishes diagnostics only for a document's newest text, and none for a closed document.
    Previously an older check could finish last and leave errors for text that no longer existed,
    and fast typing in a large document could use gigabytes of memory.
  - puts diagnostics, hover, completion and rename at the right column on lines with characters such as emoji.
  - renames a variable everywhere in its scope: inside `if` and `match` arms, prefix quotations, and match and `=>` bindings.
  - fixes every `new` mark of a large file at once in a fraction of a second. Previously it took seconds and blocked other requests.
  - shows errors in the startup files, as the command line does.
- `chunk` with a size of 0 or less exits with an error. Previously it never finished.
- `ssh` completion after an option it has no list for offers the options and hosts. Previously it gave back a quotation instead of a list.
- `loop` on a stored quotation runs it in the variables it captured, as `x` and `each` do.
  Previously it read and wrote the variables of the code around the loop.
- `=` and `!=` on two `Maybe`s that hold values of different kinds, such as `null` and an int from a `Maybe[int | null]`, give false.
  Previously one order was an error.
- A slice of a pipe (`@p 1:`) is a new list. Previously it shared storage with the pipe, so `setAt` or `append` on the slice could change the pipe.
- `=` and `!=` on two `Maybe` values compare their contents. Previously every comparison of two `Maybe`s was false, including `none none =`.
- `str` and `toJson` on a list or dict that contains itself are an error. Previously they crashed with a Go stack overflow or never finished.
  Printing, `toJson` and `=` also work on values nested any number of levels deep,
  and `=` finishes on dicts that contain themselves.
- The type checker now checks list literals, dict values, and grid cells on their own empty stack, as they run.
  Code like `1 [drop]` or `1 {a: drop}` is a type error instead of a type checker crash or a pass that fails at runtime,
  and a dict value must produce exactly one value (#341).
- `utcToCst` and `cstToUtc` no longer change their input. Previously, a variable passed to them was also changed to the converted time.
- A terminal resize no longer prints another prompt. The prompt and command are redrawn
  in place at the new width.
  In particular, a new Windows Terminal pane reports its parent's size until the first key
  arrives, which used to print a duplicate prompt on that first keystroke.
  Set `MSHREFLOW=0` for terminals such as xterm that do not rejoin wrapped rows on resize.
- The OSC 7 working-directory report is now actually emitted; a reversed check meant it was
  only sent when the hostname lookup failed.
- `toDt` no longer guesses the order of ambiguous numeric dates.
  Every date is tried as year-month-day, month-day-year, and day-month-year.
  A single valid reading is accepted, so `12/16/25` and `16/06/2025` now parse
  (`12/16/25` previously became `2013-04-25`). When several readings are valid, like
  `01/02/2026`, the order learned from the most recent unambiguous non-ISO date in the same
  evaluation is used. With no such evidence the result is `none`, where it previously
  assumed US month/day/year. Dates with a leading four digit year are unaffected.
- `toDt` now returns `none` for out of range components such as month 13, Feb 30, or hour 25,
  instead of silently rolling them over into the next month or day.
- `return` inside an `if` or `match` now leaves the whole definition when the definition was called from a quotation run by a function like `map` or `each`.
  It used to leave only the `if` or `match`, and the rest of the definition kept running.
- A definition called as the last item of a redirected quotation run by `iff`, such as ``true (myDef) `out.txt` > iff``, now writes to the file.
  The file used to be closed before the definition ran.
- `and` and `or` with a quote give an error, not a crash, when the left value is not a `bool`.
  A `break` in the quote no longer drops a value from the stack.
- `mod` with a top value that is not a number gives an error. It used to drop both values silently.
- `parseCsv` and `parseHtml` give an error, not a crash, on input that is not a string or path.
- `=` and `!=` with `null` on one side work in both orders. `null 1 =` is `false`; it used to be an error.
- `toFixed` with a negative number of places gives an error instead of printing `%!(BADPREC)`.
- `toJson` writes `null` for a NaN or infinite float, as JavaScript does. It used to write nothing, giving invalid JSON.
- `leftPad` counts code points, not bytes, so it no longer cuts a multi-byte pad character.
- The strings from `binPaths` compare equal to other strings.
- `psub` with input that is not a string no longer leaves a temporary file behind.
- Some error messages named the wrong function, such as `date` saying `day`. They now name the function that failed.
- `gridSetCell` writes a value of a different kind than the rest of its column, such as a string into a column of integers. It used to drop the value silently.
- The quotes `completionDefs` gives run their definition as a call: its variables no longer overwrite the caller's, and a `return` in it no longer returns from the caller.
- A pipe (`|`) has its own copy of its list of commands. Previously changing the list afterwards also changed the pipe.
- `seq` and `leftPad` with an impossibly large count give an error instead of crashing.
- `numFmt` with `sigFigs` above 17 gives an error. A float holds about 17 significant digits, so more gave wrong digits.
- `else*` is recognized only as itself: any four-letter word starting with `el` and followed by `*`, such as `elxx*`, lexed as `else*`.

### Added

- Bracketed paste in the interactive editor: pasted multiline text, tabs, and key chords are inserted literally without executing commands.
- Alt-Shift-R in the interactive editor forgets measured text widths and measures them again,
  for use after reattaching from a different terminal or changing fonts.
- HTTP cookie jars: pass a shared list as `cookieJar` to `httpGet` / `httpPost`.
  Requests and responses reuse and update the same list, including across redirects,
  with domain/path scoping, expiration, deletion, creation ordering, and JSON persistence.
- Assertive destructuring with the `=>` operator.
  It consumes a list, dictionary, or Just value, binds its structural pattern names,
  and fails at runtime when the pattern does not match.
  Structural patterns reject duplicate bindings and are limited to 256 positions or entries.
- CLI: an explicit `-` argument reads the program from standard input,
  with arguments after the `-` passed as positional arguments (`some-command | msh - arg1`).
- The language server now offers a `Quote all literals in list` code action that
  single-quotes every bare literal in the innermost list containing the cursor.

- Functions
  - `envInspect`: Get the session-local change history for an environment variable, including when and where it was inherited, set, or unset. The latest 256 events per variable are retained. `(str -- [{dt: datetime, kind: str, source: str, changed: bool}])`
  - `longestCommonPrefix`: Longest leading substring shared by every string in a list. `([str] -- str)`
  - `whenJust`: Run a quotation on the inner value for its side effects when the Maybe is Just;
    does nothing on None. `(Maybe[a] (a -- ) -- )`
  - `setenv`: Set an environment variable by name, taking the value then the name.
    Use when the name is not known statically; otherwise prefer `$NAME!`. `(str str -- )`
  - `stdinIsTerminal`, `stdoutIsTerminal`, and `stderrIsTerminal`: Report whether
    the current effective standard stream is connected to a terminal or Windows
    console. Regular files, pipes, captures, and non-file streams return false.
    Redirections and symlinks are classified by their opened target.

- The GitHub action can now install unreleased builds:
  passing a commit SHA or branch name as `version` clones the repository at that ref and builds from source with Go.
  Release tags (`vX.Y.Z`) and `latest` still download pre-built binaries.

- The tar write functions (`tarDirInc`, `tarDirExc`, `tarPack`) now accept a
  dictionary destination `{path: str|path, compress?: bool}`, where `compress`
  overrides the extension-based gzip inference in either direction — useful
  for destinations without a meaningful extension (e.g. `redo`'s `$3` temp files).

- Stream merge redirects `2>&1` (stderr to stdout's destination) and `1>&2`
  (stdout to stderr's destination). Each is a single token with no internal
  spaces, and works on command lists, pipeline stages, and quotations.
  Unlike POSIX, they are not order-sensitive fd duplication: the merged
  stream follows the other stream's *final* destination, so `2>&1 *` captures
  both streams interleaved and `[[make] 2>&1 [grep err]] |;` sends stderr
  through the pipe, cross-platform.

- CLI completions for `cargo`: subcommands (including installed third-party
  ones via `cargo --list`), per-subcommand options, and dynamic values for
  `--target`, `--features`, `-p`/`--package`, `--bin`/`--example`/`--test`/`--bench`
  target names, and installed crates for `cargo uninstall`.
- Match arms may list several literals in a row, matching if the subject equals
  any of them (OR), e.g. `'-h' '--help' : ...`. All alternatives in one arm
  must be the same literal kind: all strings, all integers, or all paths.
- Optional `followRedirects` key (bool, default `true`) on the `httpGet` /
  `httpPost` request dictionary. Set it to `false` to get the first response
  back as-is instead of following redirects, e.g. to inspect the `Location` or
  `Set-Cookie` headers of a `3xx` response after a login POST.
- Octal, hexadecimal, and binary integer literals via `0o`, `0x`, and `0b`
  prefixes (case-insensitive), e.g. `0o644`, `0xFF`, `0b101`. The base is purely
  a way of writing the literal; the value is an ordinary integer and prints in
  decimal. There are no digit separators.
- Functions
  - `toBase` / `fromBase`: format an integer in / parse a string from an
    arbitrary base (2–36). `fromBase` returns `Maybe[int]`.
  - `toHex` / `toOctal` / `toBin` and `parseHex` / `parseOctal` / `parseBin`:
    convenience wrappers over `toBase` / `fromBase` for the common bases.
  - `tarDirInc` / `tarDirExc` / `tarPack` / `tarList` / `tarExtract` /
    `tarExtractEntry` / `tarRead`: create, list, extract, and read `.tar`
    archives, mirroring the existing `zip*` functions (same argument order and
    option dicts). Compression is chosen from the destination extension when
    writing (`.tar.gz` / `.tgz` → gzip, `.tar` → uncompressed) and auto-detected
    from the gzip magic bytes when reading, so `.tar.gz` is handled
    transparently. Symlinks are preserved on pack and recreated on extract
    (with a guard against targets escaping the destination); hard links and
    device nodes are rejected.
- Optional `maxBytes` key (int, default `0` = unlimited) on the `zipExtract` /
  `zipExtractEntry` / `tarExtract` / `tarExtractEntry` options dict: caps the
  total uncompressed bytes written during an extraction to guard against
  decompression bombs.
- Archive extraction (both `zip*` and `tar*`) now refuses to write through a
  symlink that already exists in the destination directory and points outside
  it, closing a path-traversal vector when extracting into a directory that
  contains symlinks.
- Archive extraction (both `zip*` and `tar*`) no longer follows a symlink at
  the final path component: regular files are created with `O_EXCL`, and in
  `overwrite` mode an existing name is unlinked (never dereferenced) before a
  fresh file is created. This mirrors GNU tar's behavior and prevents an
  `overwrite` extraction from writing through a pre-existing symlink at the
  destination name (e.g. `dest/report` -> `/etc/passwd`).
- Archive extraction (both `zip*` and `tar*`) now performs every write through
  an `os.Root` anchored at the destination directory. The kernel enforces that
  no path can escape the destination via `..` or a symlink component (using
  `openat2`/`RESOLVE_BENEATH` on Linux), closing the time-of-check/time-of-use
  race that a purely lexical containment check leaves open. Legitimate symlinks
  that stay within the destination continue to work.
- Optional fields in dictionary shape types, written `name?: T` (and
  `"name"?: T` in `def` signatures). An optional field may be absent from a
  value; when present, its value is still type-checked. This lets option-style
  APIs be typed precisely instead of as a loose `{v}` dict — e.g. `numFmt`,
  `httpGet`/`httpPost`, grid `groupBy` aggregation specs, and the `zip*` option
  dicts now declare their required and optional keys. A required value satisfies
  an optional parameter, but an optional value does not satisfy a required one.
- The type checker now tracks the value of a string literal (as a `str`
  refinement) so a `get` with a known key resolves a shape field the same way
  the `:name` getter does: `resp "body" get` yields the declared `body` field's
  type instead of the union of every field type, so `httpGet? "body" get?`
  type-checks as `bytes`. Because the key rides the stack as a type, it resolves
  even when the literal reaches `get` through a variable; a key computed at
  runtime still returns the generic `Maybe[value]`.
- The language server now reports an informational diagnostic when a `?` unwrap
  is statically guaranteed to fail — unwrapping a getter (`:k?`) for a field a
  concrete shape does not declare, or unwrapping a bare `none`. The hint is
  placed on the `?` and fires even when the value flows through a variable first
  (e.g. `:b val! @val ?`). Homogeneous dictionaries (`{str: T}`) return a genuine
  `Maybe[T]` for any key and are never flagged, and a value with a declared
  `Maybe[T]` type is never flagged.
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
- A new `null` type representing the JSON null value, distinct from `none` (the
  empty case of `Maybe`). `parseJson` now produces `null` for JSON `null`, the
  `null` literal pushes one, and `null` can be used in union types (e.g.
  `int | null`) and matched with a `null` arm. `( -- null)`

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

- The interactive editor lays out and repaints commands from widths measured on the current terminal.
  Long commands wrap across rows, wide and multi-codepoint clusters such as emoji stay whole,
  tab and control bytes display safely, and completion rows repaint with the command.

- A trailing comma after a comma-separated variable store list (`a!, b!,`) is now a parse error.
  Commas also separate `match` arms, so the trailing comma was ambiguous.

- A trailing comma after a comma-separated indexer list (`:0:, 2:,`) is now a parse error,
  for the same reason as variable stores.

- A number immediately followed by a literal character now lexes as a single
  literal token instead of a float/int plus a separate literal, so bare file
  arguments like `redo 1.pdf` work. Floats still end at token-ending
  characters, e.g. `(1.5)`, `[2.5]`, `3.5;`, and `4.5,` lex as floats.

- Each of stdout/stderr now has exactly one destination: combining two
  destinations on the same stream (e.g. capture `*` plus file redirect `>`,
  `&>` plus `2>`, a second `>`, or a merge plus anything else on that stream)
  is now an error, caught both by the static type checker and at runtime.
  Previously the extra redirect was silently ignored (capture won over file
  redirects) or last-wins. `2>&1` combined with `<>` is also rejected.
- `loop` quotations now honor stderr redirects, append mode, and merges, and
  inherit the enclosing quotation's redirected streams (previously a loop
  body wrote straight to the terminal even inside a redirected quotation).
- Quotations accept only the redirects that don't change the stack: file
  redirects, stdin, and the merges. Captures (`*`, `*b`, `^`, `^b`) on a
  quotation now give a clear error at both layers instead of falling into
  the multiplication error, and the type checker now rejects `<>` on a
  quotation (the runtime always did). Capture individual command lists
  instead, e.g. `[[cmd1] [cmd2]] (* !) map`.

- `zipPack` entries can now be a bare string or path in addition to the
  dictionary form. A bare entry adds the file or directory under its base name,
  keeping its own mode:
  `([str | path | {path: str | path, archivePath?: str | path, mode?: int}] str | path -- )`
- Breaking: the type checker no longer accepts an empty quote `()` as the
  predicate to `any` / `all`.
  Pass `(id)` instead, e.g. `[true false] (id) any`.
  Both now carry the single signature `([T] (T -- bool) -- bool)`, matching their
  `std.msh` definitions.
- A command that cannot start (not found, permission denied, bad format, ...) run
  with `?` or `;` no longer aborts the script.
  Instead, `?` leaves a negative exit code carrying the exact reason: `-(256+errno)`
  for POSIX start failures, `-(1024+winerror)` on Windows, `-(128+signal)` for a
  process killed by a signal (replacing the old flat `-1`), `-255` for a command
  not found on `PATH`, and `-256` when the OS error cannot be read.
  Negative codes never collide with a real exit status (`0`-`255`).
  `!` still stops on these, exiting `msh` itself with the conventional 127/126/128+N.

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
- Extended the `:name` getter (and `get` built-in) to accept `Grid` and `GridView`. On a grid the getter returns the named column as `Maybe[[T]]` — the materialized column when present, `none` when the column is absent — making `g :n?` a shorthand for `g "n" gridCol`. On a `GridView` the values are projected through the view's row indices. The type checker now resolves the element type from the grid's schema when known. The runtime error message for `:` on an unsupported type now lists `Grid` and `GridView` alongside `dict` and `GridRow`.
- The type checker now rejects `pivot` aggregation quotations whose return type resolves to a container (`[T]`, `{V}`, shape, `Grid`, `GridView`, `GridRow`), mirroring the runtime constraint that pivoted cells must be scalars. The check fires only when the quote's output is concretely a container after substitution; if the output stays as an unconstrained type variable (e.g. `(:foo?)` quotes that infer through a synthesized fresh input), the call still type-checks and the runtime still catches it.
- Tightened the `w` / `wl` / `we` / `wle` write-builtin type signatures to match the runtime: `wl` / `wle` are now `(str -- ) | (int -- )`, and `w` / `we` are now `(str -- ) | (int -- ) | (bytes -- )`. Previously these were typed as `(T -- )` and silently accepted floats, bools, datetimes, lists, etc. — all of which crash at runtime. Convert with `str` first (`1.5 str wl`) for other types.

### Removed

- The `pick` stack operator was removed.
  Its stack effect depends on a runtime integer, so it could not be expressed in the static type checker, and it saw no real use.

### Fixed

<<<<<<< HEAD
- A command whose first token is a lexer error, such as a lone quote, no longer crashes the interactive shell. It reports the parse error and prompts again.
- Interactive movement, word deletion, typing, and completion replacement respect grapheme boundaries, keeping combining accents and joined emoji intact.
  Unicode aliases and multiline completion prefixes use the correct source positions; cycling completions preserves adjacent text even when it joins the inserted grapheme.
=======
- Unix command history is now private by default, with existing owned storage permissions repaired on load/save.
  Unsafe history objects are rejected and permission errors are reported.
>>>>>>> main

- The type checker gave `index` and `lastIndexOf` a result type of `int`, but both
  return `Maybe[int]` at runtime (`none` when the substring is not found). The
  signature is now `(str str -- Maybe[int])`
- The GitHub action now produces `msh.exe` on Windows.
  Previously, Git Bash's transparent `.exe` handling made `[ -f mshell ]` succeed when only `mshell.exe` existed,
  so the install step created an extensionless `msh` copy that native Windows PATH lookup could not resolve,
  and `shell: msh {0}` steps failed with "command not found".
  The install checks are now gated on the runner OS.
- Commands no longer fail with "Error reclaiming terminal control: ... no such process"
  when several mshell processes share one terminal, as under parallel build runners
  (`redo`, `make -j`) or when a script is backgrounded.
  mshell now transfers terminal control only when it is itself the terminal's current
  foreground process group (the same gate bash and fish use), and a hand-back to a
  previous foreground group that has since exited falls back to mshell's own group
  instead of failing the command.
  A reclaim problem is now at most a warning on stderr; the command's own exit code always stands.
- A fast pipeline whose processes finished before mshell could transfer terminal
  control is no longer killed and reported as failed; the transfer is skipped,
  since the work is already done.
  Restoring terminal modes is now also protected from `SIGTTOU`,
  which could previously stop the shell mid-cleanup when another process group owned the terminal.
  A failure while closing the retained pipeline terminal handle is likewise now a warning,
  never a failure of a pipeline whose commands succeeded.
- Attempted to formally improve the semantics of job control and terminal control on both Linux and Windows.
  Should fix potential bugs when running TUI programs from within mshell scripts.
- On Windows, a command name containing a forward slash (e.g. `./script.msh`)
  is now treated as a file reference instead of being searched for on `PATH`,
  matching the behavior on Linux/macOS. Previously only backslashes were
  recognized as path separators in command position on Windows, so
  cross-platform scripts invoking local scripts with `./` failed.
- Interactive programs now work as a stage of a pipeline. A command that drives
  the terminal (e.g. `... | nvim -`, `... | less`, `... | fzf`) is no longer
  stopped on startup: every external stage of a pipeline is now placed in one
  shared process group that becomes the terminal's foreground group, instead of
  each stage getting its own group with only one (whichever started first)
  receiving the terminal. The pipeline leader now also waits for every stage to
  start before reaping itself, fixing an intermittent `setpgid` race that could
  drop a stage with an "operation not permitted" error and lose its output.
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
