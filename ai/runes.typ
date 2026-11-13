// Design: a rune type and rune literals.
// Build: typst compile ai/runes.typ

#set document(title: "Runes in mshell", author: "Claude (draft for Mitchell)")
#set page(paper: "us-letter", margin: (x: 1in, y: 1in), numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt)
#set par(justify: true, leading: 0.62em)
#show table: set par(justify: false)
#show table: set text(size: 9.5pt)
#set heading(numbering: "1.1")
#show heading.where(level: 1): it => { v(0.8em); it; v(0.3em) }
#show raw: set text(font: "DejaVu Sans Mono", size: 0.86em)
#set raw(syntaxes: "msh.sublime-syntax")
#show raw.where(block: true): it => block(
  fill: luma(245), inset: 8pt, radius: 3pt, width: 100%, it,
)

#let callout(kind, body) = block(
  width: 100%, inset: (left: 10pt, rest: 8pt), stroke: (left: 2pt + luma(120)),
  fill: luma(250),
  [*#kind.* #body],
)
#let decision(body) = callout("Decision", body)
#let note(body) = callout("Note", body)
#let principle(n, title, body) = block(
  width: 100%, inset: (left: 10pt, rest: 8pt), stroke: (left: 2pt + rgb("#30638e")),
  fill: rgb("#f3f6fa"),
  [*P#n. #title* #body],
)

#align(center)[
  #text(size: 20pt, weight: "bold")[Runes in mshell]
  #v(0.3em)
  #text(size: 11pt)[Design draft --- 2026-10-18, branch `cli-bindings-design`]
  #v(0.2em)
  #text(size: 10pt, style: "italic")[Written by Claude from the lexer on `main` (e68153d)
  and the key-binding design (`ai/cli_bindings.typ`), which needs it.]
]

#v(1em)

= Summary

mshell has no value for "one character".
Code that means one character uses a string and hopes it has length one.
The key-binding design is where this stops being tolerable: a key is one character plus modifiers,
and with only strings a typo such as `ctlr` inside a list silently becomes the text key `"ctlr"`.

This design adds a type, `rune`, whose values are the Unicode scalar values, and two ways to write one:

```msh
r'a'        # a rune you can see
U+001B      # a rune given by its code point: Escape
```

A rune is one code point --- not a byte, and not a grapheme cluster.
`r'…'` holds exactly one code point; anything else is a lexer error that says what went wrong,
including the error a Python user will make, `r'raw string'`.
Between the two forms every rune can be written, so `r'…'` needs no escapes.

#table(
  columns: (auto, 1fr),
  inset: 5pt,
  stroke: 0.4pt + luma(180),
  [*Adds*], [*What for*],
  [type `rune`, its own kind], [`rune | NamedKey | ModifiedKey` is a legal union, and `str` is not in it],
  [`r'a'`, `U+0301`], [the two literal forms (section 5)],
  [`runes`, `toRune`, `toInt`, `str`], [conversion to and from strings and integers (section 9)],
  [`=`, `<`, `match`], [comparison by code point; value and type patterns (section 7)],
)

= Why a code point

Text in mshell has three units, and each is right for something.

#table(
  columns: (auto, 1fr, 1fr),
  inset: 5pt,
  stroke: 0.4pt + luma(180),
  [*Unit*], [*Decided*], [*Right for*],
  [byte], [immediately], [files, sockets, `binary`],
  [code point (rune)], [when its last byte arrives], [keys, lexers, parsers, character classes],
  [grapheme cluster], [only when the _next_ code point arrives], [cursor movement, width, what a person calls "a character"],
)

