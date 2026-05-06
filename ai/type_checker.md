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

### Phase 3 — Composites: lists, dicts, shapes, opaque grids — DONE

Phase 1 already provided the arena constructors. Phase 3's contribution
is on the checker side:

- `Checker.unify` now dispatches on `TypeKind`:
  - `TKMaybe`, `TKList` — recurse on inner.
  - `TKDict` — recurse on K and V.
  - `TKShape` — width subtyping (every required field of `want` must
    appear in `got` with a unifiable type; extras allowed in `got`).
    Linear merge thanks to pre-sorted field lists.
  - `TKUnion` — subset rule (every arm of `got` must unify with some
    arm of `want`); brand ids must match.
  - `TKBrand` — nominal: same brand id required, no implicit unwrap to
    underlying. Casting (`as`) is the future escape hatch.
  - `TKQuote` — pairwise unify inputs and outputs, with Fail/Pure flags
    matched directly (always default in V1).
  - `TKGrid` / `TKGridView` / `TKGridRow` — opaque in Phase 3:
    unknown-schema (`Extra == 0`) unifies with any same-kind grid;
    different known schemas don't (Phase 8 adds real schema tracking).
- `FormatType` rendering for all composite kinds: `[T]`, `Maybe[T]`,
  `{K: V}`, `{a: int, b: str}`, `int | str`, branded `Mine(int | str)`,
  `UserId(int)`, `(int -- str)`, `Grid`, `T0` (var rendering for Phase 6).

Tests added in `mshell/TypeCheckerComposite_test.go` (15 tests):
list equality / mismatch / cross-kind, Maybe recursion, shape width
subtyping in both directions, shape field-type mismatch, union subset
in both directions, branded-union distinctness, brand nominality
(including no-implicit-unwrap), quote arity / input mismatch, opaque
grid unify and Grid-vs-GridView distinctness, TidBottom unification,
applySig with a composite-typed input (matching and mismatching),
and FormatType rendering for every composite kind.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib or existing test files
modified.

Notes / deferred:

- The composite recursion in `unify` will need a new pass for type
  variables (Phase 6) — the recursive calls are where TKVar bindings
  will hook in.
- No name-keyed builtin table yet. The TokenType-keyed table covers
  arithmetic and comparison; word-style builtins (`len`, `map`, etc.)
  arrive with overload dispatch in Phase 9.
- No quote-body inference or `if`/`else` / `match` reconciliation —
  those land in Phases 6b and 7.

### Phase 6 — Type variables and unification — DONE (taken out of order)

Did Phase 6 before Phase 4 because Maybe's `just` / `none` constructors
are inherently parametric — building them on top of stub generics would
just have to be torn out.

Files added:

- `mshell/TypeUnify.go` — `Substitution` (slice indexed by `TypeVarId`),
  `FreshVar`, `Apply` (recursive, rebuilds composites through the arena
  so hashconsing stays canonical, with path compression on var chains),
  `Bind` (with occurs check), `occurs` (recursive into all composite
  kinds), and `(*Checker).Instantiate` + `renameVars` for fresh-renaming
  a polymorphic sig at a call site.
- `mshell/TypeUnify_test.go` — 16 tests: fresh-var distinctness, var
  binding from either side, vacuous self-unify, transitive resolution
  via two vars, var/concrete conflict, occurs check, var inside list,
  Apply rebuilding [T] → [int], polymorphic id sig at two call sites
  with different concrete types, parametric `just`-style sig, two-var
  pair-to-dict sig, `(T T -- T)` constraint across inputs (positive and
  negative), Apply rebuilding a quote, `T7` formatting.

`Checker.unify` updated:

- Both sides go through `subst.Apply` first, so resolved chains are
  followed before structural checks.
- After Apply, if either side is still `TKVar`, it must be unbound;
  bind it (with occurs) to the other side.
- The Phase-3 composite cases are otherwise untouched — recursive
  `c.unify` calls inside them automatically pick up the new var
  resolution.

`Checker.applySig` now calls `Instantiate(sig)` on entry (no-op for
monomorphic sigs) and pushes outputs through `subst.Apply` so freshly
bound vars resolve immediately on their way to the stack.

`FormatType` already had `T<n>` rendering from Phase 3.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib or existing test files
modified.

### Phase 4 — Maybe[T] constructors — DONE (match-arm dispatch deferred)

Constructors:

