# Type checker enhancements: implementation handoff

Read [the design](type-checker-enhancements-design.md) completely first.
This guide is intended to be executable by an agent with limited prior mshell context.
Every review gate and open question is answered in design section 0; read it before coding.
Do not implement until the user gives the implementation instruction.

## 1. Scope and operating instructions

Work on `type-checker-enhancements`.
The starting commit is main `2d60ee1`.
This plan and its companion design are the only intended changes in the initial handoff.
Do not implement features, cherry-pick, commit, push, or modify source branches merely because this planning document exists.
Wait for the user's implementation instruction.

On implementation authorization:

1. Read repository AGENTS.md and the HTML documentation in doc/.
2. Read the companion design and resolve outstanding gates from conversation context.
3. Run `git status --short --branch` and preserve all existing user changes.
4. Establish a main baseline using the build and test commands below.
5. Implement one phase at a time and report the phase, tests, and unresolved failures.
6. Keep a short progress record under ai/ with completed phase IDs and actual commit IDs used.

Do not run gofmt without user permission.
Do not edit design/ or generated doc/build files directly.
Do not add changelog entries for every internal refactor.
Do not create agents unless explicitly authorized by the user or applicable instructions.
Do not fetch or cherry-pick a differently named branch on assumption: inspect refs first.
The branch names below are local remote-tracking snapshots, not promises of current upstream state.

## 2. Source-selection guide

Inspect each source commit using `git show --stat HASH` and `git show HASH -- relevant/files` before using it.
Prefer porting specific hunks when a commit includes old brand semantics or unrelated work.
`git cherry-pick --no-commit HASH` is useful only after inspecting the entire commit; it may import unwanted tests, documentation, and behavior.
Never resolve conflicts by accepting one whole side without reading it.
Do not reset user work to discard a bad pick.

### Enum branch: reuse constructors, tags, and patterns

Source tip: `origin/enum-types` at `7b7e03e`.

| Source | Use | Caveat |
|---|---|---|
| `0ca5b0b`, `27821a8`, `15782c0` | Initial enum parser, type, runtime and tests | Early syntax and interleaved fixes; not clean standalone picks |
| `dde7f95`, `52bfa28` | Final end-terminated payload syntax and optional leading pipe | Port the final parser state, not all intermediate parser bugs |
| `80be0a9` | Enum payloads referring to named types | Replace ordered brand declaration logic with shared predeclaration |
| `a4b24a0` | Recursive values and enum/union matching tests | Broad runtime changes; separate relevant coverage |
| `8abcaee`, `8087f6c`, `2e2f4b6` | Payload binding validation and name-collision rules | Include startup/def collision tests; inspect main's current equivalents |
| `c441027` | Startup declaration registration | Rework to the shared descriptor phases |
| `da5f6a9` | Match subject inference inside quotations | Do not pin a broad inferred subject from only its first arm |
| `d3a1835` | Wildcard token handling | Evaluate necessity and CLI/list-literal compatibility before importing lexer changes |
| `99ac5b1`, `3d6f3f9` | Regression tests only | Do not import one-layer brand unwrapping |
| `fc7ba60`, `b28b00a` | Syntax highlighting examples | Limit to new syntax; avoid unrelated grammar overhaul |
| `7b7e03e` | Historical design context | Do not copy its erased-brand/transparent-alias conflation |

Useful final files: mshell/TypeEnum.go, mshell/TypeEnum_test.go, enum changes in Type.go, TypeCheckProgram.go, TypeBranch.go, TypeParseIntegration.go, Evaluator.go, MShellObject.go, and tests containing enum in their names.

Runtime prerequisites require special care.
The branch contains a long sequence repairing recursive rendering/equality and DAG blowups:
`3413d03`, `debe03e`, `0d78c14`, `88d8de3`, `e7658a7`, `6341f99`, `2a326e3`.
Inspect their final implementation together; an early partial pick may recreate a known hang.
If enum runtime operations require the shared walker, bring it as an explicit reviewed prerequisite with its own tests.
Do not ship recursive enum values that hang on print/JSON/equality just to keep the diff small.
Changes such as `6ce234f` (gridSetCell), `67b3f55` (uniq), and `bdd26bd` (general equality) are not automatically in scope; document actual dependencies before inclusion.