A grapheme boundary depends on what follows (UAX \#29): a combining mark, a zero-width joiner,
a second regional-indicator letter or the next Hangul jamo each extend the cluster before it.
A key-sequence matcher that worked on graphemes would have to wait for the next key before deciding
anything, which the binding design forbids (it never waits to show what was typed).
A code point is complete when its bytes are.
Terminals agree: the kitty keyboard protocol reports a key that produces text by its Unicode code point,
and gives keys without text (F13 and up, keypad, modifier keys) code points of their own in the
Private Use Area from U+E000.
Arrows, Home and F1--F12 keep their legacy encodings, which the decoder names.
Either way the binding trie sees one code point, or one named key, plus modifiers.

A byte is too small: `é` is two bytes, and half of it is not text.
A grapheme is too large and arrives too late.
The code point is the unit that is both complete and immediate.

== Scalar values, not code points

Unicode has 1,114,112 code points, U+0000 to U+10FFFF.
2,048 of them, U+D800 to U+DFFF, are _surrogates_: they are reserved for UTF-16,
which writes each code point above U+FFFF as a pair of them (😀 U+1F600 is `D83D DE00`).
They are never characters, and UTF-8 has no encoding for them.
The other 1,112,064 code points are the _scalar values_, and they are what UTF-8 encodes ---
including everything above U+FFFF, in four bytes (😀 is `F0 9F 98 80`).
Over half of all assigned characters are above U+FFFF: most emoji, the CJK extensions from U+20000,
Adlam, mathematical letters, historic scripts.

mshell source is UTF-8 and its strings are UTF-8, so a rune is a scalar value.
Every rune then converts to a one-rune string and back, with nothing lost.

= Prior art

#table(
  columns: (auto, auto, auto, 1fr),
  inset: 5pt,
  stroke: 0.4pt + luma(180),
  [*Language*], [*Type*], [*Literal*], [*Values*],
  [Rust], [`char`], [`'a'`, `'\u{301}'`], [scalar values; `'\u{D800}'` is a compile error. The closest match to this design.],
  [Go], [`rune` (int32)], [`'a'`, `'́'`], [any int32; the literal rejects surrogates and values above 10FFFF, but `rune(0xD800)` is allowed],
  [Swift], [`Character` / `Unicode.Scalar`], [`"a"` with a type], [`Character` is a grapheme cluster; `Unicode.Scalar` is a scalar value],
  [Haskell], [`Char`], [`'a'`], [every code point, surrogates included],
  [Java, C\#], [`char`], [`'a'`], [a UTF-16 code unit: 😀 is two `char`s. The mistake to avoid.],
  [C++], [`char32_t`], [`U'a'`], [a 32-bit unit],
  [Python, JavaScript], [none], [---], [a string of length one; Python strings may hold lone surrogates],
  [Emacs Lisp, Ruby, Elixir], [integer / string], [`?a`], [a code point (Ruby: a one-character string)],
  [Clojure, Scheme], [character], [`\a`, `#\a`], [a code point],
  [Factor], [integer], [`CHAR: a`], [a code point --- the concatenative precedent],
)

Rust and Go chose the code point, and Rust chose scalar values.
Java chose the UTF-16 unit thirty years ago and still pays for it.
Swift's `Character` shows the other path: graphemes for "a character", with scalars underneath.
mshell takes Rust's values and Go's name.

= Principles

#principle(1, "One rune is one scalar value.")[
No byte runes, no surrogate runes, no "character" that might be several code points.
Every rune encodes to UTF-8, and every one-rune string decodes to a rune.]

#principle(2, "Every rune has a spelling, and only one form is needed for each.")[
`r'…'` for runes that are visible on their own; `U+XXXX` for the rest.
No escape language inside `r'…'`, as there is none inside `'…'`.]

#principle(3, "A wrong literal is a lexer error that names the mistake.")[
The likely mistakes are known: a raw string, a character made of two code points,
an apostrophe, a surrogate. Each gets its own message (section 6).]

#principle(4, "No implicit conversion between rune and str.")[
`r'a'` is not `'a'`. A union may hold both because they are different kinds,
and a typo that makes a string must not pass where a rune is wanted.]

#principle(5, "Printing round-trips.")[
A rune prints, inside a list or a record, in a form the lexer reads back as the same rune.]

= Literals

== `r'…'`

`r` followed immediately by `'`, at the start of a token, begins a rune literal.
The lexer reads to the next `'` exactly as `parseSingleQuoteString` does, then counts code points.
Exactly one gives a `RUNE` token; anything else gives one error token covering the whole literal.

```msh
r'a' r';' r'\' r'é' r'😀' r'Ω' r' '
```

