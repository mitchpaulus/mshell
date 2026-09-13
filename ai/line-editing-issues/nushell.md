# Nushell / Reedline interactive line-editing failure modes

Sources: `nushell/reedline` and `nushell/nushell` issue trackers (GitHub search via `gh issue list` /
`gh issue view --comments`). Scope: interactive prompt/line-editor display and input bugs only.

## Width measurement

### CJK/full-width characters break IDE-layout completion menu
- Symptom: When file/directory names contain full-width (CJK) characters, the `ide`-layout completion menu's column alignment is broken — entries are visually shifted/misaligned.
- Root cause: the menu layout computes column positions from character count rather than actual terminal display width (full-width glyphs occupy 2 columns).
- How resolved: closed as fixed in reedline; the nushell-side duplicate was redirected to the reedline issue.
- Issues: https://github.com/nushell/reedline/issues/997, https://github.com/nushell/nushell/issues/17281

### Non-ASCII path characters miscounted when wrapping completion candidates
- Symptom: Autocompletion inserted spurious newlines before every candidate when completing paths containing a character whose display width differs from a plain ASCII character, even though the line actually fit on screen.
- Root cause: the wrap-width calculation used a width function that doesn't match what the terminal actually renders for certain codepoints/grapheme clusters (confirmed to vary even between terminals: reporter tested Alacritty vs WezTerm with different grapheme-merging behavior).
- How resolved: open — acknowledged as terminal-width-measurement disagreement with no full fix landed.
- Issues: https://github.com/nushell/reedline/issues/572

### Columnar completion menu column spacing wrong with accents/emoji
- Symptom: In the columnar completion menu, columns become misaligned ("shifted") when candidate filenames contain accented characters or emoji.
- Root cause: column width/padding computed from byte or char count instead of display width of the grapheme.
- How resolved: open at time of report (root-caused in issue thread, no confirmed final fix tracked in this search).
- Issues: https://github.com/nushell/reedline/issues/793

### Panic on multi-byte UTF-8 completion candidates containing punctuation
- Symptom: Reedline panics when the completion menu contains UTF-8 (e.g. Japanese) candidates followed by a `(` character, when the user types a prefix and presses Tab.
- Root cause: byte-offset slicing of the candidate string done without respecting UTF-8 character boundaries.
- How resolved: fixed (linked to a nushell-side duplicate, closed).
- Issues: https://github.com/nushell/reedline/issues/837, https://github.com/nushell/nushell/issues/13951

### Panic in fuzzy completion on filenames starting with a multibyte character
- Symptom: Fuzzy-matching completion panics when a directory contains files whose names start with a multibyte Unicode character and the user types a prefix and hits Tab.
- Root cause: fuzzy-match scoring/highlighting code indexed into the filename string using byte offsets that didn't align to UTF-8 char boundaries.
- How resolved: fixed via a linked PR (multiple related nushell-side issues consolidated into this one).
- Issues: https://github.com/nushell/reedline/issues/919

### Table wrapping hangs/OOMs on certain emoji (variation-selector sequences)
- Symptom: Rendering a table whose column text must be line-wrapped and contains certain emoji (e.g. `©️`, `⚠️` — base codepoint + U+FE0F variation selector) causes an infinite loop that pins a CPU core and grows memory until the process is OOM-killed.
- Root cause: the line-wrap width accounting for a codepoint that has zero/ambiguous display width (the variation selector) miscomputes remaining space, causing the wrap loop to never make forward progress.
- How resolved: fixed via linked PR; a second occurrence was a duplicate of the same class of bug.
- Issues: https://github.com/nushell/nushell/issues/15256, https://github.com/nushell/nushell/issues/15505

## Grapheme/cursor editing

### Vi visual-mode selection excludes the character under the cursor
- Symptom: In Vi visual selection mode, the selection does not include the last character under/at the cursor, unlike real Vim where both endpoints are inclusive.
- Root cause: `get_selection` computed the end offset without advancing one grapheme cluster past the cursor.
- How resolved: fixed.
- Issues: https://github.com/nushell/reedline/issues/843

### Vi cursor semantics not mode-aware (root cause of a family of off-by-one bugs)
- Symptom: A cluster of Vi-mode bugs (cursor lands one position too far right/left, `$`/word-motions land past end of line, etc.) all traced back to one design flaw.
- Root cause: `LineBuffer::insertion_point` is a plain byte offset with no mode-aware bounds checking; Vi insert mode needs the cursor to sit *between* characters (range `0..=len()`) while Vi normal mode needs it to sit *on* a character (range `0..last_grapheme_start`), and reedline didn't distinguish the two.
- How resolved: acknowledged by maintainers as the correct diagnosis; tracked as the umbrella issue for several related bug reports.
- Issues: https://github.com/nushell/reedline/issues/1066

