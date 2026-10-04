# Strings, regex, numbers, conversions, encoding: audit

Line numbers are `mshell/Evaluator.go` unless another file is named.
`cs` below means `str | path | int`: exactly what `CastString` accepts (MShellObject.go:2115-2149: string, bare-word literal, path, int; float, bool, bytes, datetime, Maybe, null, list, dict, quote, grid all fail).
A bare word in a list literal (`[ls -l]`) is an `MShellLiteral` at runtime and `str` in the core checker (TypeCore.go `word`), so every `str` input must also accept a literal at runtime; the rows that do not are flagged.

## How to paste `strings.go.txt`

It REPLACES these seed lines of `buildCoreTable`: the `arithmetic`/`comparison` vars and every `regTok` from PLUS to NOT (keep `QUESTION` and its `keeps`), `/`, `mod`, `abs`, `inc`, `toInt`, `toFloat`, `lines`, `split`, `join`, the `trim...title` loop, and `parseJson`.
The two `t.partialName[...] = ...` lines at its end must run after the seed's `t.partialName = map...` literal (move them below it, or move the literal up).
I spliced it into a copy of the repo: the table builds, and `go test` passes except these `TestCoreChecker` rows, which encode the old wrong entries:

- `none 5 just = drop` (want ok): now "no matching overload for '='". The Maybe form of `=` is SPECIAL (below). Note that `partialToken` cannot cover it: `partial()` (TypeCore.go:819) only reports a token form as "not covered yet" when a list or quote is in the top two slots, and for `=` lists are exactly the case that must be an error.
- `[1] ["a"] = drop` (want ok): `=` on lists is always a runtime error ("Equality currently not defined for lists", MShellObject.go:2059). The row should expect an error, and the "join of generic arguments" it was testing needs another word.
- `(1 +) drop`, `(1 +) q!` (want "annotate"), `(1 +) q! 2.5 @q x str wl` (want ok), `(1 +) q! "a" @q x drop` (want "no matching overload for '+'"): with `(float int -- float)` gone, `(1 +)` is unambiguously `(int -- int)`. `2.5 1 +` fails at runtime. `(dup +)` gives the intended behaviour for all four rows (checked: ambiguous alone, `5`/`2.5` ok, `true` gives "no matching overload for '+'").

## Table

