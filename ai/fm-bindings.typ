// Design: user-defined key bindings for the built-in file manager.
// Build: typst compile ai/fm-bindings.typ

#set document(title: "Key bindings in the file manager", author: "Claude (draft for Mitchell)")
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
  #text(size: 20pt, weight: "bold")[Key bindings in the file manager]
  #v(0.3em)
  #text(size: 11pt)[Design draft --- 2026-10-22, branch `fm-bindings`]
  #v(0.2em)
  #text(size: 10pt, style: "italic")[Written by Claude from the code on `main` (b4bbe4d), the shell's key binding design (`ai/cli_bindings.typ`, here "the CLI design"),
  and a check of other file managers' sources (section 3).]
]

#v(1em)

= Summary

The file manager (`msh fm`, `mshFileManager`, Ctrl-O in the shell) has its keys compiled into three functions in `mshell/FileManager.go`:
`handleInputEvent` for browsing, `handleSearchInput` for the `/` line and `handleRenameInput` for the `r` line.
None of them can be changed, and each decodes bytes by hand, differently.

The shell's keys became data in commit 1afca2f (\#377). This design does the same for the file manager, with the same machinery:

```msh
{keys: [r'g' r'h'], action: [~ fmCd] fmOps, description: 'home'} fmBindingAdd
{keys: [r'L'], action: [([lazygit];) fmRun] fmOps, description: 'lazygit'} fmBindingAdd
{keys: [r'b' keyCtrl], action: [editLeft] fmOps, mode: fmModeInsert} fmBindingAdd
[r'x'] fmBindingErase
```

Five ideas carry it:

+ *One resolver, a second table.* `Key`, the trie, pending keys, rollback, the conflict rule, `within` and `idleBefore` are the CLI design's, unchanged and shared in the code.
  The file manager gets its own table (`FmBinding`, `fmBindingAdd`), its own verbs (`FmOp`), and its own modes.
+ *Two modes for keys, as the shell has, and one for help.* In `fmModeNormal` a key is a command (`j`, `y y`).
  In `fmModeInsert` keys type into the prompt line (search, rename, and prompts a binding opens), and that line is edited by the shell's own line-edit ops:
  `editDeleteWordLeft` means the same thing in both places, so a binding's program is a list of `FmOp | EditOp`.
+ *A program can cross modes.* `[fmPromptSearch fmPrompt 'TODO' editInsert fmLineAccept]` is a search for `TODO` on one key.
  The mode is followed through the program when the binding is added, and an op that cannot run where it stands makes the binding inert, as an op after `editAccept` does in the shell.
+ *Moving the cursor is the file manager's pure edit.* It changes only the current entry and the scroll, so it can be done at once and taken back, as typed text is in the shell.
+ *Dialogs are not keymaps.* "Delete this?", "Overwrite?" and "Which bookmark?" show their answers on screen and read one key. They stay fixed.

New for the user besides configuration:

- the prefix popup (today's `y` and `g` boxes) is drawn from the table, so a user's `g h` appears in it with its description;
- `?` opens a help screen listing every binding, and every binding that cannot fire with the reason;
- a binding can open a prompt of its own (`New directory: `) and run a quotation with what was typed;
- `fmRunWait` runs a command and keeps its output on screen until a key is pressed;
- Space selects entries, as in lf and ranger, and cut, copy, trash and yank act on all of them;
- `Q` quits without changing directory;
- a paste is refused outside a prompt line, and the line has a length limit;
- `msh fm` loads `init.msh`, so the bindings work there too.

= What exists today

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Keys (browsing)*], [*Does*]),
  [`q`], [quit; the shell then changes to the directory shown],
  [`j` / Down, `k` / Up], [cursor down / up],
  [`h` / Left], [parent directory (on Windows, at a drive's root, the list of drives)],
  [`l` / Right / Enter], [enter the directory; open a file (text in `$EDITOR`, otherwise the system opener)],
  [`g g`, `G`], [first / last entry],
  [Ctrl-D, Ctrl-U], [10 entries down / up],
  [`e`], [the file in `$EDITOR` (`vi` when unset)],
  [`r`], [rename: opens the `rename:` line, cursor before the extension],
  [`/`, `n`, `N`], [search: opens the `/` line; next / previous match],
  [`d`, `y y`, `p`, `c`], [cut, copy, paste, clear the file manager's clipboard (a file shared by every instance)],
  [`y f`, `y p`, `y g`], [file name, full path, path from the git root, to the system clipboard],
  [`x`], [move to the trash, after a question (`y` confirms)],
  [`m` _c_, `;` _c_], [set bookmark _c_ to this directory; go to bookmark _c_ (_c_ is `0`--`9`, `a`--`z`, `A`--`Z`)],
  [F5], [read the directory again],
)

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*Keys*], [*Search line*], [*Rename line*]),
  [printable ASCII], [append], [insert at the cursor],
  [Enter], [jump to the first match at or after the cursor], [rename],
  [Escape], [close the line], [close the line],
  [Backspace, Ctrl-U], [delete the last character; clear], [delete before the cursor; delete to the start],
  [Ctrl-W], [delete back to a space], [delete back to a space or a `.`],
  [Left, Right, Home, End, Ctrl-A, Ctrl-E, Ctrl-K], [---], [move; delete to the end],
)

Problems, apart from being compiled in:

+ *Three decoders, each partial.* Browsing knows `ESC [ A`--`D` and `ESC [ 15 ~` only, so Home, End, Page Up and Down, F-keys other than F5, Alt chords and modified keys do nothing.
  The lines take bytes 32 to 126 only. I checked these with a throwaway test against the current code:
  - typing `é` into the rename line inserts nothing, so a file cannot be renamed to `café.txt`, and search cannot find it;
  - Delete in the rename line inserts `~` (`ESC [ 3` is skipped as an unknown sequence and the `~` is typed);
  - Up or Down in the search line closes it (any escape sequence counts as Escape there).
+ *Prefix keys by hand.* `lastKey` holds one key; `g g` and the four `y` sequences are `if` tests spread through the `switch`,
  and the popup that lists the follow-up keys (`renderPrefixOverlay`) is a second, separate list that must be kept in step with them.
+ *Two line editors, both unlike the shell's.* Search has no cursor; rename has a cursor but no Delete or word motion; their Ctrl-W rules differ; neither counts graphemes.
+ *Paste runs commands.* Bracketed paste is off in the file manager, so text pasted while browsing is read as keys: a pasted `q` quits, `x` asks to trash, `c` clears the clipboard.
+ *`msh fm` gives the editor the wrong output.* `msh fm` draws on `/dev/tty` so that its standard output carries only the final directory (`cd "$(msh fm)"`),
  but `e` and `l` run `$EDITOR` with the process's standard output, which is then the pipe. (From the code; not run.)
+ *Nothing documents the keys*, except entries in `CHANGELOG.md`.

= Prior art

The CLI design surveyed shells and editors. For this one an agent checked specific claims about file managers against their current sources (2026-10-22); the commit read is given for each.

