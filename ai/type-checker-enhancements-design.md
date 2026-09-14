# Type checker enhancements: unified design

Date: 2026-09-14.
Branch: `type-checker-enhancements`, created from local `main` at `2d60ee1` (also the local `origin/main` tip).
Status: design proposal for review; no implementation changes have been made.
Implementation guide: [type-checker-enhancements-plan.md](type-checker-enhancements-plan.md).

## Read this first

The user wants nominal types, recursive data, useful boundary validation, and as few independent semantic mechanisms as possible.
Extra syntax is acceptable when it is demonstrably sugar over an existing mechanism.
Do not interpret the desire for fewer constructs as permission to remove nominal type safety.
Do not merge the three source branches wholesale.
Their individual tests pass, but their rules conflict in important places.

This document specifies a recommended destination, not a claim that every decision below has already been approved.
Before implementing behavior changes, obtain the decisions in the review gates below unless the user has subsequently approved them.
Approval to write this plan was not approval to change container semantics or migrate existing nominal declarations.
Do not ask again for decisions already answered in subsequent conversation.

Repository rules still apply: use a feature branch; never run gofmt without permission; do not edit `design/`; rebuild the binary before testing.
Older files in `ai/` and the enum branch's design document contain historical decisions and outdated syntax.
Use them as evidence, not as instructions that override this document or the user.

## 1. Review gates

### G1: declarations and migration

Recommended destination:

- `type Name = Expr` is a transparent structural alias, including guarded recursive aliases.
- `enum Name = constructor Payload ... | ... end` is the only nominal declaration mechanism.
- A one-constructor enum is the nominal wrapper/record form.
- Remove erased `TKBrand` types and brand IDs on unions after migration.
- `as` becomes static ascription only; it does not construct nominal values.

This changes the meaning of existing `type` declarations on main.
The user supports nominal typing but has not explicitly approved this spelling/migration choice.
Ask for approval of this destination before changing declaration semantics.
If the user wants `type` to remain a nominal spelling, propose an explicit constructor-bearing sugar and a separate alias spelling, then update this document with its exact grammar and expansion.
Do not silently keep the old erased-brand semantics alongside the new core.
Do not generate constructor names by capitalization: mshell is intentionally case-independent.

### G2: mutable containers and persistent refinements

Recommended for minimum type-system complexity: value semantics for ordinary lists and dictionaries, including nested containers.
An update returns a new logical value; another reference retains its old logical value.
This is a significant runtime migration, not an incidental checker fix, and requires explicit user approval.
Grid/GridView/GridRow have separate reference semantics; this recommendation does not silently change them.

If shared mutation must remain, a complete storage/alias/refinement policy is required before shipping persistent typed refinements.
Possible policies include ownership, alias-aware refinement invalidation, or runtime-enforced storage contracts.
The implementation agent must write and obtain approval of that policy as an addendum before implementing it.
It is not specified by this plan because the user has not selected it.
Do not claim soundness by merely returning a read-only view: another alias can still mutate the object.
Do not special-case `tryAs` to copy while equivalent typed patterns share the object.
Do not regard a shallow copy as isolation for nested mutable containers.

### G3: proposed typed-pattern spelling

This plan proposes `is TypeExpr binding` as a typed pattern.
`is` is contextual in pattern-head position, not a new globally reserved word.
The binding is mandatory and may be `_`.
Example: `is [int] items : ...`.
Approve this spelling along with G1, or replace it consistently in the parser plan and examples.
The semantics do not depend on the spelling.

Implementation may inspect code, prepare source-selection notes, and establish baselines before these gates are answered.
Do not fill unresolved decisions with ad hoc permissive behavior.

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

Recommended alias examples (depend on G1):

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

Typed patterns use the proposed form:

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
It can guide empty-literal inference and widen a type; it cannot pick one union alternative.
Checking a call to a nominal constructor checks its payload types and produces its declared enum type.
There is no special "casting mode" in generic unification.

`tryAs` and typed patterns do not require assignability before checking.
For example `(int | str -- Maybe[int]) tryAs int` is valid.
An optional diagnostic may reject or warn about tests proved disjoint, but the implementation must be conservative.
Do not reuse `castOk` as an overlap check.
Do not infer disjointness of `[int]` and `[str]`: empty lists can satisfy both structural predicates.

The key laws, subject to the approved storage policy:

- Alias replacement does not change acceptance.
- A constructor's result fits its nominal type, never another enum solely because payloads match.
- If a typed check succeeds with result typed T, the checked predicate establishes T.
- An `as T` never relies on a runtime check that did not happen.
- `tryAs T` and its match expansion have equivalent values, side effects, bindings, and failure behavior.
- `=>` and its assertive-match expansion have equivalent bindings and failure behavior.

Keep semantic equivalence, directional assignability, generic inference, and pattern refinement as distinct APIs/concepts.
Function applications instantiate generics; function bodies check rigid generics.
Quotation inputs are contravariant and outputs covariant under the permitted storage rules.
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

For V1 boundary validation, recommend rejecting cyclic value/type traversal paths with a resource/validation error; finite recursive trees remain supported.
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
Shape-to-dictionary compatibility must respect complete value information and the G2 write policy.

Preserve main's actual JSON number behavior in this work unless separately approved: current parsing produces floats for JSON numbers.
Give the built-in Json descriptor precisely the alternatives the parser produces; omit int if the parser remains float-only.
Do not make `tryAs int` silently convert integral floats.
If integer parsing is changed separately, update Json, runtime tests, documentation, and validation expectations together.

For HTML, the recommended minimal migration preserves the existing dictionary runtime:
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

The result is complete only when the implementation plan's acceptance matrix passes, migration documentation matches runtime behavior, and the approved G2 policy prevents stale refinements.
Deleting duplicated helpers without establishing those properties is not completion.
Do not ship a mechanically merged branch with a promise to reconcile the core semantics later.
