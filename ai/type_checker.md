# Type Checker Implementation Plan

Implementation plan for mshell's static type checker.
Reflects the design decisions from a sequence of architectural discussions:
structural unions with optional nominal brands,
no user-defined tagged sums,
`Maybe[T]` as a privileged built-in tagged parametric type,
user-visible overloads,
and a checker that gates execution on type errors but does not (yet) drive runtime dispatch.

A `<fail E>` effect with typed error payloads
and an optional `pure` annotation
are part of the long-term design but **deferred to Phase 2** — see the section at the end.

## Goals and constraints

- **Performance is critical.**
  No interface boxing for types,
  no string-keyed maps in hot paths,
  preallocated stacks,
  hashconsed type representation.
- **Type errors gate execution.**
  If the checker finds errors,
  print them and exit;
  the program does not run.
- **Runtime checks stay as-is for now.**
  The existing per-op arity, bounds, and type checks at runtime are independent of the new checker.
  Once the static analysis is proven robust,
  those runtime checks come out — that's the eventual performance win.
  Until then,
  the runtime is its own safety net.
- **No runtime dispatch binding yet.**
  Overload resolution and other checker results are not yet wired into the evaluator.
  Runtime continues to use whatever dispatch it currently does.
- **Match the user-visible design exactly (V1 scope).**
  Structural unions with brands,
  `Maybe[T]` privileged,
  user-visible overloads with most-specific-first dispatch.
  `<fail E>` and `pure` come in Phase 2.
- **Replace `TypeChecking.go`.**
  Build the new module alongside,
  reach feature parity,
  delete the old one.

## V1 vs Phase 2 — what's in scope

**V1 (this plan, the bulk of what follows):**

- Arena, hashconsing, stack simulation.
- Primitives, lists, dicts, shapes, unions with brands, quotes, generics, `Maybe[T]`,
  grid family (opaque first, schema tracking later).
- Overload dispatch.
- Integration in `Main.go` — type errors gate execution.

**Phase 2 (separate later plan):**

- The `<fail E>` effect, `try:` blocks, branch reconciliation against `TidBottom`.
- `pure` annotation and checking.
- Migration of builtin sigs to declare `<fail>` and `pure` where applicable.

V1 keeps a few **forward-compatibility hooks** so Phase 2 lands cleanly:

- `<fail>`, `pure`, `try`, `fail` are **reserved keywords** in V1, even though nothing parses or uses them.
  Otherwise users will write functions or variables called `fail` and the migration breaks them.
- The parser **may optionally accept** `<fail>` and `pure` annotations in sigs,
  parse them, and store them on the parsed sig — but the checker ignores them.
  This lets users start annotating ahead of enforcement.
- `QuoteSig` carries a `Fail TypeId` field and a `Pure bool` field from V1.
  V1 leaves them at default (`TidNothing`, `false`);
  Phase 2 populates them.
  No struct migration when the time comes.
- `TidBottom` (the divergent / Never type) is reserved as a primitive in V1,
  used by `exit` and infinite loops.
  Phase 2 reuses it for `try:` reconciliation.
- Overload-dispatch specificity rules in V1 are written as
  "specificity is determined by argument types alone (effects considered when added in Phase 2)."
  Keeps the door open without locking in.

## File layout

Five new files under `mshell/`:

- `Type.go` — type representation, the arena, hashconsing.
- `TypeUnify.go` — substitution, unification, generic instantiation.
- `TypeChecker.go` — the main pass, stack simulation.
- `TypeBuiltins.go` — builtin sigs as a contiguous static table.
- `TypeError.go` — error values and formatting.

The existing `TypeChecking.go` stays in place until parity is reached, then is deleted.

## Core data structures

### What "hashconsed arena" means

Two ideas in one phrase, both about how we store types in memory.

**Arena.**
Instead of allocating each type as its own heap object linked by pointers,
we put them all in one big slice ("the arena").
A "type" is then a `uint32` index into that slice.
Passing a type around is passing a 4-byte integer, not a pointer to a struct.
Iterating types is iterating contiguous memory,
which the CPU cache loves.
This is the same trick ECS engines, compilers, and DB engines use to keep tight inner loops fast.

```text
Arena (one slice, all types live here):
  [0] Nothing       <-- TypeId 0
  [1] Bool          <-- TypeId 1
  [2] Int           <-- TypeId 2
  [3] Float
  [4] Str
  ...
  [n] List<Int>     <-- TypeId n  (refers to TypeId 2 for its element)
  [n+1] List<Int>?  -- if we naively built another, this would duplicate [n]
```

**Hashconsing.**
A discipline on top of an arena that says:
*never store the same structural type twice.*
Before adding a new type to the arena,
we check whether an identical one is already there;
if so,
return the existing id instead of allocating a new slot.
The check is done with a hash table keyed on the type's structure
(kind, plus the ids of its components).