*lf* (664d7c8). `map` binds in normal and visual mode, `nmap` and `vmap` in one, `cmap` on the command line, whose verbs are their own (`cmd-escape`, `cmd-enter`, `cmd-left`, `cmd-delete-word`, ...).
`cmd` defines commands, run in lf (`:`) or the shell (`$`, `!` waiting for a key afterwards, `&` in the background, `%` with output to the status line).
`push` feeds keys as if typed: `map r push :rename<space>` opens the command line with text in it.
`map gh` with no command removes a binding. Marks are "modal" commands that read the next key (`m`, `'`).
Sequences: if the keys typed match a binding exactly, it runs at once, *even when a longer binding starts with them*, which can then never be reached; lf waits, without a time limit, only when nothing matches exactly.
The pending keys show in the ruler, and `showbinds` (on by default) lists the bindings that could follow.

*ranger* (46e957a). `rc.conf` has `map`, `cmap` (console), `pmap` (pager), `tmap` (task view), `copymap` and `unmap` with per-map variants.
Defaults are `dd` cut, `yy` copy, `pp` paste, `dD` delete after a question, `m<any>` set bookmark, #raw("`<any>") and `'<any>` go to it.
An incomplete sequence draws hints: the possible continuations.

*yazi* (7c45837). `keymap.toml` has a section per layer: `[mgr]`, `[tasks]`, `[spot]`, `[pick]`, `[input]`, `[confirm]`, `[cmp]`, `[help]`.
Entries are `{ on = ["g", "g"], run = "arrow top", desc = "..." }`; `prepend_keymap` and `append_keymap` add to the defaults, and a user entry with the same `on` replaces a default.
The input box has its own vi-like normal, insert and visual modes. A help layer lists the bindings with their `desc`; the first key of a longer sequence opens a "which" popup.

*Midnight Commander.* `mc.keymap` is an ini file with a section per context: `[filemanager]`, `[panel]`, `[input]` for every input line, `[dialog]`, `[viewer]`, `[editor]`, and others.

*vifm* maps per mode with Vim's commands (`nnoremap`, `cnoremap`), and waits `timeoutlen` (1 s) when a sequence is ambiguous.
*nnn* compiles its keys in (`src/nnn.h`); plugins are reached with `;` and a key set in `NNN_PLUG`.

*Escape.* tcell (used by lf) waits 50 ms after a lone ESC for a following byte; ranger sets curses' `ESCDELAY` to 25 ms; yazi's own terminal crate waits 10 ms.
crossterm needs no timer: an ESC that ends what one read returned is Escape, and an ESC followed by more bytes in the same read starts a sequence or an Alt chord.
That is also what the file manager does today, and what section 10.2 adopts for the shell as well.

#table(
  columns: (1fr, 1fr),
  inset: 4pt,
  table.header([*Taken*], [*Left, and why*]),
  [A separate keymap for the input line (lf `cmap`, ranger `cmap`, yazi `[input]`, mc `[input]`)], [Emacs's minibuffer map inheriting from the global map: see the decision in section 5.3],
  [Descriptions on bindings, shown when a prefix is pending (yazi, ranger, lf `showbinds`)], [---],
  [Commands that read one more key (lf's marks, ranger's `m<any>`)], [Making those questions bindable: section 9],
  [Opening the input line with text in it (lf `push`)], [Feeding keys as if typed: a program of ops does it with no deadlock (`map j push 2j` deadlocks lf)],
  [Running mshell code on a key, in the directory shown (lf `$`), and waiting for a key afterwards (lf `!`)], [lf's four run prefixes; three ops: run, run and wait, run and return ops (section 7.3)],
  [A help screen built from the bindings and their descriptions (yazi `[help]`)], [---],
  [Selecting entries with Space, across directories; cut, copy and delete act on the selection or else the current file, then clear it (lf `toggle`, `invert`, `unselect`, `currFileOrSelections`)], [---],
  [Read boundaries for Escape (crossterm)], [Escape timers (tcell, ranger, yazi): the editor has no timers (CLI design 6.4)],
  [Longest match, with rollback (CLI design)], [lf's shortest exact match, which makes longer bindings unreachable],
)

= Principles

The nine principles of the CLI design (P1--P9) hold as written, with "the editor" read as the file manager. Four more:

#principle(10, "One way to bind a key.")[
A user who has written a shell binding can write a file manager binding: the same `Key` values, the same record fields, the same three builtins with `fm` in front,
the same rules for sequences, timing, conflicts and inert entries. Only the verbs and the modes differ, because the programs differ.
]

#principle(11, "A line is edited the same way everywhere.")[
The prompt line (search, rename, a binding's own prompts) is a line editor. It counts graphemes, and its verbs are the shell's line-edit ops, so `editDeleteWordLeft` is learned once.
It is a separate keymap, because a file name is not a command line (section 5.3).
]

#principle(12, "A question is not a keymap.")[
A question shows its answers on screen and reads one key. Escape, or any key it does not offer, is no.
It ends a pending sequence, as a paste does, and its keys never reach a binding.
]

#principle(13, "The defaults are today's keys.")[
Every key in section 2 does what it does today, except the fixes and additions listed in section 5.5.
]

= The design

== Types

Added to `builtinDeclSource` after the shell's binding types, so the names are reserved.

```msh
enum FmMode = fmModeNormal | fmModeInsert | fmModeHelp end

enum FmPrompt =
      fmPromptSearch          # "/": the matches follow the line; Enter jumps to one
    | fmPromptRename          # "rename: ": the current entry's name; Enter renames it
    # A label, the line to start from, and what Enter runs with the line typed.
    | fmPromptAsk str str (str FmState -- Maybe[[FmOp | EditOp]])
end
type FmLine = {prompt: FmPrompt, before: str, after: str}

type FmState = {dir: str, current: Maybe[str], selected: [str], mode: FmMode, line: Maybe[FmLine], keys: [Key]}

enum FmDistance =
      fmRows int              # this many entries
    | fmPercent float         # this percentage of the visible rows: 50.0 is half a screen
end

enum FmOp =
    # Normal mode. These move the cursor only, and can be taken back.
      fmDown | fmUp | fmTop | fmBottom
    | fmDownBy FmDistance | fmUpBy FmDistance
    | fmSearchNext | fmSearchPrev
    | fmCursorTo str          # put the cursor on the entry with this name, if there is one
    # Normal mode, effects.
    | fmOpen                  # the current entry: enter the directory, or open the file
    | fmParent
    | fmCd str                # show this directory
    | fmReload
    | fmOpenEditor            # the selected files, or the current one, in $EDITOR
    | fmToggle                # add the current entry to the selection, or take it out
    | fmSelectionInvert       # toggle every entry of the directory shown
    | fmSelectionClear
    | fmPrompt FmPrompt       # open the prompt line, in insert mode
    | fmCut | fmCopy | fmPaste | fmClipboardClear
    | fmYankName | fmYankPath | fmYankGitPath
    | fmTrash                 # asks first
    | fmBookmarkSet | fmBookmarkGo   # ask for the bookmark's key
    | fmHelp                  # the help screen, in help mode
    # Insert mode, effects. Its pure edits are EditOp's line edits.
    | fmLineAccept | fmLineCancel
    # Help mode. Its pure edits are the moves above, which scroll the help.
    | fmHelpClose
    # Any mode.
    | fmRun ( -- )            # run this quotation with the terminal
    | fmRunWait ( -- )        # the same, then wait for a key before drawing over its output
    | fmRunOps (FmState -- Maybe[[FmOp | EditOp]])  # the same, then run the ops it returns
    | fmMessage str           # show a line at the bottom until the next key
    | fmQuit                  # last; the shell goes to the directory shown
    | fmQuitNoCd              # last; the directory stays as it was
end

type FmProgram = [FmOp | EditOp]

enum FmAction =
      fmOps FmProgram                          # always this program
    | fmFunc (FmState -- Maybe[FmProgram])     # the program to run, or none to decline
end

type FmBinding = {
    keys: [Key],
    action: FmAction,
    mode?: FmMode,             # fmModeNormal when missing
    within?: int,
    idleBefore?: int,
    description?: str,         # shown in the prefix popup, and given back by fmBindingList
}
```

