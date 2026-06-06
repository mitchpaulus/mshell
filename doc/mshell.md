# Summary

`mshell` is a concatenative programming language, with an emphasis on:

- Ease of execution of external processes
- Brevity in syntax
- Strong typing as opposed to stringly typed bash or fish

## Execution

Execution of external commands or binaries is different in `mshell`.
Instead of it being the main syntactical construct, in `mshell` you build up a list of arguments, and then at a time of your choosing, you execute it.

```mshell
['my-program' 'arg1' 'arg2'];
```

Often there are different things you want out of your execution, or you want different behavior depending on the exit code.
`mshell` gives you full flexibility to decide with concise syntax.

To initiate execution, you use one of 3 operators:

- `;`: Execute command, always continue. Don't provide any information on the exit code.
- `!`: Execute command, but stop the running script execution on any non-zero exit code.
- `?`: Execute command, leaving the exit code integer on the stack.

```mshell
['false']; # mshell will continue past this point
['true']! # Will execute and continue because of 0 exit code
['my-command']? exitCode!
$"Exit code was {@exitCode}" wl
```

A command that cannot even start (not found, no permission, bad format, ...) is
no longer a fatal error: under `;` and `?` execution continues, and `?` leaves a
*negative* exit code describing exactly what went wrong.
Real processes only return `0`–`255`, so negative codes never collide with a
genuine exit status, and they still count as a failure in `if`/`iff` (only `0`
is success).
The negative codes are organized into bands that carry the underlying OS error
verbatim:

Code              | Meaning                                          | Decode
------------------|--------------------------------------------------|------------------------
`0`–`255`         | Normal exit status from the process.             | —
`-(128 + N)`      | Killed by signal `N`.                            | `signal = -code - 128`
`-255`            | Command not found while searching `PATH`.        | (fixed)
`-256`            | Failed to start, OS error could not be read.     | (fixed)
`-(256 + errno)`  | POSIX start failure, carrying the raw `errno`.   | `errno = -code - 256`
`-(1024 + err)`   | Windows start failure, carrying the raw error.   | `winErr = -code - 1024`

Because the codes carry the *raw* OS error number, they are platform-specific.
Common Linux examples (see `man errno` / `asm-generic/errno.h` for the full set):

Code   | errno      | Meaning
-------|------------|-------------------------------------------
`-258` | `ENOENT`   | Not found at exec time (e.g. missing `#!` interpreter).
`-264` | `ENOEXEC`  | Exec format error (not a runnable binary).
`-269` | `EACCES`   | Permission denied — no execute bit, or the target is a directory.
`-130` | —          | Killed by `SIGINT` (Ctrl-C); `-(128 + 2)`.
`-137` | —          | Killed by `SIGKILL`; `-(128 + 9)`.

mshell does not normalize these across operating systems — if you need a check
that works on more than one platform, OR the relevant codes together yourself:

```mshell
# "Was it not found?" on both Linux (-258 at exec, -255 on PATH) and Windows (-1026)
[my-cmd]? notFound!
@notFound -255 = @notFound -258 = @notFound -1026 = or or
```

The other choice you often have when executing commands is what to do with the standard output. Sometimes you will want to redirect it to a file, other times you will want to leave the contents on the stack to process further. For that, you use the `>`, `>>`, `*`, and `*b` operators.

```mshell
[yourCommand] `fileToRedirectTo` > !  # Redirects stdout to the file, truncating it.
[yourCommand] `fileToRedirectTo` >> ! # Redirects stdout to the file, appending.
[yourCommand] * !                     # Puts all of stdout on the stack as a string.
[yourCommand] *b !                    # Puts all of stdout on the stack as a binary.
```

You can do similar things with standard error. To redirect standard error to a file, use `2>`, and `^` instead of `*`.

To redirect both standard output and standard error to the same file, use `&>` (truncate) or `&>>` (append). This ensures both streams share the same file descriptor, preserving the order of output.

```mshell
[yourCommand] `output.log` &> !  # Redirects both stdout and stderr to the file, truncating it.
[yourCommand] `output.log` &>> ! # Redirects both stdout and stderr to the file, appending.
```

If you manually use `>` and `2>` with exactly the same path string, mshell will automatically use a single file descriptor for both streams, avoiding race conditions. However, if the append modes differ (e.g., `>` and `2>>` to the same file), an error will be raised.

```mshell
[yourCommand] `errors.log` 2> !   # Redirects stderr to the file, truncating it.
[yourCommand] `errors.log` 2>> !  # Redirects stderr to the file, appending.
[yourCommand] ^ !                # Puts all of stderr on the stack as a string.
[yourCommand] ^b !               # Puts all of stderr on the stack as a binary.
```

If you want to put both standard output and standard error onto the stack, you can do that.
Standard output will always be pushed first, and then standard error.

So the following are equivalent:

```mshell
[yourCommand] *b ^b ! # Order here does not matter
[yourCommand] ^b *b !
```

Summary of external command operators:

Operator | Effect on external commands                | Notes
---------|--------------------------------------------|-----------------------------------------------------
`;`      | Execute and continue.                      | No exit code on the stack.
`!`      | Execute and stop on non-zero exit.         | Uses the command exit code.
`?`      | Execute and push the exit code.            | Integer left on the stack; negative if the command could not start (see codes above).
`>`      | Redirect stdout to a file.                 | Truncates the file.
`>>`     | Redirect stdout to a file.                 | Appends to the file.
`*`      | Capture stdout to the stack.               | As a string.
`*b`     | Capture stdout to the stack.               | As binary.
`2>`     | Redirect stderr to a file.                 | Truncates the file.
`2>>`    | Redirect stderr to a file.                 | Appends to the file.
`&>`     | Redirect both stdout and stderr to a file. | Truncates the file.
`&>>`    | Redirect both stdout and stderr to a file. | Appends to the file.
`^`      | Capture stderr to the stack.               | As a string.
`^b`     | Capture stderr to the stack.               | As binary.
`<`      | Feed stdin from a value.                   | String, path, or binary.
`<>`     | In-place file modification.                | Reads file to stdin, writes stdout back on success.
`&`      | Mark the command list to run asynchronously. | Marks the list; the trailing `;`/`!` starts the subprocess and returns immediately without waiting. Stdout and stderr default to discarded.

### Redirection on quotations

All of the redirection operators above also work on quotations. This is useful when you want to redirect the output of mshell code that uses `wl`, `wle`, or other built-in functions that write to stdout or stderr. It is also useful when you have many commands that you want to run while appending all the outputs to a single file, without having to put the redirection on each command invocation.

```mshell
(
    "Hello from stdout" wl
    "Hello from stderr" wle
) `output.log` &> x # Redirects both stdout and stderr from the quotation to output.log
```

```mshell
(
    [echo "Running step 1"]!
    [echo "Running step 2"]!
    [echo "Running step 3"]!
) `build.log` >> x # All command outputs appended to build.log
```

### Input Redirection

Use `<` to feed data into stdin. The type of the value on top of the stack determines how the input is provided.

`String` values are encoded as UTF-8 and streamed as text.

```mshell
[wc -l] "line 1\nline 2\n" < ; # Counts the lines from the provided string
```

`Path` values open the referenced file and stream its contents.

```mshell
[wc -l] `myfile.txt` < ; # Equivalent to shell input redirection from a file
```

`Binary` values are written directly without any string conversion.

```mshell
[md5sum] `binary_stdin.bin` readFileBytes < ; # Streams raw bytes into the command
```

### In-place file modification

The `<>` operator enables in-place file modification. It reads a file's contents, passes them to the command's stdin, and on successful completion (exit code 0), writes the command's stdout back to the same file. This is similar to the `sponge` command from moreutils.

```mshell
[sort -u] `file.txt` <> !
```

This is equivalent to, but safer than:

```mshell
[sort -u] `file.txt` < `file.txt.tmp` > !
[mv file.txt.tmp file.txt] !
```

