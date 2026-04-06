# Optional Arguments

## Problem Statement

Oftentimes, there is a definition or function that has a few key inputs, but many "defaults".
In that case, it is cumbersome and annoying to have to be explicit about many inputs that are trivial or only need to be changed in rare cases.
Python has something like keyword arguments, where a bunch optional named items can be collected into a dictionary.

As a stack based language, the number of items on the stack must be known, it cannot be variable.

## Potential Examples

```mshell
`myzip.zip` zipExtract  # Running without setting explicit optional dict
`myzip.zip` % { 'overwrite': false } zipExtract

{
  'myprop': 1
} defaults!

myinput % @defaults myFunctionWithDefaults
```

Usage within a definition:

```mshell
def mydef (str % { 'my_opt_prop': 10 } -- str)
  @opt :my_opt_prop 5 maybe # default to 5 for optional property
  # do things..
end
```

## Current Direction

The design should treat optional arguments as:

- fixed-arity calls
- positional required arguments
- plus one optional caller-supplied dict attached to a specific callable via `%`

The key point is attachment.

This should **not** be implemented as ambient interpreter state that lives until some later function call happens to consume it.

Instead, the parser should bind the `% ...` part to exactly one following callable.

Conceptually:

```mshell
myinput % @defaults myFunctionWithDefaults
```

should parse more like:

```text
CallWithOptionalArgs(
  target = myFunctionWithDefaults,
  opt = @defaults,
  positional = [myinput]
)
```

and not like:

```text
PushPendingOptionalArgs(@defaults)
...
LaterCall(myFunctionWithDefaults)
```

The important implication is:

- `%` is part of call syntax
- `%` is not a way to push an ordinary dict argument on the stack
- there is no separate "trailing explicit dict" calling convention here

## Why Bare `%` Is Plausible

`#` is already taken for comments, so it is a poor fit.

Bare `%` is much more plausible:

- it is visually small
- it works with a variable, not just a literal dict
- it naturally supports reuse of a shared options object

Example:

```mshell
@input % @defaults myFunction
```

That is the main reason bare `%` is better than `%{ ... }` for your stated use case.

## Grammar Impact

Today `%` is not a dedicated token.
It is just part of a literal.

That means adding bare `%` as syntax requires reserving `%` in the lexer.

### Lexer Decisions

The lexer must decide:

- `%` as a standalone token becomes special
- `%foo` should probably no longer be a single literal
- `%` should likely be added to `notAllowedLiteralChars`

Compatibility concern:

- this could break any existing script that uses literals beginning with `%`

I did not find evidence of that in the repo's `mshell` source files, but it is still a language-level compatibility change.

### Parser Decisions

The parser should keep `%` narrow.

It should **not** accept an arbitrary expression on the right, because then the grammar needs some extra way to mark where that expression ends before the callable begins.

Recommended parse rule:

- `%` is an infix binder
- the thing immediately after `%` is one parse item, not a full expression
- the thing immediately after that parse item is the target callable

So this is valid:

```mshell
myinput % @defaults myFunction
myinput % { 'a': 1 } myFunction
```

and this should be invalid:

```mshell
myinput % @defaults 1 + myFunction
```

Practical allowed forms on the right side of `%` are likely:

- a dict literal
- a variable retrieve like `@defaults`

The callable must appear immediately after the optional-args item.

### Internal Parse Representation

Ordinary literals should stay exactly as they are today:

- a normal `Token` with type `LITERAL`

`%` should not create a second kind of literal token.

Instead, the parser should create a new compound parse item that implements `MShellParseItem`.

Conceptually:

```go
type MShellParseOptionalCall struct {
    OptArg MShellParseItem // dict literal or variable retrieve
    Target Token           // the callable literal token
}
```

So:

```mshell
myinput myFunction
```

would still parse as ordinary items, while:

```mshell
myinput % @defaults myFunction
```

would parse as:

- `Token(LITERAL, "myinput")`
- `MShellParseOptionalCall{OptArg: Token(VARRETRIEVE, "@defaults"), Target: Token(LITERAL, "myFunction")}`

This keeps the separation clean:

- the lexer only needs a `%` token
- the parser owns the "bind opt dict to immediate callable" rule
- the evaluator can resolve the target token at runtime the same way normal literal calls already resolve