`r'\'` is a backslash, as `'\'` is a one-character string: there are no escapes.
A rune literal cannot hold `'`, and should not hold a combining mark on its own, since the mark
attaches to the quote before it and cannot be seen; both are written with `U+`.

The `r` is lexed only at the start of a token.
`xr'a'` is the bare word `xr` followed by the string `'a'`, as today.
Today `r'a'` is the word `r` followed by the string `'a'` with no space between them;
no file in `lib`, `tests` or `~/dotfiles/.config/msh` writes that, so nothing changes meaning.

#note[
Python reads `r'…'` as a raw string.
That reading is harmless for one character --- a Python user who reads `r'a'` still sees `a` ---
and wrong only for longer text, where the lexer stops them with a message that explains it (section 6).
`%'a'` was the other candidate: it has no meaning in any common language.
`r` was kept because it is the type's name and Go's.]

== `U+XXXX`

`U+` followed by a hexadecimal digit, at the start of a token, begins a code-point literal.
It takes 4 to 6 hex digits, upper or lower case, and must be followed by whitespace,
a character that ends a literal (`]`, `)`, `}` and the rest of `literalStop`), or the end of input.
This is the Unicode standard's own notation, so a character can be copied from a code chart or an error message.

```msh
U+001B      # Escape
U+0027      # apostrophe
U+0301      # combining acute accent
U+00A0      # no-break space
U+1F600     # 😀, same rune as r'😀'
```

Uppercase `U` only: `u+` stays a bare word.
Lowercase hex digits are accepted, but the printer writes uppercase.

Today `U+001B` is a bare word: an unknown identifier at top level, the string `"U+001B"` inside a list.
That is the same silent-string trap the type is meant to close, and this closes it.
`U+` followed by something other than a hex digit stays a bare word.

== Grammar

```
rune      = "r'" one-code-point "'"
          | "U+" hex hex hex hex [hex [hex]]      (followed by a literal stop)
hex       = "0".."9" | "a".."f" | "A".."F"
```

The value must be a scalar value: at most U+10FFFF, and not U+D800--U+DFFF.

= Errors

Each error covers the whole literal, so `--lex`, the type checker and the interactive highlighter
all mark the same span.

#table(
  columns: (auto, 1fr),
  inset: 5pt,
  stroke: 0.4pt + luma(180),
  [*Input*], [*Message*],
  [`r'asdf'`], [`r'asdf' is a rune literal and holds exactly one code point. r is not a raw-string prefix: single-quoted strings are already raw, so write 'asdf'.`],
  [`r'é'` as e + U+0301], [`r'é' holds 2 code points (U+0065 U+0301) that display as one character. A rune is one code point. Use U+00E9, or write the runes separately: r'e' U+0301.`],
  [`r'👨‍👩‍👧'`], [The same message, listing the five code points, without a one-code-point suggestion.],
  [`r''`], [`empty rune literal: a rune holds exactly one code point.`],
  [`r'` and a tab `'`], [`r'…' holds a control character: write it as U+0009.`],
  [`r'a` and a newline], [`Unterminated rune literal: r' needs its closing quote on the same line.`],
  [`r'''`], [`a rune literal cannot contain ': write the apostrophe as U+0027.`],
  [`r'a` (end of input)], [`unterminated rune literal.`],
  [`U+41`, `U+0000041`], [`U+41: a code point needs 4 to 6 hex digits, as in U+0041.`],
  [`U+00GG`, `U+0041x`], [`U+00GG: expected 4 to 6 hex digits after U+, then a space or delimiter.`],
  [`U+110000`], [`U+110000 is above U+10FFFF, the last code point.`],
  [`U+D83D`], [`U+D83D is a surrogate: it exists only inside UTF-16 and is not a character. Write the character's own code point, such as U+1F600.`],
)

How the messages are chosen:

- *Raw string or two-code-point character.*
  `uniseg` is already a dependency.
  More than one grapheme is the raw-string case; one grapheme of several code points is the
  "looks like one character" case.
- *The single-code-point suggestion.*
  `golang.org/x/text` is already required, so `x/text/unicode/norm` costs an import, not a module.
  The suggestion is given only when NFC turns the text into one code point.
