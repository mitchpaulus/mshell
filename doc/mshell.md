## Built-ins

`dup`: Duplicate
`swap`: Swap
`drop`: Drop
`append`: Append
`.s`: Print stack at current location
`over`: Over, copy second element to top

`w`: Write
`wl`: Write line
`we`: Write error
`wle`: Write error line

`len`: Length of string/list
`args`: List of string arguments
`nth`: Nth element of list (0-based)

`glob`: Run glob against string/literal on top of the stack. Leaves list of strings on the stack.

## Process Substitution

With a list on the stack, the following operators will leave output content on the stack after execution:

```mshell
o: List[string], Stadard output, split by lines
oc: string, Standard output, complete untouched
os: List[string]: Standard output, stripped
e: List[string], Standard error, split by lines
ec: string, Standard error, complete untouched
es: List[string], Standard error, stripped
```

## Tilde Substitution

When encountering a literal token that begins with `~/` or is `~` alone,
the token will be replaced with the user's home directory.

## Environment Variables

Environment variables are accessed like other variables.

```mshell
[cd HOME!];
```

## Indexing

If the indexing is fixed, there is dedicated syntax for it.

```mshell
[ 4 3 2 1 ] :1:  # 3
[ 4 3 2 1 ] 1:3  # [ 3 2 ]
[ 4 3 2 1 ] :3   # [ 4 3 2 ]
[ 4 3 2 1 ] 2:   # [ 2 1 ]
```