#### Semantics

- The file is read when `<>` is evaluated, capturing its contents at that moment.
- If the command exits with code 0 (success), stdout replaces the file contents.
- If the command exits with a non-zero code (failure), the original file is preserved unchanged.
- File permissions are preserved.

#### Requirements

- Only works with lists (external commands), not quotations.
- Requires a `Path` type (backtick-quoted), not a string.
- The file must exist at the time `<>` is evaluated.

#### Examples

```mshell
# Sort a file in place, removing duplicates
[sort -u] `data.txt` <> !

# Format JSON in place
[jq .] `config.json` <> !

# Filter lines in place (keep only lines containing "error")
[grep error] `log.txt` <> !

# Using with ? to check exit code without failing
[sort -u] `file.txt` <> ?
0 = if
    "File sorted successfully" wl
else
    "Sort failed, file unchanged" wl
end
```

### How mshell finds binaries

When executing an external command, `mshell` resolves the binary name using a two-step process. First it checks the bin map file for an override, and if none is found it falls back to `PATH`.

The bin map file lives alongside the history files (for example, `$XDG_DATA_HOME/msh/msh_bins.txt` or `~/.local/share/msh/msh_bins.txt` on Linux/macOS, or `%LOCALAPPDATA%\\msh\\msh_bins.txt` on Windows). Each non-empty line is a tab-separated pair of fields: the binary name and the absolute path to the binary. Both fields are trimmed, the name must not contain path separators, and the line must contain exactly two fields.

```
mytool	/usr/local/bin/mytool
another	/home/me/bin/another
```

To manage the file, use the `msh bin` subcommands:

- `msh bin add <path>`: add/replace using the file basename and absolute path
- `msh bin add <name> <path>`: add/replace using an explicit name
- `msh bin remove <name>`: remove an entry by name
- `msh bin list`: print the bin map file contents
- `msh bin path`: print the bin map file path
- `msh bin edit`: edit the file in `$EDITOR`, or the platform default opener if `$EDITOR` is unavailable
- `msh bin audit`: report invalid or missing entries
- `msh bin debug <name>`: show lookup details for a binary

## Process Substitution

Process substitution is done using the `psub` operator.

```mshell
[my_command_needing_file "my test" psub];
```

## Startup Files

`msh` loads startup files before running code.

- Startup files are always version-specific.
- The standard library is loaded from
  `$XDG_DATA_HOME/msh/<version>/std.msh` on Linux/macOS
  (falling back to `~/.local/share/msh/<version>/std.msh`)
  or `%LOCALAPPDATA%\msh\<version>\std.msh` on Windows.
- The user init file is loaded from
  `$XDG_CONFIG_HOME/msh/<version>/init.msh` on Linux/macOS
  (falling back to `~/.config/msh/<version>/init.msh`)
  or `%LOCALAPPDATA%\msh\<version>\init.msh` on Windows.
- The standard library is required at the resolved path.
- For scripts without `VER`, `msh` uses the current executable version.
- For scripts without `VER` and interactive use, missing `init.msh` is allowed unless `MSHINIT` is explicitly set.
- If a script declares `VER "vX.Y.Z"` and the current executable is a different version, `msh` looks for `msh-vX.Y.Z` on `PATH` and re-executes the script with that binary.
- When `VER` is present, the version-specific `init.msh` is required.
- When `VER` is present, `MSHSTDLIB` and `MSHINIT` are cleared so startup comes from the versioned locations.
- `MSHSTDLIB` and `MSHINIT` only override startup for interactive use and scripts without `VER`.
- `msh edit init` opens the current init file path using `$EDITOR`; if `$EDITOR` is unavailable, it falls back to the platform default opener (`xdg-open` on Linux, `open` on macOS, `Start-Process` via PowerShell on Windows).

## Tilde Substitution

When encountering a literal token that begins with `~/` or is `~` alone,
the token will be replaced with the user's home directory.

## Environment Variables

Environment variables are accessed and set using the `$` prefix.
They can be set like a normal variable using the '!' suffix.
Environment variables are always "exported" to subprocesses.

The presence of an environment variable can be checked using the `?` suffix.

```mshell
@HOME cd

# Setting and exporting for subprocesses
"Hello, World!" $MSHELL_VAR!

# Checking for variable existence
[($MY_ENV_VAR?) ("MY_ENV_VAR exists") ("MY_ENV_VAR does not exist")] if wl
```

## Indexing

If the indexing is fixed, there is dedicated syntax for it.

```mshell
[ 4 3 2 1 ] :1:  # 3
[ 4 3 2 1 ] 1:3  # [ 3 2 ]
[ 4 3 2 1 ] :3   # [ 4 3 2 ]
[ 4 3 2 1 ] 2:   # [ 2 1 ]
# Works on any list element on the stack, not just list literals
[ 4 3 2 1 ] myList!
@myList :1:  # 3
[ [ 'nested' 'list' ] ['is' 'here'] ] nested!
@nested :1: :0: # 'is'
```

For non-fixed indexing, you have the `nth` operator.

```mshell
[ 4 3 2 1 ] 2 nth # 2
```

## Error Handling

By default, executing a process that returns with a non-zero exit code does not stop the execution of the script.
If the desired behavior is to stop the execution on any non-zero exit code, the keyword `soe` can be used.

## Interactive CLI

History search is prefix-based and case-insensitive. The prefix is whatever is currently in the input buffer; editing the buffer resets the prefix for the next search.

- Ctrl-P: search backward through history by prefix
- Ctrl-N: search forward through history by prefix
- Ctrl-Y: accept the inline history completion
- Ctrl-Space: insert a literal space without expanding aliases
- Alt-.: insert the last argument from history; repeat to cycle older entries
- Tab: complete the current token; press Tab again to cycle matches and fill the input
- Shift-Tab: cycle completion backward when matches are active
- Ctrl-N/Ctrl-P: when cycling completions, move forward/backward through matches

### Definition-based completions

The CLI can use definition metadata to provide argument completions for binaries. Add a `complete` key in the metadata dictionary of a `def` to register it for one or more command names. The definition is invoked with a clean stack containing a single list of argument tokens (excluding the binary name and the current prefix), and it should return a list of strings.

```mshell
def mshCompletion { 'complete': ['msh' 'mshell'] } ([str] -- [str])
    input!
    ['-h' '--help' '--html' '--lex' '--parse' '--check-types' '--type-check-only' '--version' '-c'] options!
    ['lsp' 'bin' 'edit' 'completions'] subcommands!
    @options @subcommands extend
end
```

The standard library includes `__gitCompletion` for git argument completion.

### Shell completions

mshell can emit shell completion scripts for:

- bash
- fish
- nushell
- elvish

Run `msh completions <shell>` and source the output:

```
# bash
source <(msh completions bash)

# fish
msh completions fish | source

# elvish
msh completions elvish | eval

# nushell
msh completions nushell | save --force $"($nu.default-config-dir)/completions/msh.nu"
use $"($nu.default-config-dir)/completions/msh.nu" *
```

### Binary map overrides

mshell supports a simple bin map file that overrides PATH lookups. The file lives alongside the history files (e.g. `$XDG_DATA_HOME/msh/msh_bins.txt` or `~/.local/share/msh/msh_bins.txt` on Linux/macOS, or `%LOCALAPPDATA%\msh\msh_bins.txt` on Windows).

Each line is a single mapping in the form:

```
binary	/full/path/to/binary
```

mshell expects two tab-separated fields and trims both sides.

CLI helpers:

- `msh bin add <path>`: add/replace an entry using the file basename and absolute path (fails if the file does not exist)
- `msh bin add <name> <path>`: add/replace an entry using the provided name and path (fails if the file does not exist)
- `msh bin remove <name>`: remove an entry by binary name
- `msh bin list`: print the bin map file contents
- `msh bin path`: print the msh_bins.txt file path
- `msh bin edit`: edit the bin map file in `$EDITOR`, or the platform default opener if `$EDITOR` is unavailable
- `msh bin audit`: report entries that are missing, not absolute, broken symlinks, or not executable (and report if the file is missing)
- `msh bin debug <name>`: print PATH/bin map lookup details for a binary