`FmState` is the file manager as a binding sees it:

- `dir`: the directory shown, absolute. While Windows' list of drives is shown, the directory it was opened from.
- `current`: the entry under the cursor, absolute; `none` in an empty directory.
- `selected`: the selected entries, absolute, in the order they were selected. They can be in other directories than `dir`.
- `mode`: the mode the binding was found in.
- `line`: the prompt line in insert mode, split at its cursor as `EditState` is, with the prompt it belongs to; `none` in the other modes.
  An insert-mode binding can match on `prompt` to act only in search, or only in its own `fmPromptAsk` prompt.
- `keys`: the keys of the sequence that ran the binding.

`fmPromptAsk` and `fmRunOps` spell out `[FmOp | EditOp]` because `FmProgram` is declared after `FmOp`.
The types refer to each other in a loop (`FmPrompt` names `FmState`, which holds an `FmLine`, which holds an `FmPrompt`); the checker accepts it.

A distance in percent becomes the visible rows times the percentage over 100, rounded down, and at least one entry, so it follows the window as it is resized.
`fmPercent` takes a `float`, so half a screen is `50.0 fmPercent`; `50 fmPercent` is a type error.

#decision[
*How far Ctrl-D moves is the binding's choice.*
Today Ctrl-D and Ctrl-U move 10 entries; Vim and lf move half a screen. Neither is wrong, so the op takes the distance:
`[10 fmRows fmDownBy]` or `[50.0 fmPercent fmDownBy]`. The defaults keep 10 (P13).
`fmDown` and `fmUp` stay as words of their own, since one entry is what `j`, `k` and the arrows want, and `[1 fmRows fmDownBy]` is a long way to write it.
]

I declared all of this in a script, with the three builtins as defs that print their record, and type checked and ran every example in this document with the current `msh` (section 7.6).

#decision[
*A second table, not more modes in the shell's.*
One `Binding` with modes `fmModeNormal` and `fmModeInsert` added to `EditMode` would put the shell's and the file manager's bindings in one list, and save three builtins.
But a `bindingFunc` is given an `EditState`, and a file manager quotation needs an `FmState`, so the action's type must differ; once it does, the record does.
The type would also accept `editAccept` or `editHistoryPrev` in a file manager mode, where they mean nothing.
With two records, giving the wrong ops to either is a type error, and `bindingList` stays a list of one type.
]

#decision[
*A program is a list of `FmOp | EditOp`.*
The prompt line needs fourteen line edits that `EditOp` already has. Three ways to give them to the file manager:

- Repeat them in `FmOp` (`fmLineLeft`, `fmLineBackspace`, ...): fourteen names that mean what fourteen existing names mean.
- Wrap them (`[editLeft fmEdit]`): every line binding pays a word for nothing.
- A union, as here: `[editLeft]` is written as in the shell.

The union costs one thing, which I checked: lists are invariant, so a list *stored* as `[EditOp]` is not an `FmProgram`.
A literal written in place, a def that returns `new Maybe[FmProgram]`, and a quotation in a `fmFunc` all check. A stored one is made with `as FmProgram`, as the checker's message says.
The ops of `EditOp` that are not line edits (`editAccept`, `editExpandAbbr`, ...) are allowed by the type and refused by `fmBindingAdd` (section 5.3).
]

#decision[
*Directories are `str`.*
`fmCd` cannot take `str | path`: an enum payload names one type, and a union needs a new alias.
`str` is the one that serves the common case: a bare `~/repos` is a `str` after tilde substitution, while a backtick path is not substituted (#raw("`~/repos`") names a directory called `~`).
For the same reason, and to match `EditState` and `AbbrInfo`, `FmState` gives directories as `str`. `cd`, `ls` and commands take either.
]

#decision[
*Selected means marked; the entry under the cursor is current.*
Until this design the file manager's code and this document called the entry under the cursor "selected".
lf and ranger use "selection" for the entries marked with Space, and "current file" for the one under the cursor, and so does this design:
`FmState.current`, `fmCursorTo`, and `selected` only for the marked set.
]

== Which ops run where

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Mode*], [*Ops*]),
  [`fmModeNormal`], [every `FmOp` from `fmDown` to `fmHelp`],
  [`fmModeInsert`], [the line edits of `EditOp`: `editInsert`, `editSelfInsert`, `editSetLine`, `editLeft`, `editRight`, `editWordLeft`, `editWordRight`, `editLineStart`, `editLineEnd`, `editBackspace`, `editDelete`, `editDeleteWordLeft`, `editDeleteToStart`, `editDeleteToEnd`; and `fmLineAccept`, `fmLineCancel`],
  [`fmModeHelp`], [`fmDown`, `fmUp`, `fmTop`, `fmBottom`, `fmDownBy`, `fmUpBy`, which scroll the help; `fmHelpClose`],
  [any], [`fmRun`, `fmRunWait`, `fmRunOps`, `fmMessage`, `fmQuit`, `fmQuitNoCd`],
)

The mode is followed through a program: `fmPrompt` switches to insert mode and `fmLineAccept` and `fmLineCancel` back to normal mode; `fmHelp` switches to help mode and `fmHelpClose` back.
So `[fmPromptSearch fmPrompt 'TODO' editInsert fmLineAccept]` checks, and `[fmDown editLeft]` does not.
`fmBindingAdd` follows the mode of a `fmOps` program from the binding's `mode`, and makes the binding inert at the first op that cannot run where it stands.

At run time an op can still find the wrong mode: `fmPromptRename fmPrompt` opens no line in an empty directory, and a quotation's program is not known in advance.
Then the program stops at that op, and a message names it.

`editSelfInsert` inserts the text of the last key of the sequence, as in the shell. In a word op, a word in the prompt line ends at a space or a `.`, which is today's rename rule:
in `report.final.pdf`, with the cursor before `.pdf`, Ctrl-W deletes `final`.

== Builtins

#table(
  columns: (auto, auto, 1fr),
  inset: 4pt,
  table.header([*Word*], [*Signature*], [*Does*]),
  [`fmBindingAdd`], [`(FmBinding -- )`], [Adds a binding; one with the same keys and `mode` is replaced.],
  [`fmBindingErase`], [`([Key] -- )`], [Removes the bindings with these keys, in every mode, defaults included.],
  [`fmBindingList`], [`( -- new [FmBinding])`], [Every binding, oldest first, defaults first, `mode` always present.],
)