### Vi-mode cursor doesn't behave like real Vi
- Symptom: Pressing Escape from insert mode doesn't step the cursor back one position as in Vim/zsh; moving to the last character with `$`/`w`/`e` places the cursor one position after the last character instead of on it.
- Root cause: insert-mode/normal-mode cursor semantics conflated (same underlying issue as above, reported independently and earlier).
- How resolved: fixed via linked PR.
- Issues: https://github.com/nushell/reedline/issues/694

### Vi `r` (replace character) moves the cursor forward
- Symptom: Using `rX` in Vi normal mode to replace the character under the cursor leaves the cursor one position to the right, whereas Vim keeps the cursor on the replaced character.
- Root cause: character-replace edit command advances the cursor like an insert, rather than leaving it in place as a normal-mode positional edit.
- How resolved: open at time of report.
- Issues: https://github.com/nushell/reedline/issues/1026

### History arrow-key navigation doesn't relocate the cursor
- Symptom: Pressing Up to cycle through history replaces the buffer text but leaves the cursor at its old position instead of moving it to the end (or expected position) of the newly-loaded history entry.
- Root cause: `update_buffer_from_history` updates the buffer contents without recomputing/moving `insertion_point`.
- How resolved: discussed as intended-but-confusing prefix-search behavior; usability concerns raised, tracked for follow-up.
- Issues: https://github.com/nushell/reedline/issues/113

### `commandline -c` (host-reported cursor position) is byte/char-counted, not line-aware
- Symptom: A keybinding that queries the current cursor position via `commandline -c` gets a value that's wrong once the buffer spans multiple lines — position is computed by counting characters, ignoring line breaks.
- Root cause: cursor-position query logic treats the buffer as a flat character sequence instead of accounting for line boundaries when converting to a row/column-relevant offset.
- How resolved: open in reedline; a corresponding nushell-side issue was filed to track the user-visible effect.
- Issues: https://github.com/nushell/reedline/issues/685

## Wrapping and geometry

### Wrap-triggered cursor-position math wrong when the wrap happens at the bottom row
- Symptom: When a line wraps and that wrap pushes content past the bottom of the terminal (shifting everything up), the tracked cursor screen-position becomes wrong.
- Root cause: cursor-position tracking didn't account for the screen scrolling as a side-effect of a wrap at the last row, and needed to avoid constantly polling the real terminal cursor position (too slow) while still tracking size/prompt/continuation-prompt changes.
- How resolved: resolved (closed as fixed for the reported case).
- Issues: https://github.com/nushell/reedline/issues/176

### Wrap detection only ran on single-character insertion
- Symptom: Operations other than typing a single character — paste, undo — could also cause the buffer to overflow a line and require a wrap/cursor-repositioning, but the wrap check wasn't invoked for those paths, leaving stale cursor/paint state.
- Root cause: wrap detection was hard-coded into the "insert one char" code path instead of being a general post-edit invariant.
- How resolved: fixed by moving wrap-detection logic out of the insertion-specific path and into the painter, applied uniformly after any edit.
- Issues: https://github.com/nushell/reedline/issues/186

### Multiline repaint spawns extra prompt lines when the prompt sits at the bottom of the screen
- Symptom: Entering a newline (Alt-Enter) for a multi-line command while the prompt is positioned near the bottom of the terminal causes every subsequent repaint to insert an extra, erroneous prompt line; stops only after `Ctrl-L` repositions the prompt to the top.
- Root cause: repaint logic recomputed the prompt's anchor row incorrectly once the buffer had scrolled the screen.
- How resolved: fixed via linked PR.
- Issues: https://github.com/nushell/reedline/issues/119

### Multi-line command's last line gets overwritten by command output
- Symptom: In a multi-line `do { ... }` block, if the cursor is left on any line other than the last one when Enter is pressed, the command's own output overwrites the last visible line of the command instead of appearing below it.
- Root cause: the row the painter assumed was "last row of input" was computed relative to cursor position rather than the actual end of the buffer.
- How resolved: fixed (confirmed by reporter after updating to a newer reedline).
- Issues: https://github.com/nushell/reedline/issues/261

### Prompt overwrites the previous output line when that line has no trailing newline
- Symptom: If a preceding program prints a final line without a trailing `\n` (e.g. a partial write from `notepad`/`cat`), the next reedline prompt is drawn on top of that line instead of below it.
- Root cause: reedline's row/position bookkeeping assumes prior output always ends with a newline before it starts drawing the prompt.
- How resolved: open; reporter's testing suggests it also interacts with terminal size / prior resizes.
- Issues: https://github.com/nushell/reedline/issues/398

### Multi-line input taller than the terminal window panics
- Symptom: Pasting or otherwise entering content that requires more lines than the terminal currently has available crashes reedline (originally a panic while calculating layout for oversized input).
- Root cause: layout/position math assumed the buffer's rendered height always fits within the terminal height.
- How resolved: fixed via linked PR.
- Issues: https://github.com/nushell/reedline/issues/183