### Recursive branch: reuse representation knowledge and regressions

Source: `fix/recursive-named-type-narrow-hang` at `4328701`.

- `51d33d8`: inspect predeclaration, recursive tests, precise HtmlNode/Json signatures, stdlib helpers, and docs.
- `f5178e6`: inspect rather than assuming the vague commit title is harmless.
- `34e7f63`: preserve the nested-reference termination regression, not its brand-specific workaround.
- `4328701`: inspect type-keyword fixes; keep the intent of preventing reserved keywords from becoming accidental generics.

Do not import branded-union placeholders, `casting` mode, recursive tag-in casts, or getter-specific nominal transparency.
Alias placeholders and closed-type traversal optimizations should be implemented against the new reference kind.
JSON integer parsing was approved 2026-09-14 (design section 0), so the Json descriptor includes both `int` and `float`; the recursive branch's int alternative is the intended shape.

### try-as branch: reuse syntax, boundary tests, and runtime cases

Source: `origin/try-as` at `5db3e65`.

- `7dce9f5`: inspect parser/lexer spelling, primitive/container checks, and boundary examples.
- `ba9937b`: preserve resource-error tests, but replace depth-only recursion handling with the chosen common validator worklist.
- `5db3e65`: merge commit; do not cherry-pick as a feature implementation.

Do not import TryCast's call to castOk.
Do not import a parallel AST-based runtime type environment, quotation kind-only certification, or ignored wildcard constraints.
Port checks to the resolved descriptor/PatternPlan engine.
The name `builtinNamedTypes` is used incompatibly by the recursive branch and try-as branch; resolve through one registry, not renaming both duplicates and retaining both.
Do not copy prose claiming as can narrow inferred unions or that recursive types are unsupported.

## 3. Expected architecture and file map

Exact helper names may vary, but keep the responsibilities separate.

| Area | Main files / proposed additions | Responsibility |
|---|---|---|
| Type graph | Type.go, TypeExpr.go, TypeParseIntegration.go | Stable declaration refs, structural nodes, enum IDs, shared resolution |
| Declaration loading | TypeCheckProgram.go, Main.go, lsp.go, proposed TypeDeclarations.go | Predeclare all names, resolve bodies, validate, register constructors |
| Static relations | TypeChecker.go, TypeUnify.go, TypeCast.go, proposed TypeRelations.go | Equality, assignability, inference constraints, static ascription |
| Pattern plans | Parser.go, TypeCheckProgram.go, TypeBranch.go, proposed Pattern.go | One pattern interpretation and coverage/binding analysis |
| Runtime tests | Evaluator.go, proposed TypeValidation.go | Shared descriptor validator and runtime pattern execution |
| Enum values | TypeEnum.go, MShellObject.go, Evaluator.go | Constructors, tags, payloads, observable operations |
| Sugar lowering | Parser.go / a dedicated elaboration pass | Hygienic tryAs and assertive destructuring expansion |
| User-facing support | doc/, lib/std.msh, code/, shell completions where applicable | Accurate semantics, examples, syntax highlighting |

Suggested interfaces, not mandatory Go signatures:

```text
ResolveDeclarations(items, visibleEnvironment) -> ResolvedDeclarations | errors
Equivalent(S, T, context) -> bool
Assignable(S, T, constraints) -> bool
DefinitelyDisjoint(S, T) -> bool  // conservative; optional initially
ResolvePattern(patternSyntax, declarations) -> PatternPlan | errors
AnalyzePattern(subjectType, plan) -> refinedType, bindings, coverage
Checkable(targetDescriptor) -> bool | explanatory error
RunPattern(value, plan, budget) -> matched(bindings) | mismatch | error
```

Malformed descriptors and budget errors must not be represented as ordinary mismatch.
The resolved type schema should define all variants centrally; add an exhaustive test covering all TypeKinds because a Go switch is not compiler-enforced exhaustive.
Do not force every static type to provide a successful validator: explicitly represent non-checkable types.

## 4. Phases and exit checks

### P0: decisions and baseline

