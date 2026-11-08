// Design: a dedicated GUI terminal and shell for mshell.
// Build: typst compile ai/gui-terminal.typ

#set document(title: "mshell GUI terminal", author: "Claude (draft for Mitchell)")
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

#align(center)[
  #text(size: 20pt, weight: "bold")[mshell GUI terminal]
  #v(0.3em)
  #text(size: 11pt)[Design notes --- 2026-10-25, updated 2026-11-07, branch `gui-terminal-design`]
  #v(0.2em)
  #text(size: 10pt, style: "italic")[Early thinking, nothing decided. Nothing is built yet.]
]

#v(1em)

= Goal

A dedicated GUI app for mshell: a terminal and shell together, built around the language rather than a generic character grid.
For now it is for Mitchell and a few close friends, not a public product.
It should run on Windows, macOS and Linux.

The top requirement is *performance*: start instantly, and never lag on anything (@sec-perf).

Features we want:

- Much better prompt editing.
- A live view of the stack.
- Easy searching through the output of earlier commands.
- Tabs and split panes.
- Full support for ordinary terminal programs and full-screen TUI apps.
- Full control over the UI: we draw everything, not a native widget toolkit.

Longer term: move away from the terminal as the main abstraction, keeping it as a fallback (@sec-beyond).

Deferred: IME and screen-reader accessibility (@sec-deferred).
The design shouldn't block adding them later, but they aren't built now.

= Performance <sec-perf>

Performance comes before features.
A feature that adds lag doesn't ship until it stops adding lag.

== Targets <sec-targets>

These are proposals to measure against, not agreed numbers.

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*What*], [*Target*]),
  [Launch to first frame with an editable prompt], [Under 100 ms, slowest launches included, with files in the page cache; no slower than Alacritty or Ghostty on the same machine],
  [New tab or pane], [Shown in the next frame; engine ready in under 20 ms],
  [Keystroke to frame], [Frontend work under 1 ms; the frame never waits on the engine; never miss a frame],
  [Output flood (`cat` of a huge file, `yes`)], [UI keeps responding to input and scrolling; throughput comparable to Alacritty (vtebench)],
  [Scrolling through a huge history], [At the display refresh rate, with no hitches],
  [Steady state], [No per-frame allocations; no work when nothing changes],
)

#note[`msh -c '1 wl'` starts in about 11 ms on the FX-8350 machine (hyperfine, 2026-10-25).
So the engine is unlikely to be the slow part of startup.
GPU setup and font loading were the likely problems (@sec-startup).

The startup spike (`gui/startup-spike`, 2026-11-07) gets a first frame to the compositor 18–19 ms after launch on that machine (median),
against 160 ms for Alacritty, 279 ms for WezTerm and Konsole, and 462 ms for Ghostty, measured the same way.
Drawn by the GPU instead, its first frame takes 44–68 ms, depending on how much work goes into GPU startup.
Even the plainest version (stock winit, Vulkan only, GPU on the first frame) stayed under 90 ms in all 70 launches measured,
and under 75 ms on most of them with implicit Vulkan layers disabled.
So 100 ms leaves room for the prompt and the engine, which the spike doesn't have yet.]

Alacritty, Ghostty and WezTerm are installed on the benchmark machine, so they can serve as references.

== Startup <sec-startup>

- *Show the window and the prompt before the engine is ready.*
  Start the engine process at the same time as GPU setup.
  The user's key bindings don't exist until the engine has run `init.msh`,
  so keys typed before the engine sends them are queued, each with the time it arrived,
  and handled as soon as they arrive (@sec-editor-config).
- *The first frame doesn't wait for the GPU.*
  On the FX-8350 machine, getting a Vulkan device takes 20–30 ms even after every fix below, most of it loading the driver (RADV links libLLVM).
  So a frame thread draws the first frame on the CPU into shared memory while the window is being created,
  the main thread shows it as soon as the window exists,
  and the GPU takes over when it's ready, with the same pixels (measured: `gui/startup-spike/README.md`).
