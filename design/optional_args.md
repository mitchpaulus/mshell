# Optional Arguments

## Problem Statement

Oftentimes, there is a definition or function that has a few key inputs, but many "defaults".
In that case, it is cumbersome and annoying to have to be explicit about many inputs that are trivial or only need to be changed in rare cases.
Python has something like keyword arguments, where a bunch optional named items can be collected into a dictionary.

As a stack based language, the number of items on the stack must be known, it cannot be variable.

## Potential Examples

```mshell
`myzip.zip` zipExtract  # Running without setting explicit optional dict
`myzip.zip` # { 'overwrite': false } zipExtract
```

Usage within a definition:

```
def mydef (str # { 'my_opt_prop': 10 } -- str)
  @opt :my_opt_prop 5 maybe # default to 5 for optional property
  # do things..
end
```

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

## Lexer Decisions

These decisions have to be made before picking syntax.

### 1. Is The Feature Introduced By A New Sigil?

If yes, the lexer needs a dedicated token for something like:

- `?{`
- `&{`
- some other prefix immediately before `{`

If no, and the syntax is keyword-based, the lexer might not need to change at all.

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

## Semantic Decisions

These are the biggest design choices.

### 1. What Is The Lifetime Of The Bundle?

If call-site sugar creates a pending bundle, does it apply to:

- the very next callable token only
- the next definition or built-in only
- the next thing that consumes a call frame

The rule should be narrow.

Recommended rule:

- it applies only to the immediately following named built-in or named definition
- anything else in between is an error

That prevents spooky action at a distance.

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
