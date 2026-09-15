# Type checker enhancements: unified design

Date: 2026-09-14.
Branch: `type-checker-enhancements`, created from local `main` at `2d60ee1` (also the local `origin/main` tip).
Status: decisions recorded (section 0); implementation not started.
Implementation guide: [type-checker-enhancements-plan.md](type-checker-enhancements-plan.md).

## Read this first

The user wants nominal types, recursive data, useful boundary validation, and as few independent semantic mechanisms as possible.
Extra syntax is acceptable when it is demonstrably sugar over an existing mechanism.
Do not interpret the desire for fewer constructs as permission to remove nominal type safety.
Do not merge the three source branches wholesale.
Their individual tests pass, but their rules conflict in important places.

Section 0 records the user's decisions on every question this document raised.
Sections 1 onward are the destination and its rationale; where a later section still reads as a proposal, section 0 is the answer.
Do not ask again for decisions already recorded there.

Repository rules still apply: use a feature branch; never run gofmt without permission; do not edit `design/`; rebuild the binary before testing.
Older files in `ai/` and the enum branch's design document contain historical decisions and outdated syntax.
Use them as evidence, not as instructions that override this document or the user.

## 0. Decisions recorded 2026-09-14

These answers supersede the recommendations and open questions below.
Later sections are kept as the rationale; where they conflict with this section, this section wins.

| Topic | Decision |
|---|---|
| G1 declarations | Approved as recommended: `type` is a transparent structural alias, `enum` is the only nominal form, `as` is static ascription only. Migrate the five existing test files that declare types. |
| G2 mutation | Containers keep shared reference semantics. No copy-on-validate, no value semantics. Container types are invariant and every in-place write is checked against the container's static type; see the policy below. |
| G3 typed patterns | Keep `is TypeExpr binding`. It marks the one pattern class that performs full structural validation. Bare primitive keywords stay as they are. No further bare-name sugar for aliases in V1. |
| JSON numbers | A JSON number with no fraction and no exponent parses as `int`; others parse as `float`. The built-in `Json` alias includes both `int` and `float`. Update parser, `Json`, runtime tests, and docs together. |
| Operations on raw `Json` | None. `Json` is an ordinary union; getters, `map`, `len`, and similar are type errors on it. Narrow with `match`, `tryAs`, or `=> is`. |
| Validation failure diagnostics | Unchanged for V1. `tryAs` yields a bare `none`; a failing `?` is the signal that the data did not conform. |
| Enum value behavior | As shipped on the enum branch: `toJson` uses the externally tagged form (`"member"`, `{"member": v}`, `{"member": [v0, v1]}`); `str` prints `member` or `member(p0 p1)`; equality compares enum name, member, then payloads; ordering compares enum name, member declaration index, then payloads. |
| Type parsers | Share the type parser between `type`, `is`, `tryAs`, and `def` signatures wherever the grammars agree. Do not maintain two spellings for the same shape. |
| Coverage of `is` arms | An `is T` arm covers a union member only when that member is equivalent to `T`. Anything else contributes no coverage and the match needs `_`. |
| Redeclaration | No special cases. A duplicate declaration is an error everywhere, including interactive sessions. |
| Name collisions | No shadowing in any direction. A constructor, type name, or definition that collides with an existing name is an error. Namespacing is future work. |
| Checking by default | Work as if `--check-types` will become the default, including in the interactive REPL. It may be a while before the switch is made, but every decision here is judged as if all user code is checked. |

### G2 policy: shared mutable containers, invariant container types

Decided 2026-09-14 after reviewing how Java, C#, Dart, TypeScript, and Python handled the same question.
Containers keep shared reference semantics at runtime.
The checker keeps every container's static type fixed, so a write through one name can never invalidate what another name knows.

