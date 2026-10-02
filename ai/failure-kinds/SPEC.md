# Classifying runtime failures (plan stage 1, item 5)

Every runtime failure the evaluator reports gets one of two kinds.
The soundness tests (plan stage 7) run checked programs and fail on any *type mismatch*, so the kinds must be right.

- **M, type mismatch**: a program that passes the type checker (`msh --type-check-only`) can never reach this failure.
  Wrong runtime kind of a value (`+` on a string, `x` on a non-quote), too few values on the stack, a quote that leaves the wrong number or kind of values, a missing *required* key, a redirect that conflicts with the stream's state, an unknown word, a malformed pattern the parser let through, an "unreachable"/"not implemented" internal error.
- **C, checked failure**: a program that passes the checker may still reach this failure, because the types do not rule it out.
  An index or slice out of range, `?` on `none`, a missing file or any other OS / I/O error, a failed process, text that does not parse (bad JSON, bad CSV, a bad date, a bad regex, a bad base64 string), division or mod by zero, a column or key name that is data (names known only at run time, or a grid whose schema is unknown), a validation budget, `deepCopy` or `str` of a cyclic value, reading a variable that is not set (a stored quote can read one later), a value out of an allowed range (a negative count, a mode that is not valid), network errors.

The test for each site: **can some program the core checker accepts reach it?**
If yes, C. If no, M. When you cannot tell, choose M and say "unsure" in the note: a wrong M shows up as a false alarm in the soundness tests and gets fixed, while a wrong C hides a real hole forever.

How to decide, not guess:

- The checker's builtin signatures are in `mshell/TypeCoreBuiltins.go` (the table, read with `grep -n '"name"'`), and words the walker types itself are in `TypeCore*.go` (`TypeCoreGrid.go`, `TypeCoreDict.go`, `TypeCoreCommand.go`, `TypeCoreMatch.go`, `TypeCoreValidate.go`, ...). If the signature makes the failing input impossible (the parameter is `int`, and the site fails on a non-int), it is M.
  If the signature takes a union and the site fails on one member's *value* (not its kind), it is C.
- When one site handles several conditions (`if obj is not a list || index out of range`), and they have different kinds, say so: kind `S` (split) with a note saying how to split it.
- A site that reports `err.Error()`: find what produced `err`. Stack pops (`stack.Pop`, `Pop2`, ...) fail only on underflow: M. OS, `io`, `strconv` on user text, `regexp.Compile`, `time` parsing, HTTP: C.
  A helper defined in this repository (`obj.Index`, `getStringOption`, `parseZipExtractOptions`, `extendGrid`, ...): read it. If every error it returns is one kind, use that kind. If it returns both kinds, use kind `E`, and describe the helper in the helpers file (below): its file and line, and which of its `return ..., err` statements are checked failures. The site will then call `failErr(err, msg)`, and the helper will mark its checked errors with `checkedErrorf` / `asChecked`.

Do not edit any file except your two output files.

## Output

Two files in `ai/failure-kinds/`, named after your range (for example `range1.tsv`, `range1-helpers.md`):

1. `rangeN.tsv`: one line per call site of `FailWithMessage(` or `failPtr(` in your line range of `mshell/Evaluator.go`, in order, tab separated, no header:

   ```
   <line>	<M|C|E|S>	<short note: the condition, and why this kind; "unsure" if unsure>
   ```

   Every site in your range must appear exactly once. Use the line numbers of the file as it is now (it does not compile at the moment, on purpose: the old function names were removed; do not fix that).

2. `rangeN-helpers.md`: for each helper you marked `E`, the helper's location and which error returns are checked; and for each `S` site, how to split it. Also note anything surprising: a failure that looks reachable in a checked program but should not be (a possible checker hole), or a runtime bug. Empty if nothing.
