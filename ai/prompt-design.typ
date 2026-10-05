// A typed, user-defined interactive prompt for mshell.
// Build: typst compile ai/prompt-design.typ

#set document(title: "A Typed Prompt for mshell", author: "Claude (draft for Mitchell)")
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

#let open(body) = block(
  stroke: (left: 2pt + rgb("#c08000")), inset: (left: 8pt, y: 4pt), width: 100%,
  [*Open:* #body],
)

#align(center)[
  #text(size: 17pt, weight: "bold")[A Typed Prompt for mshell]
  #v(0.2em)
  #text(size: 10pt)[Design notes, 2026-10-05. Starting point: `prompt_types.msh`.]
]

= Goal

Move the prompt out of the hard-coded string in `printPrompt` (`mshell/Main.go`)
and let the user supply it from their init file.
The user does not hand the shell bytes.
They hand it a list of `PromptItem` enum values:
text, line breaks, and style changes, each a separate member.

The payoff is that the shell knows the width of every cell in the prompt.
`paintPrompt` (`mshell/CommandPaint.go`) already lays the prompt out with the same
atoms, measured widths, hard breaks and wrap rules as command text.
If text items are always sanitized and style items have no width,
that layout stays exact.
There is no equivalent of bash's `\[ \]`,
where the user must mark which bytes take no space and a mistake corrupts line editing.

= Types

```
enum Base16Color =
      baseBlack | baseRed | baseGreen | baseYellow
    | baseBlue | baseMagenta | baseCyan | baseWhite
end

enum BaseBrightness = baseNormal | baseBright end

enum TextAttribute =
      attrBold | attrFaint | attrItalic | attrUnderline
    | attrBlinking | attrReverse | attrHidden | attrStrikethrough
end

enum PromptItem =
      promptText str
    | promptNewline
    | setFgColorRgb int int int
    | setBgColorRgb int int int
    | setFgColor256 int
    | setBgColor256 int
    | setFgColorBase16 Base16Color BaseBrightness
    | setBgColorBase16 Base16Color BaseBrightness
    | setFgDefault
    | setBgDefault
    | setTextAttr TextAttribute
    | clearTextAttr TextAttribute
    | resetStyle
    | setWindowTitle str
    | promptHyperlink str str      # url, text
end
```

Changes from `prompt_types.msh`:

/ `promptNewline`: the only way to start a new line. See @newlines.
/ `setFgDefault`, `setBgDefault`: SGR 39 and 49.
  `resetStyle` clears everything;
  these drop only the color, so a bold prompt can stop its background and stay bold.
/ `setFgColor256`, `setBgColor256`: the 256-color palette.
  Many themes use it, and it works where truecolor does not (tmux without extra config).
/ `clearTextAttr`: turns off one attribute (SGR 22, 23, 24, 25, 27, 28, 29).
  Bold and faint share SGR 22, so clearing either clears both; document it.
/ `resetStyle`: moved out of `TextAttribute`.
  As a member there, `clearTextAttr attrReset` would be meaningless.
/ `attrReverse`: renamed from `attrHighlighted`.
  SGR 7 is called reverse or inverse in every terminal reference.
/ `setWindowTitle`: OSC 2.
  Putting the directory in the tab title is one of the most common prompt customizations.
  It has no width and its text is sanitized like `promptText`.
