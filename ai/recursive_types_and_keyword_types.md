# False positive in `mre.msh`: bare `dict` in signatures, and typing `parseHtml`

Date: 2026-09-11. Status: implemented on branch `recursive-named-types` (see "What was implemented" at the end).

## The repro

```mshell
def childCount (dict -- int)
      :children? len
end

"<p>x</p>" parseHtml childCount wl
```

`msh --type-check-only mre.msh` reports:

```
type error at line 2, column 8: ':' getter expects a dict, GridRow, Grid, or GridView, got dict
```

## Root cause 1: `dict` is not a type name in signatures

`resolveTypeExpr` (`mshell/TypeExpr.go`) only knows `bytes`, `null`, `path`,
`datetime`, `Grid`, `GridView`, `GridRow`, `Maybe`, declared `type` names, and
the lexer primitives `int`/`float`/`str`/`bool`. Any other bare word inside a
`def` signature falls through to the implicit-generic path. So:

- `(dict -- int)` is resolved as `(a -- int)` with a generic literally named `dict`.
- `rigidDefSig` turns that generic into a rigid type for the body check.
- `getterReceiverInvalid` has no `TKRigid` case, so `:children?` on it is an error,
  printed with the generic's name: "got dict".

Evidence beyond the repro (all run against the current `msh` binary):

| Program | Result | Why |
|---|---|---|
| `def f (dict -- int) drop 1 end  1 f` | passes | `dict` is a generic, so an int flows in. False negative. |
| `def f (list -- int) len end  [1 2] f` | fails, "len ... stack: list" | `list` is also a generic. |
| `def f (dict -- int) len end` | fails, "len ... stack: dict" | same |

The stdlib has the same latent problem. `htmlDescendents (dict -- [dict])`,
`htmlDescendentsAcc`, and `findByTag` in `lib/std.msh` type-check only because
`RegisterStdlibSigs` registers signatures and never checks stdlib bodies.
Copying the body of `htmlDescendentsAcc` into a user file fails on
`"children" get?` with "no matching overload for 'get'".

## Root cause 2: `parseHtml` is typed as `{v}`, which is not what the runtime returns

`nodeToDict` (`mshell/MShellObject.go`) always builds the same record:

```
{ tag: str, attr: {str: str}, children: [<same record>], text: str }
```

The registered signature is `(str | path -- {v})`, a string-keyed dict with a
single value type `v`. Consequences:

- `parseHtml as {tag: str, children: [{tag: str}]}` is rejected: "cannot cast
  {str: T0} to ...". `unifyDictToShape` must unify one `T0` with both `str` and a
  list, which cannot succeed. So the documented "cast to a shape" escape hatch
  does not work for HTML nodes at all.
- At top level, `parseHtml :children? (:tag?) map len` passes only because the
  free `v` binds to whatever the next word needs. Inside a def the same value type
  is rigid and nothing works. The checker is not modelling the value; it is
  guessing.
- Even with root cause 1 fixed by mapping `dict` to `{str: a}`, `mre.msh` would
  still fail at `len`, only with a better message. Fixing the keyword does not
  fix the repro. The repro needs a real type for an HTML node.

## Root cause 3: recursive `type` declarations do not work

`type Node = {tag: str, children: [Node]}` fails with "unknown type 'Node'".
Mutual recursion (`type A = {b: B}  type B = {a: A}`) fails the same way.
`CheckProgram` resolves each body and then calls `DeclareType`; the placeholder
scheme in `ai/type_checker.md` ("Top-level item processing order", steps 2 and 3)
was never implemented.

This is the piece that matters for `parseHtml`. The earlier decision to reject a
recursive `Json` type (memory: json-typing-via-cast) rested on the checker not
knowing a JSON file's shape. That argument does not apply here: `parseHtml`'s
shape is fixed by `nodeToDict`, so the checker knows it exactly. Same for any
future tree-shaped builtin (a file tree, a TOML/YAML AST, an XML parser).