They are `bindingAdd`, `bindingErase` and `bindingList` over the file manager's table, with the same rules (CLI design 5.3): none fails on a value of its type, and a binding that can never fire is kept, listed and inert,
with one line on stderr when the shell is interactive. The reasons are the CLI design's, and two more:

- an op that cannot run in the mode it stands in (section 5.2), which includes every `EditOp` that is not a line edit;
- an op after `fmQuit` or `fmQuitNoCd`;
- a distance of zero or less (`0 fmRows`, `-5.0 fmPercent`). In a program a quotation returns, such an op does nothing.

#decision[
*The prompt line has its own keymap.*
Emacs's minibuffer inherits the global map and overrides a few keys, which would give the file manager's line every line binding the user made in the shell.
It would also give it the rest: the abbreviation triggers (`space` is `[editExpandAbbr editSelfInsert]`), immediate abbreviations (`| |` to `| less`), and `j f` as Enter.
Taking only the shell's pure bindings would still bring the first two into file names.
Every file manager checked keeps its input line's keys apart (section 3), and so does this design.
The cost is one line in `init.msh` for a user who changes a line-editing key and wants it in both places.
]

== The default keymap

The file manager adds these to its table before `init.msh` runs. Each has a `description`, which the prefix popup shows.

#table(
  columns: (auto, auto, 1fr),
  inset: 4pt,
  table.header([*Keys* (`fmModeNormal`)], [*Action* (`fmOps`)], [*Description*]),
  [`r'q'`], [`[fmQuit]`], [quit],
  [`r'Q'`#super[new]], [`[fmQuitNoCd]`], [quit without changing directory],
  [`r'j'`, `keyDown`], [`[fmDown]`], [down],
  [`r'k'`, `keyUp`], [`[fmUp]`], [up],
  [`r'h'`, `keyLeft`], [`[fmParent]`], [parent directory],
  [`r'l'`, `keyRight`, `keyEnter`], [`[fmOpen]`], [open],
  [`r'g' r'g'`, `keyHome`#super[new]], [`[fmTop]`], [jump to top],
  [`r'G'`, `keyEnd`#super[new]], [`[fmBottom]`], [jump to bottom],
  [`r'd' keyCtrl`], [`[10 fmRows fmDownBy]`], [10 down],
  [`r'u' keyCtrl`], [`[10 fmRows fmUpBy]`], [10 up],
  [`keyPageDown`#super[new]], [`[100.0 fmPercent fmDownBy]`], [a page down],
  [`keyPageUp`#super[new]], [`[100.0 fmPercent fmUpBy]`], [a page up],
  [`r'e'`], [`[fmOpenEditor]`], [edit in \$EDITOR],
  [`r'r'`], [`[fmPromptRename fmPrompt]`], [rename],
  [`r'/'`], [`[fmPromptSearch fmPrompt]`], [search],
  [`r'n'`, `r'N'`], [`[fmSearchNext]`, `[fmSearchPrev]`], [next match, previous match],
  [`r'd'`], [`[fmCut]`], [cut to the file-manager clipboard],
  [`r'y' r'y'`], [`[fmCopy]`], [copy entry to file-manager clipboard],
  [`r'y' r'f'`], [`[fmYankName]`], [file name → system clipboard],
  [`r'y' r'p'`], [`[fmYankPath]`], [full path → system clipboard],
  [`r'y' r'g'`], [`[fmYankGitPath]`], [git-relative path → system clipboard],
  [`r'p'`], [`[fmPaste]`], [paste the file-manager clipboard],
  [`r'c'`], [`[fmClipboardClear]`], [clear the file-manager clipboard],
  [`r'x'`], [`[fmTrash]`], [move to the trash],
  [`r' '`#super[new]], [`[fmToggle fmDown]`], [select, and move down],
  [`r'v'`#super[new]], [`[fmSelectionInvert]`], [invert the selection],
  [`r'u'`#super[new]], [`[fmSelectionClear]`], [clear the selection],
  [`r'm'`], [`[fmBookmarkSet]`], [set a bookmark],
  [`r';'`], [`[fmBookmarkGo]`], [go to a bookmark],
  [`keyF5`], [`[fmReload]`], [reload],
  [`r'?'`#super[new]], [`[fmHelp]`], [help],
)

The four descriptions of `y` and `g g` are the text of today's popup. Space, `v` and `u` are lf's keys for the same ops.

#table(
  columns: (auto, auto),
  inset: 4pt,
  table.header([*Keys* (`fmModeInsert`)], [*Action* (`fmOps`)]),
  [`keyEnter`], [`[fmLineAccept]`],
  [`keyEscape`, `r'c' keyCtrl`#super[new]], [`[fmLineCancel]`],
  [`keyLeft`, `r'b' keyCtrl`#super[new]; `keyRight`, `r'f' keyCtrl`#super[new]], [`[editLeft]`; `[editRight]`],
  [`r'b' keyAlt`#super[new], `r'f' keyAlt`#super[new]], [`[editWordLeft]`, `[editWordRight]`],
  [`keyHome`, `r'a' keyCtrl`; `keyEnd`, `r'e' keyCtrl`], [`[editLineStart]`; `[editLineEnd]`],
  [`keyBackspace`; `keyDelete`#super[new]], [`[editBackspace]`; `[editDelete]`],
  [`r'w' keyCtrl`, `r'h' keyCtrl`#super[new], `keyBackspace keyCtrl`#super[new]], [`[editDeleteWordLeft]`],
  [`r'u' keyCtrl`; `r'k' keyCtrl`], [`[editDeleteToStart]`; `[editDeleteToEnd]`],
)

These are the shell's line-editing defaults, apart from Ctrl-F (here `editRight`; the line has no history suggestion to accept), Escape and Ctrl-C.
A text key with no binding inserts itself, as in the shell.

#table(
  columns: (auto, auto),
  inset: 4pt,
  table.header([*Keys* (`fmModeHelp`, all new)], [*Action* (`fmOps`)]),
  [`r'j'`, `keyDown`; `r'k'`, `keyUp`], [`[fmDown]`; `[fmUp]`],
  [`r'd' keyCtrl`; `r'u' keyCtrl`], [`[10 fmRows fmDownBy]`; `[10 fmRows fmUpBy]`],
  [`keyPageDown`; `keyPageUp`], [`[100.0 fmPercent fmDownBy]`; `[100.0 fmPercent fmUpBy]`],
  [`r'g' r'g'`, `keyHome`; `r'G'`, `keyEnd`], [`[fmTop]`; `[fmBottom]`],
  [`r'q'`, `keyEscape`, `r'?'`], [`[fmHelpClose]`],
)

The help mode's moves are normal mode's keys, so the two read alike; `q` closes the help instead of quitting, as in `less`.

== What changes for the user

- Search and rename share one line editor with the keys above: search gains a cursor, and both gain Delete, Ctrl-B, Ctrl-F, Alt-B, Alt-F, and Ctrl-C to cancel.
  Ctrl-W in search now also stops at a `.`.
- Any character can be typed into the line; Delete deletes; Up and Down no longer close the search line.
- Home, End, Page Up and Page Down move the cursor.
- `?` shows the help screen (section 7.5).
- A paste in normal or help mode is refused with a message; in insert mode it goes into the line if it fits (section 8.1).
- The prompt line holds at most 4096 bytes (section 7.4).
- Space selects entries, `v` inverts the selection, `u` clears it; cut, copy, trash, the yanks and `e` act on every selected entry (section 7.1).
- `Q` quits without changing directory.
- In the shell, in the legacy encoding, Escape becomes a key, and Escape then a key typed after it is no longer Alt and that key (section 10.2).
- `msh fm` loads `init.msh` (section 11), and gives programs it runs the terminal as their output.
- The prefix popup lists the user's bindings too.

= Resolving keys

The CLI design's section 6 applies as written, with one substitution: what a pure edit changes, and a snapshot keeps.

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*Mode*], [*A pure edit changes*], [*A text key with no binding*]),
  [`fmModeNormal`], [the cursor: the current entry's index and the scroll offset], [does nothing],
  [`fmModeInsert`], [the prompt line and its cursor], [inserts its text],
  [`fmModeHelp`], [the scroll offset of the help], [does nothing],
)

The search matches, the preview and the highlighting are worked out from those, so restoring them restores everything a pure edit changed, as in the shell.
The listing itself changes only through effects (`fmOpen`, `fmCd`, `fmReload`, ...), and an effect clears the pending keys, so a snapshot never points into a listing that has gone.

So with `j j` bound to something, `j` moves down at once, and the second `j` puts the cursor back and runs the binding.
With the defaults, which have no sequence that starts with a pure key, this never happens.

Three events end a pending sequence, as a paste does in the shell: a paste, a mode change (inside a program or not), and a question.

A binding is *pure* when its action is `fmOps` and every op in its program is a pure edit of the mode the op runs in:
`fmDown`, `fmUp`, `fmTop`, `fmBottom`, `fmDownBy`, `fmUpBy`, `fmSearchNext`, `fmSearchPrev` and `fmCursorTo` in normal mode, the moves among them in help mode, and the fourteen line edits in insert mode.
A pure edit changes only what the snapshot holds (the table above), so restoring the snapshot takes it back.
Every other binding is an *effect*: one with any other op, and every `fmFunc`, whose program is not known until it runs.
This is the shell's definition (`bindingEntry.pure`), with the file manager's pure edits.
`fmToggle` is an effect: the selection is not in the snapshot, which holds two numbers, not a copy of a set of paths.
So Space, `[fmToggle fmDown]`, conflicts with any longer binding that starts with Space.

The conflict rule is the shell's too (CLI design 6.3, `bindingsConflict`). Two bindings of the same mode conflict when
+ they have the same keys, or
+ one of them is an effect, and its keys occur in the other's keys ending before the other's last key.
The common case of the second is a prefix (`y` against `y y`); the other is a sequence inside a longer one (`y` against `x y z`).
Of two bindings that conflict, the one added later is in force and the other is inert.
A pure binding conflicts with nothing longer: `j` as `[fmDown]` and `j j` can both be in force, as the example above shows.
The rule comes up more often here than in the shell, because most normal-mode ops are effects, while most of the shell's keys type text.

`d` is cut. Adding `d d` (ranger's cut) makes `d` inert, with the message `Binding [r'd'] never fires: it conflicts with the newer binding [r'd' r'd']`, every time `init.msh` runs.
The way to say "replace `d` with `d d`" is to erase `d` first; the example in section 7.6 does.
This is the decision lf leaves to chance: there, `d` would run at once and `d d` could never be typed.

== The prefix popup

When a pending key has no action of its own, the shell shows the pending keys below the line (CLI design 6.3).
The file manager shows a box instead, as it does today for `y` and `g`: titled with the pending keys, it lists each binding in force in the mode that continues them,
one per line, as the keys still to type and the binding's `description`, or its ops written as source when it has none, or `(quotation)` for a `fmFunc`.

```
 y
  y  copy entry to file-manager clipboard
  f  file name → system clipboard
  p  full path → system clipboard
  g  git-relative path → system clipboard
```

Keys in the box are written short, as this document's prose writes them (`g`, `ctrl-d`, `alt-x`, `enter`, `F5`): the box is read in a glance, not pasted into `init.msh`.
It replaces `renderPrefixOverlay` and `lastKey`. As today, it hides the image preview while it is up.

The popup waits for the next key with no time limit, as today's `y` does. Nothing is lost by waiting: the key that comes next either continues the sequence,
or ends it and then acts on its own, so Escape, unbound in normal mode, closes the popup and does nothing else.
With `within` on the binding, a key that comes too late does not complete it; there is no timer, so the popup stays up until that key arrives.

= Running actions

== Ops

The ops of a program run in order, and the screen is drawn once after it.
`fmCd` takes a path from the directory shown, or an absolute one; if it is not a directory, a message says so and nothing else changes.
`fmCursorTo` puts the cursor on the entry with that name in the listing, or does nothing.

The selection is a set of absolute paths, as in lf: entries stay selected when the file manager changes directory, so entries from several directories can be cut or copied together.
`fmToggle` adds the current entry to it or takes it out, `fmSelectionInvert` toggles every entry of the directory shown, and `fmSelectionClear` empties it.
Selected entries are drawn with a mark before the name, and the top row shows how many there are, beside the clipboard's count.
The selection lasts as long as the file manager runs; unlike the clipboard, it is not saved.

These ops act on the selected entries when there are any, otherwise on the current entry, and then clear the selection, as lf's do:

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Op*], [*With a selection*]),
  [`fmCut`, `fmCopy`], [adds every selected entry to the file-manager clipboard],
  [`fmTrash`], [one question for all of them, `Delete 3 entries?`, listing their names],
  [`fmYankName`, `fmYankPath`, `fmYankGitPath`], [one name or path per line to the system clipboard],
  [`fmOpenEditor`], [`$EDITOR` with every selected file as an argument; directories are left out],
)

Every other op acts on the current entry only. A selected path that no longer exists when an op uses it is left out, and the message after the op says how many were.

`fmQuit` ends the file manager in the directory shown: `mshFileManager` changes the shell's directory to it, and `msh fm` prints it.
`fmQuitNoCd` ends it without that: `mshFileManager` leaves the directory as it was, and `msh fm` prints the directory it started in, so `cd "$(msh fm)"` stays where it was.
The ops that need a real directory (`fmOpenEditor`, `fmPromptRename fmPrompt`, the clipboard and trash ops) do nothing on Windows' list of drives, as today.

== Quotations: `fmFunc`

As `bindingFunc` (CLI design 7.2): run by `runUserQuote` on its own stack with an `FmState`, stopped after 1 second, output captured; `just` a program runs it, `none` declines, and a failure is shown at the bottom and counts as declining.

== `fmRun`, `fmRunWait` and `fmRunOps`

`fmRun q` runs `q` as `e` runs the editor today, and as `editRun` runs a quotation in the shell:

+ The file manager gives up the terminal: it shows the cursor, leaves the alternate screen, pops the keyboard protocol, turns bracketed paste off, and restores the terminal mode it was started in.
+ `q` runs in the foreground on its own empty stack, with no time limit, in the directory shown. Its standard output is the terminal: in `msh fm`, that is `/dev/tty`, not the pipe.
+ When `q` ends, the file manager takes the terminal back, reads its size again, and reads the directory again, keeping the cursor on the same entry by name.
  If `q` changed the working directory, the file manager shows the new one.
+ If `q` failed, its error is printed and the file manager waits for Enter before drawing over it, as `e` does today when the editor fails.

`fmRunWait q` does the same, and when `q` ends, writes `Press any key to return` and reads one key before taking the screen back, so what `q` printed can be read.
The key is read as a question's is (section 9): it ends any pending sequence and reaches no binding. lf's `!` works the same way.

`fmRunOps q` does the same as `fmRun`, gives `q` the `FmState`, and runs the ops it returns, which may be `fmMessage`, `fmCd`, `fmCursorTo`, or anything else that fits the mode.
The mode and the prompt line are kept across both: `fmRun` pressed while renaming comes back to the same line.

`exit` in `q` ends the file manager and then does what `exit` does anywhere.

== Prompts

`fmPrompt p` opens the prompt line at the bottom of the screen and switches to insert mode. What the line starts as, and what Enter does, come from `p`:

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*Prompt*], [*Opens with*], [*`fmLineAccept` (Enter)*]),
  [`fmPromptSearch`], [`/` and an empty line; the matches follow the line as it changes], [put the cursor on the first match at or after it],
  [`fmPromptRename`], [`rename: ` and the current entry's name, the cursor before its extension; only with a current entry in a real directory], [rename the entry, and put the cursor on it],
  [`fmPromptAsk label text q`], [`label` and `text`, the cursor at the end], [run `q` with the line and the `FmState`, then the program it returns],
)

`fmLineCancel` (Escape) closes any of them, and nothing runs.

The line holds at most 4096 bytes, the longest path Linux accepts (`PATH_MAX`), so any path or file name fits, and so does a long command.
Bytes, not characters, because bytes are what the system limits.
An edit that would make the line longer is not done, and a message says so: a typed key, `editInsert`, `editSetLine`, or a paste (section 8.1). A program stops at that op.
A `fmPromptAsk` whose `text` is longer does not open.
When the line is wider than the terminal, it scrolls sideways to keep its cursor on screen.

For `fmPromptAsk`, the line closes and the mode is normal before `q` runs, so `q` is given an `FmState` with `mode: fmModeNormal` and no `line`, and its program runs in normal mode.
`q` runs as a `fmFunc` quotation does (section 7.2): on its own stack, stopped after 1 second, its output captured, a failure shown at the bottom.
`none` does nothing.
Anything that needs the terminal or more time goes in the program `q` returns, as `fmRun`, `fmRunWait` or `fmRunOps`; the shell command prompt of section 7.6 does that.

A prompt can also be built by a `fmFunc` from the state, to start the line from the current entry's name or ask a different question in different places.

#decision[
*Search, rename and a binding's prompts are one op.*
All three are a label, a line, and something Enter does with it, so they are one op, `fmPrompt`, and the enum `FmPrompt` says which.
The line editor, its keymap and its mode are the same for all of them, and `FmLine.prompt` lets a binding in insert mode tell them apart.
Search and rename stay members of their own, not `fmPromptAsk` with a built-in quotation, because they do what a quotation cannot: search updates the matches while the line is typed.
]

== The help screen

`fmHelp` replaces both panes with a list of bindings and switches to help mode. The header row stays.
The list has a heading for each mode (normal, the prompt line, help), and under it one line per binding in force: its keys written short, as in the popup, and its `description`, or its ops written as source, or `(quotation)`.
After them comes *Not in force*: each inert binding, with the reason `fmBindingAdd` gave.
That is the one place a user of `msh fm` can see why a binding does nothing, since `init.msh`'s messages go to standard error before the screen is drawn.

The moves scroll the list, so `j`, `k`, Ctrl-D and the others work as they do in the listing, and can be taken back the same way.
`fmHelpClose` returns to normal mode with the listing as it was.

#decision[
*Help is a mode, not a question.*
A question reads one key. The help screen reads many, to scroll, and those keys should be the user's own: someone who moved `fmDown` off `j` in the listing expects the same in the help.
A mode gives that with the ops that exist and one more, `fmHelpClose`. yazi has a help layer in its keymap for the same reason.
`fmHelp` is a normal-mode op only: in the prompt line, `?` is text.
]

== Examples

```msh
# Go home, or to the repositories, with g h and g r. They join g g in the popup.
{keys: [r'g' r'h'], action: [~ fmCd] fmOps, description: 'home'} fmBindingAdd
{keys: [r'g' r'r'], action: [~/repos fmCd] fmOps, description: 'repositories'} fmBindingAdd

# ranger's keys for cut and trash. Erasing first keeps `d` from being reported as inert.
[r'd'] fmBindingErase
[r'x'] fmBindingErase
{keys: [r'd' r'd'], action: [fmCut] fmOps, description: 'cut'} fmBindingAdd
{keys: [r'd' r'D'], action: [fmTrash] fmOps, description: 'move to the trash'} fmBindingAdd

# lazygit in the directory shown.
{keys: [r'L'], action: [([lazygit];) fmRun] fmOps, description: 'lazygit'} fmBindingAdd

# Search for TODO in one key.
{keys: [r't' keyAlt], action: [fmPromptSearch fmPrompt 'TODO' editInsert fmLineAccept] fmOps} fmBindingAdd

# A new directory, named at a prompt, with the cursor put on it.
def newDir (str FmState -- new Maybe[FmProgram])
    name!, s!
    @name '' = if
        none
    else
        $"{@s :dir?}/{@name}" mkdir
        [fmReload @name fmCursorTo] just
    end
end
{keys: [r'a'], action: ['New directory: ' '' (newDir) fmPromptAsk fmPrompt] fmOps, description: 'new directory'} fmBindingAdd

# A shell command in the directory shown; its output stays until a key is pressed.
def shellCommand (str FmState -- new Maybe[FmProgram])
    drop cmd!
    [([sh -c @cmd];) fmRunWait] just
end
{keys: [r'!'], action: ['sh: ' '' (shellCommand) fmPromptAsk fmPrompt] fmOps, description: 'shell command'} fmBindingAdd

# Run the current entry with Enter when it is an mshell script; anything else opens as before.
def runScript (FmState -- new Maybe[FmProgram])
    :current? match
        just f : @f '.msh' endsWith if [([msh @f];) fmRun] else [fmOpen] end just,
        none : none,
    end
end
{keys: [keyEnter], action: (runScript) fmFunc} fmBindingAdd

# The root of the git repository, found by a command.
def gitRoot (FmState -- new Maybe[FmProgram])
    :dir? d!
    [git -C @d rev-parse --show-toplevel] * ? code! out!
    @code 0 = if [@out chomp fmCd] just else ['Not in a git repository' fmMessage] just end
end
{keys: [r'g' r'R'], action: [(gitRoot) fmRunOps] fmOps, description: 'git root'} fmBindingAdd

# Zip the selected entries into archive.zip in the directory shown.
def zipSelected (FmState -- new Maybe[FmProgram])
    :selected? files!
    @files len 0 = if
        ['Nothing selected' fmMessage] just
    else
        [(['zip' '-r' 'archive.zip'] @files + ;) fmRunWait fmSelectionClear] just
    end
end
{keys: [r'z' r'a'], action: (zipSelected) fmFunc, description: 'zip the selection'} fmBindingAdd

# Today's date at the cursor while renaming.
{keys: [r'd' keyAlt], action: (drop [now isoDateFmt editInsert] just) fmFunc, mode: fmModeInsert} fmBindingAdd
```

`newDir` makes the directory itself, since `mkdir` is quick, and returns the ops that show it; a failed `mkdir` is shown at the bottom.
`shellCommand` returns `fmRunWait` instead of running the command itself, because a command needs the terminal and may take longer than a second.

`runScript` binds Enter in normal mode, so it replaces the default `keyEnter` binding, and returns `[fmOpen]` for any other file to do what that binding did.
It could not decline instead: a declined binding leaves the key to its own action, and in normal mode a key has none.

The defs are tested without a terminal:

```msh
{dir: '/tmp', current: '/tmp/a.msh' just, selected: [], mode: fmModeNormal, line: none, keys: [keyEnter]} runScript str wl
# Just([fmRun(...)])
{dir: '/tmp', current: none, selected: [], mode: fmModeNormal, line: none, keys: [r'g' r'R']} gitRoot str wl
# Just([fmMessage("Not in a git repository")])
```

= Modes

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Event*], [*Mode afterwards*]),
  [The file manager starts], [`fmModeNormal`],
  [`fmPrompt`], [`fmModeInsert`, except `fmPromptRename` with no current entry in a real directory, which leaves the mode unchanged (section 7.4)],
  [`fmLineAccept`], [`fmModeNormal`, then what the prompt does on Enter],
  [`fmLineCancel`], [`fmModeNormal`; the search line closes as Escape closes it today],
  [`fmHelp`], [`fmModeHelp`],
  [`fmHelpClose`], [`fmModeNormal`],
  [`fmRun`, `fmRunWait`, `fmRunOps`, a question, anything else], [unchanged],
)

