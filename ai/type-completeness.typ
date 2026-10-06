// Completeness of the type checker: what it refuses that it should accept.
// Build: typst compile ai/type-completeness.typ

#set document(title: "Type Checker Completeness", author: "Claude (for Mitchell)")
#set page(paper: "us-letter", margin: (x: 1in, y: 1in), numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt)
#set par(justify: true, leading: 0.62em)
#show table: set par(justify: false)
#set heading(numbering: "1.1")
#show raw: set text(font: "DejaVu Sans Mono", size: 0.86em)

#align(center, text(size: 16pt, weight: "bold")[Type Checker Completeness])
#align(center)[Record of the work started 2026-10-06]

= Why this memo

The checker can be wrong in two directions.
It can accept a program that then stops with a type mismatch (a soundness bug), or refuse a program the
typing rules accept (a completeness bug, a false rejection).
Everything built so far checks the first direction:

- `formal-ver/` proves soundness of the declarative rules. It has no theorem that the checker accepts
  every program the rules type, and it does not model the Go code.
- The OCaml oracle extracted from Rocq checks `Sub`, `Retype` and `JoinSlot` against Go in both
  directions, but only for those relations, and only against the Rocq functions, which are themselves
  proved only to give upper bounds (see @sec-next).
- `TestGeneratedProgramsSound` keeps a statement only if the checker accepts it, so it cannot see a
  false rejection. The design doc planned a generator that runs the rules backwards; the one built is
  filtered by the checker.
- `tests/typecheck_test.sh` over `tests/success` is the only program-level check of the second
  direction, and it is written by hand.

The quote fixes of 2026-10-06 (`0af057e`, `3519dac`) were both false rejections: the rules type
`(:a?)` at `int | ({a: int} -- int)` (*Quote* at `{a: int}`, then subsumption), but the checker typed
the literal on its own, because it tested `Kind == TKQuote` on a type that was a union or an alias.

= Two exercises

== Refused "safe" statements

The generator labels each statement safe or risky.
With `MSH_GEN_SAFE_DIR=dir` (added to `TypeSoundGen_test.go`), each refused safe statement is written to
`dir/seed<n>.txt` with the program and the first error.
A run of 300 programs refused 463 safe statements of about 24,000.
Run without the checker, 66 of those programs stopped with a type mismatch, 11 failed otherwise, and 386
ran; but a refused statement often sits in a branch that did not run, so "ran" is weak evidence.
Sorted by hand:

- most are the generator's mistake: a bare `just` pattern, `int str +`, interpolating a dict, a generic
  def's output treated as new, widening an open shape after `deepCopy`;
- some are design limits (@sec-design);
- the rest are the false rejections A, B and C of @sec-found.

== Every written type through an alias

A test-only switch in the type resolver made every written type with no generic in it an alias of
itself; every `tests/success` program must still check.
6 of 267 failed. Rewritten with aliases a program can declare, they gave D to G of @sec-found.
The switch is removed: after the refactor of @sec-refactor, an alias that does not refer to itself is
the same `TypeId` as its body, so the test can no longer fail.

= False rejections found <sec-found>

#table(
  columns: (auto, 1fr, 1fr, auto),
  align: (left, left, left, left),
  table.header([], [Refused program], [Cause], [Status]),
  [A], [`true if none just else none end`, `[none just none]`, `true if none else [] just end`],
    [`none` does not join with `Maybe[X]` while X has a part not worked out yet. Go only.], [open],
  [B], [`[true 1 "s" as int | str]`; `["s" as int | str true 1]` passes],
    [A union joined with a union has no join, so the answer depends on the order. In the spec:
     `formal-ver/Join.v`, `TUnion _ _, TUnion _ _ => None`.], [open],
  [C], [`@row toDict as {a: int | str, b: [int]}`, a row with a list column],
    [`gridToDict` makes the dict shared when a column holds lists; it should be new at the top with
     shared contents, as a dict literal is.], [open],
  [D], [`type N = null`, then `=` on two `int | N` values], [overload matching does not look through an
    alias member of a union], [fixed by @sec-refactor],
  [E], [`type Q = P` (P recursive), a def with output `Q` built from a new dict],
    [the extra alias stops the retype of a partly new value], [fixed by @sec-refactor],
  [F], [`type U = [str | int]`, then `as U append`], [`appendBelow` does not look through the alias],
    [fixed by @sec-refactor],
  [G], [`type A = Maybe[int]`, `type B = Maybe[float]`, `if @a else @b end`],
    [the join treats every alias as opaque, as the model treats recursive ones], [fixed by @sec-refactor],
  [H], [`type Cmds = [[str]]`, then `@cmds |`], [the pipe does not look through the alias],
    [fixed by @sec-refactor],
)