## Recommendation

Three changes, in this order. The first is small and fixes the misleading
message and the false negative. The second is the general solution. The third
applies it to `parseHtml`.

### 1. Make signature keywords real types (small)

In `resolveTypeExpr`, before the implicit-generic fallback:

- `dict` -> `{str: a}` with a fresh generic `a` per occurrence.
- `list` -> `[a]` with a fresh generic per occurrence.
- `quotation`, `maybe`, `date`, `binary`: either map (`date` -> datetime,
  `binary` -> bytes) or reject with a hint. `quotation` and `maybe` without
  arguments should be rejected with a hint pointing at `( -- )` and `Maybe[T]`.

Also fix `typeKeywordTokenType` so `dict` and `list` match arms narrow to
`{str: fresh}` and `[fresh]` instead of being rejected.

Guard the implicit-generic path: a name that is a lexer or match keyword must
never become a generic. This is what silently produced "got dict".

This does not make `mre.msh` pass. With `dict` meaning `{str: a}`, `:children?`
yields a rigid `a` and `len` on it is still an error, which is correct: nothing
in that signature says the value is a list. Documented message should say so.

### 2. Implement recursive named types with a brand as the knot (medium)

Mechanism, using existing pieces:

1. Pre-pass over `MShellTypeDecl` items: for each name, reserve a placeholder
   node `MakeBrand(nameId, TidNothing)` and put it in `typeEnv`. Duplicate names
   are already an error, so the `(brand, 0)` hashcons key is unique.
2. Resolve each body with the names visible. A self-reference resolves to the
   placeholder id.
3. Patch the placeholder in place: `arena.nodes[placeholder].B = body`. The
   brand node is now the recursion knot; the body shape refers back to the brand
   id, never to itself directly.
4. Non-recursive declarations keep today's behaviour exactly (branded union or
   `TKBrand` over the body) by resolving to `brandify(body)` when the placeholder
   was not referenced. This is detectable with `walkTypeVars`-style traversal or
   a "referenced" flag set in `resolveTypeExpr`.

Recursive unions (`type Tree = int | {kids: [Tree]}`) become `TKBrand(Tree,
union)` rather than a branded union. That changes nothing for unification
(brand ids compare first) but it is a visible difference in `FormatType` and
`match`; decide whether that is acceptable or whether `brandify` should learn to
patch a branded union in place too.

Cycle safety. Every structural walker currently recurses into `TKBrand.B`:
`Subst.Apply`/`mapType`, `walkTypeVars`, `occurs`, `renameVars`, `Instantiate`,
`FormatType`, `unify`, `castOk`/`acceptsAs`, and the exhaustiveness code. The
cheap and sound rule:

- A declared type's body is closed. `resolveTypeExpr` with a nil context rejects
  any unknown name, so a `type` body can contain no type variables.
- Therefore every variable-oriented walker (`Apply`, `occurs`, `walkTypeVars`,
  `renameVars`, `Instantiate`) may return immediately at a `TKBrand` whose brand
  id is a declared type. No descent, no cycle. Builtin brands created in Go must
  follow the same rule (they are closed too).
- `unify` on two `TKBrand` nodes compares ids; equal ids share one body, so it
  can return true without descending. Different ids fail at the brand. The only
  descent into a brand body is one layer via `underlying` (casts) and via
  `lookupGetterValueType` (getters). Both are finite by construction.
- `FormatType` should print the brand name only (`HtmlNode`), or the body with
  a depth cap of one. Today it prints `Node({...})`, which would loop.

Add a `TypeArena` helper `IsDeclaredBrand(id)` (a set of declared brand ids) so
the early-return rule is one line per walker. Add tests: self-recursive shape,
mutual recursion, recursive union, cast into and out of a recursive type,
getter through the knot two levels deep, `FormatType` termination.

### 3. Give `parseHtml` its real type (small once 2 exists)

Declare a built-in named type in the checker, next to `Grid`:

