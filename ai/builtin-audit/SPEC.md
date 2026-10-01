# Porting the builtin table to the core checker: spec for one category

Repository: /home/mitch/repos/mshell (Go code in `mshell/`). Do NOT edit any file in the repository.
Write your two outputs to the directory given in your task.

## Background (read these, in this order, only the parts you need)

- `ai/type-core-calculus.typ`: the design. The sections that matter: "Unions have distinct kinds",
  "Subtyping" (invariance), "Runtime keys" (read-only dict builtins), "Freshness" (incl. "One rule for
  new lists", "New def outputs"), "Operations that return lists", "The builtin contract", "Quotes that break".
- `mshell/TypeCoreBuiltins.go`: the seed of the new table. Follow its style exactly.
- `mshell/TypeBuiltins.go`: the OLD table (`builtinSigsByName`). It is a starting point only; its entries
  were never audited, and several follow rules the design removes.
- `mshell/Evaluator.go`: the runtime. The truth for what each builtin accepts and returns. Find each
  builtin with `grep -n 'case "NAME"' mshell/Evaluator.go` (some share a case with others).

## What a table entry is

```go
b.reg("name", "(in1 in2 -- out1)", "(other overload -- ...)")
```

Stack order: inputs are listed bottom to top, so the LAST input is the top of the stack. Check the
runtime's pop order (`obj1, obj2, err := stack.Pop2(t)` gives obj1 = top, obj2 = below it).

Type syntax the core resolver accepts:

| Syntax | Meaning |
|---|---|
| `int float str bool bytes path datetime null` | base types |
| `[T]` | list of T (invariant) |
| `{str: T}` or `{T}` | dictionary: any string key, every key deletable |
| `{a: T, b?: U}` | a shape type, OPEN: other keys may exist, unknown, read-only |
| `{a: T, *: U}` | a shape whose other keys are U |
| `{}` | any dict-kinded value, read-only (the design's `{| open}`) |
| `Maybe[T]`, `Json` | the built-in enum, the built-in recursive alias |
| `T | U` | a union; members must have DIFFERENT runtime kinds (no `[int] | [str]`, no two shapes) |
| `(a b -- c)`, `(a -- never)` | quote types |
| `a`, `b`, ... | generics: single lowercase letters |
| `Grid`, `GridView`, `GridRow` | grids, schema unknown |
| `new T` | (outputs only) the output is always new: nothing else references it or anything inside it |

Marks, called after `b.reg`:

- `b.newList("name")`: every candidate's first output is a NEW list built from the inputs' elements or
  from a quote's results. It is fresh exactly when its element type is immutable (design: "One rule for
  new lists"). Use for map, filter, sort, reverse, take, skip, slices, uniq, ...
- `b.keeps(t.name(res.names.Intern("name")))`: the output is fresh when every input is fresh or immutable,
  because it is the input itself or wraps it (just, `?`, append returns its receiver).
- `b.child("name")`: the builtin runs its quote arguments on a CHILD stack (each, map on a list, filter, the
  grid functions); see "Quotes that break". A builtin that runs a quote on the CURRENT stack (map on a
  Maybe, map2, bind, and/or with a quote) is not marked.
- `new` in the output text: the output is a newly allocated value made only of new objects and immutable
  values (parseJson's tree, split/lines' `[str]`, a new dict of strings).
- No mark: the output is SHARED (it may be referenced elsewhere: an element read out of a container, a
  value from a variable, ...). Shared is always safe; fresh must be justified.

## Rules (from the design; these change entries from the old table)

1. **Accept exactly what the runtime accepts.** Read the Go case. If the runtime accepts `str | path`,
   write that; if it rejects int where the old sig accepts it, drop int. If it accepts more, add it.
2. **Invariance.** `[int]` is not below `[int | float]`. A builtin that only reads list elements is
   generic (`[a]`) or has per-type overloads (`[int]`, `[float]`), never `[int | float]`.
3. **Read-only dict builtins** accept every dict-kinded value. `keys`, `len`, `in` take `{}`.
   Builtins that read VALUES out of a dict with a runtime key or all keys (`values`, `getDef`, runtime-key
   `get`, `filter` on a dict, `keyValues`, ...) cannot be written as a signature: the result type is the
   join of every label's type (design "Get-Key"). List them as SPECIAL. Only `setd` and `del` need
   `{str: T}`.
4. **No free variable in an output** that no input determines (P10): `parseJson (str -- t)` is wrong; it
   is `new Json`. Data from outside is `Json`, `str`, `[str]`, ... never a free generic.
5. **In-place type changes** (redirects, `updateCol`, `gridAddCol`, `gridRemoveCol`, `gridRenameCol`, a
   type-changing `gridSetCell`, and anything else you find that changes the TYPE of an object it is given)
   are allowed only on a fresh operand. List them as SPECIAL with the exact runtime behavior.
6. **Overloads** are chosen from the argument types. Two candidates should not both fit the same known
   arguments; if they would, say so.
7. **Maybe** is written `Maybe[T]`. `none` is already in the table.
8. Anything that cannot be a signature (stack effect depends on a value, a literal key, a schema, a
   variable number of items, a quote's arity) is SPECIAL: describe exactly what the runtime does.

## Your two outputs

1. `<category>.go.txt`: Go code to paste into `buildCoreTable`, in the style of `TypeCoreBuiltins.go`
   (comments in the same voice: short, factual, no history). Entries for everything expressible.
2. `<category>.md`: an audit table, one row per builtin:
   `| name | runtime accepts (Evaluator.go:line) | old sig | new sig + marks | notes |`
   Notes say what changed from the old sig and why (which rule), and list SPECIAL handling precisely.
   End with a list of anything in the runtime that looked like a bug, and anything you were unsure of.

Be exact and concise. Do not guess: when unsure, read the Go code; if still unsure, say so in the notes.