D to H, and the quote alias of `3519dac`, share a cause: the checker kept non-recursive aliases as
alias types, and each place that looks at a type's kind had to remember to look through them.

= The refactor: an alias that does not refer to itself is its body <sec-refactor>

== What changed

- *Declarations* (`eraseAliases`, `TypeCoreDecl.go`). Pass 1 still makes an alias type for every
  declared name, so a body may name an alias declared after it and the check for unguarded cycles
  works. After that check, each alias that does not refer to itself (`aliasRecursive`) is replaced by
  its body: its name resolves to the body, and every reference to it in other aliases' bodies and in
  enum payloads is replaced. Recursive aliases stay alias types, with those references in their
  bodies replaced.
- *Built-in aliases* (`HttpRequest`, `PromptInfo`, `UrlEncodable`, ...): none refers to itself, so
  each name resolves to its body. `Json` and `HtmlNode` are recursive and are unchanged.
- *Names in messages* (`TypeArena.NameType`, `DisplayName`, used by `FormatType`). The body of a
  non-recursive alias gets the alias's name for messages when it is a record with labels, a union or
  a quote type. Not `int`, `[int]` or `{str: int}`: their name would print for every such type where
  the program never wrote it. The first name given to a type stays. A REPL line that is refused takes
  its names back with its declarations.
- *Checker*. `plainAlias` is gone: it looked through non-recursive aliases, and there are none. Its
  callers in `matchSub`, `distribute`, `redirectTarget`, `unionMemberOfKind`, `msub` and
  `storedBlockers` use the type as it is.

== The invariant

*Every alias type the checker meets is recursive* (or belongs to a declaration with an error).
This is how `formal-ver` already models aliases: a recursive alias is `TMu`, any other alias is its
body. So the checker and the model now agree on what an alias is, and a kind test on a type can only
miss a recursive alias.

== What is left for recursive aliases

`unfold` looks through a recursive alias; `members` and `memberOfKind` look through aliases and
unions. The places the agent's audit listed that do not unfold (`appendBelow`, `widenForAppend`,
`pipe`) now see only recursive aliases. The model never widens inside a recursive type, so those were
left alone: unfolding there must be checked against the rules first, not added for consistency.

= Design limits, not bugs <sec-design>

These are refusals the rules make, or the checker makes on purpose; each is a decision for Mitchell.

- *`cond if A else B end as T`* does not give `T` to the arms; the arms must join first (31 of the
  refusals). The message says to put `as` in each arm. Same shape as the quote bug: the wanted type is
  written after the value.
- *Freshness does not flow through quotes.* `(drop {a: false}) map` gives elements the checker treats
  as shared, since quote types have no `new` outputs (38).
- *Builtin signatures stricter than the runtime*: `[] [] =`, `2.5 "é" !=`, interpolating a bool.
- *`tryAs` that can never pass*, such as an int against `{str: int}`, is refused with the message
  about values referenced elsewhere, which is the wrong reason.

= Next steps <sec-next>

+ *Join.* Fix B in `Join.v` and Go together, and prove a completeness lemma: when both
  slots have an upper bound of the forms the join builds, `join_slot` finds one. `join_slot_ub` proves
  only that an answer is an upper bound. Rebuild the oracle, which today agrees with Go because both
  have the gap.
+ Fix A (joins with types not worked out yet) and C (`toDict`).
+ Make the generator's labels honest, then fail `TestGeneratedProgramsSound` on a refused safe
  statement, so the generator checks both directions.
+ Longer term: a checking-mode rule in the design doc (a literal where a type is wanted is checked
  against the quote type found in it), and a generator built from the rules.
