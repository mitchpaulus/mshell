# Dicts and grids: audit of the builtin table

Line numbers are `mshell/Evaluator.go` unless noted. `key` = `str | path | int` (what `CastString` accepts; see Unsure 1).
`υ(D)` = the Get-Key read type of a dict-kinded `D`: the join of every label's type; `unknown` when the remainder is `open` or no join exists (see Unsure 4).
"Fresh when cols immutable" = the new grid is a fresh tree exactly when every column type is immutable (grid and column metadata are shared, but they are only ever exposed read-only as `{}`, so they never need retyping).

The `.go.txt` was checked by pasting it into a scratch copy of the package: the table builds, `TestCore*` passes, and small programs over each registered entry check as expected.

## Table

| name | runtime accepts (Evaluator.go:line) | old sig | new sig + marks | notes |
|---|---|---|---|---|
| `keys` | dict only; new `[str]`, sorted (8448) | `({v} -- [str])` | `({} -- new [str])` | Rule 3: shapes were rejected by `{v}`. Output new, immutable elements. |
| `values` | dict only; new list, sorted by DebugString (8448) | `({v} -- [v])` | SPECIAL, not registered | `(D -- [υ(D)])`, newList (fresh when υ immutable). Rule 3. |
| `keyValues` | dict only; new list of new dicts `{k, v}`, sorted by key (10664) | `({v} -- [{k: str, v: v}])` | SPECIAL, not registered | `(D -- [{k: str, v: υ(D) \| exact}])`; the pair dicts are new exact shapes; whole result fresh when υ immutable. Rule 3. |
| `in` | needle `CastString`; haystack dict (key test) or `CastString` (substring) (6608) | `({v} str -- bool)`, `(str str -- bool)` | `({} key -- bool)`, `(key key -- bool)` | Rule 3 (`{}`), Rule 1 (path/int accepted on both sides). Candidates differ by the haystack's kind. |
| `get` | key `CastString`; dict → `Maybe[value]`; GridRow → `Maybe[cell]`; Grid/GridView → `Maybe[new column list]`; missing → none (8368) | `({v} str -- Maybe[v])`, `(GridRow str -- Maybe[t])`, `(Grid \| GridView str -- Maybe[[t]])` | SPECIAL, not registered | Old GridRow/Grid forms had a free `t` (Rule 4). See "get and the getter" below. |
| `:name` getter | same as `get` with the literal name (`processGetter`, 2077) | `lookupGetterValueType` (TypeCheckProgram.go:858, 2028) | SPECIAL (core: "getters" unsupported, TypeCore.go:386) | See below. |
| `getDef` | dict only (not GridRow); key `CastString`; default any; returns the stored value or the default (8402) | `({v} str v -- v)` | SPECIAL, not registered | Runtime key: `(D key d -- υ(D) ⊔ d)`. Literal key ℓ: required → `τ`; optional/deletable → `τ ⊔ d`; absent → `d`; open → `unknown`. Error when the join does not exist (e.g. `[int]` and `[str]`). Shared output. Rule 3. Note the key is the SECOND item, so a "literal directly before the word" elaboration does not reach it. |
| `set` | dict only; key `CastString`; writes in place; returns the dict (8426) | `({v} str v -- {v})` | `({str: a} key a -- {str: a})`, `keeps`; partial: literal key | Rule 1 (key). Literal-key forms SPECIAL: *Set* (declared field, or `*: T` remainder, value ≤ field type, any dict) and *Set-Fresh* (fresh dict: label becomes `ℓ: τ`, other labels unchanged). Set-Fresh is how `{} "a" 1 set` builds a dict, today "unsupported". Runtime key on a shape whose every label is writable at one type (`{*: T}` with all declared fields optional at `T`) is sound (Set-Key) but not expressible. |
| `setd` | as `set`, pushes nothing (8426) | `({v} str v -- )` | `({str: a} key a -- )`; partial: literal key | Same as `set`. |
| `len` (dict/grid forms) | list, str, path, Grid (rows), GridView (rows), GridRow (columns), dict (6155) | `({v} -- int)`, `(… Grid \| GridView \| GridRow -- int)` | add `({} -- int)`, `(Grid \| GridView \| GridRow -- int)` | Rule 3. Partial mark for len removed. |
| `map` (dict) | quote on a child stack per value, exactly one result; new dict, same keys (10419) | `({v} (v -- u) -- {u})` | SPECIAL; partial stays | `(D (υ(D) -- b) -- {str: b})`, child, new dict fresh when `b` immutable. Rule 3. (Could keep D's labels, `{ℓ: b ...}`; `{str: b}` is enough.) |
| `map` (grid) | Grid/GridView; quote on a child stack per GridRow, takes the TOP result only (empty is an error); first row's result decides the columns (10311) | `(Grid \| GridView (GridRow -- {v}) -- Grid)` | add `(Grid \| GridView (GridRow -- {} \| GridRow) -- Grid)` | Runtime also accepts a GridRow result; old free `v` gone. Schema SPECIAL (below). Seed's newList leaves the Grid shared (safe). |
| `filter` (dict) | child stack, exactly one bool; new dict of the kept entries (10173) | `({v} (v -- bool) -- {v})` | SPECIAL; partial stays | `(D (υ(D) -- bool) -- {str: υ(D)})`, child, fresh when υ immutable. Rule 3. |
| `filter` (grid) | quote per GridRow, takes the top result, must be bool; returns a GridView over the SAME source grid (10204) | `(Grid \| GridView (GridRow -- bool) -- GridView)` | add same | Output is a view of the input's grid: shared (newList leaves it shared, correct). |
| `each` (grid) | Grid/GridView, quote per GridRow on a child stack; leftovers discarded (10519). No dict form exists. | `(Grid \| GridView (GridRow -- ) -- )` | add same | The seed's partial text ("dict and grid forms") is wrong: there is no dict `each`. Partial entry removed. |
| `reverse` (grid) | Grid/GridView → NEW Grid (8323) | `(Grid \| GridView -- GridView)` | add `(Grid \| GridView -- Grid)` | Old output kind wrong. Schema unchanged. Fresh when cols immutable (left shared by newList). |
| `sortBy` | Grid/GridView; spec is an MShellString (only) or a list of `CastString`; non-empty; → NEW Grid (8223). No list form. | `(Grid \| GridView str \| [str] -- GridView)` | `(Grid \| GridView str \| [str] -- Grid)` | Old output kind wrong. List narrowed to `[str]` (Unsure 2). Fresh when cols immutable (left shared). |
| `sortByCmp` (grid) | Grid/GridView; comparator `(GridRow GridRow -- int)` on a child stack, top result; → NEW Grid (10988) | `(Grid …)-- Grid`, `(GridView … -- GridView)` | add `(Grid \| GridView (GridRow GridRow -- int) -- Grid)`, `child` | Not on my list; done because it is a grid form. GridView input gives a Grid, not a GridView. List form sorts IN PLACE and returns the receiver (for the list agent: `keeps`, not newList). |
| `extend` (grid) | receiver Grid or GridView, source Grid or GridView; same column-name sets; appends rows IN PLACE; returns the receiver (GridView receiver: its source grid grows and the view gets the new indices) (10696, `extendGrid` 2732) | `(Grid \| GridView Grid \| GridView -- Grid)` | add `(Grid Grid \| GridView -- Grid)`, `(GridView Grid \| GridView -- GridView)`, `keeps` | Old output wrong for a GridView receiver. Type change in place: SPECIAL (below). |
| `uniq` | list only (10054) | `([t] -- [t])` | none here | No dict/grid form. |
| `+` (grid) | Grid/GridView + Grid/GridView → new Grid (`concatGrids` 2569, PLUS 12465) | seed: `(Grid \| GridView Grid \| GridView -- Grid)` | unchanged | Correct. Fresh when cols immutable (left shared). Schema SPECIAL. No `-` grid form exists. |
| `join` (grid) | when the top is a quote: `grid grid (left key) (right key) join`, inner join (6331, `executeGridJoin` 3066) | `(Grid Grid (GridRow -- k) (GridRow -- k) -- Grid)` + `tryGridJoin` | add `(Grid \| GridView Grid \| GridView (GridRow -- a) (GridRow -- b) -- Grid)`, `child` on join | Runtime takes GridViews too; the two key types are independent (keys compare by encoding). Key: scalar, Maybe (none never matches), or a list of non-list scalars; dict/grid → runtime error. Schema SPECIAL. |
| `leftJoin`, `outerJoin` | as `join`; unmatched cells are `none` (9403, 9408) | as `join` | `(Grid \| GridView Grid \| GridView (GridRow -- a) (GridRow -- b) -- Grid)`, `child` | `innerJoin`/`rightJoin` in the old table do not exist at runtime. |
| `gridRows` | Grid, GridView (8486) | `(Grid \| GridView -- int)` | same | — |
| `gridCols` | Grid, GridView; new `[str]` (8501) | `(Grid \| GridView -- [str])` | `(Grid \| GridView -- new [str])` | Output new. |
| `gridMeta` | Grid, GridView; `Maybe` of the grid's meta dict itself (8523) | `(Grid \| GridView -- Maybe[{v}])` | `(Grid \| GridView -- Maybe[{}])` | Rule 4 (free `v`). The dict is shared with whatever it came from: `derive` and grid `groupBy` store the user's dict by reference, and projections share it. Only the read-only `{}` is sound. |
| `gridColMeta` | Grid/GridView, column `CastString`; missing column → error (8545) | `(Grid \| GridView str -- Maybe[{v}])` | `(Grid \| GridView key -- Maybe[{}])` | As `gridMeta`. |
| `gridCol` | Grid/GridView, `CastString` name; missing column → runtime ERROR (not none); new list (8578) | `(Grid \| GridView str -- [t])` | SPECIAL, not registered | Rule 4. Literal `c ∈ S` → `[T_c]` newList; `c ∉ S` → static error; runtime name → `[υ(S)]`; unknown schema → `[unknown]`. |
| `gridValues` | Grid/GridView → new list of new row lists (8615) | `(Grid \| GridView -- [[t]])` | SPECIAL, not registered | Rule 4. `[[υ(S)]]`, fresh (whole tree new) when υ immutable; unknown schema → `[[unknown]]`. |
| `toDict` | GridRow only → new dict of the row's cells (8646) | `(GridRow -- {v})` | SPECIAL, not registered | Rule 4. `GridRow{S}` → new exact shape `{c_i: T_i \| exact}`, fresh when all `T_i` immutable; unknown schema → a new `{str: unknown}`. |
| `gridCompact` | Grid → the SAME grid; GridView → new grid of the view's rows (8696) | `(Grid \| GridView -- Grid)` | `(Grid -- Grid)`, `(GridView -- Grid)`, `keeps` | Grid form is identity, so it keeps freshness. Schema unchanged. |
| `select` | Grid/GridView, `[CastString]`, no duplicates, all must exist → new grid (8952) | `(Grid \| GridView [str] -- Grid)` | same | List narrowed to `[str]` (Unsure 2). Schema SPECIAL. Fresh when cols immutable (left shared). |
| `exclude` | as `select`; names must exist (8986) | `(Grid \| GridView [str] -- Grid)` | same | As `select`. |
| `derive` | `grid name meta quote`: Grid/GridView, `CastString` name (must not exist), meta any dict (stored by reference), quote per GridRow of the SOURCE, exactly one result, any kind → new grid (9025) | `(Grid \| GridView str {v} (GridRow -- t) -- Grid)` | `(Grid \| GridView key {} (GridRow -- a) -- Grid)`, `child` | `{v}` rejected shapes and claimed a writable view; meta is only read back as `{}`. Schema SPECIAL. |
| `updateCol` | `grid name quote`: Grid → column replaced IN PLACE, returns the same grid; GridView → NEW grid of the view's rows. Quote per cell on a child stack, exactly one non-container result (8847) | `(Grid \| GridView str (t -- u) -- Grid)` | SPECIAL, not registered | Old `t` was unrelated to the column (a quote assuming `[int]` cells could append to `[str]` cells). Rule 5. See below. |
| `gridSetCell` | `grid name row value`: Grid only, `CastString` name (must exist), `int` row (negative from end), any value; in place; returns the grid (8659) | `(Grid str int t -- Grid)` | SPECIAL, not registered | Old free `t` let any value into any column. Rule 5. Runtime bug 1. |
| `gridAddCol` | `grid name values`: Grid only, `CastString` name (must not exist); a LIST is the per-row values (length = rows), anything else is repeated in every row; in place; returns the grid (8731) | `(Grid str [t] -- Grid)`, `(Grid str t -- Grid)` | SPECIAL, not registered | The old two candidates both fit a list (Rule 6); the runtime dispatches on the value's kind. Rule 5. |
| `gridRemoveCol` | Grid only, name must exist; in place (8775) | `(Grid str -- Grid)` | SPECIAL, not registered | Rule 5. |
| `gridRenameCol` | `grid old new`: Grid only; old must exist, new must not; in place (8808) | `(Grid str str -- Grid)` | SPECIAL, not registered | Rule 5. |
| `toGrid` | list of lists; first row = headers (`CastString`, no duplicates); every cell `CastString`, stored as str (9605, 2783) | `([t] -- Grid)` | `([[str]] -- new Grid)` | Old accepted anything. Every column is `str`; schema unknown (names are data). Narrowed to `[[str]]` (Unsure 2). |
| `parseCsv` | str (contents) or path (file) (9559) | `(str \| path -- [[str]])` | `(str \| path -- new [[str]])` | Output new. Runtime bug 4. |
| `parseExcel` | path or bytes (9666, Excel.go:153) | `(path \| bytes -- [{name: str, data: [[str \| float \| bool \| Maybe[v]]], hidden: bool, visibility: str}])` | Go-built: `(path \| bytes -- new [{name: str, data: [[str \| float \| bool \| Maybe[⊥]]], hidden: bool, visibility: str \| exact}])` | Rule 4: error cells are always none, so `Maybe[⊥]` (no sig syntax for ⊥, hence Go). Records are exact: the runtime makes exactly these keys. |
| `groupBy` (list) | top is a quote: `[a] (a -- key) groupBy`, child stack, exactly one result, `CastString` → new dict of new lists (9089) | `([t] (t -- str) -- {[t]})` | `([a] (a -- key) -- {str: [a]})`, `child`; partial: grid form | Rule 1 (key result). Fresh when `a` immutable, but no mark says "new dict of new lists", so it is left shared (safe; see Prerequisite 6). |
| `groupBy` (grid) | top is a list: `grid [keys] [specs] groupBy` (9141, `parseGridGroupByAggSpecs` 2923) | hand-built: `(Grid \| GridView [str] [{agg: (GridView -- V), name?: str}] -- Grid)`, `V` per spec | SPECIAL, not registered | See below. |
| `pivot` | `grid [rowKeys] colKey quote`: quote per (row group, column value) on a child stack, exactly one non-container result (9251) | `(Grid \| GridView [str] str (GridView -- t) -- Grid)` + `tryPivot` | `(Grid \| GridView [str] key (GridView -- a) -- Grid)`, `child` | Non-container check SPECIAL (a container is a checked runtime error, so the entry is sound without it). Output schema always unknown. |

## get and the getter (replaces `tryGetLiteralKey`, `lookupGetterValueType`, `getterReceiverInvalid`)

The runtime always pushes a `Maybe` (none when the key is missing).
Receivers: dict, GridRow, Grid, GridView; anything else is a runtime error.

| receiver | literal key ℓ | runtime key |
|---|---|---|
| dict `{F \| ρ}` | ℓ required, optional or deletable: `Maybe[τ]`; absent: `Maybe[⊥]`; open: `Maybe[unknown]` | `Maybe[υ(D)]` |
| `GridRow{S}` | `ℓ ∈ S`: `Maybe[T_ℓ]`; `ℓ ∉ S`: `Maybe[⊥]` (a schema is exact) | `Maybe[υ(S)]` |
| `Grid{S}`, `GridView{S}` | `ℓ ∈ S`: `Maybe[[T_ℓ]]`, the list is new (fresh when `T_ℓ` immutable); `ℓ ∉ S`: `Maybe[⊥]` | `Maybe[[υ(S)]]` |
| unknown schema | `Maybe[unknown]` / `Maybe[[unknown]]` | same |
| union | distribute over members | |
| type variable | wait | |

Outputs are shared except the Grid column list.
What changed from the old checker:
- Old `lookupGetterValueType` gave a fresh type variable for a GridRow column missing from a known schema, and for every unknown receiver (Rule 4). The new answer is `⊥` and `unknown`.
- Old `{str: V}` gave `V` for any name; still right for `{str: V}`, and a shape now gets its label's type.
- `tryGetLiteralKey` fired when the key's TYPE was a string literal, so it worked through variables. The design says the literal must be syntactic. But `set`, `setd`, `getDef`, `gridCol`, `gridSetCell`, `updateCol`, `derive`, the grid column-name words and the key lists of `select`/`exclude`/`groupBy`/`pivot` take their names below the top. "The token directly before the word" is not enough for any of them. Prerequisite 3 proposes slot-level tracking.
- The old `?` flagged an unwrap of `Maybe[⊥]` as always failing; the new checker can keep that lint.

## Grid schemas: what each word needs (SPECIAL)

`S` is the input's schema (ordered columns `c: T`). "Literal" means a name, or a list of names, known at the call (Prerequisite 3); otherwise the result has the unknown schema.
"Fresh only" means: an error on a shared grid, with the `deepCopy` hint (design, "Grids are shapes of columns").

- `gridRows`, `gridCols`, `gridMeta`, `len`: any schema; nothing depends on it.
- `gridColMeta`: literal `c ∉ S` can be a static error (runtime error).
- `gridCol`, `gridValues`, `toDict`, `get`, getter: see the table and the section above.
- `filter`: quote input `GridRow{S}`; output `GridView{S}`.
- `each`: quote input `GridRow{S}`.
- `sortBy`, `reverse`, `sortByCmp`, `gridCompact`: output `Grid{S}`; `sortBy` literal names must be in `S`; `sortByCmp` quote inputs `GridRow{S} GridRow{S}`.
- `select`: literal list → columns in LIST order with their `S` types; a name not in `S` or a repeat is a static error. `exclude`: `S` minus the listed names, in `S` order; names must be in `S`.
- `derive`: quote input `GridRow{S}` (rows of the source); output `S ++ [name: a]` (appended last); literal `name ∈ S` is an error.
- `map` (grid): quote input `GridRow{S}`. The runtime takes column names from the FIRST row's result (dict keys byte-sorted, or a GridRow's columns) and fills the others by name. Output schema:
  - quote output a shape with only required labels and an exact remainder: those labels, byte-sorted, at their types;
  - `GridRow{S'}`: `S'`;
  - anything else (`{str: T}`, open shape, optional labels, a union): unknown.
  On an EMPTY input the runtime returns `S`'s columns instead. With zero rows no cell is ever read, so claiming the quote's schema only risks checked errors (`extend`, `+`, `gridAddCol` name clashes), not wrong cell types. Optional labels are excluded because a missing key leaves a hole (bug 3).
