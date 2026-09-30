// A core calculus for mshell's static types.
// Build: typst compile ai/type-core-calculus.typ

#set document(title: "A Core Calculus for mshell Types", author: "Claude (draft for Mitchell)")
#set page(paper: "us-letter", margin: (x: 1in, y: 1in), numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt)
#set par(justify: true, leading: 0.62em)
#show table: set par(justify: false)
#set heading(numbering: "1.1")
#show heading.where(level: 1): it => { v(0.8em); it; v(0.3em) }
#show raw: set text(font: "DejaVu Sans Mono", size: 0.86em)
#show raw.where(block: true): it => block(
  fill: luma(245), inset: 8pt, radius: 3pt, width: 100%, it,
)

// ---------- helpers ----------

// An inference rule: rule("Name", prem1, prem2, ..., conclusion).
#let rule(name, ..args) = {
  let a = args.pos()
  let concl = a.last()
  let prems = a.slice(0, a.len() - 1)
  let top = if prems.len() == 0 { $space$ } else { prems.join($quad$) }
  box(inset: (y: 4pt), $ frac(#top, #concl) #h(0.4em) #text(size: 0.8em, smallcaps(name)) $)
}

#let rules(cols: 2, ..rs) = align(center, grid(
  columns: cols, column-gutter: 2em, row-gutter: 0.9em, align: center + horizon,
  ..rs.pos(),
))

#let callout(kind, title, body) = block(
  width: 100%, inset: (left: 10pt, rest: 8pt), stroke: (left: 2pt + luma(120)),
  fill: luma(250),
  [*#kind* #if title != none [(#title)]. #body],
)
#let theorem(title, body) = callout("Theorem", title, body)
#let lemma(title, body) = callout("Lemma", title, body)
#let defn(title, body) = callout("Definition", title, body)
#let principle(n, body) = callout("Principle " + str(n), none, body)

#let w(s) = raw(s)                    // an mshell word, in math or text
#let ty(s) = math.upright(math.sans(s)) // a type constructor name
#let eff(a, b) = $#a => #b$             // stack effect
#let qt(a, b) = $[#a -> #b]$            // quote type
#let vec(x) = $overline(#x)$
#let fr(x) = $#x^bullet$                // a fresh (unaliased) stack slot

// ---------- title ----------

#align(center)[
  #text(size: 20pt, weight: "bold")[A Core Calculus for mshell Types]
  #v(0.3em)
  #text(size: 11pt)[Draft for review --- revised 2026-09-29, checked against the Rocq development in `formal-ver/`]
  #v(0.2em)
  #text(size: 10pt, style: "italic")[Written by Claude from a review of the checker on `main` (b511d9b).
  Implementation plan: `ai/type-system-plan.md`]
]

#v(1em)

#block(inset: 10pt, stroke: 0.5pt + luma(160), radius: 3pt)[
  *Summary.*
  The current checker has no single set of rules one could write down and prove.
  This document gives a small _core calculus_ with a standard, textbook-shaped soundness proof,
  even though mshell has mutable, aliased lists, dicts, shapes and grids.
  Everything else in the language is _elaboration_ into the core; only the core has to be trusted.

  It builds on these decisions (`type` is a transparent alias with guarded recursion, `enum` is the
  only nominal form, `as` is static ascription only, `tryAs` is the runtime check, containers are
  invariant, shapes have width subtyping), and adds the one rule they were missing:

  #align(center)[*A shared object never gets a second, incompatible static type.
  A fresh object can be given any type it satisfies.*]

  That rule closes the hole the earlier design had accepted (narrowing an aliased container twice),
  and it fixes redirects, `updateCol` and `tryAs` with the same mechanism.
  The checker only accepts or rejects: no word's runtime behavior depends on it. Where a program
  would give a shared object a second type, it is rejected, and the fix is an explicit `deepCopy`.
  The goal is *no known unsoundness at all*: every counterexample is either a type error or a proof case.

  *Mechanized.* The core is formalized in Rocq in `formal-ver/`, with a machine-checked soundness
  theorem that depends on no axioms (@sec-mech). Formalizing it found three holes in the previous
  draft, and each has a runnable counterexample: a runtime-key `get` on a shape (@sec-dyn-key),
  an abstract type escaping its pattern arm (@sec-unknown), and a shape literal marked fresh
  around a shared value (@sec-fresh). Adding generic enums to the proof found three more:
  "enums are immutable" (@sec-fresh), fresh retyping of an enum argument used under a quote
  (@sec-sub), and joins that widen inside fresh quotes (@sec-join). Proving that a polymorphic def
  checked once is enough found two more: a type variable counted as immutable (@sec-fresh) and a
  kind pattern on a type variable (@sec-unknown). Those rules are corrected below.
  Rules the proof showed to be stricter than soundness needs are marked as usability choices.
]

#outline(depth: 2, indent: auto)

#pagebreak()

= Why a core calculus

== Programs the checker accepts that then fail with type errors

Each program passes `msh --type-check-only` and then fails at runtime with a type mismatch
(not an intended failure such as a missing file). P1--P13 are on `main`; R6 is on the `try-as` branch.

#table(
  columns: (auto, 1fr, auto),
  inset: 6pt,
  stroke: 0.5pt + luma(180),
  table.header([*\#*], [*Program*], [*Runtime*]),
  [P1],
  [```
def setA ({a: int | str} -- ) "a" "x" set drop end
{a: 1} dup setA :a? 1 + wl
```],
  [int + String],

  [P2],
  [```
def onlyA ({a: int} -- {a: int}) end
def vals ({str: int} -- ) values (1 + wl) each end
{a: 1, b: "x"} onlyA vals
```],
  [int + String],

  [P3],
  [```
def getA ({a: int} -- int) :a? end
{} as {str: int} getA 1 + wl
```],
  [`?` on None],

  [P4],
  [```
def put ({str: int | str} -- ) "a" "x" set drop end
{a: 1} dup put :a? 1 + wl
```],
  [int + String],

  [P6],
  [```
[| x; 1; 2; 3 |] g!
@g "x" (str) updateCol drop
@g "x" gridCol (1 +) map len wl
```],
  [int + String],

  [P7],
  [```
[echo hi] c!
@c * ; drop
5 @c ; 1 + wl
```],
  [int + String],

  [P10],
  [```
"\"hi\"" parseJson 1 + wl
```],
  [int + String],

  [P13],
  [```
"{\"a\": 1.0}" parseJson as {a: int} :a? 1 + wl
```],
  [int + Float],

  [R6],
  [```
"{\"age\": 1}" parseJson j!
@j tryAs {age: float} ? p!
@j tryAs {str: float | str} ? d!
@d "age" "x" set drop
@p :age? 1.0 + str wl
```],
  [float + String],
)

- P7: `*` sets the capture on the list object in place; the checker still types `@c ;` as producing nothing.
- P10: `parseJson` is declared `(str | path | bytes -- t)` with a free `t`, so its result fits any type.
- P13: `as` performs no runtime work, so it asserts what nothing checked.
- R6 is the hole the earlier design accepted:
  two `tryAs` refinements return _the same_ dict under two incompatible static types.

== The diagnosis

`unify` in `mshell/TypeChecker.go` does three jobs:

+ *Equality unification* of type variables (Hindley--Milner).
+ *Directional subtyping*: union injection, width subtyping on shapes,
  string literal $subset.eq$ `str`, contravariant quote inputs.
+ *Implicit conversions*: dict $arrow.l.r$ shape.

Subtyping, conversions and mutable aliased references together are the classic recipe for unsoundness
(TAPL §15.5). The symptoms in the code:

- *Patch-per-symptom.* The \#351 invariance fix added a freshness tracker, a `readOnlyArgs` mode,
  and a hand-maintained `mutatingBuiltins` list. It covers lists and dicts,
  but shapes (P1), conversions (P2, P4), grids (P6) and commands (P7) go around it.
- *Order dependence.* `inputUnifyOrder` exists, and union matching commits to the first arm that
  unifies. The answer can depend on the order constraints are visited.
- *Too many simultaneous mechanisms.* Branch inference yields _sets_ of signatures,
  on top of which sit user overloads and distribution over unions.

Subtyping itself is not the problem. The problem is that there is no single written relation,
so nothing says which uses of it are safe. This document writes that relation down (@sec-sub)
and shows, clause by clause, which write would break an alias if the clause were weaker.

== What we are proving

#defn("Type soundness")[
  A well-typed program never reaches a state where a builtin or core operation
  receives a value of the wrong _runtime type_.
  It may still stop with a *checked error*: unwrapping `none`, index out of bounds,
  a failed process, `exit`, division by zero, a validation resource limit, and so on.
]

Soundness does _not_ promise that programs do not fail.
It promises that failures are the ones visible in the types (a `Maybe`, a process call),
never "Cannot add an integer to a String".
That is exactly the property that lets the runtime type checks in `Evaluator.go` be removed,
the original performance goal of the checker. A single accepted hole would keep them all.

#pagebreak()

= Design principles

#principle(1)[
  *One written subtyping relation, and nothing else.*
  Subtyping ($tau <= upsilon$) exists only where no write can observe it:
  union injection, enum parameters by their variance (`Maybe` is covariant), quote variance, and shape width subtyping under the
  conditions of @sec-sub. List elements, dict values and shape field types are *invariant*.
  Type variables are solved by equality only; subtyping is checked where both sides are known.
]

#principle(2)[
  *A shared object keeps its type.*
  Each list, dict, shape, grid and variable cell gets a type when it is created.
  While another reference to it may exist, nothing changes that type, and every write stores
  a value of exactly the declared type.
]

#principle(3)[
  *A fresh object can be given any type it satisfies.*
  If no other reference exists, nobody can observe a change of type.
  This single exception covers: `as` on a literal (`[1 2] as [int | str]`),
  redirects on a literal command (`[cmd]*!`), type-changing `updateCol` on a new grid,
  and `tryAs` validating `parseJson` output in place.
  When the operand is *not* fresh, those operations are type errors. The program makes a fresh value
  first with an explicit `deepCopy` (@sec-copy).
]

#principle(4)[
  *Everything else is elaboration.*
  Overloads, `iff`, optional fields, literal-key `get`, `tryAs`, `=>`, `is` patterns:
  all translate into core terms before core checking. Elaboration only has to produce well-typed
  core, and the core checker re-checks what it produces.
]

#principle(5)[
  *Builtins are axioms with a contract.*
  The core has a table $Phi$ of builtin signatures. Each Go implementation must honor its entry
  (@sec-contract). This is the only part of soundness not proved on paper; it is tested.
]

#principle(6)[
  *The checker never changes what a program does.*
  Every word has one runtime behavior, and it does not depend on any static fact such as freshness.
  The checker only accepts or rejects. There are no hidden copies and no copy-on-write:
  when a program needs a copy, it says `deepCopy`.
  Type checking is optional (`--check-types`), and a program must behave the same with it or without it.
]

#principle(7)[
  *No two lists share storage.*
  An operation either changes the list it is given and returns that same list (`append`, `setAt`, `del`),
  or returns a new list that nothing else references (`take`, `skip`, slices, `...rest`, `map`).
  No operation returns a list that shares its elements' storage with another list (@sec-new-lists).
]

Principles 2 and 3 are where reference semantics enter. Reference semantics do not prevent a clean
proof: ML has mutable arrays and references, and its soundness proof (Wright & Felleisen 1994) is a
textbook exercise. What breaks proofs is a location being seen at two types that disagree about
what may be written. Principle 2 forbids that for shared objects; Principle 3 notes that a fresh
object has only one viewer, so there is nothing to disagree with.

= Types

== Grammar

$
  "base" B & ::= "int" | "float" | "str" | "bool" | "bytes" | "path" | "datetime" | "null" \
  "types" tau & ::= B | alpha | k | bot | qt(vec(tau), vec(tau))
    | ty("List") tau | ty("Dict") tau \
  & quad | {f_1, ..., f_n | rho} & "(shape)" \
  & quad | ty("Grid"){c_1, ..., c_n} | ty("Grid"){k} & "(grid, known or abstract schema)" \
  & quad | E | E[vec(tau)] & "(nominal enum, possibly generic; " ty("Maybe") tau " is one)" \
  & quad | tau_1 | tau_2 & "(union of distinct kinds, see below)" \
  & quad | X & "(reference to a type alias)" \
  "fields" f & ::= ell : tau | ell? : tau \
  "remainder" rho & ::= "exact" | * : tau | "open" \
  "stacks" sigma & ::= epsilon | sigma space tau | sigma space fr(tau) & "(top on the right; " fr(tau) " = fresh)" \
  "schemes" s & ::= forall vec(alpha). qt(vec(tau), vec(tau)) | forall vec(alpha). qt(vec(tau), "never")
$

- $alpha$ is a type variable (solved by equality). $k$ is an *abstract* type: a fresh rigid type
  introduced when a value's contents are unknown (@sec-unknown). $bot$ is the empty type of divergence.
- $qt(vec(tau)_1, vec(tau)_2)$ is a *quote type*, exactly the surface `(a b -- c)`.
  It is implicitly polymorphic in the rest of the stack (the frame lemma, @sec-sound),
  so there are no stack variables. Stack variables would need associative unification,
  which has no most general solutions.
- A shape lists its declared fields (`ell?` marks a key that may be absent) and a *remainder*
  saying what may be under undeclared keys: nothing (`exact`), values of one type (`*: T`),
  or unknown (`open`, the default for a written shape type).
  A shape literal has an `exact` type.
- `Maybe` is a generic enum, declared built in as `enum Maybe[a] = just a | none end` (@sec-surface).
  $ty("Maybe") tau$ in this document is that enum at $tau$, and what is said about it holds for any
  enum parameter that is covariant. Users declare generic enums the same way.
- Enums are nominal: an enum value carries its enum's identity, its constructor and its payload
  (@sec-surface).

== Aliases and recursive types <sec-alias>

`type X = T` is a *transparent* alias: $X$ and $T$ are interchangeable everywhere;
the name only improves diagnostics.

Aliases may be *recursive* when every recursive reference is *guarded* by a list, dict, shape field,
`Maybe` or quote. So both of these are accepted:

```
type Json = null | bool | int | float | str | [Json] | {str: Json}
type Person = {name: str, age: int, friends: [Person]}
```

and `type A = A`, `type A = int | A` are rejected. Recursion is not limited to enums.

*Meaning.* A recursive alias denotes the infinite regular tree obtained by unfolding it.
Two types are equal when their trees are equal, and $tau <= upsilon$ holds when it holds of the trees.

*Algorithm.* The checker keeps the alias as a reference node and unfolds it only when it needs
to look inside. To decide $tau <= upsilon$ or $tau equiv upsilon$ it carries a set of assumed pairs:
if the pair is already assumed, it holds; otherwise assume it and compare one level.
Guardedness makes every unfolding reach a constructor, and there are finitely many pairs of
subterms, so this terminates (Amadio & Cardelli 1993; Kozen, Palsberg & Schwartzbach 1995).
Assumptions made inside a failed union alternative or overload trial are rolled back with it.
Only the alias _name_ is hashconsed; unfolded trees are never stored.

== JSON

`Json` is the built-in recursive alias above. Its members are exactly the values `parseJson` produces,
so a Json value is a plain mshell value with no wrapper.

- `parseJson : (str | path | bytes -- Json)`, and its result is *fresh* (@sec-fresh). This closes P10.
- Integral JSON numbers (no fraction, no exponent) parse as `int`, others as `float`.
- There are no operations on raw `Json`: narrow first with `match`, `is` or `tryAs`.
- Converting into your own type is one step, validated in place with no copy:

```
type Person = {name: str, age: int, friends: [Person]}
"people.json" parseJson tryAs [Person] ?
```

== Unions have distinct kinds <sec-unions>

Every mshell value carries its runtime kind. A union is well-formed only when its members have
*pairwise distinct kinds*:

#defn("Kind of a type")[
  Each base type is its own kind (`int`, `float`, `str`, `bool`, `bytes`, `path`, `datetime`, `null`).
  $"kind"(ty("List") tau) = "list"$; $"kind"(ty("Dict") tau) = "kind"({F | rho}) = "dict"$;
  every quote type has kind `quote`; `Grid`, `GridView`, `GridRow` are three kinds; each enum is its
  own kind, shared by all its instances (so $"kind"(ty("Maybe") tau) = "Maybe"$).
  An alias has the kind of its unfolding (guardedness makes that defined).
  Type variables, abstract types and $bot$ have no kind and cannot be union members.
]

So `int | float | str | null`, `int | [str]` and the recursive `Json` are fine, while
`[int] | [str]`, `{a: int} | {b: str}`, `{str: int} | {a: int}` and `(int -- int) | (str -- str)`
are rejected: two members of the same kind. Write an enum for those
(`enum Ids = ints [int] | strs [str] end`).

What this buys:

- *Taking a union apart is a kind test.* A kind pattern (`int n`, `list xs`, `dict d`) identifies
  exactly one member, in constant time, with no validation.
- *The bound value is writable, with no copy.* If `x : int | [str]` and `@x match list xs : ...`,
  then `xs : [str]` is the list's own type (a list keeps its exact type inside a union), so writing
  through `xs` can break nothing. `is T` and `tryAs` are needed only to go _beyond_ what the static
  type says, such as `Json` to `Person`.
- *No ambiguous values.* Without the rule, `[]` is both an `[int]` and a `[str]`, and the checker
  would have to validate every element to decide which member it holds.

*This is a usability rule, not a soundness rule, and it is kept for now (decided 2026-09-29).* The
mechanized core does not assume it: a kind pattern there binds the union of _every_ member of the
tested kind (`kind_then` in `Typing.v`), which is sound for any union. With distinct kinds that union
is the single member, which is what makes the binding writable and useful. So the rule can be relaxed
later without revisiting soundness, as long as a pattern on a same-kind union binds the whole same-kind part.

== Unknown contents are abstract types <sec-unknown>

Several operations learn a value's _kind_ but not its contents:
a kind pattern (`list xs`) on a value whose static type is itself abstract, reading an undeclared
key of an `open` shape, a grid whose schema is not known statically (`pivot`, schema-less readers).
Each introduces a *fresh abstract type* $k$, like unpacking an existential type:

- `@v match list xs : ...` with $v : k_0$ abstract binds $#w("xs") : ty("List") k$ for a fresh $k$.
  (When $v$'s type is a union, the pattern binds its list member instead; @sec-unions.)
- Reading an undeclared key of an open shape gives $ty("Maybe") k$ for a fresh $k$.
- A grid with unknown schema is $ty("Grid"){k}$.

Generic code works on these (`len`, `each`, `map` with a generic quote, `str`),
and a value of type $k$ can be written back into a container whose element type is the same $k$
(for example, reordering the elements of `xs`). Nothing else accepts a $k$: it is not an `int`,
not a `str`, and a different pattern gives a different $k'$. Narrowing uses `is` or `tryAs`.

This is why a kind pattern cannot be used to break an alias: `xs` can only receive values that came
out of `xs`.

*$k$ must not escape its arm (corrected).* The previous draft introduced "a fresh $k$" per pattern
without saying how long $k$ lives. In a static checker $k$ belongs to the pattern _site_. If $k$ can
outlive one run of the arm, the site binds lists with different element types on different runs,
and they all share one $k$. That is unsound:

```
[] acc!                                   # acc : [α], α a type variable
@keys (key!
    @o @key getd ? match list xs :        # xs : [k], k fixed for this site
        @acc len 0 > if @xs @acc 0 getAt append drop end   # writes an old k into xs
        @xs 0 getAt @acc swap append drop  # α := k, so acc : [k]
    end) each
```

On the first run `acc` receives an element of one list; on the second run that element is appended
to a different list with another element type. The rule the proof uses is: *the arm is checked for
every element type* (`tw_kind_list` in `Typing.v`: $forall a.$ arm $: sigma space ty("List") a -> sigma'$).
For the checker this means a skolem escape check:

- $k$ may not appear in the arm's output stack type $sigma'$;
- $k$ may not appear in the type of any variable, since $Gamma$ is per scope and outlives the arm;
- no type variable created outside the arm may be unified with a type containing $k$
  (the `acc` above). This is the standard level check for existential unpacking.

`Examples.v` (`hole_exists`) runs the smallest version of this program: it types the arm for
one fixed element type and gets stuck in the interpreter.

*Enum kind patterns on unknown values.* A pattern for enum `E` on a value of abstract type binds
`E[k_1 ... k_n]` with fresh abstract arguments, under the same escape check (`tw_kind_enum` in
`Typing.v` checks the arm for every argument list). `E[unknown]` would be wrong when a parameter is
invariant: an `E[int]` is not an `E[unknown]`, and writes through the payload could break it.

*Type variables (corrected).* A kind pattern on a value whose type is a rigid type variable $a$ binds
the unknown contents of that kind, as for an abstract type: `str x` binds `x : str`, `list xs` binds
`xs : [k]` under the escape check. It must not read $a$ as "no member of kind `str`" and check the arm
vacuously: at the instance $a = #w("str")$ the arm runs (`hole_tvar_kind_stuck` in `Examples.v`). The
substitution lemma forces this (`kind_then_tsub` in `Generic.v`).

*Dict-kind patterns on unknown values.* `@v match dict d : ...` with $v : k_0$ cannot bind
$d : {"str": k}$: the object may be a shape, and a `Dict` view could then write one field's value
into another field. It binds $d : {| "open"}$, the read-only view of "some dict", which every shape and
every `Dict` is a subtype of (@sec-sub). Reads through it give `Maybe` of an unknown type.
Writes through it are not allowed.

#pagebreak()

= Subtyping <sec-sub>

$tau <= upsilon$ is the only subtyping relation. It is used at *checking positions*
(def arguments, `as`, def outputs, quote arguments, `if`/`match` joins),
never to solve a type variable.

#rules(
  rule("Refl", $tau <= tau$),
  rule("Bot", $bot <= tau$),
  rule("Union-L", $tau_1 <= upsilon$, $tau_2 <= upsilon$, $tau_1 | tau_2 <= upsilon$),
  rule("Union-R", $tau <= upsilon_i$, $tau <= upsilon_1 | upsilon_2$),
  rule("Maybe", $tau <= upsilon$, $ty("Maybe") tau <= ty("Maybe") upsilon$),
  rule("Quote",
    $vec(upsilon)_1 <= vec(tau)_1$, $vec(tau)_2 <= vec(upsilon)_2$,
    $qt(vec(tau)_1, vec(tau)_2) <= qt(vec(upsilon)_1, vec(upsilon)_2)$),
)

There is *no* rule for $ty("List")$, $ty("Dict")$ or $ty("Grid")$ other than *Refl* (up to alias unfolding):
they are invariant. $ty("Maybe")$ is covariant because nothing writes into a `Maybe`;
the check inside still uses $<=$. The same holds for every generic enum, one parameter at a time:
enum values are never written into, so a parameter's variance is decided only by where it appears in
the payload types. Walk down from each payload to each occurrence of the parameter: a quote input or a
contravariant enum parameter flips the direction, a quote output, `Maybe` or covariant enum parameter
keeps it, and a list, dict, shape field or invariant enum parameter makes it invariant. The parameter
is covariant if every occurrence ends up in the unflipped direction, contravariant if every one is
flipped, and invariant otherwise. A parameter that appears nowhere is covariant. So `Maybe[int]` $<=$ `Maybe[int | str]` holds, but
`Maybe[[int]]` $<=$ `Maybe[[int | str]]` fails, because the lists inside are invariant.

*Fresh-covariance (corrected).* Each parameter has a second property, used only when retyping a
_fresh_ value (@sec-fresh). A parameter is *fresh-covariant* when every occurrence is in a data
position: directly a payload, or inside a list element, a dict or shape value, a `Maybe`, a union, or a
fresh-covariant argument of an enum, and never inside a quote type. A fresh `E[a]` may be retyped to
`E[b]` when each fresh-covariant argument satisfies $a_i subset.sq.eq b_i$ and every other argument
satisfies its variance. `enum Box[a] = box [a] end` is invariant but fresh-covariant, so a fresh
`[1] box` may become a `Box[int | str]`, as a fresh `[1]` may become an `[int | str]`.
`enum F[a] = f (a -- a) end` is invariant and _not_ fresh-covariant: a fresh `F[int]` holds only a
quote, and freshness stops at quotes, so retyping it to `F[int | str]` would let `(1 +)` be called
with a string (`hole_quote_arg_stuck` in `Examples.v`). `Maybe` is covariant and fresh-covariant.

*What the checker must check (mechanized).* The proof takes each parameter's variance and
fresh-covariance as declared, and checks every constructor's payload types against them
(`wf_payload` in `Subtyping.v`); `Variance.v` proves that this makes the rules sound. A recursive
reference is checked with the variances the enum itself carries, with no unfolding, so recursive and
generic enums need nothing extra. The checker computes the variances (the most permissive ones that
pass this check) instead of reading them from the declaration.

== Shape width subtyping

${F_s | rho_s} <= {F_t | rho_t}$ holds when the per-label rule of @sec-per-label holds at every key.
That rule is what the checker implements and what the proof checks (decided 2026-09-29).
Read off the two written types, it comes to the four conditions below,
each annotated with the write it prevents. "$equiv$" is type equality (invariance). Here ${"str": tau}$
counts as a type with no declared fields whose every key is optional and deletable.

#table(
  columns: (auto, 1fr, 1fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*\#*], [*Condition*], [*Otherwise this breaks*]),
  [S1], [Target field $ell : tau$ (required): source has $ell : tau'$ required, $tau' equiv tau$.],
    [P1: writing `str` into an `int` field through the wider view.],
  [S2], [Target field $ell? : tau$: source has $ell : tau'$ or $ell? : tau'$, or does not declare $ell$
    but has remainder $* : tau'$ or is ${"str": tau'}$; in each case $tau' equiv tau$.
    A source that lacks $ell$ and has an `exact` or `open` remainder is *not* accepted.],
    [The target view may set $ell$, adding a key the source's type says is absent (`exact`), or one that
    another view types differently (`open`: the key may have been forgotten by width subtyping).],
  [S3], [Source field not declared in the target: target remainder is `open`,
    or is $* : tau$ with the field's type $equiv tau$.],
    [Reading that key through the target's $* : tau$ remainder at the wrong type.],
  [S4], [Remainders: target `open` accepts any source; target $* : tau$ needs source $* : tau'$ or ${"str": tau'}$,
    $tau' equiv tau$; target `exact` needs source `exact`; target ${"str": tau}$ needs source ${"str": tau'}$, $tau' equiv tau$.],
    [A $* : tau$ view adds keys; an `exact` source must never gain keys, or a later $* : upsilon$ view of
    it reads them at $upsilon$; and only a ${"str": tau}$ object may have keys deleted.],
)

Writes through a shape view are limited to: setting a declared field (required or optional) to its
declared type, and setting an undeclared key through a $* : tau$ remainder to type $tau$.
*No write deletes a shape key* (`del` is for `Dict` only), and nothing writes through an `open` remainder.

Two consequences worth stating:

- S2 is stricter than today's docs ("a required value satisfies an optional parameter" still holds;
  but a value whose type does not cover the key does not satisfy `timeout?: int` unless it is fresh,
  @sec-fresh). The everyday case, an option dict written as a literal at the call site, is fresh and unaffected.
- *A shape is never a ${"str": tau}$*: a dict view could delete a key the shape requires, or add one
  an `exact` shape says is absent. *A ${"str": tau}$ is a shape only when every declared field is
  optional at type $tau$* and the remainder is $* : tau$ or `open`: a required field would claim a
  key exists (P3). This removes P2, P3 and P4. The read-only "some dict" view ${| "open"}$, which a
  `dict d` pattern binds on an unknown value (@sec-unknown), is one case of this rule.
- *Workaround when a conversion is refused:* `as T` on a fresh value (@sec-fresh), `deepCopy as T` on a
  shared one, or `tryAs T` for data from outside.

== The per-label reading (what the proof checks) <sec-per-label>

