// Design: user-defined key bindings for the interactive shell.
// Build: typst compile ai/cli_bindings.typ

#set document(title: "Key bindings in mshell", author: "Claude (draft for Mitchell)")
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
  #text(size: 20pt, weight: "bold")[Key bindings in mshell]
  #v(0.3em)
  #text(size: 11pt)[Design draft --- 2026-10-18, branch `cli-bindings-design`]
  #v(0.2em)
  #text(size: 10pt, style: "italic")[Written by Claude from the code on `main` (e68153d), the abbreviation design (`ai/abbr.typ`), the rune design (`ai/runes.typ`),
  and a survey of shells, editors and keyboard firmware (section 3).]
]

#v(1em)

= Summary

The interactive shell has key bindings compiled into `HandleToken` in `mshell/Main.go`.
Some are standard line editing (Ctrl-A, Ctrl-K, Up), some are personal to one user
(`jf` is Enter, `vl` clears the screen, `;r` and `;j` run the definitions `r` and `j` from Mitchell's `init.msh`, `qq` exits, F1 runs `lf`).
None can be changed without rebuilding the shell, and the personal ones make letters impossible to type: no one can type `vlc`.

This design makes every binding data, declared in `init.msh` and checked like the prompt and the abbreviations:

```msh
{keys: [r'j' r'f'], action: [editAccept] bindingOps} bindingAdd
{keys: [r'v' r'l'], action: [editClearScreen] bindingOps, within: 150, idleBefore: 150} bindingAdd
{keys: [r';' r'r'], action: [(r) editRun] bindingOps} bindingAdd
{keys: [r'x' keyCtrl r'd' keyCtrl], action: (insertDate) bindingFunc} bindingAdd
```

A key is a typed value: a rune (`r'j'`, from `ai/runes.typ`), a named key (`keyEnter`), or either with modifiers (`r'x' keyCtrl`).
A misspelled key is a type error, found before `init.msh` runs.

Four ideas carry it. Each is argued from first principles in section 4.

+ *A binding returns a program for the editor, as data.*
  Its action is a list of `EditOp` values (a closed, built-in enum of editor verbs),
  or a quotation `(EditState -- Maybe[[EditOp]])` that computes one.
  The quotation never touches the editor; the editor runs what it returns, as it renders what the prompt quotation returns.
  So bindings are type checked at startup, testable in a script with no terminal, and composed by concatenating lists.
+ *Typing is never delayed.* A text key that begins a longer binding (the `j` of `j f`) is inserted at once.
  If the binding completes, the editor restores the line it had before the sequence began and runs the binding.
  The line is an immutable string, so restoring it is keeping a pointer: no timer, no lag, no copy.
+ *Effects wait; edits do not.* What a key does is either a _pure edit_ (it changes only the line and cursor, and can be undone by restoring them)
  or an _effect_ (it runs code, submits the line, clears the screen).
  Only pure edits are done provisionally. An effect never runs and is then taken back.
+ *Time is declared, not ambient.* A sequence has no time limits unless its binding gives them:
  `within` (the longest gap between its keys), `idleBefore` (quiet before its first key) and `idleAfter` (quiet after its last key, before an effect runs).
  Those are the signals keyboard firmware uses to tell a deliberate burst from typing (section 3.4).
  Time is the one input a test has to supply; everything else is a function of the keys and the line.

As far as the survey found, no shell does the second and third: fish, zsh, bash and PSReadLine hold the first key back,
and Nushell and Elvish have no multi-key bindings yet. Editors do it through plugins (better-escape.nvim, houdini.nvim, evil-escape, a Kakoune hook),
each patching the leaks of a tentative character by hand. Here it falls out of the line being immutable.

The defaults (Enter, Tab, Ctrl-A, the abbreviation triggers, ...) become ordinary entries in the same table, added before `init.msh` runs,
so every one of them can be listed, replaced or erased. The personal bindings move to Mitchell's `init.msh` (section 11).

= What exists today

`HandleToken` (Main.go) is a `switch` over the token types the terminal lexer (`InteractiveLexer`) produces:
`AsciiToken` (a byte below 128), `MutliByteToken` (one non-ASCII rune), `SpecialKey` (a closed list of 27 decoded keys), `PasteToken` and `CsiToken`.

#table(
  columns: (auto, 1fr, auto),
  inset: 4pt,
  table.header([*Keys*], [*Does*], [*Kind*]),
  [Ctrl-A / Home, Ctrl-E / End], [cursor to start / end], [standard],
  [Ctrl-B / Left, Right], [cursor left / right; in tab cycling, change column], [standard, modal],
  [Alt-B, Alt-F], [word left / right], [standard],
  [Ctrl-F], [accept the history suggestion at the end of the line, else right], [standard],
  [Ctrl-K, Ctrl-U], [delete to end / to start], [standard],
  [Ctrl-W, byte 8 (Ctrl-H, Ctrl-Backspace)], [delete the word before the cursor], [standard],
  [Backspace, Delete], [delete a grapheme], [standard],
  [Up, Down], [history; in tab cycling, previous / next match], [standard, modal],
  [Ctrl-P, Ctrl-N], [history prefix search; in tab cycling, previous / next match], [standard, modal],
  [Ctrl-Y], [accept the history suggestion], [standard],
  [Alt-.], [last argument of history, repeat to cycle], [standard],
  [Tab, Shift-Tab], [complete, cycle], [standard],
  [Enter], [accept a tab match while cycling; otherwise expand the abbreviation at the cursor and run], [standard, modal],
  [Space, `| ; < > ] )`], [expand the abbreviation before the cursor, then insert], [abbreviation trigger],
  [Ctrl-Space], [insert a space without expanding], [abbreviation],
  [Ctrl-L], [clear the screen], [standard],
  [Ctrl-C, Ctrl-D, Alt-O], [exit the shell], [standard (Ctrl-C is unusual)],
  [Ctrl-O], [run the built-in file manager in the current directory], [mshell],
  [Alt-Shift-R], [forget measured text widths], [mshell],
  [Alt-D], [insert today's date], [personal],
  [F1], [replace the line with `lf` and run it], [personal],
  [`j` `f`], [Enter], [personal],
  [`v` `l`], [clear the screen], [personal],
  [`q` `q`], [replace the line with `0 exit` and run it], [personal],
  [`;` `r`, `;` `j`], [replace the line with `r` (or `j`) and run it], [personal],
)

The two-key sequences are written out by hand, five times. For `j`:

```go
} else if t.Char == 'j' {
    token, err = state.readInputToken()   // blocks until the next key
    ...
    if t.Char == 'f' { return state.HandleToken(AsciiToken{Char: 13}) }
    state.PushChars([]rune{'j'})
    return state.HandleToken(token)
```

Problems, apart from being compiled in:

+ *The first key is invisible.* `readInputToken` blocks inside the handler, before any repaint, so a typed `j` does not appear until the next key arrives.
+ *The pairs cannot be typed.* There is no timeout and no escape: `vlc`, `qq`, `jfk`, and `;r` in a line are impossible to enter.
+ *They destroy the line.* `;r`, `;j` and `qq` set `currentCommand` to `""` before running, so whatever was typed is lost, and `r` goes into history.
+ *They assume one user's `init.msh`.* On any other machine `;r` runs `r`, which is not defined.
+ *Context is scattered `if`s.* Nine keys test `tabCycleActive` themselves; `;` has to call `typeAbbrTrigger` by hand so that it still triggers abbreviations.
+ *Keys are lost.* The lexer decodes only the keys someone has bound: modifiers on Home and End are dropped, Ctrl-Delete is decoded and then ignored,
  most Alt chords and a lone Escape are discarded (`Ignoring lone ESC`), and Escape waits for the following byte with no time limit.
+ *Nothing documents them*, except a partial list in `doc/mshell.md`.

= Prior art

Two surveys, done for this design in October 2026, read the documentation and, where it was vague, the source:
fish (commit 47a8719), PSReadLine, prompt_toolkit, reedline, ble.sh, Vim and Neovim, Helix, better-escape.nvim, houdini.nvim, evil-escape, key-chord.el,
QMK, keyd, kanata and tmux. zsh 5.9, bash 5.3, Nushell 0.115, Elvish 0.21 and IPython 9.17 were also driven through a pty.
What is marked _unverified_ there is left out here or said to be so.

== Shells

*fish 4.9.* `bind ctrl-x,ctrl-e edit_command_buffer`. Keys are comma separated; modifiers `ctrl- alt- shift- super-`;
named keys include `enter tab space escape backspace delete home end pageup pagedown f1`–`f12 minus comma`; `alt-W` is the same as `alt-shift-w`, and `shift-A` is refused.
A binding is any mix of about 111 named input functions and fish code; the code reads and changes the line through the `commandline` builtin
and must call `repaint` itself. `-M mode` and `-m newmode` give modes; vi counts (4.4) are built from modes alone.
`fish_escape_delay_ms` is 30 ms; `fish_sequence_key_delay_ms` is *infinite* by default (\#7401). The first key of a sequence is held back and not drawn.
User bindings beat preset ones, and erasing a user binding brings the preset back. Bad keys are refused at `bind` time.
fish asks for the kitty keyboard protocol with flags 1 and 4 (`CSI = 5 u`), and carries a long list of per-terminal workarounds (Konsole, old iTerm, WezTerm's modifyOtherKeys, layouts).

*Nushell / reedline.* Records in `$env.config.keybindings`: `{name, modifier: control_alt, keycode: char_e, mode, event: {send: OpenEditor}}`, or `{edit: insertstring, value: ...}`, or a list of events.
There is no multi-key sequence at all. Two pull requests for one (a trie, and a resolver with a timeout, motivated by `jj`) were closed in September 2026 in favor of
design issue reedline \#1204, "one shared resolver over a trie", in which a key may be both a binding and a prefix and the mode supplies the fallback.
Code bound with `executehostcommand` changes the line with `commandline edit`. The kitty protocol is off by default.

*Elvish 0.21.* `set edit:insert:binding[Ctrl-L] = { ... }`: a map per mode (insert, completion, navigation, history, ...) from a key to a function.
No multi-key bindings (\#730, open since 2018). The cursor, `$edit:-dot`, is a byte index.

*zsh.* Widgets are functions registered with `zle -N`; they read and write `BUFFER`, `LBUFFER`, `RBUFFER` and `CURSOR`, and read `KEYS` and `KEYMAP`.
Keys are raw byte strings (`'^Xd'`, `'jk'`) with no symbolic names. `KEYTIMEOUT` (0.4 s) applies only when the prefix is itself bound; an unbound prefix waits forever.
In the pty, `j` with `jk` bound in `viins` did not appear until the timeout. `bindkey '^Xz' nosuchwidget` succeeds and fails only at the key.

*bash / readline.* `"\C-x\C-r": re-read-init-file` in `.inputrc`; `bind -x` runs shell code that changes `READLINE_LINE` and `READLINE_POINT`.
`keyseq-timeout` is 500 ms; the first key is held back. Errors are silent: an unknown function returns 0, and `set keyseq-timeout banana` becomes 0, which means wait forever.

*PowerShell PSReadLine.* `Set-PSReadLineKeyHandler -Chord 'Ctrl+x,Ctrl+e' -ScriptBlock { ... }`, where the block calls a static API
(`GetBufferState`, `Insert`, `Replace`, `AcceptLine`). Custom chords are at most two keys, and the second is read with a blocking `ReadKey`, with no timeout.
Issue \#1701 (open since 2020) asks that binding `j,k` not make `j` untypeable; the community workaround inserts `j` and deletes it if `k` comes within a second.

*prompt_toolkit (IPython, xonsh).* `@kb.add('j', 'k', filter=vi_insert_mode)`: Python handlers with full access to the buffer; composable `Condition` filters;
`eager` to opt out of waiting; among matches the last added wins. `timeoutlen` is 1 s. While a sequence is pending, it draws the pending key at the cursor as a
"phantom" cell that is not in the buffer: the only shell that shows the first key at all.

*murex.* `event onKeyPress name={F4} { $EVENT_RETURN.Actions = %[ Undo Undo ] }`: the handler is given the line and cursor as JSON and *returns* a structure
(`Actions`, `SetLine`, `SetCursorPos`, `Continue` to fall through). This is the nearest precedent for a binding that returns what to do instead of doing it.

*Atuin.* `[keymap.emacs]` tables in TOML: `"ctrl-alt-x"`, sequences separated by spaces (`"g g"`), `"G"` for shift-g, and
rule lists with conditions (`{ when = "cursor-at-start", action = "exit" }`, the first matching rule wins).

== Editors

*Emacs.* A keymap is a trie of keymaps; a key bound to a keymap is a prefix. A key cannot be both a command and a prefix, so a prefix never needs a timeout.
Commands are named, and `[remap cmd]` rebinds every key that runs a command. Since Emacs 29, `keymap-set` takes strings checked by `key-valid-p`:
keys separated by single spaces, modifiers in a fixed order (`A-C-H-M-S-s`). Precedence is an ordered list of maps (overriding, emulation, minor modes, local, global);
`set-transient-map` gives a map for the next key or a few. which-key (built in since 30.1) shows the possible next keys after 1 s.
*key-chord.el* binds two keys pressed within 0.1 s (0.2 s for the same key twice), holding the first back with `read-event`;
fast typing gives false positives, and version 0.8 added "typing detection": after two keys less than 0.1 s apart, chords are off until 0.5 s of idle.

*Vim and Neovim.* `inoremap jk <Esc>` holds `j` for `timeoutlen` (1000 ms); Vim draws the pending character at the cursor without inserting it,
so anything that depends on the buffer lags. `ttimeoutlen` (Neovim 50 ms) is the separate, short wait for terminal codes.
*better-escape.nvim* removes the lag by mapping `j` to insert itself at once and, when `k` follows within the timeout, feeding `<BS>` and the action.
Its leftovers are the ones a tentative character always has: the redo buffer holds `j<BS>`, the modified flag must be put back by hand, and recorded macros replay with no timing.
*houdini.nvim* also drops the undo entry and edits recorded macros so replay cannot fire; *evil-escape* does the same dance in Emacs and turns off dot-repeat.

*Kakoune* does it with no timer: a hook on every inserted `k` checks whether the two characters before the cursor are `jk`, deletes them, and escapes.
"There is no delay to insert j." The cost: `jk` always escapes, typed a minute apart or not.

*Helix.* `[keys.insert] j = { k = "normal_mode" }`, a trie (`KeyTrie`) of named commands. There is no timeout: a pending `j` waits forever with a popup.
When a sequence fails in insert mode, the pending keys are inserted without being matched again, so with `jf` bound, `j j f` gives `jjf` (\#2612, open; \#8486 would rescan).

*VS Code.* Rules `{key: "ctrl+k ctrl+c", command, when}`, read from the bottom; `-command` removes a default, `when` is a boolean expression over context.
A pending chord shows "(Ctrl+K) was pressed. Waiting for second key of chord..." in the status bar and gives up after 5 s.

== Terminals

*tmux* has key tables: `switch-client -T table` makes the next key come from that table, which builds any sequence; `escape-time` is 10 ms.
It does not yet speak the kitty keyboard protocol (\#5615 is open, and discards key releases).
*WezTerm* has a leader key with a timeout and a stack of key tables (`one_shot`, `until_unknown`).

*The kitty keyboard protocol* is pushed with `CSI > flags u`, popped with `CSI < u` and queried with `CSI ? u`.
Flag 1 makes Escape, Alt chords and Ctrl-I, Ctrl-M, Ctrl-H distinct from Escape, Tab, Enter and Backspace; flag 4 reports the key's place on a US layout,
so `ctrl-w` works on a Cyrillic layout; flag 2 reports releases, but for keys that type text only together with flag 8, which turns every key into an escape code.
It is listed as implemented by kitty, alacritty, foot, ghostty, iTerm2, Windows Terminal, rio, WezTerm (when enabled), Warp and xterm.js;
not by tmux, and fish turns it off on Konsole for bugs.

== Keyboard firmware

Firmware sees presses and releases with millisecond times, and has had to tell chords from typing for years.

- *Window between keys.* QMK `COMBO_TERM` 50 ms, ZMK `timeout-ms` 50, keyd `chord_timeout` 50. QMK buffers the keys while it waits; kanata's "zippychord" types eagerly and erases instead.
- *Prior idle.* ZMK `require-prior-idle-ms`: "If any non-modifier key is pressed within require-prior-idle-ms before a key in the combo, the combo will not trigger."
  kanata has `chords-v2-min-idle`, QMK "Flow Tap" (150 ms recommended) treats a key pressed soon after another as typing. This is the strongest of the signals.
- *Release order.* keyd abandons a chord when any key is released. It is the weakest signal in practice: fast typists overlap 40--70% of keystrokes (Dhakal et al., CHI 2018).
- *Context.* QMK `combo_should_trigger`, per layer.

The same study gives the numbers to set windows against: a mean gap between keys of 239 ms, about 120 ms for fast typists, and a floor near 60 ms.

== What this design takes, and leaves

#table(
  columns: (1fr, 1fr),
  inset: 4pt,
  table.header([*Taken*], [*Left, and why*]),
  [Command names separate from keys; prefix and command cannot share a key (Emacs, Helix, VS Code)], [Raw byte notation (zsh, readline): it names bytes, not keys],
  [Keys checked strictly, before any key is pressed (Emacs 29, fish)], [Key strings in any notation (Emacs, VS Code, Atuin, fish): a misspelled key is found only when the string is parsed at run time. Typed keys are found by the checker.],
  [Insert at once, take back on completion (better-escape, Kakoune, zippychord)], [Holding the key back (fish, zsh, bash, PSReadLine, key-chord) and phantom keys (Vim, prompt_toolkit): both lag what depends on the line],
  [Matching the rest again when a sequence fails], [Helix's literal insert of the failed keys (\#2612)],
  [Window, prior idle, and quiet after, as declared fields (firmware)], [Release order, until terminals and tmux report it],
  [A binding returns what to do (murex), typed], [A binding that calls into the editor (fish `commandline`, zsh widgets, PSReadLine): untyped, and hard to test],
  [Declining, to fall through (murex `Continue`, Atuin rule lists)], [`when` expressions over named context (VS Code): `none` from a quotation does the same with the full language],
  [Showing a pending prefix (Emacs echo area, VS Code status bar)], [—],
  [Two delays: a short escape delay, sequence times only if asked (fish)], [Vim's 1 s default],
  [Refusing bad bindings when they are added (fish, Emacs, PSReadLine)], [Silent acceptance (readline, zsh)],
)

= Principles

#principle(1, "Keys are events; text is data.")[
A key press is not a character. The terminal delivers key presses, and separately text that did not come from one key: pasted text, and text an input method composed.
Bindings see key presses only. Pasted text never passes through a binding (this is already an invariant of `PasteToken`).
A key that types text has a default action, which is to insert that text; binding the key replaces that action.
]

#principle(2, "The keymap is data; the editor's verbs are names.")[
_Where_ (a key sequence) and _what_ (an editor verb) are separate, as in Emacs, where any command can be put on any key.
The verbs are a closed enum, `EditOp`, so a misspelled verb is a type error, and the set of things a binding can do is written down in one place.
The default keymap is entries in the same table, not code: there is no binding the user cannot see, replace, or remove.
]

#principle(3, "A binding computes; the editor acts.")[
A binding's action is a program for the editor: a list of `EditOp`, or a quotation that returns one from a snapshot of the editor (`EditState`).
This is how `setPrompt` already works: the quotation returns `[PromptItem]` and the shell renders it.
The quotation cannot reach into the editor, so it can be type checked against one signature, run in a script with a record made by hand,
and given a time limit without leaving the editor half changed. A list of ops is a concatenative program: two bindings compose by `+`.
]

#principle(4, "Never wait to show what was typed.")[
When a text key begins a longer sequence, its own action is done at once. If the sequence completes, the editor goes back to the line it had before the sequence and runs the binding instead.
The cost of a multi-key binding is paid only when it is used, never by typing that merely resembles it.
]

#principle(5, "An effect is never taken back.")[
Undoing is only possible for what the editor owns: the line and the cursor.
Running code, submitting a line, clearing the screen or leaving the shell cannot be undone, so they are never done provisionally.
A key whose action is an effect waits for its sequence to be decided.
]

#principle(6, "Time is a declared input.")[
Given the keys and the line, the result is fixed. Time enters only through three optional fields of a binding
(`within`, `idleBefore`, `idleAfter`, section 6.4) and through the terminal's own escape delay (section 10). A test that gives the arrival times gets the same result every run.
]

#principle(7, "A key is a typed value; how it is spelled does not matter.")[
A key is one rune, a named key, or a key with modifiers, and the type checker refuses anything else, a misspelled word included.
The order of modifiers, a repeated modifier, `r'J'` against `r'j' keyShift`: `bindingAdd` turns each key into one key code, so two spellings of a key are the same key.
No user compares keys; the shell does, on the codes.
]