- *Start the GPU at the top of `main`, on its own thread,* and keep it off the main thread's path:
  - only the Vulkan backend (wgpu initialises OpenGL too by default: ~15 ms),
  - no implicit Vulkan layers (Mesa's device-select layer opens its own compositor connection: ~10 ms),
  - only the Vulkan driver that worked last time (the loader otherwise loads every installed driver),
  - before the GPU thread starts, create every thread and do everything else that takes glibc's loader lock
    (`dlopen`, `dlsym`, thread-local destructors), and map the first frame's memory,
    because loading the driver holds that lock, and the kernel's `mmap_lock`, for milliseconds at a time.
- *Defer what the first frame doesn't need.*
  Inside libraries too, where it pays: winit compiles the keymap and parses the Compose file before the first frame (~5–12 ms).
  A small patch to winit defers them (kept as `gui/startup-spike/winit-defer-keymap.patch`),
  but it isn't worth carrying a copy of winit for under a 100 ms target; send it upstream, or apply it if the target tightens.
- *Fonts: never scan the system's fonts at startup.*
  Load the configured font file directly.
  Load fallback fonts (emoji, CJK) only when a glyph is missing.
  Cache the rasterised ASCII atlas on disk, keyed by font file, size and display scale: then startup doesn't read the font at all.
  The display scale isn't known until the window exists, so the first frame uses the last one seen.
- *Load everything else lazily:* history, the LSP, the search index, completions.
- One binary with few dynamic libraries.
  Pre-`main` work measured about 2 ms; a non-PIE build and skipping Rust's runtime setup save about 1 ms of it,
  but that didn't reach the first frame, which waits on the compositor.
- Optional later: a single-instance mode, where launching again opens a window in the already running process.
  That makes new windows instant, at the cost of one crash taking down every window.
- Keep one warm spare engine process ready, so a new tab doesn't wait for process startup.

== Input <sec-input>

- The prompt is edited in the frontend (@sec-editor).
  The rule is that *the frame never waits on the engine*.
  Most keys never leave the frontend.
  A key that runs mshell code (a `bindingFunc` or an `abbrFunc`) waits for the engine,
  but only the line's contents wait, not the frame.
- Highlighting is done in the frontend, by a Rust port of the lexer (@sec-port).
  Abbreviation matching on Space needs the lexer too, so the port is needed anyway,
  and asking the engine for highlighting would put a round trip on the most common keys.
- Completions, diagnostics and stack types arrive asynchronously and never hold up a frame.
- Same rule as the interactive line editor: no allocations on the keystroke path; reuse buffers.

== Rendering

- Draw only when something changes; no continuous render loop.
- Track which regions changed and redraw only those where possible.
  For terminal output, libghostty-vt reports this directly: each render-state update is clean, partly dirty or fully dirty, with dirty flags and stable ids per row (@sec-vt).
- Respect synchronized output (mode 2026): hold the block's frame while the program asks for it.
- Text goes through a glyph atlas and is drawn as instanced quads.
- Fast path for monospace terminal cells: plain ASCII doesn't need full text shaping.
- Lay out only the blocks that are visible.
  Finished blocks keep compact storage and are laid out again only when scrolled into view.
- Keep presentation latency low: avoid extra buffering, and on Wayland use frame callbacks.

== Output <sec-output>

- The engine reads PTY output in large chunks and sends it on raw, without re-encoding.
- The frontend runs the VT parser (libghostty-vt, @sec-vt) off the UI thread and draws at most once per display refresh, however much output arrives.
  The UI thread holds the terminal's lock only to start a render-state update; finishing it and drawing happen while the parser keeps going.
- Backpressure: if the frontend falls behind, the engine stops reading from the PTY, rather than buffering without limit.
  The program then blocks on write, which is what a normal terminal does too.
- Large mshell values (big grids) are paged on demand, not sent whole.
- The search index is built incrementally on a background thread.

== Measuring <sec-measuring>

Build the measuring tools from the start, not afterwards:

- a startup trace (process start, window, GPU ready, font ready, first frame, engine connected),
- keystroke-to-frame latency,
- vtebench throughput,
- frame-time histograms while scrolling and during output floods.

Run them on the benchmark machine and compare against Alacritty and Ghostty.

= The central idea: blocks, not a character grid

A normal terminal is a grid of characters with a shell writing into it.
It can't tell the prompt apart from program output, so features like "search the output of command 12" have to be bolted on afterwards.

In this app, the prompt editor, the stack view and the history are real UI pieces owned by the app.
Each command becomes a *block*:

- the input text,
- the stack before and after,
- the output (structured values, terminal output, or both),
- the exit status, and when the command started and how long it took,
- the working directory it ran in.

Most of the features below come from having blocks.

= Features

== Prompt editing

The prompt is a real text editor: mouse selection, several lines, normal cursor movement, undo.
Highlighting uses a Rust port of the mshell lexer (@sec-port).
Completions, diagnostics and hover come from the existing LSP (`mshell/lsp.go`), so the GUI doesn't need its own version of each.
The existing key binding and abbreviation systems carry over unchanged, so bindings work the same in the terminal and the GUI;
@sec-editor covers how.

== Live stack

After each command, the stack panel shows the actual values.
Lists, dicts and grids show as real tables, with sorting and scrolling, not as printed text.
No other terminal can do this, so this is where we'd stand out.

A second, more ambitious feature: as you type, show the _types_ the type checker expects on the stack, before anything runs.
That has no side effects, so it is safe to update on every keystroke, as long as it runs asynchronously (@sec-input).
How well this works depends on how far the type checker gets.

== Searching output

Each block keeps its own output, so search can be scoped:

- search everything, or within one block,
- filter by exit status, command name, directory or time,
- fold, copy or save one block's output,
- take an earlier block's output back in as a value (syntax to be decided).

For terminal output, the searchable text comes from the block's emulator, both the screen and its scrollback.
libghostty-vt's incremental search does this, and its work steps can run on a background thread while the terminal keeps receiving output (@sec-vt).
Copying or saving a block's output goes through its formatter, as plain text or with styles.
For mshell values, search runs over the values themselves.

== Tabs and panes

Ordinary UI work once blocks exist.
Each pane is one session with its own engine process and PTY (@sec-pty), and shows a list of blocks with the prompt editor at the bottom.

= Terminal programs and TUI apps <sec-pty>

This is where the real complexity is.

#decision[*One PTY per pane* (decided 2026-11-07), because it is what `msh` does today and has the least complexity.
Each pane has its own engine process (@sec-arch).
The engine calls `setsid` and takes the pane's PTY as its controlling terminal, just as terminal `msh` has its terminal.
Every block in the pane uses that PTY in turn.]

Why not a PTY per block: a PTY can be the controlling terminal of only one session,
a process group can be put in the foreground only on its own session's terminal,
and processes can only join groups in their own session.
So one engine process can drive only one PTY with job control,
and a PTY per block would need a helper process per block to be each one's session leader.

With one PTY per pane, the existing job control is reused unchanged:
each pipeline gets its own process group (`Evaluator.go`), which is put in the foreground of the terminal (`SetForegroundProcessGroup` in `Pathbin_linux.go`).
So pipelines, Ctrl-C, `sudo` password prompts, and programs that open `/dev/tty` (`git log | fzf`) work exactly as they do in a terminal.

The rules:

+ *Every block that runs an external program has its own terminal emulator* (a fresh libghostty-vt terminal, @sec-vt), fed from the pane's PTY.
  Even "plain" programs print colours, `\r` progress bars and cursor moves,
  so every block needs a full emulator, not only TUI ones.
  The engine sends `block_started` and `block_finished` in order with the PTY output, and the frontend sends the bytes in between to that block's emulator.
  A fresh emulator per block means modes a crashed program left on (mouse tracking, bracketed paste) don't leak into the next block.
  The kernel's terminal settings (termios) do carry over, as in a terminal.
+ *While a foreground process is running, keystrokes go to its PTY.*
  That covers `python`, `ssh`, `sudo` password prompts and fzf's inline mode, with no special cases.
+ *When a program switches to the alternate screen (DECSET 1049)*, its block grows to fill the pane and gets raw input.
  When it exits, the block shrinks back to a summary.
+ *Pure mshell values never touch a PTY.*
  They go straight to the block as structured data.

Things still to work out:

- Background jobs: they share the pane's PTY, so their output lands in whichever block is current, as in any terminal.
  Giving them their own block later would mean a helper process and a PTY for each background job only.
- Resizing: each block's emulator needs a width; the active block follows the pane width.
  Do finished blocks reflow or keep their width?
- A command that writes both mshell values and terminal output: what order is shown.
- Windows: one pseudo console per pane, with each child attached to it through `CreateProcess` attributes, which Go's `os/exec` doesn't expose, so Windows needs its own spawning code.
  ConPTY also behaves differently from Unix PTYs (it rewrites output and has its own resize quirks).
  libghostty-vt has an option for exactly this: turn off pulling rows back from scrollback on resize, since ConPTY keeps its own screen buffer.