- G1/G2/G3 and the other open questions are answered in design section 0; re-read it before starting.
- Inventory existing uses of type/as, especially in lib/std.msh, tests, startup handling, docs, and editor support.
- Record which existing tests must intentionally change because they assert old nominal-brand behavior.
- Rebuild and run all three required test commands; record pre-existing failures without masking them.

Exit: approved target semantics and a baseline report.

### P1: common declaration graph

- Add stable alias-reference and enum-declaration identities.
- Predeclare all names before body resolution.
- Validate guarded alias cycles and reject duplicates/unknown names deterministically.
- Wire startup, file execution, checker, and LSP to the same resolved declarations.
- Keep checker state (variable environment, substitution, declaration graph) able to persist across REPL lines; the REPL is intended to check by default.
- Preserve scope; do not expose later files' declarations prematurely or add local generativity.
- Implement finite formatting for recursive descriptors.

Exit: forward/self/mutual declaration tests, invalid-cycle tests, and diagnostic termination tests pass.

### P2: enum core and nominal migration

- Port final enum syntax, constructor signatures, tagged runtime values, and constructor patterns.
- Include only the runtime support actually needed for correct enum behavior.
- Test name collisions and checked/unchecked execution paths.
- Migrate erased-brand tests to either alias equivalence or explicit enum construction.
- Remove brand IDs from union semantics and eliminate TKBrand once no users remain.
- Do not give ordinary getters access to enum payloads; use patterns.

Exit: nominal ID/list/record/tree cases work, wrong nominal arguments fail, recursive runtime operations terminate.

### P3: relations and the G2 policy

- Separate alias-aware equivalence/assignability from inference variable binding.
- Implement guarded pair reasoning for recursive aliases with transactional union/overload trials.
- Keep generic-body variables rigid and preserve inference occurs checks.
- Fix shape presence/remainder handling and bottom directionality.
- Make list element and dictionary value types invariant in assignability: `[int]` is rejected where `[int | str]` is declared, and the reverse.
- Remove the two widening `append` overloads (`([t] u -- [t | u])`, `(t [u] -- [t | u])`) and audit every other mutating list/dict builtin so each write is checked against the container's static type.
- Keep the bind-once rule for an empty literal's element type; a mixed list is declared with `[] as [int | str]`.
- Reject writes to keys an open shape does not declare, unless a `*: T` remainder is declared.
- Explicitly audit lists/dicts reachable through Grid cells or other reference-bearing values; reject unsupported refinement paths until safe.

These two programs pass the checker on main and fail at runtime; both become `typecheck_fail` cases:

```mshell
[1 2 3] xs!
@xs "a" append drop
@xs (1 +) map
```

```mshell
def addLabel ([int | str] -- [int | str]) "total" append end
[1 2 3] nums!
@nums addLabel drop
@nums (1 +) map
```

Exit: both programs are rejected, a read-only generic `([a] -- str)` accepts a `[int]`, ordinary collection workflows pass, and no global cast mode remains.

### P4: shared patterns and typed validation

- Resolve all pattern forms into PatternPlan once.
- Implement `is TypeExpr binding` (or the approved replacement spelling).
- Add eager Checkable validation for every referenced target, including unused union arms and empty containers.
- Implement structural validation, enum identity/payload checks, cycle detection, and work budgets.
- Bind unknown list/dict contents conservatively.
- Make recognition, binding types, runtime execution, and exhaustiveness use the same pattern meaning.
- Support patterns inside inferred and explicitly typed quotations without first-arm over-narrowing.

Exit: typed checks behave consistently in direct match, quotations, and startup-defined types.

### P5: sugar and static ascription

- Lower tryAs through the typed-pattern operation with a hygienic temporary binding.
- Lower/execute => through the same match operation while preserving outer-scope bindings.
- Extend => to constructor and typed patterns without introducing another matcher.
- Implement as using assignability only; retain contextual literal typing.
- Preserve Maybe ? behavior and existing source diagnostics.
- Delete old validators and brand-specific match/getter/cast paths.

Exit: paired sugar/expansion tests agree on stack, bindings, results, evaluation count, and errors.

### P6: builtins, migration, and handoff