#principle(8, "The same machinery as abbreviations.")[
Records with optional fields for independent settings, enums for choices; add, erase and list builtins; entries that cannot work are kept but inert, with a line on stderr;
quotations run by `runUserQuote` with a time limit; `none` declines.
A user who has written an abbreviation already knows how to write a binding.
]

#principle(9, "Bounded and allocation free on the key path.")[
A sequence is at most 8 keys. A key that matches no binding is decided with fixed-size state and no allocation, as every key is today (`TestKeyPathAllocations`).
]

= The design

== Keys

A key sequence is a list of keys, at most 8. A key is one of three things:

```msh
enum NamedKey =
      keyEnter | keyTab | keyBackspace | keyEscape | keyDelete | keyInsert
    | keyUp | keyDown | keyLeft | keyRight | keyHome | keyEnd | keyPageUp | keyPageDown
    | keyF1 | keyF2 | keyF3 | keyF4 | keyF5 | keyF6 | keyF7 | keyF8 | keyF9 | keyF10
    | keyF11 | keyF12 | keyF13 | keyF14 | keyF15 | keyF16 | keyF17 | keyF18 | keyF19 | keyF20
    | keyF21 | keyF22 | keyF23 | keyF24 | keyF25 | keyF26 | keyF27 | keyF28 | keyF29 | keyF30
    | keyF31 | keyF32 | keyF33 | keyF34 | keyF35
end
enum ModifiedKey = keyCtrl Key | keyAlt Key | keyShift Key | keySuper Key end
type Key = rune | NamedKey | ModifiedKey
```