So if your program asks for `List<Int>` in five different places,
all five get the same `TypeId`.
There is exactly one `List<Int>` in the arena, ever.

This buys two big things:

1. **Type equality is integer equality.**
   Are these two types the same?
   Compare their `TypeId`s.
   No tree walk, no recursive structural comparison.
   In a checker that asks "is this the same type as that?" thousands of times per program,
   the speedup is enormous.

2. **Memory stays small.**
   A program that mentions `List<Int>` a thousand times
   pays for one `List<Int>` node in the arena, not a thousand.

The cost is one hash-table lookup at construction time.
That cost is paid once per *unique* type in the program,
not once per *use* of a type.
Cheap.

A concrete example:

```text
Build List<Int>:
  Look up key (Kind=List, A=TidInt) in the cons table.
  Not found -> append new node, store id in cons table, return id (say 42).

Build List<Int> again (anywhere in the program):
  Look up the same key.
  Found -> return id 42. No new node allocated.

Now: are these two TypeIds equal?
  id1 == 42, id2 == 42.  Yes. One integer comparison.
```

This is why the design says "comparison is integer equality" —
it's a consequence of hashconsing,
not a separate optimization.

### The arena

Types are `uint32` indices into a hashconsed arena.
Comparison is integer equality.
Storage is contiguous.

```go
type TypeId uint32

const (
    TidNothing TypeId = 0   // sentinel "no type" (used for forward-compat slots)
    TidBool    TypeId = 1
    TidInt     TypeId = 2
    TidFloat   TypeId = 3
    TidStr     TypeId = 4
    TidBytes   TypeId = 5
    TidNone    TypeId = 6
    TidBottom  TypeId = 7   // divergent (exit, infinite loop; reused by Phase 2 try:)
    // composite IDs start here
)

type TypeKind uint8
const (
    TKPrim TypeKind = iota
    TKMaybe     // payload A = inner T
    TKList      // payload A = element T
    TKDict      // A = key T, B = value T
    TKShape     // Extra = index into shapeFields
    TKQuote     // Extra = index into quoteSigs
    TKUnion     // Extra = index into unionMembers; A = brand id (or 0)
    TKBrand     // A = brand id, B = underlying TypeId (newtype-style)
    TKVar       // A = TypeVarId

    // Grid family — built-in like Maybe; see "Grid types" below.
    TKGrid      // Extra = index into gridSchemas (or 0 if unknown)
    TKGridView  // Extra = index into gridSchemas (or 0 if unknown)
    TKGridRow   // Extra = index into gridSchemas (or 0 if unknown)
)

type TypeNode struct {
    Kind  TypeKind
    A     uint32
    B     uint32
    Extra uint32
}

type TypeArena struct {
    nodes []TypeNode
    cons  map[typeKey]TypeId

    shapeFields  [][]ShapeField
    quoteSigs    []QuoteSig
    unionMembers [][]TypeId   // sorted, deduped
    gridSchemas  []GridSchema // column lists for grid family; index 0 = "unknown"
}

type GridSchema struct {
    Columns []GridColumn   // ordered (grids have column order)
}

type GridColumn struct {
    Name NameId
    Type TypeId
}

type ShapeField struct {
    Name NameId
    Type TypeId
}

type QuoteSig struct {
    Inputs   []TypeId
    Outputs  []TypeId
    Fail     TypeId       // Phase 2: TidNothing in V1, populated when <fail> lands
    Pure     bool         // Phase 2: false in V1
    Generics []TypeVarId
}
```