In search, every change to the line updates the matches, as typing does today.

== Pastes

With bracketed paste on (section 10.3), a paste reaches the file manager as one event, never as keys, so no binding sees any of it, and it ends any pending sequence.

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Mode*], [*A paste*]),
  [normal, help], [is refused: nothing changes, and the bottom row says `Paste ignored (12 bytes): paste into a prompt line, such as / or r`],
  [insert], [goes into the line at the cursor, as one edit, once line breaks at its end are dropped; it is refused whole, with a message saying why, if it holds any other control character or would make the line longer than 4096 bytes],
)

#decision[
*A paste outside a prompt is refused out loud, and a paste that does not fit is refused whole.*
Today a paste in normal mode runs as commands. Dropping it without a word would leave the user wondering where it went, and opening a search with it would be a guess.
A path or name cut to fit is worse than none: it renames to, or searches for, something the user did not paste.
]

= Questions

Three things ask before they act, and each reads one key:

#table(
  columns: (auto, auto, 1fr),
  inset: 4pt,
  table.header([*Question*], [*Answers*], [*Asked by*]),
  [Delete "_name_"?, or Delete _n_ entries? with their names], [`y`], [`fmTrash`],
  [_name_ already exists], [`o` overwrite, `v` add a version number], [`fmPaste`, for each name that exists],
  [Set bookmark / Go to bookmark], [a key that types `0`--`9`, `a`--`z`, `A`--`Z`], [`fmBookmarkSet`, `fmBookmarkGo`],
)