#decision[Do not write the VT emulator ourselves.
Getting it right takes decades of edge cases; use a battle-tested one.
*All interpretation of escape sequences and program output goes through libghostty-vt* (@sec-vt).
No other part of the app, frontend or engine, parses escape sequences.]

= VT emulation: libghostty-vt <sec-vt>

`libghostty-vt` 1.0 was released on 2026-11-06 (tag `vt-1.0.0`).
It is the terminal emulation core of Ghostty, as a C library: parsing, terminal state, screen and scrollback, modes, and the protocols built on them.
Both the library and its C ABI are stable from 1.0 and follow semantic versioning.
It is MIT licensed with no copyleft dependencies, needs nothing beyond libc at runtime, and creates no threads of its own.
JetBrains' IDE terminal moved to it in 2026.3.

It has *no* renderer, fonts, text shaping or PTY code.
That is the split this design wants: PTYs live in the Go engine (@sec-arch), and the frontend owns drawing and text (@sec-frontend).

Every block that runs an external program has its own libghostty-vt terminal.
The engine sends PTY bytes on raw (@sec-output), and the frontend writes them into that terminal.
Anything the frontend wants to know about the output, it asks the terminal for.

== What it covers for us

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Need*], [*libghostty-vt*]),
  [Replies to queries (DA, DSR, size reports, …)], [A PTY-write callback. Its bytes go back to the engine as an `input` message for the block.],
  [Drawing], [A render state is a copy of one frame's rows, cells, cursor and colours. Updating it is split in two, so only the first half needs the terminal's lock. Dirty state per update and per row, and stable row ids.],
  [Synchronized output (mode 2026)], [A render-hold callback.],
  [Keys and mouse to the program], [Key and mouse encoders that take their settings from the terminal's modes, including the Kitty keyboard protocol. The frontend encodes keys and sends the bytes.],
  [Alternate screen (@sec-pty)], [No callback when it changes. Read the active screen after each write, which is a cheap getter.],
  [OSC 133 and OSC 7], [Callbacks for each step of a command (with exit code and command line) and for directory changes. Not how our own blocks are found (the protocol does that), but it makes a nested shell work, e.g. bash over `ssh` inside a block.],
  [Clipboard (OSC 52), title, bell, notifications, progress, reset], [Callbacks.],
  [Search], [Incremental search over the screen and scrollback of both screens; its work steps are safe on a background thread (@sec-output).],
  [Many emulators at once], [Scrollback limits in bytes and lines, a memory-usage query, and scrollback compression that can run in small steps when idle.],
  [Compact finished blocks], [The whole terminal can be saved to bytes and restored. A block scrolled far out of view could be kept that way. The byte format isn't covered by the ABI promise, which doesn't matter inside one process.],
  [Selection, Kitty graphics, plain or styled text export], [Included.],
)

== Costs and gaps

- *Zig to build it.* `vt-1.0.0` needs Zig 0.17.x exactly (`minimum_zig_version`; 0.16 and 0.18 are refused).
  The signed release tarballs build offline in ~5 minutes on the FX-8350 (`gui/ghostty-vt-sys/fetch.sh`).
  It links statically with no other libraries, so one binary is still possible (@sec-startup).
- *Rust bindings are community-maintained.* `libghostty-rs` has a raw `-sys` crate and safe wrappers, but on 2026-11-07 it pinned a Ghostty commit 245 commits before `vt-1.0.0`.
  Now the ABI is stable, our own thin `-sys` crate generated with bindgen is a small, low-risk fallback.
- *Threading is ours to arrange.* The render-state split and the search steps are documented as safe alongside writes; everything else on one terminal has to be serialized, e.g. one lock per terminal shared by the parser thread and the UI thread.
- *Scrollback must be limited explicitly.* The default is 10,000 bytes, which is one ~400 KB page in practice.
  Unlimited, history costs a full row of cells per line however short: 67 MB of short lines took 1.3 GB.
  So each block gets a limit in bytes and lines, and finished blocks are compressed.
- *No shaping.* It handles graphemes, not shaping or layout; that stays with the text library (@sec-recommendation).

Measured in the startup spike (2026-11-08, `gui/startup-spike/README.md`):
- Creating a terminal takes 12 µs warm (47–130 µs for the first in a process), and freeing one 12 µs. A terminal per block is cheap.
- An idle 100 x 30 terminal holding one screen of output takes 34–38 KB, or 175 KB when every character has its own 24-bit colour.
- Parsing runs at 75–214 MB/s with the default scrollback, depending on how much styling the output has, and 66–82 MB/s when every line is kept, where the time goes into new pages.
- At startup it costs nothing measurable: under 1 ms on the frame thread, before the window exists.

Still to compare: throughput against Alacritty and Ghostty on vtebench, which measures the whole terminal.

= Beyond the terminal <sec-beyond>

The longer-term aim is to move away from the terminal as the main abstraction,
and keep it only as a fallback for programs that speak nothing else.
This section collects prior art and a proposal.
Nothing here is decided.

== Prior art

The ideas fall into six groups.

*Output is a value, not bytes.*

- Symbolics Genera and CLIM (1980s): everything printed is a _presentation_, which remembers the object it came from and that object's type.
  When a command expects an argument of some type, every earlier output of that type on screen becomes clickable, and clicking it supplies the object.
  That answers open question 6 with no syntax at all.
- PowerShell: objects stay objects through the pipeline, and are only formatted as text by the host.
  A GUI host can render them differently.
- Jupyter and Mathematica notebooks: a result is a bundle of formats (plain text, HTML, image), and the front end shows the richest one it can render.
- TermKit (2011): split a program's output into a data channel and a separate view channel.
  Abandoned, but influential.
- libxo (FreeBSD): one command can emit text, JSON, XML or HTML.

*A semantic UI protocol, with the character grid as a fallback.*

- Neovim's remote UI protocol: it started as a single character grid, then added a grid per window and events for the popup menu, the command line and messages.
  GUIs such as Neovide draw those as native widgets.
  Probably the most successful real example.
- Arcan's TUI library and its shell, Lash\#Cat9: no terminal emulator;
  legacy programs run in a separate window that emulates one.
  See #link("https://arcan-fe.com/2025/01/27/sunsetting-cursed-terminal-emulation/")[Sunsetting Cursed Terminal Emulation] (2025).
- Textual: an app describes a widget tree, which is drawn either to a terminal or to a browser.