- *A rune* is a key that types text, named by the text: `r'j'`, `r'J'`, `r';'`, `r'é'`, `r' '` (the space bar).
  The `rune` type and its literals, `r'a'` and `U+001B`, are designed in `ai/runes.typ`.
- *A named key* is a key that types no text.
- *A modified key* wraps a key in a modifier, postfix like every other constructor: `r'x' keyCtrl` is Ctrl-X,
  `r'r' keyShift keyAlt` is Alt-Shift-R, `keyLeft keyAlt keyCtrl` is Ctrl-Alt-Left.

The member names start with `key` because enum member names are global.
Inside a list literal the constructors run as they do anywhere, so a sequence reads `[r'x' keyCtrl r'e' keyCtrl]`.

In the prose of this document a sequence is still written short, keys separated by spaces: `j f` for `[r'j' r'f']`, `ctrl-x ctrl-e` for `[r'x' keyCtrl r'e' keyCtrl]`.

=== Why runes

The keymap is a trie, and what it branches on is the rune, not the grapheme cluster or the byte.
A grapheme cluster ends only when the _next_ code point shows it cannot join (UAX \#29), so matching on clusters would mean waiting for a key that has not been pressed, against P4.
A rune is complete when its last byte arrives.
The terminal works the same way: the legacy lexer yields one rune per `MutliByteToken`, and the kitty protocol reports a text key by its code point.

Two consequences:

- A key press can type several runes (an input method, a compose key, an emoji with a variation selector).
  They arrive in one read, with gaps of zero, and the trie sees them as that many keys. Nothing special is needed, and a binding on `e` fires before a combining mark that follows it.
- A rollback restores the line as it was, as a string (section 6.2), so a provisional rune that joined the grapheme before it is removed exactly.

=== Normal form

Spelling is free (P7). `bindingAdd` turns each key into a 32-bit key code: a code point, or a named key numbered above U+10FFFF, and four modifier bits.

- Modifiers are a set: their order does not matter and a repeat counts once.
- A letter with Shift and no other modifier is the shifted letter: `r'j' keyShift` is `r'J'`.
  For a non-letter the shell cannot know what Shift types on the user's layout, so `r';' keyShift` alone is inert, and the message says to write the character (`r':'`).
- With Ctrl, Alt or Super, a letter is its lowercase key plus Shift: `r'W' keyAlt` is `r'w' keyShift keyAlt`, as in fish.
  A key with Ctrl or Alt types no text, so the rune names the key, and when the terminal reports alternate keys (section 10) it is the key in the same place on a US layout, so Ctrl-W works on a Cyrillic layout.

#decision[
*Keys are typed values, not strings.*
The first draft wrote sequences as strings (`'ctrl-x ctrl-e'`), as Emacs 29, VS Code, Atuin and fish do, and checked them when `init.msh` ran.
Strings have one hole that matters in mshell: a bare word in a list literal is a string, so any typo inside a key list type checks as text.
With `rune` instead of `str` in `Key`, `[r'x' ctlr]` is a `[rune | str]`, which is not a `[Key]`: the checker refuses it before anything runs.
A rune also cannot hold `'jf'` or nothing, so "a text key is exactly one character" is a fact of the type, not a check.

Three other forms were rejected:

- *`keyText str` in place of `rune`.* It closes the typo hole, but only by wrapping a string; the rune is the thing a key is.
- *Layered types that fix the order of modifiers* (`K0` ... `Key`, so that `keyCtrl` can only wrap what `keyAlt` cannot).
  They make one spelling per key a fact of the type, which nothing needs: equality of keys is decided in Go, on codes.
- *A record of a base key and a set of modifiers.* Longer to write than the constructors, for the same information.
]