| name | runtime accepts (Evaluator.go:line) | old sig | new sig + marks | notes |
|---|---|---|---|---|
| `+` (PLUS) | 12403: int int, float float, str/literal pairs, list list (new list), path path (string concat), Grid/GridView pairs (concatGrids). int with float fails | `(int int -- int)` `(float float -- float)` `(int float -- float)` `(float int -- float)` `(str str -- str)` `([t] [t] -- [t])` `(path path -- path)` `(Grid \| GridView Grid \| GridView -- Grid)` | same minus the two mixed rows; list row newListOut (as seed) | Rule 1: removed `(int float -- float)` and `(float int -- float)`; the runtime says "'+' does not coerce numeric types". The seed had them too. |
| `-` (MINUS) | 12483: int int, float float, datetime datetime (days as float). Mixed fails | arithmetic + `(datetime datetime -- float)` | `(int int -- int)` `(float float -- float)` `(datetime datetime -- float)` | Rule 1: mixed int/float removed (seed too). |
| `*` (ASTERISK) | 11572: numeric form int int, float float; otherwise command capture | `(int int -- int)` `(float float -- float)` + captures | `(int int -- int)` `(float float -- float)` | Unchanged. Capture forms stay in the seed's `partialToken[ASTERISK]`. |
| `<` `>` | 12599: int int, float float, datetime datetime (12740); other operands are redirects | comparison + redirect | `(int int -- bool)` `(float float -- bool)` `(datetime datetime -- bool)` | Unchanged; redirects stay partial (seed). |
| `<=` `>=` | 12547: int int, float float, datetime datetime. No other forms | comparison | same | Unchanged. No string comparison exists at runtime. |
| `=` `!=` (EQUALS, NOTEQUAL) | 12790, 12826: `top.Equals(below)`, MShellObject.go:149-2110. Errors for: list, quote, pipe, grid, view, row on top (always); int/float/bool/bytes on top with another kind; str on top with anything but str/literal (path errors); path on top with anything but path/literal (str errors). No error with null, datetime, Maybe or dict on top (different kind gives false). Maybe recurses into payloads; dict compares keys, then values whose TypeName match | `(a a -- bool)` `(path str -- bool)` `(str path -- bool)` | `(int int -- bool)` `(float float -- bool)` `(str str -- bool)` `(bool bool -- bool)` `(bytes bytes -- bool)` `(path path -- bool)` `(datetime datetime -- bool)` `(null null -- bool)`. SPECIAL for Maybe and dict | Rule 1: `(a a -- bool)` accepts lists, quotes, grids and unions such as `int \| str` (whose members can differ at runtime), all errors. `(path str)` and `(str path)` both error at runtime for a real string (they only work for a bare-word literal). **SPECIAL (Eq):** `=`/`!=` also take two values of one type `T` when `Eq(T)`: `Eq` holds for the eight scalars above, for `Maybe[E]` when `Eq(E)`, and for a dict or shape (`{str: V}`, shapes, any dict-kinded type) when every label type (declared and remainder; an `open` remainder fails) is a union whose every member is `Eq` (dict values of different kinds compare false without error, MShellObject.go:578, so unions are fine there and only there). `Json` is not `Eq` (it holds lists). Until this check exists, Maybe and dict operands get "no matching overload". Unsure: whether the core checker's per-member union matching tries every pair (int with str) for two union arguments; it must, or `x x =` with `x : int \| str` slips through. |
| `str` (STR) | 12208: any value, `ToString` | `(a -- str)` | `(a -- str)` | Unchanged. Overflows the Go stack on a cyclic value (design, sec-alias). |
| `not` (NOT) | 12525: bool, int (0 is false) | `(bool \| int -- bool)` | same | Unchanged. |
| `/` | 6631: int int (int division), float float, path path (filepath.Join). Mixed fails; zero divisor is an error | `(int int -- int)` `(float float -- float)` `(path path -- path)` | same | Unchanged. |
| `mod` | 7016: int int, float float (math.Mod) | `(int int -- int)` `(float float -- float)` | same | Unchanged. Runtime bug: no default case (below). |
| `abs` | 9881: int, float | `(int -- int)` `(float -- float)` | same | Unchanged. |
| `inc` | 10649: int | `(int -- int)` | same | Unchanged. The comment "as a reference" is wrong; it pushes a new int. |
| `floor` `ceil` | 9758, 9778: int (returned as is), float | `(int \| float -- int)` | same | Unchanged. |
| `round` | 9798: int, float (via float64) | `(int \| float -- int)` | same | Unchanged. An int above 2^53 loses precision (floor/ceil return it as is). |
| `sin` `arctan` `ln` `sqrt` | 9812, 9824, 9853, 9869: float only | `(float -- float)` | same | Unchanged. |
| `pow` | 9836: float base below, float exponent on top | `(float float -- float)` | same | Unchanged. |
| `random` `randomFixed` | 9865, 9867 | `( -- float)` | same | Unchanged. |
| `toInt` | 6729: MShellString (TrimSpace, Atoi; none on failure), int, float (truncates) | `(str -- Maybe[int])` `(float -- int)` `(int -- int)` | same | Unchanged. Literal hole: a bare-word literal fails ("Cannot convert a Literal"). |
| `toFloat` | 6707: MShellString (TrimSpace, ParseFloat), int, float | `(str -- Maybe[float])` `(int -- float)` `(float -- float)` | same | Unchanged. Literal hole as for toInt. |
| `toBase` | 6751: int value below, int base (2-36) on top | `(int int -- str)` | same | Unchanged. Base out of range is a runtime error. |
| `fromBase` | 6770: str or literal below, int base on top | `(str int -- Maybe[int])` | same | Unchanged. |
| `toFixed` | 10017: int or float below, int places on top | `(int int -- str)` `(float int -- str)` | `(int \| float int -- str)` | Same set, one candidate (same output). Negative places produce garbage (below). |
| `numFmt` | 7207: int or float below, dict on top. Options read by literal key (289-418): `decimals` int, `sigFigs` int (else `sigfigs`), `preserveInt` bool, `thousandsSep`/`decimalPoint` via CastString, `grouping` list of int. Other keys ignored | `(int {opts} -- str)` `(float {opts} -- str)`, string options `str` | `(int \| float {decimals?: int, sigFigs?: int, sigfigs?: int, preserveInt?: bool, decimalPoint?: cs, thousandsSep?: cs, grouping?: [int]} -- str)` | Rule 1: string options accept path and int. One candidate. The open shape takes every dict-kinded value whose matching labels fit (rule 3). Value checks (negative decimals, non-positive sigfigs or grouping entries) are runtime errors. |
| `toJson` | 9691: any value, `ToJson` (no error path) | `(t -- str)` | `(a -- str)` | Renamed generic only. NaN/Inf give an empty string (below). |
| `typeof` | 9700: any value, `TypeName` | `(t -- str)` | `(a -- str)` | Renamed generic only. Returns "Literal" for a bare word, "String" for a string, though both are `str` statically. |
| `split` | 6291: CastString on both; string below, delimiter on top | `(str str -- [str])` | `(cs cs -- new [str])` | Rule 1: CastString. `new`: a new list of strings (seed). |
| `wsplit` | 6313: CastString | `(str -- [str])` | `(cs -- new [str])` | Rule 1; `new` added. |
| `lines` | 6394: MShellString only | `(str -- [str])` | `(str -- new [str])` | As seed. Literal hole: a literal is rejected. |
| `join` | 6331: top is a quote: grid join (3066). Else delimiter str/literal on top, list below whose items are all str/literal | `([str] str -- str)` + `(Grid Grid (GridRow -- k) (GridRow -- k) -- Grid)` | `([str] str -- str)`; grid form partial | The string form is unchanged. **SPECIAL / partial:** the grid form (`left right leftKey rightKey join`, the inner join) is the same as `innerJoin`: Grid or GridView twice, two key quotes each run on a child stack with one GridRow and leaving exactly one key (a scalar, a Maybe of one, none skips the row, or a flat list of scalars), giving a new Grid; fails if a column name is in both grids. It belongs with the grid category's join entry; whoever registers it must register it on `join` here (reg panics on duplicates), mark it `child`, and drop the partialName line. |
| `unlines` `unlinesCrLf` | 6373: list of str/literal; adds the line ending after every item | `([str] -- str)` | same | Unchanged. |
| `trim` `trimStart` `trimEnd` | 7161: CastString | `(str -- str)` | `(cs -- str)` | Rule 1. trim uses TrimSpace; trimStart/trimEnd trim only space, tab, newline (not `\r`). |
| `upper` `lower` `title` | 7180: string, literal (stays literal), path (stays path) | `(str -- str)` | `(str -- str)` `(path -- path)` | Rule 1: path form added. Int is rejected (no CastString here). |
| `strEscape` | 6279: CastString | `(str -- str)` | `(cs -- str)` | Rule 1. |
| `startsWith` `endsWith` | 7393: CastString on both; string below, prefix/suffix on top | `(str str -- bool)` | `(cs cs -- bool)` | Rule 1. |
| `countSubStr` | 10035: CastString on both; string below, substring on top | `(str str -- int)` | `(cs cs -- int)` | Rule 1. |
| `findReplace` | 6252: CastString on all three; original, find, replacement (top) | `(str str str -- str)` | `(cs cs cs -- str)` | Rule 1. |
| `leftPad` | 7415: input (CastString), pad (CastString, non-empty), int length on top | `(str str int -- str)` | `(cs cs int -- str)` | Rule 1. Empty pad or negative length are runtime errors. |
| `reMatch` | 9413: CastString both; string below, regex on top | `(str str -- bool)` | `(cs cs -- bool)` | Rule 1. Bad regex is a runtime error. |
| `reReplace` | 9441: CastString all; string, regex, replacement on top (`$1` expands) | `(str str str -- str)` | `(cs cs cs -- str)` | Rule 1. |
| `reFindAll` | 9470: CastString both | `(str str -- [[str]])` | `(cs cs -- new [[str]])` | Rule 1; `new` (outer and inner lists are new, strings immutable). Each inner list is the whole match then each group; a group that did not take part is "". |
| `reFindAllIndex` | 9501: CastString both | `(str str -- [[int]])` | `(cs cs -- new [[int]])` | Rule 1; `new`. Byte offsets as start,end pairs; a group that did not take part gives -1 -1. |
| `reSplit` | 9532: CastString both | `(str str -- [str])` | `(cs cs -- new [str])` | Rule 1; `new`. |
| `base64encode` | 11264: bytes | `(bytes -- str)` | same | Unchanged. |
| `base64decode` | 11278: MShellString only | `(str -- bytes)` | same | Unchanged. Literal hole. Invalid base64 is a runtime error. |
| `utf8Str` | 11296: bytes | `(bytes -- str)` | same | Unchanged. Does not validate UTF-8. |
| `utf8Bytes` | 11311: MShellString only | `(str -- bytes)` | same | Unchanged. Literal hole. |
| `md5` | 10812: MShellString (content), bytes (content), path (file contents) | `(str \| path \| bytes -- str)` | same | Unchanged. Literal hole: a literal is rejected (not read as a file). |
| `sha256sum` | 8335: CastString, always a file name | `(path -- str)` | `(cs -- str)` | Rule 1: str and int name a file too. Note the asymmetry with md5, where a str is hashed as content. |
| `uuid` `uuid7` | 10943, 10949 | `( -- str)` | same | Unchanged. |
| `urlEncode` | 11222: string or literal (QueryEscape); or a dict whose every value is CastString-able or a list of CastString-able items (a list becomes repeated `k=v`) | `(str \| {str \| int \| path \| [str] \| [int] \| [path]} -- str)` | `(str -- str)`; dict form partial | **SPECIAL:** the old dict form is now an ill-formed union (three list members, rule "distinct kinds") and invariance stops `{str: str}` from fitting any single dict type. Rule: urlEncode takes any dict-kinded value (rule 3, shapes included) when every label type (declared and remainder; an `open` remainder fails) is a union whose members are each `str`, `int`, `path`, or one list `[E]` with `E` a union of `str`, `int`, `path`. A top-level path or int is rejected at runtime, so the str form stays `str` only. |
| `parseJson` | 9622: path or literal (reads that file), MShellString (the text), bytes (must be UTF-8) | `(str \| path \| bytes -- t)` | `(str \| path \| bytes -- new Json)` | As seed (rule 4). A literal is read as a FILE NAME, a string as JSON text. Every number becomes float (4486), never int (below). |
| `parseHtml` | 10616: path or literal (reads file), MShellString (text). Returns `nodeToDict` of the document node (MShellObject.go:2160) | `(str \| path -- {v})` | `(str \| path -- new {tag: str, attr: {str: str}, children: [Json], text: str})` | Rule 4: the free `v` is gone. **Exact runtime type:** `type HtmlNode = {tag: str, attr: {str: str}, children: [HtmlNode], text: str}`, all four keys always present, every dict and list new. The root is the document node: `tag` "", `attr` empty, `children` the element children (normally one `<html>` node). `tag` is the element name; `attr` maps attribute name to value; `children` holds element children only (text, comment and doctype nodes are skipped); `text` is the direct text children, each TrimSpace'd, concatenated with no separator (descendants' text is not included). The key is `attr`, not `attrs` (the unused `NodeDict` struct at MShellObject.go:2153 says `attrs`). The entry types the root precisely and `children` as `[Json]`, which is sound since every node is a Json value and the list is new; with a built-in recursive `HtmlNode` alias (like `Json`) it becomes `(str \| path -- new HtmlNode)`. Written shapes are open, so the four-key exactness is lost either way. |
| `parseLinkHeader` | 11345: MShellString only. Returns a list of dicts with keys `url` (str), `rel` (str), `params` (dict of str) | `(str -- [{v}])` | `(str -- new [{url: str, rel: str, params: {str: str}}])` | Rule 4: exact output. Each dict always has exactly these three keys (open in the table syntax). Literal hole. Parse failure is a runtime error. |