Hashconsing key is whatever uniquely identifies a structural type:
sorted shape fields,
sorted union arms,
input/output spans plus fail/pure flags for quotes
(the flags are part of the key from V1 even though they're always default,
so Phase 2 doesn't need to redo hashconsing).
Constructed types look up in `cons` first;
identical types share an id.

### Names

Every name is interned:

```go
type NameId uint32

type NameTable struct {
    ids   map[string]NameId
    names []string
}
```

Builtins, user definitions, field names, brand names, type variables — all `NameId`.
Comparison is integer equality.
Strings only appear in error formatting.

### The stack

The checker simulates the runtime stack at the type level:

```go
type TypeStack struct {
    items []TypeId   // top = items[len-1]
}
```

Reuse one `TypeStack` across the whole-program check;
reset between top-level items.

### Checker state

```go
type Checker struct {
    arena     *TypeArena
    names     *NameTable

    stack     TypeStack
    vars      VarEnv         // bound variables -> current type
    subst     Substitution
    errors    []TypeError

    sigs      []QuoteSig         // contiguous; defs and builtins both live here
    overloads map[NameId][]uint32

    currentFn *FnContext   // function we're checking right now
}

type VarEnv struct {
    bound map[NameId]TypeId   // current scope's bindings
    // For branch reconciliation, the checker snapshots and restores VarEnv
    // around each arm and then unions the results.
}

type FnContext struct {
    Sig QuoteSig
    // Phase 2 will add DeclaredFail / DeclaredPure / InferredFail / InferredPure
}
```

## Core operation: applySig

The hot path.
Given a sig and the current stack, check arity, unify each input, pop, push outputs.

```go
func (c *Checker) applySig(sig QuoteSig, callSite Position) {
    if len(c.stack.items) < len(sig.Inputs) {
        c.errStackUnderflow(callSite, sig)
        return
    }
    base := len(c.stack.items) - len(sig.Inputs)
    for i, want := range sig.Inputs {
        got := c.stack.items[base+i]
        if !c.unify(got, want) {
            c.errType(callSite, want, got, i)
        }
    }
    c.stack.items = c.stack.items[:base]
    for _, out := range sig.Outputs {
        c.stack.items = append(c.stack.items, c.subst.Apply(out))
    }
    // Phase 2: if sig.Fail != TidNothing, record the fail effect on currentFn
    //          and either propagate or require a try: handler.
}
```

## Unification

Standard Hindley-Milner with extras for unions and brands.
Substitution is an array indexed by `TypeVarId`:

```go
type Substitution struct {
    bound []TypeId
}

func (s *Substitution) Apply(t TypeId) TypeId
func (s *Substitution) Bind(v TypeVarId, t TypeId) bool   // includes occurs check
```

Unification cases:

- Same id — trivial.
- One is `TKVar` — bind it.
- One is `TidBottom` — unify with anything.
  In V1 this is reached via `exit` or infinite loops;
  Phase 2 also produces `TidBottom` from divergent fail-arms in `try:` blocks.
- Both `TKMaybe` — recurse on inner.
- Both `TKList` — recurse on element.
- Both `TKDict` — recurse on K, V.
- Both `TKShape` — width-subtype rule:
  every required field of expected must appear in actual with unifiable type.
  Extras allowed in actual (covariant width).
- Both `TKQuote` — unify input/output spans.
  In V1 the fail/pure flags are always default and trivially match;
  Phase 2 adds compatibility checks.
- Both `TKUnion` — subset relation.
  Actual must be a subset of expected (or exactly equal).
- `TKBrand` — only unifies with the same brand id (nominal).
  Pattern-match sites look through the brand to the underlying type.
- Anything else — type error.

Generic instantiation at call sites:
clone the sig,
fresh-rename its `TypeVarId`s,
run unification.

## Grid types

`Grid`, `GridView`, and `GridRow` are first-class built-ins,
treated like `Maybe[T]` —
their own runtime representation,
their own type kinds in the arena,
not derivable from primitives.

Three kinds:

- **`TKGrid`** — a materialized table.
- **`TKGridView`** — a lazy view over a grid (filtered, projected, sliced, etc.).
  Distinct from `TKGrid` for the same reason `Customer` differs from `Employee` even with the same fields:
  the runtime distinguishes them and many builtins accept the union (`Grid|GridView`).
- **`TKGridRow`** — a single row, accessed by column name.

The interesting part is **column schema** —
the ordered list of (column-name, column-type) pairs.
Schema is what would let the checker prove,
say,
that `"name" gridCol` is valid because the grid has a `name` column of type `str`.

We do this in two phases.

### Phase one: opaque grids

Treat grids as schema-less.
The kind alone says "this is a grid";
column-level structure is not tracked at the type level.
Behavior in this phase:

- `sortBy ( Grid|GridView str|[str] -- Grid )` — accepts any column name string;
  the runtime catches column-not-found.
- `gridCol ( Grid|GridView str -- [T] )` — returns a permissive value type
  (effectively a fresh type variable resolved by usage,
  or `any` if we add such a thing).
- `GridRow` field access — same: returns a permissive type.
- Two `TKGrid` types are equal iff both have schema index 0 (unknown).

This is enough to make grids first-class for the type checker
and to type the wide-grained operations (`+`, `extend`, `reverse`, `sortBy`, `join`).
It does not catch column-name typos or column-type mismatches statically.

### Phase two: schema tracking

When the schema is known
(literal grid construction,
type annotations,
result of `parseCsv` with declared types),
carry the column list as side-table data via the grid kind's `Extra` slot.
With schema:

- `sortBy "colName"` checks that `"colName"` is in the schema.
- `gridCol "colName"` returns the column's declared type,
  not a permissive value.
- `GridRow` field access returns the column type from the row's grid schema.
- `join` keys can be checked across the two grids.

Two grid types with schema are equal iff
their schemas have the same columns in the same order with the same types.
Width subtyping does **not** apply —
a grid's column set is exact,
unlike a record where extras are allowed.
Adding or removing a column produces a different type.

Operations that change schema
(`gridAddCol`, `gridRemoveCol`, `gridRenameCol`, projection)
need to compute the result schema from the inputs.
That's straightforward when both schema and operation arguments are statically known;
when the operation argument is dynamic
(a column name from a runtime value),
fall back to schema-unknown for the result.

`TKGridView` and `TKGrid` carry schemas the same way.
A view over a grid has the same schema as the underlying grid,
modulo any projection.

(Both grid sub-phases are within V1 — phase one with the rest of the composites,
phase two as a follow-on once unification is in.)

## Maybe[T] handling

`TKMaybe` is its own kind, not modeled as `T | none`.
The arena has a node type for it.
The parser wires `just`/`none` to dedicated builtin sigs:

```go
// just : ( T -- Maybe[T] )    — T fresh per call site
// none : ( -- Maybe[T] )      — T fresh; resolved by context
```

Match-arm dispatch reads the static type of the matched value:

```go
func (c *Checker) checkMatch(matched TypeId, arms []ParsedArm) {
    node := c.arena.nodes[matched]
    switch node.Kind {
    case TKMaybe:
        c.checkMaybeMatch(matched, arms)   // expects 'just @v' / 'none' arms
    case TKUnion:
        c.checkUnionMatch(matched, arms)   // expects type-arm heads
    case TKShape:
        c.checkShapeMatch(matched, arms)   // dict pattern arms
    // ...
    }
}
```

Lifting `T → Maybe[T]` requires explicit `just`.
A bare `5` will not flow into a `Maybe[int]` slot.

## Overload dispatch

Each name maps to a list of candidate sig indices.
At a call site:

1. Snapshot stack and substitution.
2. Filter candidates by arity match.
3. For each, try unification with the current stack top.
4. Rank surviving candidates by specificity:
   - Fewer type variables → more specific.
   - Concrete shape > shape with vars > pure type variable.
   - Brand match > unbranded structural match.
   - (Phase 2 adds: effect signatures considered.)
5. Pick the unique most-specific.
   Ambiguity is a static error,
   listing all surviving candidates.

This is per-call-site work,
but each call has a small number of overloads in practice.
If profiling shows it's hot,
precompute dispatch tables for monomorphic call sites.

## Builtins as a static table

```go
// TypeBuiltins.go
var BuiltinSigs = []QuoteSig{
    // index 0 reserved
    1: {Inputs: []TypeId{TidInt, TidInt}, Outputs: []TypeId{TidInt}}, // +
    2: {Inputs: []TypeId{TidStr},          Outputs: []TypeId{TidStr}}, // readFile (V1: no <fail>; Phase 2 adds Fail: ...)
    // ...
}

var BuiltinByName = map[NameId]uint32{ /* NameId -> index */ }
```

Token-level fast dispatch:
where `processIff` and friends live in `Evaluator.go`,
the checker has a parallel switch keyed on `TokenType`.
For arithmetic the sig is fixed.
For things like `iff` whose arity depends on a value's type,
the same runtime-style dispatch happens at typecheck time,
but on types instead of values.

## Error reporting

Errors collect in a slice;
no `fmt.Sprintf` in the hot path:

```go
type TypeError struct {
    Kind     TypeErrorKind
    Pos      Position
    Expected TypeId
    Actual   TypeId
    Hint     string
}
```

Format to string only at print time,
when traversing the arena to build human-readable type names.

## Integration

The checker runs as a parse-tree pass between parser and evaluator in `Main.go`.
On any type errors,
print them to stderr and exit non-zero.
The evaluator only runs when checking succeeds.

```go
errs := TypeCheck(file.Items, allDefinitions)
if len(errs) > 0 {
    for _, e := range errs {
        fmt.Fprintln(os.Stderr, e.Format(arena, names))
    }
    os.Exit(1)
}
// evaluator runs only when type checking passes
```

Existing tests with `.stderr` fixtures expect a specific runtime error message.
If the checker now catches the same case statically,
the fixture has to be updated to the new (checker) message,
or the test expressed in a way the checker can't statically detect.
This will require a sweep across the test suite when integration lands.

For staged rollout —
to avoid one giant test-fixture migration —
consider a `--no-typecheck` CLI flag (or env var) that skips the pass entirely.
Useful during development of the checker;
remove once the checker is the default and the suite is fully migrated.

## Decided rules

These were open questions that have been resolved.
Listed here so the implementation has a single source of truth.

### Cast syntax

The cast expression is **`<value> as <Type>`**.
Postfix word `as` followed by a type expression.
No sigil form, no `::`.

```
42 as Result
{ name: "x", age: 30 } as Person
parsed as Result[int, str]
```

`as` is a reserved keyword.
The cast is purely static —
it changes how the checker views the value,
no runtime work.

### Generic type variables

In a type position,
**any identifier that is not a known type name (built-in or declared) and is not a reserved keyword is treated as a generic type variable**.
Haskell-convention.

```
def first2  ( [T] -- T T )           # T is generic
def map     ( [T] (T -- U) -- [U] )  # T and U are generic
def first2  ( [int] -- int int )     # int is the built-in, not generic
```

No explicit `forall T.` introducer needed.
Each type variable is fresh per call site.
Multiple uses of the same variable name within one sig must unify
(`(T T -- T)` requires both inputs to be the same type).

### Branch reconciliation

All branching constructs (`if`/`else`, `match`, and Phase 2 `try:`) share one rule:

- **Stack sizes across branches must match exactly.**
  This is a hard constraint —
  the checker requires statically known stack sizes everywhere.
- **Stack types across branches need not match.**
  When branches produce different types in the same stack slot,
  the post-branch type is the **union** of the per-branch types.
  Branches that diverge (`exit`, infinite loop, Phase 2 propagated `fail`) contribute `TidBottom`,
  which unifies with anything and drops out of the union.

```
if cond:
    42       # int
else:
    "x"     # str
;
# stack here: int|str at the top slot
```

A single `reconcile` function takes the per-branch tail stacks
and produces the post-branch stack;
it is shared between `if`, `match`, and `try:`.

### Variable storage and scope

Variables carry types that evolve as the program proceeds —
this is the type system's primary mechanism for tracking flow.

Rules:

- **All branches of a branching construct must result in the same set of bound variables.**
  You cannot bind a variable in one arm of an `if` and not the other.
  The branch reconciliation rule applies symmetrically to bindings:
  same names, same set on every arm.
- **A variable's type may change as it is reassigned.**
  Reassignment is allowed and does not require the same type.
  After the assignment, the variable's type is the new value's type.
- **At branch reconciliation, a variable's type becomes the union of its per-branch types**
  (same rule as the stack).
  If both branches store an `int`, the post-branch type is `int`.
  If one stores `int` and the other stores `str`, the post-branch type is `int|str`.

This gives the checker a clean way to narrow and widen types as the program flows,
and keeps branches symmetric without forcing users to pin one type up front.

The checker maintains a per-scope environment `map[NameId]TypeId`,
extended on bind, mutated on reassignment,
reconciled at branching boundaries.

### Stack sizes are statically known everywhere

Hard constraint.
**Operations whose effect on stack size depends on a runtime value are forbidden.**
This rules out classic `splat` —
its output arity depends on the length of the list at runtime.
Any future op that can't be assigned a fixed `( in -- out )` is similarly forbidden.

The pre-existing parser-level forbidding of splat carries forward.
2unpack, 3unpack, etc. are fine — fixed arity.

### Match exhaustiveness

Required.
For a value of type `Maybe[T]`, both `just` and `none` arms must appear.
For a value of a closed union `type X = A | B | C`,
all three arms must appear,
or a wildcard arm `_ -> ...` covers the remainder.
For records, an arm matching the record's shape is required;
a wildcard covers other shapes if the matched type is a union of records.

Non-exhaustive matches are a static error.

### No implicit numeric coercion

`int` does not flow into a `float` slot.
Use `toFloat` (or whatever the explicit conversion is)
to widen.
Same in the other direction.
This matches the existing runtime — it does not auto-coerce.

### Empty literal typing

- `[]` has type `[T]` for a fresh type variable `T`,
  resolved from context.
  If context doesn't pin it, an error.
- `{}` has type `{}` (the empty shape),
  which width-subtypes into any record type that has no required fields,
  i.e. the universal record.
- Standard Haskell / TypeScript / Rust behavior.

### No `any` type

We use type variables for "permissive" return types
(opaque grid columns, dict lookups whose value type is unknown).
A fresh type variable is unifiable with whatever the user does next;
that is enough.
A top-type `any` is not introduced —
it would degrade inference quality
and there is no concrete need.

### Reserved keywords and reserved type names

Reserved keywords (lexer level, cannot be used as identifiers):

- `def`, `if`, `else`, `match`, `type`, `as` — language structure.
- `<fail>`, `pure`, `try`, `fail` — Phase 2 placeholders.
- `just`, `none` — `Maybe[T]` constructors.
- All existing keywords.

Reserved type names (cannot be shadowed by `type X = ...`):

- `int`, `float`, `str`, `bool`, `bytes`, `none`,
  `Maybe`, `Grid`, `GridView`, `GridRow`.

Attempting to redefine any of these is a static error.

### Top-level item processing order

Six-step pass over a parsed file:

1. Lex and parse the whole file (already done by the existing parser).
2. Collect all `type X = ...` headers.
   Reserve a placeholder `TypeId` for each;
   names enter the type environment.
3. Resolve all `type` bodies using the placeholders.
   Mutual recursion between types is handled here.
4. Collect all `def` sigs.
   This makes forward references and mutual recursion between defs work.
5. Type-check each `def` body against its declared (or inferred) sig.
6. Type-check top-level expressions in source order,
   threading the stack and the variable environment through.

### Standard library handling

`lib/std.msh` (the stdlib loaded via `MSHSTDLIB`) is **type-checked every time** a program runs —
the user can edit the file,
and we do not trust a stale cache against an edited source.

Optimization:
hash the stdlib file contents on load;
cache the type-checked result keyed by that hash.
Cache hit on unchanged file skips the recheck.
Cache miss reruns and updates the cache.

Builtin sig table is registered before stdlib loads;
stdlib types and sigs are available before user-program type-checking starts.

## Risks and open questions (V1)

- **Match-form ambiguity at branded unions.**
  `Maybe[T]` matches on constructors;
  branded unions match on type.
  Make sure the parser doesn't choose;
  defer to the checker, which knows the static type.
- **Recursive types without forward declaration.**
  Handled by step 2 of the top-level processing order
  (placeholder TypeIds for `type` headers).
- **Quote-body inference.**
  `[2 +]` should infer as `(int -- int)`.
  Recursive checker invocation on the quote's items with a fresh empty stack.
  Inputs derived from observed underflow,
  outputs are whatever's on the stack at end.
- **Generic Maybe interaction with overload dispatch.**
  Overloading `f` for `Maybe[int]` vs `int` —
  bare `5` should pick `int`, not auto-lift.
  Verify no auto-coercion path bypasses the explicit-`just` rule.
- **Brand-equality semantics for unions.**
  Two declarations `type A = int|str` and `type B = int|str` produce distinct branded types.
  Casting from `int|str` to `A` is required (use `as A`);
  no implicit narrowing.

## Phased implementation (V1)

Each phase is testable independently and produces visible value.

1. **Arena and primitives.**
   Type kinds, hashconsing, TypeId issuance,
   `TidBottom` reserved for `exit` and divergent operations.
   Reserve `as`, `<fail>`, `pure`, `try`, `fail`, `just`, `none` as keywords in the lexer.
   Reserve built-in type names (`int`, `float`, `str`, `bool`, `bytes`, `none`,
   `Maybe`, `Grid`, `GridView`, `GridRow`) so user `type` decls can't shadow them.
   No composites, no checker yet.
2. **Stack and applySig with primitive-only sigs.**
   Validate against arithmetic and comparison builtins.
3. **Composites: lists, dicts, shapes, grids (opaque).**
   Add the structural type kinds plus `TKGrid`, `TKGridView`, `TKGridRow` without schema tracking.
4. **`Maybe[T]` and constructors.**
   `just`, `none`, Maybe-pattern matching.
5. **Unions and brands.**
   Structural unions with canonical sorting; brand wrapper.
   `as Type` cast expression.
6. **Type variables and unification.**
   `TypeVarId`, `Substitution`, `unify()`, generic instantiation.
   Generic-as-bare-identifier rule in type positions.
6b. **Branch reconciliation and variable environment.**
   `VarEnv`, scope snapshot/restore around branches,
   stack-size match enforcement,
   per-slot type union across branches.
   Wire into `if`/`else` and `match`.
   Match exhaustiveness check.
7. **Quote types and body inference.**
   Recursive check on quote items;
   underflow-derived inputs.
8. **Grid schema tracking.**
   Promote the opaque grid kinds added in phase 3 to carry column schemas;
   teach schema-shaping operations (`gridAddCol`, projection, etc.) to compute result schemas.
9. **Overload dispatch.**
   Multiple def sigs with same name;
   specificity ranking.
10. **Integration in `Main.go`.**
    Parse-tree pass after parser, before evaluator.
    Type errors gate execution.
11. **Migrate builtin sigs from `TypeChecking.go`.**
    Reach parity, delete the old module.

## First slice

Phases 1 and 2 together are a small but complete vertical slice:
the arena,
hashconsing,
the stack,
`applySig` against a hardcoded set of arithmetic builtins.
That proves the design before committing to the full surface.

## Implementation progress

Branch: **`type-checker-v1`** (off main; not yet pushed).

The user has imposed two standing constraints during implementation:
1. If a type definition in the **standard library** (`lib/std.msh`) looks wrong, **stop and ask** before changing it.
2. If an **existing test fails**, **stop and ask** before editing the test or the code that broke it.

Per CLAUDE.md, do not run `gofmt` without asking, and do not commit without an explicit user request.

### Phase 1 — Arena and primitives — DONE

Files created:

- `mshell/Type.go` — type representation, arena, hashconsing, name interning,
  reserved-type-name predicate.
- `mshell/Type_test.go` — 18 unit tests covering hashconsing, shape normalization,
  union flatten/dedupe/sort/collapse, brand semantics, quote hashconsing,
  grid family, var ids, name table, reserved-type-name lookup.

Concrete primitives in the arena (assigned in this fixed order during
`NewTypeArena`):

```
TidNothing = 0   // sentinel
TidBool    = 1
TidInt     = 2
TidFloat   = 3
TidStr     = 4
TidBytes   = 5
TidNone    = 6
TidBottom  = 7
```

`QuoteSig` already carries the `Fail TypeId` and `Pure bool` fields; defaults
in V1 are `TidNothing` and `false`. Phase 2 does not require a struct
migration.

`NewTypeArena` reserves index 0 in the side tables (`shapeFields`, `quoteSigs`,
`unionMembers`, `gridSchemas`) so non-zero `Extra` is always meaningful.

Verification:

- `go build ./...` clean.
- `go test ./...` passes — new tests + all pre-existing Go tests.
- `./build.sh` builds the binary.
- `./tests/test.sh` — every integration test passes, no regressions.

No stdlib or existing test files were modified.

Note: `firstCompositeId` constant in `Type.go` is currently unused (a
diagnostic warning); it is kept as documentation of the boundary between
primitive ids and composite ids. Safe to remove if it bothers anyone.

### Naming gotcha encountered

`GridColumn` already exists in `mshell/MShellObject.go` as the runtime
representation. The type-system column struct was renamed to
`GridSchemaCol` to avoid the collision. If future grid-related types come
up, watch for similar collisions with runtime names in `MShellObject.go`.

### Phase 1 — what was deferred from the plan

- **Lexer keyword reservation** for `as`, `<fail>`, `pure`, `try`, `fail`,
  `just`, `none`. Not done yet — `just` and `none` already exist as
  language constructs, the others (`as`, `try`, `fail`, `pure`) were
  confirmed unused as bare identifiers in stdlib & tests but the actual
  lexer additions are deferred until they are needed by the parser
  (Phase 5 onward). When done, add `TokenType` entries (e.g. `AS`, `TRY`,
  `FAIL_KEYWORD`, `PURE`) and dispatch them from
  `Lexer.literalOrKeywordType`.
- **Built-in type-name reservation** at the parser level. The
  `IsReservedTypeName` predicate exists in `Type.go` but is not yet wired
  into a `type X = ...` parser. Wire when the parser learns to handle
  `type` declarations.

### Phase 2 — Stack and applySig — DONE

Files created:

- `mshell/TypeChecker.go` — `Checker`, `TypeStack`, `VarEnv` (placeholder),
  `FnContext`, `applySig`, primitive-only `unify` (TidBottom unifies with
  anything; otherwise integer equality of `TypeId`).
- `mshell/TypeBuiltins.go` — `builtinSigsByToken()` returning a small
  `map[TokenType]QuoteSig` for `+`, `-`, `*`, `**`, `<`, `>`, `<=`, `>=`.
  All entries are `(int int -- int)` or `(int int -- bool)`.
- `mshell/TypeError.go` — `TypeError`, `TypeErrorKind`, `Format`, plus
  a primitive-only `FormatType`. Composite kinds render as
  `<Kind #id>` for now.
- `mshell/TypeChecker_test.go` — covers `2 3 +`, `2 "x" +`,
  underflow with 0 and 1 args, comparison returns bool, all four
  primitive literal pushes, unknown identifier, and error formatting.

Behavior decisions worth flagging:

- On stack underflow, `applySig` reports the error and **leaves the
  stack untouched**. On a type mismatch, it still pops inputs and
  pushes outputs. Both choices are about reducing cascading errors
  in a multi-token stream.
- `LITERAL` tokens fall through to `TErrUnknownIdentifier` since
  Phase 2 has no name resolution. The parser-driven path replaces
  this in Phase 10.

Verification:

- `go build ./...` clean.
- `go test ./...` passes (existing tests + 7 new Phase-2 tests).
- `./build.sh` + `./tests/test.sh` — all integration tests pass.

No stdlib or existing test files were modified. The Phase-1 deferrals
(lexer keyword reservation for `as`/`<fail>`/`pure`/`try`/`fail`,
parser-level reserved-type-name enforcement) remain deferred —
nothing in Phase 2 needs them.

What does NOT belong in Phase 2 (and was not done):

- Lists/dicts/shapes/unions/brands (Phase 3+).
- Generics, substitution (Phase 6).
- `if`/`else`, `match`, branch reconciliation (Phase 6b).
- Any wiring into `Main.go` (Phase 10).

### Phase 3 — Composites: lists, dicts, shapes, opaque grids — NEXT

Scope per the plan above. The arena already has constructors
(`MakeList`, `MakeDict`, `MakeShape`, `MakeGrid` etc.); Phase 3
extends the checker to push/pop them and adds composite cases to
`unify` (still without type variables — that's Phase 6).

### Migration thread

`mshell/TypeChecking.go` (the existing 883-line interface-based checker)
remains untouched and operational. It will be deleted at the end of
Phase 11, once the new module reaches feature parity. Don't poke it
during V1 unless integrating in Phase 10 forces a change.

## Phase 2: `<fail>` effect and `pure`

Deferred from V1.
Captured here so context isn't lost.

### What this adds

- The `<fail E>` effect annotation on sigs.
  A function with `<fail>` may bypass its success path and produce a typed error value.
  Default error type is `str`;
  parameterized form `<fail E>` uses a user type.
- The `try:` block syntax for handling failure.
  Branch reconciliation against the success-path stack;
  fail arms must either match the success arity/types or diverge (`exit`, propagated `fail`).
- The `fail` keyword for raising failure with a value.
- The `pure` annotation on sigs.
  A `pure` function may not call any non-pure or `<fail>` operation.
  Checked statically.
- Effect inference, Zig-style.
  A function calling a `<fail>` operation without handling silently inherits `<fail>`
  unless the user declared a sig that explicitly omits it
  (in which case it's a static error).

### What stays the same

- Runtime is unchanged. `<fail>` is a type-level annotation only.
  Builtins that fail still call into the existing runtime error path.
- `Maybe[T]` continues to exist as the value-shaped optionality type.
  It does not collapse with `<fail>` —
  these are duals, used in different situations
  (lookup-where-absent-is-fine vs operation-where-failure-is-an-error).

### Implementation sketch

- Populate `QuoteSig.Fail` and `QuoteSig.Pure` for fallible/pure builtins.
- Extend the parser to parse `<fail>` / `<fail E>` / `pure` annotations on sigs
  and `try:` blocks with `fail @e -> ...` arms.
- Extend `Checker` and `FnContext`:
  ```go
  type Checker struct {
      // ...
      failHandlerDepth int          // > 0 inside try:
  }
  type FnContext struct {
      Sig            QuoteSig
      DeclaredFail   TypeId
      DeclaredPure   bool
      InferredFail   TypeId
      InferredPure   bool
  }
  ```
- Extend `applySig` to record fail effects:
  ```go
  if sig.Fail != TidNothing {
      if c.failHandlerDepth > 0 {
          // handled locally — no propagation
      } else {
          c.recordFailEffect(callSite, sig.Fail)
      }
  }
  if !sig.Pure && c.currentFn.DeclaredPure {
      c.errPureViolation(callSite)
  }
  ```
- Extend `unify` for `TKQuote` to compare fail and pure flags.
- Add `try:` block handling:
  recursive check on the body with `failHandlerDepth++`;
  recursive check on each fail arm;
  unify all arms' tail stacks against the success-path stack
  (with `TidBottom` from divergent arms unifying with anything).
- Update overload-dispatch specificity to consider effects.

### Migration strategy when Phase 2 lands

The risky part: previously-passing programs may stop passing once `<fail>` enforcement is on.
Use Zig-style inference to minimize friction:

- Builtin sigs are updated to declare `<fail>` where they can fail.
- User functions without explicit sigs auto-inherit `<fail>` from any fallible callee.
- The fail propagates to top-level,
  where it falls into "let runtime crash" — the same behavior as today.
- User code only breaks where a function explicitly declares `pure` (impossible in V1) or
  declares a sig that explicitly omits `<fail>` while the body needs it.
  The latter is a real signal to handle; not silent.

A `--strict-effects` flag (default off) can promote effect inference into a hard error during the transition,
letting early adopters tighten their codebase.

### Phase 2 risks and open questions

- **Effect inference vs declaration.**
  Auto-promote a function that calls a `<fail>` builtin?
  Lean toward yes — Zig's inferred error sets work well,
  and forcing every internal helper to declare `<fail>` is friction.
  But: if a sig is *declared* without `<fail>` and the body calls a `<fail>` op,
  is that an error or auto-promotion?
  Tentative rule:
  declared sig is binding,
  missing `<fail>` is an error;
  undeclared sig is inferred,
  promotion happens silently.
- **Diagnostic vs payload.**
  We landed on typed payloads (`<fail E>`) over Zig-style diagnostics with ambient slot.
  Confirm at implementation time that the payload approach scales as we annotate the standard library.

## Future work (out of scope, even for Phase 2)

- Binding type-check results to runtime dispatch
  (eliminate per-op stack-bounds checks,
  driven by overload resolution).
- Additional effects beyond `<fail>` and `pure`
  (`<io>`, `<exec>`, etc.) —
  deferred per the Rust/Zig precedent that failure is the only effect worth tracking.
- `defer` / `errdefer` for resource cleanup on the failure path.
- Nominal records (`type!`) and newtype primitives (`type UserId = int`),
  deferred until a concrete need arises.