=== What the terminal can tell apart

Keys are logical keys, not bytes. Which of them a terminal can report depends on its keyboard protocol (section 10).
In the legacy encoding, which every terminal speaks, the bytes are read as:

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Bytes*], [*Key*]),
  [`0x00`], [`r' ' keyCtrl`],
  [`0x01`--`0x1a`], [`r'a' keyCtrl` ... `r'z' keyCtrl`, except the four below],
  [`0x08`], [`r'h' keyCtrl` (many terminals send it for Ctrl-Backspace too)],
  [`0x09`], [`keyTab` (never `r'i' keyCtrl`)],
  [`0x0d`], [`keyEnter` (never `r'm' keyCtrl`)],
  [`0x0a`], [`r'j' keyCtrl`],
  [`0x1b`], [`keyEscape`, when no byte follows within the escape delay (section 10)],
  [`0x1c`--`0x1f`], [`r'\' keyCtrl`, `r']' keyCtrl`, `r'^' keyCtrl`, `r'_' keyCtrl`],
  [`0x7f`], [`keyBackspace`],
  [`ESC` then a byte], [that key with `keyAlt`: `ESC x` is `r'x' keyAlt`, `ESC X` is `r'x' keyShift keyAlt`, `ESC .` is `r'.' keyAlt`],
  [`CSI` / `SS3` sequences], [named keys, with modifiers from the parameter (`CSI 1;5C` is `keyRight keyCtrl`, `CSI 1;2H` is `keyHome keyShift`)],
  [any other UTF-8], [one rune per code point],
)

A binding for a key the terminal cannot send, such as Ctrl-I or Ctrl-Shift-X in the legacy encoding, is valid and simply never fires there.
`bindingAdd` does not warn about it, since the same `init.msh` is used in terminals that can.

== Types

Added to `builtinDeclSource` next to the prompt's and the abbreviations' types, so the names are reserved, together with `NamedKey`, `ModifiedKey` and `Key` from section 5.1.

```msh
enum EditMode = modeInsert | modeComplete end

type EditState = {before: str, after: str, keys: [Key], mode: EditMode}

enum EditOp =
    # Pure edits: they change only the line and the cursor.
      editInsert str          # insert this text at the cursor (no abbreviation, no binding)
    | editSelfInsert          # insert the text of the last key of the sequence
    | editSetLine str str     # the line becomes before + after, with the cursor between them
    | editLeft | editRight | editWordLeft | editWordRight | editLineStart | editLineEnd
    | editBackspace | editDelete | editDeleteWordLeft | editDeleteToStart | editDeleteToEnd
    | editExpandAbbr          # expand the abbreviation that ends at the cursor, if one applies
    # Effects.
    | editAccept              # expand the abbreviation at the cursor, run the line (last)
    | editExit                # leave the shell (last)
    | editRun ( -- )          # run this quotation with the terminal; keep the line
    | editRunOps (EditState -- Maybe[[EditOp]])  # the same, then run the ops it returns
    | editForward             # right, or accept the history suggestion at the end of the line
    | editAcceptSuggestion
    | editHistoryPrev | editHistoryNext
    | editHistorySearchBack | editHistorySearchForward
    | editLastArg
    | editComplete | editCompleteBack
    | editCompleteNext | editCompletePrev | editCompleteLeft | editCompleteRight
    | editCompleteAccept
    | editClearScreen
    | editRemeasure           # forget measured text widths (today's Alt-Shift-R)
    | editMessage str         # show a line below the command line
end

enum BindingAction =
      bindingOps [EditOp]                          # always this program
    | bindingFunc (EditState -- Maybe[[EditOp]])   # the program to run, or none to decline
end

type Binding = {
    keys: [Key],               # [r'x' keyCtrl r'e' keyCtrl], [r'j' r'f'], [keyF1]
    action: BindingAction,
    mode?: EditMode,           # modeInsert when missing
    within?: int,              # most ms between two keys of the sequence
    idleBefore?: int,          # least ms with no key before the first key
    idleAfter?: int,           # least ms with no key after the last key, before an effect runs
    description?: str,         # given back by bindingList
}
```

`EditState` is the editor as the binding sees it:

- `before` and `after`: the line, split at the cursor.
- `keys`: the keys of the sequence that ran the binding, in normal form (section 5.1), so one quotation can serve several keys.
- `mode`: the keymap the sequence was found in (section 8).

`EditState` comes before `EditOp` because `editRunOps` names it.

#decision[
*The cursor is where the line splits, not a number.*
An offset would need a unit, and mshell strings do not have one obvious unit: `len`, indexing and `take` count bytes, though the docs describe the last two in characters (`ai/runes.typ`, section 11).
Shells disagree: Elvish's `$edit:-dot` counts bytes, PSReadLine's cursor is an index into a .NET string (UTF-16 units), and none of them counts what the terminal shows (graphemes).
`before` and `after` avoid the question: a binding that inserts writes `[@s :before? 'x' + @s :after? editSetLine]`; one that looks at the word before the cursor reads the end of `before`.
zsh offers the same split as `LBUFFER` and `RBUFFER` beside `BUFFER`, and most of its widgets use those.
If a binding splits the line inside a grapheme (`before` ends in the middle of one), the cursor moves forward to the end of that grapheme, as it does after any edit.
]

#decision[
*Two kinds of action, because one of them can be read before it runs.*
`bindingOps` is a fixed program. The editor can look at it when it is added: whether it is a pure edit (section 6.2), whether `editAccept` is last.
`bindingFunc` is computed at the key, so nothing is known about it in advance; it is always treated as an effect.
Making every action a quotation (`(drop [editAccept] just)`) would lose that, and would run the evaluator for Enter and every other default key.
The enum is used for the same reason as `AbbrExpansion` (`ai/abbr.typ` 5.1): a quotation literal given as an enum payload is checked against the payload's type, so `(:before? ...) bindingFunc` needs no `as`.
]

#decision[
*`editRun` takes `( -- )`, and `editRunOps` returns its result, so a binding cannot change the stack.*
The interactive shell keeps a stack between lines. A binding runs out of band, and a user would not expect a key to push or pop values;
the signature makes that a type error rather than a rule. The quotation runs on its own empty stack, and what it leaves is checked to be nothing.
To work on the stack, a binding submits a line: `['drop' '' editSetLine editAccept]` behaves exactly as if `drop` were typed and Enter pressed.
]

#decision[
*Accepting and exiting end the program.*
`editAccept` and `editExit` start something the program cannot follow: a new line, or no shell.
In a `bindingOps` program an op after either makes the binding inert. In a program a `bindingFunc` returns, the ops after are ignored and a message says so.
]

== Builtins

#table(
  columns: (auto, auto, 1fr),
  inset: 4pt,
  table.header([*Word*], [*Signature*], [*Does*]),
  [`bindingAdd`], [`(Binding -- )`], [Adds a binding. One with the same keys, after normalizing (section 5.1), and the same `mode` is replaced; there is at most one binding per sequence per mode.],
  [`bindingErase`], [`([Key] -- )`], [Removes the bindings with these keys, in every mode, defaults included. A text key then types itself again; any other key does nothing. Erasing a missing binding is not an error.],
  [`bindingList`], [`( -- new [Binding])`], [Every binding in force, oldest first, so the defaults come first, with `keys` as they were written. `mode` is always present.],
)

As with the abbreviation builtins, none fails on a value of its type, since `init.msh` is loaded for scripts too.
A binding that can never fire is added, listed, and *inert*; when the shell is interactive `bindingAdd` writes one line to stderr naming its `keys` and the reason:

- `keys` is empty or has more than 8 keys, or has Shift alone on a rune that is not a letter (section 5.1);
- `within`, `idleBefore` or `idleAfter` is zero or negative;
- a `bindingOps` program has an op after `editAccept` or `editExit`;
- it conflicts with a binding in the same mode that is not inert (section 6.3). The newer one wins and the older one becomes inert, so a user's binding always beats a default.

== The default keymap

The shell adds these before `init.msh` runs. They reproduce today's standard and mshell-specific keys; the personal ones are left out (section 11).

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Keys* (mode `modeInsert`)], [*Action* (`bindingOps`)]),
  [`keyEnter`], [`[editAccept]`],
  [`r' '`, `r'|'`, `r';'`, `r'<'`, `r'>'`, `r']'`, `r')'`], [`[editExpandAbbr editSelfInsert]`],
  [`r' ' keyCtrl`], [`[' ' editInsert]`],
  [`keyTab`, `keyTab keyShift`], [`[editComplete]`, `[editCompleteBack]`],
  [`keyLeft`, `r'b' keyCtrl`, `keyRight`], [`[editLeft]`, `[editLeft]`, `[editRight]`],
  [`r'f' keyCtrl`], [`[editForward]`],
  [`r'b' keyAlt`, `r'f' keyAlt`], [`[editWordLeft]`, `[editWordRight]`],
  [`keyHome`, `r'a' keyCtrl`, `keyEnd`, `r'e' keyCtrl`], [`[editLineStart]` ×2, `[editLineEnd]` ×2],
  [`keyBackspace`, `keyDelete`], [`[editBackspace]`, `[editDelete]`],
  [`r'w' keyCtrl`, `r'h' keyCtrl`, `keyBackspace keyCtrl`], [`[editDeleteWordLeft]`],
  [`r'u' keyCtrl`, `r'k' keyCtrl`], [`[editDeleteToStart]`, `[editDeleteToEnd]`],
  [`keyUp`, `keyDown`], [`[editHistoryPrev]`, `[editHistoryNext]`],
  [`r'p' keyCtrl`, `r'n' keyCtrl`], [`[editHistorySearchBack]`, `[editHistorySearchForward]`],
  [`r'y' keyCtrl`], [`[editAcceptSuggestion]`],
  [`r'.' keyAlt`], [`[editLastArg]`],
  [`r'l' keyCtrl`], [`[editClearScreen]`],
  [`r'o' keyCtrl`], [`[('.' mshFileManager) editRun editClearScreen]`],
  [`r'e' keyAlt`], [`[(editInEditor) editRunOps]` (section 7.4)],
  [`r'R' keyAlt`], [`[editRemeasure]`],
  [`r'c' keyCtrl`, `r'd' keyCtrl`, `r'o' keyAlt`], [`[editExit]`],
)

Each entry is one key, so its `keys` is a list of one: `{keys: [keyEnter], action: [editAccept] bindingOps}`.

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Keys* (mode `modeComplete`)], [*Action*]),
  [`keyTab`, `keyDown`, `r'n' keyCtrl`], [`[editCompleteNext]`],
  [`keyTab keyShift`, `keyUp`, `r'p' keyCtrl`], [`[editCompletePrev]`],
  [`keyLeft`, `keyRight`], [`[editCompleteLeft]`, `[editCompleteRight]`],
  [`keyEnter`], [`[editCompleteAccept]`],
)

Ctrl-O is the one default that is mshell code, and it shows why the ops compose:
the file manager is an ordinary builtin run by `editRun`, and clearing the screen afterwards is one more op, not a special case in the handler.

= Resolving key sequences

== What the result is

The editor's behavior is defined declaratively first; the algorithm in section 6.5 must agree with it.

Let the keys be $k_1, k_2, dots$ with arrival times $t_1, t_2, dots$, all in one mode.
A binding with sequence $s$ of length $n$ *completes at key* $i$ when

+ $k_(i-n+1) dots k_i = s$;
+ each gap $t_(j+1) - t_j$ in that range is at most the binding's `within`, if it has one;
+ the gap before its first key, $t_(i-n+1) - t_(i-n)$, is at least its `idleBefore`, if it has one (the first key of all has no gap before it, and passes);
+ the gap after its last key, $t_(i+1) - t_i$, is at least its `idleAfter`, if it has one (no next key passes);
+ none of $k_(i-n+1) dots k_(i-1)$ belongs to a binding that completed earlier;
+ it is not declined (a `bindingFunc` that returns `none`).

At each key, of the bindings that complete there the *longest* is used. Then:

- every key that is part of a used binding has the effect of that binding only, applied to the line as it was before the binding's first key;
- every other key has the effect of its own one-key action: its binding of length one, or, for a text key without one, inserting its text;
  a key that is neither (an unbound `ctrl-x`) does nothing.

The longest wins because its first keys were typed, and shown, on the way to it: in `x ; r` with bindings `; r` and `x ; r`, the user typed `x` first.
A conflict rule (section 6.3) makes "longest" unique, since two bindings that complete at the same key with the same length have the same keys.

== Pure edits act at once

An op list is a *pure edit* when every op in it is one of the pure edits of `EditOp` (the first group in section 5.2). A text key with no binding is a pure edit (`[editSelfInsert]`).

When a key may begin or continue a longer binding, and its own action is a pure edit, the editor:

+ keeps the line as it is before the key (a *snapshot*: two string headers, since the line's text lives in `lineArena` and is never written again);
+ does the key's own action, and repaints;
+ remembers the key as *pending*, with its time.

When a binding then completes, the editor goes back to the snapshot taken before its first key and runs the binding on it.
`EditState` is made from that snapshot, so the quotation of `j f` sees the line without the `j`.
If it declines, nothing is restored: the pending keys stay done, and the last key does its own action.

#note[
*Why this needs no timer.* Nothing is waiting: every key that has been typed is already on the screen.
`within` is checked when the next key arrives, by comparing two times; if the gap is too long the sequence simply is not complete.
better-escape.nvim does the same for `jk` in Neovim's insert mode, which is why it has no lag where `inoremap jk <Esc>` does (section 3.2).
Those plugins then have to keep the tentative character out of the undo history, the modified flag, the redo buffer and macros.
Here a rollback restores the whole of what a pure edit can change, so there is nothing to patch:
the history suggestion, the highlighting and the abbreviation lookup are all computed from the line, and the line is back.
The one thing a provisional key changes outside the line is that an edit resets a history prefix search (Ctrl-P), which is harmless.
When the editor has undo, a rollback removes the provisional step instead of adding one, as houdini.nvim does.
The abbreviation triggers are pure edits too, so in `; r` the `;` expands the word before it and is inserted at once; if `r` follows, both are taken back.
]

== Effects wait, and conflicts

A key whose own action is an effect (an unbound named key counts as doing nothing, which is not an effect) cannot be done provisionally.

- If its sequence is a proper prefix of another binding's, the two *conflict*: either the effect would run and then be wrong, or the editor would have to wait to know.
  Emacs refuses the same thing ("Key sequence ... starts with non-prefix key"). The newer binding wins and the older becomes inert (section 5.3).
  So binding `ctrl-x ctrl-e` makes a binding of `ctrl-x` alone inert, and the reverse.
- A pending key with no action of its own (an unbound `ctrl-x` that begins `ctrl-x ctrl-e`) shows nothing in the line.
  The editor shows the pending keys below the line (`ctrl-x -`), as Emacs does in its echo area, and waits for the next key without a time limit, unless the binding has `within`.
  Escape or `ctrl-g`, unless bound in the sequence, cancels a pending sequence of such keys.

Pure edits never conflict with longer sequences: `space` is bound to `[editExpandAbbr editSelfInsert]`, and `space space` can still be bound.

Two things make an effect wait after its last key, and they wait the same way:

- `idleAfter` (section 6.4): the effect runs only if no key comes for that long.
- An *overlap*: a binding that completes while a longer one is still possible, as with `; r` while `x ; r q` is pending after `x ; r`.
  The `; r` effect is held until the longer one completes or fails: at the next key, or when `x ; r q`'s `within` runs out.
  `bindingAdd` warns when it creates an overlap.

These are the only places where the editor acts on time with no key arriving, so it reads with a deadline, as the file manager already does for terminal replies (`ai/terminal-query`).
While it waits, the keys are on the screen as typed; only the effect is late, by an amount the binding declared.

== Time