- `just : (T -- Maybe[T])` with `Generics: [T]`.
- `none : ( -- Maybe[T])` with `Generics: [T]` and no inputs.
  The free `T` remains an unbound TKVar until surrounding context
  pins it; if nothing pins it, the var sits on the stack and
  later phases will diagnose ("ambiguous Maybe[?]" — likely
  alongside Phase 6b's reconciliation).

`Checker` gained a name-keyed builtin table (`nameBuiltins
map[NameId]QuoteSig`) populated by `builtinSigsByName(arena, names)`
in `TypeBuiltins.go`. `checkOne` now routes `LITERAL` tokens through
this table by interning the lexeme and looking up; misses fall
through to the existing unknown-identifier error.

Canonical sigs in the name table use plain `MakeVar(0)` rather than
allocating through `Substitution.FreshVar`. That's safe because every
call to `applySig` runs `Instantiate` first, which fresh-renames the
sig's `Generics` to fresh substitution slots; two canonical sigs may
both reuse `TypeVarId(0)` without colliding because renameVars
produces fresh per-call vars before unification touches them.

Tests added in `mshell/TypeMaybe_test.go` (7 tests):

- `5 just` → `Maybe[int]`.
- Two `just` calls on int and str independently produce `Maybe[int]`
  and `Maybe[str]` (call-site freshness).
- `none` produces `Maybe[T?]` with an unbound TKVar inside.
- `none` flowing into a `Maybe[int]` consumer binds the var.
- `5 just` flows into a `Maybe[int]` consumer.
- `5 just` rejected by a `Maybe[str]` consumer.
- A bare `5` rejected by a `Maybe[int]` consumer (no implicit lift).

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green.

Deferred to Phase 6b:

- Match-arm dispatch (`Checker.checkMatch`). Maybe pattern-matching
  needs branch reconciliation infrastructure that doesn't exist yet
  (snapshot/restore VarEnv across arms, stack-size match, per-slot
  type union). Building it standalone would be wasted work — Phase 6b
  is where it belongs.
- The "ambiguous Maybe[?]" diagnostic for an unbound var lingering
  on the stack at end of program. Probably folds into the same
  reconciliation pass.

### Phase 6b — Branch reconciliation and exhaustiveness — DONE

Files added:

- `mshell/TypeBranch.go` — `ScopeSnapshot`, `Snapshot`, `Fork`,
  `BranchArm`, `CaptureArm`, `ReconcileArms`, `MatchArmKind`,
  `MatchArmTag`, `CheckMatchExhaustive`.
- `mshell/TypeBranch_test.go` — 16 tests (snapshot detachment,
  fork restoration, same-type reconciliation, type-union
  reconciliation, stack-size mismatch, var-set mismatch, var-type
  union, diverged-arm drop, all-diverged clears state, Maybe match
  exhaustive / missing-none / wildcard, union match exhaustive /
  missing-arm / wildcard, Maybe-pattern style match returning int).

`TypeError.go` extended with three new kinds:
`TErrBranchStackSize`, `TErrBranchVarSet`, `TErrNonExhaustiveMatch`.

Reconciliation rules implemented:

- **Stack sizes must match** across non-diverged arms; size mismatch
  is `TErrBranchStackSize`. Recovery uses the first non-diverged
  arm's tail so downstream errors don't cascade.
- **Var sets must match** across non-diverged arms; mismatch is
  `TErrBranchVarSet`. Same recovery as above.
- **Per-slot type union** via `arena.MakeUnion` (auto-flatten /
  dedup / collapse). Same union for per-var types.
- **Diverged arms dropped entirely** from size, var-set, and union
  computations. The `Diverged` flag is supplied by the caller
  (parser-driven path will set it from `exit` / infinite-loop
  detection in Phase 10).
- **All-diverged branches clear state**: stack and vars empty,
  representing dead code downstream. No error reported here —
  Phase 10 may add a "dead code" warning at the integration layer.

Exhaustiveness:

- **Maybe[T]**: requires both `MatchArmJust` and `MatchArmNone`,
  or a `MatchArmWildcard`.
- **Union**: every flattened arm must be covered by an exact
  `MatchArmType` pattern, or a `MatchArmWildcard`.
- **Other kinds**: no rule encoded; flagged non-exhaustive unless
  a wildcard appears. Shape/brand exhaustiveness lands when the
  parser-driven path produces those arm shapes.

Substitution intentionally NOT rolled back between arms. Bindings
from arm 1 persist into arm 2's checking. This may cause spurious
errors in pathological cases; promoting it to a snapshot/restore is
a localized fix if needed in practice. Documented at the head of
`TypeBranch.go`.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green.

What this unlocks:

- Phase-4's deferred match-arm dispatch is now buildable: a parser
  pass can run each arm's body through Fork → check tokens →
  CaptureArm → ReconcileArms, with `CheckMatchExhaustive` on the
  pattern side. The user-visible `match` lands in Phase 10 when the
  parser is wired in.
- Phase 7 (quote-body inference) will use Snapshot/Fork to recurse
  on a quote's items with a fresh stack.

### Phase 7 — Quote-body inference — DONE

Files added:

- `mshell/TypeQuote.go` — `(*Checker).InferQuoteSig(body []Token) QuoteSig`.
  Snapshots outer state, runs the body against an empty stack with
  inference mode on, then restores. Inputs accumulate (deepest-first)
  via fresh-var synthesis on underflow; outputs are whatever's left
  on the stack at end. Both lists are pushed through
  `subst.Apply` before returning so concretized vars resolve.
- `mshell/TypeQuote_test.go` — 11 tests: empty quote, literal-only,
  `[2 +]` → `(int -- int)`, `[+]` → `(int int -- int)`,
  `[+ +]` → `(int int int -- int)`, `[<]` → `(int int -- bool)`,
  `[just]` produces a Maybe over the input var, `[none]` produces
  Maybe with a fresh var, outer state restoration including the
  `inferring` flag and `inferInputs` slice, type mismatch surfacing
  inside a body, and round-trip use of the inferred quote at a
  higher-order call site.

`Checker` gained two fields: `inferring bool` and `inferInputs []TypeId`.
`applySig` now branches on `c.inferring` when underflow is detected:
each missing slot is filled with a fresh `subst.FreshVar`, those vars
are prepended to `c.stack.items` (deepest at the bottom) and
prepended to `c.inferInputs` (deepest at the front, matching
caller-stack order).

Known limitation, deferred:

- **No generalization.** Free type variables in the inferred sig
  remain as plain unbound vars in the global substitution. A body
  like `[dup]` returns `(T0 -- T0 T0)` with `T0` shared across all
  subsequent uses; calling such a quote at two different concrete
  types will conflict on the second call. Real let-polymorphism
  lands when a concrete need surfaces — likely with Phase 9
  overloading or with user-written `def` bodies that lack sigs.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green.

### Phase 9 — Overload dispatch — DONE (taken before Phase 5)

Did Phase 9 before Phase 5 per the recommendation: stays at the
pure-checker layer, while Phase 5's user-facing pieces (`type X =
A | B` declarations and the `as` cast) are mostly parser work.

Files added:

- `mshell/TypeOverload.go` — `(*Checker).resolveAndApply(candidates,
  callSite)` and `specificityScore`.
- `mshell/TypeOverload_test.go` — 10 tests: concrete-vs-generic
  preference (both directions), different-arity overloads, no-match
  fallback, ambiguity detection, shape-beats-var, brand-beats-var,
  trial-isolation across calls (verifies one trial's bindings don't
  leak into the next), specificity-score spot checks, subst
  Checkpoint/Rollback round-trip.

Other changes:

- `Substitution` gained `SubstCheckpoint`, `Checkpoint`, and
  `Rollback`. Rollback shrinks `bound` as needed; vars allocated
  after the checkpoint become unreachable through this substitution
  (their arena nodes remain, harmlessly orphaned).
- `Checker.nameBuiltins` retyped from `map[NameId]QuoteSig` to
  `map[NameId][]QuoteSig` so a name can carry multiple signatures.
- `builtinSigsByName` updated to register one-element slices for
  `just` and `none`.
- `checkOne` now routes LITERAL tokens through `resolveAndApply`
  instead of calling `applySig` directly.
- Two new error kinds: `TErrAmbiguousOverload`,
  `TErrNoMatchingOverload`.

Resolution algorithm (per `TypeOverload.go` header):

1. Snapshot stack and substitution.
2. For each candidate: restore both, instantiate, drop on
   arity-fail, trial-unify each input. Score the candidate from
   its **pre-instantiation** sig so generics with remaining vars
   score lower than concrete inputs.
3. Restore both snapshots once more so the actual application
   below starts clean.
4. If exactly one candidate has the highest score, apply it. Ties
   are reported as ambiguity (and the first tied candidate
   applied for recovery). No matches are reported as no-match
   (and the first listed candidate applied for recovery).

Specificity score: every non-TKVar arena node in an input
contributes 1, brand wrappers add a +1 bonus, TKVar contributes 0.
Higher = more specific. Sum across the input list.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib or existing test files
modified.

Forward-compatibility hooks for Phase-2-of-effects already in place:
the resolver uses sig.Inputs only for scoring; when `<fail>` /
`pure` are added, the score function can fold them in without
changing the resolver loop.

### Phase 5 — Unions, brands, and `as` cast — DONE (option 1, pure-checker minimal)

Took option 1 from the two paths described below: pure checker, no parser
or lexer changes. The user-visible `type X = ...` form and the postfix
`as` operator land together with the rest of the parser integration in
Phase 10.

Files added:

- `mshell/TypeCast.go` — `(*Checker).DeclareType`, `LookupType`, `Cast`,
  plus internal helpers `brandify`, `castOk`, `acceptsAs`, `underlying`.
- `mshell/TypeCast_test.go` — 14 tests: union-type declaration produces
  branded union; newtype declaration over a primitive produces TKBrand;
  reserved type names rejected; duplicate names rejected; two distinct
  declarations with the same body produce distinct nominal types and do
  not unify; cast int → branded union (member of arms); cast int|str →
  branded union (subset of arms); cast bool → Result rejected as
  TErrInvalidCast; cast brand A → brand B rejected; cast A → underlying
  union allowed; newtype cast from underlying allowed; newtype cast from
  wrong underlying rejected; cast underflow recovers by pushing target;
  identity cast is a no-op; LookupType on missing name returns
  TidNothing.

`Checker` gained a new field:

```go
typeEnv map[NameId]TypeId
```

initialized lazily on first DeclareType. Reserved built-in type names
are NOT stored here — `IsReservedTypeName` is consulted before
declaration and the parser-driven path will recognize built-ins
directly.

Three new error kinds in `TypeError.go`:

- `TErrReservedTypeName` — attempt to redefine `int`, `Maybe`, etc.
- `TErrDuplicateTypeName` — same name declared twice.
- `TErrInvalidCast` — source type not compatible with target.

DeclareType behavior (`brandify`):

- `type X = A | B` over an unbranded union: returns a NEW union with
  the same arms but `brandId = NameId(X)`. Hashconsing in
  `MakeUnion` already keys on the brand id, so two declarations with
  the same arms but different names produce distinct TypeIds.
- `type X = T` over any non-union type (primitive, list, dict, shape,
  Maybe, brand, etc.): returns a `TKBrand` wrapping `T`. This is the
  newtype case.
- Re-branding an already-branded union is reported as an error using
  `TErrReservedTypeName` (closest existing kind, with a hint;
  `TErrInvalidTypeBody` could be added later if this case shows up
  more).

Cast compatibility (`castOk`) tries four trials in order, each isolated
by a substitution checkpoint:

1. Identity (`src == dst`).
2. Direct unification.
3. Tag-in: `src` is acceptable where `dst`'s UNDERLYING is expected.
   This covers a primitive (or any non-union) flowing into a branded
   union, since primitives don't unify with a union directly.
4. Untag: `dst` is acceptable where `src`'s UNDERLYING is expected.
   Lets a branded value cast back to its structural form.

`acceptsAs` is a cast-only "is src valid where dst is expected" check.
It differs from `unify` in one crucial direction: when `dst` is a
union, `src` matches if it's compatible with any arm. `unify` is
symmetric on kind, so `int` can't unify with `int|str` directly; cast
acceptance is asymmetric and handles the membership case.

`underlying`:

- Branded union → unbranded union with same arms (one layer).
- TKBrand wrapper → the wrapped underlying TypeId (one layer).
- Anything else → the type itself.

Recursion past one layer was not needed by any current test.
Promotion to multi-layer peeling is a localized fix if needed.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib or existing test files
modified.

What this unlocks:

- Phase 10 parser work for `type X = A | B` declarations and the
  postfix `as` operator can call `DeclareType` / `Cast` directly.
- Other phases that need named-type lookup (sig parsing, pattern arms
  matching against branded unions) call `LookupType`.

Original notes (left here for context):

Phase 3 already implemented union/brand unification at the
`unify()` level. Phase 5 adds the surface syntax: `type X = A | B`
declarations producing a `TypeId` in the type environment, and
`<value> as <Type>` casts that re-tag a value's static type.

Phase 5 is the first phase where touching the parser becomes
hard to avoid. Two options:

1. **Pure-checker minimal**: add a "type environment" (`map[NameId]TypeId`)
   and a "cast" entry point on the Checker (`(c *Checker) Cast(target TypeId, callSite Token)`).
   Defer the parser/lexer changes for `type` declarations and the
   `as` postfix until Phase 10. This keeps the same shape as the
   prior phases — pure checker plus tests via constructed token
   streams or direct API calls.
2. **Start parser integration now**: register `type` as a top-level
   form, parse `T = A | B`, build the union via the arena, and
   land the postfix `as` operator in the lexer. This pulls Phase 10
   forward but front-loads the schema work.

Recommendation when continuing: option 1, then Phase 7 (quote-body
inference) and Phase 9 (overload dispatch), then a single parser
integration pass for Phases 5/10/11 together.

### Phase 10 step 1 — Lexer keyword reservation + type-expression parser — DONE

Took the split-Phase-10 path (option B): land lexer + parser surface in
chunks before wiring the checker into the run path. This entry covers
the first chunk.

#### Breaking change: runtime `type` builtin renamed to `typeof`

`type` was a runtime introspection builtin (`(a -- str)`); the design
reserves `type` as a keyword for `type X = ...` declarations. Resolved
by renaming the builtin to `typeof`.

Edits:

- `mshell/Evaluator.go:6338` — dispatch + error message.
- `mshell/BuiltInList.go:190`.
- `tests/grid.msh`, `tests/grid_concat.msh`, `tests/grid_groupby.msh`,
  `tests/parse_excel.msh` — six bare-word call sites; one inside a
  format string (`{@cell typeof}`).
- `doc/mshell.md`, `doc/mshell.html`, `doc/functions.inc.html`.

When the next release cuts, this should land in `## Unreleased / ###
Changed` with a note that `type` is now a reserved keyword.

#### Lexer keyword reservations

`mshell/Lexer.go` — five new TokenType constants and recognition:

- `AS`, `TYPE`, `TRY`, `FAIL_KEYWORD`, `PURE`.
- `String()` cases for each.
- New top-level branches in `literalOrKeywordType`: `'a'` for `as`,
  `'p'` for `pure`. Existing `'t'` and `'f'` branches widened to
  sub-switch on the second character so `true`/`try`/`type` and
  `false`/`fail`/`float` coexist.

`mshell/Lexer_test.go` — `TestTypeCheckerKeywords` covers the five new
keywords, the neighbors they share prefixes with (`true`, `false`,
`float`), and prefix variants (`types`, `asx`, `trying`, `failed`,
`purest`) that must remain `LITERAL`.

`AS` and `TYPE` are user-visible from the next chunk onward; `TRY`,
`FAIL_KEYWORD`, `PURE` are reserved early so future Phase 2 migration
doesn't break user identifiers.

#### Type-expression parser

`mshell/TypeExpr.go` — `ParseTypeExpr(c *Checker, tokens []Token)
(TypeId, int, []TypeError)`. Self-contained recursive descent that
consumes a token slice and returns a TypeId.

Grammar (per file header):

```
typeExpr := union
union    := primary ( '|' primary )*
primary  := '(' sig ')'
          | '[' typeExpr ']'
          | '{' entry ( ',' entry )* '}'
          | named
          | TYPEINT | TYPEFLOAT | TYPEBOOL | STR
sig      := typeExpr* '--' typeExpr*
entry    := key ':' typeExpr
named    := LITERAL ( '[' typeExpr ']' )?
```

Supported forms:

- Primitives via dedicated tokens (`int`, `float`, `bool`, `str`) and
  via LITERAL lexeme (`bytes`, `none`).
- `[T]`, `Maybe[T]`.
- `Grid`, `GridView`, `GridRow` (opaque schema; Phase 8 adds tracking).
- `(in* -- out*)` quote sigs.
- Unions `A | B | C` with left-to-right fold, hashconsed via
  `MakeUnion`.
- Dict `{K: V}` and shape `{a: T, b: U}`.
- User-declared types via `Checker.LookupType`.

Dict-vs-shape disambiguation on the first key inside `{...}`:

- LITERAL key followed by `:`, where the LITERAL is NOT a primitive
  type name spelled as a literal (`bytes`/`none`/`Maybe`/`Grid`/
  `GridView`/`GridRow`) → shape.
- Anything else → dict (single pair). Multi-pair dict is rejected
  with a hint pointing at shapes.

Generics deferred. A bare LITERAL that doesn't resolve to a built-in
or declared type is an error today. Generics land alongside `def`
signature parsing in the next chunk.

New error kind: `TErrTypeParse`. Pos is the offending token; Hint
carries the message.

`mshell/TypeExpr_test.go` — 22 tests:

- All six primitives.
- List, nested list, list-of-Maybe, list-with-union-element.
- Maybe[int], missing-arg error.
- Dict `{str: int}`.
- Shape: 2-field, empty `{}`, duplicate-field rejected,
  multi-pair-dict rejected.
- Union (2 arms, 3 arms).
- Quote `(int int -- int)`, empty `( -- )`, missing `--` error.
- Grid family (kind verification).
- User-declared type lookup via DeclareType + name reference.
- Unknown identifier produces `TErrTypeParse`.
- Consumed-count check (parser stops at the type expression boundary,
  leaving trailing tokens for the caller).

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib changes; tests modified only
to reflect the `type` → `typeof` rename.

### Phase 10 step 2 — Parser integration of `type` and `as` — DONE

Wired the type-expression parser into `MShellParser`. Both forms
parse cleanly through the existing pipeline; they are no-ops at
evaluation time (purely static, by design). The Checker is NOT yet
invoked at run time — that's step 3 behind `--typecheck`.

Refactor:

`mshell/TypeExpr.go` — split into a stateless AST + a Checker-driven
resolver:

- `TokenSource` interface (`Peek`, `Advance`, optional `PeekAt(n)`).
- `SliceTokenSource` adapter (powers existing slice tests).
- `TypeExprAST` interface and concrete nodes: `TypePrimAST`,
  `TypeNamedAST`, `TypeListAST`, `TypeDictAST`, `TypeShapeAST`
  (with `ShapeFieldAST`), `TypeQuoteAST`, `TypeUnionAST`.
- `ParseTypeExprAST(src) (TypeExprAST, []TypeError)` — stateless;
  no arena dependency.
- `ResolveTypeExprAST(c, ast) TypeId` — walks the AST and builds a
  TypeId via the Checker's arena, looking up named types in
  `typeEnv`.
- `ParseTypeExpr(c, tokens)` is now a shim: parse to AST, resolve.
  Existing 22 tests still pass unchanged.

Forward references work naturally: parser stores AST, resolution
runs when the checker pass executes.

Files added:

- `mshell/TypeParseIntegration.go` — new parse-tree node types and
  parser methods.
- `mshell/TypeParseIntegration_test.go` — 8 integration tests.

New parse-tree node types:

```go
type MShellTypeDecl struct {
    Name      string
    NameToken Token
    StartTok  Token       // the TYPE keyword
    Body      TypeExprAST
}
type MShellAsCast struct {
    AsToken Token
    Target  TypeExprAST
}
```

Both implement `MShellParseItem`. Both are static-only at
evaluation time — the evaluator has new cases that return
`SimpleSuccess()` for them.

Streaming adapter (`parserTokenSource`):

- Wraps `*MShellParser`; pre-loaded with `parser.curr`.
- `ensureN(n)` pulls more tokens from the lexer as needed.
- `PeekAt(n)` works for arbitrary `n`; the AST parser only needs
  `n <= 1` (shape-vs-dict disambiguation).
- `finish()` writes the un-consumed lookahead token back into
  `parser.curr`. Asserts no more than one un-consumed token remains;
  if it ever fires, the AST parser has a buffering bug.

Parser additions:

- `(*MShellParser).parseTypeExprStreaming()` — runs `ParseTypeExprAST`
  against the live token stream and tidies up.
- `(*MShellParser).ParseTypeDecl()` — handles `type Name = <typeExpr>`.
- `(*MShellParser).ParseAsCast()` — handles `as <typeExpr>`.
- `case TYPE:` added to `ParseFile` (top-level only).
- `case AS:` added to `ParseItem` (works inside lists/quotes too,
  since `ParseItem` is the recursive descent core).

Tests in `TypeParseIntegration_test.go` (8):

- Parse `type Result = int | str` → `MShellTypeDecl` with
  `TypeUnionAST` body of 2 arms.
- `type UserId = int  42 wl` — type decl followed by program; verify
  trailing items survive.
- `type R = int  42 as R` — `as` postfix produces `MShellAsCast` with
  `TypeNamedAST` target.
- `type Result = int  42 as Result wl` — trailing `wl` not swallowed
  by the cast (verifies the streaming adapter restores `parser.curr`
  correctly).
- `type Box = Maybe[int]` — Maybe with bracketed argument.
- `type Person = {name: str, age: int}` — shape body.
- `type = int` (missing name) → parse error.
- `type X int` (missing `=`) → parse error.

End-to-end smoke test: a real `.msh` program containing both
`type Result = int | str` and `42 as Result` parses, type/as nodes
no-op at evaluation, and the rest of the program (`"hello" wl`)
runs unchanged.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib changes required.

### Phase 10 step 3 — Main.go integration behind `--check-types` — DONE

The new Checker is now invokable from the CLI as a pre-evaluation
gate. Off by default during fixture migration; flips to default
once the suite is clean.

Files added:

- `mshell/TypeCheckProgram.go` — `TypeCheckProgram(file *MShellFile)
  ([]string, bool)` entry point.
- `mshell/TypeCheckProgram_test.go` — 9 unit tests.

Edits to `mshell/Main.go`:

- New `checkTypes` variable wired from the `--check-types` flag.
- Help text entry.
- Gate call between parse and evaluate: on failure, print formatted
  errors to stderr and `os.Exit(1)`.

Today's check is intentionally narrow:

1. Pre-collect all `MShellTypeDecl` items from `file.Items`.
2. For each decl: resolve body AST → TypeId via `ResolveTypeExprAST`,
   then call `Checker.DeclareType`. Errors flow into the checker:
   reserved-type-name (e.g. `type Maybe = ...`), duplicate-name,
   and unknown-type-in-body all surface.
3. Walk every parse item with `visitForAsCasts`, recursing into
   `MShellParseList`, `MShellParseQuote`, and `MShellParseDict`.
   For each `MShellAsCast`, resolve the target AST. This catches
   unknown-type targets and Maybe-arity errors at well-formedness
   time even before the program-flow walker exists.
4. Format any accumulated errors via `TypeError.Format(arena, names)`.

What is NOT done yet (and is the obvious next chunk):

- Driving the `TypeStack` through every parse-tree node so the full
  set of static type checks (`applySig`, branch reconciliation,
  match exhaustiveness, overload dispatch) actually runs over user
  code. The infrastructure for all of these exists; only the
  parse-tree walker is missing.
- Definition (`def`) sig parsing. Currently `def` bodies are not
  type-checked.
- Stdlib hashing/caching from the design.

Reserved-name parser quirk (worth flagging): `type int = ...` fails
at PARSE time because `int` lexes as TYPEINT, not LITERAL — the
parser's name slot only accepts LITERAL. Same for `float`, `bool`,
`str`. The LITERAL-shaped reserved names — `Maybe`, `Grid`,
`GridView`, `GridRow`, `bytes`, `none` — DO reach the parser and
get rejected at the checker level via `IsReservedTypeName`. That
split is fine for users (both forms are still rejected) but the
error messages differ.

Tests in `TypeCheckProgram_test.go` (9):

- Empty program passes.
- Valid `type Result = int | str` passes.
- `type Maybe = int` fails (reserved-name path through the checker).
- Two `type X = ...` decls fail (duplicate name).
- `type X = Nope` fails (unknown body).
- `type A = int  type B = A | str` passes (cross-decl forward ref).
- `type R = int  42 as R` passes (cast target resolved).
- `42 as Nope` fails (unknown cast target at top level).
- `[42 as Nope]` fails (visitor recurses into composite items).
- Plain mshell program with no `type`/`as` passes (we don't yet do
  flow checking).

End-to-end CLI smoke verified:

| Program | Flag           | Result                       |
| ------- | -------------- | ---------------------------- |
| good    | none           | runs, prints output          |
| good    | `--check-types` | check passes, then runs     |
| bad     | none           | bypassed, runs (no-op), 0    |
| bad     | `--check-types` | check fails, stderr, exit 1 |

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib changes; no fixture
migration needed (flag is opt-in and existing tests don't pass it).

### Phase 10 step 4 — Parse-tree flow walker (first cut) — DONE

The Checker now walks the parse tree and drives the TypeStack
through tokens and casts. Programs with arithmetic, comparison,
`just`/`none`, type declarations, `as` casts, and varstore/getter
flow check end-to-end under `--check-types`.

Programs that lean on word builtins not yet in
`builtinSigsByName`/`builtinSigsByToken` (most existing mshell
code) will surface unknown-identifier errors. That's the intended
signal: as the builtin table fills out, more programs become
checkable. Until then, `--check-types` stays opt-in.

`mshell/TypeCheckProgram.go`:

- `(*Checker).CheckProgram(file)` — registers all `MShellTypeDecl`
  in declaration order (forward refs work), then walks
  `file.Items` through `checkParseItem`.
- `(*Checker).checkParseItem(item)` — dispatcher.

Per-node behavior in this cut:

| Parse item                       | Stack effect                                 |
| -------------------------------- | -------------------------------------------- |
| `*MShellTypeDecl`                | skip (registered in pre-pass)                |
| `*MShellAsCast`                  | resolve target, call `Checker.Cast`          |
| `Token`                          | dispatch via `checkOne` (existing builtins)  |
| `*MShellParseList`               | recurse into items (sandboxed), push `[T?]`  |
| `*MShellParseDict`               | recurse into values, push empty shape        |
| `*MShellParseQuote`              | recurse into body, push `( -- )` placeholder |
| `*MShellParsePrefixQuote`        | no-op                                        |
| `*MShellParseIfBlock`/`Match`    | no-op (branch reconciliation deferred)       |
| `*MShellParseGrid`               | push opaque `Grid`                           |
| `*MShellIndexerList`             | pop one, push fresh var                      |
| `MShellVarstoreList`             | pop per name, bind in `VarEnv`               |
| `*MShellGetter`                  | push `VarEnv` lookup or fresh var            |

Recursion into list/dict/quote items is sandboxed via
`snapshotStack`/`restoreStack` so nested casts get walked but
inner stack effects don't leak out (we don't yet attempt
per-element list inference).

Variable-binding semantics for `MShellVarstoreList`: pops one
item per name in reverse (top-of-stack binds to the rightmost
name, matching the runtime). The bound type lands in
`VarEnv.bound`; `MShellGetter` reads from there.

Tests in `TypeCheckProgram_test.go` (5 new on top of existing 9):

- Arithmetic flow check passes (`2 3 +`).
- Arithmetic flow check fails on type mismatch (`2 "x" +`).
- Unregistered builtin (`wl`) surfaces as unknown identifier —
  documents the migration boundary.
- AsCast against the live stack: `42 as Result` where
  `Result = int|str` passes; `42 as Flag` where `Flag = bool`
  fails with `TErrInvalidCast`.
- Varstore + getter + arithmetic: `2 n! :n 3 +` passes
  end-to-end.

Reserved-name parse-time discovery worth flagging: the lexer
treats single-character `x` as `INTERPRET` (a reserved token),
so `x` cannot be used as a variable name. Tests use `n` instead.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. Existing integration tests
unaffected (flag is opt-in).

#### What this unlocks (continuation)

Remaining V1 chunks:

- **Builtin table buildout.** The most impactful next step. Each
  registered builtin makes another class of program checkable.
  Order roughly by how often each appears in the test suite:
  `wl`/`print`, list ops (`len`, `map`, `filter`), string ops,
  Maybe pattern matching, etc.
- **Branch reconciliation wiring.** `if`/`else` and `match` block
  walkers that drive `Fork`/`CaptureArm`/`ReconcileArms` already
  built in `TypeBranch.go`. Match exhaustiveness is checkable
  via `CheckMatchExhaustive`.
- **Definition (`def`) sig parsing + body checking.** Parse the
  sig declaration, register a `QuoteSig` keyed by the def's
  name, type-check the body in a fresh `FnContext`.
- **Quote-body inference integration.** `InferQuoteSig` already
  exists (Phase 7); plug it into the quote walker case.
- **Stdlib hashing/caching** (per design line 765-772).
- **Phase 11:** delete `mshell/TypeChecking.go` once parity is
  reached.

### Phase 10 step 4b — Builtin table buildout (first pass) — DONE

`mshell/TypeBuiltins.go` grew a meaningful batch of common
builtins. The bar for inclusion is "appears often in real
programs and has a clean sig". As programs in the test suite
surface unknown-identifier errors under `--check-types`, the
right response is usually to add the offending builtin here.

Refactor: `builtinSigsByToken()` now takes `*TypeArena` so it
can build generic sigs (specifically STR's `(T -- str)`).
`Checker` constructor passes its arena in.

Builtins added:

**Stack manipulation (LITERAL, all polymorphic):**
- `dup`, `drop`, `swap`, `over`, `rot`, `nip`, `tuck`

**I/O (LITERAL, polymorphic consumers):**
- `wl`, `wle`, `print`, `printe`  — `(T -- )`
- `wln` — `( -- )`

**Boolean (LITERAL, except `not`):**
- `not` — `(bool -- bool)`, registered as NOT token (it's a
  reserved keyword, not LITERAL)
- `and`, `or` — `(bool bool -- bool)`

**Arithmetic helpers (LITERAL, overloaded across int/float):**
- `abs`, `neg`

**Numeric conversions (LITERAL, overloaded):**
- `toFloat` — `int -> float | float -> float`
- `toInt`   — `float -> int | str -> int | int -> int`

**List ops (LITERAL):**
- `len` — `[T] -> int | str -> int | {K: V} -> int`
- `append`, `push` — `([T] T -- [T])`
- `reverse` — `[T] -> [T] | str -> str`

**Type introspection (LITERAL):**
- `typeof` — `(T -- str)`

**Token-typed ops (byToken):**
- `STR` — `(T -- str)` generic conversion (lexer emits STR for
  bare `str` keyword in expression position; TypeExpr parser
  handles STR-in-type-position separately)
- `NOT` — `(bool -- bool)`
- `EQUALS` (`=`), `NOTEQUAL` (`!=`) — `(T T -- bool)` polymorphic
  (both inputs must unify, so `true 1 =` is rejected)

Tests added in `TypeCheckProgram_test.go`:

- `TestTypeCheckProgramRegisteredBuiltins` — 12 small programs
  using the new builtins all flow-check cleanly.
- `TestTypeCheckProgramBuiltinTypeMismatches` — three negative
  cases that the checker now catches: `true not 5 +`,
  `"hello" 1 +`, `true 1 =`.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green.

Known token-LITERAL gotchas worth flagging for future work:
- `not` is NOT (token), not LITERAL. Same convention applies
  to other reserved keywords.
- `str` is STR (token) in expression position, TidStr in type
  position. `typeof` was renamed from `type` for this reason
  (see Phase 10 step 1 notes).
- Single-character `x` is INTERPRET (reserved), can't be used
  as a variable name.

### Phase 10 step 4c — `if`/`else-if`/`else` branch reconciliation — DONE

`MShellParseIfBlock` is no longer a no-op stub. The walker now
drives every arm through `Snapshot`/`Fork`/`CaptureArm`/
`ReconcileArms` from Phase 6b's `TypeBranch.go`.

mshell `if` syntax (per `tests/else_ifs.msh`):

```
<condition> if
    <true body>
else* <condition2> *if
    <else-if body>
else
    <else body>
end
```

The condition for the main `if` lives on the stack at entry —
the runtime pops it before executing the body, and we mirror
that. Else-if condition bodies are inline code that push a
bool/int and then get popped before the arm body runs.

`(*Checker).checkIfBlock(ifBlock)`:

1. Pop condition from the live stack; report `TErrTypeMismatch`
   if it's not bool or int (`isBoolOrInt` resolves through the
   substitution first).
2. `Snapshot` the post-pop state.
3. Walk `IfBody`, capture as a non-diverged `BranchArm`.
4. For each `ElseIf`: `Fork(snap)`, walk its `Condition` items,
   pop and check the resulting bool/int, walk its `Body`,
   capture.
5. If `ElseBody` present: `Fork(snap)`, walk it, capture.
   Otherwise add an implicit "did nothing" arm equal to the
   snapshot — at runtime an else-less if may simply not fire.
6. `ReconcileArms` merges per-arm tails: stack sizes must
   match across non-diverged arms; per-slot types are unioned;
   var sets must agree.

`MShellParseMatchBlock` is still a stub (recurses into arm
bodies for nested casts but doesn't drive the stack). Match
arm dispatch is more involved because pattern semantics
depend on the matched value's static type — that's its own
chunk.

Tests added in `TypeCheckProgram_test.go` (4):

- `TestTypeCheckProgramIfBlock` — four happy-path forms:
  basic if/else, comparison condition, else-less if, full
  else-if chain.
- `TestTypeCheckProgramIfNonBoolCondition` —
  `"hello" if 1 wl else 2 wl end` rejected.
- `TestTypeCheckProgramIfStackSizeMismatch` —
  `true if 42 else end` rejected (true branch leaves 42, else
  branch leaves nothing).
- `TestTypeCheckProgramIfBranchTypeUnion` —
  `true if 42 else "hi" end wl` passes because the post-branch
  slot is `int|str` and `wl` accepts anything (polymorphic
  consumer).

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green.

### Phase 10 step 4d — `def` sig registration — DONE

User-defined functions (`def` declarations) now register their
signatures in the new Checker so call sites resolve correctly.
Body type-checking against the declared sig is a separate
chunk — for now we trust the declared sig.

Bridge: `mshell/TypeDefTranslate.go` translates the
old-parser-built `TypeDefinition` (`[]MShellType` + `[]MShellType`)
into a new-Checker `QuoteSig`. This lets every existing user
def get a sig in the new arena without touching the parser.
Phase 11 deletes the old type-system parse tree and the
translator with it.

Translation table:

| Old `MShellType`           | New `TypeId`                  |
| -------------------------- | ----------------------------- |
| `TypeInt{}`                | `TidInt`                      |
| `TypeFloat{}`              | `TidFloat`                    |
| `TypeString{}`             | `TidStr`                      |
| `TypeBool{}`               | `TidBool`                     |
| `TypeBinary{}`             | `TidBytes`                    |
| `TypeGeneric{Name}`        | fresh `TKVar` (name-scoped)   |
| `*TypeHomogeneousList`     | `MakeList(elem)`              |
| `*TypeQuote`               | `MakeQuote(...)` recursive    |
| `*TypeDictionary` (wild)   | `MakeDict(str, V)`            |
| `*TypeDictionary` (named)  | (skipped, error logged)       |
| `*TypeTuple`               | (skipped, error logged)       |

Generics scoping: each def has its own `map[string]TypeVarId`.
First occurrence of a name allocates a fresh `TypeVarId`;
later occurrences reuse it. All allocated IDs flow into
`QuoteSig.Generics` so `Checker.Instantiate` fresh-renames
at every call site.

`(*Checker).CheckProgram(file)` gained pre-pass 2:

```go
for i := range file.Definitions {
    def := &file.Definitions[i]
    sig, _ := TranslateTypeDef(c.arena, &def.TypeDef)
    nameId := c.names.Intern(def.Name)
    c.nameBuiltins[nameId] = append(c.nameBuiltins[nameId], sig)
}
```

Defs are appended to `nameBuiltins`, so they participate in the
Phase-9 overload dispatch the same way builtins do — a name with
multiple sigs (one builtin + one user def, or multiple user defs)
gets the most-specific resolution per call site.

Tests in `TypeCheckProgram_test.go` (4 new):

- `TestTypeCheckProgramDefRegisteredAtCallSite` —
  `def inc (int -- int) ... end  5 inc wl` clean-checks; the
  sig translates to `(int -- int)` and the call site unifies.
- `TestTypeCheckProgramDefCallSiteTypeMismatch` —
  `"hi" inc` with the same `inc` rejected as type mismatch.
- `TestTypeCheckProgramDefGenericIdentity` — `def id (T -- T)`
  used at two distinct types (`5 id`, `"x" id`); both pass
  because Instantiate fresh-renames T at each call site.
- `TestTypeCheckProgramDefList` —
  `def firstOf ([T] -- T)` against `[1 2 3] firstOf` passes.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. No stdlib changes.

What's still NOT done:

- **Def body type-checking.** The body should be checked
  against the declared sig in a fresh `FnContext`. The
  infrastructure exists; just needs wiring. This is the next
  obvious chunk.
- **`type X = ...` declarations inside def bodies.** Out of
  scope; type decls are file-level only by design.
- **Match block walker** (still a stub).
- **Quote-body inference integration** (Phase 7 hookup).

### Phase 10 step 4e — `def` body type-checking — DONE

Each user-defined function's body is now type-checked against
its declared signature. Bugs inside def bodies surface at check
time, not just call sites.

Pre-pass 3 in `(*Checker).CheckProgram`:

```go
for i := range file.Definitions {
    c.checkDefBody(&file.Definitions[i], defSigs[i])
}
```

`(*Checker).checkDefBody(def, sig)` flow:

1. Save outer state: stack, var env, currentFn, substitution
   checkpoint.
2. Reset stack and var env to empty (the body sees only its
   declared inputs and its own variable scope).
3. `Instantiate(sig)` to fresh-rename generics for this body
   check.
4. Push declared inputs onto the empty stack in declaration
   order.
5. Walk the body items through `checkParseItem` — which means
   the entire walker (operators, casts, if-blocks, vars, etc.)
   is available inside def bodies.
6. After the walk, verify the resulting stack length equals the
   declared output arity. If yes, unify each stack slot against
   the corresponding declared output. Report mismatches with the
   def's `NameToken` as the position.
7. Restore outer state. The substitution rollback ensures
   per-body bindings don't leak across defs.

Recursion works naturally: pre-pass 2 registers all sigs in
`nameBuiltins` before pre-pass 3 starts checking bodies, so a
def calling itself resolves through the same overload-dispatch
path as any other call.

Tests in `TypeCheckProgram_test.go` (3 new):

- `TestTypeCheckProgramDefBodyArityMismatch` —
  `def bad (int -- int) dup end` rejected (body produces 2
  outputs, sig declares 1).
- `TestTypeCheckProgramDefBodyTypeError` —
  `def bad (int -- int) true + end` rejected (`true 1 +`
  pattern: bool + int).
- `TestTypeCheckProgramDefRecursiveCallChecks` —
  `def rec (int -- int) rec end` clean-checks; the body's
  call to `rec` resolves against the pre-registered sig.

The earlier `TestTypeCheckProgramDefList` was updated: its
empty body would now correctly fail body checking
(`[T] -> T` with empty body leaves `[T]` on the stack), so the
test became `def listLen ([T] -- int) len end` which uses a
real builtin to satisfy the sig.

Verification: `go build ./...`, `go test ./...`, `./build.sh`,
`./tests/test.sh` — all green. Existing integration tests
unaffected (flag still opt-in; def body checks only run when
`--check-types` is set).

What's still NOT done:

- **Match block walker.**
- **Quote-body inference integration.** Phase 7's
  `InferQuoteSig` exists but isn't yet plugged into
  `MShellParseQuote`'s case in `checkParseItem`. With it,
  quotes pushed onto the stack would carry their inferred
  sigs, enabling higher-order builtins like `map` and
  `filter` to type-check.
- **More builtin sigs.** Especially the higher-order ops
  (`map`, `filter`, `reduce`, `each`, …).
- **Stdlib hashing/caching.**
- **Phase 11:** delete `mshell/TypeChecking.go`.

Original step-4 plan (kept for reference):

- After parsing, walk `file.Items` collecting `MShellTypeDecl`
  nodes and call `Checker.DeclareType(name, ResolveTypeExprAST(c,
  body))` to register them.
- Walk again, type-checking the program. At each `MShellAsCast`
  node, resolve target → `TypeId` and call `Checker.Cast`.
- Gate the whole pass behind `--typecheck` (off by default during
  fixture migration; flip to default once the test suite is clean).
- Sweep `.stderr` fixtures whose runtime errors the checker now
  catches statically.
- Delete the old `mshell/TypeChecking.go` (Phase 11).

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
