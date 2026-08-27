# Checked casts at the JSON boundary (`tryAs`)

Status: implemented on branch `try-as` (2026-07-09). Doc pattern + `tryAs`
both landed. One finding from implementation: `parseJson` maps every JSON
number to `MShellFloat` (Go's default unmarshal), so `tryAs {str: int}` on
JSON counts is always `none` — validate JSON numbers as `float`. Whether
`parseJson` should produce `int` for integral numbers is an open follow-up.

## Problem

Drilling into parsed JSON under the type checker is painful.
Extracting `.packages[].name` took four nested `match` blocks,
because `parseJson` returns the wide union `list|dict|numeric|str|bool|null`
and every union member must be handled at every level.
Each `match dict:` arm proves a fact, uses it once, and throws the proof away —
boilerplate scales with nesting depth.

## What already works (and the doc gap)

The static side of the fix already exists:

```mshell
type Manifest = {packages: [{name: str}]}

"pkgs.json" parseJson as Manifest :packages? (:name?) map
```

This type-checks and runs today. `type_system.inc.html` even recommends the
pattern ("prefer adding a named type and an `as` assertion near the boundary"),
but `doc/mshell.md` — the doc agents actually read — never shows
`parseJson ... as T`. **Cheapest immediate action: document this pattern in
`doc/mshell.md`.**

## The gap: `as` is static-only

`as` is a checker hint with no runtime work (`Evaluator.go`, `MShellAsCast`
case). When the JSON doesn't match the assertion, the failure surfaces late
and badly: `{"packages": [{"name": 42}]}` through the cast above dies with
`Cannot get length of a Float.` at a downstream call site, not at the boundary.

## Design: `tryAs` — parse, don't validate

One new checked cast that returns proof as a more precise type:

```mshell
"pkgs.json" parseJson tryAs Manifest      # ( -- Maybe[Manifest]), recoverable
"pkgs.json" parseJson tryAs Manifest ?    # unwrap-or-die, composes with existing ?
```

- Runtime: structural walk of the value against the reified type expression.
  JSON values are only dicts/lists/scalars, so the validator is a small
  recursive function and structural checking is complete in this domain.
- Success: `just` wrapping the value, statically typed `T`.
  Downstream `:field?` / `map` need no further matching.
- Failure: `none`.

### Naming rationale

- `as?` rejected: `?` consistently means *unwrap* a Maybe (`?`, `:field?`),
  so a Maybe-*returning* `as?` reads backwards.
- `as!` rejected: `!` means *store to variable* (`name!`).
- `tryAs` follows the getter convention: base word returns Maybe,
  existing `?` unwraps. Die-loud form is derived (`tryAs T ?`), not a
  second primitive.

### Division of labor with static `as`

Both stay; the dividing line is trust boundaries:

- `as` — value originates inside typed code: empty/ambiguous literals
  (`[] as [str]`), narrowing inferred unions, branding. Checker can see
  everything; runtime check would verify the statically obvious. Free.
- `tryAs` — value crosses in from outside (parseJson, process output,
  spreadsheets). Only the runtime can know the shape; pay one walk,
  get a `Maybe[T]` proof.

Possible follow-on: once `tryAs` exists, a hint diagnostic when bare `as`
narrows an external-data union (e.g. the parseJson result) to a shape —
"did you mean tryAs?" — while leaving literal-hint uses untouched.

## Deferred (deliberately)

- **Path-precise error messages.** `tryAs ... ?` failing as a bare `none`
  loses ".packages[3].name: expected str, got float". A future Result type
  with an error string is the planned home for this; the validator computes
  the message internally anyway, so a Result-returning variant just exposes
  it. The "no message on bad unwrap" problem is bigger than JSON and is not
  being solved here.
- **`dig` with typed default** (`json ['packages' 0 'name'] "unknown" dig`,
  signature `(json [str|int] a -- a)`, default's type = result type via
  existing generics). Nice for one-off drills without declaring a type.
  If built: no `'*'` wildcard segments (they change the result shape and
  break `a -- a`); `tryAs` + `map` owns extract-many. Conflates missing
  with the sentinel value.
- **Recursive types** (`type Json = int | str | [Json]`) don't work today —
  the name isn't in scope inside its own body. A first-class "any JSON"
  type would need checker work; with checked casts at the boundary it's
  rarely needed.

## Implementation notes

Structure (after review feedback on exhaustiveness): there is no separate
validator file. `TypeExpr.go` defines a `TypeExpression` interface
(`MShellParseItem` + `validateObj`) that every type-expression node must
implement, and the parser productions return it — so a new node kind
without runtime validation does not compile. Built-in named types (bytes,
null, path, datetime, Grid, GridView, GridRow) live in one
`builtinNamedTypes` table consulted by BOTH the checker's resolveTypeExpr
and the runtime validator, so a type cannot be half-added; a name missing
from the table does not exist statically either. `Maybe` (parametric) and
`none` (constructor, not a type) are deliberately special-cased outside
the table.

- Type declarations are currently erased at runtime (`MShellTypeDecl` is
  static-only). `tryAs` needs type expressions reified into the evaluator
  so the validator can walk them.
- The shape language already covers everything JSON needs: shapes, optional
  fields (`name?: T`), unions, `null` vs `none`, `Maybe`.