1. List element types and dictionary value types are invariant.
   Wherever a value meets a declared type (definition parameters, `as`, quotation signatures, enum payloads), `[int]` is not accepted for `[int | str]`, and `[int | str]` is not accepted for `[int]`.
   A read-only function over lists is declared generically, `([a] -- str)`; that is the covariant read-only view, and most of the standard library already reads this way.
   A literal passed to a union-typed parameter is written `[1 2 3] as [int | str] f`, because a postfix language types the literal before the word that consumes it.
2. Every in-place write is checked against the container's static type.
   `setAt`, `insert`, `extend`, and dict `set` already do this on main.
   Remove the two widening overloads of `append` (`([t] u -- [t | u])` and `(t [u] -- [t | u])`) so `append` does too.
   Audit every other mutating list and dict builtin for the same property.
3. An empty literal gets a type variable for its element type, and the first unification fixes it.
   Later writes must match.
   A mixed list is declared up front with `[] as [int | str]`.
   This is the same rule ML applies to a mutable cell created empty.
4. Shape width subtyping is allowed: `{name: str, age: int}` is accepted where `{name: str}` is declared.
   Writing a key the static shape does not declare is an error unless the shape declares a `*: T` remainder, in which case the value is checked against `T`.
   Declared field types are invariant.
   Reading an undeclared key through an open shape yields `Maybe[value]` with the unknown remainder, never a declared field's type.
5. `Maybe[T]` is covariant because it is immutable.
   Quotation inputs are contravariant and outputs covariant, with the container rules applied inside.
6. Typed patterns and `tryAs` bind a new name at the target type and leave the subject binding at its original type.
   Known hole, documented rather than prevented: narrowing the same container twice to two different container types and writing through the wider name.
   This is not reachable through plain assignability because of rule 1.
   The runtime's per-operation checks still stop the wrong operation.

No loop re-check is needed under this policy, because no static type changes after it is fixed.

Because checking is intended to become the default, three things follow for this work:

- Every read-only list or dictionary function in `lib/std.msh` must be declared generically (`[a]`, `{str: a}`), so a user never meets a spurious invariance rejection from the standard library. This audit is part of P6, not optional cleanup.
- The REPL will check each line against the state left by earlier lines, so the checker's variable environment, substitution, and declaration graph must persist across lines in the same way the evaluator's state does. P1 must design the declaration graph with that in mind.
- The deferred literal-widening rule below moves up in priority if `as` on literals turns out to be common once the stdlib is generic.

Deferred, not rejected: letting a literal's element type variable widen when the literal meets a wider declared parameter, so `[1 2 3] f` works without `as`.
It is sound (every name shares the variable, and declared types never widen) but needs a rebindable type variable, a widening rule in container unification, and a loop re-check to a fixpoint.
Build it only if `as` on literals turns out to be common in real scripts.

The acceptance rows A34 and A35 are covered by rules 1 and 2: the write through the wider binding is rejected at the call site or at the write, and the documented hole in rule 6 is the only accepted exception.

## 1. Decided questions (formerly review gates)

### G1: declarations and migration

Decided: approved.

- `type Name = Expr` is a transparent structural alias, including guarded recursive aliases.
- `enum Name = constructor Payload ... | ... end` is the only nominal declaration mechanism.
- A one-constructor enum is the nominal wrapper/record form.
- Remove erased `TKBrand` types and brand IDs on unions after migration.
- `as` is static ascription only; it does not construct nominal values.

This changes the meaning of the `type` declarations on main.
No `type` declaration exists in `lib/std.msh`; five test files declare types and are migrated as part of P2.
Do not keep the old erased-brand semantics alongside the new core.
Do not generate constructor names by capitalization; identifiers are case-sensitive and capitalization carries no meaning.

### G2: mutable containers and refinements

Decided: containers keep shared reference semantics at runtime, and container types are invariant in the checker.
The full policy is in section 0.
Value semantics, copy-on-validate, ownership, and alias-aware refinement invalidation were considered and rejected.
Do not claim soundness by returning a read-only view of a shared object, and do not special-case `tryAs` to copy.

### G3: typed-pattern spelling

Decided: `is TypeExpr binding`.