The mechanized core states width subtyping one label at a time, and treats `Dict` as a shape.
Every label of a dict-kinded type has a _status_:

#table(
  columns: (auto, 1fr),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*Status*], [*Meaning at that label*]),
  [$ell : tau$], [present, of type $tau$; writable at $tau$],
  [$ell? : tau$], [maybe present, of type $tau$; writable at $tau$; not deletable (a declared optional field, or any undeclared label under a $* : tau$ remainder)],
  [$ell?^"del" : tau$], [like $ell? : tau$, and deletable: every label of ${"str": tau}$],
  [absent], [never present (an undeclared label of an `exact` shape)],
  [unknown], [maybe present, any type, read-only (an undeclared label of an `open` shape)],
)

A view with status $t$ at label $ell$ is safe on an object whose own status there is $s$ when:
$t$ required needs $s$ required; $t$ optional needs $s$ required, optional or deletable;
$t$ deletable needs $s$ deletable; $t$ absent needs $s$ absent; $t$ unknown accepts anything.
Every pair with a writable target also needs the two types equal ($equiv$).
${F_s | rho_s} <= {F_t | rho_t}$ holds when this holds at every label.
S1--S4 are this rule applied to the finite representation. `Subtyping.v` proves the relation transitive.

Two things the proof established:

- *Optional must not become deletable.* It is tempting to let a shape's optional field be viewed as
  a deletable dict entry. That single step is sound on its own, but it breaks transitivity:
  required $<=$ optional $<=$ deletable would let a view delete a required key. The formalization
  found this while proving transitivity. It is the precise reason a shape is never a `Dict`.
- *An earlier draft of S1--S4 refused two sound cases* that the per-label rule accepts: (a) an
  optional target field when the source lacks it but has a $* : tau'$ remainder with $tau' equiv tau$;
  and (b) ${"str": tau} <= {ell_1? : tau, ... | * : tau}$ or $| "open"$. The conditions above now
  include both, so they match the per-label rule exactly (decided 2026-09-29).

#pagebreak()

= Core terms

$
  "words" w & ::= c | (e) | p | f | #w("x") | #w("@")x | x#w("!") & "(literal, quote, builtin, def call, exec, load, store)" \
  & quad | #w("if") e #w("else") e #w("end") | #w("loop"){e} | #w("break") | #w("continue") | #w("return") \
  & quad | #w("each"){e} | #w("bind"){e} | dots & "(quote-taking builtins with a literal quote)" \
  & quad | #w("shape"){ell_1, ..., ell_n} | #w("get")_ell | #w("set")_ell & "(shapes)" \
  & quad | C | #w("match"){"arm"_i -> e_i} & "(enums, patterns)" \
  & quad | #w("as")_tau | #w("tryAs")_tau & "(ascription, validation)" \
  "programs" e & ::= epsilon | w space e
$

Lists, dicts, grids and processes have no special syntax in the core:
their literals and operations are builtins $p$ with entries in $Phi$.

`if` and `loop` are syntactic forms. Surface `cond (a) (b) iff` with literal quotes elaborates
to `if`; `(body) loop` to `loop{body}`; `(body) each` to `each{body}`, and likewise for the other
quote-taking builtins (@sec-break). This is what lets `break`, `continue` and `return` be typed:
they appear only inside these forms and def bodies, never in a quote that could be stored and run elsewhere.

= Typing rules

The judgment is
$ Gamma; L; R tack.r e : eff(sigma_1, sigma_2) $
"in variable context $Gamma$, inside a loop whose stack is $L$ and a def that returns $R$,
program $e$ turns a stack of type $sigma_1$ into one of type $sigma_2$."
$L$ is $dot$ when there is no enclosing loop, and $R$ is $dot$ inside a quote body. In top-level code
$R$ is $top$: `return` there ends the script, and nothing reads the stack, so any stack is accepted
(`tests/success/return_top_level.msh`; `RAny` and `tw_return_any` in `Typing.v`).
$L$ may also be $star$, inside a child-stack body where `break` is allowed (@sec-break).
$Gamma$ maps variables to (monomorphic) types.

== Structure and stack

