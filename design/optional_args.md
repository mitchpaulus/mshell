# Optional Arguments

## Problem Statement

Oftentimes, there is a definition or function that has a few key inputs, but many "defaults".
In that case, it is cumbersome and annoying to have to be explicit about many inputs that are trivial or only need to be changed in rare cases.
Python has something like keyword arguments, where a bunch optional named items can be collected into a dictionary.

As a stack based language, the number of items on the stack must be known, it cannot be variable.

## Potential Examples

```mshell
`myzip.zip` zipExtract  # Running without setting explicit optional dict
`myzip.zip` %{ 'overwrite': false } zipExtract

# Also, %{ is not a single token, it is split so you could do

{
  'myprop': 1
} defaults!

myinput % @defaults myFunctionWithDefaults
```

Usage within a definition:

```
def mydef (str %{ 'my_opt_prop': 10 } -- str)
  @opt :my_opt_prop 5 maybe # default to 5 for optional property
  # do things..
end
```

## Immediate Review Of The Examples

Two important observations from the current grammar:

- `#` is already the line comment token in the lexer, so `# { ... } zipExtract` cannot work without changing comment lexing
- your instinct that the optional dict must be parsed together with the callable is correct; ambient "set defaults now, consume them later" state is too implicit

That means the design should probably enforce a shape like:

```mshell
`myzip.zip` <optional-args-syntax> zipExtract
```

where the parser can bind the optional-args node directly to `zipExtract`, rather than storing some interpreter-global pending dictionary.

## Goals

Optional arguments should:

- keep stack arity predictable
- work for both built-ins and user definitions
- make the common call site short
- keep uncommon overrides possible and readable
- be visible to the parser, type checker, docs, and error messages

## Existing Constraints In mshell

Some current language details matter a lot here:

- Definitions only have positional type signatures today
- The parser does not currently support parameter names in signatures
- Documentation sometimes shows labels like `path:zipPath`, but that is documentation, not callable syntax
- Several built-ins already use an explicit trailing options dictionary, for example `numFmt`, `zipExtract`, and `zipExtractEntry`
- Dictionaries already have important colon ambiguity with indexers, for example `{ "k":1 }` is bad because `:1` lexes as an indexer
- Dictionary values are expressions, but they must evaluate to exactly one object
- Metadata dicts on definitions are static, so they are a plausible place to store defaults, but not dynamic expressions

Because of that, the cleanest mental model is:

> optional arguments should probably desugar to a normal trailing options dictionary

That preserves fixed stack arity.

The main design question is then what syntax, if any, should sugar that dictionary.

## Design Families

### 1. Keep Explicit Options Dict Only

Example:

```mshell
123 { 'decimals': 2 } numFmt
`archive.zip` `out/` { 'overwrite': true } zipExtract
```

Pros:

- no lexer changes
- no parser changes
- no special runtime state
- matches existing built-ins

Cons:

- not really a new optional-arguments feature
- user definitions cannot advertise optional keys in a first-class way
- docs and type checking still treat this mostly as an unstructured `dict`

This is the baseline to compare against.

### 2. Sugar For A Trailing Options Dict

Example idea:

```mshell
123 ?{ 'decimals': 2 } numFmt
`archive.zip` `out/` ?{ 'overwrite': true } zipExtract
```

Semantics:

- `?{ ... }` is not a normal dict pushed on the stack
- it is a special optional-argument bundle
- when the next callable runs, mshell merges the bundle with that callable's declared defaults
- internally this becomes the same as calling the function with an explicit trailing dict

Pros:

- preserves fixed arity
- call sites stay in normal postfix order
- built-ins and definitions can share one runtime model
- explicit dict calling style can still keep working as the canonical low-level form

Cons:

- needs new lexer and parser support
- introduces "pending optional args" state in evaluation unless desugared very early
- needs a clear answer for what "the next callable" means

This family is the closest match to the original idea of "a special dictionary set before execution".

### 3. Use A Keyword Instead Of A Sigil

Example idea:

```mshell
123 { 'decimals': 2 } withArgs numFmt
`archive.zip` `out/` { 'overwrite': true } withArgs zipExtract
```