A deliberate sequence of letters is a *burst*: its keys come close together, with quiet before and after.
Typing has bursts too, so no rule is sure; but firmware has found which signals work (section 3.4), and each is one optional field, in milliseconds:

#table(
  columns: (auto, 1fr, auto),
  inset: 4pt,
  table.header([*Field*], [*The binding completes only if*], [*Firmware*]),
  [`within`], [every gap between two of its keys is at most this], [`COMBO_TERM`, `chord_timeout`],
  [`idleBefore`], [no key came for at least this long before its first key], [`require-prior-idle-ms`, Flow Tap],
  [`idleAfter`], [no key comes for at least this long after its last key], [--- (a shell's addition)],
)

A binding with none of them has no time limits: `j`, a coffee, then `f` is still Enter, which is what happens today, and what fish does by default.

- `within` makes the keys typeable as text by pausing between them: with `within: 150`, `v`, a pause, `l` is `vl`.
- `idleBefore` stops a sequence in the middle of a word: fast typists' keys come about 120 ms apart (section 3.4), so with `idleBefore: 150` the `v l` in `devlog` does not fire.
  It also means a sequence typed straight after a command (`git status -u` then `j f`) does not fire unless there was a pause, so it suits keys that start something more than keys that end it.
- `idleAfter` stops a sequence that continues into a word: with `idleAfter: 100` on `j f`, typing `jfk` quickly gives `jfk`, and only `j f` followed by a pause submits.
  It delays the effect by that much, never the text.

Measured numbers to set them by (Dhakal et al., 136 million keystrokes): a mean gap of 239 ms, about 120 ms for fast typists, a floor near 60 ms.
For a two-letter sequence, `within` 100--150 with `idleBefore` or `idleAfter` around 150 is a reasonable start; firmware uses 50 ms for keys pressed together.
None of this is sure. The only sure way is to choose sequences that do not occur in what you type, and `;r` is a better choice in mshell than in a POSIX shell, where `cmd;rm` is common.

Each key's time is when it was read. Keys that arrive in the same read get the same time, so a paste in a terminal without bracketed paste
has gaps of zero: it passes `within` but fails `idleBefore` and `idleAfter`, which is a second reason to set one of them.
Over ssh, jitter can stretch or squeeze gaps, so values under about 100 ms are not reliable there.

== The algorithm

The editor keeps at most 8 pending keys, each with its time and the snapshot before it, in a fixed array on `TermState`.
The keymap of each mode is a trie keyed by the key code of section 5.1 (a code point or named key, and four modifier bits, in 32 bits), built when a binding is added.

On each key $k$ at time $t$:

+ Let $P$ be the pending keys. For each suffix of $P + [k]$ from the longest, look it up in the trie; drop matches whose gaps break `within` or `idleBefore`.
  Note the longest suffix that is a complete binding, and whether any suffix is a proper prefix of a binding.
+ If a complete binding was found and no longer binding is still possible: if it has `idleAfter`, read with that deadline;
  a key that arrives in time abandons the binding (go to step 4 for $k$, then handle the new key).
  Otherwise restore the snapshot before its first key and run it; if it declines, go to step 4. Clear $P$.
+ Else if some suffix can still grow: do $k$'s own action now if it is a pure edit, otherwise show it as pending; push $k$ (dropping keys from the front of $P$ that no growing suffix uses).
+ Else: do $k$'s own action, clear $P$.

At most 8 lookups per key, each a walk of at most 8 trie nodes, with no allocation. A key that is not the start of any binding does one lookup and no snapshot.
This is the longest-match-ending-here search of the definition in section 6.1; section 13 says how the two are checked against each other.

== Worked examples

With `j f` bound to `[editAccept]` and no `within`:

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Typed*], [*Result*]),
  [`ls` `j` `f`], [`lsj` is shown after `j`; at `f` the line goes back to `ls`, which runs.],
  [`j` `j` `f`], [`j`, then `jj` (the first `j` is committed when the second starts a new sequence); at `f` the line goes back to `j`, which runs. Helix gives `jjf` here (\#2612), since it does not match the failed keys again.],
  [`j` `k`], [`jk`. Nothing was waiting.],
  [`j` `f` `k` quickly, with `idleAfter: 100`], [`jfk`: `k` came during the wait, so `j f` did not complete.],
  [`j` paste `f`], [The paste is not a key: it breaks the sequence, and `f` is typed.],
)

With `; r` bound to `bindingFunc` of a quotation that declines unless the line is empty (section 7.5):

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Typed*], [*Result*]),
  [`;` `r` on an empty line], [`;` shown; at `r` the line goes back to empty, the file manager runs, then the prompt is drawn again with the empty line.],
  [`[ls]` `;` `r`], [The quotation declines: the line is `[ls];r`.],
  [`s` `;` `r`], [`;` expands `s` to `git status -u` and is inserted. The line is not empty, so `r` declines too: `git status -u;r`.],
)

= Running actions

== Ops

The ops of a program run in order. Each pure edit sees the line as the op before left it.
After the program, the line is repainted once; a program is one step for undo, when the editor has undo.

== Quotations: `bindingFunc`

A `bindingFunc` quotation runs as an abbreviation's quotation does (`ai/abbr.typ` 6.6), by `runUserQuote`:
on its own stack, with an `EditState`, stopped after 1 second together with any command it runs.
`just ops` runs the ops; `none` declines (section 6.1).
If it fails or is stopped, it is treated as declining and nothing else is tried, and the error is shown below the line, once while it keeps failing the same way.
The quotation is waited on at a keystroke, so it should be quick; anything long belongs in an `editRun` it returns.

== `editRun`

`editRun q` runs `q` as a line runs, not as a quotation at a keystroke:

+ The editor leaves the command line as Enter does: the cursor goes below it, the terminal leaves raw mode (and the keyboard protocol is popped, section 10).
+ `q` runs in the foreground on its own empty stack, with no time limit; Ctrl-C interrupts it as it interrupts a line. It can use the terminal: `mshFileManager`, `lf`, `fzf`.
+ When it ends, the prompt is drawn again (its quotation runs again, since `q` may have changed directory) with the line and cursor as they were before the binding.

The line is kept because the binding was pressed in the middle of typing it; losing it, as `;r` does today, is the mistake this fixes.
`editRun` does not add to history and does not change `lastExitCode` or `lastLineOk` in `PromptInfo`: history is the record of lines the user submitted, and the prompt reports on the last of them.
A binding that should be in history submits a line instead, as `;r` does today: `['r' '' editSetLine editAccept]`.

== `editRunOps`

`editRunOps q` runs `q` as `editRun` does --- with the terminal, in the foreground, with no time limit --- but gives it the `EditState` and runs the ops it returns:

- `just ops`: the ops run on the line as it was before the binding, and the prompt is drawn again with the result.
- `none`: the line is kept, as after `editRun`.

`editRun` stays, for the common case of a program that has nothing to say about the line: `[(r) editRun]` instead of `[(drop r none) editRunOps]`.
History and `PromptInfo` are left alone, as for `editRun`.

The standard library uses it to edit the line in the user's editor:

```msh
def editInEditor (EditState -- new Maybe[[EditOp]])
    s!
    '.msh' tempFileExt f!
    @s :before? @s :after? + @f writeFile
    "EDITOR" getEnv "vi" maybe wsplit [@f str] + ? code!
    @f readFile chomp text!
    @f rm
    @code 0 = if [@text '' editSetLine] just else none end
end
```

The file ends in `.msh` so the editor highlights mshell. The result is put on the line, not run, as fish does: Enter runs it.
A user who wants bash's `edit-and-execute-command` binds `[(editInEditor) editRunOps editAccept]`.
No `+line` argument is passed, since not every editor takes one (`code -w` would open a file named `+1`).
I ran this def with `$EDITOR` set to `sed -i s/a/X/g` (the line comes back edited) and to `false` (`none`); the temp file is removed either way.

== Examples