Escape, or any key not offered, is no. The key is read with the file manager's decoder (section 10), so Escape under the kitty protocol works.
A question ends any pending sequence; its key never reaches a binding.

#decision[
*Answers are fixed.*
A question is a dialog: its box shows what to press, and the answer is a letter of a word on screen (`y`, `o`, `v`).
Binding them would need a mode per question, for keys no one has asked to change.
Bookmarks are the case that looks like a keymap, but their key is data, a bookmark's name, and making it a binding would mean one binding per bookmark.
ranger and lf do the same: `m<any>` and `mark-save` read the next key.
What a user can change is how a question is reached: `fmTrash` can be put on any key, or erased.
]

= The terminal layer

== One decoder

The three hand-written decoders go. The file manager reads its input with the shell's terminal lexer and `keyOfToken`, so every key the shell can read, the file manager can read:
modified keys, F1--F35, Home, End, Page Up and Down, Delete, Alt chords, UTF-8 text, the kitty protocol, and bracketed paste.
Its joining of a sequence split across reads (`pendingInput`) and its dropping of late terminal replies (`isTerminalReply`) carry over.
The reply to the kitty protocol query, `CSI ? flags u`, is dropped too.

== Escape, in both programs

Escape closes the prompt line and answers no to a question, and it has to do so when it is pressed.
The shell's rule (CLI design 10) cannot: there, the byte after an ESC is read with a blocking read, so an Escape does nothing until the next key arrives,
and that key is then taken as Alt and the key, or as the key alone. Escape is lost either way.