*The session is a document.*
Acme and 9term on Plan 9 have no cursor addressing at all, so curses programs don't run.
Output is editable text, and any text can be run by middle-clicking it.
Oberon and the MPW Shell's worksheets work similarly, as do Emacs shell buffers and notebooks.

*The shell as an engine behind a UI.*
Mathematica (1988) and Jupyter separate the front end from the kernel.
Oils' #link("https://www.oilshell.org/release/0.9.8/doc/headless.html")[headless mode] is closest to @sec-arch:
a GUI starts `osh --headless` and talks to it over a Unix socket, with a protocol called FANOS (file descriptors and netstrings over sockets).
Because it passes file descriptors, the GUI can own the terminal and hand it to the shell for each command,
which bears on the PTY questions in @sec-pty.
It is still in progress.

*UI generated from command descriptions.*
MPW Commando built a dialog from a command's description of its options.
Genera's command processor prompted for arguments by type.
Gooey turns Python argparse programs into forms.

*Jobs as objects.*
In Cat9, a job's output is an object you can re-run, view differently, or detach into its own window.
Wave Terminal and Warp's blocks are lighter versions of this.

Two smaller ones:
Mosh's guessed local echo over SSH is prior art for guessing ahead with key bindings (@sec-editor-ask),
and FinalTerm (2013) is where OSC 133 came from (@sec-osc).

== Why most of these failed

TermKit, FinalTerm, Upterm and Black Screen all died, mostly because:

- they needed programs to change,
- they broke SSH and tmux,
- they were built on slow web technology.

The survivors (Neovim's protocol, Jupyter, PowerShell) are _additive_:
legacy programs keep working, and programs that know about the richer layer get more.
The terminal persists because a stream of bytes works over anything, and every program already speaks it.

#note[Any step away from the terminal must be additive.
The VT emulator stays mandatory for programs we don't own,
and everything has to keep working over a byte stream such as SSH.]

== Proposal: values first, the terminal as a fallback

mshell has something none of the prior art had: a static type checker.
CLIM's presentation types were a runtime idea; mshell can have them statically.

#table(
  columns: (auto, 1.4fr, 1fr),
  inset: 4pt,
  table.header([*Layer*], [*What*], [*Who uses it*]),
  [Bytes], [The VT emulator in each block (@sec-pty)], [Every legacy program and TUI],
  [Values], [mshell's closed set of value types is the document format], [mshell code natively. External programs through JSON (`--json` flags, which mshell already parses), or an optional value channel on a separate file descriptor, as in TermKit and Oils.],
  [Views], [Rendering chosen by type, and definable in mshell the way `complete` metadata is today. Glamorous Toolkit's "moldable views" are prior art.], [Users' own types],
  [Typed input], [The type checker knows the type expected at the cursor (the live stack types), so earlier values of that type become clickable, as in CLIM.], [Open question 6],
  [Semantic UI], [Programs describe widgets instead of cells, as in Neovim's protocol.], [mshell's own interactive programs first],
)

The file manager is a good first program for the semantic UI layer.
We own it, and `FmState` and `FmLine` already describe its state as values,
so the GUI could draw it as native widgets instead of through a terminal.

== What generative AI changes

*The values layer becomes affordable.*
PowerShell and Nushell rewrote `ls`, `ps`, `cut` and the rest themselves, with a team.
One person with AI can now do that for the small, well-understood utilities.
mshell is already partly there: list and grid operations replace `cut`, `sort`, `uniq`, `head` and `tail`,
and there are builtins for JSON, TOML, Excel, HTTP, tar and zip.

The replacements also travel with the engine.
With `ssh host msh server`, the remote side runs our static `msh` binary and its builtins,
not whatever GNU or BSD tools are installed there.

*What it doesn't change:*

- *Programs we don't own:* git, docker, ssh, compilers, package managers, vim.
  Many now have JSON output (`gh`, `kubectl`, `docker`, `cargo --message-format=json`, git's porcelain formats).
  Where they don't, AI makes a parser per command cheap, as `jc` does,
  but such parsers break when output changes between versions or locales.
- *Checking the code is right.* Writing is cheap; making sure it's right isn't.
  Reimplementing `ls` means symlinks, odd file names, permissions on Windows and multibyte text.
  Reimplementations should be compared automatically against the originals (GNU coreutils),
  the same way the Rust port is checked against the Go code (@sec-port).
- *The cost of each builtin.* Every builtin needs an entry in `BuiltInList.go`, a type signature, documentation and a CHANGELOG line,
  and is kept forever.
  Anything that combines existing functions belongs in `std.msh`.

*The bottleneck moves to the data model.*
What does a file entry look like? A process? A grep match? A git commit?
What are the fields called, so every command agrees with the others?
Nushell found that writing the commands was easy, and keeping the data shapes consistent across them was the real work.
In mshell this lives in the type system:
a shared `FileEntry` type used by `ls`, `find` and the file manager is worth more than any single command.

*Rewrite only where values pay.*
It's worth it where the output is records or a table
(`ls`, `ps`, `df`, `du`, `find`, grep matches, git status and log),
because the GUI can then sort, filter and click them.
It isn't worth it for `cat` on a text file, `less` or `vim`.
The msh history file can rank commands by how often they're used, so the order comes from data rather than guesses.

== Agents as a frontend <sec-agents>

A large share of shell use is now agents, which drive a shell by scraping bytes out of a terminal.

- *Against us:* agents are trained on bash and coreutils, and know little mshell,
  so the more novel the utilities, the more documentation agents need (`doc/mshell.md` exists for this).
- *For us, and probably stronger:* typed values with documented signatures are much easier for an agent than parsing the columns of `ls -la`.
  The engine protocol (@sec-protocol), with blocks, exit status, typed values and stack types, is a better interface for an agent than a PTY.

So an agent could talk to `msh server` directly, and the same protocol would serve the GUI and agents.
Nothing in the protocol should assume a human at a screen.

== Not pursued: UI generated at runtime

Having a model make up UI on the spot ("show this grid as a chart") doesn't fit an app where performance comes first and behaviour should be predictable.
Here AI is a tool for building:
it writes views, parsers and builtins once, and they become ordinary code we test and measure.
The app itself doesn't depend on a model.

= Architecture: separate engine and frontend <sec-arch>

`msh` runs as a *separate process that the GUI talks to over a protocol*, the way a language server or a Jupyter kernel works.
It is not a library linked into the GUI.

```
+-------------------+   protocol (pipe / socket / ssh)    +----------------------+
| Frontend (GUI)    | <---------------------------------> | Engine (msh server)  |
| - rendering       |   input, resize, requests  -->      | - evaluator, stack   |
| - VT emulators    |   <-- events, PTY bytes, values     | - PTYs, job control  |
| - editor, panes   |                                     | - history, LSP       |
+-------------------+                                     +----------------------+
```

The engine (Go) owns the evaluator, the stack, the PTYs and job control.
The frontend only renders, runs the VT emulators over the PTY bytes it receives, and sends input.

What we get from this:

- *Remote sessions almost for free:* `ssh host msh server` gives the full GUI against a remote machine.
- A crash in one can't take down the other.
- The frontend can be written in any language.
- The protocol becomes the specification of the app, which fits the habit of specifying things formally before building them.

The costs:

- Designing the protocol and serialising values.
  mshell's value types are a small, closed set, so serialisation is manageable.
- A process boundary on paths that need to be fast.
  Startup isn't affected: the engine starts in parallel with the window (@sec-startup).
  The prompt is edited locally (@sec-input), and PTY bytes pass through raw (@sec-output).

There is *one engine process per pane* (decided 2026-11-07, @sec-pty).
Each engine has one session, so the evaluator never needs to run several sessions in one process.
That matters because the working directory (`os.Chdir`) and the environment (`os.Setenv`) belong to the process.

#note[Measured on the FX-8350 (Linux, 2026-11-07), round trips with 64-byte messages:
a pipe between a C process and a Go echo loop takes 13--16 µs at the median, 24--42 µs at p99, and 39--103 µs at p99.9.
Two threads in one process woken by a futex take 17--19 µs, so an engine linked in as a library would be no faster.
The cost is waking a sleeping thread, not crossing processes.
Only busy-polling shared memory is faster (0.2--0.6 µs), and it burns a core per pane.
Throughput through a Go relay in 64 KiB chunks is 1.5 GB/s, well above any VT parser.
Windows and macOS are not measured yet.]