## Data Types

### `str` Strings

Double quoted strings have the following escape sequences:

- `\e`: Escape
- `\n`: Newline
- `\t`: Tab
- `\r`: Carriage return
- `\\`: Backslash
- `\"`: Double quote

No escaping is done within single quoted strings or paths.

### Paths

Since paths are such a common object to deal with in shell scripts, mshell has a dedicated type for paths.

Paths are created by wrapping a string in backticks. No escaping is done. If you need to escape, then use string interpolation and `toPath`.

```mshell
`/path/to/file`
```

### Booleans

Booleans are represented by `true` and `false`.

### Quotations

Quotations are blocks of code that can be executed. They are created by wrapping code in parentheses.

```mshell
(1 2 +)
```

### Lists

Lists are created by wrapping elements in square brackets.
No commas are required between elements.

```mshell
[1 2 3]
```

Lists can be added together with the `+` operator. The result is a new list object.

```mshell
[1 2] [3 4] + # [1 2 3 4]
```

### Dictionaries/Associative Arrays

mshell has the concept of dictionaries or associative arrays, but only with string keys.

Dictionaries can be instantiated using dictionary literals, like:

```
{ "key": 1 }
```

Be careful with some of the lexing around the colon, as it's used with indexing.

```
{ "key":1 } # Bad because ':1' is treated as index
```

### Date/Times

Date/times can be entered using a literal syntax, in ISO-8601 format.

```mshell
# All are valid
2023-10-01
2023-10-01T13
2023-10-01T13:01
2023-10-01T13:01:30
```

Dates can be subtracted from each other, and the result is a floating-point number of days.

```mshell
2023-10-02 2023-10-01 - # 1.0
```

## Type System

Use `msh --check-types script.msh` to run static type checking before script execution.
Use `msh --type-check-only script.msh` to run the same static checks and exit without evaluating the script.
The checker validates stack effects, definition bodies, quotation arguments, built-ins, variable bindings, and branch reconciliation.

Definitions use stack-effect signatures.
Inputs are listed before `--`, outputs after it, and the rightmost input is the top stack item consumed first.

```mshell
def addOne (int -- int)
    1 +
end

def fullName (str str -- str)
    last!, first!
    $"{@first} {@last}"
end
```

Primitive static type names include `int`, `float`, `bool`, `str`, `path`, `datetime`, `bytes`, and `none`.
Named runtime types such as `Grid`, `GridView`, and `GridRow` are also available.

Type expressions compose with lists, dictionaries, unions, `Maybe`, and quotation types.

```mshell
[str]                 # list of strings
{str: int}            # string-keyed dictionary of ints
{name: str, age: int} # dictionary shape
Maybe[int]            # optional int
int | str             # union
(int int -- bool)     # quotation type
```

Top-level type declarations name larger type expressions.
Use them for casts and for naming record-like dictionaries and unions that are reused by other type declarations.
Current definition signatures still use the historical signature parser, so dictionary shapes in `def` signatures are written with quoted field names.

```mshell
type Person = {name: str, age: int}
type Cell = int | float | str | bool | none
type Row = [Cell]

{ "name": "Ada", "age": 36 } as Person :age? 1 +
```

Dictionary types are split into homogeneous dictionaries and shapes.
A homogeneous dictionary is for dynamic keys where every value has the same type.
In a type expression, write `{str: int}`.
In older definition signatures, `{ int }` or `{ *: int }` means the same string-keyed dictionary of ints.

```mshell
{ "passed": 10, "failed": 2 } as {str: int} values len
```

A shape is for record-like dictionaries with known fields.
Shapes let `:field?` access preserve the precise field type.

```mshell
def labelPerson ({ "name": str, "age": int, "active": bool } -- str)
    person!
    @person :name? name!
    @person :age? age!
    $"{@name} ({@age})"
end
```

Lists are homogeneous when every element has one type, such as `[int]` or `[Person]`.
This is the strongest list type because higher-order functions preserve the element type.

```mshell
def doubleAll ([int] -- [int])
    (2 *) map
end

def names ([{ "name": str, "age": int }] -- [str])
    (:name?) map
end
```

Heterogeneous lists are represented as lists whose element type is a union.
For example, `[int | str]` means every element is either an int or a string.
This is useful for JSON-like data, spreadsheet rows, and other shell data where each cell can be one of a fixed set of types.

```mshell
type Cell = int | float | str | bool
type Row = [Cell]
type Table = [Row]

[1 "Ada" true] as [int | str | bool]
```

The checker currently models heterogeneous lists as lists of unions, not fixed-length tuples with per-index types.
That means index `:0:` does not by itself prove a specific per-position type unless the value is converted or asserted.

Quotation types describe the stack effect of code values.
Operators with multiple valid signatures keep an overload set until context resolves them.
For example, `(>)` can become `(int int -- bool)`, `(float float -- bool)`, `(str str -- bool)`, or `(datetime datetime -- bool)` depending on the expected quotation type.

Control-flow branches must reconcile stack and variable state across reachable paths.
Branches that diverge with `return`, `break`, or `continue` are excluded from reconciliation.
When the reachable arms of a `match` or `if`/`else` block leave different types in a
stack slot, those types are joined into a union for the code that follows.
For example, `match []: 0.0, _ :> sum end` produces an `int | float`.
An overloaded operation applied to a union operand is resolved for every member of
the union; it type-checks when each member is handled, and the result is the union
of the per-member results.
So `int | float` through `toFloat` gives `float`, and `int | float { … } numFmt`
formats fine.
An operation that is valid for only some members is a type error — dividing an
`int | float` by a `float` fails, because the `int` member has no matching overload.

For more detail, see the generated Type System help page.

## Definitions

Definitions use `def` with an optional metadata dictionary before the type signature.

```mshell
def myfunction { 'key': 10 } (str -- str)
  ...
end
```

Metadata values must be static: strings (single or double quoted), integers, floats, booleans, or nested lists/dicts of the same. Interpolated strings are not allowed.

### Tail-Call Optimization

Recursive definitions in tail position are optimized to avoid stack overflow.
A call is in tail position when it is the last operation in a definition body (including inside `iff` branches).

```mshell
# Tail-recursive countdown - optimized
def countdown (int -- int)
    n!
    @n 0 <= (0) (@n 1 - countdown) iff
end

# Tail-recursive factorial with accumulator - optimized
def factorial (int int -- int)
    n!, acc!
    @n 1 <= (@acc) (@n 1 - @acc @n * factorial) iff
end

# Non-tail recursion - not optimized (uses Go stack)
def sumTo (int -- int)
    n!
    @n 0 <= (0) (@n 1 - sumTo @n +) iff
end
```

In the `sumTo` example, the recursive call is not in tail position because `@n +` follows it.
To optimize, convert to an accumulator pattern:

```mshell
def sumTo-acc (int int -- int)
    n!, acc!
    @n 0 <= (@acc) (@n 1 - @acc @n + sumTo-acc) iff
end

# Call with initial accumulator of 0
def sumTo (int -- int)
    0 sumTo-acc
end
```

## Control Flow

### if / else* / else / end

`if` is the primary conditional. The condition is evaluated before `if` and
popped from the stack.

```mshell
true if
    "condition was true" wl
end
```

Add an `else` branch for the false case:

```mshell
false if
    "true branch" wl
else
    "false branch" wl
end
```

Use `else*` and `*if` for else-if chains:

```mshell
2 n!
@n 1 = if
    "one" wl
else* @n 2 = *if
    "two" wl
else* @n 3 = *if
    "three" wl
else
    "other" wl
end
```

Conditions can be booleans or integers.
For integers, `0` is true (like Unix exit codes) and non-zero is false.

```mshell
0 if "zero is true" wl end
1 if "not printed" wl end
```

