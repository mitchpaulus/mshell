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
    ['-h' '--help' '--html' '--lex' '--parse' '--version' '-c'] options!
    ['lsp' 'bin' 'completions'] subcommands!
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

mshell supports a simple bin map file that overrides PATH lookups. The file lives alongside the history files (e.g. `~/.local/share/msh/msh_bins.txt` on Linux/macOS or `%LOCALAPPDATA%\mshell\msh_bins.txt` on Windows).

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
- `msh bin edit`: edit the bin map file in `$EDITOR`
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

## Built-ins

- `.s`: Print stack at current location (--)
- `.b`: Prints paths to all known binaries (--)
- `.def`: Print available definitions at current location (--)
- `.env`: Print all environment variables to stderr in sorted order (--)
- `dup`: Duplicate (a -- a a)
- `swap`: Swap (a b -- b a)
- `drop`: Drop (a -- )
- `over`: Over, copy second element to top `(a b -- a b a)`
- `pick`: Pick, copy nth element to top, `(a b c int pick` -- `a b c [a | b | c])`
- `rot`: Rotate the top three items, `( a b c -- b c a )`
- `-rot`: Rotate the top three items in the opposite direction `( a b c -- c a b )`
- `nip`: Remove second item, `( a b -- b )`
- `w`: Write to stdout (str|binary -- )
- `wl`: Write line to stdout (str -- )
- `we`: Write error to stderr (str|binary -- )
- `wle`: Write error line stderr (str -- )
- `len`: Length of string/list `([a] -- int | str -- int)`
- `args`: List of string arguments. Does not include the name of the executing file. `( -- [str])`
- `glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack. Relies on golang's [filepath.Glob](https://pkg.go.dev/path/filepath#Glob), which in the current implementation, the response is sorted. `(str -- [str])`
- `/`: Divide numbers or join paths. `(numeric numeric -- numeric)` treats the top of stack as divisor. `(path path -- path)` joins the paths using the OS separator.
- `x`: Interpret/execute quotation `(quote -- )`
- `toFloat`: Convert to float. `(numeric -- Maybe[float])`
- `toInt`: Convert to int. `(numeric -- Mabye[int])`
- `exit`: Exit the current script with the provided exit code. `(int -- )`
- `read`: Read a line from stdin. Puts a str and bool of whether the read was successful on the stack. `( -- str bool)`
- `stdin`: Drop stdin onto the stack `( -- str)`
- `::`: Drop stdin onto the stack and split by lines `( -- [str])`. This is a shorthand for `stdin lines`.
- `foldl`: Fold left. `(quote initial list -- result)`
- `wt`: "Whitespace table", puts stdin split by lines and whitespace on the stack. `( -- [[str]])`
- `tt`: "Tab table", puts stdin split by lines and tabs on the stack. `( -- [[str]])`
- `ttFile`: "Tab table" from file, puts content from file name split by lines and tabs on the stack. `(str -- [[str]])`
- `uw`: Shorthand for `unlines w` `([str] -- )`
- `tuw`: Shorthand for `(tjoin) map uw` `([[str]] -- )`
- `runtime`: Get the current OS runtime. This is the output of the GOOS environment variable. Common possible values are `linux`, `windows`, and `darwin`. `( -- str)`
- `hostname`: Get the current OS hostname. On failure to get, puts 'unknown' on the stack. `( -- str)`
- `parseCsv`: Parse a CSV file into a list of lists of strings. Input can be a path/literal file name, or the string contents itself. (`path|str -- [[str]])`
- `parseJson`: Parse JSON from a string, binary, or file path into mshell objects. (`path|str|binary -- list|dict|numeric|str|bool`)
- `seq`: Generate a list of integers, starting from 0. Exclusive end to integer on stack. `2 seq` produces `[0 1]`. `(int -- [int])`
- `repeat`: Create a list containing the provided value repeated `n` times. `(a int -- [a])`
- `binPaths`: Puts a list of lists with 2 items, first is the executable name, second is the full path to the executable. `(-- [[str]])`
- `urlEncode`: URL-encode a string or dictionary of parameters. `(str|dict -- str)`
- `type`: Return the type name of the top stack item `(a -- str)`


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
- `max`: Maximum of list of numbers `([numeric] -- numeric)`
- `transpose`: Transpose list of lists `([[a]] -- [[a]])`
- `min2`: Minimum of two numbers `(numeric numeric -- numeric)`
- `min`: Minimum of list of numbers `([numeric] -- numeric)`
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
- `take`: Take first n characters from string. `(str int -- str)`
- `base64encode`: Encode binary data as base64. `(binary -- str)`
- `base64decode`: Decode base64 string into binary data. `(str -- binary)`
- `utf8Str`: Decode UTF-8 bytes into a string. `(binary -- str)`
- `utf8Bytes`: Encode a string as UTF-8 bytes. `(str -- binary)`

## List Functions

- `append`: Append, `([a] a -- [a])`
- `map`: Map a quotation over a list, `([a] (a -- b) -- [b])`
- `enumerate`: Pair each element with its zero-based index, returning `[a, int]` pairs. `([a] -- [[a int]])`
- `enumerateN`: Pair elements with indices starting from the supplied offset, returning `[a, int]` pairs. `([a] int -- [[a int]])`
- `each`: Execute a quotation for each element in a list, `([a] (a -- ) -- )`
- `eachWhile`: Execute a quotation for each element in a list, stopping when a false is left on the stack `([a] (a -- bool) -- )`
- `takeWhile`: Return the leading elements of a list while the predicate remains true. `([a] (a -- bool) -- [a])`
- `dropWhile`: Drop leading elements while the predicate remains true. `([a] (a -- bool) -- [a])`
- `2unpack`: Unpack a two-element list onto the stack. `([a] -- a a)`
- `2apply`: Apply a binary quotation to a two-element list. `([a] (a a -- c) -- c)`
- `2each`: Apply a quotation to the two values on the stack individually, returning results in the original order. `(a b (a -- c) -- c c)`
- `2tuple`: Pack the top two stack values into a new two-element list, `(a b -- [a])`
- `del`: Delete element from list, `(list index -- list)` or `(index list -- list)`
- `extend`: Extends an existing list with items from another list. Difference between this and `+` is that it modifies the original list in-place. `(originalList toAddList -- list)`
- `insert`: Insert element into list, `(list element index -- list)`
- `setAt`: Set element at index, `(list element index -- list)`
- `nth`: Nth element of list (0-based) `([a] int -- a)`
- `reverse`: Reverse list, `(list -- list)`
- `sum`: Sum of list, `([numeric] -- numeric)`
- `filter`: Filter list, `([a] (a -- bool) -- [a])`
- `any`: Check if any element in list satisfies a condition, `([a] (a -- bool) -- bool)`
- `all`: Check if all elements in list satisfy a condition, `([a] (a -- bool) -- bool)`
- `skip`: Skip first n elements of list, `(list int -- list)`
- `uniq`: Remove duplicate elements from list. Works for all non-compound types. `([a] -- [a])`
- `zip`: Zip two lists together. If the two list are different lengths, resulting list will be the same length as the shorter of the two lists. `([a] [b] (a b -- c) -- [c])`
- `concat`: Flatten list of lists one level. Useful for things like a `flatMap`, which can be defined like `map concat`. `([[a]] -- [a])`
- `cartesian`: Produces the Cartesian product between two lists. Output is a list of lists, in which the inner list has two elements. `([a] [a] -- [[a]])`
- `groupBy`: Groups items of a list into a dictionary based on a key function. The key function should take each item as input and produce a string.
             The output is a dictionary with the unique keys and values that are lists of the corresponding items.
             `[a] (a -- str) -- dict [a])`
- `listToDict`: Transform a list into a dictionary with a key and value selector function. `([a] (a -- b) (a -- c) -- { b: c })`
- `take`: Take the first `n` number of elements from list. `([a] int -- [a])`
- `repeat`: Build a list by repeating the value the requested number of times. `(a int -- [a])`
- `chunk`: Group a list into consecutive sublists of size `n`. The final chunk may be shorter if the list length isn't divisible by `n`. `([a] int -- [[a]])`
- `pop`: Pop the final element off the list. Returns a Maybe, `none` for the empty list. Leaves the modified list on the stack. `([a] -- [a] a)`

## Sorting

- `sort`: Sort list. Converts all items to strings, then sorts using go's `sort.Strings` `(list -- list)`
- `sortV`: Version sort list. Converts all items to strings, then sorts like GNU `sort -V` (`list -- list`)
- `sortByCmp`: Sort a list by a comparison function. The function/quotation should return -1 when a < b, 0 when a = b, or 1 when a > b. `[a] (a a -- int) -- [a]`
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
- `keyValues`: Get key/value pairs from dictionary as a list of lists. Each inner list is a two-element list with the key and value. Sorted by key. `(dict -- [[str a]])`
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
- `toUnixTime`: Get unix time from date `(date -- int)`
- `fromUnixTime`: Get date from unix time int `(date -- int)`
- `toOleDate`: Convert a date to an OLE Automation date float `(date -- float)`
- `fromOleDate`: Convert an OLE Automation date float to a date `(numeric -- date)`
- `addDays`: Add days to date `(date numeric -- date)`
- `utcToCst`: Convert a UTC datetime to US Central Time `(date -- date)`
- `cstToUtc`: Convert a US Central Time datetime to UTC `(date -- date)`

## Regular Expression Functions

All regular expression functions use the [Go regular expression syntax](https://pkg.go.dev/regexp/syntax).
See [Regexp.Expand](https://pkg.go.dev/regexp#Regexp.Expand) for replacement syntax.

- `reMatch`: Match a regular expression against a string. Returns boolean true/false. `(str re -- bool)`
- `reFindAll`: Get all the matches of a regular expression. `(str re -- [[ str ]])`
- `reFindAllIndex`: Get all match index pairs (start and end offsets) for a regular expression, including capture groups. `(str re -- [[int]])`
- `reReplace`: Replace all occurrences of a regular expression in a string with a replacement string. `(str:orig re str:replacement -- str)`
- `reSplit`: Split a string by a regular expression delimiter. `(str re -- [str])`

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

- `httpGet`: Make a HTTP GET request. Signature is `(dict -- dict)`. Takes the request information in a dictionary that should have the following keys:

  - `url`: Full URL, including all the query parameters
  - `headers`: A dictionary of key-value pairs for the request headers

  Returns a Maybe wrapping a response dictionary.
  The response is `none` is the web request totally fails, like hitting a timeout.
  Otherwise a Just Dictionary is returned, with fields

  - `status`: Integer status code
  - `reason`: Full reason line, ex: `"200 OK"`
  - `headers`: Dictionary of key-value header pairs
  - `body`: Body of response, read as UTF-8 string.

- `httpPost`: Make a HTTP POST request. Signature is `(dict -- dict)`. Only difference from `httpGet` is that on the request dictionary, you can also set the `body` field to a string.

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
`?`      | Execute and push the exit code.            | Integer is left on the stack.
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

The bin map file lives alongside the history files (for example, `~/.local/share/msh/msh_bins.txt` on Linux/macOS or `%LOCALAPPDATA%\\mshell\\msh_bins.txt` on Windows). Each non-empty line is a tab-separated pair of fields: the binary name and the absolute path to the binary. Both fields are trimmed, the name must not contain path separators, and the line must contain exactly two fields.

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
- `msh bin edit`: edit the file in `$EDITOR`
- `msh bin audit`: report invalid or missing entries
- `msh bin debug <name>`: show lookup details for a binary

## Process Substitution

Process substitution is done using the `psub` operator.

```mshell
[my_command_needing_file "my test" psub];
```

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
```

For non-fixed indexing, you have the `nth` operator.

```mshell
[ 4 3 2 1 ] 2 nth # 2
```

## Error Handling

By default, executing a process that returns with a non-zero exit code does not stop the execution of the script.
If the desired behavior is to stop the execution on any non-zero exit code, the keyword `soe` can be used.

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
