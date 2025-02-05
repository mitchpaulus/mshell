## Data Types

### `str` String

Double quoted strings have the following escape sequences:

- `\n`: Newline
- `\t`: Tab
- `\r`: Carriage return
- `\"`: Double quote
- `\\`: Tab

No escaping is done within single quoted strings.

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
- `w`: Write (str -- )
- `wl`: Write line (str -- )
- `we`: Write error (str -- )
- `wle`: Write error line (str -- )
- `len`: Length of string/list `([a] -- int | str -- int)`
- `args`: List of string arguments `( -- [str])`
- `nth`: Nth element of list (0-based) `([a] int -- a)`
- `glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack. `(str -- [str])`
- `x`: Interpret/execute quotation `(quote -- )`
- `cd`: Change directory `(str -- )`
- `pwd`: Get current working directory `( -- str)`
- `toFloat`: Convert to float. `(numeric -- float)`
- `toInt`: Convert to int. `(numeric -- int)`
- `readFile`: Read file into string. `(str -- str)`
- `readTsvFile`: Read a TSV file into list of list of strings. `(str -- [[str]])`
- `stdin`: Drop stdin onto the stack `( -- str)`
- `::`: Drop stdin onto the stack and split by lines `( -- [str])`. This is a shorthand for `stdin lines`.
- `foldl`: Fold left. `(quote initial list -- result)`
- `wt`: "Whitespace table", puts stdin split by lines and whitespace on the stack. `( -- [[str]])`
- `tt`: "Tab table", puts stdin split by lines and tabs on the stack. `( -- [[str]])`
- `ttFile`: "Tab table" from file, puts content from file name split by lines and tabs on the stack. `(str -- [[str]])`

## Math Functions

- `abs`: Absolute value `(numeric -- numeric)`
- `max2`: Maximum of two numbers `(numeric numeric -- numeric)`
- `max`: Maximum of list of numbers `([numeric] -- numeric)`
- `transpose`: Transpose list of lists `([[a]] -- [[a]])`
- `min2`: Minimum of two numbers `(numeric numeric -- numeric)`
- `min`: Minimum of list of numbers `([numeric] -- numeric)`

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

## Variables

```mshell
# Storing
10 my_var!
# Retrieving
@my_var
```

## Process Substitution

With a list on the stack, the following operators will leave output content on the stack after process execution:

```mshell
o: [str], Standard output, split by lines
oc: str, Standard output, complete untouched
os: str: Standard output, stripped
e: [str], Standard error, split by lines
ec: str, Standard error, complete untouched
es: str, Standard error, stripped
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