```msh
# Enter, and clear the screen, on letters, as today; `jfk` and `devlog` can still be typed.
{keys: [r'j' r'f'], action: [editAccept] bindingOps, within: 150, idleAfter: 100} bindingAdd
{keys: [r'v' r'l'], action: [editClearScreen] bindingOps, within: 150, idleBefore: 150} bindingAdd

# Open a file manager on the repos or the jobs directory without losing the line.
{keys: [r';' r'r'], action: [(r) editRun] bindingOps} bindingAdd
{keys: [r';' r'j'], action: [(j) editRun] bindingOps} bindingAdd

# Only on an empty line: elsewhere `;r` is typed as it is.
def onEmptyLine (EditState (--) -- new Maybe[[EditOp]])
    s!, q!
    @s :before? @s :after? + '' = if [@q editRun] just else none end
end
{keys: [r';' r'r'], action: ((r) onEmptyLine) bindingFunc} bindingAdd

# Insert today's date (today's Alt-D).
def insertDate (EditState -- new Maybe[[EditOp]])
    drop [now isoDateFmt editInsert] just
end
{keys: [r'd' keyAlt], action: (insertDate) bindingFunc} bindingAdd

# Put `sudo ` in front of the line, keeping the cursor where it was in the text.
def sudoLine (EditState -- new Maybe[[EditOp]])
    s!
    [$"sudo {@s :before?}" @s :after? editSetLine] just
end
{keys: [r's' keyAlt], action: (sudoLine) bindingFunc, description: 'sudo the line'} bindingAdd

# Tab cycling with ctrl-j and ctrl-k.
{keys: [r'j' keyCtrl], action: [editCompleteNext] bindingOps, mode: modeComplete} bindingAdd
{keys: [r'k' keyCtrl], action: [editCompletePrev] bindingOps, mode: modeComplete} bindingAdd

# Edit the line in $EDITOR on bash's key as well, and run lf on F1.
{keys: [r'x' keyCtrl r'e' keyCtrl], action: [(editInEditor) editRunOps] bindingOps} bindingAdd
{keys: [keyF1], action: [(lf) editRun] bindingOps} bindingAdd
```

The defs say `new Maybe[[EditOp]]` because each builds its list on every path, and the checker requires a def to say so (`doc/mshell.md`, "New and stored values").
The payload type of `bindingFunc` has no `new` (a quotation type cannot carry it), and a def marked `new` fits it.
A quotation literal written in place, such as `(:before? 'x' + b! [@b '' editSetLine] just)`, needs nothing.

I checked all of this section against a prototype: the types above declared in a script, with `bindingAdd` a def that prints its record.
Runes do not exist yet, so the prototype has `str` in place of `rune` in `Key` and `'j'` in place of `r'j'`.
With that change every example type checks, including the nested modifiers and `[keyF1] bindingErase`, and the two tests below print `true` and `Just([editSetLine("sudo ab" "cd")])`.
What the prototype cannot show is the point of the rune: with `str` in `Key`, `[r'x' ctlr]` would type check too.

A binding is tested like any other def, in a script, without a terminal:

```msh
{before: 'ls', after: '', keys: [r';' r'r'], mode: modeInsert} (r) onEmptyLine isNone str wl   # true
{before: 'ab', after: 'cd', keys: [r's' keyAlt], mode: modeInsert} sudoLine str wl
```

= Modes

A mode is a keymap. There are two, and they are fixed:

- `modeInsert` is where the editor starts, and where every binding is unless it says otherwise.
- `modeComplete` is entered when Tab leaves several matches to cycle through, and left when a match is accepted or any key it does not bind is pressed.
  A key not bound in `modeComplete` leaves the mode and then acts as in `modeInsert`; that is today's behavior, where any other key ends tab cycling.
  The nine `if state.tabCycleActive` tests in `HandleToken` become this table.

The mode is an enum, not a string, so a misspelled mode is a type error, as a misspelled key is.

== When the mode changes

The mode belongs to the line being edited, as the line and the cursor do.

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Event*], [*Mode afterwards*]),
  [The shell starts; `editAccept` runs a line], [`modeInsert`, for the new line],
  [Tab leaves several matches], [`modeComplete`],
  [A match is accepted, or a key `modeComplete` does not bind is pressed], [`modeInsert`],
  [`editRun`, `editRunOps`], [`modeInsert`: the quotation may have changed the directory the matches came from],
  [History, any other edit; a `bindingFunc` that declines, fails or is stopped], [Unchanged],
)

A mode change ends any pending sequence: its keys stay as typed, and the next key starts a fresh search in the new mode.
A sequence is found in one mode (section 6.1), so a binding cannot begin in one mode and finish in another.

#note[
There are no user-defined modes. A leader key is a sequence (`; g s`) with no mode at all,
and editing beyond what the line editor does belongs in the user's own editor (`editInEditor`, section 7.4).
]

= Bindings and abbreviations

Abbreviations are about *words*: a word, in a position the lexer finds, replaced when a separator is typed. Bindings are about *keys*.
The two meet in three places, and the design keeps one mechanism for each thing:

- *The triggers are bindings.* Space, `| ; < > ] )` are bound by default to `[editExpandAbbr editSelfInsert]`, and Ctrl-Space to `[' ' editInsert]`.
  `isAbbrTrigger` and `typeAbbrTrigger` go away from `HandleToken`, and a user who wants Tab or `,` to expand abbreviations binds it.
  Enter's expansion is part of `editAccept`, since it is part of what is run (`ai/abbr.typ` 6.1).
- *Immediate abbreviations are bindings.* Elvish's `edit:abbr`, which replaces text the moment it is typed (`||` becomes `| less`), is a binding of text keys to a pure edit:
  `{keys: [r'|' r'|'], action: ['| less' editInsert] bindingOps}`. No third mechanism is needed.
- *Order.* Bindings are found first. An abbreviation is expanded only by an op that asks for it.

= The terminal layer

Keys are only as good as the decoder under them. Three changes there, all below the binding table:

+ *Decode every key, bound or not.* The lexer produces a key value (base key and modifiers) for every key it can read, including the modifiers on Home, End and arrows and every Alt chord.
  Whether a key does anything is the keymap's business. Today the lexer drops what nothing binds.
