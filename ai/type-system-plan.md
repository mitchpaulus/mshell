# mshell type system: implementation plan

Date: 2026-09-29.
Specification: the Typst design doc, `ai/type-core-calculus.typ`. This plan says how to build it.
Where the two disagree, the Typst design doc wins, and one of them gets fixed.

This plan replaces the earlier type-checker design and plan files (section 3).
The decisions from them that still hold are now in the Typst design doc.

## 1. How to work from this plan

- Read the Typst design doc first, all of it. Read `formal-ver/README.md` for what the proof covers.
- Read the user documentation in `doc/` for the language itself, and `AGENTS.md`/`CLAUDE.md` for repository rules.
- Work on `type-checker-enhancements`, merging `main` in regularly. Never run `gofmt` without permission. Do not commit or push unless asked.
- Build both binaries before testing: `tests/test.sh` runs `mshell/mshell`, and `tests/typecheck_test.sh` runs `mshell/msh`.
- If a type signature in `lib/std.msh` looks wrong, stop and ask before changing it.
- If an existing test fails, stop and ask before changing the test or the code it tests.
  The one exception is a test listed in this plan as intentionally changing.
- Keep a short progress log in `ai/type-system-progress.md`: stage, what was done, commits, test results, and anything left open.
- Stages are named, not numbered with letters. In the Typst design doc, P1–P13, R6 and H1–H3 name counterexample *programs*; this plan uses those names only for those programs.

## 2. Open questions

This is the only list of open questions: add new ones here, and once one is answered,
put the answer in the Typst design doc and remove the row.

| # | Question | Context |
|---|---|---|

## 3. Files

Current:

- `ai/type-core-calculus.typ`: the specification.
- `ai/type-system-plan.md`: this plan, including every open question.
- `ai/type-system-progress.md`: the progress log, created when work starts.
- `formal-ver/`: the Rocq proof of the core.
- `formal-ver/oracle/`: the proved decision procedures for `≤` and `⊑` and the branch join, extracted to an OCaml program, for comparing with the Go port (stage 2).
- `ai/check_survey.sh`: runs `--check-types` over the test corpus; useful for stage 0.
- `tests/typecheck_test.sh`: every `tests/success` program must pass the checker, and every `tests/typecheck_fail` program must fail it. Since stage 6 there is one checker and no skip lists.

Removed 2026-09-29 as superseded (recoverable from git):
`ai/type-checker-enhancements-design.md`, `ai/type-checker-enhancements-plan.md`, `ai/type_checker.md` (the plan for the current checker), `ai/mutation-effects-exploration.html` (an approach to mutation that was not taken).
The enum, try-as and recursive-type branches carry their own design notes (`design/literal_or_enum_typing.html`, `ai/enum_implementation_plan.md`, `ai/json_boundary_casts.md`, `ai/tryas_review.md`, `ai/recursive_types_and_keyword_types.md`). Those are superseded too; do not bring them over when porting code.

## 4. Where the code is today

Facts from reading `main` at `b511d9b`, with file references, so this plan can be checked against them.

### The checker

About 6,600 lines of Go outside tests (`mshell/Type*.go`), plus about 4,300 lines of Go tests and 62 files in `tests/typecheck_fail/`.
`tests/typecheck_test.sh` requires every `tests/success/*.msh` (207 files) to pass `--type-check-only`, and every `tests/typecheck_fail/*.msh` to fail. Only exit codes are compared.

Reusable as they are, or nearly:

- `Type.go`: the hashconsed type store (`TypeArena`, `TypeId`), name interning, union flattening.
- `TypeExpr.go`, `TypeParseIntegration.go`: the type-expression parser. The parser keeps `*: T` in shapes, but the resolver drops it (`TypeExpr.go:151-159`); that needs fixing.
- `TypeError.go`: error kinds and formatting.
- The substitution half of `TypeUnify.go`: variables, bind with occurs check, checkpoints.
- `TypeBuiltins.go`: about 200 registration calls (many inside loops over name lists) plus 30 token entries, written in signature syntax. The text is reusable. The entries still need auditing and the new freshness marks.

Replaced, because the Typst design doc removes what they do:

| Current mechanism | Where | Calculus replacement |
|---|---|---|
| `unify` doing equality, subtyping and dict↔shape conversion together | `TypeChecker.go:1405-1682` | equality unification only; subtyping checked separately, only where both sides are known (§Inference) |
| union matching that commits to the first arm that fits; union distribution over overloads | `unifyTypeToUnion`, `tryDistributeOverUnion` | distinct-kind unions; a `match` per member, written or elaborated |
| overload fan-out into several branches (`branchSpawn`), candidate order mattering | `TypeOverload.go` | choose the candidate from the known argument types; ambiguous at the end of the def is an error |
| quotes with several signatures (`TKOverloadedQuote`) | `TypeQuote.go` | one quote type; a literal quote given to a builtin is checked against that builtin's parameter |
| `inputUnifyOrder` | `TypeOverload.go:318` | not needed without the above |
| `readOnlyArgs` and the `mutatingBuiltins` list | `TypeChecker.go:216, 342, 1508` | invariance everywhere; read-only builtins are generic |
| freshness as "count of fresh slots from the top" | `TypeChecker.go:181-208`, `TypeCheckProgram.go:602` | a fresh mark on each stack slot; deep freshness; the freshness mark on builtin outputs |
| erased brands (`TKBrand`, branded unions) | `TypeCast.go:42-105` | transparent aliases and enums |
| string-literal types (`TKStrLit`) | `Type.go`, `tryGetLiteralKey` | a literal key before `get` is syntax |
| `exit` as "pushes a bottom value", the global `diverged` flag | `TypeChecker.go:1381, 420` | divergence on the stack effect; `never` |
| `break`/`continue` accepted anywhere | `TypeChecker.go:562` | break and continue contexts |
| `as` that tags, untags, and widens | `TypeCast.go:176` | `as` = subtyping; widening only for fresh values |
| variables retyped on each store | `TypeCheckProgram.go:813-856` | one type per variable per scope |

Hand-written special cases in `checkOne` that must be carried over in some form:
`tryIff`, `tryLoop`, `tryRedirect`, `tryCapture`, `applyMergeRedirect`, `tryExecCommand`, `tryReturn`, `tryGridJoin`, `tryGetLiteralKey`, `tryPivot`, `tryRejectPathWrite`, `tryAppend`, `dbg`, the bare-word-in-list-literal rule, and format-string interpolation checks.

