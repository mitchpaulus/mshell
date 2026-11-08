# msh-gui

The mshell GUI, first version (`ai/gui-terminal.typ`, step 5 of "Suggested order of work"):
one window, one pane of blocks, and the prompt edited locally.

```
../mshell/build.sh            # msh with `msh server` (from the repository root: mshell/build.sh)
cargo run --release -p msh-gui
```

It starts `msh server` (from `$MSH_BIN`, else the repository's `mshell/mshell`, else `msh` on `PATH`),
with your `init.msh`, so your bindings and abbreviations work at the prompt.
`MSH_GUI_FONT` and `MSH_GUI_FONT_SIZE` pick the font (default Noto Sans Mono, 15 px).

- Not a terminal emulator: each command is a card (a header with the command, a status dot,
  its time and exit code, then its output, which alone runs in its own libghostty-vt terminal),
  and the prompt is a box of its own at the bottom.
- The prompt is yours: the engine runs `setPrompt`'s quotation (or the built-in prompt) and sends it with its colours.
- Tab completes as terminal msh does, run by the engine; the candidates show in a menu above the prompt box,
  and Tab, Shift-Tab and the arrows move through them.
- The prompt is edited by the Rust port of msh's editor (`gui/msh-editor`):
  bindings, chords with their timings, abbreviations, `bindingFunc`s and `abbrFunc`s run by the engine.
- While a block runs, keys go to its program, encoded by libghostty from the terminal's modes.
  A program on the alternate screen (`less`, `vim`, `htop`) gets the whole pane.
- The mouse wheel scrolls back through the blocks, including long output kept in a block's scrollback.
  Clearing the screen (Ctrl-L) puts the cards so far above the view, as a terminal does; scrolling up shows them.
- `editRun` and `editRunOps` bindings run their quotation as a card (named by the binding's keys); the line stays.
- Tabs hold panes, each with its own engine, laid out as a tiling window manager does.
  The window's keys come before the editor's and a program's:

  | Key | |
  |---|---|
  | Alt-Enter | new tab |
  | Alt-N, Alt-P | next, previous tab |
  | Alt-H/J/K/L | focus the pane left, below, above, right |
  | Alt-S | split: a new pane in this tab |
  | Alt-R | reorder: swap the pane with the first |
  | Alt-Space | next layout: everything (the focused pane), columns, right halves, right thirds |

## Checking it without a window

```
msh-gui --script 'echo hi<Enter><wait>' --dump frame.ppm [--size 100x30]
```

runs the app headless from a script of keys and writes the frame it would draw (`src/script.rs` has the syntax).

## Not yet

History, selection and copy, mouse reporting, a GUI file manager (the terminal one runs in its pane),
resizing finished blocks, fallback fonts, wide characters and bold.
A cell shows its first code point, one cell wide.