/ `promptHyperlink`: OSC 8, for a clickable directory or git branch.
  The text goes through normal layout.
  The URL is validated the way `directoryFileURL` builds the OSC 7 URL:
  no control bytes, no `ESC \`.
/ `setCursorShape`: removed. See @cursor.
/ `propmtText`: typo fixed.

== Built in, with global names

These enums, `PromptInfo` and `CursorShape` are built into the shell, not declared in `lib/std.msh`.
The type checker knows them before any file loads,
and no user file can redeclare or change them.
The painter can rely on exactly these members and payload types.

A member, an enum, a `type`, a definition and a builtin each need a name of their own,
so every member name is still taken from every user script:
nobody can define their own `resetStyle` or `baseRed`.
This is why every member above carries a prefix (`base`, `attr`, `prompt`, `set`).
Before landing, check each name against `BuiltInList.go` and `lib/std.msh`.

== Integer ranges

An enum payload cannot say "0 to 255".
An RGB component or palette index outside that range is a runtime error when the prompt is painted,
which falls back to the built-in prompt (@failure).
Clamping would hide the mistake.

= The prompt definition

The user registers a quote in their init file:

```
setPrompt ((PromptInfo -- [PromptItem]) -- )
```

`setPrompt` is a builtin; with type checking on, the quote's type is checked where it is registered,
not when it first runs.
The shell calls the quote before every prompt.

== The quote runs on an empty stack

The quote does not see the user's stack.
It could consume values from it,
and the type checker's state, which persists between REPL lines, would no longer match.
Everything the quote needs to know about the session comes in through `PromptInfo`.

== `PromptInfo`

```
type PromptInfo = {
    stackDepth: int,
    stackTypes: [str],          # bottom first, e.g. ["int", "[Path]"]
    lastLineOk: bool,
    lastExitCode: Maybe[int],
    lastDurationMs: int,
}
```

It holds only what the REPL alone knows.
The directory, hostname, time, environment and git state are all available
through existing builtins and processes, and are not duplicated here.

/ `stackDepth`: what the hard-coded prompt shows today as `(3)`.
/ `stackTypes`: the checker's static type of each stack value.
  The checker already tracks these across REPL lines,
  so a prompt can show `(2: int [Path])` at no runtime cost.
/ `lastLineOk`: false when the previous line had a runtime error or failed its type check.
/ `lastExitCode`: see @exitcode.
  `Maybe[int]` rather than `lastExitCode?: int`:
  the shell always builds the record, so the key is always present;
  only the value can be absent.
/ `lastDurationMs`: wall time of the previous line.

Passing this as an argument instead of adding builtins that read REPL state has two advantages.
A builtin like "the REPL's stack" means nothing in a script.
And the prompt is testable: a script can call the quote with a hand-built `PromptInfo`
and inspect the list it returns.

There is no preview of the top value for now.
It would cost a format of an arbitrary value on every prompt;
it can be added later as another field.

= Exit code tracking <exitcode>

`EvalState` does not track a last exit code today.
At the one place a process or pipeline runs (`mshell/Evaluator.go`, the `*MShellList` / `*MShellPipe` switch),
`?` pushes the code, `!` and `soe` check it, and `;` drops it.
Pipeline stages run through `Execute` inside `RunPipeline`, which returns the last stage's code.

The change: one field on `EvalState`, written right after that switch and before the `!` check.
That covers `;`, `!` and `?` alike, a line stopped by `!`,
start failures (the negative codes) and signals.
Scripts get the field too; it costs nothing and they have no reason to care.

The REPL decides what "last" means:

+ Before each line that is not empty, it sets the field to `none`.
  An empty line leaves everything `PromptInfo` says about the last line as it was,
  as fish and zsh keep their status.
  A line that runs no process, like `1 2 +`,
  reports `none` instead of a failure from three lines earlier.
+ Before calling the prompt quote, it copies the field into `PromptInfo`.
  Prompt quotes often run processes themselves (`git`),
  which would otherwise overwrite the value.

A line can run several processes.
"Last" means the last one to finish;
an earlier failure in a `;` sequence is not reported.
`lastLineOk` is there for prompts that want to know whether the line as a whole succeeded.

= Text and newlines <newlines>

Every `promptText` (and `setWindowTitle`, and hyperlink text) is passed through `terminalSafeText(text, false)`:
every control byte, including `\r`, `\n` and `\t`, becomes visible caret notation,
C1 controls and invalid UTF-8 become `?`.
The only way to break a line is `promptNewline`.

- Process output usually ends in `\n`.
  `[git branch --show-current]` captured into the prompt would otherwise
  add a blank line silently; with sanitizing it shows as a visible `^J`.
  This matches how the current prompt treats a directory name.
- `\r` can never be allowed.
  It moves the cursor to column one without the layout knowing.
- The cost to the user is one extra item per line break, and it states the intent in the code.

Soft wrapping of long lines is already done by `layoutAtomsInto`.
The only line breaking the shell adds is the explicit one.

= Painting

The list is turned into what `paintPrompt` already understands:

+ Concatenate the sanitized text of the text items into one `SourceText`,
  writing `"\n"` for each `promptNewline`.
  Since text is sanitized, every `\n` in the source came from a `promptNewline`,
  and the existing `AtomHardBreak` handling does the rest.
+ Keep style items aside as a list of (byte offset, escape sequence).
  The paint loop writes each sequence when it reaches the first atom at or past that offset.

Style has no width, so the layout is unchanged.
`rowsToPromptTop` and `redrawPromptAfterResize` keep working on the saved `promptText`;
the style list is saved next to it so a resize repaints the same colors.

Details:

- Before each `\r\n` between rows, reset the background, and set the current style again after it.
  Otherwise, when the screen scrolls, the terminal fills the rest of the new line with the background color.
- The hard-coded magenta (`\033[35m`) goes away; a user prompt gets no default color.
- The trailing `\033[0m` stays, so the command line never inherits the prompt's style.
- Window title and hyperlink escapes are written outside the layout, like the OSC 7 report.

== What stays hard-coded

The OSC 7 / Windows Terminal working-directory report stays in `printPrompt`, outside the user's quote.
It is plumbing for the terminal, not appearance,
and no user should have to remember to keep it.
The `promptNewlineSequence` handling of output without a trailing newline also stays.

= Cursor shape <cursor>

Cursor shape is not prompt content.
It is a terminal mode: it persists into the command line and into every child program.
If the prompt sets it, the shell must reset it (DECSCUSR 0) before running an external command
and set it again afterward, or `less` and friends inherit it.

So it is a separate setting, made once in the init file:

```
enum CursorShape =
      blinkingBlock | steadyBlock
    | blinkingUnderline | steadyUnderline
    | blinkingBar | steadyBar
end

setCursorShape (CursorShape -- )
```

= Failure <failure>

If the quote has a runtime error, or returns an item the painter rejects (an RGB value of 300),
the shell paints the built-in prompt and prints the error.
It prints it once, not before every line, until the quote is replaced or succeeds again.

A slow quote, such as `git status` in a large repository, delays every Enter.
The quote gets 3 seconds.
If it has not returned by then, the shell cancels it the same way Ctrl-C would,
including the processes it started,
and paints the built-in prompt with a message saying the prompt took too long.
Ctrl-C while the quote runs does the same before the limit.
A process the quote runs owns the terminal, so Ctrl-C then reaches the process, not the shell;
a process ended by Ctrl-C (exit code -130) stops the quote too.
The evaluator checks for a stop before each item, and `sleep` wakes for it.
Like other failures, the message is printed once,
not again on every line while the quote keeps timing out.
"The same failure" means the same message: the runtime errors the quote gives are collected,
not printed, and compared with the last one shown.
A quote that finished with its list is shown even if the time ran out as it finished.

= Deferred: right-aligned content

The most requested feature not covered is right-aligned text
(fish's right prompt, Starship's `$fill`).
It conflicts with resize handling:
`rowsToPromptTop` lays the saved prompt out again at the new width,
and a fill's width depends on the terminal width.
Supporting it means storing the item list, not the flattened source, and laying it out again on each resize.

Nothing above prevents adding it later as another `PromptItem` member.

= Implementation checklist

- `PromptItem`, `Base16Color`, `BaseBrightness`, `TextAttribute`, `CursorShape` and `PromptInfo` built into the shell and the type checker.
- `setPrompt` and `setCursorShape` in `Evaluator.go`, `BuiltInList.go`, and the type checker with signatures that match the runtime exactly.
- Last exit code field on `EvalState`; REPL clears it per line.
- `PromptInfo` built by the REPL from the stack, the checker state and the last line.
- Painter: flatten items, style offsets, background reset at line breaks.
- Documentation in `doc/` (interactive mode only; not `mshell.md`), CHANGELOG entry.