Semantics are the same as family 2.

Pros:

- much smaller lexer change, maybe none
- easier to read than punctuation-only syntax
- avoids trying to find an unused sigil

Cons:

- more verbose
- feels less "part of the language" and more like a special built-in
- still has the same semantic questions about lifetime and consumption

This is probably the lowest-risk path if you want the feature soon.

### 4. Attach Named Arguments Directly To The Call

Example ideas:

```mshell
123 numFmt(decimals = 2)
123 numFmt[decimals = 2]
`archive.zip` `out/` zipExtract(overwrite = true)
```

Pros:

- names are obviously attached to the callee
- no ambient "pending options" state
- familiar to many users

Cons:

- much bigger grammar shift
- makes ordinary definition calls syntactically special
- less uniform with the rest of a concatenative language
- harder to generalize to higher-order values and quotations

I would treat this as the least mshell-like option.

## Recommendation

The best overall shape is:

1. keep the canonical runtime ABI as `required positional args + trailing dict`
2. make optional arguments first-class in definition/built-in metadata or signature syntax
3. optionally add a small call-site sugar that desugars to that trailing dict

That keeps the evaluator and type system grounded in a fixed-arity model.

The remaining question is whether the sugar should be a sigil form like `?{ ... }` or a keyword form like `withArgs`.

My bias:

- `withArgs` is easier to implement and easier to reason about
- a sigil form is nicer if this is meant to feel fundamental and common

One thing I would change from the earlier draft:

- do not model this as long-lived pending state in the evaluator
- do parse it as a call-site construct attached to the following named callable

In other words, the internal representation should look more like:

- `CallWithOptionalArgs(target=zipExtract, overrides=...)`

and less like:

- `PushPendingOptionalDict(...)`
- `LaterCallSomethingAndMaybeConsumeIt(...)`

## Candidate Declaration Syntaxes

Call-site sugar is only half the problem.

User definitions also need a way to declare:

- which optional keys are allowed
- the type of each key
- the default value for each key

Here are the main options.

### A. Store Optional Schema In Definition Metadata

Example:

```mshell
def copyFile {
    'optional': {
        'overwrite': { 'type': 'bool', 'default': false },
        'mode': { 'type': 'int', 'default': 420 }
    }
} (path path --)
    ...
end
```

Pros:

- no type-signature grammar change
- metadata is already static, which matches static defaults well

Cons:

- types become awkward strings or ad hoc metadata objects
- information is split between the signature and metadata
- built-ins and definitions would need a second schema format besides `TypeDefinition`

This is easy to prototype, but it is not especially elegant.

### B. Extend The Type Signature With An Optional Section

Example:

```mshell
def copyFile (path path | overwrite: bool = false mode: int = 420 -- )
    ...
end
```

This is only an example shape.

The exact separator could be something besides `|`.

Pros:

- optional arguments become first-class language syntax
- docs and type checking can use one source of truth
- much easier to surface in errors and completions

Cons:

- requires real parser work
- needs careful token choices
- current signatures do not support names at all, so this is a real extension, not a small tweak

If optional args are meant to be a lasting core feature, this is the better long-term direction.

### C. Treat The Final Dict Type As Special

Example:

```mshell
def copyFile (path path { 'overwrite': bool, 'mode': int } -- )
    ...
end
```

Then defaults could live in metadata or a second clause.

Pros:

- very close to the existing "options dict" model
- smaller conceptual jump

Cons:

- still does not say which keys are optional vs required
- still needs another place for defaults
- the type signature alone is incomplete

This is a decent internal representation, but not a full source-level design by itself.

## Concrete Syntax Candidates For The Call Site

If the language gets sugar, these are the realistic candidates.

### `#{ ... }`

```mshell
123 #{ 'decimals': 2 } numFmt
```

Pros:

- visually suggestive
- compact

Cons:

- not currently available, because `#` starts a line comment today
- would require reworking comment lexing rules
- likely not worth the grammar cost

So although this is aesthetically plausible, it is currently a poor fit for mshell as implemented.