So there is one rule, for the file manager and the shell. It is the file manager's rule today, and crossterm's:

+ An ESC that ends what one read returned is `keyEscape`, unless that read filled the buffer, which means more bytes are waiting.
+ An ESC followed in the same read by a CSI or SS3 sequence is that key; an unfinished one waits for the rest, as today.
+ An ESC followed in the same read by one key is that key with `keyAlt` if a binding of the current mode starts with it or continues the pending keys with it; otherwise it is `keyEscape` and then the key.

Nothing waits for time, and nothing runs beside the reader: each rule looks only at bytes one read has already returned.
In the shell's lexer, rule 1 is one comparison: after an ESC, `StdinReaderState` has no bytes left (`i >= n`) and the read was shorter than the buffer.
Rule 3 is today's `EscOf` with one change: the fallback is Escape and then the key, not the key alone.

#decision[
*One rule, with no timer, goroutine or signal.*
The shell's blocking read has none either, but it cannot give the file manager an Escape that acts when pressed.
Two rules would mean two decoders and a `msh keys` that is right for only one of them. The read-boundary rule serves both, and makes the shell simpler:
the lexer no longer blocks for a second byte in the middle of a key.

What it costs, in the legacy encoding only:

- Escape and then a key, typed one after the other, is no longer Alt and that key in the shell. That is the Emacs habit of Escape as Meta; the CLI design kept it.
  Now Escape is a key of its own, unbound by default, so the key after it acts alone, which is what happens today when Alt and that key is not bound.
- The rule assumes a key's bytes arrive in one read. Terminals write a key's bytes in one write, and a slow link does not split them:
  ssh sends what its client read in one packet, and the server cannot use a packet until all of it has arrived, so slowness changes the gaps between keys, never the bytes within one.
  A split needs a terminal that writes one key in two writes. If one does, an arrow key becomes Escape and then `[A` typed as text.
  The serious case is the start of a paste, `ESC [ 200 ~`: split after the ESC, the pasted text would be read as keys, and in the shell a newline in it would run a line.
  Inside a paste nothing changes: the lexer reads to the end marker across reads, as it does today.
  How the Windows console delivers a key's bytes when it turns key events into escape sequences is not known; testing it is deferred (section 15).
