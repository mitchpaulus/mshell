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

- `.s`: Print stack at current location
- `.def`: Print available definitions at current location
- `dup`: Duplicate
- `swap`: Swap
- `drop`: Drop
- `append`: Append, `(list item -- list)`
- `over`: Over, copy second element to top
- `pick`: Pick, copy nth element to top, `(a b c n pick` -- `a b c [a | b | c])`
- `rot`: Rotate the top three items, `( a b c -- b c a )`
- `nip`: Remove second item, `( a b -- b )`
- `w`: Write
- `wl`: Write line
- `we`: Write error
- `wle`: Write error line
- `len`: Length of string/list
- `args`: List of string arguments
- `nth`: Nth element of list (0-based)
- `glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack.
- `x`: Interpret/execute quotation
- `cd`: Change directory `(string -- )`
- `toFloat`: Convert to float. `(numeric -- float)`
- `readFile`: Read file into string. `(string -- string)`
- `readTsvFile`: Read a TSV file into list of list of strings. `(string -- list[list[string]])`
- `stdin`: Drop stdin onto the stack `( -- string)`
- `..`: Drop stdin onto the stack and split by lines `( -- list[string])`
- `foldl`: Fold left. `(quote initial list -- result)`
- `wt`: "Whitespace table", puts stdin split by lines and whitespace on the stack. `( -- list[list[string]])`
- `tt`: "Tab table", puts stdin split by lines and tabs on the stack. `( -- list[list[string]])`

## Math Functions

- `abs`: Absolute value `(numeric -- numeric)`
- `max2`: Maximum of two numbers `(numeric numeric -- numeric)`
- `max`: Maximum of list of numbers `([numeric] -- numeric)`
- `transpose`: Transpose list of lists `([[a]] -- [[a]])`
- `min2`: Minimum of two numbers `(numeric numeric -- numeric)`
- `min`: Minimum of list of numbers `([numeric] -- numeric)`

### String Functions

- `str`: Convert to string
- `findReplace`: Find and replace in string. `findReplace (string string, string find, string replace -- string)`
- `lines`: Split string into list of string lines
- `split`: Split string into list of strings by delimiter. (string delimiter -- list[string])
- `wsplit`: Split string into list of strings by runs of whitespace. (string -- list[string])
- `join`: Join list of strings into a single string, (list delimiter -- string)
- `in`: Check for substring in string. (totalString subString -- bool)
- `tab`: Puts a tab character on the stack `( -- string)`
- `tsplit`: Split string into list of strings by tabs. `(string -- list[string])`

### List Functions

- `map`: Map a quotation over a list, `(list quote -- list)`
- `each`: Execute a quotation for each element in a list, `(list quote -- )`
- `del`: Delete element from list, `(list index -- list)` or `(index list -- list)`
- `insert`: Insert element into list, `(list element index -- list)`
- `setAt`: Set element at index, `(list element index -- list)`
- `reverse`: Reverse list, `(list -- list)`
- `sum`: Sum of list, `(list[numeric] -- numeric)`
- `filter`: Filter list, `(list quote -- list)`

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
o: List[string], Stadard output, split by lines
oc: string, Standard output, complete untouched
os: string: Standard output, stripped
e: List[string], Standard error, split by lines
ec: string, Standard error, complete untouched
es: string, Standard error, stripped
```

## Tilde Substitution

When encountering a literal token that begins with `~/` or is `~` alone,
the token will be replaced with the user's home directory.

## Environment Variables

Environment variables are accessed like other variables.
`export` is used to export a variable to the environment for subprocesses.

```mshell
@HOME cd

# Exporting for subprocesses
"Hello, World!" MSHELL_VAR! MSHELL_VAR export
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
