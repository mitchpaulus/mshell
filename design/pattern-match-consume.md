# Pattern-Matching stack semantics

As of 2026-04-18, whether or not a `match` arm consumes the subject on the stack
depends on the pattern itself.

This creates unnecessary mental overhead.
Consumption should be a user choice at the arm separator, not something inferred
from the pattern shape.

## Proposal

`match` arms support two separator tokens:

```text
':'  Consume the matched subject before running the arm body
':>' Preserve the matched subject when running the arm body
```

Separator choice fully determines consumption.
It is completely independent of pattern kind and independent of whether the
pattern introduces bindings.

## Semantics

For a matching arm:

- `:` consumes the matched subject before the arm body runs.
- `:>` leaves the matched subject on the stack when the arm body runs.

This rule applies to all pattern forms:

- Literal patterns
- Type patterns
- `_`
- `none`
- `just v`
- `just _`
- List patterns
- Dict patterns

There are no pattern-specific exceptions.
In particular, patterns that destructure without introducing bindings still
follow the separator rule.
Examples: `just _`, `[]`, `{ 'k': _ }`.

Bindings are independent from consumption.
For example, `just v :>` is valid and should both bind `v` and preserve the
original matched subject on the stack.

## Breaking Change

This is an intentional breaking change with no compatibility mode.

Code using `:` for match arms will now always consume the subject, even for
patterns that previously preserved it.
Users should be informed through the changelog and updated documentation.
