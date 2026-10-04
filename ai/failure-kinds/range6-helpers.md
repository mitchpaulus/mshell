# Range 6 (Evaluator.go 11941 to end): helpers and notes

## E helpers

### `MShellObject.Index(index int)` (site 12712)

Implementations in `mshell/MShellObject.go`.

- Checked (out of range): every error that comes from `IndexCheck` (literal 1352, quotation 1367, list 1377, string 1388, path 1399, pipe 1410);
  `MShellBinary.Index` (94, its own range check);
  `MShellGrid.Index` (2330), `MShellGridView.Index` (2482), `MShellGridRow.Index` (2601), each with its own range check.
  Mark these with `checkedErrorf`. `IndexCheck` is the shared place to do it for the first group.
- Mismatch: "Cannot index into ..." from bool (1363), int (1423), float (1427), simple (1431), and the Maybe, enum, null, datetime and dict versions (204, 281, 346, 425, 513).

### `SliceStart` / `SliceEnd` (site 12742)

- Checked: errors from `IndexCheckExc` (literal, quotation, list, string, path, pipe: 1436 to 1606);
  `MShellBinary.SliceStart` (102) and `SliceEnd` (109);
  `MShellGrid.SliceStart` / `SliceEnd` (2340, 2354);
  `MShellGridView.SliceStart` / `SliceEnd` (2492, 2502).
- Mismatch: "cannot slice ..." from bool, int, float, simple, GridRow (2611, 2615), and the Maybe, enum, null, datetime and dict versions.

### `Slice(start, end)` (site 12762)

- Checked: errors from `SliceIndexCheck` (literal, quotation, list, string, path, pipe: 1627 to 1712);
  `MShellBinary.Slice` (117), `MShellGrid.Slice` (2368), `MShellGridView.Slice` (2512).
- Mismatch: "Cannot slice ..." from bool, int, float, simple, GridRow (2619), and the Maybe, enum, null, datetime and dict versions.
- Site 12762 drops `err` and always prints "Cannot slice index a X", even for a range error.
  When it becomes `failErr(err, msg)`, the message should include `err`.

The INDEXER / ENDINDEXER / STARTINDEXER / SLICEINDEXER branches (12697 to 12765) are probably dead code.
The parser always wraps those tokens in `MShellIndexerList` (Parser.go 991 and 1435), and the evaluator then runs `processIndexerList` (Evaluator.go 2295).

## S sites

None.
The list-vs-quote redirect conflicts would split as "list: M, quote: C (as the checker is now)".
I marked them M because they are checker holes (below), not intended checked failures.

## Possible checker holes (reachable in checked programs, classified M)

Each was confirmed with the current `mshell/msh` binary: `--type-check-only` passes and the run fails at the site.

1. **Redirects and merges on quotations are not tracked** (sites 12193, 12362, 12392, 12395, 12463, 12467, 12495, 12500, 13118).
   `redirect()` and `merge()` in `TypeCoreCommand.go` return early for a quote without a `claimed` or circular check.
   The runtime does check the quote's state.
   - `([echo hi];) `a.txt` > `b.txt` > x` fails "stdout already has a file redirect".
   - `([echo hi];) 2>&1 2>&1 x` fails "stderr already has a merge".
   - `([echo hi];) 2>&1 1>&2 x` fails "circular".
   A stored quote is also mutated in place by a redirect (no freshness rule for quotes), so `@q `a` >` twice conflicts as well.
   Related, and C as it stands: `([echo hi];) `a.txt` > `a.txt` 2>> x` fails in `BuildExecutionContext` with "same file with different append modes" (sites 11983/12012 and wherever quotes run).
2. **`$NAME!` accepts any type** (site 12519).
   `TypeCore.go` `token()`, case ENVSTORE, pops the value without checking its type.
   `true $FOO!` and `3.0 $FOO!` pass the checker and fail "Cannot export a Boolean/Float".
   The runtime takes str, path, a bare-word literal and int (`CastString`).
3. **`&` on a pipe** (site 12786).
   `background()` accepts any command, pipes included.
   The runtime takes only `*MShellList`, so `[[echo hi] [cat]] | & ;` fails "Cannot execute '&' on a Pipe".
4. **`2>&1` / `1>&2` on a pipe** (site 12506).
   `merge()` accepts a pipe (commandParts succeeds and the states are set).
   The runtime falls to its default case, so `[[echo hi] [cat]] | 2>&1 ;` fails "Cannot apply '2>&1' to a Pipe".

## Runtime bugs

1. **`and` / `or` with a quote pop before they check the quote's result** (sites 11989, 12018).
   The code pops the quote's result before `result.ShouldPassResultUpStack()`.
   If the quote calls `exit`, `break`, `continue` or fails, and nothing is left below, the program reports "After executing the quotation in or, the stack was empty" and does not exit or pass the failure up.
   `false (0 exit) or drop` prints that error instead of exiting with 0.
   The fix is to check the result before the pop. After that, both sites are truly M.
2. **`a:b` slice lexeme** (site 12757): when only `err2` is set, `err.Error()` dereferences nil.
   This branch is probably dead code (see above).
3. **`and` / `or` quote branch** (11980, 12006): `obj2.(MShellBool)` is an unchecked assertion, so a non-bool below the quote would panic.
   The checker rules this out.
4. **Bare-word literal as a redirect target** (13172 to 13195).
   On a quotation the branch does not push `obj2` back, so the quote disappears from the stack.
   With `<` it also sets `StandardInputContents` on a quote but `StandardInputFile` on a list, which differs from a str target (contents).
5. **`MShellBinary` indexing** (MShellObject.go 94 to 123) does not accept negative indexes, unlike every other kind.
   Also, `SliceStart(len)` fails where the other kinds return an empty value.

## Literal decoding at run time (C, but arguably static errors)

The lexer stores a nil `Value` when a literal does not decode, and the evaluator decodes it again and fails.
The checker just pushes `int` / `float` / `str`.

- 12861: an integer literal out of range (`99999999999999999999`).
- 13295: a float literal out of range (a 1 followed by 400 zeros, then `.0`).
- 12562: `$٣`. The lexer's `parsePositional` uses `unicode.IsDigit`, so non-ASCII digits lex as a positional and then fail `Atoi`.
  `$0` (12566) also passes the checker.

String literals are fine: the lexer rejects bad escapes with the same `escapedRune` set, so 12872 is unreachable.