No word in this category takes a quote (except grid `join`), so none is marked `child`; none returns its input, so none is marked `keeps`.
No two candidates of any entry fit the same known arguments.

## Runtime bugs found

1. `mod` (7027): the outer switch on the top value has no default. With a non-numeric top (`1 "a" mod`) both items are popped, nothing is pushed, and no error is raised.
2. `parseHtml` (10625): the type switch has no default. Any input other than string, literal or path leaves `reader` nil and `html.Parse(nil)` panics.
3. Literal hole: `lines`, `toInt`, `toFloat`, `md5`, `base64decode`, `utf8Bytes` and `parseLinkHeader` accept MShellString but not MShellLiteral, while the checker types a bare list word as `str`. `[1a 2b] (toInt) map` type-checks and fails with "Cannot convert a Literal to an int". The runtime should treat a literal as a string in these words.
4. `parseJson`, `parseHtml` (and `parseCsv`) read a literal as a file path but a string as content. The checker cannot tell them apart (both `str`), so `[data.json] (parseJson) map` reads files while the same text from `split` is parsed as JSON.
5. `parseJson` makes every JSON number a float (`ParseJsonObjToMshell`, 4486). The design (section JSON) says integral numbers parse as int. Typing stays sound (`Json` contains both), but an `int n` match arm on parsed JSON never fires.
6. `=`/`!=` are not symmetric: with null, datetime, Maybe or dict on top, any other kind gives false; with int, float, bool, bytes, str or path on top it is an error (`1 null =` is false, `null 1 =` fails). And `str`/`path` comparison fails in both orders, while literal/path works.
7. `toFixed` with negative places: `fmt.Sprintf("%.*f", -2, 3.14)` gives `%!(BADPREC)3.140000` instead of an error.
8. `toJson` on NaN or ±Inf: the `json.Marshal` error is dropped and the output is an empty string (invalid JSON); inside a list it gives `[,1]`-like text.
9. `leftPad` counts bytes, not runes, and `padStr[:remainder]` can cut a multi-byte pad character in half.
10. Outside this category, seen while reading `<`/`>` (12690): a bare-word literal redirect onto a quotation sets `StandardInputContents` (not `StandardInputFile`) for `<`, and neither `<` nor `>` pushes the quotation back, so it disappears from the stack.

## Unsure

- Whether `str | path | int` (the exact CastString set) is wanted for the string words, or whether the runtime should be narrowed to `str` instead. I followed rule 1. Int is the surprising member (`5 " " split` works).
- Whether the core checker's per-member union matching tries every pair of members for two union arguments (needed for `=` with `x : int | str`).
- Whether `{}` as a written empty shape also means "any dict-kinded value" in the resolver. I did not need `{}` in this category, so I did not check.
- `toInt` on a float NaN, ±Inf or out-of-range value uses Go's `int(f)`, whose result is implementation-defined. It is not a type problem.
