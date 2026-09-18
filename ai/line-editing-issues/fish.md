# fish-shell line-editor failure modes (checklist for building another shell's line editor)

Derived from GitHub issue search/triage of fish-shell/fish-shell (`gh issue list --search ...` across
~40 term combinations, ~330 unique candidate issues screened, ~190 read in full, key threads' comments
pulled for root cause / resolution). Scope: interactive prompt/reader/line-editor display and input
handling only. Deduplicated into failure modes below; many issues share one root cause.

---

## Width measurement

### Emoji / wide-character width guessing disagrees with terminal
- Symptom: Prompts and command lines "backspace" themselves, characters vanish, or cursor drifts right
  after typing an emoji or wide glyph (e.g. lightning bolt ⚡, ☺️, ☁️) — worse in some terminals
  (alacritty, foot) than others (kitty).
- Root cause: fish must *guess* whether the terminal renders a given codepoint as 1 or 2 columns
  (there's no portable API for this); its guess table disagrees with what some terminals actually do,
  especially newer emoji and terminals with different Unicode-version support.
- How fish resolved it: added `$fish_emoji_width` / `$fish_ambiguous_width` overrides and periodic
  guess-table updates; fundamentally unsolved in general (still requires manual tuning per terminal).
- Issues: https://github.com/fish-shell/fish-shell/issues/2924, https://github.com/fish-shell/fish-shell/issues/3986, https://github.com/fish-shell/fish-shell/issues/4539, https://github.com/fish-shell/fish-shell/issues/6754, https://github.com/fish-shell/fish-shell/issues/7237, https://github.com/fish-shell/fish-shell/issues/7510, https://github.com/fish-shell/fish-shell/issues/8003, https://github.com/fish-shell/fish-shell/issues/10461, https://github.com/fish-shell/fish-shell/issues/12500

### Fitzpatrick / emoji modifiers counted as separate wide characters
- Symptom: `string length -V \U0001F44D\U0001F3FB` (👍🏻, thumbs-up + skin-tone modifier) reports width 4
  instead of 2 — fish treats the modifier as its own emoji rather than combining with the base.
- Root cause: fish's width table has no concept of "modifier combines with preceding base emoji"; each
  codepoint is scored independently.
- How fish resolved it: open at time of report; noted as needing a hardcoded table of which emoji accept
  modifiers, with no clean general solution.
- Issues: https://github.com/fish-shell/fish-shell/issues/8275

### Variation Selector-16 width assumption wrong on some terminals
- Symptom: Emoji built with VS-16 (U+FE0F) measured as width 2 by fish, but some terminals (macOS
  Terminal.app) render the selector as width 0, causing consistent 1-column drift.
- Root cause: fish hardcodes VS-16 as forcing width 2 regardless of `$fish_emoji_width`/guessed width,
  ignoring that some terminals treat it as zero-width.
- How fish resolved it: open at time of report.
- Issues: https://github.com/fish-shell/fish-shell/issues/8276

### `$fish_emoji_width` cache not invalidated
- Symptom: Setting `fish_emoji_width` then unsetting it leaves the old value baked into cached widths;
  setting it in a local scope poisons the outer scope's cached guess too.
- Root cause: the emoji-width guess is memoized globally and the cache isn't cleared on `set -e` or when
  the variable goes out of scope.
- How fish resolved it: reported as a bug; workaround was `set fish_emoji_width 0` to force re-guessing.
- Issues: https://github.com/fish-shell/fish-shell/issues/8274

### Backspace character (BS/0x08) not treated as -1 width
- Symptom: A prompt that emits `printf "x\b> "` renders with an extra phantom space — fish counts BS as
  zero-width instead of "move cursor back one column."
- Root cause: the width-calculation routine had no special case for control character 0x08.
- How fish resolved it: fixed to treat BS as width -1 to match real terminal behavior.
- Issues: https://github.com/fish-shell/fish-shell/issues/8277

### Zero-width space causes rendering corruption from history
- Symptom: A command containing U+200B (zero-width space) renders garbled when redisplayed from history,
  even though other shells (bash) just show it as effectively invisible/normal.
- Root cause: fish's width table didn't special-case ZWSP as zero-width, so its internal column
  accounting diverged from the terminal's.
- How fish resolved it: reported open at time of triage; general pattern of "characters with
  non-obvious width need a hardcoded table entry."
- Issues: https://github.com/fish-shell/fish-shell/issues/4571

### CJK / East-Asian "ambiguous width" characters mismeasured
- Symptom: Typing certain Japanese katakana (グ, ジ) after a filename with Japanese characters makes the
  cursor "jump" several columns and completion output shows corrupted glyphs; separately, U+23CE (⏎,
  fish's own line-continuation indicator) can't even be encoded in some CJK narrow locales.
- Root cause: East-Asian "ambiguous width" characters are 1 column in some fonts/terminals and 2 in
  others (per UAX #11); fish's static guess doesn't match the user's actual terminal/font, and there was
  no option to force ambiguous-width handling.
- How fish resolved it: users requested (and fish eventually added related knobs for) an
  ambiguous-width override; underlying ambiguity remains a fundamental unsolved problem shared with
  every other shell/editor.
- Issues: https://github.com/fish-shell/fish-shell/issues/3420, https://github.com/fish-shell/fish-shell/issues/3503, https://github.com/fish-shell/fish-shell/issues/5149, https://github.com/fish-shell/fish-shell/issues/12444

### Multi-byte prompt characters break history recall / cursor math (byte vs. char count)
- Symptom: A prompt containing a multi-byte UTF-8 character (e.g. ¥, λ) causes recalled history lines to
  be jumbled — a leading character duplicated, or a space silently dropped, offset by exactly one.
- Root cause: some code path counted bytes instead of characters (or vice versa) when computing prompt
  width / cursor offsets, so multi-byte characters shifted the accounting by the byte/char delta.
- How fish resolved it: fixed as a byte-vs-character counting bug in the affected version; recurring
  pattern across several point releases (High Sierra unicode-prompt regression, Termux report) rather
  than a single fix.
- Issues: https://github.com/fish-shell/fish-shell/issues/4269, https://github.com/fish-shell/fish-shell/issues/4539

---

## Grapheme/cursor editing

### No grapheme-cluster support — ZWJ sequences and combining marks split apart
- Symptom: Pasting a ZWJ family emoji sequence (👩‍❤️‍👨) inserts visible extra whitespace/garbage between
  the codepoints instead of treating the whole cluster as one editable unit; cursor movement / delete
  operate per-codepoint, not per-cluster.
- Root cause: fish (like most terminal apps) has no grapheme-cluster segmentation; it operates on
  Unicode scalar values (`wcwidth`-style) throughout the codebase.
- How fish resolved it: explicitly declined as a "good first issue" — maintainers called it a deep,
  invasive change needed "across the board," made harder mid-port from C++ to Rust; still open.
- Issues: https://github.com/fish-shell/fish-shell/issues/9964

### Starship/other prompts with two adjacent codepoint icons produce phantom spaces
- Symptom: A prompt segment using two consecutive codepoint-based icons (e.g. from starship) renders
  with unexpected extra spaces in fish 4.0.
- Root cause: width/columnation regression tied to the same lack of grapheme-aware width math (adjacent
  narrow "icon" codepoints miscounted).
- How fish resolved it: reported against fish 4.0; tracked as a regression in the width-handling rewrite
  during the Rust port.
- Issues: https://github.com/fish-shell/fish-shell/issues/11586

### Cursor gets stuck / can't delete first character while suggestion pager is active
- Symptom: After using Tab-Tab to cycle through completion suggestions for `cd`, pressing backspace
  cannot remove the first letter of the typed command.
- Root cause: the pager-selection state kept a cursor/selection invariant that didn't allow editing back
  past the point where the pager took over the line.
- How fish resolved it: fixed as a completion/cursor-state bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/3132

### Crash holding Alt+F (forward-word) through an autosuggestion
- Symptom: Holding Alt+F with key-repeat to walk word-by-word through a long autosuggestion panics:
  `assertion failed: position <= self.len()` in `editable_line.rs`.
- Root cause: rapid repeated forward-word events could advance the cursor position past the end of the
  (dynamically changing) autosuggestion buffer before the length was re-checked.
- How fish resolved it: reported as a crash bug in 4.3.3 (Rust reader); fix required bounding the cursor
  to current buffer length on each step.
- Issues: https://github.com/fish-shell/fish-shell/issues/12383

---

## Wrapping and geometry

### Prompt truncates itself to ">" when fish misjudges it as "too long"
- Symptom: A themed prompt (e.g. robbyrussell-style) intermittently collapses to a bare `>` for no
  content-related reason.
- Root cause: fish's own heuristic for "prompt too wide, abbreviate" miscalculated the prompt's real
  display width (interacting with color escapes / wide glyphs).
- How fish resolved it: fixed as a prompt-width calculation bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/904

### Cursor flickers/glitches only in sufficiently wide terminal windows
- Symptom: In iTerm2 windows ≥170 columns, the cursor flickers and jumps to random columns on the prompt
  line while typing; doesn't reproduce in Terminal.app or with other shells.
- Root cause: unclear/unconfirmed — plausibly a cursor-position escape sequence miscomputed once line
  length crosses a width threshold, exposed only by that terminal's redraw timing.
- How fish resolved it: unresolved / not clearly root-caused in the thread.
- Issues: https://github.com/fish-shell/fish-shell/issues/1448

### Line wrapping at narrow widths shifts the whole line down instead of just the overflow
- Symptom: With `$COLUMNS` under ~77, once typed text reaches the terminal edge, the *entire* line
  (prompt included) gets shifted down to the next row instead of only the overflow wrapping.
- Root cause: the line-wrap-handling code in `screen.cpp` had special-cased "does not fit" logic that
  moved the whole line rather than allowing natural wrap once the left-prompt-to-width ratio crossed
  ~33%.
- How fish resolved it: confirmed reproducible via bisection to `$COLUMNS < 77`; maintainers debated
  whether the "extra functionality" was even needed, leaning toward removing it.
- Issues: https://github.com/fish-shell/fish-shell/issues/5118

### Wrapped-but-unconfirmed line is preserved as a phantom blank output line
- Symptom: If typed text exactly fills the last column (triggering the terminal's pending-wrap state)
  and the command is executed immediately (Enter) before fish's own redraw moves the cursor to the next
  row, the wrapped row is left behind and looks like an extra line of output.
- Root cause: fish and the terminal disagree about whether the "phantom" wrapped row is real content;
  fish doesn't account for the terminal's deferred/pending-wrap semantics at the last column before
  executing.
- How fish resolved it: documented as a known last-column pending-wrap edge case.
- Issues: https://github.com/fish-shell/fish-shell/issues/6826

### Autosuggestion (ghost text) disappears when the line wraps to a new row
- Symptom: Repeatedly pressing Alt+Right to accept-suggestion-by-word works until the growing line wraps
  onto a second terminal row, at which point the ghost/gray suggestion text vanishes even though
  completion still works.
- Root cause: the autosuggestion-rendering path didn't handle the multi-row case once the command line
  itself spans more than one wrapped row.
- How fish resolved it: fixed as a redraw bug in the autosuggestion renderer.
- Issues: https://github.com/fish-shell/fish-shell/issues/7213

### Autosuggestion "freaks out" (visibly corrupts) when suggested text is wider than the terminal
- Symptom: When the autosuggested filename/command is longer than the terminal window, the ghost text
  rendering becomes visibly broken/glitchy rather than gracefully truncating or wrapping.
- Root cause: overflow of the too-wide suggestion wasn't clamped/truncated before being written, so the
  screen's line-length bookkeeping diverged from the terminal's actual wrap.
- How fish resolved it: fixed as part of the wrap-and-redraw hardening around this era.
- Issues: https://github.com/fish-shell/fish-shell/issues/7249

### Multi-line prompt's first line gets reprinted spuriously on history navigation / redraw
- Symptom: With a multi-line `fish_prompt`, pressing Up/Down to browse history (or typing a character on
  a fresh line) re-prints the first prompt line above itself repeatedly, producing a stack of duplicate
  lines.
- Root cause: the repaint routine recomputed how many rows the *previous* prompt occupied incorrectly
  when the prompt spans multiple lines, so it didn't move the cursor up far enough before overwriting.
- How fish resolved it: fixed multiple times as regressions recurred across releases (a recurring class
  of bug rather than one fix); fzf/`<C-r>` retrieval and tmux narrow-resize triggered the same underlying
  row-count miscalculation.
- Issues: https://github.com/fish-shell/fish-shell/issues/7722, https://github.com/fish-shell/fish-shell/issues/8233, https://github.com/fish-shell/fish-shell/issues/10800, https://github.com/fish-shell/fish-shell/issues/10809

### Right prompt shifts, wraps, or misaligns with wide/ambiguous glyphs
- Symptom: A right-prompt (fish_right_prompt) is shifted by one cell with powerline glyphs; separately,
  a right-prompt containing `…` (horizontal ellipsis) starts unexpectedly wrapping under specific
  conditions.
- Root cause: right-prompt placement is computed from the same fallible width math as the rest of the
  line, so any width-guess error (ambiguous/powerline glyphs) throws off the right-justification
  arithmetic.
- How fish resolved it: individually patched per report; systemic issue (right-prompt math is downstream
  of the width-measurement problems above) never fully eliminated.
- Issues: https://github.com/fish-shell/fish-shell/issues/3673, https://github.com/fish-shell/fish-shell/issues/5742, https://github.com/fish-shell/fish-shell/issues/7491, https://github.com/fish-shell/fish-shell/issues/10996

### Last character of first prompt line silently dropped
- Symptom: With a custom multi-line prompt, the final character on the first line goes missing on
  screen, starting in fish 3.2.2 (regression vs 3.0.2/3.1.0).
- Root cause: off-by-one in the row-splitting/column accounting introduced by a prompt-rendering change.
- How fish resolved it: regression fixed after bisection.
- Issues: https://github.com/fish-shell/fish-shell/issues/8002

### `clr_eol` (clear-to-end-of-line) confuses terminals when prompt exactly fills the line
- Symptom: When the prompt's rendered width exactly equals terminal width, using the terminal's
  "clear to end of line" capability to redraw breaks in some terminals (they interpret it differently
  at the boundary).
- Root cause: relying on `clr_eol`/`el` terminfo capability at the exact last column is
  terminal-implementation-dependent; "clear the line first, then draw" was tried and made a different
  bug (shrinking lines) worse.
- How fish resolved it: acknowledged as not simply fixable by "clear-then-draw"; needed careful
  case-by-case handling instead.
- Issues: https://github.com/fish-shell/fish-shell/issues/8164

### Non-existent/failed completion visibly corrupts the prompt line
- Symptom: Pressing Tab on a completion that doesn't exist (`git ---<Tab>`) should just flash, but
  instead spawns an extra newline/prompt and leaves the display slightly broken.
- Root cause: the "no completions found" code path fell through to a redraw routine not designed for
  the zero-results case.
- How fish resolved it: fixed as a display regression noted in 2.3.0.
- Issues: https://github.com/fish-shell/fish-shell/issues/3093

### Completion pager overflowing the terminal misrenders progressively
- Symptom: Starting to type a command whose completion is longer than the terminal window produces
  visibly corrupted output that gets worse with each additional keystroke, and doesn't fully recover
  even after backspacing.
- Root cause: pager column-layout computation didn't clamp to available terminal width/height, so
  successive redraws compounded the misalignment.
- How fish resolved it: fixed as a pager-layout bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/2484, https://github.com/fish-shell/fish-shell/issues/692

### Command line taller than the terminal isn't redrawn correctly / gets truncated
- Symptom: Binding a key to insert a commandline with more lines than `$LINES` shows fish drawing at the
  wrong on-screen location, with characters typed afterward not echoing at all; separately, long
  multi-line pasted/history commands are truncated at screen height and excluded from terminal
  scrollback entirely (only fish's own internal up/down redraw can see the full content).
- Root cause: the multi-row repaint logic assumes the whole command line's rendered height fits on
  screen; once content exceeds `$LINES`, row-position bookkeeping goes wrong, and because fish redraws
  in-place rather than emitting real newlines, terminal-native scrollback never captures the overflowed
  rows.
- How fish resolved it: the redraw-position bug (#7296) was fixed; the scrollback-exclusion behavior
  (#11029) remained open, with "edit in `$EDITOR` via alt-v" as the only full workaround.
- Issues: https://github.com/fish-shell/fish-shell/issues/7296, https://github.com/fish-shell/fish-shell/issues/11029, https://github.com/fish-shell/fish-shell/issues/10827

### Prompt shifts down one line every time vi-mode is toggled, if prompt already wraps
- Symptom: With a long single-line external prompt (e.g. starship) that wraps past terminal width,
  every Esc/i mode switch in fish's vi bindings causes the prompt to visually drop down by one more line.
- Root cause: each vi-mode-switch triggers a prompt repaint that doesn't correctly account for the
  already-wrapped row when repositioning the cursor before redrawing.
- How fish resolved it: reported against fish 4.x; regression tied to vi-mode redraw path.
- Issues: https://github.com/fish-shell/fish-shell/issues/12247

### Long autocomplete overwrites a tall/multi-row prompt
- Symptom: With a prompt that expands to 3-5 rows (error codes, notifications, etc.), invoking a long
  completion causes the completion to render at the top prompt row, overwriting/erasing the custom
  prompt content even after Ctrl-C.
- Root cause: completion rendering positions itself relative to the wrong anchor row when the prompt
  occupies more than the expected number of lines.
- How fish resolved it: reported open in fish 4.3.2.
- Issues: https://github.com/fish-shell/fish-shell/issues/12287

---

## Prompt content

### Combined/chained ANSI SGR escape sequences in the prompt not parsed correctly
- Symptom: A Powerline-style prompt that uses combined escape sequences (`^[^[[0;38;5;231;...m`) to set
  multiple SGR attributes in one code fails to render correctly, though splitting into one attribute per
  sequence was also tried without success.
- Root cause: fish's internal width/escape scanner didn't correctly account for the width contribution
  (zero) of chained/combined escape sequences embedded in prompt output.
- How fish resolved it: prompted fixes to the escape-sequence-aware width scanner used for prompts.
- Issues: https://github.com/fish-shell/fish-shell/issues/767

### Unrecognized/unknown control sequences leak into the visible prompt or command line
- Symptom: When fish receives a control sequence it doesn't recognize, it echoes the trailing characters
  (minus the initial ESC byte) onto the prompt/command line as literal text, rather than swallowing or
  substituting them.
- Root cause: no defined policy for unrecognized escape sequences (contrast: bash/zsh emit a bell,
  dash prints a visible placeholder).
- How fish resolved it: discussed several approaches (borrow bash's bell, or dash's visible-escape
  representation); not fully resolved to one policy at report time.
- Issues: https://github.com/fish-shell/fish-shell/issues/1924

### Stray "⏎" (U+23CE) glyph flashes or persists before/after prompts
- Symptom: A return-symbol character (⏎) appears at the end of output, before the prompt, or briefly
  flashes between prompts, particularly over SSH/cmd.exe/ConEmu and in certain locales.
- Root cause: fish uses U+23CE as its own "line wasn't newline-terminated" indicator; multiple distinct
  bugs caused it to be emitted or left behind incorrectly: in one case a locale (Japanese Windows)
  couldn't encode/display it; in a later regression during the terminfo-removal rewrite, the
  "clear to end of line" call after printing it was dropped, leaving it on screen.
- How fish resolved it: the terminfo-removal regression was root-caused precisely (missing
  `ClearToEndOfLine` after refactor from `Vec<u8>` to `Outputter`) and fixed; older locale-encoding
  reports were closed as terminal/locale limitations.
- Issues: https://github.com/fish-shell/fish-shell/issues/789, https://github.com/fish-shell/fish-shell/issues/1411, https://github.com/fish-shell/fish-shell/issues/4717, https://github.com/fish-shell/fish-shell/issues/12476

### Background-color "clear to end of line" (EL) sequence in prompt behaves inconsistently
- Symptom: A prompt that sets a background color and uses `\e[K` (erase-to-end-of-line) renders
  differently depending on whether it's run in a plain shell vs. inside another program (e.g. a
  multiplexer or pager) reading the same output.
- Root cause: reliance on the terminal's own EL semantics for a colored background is inherently
  context-dependent (differs by what's "restoring" the background color after).
- How fish resolved it: reported open, tied to broader inconsistency in how fish and various terminals
  handle background-color persistence across EL.
- Issues: https://github.com/fish-shell/fish-shell/issues/11234

### No documented mechanism to strip invisible escapes from captured/piped output
- Symptom: Users want a `string`-family command to strip only the invisible escape codes (colors, bold)
  from another program's colored output, without corrupting genuinely-visible content.
- Root cause: feature gap — `string length`/`string pad` gained escape-awareness, but no
  strip-only-the-invisible-parts primitive existed.
- How fish resolved it: open feature request at time of report.
- Issues: https://github.com/fish-shell/fish-shell/issues/8217

---

## Resize

### `$LINES`/`$COLUMNS` on-variable handlers don't fire on SIGWINCH
- Symptom: A function registered with `--on-variable LINES` never runs even though the terminal was
  resized and `$LINES` visibly changed when checked manually.
- Root cause: the variable-change event machinery only fires for changes made via `set`, not for the
  internal update path triggered by SIGWINCH.
- How fish resolved it: fixed to also fire on-variable events for SIGWINCH-driven updates.
- Issues: https://github.com/fish-shell/fish-shell/issues/2316

### Multi-line prompts get reprinted/duplicated on terminal resize
- Symptom: Resizing the terminal window while a multi-line prompt is displayed causes the first line(s)
  of the prompt to be redrawn again below themselves.
- Root cause: SIGWINCH-triggered repaint didn't correctly account for how many rows the old,
  already-displayed multi-line prompt occupied before drawing the new one.
- How fish resolved it: fixed repeatedly as regressions across versions (same underlying class as the
  "multi-line prompt reprint" wrapping-and-geometry bugs above).
- Issues: https://github.com/fish-shell/fish-shell/issues/2320

### `$COLUMNS` wrong for the very first prompt of a session
- Symptom: The first fish prompt after starting a new terminal (tab/window/nested shell) reports
  `$COLUMNS` as 80 (a fallback default) instead of the terminal's actual width; any subsequent redraw
  (pressing Enter, resizing, pasting, or `pkill -SIGWINCH`) fixes it.
- Root cause: fish queries terminal size once at startup, but on some terminals (iTerm2) the real size
  isn't available/propagated by the time fish's first prompt is computed; nothing later triggers a
  re-query until some other event forces a repaint.
- How fish resolved it: reported as an initialization-order race; related to `#12995` below (SIGWINCH
  arriving before fish finishes its startup handshake).
- Issues: https://github.com/fish-shell/fish-shell/issues/4141

### Terminal resize during `read`/eval leaks internal state onto the display
- Symptom: If the terminal is resized while a `read` builtin's callback is being evaluated, the raw
  contents of the command line get printed to the screen unexpectedly.
- Root cause: the SIGWINCH-triggered repaint fired in the middle of an unrelated evaluation context that
  wasn't expecting to touch the display.
- How fish resolved it: fixed as a reentrancy bug between signal-driven repaint and script evaluation.
- Issues: https://github.com/fish-shell/fish-shell/issues/4876

### Resizing produces a fully garbled screen ("huge mess") in some terminals
- Symptom: Resizing the window in iTerm2 (and separately xterm/sakura) leaves the whole visible area a
  mess of overlapping/duplicated text; in the xterm/sakura case, increasing width repaints (timestamp
  updates) but ignores the new size, and decreasing width plus typing corrupts the display further.
  Clearing the screen (Ctrl-L) works around it.
- Root cause: unclear/unconfirmed in the thread — consistent with the same repaint-doesn't-know-old-row-
  count class of bug as the multi-line-prompt resize issue, amplified when width (not just height)
  changes.
- How fish resolved it: not conclusively root-caused in the threads; workaround is manual clear.
- Issues: https://github.com/fish-shell/fish-shell/issues/5962, https://github.com/fish-shell/fish-shell/issues/7985

### No way to distinguish a WINCH-triggered repaint from a Ctrl-C cancellation repaint
- Symptom: A "shell integration" prompt-marking protocol implementer needs to tell, from the output
  stream alone, whether a redraw happened because the user resized the window or because they hit Ctrl-C
  — fish gives no signal to distinguish the two.
- Root cause: feature/protocol gap — both cases just produce a repaint with no differentiating marker.
- How fish resolved it: open design discussion at time of report (predecessor to later OSC 133 semantic
  prompt marking work).
- Issues: https://github.com/fish-shell/fish-shell/issues/5973

### Combined focus + resize events (tmux) corrupt the command line
- Symptom: With `tmux set focus-events on`, closing/creating a split window causes garbage to appear on
  the fish command line.
- Root cause: fish's input parser didn't correctly disambiguate a focus-event escape sequence arriving
  interleaved with (or immediately around) a resize event.
- How fish resolved it: fixed as an input-parsing bug for combined focus/resize sequences.
- Issues: https://github.com/fish-shell/fish-shell/issues/8628

### `write()` interrupted by SIGWINCH (EINTR) treated as fatal, killing output mid-stream
- Symptom: Piping shell history to `fzf`/`fzf-tmux` inside tmux with a large history intermittently
  prints `write: Interrupted system call` and only a fraction of the expected entries reach the pipe.
- Root cause: `FdOutputStream::append()` only retried `write()` on EINTR from SIGINT/SIGHUP, treating
  EINTR from any other signal (including benign SIGWINCH) as a fatal error instead of retrying.
- How fish resolved it: identified as the `write()` analog of a previously-fixed `open_cloexec()` bug
  (#10250/#10251); fix is to retry on EINTR from non-cancel signals.
- Issues: https://github.com/fish-shell/fish-shell/issues/12496

### SIGWINCH arriving during startup's terminal-capability query leaves the prompt blank until a keypress
- Symptom: In a VTE-based embedded terminal that resizes its pane multiple times within ~220ms right
  after spawning fish, the pane stays completely blank (no greeting, no prompt) until the first keypress,
  which then makes the prompt appear immediately with that key already typed.
- Root cause: fish's startup sequence sends terminal capability/state queries and blocks reading the
  replies; a SIGWINCH arriving in that window interrupts/desyncs the read loop so the reply is never
  processed and the prompt draw is deferred indefinitely.
- How fish resolved it: reported open; `set -Ua fish_features no-query-term` is a full workaround
  (skips the startup query entirely).
- Issues: https://github.com/fish-shell/fish-shell/issues/12995

---

## Terminal queries and input races

### Initial cursor position wrong on shell startup until first keystroke
- Symptom: On starting a new fish session, the on-screen cursor appears positioned over/inside the
  prompt text rather than after it; typing anything immediately "fixes" the visual position.
- Root cause: consistent with a startup-time desync between where fish believes the cursor is and where
  the terminal actually drew it, resolved only once fish issues its first real repaint.
- How fish resolved it: not conclusively root-caused in the thread (early fish 1.x/2.x report).
- Issues: https://github.com/fish-shell/fish-shell/issues/418

### Locale-driven Unicode normalization mismatch corrupts `$PWD` display
- Symptom: `cd`-ing into a directory with an accented character (Äpfel) and then opening a new tab shows
  `$PWD` as `AÌ^Hpfel` — garbled with a literal backspace-like artifact — while `pwd` itself still prints
  correctly.
- Root cause: macOS/HFS+ stores/returns filenames in NFD (decomposed) Unicode form; fish's variable
  serialization/escaping assumed NFC and mis-encoded the decomposed combining sequence when passed
  between sessions/tabs.
- How fish resolved it: root-caused to Unicode Normalization Form mismatch; tracked as a
  locale/filesystem-encoding bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/2613

### Ctrl-C during input overwrites/corrupts a multi-line prompt instead of preserving it
- Symptom: With a two-line `fish_prompt`, typing text and pressing Ctrl-C does not cleanly preserve the
  previously-displayed prompt lines — the multi-line layout gets stepped on.
- Root cause: the cancel/redraw path for Ctrl-C assumed a single-line prompt when repositioning the
  cursor before printing the next prompt.
- How fish resolved it: fixed as part of the general multi-line-prompt-redraw hardening.
- Issues: https://github.com/fish-shell/fish-shell/issues/3537

### `fish_prompt` event doesn't fire when the prompt redraws mid-edit (cancel/syntax-error case)
- Symptom: Shell-integration code hooking `--on-event fish_prompt` (for OSC 133 semantic marking) never
  gets called when the prompt redraws on a new line because of a Ctrl-C cancellation or a syntax error
  — only on a "real" new prompt after command completion.
- Root cause: the event is only emitted on the primary "about to display a new prompt" code path, not on
  the secondary redraw-after-cancel/error path, even though both visually produce a new prompt line.
- How fish resolved it: reported/acknowledged as a gap; relevant to semantic-prompt integrations (kitty,
  iTerm2, VS Code shell integration) that rely on the event firing every time.
- Issues: https://github.com/fish-shell/fish-shell/issues/8832, https://github.com/fish-shell/fish-shell/issues/10350

### Fish should repaint between preexec and command execution
- Symptom: Visible lag/staleness between when a command is submitted (preexec fires) and when the
  terminal actually reflects that the prompt/input has been "handed off" to the running command.
- Root cause: no forced repaint is issued between the preexec hook and starting execution, so any
  prompt-side state change in preexec isn't visible until later.
- How fish resolved it: open feature/behavior request at time of report.
- Issues: https://github.com/fish-shell/fish-shell/issues/9103

### Command entered wider than the terminal gets split into multiple logical lines (kitty)
- Symptom: In kitty, when an entered command wraps because it's longer than the terminal width, kitty
  treats each wrapped row as a separate line (not a single soft-wrapped line); resizing the window then
  exposes that the command was stored/drawn as hard-wrapped rather than soft-wrapped.
- Root cause: fish's redraw for a long command line didn't mark wrapped rows in a way the terminal
  recognizes as "continuation of one logical line," so the terminal's own reflow-on-resize breaks it
  apart.
- How fish resolved it: escalated from a kitty-side report to a fish-side issue; consistent with the
  general wrap/geometry class of bugs.
- Issues: https://github.com/fish-shell/fish-shell/issues/9409

### Cursor-Position-Report (DSR / `\e[6n`) reply parsing panics on edge-case coordinates
- Symptom: Connecting from a particular Android SSH client (ConnectBot) causes fish to panic with a
  "subtraction with overflow" in the CSI cursor-position-report parser.
- Root cause: the reply parser unconditionally computed `coordinate - 1` (assuming 1-based, ≥1
  coordinates) without checking for 0/underflow, and the specific terminal emulator sent a
  cursor-position reply with a coordinate the parser didn't expect.
- How fish resolved it: fixed by switching to checked subtraction (`checked_sub`) that fails gracefully
  instead of panicking, plus filed a companion bug against the terminal client itself.
- Issues: https://github.com/fish-shell/fish-shell/issues/11092

### ESC during command execution causes a later OSC/DSR reply to be accepted as literal input
- Symptom: Running `sleep 1`, then pressing ESC an odd number of times within that second, causes a
  terminal OSC background-color reply (`]11;rgb:2424/2424/2424`) to be typed into the next prompt as if
  it were command text, producing `fish: Unknown command: ']11'`.
- Root cause: pressing ESC during command execution apparently triggers/queues a terminal query whose
  asynchronous reply then arrives after control returns to the reader, and is read as ordinary keyboard
  input rather than recognized/consumed as a terminal response.
- How fish resolved it: reported and root-caused to the escape-sequence/terminal-reply race described
  above; fix direction was to consume expected terminal replies rather than hand them to the line buffer.
- Issues: https://github.com/fish-shell/fish-shell/issues/12379

### Fish doesn't flush pending input before regaining terminal control (echoback / TTY hijack)
- Symptom: Killing a foreground command (`cat`) before it consumes input typed into it lets that
  now-orphaned input be picked up by fish's own prompt instead — a security-relevant "TTY echoback"
  class of bug (malicious file content read by `cat` could feed escape sequences that get echoed back
  and then interpreted as commands).
- Root cause: fish doesn't call `tcflush`/discard the input queue when regaining control of the terminal
  after a foreground job exits, so bytes queued for the old job's stdin leak into the next read.
- How fish resolved it: proposed fix was to flush the input queue (`tcflush`) on regaining terminal
  control; referenced as the same class of vulnerability documented for other terminal apps (foot,
  general "ANSI terminal security" writeups).
- Issues: https://github.com/fish-shell/fish-shell/issues/10987

### tty driver's own cursor-column tracking desyncs from fish's after spawning an external command
- Symptom: Inside `script`, typing `cat`, Enter, then Tab, Backspace, Enter shows the OS pty layer
  emitting the wrong number of backspace characters to move the cursor back, because it thinks more
  characters were typed than actually were.
- Root cause: shared with other shells (originally filed against elvish) — the kernel tty driver
  maintains its own idea of cursor column for canonical-mode echo, and it can desync from what the
  application (fish) actually drew, especially around control characters like Tab.
- How fish resolved it: acknowledged as a cross-shell tty-layer issue, not fully fixable purely in fish.
- Issues: https://github.com/fish-shell/fish-shell/issues/4839

### Cursor position wrong in the history-pager when results contain CJK text
- Symptom: With `LANG=zh_CN.UTF-8`, using the reverse-history-search pager (Ctrl-R) misplaces the cursor
  when the matched history entries contain Chinese characters.
- Root cause: the pager's own cursor-positioning math has the same wide-character width miscalculation
  as the general width-measurement problems, applied inside the pager UI rather than the main line.
- How fish resolved it: reported against fish 4.4.0; same underlying class as CJK width issues above.
- Issues: https://github.com/fish-shell/fish-shell/issues/12444

### Mouse-reported click position wrong / broken for multi-line commands
- Symptom: Since fish 4.2.0, clicking with the mouse to reposition the cursor only works within the
  bottom visual row of a multi-line command; it can't move the cursor to a different row by clicking
  there.
- Root cause: mouse-click-to-cursor mapping wasn't updated to account for multi-row command lines,
  presumably assuming a single row's worth of column offset.
- How fish resolved it: reported as a regression against 4.2.0 (vs. working 4.1.2).
- Issues: https://github.com/fish-shell/fish-shell/issues/12121

### Chinese IME committed text not inserted on macOS
- Symptom: After upgrading, text committed by a Chinese input method (Squirrel/Rime) on macOS is silently
  dropped by fish's interactive line editor, while it works fine in bash in the same terminal.
- Root cause: unclear at report time — plausibly the newer keyboard-protocol/CSI-u input handling
  introduced in fish 4.x doesn't correctly recognize/pass through IME-composed text commit events.
- How fish resolved it: reported open against fish 4.9.0.
- Issues: https://github.com/fish-shell/fish-shell/issues/12974

---

## Paste

### Bracketed paste ending/state can be broken out of via a crafted `\cc`/SIGINT inside the payload
- Symptom: Pasting text whose payload includes a literal Ctrl-C byte causes the remaining "pasted"
  content to be executed automatically, defeating the purpose of bracketed paste (which should insert,
  not execute, arbitrary content).
- Root cause: the bracketed-paste state machine reacts to SIGINT/Ctrl-C bytes inside the paste payload
  the same way it would to a real interactive Ctrl-C, exiting paste mode early and letting the remainder
  be interpreted as normal keystrokes/execution.
- How fish resolved it: reported open; conceptually, paste mode should treat all bytes as literal until
  the real bracketed-paste end sequence, regardless of control-character content.
- Issues: https://github.com/fish-shell/fish-shell/issues/5888

### Terminal warns "paste bracketing was left on" after fish exits/crashes
- Symptom: iTerm2 shows a warning banner "Looks like paste bracketing was left on when an ssh session
  ended unexpectedly or an app misbehaved" after using fish.
- Root cause: fish enables bracketed-paste mode on the terminal but some exit paths (crash, unusual
  session termination) don't reliably disable it before the terminal session ends.
- How fish resolved it: patched to more reliably disable bracketed paste on exit; recurred across
  versions as new exit paths were added.
- Issues: https://github.com/fish-shell/fish-shell/issues/4521, https://github.com/fish-shell/fish-shell/issues/5991

### No way to fully disable bracketed paste — breaks paste inside other programs launched from fish
- Symptom: Pasting into a Python REPL launched from fish is broken because fish's bracketed-paste
  handling interferes, and unlike bash/zsh there's no `set enable-bracketed-paste off` equivalent.
- Root cause: fish always enables the terminal mode; it wasn't architected to let the mode be turned off
  (relevant since a subprocess like `python` also wants bracketed paste itself, and the two can conflict
  around when the mode is toggled).
- How fish resolved it: reported as a real gap versus bash/zsh; later became more configurable as part
  of the broader "terminal protocols" (`fish_features`) toggle system.
- Issues: https://github.com/fish-shell/fish-shell/issues/7906

### Pasted content leaks bracketed-paste control sequences onto the visible screen
- Symptom: Mode markers like `[?2004h`/`[?2004l` (bracketed paste enable/disable) sometimes appear as
  literal visible text prepended to or overwriting the first characters of console output, roughly 1 in
  20 command invocations.
- Root cause: a race between when fish toggles the bracketed-paste terminal mode and when it writes
  visible output means the mode-toggle escape sequence itself is sometimes not fully consumed/hidden by
  the terminal before content is drawn.
- How fish resolved it: reported against 3.3.1 as a regression from 3.1.0; tied to broader "terminal
  protocols toggled multiple times" chattiness later addressed in #10494.
- Issues: https://github.com/fish-shell/fish-shell/issues/8304, https://github.com/fish-shell/fish-shell/issues/8803

### Pasting during a running `bind`-triggered function can echo raw input to the screen
- Symptom: If input (e.g. from a paste) arrives while a `bind`-bound fish function is executing, that
  fishscript runs with external-command terminal modes (echo on), so the pasted bytes — including
  bracketed-paste framing sequences — can end up printed to the screen instead of being hidden.
- Root cause: the terminal-mode donation logic for bind-triggered scripts didn't distinguish "needs full
  external-command TTY control" from "just a builtin/fishscript that shouldn't echo," and was most
  reliably reproduced on sluggish WSL2 setups where the timing window widens.
- How fish resolved it: reported as needing a scoped fix (don't donate echo-enabled terminal modes to
  bind commands that don't need them).
- Issues: https://github.com/fish-shell/fish-shell/issues/7770

### Pasting into a multi-line command triggers an internal tokenizer error
- Symptom: With `commandline` already spanning multiple lines, pasting more text causes
  `__fish_tokenizer_state: Expected at most 1 args, got 2` and can corrupt the in-memory command line.
- Root cause: a regression in `fish_clipboard_paste` forcibly split the output of a command substitution
  on newlines with no opt-out, breaking the assumption that the tokenizer-state helper received exactly
  one argument.
- How fish resolved it: identified as a 3.2.0 regression in the paste helper's use of command
  substitution; also noted the same forced-newline-splitting bug likely affects completions elsewhere.
- Issues: https://github.com/fish-shell/fish-shell/issues/7782

### Very high CPU / hang displaying or pasting large strings
- Symptom: Commands/strings longer than roughly ~1KB cause 100% CPU usage on one or more cores in fish
  3.2, a regression from 3.1.2; separately, pasting long scripts produces up to ~40x as much output data
  as was pasted (long runs of `\r\n` and cursor-up sequences), and long pastes can outright freeze the
  command line, especially over SSH.
- Root cause: (large-string CPU) an O(n²)-ish or otherwise non-linear code path introduced in the
  redraw/highlighting logic for long lines; (paste explosion / freeze) excessive redraw churn while the
  multi-line paste is being incrementally inserted and re-highlighted, one redraw per inserted chunk
  rather than a single batched update.
- How fish resolved it: the CPU regression was fixed per-version as reported; the paste-explosion and
  SSH-paste-freeze reports remained open, pointing at the same "too many redraws during paste" root
  cause that motivated later synchronized-output work.
- Issues: https://github.com/fish-shell/fish-shell/issues/7837, https://github.com/fish-shell/fish-shell/issues/8812, https://github.com/fish-shell/fish-shell/issues/11668

### Backspace after a multi-line paste deletes more than intended
- Symptom: After pasting multi-line text with trailing newlines, pressing backspace once to remove a
  trailing newline instead removes all trailing newlines plus the last character of the line before
  them.
- Root cause: the backspace handler collapsed multiple trailing "virtual" newline boundaries in one
  operation instead of deleting exactly one grapheme/character per keypress.
- How fish resolved it: reported open against fish 4.5.0/4.6.0.
- Issues: https://github.com/fish-shell/fish-shell/issues/12689

### Terminal "protocol negotiation" chatter is excessive, worsened around paste/every prompt
- Symptom: Plain `echo foobar` in a fresh `--no-config` session writes on the order of 800-1500 bytes of
  terminal-protocol negotiation (bracketed paste, CSI-u key codes, etc.), toggling protocols on/off
  multiple times before even reaching the prompt — costly over SSH and pollutes `script`/asciinema
  recordings.
- Root cause: protocol enable/disable calls were sprinkled through `parser::eval_node` and fired far more
  often than logically necessary (measured: 8 enable/disable round-trips before the first prompt).
- How fish resolved it: identified precisely (excess toggles in `eval_node`); tracked for consolidation/
  reduction; related follow-on proposal to use terminal "synchronized output" sequences to at least batch
  the visual effect of this chatter.
- Issues: https://github.com/fish-shell/fish-shell/issues/10494, https://github.com/fish-shell/fish-shell/issues/8247

### Ctrl+V pastes instead of being an editable keybinding (macOS)
- Symptom: On macOS, pressing Ctrl+V (not Cmd+V) pastes clipboard contents directly instead of leaving
  it available as a normal bindable key; switching to bash removes the behavior, switching back
  restores it.
- Root cause: fish's default keybindings map Ctrl+V (or the terminal's own paste shortcut passthrough)
  to a paste action unconditionally, differing from user expectation that it should be a plain
  character/quote-next-character binding.
- How fish resolved it: reported open; effectively a default-keybinding design choice users found
  surprising.
- Issues: https://github.com/fish-shell/fish-shell/issues/6224

### Leading whitespace / parse-sensitive pastes silently dropped from history
- Symptom: A command with leading spaces (or one that otherwise trips a parse quirk) is executed but not
  added to history, so pressing Up afterward recalls an older command instead.
- Root cause: the history-append path is conditioned on successful "clean" parse/echo state that leading
  whitespace (meant as a "don't save to history" convention in some shells) or parse errors interfered
  with unexpectedly.
- How fish resolved it: reported as a history-population bug tied to leading-space handling.
- Issues: https://github.com/fish-shell/fish-shell/issues/4327

### Multi-line yanked/pasted text via Ctrl-Y is escaped as literal `\n` rather than a real newline
- Symptom: Yanking clipboard content that spans multiple "commands" via `C-y` inserts them with literal
  `\n` escapes and the following text becomes part of the first command's argument, rather than
  inserting actual multi-line editable content the way fish's own multi-line editor otherwise supports.
- Root cause: the yank path didn't reuse the same real-newline-insertion logic that `commandline` uses
  elsewhere, and instead flattened newlines to their escaped textual form.
- How fish resolved it: reported as a paste/yank UX gap; fish does support real multi-line editing
  elsewhere, so this was seen as an inconsistency rather than a fundamental limitation.
- Issues: https://github.com/fish-shell/fish-shell/issues/821

---

## Suggestions/completions/pager redraw

### Autosuggestion accepted without any input (phantom acceptance)
- Symptom: An autosuggestion is occasionally accepted onto the command line even though the user didn't
  press the accept key.
- Root cause: unclear at report time — consistent with a race between suggestion computation
  (asynchronous) and the input-event loop, where a stale "accept" signal fires after the buffer has
  already changed.
- How fish resolved it: reported open against fish 3.7.0.
- Issues: https://github.com/fish-shell/fish-shell/issues/10397

### `Ctrl-C`-bound kill-whole-line permanently breaks subsequent Tab completion for the same text
- Symptom: Binding `Ctrl-C` to `kill-whole-line`, completing `fish --ver<TAB>`, then killing the line with
  Ctrl-C, then re-typing the exact same partial command and pressing Tab again does nothing — completion
  silently fails to trigger a second time.
- Root cause: internal completion/pager state left over from the first completion cycle wasn't reset by
  the custom kill-whole-line binding (only the built-in cancel/clear paths reset it correctly).
- How fish resolved it: reported as a state-reset gap specific to custom bindings on Ctrl-C.
- Issues: https://github.com/fish-shell/fish-shell/issues/6937

### Vi-mode autosuggestion cursor placement is invalid / suggestion behaves poorly in normal mode
- Symptom: In vi mode, autosuggestion display and the reported cursor length/position become inconsistent
  when switching between insert and normal mode.
- Root cause: cursor-position and suggestion-length bookkeeping weren't kept consistent across the vi
  mode-switch code path (separate model than the plain-emacs-mode autosuggestion path).
- How fish resolved it: reported open against fish 3.7.0.
- Issues: https://github.com/fish-shell/fish-shell/issues/10286

### Completion display corrupted/overwrites previous terminal output when it doesn't fit
- Symptom: When a completion's rendered pager output doesn't fit in the remaining visible terminal
  area, it overwrites content from the first line of the viewport rather than from the cursor's actual
  line, in Terminal.app specifically.
- Root cause: the pager's anchor-row calculation assumed enough remaining rows below the cursor and
  didn't clamp/scroll when the terminal viewport was shorter than needed.
- How fish resolved it: reported open; same overflow-clamping gap as the "completion pager overflowing"
  issue under Wrapping and geometry.
- Issues: https://github.com/fish-shell/fish-shell/issues/4532, https://github.com/fish-shell/fish-shell/issues/692

### Autosuggestion experience specifically bad over high-latency links (mosh)
- Symptom: Autosuggestion redraw churn (recomputing and redrawing ghost text on every keystroke) feels
  laggy/disruptive when used over `mosh`, prompting requests to disable it.
- Root cause: autosuggestion recompute-and-redraw isn't batched/throttled enough to stay comfortable at
  high round-trip latency.
- How fish resolved it: motivated adding a way to disable autosuggestions and general awareness of
  mosh/high-latency redraw cost; not a single code fix so much as a UX/configurability response.
- Issues: https://github.com/fish-shell/fish-shell/issues/1363

---

## Key parsing

### Escape-key ambiguity vs. Alt-combinations requires a timing heuristic that's easy to get wrong
- Symptom: After fish reduced its default "escape delay" from 300ms (100ms in vi mode) to 30ms, users
  found they could almost never successfully register a lone Escape keypress or an Alt-combination —
  it was being swallowed or misinterpreted depending on typing speed.
- Root cause: distinguishing a standalone Esc keypress from the first byte of an Alt-combination (which
  also sends ESC + character) is fundamentally a timing guess in traditional terminal input, and the
  chosen default window was too aggressive for real human/keyboard-repeat timing.
- How fish resolved it: made the delay user-configurable via `$fish_escape_delay_ms`; later versions
  moved toward the Kitty keyboard protocol / CSI-u to eliminate the ambiguity where the terminal supports
  it, sidestepping the timing guess entirely.
- Issues: https://github.com/fish-shell/fish-shell/issues/6590, https://github.com/fish-shell/fish-shell/issues/6996, https://github.com/fish-shell/fish-shell/issues/7338, https://github.com/fish-shell/fish-shell/issues/7401, https://github.com/fish-shell/fish-shell/issues/1356

### Two-stage / prefix-key bindings not supported, indirectly causing accidental command execution
- Symptom: A user binds `kj`/`jk` as an Esc replacement in vi-mode insert bindings; because fish has no
  "wait indefinitely then time out and treat prefix as final" mechanism, pressing just `k` blocks
  waiting for `j` with no way to cancel; separately, some terminals combine keys like Esc+H fast enough
  to trigger the default "open manpage" binding for `H` when the user meant to just exit insert mode.
- Root cause: fish's input state machine has no notion of a bindable multi-key prefix sequence with
  independent timeout/fallback behavior distinct from the plain Escape-vs-Alt heuristic.
- How fish resolved it: individual workarounds proposed (patches modifying `input.cpp`'s peeking logic);
  not adopted as a general feature at time of report.
- Issues: https://github.com/fish-shell/fish-shell/issues/3398, https://github.com/fish-shell/fish-shell/issues/3904, https://github.com/fish-shell/fish-shell/issues/6950

### `bind \e...` sequences silently collide/override unrelated bindings
- Symptom: Binding one raw escape sequence (`bind \e\[3~ kill-word`) is later found to have overwritten
  the fish's own `-k dc` (delete-char) binding, because both resolve to the same underlying key-code
  entry.
- Root cause: named-key bindings (`-k dc`) and raw-escape-sequence bindings share the same underlying
  table keyed by decoded sequence, so binding the raw sequence silently replaces the named one without
  warning.
- How fish resolved it: reported as a surprising-but-explainable interaction; no dedicated "conflict
  warning" added at time of report.
- Issues: https://github.com/fish-shell/fish-shell/issues/4408

### `Delete` key deletes backward instead of forward (or doesn't work) in various setups
- Symptom: Multiple distinct reports: Delete removes the character *before* the cursor once a certain
  point is reached; Delete does nothing in vi-mode insert; backspace itself doesn't work in vi mode
  under `urxvt`/`terminology`; some terminals require enabling "keypad mode" (`smkx`) for Delete to send
  the expected sequence at all.
- Root cause: several independent causes bucketed under the same symptom: (a) terminal-specific Delete
  sequences not enabled/expected without keypad mode, (b) vi-mode insert bindings not wired to the
  Delete key on certain terminfo/terminal combos, (c) a genuine logic bug where hitting Delete past the
  end of remaining text fell through to a backward-delete code path.
- How fish resolved it: fixed piecemeal per report/terminal; no single unifying fix, reflecting how
  fragile terminfo-based key-sequence detection is across terminal emulators.
- Issues: https://github.com/fish-shell/fish-shell/issues/2139, https://github.com/fish-shell/fish-shell/issues/3653, https://github.com/fish-shell/fish-shell/issues/3899, https://github.com/fish-shell/fish-shell/issues/4019, https://github.com/fish-shell/fish-shell/issues/11303

### Modifier+arrow / word-boundary bindings (Ctrl-Backspace, Ctrl-Delete, Alt-Left/Right) missing or terminal-dependent
- Symptom: Ctrl+Backspace/Ctrl+Delete don't delete a whole word by default; Alt-Left/Right stop working
  after specific fish updates in specific terminals (iTerm, Ghostty) while continuing to work in others
  (Terminal.app); word-boundary definitions for Alt-B/Alt-F/Ctrl-W silently changed between versions,
  breaking muscle memory.
- Root cause: mix of (a) missing default bindings for common desktop-environment shortcuts that aren't
  standardized across terminals, (b) each terminal emitting a different escape sequence for the "same"
  modifier+key combination, and (c) intentional-but-undocumented changes to fish's own word-boundary
  logic between releases.
- How fish resolved it: added more default bindings for common xterm-like sequences over time; the
  cross-terminal inconsistency itself remains inherent to how terminals encode modified keys.
- Issues: https://github.com/fish-shell/fish-shell/issues/3730, https://github.com/fish-shell/fish-shell/issues/4770, https://github.com/fish-shell/fish-shell/issues/8790, https://github.com/fish-shell/fish-shell/issues/11004, https://github.com/fish-shell/fish-shell/issues/11914

### Kitty keyboard protocol / CSI-u adoption breaks previously-working keys and other programs
- Symptom: After fish adopted the Kitty keyboard protocol / extended CSI-u key reporting, numerous
  regressions appeared: number keys stop working on some keyboard layouts (AZERTY, shift-layer digits),
  Alt-combinations under a French/QWERTZ layout stop producing characters, Midnight Commander's Ctrl-O
  subshell shortcut breaks, `fzf` loses the ability to receive Ctrl-C after being piped to, and some
  terminals (Windows Terminal, zellij) misinterpret specific CSI-u sequences (e.g. `CSI = u`) as
  unrelated escape codes like "restore cursor position."
- Root cause: the CSI-u/Kitty protocol is a superset most terminals implement only partially or with
  bugs, and enabling it unconditionally exposes both fish-side gaps (unhandled modifier/shift
  combinations, key names not decoded into friendly bind targets) and terminal/multiplexer-side bugs
  (sequences misparsed by an intermediate program like zellij or Windows Terminal).
- How fish resolved it: mix of fish-side fixes (decode more modifier combos, add `fish_features
  no-keyboard-protocols` opt-out) and upstream bug reports filed against affected terminals/programs;
  fundamentally an ongoing compatibility-matrix problem, not a single fix.
- Issues: https://github.com/fish-shell/fish-shell/issues/10407, https://github.com/fish-shell/fish-shell/issues/10470, https://github.com/fish-shell/fish-shell/issues/10640, https://github.com/fish-shell/fish-shell/issues/10864, https://github.com/fish-shell/fish-shell/issues/10936, https://github.com/fish-shell/fish-shell/issues/10994, https://github.com/fish-shell/fish-shell/issues/11040, https://github.com/fish-shell/fish-shell/issues/11054, https://github.com/fish-shell/fish-shell/issues/11204, https://github.com/fish-shell/fish-shell/issues/11217, https://github.com/fish-shell/fish-shell/issues/11223, https://github.com/fish-shell/fish-shell/issues/11283, https://github.com/fish-shell/fish-shell/issues/11326, https://github.com/fish-shell/fish-shell/issues/11407, https://github.com/fish-shell/fish-shell/issues/11520, https://github.com/fish-shell/fish-shell/issues/11609, https://github.com/fish-shell/fish-shell/issues/12579, https://github.com/fish-shell/fish-shell/issues/12968

### Prompt visibly redrawn at seemingly-random moments while the user is typing
- Symptom: With no obvious trigger, the prompt is redrawn/reprinted while actively typing a command
  (particularly noted in git repositories, correlating with `git`-status-driven prompt segments).
- Root cause: an asynchronous background computation (e.g. git-status polling used by a prompt function)
  completing and triggering a forced repaint mid-typing, interacting poorly with in-flight input.
- How fish resolved it: reported and partially reproduced (tied to git-repo status checks); no single
  isolated fix identified in the thread.
- Issues: https://github.com/fish-shell/fish-shell/issues/10529

### Garbage/unexpected characters intermittently appended to input or output lines
- Symptom: Seemingly random extra characters (`l`, `h`, space, or combinations) get prepended/appended to
  the first or last line of input or output, especially right after a version upgrade, in specific
  terminal setups (Hyper on WSL).
- Root cause: not conclusively identified — consistent with partially-consumed/echoed terminal-mode
  toggle sequences (similar to the bracketed-paste marker leakage) being misrouted onto the visible
  line.
- How fish resolved it: reported open; overlaps with the broader "terminal protocol toggle leakage"
  class of bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/8240, https://github.com/fish-shell/fish-shell/issues/10160

---

## Multiplexers and remote

### fish fails to set up the terminal at all over certain remote transports
- Symptom: SSH-ing directly into a machine whose login shell is fish fails with "Could not set up
  terminal," while the same connection works fine over `mosh`, or when `ssh`/fish is instead launched
  from inside `tmux`; separately, later versions warn "Could not set up terminal" for specific `$TERM`
  values (e.g. `xterm-kitty`, `xterm-ghostty`) not present in the remote's terminfo database, and fall
  back to hardcoded xterm-256color values.
- Root cause: fish's terminal setup depends on terminfo lookups keyed by `$TERM`; when the remote
  system's terminfo database doesn't have an entry for the client's actual terminal (common with newer
  terminals like kitty/ghostty, or minimal remote installs), setup fails or silently degrades to a
  fallback that may not match the real terminal's capabilities.
- How fish resolved it: added explicit fallback-to-xterm-256color behavior with a warning (rather than
  hard failure) for unknown `$TERM`; root terminfo-availability problem on remote hosts remains an
  inherent SSH/terminfo distribution issue, later motivating a fish effort to reduce/remove reliance on
  the terminfo database entirely.
- Issues: https://github.com/fish-shell/fish-shell/issues/1060, https://github.com/fish-shell/fish-shell/issues/10207, https://github.com/fish-shell/fish-shell/issues/11277

### tmux-specific interaction bugs: CPU spin on detach, resize propagation, prompt corruption
- Symptom: A fish process pinned to 100% CPU after closing a terminal without detaching from a tmux
  session started via fish (but not via bash); separately, fish-launched tmux sessions don't receive
  SIGWINCH on sudden resizes; multi-line prompts get corrupted specifically inside tmux on narrow resize;
  general reports of "fish shell will cause a problem when using tmux."
  processing.
- Root cause: variously root-caused: the SIGWINCH-swallowing case (#3180) was a controlling-TTY/
  foreground-process-group assignment bug (duplicate of #2980 — using `exec` inside a fish function fixes
  it by making fish set tty modes/foreground group the same way an interactive command would); the CPU
  spin and general "tmux breaks" reports were not as cleanly root-caused.
- How fish resolved it: #2980's fix (correct tty-mode/pgrp handling for non-exec'd child processes) also
  resolved the SIGWINCH-swallowing duplicate; other tmux interaction bugs fixed independently or left
  open.
- Issues: https://github.com/fish-shell/fish-shell/issues/1097, https://github.com/fish-shell/fish-shell/issues/3180, https://github.com/fish-shell/fish-shell/issues/4468, https://github.com/fish-shell/fish-shell/issues/8628

### mosh-specific arrow-key and parser interaction bugs
- Symptom: Arrow keys typed through mosh (specifically the Chrome NaCl mosh client) arrive as literal
  `[A`/`[B`/`[C`/`[D` text rather than being recognized as cursor-movement escape sequences; separately,
  enabling an in-development "new parser" broke launching `mosh` itself with a file-not-found-style
  error.
- Root cause: (arrow keys) the specific mosh client didn't prefix the sequence with an actual ESC byte
  the way fish's key-sequence matcher expected, so it fell through to literal-text handling; (parser
  break) an unrelated regression in argument/command resolution logic that happened to affect how `mosh`
  was located/executed.
- How fish resolved it: reported/tracked; arrow-key case is a mosh-client compatibility gap more than a
  pure fish bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/1270, https://github.com/fish-shell/fish-shell/issues/1276

### Remote-session color/host indicators don't recognize all remote-access tools
- Symptom: `$fish_color_host_remote` correctly colors the hostname differently when connected via `ssh`
  or `mosh`, but not when connected via EternalTerminal (`et`); truecolor prompt colors reported broken
  specifically over SSH in another report.
- Root cause: fish detects "this is a remote session" by checking for the `SSH_TTY` environment variable
  specifically, which other remote-access tools don't set; truecolor detection over SSH is separately a
  terminfo/`$TERM`-forwarding problem (the remote side doesn't know the true local terminal's color
  capability).
- How fish resolved it: root cause identified for the `et` case (hardcoded `SSH_TTY` check, no
  general/extensible remote-detection mechanism); truecolor-over-SSH is an inherent limitation of relying
  on `$TERM`/terminfo propagation across the SSH boundary.
- Issues: https://github.com/fish-shell/fish-shell/issues/11253, https://github.com/fish-shell/fish-shell/issues/11391

### Windows Terminal / ConPTY / cmd.exe nested-session width miscalculation
- Symptom: Starting a WSL Windows Terminal session, then running `cmd.exe /c wsl.exe` to nest a second
  fish session inside it, causes fish's line/character width calculation to break — stray characters
  (`h`, `lh`) get appended to output like "Welcome to fish, the friendly interactive shellh" or
  "echo hellolh".
- Root cause: the specific nested conhost/ConPTY-under-WSL-under-Windows-Terminal chain produces terminal
  dimension or capability reporting that fish's width/geometry logic doesn't handle, distinct from the
  same scenario without nesting (which works fine).
- How fish resolved it: reported and reproduced precisely, not conclusively fixed in the thread.
- Issues: https://github.com/fish-shell/fish-shell/issues/8948

---

## Other

### New universal-notifier (inotify-based) implementation causes fish to hang on exit
- Symptom: fish repeatedly hangs forever on exit (uninterruptible sleep state, can't even attach a
  debugger) after a specific internal change; reproduces consistently once triggered, until reboot.
- Root cause: bisected to the introduction of FFI bindings for universal notifiers adopted in
  `input_common`, specifically the new inotify-based Linux notifier — a low-level resource/thread
  lifecycle bug rather than a display bug per se, but directly breaks the interactive reader's shutdown
  path.
- How fish resolved it: root-caused via bisection to a specific commit; fix required addressing the
  notifier's exit/cleanup behavior.
- Issues: https://github.com/fish-shell/fish-shell/issues/10878

### `input_readch` spins at 100% CPU when the controlling terminal is closed
- Symptom: fish pins a CPU core at 100% usage specifically when the graphical terminal it's running in
  is closed (under certain conditions), traced to a tight loop in the input-reading code.
- Root cause: a code path in `input_readch`/its callers kept calling into the same read logic without
  properly detecting/handling the terminal-gone (EOF/error) condition, spinning instead of exiting or
  blocking.
- How fish resolved it: confirmed via gdb backtrace pointing at the specific loop; fixed as an input-loop
  termination-condition bug.
- Issues: https://github.com/fish-shell/fish-shell/issues/3214
