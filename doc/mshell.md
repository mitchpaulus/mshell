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

Dates can be subtracted from each other, and the result is a float number of days.

```mshell
2023-10-02 2023-10-01 - # 1.0
```

## Built-ins

- `.s`: Print stack at current location (--)
- `.b`: Prints paths to all know binaries (--)
- `.def`: Print available definitions at current location (--)
- `dup`: Duplicate (a -- a a)
- `swap`: Swap (a b -- b a)
- `drop`: Drop (a -- )
- `over`: Over, copy second element to top `(a b -- a b a)`
- `pick`: Pick, copy nth element to top, `(a b c int pick` -- `a b c [a | b | c])`
- `rot`: Rotate the top three items, `( a b c -- b c a )`
- `nip`: Remove second item, `( a b -- b )`
- `w`: Write to stdout (str -- )
- `wl`: Write line to stdout (str -- )
- `we`: Write error to stderr (str -- )
- `wle`: Write error line stderr (str -- )
- `len`: Length of string/list `([a] -- int | str -- int)`
- `args`: List of string arguments. Does not include the name of the executing file. `( -- [str])`
- `glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack. Relies on golang's [filepath.Glob](https://pkg.go.dev/path/filepath#Glob), which in the current implementation, the response is sorted. `(str -- [str])`
- `x`: Interpret/execute quotation `(quote -- )`
- `toFloat`: Convert to float. `(numeric -- Maybe[float])`
- `toInt`: Convert to int. `(numeric -- Mabye[int])`
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
- `parseCsv`: Parse a CSV file into a list of lists of strings. Input can be a path/literal file name, or the string contents itself. (`path|str -- [[str]])`
- `seq`: Generate a list of integers, starting from 0. Exclusive end to integer on stack. `2 seq` produces `[0 1]`. `(int -- [int])`


## File/Directory Functions

- `toPath`: Convert to path. `(str -- path)`
- `isDir`: Check if path is a directory. `(path -- bool)`
- `isFile`: Check if path is a file. `(path -- bool)`
- `hardLink`: Create a hard link. `(existingSourcePath newTargetPath -- )`
- `tempFile`: Create a temporary file, and put the full path on the stack. `( -- str)`
- `tempDir`: Return path to the OS specific temporary directory. No checks on permission or existence, so never fails. See [`os.TempDir`](https://pkg.go.dev/os#TempDir) in golang. `$TMPDIR` or `/tmp` for Unix, `%TMP%`, `%TEMP%, %USERPROFILE%`, or the Windows directory (`C:\Windows`). `( -- str)`
- `rm`: Remove file or directory. `(str -- )`
- `cp`: Copy file or directory. `(str:source str:dest -- )`
- `mv`: Move file or directory. `(str:source str:dest -- )`
- `readFile`: Read file into string. `(str -- str)`
- `readTsvFile`: Read a TSV file into list of list of strings. `(str -- [[str]])`
- `cd`: Change directory `(str -- )`
- `pwd`: Get current working directory `( -- str)`
- `writeFile`: Write string to file (UTF-8). `(str content str file -- )`
- `appendFile`: Append string to file (UTF-8). `(str content str file -- )`
- `fileSize`: Get size of file in bytes. `(str -- int)`
- `lsDir`: Get list of all items (files and directories) in directory. Full paths to the items. `(str -- [str])`
- `sha256sum`: Get SHA256 checksum of file. `(path -- str)`
- `files`: Get list of files in current directory. Not recursive. `(str -- [str])`
- `dirs`: Get list of directories in current directory. Not recursive. `(str -- [str])`
- `isCmd`: Check whether item is a command that can be found in PATH. `(str -- bool)`

## Math Functions

- `abs`: Absolute value `(numeric -- numeric)`
- `max2`: Maximum of two numbers `(numeric numeric -- numeric)`
- `max`: Maximum of list of numbers `([numeric] -- numeric)`
- `transpose`: Transpose list of lists `([[a]] -- [[a]])`
- `min2`: Minimum of two numbers `(numeric numeric -- numeric)`
- `min`: Minimum of list of numbers `([numeric] -- numeric)`
- `mod`: Modulus `(numeric numeric -- numeric)`
- `round`: Round to nearest integer. Rounds half-way away from zero. `(numeric -- int)`

## String Functions

- `str`: Convert to string
- `findReplace`: Find and replace in string. `findReplace (str str, str find, str replace -- str)`
- `lines`: Split string into list of string lines
- `split`: Split string into list of strings by delimiter. (str delimiter -- [str])
- `wsplit`: Split string into list of strings by runs of whitespace. (str -- [str])
- `join`: Join list of strings into a single string, (list delimiter -- str)
- `in`: Check for substring in string. (totalString subString -- bool)
- `index`: Get index of first occurrence of substring in string. Returns Maybe[int] with None for the substring not being found. `(str str -- Maybe[int])`
- `tab`: Puts a tab character on the stack `( -- str)`
- `tsplit`: Split string into list of strings by tabs. `(str -- [str])`
- `trim`: Trim whitespace from string. `(str -- str)`
- `trimStart`: Trim whitespace from start of string. `(str -- str)`
- `trimEnd`: Trim whitespace from end of string. `(str -- str)`
- `startsWith`: Check if string starts with substring. `(str str -- bool)`
- `endsWith`: Check if string ends with substring. `(str str -- bool)`
- `lower`: Convert string to lowercase. `(str -- str)`
- `upper`: Convert string to uppercase. `(str -- str)`
- `toFixed`: Convert number to string with fixed number of decimal places. `(numeric int -- str)`
- `countSubStr`: Count occurrences of substring in string. `(str str -- int)`

## List Functions

- `append`: Append, `([a] a -- [a])`
- `map`: Map a quotation over a list, `([a] (a -- b) -- [b])`
- `each`: Execute a quotation for each element in a list, `([a] (a -- ) -- )`
- `del`: Delete element from list, `(list index -- list)` or `(index list -- list)`
- `extend`: Extends an existing list with items from another list. Difference between this and `+` is that it modifies the original list in-place. `(originalList toAddList -- list)`
- `insert`: Insert element into list, `(list element index -- list)`
- `setAt`: Set element at index, `(list element index -- list)`
- `nth`: Nth element of list (0-based) `([a] int -- a)`
- `reverse`: Reverse list, `(list -- list)`
- `sum`: Sum of list, `([numeric] -- numeric)`
- `filter`: Filter list, `(list quote -- list)`
- `any`: Check if any element in list satisfies a condition, `([a] (a -- bool) -- bool)`
- `all`: Check if all elements in list satisfy a condition, `([a] (a -- bool) -- bool)`
- `skip`: Skip first n elements of list, `(list int -- list)`
- `sort`: Sort list. Converts all items to strings, then sorts using go's `sort.Strings` `(list -- list)`
- `sortV`: Version sort list. Converts all items to strings, then sorts like GNU `sort -V` (`list -- list`)
- `uniq`: Remove duplicate elements from list. Works for all non-compound types. `([a] -- [a])`
- `zip`: Zip two lists together. If the two list are different lengths, resulting list will be the same length as the shorter of the two lists. `([a] [b] (a b -- c) -- [c])`

## Dictionary Functions

- `get`: Get value from dictionary by key. Returns a Maybe, with None representing a key not being found. `(dict str -- Maybe[a])`
- `getDef`: Get value from dictionary by key. Returns default value if key not found. `(dict str a -- a)`
- `set`: Set value in dictionary by key. `(dict str a -- dict)`
- `setd`: Set value in dictionary by key. Drop dict after. `(dict str a --)`
- `keys`: Get keys from dictionary. Sorted. `(dict -- [str])`
- `values`: Get values from dictionary. Sorted. `(dict -- [str])`
- `keyValues`: Get key/value pairs from dictionary as a list of lists. Each inner list is a two-element list with the key and value. Sorted by key. `(dict -- [[str a]])`
- `in`: Check if key exists in dictionary. `(dict str -- bool)`

## Date Functions

- `toDt`: Convert string to date/time `(str -- Maybe[date])`
- `date`: Push current local date/time onto the stack `( -- date)`
- `year`: Get year from date `(date -- int)`
- `month`: Get month from date (1-12) `(date -- int)`
- `day`: Get day from date (1-31) `(date -- int)`
- `hour`: Get hour from date (0-23) `(date -- int)`
- `minute`: Get minute from date (0-59) `(date -- int)`
- `dateFmt`: Format a date using the [golang format string](https://pkg.go.dev/time#Layout) `(date str -- str)`. Jan 2, 2006 at 3:04pm (MST) is the reference time.
- `isoDateFmt`: Format a date using the ISO 8601 format YYYY-MM-DD `(date -- str)`
- `isWeekend`: Check if date is a weekend `(date -- bool)`
- `isWeekday`: Check if date is a weekday `(date -- bool)`
- `dow`: Get day of week from date (0-6). Sunday = 0, .., Saturday = 6 `(date -- int)`
- `toUnixTime`: Get unix time from date `(date -- int)`
- `fromUnixTime`: Get date from unix time int `(date -- int)`
- `addDays`: Add days to date `(date numeric -- date)`

## Regular Expression Functions

All regular expression functions use the [Go regular expression syntax](https://pkg.go.dev/regexp/syntax).
See [Regexp.Expand](https://pkg.go.dev/regexp#Regexp.Expand) for replacement syntax.

- `reMatch`: Match a regular expression against a string. Returns boolean true/false. `(str re -- bool)`
- `reReplace`: Replace all occurrences of a regular expression in a string with a replacement string. `(str:orig re str:replacement -- str)`

## Paths

- `dirname`: Get directory name from path `(path -- path)`
- `basename`: Get base name (aka file name or not directory portion) from path `(path -- path)`
- `ext`: Get extension from path, includes period. `(path -- path)`

## Shell Utilities

- `mkdir`: Make directory `(str -- )`
- `mkdirp`: Make directory and required parents `(str -- )`

## Maybe

- `isNone`: Check if Maybe is None. `(Maybe[a] -- bool)`
- `just`: Wrap value in Maybe. `(a -- Maybe[a])`
- `none`: Create a None Maybe. `( -- Maybe[a])`
- `?`: If Maybe is None, fail immediately. If it is Just, unwrap and continue. `(Maybe[a] -- a)`

## HTML

- `parseHtml`: Parse HTML from string or file. Returns a dictionary of node data. The dictionaries have keys `tag`, `attr`, `children`, and `text`. `(str | path -- dict)`
- `htmlDescendents`: Get all descendants of a node. Returns a list of dictionaries with the same keys as `parseHtml`. Includes the starting node.  `(dict -- [dict])`

## Variables

```mshell
# Storing
10 my_var!
# Retrieving
@my_var
```

## Command Substitution

With a list on the stack, the following operators will leave output content on the stack after process execution:

```mshell
o: [str], Standard output, split by lines
oc: str, Standard output, complete untouched
os: str: Standard output, stripped
e: [str], Standard error, split by lines
ec: str, Standard error, complete untouched
es: str, Standard error, stripped
```

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

# Checking for variable existance
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