### Completion menu overflow not truncated or wrapped for narrow/long terminals
- Symptom: Completion candidates longer than the terminal width, or a columnar menu in a narrow terminal where entries need more than one row, aren't truncated or measured correctly — this causes empty padded lines, the initially-selected item to be invisible, or (in narrow terminals) the prompt itself to be pushed off-screen/disappear.
- Root cause: menu-height/row estimation assumed one candidate = one screen row and didn't reconcile against actual candidate render width.
- How resolved: reedline-side case fixed via linked PR; the nushell-side report was identified as the same underlying reedline issue.
- Issues: https://github.com/nushell/reedline/issues/972, https://github.com/nushell/nushell/issues/13141

### Columnar menu doesn't truncate long descriptions, corrupting prompt rendering
- Symptom: When a completion candidate's description text is too long to fit on one line, the columnar menu's rendering breaks and distorts the prompt below it.
- Root cause: description strings inserted into the menu row without a width-based truncation/ellipsis step.
- How resolved: investigated as caused by a specific PR change; tracked for fix.
- Issues: https://github.com/nushell/reedline/issues/738

### Prompt explodes/crashes when terminal is narrower than the prompt itself (Windows)
- Symptom: Shrinking the terminal window until the prompt string itself would need to wrap causes nushell to error out ("explode") on Windows, rather than degrading gracefully.
- Root cause: prompt-width vs terminal-width edge case not handled — code assumed the prompt always fits on one row.
- How resolved: reported as fixed in a later version.
- Issues: https://github.com/nushell/nushell/issues/439

## Prompt content

### Terminal cursor invisible when the prompt indicator is an empty string
- Symptom: Setting `PROMPT_INDICATOR` to `""` makes the blinking terminal cursor disappear entirely until the user types a character.
- Root cause: cursor-show/hide logic tied to the presence of visible prompt-indicator content rather than being independent of it.
- How resolved: reported in the wrong repository by the author (reedline vs nushell) — left unresolved in this thread.
- Issues: https://github.com/nushell/reedline/issues/507

### Menu/prompt marker starting with a newline swallows the rest of the string
- Symptom: If a completion-menu marker string begins with `\n`, everything after the newline fails to render — only the newline itself shows.
- Root cause: painter's line-splitting/row-advance logic for the marker mishandled a leading newline as consuming the whole segment.
- How resolved: fixed via linked PR.
- Issues: https://github.com/nushell/reedline/issues/707

### Right prompt drawn one row above the left prompt
- Symptom: Using `oh-my-posh` with both left and right prompts, the right prompt is rendered one line above the left prompt as soon as the prompt is redrawn (e.g. after resize/first redraw), instead of level with it.
- Root cause: not fully root-caused in the thread; behavior changed between nushell versions and was sensitive to an "always keep right-prompt on last line" config flag, suggesting the multi-line-prompt row anchor used for the right prompt diverges from the one used for the left prompt.
- How resolved: open — workaround (config flag) identified but underlying cause unclear even to maintainers.
- Issues: https://github.com/nushell/reedline/issues/804

### Long history-search hint text hides the prompt
- Symptom: When the inline history "ghost text" hint is long and the terminal is small, the hint's wrapped lines push the prompt off-screen/out of view.
- Root cause: precisely diagnosed in-thread — the routine that decides how many buffer lines to skip when repainting (`skip_buffer_lines`) only counts explicit `\n` characters and ignores lines produced purely by soft line-wrapping.
- How resolved: open, with root cause pinpointed to a specific function/line range in `painting/painter.rs`.
- Issues: https://github.com/nushell/reedline/issues/974

### Prompt jumps to the last screen row on first keystroke
- Symptom: On Windows Terminal, any input action (typing, arrow key, even backspace with nothing to delete) causes the prompt to immediately jump to the bottom line of the terminal.
- Root cause: traced to reedline defaulting to an 80x24 terminal size when no size is reported by the terminal at startup, then correcting once a real size arrives — causing a visible jump.
- How resolved: open; root cause (fallback size before first real size report) identified by a maintainer.
- Issues: https://github.com/nushell/reedline/issues/665

### Prompt renders on the wrong row when cursor is near the bottom of the screen
- Symptom: With the cursor one row from the bottom of the terminal, running a command causes the following new prompt to render on an incorrect row.
- Root cause: not fully diagnosed in the thread (reporter supplied a reliable repro); believed to be a row-tracking bug in reedline's prompt placement.
- How resolved: open — repro confirmed, fix not yet landed.
- Issues: https://github.com/nushell/reedline/issues/527

### Prompt redrawn on every keystroke when its start row is row 0
- Symptom: After clearing the screen (prompt anchored at the very top row), every keystroke causes a full new prompt to be rendered rather than an in-place update.
- Root cause: believed tied to how the PTY reports geometry in this configuration (discussion points at similar WezTerm-on-Windows behavior); not conclusively fixed.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/756