`is` is contextual in pattern-head position, not a new globally reserved word.
The binding is mandatory and may be `_`.
Example: `is [int] items : ...`.
`is` marks the one pattern class that performs full structural validation of the subject; the other pattern heads test a kind, a tag, a literal, or a length or key presence.
Bare primitive keywords (`int n`) keep their existing meaning; for primitives the kind test and the full check coincide.
No further bare-name sugar for aliases in V1.

## 2. Minimal semantic core

| Mechanism | Responsibility | Must not do |
|---|---|---|
| Structural type expressions | Describe primitive values, containers, shapes, unions, and quotation effects | Introduce nominal identity |
| Alias declarations | Name structural descriptions and tie guarded recursive references | Construct or convert values |
| Nominal declarations/constructors | Create a distinct type and its tagged values | Infer identity from matching payload structure |
| Static ascription (`as`) | Check an already justified static description | Validate at runtime or narrow without evidence |
| Patterns and `match` | Test, refine, bind, and select a branch | Invent types or silently convert payload representations |

`tryAs`, `=>`, and generated nominal accessors elaborate into this core.
Recursion is a property of declaration references, not an additional cast operation.
Maybe is conceptually a parameterized nominal sum; its current specialized runtime can remain.
Generic user enums are deferred, so do not prematurely replace Maybe's implementation.

## 3. Type expressions and declarations

Retain primitives, `[T]`, `{str: T}`, field shapes, `Maybe[T]`, untagged `T | U`, and quotation stack effects.
Unions describe alternatives already present in the runtime value.
Enums distinguish constructors even when their payload types are identical.
Neither subsumes the other without changing runtime representations.

Alias examples:

```mshell
type Names = [str]
type ManifestData = {packages: [{name: str}]}
type Json = null | bool | int | float | str | [Json] | {str: Json}
```

The last line describes the built-in concept; users may not redeclare reserved `Json`.
Aliases with equivalent bodies are equivalent types.
Names should be retained for diagnostics, not compatibility barriers.
Hashconsed ID equality is a fast sufficient equality check; unequal IDs are not proof of semantic inequality for recursive aliases.

Nominal examples:

```mshell
enum UserId = userId int end
enum OrderId = orderId int end
enum Tree = leaf int | branch Tree Tree end
enum Manifest = manifest {packages: [{name: str}]} end
```

`userId` has stack effect `(int -- UserId)`.
The `userId n` pattern binds `n : int`.
`UserId` and `OrderId` remain different types, regardless of identical payload representations.
Constructors remain globally unique ordinary words, matching the enum branch's current namespace policy.
Reject collisions with builtins, definitions, other members, and language keywords.
Retain the optional leading `|` and mandatory `end` from the enum branch.
Retain type-primary payload parsing; a union used as a payload must be named through an alias.
Do not restore the obsolete `member(T...)` syntax from its implementation-plan prose.

Constructor visibility is public in V1.
This protects against accidental interchange, not deliberate construction of semantically invalid domain values.
Opaque modules/smart-constructor enforcement are separate future work.
Nominal wrapping does not itself establish properties such as positivity or valid configuration.

## 4. Patterns: one interpretation, two consumers

Parse each pattern into a resolved PatternPlan rather than independently rediscovering its meaning in recognition, binding, exhaustiveness, and runtime code.
The static consumer computes possible matches, bound types, and coverage.
The runtime consumer tests the value and produces bindings.
Both reference the same declaration/type descriptors.
They cannot share literally every algorithm: static approximation and runtime inspection answer different questions.
They must share the meaning of each pattern form.

Retain existing literal, wildcard, list, dictionary, and Maybe patterns.
Add enum constructor patterns from `enum-types`.
Extend assertive destructuring to constructor and typed patterns through the same PatternPlan.
Bindings must be valid names or `_`; reject duplicate non-wildcard bindings before executing any arm.
On mismatch or malformed pattern, never install partial bindings.

Typed patterns use this form:

```mshell
@input match
    is ManifestData data : @data :packages? len,
    _ : 0,
end
```

Parse `is`, then exactly one full type expression using the shared type parser, then exactly one binding identifier or `_`.
Reject missing bindings, extra tokens, malformed types, and ambiguous parses.
Test union, shape, quotation, and nested type expressions explicitly.
Runtime-checkability is a later semantic check, not a grammar restriction.

Primitive type patterns (`int n`, etc.) use the same primitive predicate.
`list` and `dict` kind patterns only establish the runtime kind, not arbitrary element or field facts.
If the subject contains known list alternatives, retain their element information.
For an unknown subject, use an internal unknown element type, not a fresh inference variable that can become `int` just because the body performs addition.
The internal unknown type is not an unchecked `any`; operations requiring facts about it are rejected until refined.
Do not add public unknown-type syntax without a concrete need.

Constructor patterns test a tag and bind payloads at their declared types.
An enum type-name pattern tests existing enum identity; it does not expose its payload.
Bare enum type-name patterns may remain sugar for `is EnumName _`.
Structural aliases may be tested through `is Alias binding`, which performs the complete check.
Do not reinterpret a dictionary destructuring pattern as a full field-type validator.
`{"name": n}` requires that key and binds its value; it does not prove `n : str`.

Conceptually, matching refines the subject to its intersection with the predicate's accepted values.
This does not require public intersection/negation syntax or a complete general semantic-subtyping solver.
Use conservative approximations; never grant a fact that the runtime predicate did not establish.
Exhaustiveness may conservatively require `_` when full coverage cannot be proved.
Subtract only coverage actually proved, accounting for earlier arms.
An impossible arm has an empty refinement; do not silently treat it as the original subject type.

`:>` retains the original subject value with its justified branch refinement; it never replaces it with an extracted payload.
A retained enum remains the enum.
With the destination design there are no erased nominal wrappers for match to peel.

## 5. Exact sugar contracts

### Assertive destructuring

```mshell
value => pattern
```

means a consuming single-arm match with an empty success body and a failure that stops execution.
The successful bindings are available after the expression in the current scope.
The existing restriction requiring at least one binding can remain.
If the current generic match lowering loses those bindings, repair scope handling or keep an elaborated node with equivalent semantics; do not silently change `=>` scope.
Preserve recognizable source locations and existing useful failure diagnostics.

### Checked refinement

```mshell
value tryAs T
```

elaborates conceptually to:

```mshell
value match
    is T freshInternalBinding : @freshInternalBinding just,
    _ : none,
end
```

The binding must be hygienic and must not escape.
Evaluate and consume the input exactly once.
Success has type `Maybe[T]` and contains the original value under the chosen common value/alias policy.
Mismatch produces `none`.
Malformed targets, unsupported validation, and exhausted validation resources are errors, not mismatches.
Every error/resource rule is shared with the corresponding typed pattern.

`tryAs T ?` is the above followed by the existing Maybe unwrap behavior.
Do not add `as?`, `as!`, or another fatal-cast primitive.
Only the Maybe meaning of `?` is discussed here; do not alter its unrelated command-execution uses.

### Nominal accessors

An accessor for a one-constructor record is an ordinary definition that pattern-matches its constructor then performs a field lookup.
Generation may automate that definition later.
Do not preserve a separate brand-unwrapping getter rule.
Do not add accessor-generation syntax in V1; ordinary definitions suffice.

## 6. Static ascription, validation, and construction

`value as T` requires assignability from the source to T.
It can guide empty-literal inference and, for a fresh literal, declare a wider element type (`[1 2 3] as [int | str]`); it cannot pick one union alternative, and it cannot change the container type of a value that already has one.
Checking a call to a nominal constructor checks its payload types and produces its declared enum type.
There is no special "casting mode" in generic unification.

`tryAs` and typed patterns do not require assignability before checking.
For example `(int | str -- Maybe[int]) tryAs int` is valid.
An optional diagnostic may reject or warn about tests proved disjoint, but the implementation must be conservative.
Do not reuse `castOk` as an overlap check.
Do not infer disjointness of `[int]` and `[str]`: empty lists can satisfy both structural predicates.