- `+`: column NAME sets equal (any order; else static error); result in LEFT order, each column `T_l ⊔ T_r` (no join → error). Fresh when cols immutable.
- `extend`: name sets equal. Rows are appended in place to the receiver's columns (for a GridView receiver, to its source grid). Column by column: `T_src ≤ T_recv` is a plain write and allowed on any receiver. Otherwise it is a type change (receiver column becomes `T_recv ⊔ T_src`), allowed only on a fresh receiver (Rule 5). A view from `filter` is shared, and its source grid may be seen elsewhere. Unknown schemas: allowed only if both are the same abstract schema (Prerequisite 2).
- `join` / `leftJoin` / `outerJoin`: left quote `GridRow{S_L}`, right quote `GridRow{S_R}`. Output `S_L ++ S_R` (left columns, then right). Overlapping names are a static error (runtime error). `leftJoin`: each right column `T ⊔ Maybe[⊥]`. `outerJoin`: every column `T ⊔ Maybe[⊥]`. Inner `join`: unchanged. Either schema unknown → unknown (the design: "join needs both schemas known"). Fresh when cols immutable.
- `pivot`: quote input `GridView{S}`; the quote output `a` must have no list, dict, Grid, GridView or GridRow member at top level (`isContainerType`; a `Maybe[[int]]` passes the runtime check). Literal `colKey ∈ S` should have type `str` (the runtime requires str cells). Output schema: row-key columns (S types, list order) then one column per distinct colKey value (version-sorted), each `a ⊔ Maybe[⊥]`. The names are data, so always unknown. Fresh when `a` and the row-key types are immutable.
- `groupBy` (grid): keys `[key]` literal, each in `S`. Specs: a list of dicts with keys only `agg` (required quote `(GridView{S} -- t_i)`), `name` (exactly `str`), `meta` (any dict, stored by reference). Any other key is a runtime error, so the shape is closed. Each agg output non-container (as `pivot`). Output: key columns (S types, key order), then one column per spec named `name` or `AggCol<i>` (1-based), type `t_i`. With a literal spec list each spec keeps its own `t_i`; through a variable the list's element type joins them. Not expressible: list invariance plus a per-element quote type. The old checker hand-built it with a generic local to each `agg` quote.
- `updateCol` (`grid c quote`): quote input `T_c` (the column's type, NOT a free generic), output `u` non-container. On a Grid, in place: `u ≤ T_c` is allowed on any grid (output `Grid{S}`, keeps). Otherwise fresh only, output `S[c := u]` (every row is rewritten, so `u`, not a join). On a GridView: new grid `S[c := u]`, allowed on any view, fresh when cols immutable. Runtime-name `c`: input `υ(S)` and output must fit every column, so effectively unknown. Unknown schema: the quote input is an abstract type; fresh only; result unknown.
- `gridSetCell` (`grid c row v`, Grid only): literal `c ∈ S`: `v ≤ T_c` is allowed on any grid (keeps). Otherwise, per the design, fresh only, giving `S[c := T_c ⊔ v]`, but the runtime drops such a write for typed storage (bug 1). Runtime-name `c`: `v` must be ≤ every column type. Unknown schema: fresh only.
- `gridAddCol` (`grid c values`, Grid only), fresh only: the new column's type is the element type `a` when `values : [a]`, else `values`' type. A union with a list member needs the join of the element type and the other members. Output `S ++ [c: T]`; literal `c ∈ S` is an error.
- `gridRemoveCol` (Grid only), fresh only: `S` minus `c`; `c` must be in `S`.
- `gridRenameCol` (`grid old new`, Grid only), fresh only: `old` renamed to `new` at the same position; `old ∈ S`, `new ∉ S`.
- `toGrid`, `parseCsv`: unknown schema (`toGrid`: every column is `str`, which an abstract schema cannot say).

## What the old checker's special cases become

- `tryGridJoin` (TypeChecker.go:938): popped 4, pushed `Grid{0}` whenever two grids and two quotes were there, without checking the quotes. Now ordinary entries (`join` grid form, `leftJoin`, `outerJoin`), plus the join schema rule above.
- `tryPivot` (TypeChecker.go:989): ran the overloads, then rejected a container quote output (silent for an unsolved variable). Now: the `pivot` entry plus the non-container check, shared with `updateCol` and grid `groupBy` aggs. It should apply once the quote's output is solved; a still-generic output can wait.
- `lookupGetterValueType`, `getterReceiverInvalid`, `tryGetLiteralKey` (TypeCheckProgram.go:858, 1972, 2028, 2093): the getter/get table above. A receiver that can never work (list, str, ...) is an error; a type variable waits.

## Prerequisites in the core checker

1. **Schema-polymorphic grid inputs.** `Grid` in a signature resolves to `Grid{0}`, and `Sub` on grids is Refl only (TypeRelations.go:196). So every registered grid entry accepts only unknown-schema grids. That is enough today: the core has no grid literals and nothing produces a known schema. Once known schemas exist, each candidate needs a schema variable `s` shared by its grid positions (`(Grid{s} (GridRow{s} -- bool) -- GridView{s})`), or a table rule saying so.
2. **One `Grid{0}` for all unknown schemas.** The design wants a fresh abstract schema per source; the arena has one TypeId. This stays sound only while a cell read from an unknown-schema grid is always `unknown`/abstract and no write into such a grid is a plain write. `extend` and in-place `updateCol`/`gridSetCell` must then demand a fresh receiver, and `+`/`extend`/`join` cannot match two unknown schemas.
3. **Literal names on stack slots.** A `coreSlot` field for "this `str` is the literal L" (and "this `[str]` is the literal list Ls"). It is set by string literals and list literals of string literals, kept by stack shuffles, and dropped by stores and every other word. The literal-key `get`/`set`/`setd`/`getDef` and every schema rule above need it.
4. **Non-container quote outputs** (`pivot`, `updateCol`, grid `groupBy`): no member of kind list, dict, Grid, GridView, GridRow.
5. **Get-Key read type `υ`** for `values`, `keyValues`, `getDef`, `get`, `map`/`filter` on dicts.
6. **Fresh-when-immutable for new grids and new dicts of new lists.** newList only looks at a list's element type. A mark for "new grid, fresh when every column type is immutable" would cover `+`, `reverse`, `sortBy`, `sortByCmp`, `select`, `exclude`, `derive`, the joins, `map` (grid), `gridCompact` on a view, and `updateCol` on a view. A mark for "new dict of new lists" would cover list `groupBy`. Until then they are shared, which is safe.

## Runtime behavior that looks like a bug

1. `gridSetCell` silently drops a value whose kind does not match the column's typed storage (MShellObject.go:2280). Examples: an int into a float column, a str into an int column. It even drops a type-correct write: a column typed `int | str` whose cells were all ints is stored as ints, and setting `"x"` does nothing.
2. `optimizeColumnStorage` (5794): the `MShellInt` case does not clear `allFloats`, so a column mixing ints and floats is stored as floats, and its ints read back as floats. Nil cells become `0`, `0.0` or `""`.
3. Grid `map` (10366): columns come from the first row only. A later row missing a key leaves a nil cell (then `0`/`""`, or a Go nil in a generic column). Extra keys are dropped. A later result that is neither dict nor GridRow is ignored. An empty input returns the source's columns.
4. `parseCsv` (9568) has no default case: any input other than str/path leaves `reader` nil and panics.
5. Child-stack arity is inconsistent. Grid `map`, grid `filter`, `sortByCmp` and `each` take the top result and ignore leftovers. List/dict `map` and `filter`, `updateCol`, `derive`, `pivot`, `groupBy` and the joins require exactly one.
6. `leftJoin`/`outerJoin`/`pivot` fill missing cells with `none` but store matched values bare, so a column is `T ⊔ Maybe[⊥]`. When `T` is itself a `Maybe`, "unmatched" and "matched a none" cannot be told apart.
7. `extend` on a GridView appends rows to the view's source grid, visible through every other view and variable holding that grid.
8. Name arguments are inconsistent. `sortBy`'s single spec must be an MShellString, while its list items and every other column name take path/int too. A groupBy spec `name` must be a str.
9. Dict `map`/`filter` iterate a Go map, so the quote's side effects run in random order.

## Old-table entries that do not match the runtime

`innerJoin` and `rightJoin` do not exist.
`sortBy`, `reverse` and `sortByCmp` on grids return a Grid, not a GridView.
`extend` with a GridView receiver returns the GridView.
The seed's partial text claims a dict form of `each`; there is none.
The design names `del` as a dict write with a runtime key, but `del` (6476) handles only lists.

## Unsure

1. **Key type.** I used `str | path | int` (Rule 1: `CastString` accepts all three). The seed's `readFile` uses `CastString` too and says `str | path`. One convention should be chosen for the whole table.
2. **Name lists** are `[str]` (and `toGrid` `[[str]]`). The runtime also accepts `[path]`, `[int]` and mixed lists. Adding `[key]` candidates would make two candidates fit a fresh `["a"]` (Rule 6), so I narrowed.
3. **Literal `get` on a required field.** The design's *Get* rule gives `τ`, but the runtime `get`/`:ℓ` gives `Maybe[τ]` (always `just`). I wrote `Maybe[τ]`; the design text ("`:a?` cannot fail") agrees, but the rule as written does not.
4. **`υ` for an open remainder, or when no join exists.** The design says "the unknown type" in one place (Runtime keys) and "a fresh abstract `k`" in another (Unknown contents). For a read either is sound. `unknown` (top, `TidUnknown`) needs no escape check. When the labels have no join (e.g. `{a: [int], b: [str]}`), `unknown` is also a sound answer; an error may be friendlier.
5. **Metadata as `{}`.** It is sound, but reading a meta value gives `Maybe[unknown]`, and a shared operand cannot be narrowed with `tryAs` without `deepCopy`. `Json` would be unsound, because `derive` and `groupBy` store the user's own dict by reference.
6. **Type-changing `gridSetCell`.** The design allows it on a fresh grid, but the runtime cannot do it (bug 1). Until the runtime widens storage, it may be better to require `v ≤ T_c` always.
7. **`{str: unknown}` and `Maybe[⊥]`** have no signature syntax. That is why `parseExcel` is built in Go and `toDict` on an unknown schema is SPECIAL.
8. **groupBy (grid) spec shape.** `meta?: {}` as a written optional field needs an equal field type in the argument (S2), so only a fresh literal spec list fits after retyping. A stored spec list `[{agg: ..., meta: {unit: "kg"}}]` would be rejected.