### Prompt climbs one screen row per keystroke when the kernel-reported terminal height is wrong
- Symptom: When the OS-reported terminal size (winsize) is smaller than the actual attached terminal (serial console, IPMI SOL, `expect`-driven sessions, etc.), the prompt visibly moves up one row per keystroke, leaving blank rows below it, until it settles at the wrong "bottom."
- Root cause: reedline anchors the prompt based on the believed-bottom row derived from a possibly-stale/incorrect winsize; when actual terminal geometry disagrees, each repaint re-derives a different anchor.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/1205

## Resize

### Terminal resize overwrites prior scrollback output
- Symptom: Resizing the window (observed both when the screen was full and only partially full) causes previously-printed output above the prompt to be overwritten/lost.
- Root cause: resize repaint logic recomputes screen layout without preserving what was already on screen above the current input; horizontal resizes in particular "eat" previous output.
- How resolved: partially improved (vertical resizing behavior improved by a later PR) but horizontal resize still eats output; tracked as ongoing.
- Issues: https://github.com/nushell/reedline/issues/42

### Shrinking the window below the prompt's current row causes repaint corruption
- Symptom: If the terminal is shrunk vertically enough that the current prompt row would now be off-screen, the prompt repaints incorrectly (extra/misplaced lines), reproducing the same symptom as the bottom-of-screen wrap bug.
- Root cause: repaint math doesn't re-anchor the prompt when the previously valid row number is invalidated by a smaller window.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/157

### Unthrottled resize events cause visible lag/animation
- Symptom: Dragging to resize the window causes every intermediate resize event to be processed, so the redraw visibly lags and "animates" between sizes instead of settling immediately.
- Root cause: no debouncing/coalescing of consecutive resize events — each one triggers a full repaint.
- How resolved: performance improved by only using the latest event in a batch (correctness issues from resize tracked separately).
- Issues: https://github.com/nushell/reedline/issues/166

### No "intelligent" resize reflow compared to other shells
- Symptom: Compared to fish, nushell's handling of terminal resize is visibly worse (screenshots comparing the two side by side) — content doesn't reflow the way users expect.
- Root cause: unresolved design gap; maintainers acknowledge the experience is worse and no one has implemented a proper fix.
- How resolved: open, long-standing, explicitly described by maintainers as something they'd like someone to tackle.
- Issues: https://github.com/nushell/reedline/issues/477

### Multi-line prompt overlaps itself after a window resize
- Symptom: With a multi-line prompt, resizing the terminal window causes the next-drawn prompt to overlap the previous one rather than replacing it cleanly.
- Root cause: not resolved in-thread; consistent with the general resize-anchor-invalidation class of bugs above.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/809

### Right-prompt repaint breaks specifically on resize
- Symptom: With a multi-line left prompt and a non-empty right prompt, resizing the terminal causes incorrect repaint; behavior differs across terminals (Ghostty vs WezTerm) and is affected by whether OSC 133 shell-integration is enabled.
- Root cause: unresolved; multiple interacting factors (right-prompt placement, OSC133, multi-line left prompt) identified but no single root cause confirmed.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/897

### Prompt disappears on resize due to terminal's own shell-integration heuristics
- Symptom: In Ghostty, resizing the window made the prompt disappear.
- Root cause: diagnosed by the terminal author — Ghostty, upon seeing an OSC-133 "prompt start" semantic marker, assumes the shell integration will also redraw the prompt on resize; reedline wasn't doing that redraw, so the terminal's own compensating behavior produced a blank prompt.
- How resolved: fixed via linked PR (reedline now redraws the prompt on resize).
- Issues: https://github.com/nushell/reedline/issues/684

### Window maximize/minimize cycling reprints the prompt (Windows)
- Symptom: On Windows, maximizing/minimizing the terminal window (or switching focus via the taskbar) causes the prompt to be reprinted, and in some cases program output is pushed up out of easy view even though the resize itself is otherwise handled correctly.
- Root cause: window state transitions on Windows generate resize events that trigger the same repaint path as an intentional resize.
- How resolved: open; follow-up testing showed output is preserved in scrollback but the extra reprint remains.
- Issues: https://github.com/nushell/reedline/issues/620

### Prompt-computing closure not re-invoked after resize
- Symptom: A custom prompt that computes content based on terminal width (e.g. a horizontal fill/divider bar) does not recompute after the terminal is resized — it keeps rendering based on the width at prompt-creation time until the next full redraw trigger.
- Root cause: resize handling redraws the existing prompt string rather than re-invoking the user's `PROMPT_COMMAND` closure to regenerate width-dependent content.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/938

## Terminal queries and input races

### Cursor position query hangs/times out, breaking the session
- Symptom: Reedline reports the error "The cursor position could not be read within a normal duration," intermittently, on macOS and other platforms; occurs without a clear, stable repro.
- Root cause: not conclusively identified — related to `crossterm`'s device-status-report (`\e[6n`) query mechanism possibly failing to acquire an internal event-reader lock, or the terminal not answering the query in time.
- How resolved: open on both reports; maintainers suggested raising it against `crossterm` directly.
- Issues: https://github.com/nushell/reedline/issues/132, https://github.com/nushell/reedline/issues/870