== Protocol sketch <sec-protocol>

Very rough; just the kinds of messages.
Binary and length-prefixed: no JSON on the hot paths.

From the frontend to the engine:

- `run {block_id, source}`
- `input {bytes}`: keystrokes for the running process, written to the pane's PTY (Ctrl-C is just the byte; the kernel sends the signal)
- `resize {cols, rows}`: the pane's PTY
- `signal {sig}`: a signal for the foreground job, for cases a byte can't express
- `binding_call {version, quote_id, edit_state}`: run a `bindingFunc` or `abbrFunc` (@sec-editor-ask)
- `run_quote {quote_id, edit_state}`: an `editRun` or `editRunOps` from a fixed binding; it becomes a block
- LSP-style requests: completion, hover, diagnostics, stack types at the cursor

From the engine to the frontend:

- `block_started {block_id, cwd, time}`
- `pty_output {block_id, bytes}`: tagged with the block that is current when the bytes are read
- `value {block_id, value}`: a structured mshell value, paged if large
- `stack {block_id, values}`: or a diff from the previous stack
- `block_finished {block_id, exit_code, duration}`
- `editor_config {version, keymap, abbreviations}`: the whole config, sent whenever it changes (@sec-editor-config)
- `binding_result {version, result}`: the line and cursor after the quotation's operations, or that it declined
- `history_append {entries}`: keeps the frontend's copy of history current

Open questions: flow control (@sec-output), and the paging scheme for large values.

=== What exists (2026-11-08)

`msh server` (`mshell/Server.go`) implements the minimal set: `run`, `input` and `resize` in; `hello`, `block_started`, `pty_output`, `block_finished` and `exit` out.
Each frame is a `u32` length, a `u8` type and the payload, little-endian, with any variable-length bytes last; the layout of each message is in the file's header comment.
Exit codes are as `msh` reports them: a program killed by signal N gives -(128 + N), so Ctrl-C gives -130.

How it works:
- At startup the engine keeps private copies of its stdin and stdout for the protocol.
  It then makes the PTY's slave side its stdin, stdout, stderr and controlling terminal, after `setsid`.
  Started from a shell it leads its process group and can't `setsid`, so it runs a copy of itself in a new session.
  The evaluator, job control and every child see exactly what interactive `msh` sees on a terminal, and no evaluator code changed.
  Lines run through the same function as the interactive editor's (`runLine`), so parsing, the type check and the stack behave the same.
- *Block boundaries are marked in the PTY stream itself.*
  The engine writes a private OSC marker, with a random per-engine nonce, into the PTY before and after each block.
  Its PTY reader strips each marker and sends `block_started` or `block_finished` at that exact point.
  The kernel hands PTY output to the reader asynchronously, so "the line has finished" can't be matched to "the output read so far" any other way.
  Output outside any block (startup messages, background jobs) has block id 0.
- Before each line, and before the end marker, the terminal goes back to the cooked state saved at startup, as interactive `msh` does.
  A program that leaves the terminal raw doesn't affect the next block, and the end marker always passes through unchanged.
- Ctrl-C, Ctrl-\ and Ctrl-Z while the engine itself is in the foreground are caught and ignored, rather than signal-ignored, so children get the default actions back.
  So pure mshell code can't be interrupted yet; that needs the `signal` message, or the engine acting on its own SIGINT.
- Closing the protocol exits the engine; the PTY hangs up and the foreground job gets SIGHUP, as when a terminal window closes.

Not yet: Windows (ConPTY), values and the stack as structured messages, the editor messages, and flow control beyond the pipe's own backpressure.
Tests: `mshell/Server_unix_test.go` runs blocks, input, Ctrl-C, exit and the marker scanner, through a real engine process.

= The line editor <sec-editor>

The prompt is edited in the frontend, but key bindings and abbreviations can run mshell code, which only the engine can run.
This section is how the two fit together.

Today's terminal editor already runs mshell code on the keystroke path:
a `bindingFunc` or an `abbrFunc` runs synchronously, and is stopped after 1 second.
The GUI keeps that behaviour, minus the frozen frame.

== Who does what

