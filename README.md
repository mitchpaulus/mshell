# `mshell`

`mshell` is my personal scripting language, meant to replace short shell scripts (< ~100 LOC) and other shell one-liners.
A concatenative language is a good fit for this purpose, as it allows for easy composition of simple functions and pipelines.

The goal is to provide most of the simple Gnu utilities as part of the language,
while making calls to external programs and pipelines simple and easy.

Future goals are to even add some type safety.

# Examples

Best way to understand purpose and syntax of `mshell` is to see it in action. Here are some examples.

*Better Awk One-liners*. Examples from the `awk` book, translated to `mshell`

```sh
# 1. Print the total number of input lines:
# END { print NR }
.. len wl

# 2. Print the 10th input line:
# NR == 10
.. :9: wl

# 3. Print the last field of every input line:
# { print $NF }
wt (:-1: wl) each

# 4. Print the last field of the last input line:
#     { field = $NF }
# END { print field }
wt :-1: :-1: wl

# 5. Print every input line with more than four fields
# NF > 4
.. (wsplit len 4 >) filter (wl) each

# 6. Print every input line in which the last field is more than 4
# $NF > 4
.. (wsplit :-1: toFloat 4 >) filter (wl) each

# 7. Print the total number of fields in all input lines
#     { nf = nf + NF }
# END { print nf }
.. (wsplit len) map sum wl

# 8. Print the total number of lines that contain 'Beth'
# /Beth/ { nlines = nlines + 1 }
# END { print nlines }
.. ("Beth" in) filter len wl

# 9. Print the largest first field and the line that contains it (assumes some $1 is positive):
# $1 > max { max = $1; line = $0 }
# END      { print max, line }

0 @max
..
# line first
(dup wsplit :1: toFloat [(dup max! >) (@max @line)] if)
each max! w " " w line! w

# 10. Print every line that has at least one field
# NF > 0
.. (len 0 >) filter (wl) each

# 11. Print every line longer than 80 characters
# length($0) > 80
.. (len 80 >) filter (wl) each

# 12. Print the number of fields in every line followed by the line itself
# { print NF, $0 }
.. (dup wsplit len w " " w wl) each

# 13. Print the first two fields in opposite order, of every line
# { print $2, $1 }
.. (wsplit :1: :2: w w) each

# 14. Exchange the first two fields of every line and then print the line
# { temp = $1; $1 = $2; $2 = temp; print }
# Need a way to write value into an index.
# .. (dup wsplit :1: :2: swap w w) each

# 15. Print every line with the first field replaced by the line number
# { $1 = NR; print }
# Need a way to write value into an index.
# .. (dup w 1 w w) each

# 16. Print every line after erasing the second field
# { $2 = ""; print }
# Need a way to delete at an index.

# 17. Print in reverse order the fields of every line
# { for (i = NF; i > 0; i = i - 1) printf "%s ", $i
# printf "\n"
# }
.. (wsplit reverse " " join wl) each

# 18. Print the sums of the fields of every line
# { sum = 0
#   for (i = 1; i <= NF; i = i + 1) sum = sum + $i
#   print sum
# }
.. (wsplit (toFloat) map sum wl) each

# 19. Add up all fields in all lines and print the sum
# { for (i = 1; i <= NF; i = i + 1) sum = sum + $i }
# END { print sum }
.. (wsplit (toFloat) map sum) map sum wl

# 20. Print every line after replacing each field by its absolute value
# { for (i = 1; i <= NF; i = i + 1) $i = ($i < 0) ? -$i : $i; print }
.. (wsplit (toFloat abs) map " " join wl) each

```

<!-- | Objective | `awk` | `mshell` | -->
<!-- |-----------|-------|----------| -->
<!-- | Print the total number of input lines             | `END { print NR }`                    | `.. len wl` | -->
<!-- | Print the 10th input line                         | `NR == 10`                            | `.. :10: wl` (Failure if < 10 lines) | -->
<!-- | Print the last field of every input line          | `{ print $NF }`                       | `.. (ws split :-1: wl) each` | -->
<!-- | Print the last field of the last input line       | `{ field = $NF } END { print field }` | `.. :-1: (ws split :-1: wl)` | -->
<!-- | Print every input line with more than four fields | `NF > 4`                              | `.. (dup ws split len [(4 >) (wl) (drop)] if) each`  | -->


*Simpler execution of common shell idioms*

| Objective | `sh` | `mshell` |
|-----------|-----|----------|
| Print the number of files in the current directory | `ls \| wc -l`                                                | `* glob len wl` |
| `find`/`xargs`                                     |  `find . -t x -name '*.sh' -print0 \|  xargs -0 mycommand`   | `[mycommand [find . -t x -name "*.sh"]]o;` |

# TODO

- Floating point numbers
- Dictionaries