The key laws:

- Alias replacement does not change acceptance.
- A constructor's result fits its nominal type, never another enum solely because payloads match.
- If a typed check succeeds with result typed T, the checked predicate establishes T.
- An `as T` never relies on a runtime check that did not happen.
- `tryAs T` and its match expansion have equivalent values, side effects, bindings, and failure behavior.
- `=>` and its assertive-match expansion have equivalent bindings and failure behavior.

Keep semantic equivalence, directional assignability, generic inference, and pattern refinement as distinct APIs/concepts.
Function applications instantiate generics; function bodies check rigid generics.
Quotation inputs are contravariant and outputs covariant; list element and dictionary value types inside them are invariant (section 0, G2 policy).
Preserve occurs checks for inference variables; explicit declaration recursion is not permission to infer `a = [a]`.
Audit `TidBottom`: actual bottom can flow into any expected type, but arbitrary actual values must not satisfy expected bottom.
Preserve bottom as the empty/divergent case, not an unchecked wildcard.

## 7. Runtime-checkable types

Support primitives, lists, dictionaries, field shapes, unions, Maybe, transparent references to supported types, and enums with runtime identity.
For enums, validate existing identity and check payload obligations as needed when values may originate in unchecked execution.
Do not turn a raw payload or JSON object into an enum during a type test.
The tag is not a substitute for payload validation when unchecked construction can violate a declaration.

Reject quotation-effect targets in V1, even inside another target type.
A quotation runtime-kind test can remain valid, but must not certify an arbitrary `(A -- B)` signature.
Supporting checked quotation signatures later requires genuine evidence or contracts, not a kind test.
Reject runtime tests against unresolved generic variables.
Schema-specific Grid types require a real schema check; do not grant them through a Grid kind test.

Validate the whole target descriptor eagerly before inspecting data.
Unknown names and invalid targets must fail even for empty lists, `none`, or a union whose first arm matches.
Do not make declaration errors value-dependent.

For V1 boundary validation, reject cyclic value/type traversal paths with a resource/validation error; finite recursive trees remain supported.
Use an iterative worklist and active-path tracking keyed by value identity and resolved target ID.
Repeated sharing in a DAG is not a cycle; completed pair results may be memoized.
Resource budgets count bounded work and must be shared by `match is` and `tryAs`.
Do not silently map resource exhaustion to `none`.
Do not use a depth cap as the semantic definition of recursive type compatibility.
If cyclic graph acceptance is desired instead, explicitly specify its coinductive meaning before implementation.

## 8. Shapes, containers, and data boundaries

Dictionary keys remain strings.
Optional fields and Maybe remain distinct: an absent key versus a present optional value.
Plain structural shape annotations permit extra keys.
An open shape therefore needs an unknown remainder for generic `values`/dynamic-key lookup.
Exact literal/builtin shapes may retain a known complete field set.
If a value is widened to an open shape, do not continue to claim that its only values are those of its declared fields.
Represent this distinction internally (for example exactness plus a remainder type); do not require new public syntax for exact shapes in V1.

`*: T` in a shape constrains extra, undeclared fields to T.
Named fields retain their own explicitly declared types.
Store and check this remainder in both static and runtime models.
Do not parse a wildcard constraint and discard it.
A homogeneous dictionary does not prove any particular required key exists.
Remove the current unsound general dict-to-required-shape compatibility rule.
Shape-to-dictionary compatibility must respect complete value information and the G2 policy: an open shape with an unknown remainder is not a `{str: T}`.

JSON number parsing changes as part of this work (approved 2026-09-14): a number with no fraction and no exponent parses as `int`, all others as `float`.
The built-in Json descriptor includes both `int` and `float`.
Do not make `tryAs int` silently convert integral floats.
Update parser, Json, runtime tests, documentation, and validation expectations together.