### `?{ ... }`

```mshell
123 ?{ 'decimals': 2 } numFmt
```

Pros:

- visually reads as "optional"
- compact

Cons:

- `?` already has meaning in mshell
- requires lexer special-casing for `?{`

### `&{ ... }`

```mshell
123 &{ 'decimals': 2 } numFmt
```

Pros:

- compact
- `&{` does not currently mean anything

Cons:

- `&` already appears in other syntax, so this still needs careful review
- the meaning is not self-evident

### Keyword Form

```mshell
123 { 'decimals': 2 } withArgs numFmt
```

Pros:

- no sigil search problem
- very explicit

Cons:

- more verbose

### Postfix Binder Form

```mshell
123 { 'decimals': 2 } -> numFmt
```

or

```mshell
123 { 'decimals': 2 } @> numFmt
```

Pros:

- visually binds the dict to the next callable
- avoids the idea that the dict has an independent lifetime

Cons:

- requires finding a token that is actually available
- introduces more grammar surface than a keyword

## Lexer Decisions

These decisions have to be made before picking syntax.

### 1. Is The Feature Introduced By A New Sigil?

If yes, the lexer needs a dedicated token for something like:

- `#{`
- `?{`
- `&{`
- some other prefix immediately before `{`

If no, and the syntax is keyword-based, the lexer might not need to change at all.

Important current fact:

- `#` is already unconditionally lexed as `LINECOMMENT`

So `#` is not currently available for optional-argument syntax without a deliberate lexer redesign.

### 2. Can Option Names Be Bare Literals?

If the syntax uses `name: value`, bare names are attractive.

But colon already has heavy meaning in:

- dictionary literals
- getters
- indexers
- match arms

So any bare-name syntax needs to be checked carefully against current tokenization.

### 3. Do We Want `=` In Named-Argument Syntax?

This is subtle.

Today `=` is allowed inside literals so CLI flags like `--color=always` stay one token.

That means a token like `timeout=2` currently wants to lex as one literal, not `timeout`, `=`, `2`.

So if the design uses `name=value` without spaces, the lexer rules must change.

### 4. Should The Optional-Bundle Syntax Reuse Normal Dict Lexing?

If the syntax is prefix-plus-dict, the lexer can either:

- emit a new "optional dict start" token
- or emit the prefix token and normal `{`, leaving the parser to combine them

A single dedicated token is usually easier to parse.

### 5. Does The Syntax Need To Be Adjacent?

For example, should this be valid:

```mshell
?{ 'a': 1 } fn
```

but this invalid:

```mshell
? { 'a': 1 } fn
```

Requiring adjacency usually keeps lexing simpler and makes the feature easier to spot.

## Parser Decisions

### 1. Where Is The Optional Schema Declared?

Options:

- definition metadata
- an extended type signature
- a special final-dict declaration

This affects `MShellDefinition` and probably `TypeDefinition`.

### 2. Is Call-Site Sugar Desugared In The Parser?

That is preferable if possible.

If the parser can turn:

```mshell
123 ?{ 'decimals': 2 } numFmt
```

into a normal internal call shape early, later stages stay simpler.

If not, the evaluator needs special pending-option behavior.

Given your concern about accidental lifetime, parser desugaring now looks much better than evaluator-side pending state.

The parser could produce an AST node that already couples:

- the override dictionary
- the target callable token

That keeps the lifetime lexical and explicit.

### 3. How Are Built-Ins Described?

Built-ins currently have positional type definitions in `TypeChecking.go` and a separate built-in list.

Optional args likely need a new shared schema structure for:

- built-ins
- user definitions
- docs
- completions
- runtime validation

### 4. Do Quotations And Higher-Order Calls Support Optional Bundles?

Examples:

```mshell
?{ 'decimals': 2 } numFmt
.map ?{ 'overwrite': true } ...
```

You need a rule for whether optional bundles only target named definitions/built-ins or also quotations and prefix-quote calls.

My recommendation is to start with named built-ins and named definitions only.

### 5. Does The Parser Require Immediate Adjacency To The Callee?