#table(
  columns: (auto, 1.4fr, 1fr, 1fr),
  inset: 4pt,
  table.header([*Needs*], [*Operations*], [*Default keys*], [*Runs in*]),
  [Nothing but the line], [`editInsert`, `editSelfInsert`, `editSetLine`, the moves and deletes, `editMessage`], [Letters, arrows, Ctrl-A/E/W/U/K, Backspace], [Frontend],
  [The lexer and the abbreviation table], [`editExpandAbbr` on a text abbreviation], [Space, `|`, `;`, `<`, `>`, `]`, `)`], [Frontend],
  [Running mshell code], [`bindingFunc`; `editExpandAbbr` on a word an `abbrFunc` or `abbrRegex` abbreviation could match; completions; `editRun`, `editRunOps`], [Tab, Ctrl-G, Ctrl-O, user bindings], [Engine],
  [History], [`editHistory*`, `editForward`, `editAcceptSuggestion`, `editLastArg`], [Up/Down, Ctrl-P/N, Ctrl-F, Ctrl-Y, Alt-.], [Frontend, on a copy of history the engine keeps current],
  [Running the line], [`editAccept`, `editExit`], [Enter, Ctrl-D], [Engine],
  [Nothing, or something else], [`editRemeasure` (the shaper gives widths); `editClearScreen` (what clearing means for blocks is to be decided)], [], [Frontend],
)

Regex abbreviations always ask the engine, because Go and Rust regex syntax differ.

The frontend never runs mshell.
A binding quotation's interface with the editor is data:
an `EditState` (`before`, `after`, `keys`, `mode`) goes in, and `Maybe[[EditOp]]` comes out.
Whatever the quotation does in between happens in the engine.
So new builtins, new types and type checker changes never touch the frontend.

Quotations inside the keymap, such as the `([lf];)` of an `editRun`, reach the frontend as IDs only the engine can resolve.
Firing one sends "run quotation \#17" back, and the engine runs it as a block.

== Keys that ask the engine <sec-editor-ask>

When a key fires a `bindingFunc`, or reaches an `abbrFunc`:

+ The frontend sends the engine the quotation's ID and the `EditState`, with the line's version.
+ Keys typed while waiting are queued, each with the time it arrived.
  `within` and `idleBefore` are measured from those times,
  so a queued key resolves exactly as it would have if nothing had waited.
+ The engine runs the quotation.
  If it returns operations, the engine applies them and replies with the resulting line and cursor.
  If it declines, the reply says so.
+ The frontend takes the new line, or after a decline falls back as the resolver does today
  (a shorter binding, then the key's own action),
  and then runs the queued keys through the resolver.

The frontend has to wait rather than guess, because a `bindingFunc` can decline,
and then its keys act as if the binding were not there:
`;` `r` bound with `onEmptyLine` types `;r` on a line that isn't empty.
Firing a `bindingFunc` also clears the resolver.
So what a key does, and how the keys after it resolve, isn't known until the reply.

Frames keep drawing while the line waits: the cursor, output, other panes.
Locally the wait is the quotation's run time plus microseconds.
Over SSH it adds one round trip, and only on keys that run mshell code.

Operations that aren't pure (`editAccept`, `editRun`, completion, history) wait until the queue is empty,
so Enter can never run a line before an earlier binding has been applied.

#decision[The engine applies the operations a quotation returns, and replies with the final line and cursor.
The frontend then only ever applies the fixed operations in its own keymap,
which removes a whole class of ways for the two sides to disagree.]

Guessing ahead is possible later: show the result of a decline at once, and undo it and replay the queued keys if the quotation returns operations.
The resolver's snapshots already support undoing (each pending key carries the line before it).
It's only worth adding if waiting over SSH turns out to be annoying, since it is more complex and makes the line flicker.

== Keeping the frontend's keymap current <sec-editor-config>

`bindingAdd`, `bindingErase`, `abbrAdd`, `abbrErase` and `abbrRename` can run at any time:
in `init.msh`, from a line typed at the prompt, or inside a `bindingFunc` or an `editRun` quotation.
The engine owns the keymap and the abbreviation table.
The frontend holds a read-only copy of both, the _editor config_, with a version number.

They can only change while mshell runs,
and in the engine mshell only runs in response to a request from the frontend:
running a line, a binding or abbreviation quotation, a completion, or `init.msh` at startup.
Nothing runs mshell code on a timer or in the background.
(Checked 2026-11-02: the engine's goroutines are pipeline stages inside a running command, watchdogs, cancellation and saving history.)

#decision[If a request changed the editor config, the engine sends the new config before the request's reply (or `block_finished`), on the same ordered connection.
The frontend switches to it before handling any key queued behind that request.]

#table(
  columns: (1fr, 1.2fr, 1.2fr),
  inset: 4pt,
  table.header([*How the binding is added*], [*Keys typed meanwhile*], [*Which keymap handles them*]),
  [Typed at the prompt, then Enter], [Go to the program's PTY if one is running; otherwise they're held for the next prompt], [The new one: the config arrives before `block_finished`. A terminal behaves the same: held keys wait in the terminal's buffer until the editor reads them after the command.],
  [Inside a `bindingFunc`], [Already queued behind the reply (@sec-editor-ask)], [The new one: the config arrives with the reply.],
  [Inside an `editRun` quotation], [Go to its PTY while it runs], [The new one: the config arrives when the quotation finishes.],
  [The change lands mid-chord], [The pending keys stay pending], [Go rebuilds its lookup tries and walks the pending keys through the new ones; Rust does the same.],
)

The engine sends the whole config each time, not just the changes.
It's at most a few hundred bindings and abbreviations, a few KB, and sent rarely.
Each binding is its keys, mode, `within`, `idleBefore`, and an action: fixed operations, or a quotation ID.
The frontend rebuilds its lookup tries from scratch, which is cheap at that size.
Sending only changes would be one more way for the two sides to disagree.

Warnings such as "Binding … never fires" are worked out in the engine and shown as messages.

*Startup.* The user's bindings don't exist until the engine has run `init.msh`.
If early keys were handled with the default keymap, a chord such as `j` `f` would be typed as text instead of running the line.
So the prompt shows at once, but keys wait for the first config, probably tens of milliseconds (@sec-startup).
Caching last session's config on disk is the fallback if that measures too slow,
but `init.msh` can build bindings from the environment or the host name, so a cached copy can be wrong and would need correcting.

*Panes.* In the terminal, each `msh` process has its own keymap.
Each pane has its own engine (@sec-arch), so each pane gets its own copy and behaves the same way:
a binding added in one pane doesn't appear in the others.

*Later.* This guarantee holds because mshell never runs code unprompted.
If it ever gains background mshell jobs, timers or file-watch hooks, the config still arrives by push;
the only loss is control over which keystroke first sees a new binding, and a terminal has the same race.

= The Rust port <sec-port>

The frontend needs Rust versions of some of the Go code.
Terminal `msh` keeps its own editor, so these are second copies that have to be kept in sync.