+ *An escape delay.* After `ESC` with nothing following, the lexer waits 30 ms (fish's `fish_escape_delay_ms`; tmux uses 10, Neovim 50) for the next byte, and reports `escape` if none comes.
  Today it waits forever and then discards the Escape. This is a read with a deadline, the same as for terminal replies.
  The delay is a constant, not a setting: with the kitty protocol it is not used, and the protocol is the way to make it go away.
+ *The kitty keyboard protocol, when the terminal has it.* At startup the shell asks (`CSI ? u`, followed by DA1 so that a terminal without it still answers).
  If it does, the editor pushes flags 1 and 4 (`CSI > 5 u`: disambiguate, and report alternate keys, as fish asks for) while it reads a line,
  and pops them (`CSI < u`) before running anything, including `editRun`, so programs inherit the terminal as they expect.
  Then `escape`, `ctrl-i`, `ctrl-m`, `ctrl-h`, `ctrl-backspace` and `ctrl-shift-x` are all distinct, Escape needs no delay, and Ctrl and Alt bindings follow the key's place on the keyboard across layouts.
  The protocol is listed as implemented by kitty, alacritty, foot, ghostty, iTerm2, Windows Terminal, rio, WezTerm (when enabled), Warp and xterm.js, but not tmux (its \#5615 is open).
  fish's experience says terminals get it partly wrong (it turns the protocol off on Konsole and on old iTerm),
  so the shell needs the same way out: one setting that keeps the legacy encoding.

The push and pop are a new handoff of the terminal, and belong in the `ai/terminal-control` model: a crash or a signal between push and pop
must not leave the user's terminal sending escape codes for every key.

#note[
*Chords, later, and probably not.* With flags 2 and 8 the terminal reports releases, so a binding could require its keys to be held down _together_, as firmware combos and keyd's `j+k` do.
It would be one more optional field (`together?: bool`) with no change to the notation.
The survey argues against hurrying: flag 8 turns every key into an escape code, tmux discards releases, and fast typists overlap 40--70% of their keys anyway,
so release order is the weakest of firmware's signals. `idleBefore` and `idleAfter` are the strong ones, and they work in every terminal.
]

= Moving the current bindings

Delete the five hand-written sequences, the F1 and Alt-D cases, and the `tabCycleActive` tests from `HandleToken`. Put this in Mitchell's `init.msh`:

```msh
{keys: [r'j' r'f'], action: [editAccept] bindingOps, idleAfter: 100} bindingAdd
{keys: [r'v' r'l'], action: [editClearScreen] bindingOps, idleBefore: 150} bindingAdd
{keys: [r'q' r'q'], action: [editExit] bindingOps, idleBefore: 150, idleAfter: 100} bindingAdd
{keys: [r';' r'r'], action: [(r) editRun] bindingOps} bindingAdd
{keys: [r';' r'j'], action: [(j) editRun] bindingOps} bindingAdd
{keys: [keyF1], action: [(lf) editRun] bindingOps} bindingAdd
{keys: [r'd' keyAlt], action: (insertDate) bindingFunc} bindingAdd
```

What changes for these: the first key appears as it is typed; the line is kept by `;r`, `;j` and F1, which no longer add `r`, `j` or `lf` to history;
`qq` leaves through `editExit` instead of running `0 exit`;
and the timing fields make `jfk`, `devlog` and `qqq` typeable. The values are guesses to be tuned by use; with none of them, the behavior is today's, minus the lag.
`insertDate` is the def in section 7.5.

= Cost of a key press

The abbreviation work made the key path allocation free (`TestKeyPathAllocations`, `BenchmarkKeyPath`); bindings must keep it so.

- The lookup is by an integer key code in a trie built at `bindingAdd`, so no key name is formatted or compared as a string at a keystroke.
  The `[Key]` of `EditState` is made only when a `bindingFunc` runs.
- A snapshot is two string headers into `lineArena`; the pending array is fixed size. Nothing is copied unless a quotation is given the line, which copies it, as for an abbreviation.
- A `bindingOps` program runs as a switch over its ops. Only `bindingFunc`, `editRun` and `editRunOps` start the evaluator.

The expected cost for an unbound text key is one trie lookup, well under the microsecond the key path takes today; it is measured with the existing benchmark.

= Verification

Two things are checked mechanically, in the style of `ai/terminal-query` and `TestAbbrSiteFromLineTokens`.

+ *A model of resolution* (`ai/key-bindings/KeyResolution.tla`), over a small keymap (text keys, a named key, a prefix, pure and effect actions, the three timing fields), keys with times, and pastes. Properties:
  - `NoLag`: after a key whose own action is a pure edit, the line shown includes it before the next key is read.
  - `ExactRollback`: a binding's action runs on exactly the line that preceded its first key.
  - `EffectsNeverUndone`: no effect runs for a key that a later key makes part of a binding.
  - `PasteInert`: a paste never completes or continues a sequence.
  - `Bounded`: at most 8 pending keys; the editor waits on a deadline only for `idleAfter` and the overlap case of section 6.3, and never longer than the longest declared time.
  - `TextNeverLate`: no deadline wait delays the display of a typed key; only effects wait.
  - `MatchesDefinition`: the final line equals the declarative result of section 6.1.
+ *An oracle test in Go*: the declarative definition written plainly (try every binding at every key) against the trie algorithm,
  on random keymaps and random key streams with random gaps, comparing the line, the cursor and the effects run.

= Implementation plan

+ *Runes.* The `rune` type and its literals (`ai/runes.typ`), which `Key` needs. Until then the rest can be built with `str` in its place, as the prototype was.
+ *Decoder.* A key code for every key the lexer can read, with modifiers; the escape delay.
  Table tests: every legacy byte, every CSI form in `homeEndKey` and the arrows, with each modifier.
+ *Normal form.* A `Key` value to a key code (section 5.1), and back for `EditState`. Table tests for each rule, including the inert cases.
+ *Types and builtins.* The declarations in `builtinDeclSource`, `Key` among them; `bindingAdd`, `bindingErase`, `bindingList` in `Evaluator.go`, `BuiltInList.go` and `TypeCoreBuiltins.go`; inert entries and messages.
+ *Ops.* One function that runs an `[EditOp]` on `TermState`. Each existing case in `HandleToken` becomes an op; the cases themselves go.
+ *Defaults.* The table of section 5.4, added before `init.msh`. `HandleToken` becomes: decode, resolve, run.
+ *Resolution.* The pending array, snapshots, the trie per mode, the timing fields; the deadline read for `idleAfter` and overlaps.
+ *Modes.* `modeInsert` and `modeComplete`.
+ *`editRun` and `editRunOps`.* Leave the line, run in the foreground, redraw with the line kept or with the returned ops applied. `editInEditor` in `lib/std.msh`.
+ *Kitty protocol.* Query, push and pop around every handoff of the terminal. This touches `ai/terminal-control`, whose model should cover the push and pop.
+ *Tests.* The oracle test and the model (section 13); `tests/success` for add, replace, erase, list and inert entries; `tests/typecheck_fail` for a `bindingFunc` of the wrong type and an `editRun` that leaves a value or an `editRunOps` that returns the wrong type.
+ *Documentation.* A "Key bindings" section in `getting-started.inc.html` after "Abbreviations", with the default table; the builtins in `functions.inc.html`; the type names in `type_system.inc.html`;
  one line in `mshell.md` that the names are reserved; the key list in `mshell.md` replaced by a pointer; `CHANGELOG.md`.

= Open questions

+ *Ctrl-C exits the shell* today. fish, zsh and bash cancel the line instead. Should the default be `['' '' editSetLine]`?
+ *Alt-O and Alt-D*: are they defaults for everyone, or personal? The draft keeps Alt-O and moves Alt-D to `init.msh`.
+ *Default timing* for sequences made only of text keys, so that a pair is typeable unless the user asks otherwise.
  The draft has none, to keep time out unless declared (P6), as fish does; zsh (0.4 s) and readline (0.5 s) have one, but theirs exists to end the wait for a held-back key, which this design does not have.
+ *A setting for the kitty protocol*, for terminals that implement it badly (section 10). An environment variable, as `MSH_CELL_PIXELS` is for the file manager, or a builtin?
+ *Undo.* A binding's program is one undo step, and a rollback is not an undo step at all; both need the editor to have undo first.
+ *A script builtin that replays keys* (`[{key: Key, ms: int}]` to an `EditState`), so users can test sequences and `within` the way the Go tests do.
+ *More named keys.* The kitty protocol also reports keypad keys, media keys and the modifier keys themselves; they can be added to `NamedKey` when someone binds one.

= Sources

The two surveys behind section 3 read these; issue numbers are in the project named.

- fish: #link("https://fishshell.com/docs/current/cmds/bind.html")[bind]; source `src/input/input.rs`, `src/input/binding.rs`, `src/terminal.rs` at 47a8719; issues \#7401, \#10359, \#12214, \#12504.
- Nushell and reedline: #link("https://www.nushell.sh/book/line_editor.html")[line editor book]; reedline \#1204, \#989, \#1016, \#1214; nushell \#18870.
- Elvish: #link("https://elv.sh/ref/edit.html")[edit module]; \#730.
- zsh: #link("https://zsh.sourceforge.io/Doc/Release/Zsh-Line-Editor.html")[Zsh Line Editor]. bash: `bash(1)` and NEWS for 5.3.
- PSReadLine: `Set-PSReadLineKeyHandler` reference; `ConsoleKeyChordConverter.cs`, `ReadLine.cs`; \#1701.
- prompt_toolkit: `key_binding/key_processor.py`, `application.py`. IPython terminal options. xonsh `docs/prompt.rst`.
- ble.sh: manual §3, key binding; \#31. murex: `docs/events/onkeypress.md`. Atuin: #link("https://docs.atuin.sh/latest/configuration/advanced-key-binding/")[advanced key binding].
- Emacs: `doc/lispref/keymaps.texi`, `NEWS.29`, `NEWS.30`, `lisp/keymap.el`; #link("https://github.com/emacsorphanage/key-chord")[key-chord.el]; key-seq.el; evil-escape.
- Vim `src/getchar.c`; Neovim `src/nvim/input.c`, `src/nvim/options.lua`;
  #link("https://github.com/max397574/better-escape.nvim")[better-escape.nvim]; #link("https://github.com/TheBlob42/houdini.nvim")[houdini.nvim].
- Kakoune wiki, "Avoid the escape key". Helix `helix-term/src/keymap.rs`, `ui/editor.rs`; \#2612, \#8486, \#14214.
- VS Code: #link("https://code.visualstudio.com/docs/configure/keybindings")[keybindings]; `abstractKeybindingService.ts`.
- tmux `tmux.1`, `options-table.c`; \#5615. WezTerm key tables. #link("https://sw.kovidgoyal.net/kitty/keyboard-protocol/")[kitty keyboard protocol].
- QMK #link("https://docs.qmk.fm/features/combo")[combos] and #link("https://docs.qmk.fm/tap_hold")[tap-hold]; #link("https://zmk.dev/docs/config/combos")[ZMK combos];
  kanata `docs/config.adoc`; keyd `docs/keyd.scdoc`, `src/keyboard.c`.
- V. Dhakal, A. M. Feit, P. O. Kristensson, A. Oulasvirta, "Observations on Typing from 136 Million Keystrokes", CHI 2018.