This needs a firm answer.

Recommended rule:

- optional-argument syntax must be immediately followed by the callable it targets
- any other token in between is a parse error

That prevents the feature from silently becoming ambient state.

## Semantic Decisions

These are the biggest design choices.

### 1. What Is The Lifetime Of The Bundle?

The best answer is now:

- none, as an independent runtime concept

The bundle should not live on its own.
It should be part of the call syntax for one specific call site.

That avoids spooky action at a distance entirely.

### 2. Unknown Keys: Error Or Ignore?

This should be an error.

Silent ignore makes typos too dangerous.

### 3. Duplicate Keys: Error Or Last-Wins?

This should also probably be an error.

If both the bundle and explicit dict syntax are allowed, silent precedence rules will be confusing.

### 4. Are Defaults Static Or Dynamic?

Static defaults are much easier:

- parse-time visible
- doc-friendly
- type-checker friendly
- deterministic

Dynamic defaults would mean the default is really a hidden expression, which is much harder to reason about.

I would strongly prefer static defaults first.

### 5. Is The Options Dict Visible Inside The Body?

There are two broad models:

- the body receives a real dict value
- the evaluator resolves options before body execution and stores them in locals

Receiving a real dict is simpler and matches current built-ins.

The body can always unpack what it needs.

But there is also a performance tradeoff:

- materializing a merged dict for every call is simple
- not materializing it unless needed is faster

### 6. Can Callers Still Pass An Explicit Dict?

I think yes.

That should remain the canonical low-level ABI.

Then the sugar is optional and non-breaking.

### 7. Are Required Named Arguments In Scope For This Feature?

Probably not initially.

Start with:

- positional required args
- optional named args

If required named args ever appear, they can use the same schema machinery later.

### 8. Is Omission Distinct From "Provided Value Equals Default"?

This matters if a definition wants to know whether the caller explicitly set a key.

There are two models:

- only the final merged value matters
- the runtime also tracks which keys were explicitly overridden

The first model is simpler and cheaper.

### 9. Does A Definition Receive Fully Merged Options Or Just Overrides?

Choices:

- pass only overrides and let the body call `maybe`/`getDef`
- pass a fully merged dict
- bind resolved option locals before entering the body

This is one of the biggest semantic and performance choices.

### 10. Are Optional Args Part Of Overload Resolution?

If multiple built-in type definitions exist, do optional keys participate in choosing one?

Prefer no, initially.

Overload resolution should stay positional first.

### 11. Can Optional Args Be Used On Prefix-Quote Calls?

Example:

```mshell
[1 2 3] ?{ 'n': 2 } take.
```

This is tempting, but it complicates parsing and should probably wait.

## Performance Review

If the feature is implemented naively, it can absolutely create a lot of garbage:

- parse an override dict literal
- allocate a runtime dict object
- allocate a second merged dict object
- maybe unpack values into locals
- then discard the dicts after one call

That would be wasteful for hot built-ins.

### Performance Goal

The common path should be:

- zero extra allocations when no optional args are provided
- at most one small allocation when overrides are provided
- no full merge dict allocation unless the callee actually needs a dictionary object

### Recommended Runtime Model

Represent optional args internally as:

- schema/defaults stored once on the callable
- per-call overrides stored separately

Then give the callee a lightweight lookup object that answers:

1. is key overridden?
2. otherwise return static default

That avoids building a merged dictionary eagerly.

Conceptually:

```text
callable schema:
  overwrite -> false
  stripComponents -> 0

call site overrides:
  overwrite -> true

lookup("overwrite") => true
lookup("stripComponents") => 0
```

### Where To Store Defaults

For performance, defaults should be parsed once and stored on the definition/built-in object, not rebuilt per call.

For user definitions that means:

- compile the default expressions at definition load time if they are static literals
- store resulting `MShellObject`s directly in the definition metadata/schema

For built-ins:

- hardcode the schema once in Go

### Avoid Eager Merge Dicts

The most important performance recommendation is:

- do not eagerly allocate a merged `MShellDict` on every call

Instead:

- keep overrides as one small dict or compact slice
- fall back to schema defaults on lookup

Only materialize a real merged dict if:

- the function body explicitly needs one as a first-class object
- or the existing implementation already consumes a dict and rewriting it is not worth it yet

### Prefer Key Interning Or Small Fixed Layouts For Built-Ins

For hot built-ins with a small known option set, a generic `map[string]MShellObject` is not ideal.

A more efficient internal representation is:

- schema assigns each known option a small integer slot
- overrides are stored in a small slice or bitset-plus-slice

That reduces:

- string hashing
- map allocation
- GC pressure

This is especially attractive for built-ins like `zipExtract` and `numFmt` where the key set is tiny and fixed.

### Keep The Fast Path Fast

For calls with no optional overrides:

- the caller should not allocate any options object at all
- the callee should branch to its existing default behavior

That means syntax like:

```mshell
`myzip.zip` zipExtract
```

should ideally execute with essentially the same runtime cost as today, aside from one extra nil-check on optional schema.

### User Definition Tradeoff

User definitions are trickier than built-ins.

If the definition body wants `@opt` as a normal dict, you probably will allocate one.

If instead the implementation binds option locals on entry, like:

- `overwrite`
- `stripComponents`

then you can avoid a materialized dict entirely for the common case.

That suggests two implementation tiers:

- first version: materialize one merged dict for user definitions, keep built-ins optimized enough
- later version: compile option lookups into local bindings and avoid the dict

### GC-Critical Questions

These are the questions I would answer before implementing:

- Does a user definition need an actual options dict object, or are resolved locals enough?
- Are default values required to be static literals, so they can be stored once?
- Can built-ins use specialized structs instead of general dictionaries?
- Is the common case "no overrides", and can that be represented as `nil`?
- Do we need to preserve "was explicitly provided" information, or only the resolved value?

## More Questions

- Should the optional-argument syntax be legal only before named callables, or also before quotations?
- Should the declaration syntax allow defaults that reference earlier positional inputs, or must defaults be closed/static?
- If a caller passes an explicit trailing dict and also uses optional-argument sugar, is that forbidden?
- Should optional args appear in `defs` output and completion metadata?
- Should unknown keys be a parse-time error for statically known callables, or always runtime?
- If a definition is recursive, are option defaults re-evaluated each recursive call, or stored once?
- Can optional args be inherited through wrappers, or must wrappers manually forward them?
- Do you want syntax for "required keyword-like args" later, and should this design leave room for that?

## A Plausible First Version

If the goal is a practical first implementation, this seems like the least risky shape:

### Source-Level Model

- A callable may declare an optional schema
- The runtime ABI still ends with a dict
- Callers may either pass that dict explicitly or use sugar

### Declaration

Either:

- add an optional section to definition signatures

Or:

- temporarily store optional schema in definition metadata

The signature-section approach is better long-term.

### Call Site

Choose one:

- `?{ ... } fn`
- `{ ... } withArgs fn`

I would prototype the keyword form first because it avoids a lot of lexer churn.

### Semantics

- merge caller bundle over defaults
- reject unknown keys
- reject duplicate keys
- materialize one dict object
- call the target exactly as if that dict had been passed explicitly

## Open Questions

- Do you want optional args to feel like sugar over dicts, or like a new core calling convention?
- Is a keyword form acceptable, or do you specifically want a punctuation-based bundle like `?{ ... }`?
- Should optional argument declarations live in metadata first for speed, or should the feature start with a real signature extension?
- Do built-ins and user definitions have to share exactly the same declaration syntax from day one?

## Current Bias

If I were choosing a direction now:

1. make optional args a thin layer over an explicit trailing dict
2. reject unknown and duplicate keys
3. keep defaults static
4. support named built-ins and named definitions first
5. start with either:

```mshell
123 { 'decimals': 2 } withArgs numFmt
```

or, if you want real syntax immediately:

```mshell
123 ?{ 'decimals': 2 } numFmt
```

6. plan a later follow-up that moves declaration of optional args into the type-signature grammar

That gives you a clean semantic core now, and leaves room to polish the surface syntax later.