#table(
  columns: (auto, auto, 1fr),
  inset: 4pt,
  table.header([*Piece*], [*Go source*], [*What the port needs*]),
  [Lexer], [`Lexer.go`, 1,749 lines], [Token kinds and positions, for highlighting and abbreviation positions. Not literal values or detailed error messages.],
  [Key resolver], [`KeyResolver.go`, 594 lines], [All of it, including `within` and `idleBefore`.],
  [Fixed edit operations], [Part of `Bindings.go`, 688 lines], [Only the operations that need nothing but the line (@sec-editor).],
  [Abbreviation positions], [Part of `Abbr.go`, 710 lines], [Command word, head of a list, after `|`, `abbrArgOf`. Regex abbreviations ask the engine.],
  [Grapheme segmentation], [], [The same Unicode version on both sides, or cursor moves disagree on new emoji.],
)

An estimated 2,500--3,000 lines of Rust to start with.

The port doesn't grow with the language.
The evaluator, the builtins, the type system and every binding and abbreviation quotation stay in the engine.
Only new syntax (the lexer) and new `EditOp`s touch it.

== How often it would change

Over the 12 months to 2026-11-02, `Lexer.go` had 28 commits, about two a month.
Half of them changed 10 lines or fewer, and some were internal work with no change to the tokens (\#337, parsing literals once during lexing; \#356, faster startup).
`KeyResolver.go` and `Bindings.go` are new (October 2026), so they have no track record; new code usually changes a lot at first, then settles.
Expect one or two small mirrored changes a month, almost all in the lexer.

The repository already has hand-kept copies of the syntax: the TextMate grammar for VS Code and `sublime/msh.sublime-syntax`.
Since November 2025 they've had 6 and 3 commits, against 28 for the lexer.
Some of those lexer commits didn't change tokens, so this doesn't prove the grammars are behind, but nothing checks them against the lexer.
The Rust port must not work the same way.

== Keeping it in sync

#decision[Build the tests before the Rust code.
The Go code is the reference, and any drift fails CI.]

- *Lexer: compare against `msh --lex`.*
  It already prints a stable list of line, column, kind and text for every token:
  ```
  1:7:FORMATSTRINGSTART $"a{
  1:11:VARRETRIEVE @x
  1:13:FORMATSTRINGEND }b"
  ```
  Run both lexers over the 620 `.msh` files in `tests/` and `lib/`, plus fuzzed input, and compare.
  The lexer keeps state through format strings, so cover nested and unfinished interpolations; unfinished ones are what the line holds mid-typing.
  A failing comparison is a precise spec for the change, so mirroring it is mechanical work for the same pull request.
- *Resolver: record cases in Go, replay them in Rust.*
  A hidden `msh` debug command prints cases: keymap, keys with their timings, and starting line in; final line, cursor and bindings fired out.
  They come from `Bindings_test.go` and from random key sequences.
  Both versions also answer to `ai/key-bindings/KeyResolution.tla`.
- *Data, not code.*
  The keymap, the abbreviation table and the lexer's keyword list come from the engine (@sec-editor-config), so they never need porting.
- *No allocations on the keystroke path*, in the port as in the Go editor.

How bad drift is if it gets through:
lexer drift is usually wrong colours, and rarely an abbreviation that expands where terminal `msh` wouldn't expand it.
Resolver drift makes keys behave differently in the GUI and the terminal, which is confusing, so the resolver gets the most thorough testing even though it is the smaller piece.

== Alternatives considered

- *Thin frontend:* the engine keeps the editor, and the frontend only draws.
  No port, and locally probably well under 1 ms a key.
  But every key costs a network round trip over SSH, and Go's garbage collection pauses land on the typing path.
  Worth measuring locally (open question 5).
- *One editor core in Rust, linked into `msh` through cgo:* no second copy,
  but building `msh` would need a Rust toolchain, and cross-compiling (`build_win.sh`) gets harder.
- *The Go code compiled to WebAssembly with TinyGo, run inside the frontend:* one copy.
  Unproven: whether TinyGo handles the lexer's dependencies (`SourceText`, literal parsing), the runtime's startup cost, and staying allocation-free across the boundary on every key.
  A spike of about a day would answer it.

= Frontend stack <sec-frontend>

== What "full control of the UI" means

We draw every pixel ourselves.
That means owning things a native toolkit would normally provide:

- text shaping, font fallback, ligatures, emoji,
- clipboard, drag and drop, DPI scaling, window management,
- smooth scrolling and selection,
- later: IME and accessibility (@sec-deferred).

Libraries exist for each of these, so "full control" means putting libraries together, not writing it all from scratch.
It also suits the performance goal: no toolkit layer in the way that we can't measure or change.

== Deferred: IME and accessibility <sec-deferred>

Not needed for now.
Recorded here so the design doesn't make them harder later.

*IME* (input method editors) is how people type text that doesn't map directly to keys:
Chinese, Japanese and Korean input, and also dead keys, the compose key, emoji pickers and dictation.
The app draws the uncommitted _preedit_ text at the cursor, tells the OS where the cursor is so the candidate window appears in the right place,
and keeps composition keystrokes away from the key bindings.
winit delivers the events (`Ime::Preedit`, `Ime::Commit`, `set_ime_cursor_area`).
What to keep in mind now: key handling should have a place for "text input" separate from "key binding", so this can slot in.

*Accessibility* here mainly means screen readers, which read a tree of UI elements published through an OS API, not pixels.
A GPU-drawn UI is invisible to them unless we publish that tree; AccessKit does this across platforms and plugs into winit.
What to keep in mind now: keep the UI's element structure clean (prompt, block list, block, stack panel), so a tree can be generated from it later.

== Options

#table(
  columns: (1fr, 1fr, 1.2fr, 1.4fr),
  inset: 4pt,
  table.header([*Option*], [*VT emulator*], [*Strengths*], [*Weaknesses*]),
  [*Rust: winit + wgpu + text library*], [libghostty-vt (@sec-vt)], [Full control; each piece is a focused library], [We build the layout, widget and editor layer ourselves; wgpu startup cost must be measured],
  [Rust: winit + direct OpenGL / Metal], [same], [What Alacritty (OpenGL) and Ghostty (OpenGL, Metal) do; startup cost well understood; renderer is small], [Two backends to maintain; OpenGL is deprecated on macOS],
  [Rust + GPUI (Zed's framework)], [`alacritty_terminal` (as in Zed)], [Editor and terminal panes already proven together], [Before 1.0, thin docs, API follows Zed's needs; its own element model limits control],
  [Native per platform + libghostty], [libghostty-vt (C ABI)], [Best feel on each OS], [Three UI codebases; not "full control"],
  [Tauri/Wails + xterm.js], [xterm.js (WebGL)], [Fastest to build rich UI], [Webview startup, latency and memory; rules itself out under @sec-perf],
  [Go + Gio], [libghostty-vt through cgo (`go-libghostty`)], [One language; draw everything; reuses the Go lexer and key resolver, so no port (@sec-port)], [GC pauses on the frame path; a cgo call costs 55--67 ns (measured 2026-11-07), so a full redraw read cell by cell is 2--3 ms; thinner GPU and text libraries],
)

