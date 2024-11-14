# `mshell`

`mshell` is my personal scripting language, meant to replace short shell scripts (< ~100 LOC) and other shell one-liners.
A concatenative language is a good fit for this purpose, as it allows for easy composition of simple functions and pipelines.

The goal is to provide most of the simple Gnu utilities as part of the language,
while making calls to external programs and pipelines simple and easy.

Future goals are to even add some type safety.

# Examples

Best way to understand purpose and syntax of `mshell` is to see it in action. Here are some examples.

*Better Awk One-liners*. Examples from the `awk` book, translated to `mshell`

| Objective | `awk` | `mshell` |
|-----------|-------|----------|
| Print the total number of input lines             | `END { print NR }`                    | `.. len wl` |
| Print the 10th input line                         | `NR == 10`                            | `.. :10: wl` (Failure if < 10 lines) |
| Print the last field of every input line          | `{ print $NF }`                       | `.. (ws split :-1: wl) each` |
| Print the last field of the last input line       | `{ field = $NF } END { print field }` | `.. :-1: (ws split :-1: wl)` |
| Print every input line with more than four fields | `NF > 4`                              | `.. dup (ws split) map (len 4 >) filter (" " join) map unlines w`  |


*Simpler execution of common shell idioms*

| Objective | `sh` | `mshell` |
|-----------|-----|----------|
| Print the number of files in the current directory | `ls \| wc -l`                                                | `* glob len wl` |
| `find`/`xargs`                                     |  `find . -t x -name '*.sh' -print0 \|  xargs -0 mycommand`   | `[mycommand [find . -t x -name "*.sh"]]o;` |

# TODO

- Floating point numbers
- Dictionaries
