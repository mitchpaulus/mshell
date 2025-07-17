# `mshell`

> [!WARNING]
> This is still very much an experiment.
> I still am changing significant parts of the syntax and semantics.

`mshell` is my personal scripting language, meant to replace short shell scripts (< ~100 LOC) and other shell one-liners.
A concatenative language is a good fit for this purpose, as it allows for easy composition of simple functions and pipelines.

The goal is to provide most of the simple Gnu utilities as part of the language,
while making calls to external programs and pipelines simple and easy.

Future goals are to even add some type safety.

# Examples

Best way to understand purpose and syntax of `mshell` is to see it in action. Here are some examples.

*Better Awk One-liners*. Examples from the `awk` book, translated to `mshell`. You can run these examples like:

```sh
msh file_with_contents.msh < input_file_to_process.txt
awk -f file_with_contents.awk < input_file_to_process.txt
# OR (using 1st example)
msh -c 'sl len wl' < input_file_to_process.txt
awk 'END { print NR }' < input_file_to_process.txt
```

Note that you'll also need the environment variable `MSHSTDLIB` pointing to the file at `lib/std.msh`.

```sh
# 1. Print the total number of input lines:
# END { print NR }
sl len wl

# 2. Print the 10th input line:
# NR == 10
sl :9: wl

# 3. Print the last field of every input line:
# { print $NF }
wt (:-1: wl) each

# 4. Print the last field of the last input line:
#     { field = $NF }
# END { print field }
wt :-1: :-1: wl

# 5. Print every input line with more than four fields
# NF > 4
sl (wsplit len 4 >) filter uw

# 6. Print every input line in which the last field is more than 4
# $NF > 4
sl (wsplit :-1: toFloat? 4 >) filter uw

# 7. Print the total number of fields in all input lines
#     { nf = nf + NF }
# END { print nf }
sl (wsplit len) map sum wl

# 8. Print the total number of lines that contain 'Beth'
# /Beth/ { nlines = nlines + 1 }
# END { print nlines }
sl ("Beth" in) filter len wl

# 9. Print the largest first field and the line that contains it (assumes some $1 is positive):
# $1 > max { max = $1; line = $0 }
# END      { print max, line }
(
 line! prev!
 @line wsplit :0: toFloat? new!
 @new @prev :0: > ([@new @line]) (@prev) iff
)
[-99999999  ""] sl foldl
dup :0: max! :1: maxLine! $"{@max str} {@maxLine}" wl

# 10. Print every line that has at least one field
# NF > 0
sl (wsplit len 0 >) filter uw

# 11. Print every line longer than 80 characters
# length($0) > 80
sl (len 80 >) filter uw

# 12. Print the number of fields in every line followed by the line itself
# { print NF, $0 }
sl (dup wsplit len w " " w wl) each

# 13. Print the first two fields in opposite order, of every line
# { print $2, $1 }
wt (:1:, :0: wjoin wl) each

# 14. Exchange the first two fields of every line and then print the line
# { temp = $1; $1 = $2; $2 = temp; print }
wt (:1:, :0:, 2: wjoin wl) each

# 15. Print every line with the first field replaced by the line number
# { $1 = NR; print }
wt d!
@d len seq (1 + str) map
@d (line! lineNum! [@lineNum] @line 1: + " " join) zip uw

# 16. Print every line after erasing the second field
# { $2 = ""; print }
wt ("" 1 setAt wjoin wl) each

# 17. Print in reverse order the fields of every line
# { for (i = NF; i > 0; i = i - 1) printf (i == 1 ? "%s" : "%s "), $i
# printf "\n"
# }
wt (reverse wjoin wl) each

# 18. Print the sums of the fields of every line
# { sum = 0
#   for (i = 1; i <= NF; i = i + 1) sum = sum + $i
#   print sum
# }
wt ((toFloat?) map sum str wl) each

# 19. Add up all fields in all lines and print the sum
# { for (i = 1; i <= NF; i = i + 1) sum = sum + $i }
# END { print sum }
wt ((toFloat?) map sum) sum str wl

# 20. Print every line after replacing each field by its absolute value
# { for (i = 1; i <= NF; i = i + 1) $i = ($i < 0) ? -$i : $i; print }
wt ((toFloat? abs str) map wjoin wl) each

```


*Simpler execution of common shell idioms*

| Objective | `sh` | `mshell` |
|-----------|-----|----------|
| Print the number of files in the current directory | `ls \| wc -l`                                                | `"*" glob len wl` |
| `find`/`xargs`                                     |  `find . -t x -name '*.sh' -print0 \|  xargs -0 mycommand`   | `[mycommand [find . -t x -name "*.sh"]]o;` |
| `head` | `head -n 10` | `sl :10 uw` |
| `tail` | `tail -n 10` | `sl :-10 uw` |
| `wc` | `wc -l` | `sl len wl` |
| `grep` | `grep 'pattern'` | `sl ("pattern" in) filter uw` |
| `cut` | `cut -d ';' -f 2` | `sl (";" split :1: wl) each` |


# TODO

- Improved error messages
- Type checking

# References/Inspirations

- [Porth](https://gitlab.com/tsoding/porth)
- [Factor](https://factorcode.org/)
- [`dt`](https://dt.plumbing/)