```
type HtmlNode = {tag: str, attr: {str: str}, children: [HtmlNode], text: str}
```

Register it in Go (so `TypeBuiltins.go` signature strings can name it), add it to
the reserved-name list, and set:

```
parseHtml (str | path -- HtmlNode)
```

Update the stdlib helpers to `(HtmlNode -- [HtmlNode])`,
`(HtmlNode [HtmlNode] -- [HtmlNode])`, `(HtmlNode str -- [HtmlNode])`, and
the docs (`doc/mshell.md`, the type-system page, `parseHtml` entry). Then
`mre.msh` is written as `(HtmlNode -- int)` and checks precisely:
`:children?` is `[HtmlNode]` and `len` accepts it.

Brand friction: `HtmlNode` is nominal, so a hand-written dict literal must be
cast with `as HtmlNode` to be used where one is expected. That is consistent
with the newtype rule already in the design and is rare in practice, since nodes
come from `parseHtml`. If it turns out to bite, the alternative is a transparent
alias kind (`TKAlias`) that `unify` unfolds one layer with a visited-pair set.
Start with the brand; it reuses everything that exists.

Other builtins with the same "fixed shape but typed `{v}`" smell, worth a look
after this lands: `parseLinkHeader (str -- [{v}])`, `keyValues`, `toDict`.
`parseJson` stays `( -- t)` per the earlier decision.

### 4. Follow-up: check stdlib bodies

`RegisterStdlibSigs` only registers signatures. The design says the stdlib is
checked on every run. Once 1 and 3 land, turning body checking on for the
stdlib will catch the next `(dict -- ...)` before a user does.

## What was implemented

- `Type.go`: `named` set on the arena, `NewBrandPlaceholder`/`PatchBrand`,
  `NewUnionPlaceholder`/`PatchUnion`. `HtmlNode` added to the reserved names.
- `TypeCast.go`: `declareTypes` does the three-step pre-pass (placeholders,
  resolve and patch, unguarded-cycle DFS). `castOk` sets `casting` so unify
  may tag an unbranded value into a brand at any depth (needed to build a
  value of a recursive type from a literal). Brand-to-brand stays rejected.
- `TypeUnify.go`: the substitution walker and `walkTypeVars` stop at named
  types (closed bodies, no variables inside).
- `TypeError.go`: `FormatType` prints a named type's body once and a
  recursive back-reference by name.
- `TypeExpr.go`: `dict`, `list`, `date`, `binary` resolve to real types in
  signatures; `quotation` and bare `maybe` are rejected with hints.
- `TypeCheckProgram.go`: `list l` / `dict d` in a match on a union bind only
  the members of that kind (`narrowKeywordBinding`).
- `TypeBuiltins.go`: `builtinNamedTypes` declares `HtmlNode`; `parseHtml`
  returns it. `lib/std.msh` html helpers use it.
- Correction to the analysis above: `dict` match arms were never broken; my
  probes used `->` and omitted the arm commas. Declared type names as match
  arms are accepted by the checker but rejected by the runtime ("Unknown
  match pattern literal"), which is a separate pre-existing gap.
- Not done: stdlib bodies are still not type-checked (follow-up 4).
- Reopened 2026-09-11 at the user's request: `parseJson` now returns the
  built-in `Json` instead of a free variable. A first attempt let `as`
  narrow a union to one arm; that was reverted the same day because `as`
  is static only and must never fail. Semantics settled: `as` widens or
  names (including into nested brands at depth), never narrows; narrowing
  external data is `match` on the runtime kind today and `tryAs` (PR #275)
  once it lands. The checker now rejects a declared type name as a match
  arm, since declarations are erased at runtime; #275's validator is the
  natural way to make such arms real later. The brand-to-brand rule is
  explicit in `castOk`.
- Open for the user: the runtime turns every JSON number into a float, so
  the `int` arm of `Json` never occurs at runtime. Either drop it or make
  `parseJson` produce ints for integral numbers (#275 lists this too).