- *`r'''`.*
  Without a special case this lexes as `r''` and then an unterminated string.
  When `r''` is followed directly by `'`, the lexer gives the apostrophe message instead of two confusing ones.
- *Interactive input.*
  As with `UNFINISHEDSINGLEQUOTESTRING`, an unfinished `r'a` gives an `UNFINISHEDRUNE` token for highlighting,
  and a literal that cannot become valid by typing more (`r'ab'`) is shown as an error as soon as it closes.

All of these are on the error path.
A valid literal costs one extra comparison at the start of a token beginning with `r` or `U`.

= The type

`rune` is a base type, beside `int`, `str`, `bool`, `float`, `path`, `datetime` and `binary`,
and it is its own kind.
So `rune | str` and `rune | NamedKey | ModifiedKey` are legal unions, and a `match` can tell their members apart.

== Comparison

`=` gains the overload `(rune rune -- bool)`.
`r'a' 'a' =` is a type error, as `1 "1" =` is today:
"no matching overload for '=': the stack has (rune str)".
`<`, `<=`, `>`, `>=` order runes by code point.
`sort` is not extended: it turns every element into a string first, and a rune's string sorts by code point anyway, since UTF-8 byte order is code point order.
Code-point order is not alphabetical order for any language; it is the only order that needs no locale.

== Patterns

`rune` is a type pattern (`rune r : …`), and rune literals are value patterns.
An arm may list several runes, as it may list several strings or integers:

```msh
@c match
    r'(' r'[' r'{' : open,
    r')' r']' r'}' : close,
    U+0009 r' '    : space,
    _              : other,
end
```

All alternatives in one arm are runes, under the existing rule that an arm's alternatives are one kind.

== In list literals

A rune literal in a list is a rune: `[r'a' r'b']` is a `[rune]`.
A bare word in a list is still a string, so `[r'x' ctlr]` is a `[rune | str]`,
which is not a `[Key]` when `Key = rune | NamedKey | ModifiedKey` --- the typo is caught.

= Printing

#table(
  columns: (auto, 1fr),
  inset: 5pt,
  stroke: 0.4pt + luma(180),
  [*Where*], [*Form*],
  [`wl`, `w`, `str`], [the character itself: `r'a' str` is `"a"`; `U+001B wl` writes the Escape byte],
  [inside a list, record or error message], [source form: `r'a'` when the rune is graphic, not a mark, not `'`, and not a space other than U+0020; otherwise `U+XXXX` with at least four uppercase digits],
  [`toJson`], [a one-character JSON string: `r'a' toJson` is `"a"`. JSON has no character type; parsing it back gives a `str`.],
)

So `[r'a' U+0301 r'😀' r' ' U+00A0] str` is `[r'a' U+0301 r'😀' r' ' U+00A0]`, which reads back as the same list (P5).
The space bar's rune prints as `r' '`. The other spaces (no-break, ideographic, the fixed-width ones) print as `U+XXXX`, because inside quotes they cannot be told from it.

= Builtins

#table(
  columns: (auto, auto, 1fr),
  inset: 5pt,
  stroke: 0.4pt + luma(180),
  [*Word*], [*Type*], [*Does*],
  [`runes`], [`(str -- [rune])`], [split into code points],
  [`toRune`], [`(int -- Maybe[rune])`], [`none` above 10FFFF, below 0, or in the surrogates],
  [`toRune`], [`(str -- Maybe[rune])`], [`just` only for a string of exactly one code point],
  [`toInt`], [`(rune -- int)`], [the code point; a plain `int`, as for `float`],
  [`str`], [`(rune -- str)`], [the one-rune string],
)

`toRune` and `toInt` follow `toInt` and `toFloat`: conversion that can fail returns `Maybe`, conversion that cannot does not.
`+` does not join a rune to a string (P4): `'ab' r'c' str +`.

Character classes --- `isLetter`, `isDigit`, `isSpace`, Unicode general categories --- are the obvious next words,
and are left out until a use needs them (section 14).

= Use in key bindings