- Register Json and HtmlNode using the shared graph and actual parser representations.
- Audit every read-only list and dictionary definition in lib/std.msh and declare it generically (`[a]`, `{str: a}`); a definition that writes keeps its concrete element type. Checking is intended to become the default, so spurious invariance rejections from the standard library are bugs.
- Keep HTML helpers typed precisely and migrate nominal usages to constructors where intended.
- Update documentation source and doc/mshell.md, and rebuild docs.
- Update editor grammars and relevant completions if syntax/CLI surfaces changed.
- Add grouped user-facing CHANGELOG entries and a migration explanation.
- Run all required tests with a fresh binary; review the final diff for unwanted source-branch baggage.

Exit: acceptance matrix passes, docs match implementation, remaining limitations are explicit and approved.

## 5. Acceptance matrix

Each row needs a focused regression test or an existing test cited in the progress record.
Use integration tests for syntax/runtime behavior and Go tests for graph/constraint invariants.
Do not merely assert implementation details.

| ID | Case | Required result |
|---|---|---|
| A01 | Two aliases of [int], including alias-of-alias | Equivalent; len accepts both |
| A02 | UserId and OrderId singleton enums with int payloads | Cannot interchange at calls; explicit constructors work |
| A03 | Recursive Tree enum | Construction and exhaustive payload matching work |
| A04 | Recursive HtmlNode alias | keys, ordinary dict-consuming helper, children access work |
| A05 | Forward/mutual alias and enum references | Resolve independently of order within the unit |
| A06 | Alias-only cycles and union-only cycles | Deterministic declaration error; no hang |
| A07 | Nested aliases to enum/Maybe/list | Matching works without one-layer brand exceptions |
| A08 | Two distinct nominal alternatives sharing a payload type | Runtime tags distinguish them |
| A09 | `int | str` input, `tryAs int` | Accepted statically; runtime just/none according to value |
| A10 | `int | str` input, `as int` | Rejected without a proof that the value is int |
| A11 | Raw int tested against UserId enum | Never constructs UserId; none or definite-disjoint diagnostic |
| A12 | Invalid quotation-effect target, including nested list target | Rejected as non-checkable, not certified by kind |
| A13 | Unknown type in unused union arm or empty-list target | Error before inspecting value |
| A14 | Optional shape field absent / present wrong type | Success / mismatch respectively |
| A15 | Required key absent in generic dictionary | No static presence proof; runtime mismatch |
| A16 | Shape wildcard with incorrect extra field | Mismatch; wildcard is not ignored |
| A17 | Open shape containing untracked extra fields | values/dynamic get do not claim only declared field types |
| A18 | Recursive finite runtime value | Typed test succeeds and terminates |
| A19 | Cyclic value / work budget exceeded | Shared explicit error policy for tryAs and typed match |
| A20 | Shared DAG without cycle | Not falsely rejected as cyclic; no exponential repeated traversal |
| A21 | Named targets defined in startup files | Identical visibility in checker, runtime, and LSP |
| A22 | Adding enum member | Exhaustive matches missing it fail; wildcard matches remain valid |
| A23 | Duplicate payload binding name | Rejected before execution; no overwriting prior binding |
| A24 | => successful bindings used afterward | Correct scope and types, subject consumed once |
| A25 | => mismatch | Stops before installing any bindings |
| A26 | tryAs expansion | Exactly one input evaluation, Maybe output, no leaked temp binding |
| A27 | `:>` constructor arm | Retains enum value, not its payload |
| A28 | Unknown value matched as list, then element arithmetic | Reject unsupported element assumption; len still works |
| A29 | Inferred quote with alternatives from multiple types | Do not pin input to the first constructor's enum and discard other valid inputs |
| A30 | Generic repeated parameter and higher-order quote | Relations preserved across all slots; no failed-trial pollution |
| A31 | Inference variable required to equal [itself] | Occurs-check rejection; declared recursion unaffected |
| A32 | Failed union/overload trial involving recursion | Does not leave successful memo assumptions or bindings behind |
| A33 | Empty lists tested against element types | Do not diagnose definitely disjoint solely from different element types |
| A34 | `[int]` passed where `[int | str]` is declared, or a str appended to a `[int]` | Rejected at the call site or at the write |
| A35 | Nested container: `[[int]]` element passed to a `([int | str] -- ...)` definition, or written to through `nth` | Rejected by the same invariance rule; a `[[int]]` is not a `[[int | str]]` |
| A36 | Actual bottom vs expected bottom | Directional subtyping; arbitrary actual values do not satisfy bottom |
| A37 | Enum runtime str/JSON/equality/sort on deep data | Correct specified tagged behavior and termination |
| A38 | parseJson numbers | Runtime representation, Json descriptor, and typed checks agree |
| A39 | Direct dictionary getter versus literal-key get | Same field type/Maybe behavior, including optional fields |
| A40 | Unsupported refined Grid/quotation values in containers | No unsupported certification through a nested target |

