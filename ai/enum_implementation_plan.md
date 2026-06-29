# Enum (generative tagged sum type) — implementation plan

Companion to `design/literal_or_enum_typing.html` (the design + rationale). This is the
file-by-file build plan. Plans live here in `ai/`; the design lives in `design/`.

**Status: implemented on branch `enum-types`** — nullary + payload-carrying members,
construction, nominal distinctness, and `match` (member dispatch, payload binding,
exhaustiveness) all ship in one PR. Payloads use a parenthesized list (`member(T..)`)
rather than the space-separated form originally sketched, because mshell has no statement
terminator and space-separated payloads are ambiguous against following code.
Out of scope, as agreed: derived `decode`/`encode`/`values`, backing strings, qualified
`Enum.member` names, generics, and `Result` (Maybe suffices). JSON stays a structural union.

## Scope & non-goals

In scope (a generative tagged sum type declared with `enum`, inline `= a | b | c`):

- `enum Name = c1 | c2 | ...` and `enum Name = c1 t.. | c2 t.. | ...`.
- Constructors are case-free; produced only by a constructor word or `decode` (**Position 1** —
  no implicit coercion, `str as Enum` rejected).
- `match` over members with exhaustiveness; payload binding reuses the `just v` path.

Explicit non-goals (per the design + owner direction):

- **No `decode` / `encode` / `values` derived functions** in v1. Reading config back is handled at
  the use site with `match` (or whatever fits). This removes the wire/serialization surface.
- **No backing strings** (`member = "wire"`) in v1 — they only existed to feed `decode`/`encode`.
  The member's own name is its identity. (Easy to add later when serialization is wanted.)
- **No qualified `Enum.member` / `Enum.method` dispatch** in v1. Members are referenced by bare name,
  resolved by context; member names are unique across enums (collision is a declaration error). This
  removes the `.`-lexing / qualified-dispatch unknown entirely.
- **No `Result` type.** `Maybe` already covers the common case.
- **No change to JSON typing.** `JsonScalar` / `Json` stay *structural unions* — their variants are
  distinguishable by structural type, so they do not need tags. Enums are only for cases structure
  cannot discriminate (e.g. two variants with the same payload type) and for closed config sets.
- **No generic enums** (`Enum[t, e]`) in v1.
- **No `?`-propagation sugar** in v1.
- A *checked* `"GET" as Method` stays deferred (it needs literal/singleton types).

## Phasing

Two PRs. Phase 1 (nullary enums) is now small — declaration, construction, match — with **no
serialization surface and no qualified names**. Phase 2 adds payload variants and the tagged runtime
value where `Evaluator.go` gets touched substantially.

---

## Phase 1 — Nullary enums

`enum Mode = read | write | readwrite`, match-by-member + exhaustiveness, Position 1. The full v1
surface is: declare, construct (bare member word), and `match`. No `decode`/`encode`/`values`, no
backing strings, no qualified names.

The runtime value just needs to carry **which member it is** (enum `NameId` + member `NameId`). A
lightweight value suffices; no new heavy `MShellObject` is required for Phase 1.

### Type system

1. **`Lexer.go`** — add an `ENUM` keyword token (via `literalOrKeywordType`). Audit existing user
   identifiers/usages of `enum`.
2. **`TypeParseIntegration.go`** — add `MShellEnumDecl` (parallel to `MShellTypeDecl`) and
   `ParseEnumDecl`: `enum` Name `=` member (`|` member)*, where a member is a bare `LITERAL`. No
   backing clause.
3. **`Parser.go`** — add `case ENUM:` beside `case TYPE:` (≈ line 677) dispatching to `ParseEnumDecl`.
4. **`Type.go`** — add `TKEnum` kind + a variants side table (`[]EnumVariant{Name NameId; Payload
   []TypeId}`; Payload empty in Phase 1), `MakeEnum`, hashconsing key, and accessors. Nominal
   identity = the declaration `NameId` (two `enum`s with identical members stay distinct, like brands).
