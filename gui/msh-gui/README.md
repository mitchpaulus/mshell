# msh-gui

The mshell GUI, first version (`ai/gui-terminal.typ`, step 5 of "Suggested order of work"):
one window, one pane of blocks, and the prompt edited locally.

```
../mshell/build.sh            # msh with `msh server` (from the repository root: mshell/build.sh)
cargo run --release -p msh-gui
```

It starts `msh server` (from `$MSH_BIN`, else the repository's `mshell/mshell`, else `msh` on `PATH`),
with your `init.msh`, so your bindings and abbreviations work at the prompt.
`MSH_GUI_FONT` and `MSH_GUI_FONT_SIZE` pick the monospace font (default Noto Sans Mono, 15 px),
for program output, the prompt and file previews;
`MSH_GUI_UI_FONT` the proportional font of the app's own text (default Noto Sans), whose missing characters come from the monospace font.

- Not a terminal emulator: each command is a card (a header with the command, a status dot,
  its time and exit code, then its output, which alone runs in its own libghostty-vt terminal),
  and the prompt is a box of its own.
- The prompt box sits where a terminal's prompt would: just after the last card, at the top of a clean screen.
  Once the cards fill the pane, or while scrolled back, it stays at the bottom, like frozen panes in a spreadsheet.
- The prompt is yours: the engine runs `setPrompt`'s quotation (or the built-in prompt) and sends it with its colours.
- The line is coloured as terminal msh colours it: strings, paths, variables, booleans, dates,
  and on a command line the program and the builtins and definitions underlined.
  The history suggestion shows after it, dimmed: Ctrl-F at the end of the line or Ctrl-Y takes it.
  Up and Down, Ctrl-P and Ctrl-N (prefix search) and Alt-. (last argument) run in the engine, on msh's history;
  each line run is saved to it at once.
- Tab completes as terminal msh does, run by the engine; the candidates show in a menu above the prompt box,
  and Tab, Shift-Tab and the arrows move through them.
- The prompt is edited by the Rust port of msh's editor (`gui/msh-editor`):
  bindings, chords with their timings, abbreviations, `bindingFunc`s and `abbrFunc`s run by the engine.
- While a block runs, keys go to its program, encoded by libghostty from the terminal's modes.
  A program on the alternate screen (`less`, `vim`, `htop`) gets the whole pane.
- The mouse wheel scrolls back through the blocks, including long output kept in a block's scrollback.
  Clearing the screen (Ctrl-L) puts the cards so far above the view, as a terminal does; scrolling up shows them.
- `editRun` and `editRunOps` bindings run their quotation as a card (named by the binding's keys); the line stays.
- The file manager (`mshFileManager`, `;r`-style bindings, Ctrl-O) is a dialog over its pane.
  Its logic stays in the engine, so its keymap, `fmBindingAdd` bindings, quotations and previews are terminal msh's;
  the engine sends the view and the dialog sends the keys (`mshell/FileManagerGUI.go`).
  Entries have vector icons (a folder, a page), drawn from path data with swash's rasteriser.
  When it hands the terminal to a program (an editor), the dialog hides and the program runs in the pane.
- Tabs hold panes, each with its own engine, laid out as a tiling window manager does.
  The window's keys come before the editor's and a program's:

  | Key | |
  |---|---|
  | Alt-Enter | new tab |
  | Alt-N, Alt-P | next, previous tab |
  | Alt-H/J/K/L | focus the pane left, below, above, right |
  | Alt-; | split: a new pane in this tab |
  | Alt-R | reorder: swap the pane with the first |
  | Alt-' | next layout: everything (the focused pane), columns, right halves, right thirds |

## Checking it without a window

```
msh-gui --script 'echo hi<Enter><wait>' --dump frame.ppm [--size 100x30]
```

runs the app headless from a script of keys and writes the frame it would draw (`src/script.rs` has the syntax).

## Not yet

Selection and copy, mouse reporting, the mouse in the file manager, image previews,
resizing finished blocks, fallback fonts, wide characters and bold.
A cell shows its first code point, one cell wide.
