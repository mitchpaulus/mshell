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

- Each command is a block: a header with the command, a status dot, its time and exit code,
  then its output in its own libghostty-vt terminal, with colours.
- The prompt is edited by the Rust port of msh's editor (`gui/msh-editor`):
  bindings, chords with their timings, abbreviations, `bindingFunc`s and `abbrFunc`s run by the engine.
- While a block runs, keys go to its program, encoded by libghostty from the terminal's modes.
  A program on the alternate screen (`less`, `vim`, `htop`) gets the whole pane.
- The mouse wheel scrolls back through the blocks, including long output kept in a block's scrollback.

## Checking it without a window

```
msh-gui --script 'echo hi<Enter><wait>' --dump frame.ppm [--size 100x30]
```

runs the app headless from a script of keys and writes the frame it would draw (`src/script.rs` has the syntax).

## Not yet

History, completion, selection and copy, mouse reporting, `editRun`, the user's prompt (`setPrompt`),
resizing finished blocks, fallback fonts, wide characters and bold.
A cell shows its first code point, one cell wide.