### Concrete seed programs

These use the decided G1/G3 spellings.
They are target acceptance programs, not claims of compatibility with main today.

```mshell
type L = [int]
type U = L | str
def countList (U -- int)
    match list xs : @xs len, _ : 0, end
end
[1 2] countList
```

```mshell
enum UserId = userId int end
enum OrderId = orderId int end
def rawUserId (UserId -- int)
    match userId n : @n, end
end
42 userId rawUserId
# Separate negative file: 42 orderId rawUserId
```

```mshell
type ManifestData = {packages: [{name: str}]}
enum Manifest = manifest ManifestData end
'{"packages": [{"name": "a"}]}' parseJson tryAs ManifestData ? manifest
=> manifest data
@data :packages? (:name?) map
```

```mshell
def selectInt (int | str -- Maybe[int]) tryAs int end
1 selectInt ?
"bad" selectInt match just _ : 1 exit, none : , end
```

Refinement scenario under the G2 policy:

1. Create a container whose declared type allows int or str in a field, currently holding an int.
2. Keep two names for it.
3. Use `tryAs` or `is` to establish a required int field through one name, binding a new name at the narrower type.
4. Write a str through the other name.
5. Attempt integer arithmetic through the narrowed name.

Step 4 is legal for the wider name and step 5 is legal for the narrower one, so this program type-checks and fails at runtime.
This is the documented hole in section 0, rule 6: it requires narrowing a value already held under a wider container type, and the runtime's per-operation check stops the arithmetic.
Add it as a runtime test that documents the behavior, not as a checker test.
Every other route to the same state, plain assignability, `as`, and in-place writes, must be rejected by the checker; test those as `typecheck_fail` cases for direct match, `tryAs`, constructor payloads, callbacks, and nested lists.

## 6. Build and verification commands

Follow the repository scripts and baseline environment.
Both test runners expect freshly built binaries under mshell/ (runtime tests use mshell/mshell; typecheck tests use mshell/msh).

```sh
cd mshell
./build.sh
go test
cd ../tests
./test.sh
./typecheck_test.sh
```

If Go cache permissions require it, set a dedicated GOCACHE path under /tmp or the repository.
For sandboxed review worktrees, `-buildvcs=false` may be needed if Go cannot inspect Git metadata; record that environmental workaround.
Do not disable actual tests to hide build or environment failures.
Use small focused tests while developing a phase, then required full checks at integration boundaries and final handoff.

After documentation edits:

```sh
cd doc
msh build.msh
```

Ensure msh resolves to the intended freshly built binary and the documented stdlib environment.
Do not manually edit generated doc/mshell.html or doc/build outputs.

Before declaring completion:

- Review `git diff --check` and `git diff --stat`.
- Search for residual TKBrand, branded-union comparisons, cast modes, one-layer underlying helpers, and duplicated validation paths.
- Check that every retained occurrence is intentional and documented; do not use grep absence as the sole correctness criterion.
- Check docs for claims that as narrows, brands are transparent, quotes are validated by kind, or type declarations must execute before tryAs.
- Report baseline versus new failures accurately.
- Report approved deviations from this plan and unimplemented gates honestly.

## 7. Definition of done

All approved phases completed; acceptance cases covered; required builds/tests/docs checks pass.
The source-commit ledger identifies what was ported, skipped, or superseded.
No accidental full-branch merge, unrelated formatting, design/ edits, or silently changed runtime contracts.
The handoff explains nominal construction, structural validation, and the exact sugar relationships with examples.
If a review gate remains unanswered, the work is incomplete: state the gate and stop dependent implementation rather than choosing hidden semantics.