### ~2 second prompt-display delay on terminals that don't answer cursor-position queries
- Symptom: Every prompt display takes about 2 seconds to appear when connected via certain terminal emulators (reported via SSH into a Termius/Android session).
- Root cause: precisely diagnosed — `initialize_prompt_position()` issues a synchronous `\e[6n` Device Status Report and blocks waiting for a response; on terminals/paths that never answer, this blocks for the full timeout on every prompt.
- How resolved: open; discussion of a `TERM=dumb`-style bypass so the query is skipped entirely for terminals known not to answer it.
- Issues: https://github.com/nushell/reedline/issues/1010

### Application-emitted `\e[6n` collides with reedline's own cursor query
- Symptom: If the user's own command emits a cursor-position-request escape sequence (`printf "\e[6n"`), the terminal's response (`^[[<row>;<col>R`) leaks into the next prompt and gets echoed/re-emitted every time Enter is subsequently pressed.
- Root cause: the terminal's response to a stray DSR query is read back by reedline's input loop as ordinary keyboard input, and something in the resulting state gets replayed on every Enter afterward. Confirmed reproducible in reedline directly, not just full nushell.
- How resolved: open; contingent on a `crossterm` release with better handling, which then needs propagating through reedline and nushell.
- Issues: https://github.com/nushell/reedline/issues/860

### Optimizing away a cursor-position call changed external-editor return behavior
- Symptom: After a performance change that avoided calling `crossterm::cursor::position()` in some paths, returning from an external editor (`Ctrl+O`) started rendering the updated line one row below where it used to (previously it redrew in place).
- Root cause: the removed/optimized cursor-position call had been implicitly relied on to re-anchor the prompt row after control returned from an external process.
- How resolved: reported as a regression from a specific PR; behavior change acknowledged.
- Issues: https://github.com/nushell/reedline/issues/1106

### Cursor visibly flickers to wrong positions during repaint
- Symptom: While reedline repaints (buffer-only repaint, full repaint including prompt, or when drawing an overlay like the completion list), the terminal cursor briefly flickers to an incorrect position before settling.
- Root cause: cursor hide/show (`crossterm::cursor::Hide`/`Show`) wasn't consistently paired around every code path that temporarily moves the cursor to paint something else.
- How resolved: fixed by a rewritten painter that centralizes cursor visibility handling.
- Issues: https://github.com/nushell/reedline/issues/174

## Paste

### No bracketed paste support at all (pastejacking risk)
- Symptom: Pasted content, including any embedded newlines that could look like commands, was indistinguishable from typed input — no bracketed-paste markers were used, so terminal-injected malicious pastes could auto-execute.
- Root cause: bracketed paste mode was never enabled/handled; required updated `crossterm` support for the ANSI sequences.
- How resolved: implemented.
- Issues: https://github.com/nushell/reedline/issues/543

### Bracketed paste doesn't preserve newlines on some terminals
- Symptom: Pasting multi-line text (e.g. a Rust struct definition) into reedline reorders/mangles the lines — content that should be on separate lines gets concatenated or scrambled.
- Root cause: some terminals (e.g. iTerm2, and separately most terminals on Windows) send `\r` instead of `\n` for pasted line breaks inside the bracketed-paste sequence; reedline didn't normalize this.
- How resolved: fix proposed (`\r` → `\n` translation); Windows bracketed paste noted as still broken even after the fix for the macOS terminal case.
- Issues: https://github.com/nushell/reedline/issues/576

### Bracketed paste only works the first time in a session
- Symptom: The first paste of multi-line content works; after pressing Ctrl-C, a subsequent paste attempt fails.
- Root cause: some paste-mode state wasn't being reset when the line editor was interrupted/reset via Ctrl-C.
- How resolved: fixed via linked PR.
- Issues: https://github.com/nushell/reedline/issues/648

### Multi-line paste of exactly ≥10 characters ending without further input doesn't render
- Symptom: Pasting a short multi-line-looking sequence of 10 or more characters produces no visible text until one more character is typed (at which point everything appears at once); 9 characters or fewer render immediately.
- Root cause: unclear in-thread — flagged as related to the paste-state issue above; suggests a buffering/threshold bug in how paste bursts are detected and flushed to the display.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/649

### Multi-line paste from an embedded terminal garbles content
- Symptom: Pasting multi-line text copied from inside a Neovim `:terminal` (running inside iTerm2) into a reedline app produces scrambled/reordered text.
- Root cause: not conclusively identified; likely an interaction between the nested-terminal's own paste handling and reedline's bracketed-paste parsing.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/558

