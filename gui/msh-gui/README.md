# msh-gui

The mshell GUI, first version (`ai/gui-terminal.typ`, step 5 of "Suggested order of work"):
one window, one pane of blocks, and the prompt edited locally.

From `gui/`:

```
msh ghostty-vt-sys/fetch.msh  # once per OS: builds libghostty-vt into gui/third_party/ (Zig 0.17.x, ~5 min)
msh msh-gui/run.msh           # builds msh into gui/target/msh-engine/, then cargo run --release -p msh-gui with it
```

`run.msh` passes its arguments on to msh-gui.
It starts `msh server` (the msh given as an argument, else `$MSH_BIN`, else the repository's `mshell/mshell`
(`mshell/msh.exe` on Windows), else `msh` on `PATH`),
with your `init.msh`, so your bindings and abbreviations work at the prompt.

On Windows, `msh server` runs on a pseudoconsole from Microsoft's ConPTY (`conpty.dll` and `OpenConsole.exe`),
which msh-gui ships beside `msh-gui.exe` and names to the engine in `MSH_CONPTY`; msh alone never needs it.
`msh msh-gui/fetch-conpty.msh` downloads the pinned package, checks it and unpacks it into `gui/third_party/`,
where msh-gui and the engine's tests (`go test` in `mshell/`, `Server_windows_test.go`) find it;
`run.msh` does this when it isn't there.
`ai/windows-server-experiments.md` has why the ConPTY built into Windows won't do.

`MSH_GUI_FONT` and `MSH_GUI_FONT_SIZE` pick the monospace font (default Noto Sans Mono, Consolas on Windows, 15 px),
for program output and file previews;
`MSH_GUI_UI_FONT` the proportional font of the app's own text (default Noto Sans, Segoe UI on Windows);
`MSH_GUI_PROMPT_FONT` the prompt box's: the prompt, the line you type, and the completion menu (default the UI font).
A monospace prompt font lays the box out as cells would, with a block cursor; a proportional one has a bar.
A character a font lacks comes from the monospace font, and one that lacks too from the first fallback font that has it:
`MSH_GUI_FALLBACK_FONTS` (files, separated as in `PATH`), else Segoe UI Symbol and Segoe UI Emoji on Windows,
and on Linux the Noto Sans Symbols, Symbols 2 and Emoji fonts that are installed.
Emoji are drawn in the text's colour, from their outlines.
Each font's bold and italic come from the files beside it (`NotoSans-Bold.ttf` beside `NotoSans-Regular.ttf`;
the Windows defaults' own, `consolab.ttf` and the rest), or from `{VAR}_BOLD`, `{VAR}_ITALIC` and `{VAR}_BOLD_ITALIC` (`MSH_GUI_FONT_BOLD`);
a style without a file is made from the nearest one, emboldened or slanted (Noto Sans Mono has no italic).

- Not a terminal emulator: each command is a card (a header with the command, a status dot,
  its time and exit code, then its output, which alone runs in its own libghostty-vt terminal),
  and the prompt is a box of its own.
- The prompt box sits where a terminal's prompt would: just after the last card, at the top of a clean screen.
  Once the cards fill the pane, or while scrolled back, it stays at the bottom, like frozen panes in a spreadsheet.
- The prompt is yours: the engine runs `setPrompt`'s quotation (or the built-in prompt) and sends its text and its typed styles
  (colours, bold, italic, faint, underline, reverse and the rest), not escape sequences; the GUI draws them.
- The prompt box is laid out as a GUI lays out text: in pixels, by each glyph's measured width, wrapping between words.
  The cards above it are terminals, in cells.
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
  The cursor takes the shape the program sets (block, bar, underline, hollow block: Neovim's modes),
  and underlines show in their five styles and colours (Neovim's spelling), as does strikethrough.
  A cursor the program asks to blink blinks, but shows while you type; the program's cursor colour (OSC 12) is used.
  While the window hasn't the keyboard, the cursor is a still, hollow block.
  Text is bold, italic, faint, blinking, concealed or overlined as the program asks.
  Synchronized output (mode 2026) is honoured: while a program redraws (Neovim does), the last frame stays, for up to a second.
  The bell flashes the pane, and, when the window hasn't the keyboard, asks the desktop for attention.
- Output is flow-controlled, as a terminal's is: a program writing without end (`cat /dev/random`) goes only as fast as
  the GUI draws it, memory stays bounded, frames keep coming, and Ctrl-C reaches it.
  A program that stops reading its input can't stall the engine or the GUI;
  input it leaves unread is discarded when its block ends.
- The mouse wheel scrolls back through the blocks, including long output kept in a block's scrollback.
  Clearing the screen (Ctrl-L) puts the cards so far above the view, as a terminal does; scrolling up shows them.
- `editRun` and `editRunOps` bindings run their quotation as a card (named by the binding's keys); the line stays.
- The file manager (`mshFileManager`, `;r`-style bindings, Ctrl-O) is a dialog over its pane.
  Its logic stays in the engine, so its keymap, `fmBindingAdd` bindings, quotations and previews are terminal msh's;
  the engine sends the view and the dialog sends the keys (`mshell/FileManagerGUI.go`).
  Entries have vector icons (a folder, a page), drawn from path data with swash's rasteriser.
  The mouse acts on the press, not the release: a press on an entry puts the cursor on it (Ctrl-press selects it too),
  and a second press there soon after opens it (`fmOpen`);
  a press on a name in a directory's preview opens that directory with the cursor on the name, and a second opens the name.
  The wheel scrolls the listing (or the help), the cursor kept in view, and the Back button goes to the parent.
  When it hands the terminal to a program (an editor), the dialog hides and the program runs in the pane.
- Tabs hold panes, each with its own engine, laid out as a tiling window manager does.
  A split shows the stack: the first pane on the left, the others in rows on the right.
  A split that would leave a pane smaller than 20 by 8 cells is refused, with a message under the prompt.
  The bar at the top shows the tabs, and, while the tab has more than one pane,
  its layout: a little map of it, the focused pane in the accent colour, and its name
  (in everything, which pane of how many).
  The window's keys come before the editor's and a program's:

  | Key | |
  |---|---|
  | Alt-Enter | new tab |
  | Alt-N, Alt-P | next, previous tab |
  | Alt-H/J/K/L | focus the pane left, below, above, right |
  | Alt-; | split: a new pane in this tab |
  | Alt-R | reorder: swap the pane with the first |
  | Alt-' | next layout: everything (the focused pane), stack, columns, right halves, right thirds |

## Checking it without a window

```
msh-gui --script 'echo hi<Enter><wait>' --dump frame.ppm [--size 100x30]
```

runs the app headless from a script of keys and writes the frame it would draw (`src/script.rs` has the syntax).

## Not yet

Selection and copy, mouse reporting, image previews,
resizing finished blocks, fallback fonts and wide characters.
A cell shows its first code point, one cell wide.