== Current recommendation <sec-recommendation>

#decision[The frontend is Rust (decided 2026-11-07), for absolute performance.
A Go frontend was reconsidered once libghostty-vt removed the worry about Go VT emulators, since it would have avoided the Rust port.
It lost on garbage collection on the frame path, the cost of cgo calls, and thinner GPU and text libraries.]

*A Go engine, and a Rust frontend on winit*, with:

- libghostty-vt for all VT emulation (@sec-vt),
- Rust ports of the lexer, key resolver and fixed edit operations (@sec-port),
- `parley` or `cosmic-text` with `swash` for text layout and rasterising, loading font files directly rather than scanning system fonts (@sec-startup),
- `arboard` for the clipboard,
- the renderer chosen by measurement (below).

The renderer is mostly a glyph atlas plus instanced quads, which is small.
So keep it behind a narrow interface, and choose the backend by measuring:

+ *First step: a startup spike.*
  A winit window, then wgpu device setup, loading one font file, an ASCII atlas, and one frame of text.
  Trace each stage on the FX-8350 / Radeon Pro WX 3100 machine under Wayland, and on a Mac and a Windows machine.
  Compare launch-to-first-frame with Alacritty and Ghostty.
+ If wgpu meets the startup target, use it: one backend for all platforms.
+ If not, write the renderer directly against OpenGL (Linux, Windows) and Metal (macOS), as Alacritty and Ghostty do.

On Linux the spike's answer is wgpu, Vulkan only: it meets the 100 ms target without tricks,
and drawing the first frame on the CPU (@sec-startup) takes the driver load off the first frame if the target tightens.
The time goes into loading the Vulkan driver, which a direct OpenGL renderer would pay too (EGL setup was slower still).
Mac and Windows are still to measure.

The PTYs live in the Go engine (@sec-arch), so the frontend doesn't need a PTY library.
Go has ConPTY wrappers for Windows.

A webview prototype is no longer recommended, even as a throwaway.
It wouldn't tell us anything about performance, which is the main risk.

= A first step that doesn't need the GUI <sec-osc>

Have the current interactive `msh` emit:

- *OSC 133* (semantic prompt marks: prompt start, command start, output start, command end with exit status),
- *OSC 7* (current directory).

*Done* (2026-11-08). OSC 7 was already there; OSC 133 is new (`mshell/SemanticPrompt.go`).
Ghostty, kitty, WezTerm, iTerm2 and tmux can now jump between prompts and select a command's output (checked in tmux).
It also works as the first draft of the block boundaries the protocol needs.
The prompt start carries `redraw=0`: `msh` redraws its prompt after a resize only when the next key arrives, so the terminal must not clear it.

= Suggested order of work

+ OSC 133 / OSC 7 in the existing interactive mode (@sec-osc). *Done.*
+ Startup spike for the renderer (@sec-recommendation), including the measurement tools (@sec-measuring). *Done* (`gui/startup-spike`), with libghostty-vt.
+ `msh server` with the minimal protocol: run, PTY output, input, resize, block start and finish. *Done* (@sec-protocol).
+ The Rust lexer, key resolver and fixed edit operations, with the comparison tests against `msh --lex` and the resolver cases built first (@sec-port).
  *Done* (`gui/msh-editor`): the lexer, grapheme clusters, the resolver, the line-only ops and abbreviation expansion match Go on every case generated (over 3 million key events), and the keystroke path doesn't allocate.
  Open: whether a word names a program (`isProgramWord`) depends on the engine's `PATH`, and it decides command position after `|` and which command a word is an argument of.
  The port asks through a callback; the frontend needs an answer without a round trip on Space, probably a set of program names sent with the editor config.
+ Frontend: one pane, prompt editor with the editor config and binding calls (@sec-editor), blocks with embedded emulators, alternate-screen handling.
+ Live stack panel with structured values.
+ Output search.
+ Tabs and panes.

Measure after every step against the targets in @sec-targets.

= Open questions <sec-questions>

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*\#*], [*Question*], [*Notes*]),
  [1], [Are the targets in @sec-targets the right ones?], [Proposals; need agreeing.],
  [2], [Does "all major platforms" mean Windows, macOS and Linux desktops, or mobile as well?], [Mobile would change the stack a lot.],
  [3], [How important are remote sessions over SSH?], [The strongest reason for the engine/frontend split.],
  [4], [Does each tab/pane get its own interpreter, or do they share one?], [*Decided:* one engine process per pane, with its own PTY and keymap (@sec-arch, @sec-pty).],
  [5], [How fast is a key round trip to a local engine, if the engine kept the editor?], [Highlighting is decided: a Rust port of the lexer (@sec-port). This measures the thin-frontend alternative, for the record.],
  [6], [Syntax for using an earlier block's output as a value.], [CLIM's answer is no syntax: click an earlier value where its type fits (@sec-beyond).],
  [7], [How pipelines and background jobs map onto blocks and PTYs.], [*Mostly decided:* one PTY per pane, with pipelines handled by the existing job control. Background job output still lands in the current block (@sec-pty).],
  [8], [Protocol encoding and paging scheme for large values.], [@sec-protocol.],
  [9], [How much of the existing interactive mode (bindings, file manager, completions) moves to the engine and is shared with the GUI.], [Bindings and abbreviations: @sec-editor. The file manager and completions are still open.],
  [10], [A quotation that returns history or completion operations needs editor state beyond the line (history position, completion cycle).], [Either the `EditState` sent to the engine carries that state, or those operations are sent back to the frontend. Rare in practice; to be decided (@sec-editor-ask).],
  [11], [What the shared data model types are (a file entry, a process, a match, a commit), and what their fields are called.], [The real work of the values layer (@sec-beyond).],
  [12], [How external programs send values: JSON on stdout only, or also a value channel on a separate file descriptor?], [@sec-beyond.],
  [13], [Should the protocol be designed for agents as well as the GUI?], [@sec-agents.],
)
