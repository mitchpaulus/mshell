# tryAs review — outstanding items

Critical review of the `try-as` branch (`7dce9f5 Implement tryAs`), 2026-07-09.
The unbounded-recursion crashes found in this review are already **fixed** on the
branch (depth budget of 1024 in `validateObj`, see `maxTryAsValidateDepth` in
`mshell/TypeExpr.go`, with regression tests in `tests/fail/tryas_cyclic_type.msh`,
`tests/fail/tryas_cyclic_value.msh`, `tests/fail/tryas_depth_exceeded.msh`, and
`tests/success/tryas_depth.msh`). Everything below is still open and needs a
decision.

## 1. The recursive-type contradiction (design decision needed)

The runtime and the checker disagree about whether recursive named types exist.

- Runtime: `tryAs Name` resolves names late through `state.typeDefs`, so
  `type Tree = {value: int, kids?: [Tree]}` validates finite values correctly.
  Verified: `type A = [B]` / `type B = [A]` works at runtime.
- Checker: `CheckProgram` pre-pass 1 resolves each `type` body in file order
  *before* declaring the name, so any self- or forward-reference is
  "unknown type" — recursive types are statically unrepresentable.

Consequence: recursive types (the natural way to type JSON trees, which is the
flagship `tryAs` use case) work only when you *don't* pass `--check-types`.
The two halves should agree. Options:

- **(a)** Register all type names before resolving bodies so the checker admits
  recursion. The arena/TypeId side needs a story for the cycle (a `TidNamed`
  indirection or iterative resolution). Runtime is already safe now that the
  depth budget exists. This is the option that makes the flagship use case work.
- **(b)** Make the runtime reject what the checker rejects: resolve/validate the
  type AST eagerly at `type` declaration time and fail on unknown names. Loses
  recursive types entirely.

## 2. JSON integer footgun passes the checker silently (high)

`parseJson` produces a `float` for every JSON number, so any `int` inside a
`tryAs` target is **always-none** against JSON data — and `--check-types` says
nothing. Verified:

```mshell
"[1, 2]" parseJson tryAs [int]    # always none; checker silent
```

The docs warn about this (good), but the branch already added a checker error
for statically-impossible casts (`5 tryAs str`); this is the same bug class and
the one users will actually hit, since `{count: int}` is what everyone writes
first. Options:

- Checker warning when the cast source is the JSON output union and the target
  contains `int` anywhere.
- Accept integral floats for `int` in `validateObj` — rejected in the branch's
  own comments because validation deliberately mirrors `match` type-arm
  semantics; changing it here without changing `match` creates a new
  inconsistency.
- Make `parseJson` produce `int` for integral numbers — biggest change, fixes
  the root cause, affects existing scripts that assume float.

## 3. Wildcard shapes validate nothing at runtime (medium)

`TypeShapeExpr.validateObj` never reads `a.Wildcard`. Verified inconsistency:

```mshell
{"a": 1, "b": "x"} tryAs {a: float, *: float}   # just  — wildcard ignored
{"a": 1, "b": "x"} tryAs {*: float}             # none  — dict form checks values
```

For static `as` dropping the wildcard was harmless (width subtyping already
allows extra keys — comment at the `TypeShapeExpr` declaration in
`mshell/TypeExpr.go`), but `tryAs` makes runtime claims: a user who writes
`*: float` is asking for enforcement they silently don't get. Options:

- Validate non-field keys against the wildcard type in
  `TypeShapeExpr.validateObj` (my preference: it's what the syntax says).
- Reject wildcard shapes as `tryAs` targets with a clear error (fail closed).

Either is better than the current silent half-validation.

## 4. Quotation targets are half-open (medium)

`tryAs (int -- int)` checks only that the value is a quotation
(`TypeQuoteExpr.validateObj`) — signatures are not verified, so
`("hi" wl) tryAs (int -- int)` produces `just`, and the checker afterward
trusts `Maybe[(int -- int)]` for a value with a different signature. That is a
static-soundness hole shaped like a checked cast. It is documented, but
consider failing closed instead: reject quote types in `tryAs` targets with an
error ("quotation signatures cannot be verified at runtime"), rather than
issuing a certificate the runtime can't back. If kind-only checking is kept,
it stays a documented sharp edge.

## 5. Union arm errors are value-dependent (low)

`1 tryAs int | Bogus` never reports the unknown type `Bogus` (first arm
matches, short-circuit); a value that matches no arm hits the error instead.
So whether a malformed type expression is diagnosed depends on the data. A
one-time eager walk of the type AST at cast time (validating all names/arity)
would make the errors deterministic. Cheap to do now; confusing bug reports
later if skipped.

## 6. Notes, no action required

- `validateObj`'s `case Maybe:` (value, non-pointer) branch is dead code — the
  codebase only ever pushes `*Maybe`. Harmless.
- The `just` result retains the original mutable object; a post-validation
  mutation through an alias (`append` on a still-reachable inner list) can
  invalidate the certified type. Inherent to the language's aliasing
  semantics, not specific to this branch; maybe worth one sentence in the
  docs next to "parse, don't validate".
- Lexer edges verified good: `tryas`, `tryAsX`, `tryA`, `trying` all stay
  LITERAL; bare `tryAs` and `Maybe`-without-argument produce clean parse
  errors.
- `builtinNamedTypes` table (single source for checker resolution + runtime
  check) and the `TypeExpression` interface forcing `validateObj` on every
  node kind are both good structure — keep that pattern for future node kinds.
- Test-coverage suggestions once decisions above land: wildcard-shape
  behavior (whichever way it goes), a quotation-target case, nested
  `Maybe[Maybe[T]]`, and a `tryAs` inside a quotation in `tests/success/`
  (works today — verified `(tryAs int ...) map` — but only top-level uses are
  covered, and quotations dispatch through the separate `evaluateItems` path
  in `mshell/Evaluator.go`).
