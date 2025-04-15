## Data Types

### `str` Strings

Double quoted strings have the following escape sequences:

- `\n`: Newline
- `\t`: Tab
- `\r`: Carriage return
- `\"`: Double quote
- `\\`: Tab

No escaping is done within single quoted strings.

### `bool` Booleans

Booleans are represented by `true` and `false`.

### `quote` Quotations

Quotations are blocks of code that can be executed. They are created by wrapping code in parentheses.

```mshell
(1 2 +)
```

### `list` Lists

Lists are created by wrapping elements in square brackets.
No commas are required between elements.

```mshell
[1 2 3]
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
- `.def`: Print available definitions at current location (--)
- `dup`: Duplicate (a -- a a)
- `swap`: Swap (a b -- b a)
- `drop`: Drop (a -- )
- `append`: Append, `([a] a -- [a])`
- `over`: Over, copy second element to top `(a b -- a b a)`
- `pick`: Pick, copy nth element to top, `(a b c int pick` -- `a b c [a | b | c])`
- `rot`: Rotate the top three items, `( a b c -- b c a )`
- `nip`: Remove second item, `( a b -- b )`
- `w`: Write to stdout (str -- )
- `wl`: Write line to stdout (str -- )
- `we`: Write error to stderr (str -- )
- `wle`: Write error line stderr (str -- )
- `len`: Length of string/list `([a] -- int | str -- int)`
- `args`: List of string arguments `( -- [str])`
- `nth`: Nth element of list (0-based) `([a] int -- a)`
- `glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack. `(str -- [str])`
- `x`: Interpret/execute quotation `(quote -- )`
- `cd`: Change directory `(str -- )`
- `pwd`: Get current working directory `( -- str)`
- `toFloat`: Convert to float. `(numeric -- float)`
- `toInt`: Convert to int. `(numeric -- int)`
- `toPath`: Convert to path. `(str -- path)`
- `readFile`: Read file into string. `(str -- str)`
- `readTsvFile`: Read a TSV file into list of list of strings. `(str -- [[str]])`
- `read`: Read a line from stdin. Puts a str and bool of whether the read was successful on the stack. `( -- str bool)`
- `stdin`: Drop stdin onto the stack `( -- str)`
- `::`: Drop stdin onto the stack and split by lines `( -- [str])`. This is a shorthand for `stdin lines`.
- `foldl`: Fold left. `(quote initial list -- result)`
- `wt`: "Whitespace table", puts stdin split by lines and whitespace on the stack. `( -- [[str]])`
- `tt`: "Tab table", puts stdin split by lines and tabs on the stack. `( -- [[str]])`
- `ttFile`: "Tab table" from file, puts content from file name split by lines and tabs on the stack. `(str -- [[str]])`
- `isDir`: Check if path is a directory. `(path -- bool)`
- `isFile`: Check if path is a file. `(path -- bool)`
- `hardLink`: Create a hard link. `(existingSourcePath newTargetPath -- )`
- `tempFile`: Create a temporary file, and put the full path on the stack. `( -- str)`
- `tempDir`: Return path to the OS specific temporary directory. No checks on permission or existence, so never fails.`( -- str)`
- `uw`: Shorthand for `unlines w` `([str] -- )`
- `tuw`: Shorthand for `(tjoin) map uw` `([[str]] -- )`
- `writeFile`: Write string to file (UTF-8). `(str content str file -- )`
- `appendFile`: Append string to file (UTF-8). `(str content str file -- )`

## Math Functions

- `abs`: Absolute value `(numeric -- numeric)`
- `max2`: Maximum of two numbers `(numeric numeric -- numeric)`
- `max`: Maximum of list of numbers `([numeric] -- numeric)`
- `transpose`: Transpose list of lists `([[a]] -- [[a]])`
- `min2`: Minimum of two numbers `(numeric numeric -- numeric)`
- `min`: Minimum of list of numbers `([numeric] -- numeric)`
- `mod`: Modulus `(numeric numeric -- numeric)`

### String Functions

- `str`: Convert to string
- `findReplace`: Find and replace in string. `findReplace (str str, str find, str replace -- str)`
- `lines`: Split string into list of string lines
- `split`: Split string into list of strings by delimiter. (str delimiter -- [str])
- `wsplit`: Split string into list of strings by runs of whitespace. (str -- [str])
- `join`: Join list of strings into a single string, (list delimiter -- str)
- `in`: Check for substring in string. (totalString subString -- bool)
- `tab`: Puts a tab character on the stack `( -- str)`
- `tsplit`: Split string into list of strings by tabs. `(str -- [str])`
- `trim`: Trim whitespace from string. `(str -- str)`
- `trimStart`: Trim whitespace from start of string. `(str -- str)`
- `trimEnd`: Trim whitespace from end of string. `(str -- str)`
- `startsWith`: Check if string starts with substring. `(str str -- bool)`
- `endsWith`: Check if string ends with substring. `(str str -- bool)`

### List Functions

- `map`: Map a quotation over a list, `([a] (a -- b) -- [b])`
- `each`: Execute a quotation for each element in a list, `([a] (a -- ) -- )`
- `del`: Delete element from list, `(list index -- list)` or `(index list -- list)`
- `insert`: Insert element into list, `(list element index -- list)`
- `setAt`: Set element at index, `(list element index -- list)`
- `reverse`: Reverse list, `(list -- list)`
- `sum`: Sum of list, `([numeric] -- numeric)`
- `filter`: Filter list, `(list quote -- list)`
- `any`: Check if any element in list satisfies a condition, `([a] (a -- bool) -- bool)`
- `all`: Check if all elements in list satisfy a condition, `([a] (a -- bool) -- bool)`

## Date Functions

- `toDt`: Convert to date/time `(str -- date)`
- `date`: Push current date/time onto the stack `( -- date)`
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
- `unixTime`: Get unix time from date `(date -- int)`

## Paths

- `dirname`: Get directory name from path `(path -- path)`
- `basename`: Get base name from path `(path -- path)`
- `ext`: Get extension from path `(path -- path)`

## Shell Utilities

- `mkdir`: Make directory `(str -- )`
- `mkdirp`: Make directory and required parents `(str -- )`

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