That means the parser rule after seeing `%` is roughly:

1. parse exactly one allowed opt-arg item
2. require the next token to be a `LITERAL`
3. emit one `MShellParseOptionalCall`

## Signature / Definition Syntax

I was previously assuming the definition declared a schema or defaults.
That was wrong.

Your clarification implies a simpler model than a schema/merge system:

- the definition receives an optional dict from the caller
- the definition author decides which keys to inspect and what fallback behavior to use

The signature marker should stay exactly in the form you sketched:

```mshell
def mydef (str % { 'my_opt_prop': 10 } -- str)
  @opt :my_opt_prop 5 maybe
end
```

This is not a keyed schema that the runtime validates or merges against.
It is just the signature marker that says this definition accepts `%` optional args.

Inside the body, brevity still matters.

So for now the options object should be available by convention via a fixed variable:

- `@opt`

The important point is that the callee gets one dict-like object and can query whatever keys it wants.

## Runtime Semantics

At runtime, the clean model is:

- a callable may accept an optional dict via `%`
- the caller may provide zero or one dict-like bundle
- the callee receives that dict and may inspect it
- fallback behavior lives in the callee body, not in a declarative merge step

This must work for:

- built-ins
- user-defined definitions
- standard-library definitions

Important semantic choices:

- missing `%` means no optional dict was supplied
- if a dict literal contains duplicate keys, that should follow normal dict rules
- unknown keys are not an error at call time, because the callee may ignore keys it does not care about
- fallback/default behavior is authored manually in the callee body using normal dict access patterns

There is no separate source-language concept of "explicit trailing dict".
There is the stack, and there is `%` as the call operator carrying optional args.

## Performance / GC Review

With the clarified model, there should be no merge step at all.

That removes most of the GC concern from the previous draft.

The runtime should not:

- build a callee default dict
- merge caller values with callee defaults
- allocate a second resolved dict just for the call

Instead, the runtime should do something much simpler:

- evaluate the single dict-like item after `%`
- attach that object to the call frame
- expose it to the callee via `@opt` or equivalent

That means the only allocation cost is whatever was already needed to produce that dict object.

### Fast Path

For calls with no optional args:

- there should ideally be no new allocation at all

For calls with optional args:

- if the caller uses an existing variable like `@defaults`, there may be no new dict allocation at the call site
- if the caller uses a dict literal, the existing dict-literal evaluation cost is paid once
- the call machinery itself should not allocate another dict

### Built-Ins Vs User Definitions

Built-ins can be optimized more aggressively.

For hot built-ins, the implementation can still read keys directly from the optional dict and apply its own fallback logic without any generic merge machinery.

User definitions are trickier.

If the body gets `@opt` directly, there is no extra work beyond normal variable binding.

The main performance question then becomes:

- does passing optional args into a call frame require copying the dict object, or can the frame just reference it?

It should just reference it.

That keeps the feature cheap.

Wrapper definitions should forward optional args explicitly, not implicitly.

## Resolved Decisions

- `%` should not accept an arbitrary expression on its right; it should take one dict-like parse item
- the callable must appear immediately after the optional-args item
- the feature must work for built-ins, user-defined definitions, and standard-library definitions
- the signature marker should use the `% { ... }` shape from the example
- access inside the body should be brief, using `@opt` for now
- there is no source-level trailing-explicit-dict form here
- there should be no merge/default-resolution machinery in the runtime
- wrapper forwarding should be explicit

## Remaining Questions

- Should `%` be allowed on quotations or other higher-order call forms later, or only on normal definition/built-in call tokens first?

## Current Bias

The best current shape seems to be:

1. reserve bare `%` as a new token
2. parse `% <single-item> <callable>` as one call-site construct
3. do not allow `%` to become long-lived ambient state
4. have the callee receive a single optional dict object
5. keep fallback/default logic in the callee body
6. avoid any merge or resolved-dict allocation in the call machinery

Concretely:

- definition marker: `def mydef (str % { 'my_opt_prop': 10 } -- str)`
- body access: `@opt`
- call-site right side of `%`: only a dict literal or a variable retrieve

That keeps the syntax short, supports reusable option variables, and avoids the "defaults leaking into unrelated later calls" problem.