### iff

`iff` is a postfix conditional that executes quotations based on a condition.
It has a two-argument form (no false branch) and a three-argument form.

```mshell
# Two-argument form: condition (true-quote) iff
true ("was true" wl) iff

# Three-argument form: condition (true-quote) (false-quote) iff
false ("true" wl) ("false" wl) iff
```

It is useful for inline conditionals:

```mshell
@count 0 > (@items process) iff
```

### loop

`loop` repeatedly executes a quotation until `break` is called.

```mshell
0 i!
(
    @i 5 >= if break end
    @i wl
    @i 1 + i!
) loop
# Output: 0 1 2 3 4 (one per line)
```

### break

`break` exits the innermost loop.

```mshell
0 i!
(
    @i 1 + i!
    @i 3 = if
        "breaking" wl
        break
    end
    @i wl
) loop
"done" wl
# Output: 1 2 breaking done (one per line)
```

### continue

`continue` skips the rest of the current iteration and starts the next one.

```mshell
0 i!
(
    @i 5 >= if break end
    @i 1 + i!
    @i 3 = if continue end
    @i wl
) loop
# Output: 1 2 4 5 (3 is skipped)
```

### Prefix Quote Syntax

Prefix quotes are an alternative syntax for applying functions to quotations.
The syntax `functionName. ... end` (a period appended to the function name) is
equivalent to `(...) functionName`.

```mshell
# Traditional postfix syntax
[1 2 3 4 5] (3 >) filter

# Prefix quote syntax
[1 2 3 4 5] filter. 3 > end
```

They work with any function that expects a quotation, including `map`, `filter`,
`each`, and user-defined functions:

```mshell
[1 2 3] map. 2 * end        # [2 4 6]
["a" "b" "c"] each. wl end  # prints a, b, c
```

Prefix quotes can be chained and nested:

```mshell
# Filter positives, then double them
[-1 2 -3 4] filter. 0 > end map. 2 * end   # [4 8]

# For each sublist, filter elements > 5
[[1 2 3] [4 5 6] [7 8 9]] map. filter. 5 > end end   # [[] [6] [7 8 9]]
```

This is also handy for turning the boolean operators `and` and `or` into a more
traditional infix lookup format:

```mshell
true or. false end and. true end   # Like (true | false) & true = true
```

## Pattern Matching

The `match ... end` block provides multi-way dispatch.
Arms are checked top-to-bottom and the first matching arm's body is executed.
If no arm matches, it is a runtime error.

Each arm has the form: `pattern : body ,` or `pattern :> body ,`
The trailing comma on the last arm is optional.

`:` consumes the matched subject before the arm body runs.
`:>` preserves the matched subject on the stack when the arm body runs.
This is independent of pattern kind and bindings.

### Wildcard

`_` matches any value (catch-all).

### Value Matching

Literal values (integers, floats, strings, booleans, paths) match
if the subject equals the pattern value.

```mshell
"hello" match
    "hello" : "greeting",
    "bye"   : "farewell",
    _       : "unknown",
end wl # Output: greeting
```

Use `:>` when the arm body needs the matched subject, for example when matching
on type before sending the value through another function:

```mshell
[1 2 3] match
    list :> len str,
    str  :> len str,
    _    : "other",
end wl # Output: 3
```

### Type Matching

Type keywords match based on the subject's type:
`int`, `float`, `str`, `bool`, `list`, `dict`, `path`, `date`, `quotation`, `maybe`, `binary`.

```mshell
42 match
    int : "integer",
    str : "string",
    _   : "other",
end wl # Output: integer
```

Follow a type keyword with a name to bind the matched value (like `just v`):

```mshell
"hello" match
    int n : @n str,
    str s : @s len str,
    _     : "other",
end wl # Output: 5
```

### Maybe Destructuring

`just v` matches a Maybe that is Just, binding the inner value to `v`.
`none` matches a Maybe that is None.
Use `just _` to match Just without binding.

```mshell
myDict "key" get match
    just v : @v,
    none   : "not found",
end wl # Output: value
```

Bindings and separator choice are independent, so preserving the subject is also valid:

```mshell
myDict "key" get match
    just v :> ?,
    none   :  "not found",
end wl # Output: value
```

### List Destructuring

A list pattern `[a b c]` matches a list of exactly that length,
binding elements to the given names.
Use `_` to discard a position.
Use `...rest` to capture remaining elements.

```mshell
myList match
    [head ...tail] : @head,
    []             : "empty",
    _              : "not a list",
end wl # Output: 1
```

`...rest` can also appear in the middle of the pattern.
Items before it match from the front, items after it match from the back,
and the spread binding receives everything in between.

```mshell
[1 2 3 4 5] match
    [first ...middle last] : [@first @middle @last] (str) map " | " join,
    _                      : "no match",
end wl # Output: 1 | [2 3 4] | 5
```

### Dict Destructuring

A dict pattern `{ 'key': v }` matches a dict that contains the given keys,
binding their values to the given names.

```mshell
person match
    { 'name': n, 'age': a } : @n,
    _                       : "missing fields",
end wl # Output: Alice
```

Destructuring bindings are added to the outer variable scope,
the same as `if` blocks.

```mshell
1 outerVariable!
10 match
    int :> @outerVariable + str,
    _   : "Not found",
end wl # Output: 11
```

## Built-ins