For HTML, the minimal migration preserves the existing dictionary runtime:
define built-in HtmlNode as a transparent recursive structural alias with the precise node fields.
This preserves keys/getters/serialization and does not force a new tag into parseHtml output.
Nominal domain wrappers can contain an HtmlNode through constructors if callers want that distinction.
If the user specifically wants parseHtml to return a nominal enum instead, treat it as a separate approved runtime/API migration with updated helpers and serialization tests.
Do not call a nominal wrapper "a dictionary" without describing its payload boundary.

## 9. Shared declaration graph

Resolve all declarations in a compilation unit in phases:

1. Register enum and alias names with stable declaration IDs.
2. Resolve all bodies against the complete visible name environment.
3. Check alias contractiveness and type well-formedness.
4. Finalize enum payloads and register constructor signatures.
5. Resolve definition signatures, then check bodies.

Run equivalent declaration loading for startup files, normal files, quoted execution paths where applicable, and the language server.
Do not require a `type` statement to execute before a statically visible descriptor can be tested.
Retain existing top-level declaration scope; do not add local generative declarations.
Reject duplicate declarations deterministically.

Aliases use reference nodes with stable IDs and resolved bodies.
An alias cycle must cross a structural constructor (list, dict/field, Maybe, quotation) to be accepted.
Reject `type A = A`, `type A = B; type B = A`, and `type A = int | A` (the semicolon here is prose separation, not example mshell syntax).
A nominal enum reference is an identity boundary and terminates ordinary compatibility traversal.
Enum construction guards its own recursive payloads.

Recursive structural equality/assignability needs a directional pair-obligation algorithm with guarded recursive assumptions.
Do not use one global visited set across failed union alternatives or overload trials.
Rollback constraints and provisional recursive assumptions together.
Memoization involving unresolved inference variables must be scoped to the substitution state.
Keep supported subtyping conservative rather than claiming completeness for arbitrary unions, intersections, and recursive function types.

## 10. Evidence and known integration conflicts

Reviewed local snapshots:

- main: `2d60ee1`.
- recursive branch: `fix/recursive-named-type-narrow-hang`, `4328701`.
- enum branch: `origin/enum-types`, `7b7e03e`.
- checked-cast branch: `origin/try-as`, `5db3e65`.

The enum branch passes Go/runtime tests and 212 type-check cases.
The try-as branch passes Go/runtime tests and 214 type-check cases.
These were separate builds, not a tested combined implementation.
Additional review probes found:

- Single-constructor ID/list/recursive-record enums work and distinct IDs are rejected at calls.
- Two erased brands around an enum fail checker pattern recognition although runtime matching succeeds.
- `tryAs int` in a function taking `int | str` is rejected by static cast compatibility.
- `42 tryAs UserId` grants the existing erased brand after checking only int.
- Runtime quotation validation accepts a quotation with the wrong declared effect.
- Shape wildcard validation ignores extra-field constraints.
- Recursive structural targets validate at runtime but fail declaration resolution in the try-as checker.

The final two quotation/wildcard probes were runtime-only tests; do not claim those exact scripts passed static checking.

Important references:

- [Recursive subtyping](https://www.cs.cornell.edu/~kozen/Papers/sub.pdf): finite graph reasoning for recursive type relations.
- [Iso-recursive types](https://i.cs.hku.hk/~bruno/papers/oopsla20-recursive.pdf): explicit introduction/elimination for recursive representations; nominal identity is a separate choice.
- [Nominal and structural subtyping](https://www.cs.cmu.edu/~aldrich/papers/ecoop08.pdf): different guarantees and complementary uses.

## 11. Completion criterion

The result is complete only when the implementation plan's acceptance matrix passes, migration documentation matches runtime behavior, and the G2 policy in section 0 is enforced: invariant container types and every in-place write checked against the container's static type.
Deleting duplicated helpers without establishing those properties is not completion.
Do not ship a mechanically merged branch with a promise to reconcile the core semantics later.