Invocation: `--check-types` and `--type-check-only` (`Main.go:516-535, 860-876`) call `TypeCheckProgram` (`TypeCheckProgram.go:49`). The LSP builds its own checker (`lsp.go:739-763`). The REPL never runs the checker.

### The runtime

- **No runtime error classification.** About 930 `FailWithMessage`/`failPtr` calls in `Evaluator.go` report type mismatches, index errors, `?` on none and process failures the same way.
- Redirect words (`*`, `>`, `2>&1`, ...) change the list object in place (`Evaluator.go:11565-12641`).
- `updateCol` on a `Grid` swaps a new column into the same grid; `gridAddCol`, `gridRemoveCol`, `gridRenameCol` and `gridSetCell` change the grid in place (`Evaluator.go:8662-8954`). Views and rows have no write operations, but they see writes to the grid.
- `...rest` (`Evaluator.go:1676`) and pipe slices (`MShellObject.go:1554, 1640, 1772`) share the source's storage. `take`, `skip` and list slices copy.
- `Maybe` equality is broken on `main`: `Maybe.Equals` (`MShellObject.go:245`) asserts the value type `Maybe`, but the runtime holds `*Maybe`, so every comparison of two `Maybe`s is false, `none none =` included. The enum branch has a fix (`622ce23`).
- List equality is not defined (`MShellList.Equals` returns an error), and `<`/`sort` do not compare containers (`sort` compares the items' strings).
- Printing, JSON and equality recurse, so a very deep value can overflow the Go stack, and a cyclic one hangs. The enum branch replaced them with iterative walkers that detect cycles (`debe03e`, `0d78c14`, and the fixes after them).

### Older branches

None of their work is on `main`.

| Branch | Tip | Behind `main` | What to take |
|---|---|---|---|
| `origin/enum-types` | `7b7e03e` | 62 commits | enum parser (final `end` syntax, `dde7f95`, `52bfa28`); runtime value `MShellEnum`; constructors; match arms; exhaustiveness; iterative walkers; `Maybe` equality fix; name-collision checks; tests |
| `origin/try-as` | `5db3e65` | 37 | the `tryAs` parser node; validation examples and tests. Not `castOk` reuse or the depth-only limit |
| `fix/recursive-named-type-narrow-hang` | `4328701` | 30 | three-pass declaration with guarded-cycle check; `Json` and `HtmlNode` definitions; finite printing of recursive types; tests. Not brand placeholders |

The branches conflict with each other (two different things named `builtinNamedTypes`, `parseTypePrimary` changing type) and with `main`.
Port by reading them, not by cherry-picking.

## 5. Strategy

**Build the new checker beside the old one, then switch.**
The new checker reuses the type store, the type parser, error formatting, the substitution, and the builtin signature text.
Everything that decides what is well typed is new code written against the Typst design doc.
A hidden option selects it (for example `MSH_CHECKER=core`, or `--check-types=core`), and `tests/typecheck_test.sh` runs both checkers until the switch.

Why not change the current checker step by step: almost every row of the replacement table above is shared, mutable state in one `Checker` struct.
`unify` reads `readOnlyArgs`; freshness is threaded through `checkParseItem`; overloads, quote inference, `if`/`match` joins and child stacks all share `quoteBranch` and `branchSpawn`.
Taking those out one at a time means rewriting the same code several times while keeping 207 programs passing at every step.

The work happens on the feature branch `type-checker-enhancements`, with `main` merged in regularly (decided 2026-09-29).
The new checker still lives beside the old one on that branch, so the test suites keep passing while it is built.

**The one algorithmic risk: quote literals and overloaded words.**
Today, `(1 +)` gets several signatures and the consumer picks one. The design has one type per quote. The plan:

- A literal quote given directly to a word that takes a quote (`map`, `each`, `filter`, `iff`, `loop`, a def with a quote parameter) is checked against that word's parameter type, after the word's other arguments are known.
  In `@xs (1 +) map`, `map` knows the element type from `@xs`, so the body is checked with an `int` input.
  In the checker, pushing a literal quote leaves a "quote not yet checked" slot that records the quote and its scope. The consumer checks it; anything else that touches the slot (a store, `dup`, `x`) infers it on its own first.
- A quote that is stored or returned is inferred on its own. Inputs it reads below its own pushes become type variables.
  An overloaded word whose arguments are not yet known becomes a pending choice. It is resolved as soon as exactly one candidate fits, and it is an error if more than one still fits at the end of the def or script ("annotate this quote").
- Measure first. Stage "Baseline" counts how many programs in `tests/success` and `tests/msh-scripts` rely on quotes with several signatures, so the size of the change is known before the core is written.

## 5a. Performance

The checker is meant to run on all user code by default, on every REPL line and on every LSP edit, so speed is a requirement at every stage, not a later pass (Mitchell, 2026-09-30).
Prefer data layouts that are fast by construction and no more complex; optimize further only with a profile.

Baseline, the old checker on this branch at `0fe830d` (FX-8350, `go test -bench`, 20 runs):

| Benchmark | Time | Bytes | Allocations |
|---|---|---|---|
| `BenchmarkTypeCheckCorpus` (tests/success, typecheck_fail, msh-scripts) | 453 ms | 117 MB | 1,120,000 |
| `BenchmarkTypeCheckEmpty` (setup cost of one check) | 0.61 ms | 219 KB | 1,980 |
| `BenchmarkLSPDiagnostics/setdiff2way.msh` | 2.7 ms | 515 KB | 7,552 |

The core checker gets the same benchmarks (`BenchmarkCoreCheck*`) in step 1 of stage 3, and is compared with these numbers at every step.
Target: faster than the old checker on the corpus, with allocations that grow with the number of distinct types, not the number of words.

Choices that cost nothing in complexity, taken from the start:

- **Ids, not pointers.** Types, names, variables, quotes not yet checked and slot origins are `uint32` indexes into flat slices. A stack slot is 12 bytes (type, origin, flags), held by value in one `[]Slot`.
- **No per-check copy of the base.** The builtin table, std signatures and the types they mention are built once per process and frozen. A check adds a local layer on top: a `TypeId` below the base length is a base type, one above is local, and hash-consing looks in the local table and then the base. `Clone` of the arena's maps per check, most of the 0.6 ms empty-check cost today, goes away; the REPL and LSP start a check in constant time.
- **The builtin table is flat.** One slice indexed by `NameId` gives a span of candidate signatures; a signature's inputs and outputs are spans of one shared `[]TypeId` pool, not slices of their own. A signature with no generics is used without instantiating it.
- **Variables by dense index.** A scope is a slice indexed by `NameId` with a generation number per entry: entering a scope bumps the generation, so nothing is cleared or hashed.
- **Shallow resolution on the hot path.** Walking needs only the head of a type (follow variable bindings, which are path-compressed). Full `Apply` is for the end of a unit and for error messages.
- **Scratch buffers owned by the checker** for arm entry stacks, instantiation renames, assumption sets and work lists, reset by length, never reallocated per word.
- **Relations without a map per query.** An assumption set is a small slice searched linearly, moving to a map only past a size limit; `a == b` and two base types answer before any set is made. Measured against the oracle test so the answers do not change.
- **Errors are values until printed.** No string is built for an error, a hint or an origin unless it is reported.
- **Per-unit state is reset, not reallocated.** The substitution, unifier pairs and end-of-unit lists keep their capacity from one def body to the next.

Later, only with a profile that shows it: one pool for union members, enum arguments and record fields instead of a slice per type; checking def bodies in parallel.

The same applies to runtime work: `deepCopy`, the cycle-safe walkers and `validate` use explicit work stacks that are reused within a call, and allocate the copied objects once at their final size.

## 6. Stages

Each stage lists its work, its tests, and when it is done.

Status (2026-10-03, twelfth session): done: stages 0 through 7, and three independent reviews whose holes are fixed (progress log). Stage 9 (the REPL checks each line) is implemented and proved for the rule decided for question 20; overload choices stay open across lines (twelfth session); no questions are open. Left: stage 8's final pass at release.
Stages 2–5 depend on each other in order. Runtime groundwork is independent and can land any time. The soundness oracle can start after the core checker.

### Stage 0: Baseline and measurements

- Build both binaries on `main`. Run `tests/test.sh`, `tests/typecheck_test.sh` and `go test`. Record any failures before changing anything.
- Run `make check` in `formal-ver/` and `make -C formal-ver/oracle test` (Rocq 9.1 and OCaml from the opam switch in `formal-ver/README.md`).
- Build `typ` with Typst 0.14 or newer (it uses `chevron`).
- Measure, with temporary counters in the current checker (not committed), over `tests/success`, `tests/msh-scripts` and `lib/std.msh`:
  - quotes that get more than one signature, and where they are used;
  - overload fan-outs and union distributions;
  - dict↔shape conversions and covariant shape uses;
  - `x` on a quote whose arity is not known;
  - `break`/`continue` outside a literal quote at a loop site;
  - variables stored at two different types;
  - `as` uses that are not plain widening of a literal.
- Record the numbers in the progress log. If quotes with several signatures are common, revisit the quote plan before the core checker stage.

Done when: the baseline and measurements are in the progress log.

### Stage 1: Runtime groundwork

Each item is independent and fixes something on its own, so each is its own commit with its own tests.
Order (decided 2026-09-30): the `Maybe` equality fix and items 1 and 2 land before stage 3 (several stage 3 acceptance tests run `deepCopy`); the rest of item 3 before stage 4; items 4 and 5 any time before stages 5 and 7.

1. **`...rest` and pipe slices allocate** new storage, like `take` (design doc §new lists). Replace `tests/success/match_rest_zero_copy.msh` with tests that `setAt`, `del` and `append` on `rest` leave the source unchanged, and that `append` and `setAt` on a pipe slice leave the pipe unchanged. (Changes existing behavior: changelog entry.)
2. **`deepCopy` builtin** (design doc §deepCopy): deep, once per path, cycles are an error naming the cycle, quotes and immutable values shared. Tests: two paths to one list give two lists; a list containing itself is an error, not a hang; each runtime kind in the Typst design doc table.
3. **Iterative, cycle-safe walkers** for `str`, `toJson`, equality and ordering (required: recursive aliases make cyclic values well typed, `cyc_try_typed` in `formal-ver/Recursive.v`; today `str` of a list that contains itself overflows the Go stack), ported from the enum branch (`debe03e`, `0d78c14`, `3413d03`, `88d8de3`, `e7658a7`, `6341f99`, `2a326e3`; read them together, since early ones alone reintroduce a known hang). Port the `Maybe` equality fix (`622ce23`) with `tests/success/equality.msh`.
4. **JSON integral numbers parse as `int`** (design doc §JSON). Changes existing behavior: changelog entry, doc update.
5. **Runtime error classification.** Give every runtime failure a kind: *type mismatch* (a type error the checker should have prevented) or *checked error* (index out of range, `?` on none, a failed process, `exit`, division by zero, a cyclic `deepCopy`, validation limits). Convert call sites file area by area, starting with arithmetic, comparison, getters and list operations. Add an option or environment variable that makes the runtime report the kind, for the soundness oracle.

Done when: each item is merged with its tests, and the full test suite passes.

### Stage 2: Types and relations

New code with Go unit tests only; nothing is wired into the checker yet.

- **Type representation** in the shared type store:
  - one record kind for shapes and dicts: declared fields (required, optional) and a remainder (`exact`, `*: T`, `open`, or the deletable remainder that `{str: T}` has). Status per label as in design doc §per-label.
  - alias reference nodes for `type` names, so recursive aliases stay finite.
  - enum kind, nominal by declaration, with type parameters and, for each, its variance, whether it is fresh-covariant, and whether the enum is immutable (design doc §Subtyping, §Freshness; `wf_payload` in `formal-ver/Subtyping.v` is the check they must pass). `Maybe` is the first instance: the built-in `enum Maybe[a] = just a | none end` replaces the separate `Maybe` kind in the checker. Its runtime values keep their current printing and JSON.
  - abstract types, each tied to the pattern site that created it.
  - quote types whose output side may be `never`.
  - abstract grid schemas.
- **Relations**, each in its own function with its own tests:
  - equality unification with occurs check and the assumption set for recursive aliases (§Aliases);
  - subtyping `≤` (§Subtyping, per label) and the fresh retype relation `⊑` (§Freshness, including enum arguments by fresh-covariance, and never widening inside a quote), as the decision procedures of `formal-ver/Decide.v`, which are proved right whenever they say yes. Port `step`/`lvl`/`chk` and `rstep`/`rlvl`/`rchk` directly:
    - the assumption set is looked up and extended only at the children of a type constructor; union and unfolding steps never look at it (sound even for unguarded types; H13, `every_step_accepts`);
    - the set is threaded through one query and restored when a union alternative fails;
    - one set per relation: where `⊑` needs `≤` (a quote, an enum argument that is not fresh-covariant) it starts a new `≤` query with an empty set (H12, `mixed_alg_accepts`);
    - caching: after a top-level yes, every pair in the final set may be cached (`subq_set_sound`); after a no, nothing from that query (`cache_early`);
  - branch join (§Joins); quotes are never widened inside; where types cannot be widened inside, the other side if one is below the other; two different enums give their union; a recursive alias is never widened inside: the other side if one is below the other (`≤`, or `⊑` when both arms are fresh), a union if the kinds do not overlap, otherwise an error asking for a declared type (`ajoin` in `formal-ver/Join.v`);
  - runtime kind of a type (an alias contributes the kinds of its unfolding's members), `immutable`, and `checkable`; through aliases these are greatest fixed points (`immutable_tunfold`, `chk_tunfold`).
- Header comments in the reused files (`Type.go`) point to the deleted `ai/type_checker.md`; point them at the Typst design doc.
- **Unit tests** from the examples in `formal-ver/Decide.v`: `Json` against its reordered spelling, `PersonLit <= Person`, a fresh `[int]` to `Json`, H12 and H13 rejected, and the `cache_early` query answering no. `formal-ver/oracle/examples.txt` has them as queries with expected answers, printed from the Rocq terms.
- **Differential tests against the extracted oracle** (`formal-ver/oracle/`, see its README). A Go test generates random types over the model's type language (`int`, `str`, `bool`, `bot`, `unknown`, `Maybe`, lists, shapes and dicts with every field status and remainder, unions, quotes including `never`, generic enums with each variance and fresh-covariance, type variables, and guarded recursive aliases written as `mu` types), writes each as a query, and compares the Go `≤`, `⊑` and join with the oracle's answers. Any Go yes that the oracle answers no (with ample fuel) is a possible soundness bug; any Go no that the oracle answers yes is a port bug. The test skips when the oracle binary is not built, so `go test` does not need Rocq.
- **Property tests** that mirror the proof: transitivity of `≤` (`sub_trans`) and of `⊑` on randomly generated types, including guarded recursive aliases; `Json` equal to the same union with its members reordered (`json_teq`); `≤` implies `⊑`; every S1–S4 row of the design doc's table, including the two dict/remainder cases in S2 and S4; the "optional must not become deletable" case; for random well-formed generic enums, `E[a] ≤ E[b]` implies each payload `subst a t ≤ subst b t`, and `E[a] ⊑ E[b]` implies `subst a t ⊑ subst b t` (`payload_sub`, `payload_rsub` in `Variance.v`).

Done when: the relations pass their tests, including randomized transitivity on at least tens of thousands of generated pairs, and the Go relations and join agree with the oracle on at least tens of thousands of generated queries.

### Stage 3: The core checker, for the language as it is today

Selected by `MSH_CHECKER=core` (read where `Main.go` calls `TypeCheckProgram`, and in the LSP). The old checker stays the default.
`tests/typecheck_core_test.sh` runs the core checker over `tests/success` and `tests/typecheck_fail`, and compares with two lists:
`tests/core_expected_rejections.txt` (success programs rejected on purpose, each with its rewrite) and `tests/core_no_longer_errors.txt` (typecheck_fail programs no longer rejected, each with the reason).
Until the switch-over, `tests/typecheck_test.sh` (the old checker) skips the acceptance programs listed in `tests/old_checker_accepts.txt` (its holes) and `tests/old_checker_rejects.txt`.
Both checkers join `tests/typecheck_test.sh` at the switch-over.

Structure (agreed 2026-09-30). The old walker is not reused: its type resolver builds `TKShape`/`TKDict`, which the stage 2 relations do not take, and it infers each quote where it is written, where the design checks a literal quote against its consumer.

| File | Contents |
|---|---|
| `TypeCoreResolve.go` | type expressions to `TKRecord`, `TKAlias`, `TKEnum`, `*: T`, `never`, `new` marks, rigid generics; not a `Checker` method; the built-in `Json` alias |
| `TypeCoreStack.go` | slots (type, fresh mark, origin), the diverges flag, break/continue contexts (`·`, `σ`, `⋆`), return context (`·`, `σ`, `RAny`) |
| `TypeCore.go` | the walker. One unit per def body and one for the script; a unit owns its unifier, its variable scope, and four end-of-unit lists: deferred checks, escape records, pending overload choices, pending quotes |
| `TypeCoreQuote.go` | quotes not yet checked, and the words that consume them |
| `TypeCoreMatch.go` | patterns, bindings, `=>`, `:>`, the escape check |
| `TypeCoreAssign.go` | definite assignment, a separate pass |
| `TypeCoreBuiltins.go` | the new builtin table: signature text, a fresh/shared mark per output, and a flag for in-place type changes that need a fresh operand |
| `TypeCoreSpecial.go` | commands and redirects, captures, command execution, grid join/pivot, format strings, `dbg`, bare words in list literals |

- Walking: a concrete type stack. A quote inferred on its own makes input variables when it runs out of stack (as the old `inferInputs`), which is the same as composing effects.
- End of a unit, with the final substitution: `Recheck` every unified pair, the deferred subtyping checks, the escape checks, every store again; a pending overload choice left is an "ambiguous, annotate" error.
- Pending overload choices: the word pushes new variables for its outputs; the choice is retried after each item and resolved when exactly one candidate fits. The substitution only grows, so a candidate that stops fitting never fits again, and the order of retries does not matter.
- Quotes not yet checked: the slot keeps the parse items and the context. A consumer unifies its other arguments, then checks the body against its parameter, with the break context of a child-stack or current-stack builtin. Shuffles move the slot; anything else that touches it infers it first. A literal followed by redirect words is still a literal at a `loop` site.
- Loops: the body's stack is the entry stack with `⊥` replaced by new variables ("a `⊥` fixes nothing"); if the body fails only because an entry slot was fresh and the end slot is shared, the body is checked once more with that slot shared.
- `new` on a recursive def written without it: the body is checked a second time assuming its own output is `new`; if that is consistent, the error is "mark it `new`".
- A quote checked a little after the code that follows it can make a store other than the first in program order fix a variable's type. That changes only which store reports the error; every store is checked again with the final substitution.
- `Maybe` becomes the built-in enum declaration in the core checker at the start of this stage (decided 2026-09-30). The old checker keeps `TKMaybe`; it never calls the relations.

Reused from the old checker: `Type.go`, `TypeError.go` (new kinds added), the substitution, the type-expression AST and `parseDefSignature`.
Ported (logic, onto the new slots): `tryRedirect`, `tryCapture`, `applyMergeRedirect`, `tryExecCommand`, `commandParts` (with the fresh-only rule), `checkFormatString`, the grid-literal schema, `tryGridJoin`, `tryPivot` (abstract schema), `tryRejectPathWrite`, the bare-word rule, pattern snippets in messages.
Rewritten: the `TypeBuiltins.go` entries, as the starting text for the audit. The audit is split by category across parallel subagents, each checking `Evaluator.go`, and reviewed before it lands (decided 2026-09-30).

Order of work, each step its own commit with every suite passing:
1. resolver, slots, literals, shuffles, variables, defs, `if` and joins, the end-of-unit checks, the option and the core test script, a hand-picked set of builtins;
2. quotes not yet checked and their consumers, `x`, `loop`/`each`, break/continue/return contexts, `never`;
3. pending overload choices, then the whole builtin table, one category at a time;
4. `match`;
5. shapes: getters, `getd`/`setd`/`del`, `as`, freshness of builtin outputs;
6. commands, grids, format strings;
7. `new` marks, the LSP, code actions;
8. the acceptance tests of section 7, and the corpus.

Work:

- Stack slots carry a type and a fresh mark. Effects carry inputs, outputs and a diverges flag. Composition as in design doc §Inference.
- Literals, including the freshness rule for list and dict literals (§ShapeLit), and partly new literals: a literal around stored values is new at the top, each stored value keeping its type (design doc §Freshness per object). Done 2026-10-01.
- Variables: one scope per def invocation and one for the script; one type per variable per scope; stores check against it; definite assignment as a separate check (§Variable scopes). The variable's type is the type of its first store in program order; a later store at a type that does not fit is an error at that store, with the hint "use a new name, or widen the first store with `as`". No renaming (decided 2026-09-29; design doc, "Renaming must not change behavior"). A `⊥` in a store's type (the contents of `none`) is replaced by a new type variable before unifying, so it fixes nothing; every store, the first included, is checked again with the final substitution, and a variable left unsolved is `⊥` (decided 2026-09-30; design doc, "A ⊥ in a store fixes nothing").
- Quotes as in section 5; `x` needs a known arity.
- **Unification is not trusted** (design doc §Inference). Record every pair the checker unifies. Once a def body or the script is solved, check each recorded pair again with the final substitution applied (the two sides must be equal), and make the deferred subtyping checks and the escape check with that substitution too. A failure is an internal checker error that names the site, never an accepted program. Overload choices need nothing extra: a choice is only the constraints of the chosen candidate. This is what lets the proofs cover the checker without proving unification or overload resolution.
- **The escape check** for kind patterns on unknown contents, exactly as proved (`kind_list_once`, `kind_enum_once` in `formal-ver/Escape.v`): check the arm once with a new rigid variable per unknown type (one per enum parameter), then, with the final substitution, reject the arm if the variable appears in any variable's type, the stack below the matched value, the arm's output stack, or the break, continue or return stacks.
- Defs: annotated signatures, rigid type variables in the body, recursion, `never` outputs (§Divergence), no `return` in a `never` def.
  A rigid type variable is not immutable, is treated as unknown contents by kind patterns, and cannot be a `tryAs` target (design doc §Def remarks; `Generic.v`).
- Top-level code may `return` with any stack; it ends the script (`RAny`).
- `new` on def outputs (design doc, "New def outputs"): parse it in signatures, check it both ways against the body's freshness (written but shared: error; missing but new: error; on an immutable type: error), take the largest consistent marks for recursive defs, and give each error a fix. Def inputs stay shared.
- Divergence: words after a diverging word are not checked (`t_div`); the diverges flag follows the `div_*` lemmas in `Frame.v`.
- `if`, `iff` with literal quotes, `loop`, `each`/`map`/... with literal quotes, `break`/`continue` contexts, `return`, `exit` (§Quotes that break, §Divergence).
- Branch joins per §Joins, including fresh-only widening. `join_slot` in `formal-ver/Join.v` is the reference: the result is fresh only when both arms are, joins inside `Maybe` keep the arms' freshness, where types cannot be widened inside the join is the other side if one is below the other, aliases are never widened inside (`ajoin`).
- When a join makes a union and a later use does not accept it, the error names the branch each member came from ("`shape` is `Shape | LoadError`: the `else` branch at line 12 leaves a `LoadError`"). Automatic unions move the error away from the mistake; this points back at it (decided 2026-09-30).
- Shapes: literal-key `get`/`set`, runtime-key `getd`/`setd` per §Runtime keys, `del` only on `{str: T}`, shape/dict subtyping by the per-label rule (a shape never becomes a `{str: T}`).
- `as`: subtyping, or `⊑` on a fresh slot.
- `match`: kind patterns on unions (the member of that kind, writable), kind patterns on unknown values (abstract types, and the escape check in §Unknown contents), `Maybe`, literal, list and dict patterns, `=>`.
  Every binding is a variable of the enclosing scope, under the one-type-per-scope rule (design doc H11): the same name in two arms at different types is an error with the hint "use a new name", and a binding whose type would mention an abstract type is an error with the hint "use `:>`".
- Unions: reject a union with two members of the same kind.
- `parseJson` returns `Json`, fresh; no operations on raw `Json`.
- **The builtin table.** Port every entry. For each one:
  - check it against `Evaluator.go`, so the signature accepts exactly what the runtime accepts;
  - mark its output *fresh* or *shared*. Every builtin that returns a new list (`map`, `filter`, `take`, `skip`, slices, `reverse`, `sort`, ...) is fresh exactly when its element type is immutable (design doc, "One rule for new lists");
  - mark in-place type changes (redirects, `updateCol` and the grid mutators) as allowed only on a fresh operand;
  - replace "accepts `[int | float]`" entries with generic or per-type overloads, since `[int]` is not below `[int | float]`;
  - give every record type a builtin takes or gives a built-in alias (`HttpRequest`, `PackEntry`, ...; `builtinAliases` in `TypeCoreBuiltins.go`), and give a builtin that only reads a list one form per list type programs store (`[str]`, `[path]`, `[str | path]`, `[PackEntry]` for `zipPack`) (decided 2026-10-01);
  - type builtins that only read a dict over every dict-kinded type, never `{str: a}`: `keys`, `len`, `in` take `{| open}`; `values`, `filter` and runtime-key `get` read at the type of *Get-Key* (design doc §Runtime keys). Only `setd` and `del` need `{str: T}`;
  - check that `extend` with a grid view cannot change the view's source grid at a new type.
- Port the special cases listed in section 4: commands and redirects, captures, command execution, grid join/pivot/groupBy, format strings, path writes, `dbg`, bare words in list literals.
- The LSP uses the core checker when the option is set.
- LSP code actions for the three `new` errors (add `new`, remove `new`) and a fix-all.

Tests: the counterexamples and rules in section 7, as `tests/typecheck_fail` and `tests/success` files, run under the core checker.

Done when:
- every counterexample in section 7 is rejected by the core checker, and every accepted variant is accepted;
- every `tests/success` program passes the core checker, except a written list of programs the new rules reject on purpose, each with its rewrite;
- every `tests/typecheck_fail` program is rejected by the core checker, or is listed as no longer an error, with the reason.

### Stage 4: Aliases and enums

Surface syntax and value behavior are in design doc §Surface language.

- Declarations are read in three passes: reserve every name, resolve bodies, then reject unguarded cycles (from `fix/recursive-named-type-narrow-hang`, rewritten for alias reference nodes, and covering enums as well). Every cycle of alias references must pass a type constructor, and an enum instance counts (`type T = Box[T]` is accepted). This is a soundness condition of the assumption rule, not only termination (H13, `unguarded_*` in `formal-ver/Recursive.v`).
- `type` is a transparent alias. `Json` and `HtmlNode` are built in; users cannot redeclare them.
- Enums: parser (final syntax from the enum branch, plus `[a b]` parameters after the name), runtime value, constructors as words (polymorphic for generic enums), constructor patterns, exhaustiveness, `str`/`toJson`/equality/ordering per design doc §Surface language. Enum names are their own runtime kind in unions.
- Generic enums: compute each parameter's variance and fresh-covariance from its payload positions, and each enum's immutability as a greatest fixed point over the declarations (design doc §Subtyping, §Freshness); a constructor that does not mention a parameter gives `⊥` for a covariant one and a fresh type variable otherwise; reject a recursive reference with different parameters (a usability rule, not a soundness one).
- An enum kind pattern on a value of unknown type binds the enum at fresh abstract arguments, with the escape check of §Unknown contents.
- Name rules: collisions and duplicate `def`s are errors (enum branch `2e2f4b6`, `8abcaee`, `8087f6c`).
- Startup files and the LSP see the same declarations.
- Migrate the test files that declare `type` today (one in `tests/success`, four in `tests/typecheck_fail`); `TKBrand` goes away with them.

Tests: the enum branch's tests listed in its survey (enum, recursive values, deep and DAG values, name collisions, exhaustiveness), and the recursive-alias tests from the recursive-type branch.

Done when: those tests pass under the core checker, and recursive enum values print, compare and serialize without hanging.

### Stage 5: Validation: `is`, `tryAs`, `deepCopy` in the checker

Done 2026-10-01 (fourth session); see the progress log.

- One runtime validator: `validate(value, type)` walks the value against a resolved type with an explicit work list. It tracks the (object, type) pairs on the current path and counts work against a budget. Exhausting the budget is an error; a pair met again on the path is assumed to hold, so a cycle validates (design doc §Validation, `cvalidate` in `formal-ver/Cycles.v`). The checker trusts the validator's `just` only for fresh operands and immutable targets (`soundness_v`), so memoizing over a DAG is fine. Shapes check declared fields and remainders, including `*: T`. Enums check identity and payloads. Grids check schemas.
- `is T x` patterns and `tryAs` (its own word, not elaborated to a match: a hidden variable would make the result shared) with the typing rule in design doc §Validation: in place always; a shared operand is accepted only if its type is already below the target or the target is immutable; otherwise a type error that suggests `deepCopy`.
- `checkable` targets: quotes (including a quote in the payload of any enum the target mentions), type variables and abstract types are rejected with a clear message. Compute enum checkability per declaration, like immutability (`chk` in `formal-ver/Checkable.v`).
- Exhaustiveness: an `is T` arm covers a union member only when the member is equivalent to `T` and `T` is checkable.
- `deepCopy` typed as `(τ -- τ•)`.

Tests: R6 as written is rejected; R6 with `deepCopy` passes and prints the right value; `parseJson tryAs T ?` validates in place (an identity test); a list that contains itself validates against `[Json]`, and `j = [j]` against `[[int]]` gives `none`; the budget gives an error, not `none`; the try-as branch's validation tests.

Done when: those pass under the core checker.

### Stage 6: Switch over, then delete the old checker

Done 2026-10-01 (fifth session). See the progress log.

- Make the core checker the default for `--check-types`, `--type-check-only` and the LSP.
- Update the programs on stage 3's list of intended rejections, and the tests listed as changing.
- `lib/std.msh`: make every read-only list function generic (`[a]`), and give read-only dict functions the same types as the read-only dict builtins (stage 3); type the HTML helpers with `HtmlNode`. Stop and ask about any signature that looks wrong.
- Run `tests/msh-scripts` through the checker and record what fails; these are real scripts.
- Delete the old checker's code paths: `TKBrand`, `TKStrLit`, `TKOverloadedQuote`, `readOnlyArgs`, `mutatingBuiltins`, `inputUnifyOrder`, union distribution, overload fan-out, and whatever else section 4 lists as replaced.

Done when: all three test commands pass with only the core checker in the binary.

### Stage 7: Soundness oracle and builtin contract tests

Can start once stage 3 works and runtime error classification exists.

- Run every checked program in `tests/success` and `tests/fail`; fail on any runtime *type mismatch*. `tests/msh-scripts` are type checked only, never run (decided 2026-10-02, question 13): they are Mitchell's own scripts and move files, touch repositories and deploy. Running one that is an interesting example needs his approval first, script by script.
- A generator of random well-typed programs, built by running the typing rules backwards and biased toward aliasing: `dup`, stores, refinements of stored values, writes through every view. Run them and fail on any type mismatch.
- For each builtin: generate inputs of its declared types; check the output types, that shared inputs keep their types, and that outputs marked fresh are unaliased.

Done when: the oracle runs in CI or a script, and finds nothing on the corpus and on a fixed number of generated programs.

### Stage 8: Documentation, editors, changelog

Throughout, with a final pass at the switch-over.

- `doc/type_system.inc.html`: rewrite for aliases, enums, shapes and remainders, invariance, unions of distinct kinds, freshness in user terms ("a new value"), `deepCopy`, `tryAs`/`is`, `never`, joins.
- `doc/mshell.md`: the `## Type System` section, for agents; no interactive-mode material.
- `doc/data-types.inc.html` and `doc/execution.inc.html` where they mention the checker or redirects.
- Rebuild the docs (`cd doc && msh build.msh`).
- New syntax in editor grammars: `enum`, `is`, `never`, `tryAs`, `deepCopy`, in `sublime/msh.sublime-syntax`, the Notepad++ themes, and `code/syntaxes/mshell.textmate.json` (which has no keyword rule today).
- `BuiltInList.go` entries for new builtins.
- `CHANGELOG.md` under Unreleased: user-facing changes only, grouped.

### Stage 9: The REPL checks each line (started 2026-10-02, eleventh session)

The REPL checks each line live, keeping the checker's state across lines, and runs a line only if it checks (decided 2026-09-29; live checking 2026-09-30; design doc §Checking by default). Open questions 20-23.

Why a session is sound without a new proof, as long as no line fails at run time: types are erased and `eval` is deterministic, so running lines 1..k+1 one after another is running their concatenation. The checker checks line k+1 as the continuation of lines 1..k, which is checking the concatenation, so `soundness` covers the session. Each prefix only needs *some* typing: the substitution may grow from one line to the next, as long as every earlier line's checks still hold under it.

Work:

1. **Rocq: the state after a checked error.** `RErr` carries the heap; `res_ok` for it says the store typing and the caller's frame still hold (the same `INV` as a normal result, with whatever stack the error left). Then the REPL theorem for question 20 (a): the slots below the line's static input depth are its frame and keep their types; the shared slots it took keep theirs (they can be put in the frame as well, since a shared slot owns no region); the new ones it took are dropped.
2. **A session checker** (`TypeCoreSession.go`): the script unit stays open across lines. Each line is checked from the stack and variables the previous lines left; at its end the unit is solved *without* committing the defaults (unsolved variables are ⊥ for the checks and stay open afterwards), and every check that still mentions an unsolved variable is kept, to be made again after each later line; checks that are ground and pass are dropped, so a line costs the same at the start and the end of a long session. A refused line rolls the checker back to where it was before it (substitution trail, variables, stack, definitions, declarations). The line's static input depth is recorded for the revert.
3. **The REPL**: check each line before running it; print the errors and do not run a refused line; on a runtime error, restore the stack (question 20) and tell the checker. `return` at the top level of a line ends the session, as it ends a script (question 23). Startup defs are checked; a call to a broken one is refused (question 21). No opt-out (question 22).
4. Benchmarks: per-line cost at the start of a session and after thousands of lines; allocations per line.
5. Docs (`doc/` interactive mode pages, not `mshell.md`), changelog.

### Later, not in this plan

- A read-only list view type (§new lists), if an `O(1)` tail is ever needed.
- The "top-fresh" slot mark (§deepCopy).
- When default parameters land and empty option dicts go away, type `{}` as `{str: T}` with a new variable `T`, as `[]` is `[T]` (decided 2026-10-01; design doc §Joins, "Why `none` needs nothing").

## 7. Acceptance tests

Each line becomes a test file; the name in brackets is a suggestion.
`fail` means `tests/typecheck_fail/`; `ok` means `tests/success/` with expected output.

### Counterexamples from the Typst design doc

| Program | Expect |
|---|---|
| P1: writing a `str` through `{a: int \| str}` into an `{a: int}` | fail [`p01_shape_field_invariant`] |
| P2: `{a: 1, b: "x"}` through `{a: int}` into `{str: int}` | fail [`p02_shape_to_dict`] |
| P3: `{}` as `{str: int}` passed as `{a: int}` | fail [`p03_dict_to_shape`] |
| P4: shape passed as `{str: int \| str}`, then `set` | fail [`p04_shape_to_dict_set`] |
| P6: type-changing `updateCol` on a stored grid | fail; with `deepCopy` first: ok [`p06_updatecol_shared`] |
| P7: `@c *` on a stored command | fail; `@c deepCopy *`: ok [`p07_redirect_shared`] |
| P10: `parseJson` result used as `int` | fail [`p10_parsejson_json`] |
| P13: `parseJson as {a: int}` | fail [`p13_as_needs_evidence`] |
| R6 as written | fail; with `deepCopy` on each refinement: ok, prints `2` [`r6_refinements`] |
| H1: runtime-key `getd` on `{a: int, *: str}` used as `str` | fail [`h1_getd_declared_field`] |
| H2: list from a kind pattern stored in an outer variable | fail [`h2_abstract_escape`] |
| H3: `{a: @xs} as {a: [int \| str]}` | fail [`h3_literal_around_shared`] |
| `{a: @xs} as {a: [int], b?: int}` with `xs : [int]`, then a write through it reaches `xs` | ok [`partly_new_dict`] |

### Rules

| Case | Expect |
|---|---|
| `[1 2] l! @l as [int \| str]` | fail |
| `[1 2] as [int \| str]` and `@l deepCopy as [int \| str]` | ok |
| S1–S4: one test per row of the design doc's table, each direction | fail / ok as the table says |
| `{url: str, *: int}` passed as `{url: str, timeout?: int}`; `{str: int}` passed as `{timeout?: int, *: int}`; `{str: int}` passed as `{timeout: int}`; `{a: int}` passed as `{str: int}` | ok; ok; fail; fail |
| `del` on a shape key; `del` on `{str: T}` | fail; ok |
| `[int] \| [str]`, `{a: int} \| {b: str}` in a signature | fail |
| `int \| [str]`, then `match list xs :` and `setAt` on `xs` | ok |
| joins: every row of the design doc's join table | as the table says |
| `never`: `def die (str -- never)` in an `else` arm; a `never` def that can return; `(-- int never)`; `[never]`; `def spin ( -- never) spin end`; a `(str -- never)` quote where `(str -- int)` is expected; `(bool -- never \| str)` | ok; fail; fail; fail; ok; ok; fail with a hint |
| `break` in a stored quote later run in a loop; `break` in a literal `each` quote inside `loop` | fail; ok |
| `x` on a quote of unknown arity | fail with "annotate this quote" |
| `@xs 1 take as [[int \| str]]` with `xs : [[int]]`; `lines 1 skip as [str \| int]` | fail; ok |
| `tryAs` on a stored `{age: int}` to `{str: int \| str}`; with `deepCopy` first | fail; ok |
| `parseJson tryAs T ?` returns the same object | ok (identity) |
| `deepCopy` of `{a: @xs, b: @xs}`; of a list containing itself | two separate lists; checked error |
| `...rest` then `setAt`/`del`; pipe slice then `append` | source unchanged |
| a variable stored at `int` then at `str`, captured by a quote | fail |
| enum: construct, match all members, missing member, unknown member, collision with a def, recursive enum value printed | ok, ok, fail, fail, fail, ok |
| recursive alias `Json`/`Person`; `type A = A`; `type A = int \| A` | ok; fail; fail |
| `type A = B` with `type B = int \| A` | fail [`h13_unguarded_alias`] |
| `type T = Box[T]` with `enum Box[a] = box [a] \| empty end`; `[] empty append box as T` | ok |
| `Json` passed where the same union with its members reordered is expected; `[Json]` where the reordered `[...]` is expected | ok; ok |
| `Json \| [int]` in a signature | fail (two list members) |
| `parseJson tryAs [Person] ?` then a nested friend's `age` | ok |
| H12: `type A = {x: [int], f: (-- A)}`, `type B = {x: [int \| str], f: (-- B)}`, `r : A` stored; `{x: [2], f: (@r)} as A as B` | fail [`h12_one_assumption_set`] |
| `[] as [Json] j!  @j @j append drop  @j tryAs [Json] ?`; the same with an `is [Json] xs` arm; `@j deepCopy` | `just`; the arm runs; checked error |
| `enum Box[a] = box [a] \| empty end`: `[1] box` as `Box[int]`; `empty` then used as `Box[str]`; a `Box[int]` passed where `Box[int \| str]` is expected | ok; ok; fail (invariant) |
| H4: `@xs box` with `xs : [int]` stored, then `as Box[int \| str]`; `[1] box as Box[int \| str]` (fresh) | fail; ok [`h4_enum_not_immutable`] |
| H5: `enum F[a] = f (a -- a) end`; `(1 +) f as F[int \| str]` | fail [`h5_fresh_retype_quote`] |
| H6: `true if (1 +) else ("a" ++) end "x" swap x` | fail [`h6_quote_join`] |
| `enum Box[a] ...`; `@v match box xs :` with `v` of unknown type, `xs` stored in an outer variable | fail (escape) |
| `enum Pair[a] = pair a a end`: a `Pair[int]` passed where `Pair[int \| str]` is expected | ok (covariant) |
| `enum List[a] = cons a List[a] \| nil end`; one whose payload refers to `List[[a]]` | ok; fail |
| `Box[int] \| Box[str]` in a signature | fail (one runtime kind) |
| `5 just str wl`; `5 just toJson wl` | prints `Just(5)`; `5` (unchanged) |
| `is T x` exhaustiveness: `x : int \| [Json]` matched by `is int n` and `is [Person] ps` with no `_` | fail |
| H7: `def g (a -- Maybe[[str]]) tryAs [str] end` | fail [`h7_tvar_not_immutable`] |
| H8: `def h (a -- int) match str x : @x 1 + , _ : drop 0 end end` | fail [`h8_tvar_kind_pattern`] |
| H10: `[[1]] (dup e! drop true) filter as [[int \| str]]`; the same with `[1 2]` and `[int \| str]` | fail; ok [`h10_filter_fresh`] |
| H11: `q! [1 "s"] (match int n : (@n) q! , str n : @q x 1 + drop end) each`; the same with `str s` | fail; ok [`h11_arm_bindings`] |
| H11: `list xs` on a value of unknown type; `list :>` with the arm reordering the list | fail with a hint to use `:>`; ok |
| `enum F = f (int -- int) end`: `tryAs F`; an `is F` arm counted as covering `F` | fail (not checkable) |
| `tryAs a` in a def with type variable `a` | fail |
| `def f ( -- never) 1 exit 1 + end` | ok (dead code after `exit` is not checked) |
| `true if @xs just else @ys just end` with `xs : [int]`, `ys : [str]` stored; the same with `[1] just` and `["a"] just` | fail; ok |
| `return` in top-level code (`tests/success/return_top_level.msh`) | ok |
| `def f ( -- new Json) "c.json" readFile parseJson end  f tryAs Config ?` | ok (validated in place) |
| `def f ( -- Json) "c.json" readFile parseJson end` | fail: output is new, mark it `new` |
| `def f ( -- new Json) "c.json" readFile parseJson j! @j end` | fail: stored in `j`, not new |
| `def f ( -- new int) 1 end` | fail: `new` on an immutable type |
| `def f (int -- new [int]) dup 0 = if drop [] else 1 - f end end`; the same without `new` | ok; fail (largest consistent mark) |
| P14: `1 x!  [0 0] (drop @x 1 + drop "a" x!) each` | fail [`p14_rename_loop`] |
| `false if 1 x! else "a" x! end @x 1 +` | fail |
| `"a\nb" text!  @text lines text!` | fail at the second store, hint names `as` and a new name |
| `1 as int \| str x!  "a" x!`; `[1] as [int \| str] xs!  ["a"] xs!`; `@ys xs!` with `ys : [str]` stored | ok; ok; fail |
| `none r!  5 just r!  none r!  @r` used as `Maybe[int]`; `[none] l!  @l 5 just append`; `none r!` never stored again, then `@r` | ok; ok; ok (`Maybe[⊥]`) |

## 8. Build and test commands

```
cd mshell && ./build.sh                                  # builds mshell and copies it to msh; the test scripts use both names
cd tests && ./test.sh && ./typecheck_test.sh
cd mshell && go test                                     # includes TestBuiltinContracts and TestGeneratedProgramsSound (60 programs)
cd tests && ./soundness_test.sh                          # runs every checked program in success/ and fail/, fails on a type mismatch
cd formal-ver && make check                              # needs Rocq 9.1
cd formal-ver/oracle && make test                        # the extracted oracle; needs OCaml from the same opam switch
typst compile ai/type-core-calculus.typ                  # needs Typst 0.14+
```

If the Go build cache is not writable, set `GOCACHE` to a directory inside the repository.