- `.s`: Print stack at current location (--)
- `.b`: Prints paths to all known binaries (--)
- `.def`: Print available definitions at current location (--)
- `.env`: Print all environment variables to stderr in sorted order (--)
- `dup`: Duplicate (a -- a a)
- `swap`: Swap (a b -- b a)
- `drop`: Drop (a -- )
- `over`: Over, copy second element to top `(a b -- a b a)`
- `rot`: Rotate the top three items, `( a b c -- b c a )`
- `-rot`: Rotate the top three items in the opposite direction `( a b c -- c a b )`
- `nip`: Remove second item, `( a b -- b )`
- `w`: Write to stdout (str|int|binary -- ). Other types must be converted with `str` first.
- `wl`: Write line to stdout (str|int -- ). Binary is not allowed because trailing newlines after raw bytes are rarely intended; use `w` for binary output. Other types must be converted with `str` first.
- `we`: Write error to stderr (str|int|binary -- ).
- `wle`: Write error line stderr (str|int -- ).
- `len`: Length of string/list `([a] -- int | str -- int)`
- `args`: List of string arguments. Does not include the name of the executing file. `( -- [str])`
- `glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack. Relies on golang's [filepath.Glob](https://pkg.go.dev/path/filepath#Glob), which in the current implementation, the response is sorted. `(str -- [str])`
- `/`: Divide numbers or join paths. `(numeric numeric -- numeric)` treats the top of stack as divisor. `(path path -- path)` joins the paths using the OS separator.
- `x`: Interpret/execute quotation `(quote -- )`
- `toFloat`: Convert to float. `(numeric -- Maybe[float])`
- `toInt`: Convert to int. `(numeric -- Mabye[int])`
- `exit`: Exit the current script with the provided exit code. `(int -- )`
- `read`: Read a line from stdin. Puts a str and bool of whether the read was successful on the stack. `( -- str bool)`
- `prompt`: Write a prompt string to the controlling TTY and read a line from the controlling TTY. Fails if no controlling TTY is available. `(str -- str)`
- `stdin`: Drop stdin onto the stack `( -- str)`
- `::`: Drop stdin onto the stack and split by lines `( -- [str])`. This is a shorthand for `stdin lines`.
- `foldl`: Fold left. `(quote initial list -- result)`
- `wt`: "Whitespace table", puts stdin split by lines and whitespace on the stack. `( -- [[str]])`
- `tt`: "Tab table", puts stdin split by lines and tabs on the stack. `( -- [[str]])`
- `ttFile`: "Tab table" from file, puts content from file name split by lines and tabs on the stack. `(str -- [[str]])`
- `unlines`: Join a list of strings into a single string using `\n` line endings. `([str] -- str)`
- `unlinesCrLf`: Join a list of strings into a single string using `\r\n` line endings. `([str] -- str)`
- `uw`: Shorthand for `unlines w` `([str] -- )`
- `tuw`: Shorthand for `(tjoin) map uw` `([[str]] -- )`
- `runtime`: Get the current OS runtime. This is the output of the GOOS environment variable. Common possible values are `linux`, `windows`, and `darwin`. `( -- str)`
- `hostname`: Get the current OS hostname. On failure to get, puts 'unknown' on the stack. `( -- str)`
- `parseCsv`: Parse a CSV file into a list of lists of strings. Input can be a path/literal file name, or the string contents itself. (`path|str -- [[str]])`
- `toGrid`: Build a Grid from a list of string rows. The first row supplies column headers and remaining rows become string-valued data rows. (`[[str]] -- Grid`)
- `gridValues`: Extract Grid or GridView cell values as row-major lists, without a header row and without coercing cell types. (`Grid|GridView -- [[a]]`)
- `toCsvCell`: Escape a single CSV cell. If the value contains `,`, `"`, or a newline, wraps the value in double quotes and doubles any embedded quotes; otherwise returns the input unchanged. (`str -- str`)
- `toCsv`: Serialize a list of rows to a CSV string. Each cell is escaped with `toCsvCell`, cells are joined with `,`, and rows are joined with `\n`. (`[[str]] -- str`)
- `parseJson`: Parse JSON from a string, binary, or file path into mshell objects. (`path|str|binary -- list|dict|numeric|str|bool`)
- `parseExcel`: Parse an `.xlsx` (OOXML) spreadsheet into a list of sheets in workbook (tab) order. Each sheet is a dict with a `name` key (the worksheet name), a `data` key holding a rectangular list of rows (list of lists), a `hidden` key (bool; `true` for hidden or veryHidden sheets), and a `visibility` key (`"visible"`, `"hidden"`, or `"veryHidden"`). Cell values are typed: numbers become floats (dates appear as Excel serial floats), strings become strings (shared, inline, and formula-string results all resolved), booleans become booleans, error cells (e.g. `#DIV/0!`) become `none`, and empty/padding cells are the empty string. Chartsheets are skipped; hidden worksheets are included. Dates are returned as raw Excel serial floats; apply `fromOleDate` at the call site to convert. `parseExcel` assumes the default 1900-based date system, which matches `fromOleDate`'s OLE epoch (1899-12-30). Workbooks saved with the 1904 date system (`<workbookPr date1904="true"/>`, seen on some files originally authored on older Mac Excel or with the "Use 1904 date system" option enabled) have serials offset by 1462 days; on those files, add 1462 to each serial before calling `fromOleDate`, e.g. `@wb :0: :data? :3: :0: 1462 + fromOleDate`. (`path|binary -- list`)
- `seq`: Generate a list of integers, starting from 0. Exclusive end to integer on stack. `2 seq` produces `[0 1]`. `(int -- [int])`
- `repeat`: Create a list containing the provided value repeated `n` times. `(a int -- [a])`
- `binPaths`: Puts a list of lists with 2 items, first is the executable name, second is the full path to the executable. `(-- [[str]])`
- `urlEncode`: URL-encode a string or dictionary of parameters. `(str|dict -- str)`
- `typeof`: Return the type name of the top stack item `(a -- str)`


## File/Directory Functions