5. **`Type.go` / `TypeUnify.go`** — extend `walkTypeVars` and `typeRewriter.mapType` with a `TKEnum`
   arm (recurse payload types; none in Phase 1). `unify` (`TypeChecker.go`): `TKEnum` unifies only
   with the same enum (by name).
6. **Constructors as words** — register each member as a nullary sig `( -- Mode)`. Members live in a
   **global constructor namespace**; a member name duplicated across two enums is a declaration error
   in v1 (no qualification to disambiguate yet). A bare member word resolves to its enum; where an
   expected enum type is in context (match subject, sig slot) that pins it.
7. **Pre-pass registration** — mirror `DeclareType` registration (`TypeCheckProgram.go:99-101`):
   collect `enum` headers with placeholder TypeIds, resolve bodies, register constructor words,
   detect cross-enum member-name collisions.
8. **Match** — `analyzeTokenPattern` (`TypeCheckProgram.go:1381`): an enum member name is a
   recognized pattern that **credits coverage** against the enum's closed set (flip the
   "value literals credit no coverage" behavior at `:1402` for enum subjects). `TypeBranch.go`:
   exhaustiveness over the member set; narrowing (subject known to be that member in the arm).

### Runtime (`Evaluator.go`)

9. A lightweight enum value (enum + member ids). Constructor evaluation pushes it; member-pattern
   matching extends the `matchTokenPattern` path that already handles `none`/type keywords near
   `:1117`; plus equality, `DebugString`, `ToJson`.

### Docs / housekeeping

10. `doc/type_system.inc.html` + `doc/mshell.md` (rebuild with `cd doc; msh build.msh`).
11. `CHANGELOG.md` → Unreleased / Added.
12. `lib/std.msh` completions, in the documented Vim-fold pattern.
13. Tests: `tests/` (+ `typecheck_test.sh`) and `mshell/ go test`. Cover: decl parse, construct,
    match exhaustive (no `_`), non-exhaustive rejected, member narrowing, `str as Enum` rejected,
    two enums with same members stay distinct, duplicate member name across enums rejected.

---

## Phase 2 — Payload-carrying variants

`enum CmdResult = ok str | failed int str | timeout`. Adds:

1. **Parser** — arms parse a constructor name followed by payload type exprs (reuse
   `parseTypeExpr` productions for each payload).
2. **`Type.go`** — `EnumVariant.Payload` populated; payload types flow through hashconsing and the
   rewriter arms added in Phase 1.
3. **Constructors with payloads** — `failed : (int str -- CmdResult)`, postfix, consume from the
   stack like `5 just`.
4. **Runtime value** — a new `MShellObject` generalizing `Maybe`: `{ enum NameId; tag; payload
   []MShellObject }`. `Maybe` is the proven two-variant precedent; follow its equality/`DebugString`/
   `ToJson` shape. Phase-1 nullary values fold in as the empty-payload case.
5. **Match payload binding** — extend the `just v`-style binding (`TypeCheckProgram.go:1348`,
   `Evaluator.go:1055`) to N payloads: `failed c e : ...` binds `c`, `e`.
6. **Recursive enums** — already work via the placeholder-TypeId pre-pass.
7. Docs / changelog / completions / tests as above (payload construct + destructure + recursive
   enum + exhaustiveness with payloads).

(Serialization helpers — `decode`/`encode`/backing strings — remain out of scope until a concrete
need appears; config reads are handled with `match` at the use site.)

---

## Process

- New feature branch before any code (per `CLAUDE.md`).
- Build in `mshell/` (`go build -o ...`, in-repo cache if needed) before testing.
- `gofmt` only with explicit permission.
- `CHANGELOG.md` for user-facing additions; `mshell/BuiltInList.go` kept in sync if builtins added.

## Decisions still to nail before coding Phase 1

None blocking. The former unknowns (qualified-name dispatch, backing defaults, decode/encode
delivery) are all dropped from v1 scope above. Remaining small calls can be made during the build:

- Exact lexical home for the lightweight runtime enum value (new `MShellObject` vs. reuse).
- Whether a bare member word with **no** expected-type context (e.g. stored straight into a var) is
  allowed (resolves via the global member namespace) or requires a context — default: allowed, since
  member names are unique across enums in v1.