With a rune type the key-binding design's key type needs no wrapper around strings:

```msh
enum NamedKey = keyEnter | keyTab | keyBackspace | keyEscape | keyUp | keyDown | keyF1 end
enum ModifiedKey = keyCtrl Key | keyAlt Key | keyShift Key | keySuper Key end
type Key = rune | NamedKey | ModifiedKey

{keys: [r';' r'r'], action: [(r) editRun] bindingOps} bindingAdd
{keys: [r'j' r'f'], action: [editAccept] bindingOps, idleAfter: 100} bindingAdd
{keys: [r'd' keyAlt], action: (insertDate) bindingFunc} bindingAdd
{keys: [keyF1], action: [(lf) editRun] bindingOps} bindingAdd
```

`bindingAdd` normalizes each key to the 32-bit key code the matcher uses (code point plus modifier bits),
so `[r'x' keyAlt keyCtrl]` and `[r'x' keyCtrl keyAlt]` are the same binding, and the matcher never sees mshell values.
What a key is, the type now says: one rune, a named key, or a modified key.
`'jf'`, `''` and `ctlr` cannot be written as a text key.
`ai/cli_bindings.typ` still describes string keys and is updated after this design is settled.

= Found while writing this

- *String indexing is by byte and can make invalid UTF-8.*
  `"éa" :1` is the string holding the single byte `C3`, which prints as `"�"`.
  `MShellString.Index` and the slice methods use byte offsets.
  The docs say `take` and `skip` count "characters" and `len` counts bytes.
  A string that is not UTF-8 contradicts "mshell strings are UTF-8".
  With `runes`, code-point access has a correct spelling; byte slicing of strings should either
  move to `binary` or refuse to cut inside a code point.
  This is outside this design, but `runes` makes the choice easier.

= Formal model

The Rocq model in `formal-ver/` has `TInt`, `TStr` and `TBool` as base types and `KInt`, `KStr`, `KBool` as kinds.
A rune adds one base type and one kind, `TRune` and `KRune`, with the same rules as `TStr`:
a literal has type `TRune`, `=` takes two of them, and kind patterns distinguish it.
Nothing about it is new to the calculus --- no variance, no generics, no recursion ---
but the union rule ("members of different kinds") is exactly what lets `rune | str` and `rune | NamedKey` exist,
so the extension is made in the model and `make check` run before the Go type checker changes.
`TStr` appears in about 90 places across 10 files; most are cases added mechanically.

The lexer rules are checked by tests, not in the model:
a table of inputs and expected tokens or messages (section 6),
and a property test that `str` of a random `[rune]` lexes back to the same list (P5).

= Implementation

+ *Model.* Add `TRune` and `KRune` to `formal-ver/`; `make check` closes with no axioms.
+ *Lexer.* `RUNE` and `UNFINISHEDRUNE` tokens; `r'` and `U+` at token start in `scanTokenAll`;
  the error messages of section 6.
+ *Parser and objects.* `MShellRune` holding a Go `rune`; equality, ordering, `str`, source form, `toJson`.
+ *Type checker.* The `rune` base type and kind; `=` and `<` overloads; value and type patterns in `match`.
+ *Builtins.* `runes`, `toRune`, the `toInt` overload, in `Evaluator.go` and `BuiltInList.go`.
+ *Syntax.* `ai/msh.sublime-syntax` and the VS Code grammar in `code/` highlight both literal forms.
+ *Docs.* A "Runes" section beside Integers and Floats in `doc/`, `doc/mshell.md`, the String Functions list,
  the type-pattern keyword list, and a `CHANGELOG` entry.
+ *Tests.* Lexer table, round-trip property, type errors (`r'a' 'a' =`), `match` on runes, and `tests/` scripts.

= Open questions

+ *Character classes.* Which words, and with which Unicode version's tables (Go's `unicode` package follows the Go release).
+ *`in`.* Whether `(str rune -- bool)` is worth an overload, or `str in` is enough.
+ *Byte indexing of strings* (section 11): its own design.
+ *`Key` names.* The binding design's named keys use the `key` prefix because member names are global; `keyCtrl` and `keyEnter` are proposed above.