### Buffer corruption / stray spaces after pasting content taller than the terminal
- Symptom: Pasting multi-line text whose total height exceeds the terminal window height results in unexpected extra spaces at the paste boundary and a visibly corrupted buffer.
- Root cause: not fully diagnosed; overlaps with the "input taller than terminal" wrapping/geometry class of bug.
- How resolved: reporter later found it "might've been fixed" by an unrelated update — not confirmed root-caused.
- Issues: https://github.com/nushell/reedline/issues/703

### Perceptible lag when the terminal (not the app) performs the paste
- Symptom: Since a specific version, pasting via terminal-handled paste (as opposed to reedline's own `PasteSystem`/`PasteCutBufferBefore` commands) shows visible staged/laggy rendering — e.g. a pasted URL appears in two visibly separate chunks with a delay between them.
- Root cause: not fully diagnosed; behavior change traced to a specific reedline version bump.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/907

### Large multi-line paste breaks normal terminal scrolling
- Symptom: After pasting a large multi-line block (50+ lines), mouse-wheel/trackpad scrolling in the terminal stops working normally — only the last few lines are reachable via scroll, and navigating the pasted content requires arrow keys instead.
- Root cause: not conclusively diagnosed; suspected interaction with how bracketed paste content is buffered/rendered relative to terminal scrollback (note: reporter's Windows repro doesn't even support bracketed paste, muddying the diagnosis).
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/948

### Some terminals (Warp/ConPTY) don't reliably send true bracketed-paste boundaries
- Symptom: Certain terminal/PTY combinations don't cleanly wrap pasted input in bracketed-paste start/end markers, making it impossible for reedline to reliably distinguish "pasted" from "typed" input on those platforms.
- Root cause: platform/terminal limitation, not a reedline bug per se — proposed as a gap requiring host-level workarounds.
- How resolved: open — proposal for optional host hooks (paste interception + timing-based "paste burst" heuristic) to work around terminals that don't support real bracketed paste.
- Issues: https://github.com/nushell/reedline/issues/1127

## Hints/completions/menu redraw

### Duplicate entries shown in the completion menu
- Symptom: Triggering Tab-completion shows the same candidate listed more than once, and arrow-key navigation can't reach the other (non-duplicated) items.
- Root cause: not detailed in the thread beyond the repro; implied stale/duplicated candidate list state.
- How resolved: fixed.
- Issues: https://github.com/nushell/reedline/issues/300

### History menu doesn't render until a subsequent keypress
- Symptom: Invoking the history menu via its keybinding shows the prompt's history-search marker but no actual menu content until the user presses Backspace.
- Root cause: menu redraw wasn't triggered by the keybinding-invoked open event itself, only by a subsequent buffer-changing edit.
- How resolved: fixed.
- Issues: https://github.com/nushell/reedline/issues/301

### Crash paging through the history menu
- Symptom: Repeatedly paging down through the history menu (~10 times) then pressing `q` causes a panic ("slice index starts at 377 but ends at 29").
- Root cause: an out-of-bounds slice operation in the history-menu rendering code when the scroll offset exceeds the available history length.
- How resolved: reported without a confirmed fix in this thread (no closing comment captured).
- Issues: https://github.com/nushell/reedline/issues/316

### Columnar-menu rendering breaks when suggestions include a description
- Symptom: Adding descriptions to completion suggestions in a `ColumnarMenu` distorts the menu's rendering.
- Root cause: menu row-width calculation didn't correctly account for the extra description column's width.
- How resolved: fixed via linked PR.
- Issues: https://github.com/nushell/reedline/issues/522

### Selecting a menu item leaves stray text after the cursor
- Symptom: Selecting a completion/history-menu item is expected to replace the full relevant span of text, but text after the cursor position is sometimes left behind, producing a mixed/incorrect result.
- Root cause: the replacement span used when accepting a menu selection didn't account for `only_buffer_difference` / cursor-position edge cases consistently.
- How resolved: fix designed and agreed upon in-thread (config option semantics reworked); tracked to land in a following release.
- Issues: https://github.com/nushell/reedline/issues/1006

### Common prefix double-inserted when accepting a columnar-menu item
- Symptom: Selecting an item from a columnar completion menu (with partial completions enabled) inserts the already-displayed common prefix a second time.
- Root cause: the completer receives only what the user actually typed, not the common-prefix text that was already auto-inserted into the visible buffer when the menu opened, so its computed replacement re-adds that prefix.
- How resolved: root-caused in-thread; fix required threading the already-inserted prefix through to the completer.
- Issues: https://github.com/nushell/reedline/issues/912

### Scrolling the completion menu re-triggers full command-line reparsing every step
- Symptom: Holding down next/prev-menu-item keys causes visible slowdown because each step re-parses/re-highlights the entire (unchanged) command line.
- Root cause: the menu next/prev event path reruns highlighting, which in turn triggers full module/command parsing, even though the actual buffer text hasn't changed.
- How resolved: open; maintainers noted async menu handling would be the real fix.
- Issues: https://github.com/nushell/reedline/issues/885

### Prefix history search broken specifically in Vi mode
- Symptom: Pressing `k` in Vi normal mode to prefix-search backward through history doesn't work.
- Root cause: tied to the same Vi cursor-position-semantics gap described above (normal vs insert mode cursor rules) — a targeted fix was reverted because a proper fix required the more holistic mode-aware cursor rework.
- How resolved: reverted pending the larger fix (tracked separately).
- Issues: https://github.com/nushell/reedline/issues/772

### Rapid ExternalPrinter messages cause full-screen wipes
- Symptom: An application using reedline's `ExternalPrinter` to print asynchronous messages (e.g. from a background thread) sees the whole screen wiped/flash when messages arrive close together.
- Root cause: each external-printer message triggers a full repaint rather than an incremental one.
- How resolved: fixed via a follow-up PR restructuring how external-printer output is merged into the repaint.
- Issues: https://github.com/nushell/reedline/issues/1005

### Prompt position corrupted after an external overlay (fzf-style) menu closes
- Symptom: After using an `fzf`-style external menu that temporarily takes over the screen, when there's little room left and the fzf content has pushed everything upward, the next prompt is positioned incorrectly.
- Root cause: the prompt's remembered "start row" isn't re-derived after an external program that repaints the screen returns control.
- How resolved: fix proposed (override the prompt start row on such return events).
- Issues: https://github.com/nushell/reedline/issues/1130

## Key parsing

### Custom keybinding on a plain character key silently does nothing
- Symptom: Binding a `ReedlineEvent` to a plain `KeyCode::Char('c')` (no modifier) has no effect — typing the letter does nothing, across multiple keys and keyboard layouts tried.
- Root cause: plain-character keybindings weren't being matched/dispatched the same way modifier-based bindings were.
- How resolved: reporter tracked the issue forward to a follow-up issue rather than resolving in-thread.
- Issues: https://github.com/nushell/reedline/issues/546

### shift+backspace does nothing under the kitty keyboard protocol
- Symptom: With `use_kitty_protocol = true`, pressing Shift+Backspace has no effect, unlike default readline/zsh-vi-mode/Vim behavior where it deletes a character.
- Root cause: the kitty-protocol key-event decoding path doesn't map this specific shifted key combination to any bound edit command.
- How resolved: open; maintainers requested more repro detail (OS/terminal/version/multiplexer) from multiple reporters, suggesting terminal-dependent behavior.
- Issues: https://github.com/nushell/reedline/issues/975

### F13–F20 keybindings don't work
- Symptom: Keybindings for function keys F13 through F20 (generated via key-remapping tools like Karabiner/AutoHotKey) don't trigger in nushell, while F1–F12 work fine and the same F13-F20 presses work in other apps (e.g. Neovim).
- Root cause: the key-event parsing table only recognizes F1–F12.
- How resolved: open; maintainer agreed extending support was reasonable.
- Issues: https://github.com/nushell/reedline/issues/1076

### Tab key with a modifier doesn't trigger its keybinding
- Symptom: A keybinding on Tab plus a non-default modifier doesn't fire, even though the same modifier works fine when bound to a different key; identified as requiring the kitty keyboard protocol to disambiguate (plain-terminal Tab-with-modifier sequences are ambiguous/unsupported without it).
- Root cause: without an enhanced keyboard protocol, the terminal cannot distinguish modified-Tab from plain Tab in the byte stream it sends.
- How resolved: clarified as a terminal-protocol limitation rather than a pure reedline bug — works with kitty-protocol-capable terminals.
- Issues: https://github.com/nushell/reedline/issues/763

### Kitty-protocol capability probe runs unconditionally, even when the feature is disabled
- Symptom: A behavior/latency change was observed at `Reedline::create()` time because the kitty-keyboard-protocol detection query now always runs eagerly, even when the caller explicitly disabled the protocol via the builder API.
- Root cause: a refactor that streamlined terminal-capability detection removed the conditional gate that used to skip the probe when the protocol was disabled.
- How resolved: fixed via a follow-up PR restoring the conditional behavior.
- Issues: https://github.com/nushell/reedline/issues/987

## Multiplexers and remote/Windows

### Completion-menu activity pollutes the tmux scrollback buffer
- Symptom: Inside tmux, merely opening/navigating tab-completion (even without submitting a command) repeatedly writes to and fills the tmux scrollback buffer with menu redraw artifacts.
- Root cause: several contributing factors identified by maintainers — a soft-keyboard-triggered resize that reedline still doesn't handle gracefully, leftover completion content not cleared after accept/reject, and the completion menu using more vertical space than necessary.
- How resolved: open, broken down into multiple sub-issues to track.
- Issues: https://github.com/nushell/reedline/issues/397

### Prompt duplicated repeatedly in tmux copy-mode/history
- Symptom: Typing at the top of a tmux window and then entering tmux's copy mode shows the prompt duplicated many times in the scrollback.
- Root cause: not conclusively identified; suspected regression, possibly overlapping with another related but not identical report.
- How resolved: open.
- Issues: https://github.com/nushell/reedline/issues/1062

### SSH into a Windows host: every typed character starts a new line
- Symptom: SSHing into a Windows machine and running nushell there causes every single character typed to appear on its own new line, with the "intermediate" lines removed once Enter is pressed.
- Root cause: OSC 133 shell-integration markers combined with how the remote PTY/terminal (Windows-side) processes them over an SSH-forwarded terminal.
- How resolved: workaround — disable `$env.config.shell_integration.osc133`; not a true fix of the underlying interaction.
- Issues: https://github.com/nushell/nushell/issues/10671

### SSH session: previous prompts get overwritten with the default prompt
- Symptom: Over SSH with an `oh-my-posh` custom prompt, completing a command causes the previous (already-drawn) prompt line to be replaced with the plain default `nu>` prompt instead of staying as it was.
- Root cause: not conclusively nushell/reedline-side — ultimately traced by the reporter to an `oh-my-posh` version issue.
- How resolved: resolved by upgrading `oh-my-posh`.
- Issues: https://github.com/nushell/nushell/issues/12180

### SSH via Windows Terminal: multi-line output silently truncated
- Symptom: After certain SSH sessions (success or failure) through Windows Terminal, subsequent multi-line string output gets truncated after the first line for the rest of the session.
- Root cause: traced to interaction with OpenSSH's terminal state manipulation rather than a nushell bug.
- How resolved: closed as not actionable on the nushell side; attributed to OpenSSH.
- Issues: https://github.com/nushell/nushell/issues/13214

### wezterm: prompt line replicated on every keystroke
- Symptom: In WezTerm, every keystroke causes the prompt line to be duplicated, quickly filling the screen with repeated prompt lines.
- Root cause: interaction between OSC 133 shell-integration and WezTerm's own prompt-redraw assumptions (cross-referenced with a WezTerm-side issue).
- How resolved: workaround — disable OSC 133 shell integration specifically when `$env.TERM_PROGRAM == "WezTerm"`; no unconditional fix.
- Issues: https://github.com/nushell/nushell/issues/5585

### Prompt-computing command re-runs on every keystroke, unusable latency over SSH
- Symptom: A custom prompt command (e.g. starship) is invoked on every single keystroke rather than only when the prompt needs to actually redraw, making input laggy to the point of being unusable over a slow SSH connection — while fish/bash/zsh remain fine on the same link.
- Root cause: acknowledged design gap — prompt regeneration isn't decoupled from per-keystroke repaint.
- How resolved: open, long-standing, explicitly described by maintainers as unfixed and wanted.
- Issues: https://github.com/nushell/nushell/issues/11837

## Other

### Cursor/output state not preserved after a host-command keybinding runs
- Symptom: A keybinding configured to run a shell command via `executehostcommand` used to leave the prompt/cursor exactly where it was; in a later version, running such a keybinding instead clears the output and resets the prompt position.
- Root cause: behavior regression between reedline versions in how state is restored after yielding control to a host command.
- How resolved: reporter identified the fix location and offered a PR; tracked for the reedline repo.
- Issues: https://github.com/nushell/reedline/issues/771

### Returning from a host command or the buffer editor spuriously redraws/duplicates the prompt
- Symptom: After a keybinding-invoked `executehostcommand` completes, or after exiting the `Ctrl+O` external buffer editor, a brand-new prompt is rendered even though nothing conceptually changed; when the prompt was on the bottom screen row, this specifically leaves a duplicate — the old prompt stays visible and a new one is drawn one row below.
- Root cause: the "return to line editor" path didn't reuse the previously-recorded prompt start position, instead treating it like a fresh prompt draw; regressed between two specific reedline versions.
- How resolved: the general case fixed via a linked PR (confirmed by a reporter on `main`); an earlier, narrower report of the same family remained open with maintainer commentary but no fix confirmed in this search.
- Issues: https://github.com/nushell/reedline/issues/755, https://github.com/nushell/reedline/issues/1196

### Ctrl+C while a completion menu is open breaks subsequent prompt spacing
- Symptom: If Ctrl+C is used to cancel an active completion menu, a configured `pre_prompt` hook that normally inserts blank-line spacing before the next prompt fails to do so — spacing only breaks in this specific menu-cancel path, not for ordinary Ctrl+C.
- Root cause: the "preserved completion row" left on screen after a menu-cancel isn't accounted for when the pre_prompt hook's output is composed with the next prompt draw.
- How resolved: fix agreed upon, planned to land after the next release.
- Issues: https://github.com/nushell/reedline/issues/1143

### Buffer editor round-trip renders duplicate lines
- Symptom: Opening the external buffer editor (`Ctrl+O`), entering multi-line text, exiting, and submitting produces duplicated lines on screen.
- Root cause: not detailed beyond repro; buffer-editor-to-line-editor handoff repaint issue.
- How resolved: fixed.
- Issues: https://github.com/nushell/reedline/issues/824