- `toPath`: Convert to path. `(str -- path)`
- `absPath`: Convert to absolute path. `(str|path -- path)`
- `isDir`: Check if path is a directory. `(path -- bool)`
- `isFile`: Check if path is a file. `(path -- bool)`
- `fileExists`: Check whether a file or directory exists. `(str|path -- bool)`
- `hardLink`: Create a hard link. `(existingSourcePath newTargetPath -- )`
- `tempFile`: Create a temporary file with go [`os.CreateTemp`](https://pkg.go.dev/os#CreateTemp) using `dir=""` (defaults to [`os.TempDir`](https://pkg.go.dev/os#TempDir)) and pattern `msh-`, so the filename starts with `msh-` plus a random suffix. Pushes the full path. The file is not removed automatically; use `rm`/`rmf` when you are done. `( -- path)`
- `tempFileExt`: Create a temporary file with a required extension. The extension input is canonicalized to start with `.` if needed. Uses go [`os.CreateTemp`](https://pkg.go.dev/os#CreateTemp) with `dir=""` (defaults to [`os.TempDir`](https://pkg.go.dev/os#TempDir)) and pattern `msh-*` plus the canonicalized extension, so the filename ends with the extension. Pushes the full path. The file is not removed automatically; use `rm`/`rmf` when you are done. `(str|path -- path)`
- `tempDir`: Return path to the OS specific temporary directory. No checks on permission or existence, so never fails. See [`os.TempDir`](https://pkg.go.dev/os#TempDir) in golang. `$TMPDIR` or `/tmp` for Unix, `%TMP%`, `%TEMP%, %USERPROFILE%`, or the Windows directory (`C:\Windows`). `( -- path)`
- `rm`: Remove file. Will stop execution on IO error, including file not found. `(str -- )`
- `rmf`: Remove file. Will not stop execution on IO error, including file not found. `(str -- )`
- `cp`: Copy file or directory. `(str:source str:dest -- )`
- `mv`: Move file or directory. `(str:source str:dest -- )`
- `readFile`: Read file into string. `(str -- str)`
- `readFileBytes`: Read file into binary data. `(str -- binary)`
- `readTsvFile`: Read a TSV file into list of list of strings. `(str -- [[str]])`
- `cd`: Change directory `(str -- )`
- `pwd`: Get current working directory `( -- str)`
- `mshFileManager`: Open the built-in file manager.
   Pops a starting directory from the stack.
   On exit, changes the working directory to the directory the user navigated to.
   On Windows, pressing `h` at the root of a drive shows the mounted drive letters so you can switch volumes.
   The preview pane short-circuits common binary extensions and shows first-level contents for `.zip` and `.tar.gz` archives.
   Yank bindings copy text about the selected entry to the system clipboard: `yf` (file name), `yp` (full path), `yg` (path relative to the enclosing `.git` directory). `(str -- )`
- `writeFile`: Write string to file (UTF-8). Overwrites file if it exists. `(str content str file -- )`
- `appendFile`: Append string to file (UTF-8). `(str content str file -- )`
- `fileSize`: Get size of file in bytes. Returns a Maybe in case file doesn't exist or other IO error. `(str -- Maybe int)`
- `lsDir`: Get list of all items (files and directories) in directory. Full paths to the items. `(str -- [str])`
- `sha256sum`: Get SHA256 checksum of file. `(path -- str)`
- `md5`: Get md5 checksum of file or string. `(path|str -- str)`
- `files`: Get list of files in current directory. Not recursive. `(str -- [str])`
- `dirs`: Get list of directories in current directory. Not recursive. `(str -- [str])`
- `isCmd`: Check whether item is a command that can be found in PATH. `(str -- bool)`
- `removeWindowsVolumePrefix`: Remove volume prefix from a Windows path `(str -- str)`

## Math Functions

- `abs`: Absolute value `(numeric -- numeric)`
- `inc`: Increment integer value in place `(int -- int)`
- `max2`: Maximum of two numbers `(numeric numeric -- numeric)`
- `max`: Maximum of list of numbers or datetimes `([numeric] -- numeric) | ([DateTime] -- DateTime)`
- `transpose`: Transpose list of lists `([[a]] -- [[a]])`
- `min2`: Minimum of two numbers `(numeric numeric -- numeric)`
- `min`: Minimum of list of numbers or datetimes `([numeric] -- numeric) | ([DateTime] -- DateTime)`
- `mod`: Modulus `(numeric numeric -- numeric)`
- `floor`: Round a number down to the nearest integer. `(numeric -- int)`
- `ceil`: Round a number up to the nearest integer. `(numeric -- int)`
- `round`: Round to nearest integer. Rounds half-way away from zero. `(numeric -- int)`

## String Functions

- `str`: Convert to string
- `findReplace`: Find and replace in string. `findReplace (str str, str find, str replace -- str)`
- `leftPad`: Pad the left side of a string to reach the requested length. `(str str int -- str)`
- `lines`: Split string into list of string lines
- `split`: Split string into list of strings by delimiter. (str delimiter -- [str])
- `wsplit`: Split string into list of strings by runs of whitespace. (str -- [str])
- `join`: Join list of strings into a single string, (list delimiter -- str)
- `unlines`: Join list of strings into a single string using `\n` line endings. `([str] -- str)`
- `unlinesCrLf`: Join list of strings into a single string using `\r\n` line endings. `([str] -- str)`
- `in`: Check for substring in string. (totalString subString -- bool)
- `index`: Get index of first occurrence of substring in string. Returns Maybe[int] with None for the substring not being found. `(str str -- Maybe[int])`
- `lastIndexOf`: Get index of last occurrence of substring in string. Returns Maybe[int] with None for the substring not being found. `(str str -- Maybe[int])`
- `tab`: Puts a tab character on the stack `( -- str)`
- `tsplit`: Split string into list of strings by tabs. `(str -- [str])`
- `trim`: Trim whitespace from string. `(str -- str)`
- `trimStart`: Trim whitespace from start of string. `(str -- str)`
- `trimEnd`: Trim whitespace from end of string. `(str -- str)`
- `startsWith`: Check if string starts with substring. `(str str -- bool)`
- `endsWith`: Check if string ends with substring. `(str str -- bool)`
- `title`: Convert string to title case (English, uses [`cases.Title`](https://pkg.go.dev/golang.org/x/text/cases#Title)). `(str -- str)`
- `lower`: Convert string to lowercase. `(str -- str)`
- `upper`: Convert string to uppercase. `(str -- str)`
- `toFixed`: Convert number to string with fixed number of decimal places. `(numeric int -- str)`
- `numFmt`: Format numbers with an options dictionary `(numeric dict -- str)`. Keys (all optional):
  - `'decimals'` (int): fixed decimal places.
- `'sigFigs'` (int): minimum significant digits; ignored when `'decimals'` is present; defaults to 3 when neither is set.
- `'preserveInt'` (bool): when true, don't round integers longer than `'sigFigs'`.
  - `'decimalPoint'` (str): character(s) to use instead of `"."`.
  - `'thousandsSep'` (str): separator (default `","`, only applied when `'grouping'` is provided).
  - `'grouping'` (list[int]): LC_NUMERIC-style group sizes (reused from the end); if `'thousandsSep'` is set without `'grouping'`, `[3]` is assumed.
- `countSubStr`: Count occurrences of substring in string. `(str str -- int)`
- `skip`: Skip first n characters from string using the same indexing logic as string slicing. `(str int -- str)`
- `take`: Take first n characters from string using the same indexing logic as string slicing. `(str int -- str)`
- `base64encode`: Encode binary data as base64. `(binary -- str)`
- `base64decode`: Decode base64 string into binary data. `(str -- binary)`
- `utf8Str`: Decode UTF-8 bytes into a string. `(binary -- str)`
- `utf8Bytes`: Encode a string as UTF-8 bytes. `(str -- binary)`

## List Functions

- `append`: Append, `([a] a -- [a])`
- `map`: Map a quotation over a list, `([a] (a -- b) -- [b])`
- `enumerate`: Pair each element with its zero-based index, returning `{"item": a, "index": int}` dicts. Access the fields with `:item?` and `:index?`. `([a] -- [{"item": a, "index": int}])`
- `enumerateN`: Pair elements with indices starting from the supplied offset, returning `{"item": a, "index": int}` dicts. `([a] int -- [{"item": a, "index": int}])`
- `each`: Execute a quotation for each element in a list, `([a] (a -- ) -- )`
- `eachWhile`: Execute a quotation for each element in a list, stopping when a false is left on the stack `([a] (a -- bool) -- )`
- `takeWhile`: Return the leading elements of a list while the predicate remains true. `([a] (a -- bool) -- [a])`
- `dropWhile`: Drop leading elements while the predicate remains true. `([a] (a -- bool) -- [a])`
- `2unpack`: Unpack a two-element list onto the stack. `([a] -- a a)`
- `2apply`: Apply a binary quotation to a two-element list. `([a] (a a -- c) -- c)`
- `2each`: Apply a quotation to the two values on the stack individually, returning results in the original order. `(a b (a -- c) -- c c)`
- `id`: Identity quote — leaves the top stack value unchanged. Useful as a no-op value selector (e.g. for `listToDict`). `(T -- T)`
- `2id`: Two-argument identity quote. `(T1 T2 -- T1 T2)`
- `3id`: Three-argument identity quote. `(T1 T2 T3 -- T1 T2 T3)`
- `2tuple`: Pack the top two stack values into a new two-element list, `(a b -- [a])`
- `del`: Delete element from list, `(list index -- list)` or `(index list -- list)`
- `extend`: Extends an existing list with items from another list, or a `Grid`/`GridView` with rows from another `Grid`/`GridView`. Difference between this and `+` is that it modifies the receiver in place. For grids, see the Grid section below. `(originalList toAddList -- list)` or `(Grid|GridView Grid|GridView -- Grid|GridView)`
- `insert`: Insert element into list, `(list element index -- list)`
- `setAt`: Set element at index, negative index is allowed.  `(list element index -- list)`
- `nth`: Nth element of list (0-based) `([a] int -- a)`
- `reverse`: Reverse a list, Grid, or GridView. See Sorting section. `(list -- list)` / `(Grid|GridView -- Grid)`
- `sum`: Sum of list, `([numeric] -- numeric)`
- `filter`: Filter a list or dictionary, returning a new collection. The input list or dictionary is not modified in place. For dictionaries, the quotation is applied to each value and matching entries are preserved. `([a] (a -- bool) -- [a])`, `(dict (a -- bool) -- dict)`
- `linearSearch`: Return the first element that satisfies the predicate, or `none` if nothing matches. `([a] (a -- bool) -- Maybe[a])`
- `linearSearchIndex`: Return the zero-based index of the first element that satisfies the predicate, or `none` if nothing matches. `([a] (a -- bool) -- Maybe[int])`
- `any`: Check if any element in list satisfies a condition, `([a] (a -- bool) -- bool)`
- `all`: Check if all elements in list satisfy a condition, `([a] (a -- bool) -- bool)`
- `skip`: Skip first n elements of list, or first n characters of string. `(list int -- list)` / `(str int -- str)`
- `uniq`: Remove duplicate elements from list. Works for all non-compound types. `([a] -- [a])`
- `zip`: Zip two lists together. If the two list are different lengths, resulting list will be the same length as the shorter of the two lists. `([a] [b] (a b -- c) -- [c])`
- `concat`: Flatten list of lists one level. Useful for things like a `flatMap`, which can be defined like `map concat`. `([[a]] -- [a])`
- `toSvgPathStr`: Build an SVG path `d` string from a list of `[x y]` pairs. First pair uses `M`, remaining pairs use `L`. `([[numeric]] -- str)`
- `scaleLinear`: Build a linear scaler from a domain/range pair; returns a quotation that maps input values. `([numeric] [numeric] -- (numeric -- numeric))`
- `cartesian`: Produces the Cartesian product between two lists. Output is a list of lists, in which the inner list has two elements. `([a] [a] -- [[a]])`
- `groupBy`: Groups items of a list into a dictionary based on a key function. The key function should take each item as input and produce a string.
  The output is a dictionary with the unique keys and values that are lists of the corresponding items. `([a] (a -- str) -- dict)`
- `listToDict`: Transform a list into a dictionary with a key and value selector function. `([a] (a -- b) (a -- c) -- { b: c })`
- `take`: Take the first `n` number of elements from list, or first n characters of string. `([a] int -- [a])` / `(str int -- str)`
- `repeat`: Build a list by repeating the value the requested number of times. `(a int -- [a])`
- `chunk`: Group a list into consecutive sublists of size `n`. The final chunk may be shorter if the list length isn't divisible by `n`. `([a] int -- [[a]])`
- `pop`: Pop the final element off the list. Returns a Maybe, `none` for the empty list. Leaves the modified list on the stack. `([a] -- [a] a)`

## Grid Functions

The `:name` getter and the `get` built-in accept a `Grid` or `GridView` in addition to `dict` and `GridRow`. On a grid the lookup returns the named column as `Maybe[[T]]` — the materialized column when present, `none` when absent — making `:n?` a shorthand for `"n" gridCol`. On a `GridView` the values are projected through the view's row indices.

- `select`: Project a `Grid` or `GridView` to a requested ordered list of column names, returning a materialized `Grid`. `(Grid|GridView [str] -- Grid)`
- `exclude`: Drop a list of column names from a `Grid` or `GridView`, returning a materialized `Grid`. `(Grid|GridView [str] -- Grid)`
- `derive`: Append a derived column to a `Grid` or `GridView`. The metadata dictionary is attached to the new column. `(Grid|GridView str dict (GridRow -- any) -- Grid)`
- `groupBy`: Group rows by key columns and return a summarized `Grid`. `(Grid|GridView [str]:keys [{"agg": (GridView -- any), "name"?: str, "meta"?: dict}]:aggs -- Grid)`
- `pivot`: Reshape into a pivot table. Rows are grouped by `rowKeys` (first-seen order); the distinct values of the `colKey` column become new column names, ordered by version-aware natural sort. The aggregation quotation runs once per (row-group, column-value) cell with a `GridView` of matching source rows. Empty cells are filled with `none` and the quotation is not invoked for them. The `colKey` column must contain only strings; column-value collisions with a row-key column name are an error. `(Grid|GridView [str]:rowKeys str:colKey (GridView -- any) -- Grid)`
- `updateCol`: Mutate a column in a `Grid` by applying a quotation to each cell. When used on a `GridView`, a new `Grid` is materialized from the viewed rows, the quotation is applied to that column, all result columns are retyped, and the backing `Grid` is left unchanged. The quotation must return exactly one non-container value. `(Grid|GridView str (any -- any) -- Grid)`
- `gridValues`: Extract cell values as row-major lists. The result does not include a header row and does not coerce cell types. `(Grid|GridView -- [[a]])`
- `join`: Inner equi-join of two grids using key extractor quotations on each side.
  `join` is polymorphic with the string-join built-in: when the top of the stack is a quotation, the grid form is used.
  Keys must be a non-container scalar or a flat list of scalars (treated as a tuple key).
  Quotation results that are `none` never match anything (SQL `NULL ≠ NULL` semantics).
  Output columns are all left columns followed by all right columns; the output grid carries the left grid's metadata.
  Column-name collisions on non-key columns raise an error before any work — resolve with `select`, `exclude`, or `gridRenameCol` first.
  `(Grid|GridView Grid|GridView (GridRow -- a) (GridRow -- a) -- Grid)`
- `leftJoin`: Left outer equi-join. Same shape as `join`; unmatched left rows are emitted with right-side cells filled with `none`. `(Grid|GridView Grid|GridView (GridRow -- a) (GridRow -- a) -- Grid)`
- `outerJoin`: Full outer equi-join. Same shape as `join`; unmatched rows from either side appear with the absent side filled with `none`. Affected columns fall back to generic storage. `(Grid|GridView Grid|GridView (GridRow -- a) (GridRow -- a) -- Grid)`
- `+` (Grid|GridView): Vertical concatenation. Returns a new `Grid` whose rows are the left operand's rows followed by the right operand's rows. Column matching is strict-by-name; the left grid's column order is preserved. Per-column types resolve dynamically: matching non-generic types stay; any other combination (including int+float) becomes `COL_GENERIC` — there is no numeric promotion. Grid-level and column-level metadata merge with left-wins on key conflicts. The result is a deep copy and shares no storage with the inputs. `(Grid|GridView Grid|GridView -- Grid)`
- `extend` (Grid|GridView): In-place vertical concatenation. Mutates the lower operand to include the upper operand's rows after its own and returns the same object on the stack. Column-name matching, type widening to `COL_GENERIC`, and metadata merging follow the same rules as `+`. Both operands may be `GridView`; when the receiver is a view, the underlying source grid is the storage that grows, and the view's indices extend to include the new row indices — other handles to the same source grid will observe the new rows. `(Grid|GridView Grid|GridView -- Grid|GridView)`

Grid `groupBy` aggregation specs are dictionaries.
Each spec requires an `agg` quotation and may include `name` and `meta`.
The `agg` quotation receives a `GridView` for one non-empty group and must return exactly one non-container value.
`name` defaults to `AggCol<N>`, and `meta` defaults to `{}`.
Key column metadata is copied from the source grid.
Groups are emitted in first-seen order, and key comparison is type-aware.
An empty aggregation list acts like distinct over the key columns.
An empty key list aggregates the whole table into one group when input has rows.
For empty input, `groupBy` returns the output schema with zero rows and does not evaluate aggregation quotations.

```mshell
[| region, sales; "East", 10; "West", 5; "East", 7 |]
["region"]
[
    { "name": "n", "agg": (gridRows) }
    { "name": "sales_total", "meta": { "unit": "USD" }, "agg": ("sales" gridCol sum) }
]
groupBy
```

## Sorting

- `sort`: Sort list. Converts all items to strings, then sorts using go's `sort.Strings` `(list -- list)`
- `sortV`: Version sort list. Converts all items to strings, then sorts like GNU `sort -V` (`list -- list`)
- `sortBy`: Sort a Grid or GridView by one or more columns ascending. Spec is a column name (str) or list of column names ([str]); priority is left-to-right. Stable; `none` cells sort last; cross-type values in a generic column error. Compose with `reverse` for descending. `(Grid|GridView str|[str] -- Grid)`
- `sortByCmp`: Sort a list, Grid, or GridView using a comparison function. The function/quotation receives two items (or two `GridRow`s) and should return -1 when a < b, 0 when a = b, or 1 when a > b. Stable. `[a] (a a -- int) -- [a]` / `(Grid|GridView (GridRow GridRow -- int) -- Grid)`
- `reverse`: Reverse a list, Grid, or GridView, returning a new value with elements/rows in reverse order. `(list -- list)` / `(Grid|GridView -- Grid)`
- `strCmp`: Compare two strings lexicographically using Go's [`strings.Compare`](https://pkg.go.dev/strings#Compare); returns -1, 0, or 1. Useful with `sortByCmp`. `(str str -- int)`
- `versionSortCmp`: A comparison function for use with `sortByCmp`. Used to implement "version sort" or "natural sort". `(str str -- int)`
- `floatCmp`: Compare two floats and return -1, 0, or 1. Useful with `sortByCmp` for numeric sorting. `(float float -- int)`

## Dictionary Functions

- `get`: Get value from dictionary by key. Returns a Maybe, with None representing a key not being found. Shorthand: `:key`, `:"key"`, or `:'key'`. `(dict str -- Maybe[a])`
- `getDef`: Get value from dictionary by key. Returns default value if key not found. `(dict str a -- a)`
- `set`: Set value in dictionary by key. `(dict str a -- dict)`
- `setd`: Set value in dictionary by key. Drop dict after. `(dict str a --)`
- `keys`: Get keys from dictionary. Sorted. `(dict -- [str])`
- `values`: Get values from dictionary. Sorted. `(dict -- [str])`
- `keyValues`: Get key/value pairs from dictionary as a list of `{k, v}` dictionaries.
Each pair dict has a `k` field with the key and a `v` field with the value, so the two halves can be typed independently.
Sorted by key.
Access the parts with `:k?` and `:v?`.
`(dict -- [{k: str, v: a}])`
- `map`: Map a quotation over dictionary values. Keys are preserved. `(dict (a -- b) -- dict)`
- `filter`: Filter dictionary values with a predicate quotation, returning a new dictionary. Keys are preserved for matching entries, and the original dictionary is not modified in place. `(dict (a -- bool) -- dict)`
- `in`: Check if key exists in dictionary. `(dict str -- bool)`

## Date Functions

- `toDt`: Convert string to date/time `(str -- Maybe[date])`
- `now`: Push current local date/time onto the stack `( -- date)`
- `date`: Drop the time portion from a datetime `(date -- date)`
- `year`: Get year from date `(date -- int)`
- `month`: Get month from date (1-12) `(date -- int)`
- `day`: Get day from date (1-31) `(date -- int)`
- `hour`: Get hour from date (0-23) `(date -- int)`
- `minute`: Get minute from date (0-59) `(date -- int)`
- `dateFmt`: Format a date using the [golang format string](https://pkg.go.dev/time#Layout) `(date str -- str)`. Jan 2, 2006 at 3:04pm (MST) is the reference time.
- `isoDateFmt`: Format a date using the ISO 8601 format YYYY-MM-DD `(date -- str)`
- `isoDateTimeFmt`: Format a date/time using ISO 8601 with seconds YYYY-MM-DDTHH:MM:SS `(date -- str)`
- `isWeekend`: Check if date is a weekend `(date -- bool)`
- `isWeekday`: Check if date is a weekday `(date -- bool)`
- `dow`: Get day of week from date (0-6). Sunday = 0, .., Saturday = 6 `(date -- int)`
- `toUnixTime`: Get unix time in seconds from date `(date -- int)`
- `toUnixTimeMilli`: Get unix time in milliseconds from date `(date -- int)`
- `toUnixTimeMicro`: Get unix time in microseconds from date `(date -- int)`
- `toUnixTimeNano`: Get unix time in nanoseconds from date `(date -- int)`
- `fromUnixTime`: Get date from unix time in seconds `(int -- date)`
- `fromUnixTimeMilli`: Get date from unix time in milliseconds int `(int -- date)`
- `fromUnixTimeMicro`: Get date from unix time in microseconds int `(int -- date)`
- `fromUnixTimeNano`: Get date from unix time in nanoseconds int `(int -- date)`
- `toOleDate`: Convert a date to an OLE Automation date float `(date -- float)`
- `fromOleDate`: Convert an OLE Automation date float to a date `(numeric -- date)`
- `addDays`: Add days to date `(date numeric -- date)`
- `utcToCst`: Convert a UTC datetime to US Central Time `(date -- date)`
- `cstToUtc`: Convert a US Central Time datetime to UTC `(date -- date)`

## Regular Expression Functions

All regular expression functions use the [Go regular expression syntax](https://pkg.go.dev/regexp/syntax).
See [Regexp.Expand](https://pkg.go.dev/regexp#Regexp.Expand) for replacement syntax.

- `reMatch`: Match a regular expression against a string. Returns boolean true/false. `(str:string str:re -- bool)`
- `reFindAll`: Get all matches of a regular expression. Each result row starts with the full match, followed by capture groups. `(str:string str:re -- [[str]])`
- `reFindAllIndex`: Get all match index pairs (start and end offsets) for a regular expression. Offsets are 0-based, the start offset is inclusive, and the end offset is exclusive. Each result row contains start/end pairs for the full match, followed by capture groups. `(str:string str:re -- [[int]])`
- `reReplace`: Replace all occurrences of a regular expression in a string with a replacement string. `(str:orig str:re str:replacement -- str)`
- `reSplit`: Split a string by a regular expression delimiter. `(str:string str:re -- [str])`

```mshell
"abc 123 def 456" "([a-z]+) ([0-9]+)" reFindAll str wl
# Output: [["abc 123" "abc" "123"] ["def 456" "def" "456"]]

"abc 123 def 456" "([a-z]+) ([0-9]+)" reFindAllIndex str wl
# Output: [[0 7 0 3 4 7] [8 15 8 11 12 15]]
```

## Paths

- `dirname`: Get directory name from path `(path -- path)`
- `basename`: Get base name (aka file name or not directory portion) from path `(path -- path)`
- `ext`: Get extension from path, includes period. `(path -- path)`
- `stem`: Get path without the final extension `(path -- path)`

## Shell Utilities

- `mkdir`: Make directory `(str -- )`
- `mkdirp`: Make directory and required parents `(str -- )`

## Maybe

- `isNone`: Check if Maybe is None. `(Maybe[a] -- bool)`
- `just`: Wrap value in Maybe. `(a -- Maybe[a])`
- `none`: Create a None Maybe. `( -- Maybe[a])`
- `?`: If Maybe is None, fail immediately. If it is Just, unwrap and continue. `(Maybe[a] -- a)`
- `maybe`: Unwrap a Maybe, returning a default value if it is None. `(Maybe[a] a -- a)`
- `bind`: This is a monadic bind operation. Allows for chaining operations on Maybe values with functions that themselves return Maybe values. `(Maybe[a] (a -- Maybe[b]) -- Maybe[b])`
- `map`: Map a function over a Maybe value. If the Maybe is None, it returns None. If it is Just, it applies the function to the value. `(Maybe[a] (a -- b) -- Maybe[b])`

## HTML

- `parseHtml`: Parse HTML from string or file. Returns a dictionary of node data. The dictionaries have keys `tag`, `attr`, `children`, and `text`. `(str | path -- dict)`
- `htmlDescendents`: Get all descendants of a node. Returns a list of dictionaries with the same keys as `parseHtml`. Includes the starting node.  `(dict -- [dict])`
- `findByTag`: Find all nodes with a given tag name. `(dict str -- [dict])`

## HTTP Requests

- `httpGet`: Make a HTTP GET request. Signature is `({str: T} -- Maybe[{status: int, reason: str, headers: {str: [str]}, body: bytes}])`. Takes the request information in a dictionary that should have the following keys:

  - `url`: Full URL, including all the query parameters (required, stringable)
  - `timeout`: Request timeout in seconds (optional, positive integer; default 30)
  - `headers`: A dictionary of key-value pairs for the request headers (optional)

  Returns a Maybe wrapping a response dictionary.
  The response is `none` if the web request totally fails, like hitting a timeout.
  Otherwise a Just Dictionary is returned, with fields

  - `status`: Integer status code
  - `reason`: Full reason line, ex: `"200 OK"`
  - `headers`: Dictionary of header name to a list of values
  - `body`: Body of response, as raw `bytes`. Decode with `utf8Str` if you want a UTF-8 string.

- `httpPost`: Make a HTTP POST request. Signature is the same as `httpGet`. The only difference is that on the request dictionary, you can also set the `body` field to a stringable value.

## Variables

You can store to several variables in one go by separating the store tokens with commas. Values are consumed from the stack for each store, and an optional trailing comma is ignored.

```mshell
# Storing
10 my_var!
# Retrieving
@my_var

# Storing multiple values at once
1 2 3 a!, b!, c!  # a is 1, b is 2, c is 3.
@a @b @c

# A trailing comma after the last store is ignored
4 5 a!, b!,
```


## Object Types

The current object types supported by `mshell` are:

1. Literal
2. Bool
3. Quotation
4. List
5. String
6. Pipe
7. Integer
8. Path
9. Date/Times
10. Dictionary
11. Maybe

## LLM Notes

- `mshell` is concatenative and stack-based: words run left-to-right, consuming values from the stack and pushing results.
- External commands are lists of strings; execute them with `;` (always continue), `!` (stop on non-zero), or `?` (push exit code).
- Comments use `#` and run to the end of the line.
- Paths are backticked literals with no escaping; use string interpolation with `toPath` when you need escapes.
- Quotations are `(...)` blocks; execute a quotation with `x`.