#rules(
  rule("Empty", $Gamma;L;R tack.r epsilon : eff(sigma, sigma)$),
  rule("Seq",
    $Gamma;L;R tack.r w : eff(sigma_1, sigma_2)$,
    $Gamma;L;R tack.r e : eff(sigma_2, sigma_3)$,
    $Gamma;L;R tack.r w space e : eff(sigma_1, sigma_3)$),
  rule("Lit", $c in B$, $Gamma;L;R tack.r c : eff(sigma, sigma space B)$),
  rule("Prim",
    $p : forall vec(alpha). qt(vec(tau)_1, vec(tau)_2) in Phi$,
    $Gamma;L;R tack.r p : eff(sigma space theta vec(tau)_1, sigma space theta vec(tau)_2)$),
  rule("Load", $Gamma(x) = tau$, $Gamma;L;R tack.r #w("@")x : eff(sigma, sigma space tau)$),
  rule("Store", $Gamma(x) = tau$, $Gamma;L;R tack.r x#w("!") : eff(sigma space tau, sigma)$),
)

$theta$ is any substitution for $vec(alpha)$. A variable's type is fixed for its whole scope:
storing a value of a different type is an error, never a retyping (Principle 2 for variable cells).

*Checking positions.* Where a def, builtin or quote declares a parameter type $upsilon$ with no
unsolved variables, an argument of type $tau$ is accepted when $tau <= upsilon$ (@sec-sub).
Where the parameter mentions unsolved variables, the argument must match it by equality;
if a union or width step would be needed there, the checker asks for an annotation instead of
guessing. This is what makes the result independent of checking order.

== Quotes, definitions, control flow

#rules(
  rule("Quote",
    $Gamma; dot; dot tack.r e : eff(vec(tau)_1, vec(tau)_2)$,
    $Gamma;L;R tack.r (e) : eff(sigma, sigma space qt(vec(tau)_1, vec(tau)_2))$),
  rule("Exec",
    $Gamma;L;R tack.r #w("x") : eff(sigma space vec(tau)_1 space qt(vec(tau)_1, vec(tau)_2), sigma space vec(tau)_2)$),
  rule("Call",
    $f : forall vec(alpha). qt(vec(tau)_1, vec(tau)_2)$,
    $Gamma;L;R tack.r f : eff(sigma space theta vec(tau)_1, sigma space theta vec(tau)_2)$),
  rule("If",
    $Gamma;L;R tack.r e_1 : eff(sigma, sigma_1)$,
    $Gamma;L;R tack.r e_2 : eff(sigma, sigma_2)$,
    $sigma_1 <= sigma'$, $sigma_2 <= sigma'$,
    $Gamma;L;R tack.r #w("if") e_1 #w("else") e_2 #w("end") : eff(sigma space "bool", sigma')$),
  rule("Loop",
    $Gamma; sigma; R tack.r e : eff(sigma, sigma)$,
    $Gamma;L;R tack.r #w("loop"){e} : eff(sigma, sigma)$),
  rule("Break", $Gamma; sigma; R tack.r #w("break") : eff(sigma, sigma')$),
  rule("Continue", $Gamma; sigma; R tack.r #w("continue") : eff(sigma, sigma')$),
  rule("Return", $Gamma; L; sigma tack.r #w("return") : eff(sigma, sigma')$),
)

#rules(
  rule("Def",
    $f : forall vec(alpha). qt(vec(tau)_1, vec(tau)_2)$,
    $Gamma_f; dot; vec(tau)_2 tack.r e : eff(vec(tau)_1, vec(tau)_2)$,
    $vec(alpha) "rigid in" e$,
    $tack.r #w("def") f space e #w("end") "ok"$),
)

Remarks.

- *Quote* types the body from its own entry stack, with no enclosing loop or def:
  a stored quote cannot `break` or `return`.
- *Exec* needs the quote's arity. In a def, quote parameters are annotated, so it is known.
  `x` on a quote of unknown arity is an error ("annotate this quote"), not a guess.
- *If* joins its arms (@sec-join).
- *Break*, *Continue*, *Return* may produce any stack because control does not continue (@sec-diverge).
- Variables are *monomorphic*; only defs are polymorphic, and defs are annotated.
  That is the value restriction in its simplest form.
- *Frame.* A quote type $qt(vec(tau)_1, vec(tau)_2)$ means "for every rest of the stack $sigma_0$,
  including any fresh slots in it, $sigma_0 space vec(tau)_1 -> sigma_0 space vec(tau)_2$". The mechanized
  *Quote* rule has exactly this premise (`tw_quote`: $forall sigma_0$). A checker types the body once,
  from its own entry stack; the frame lemma (`T_frame`, `quote_once` in `Frame.v`) justifies that. For a
  `never` quote the checker must know the body diverges at every frame, which is how divergence is
  tracked (@sec-diverge).
- *Def* instances. The proof treats a polymorphic signature as the set of its instances
  (`def_ok`). Checking the body once with rigid variables is enough: typing is closed under
  substitution (`T_subst` in `Generic.v`), so `soundness_generic` asks only for one check per body.
  It needs: the signatures in $Phi$ and the defs are closed under instantiation; enum declarations
  mention no type variables; and a rigid type variable is treated conservatively everywhere a rule
  asks a question about a type. It is not immutable (@sec-fresh), a kind pattern treats it as unknown
  contents (@sec-unknown), and it cannot be a `tryAs` target (@sec-tryas).

== Variable scopes

This matches the runtime (checked in `Evaluator.go`):

- Each def _invocation_ gets one fresh variable scope. Nothing is inherited from the caller;
  data only enters a def through its stack parameters. The top-level script is one more scope.
- A quote literal captures the scope it is created in, *by reference*
  (`MShellQuotation.Variables` is the enclosing map). Every quote in a def body shares that scope.
  A returned quote keeps it alive: `def g ( -- (-- int)) 1 n! (@n) end` then `g h! 99 n! @h x` prints 1.
- A quote can assign a variable the enclosing body reads afterwards (`(5 y!) x @y` works at runtime).

So $Gamma$ is per scope: every variable assigned anywhere in the scope, including in nested quotes,
has one type. Variable cells are heap objects, which is what makes a returned closure safe.
Reading before assignment is handled by a *definite-assignment* check, separate from types:
a read is accepted only if the variable is assigned on every path before it, counting stores inside
a quote only when elaboration inlines that quote (a literal `iff`, `loop`, `x`).

== Branches and joins <sec-join>

When the arms of an `if` or `match` leave different types in the same stack slot, the result type is
their *join*, computed slot by slot. There is no special syntax involved: the join uses only the
types and the freshness of the two slots, both of which the checker already tracks.

The result is fresh only when both slots are fresh. `Join.v` writes this section as a function and
proves the result is an upper bound of both arms (`join_slot_ub`), so an `if` joined this way checks in
the core (`if_join`).

+ Equal types join to themselves. $bot$ joins to the other side.
+ A slot with an unsolved type variable is *unified*, never joined, so the answer cannot depend on
  checking order.
+ Different kinds join to their union: `int` and `float` give `int | float`; `str` and `null` give
  `str | null`. If one side is already a union, the member of the same kind (if any) is joined with
  the other side.
+ `Maybe` joins inside: `Maybe[int]` and `Maybe[str]` give `Maybe[int | str]`. This is safe even for
  shared values because nothing writes into a `Maybe`. Any generic enum joins inside its covariant parameters.
  *Clarified:* the join inside is the join of the same freshness. For shared values it is itself a shared
  join, so `@xs just` and `@ys just` with `xs : [int]`, `ys : [str]` stored have no join: the list inside is
  still `xs` (`hole_maybe_join_stuck`). Fresh arms (`[1] just`, `["a"] just`) join to `Maybe[[int | str]]`.
  Two unions are joined only when equal; a union and a single type join as below.
+ Two different types of the *same* kind (two list types, two shapes, two instances of one enum):
  - if *both* slots are *fresh*, the join widens inside them: `[int]` and `[str]` give `[int | str]`;
    `{a: int}` and `{a: int, b: int}` give `{a: int, b?: int}`; `[1] box` and `["a"] box` give
    `Box[int | str]` (fresh-covariant arguments only, @sec-sub). The result is still fresh.
    This is Principle 3 applied at the merge: nobody else can see either value, so each may be given
    the wider type. The join must be an upper bound of both arms under $subset.sq.eq$.
  - otherwise there is no join, and it is an error. Use an enum, or build a new value.
+ *Two quote types join only by subtyping (corrected)*, fresh or not: freshness stops at quotes, so
  retyping a quote is $<=$ and nothing more. `(int -- int)` and `(int -- str)` join to
  `(int -- int | str)`; `(int -- int)` and `(str -- str)` have no useful join, since the inputs would have
  to meet at $bot$. The previous draft listed quotes with the fresh case: `(1 +)` and `("a" ++)` would
  have joined to `(int | str -- int | str)` (`hole_quote_join_stuck` in `Examples.v`).

The arms must still leave the same number of stack items (as today). Arms that diverge are ignored
(@sec-diverge).

#table(
  columns: (1.2fr, 1fr, 1.3fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*Arms leave*], [*Result*], [*What to write*]),
  [`1` / `2.5`], [`int | float`], [nothing],
  [`none` / `5 just`], [`Maybe[int]`], [nothing],
  [`"a"` / `null`], [`str | null`], [nothing],
  [`[1]` / `["a"]` (literals, fresh)], [`[int | str]`], [nothing],
  [`{a: 1}` / `{a: 2, b: 3}` (literals, fresh)], [`{a: int, b?: int}`], [nothing],
  [`@xs` : `[int]` / `@ys` : `[str]` (shared)], [error], [an enum, or a new list: `@xs (as int | str) map`],
  [stored shapes `{a: int}` / `{a: int, b: int}`], [error], [`as {a: int}` in the second arm (drops `b`)],
  [`1` / `1 exit`], [`int`], [nothing: the second arm diverges],
  [`1` / nothing], [error], [as today: the arms must leave the same number of items],
)

`as T` after `end` is then just ordinary `as`: static ascription, plus widening if the slot is fresh.
It is useful when you want a type other than the computed join, never required to get one.

*Why `none` needs nothing.* Constants whose value does not determine their full type get the most
precise type available, and joins or unification fill in the rest:

- `none` has type `Maybe[⊥]`: the only value of type $bot$ is none at all, so `Maybe[⊥]` contains
  exactly `none`. Since `Maybe` is covariant and $bot <= tau$ for every $tau$,
  `Maybe[⊥]` $<=$ `Maybe[T]` for every `T`, and the join with `Maybe[int]` is `Maybe[int]`.
  (This is how today's checker already types `none`, and how Scala types `None`.)
- `[]` and `{}` cannot use $bot$, because lists and dicts are invariant: a `[⊥]` could never be
  passed where `[int]` is expected. So an empty literal gets a fresh type variable, fixed by the
  first unification, and it is fresh, so a join may still widen it.

== Divergence <sec-diverge>

A word *diverges* when control never reaches the word after it: `exit`, `break`, `continue`,
`return`, a `loop` with no reachable `break`, and a def that always does one of these.
The core treats divergence as a property of the *stack effect*, not of a value:

#rules(
  rule("Exit", $Gamma;L;R tack.r #w("exit") : eff(sigma space "int", sigma')$),
  rule("Loop-Forever",
    $Gamma; sigma; R tack.r e : eff(sigma, sigma)$, $e "has no reachable" #w("break")$,
    $Gamma;L;R tack.r #w("loop"){e} : eff(sigma, sigma')$),
  rule("Call-Never",
    $f : forall vec(alpha). qt(vec(tau)_1, "never")$,
    $Gamma;L;R tack.r f : eff(sigma space theta vec(tau)_1, sigma')$),
)

The output stack $sigma'$ is _arbitrary_: since nothing after the word runs, any claim about the
stack there is vacuously true. In the algorithm this is the "diverges" flag on an effect
(@sec-infer): composing a diverging effect with anything gives a diverging effect, and a
diverging arm contributes nothing to a join.

*Dead code (corrected).* "Composing a diverging effect with anything" means the words after a
diverging word are not checked at all. The previous core still typed them, so
`def f ( -- never) 1 exit 1 + end` was rejected (the `1 +` leaves an `int`, not any stack). The core now
has the rule: $w space e$ checks from $sigma$ when $w$ diverges from $sigma$, whatever $e$ is (`t_div`).
"Diverges" must hold with any stack below $sigma$, and with the loop and return stacks extended to match;
otherwise the rule would not survive the frame lemma. `Frame.v` proves the ways a checker sets and
propagates the flag: `exit`, `break`, `continue`, `return`, a `never` call or quote, a loop with no
`break`, an `if` whose arms both diverge, a diverging word followed by anything, and a normal word followed
by a diverging sequence (`div_*`).

This is different from the value type $bot$:

- A diverging *effect* says "no stack comes out". It fits an arm that should leave two items as well as one.
- The *type* $bot$ says "no value has this type". It appears inside other types: `Maybe[⊥]` is the
  type of `none`, and an arm leaving a $bot$ in a slot joins to the other arm's type there.
  $bot <= tau$ holds for every $tau$, but nothing except $bot$ satisfies an expected $bot$.

Today's checker types `exit` as pushing a $bot$ value and then special-cases it when reconciling
branches. The core states it once, and the same rule covers user definitions.

*Definitions that never return.* Today this program is rejected:

```
def die (str -- ) wl 1 exit end
true if 5 else "bad" die end 1 + wl
```

because `die`'s signature says it returns normally with nothing on the stack, so the `else` arm
leaves zero items where the `if` arm leaves one. A signature says "never returns" with `never` as
its entire output side (decided 2026-09-28):

```
def die (str -- never) wl 1 exit end
true if 5 else "bad" die end 1 + wl     # accepted: the else arm diverges
```

The rules for `never`:

- *Position.* `never` is allowed only as the whole output side of a signature: a def signature
  `(str -- never)` or a quote type `(str -- never)`. It cannot be combined with other outputs
  (`(-- int never)` is an error) and is not a value type (`[never]`, `Maybe[never]` are errors;
  $bot$ stays internal, as the type of `none`'s contents).
- *Contextual.* It has meaning only in that position, like `is` in patterns, so it is not a reserved
  word elsewhere. `lib/std.msh` and the tests use `never` only in comments and strings.
- *Checked.* The body of a `never` def must diverge on every path: its inferred effect carries the
  diverges flag. A body that can fall through (`def f (str -- never) wl end`) is an error.
  Recursion counts: `def spin ( -- never) spin end` is accepted, since the call to `spin` diverges.
- *No `return` in a `never` def* (clarified). The *Def* rule gives the body the return context
  $vec(tau)_2$, and for a `never` def there is no such stack: `return` would come back normally to a
  caller that expects nothing to come back. The body is checked with no return context, the same
  as a quote body (`def_ok` in `Typing.v`).
- *What "never" means formally.* A quote or def whose output is `never` has a body that checks
  against _every_ output stack. The proof then rules out a normal return without any special
  case: pick the output stack $[bot]$, which no runtime stack can match.
- *Calls.* A call to a `never` def is typed by *Call-Never*: it consumes its inputs and leaves an
  arbitrary stack, so it is ignored in branch joins like `exit`.
- *Quotes.* A quote literal whose body always diverges has type $qt(vec(tau), "never")$, and
  $qt(vec(tau), "never") <= qt(vec(tau), vec(upsilon))$ for every $vec(upsilon)$: a quote that never returns can
  stand in wherever any result is expected. Executing it with `x` diverges. This lets a def take an
  error handler, `( ... (str -- never) -- ... )`.
- *Builtins.* `exit` is `(int -- never)` in $Phi$.
- *Might diverge is not a type.* A def that exits on _some_ paths declares what it returns when it
  returns; its diverging arms are ignored in the join:

  ```
  def mightExit (bool -- str)
      if 1 exit else "didn't" end
  end
  ```

  `(bool -- never | str)` is an error. Almost every def might not return (`exit`, an infinite loop,
  `?` on `none`, a failed process), so "might diverge" tells a caller nothing it can act on; only
  "always diverges" lets a caller's branch be ignored. The error message should say so and suggest
  the plain return type. (Rust draws the same line: `-> String` may call `process::exit`, only
  `-> !` never returns.)

*What soundness says about divergence.* Nothing, on purpose. Soundness (@sec-sound) is about not
getting stuck: `exit` steps to a final "exited with code $n$" state, which is a checked outcome, and
an infinite loop keeps stepping forever. Neither is a type error. The type system does not promise
termination, and code after a diverging word is typed vacuously; a warning for unreachable code
is a lint, not part of the rules.

== Quotes that break <sec-break>

At runtime `break` and `continue` are _dynamically_ scoped: a `break` inside a quote given to `each`
or `map` leaves the nearest enclosing running `loop`
(`tests/success/break_continue_through_builtin.msh` relies on this).
A stored quote containing `break` breaks whatever loop is running when it runs, or fails with
"break used outside of loop". The core keeps the tested pattern and rejects the rest:
quote-taking builtins given a literal quote elaborate to syntactic forms, and the stack the loop
sees after the break is typed. The two kinds of builtin differ here:

- A *child-stack* builtin (`each`, `map` on a list, `filter`, ...) discards the child stack on `break`;
  the loop sees the outer stack after the builtin popped its arguments.
- A *current-stack* builtin (`x`, `iff`, and also `map` on a Maybe, `map2`, `bind`, and `and`/`or`
  with a quote) leaves whatever the quote pushed so far on the loop's stack.

#rules(cols: 1,
  rule("Each",
    $Gamma; kappa; dot tack.r e : eff(alpha, epsilon)$,
    $kappa = cases(star & "if" L = star "or" sigma = L, dot & "otherwise")$,
    $Gamma;L;R tack.r #w("each"){e} : eff(sigma space ty("List") alpha, sigma)$),
  rule("Break-Star", $Gamma; star; R tack.r #w("break") : eff(sigma_0, sigma')$),
  rule("Bind",
    $Gamma; L; dot tack.r e : eff(sigma space alpha, sigma space ty("Maybe") beta)$,
    $Gamma;L;R tack.r #w("bind"){e} : eff(sigma space ty("Maybe") alpha, sigma space ty("Maybe") beta)$),
)

Without `break`, child-stack and current-stack execution are indistinguishable for a well-typed quote
(frame lemma). They differ only in which stack a loop sees after a `break`.

*As mechanized.* The proof keeps separate break and continue contexts, because `continue` inside
`each` also leaves the child stack and restarts the enclosing loop (checked in `Evaluator.go`: `each`
passes `Continue` up the same way as `break`). Each context is $dot$ (not allowed),
$sigma$ (the loop's stack, checked exactly), or $star$ (inside a child-stack body; the stack is discarded).
*Loop-Forever* is then a typing rule rather than a syntactic side condition: the body is checked with
break context $dot$ and continue context $sigma$. "No reachable `break`" becomes "`break` does not
type here", which also covers a `break` hidden in an `each` body inside the loop.

== Shapes

In this document "shape" means the dictionary shape `{a: T, ...}`; the literature calls these _records_.
Fields are unordered and each label appears at most once.

#rules(cols: 1,
  rule("ShapeLit",
    $ell_i "distinct"$, $"each" tau_i "is fresh or immutable"$,
    $Gamma;L;R tack.r #w("shape"){ell_1, ..., ell_n} : eff(sigma space fr(tau_1) ... fr(tau_n), sigma space fr({ell_1 : tau_1, ..., ell_n : tau_n | "exact"}))$),
  rule("ShapeLit-Shared",
    $ell_i "distinct"$,
    $Gamma;L;R tack.r #w("shape"){ell_1, ..., ell_n} : eff(sigma space tau_1 ... tau_n, sigma space {ell_1 : tau_1, ..., ell_n : tau_n | "exact"})$),
  rule("Get",
    $ell : tau in F$,
    $Gamma;L;R tack.r #w("get")_ell : eff(sigma space {F | rho}, sigma space tau)$),
  rule("Get-Opt",
    $ell? : tau in F$,
    $Gamma;L;R tack.r #w("get")_ell : eff(sigma space {F | rho}, sigma space ty("Maybe") tau)$),
  rule("Set",
    $ell : tau in F "or" ell? : tau in F$,
    $Gamma;L;R tack.r #w("set")_ell : eff(sigma space {F | rho} space tau, sigma space {F | rho})$),
)

*Get* on a required field is total, so `:a?` on a known required field cannot fail.
The proof checks this: the interpreter treats a missing required key as a type error, and
well-typed programs never reach it. *Set* writes exactly the field's type and never adds or deletes a
declared key. A literal key that is not declared is read and written through the remainder:
`get` gives $ty("Maybe")$ of the remainder type (an unknown type for `open`, $bot$ for `exact`), and `set`
is allowed only through a $* : tau$ remainder.

*ShapeLit is fresh only around fresh contents (corrected).* The previous rule marked every shape
literal fresh. `{a: @xs}` is a literal, but `xs` is shared, so *Retype* could turn it into
`{a: [int | str]}` and append a string to `xs` (`hole_literal` in `Examples.v`). The same holds for
list literals. A literal is fresh when each value it is built from is fresh or has an immutable type
(one with no list or dict inside it: base types, quotes, unions of those, and an enum whose payload
types, with its arguments substituted, are immutable; so `Maybe[int]` is immutable and `Maybe[[int]]`
and `Box[int]` are not).
*Corrected:* the previous draft listed every enum as immutable. A box around a stored list would then
be fresh, retypable, and a second view of the list (`hole_box_stuck` in `Examples.v`).
A rigid type variable is *not* immutable: an instance may be a list. Otherwise
`def g (a -- Maybe[[str]]) tryAs [str] end` could make its shared argument fresh and validate it in place,
and `@xs g` with a stored empty `[int]` would return `xs` as a `[str]` (`hole_tvar_imm_stuck`).
For a recursive enum, immutability is the greatest fixed point: `enum Tree = leaf int | node Tree Tree end`
is immutable. The proof checks it as a flag on the enum (`en_imm`), and conservatively also asks
that the arguments be immutable.
Otherwise it is an ordinary shared value, which *ShapeLit-Shared* types. This is #351's `freshDeep`.

=== Runtime keys <sec-dyn-key>

*Corrected.* The previous draft said undeclared keys are read with a runtime key "giving `Maybe` of the
remainder". A runtime key can name a _declared_ field, so that rule is unsound:

```
{a: 1} as {a: int, *: str}      # fresh, so allowed
"a" getd ? "x" ++                # claimed str, actually 1
```

(`hole_dynget` in `Examples.v` gets stuck in the interpreter.) The rules checked by the proof:

#rules(cols: 1,
  rule("Get-Key",
    $forall ell. space "type of label" ell "in" {F | rho} <= upsilon$,
    $Gamma;L;R tack.r #w("getd") : eff(sigma space {F | rho} space "str", sigma space ty("Maybe") upsilon)$),
  rule("Set-Key",
    $forall ell. space ell "is writable at" upsilon "in" {F | rho}$,
    $Gamma;L;R tack.r #w("setd") : eff(sigma space {F | rho} space "str" space upsilon, sigma space {F | rho})$),
)

For `get`, $upsilon$ must be above every declared field type and the remainder type. It is the
unknown type if the remainder is `open`. For ${"str": tau}$ it is just $tau$. For
`{a: int, *: str}` it is `int | str`. For `set`, every label must accept exactly $upsilon$, which in
practice means ${"str": tau}$. Deleting a key needs a deletable label, so again only ${"str": tau}$.

=== Type-changing updates of fresh records

A fresh record may be updated in place at a _different_ type, because no other view can see it:

#rules(cols: 1,
  rule("Set-Fresh",
    $Gamma;L;R tack.r #w("set")_ell : eff(sigma space fr({F | rho}) space fr(tau), sigma space fr({ell : tau, F without ell | rho}))$),
  rule("Del-Fresh",
    $Gamma;L;R tack.r #w("del")_ell : eff(sigma space fr({F | rho}), sigma space fr({ell "absent", F without ell | rho}))$),
)

The overwritten or deleted value becomes garbage and is committed at its current type (@sec-sound).
These are the core form of every "in place when fresh" rule in this document: redirects on a fresh
command, type-changing `updateCol`/`gridAddCol`/`gridRemoveCol`/`gridSetCell` on a fresh grid, and building a literal one key at
a time. The mechanization proves them (`tw_setk_dp`, `tw_del_dp`).

== Freshness <sec-fresh>

A stack slot marked $fr(tau)$ holds a value that nothing else references: not a variable, not
another stack slot, not a container. Fresh values come from literals whose nested containers are
themselves fresh, and from builtins whose $Phi$ entry marks the output fresh
(`parseJson`, `parseCsv`, `split`, `lines`, ...). Freshness is lost by any step that could copy the
reference: `dup`, a store, passing it to a def, putting it in a container.
This is the analysis \#351 already implements (`fresh`, `freshDeep`), moved to where it is sound.

#rules(cols: 1,
  rule("Forget", $Gamma;L;R tack.r epsilon : eff(sigma space fr(tau), sigma space tau)$),
  rule("Retype",
    $tau subset.sq.eq upsilon$,
    $Gamma;L;R tack.r #w("as")_upsilon : eff(sigma space fr(tau), sigma space fr(upsilon))$),
  rule("As",
    $tau <= upsilon$,
    $Gamma;L;R tack.r #w("as")_upsilon : eff(sigma space tau, sigma space upsilon)$),
)

*As* is static ascription: it needs evidence, $tau <= upsilon$, and does no work at runtime.
*Retype* is the one extra thing `as` may do: give a fresh literal a wider type, element by element
(`[1 2] as [int | str]`, `{url: "x"} as Request` with `timeout?` absent). This removes P13: nothing
lets `as` claim `{a: int}` for a `Json` value.

=== Freshness, precisely (as proved)

*What fresh means.* A slot is fresh when the lists and dicts reachable from its value form a
_tree_: each one is referenced exactly once, either by the slot or by its parent in the tree. No
variable, no other slot and no other object references any of them. Reachability stops at quotes: a
quote's captured scope is not part of the tree. "Deep" is essential. A fresh value whose tree shares
one list under two keys could be retyped with two different types for that list.

*Retype is a static relation.* The previous draft's premise, "$v$ is a literal satisfying $upsilon$",
mentions a runtime value, so no checker could decide it. The rule now uses a static relation $tau subset.sq.eq upsilon$:
subtyping made covariant everywhere because nobody else can observe the change:

- anything that is $tau <= upsilon$;
- $ty("List") tau subset.sq.eq ty("List") upsilon$ and $ty("Maybe") tau subset.sq.eq ty("Maybe") upsilon$ when $tau subset.sq.eq upsilon$;
- $E[vec(a)] subset.sq.eq E[vec(b)]$ when each fresh-covariant argument has $a_i subset.sq.eq b_i$ and
  every other argument satisfies its variance (@sec-sub). Not inside quotes: a quote is retyped only by $<=$;
- shapes and dicts label by label: required stays required, and required, optional or deletable may become
  optional or deletable. An _absent_ label may become optional or deletable (so `{url: "x"}` becomes a
  `Request` with `timeout?` absent), and anything may become unknown (`open`). The types inside are
  compared with $subset.sq.eq$. So a fresh shape whose fields all fit becomes a ${"str": tau}$, the
  "product to function as a new value" of @sec-primitive;
- unions on either side as for $<=$.

*Slot subsumption.* Freshness is part of the stack type, and the one subsumption rule
works slot by slot:

#table(
  columns: (auto, auto, 1fr),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*From*], [*To*], [*When*]),
  [$tau$], [$upsilon$], [$tau <= upsilon$ (*As*)],
  [$fr(tau)$], [$fr(upsilon)$], [$tau subset.sq.eq upsilon$ (*Retype*); no runtime work, and the store typing does not change],
  [$fr(tau)$], [$upsilon$], [$tau <= upsilon$ (*Forget* then *As*). The value's tree is _committed_: each object gets the one type the tree gives it],
  [$tau$], [$fr(upsilon)$], [$tau$ immutable and $tau <= upsilon$ (a value with no lists or dicts is trivially fresh)],
)

*Which words keep freshness.* Stack shuffles (`swap`, `drop`) move the mark with the value.
`just` and `?` keep it. Appending a fresh or immutable value to a fresh list keeps it (the two trees
merge). *Set-Fresh* keeps it. Everything that reads _out_ of a container gives a shared value
(`getAt`, `get`, `?` on a shared `Maybe`): the container still points to the result. Everything that
copies a reference needs a shared operand: `dup`, stores, def and quote arguments, and writes into a
shared container. The analysis in \#351 must be at least this conservative.

*New def outputs (decided 2026-09-29).* A def output may be marked `new`: `def loadConfig ( -- new Json)`.
The body must leave a new (fresh) value there, and callers get it as new, so `loadConfig tryAs Config ?`
validates in place. Without it the output is shared, and the same call is rejected (suggesting a full
`deepCopy`). The proof needed no change beyond letting signatures carry freshness marks (`g_sigs` in
`Typing.v`; `mk_*` in `Examples.v`).

The mark must match the checker exactly; there is no "could have been `new`" state:

- `new` written, but the body's output is shared: an error at the def, naming the step that made it
  shared ("the value is stored in `j` on line 3; return it without storing it, or `deepCopy` it").
- `new` missing, but the body's output is new: also an error at the def ("this output is new, from
  `parseJson` on line 2; mark it `new`"). The LSP offers the fix as a code action, and a fix-all.
- `new` is allowed only on output types that can hold a list, dict or grid: on an immutable type
  (`str`, `int`, `Maybe[int]`, ...) freshness means nothing, so `new` there is an error with a fix
  that removes it. A type variable counts as mutable (`(a -- new a)` is valid for `deepCopy a`).
- For a def that calls itself, both marks can be consistent (`[]` in one arm, the recursive result in
  the other). The checker requires the largest consistent one: `new` unless assuming it fails.
- "Exactly" means exactly what the checker's analysis computes. A later, more precise analysis (moves
  from local variables, below) turns some unmarked outputs into required `new`s; the LSP fix-all
  migrates them.

This is redundancy of the useful kind (Bright, "Redundancy in Programming Languages", 2008): the written
mark and the analysis of the body are independent, so a disagreement is always a mistake, and the error
lands in the def that changed. It follows D's split between named functions, whose attributes are
written, and function literals, whose attributes are inferred. Quote types stay inferred, with shared
outputs.

Inputs stay shared for now. Under the current rules a new input is lost at the first `req!`, which is
how most defs start, so the mark would rarely help. The refinement that would change that, a read
that is the last use of a local variable in a def whose scope cannot escape (a "move"), is deferred; it
should be mechanized before it is built.
Outputs built from a quote's results, such as `map`'s, are fresh only when their element type is
immutable: a quote's results are shared values. "Fresh when the input is fresh" would be wrong for
`map`: `[0 0] (drop @ys) map` is the stored list `ys` twice, and widening it would let a string be appended
to `ys` (`hole_map_fresh_stuck`; `map` is in the model, `tw_map`/`tw_map_imm`).

*Fresh when the input is.* Some builtins return a new list whose elements are the input's elements:
`take`, `skip`, the index slices, `...rest` (@sec-new-lists). Their result is fresh when the input
was fresh (the input is consumed, so its elements are a subtree nothing else reaches) or when the
element type is immutable (there is nothing below the new list to share). Otherwise the result is a
new list over shared elements, which is an ordinary shared value. So $Phi$ needs a third output mark
besides "fresh" and "shared": _fresh if that input is fresh or the elements are immutable_.
`lines 1 skip` is fresh; `@xs 1 take` with `xs : [[int]]` is not, because its inner lists are still `xs`'s.

== Explicit copies: `deepCopy` <sec-copy>

#rules(cols: 1,
  rule("DeepCopy",
    $Gamma;L;R tack.r #w("deepCopy") : eff(sigma space tau, sigma space fr(tau))$),
)

`deepCopy` is the only way to turn a shared value into a fresh one. It is typed exactly like this in
the mechanization (`tw_copy`), and `dcopy_fresh` in `Copy.v` proves the result is a fresh value of the
same type, made only of new locations.

*It always copies.* Even when the checker knows the operand is already fresh, `deepCopy` copies
(Principle 6). A warning for a `deepCopy` of a fresh value is a possible lint, not a rule.

*It is deep.* Freshness is deep (@sec-fresh), so the copy must be. A shallow copy of `[[int]]` would make
a new outer list over the same inner lists, and retyping it to `[[int | str]]` would give those inner
lists a second type: R6 again. A shallow copy is therefore just a shared value of the same type, and it
fixes nothing. Deep costs no more than shallow when the elements are immutable: strings, paths and
numbers are shared, not copied, so `deepCopy` of a `[str]` (a command's arguments, say) is one
allocation and one copy of the element pointers. It costs more only when there are nested
containers, which is exactly when a shallow copy would be unsound.

*It copies per path.* One object reached along two paths (`{a: @xs, b: @xs}`) is copied twice, so the
result is a tree. A memoizing copy would put one new list under both keys, and a retype could then
describe the two keys differently (`{a: [int], b: [int | str]}`): R6 inside the copy.
`copy_two_paths_separate` in `Examples.v` runs this case. Two consequences:

- *A cyclic value is a checked error* (`copy_cycle_err`). The model bounds the copy's depth by the heap
  size. The Go implementation should instead track the containers on the current path and fail
  with a message naming the cycle.
- *A value with a lot of internal sharing grows when copied*, exponentially in contrived cases.
  `toJson` does the same. This needs a sentence in the user documentation, not a rule.

*What it copies, by runtime kind:*

#table(
  columns: (auto, 1fr),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*Kind*], [*`deepCopy`*]),
  [list], [a new list object with a new backing array; the redirect and stdin fields are copied (they are part of a command's type); each element is copied],
  [dict, shape], [a new map; each value is copied],
  [`Maybe`, enum value], [a new wrapper; the payload is copied, since it may hold a list],
  [Grid], [column storage copied; `GenericData` cells copied; grid and column metadata dicts copied],
  [pipe], [its command lists are copied],
  [quote], [shared. The captured scope is by reference, and freshness stops at quotes],
  [`str`, `path`, `int`, `float`, `bool`, `datetime`, `bytes`, `null`], [shared: immutable],
  [`GridView`, `GridRow`], [the selected rows are copied into a new grid, and the result is a view (or row) over it: the same type, fresh, at a cost proportional to the view (decided 2026-09-29)],
)

*Where the checker asks for it.* Every place a program would give a shared object a second type is a
type error whose message suggests `deepCopy`:

#table(
  columns: (1fr, 1fr),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*Rejected*], [*Accepted*]),
  [`@j tryAs {str: int | str}` with `j : {age: int}` stored], [`@j deepCopy tryAs {str: int | str}`],
  [`@xs as [int | str]` with `xs : [int]`], [`@xs deepCopy as [int | str]`],
  [`@g "x" (str) updateCol`, type-changing, `g` stored (P6)], [`@g deepCopy "x" (str) updateCol`],
  [`@c *` with `c` stored (P7)], [`@c deepCopy *`],
  [`@xs 1 take as [[int | str]]` with `xs : [[int]]`], [`@xs 1 take deepCopy as [[int | str]]`],
)

The fresh cases need nothing: `[cmd]*!`, `readCsv ... updateCol`, `parseJson tryAs T ?`, `[1 2] as [int | str]`.

*A possible later refinement.* A third slot mark, "top-fresh" (the outer object is unshared, its
children are shared, and a retype may use only $<=$ on the children), would make a shallow copy useful
and would also recover `{a: @xs} as {a: [int], b?: int}`, which @sec-fresh rejects. It does not change
`deepCopy`, so it can wait.

== Operations that return lists <sec-new-lists>

Principle 7 says no two lists share storage. Reference semantics stay: a list is still one mutable
object that many names can reach. What is ruled out is two _different_ list objects over the same elements.

*Why.* The mechanized model assumes each heap object owns its contents. A Go slice is a header
(pointer, length, capacity), so two `MShellList` structs can point into one backing array. When they do,
writes that change an element (`setAt`) are seen through both, writes that reallocate (`append` past the
capacity) are not, and writes that shift elements in place (`del`) corrupt the other list. Each of those
keeps the element type, so no type is broken, but the list operations stop meaning what they say, and
the proof no longer describes the program. The only type-level alternative is a read-only view type
(below). So every operation that returns a list returns either its input or a new list with its own storage.

#table(
  columns: (auto, 1fr, auto),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*Operation*], [*Result*], [*Fresh when*]),
  [`append`, `setAt`, `del`, `insert`, `pop`], [the same list, changed in place], [the input was fresh],
  [`take`, `skip`, `:n`, `n:`, `a:b`], [a new list; elements shared (a shallow copy)], [input fresh or elements immutable],
  [`[a ...rest b]`], [`rest` is a new list, like `skip` (runtime change needed)], [input fresh or elements immutable],
  [pipe slices], [a new list of the pipe's commands (runtime change needed)], [input fresh or elements immutable],
  [`reverse`, `sort`, `filter`], [a new list over the input's elements], [input fresh or elements immutable],
  [`map`], [a new list of the quote's results], [result elements immutable (`tw_map_imm`)],
  [`deepCopy`], [a new tree], [always],
)

*Performance.* A shallow copy is one allocation and a copy of 16-byte interface values: no strings,
dicts or inner lists are copied, only pointers to them. For the list sizes scripts use, that is small
next to the cost of interpreting each word. Copies cost real time only when one large list is sliced
repeatedly: head/rest recursion over a long list, or a batching loop of `take` and `skip`. Both are
$O(n^2)$ with copying (the batching loop already is today). The fix for those is a builtin that walks
the list once, as `each`, `map` and `reduce` do; for example a `chunks` builtin copies each element once
in total.

*Why Haskell's head/rest is free.* A Haskell list is an immutable linked list: `(x:xs)` hands out a
pointer to the tail, and the sharing cannot be observed because nothing can modify either list. The
same holds for slicing immutable arrays (`Data.Vector`). Mutable arrays that slice in $O(1)$ (`MVector`,
Rust's `&[T]`) do share storage, and the type says so: a separate mutable-view type, or a read-only
borrow. If an $O(1)$ tail is ever needed in mshell, the answer is the same, a *read-only list view type*
(covariant, since nothing writes through it). That is purely an addition: nothing written against
new-list semantics would change meaning. It is not part of this design.

== Validation: `tryAs` and `is` <sec-tryas>

#rules(cols: 1,
  rule("TryAs",
    $upsilon "checkable"$,
    $Gamma;L;R tack.r #w("tryAs")_upsilon : eff(sigma space tau, sigma space ty("Maybe") upsilon)$),
)

A type is _checkable_ when it contains no quote types, no type variables and no abstract types;
enums are checked by identity and payload, grids by schema. A typed pattern
`is T x` is typed the same way and binds $x : T$ in its arm; `tryAs` is sugar for that match.

*The runtime rule* is the same for every operand: validate the value against $upsilon$ in place.
On success the result is `just` _the same value_; on mismatch it is `none`. `tryAs` never copies
(Principle 6). What depends on the operand is only whether the checker accepts it:

#table(
  columns: (auto, 1fr, auto),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*Operand*], [*Condition*], [*Result*]),
  [$fr(tau)$], [none], [$fr(ty("Maybe") upsilon)$, still fresh (`tw_try_dp`)],
  [$tau$], [$tau <= upsilon$], [$ty("Maybe") upsilon$ (`tw_try_sub`)],
  [$tau$], [$upsilon$ immutable], [$ty("Maybe") upsilon$ (`tw_try_imm`)],
  [$tau$], [otherwise], [*type error*: suggest `deepCopy` first],
)

The last row is what closes R6. There `j` is a stored `Json`, and `Json` is below neither
`{age: float}` nor `{str: float | str}`, so both refinements are rejected. Either validate before
storing (`parseJson tryAs {age: float} ? p!`, fresh, in place) or copy each refinement
(`@j deepCopy tryAs {str: float | str} ? d!`). Then `p` and `d` are two different dicts, and writing
`"x"` through `d` cannot affect `p`.
`Examples.v` checks the core form of R6, where the stored type is `{age: int}` exactly: the first
refinement, to the view `{age: int | open}`, is a subtype and is accepted in place (`r6_p_in_place_ok`);
the second is not. Without a copy the program gets stuck (`r6_stuck`) and so has no typing in any
context (`r6_rejected`); with `deepCopy` before the second `tryAs` it type-checks (`r6_copy_typed`) and
runs (`r6_copy_runs`).

So `parseJson tryAs Manifest ?` validates in place with no copy, and a program that validates a
stored container at a new type makes its copy visibly (Principle 6).

*In place on a fresh operand needs a tree.* This is why freshness is deep (@sec-fresh), and why a
builtin that marks its output fresh (`parseJson`) must return a tree. JSON is a tree.

*"Checkable" is partly a soundness rule (corrected).* Validation against a quote type simply fails (it
returns `none`), and validation against an unknown type succeeds without looking. Both are sound, and
that part of the rule is for good error messages. A *type variable* is different: types are erased, so
at runtime `tryAs a` has nothing to validate against, and treating it as "succeeds" would be unsound at
some instance. The typing rules require targets with no type variables (`fvt u = []`), which the
substitution lemma needs.

== Enums

For `enum E[a_1 ... a_k] = C_1 t_1 | ... | C_n t_n end` (with $k = 0$ for an ordinary enum), each
constructor is polymorphic in the parameters, like a def with a generic signature, and a match
substitutes the matched type's arguments into the payload types:

#rules(
  rule("Ctor",
    $Gamma;L;R tack.r C_i : eff(sigma space vec(tau)_i, sigma space E)$),
  rule("Match-Enum",
    $forall i. space Gamma;L;R tack.r e_i : eff(sigma space vec(tau)_i, sigma')$,
    $Gamma;L;R tack.r #w("match"){C_i -> e_i} : eff(sigma space E, sigma')$),
)

Unions are eliminated by `match` arms. A kind pattern (`int n`, `list xs`) tests the runtime kind and
binds the one member of that kind at its own type, writable and without a copy (@sec-unions); on a
value whose contents are not known statically it binds an abstract type (@sec-unknown).
An `is T x` arm validates by @sec-tryas.
A surface `match` without full coverage elaborates to one with a failing last arm, a checked error.
A constructor whose payloads do not mention a parameter leaves it open: it is $bot$ if that parameter is
covariant (so `none : Maybe[⊥]`, and `Maybe[⊥]` $<=$ `Maybe[T]` for every `T`), and a fresh type variable
otherwise, like an empty list literal.

*Freshness.* A constructed value is fresh when every payload is fresh or immutable (as for a literal,
@sec-fresh), and otherwise shared (`tw_con_dp`, `tw_con_sh`). A match on a fresh value pushes its
payloads as fresh values, since they own disjoint parts of its tree; on a shared value, as shared ones
(`tw_case`). A constructor with no arm is a checked error.
All instances of one enum are one runtime kind, so a union may not hold two of them (`Box[int] | Box[str]`).
The surface syntax of enums, their constructors and how their values print and compare are in @sec-surface.

#pagebreak()

= What is primitive? Dicts, shapes and grids <sec-primitive>

== Lists and dicts are just type constructors

- $ty("List") tau$ is a mutable reference to a finite sequence: $ty("Ref")(ty("Seq") tau)$.
- $ty("Dict") tau$ is a mutable reference to a finite partial function: $ty("Ref")("str" harpoon.rt tau)$.

Neither needs anything from the core except a heap-object kind and entries in $Phi$.
The one core fact about them: they are *invariant*, because they are references.

== Shapes are products; dicts are functions

A shape ${ell_1 : tau_1, ..., ell_n : tau_n | "exact"}$ is a *labeled product* $tau_1 times dots.c times tau_n$.
A dict is a *function space* $"str" harpoon.rt tau$. They share a runtime representation, but they are
different mathematical objects:

- product $->$ function exists only when all $tau_i$ agree, and only as a new value (`deepCopy`, or a fresh literal).
- function $->$ product is _partial_: keys may be missing or values of the wrong type. That is `tryAs`.

Width subtyping (@sec-sub) is the part of the product view that survives aliasing: a view may forget
fields, but may never claim a field type, a key's absence, or a remainder type that some other view
could contradict by writing.

== Grids are shapes of columns

$
  ty("Grid"){ell_1 : tau_1, ..., ell_n : tau_n}
    & tilde.equiv ty("Ref")({ell_1 : ty("Seq") tau_1, ..., ell_n : ty("Seq") tau_n | "exact"}) & "(column store)" \
  ty("GridRow"){C} & tilde.equiv ty("Grid"){C} times "int" & "(a cursor; reading it yields a shape)" \
  ty("GridView"){C} & tilde.equiv ty("Grid"){C} times ty("Seq") "int" & "(a selection that sees later writes)"
$

The equal-column-length invariant is a runtime invariant, not a typing concern.

- `updateCol` in place must preserve the column type. A type-changing update on a *fresh* grid
  (straight from `readCsv`, say) happens in place; on a shared grid it is a type error, and the
  program writes `deepCopy` first (P6).
- `gridAddCol`, `gridRemoveCol`, `gridRenameCol` and a type-changing `gridSetCell` follow the same rule: in place on a fresh grid, a type error otherwise.
- `join` needs both schemas known at the join site; the result schema is their disjoint union.
- `pivot` and schema-less readers produce $ty("Grid"){k}$ with an abstract schema (@sec-unknown),
  narrowed with `tryAs`. An abstract schema never matches a concrete one, which closes today's
  "unknown grid schema matches anything".

== Commands

A command is a list plus its redirect state, and the redirect state is part of its type
(the checker already tracks it). A redirect word changes that state, so it is a type change:

- On a *fresh* list (`[mycmd arg arg]*!`, `[cmd] 2>&1 *`, ``[cmd] `f` >``) it updates in place,
  with no allocation. This is essentially every redirect in real scripts.
- On a list that may be aliased (`@c *`, `dup *`) it is a type error; write `@c deepCopy *` (P7).
  The arguments are strings, so this copy is one allocation and a copy of the argument pointers.

== The whole picture

#table(
  columns: (auto, 1fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*Surface concept*], [*Core*]),
  [`int`, `str`, `path`, ...], [base types],
  [`[T]`, `{str: T}`], [invariant reference types + $Phi$ entries],
  [shape `{a: T, b?: U, *: V}`], [shape with fields and a remainder; width subtyping per @sec-sub],
  [`Grid`, `GridView`, `GridRow`], [shapes of columns; abstract schema when unknown],
  [`Maybe[T]`], [the built-in generic enum `Maybe[a]`, covariant],
  [`enum E[a] = ...`], [nominal; each parameter's variance follows from where it appears],
  [`T | U`], [union of distinct kinds; eliminated by a kind test],
  [`type X = T`], [transparent alias; guarded recursion allowed],
  [`enum`], [the only nominal form],
  [`Json`], [built-in recursive alias; `parseJson` returns it, fresh],
  [`as T`], [static ascription; retypes only fresh literals],
  [`tryAs T`, `is T x`], [validation, always in place; a shared operand not already below `T` is a type error],
  [`deepCopy`], [the only way to make a shared value fresh; always copies, deep, per path],
  [slices, `...rest`], [new lists with their own storage (@sec-new-lists)],
  [commands], [redirect state in the type; in place when fresh, a type error otherwise],
)

#pagebreak()

= Operational semantics (sketch)

A configuration is $chevron.l H; S; e chevron.r$: a heap $H$ from locations to objects,
a value stack $S$, and a program $e$.

$
  "values" v & ::= c | ell | "clo"(e) | C space vec(v) \
  "heap objects" o & ::= "list"[vec(v)] | "dict"{s |-> v} | "cell"(v) | "grid"(dots) | "cmd"(dots)
$

Shapes are dict objects at runtime. Variables are resolved to cell locations when a scope
is entered, so `@x` becomes $#w("load") ell_x$ and `x!` becomes $#w("store") ell_x$.

$
  chevron.l H; S; c space e chevron.r & --> chevron.l H; S space c; e chevron.r \
  chevron.l H; S; (e_1) space e chevron.r & --> chevron.l H; S space "clo"(e_1); e chevron.r \
  chevron.l H; S space "clo"(e_1); #w("x") space e chevron.r & --> chevron.l H; S; e_1 space e chevron.r \
  chevron.l H; S space ell; #w("get")_a space e chevron.r & --> chevron.l H; S space H(ell) "." a; e chevron.r \
  chevron.l H; S space ell space v; #w("set")_a space e chevron.r & --> chevron.l H[ell "." a |-> v]; S space ell; e chevron.r \
  chevron.l H; S space v; #w("tryAs")_upsilon space e chevron.r & --> chevron.l H'; S space "just"(v'); e chevron.r quad "if" "valid"(H, v, upsilon) \
  chevron.l H; S space v; #w("tryAs")_upsilon space e chevron.r & --> chevron.l H; S space "none"; e chevron.r quad "otherwise"
$

In the *TryAs* step, $v' = v$ and $H' = H$ always: validation reads the heap and changes nothing.
Validation of a cyclic value against a recursive type stops with a checked error (@sec-surface);
a DAG is fine.

$
  chevron.l H; S space v; #w("deepCopy") space e chevron.r & --> chevron.l H'; S space v'; e chevron.r quad "where" (H', v') = "copy"(H, v)
$

$"copy"$ allocates a new location for every list and dict reachable from $v$, once per path, and shares
everything else. It stops with a checked error on a cyclic value.

Builtins that take quotes and run them on a *child stack* run the closure in a nested configuration
$chevron.l H; v; e_q chevron.r$ whose stack holds only the element.

= Soundness <sec-sound>

== Definitions

A *store typing* $Sigma$ gives every shared location the type it was created at, and every variable
scope its context $Gamma$.

- *Shared values* are typed through $Sigma$: a location has type $upsilon$ when $Sigma(ell) <= upsilon$.
  A shared object is therefore only ever seen through supertypes of its one declared type.
- *Fresh values* are typed _deeply_, by reading the heap directly: a fresh list has type
  $ty("List") tau$ when its object is a list whose elements have type $tau$, and so on. This typing also
  records the value's tree of locations (its _region_). $Sigma$ is ignored on a region. That is why
  *Retype* is free at runtime: it changes the deep type and touches nothing else.
- *The invariant* on a configuration: the regions of the fresh slots are disjoint; no shared slot and
  no object outside the regions points into a region; every object outside the regions has its
  $Sigma$ type; and the current scope has type $Gamma$ in $Sigma$.
- *Commit.* When a fresh slot is forgotten, dropped, stored or passed on, each location in its region
  gets the one type its tree position gives it. Only the region's entries in $Sigma$ change. The
  region is a tree, so no location is assigned two types. This is the formal content of Principle 3.

== The builtin contract <sec-contract>

#defn("Builtin contract")[
  For each $p : forall vec(alpha). qt(vec(tau)_1, vec(tau)_2) in Phi$, every $Sigma$, $H$, $theta$,
  and values $vec(v)$ with $Sigma tack.r H$ and $Sigma tack.r vec(v) : theta vec(tau)_1$,
  running $p$ either raises a checked error, or yields $H'$, $vec(v)'$ and $Sigma'$ with
  $Sigma' tack.r H'$, $Sigma' tack.r vec(v)' : theta vec(tau)_2$, and
  $Sigma'(ell) = Sigma(ell)$ for every $ell$ reachable from anything other than a fresh input.
  An output marked fresh is reachable only from that output.
]

The last clause is the important one: a builtin may allocate and may retype an object it was
handed fresh, but may not change the type of anything else. In-place `updateCol` on a shared grid (P6)
and in-place redirects on a shared list (P7) violate exactly this clause.
A free type variable in an output position (P10) violates the second: no value has every type.

== Theorems

#lemma("Frame")[
  If $Gamma; dot; dot tack.r e : eff(sigma_1, sigma_2)$ then
  $Gamma; dot; dot tack.r e : eff(sigma_0 space sigma_1, sigma_0 space sigma_2)$ for every $sigma_0$,
  and at runtime $e$ never reads or writes the part of the stack below $sigma_1$.
]

This is what makes quote types implicitly rest-polymorphic. It holds provided no builtin computes
with the whole stack. mshell has none: the only one that looks is `stack`, which prints and
feeds nothing back (@sec-questions). Keep it that way.

#lemma("Canonical forms")[
  If $Sigma tack.r v : ty("List") tau$ then $v$ is a location with $Sigma(v) = ty("List") tau$;
  likewise $ty("Dict")$ and $ty("Grid")$ (equality: they are invariant).
  If $Sigma tack.r v : {F | rho}$ then $v$ is a location with $Sigma(v) <= {F | rho}$.
  If $Sigma tack.r v : tau_1 | tau_2$ then $Sigma tack.r v : tau_i$ for some $i$ that has a member of $v$'s runtime kind
  (exactly one when kinds are distinct).
  An abstract $k$ has no values of its own. The arm that introduces it is checked for every $k$, and at
  runtime it is instantiated with the element type the matched list actually has (its $Sigma$ type or
  its deep type).
]

#lemma("Views agree on writes")[
  If $Sigma(ell) <= {F | rho}$ and a write through the view ${F | rho}$ is allowed (@sec-sub), then the
  same write is allowed by $Sigma(ell)$, and every other view $upsilon$ with $Sigma(ell) <= upsilon$ remains valid.
]

This lemma is the table in @sec-sub read as a proof: clause S1 gives it for declared fields, S2 for
optional ones, S3 and S4 for remainders, and the absence of shape deletion for everything else.

#theorem("Soundness (mechanized: `soundness` in `formal-ver/Soundness.v`)")[
  If every definition body checks against every instance of its signature, and a program checks
  as $dot; dot; dot tack.r e : eff(epsilon, sigma)$, then for every amount of fuel $n$, running $e$ from an empty stack
  and an empty scope never produces a runtime type error. It finishes, runs out of fuel, exits,
  or stops with a checked error.
]

The proof is a single induction on fuel and on the typing derivation, in the "definitional
interpreter" style (Amin and Rompf 2017). It combines progress and preservation. The interpreter
`eval` returns `RStuck` exactly where the Go runtime reports a type mismatch, and the theorem says
it never does. Fuel makes the statement cover every finite prefix of a non-terminating run. The
inductive statement also says that `break`, `continue` and `return` carry stacks of the types their
contexts promise, that the invariant is preserved, and that scopes keep their types. It is
generalized over extra values held by callers (the stack below a child stack, the rest of an `each`
list), so those stay typed and their regions untouched.

The lemmas above correspond to these parts of the development:

- *Views agree on writes* is per-label subtyping (`fsub`, @sec-per-label) with transitivity
  (`sub_trans`). A write through a view is checked against the object's own status at that label.
- *Canonical forms* are the `vt_*_inv`, `dt_*_inv` and `Kind.v` lemmas. The proof does not use
  "exactly one union member"; see @sec-unions.
- *Retype and commit*: `dtyped_rsub` (retyping a fresh value needs no store change) and
  `commit_all` (Commit.v).
- *TryAs*: `validate_dtyped` (in place, fresh), `validate_imm` (immutable target); the subtype case
  needs no lemma of its own.
- *DeepCopy*: `dcopy_fresh` (`Copy.v`: the copy is a fresh value of the same type whose region is
  exactly the new locations) and `inv_alloc_region` (`InvOps.v`: adding that region keeps the invariant).
- *Frame*: built into quote types (@sec-break). The runtime never reads below a quote's inputs
  because the body checks with every rest of the stack.

What the mechanization does *not* cover is listed in @sec-mech.

== Where each counterexample breaks the proof

#table(
  columns: (auto, 1fr, 1fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*\#*], [*Current feature*], [*What stops it in the core*]),
  [P1], [shape field types covariant], [S1: field types are invariant],
  [P2], [open shape $->$ dict], [a shape is never a ${"str": tau}$; `deepCopy as` or `tryAs` instead],
  [P3], [dict $->$ required shape], [a dict fits only an all-optional shape; `tryAs` instead],
  [P4], [shape $->$ dict, then `set`], [same as P2],
  [P6], [in-place retyping of a shared grid], [builtin contract: only fresh inputs may be retyped],
  [P7], [in-place redirect on a shared list], [builtin contract: only fresh inputs may be retyped],
  [P10], [`parseJson` returns a free variable], [builtin contract: $Phi$ says `Json`],
  [P13], [`as` without evidence], [*As* needs $tau <= upsilon$; `Json` $lt.eq.not$ `{a: int}`],
  [R6], [two refinements share one mutable object], [*TryAs* on a shared operand needs $tau <= upsilon$; otherwise `deepCopy` first (`r6_*` in `Examples.v`)],
  [H1], [runtime-key `get` typed by the remainder (previous draft)], [*Get-Key* needs a type above every label (@sec-dyn-key)],
  [H2], [abstract type $k$ reused across runs of its arm (previous draft)], [the arm is checked for every $k$; skolem escape check (@sec-unknown)],
  [H3], [every shape literal fresh (previous draft)], [a literal is fresh only around fresh or immutable contents (@sec-fresh)],
  [H4], [every enum immutable (previous draft)], [an enum is immutable only when its payloads are (`en_imm`, `hole_box_stuck`)],
  [H5], [fresh values retyped covariantly everywhere, enum arguments under quotes included], [fresh-covariance: only data positions (`occ_fresh`, `hole_quote_arg_stuck`)],
  [H6], [joins widen inside two fresh quotes (previous draft)], [quotes join by $<=$ only (`hole_quote_join_stuck`)],
  [H7], [a type variable counted as immutable], [type variables are not immutable (`hole_tvar_imm_stuck`)],
  [H8], [a kind pattern on a type variable read as "no member"], [a type variable is treated as unknown contents (`hole_tvar_kind_stuck`)],
  [H9], [renaming a variable stored at a new type inside a loop body, or read after a branch], [no renaming: one type per variable per scope (`rename_*`)],
)

#pagebreak()

= Inference <sec-infer>

Defs are annotated, so inference is local to a def body or the top-level script.

== Stack-effect composition

An effect is a pair (inputs, outputs) of fixed sequences plus a "diverges" flag.
Composing $eff(vec(a), vec(b))$ then $eff(vec(c), vec(d))$:

- if $|vec(b)| >= |vec(c)|$: match the top $|vec(c)|$ of $vec(b)$ against $vec(c)$; result $eff(vec(a), vec(b)' vec(d))$;
- otherwise: match $vec(b)$ against the top $|vec(b)|$ of $vec(c)$; result $eff(vec(c)' vec(a), vec(d))$.

"Match" is equality unification for positions that mention unsolved variables, and $<=$ at checking
positions (@sec-sub). A diverging effect absorbs what follows. `if` pads both arms to the same input
length and joins their outputs; `loop{e}` requires the body to preserve its stack.

== Unification and subtyping, kept apart

- *Unification* is first-order with an occurs check, over the type formers above, with the
  assumption-set rule for recursive aliases (@sec-alias). It never enters a union or a width step.
- *Subtyping* is the relation of @sec-sub, checked only when both sides have no unsolved variables.
- When a check needs both at once (a union or width step against a type with unsolved variables),
  it is an error asking for an annotation, not a search.

The result: the answer does not depend on the order constraints are visited, and
`inputUnifyOrder`, first-arm union commitment and rollback-driven overload trials are unnecessary
inside the core. (Overload resolution happens in elaboration, below.)

= Elaboration from surface mshell <sec-elab>

#table(
  columns: (auto, 1fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*Surface*], [*Elaboration*]),
  [`5 f`, where `f` takes `int | str`], [checking position: `int` $<=$ `int | str`],
  [`[1 "a"] as [int | str]`], [*Retype* of a fresh literal],
  [`[1 2] l! @l as [int | str]`], [*rejected*: not fresh, and lists are invariant],
  [`{url: "x"} f` where `f` takes `{url: str, timeout?: int}`], [the fresh literal is typed at the parameter],
  [`resp "body" get` on a shape], [$#w("get")_#raw("body")$ (syntactic: literal key directly before `get`)],
  [`c (a) (b) iff` with literal quotes], [`c if a else b end`],
  [`(body) loop`, `(body) each`, ...], [`loop{body}`, `each{body}`, ... (@sec-break)],
  [`value tryAs T`], [`value match is T x : @x just, _ : none, end` with a hygienic `x`],
  [`value => pattern`], [assertive single-arm match (@sec-surface)],
  [overloaded `+`], [choose the candidate from the known argument types; ambiguous at the end of the def is an error],
  [overloaded op on a union operand], [a `match` with one arm per member (today's "distribution", made explicit)],
  [`x!` reassigned at a new type], [an error: one type per variable per scope (see below)],
)

*Renaming must not change behavior (corrected).* The core checks the elaborated program, but the
runtime runs the original, so an elaboration that changes what a program does voids the proof. Renaming
a variable does, in two cases the previous text allowed:

- *A loop body.* `1 x!  [0 0] (drop @x 1 + drop "a" x!) each`: the body is a literal quote that the
  elaboration turns into `each{...}`, so it is not a quote that captures `x`, and the renamed program
  (`x1` before, `x2` at the end) checks. The second run reads `"a"` as an `int`. The current checker on
  `main` accepts this program too, and it fails at runtime (a new counterexample, P14).
- *A read after a branch* whose arms stored different types: `false if 1 x! else "a" x! end @x 1 +`.

`Examples.v` runs both: each renamed program type-checks (so it never gets stuck) while the original gets
stuck (`rename_loop_*`, `rename_if_*`). Renaming is sound only when every read of the variable can see
stores of one type, which needs a flow analysis the proof does not cover.

*Decided (2026-09-29): no renaming.* A variable has one type per scope: the type of its first store in
program order. Every later store is checked against it at a checking position (subtyping, or retyping of
a fresh value). A mismatch is reported at that store, with the hint "use a new name, or widen the first
store with `as`". Widening works for base types, unions and `Maybe`, and for fresh containers; a stored
container cannot be widened (use `deepCopy`). Every read then sees the wide type. Reusing a name at a new
type is expected to be rare.

#pagebreak()

= Surface language <sec-surface>

Decisions about the language users write, outside the core rules. All decided 2026-09-29 unless
marked open. Open questions live only in `ai/type-system-plan.md`.

== Enums

- *Declaration:* `enum Name = member Payload ... | ... end`. The leading `|` is optional and `end` is
  required. Payloads are type primaries separated by spaces; a union payload is named with a `type`
  alias first.

  ```
  enum CmdResult = ok str | failed int str | timeout end
  enum Tree = leaf int | node Tree Tree end
  ```
- *Constructors* are ordinary postfix words: `404 "not found" failed` has type
  `(int str -- CmdResult)`. Member names are global.
- *Patterns:* `failed code msg :` binds the payloads at their declared types; `_` skips one.
- *Values:* `str` gives `member` or `member(p0 p1)`. `toJson` is externally tagged: `"member"`,
  `{"member": v}`, `{"member": [v0, v1]}`. Equality compares enum name, member, then payloads.
  Ordering compares enum name, member declaration order, then payloads.
- *Generic enums:* `enum Box[a] = box [a] | empty end`. Parameters are written in brackets after
  the name, as in `Maybe[T]`, and used in payload types. A recursive reference must use the same
  parameters in the same order (`enum List[a] = cons a List[a] | nil end` is accepted,
  `List[[a]]` inside `List[a]` is not). This is a usability and implementation rule, not a soundness
  rule: the proof accepts `enum Nest[a] = nest a Nest[[a]] | stop end` (`nest_wf`). It keeps type
  printing, validation memoization and error messages simple, and can be relaxed later.
  Variance, fresh-covariance and nullary constructors are in @sec-sub and the Enums rules.
- *`Maybe`* is the built-in declaration `enum Maybe[a] = just a | none end`. Its runtime values keep
  their current printing and JSON (`Just(5)`, `None`; `5`, `null`), so existing output does not change.

== Names

Nothing shadows anything. A constructor, type name, definition or builtin that collides with another
name is an error. A duplicate declaration or duplicate `def` is an error, including in interactive sessions.

== Type expressions

- One type parser for `type`, `is`, `tryAs`, `as` and `def` signatures, and one resolved form of each
  type, used by both the checker and the runtime validator. No second spelling or second
  implementation of the same type.
- There is no syntax for an exact shape type. Shape literals have exact types; a written shape type
  is `open` or has a `*: T` remainder.

== Patterns and validation

- `value tryAs T` is sugar for `value match is T x : @x just, _ : none, end` with a hidden `x`.
  A value that does not conform gives a bare `none`, with no diagnostic.
- `is T x` is the typed pattern. `is` has this meaning only at the head of a match arm, so it is not a
  reserved word. The binding is required and may be `_`.
- For exhaustiveness, an `is T` arm covers a union member only when that member is equivalent to `T`.
  Anything else needs a `_` arm.
- `value => pattern` is an assertive single-arm match: its bindings are available after it in the
  current scope, and a mismatch stops the program.
- Validation has a work budget and detects cycles. Exceeding the budget or meeting a cycle is an error
  that stops the program, never a quiet `none`. `match is` and `tryAs` share the same budget and rules.

== Checking by default

Every rule here is judged as if `--check-types` is the default for all user code. The REPL starts
checking each line only once the new checker is working and battle tested; the checker's state is
built to persist across lines from the start.

= What changes for users

Most of these are already runtime failures today; a few are real losses.

#table(
  columns: (1fr, 1fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*Change*], [*Cost / mitigation*]),
  [union members must be of distinct kinds: no `[int] | [str]`, no union of two shapes],
    [an enum; `Json`, `int | str | null` and similar unions are unaffected. Not required for soundness (@sec-unions), so it can be relaxed later],
  [`getd` with a runtime key on a shape gives `Maybe` of a type above _every_ field, not just the remainder],
    [use literal keys for declared fields; `{str: T}` is unaffected],
  [a literal around a stored value (`{a: @xs}`) is not fresh],
    [literals of fresh or immutable values (the common case) are unaffected],
  [a value bound by `list xs` on unknown data cannot leave the arm (no storing it in an outer variable or list)],
    [narrow with `tryAs`/`is` first, which gives a real type that can leave the arm],
  [`if`/`match` arms leaving *stored* containers of the same kind but different types have no join],
    [literal arms join automatically (@sec-join); for stored values use an enum or build a new value],
  [`if`/`match` arms leaving quotes join only by subtyping: `(1 +)` and `("a" ++)` have no useful join],
    [annotate the quote type both arms satisfy, or use an enum],
  [a def that never returns must say so: `(str -- never)`],
    [today such a def cannot be used in a branch at all; this makes it work],
  [`parseJson` returns `Json`; raw `Json` has no operations],
    [`parseJson tryAs T ?` once at the boundary, validated in place],
  [`as` needs evidence],
    [unchanged for literals; use `tryAs` for data from outside],
  [`tryAs`/`is` on a stored container, to a type its static type is not below, is a type error],
    [validate before storing (`parseJson tryAs T ?`, in place), or `deepCopy` first],
  [a shape never converts to `{str: T}`; a `{str: T}` fits only a shape whose fields are all optional],
    [`tryAs`, or write the value as a literal],
  [a value missing an optional key does not satisfy `timeout?: int` unless it is fresh],
    [option dicts written as literals at the call site are fresh],
  [no deleting keys from a shape (`del` is for `Dict`)],
    [use a `Dict` for dynamic keys],
  [type-changing `updateCol` (and `gridAddCol`, ...) on a shared grid is a type error],
    [fresh grids, e.g. straight from `readCsv`, are updated in place; otherwise `deepCopy` first],
  [a redirect on a list that may be aliased (`@c *`) is a type error],
    [`[cmd]*!` and every redirect straight after a literal: unchanged, in place, no allocation; otherwise `@c deepCopy *`],
  [`...rest` is a new list: `setAt` on `rest` no longer changes the matched list],
    [$O(n)$ per match, so head/rest recursion over a long list is $O(n^2)$; use `each`, `map`, `reduce`],
  [a slice of a pipe is a new list: changing it no longer changes the pipe],
    [none; this was an aliasing bug],
  [`break`/`continue` only inside a literal quote at the `loop`/`each`/`map`/... site],
    [the tested pattern still works; a stored quote that breaks does not],
  [unknown grid schemas do not match concrete ones],
    [`tryAs` to the declared schema once],
  [`x` on a quote of unknown arity is an error],
    [annotate; def parameters already are],
)

What we get back: `readOnlyArgs`, `mutatingBuiltins`, `TKStrLit`, `TKOverloadedQuote`,
multi-signature quote inference, union distribution inside `unify`, `inputUnifyOrder` and erased
brands can all be deleted, and the runtime type checks can go once the oracle agrees.

== Mapping from current `TypeKind`s

#table(
  columns: (auto, 1fr),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*Current*], [*Core*]),
  [`TKPrim`], [base type],
  [`TKMaybe`], [the built-in generic enum `Maybe` (covariant)],
  [`TKList`, `TKDict`], [$ty("List")$, $ty("Dict")$ (invariant)],
  [`TKShape`], [shape with fields and remainder],
  [`TKQuote`], [quote type $qt(vec(tau), vec(tau))$],
  [`TKOverloadedQuote`], [gone: resolved during elaboration, or an error],
  [`TKUnion`], [union of distinct kinds],
  [`TKBrand`], [gone: `enum`],
  [`TKCommand`], [command with redirect state; type-changing redirects only on a fresh command],
  [`TKVar`, `TKRigid`], [unification variable, rigid variable; plus abstract types $k$],
  [`TKGrid`, `TKGridView`, `TKGridRow`], [grid over a schema; abstract schema instead of "unknown matches anything"],
  [`TKStrLit`], [gone: literal-key `get` is syntax],
  [`TidBottom`, `Diverges`], [$bot$ as a value type (`none : Maybe[⊥]`); divergence as a property of effects (@sec-diverge)],
  [(new)], [alias reference nodes for guarded recursive `type`],
)

= The mechanized core <sec-mech>

`formal-ver/` holds a Rocq (9.1) development of the core. `make check` in that directory rebuilds
it and prints the assumptions of the main theorem: *none* (`Closed under the global context`). It is
about 8,000 lines. `formal-ver/README.md` maps every definition to the section of this document it
formalizes.

#table(
  columns: (auto, 1fr),
  inset: 5pt, stroke: 0.5pt + luma(180),
  table.header([*File*], [*Contents*]),
  [`Syntax.v`], [types (base, $bot$, unknown, `Maybe`, lists, per-label dict/shape types, unions, quotes with `never`), core words, values, heap objects],
  [`Interp.v`], [the interpreter: `RStuck` exactly where the Go runtime reports a type mismatch; validation; the per-path `deepCopy` (`dcopy`)],
  [`Subtyping.v`], [$<=$ (per label), its transitivity, fresh retyping $subset.sq.eq$],
  [`Typing.v`], [the typing judgment with freshness marks and break/continue/return contexts],
  [`Invariant.v`], [store typing, deep typing of fresh values, regions, the invariant],
  [`Commit.v`], [committing a fresh tree into the store typing],
  [`Validate.v`, `Kind.v`], [`tryAs` in place; kind patterns],
  [`Copy.v`], [`deepCopy` gives a fresh value of the same type],
  [`InvOps.v`, `RecOps.v`], [stack and heap operations on the invariant; type-changing updates of fresh records],
  [`Soundness.v`], [the theorem],
  [`Generic.v`], [type variables and substitution; `T_subst` (typing is closed under substitution); `soundness_generic` (each def checked once)],
  [`Frame.v`], [the frame lemma; divergence as a checker tracks it; dead code after a diverging word],
  [`Join.v`], [branch joins as a function, and the proof that they are upper bounds],
  [`Variance.v`], [the enum declaration checks are sound: substitution is monotone for variance (`payload_sub`) and for fresh retyping (`payload_rsub`), and preserves immutability (`payload_imm`)],
  [`Examples.v`], [holes H1--H8 run to `RStuck`; R6 gets stuck without a copy and type-checks and runs with `deepCopy`; the copy is per path; copying a cycle is a checked error; `Maybe`, recursive `List` and non-regular `Nest` declared as generic enums],
)

*What is modeled*: everything in the calculus that interacts with aliasing, namely shared and fresh lists and
dicts, shapes with required/optional/absent/remainder/open labels, `{str: T}`, width subtyping,
invariance, `Maybe` covariance, unions, unknown types and kind patterns (including the abstract-type
rule), type variables and polymorphic defs checked once, the frame lemma, divergence and dead code,
branch joins, `return` in top-level code, generic and recursive enums (variance, fresh-covariance, immutability, constructors, matches,
enum kind patterns, validation and `deepCopy` of enum values), validation with a work budget,
variables in heap scopes captured by quotes, quotes with frame polymorphism and `never`,
`if`, `loop` and loop-forever, `break`/`continue` through `each`, `return`, `exit`, polymorphic and
recursive definitions, `tryAs` in its three modes, `deepCopy`, and type-changing updates of fresh records.

*What is not modeled*, and what each would need:

- *Recursive aliases* (`Json`, `Person`). Types are finite trees in the model. Adding guarded recursion
  means coinductive (or alias-environment) types, validation with a cycle check, and the
  Amadio--Cardelli subtyping argument. Nothing in the proof depends on finiteness except the
  structural recursion in `validate`. (`deepCopy` does not look at types, so it is unaffected.)
- *`Maybe` as an instance of enums.* The model keeps `Maybe` as a built-in type and also shows the
  declaration `enum Maybe[a] = just a | none end` is well formed, covariant and fresh-covariant
  (`maybe_*` in `Examples.v`). The checker can implement `Maybe` as that enum.
- *Exhaustiveness.* A constructor with no arm is a checked error in the model; checking coverage
  statically is not a soundness question.
- *Grids, grid views, commands*. The model covers them through their core form: records of
  columns and strong updates on fresh records. The grid-specific builtins are $Phi$ entries.
- *Builtins generally* (Principle 5). The model proves the rules for the words listed above.
  Other builtins still need the contract in @sec-contract and tests.
- *The checker algorithm*. The theorem is about the declarative rules. The checker must produce only
  derivations of them. Proved: the substitution lemma (defs checked once), the frame lemma (quote
  bodies checked once), divergence, and joins. Not proved: unification, overload resolution and the
  skolem escape check (@sec-unknown) as algorithms.
- *"Fresh when the input is fresh"* for `take`, `skip`, slices and `...rest` over a list of containers
  (@sec-fresh). The old list stays in the heap, unreachable, pointing at the kept elements, and the
  invariant has no notion of unreachable objects. The shared case and the case of immutable elements
  need nothing new. Adding unreachable objects to the fresh-region invariant would cover it.
- *Definite assignment.* Reading an unset variable is a checked error in the model, as at runtime.
- *Slices and `...rest`.* The model has no slicing words. Principle 7 is what lets them be added as
  $Phi$ entries: each returns a new object, so a slice is a shallow copy with the freshness rule of @sec-fresh.
  A slice that shared storage with its source would need a model of backing arrays separate from objects.

= Getting confidence: the validation plan

The mechanized proof covers the core rules. It does not cover the Go code. Three things close that gap.

+ *Classify every runtime error.* Tag each error site in `Evaluator.go` as a _checked error_ or a
  _type mismatch_. Soundness is then testable: type-mismatch errors are unreachable in checked programs.
+ *Soundness oracle.* Generate random well-typed programs by running the typing rules backwards,
  run them, and fail on any type-mismatch error. Bias generation toward aliasing: `dup`, stores,
  refinements of stored values, writes through every view. Also run every file in `tests/success`.
  This would have found every row of the counterexample table.
+ *Per-builtin contract tests.* For each $Phi$ entry, generate inputs of the declared types and
  check output types, that shared inputs keep their types, and that outputs marked fresh are unaliased.

Every counterexample in this document becomes a `tests/typecheck_fail` case (for R6, also a success test
of the version with `deepCopy`), and `ai/type-system-plan.md` lists them in its acceptance tests.

= Runtime facts the rules depend on <sec-questions>

Each was checked against `Evaluator.go` on `main`, not only taken from the docs.

#table(
  columns: (0.8fr, 1.5fr, 1.5fr),
  inset: 6pt, stroke: 0.5pt + luma(180),
  table.header([*Question*], [*What the code does*], [*Consequence for the core*]),
  [Which stack does a quote run on?],
    [`x`, `iff`, `loop` run on the current stack. `each`, `filter`, `map` on a list and the grid
     functions use a fresh child stack. `map` on a Maybe, `map2`, `bind`, and `and`/`or` with a quote
     run on the current stack and only check the net stack length.],
    [No difference for a well-typed quote (frame lemma). It matters only for which stack a loop sees
     after a `break` (@sec-break).],
  [Builtins that observe the whole stack?],
    [None that compute with it. `stack` prints the whole stack to stderr.],
    [Frame lemma holds.],
  [Do redirects mutate in place?],
    [Yes: `*`, `>`, `2>&1`, ... set fields on the list object.],
    [In place when fresh, a type error when shared (P7).],
  [Do list slices share storage with the source?],
    [`take`, `skip` and the list index slices (`MShellList.Slice*`) allocate a new backing array.
     The `...rest` binding (`Evaluator.go:1676`) re-slices the source's array with its capacity capped, and
     pipe slices (`MShellPipe.Slice*`) re-slice with no cap. `setAt` on `rest` changes the source; `del`
     on `rest` shifts the source's elements (`[1 2 3 4]` becomes `[1, 3, 4, 4]`); `append` on a pipe slice
     overwrites the pipe's next command. All of these pass `--check-types` today.],
    [Principle 7: `...rest` and pipe slices must allocate, like `take` (a runtime change).
     `del` can stay in place once nothing shares storage.],
  [Variable scoping],
    [One scope per def invocation, none inherited; quotes capture their scope by reference and can
     add variables to it; closures outlive the def.],
    [$Gamma$ is per scope; cells are heap objects; definite assignment is a separate check.],
  [Enum runtime representation],
    [On the enum branch, values carry enum name, member and payload.],
    [Enum identity is checkable at runtime; payloads are validated by `is`/`tryAs`.],
  [Building shapes key by key with `set`],
    [Almost never done.],
    [Keys are added only through `*: T` remainders or fresh literals; no practical cost.],
  [Do `GridView`s see later mutations of the base grid?],
    [Yes: a view holds a pointer to the grid and a row index list.],
    [Sound, because a shared grid's column types never change in place.],
  [Is `break` lexically scoped?],
    [No. It leaves whatever `loop` is running, even from inside `each` or `map`.],
    [Kept for literal quotes at the call site; rejected in stored quotes.],
)

= References

- B. C. Pierce. _Types and Programming Languages_. MIT Press, 2002. Ch. 11 (records), 13 (references), 15 (subtyping), 21 (recursive types).
- A. K. Wright and M. Felleisen. A syntactic approach to type soundness. _Information and Computation_, 1994.
- A. K. Wright. Simple imperative polymorphism. _LISP and Symbolic Computation_, 1995. (The value restriction.)
- R. M. Amadio and L. Cardelli. Subtyping recursive types. _ACM TOPLAS_, 1993.
- D. Kozen, J. Palsberg and M. I. Schwartzbach. Efficient recursive subtyping. _Mathematical Structures in Computer Science_, 1995.
- J. C. Mitchell and G. D. Plotkin. Abstract types have existential type. _ACM TOPLAS_, 1988.
- F. Smith, D. Walker and G. Morrisett. Alias types. _ESOP_, 2000. (Changing the type of an unaliased location.)
- J. Dunfield and N. Krishnaswami. Bidirectional typing. _ACM Computing Surveys_, 2021.
- C. Diggins. Typing functional stack-based languages. 2008. (See also Kitten and Factor's stack-effect inference.)
- N. Amin and T. Rompf. Type soundness proofs with definitional interpreters. _POPL_, 2017. (The proof style of `formal-ver/`.)