- Escape and a key typed while the program is busy (the shell running a command, the file manager drawing a large preview) arrive in one read. Rule 3 keeps the Escape when Alt and that key is not bound.

Under the kitty protocol, Escape is `CSI 27 u` and Alt chords are their own sequences, so none of this applies.
]

`msh keys` decodes this way too, so it shows what both programs see.
The CLI design's section 10 and `ai/key-bindings/README.md` describe the old rule, and change with it.

== Kitty protocol and bracketed paste

The file manager pushes `CSI > 5 u` and turns on bracketed paste when it enters raw mode, and undoes both whenever it gives up the terminal (`fmRun`, the editor, a file opened in a terminal program, quitting).
This is the shell's pairing (`enterRawMode`, `leaveRawMode`), applied to the file manager's own raw mode, which it enters itself even when the shell runs it.
To know whether the terminal has the protocol, the file manager puts `CSI ? u` in front of the query it already sends at startup for sixel support (`CSI 16 t CSI 14 t CSI c`),
so the answer comes in the same round trip. `MSH_KITTY_KEYS=0` turns it off, as in the shell.

= Where bindings live

The table is on `EvalState`, beside the shell's, and `fmBindingAdd` in `init.msh` fills it.

- *`mshFileManager`* (Ctrl-O, a line, a script) uses the table of the evaluator that runs it. `RunFileManagerBuiltin` is given that evaluator, which it also needs to run quotations.
- *`msh fm`* today starts the file manager before loading anything. It will load `std.msh` and `init.msh` as a script does, then start it.
  The startup files' standard output goes to standard error, so `cd "$(msh fm)"` still gets only the directory.
  If a startup file fails, the file manager still starts, with the defaults, and shows the error at the bottom.
  Loading both files took 10 ms here (60 ms the first time, with a cold disk cache).

#decision[
*`msh fm` loads `init.msh`.*
A user who binds `g h` expects it wherever the file manager opens. `msh fm` is what other shells use (`cd "$(msh fm)"`), so leaving it on the defaults would split one tool into two.
Scripts already load `init.msh`; `msh fm` becomes one more program that does.
]

= Cost

The lookup is the shell's: an integer key code walked through a trie, at most 8 times 8 steps per key.
The file manager draws the whole screen after every key, which costs far more, so there is no allocation budget to keep here as there is on the shell's key path.

= Verification

+ *The resolver is the shell's.* It is factored out of `TermState` (section 14, step 1) without changing its behavior, so the TLA+ model (`ai/key-bindings/KeyResolution.tla`) and `TestResolveMatchesDefinition` cover both programs.
  The model needs no change: what the file manager adds to resolution is events that end a pending sequence (a mode change, a question), which the model already has as the paste.
  The oracle test gains a second instance, with the cursor as the state and `fmDown`/`fmUp` as the pure ops.
+ *Mode tracking.* Table tests for `fmBindingAdd`: each reason a binding is inert, and programs that cross modes and check, through each prompt and the help.
+ *Prompts and help.* `fmPromptAsk` with a quotation that returns a program, declines, fails and runs too long; Escape runs nothing.
  The help screen lists the defaults, a user's binding, and an inert one with its reason; scrolling past either end; closing it leaves the listing as it was.
+ *Defaults reproduce today.* A test runs the file manager over a temporary directory, feeds it bytes, and checks the directory, cursor, mode and line after each key, for every row of section 2, including `y` followed by a key that does not continue it.
+ *Selection.* `fmToggle` and `fmSelectionInvert` across two directories; each op of section 7.1 with and without a selection, and with a selected path removed in the meantime; `fmQuitNoCd` from `mshFileManager` and from `msh fm`.
+ *Pastes and the line limit.* A paste refused in normal and help mode, with its message; in insert mode, one that fits, one with a trailing line break, one with a tab, and one too long; typing at the limit.
+ *Decoder.* Table tests for the Escape rules, in the shell and the file manager: ESC alone in a read, ESC filling the buffer, ESC and a key with and without a binding that uses Alt and the key, a split CSI sequence, a late kitty reply.
  The shell's tests of today's ESC handling (`KeyToken.EscOf`) change with the rule.
  The three bugs of section 2 become regression tests.
+ *By hand, in a pty*: Ctrl-O from the shell and `cd "$(msh fm)"` from bash, in a kitty-protocol terminal and in tmux; `e` under `msh fm`; a paste in both modes.

= Implementation plan

+ *Factor the resolver.* The pending window, snapshots, the trie per mode, timing and the conflict rule leave `TermState`.
  A binding exposes its keys, mode, timing and whether it is pure; the program using the resolver supplies the snapshot and restore, a key's own action, and running a program.
  The shell's tests must pass unchanged.
+ *Escape.* The rule of section 10.2 in the shell's lexer, replacing the blocking read after an ESC; `EscOf` falls back to Escape and then the key. The CLI design's section 10 and the key-bindings README are updated.
+ *Decoder.* The terminal lexer over the file manager's input, bracketed paste, kitty detection in the sixel query.
+ *Types and builtins.* The declarations of section 5.1 in `builtinDeclSource`; `fmBindingAdd`, `fmBindingErase`, `fmBindingList` in `Evaluator.go`, `BuiltInList.go` and `TypeCoreBuiltins.go`; inert reasons, including mode tracking.
+ *The prompt line.* One line editor for every prompt on `SourceText` and a byte cursor, with the shell's grapheme functions and the file manager's word rule; `fmPromptAsk` and its quotation.
+ *Ops.* Each case of the three handlers becomes an op; the handlers become decode, resolve, run.
+ *Defaults* (section 5.4), as a Go table like the shell's.
+ *Popup* from the trie, replacing `renderPrefixOverlay` and `lastKey`.
+ *Help screen* and help mode, sharing the popup's way of writing a binding.
+ *Questions* read through the decoder.
+ *Selection*: the set of paths, its marks and count, the ops of section 7.1 over it, and `fmQuitNoCd`.
+ *Pastes and the line limit* (sections 7.4 and 8.1).
+ *`fmFunc`, `fmRun`, `fmRunWait`, `fmRunOps`*, with the terminal hand-over of section 7.3; children of `msh fm` get `/dev/tty` as output, which also fixes `e`.
+ *`msh fm` loads the startup files.* `RunFileManagerInteractive`, which nothing calls, is removed.
+ *Tests* of section 13.
+ *Documentation.* The file manager has no user documentation. A "File manager" section in `getting-started.inc.html` after "Key bindings": what it is, how to start it, the default keys, bindings with these examples, questions, the Escape rule.
  The builtins in `functions.inc.html`, the type names in `type_system.inc.html`, one line in `mshell.md` that the names are reserved, and `CHANGELOG.md`.
  The `msh fm` command line does not change, so the completions do not.

= Deferred

No questions are open. One thing is left for later, by decision (2026-10-22):

+ *A key split across reads* (section 10.2). Not tested on the Windows console or Windows Terminal.
  If it can happen, one guard with no timer: when a read ended in a lone ESC and the next read begins with `[200~`, take it as the start of a paste.
  Escape has already acted, but the pasted text is not run as keys. A person cannot type `[200~` within one read.
