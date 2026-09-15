# Elvish line-editor failure modes (pkg/edit, pkg/cli)

Compiled from `gh issue list --repo elves/elvish` searches across prompt/cursor/wrap/unicode/emoji/width/wcwidth/resize/paste/redraw/flicker/garbled/overwrite/scroll/CJK/rprompt/completion/navigation/history/tmux/windows/conpty/escape/key/terminal, followed by `gh issue view`/`gh api` on ~90 candidates. Scope: interactive display/input bugs only; pure language/packaging issues excluded.

---

## Width measurement

### Ambiguous-width emoji/symbols measured too narrow
- Symptom: An icon/emoji that occupies 2 display columns is treated as 1 column wide, clipping the last character of a (right) prompt or misplacing the cursor.
- Root cause: Elvish's `wcwidth` table did not correctly classify certain ranges (e.g. U+231B hourglass) as double-width; East-Asian/emoji width rules are inconsistent across terminals and not simply "add a Unicode range."
- How elvish resolved it: #271 fixed at the time; #1705 was still open, with maintainers noting the emoji-width problem is not simply solvable (variation selectors like U+FE0F change width of the same codepoint).
- Issues: https://github.com/elves/elvish/issues/271, https://github.com/elves/elvish/issues/1705

### No way to override wcwidth for platform-specific mismatches
- Symptom: A given codepoint's rendered width differs by terminal/platform, but Elvish hardcodes a single width per codepoint.
- Root cause: `util.Wcwidth`/`pkg/wcwidth` was a fixed lookup table with no runtime customization hook.
- How elvish resolved it: Closed by adding a way to patch/override widths (issue asked for `override-wcwidth codepoint width`).
- Issues: https://github.com/elves/elvish/issues/280

### CJK wide characters vanish after a delta (partial) redraw
- Symptom: Chinese characters typed at the prompt sometimes fail to render at all until a forced redraw (e.g. binding Ctrl-R to `redraw`).
- Root cause: The line editor's delta-redraw algorithm (only repainting the changed region) mis-tracked width changes introduced by double-width CJK glyphs.
- How elvish resolved it: Confirmed as a delta-algorithm bug; workaround was forcing a full redraw; discussed via internal writer/`Logger` debugging.
- Issues: https://github.com/elves/elvish/issues/117

### Prompt glyphs silently filtered as "unprintable"
- Symptom: Nerd Font / FontAwesome icons placed in a custom prompt function simply do not appear, even though the terminal can render them.
- Root cause: The line editor's prompt renderer stripped codepoints it judged "unprintable" (an over-broad heuristic), rather than trusting the terminal.
- How elvish resolved it: Left largely open/by-design at the time; superseded later by explicit ANSI/styled-prompt support (see Prompt content section).
- Issues: https://github.com/elves/elvish/issues/270

### Non-UTF-8 bytes render as garbled/mojibake and break completion round-trips
- Symptom: With a non-UTF-8 locale (e.g. GBK), Elvish's own "line lacks EOL" marker (`⏎`) itself renders as garbage; separately, filenames containing invalid UTF-8 byte sequences (from non-UTF-8 archives/uploads) display as mojibake and cannot be passed back to commands like `ls` via tab completion without a "No such file" error.
- Root cause: Elvish assumes a Unicode/UTF-8 locale throughout; there is no legacy-charset support, and raw non-UTF-8 bytes are treated as literal bytes rather than reconstructable filename data.
- How elvish resolved it: Declared out of scope ("Elvish will never support legacy, non-Unicode charsets"); maybe warn if locale isn't UTF-8. Filename issue stayed open/unresolved as a fundamental byte-vs-string tension.
- Issues: https://github.com/elves/elvish/issues/1184, https://github.com/elves/elvish/issues/1528

---

## Grapheme/cursor editing

### Cursor-left crash when scroll buffer is active (max-height exceeded)
- Symptom: With `edit:max-height` set and the command longer than that many lines, pressing Ctrl-A (move to line start) or moving back a word crashes the shell.
- Root cause: Cursor-position math assumed the full buffer was on-screen; didn't account for the scrolled/cropped view when `max-height` truncates the visible lines.
- How elvish resolved it: Fixed after reproduction; maintainer noted the "very small terminal" case had never been evidence-tested before.
- Issues: https://github.com/elves/elvish/issues/578

### Cursor-move functions crash when invoked concurrently from a background job
- Symptom: Calling `edit:move-dot-left` (or similar) from a backgrounded closure while the user is also typing crashes the editor.
- Root cause: The editor is not concurrency-safe; a background call mutating cursor/abbreviation-expansion state (`literalInserts`) races with the interactive insert path.
- How elvish resolved it: Root-caused to abbreviation-expansion state tracking; fixed for that path (editor overall still not documented as concurrency-safe).
- Issues: https://github.com/elves/elvish/issues/701

### No API to get/set the cursor by absolute position
- Symptom: After programmatically rewriting `edit:current-command`, the cursor always jumps to the end; there was no way to restore a prior cursor offset.
- Root cause: Cursor position was only exposed via relative-move builtins, not a settable variable/absolute index.
- How elvish resolved it: Resolved via the `$edit:-dot` read/write variable.
- Issues: https://github.com/elves/elvish/issues/415, https://github.com/elves/elvish/issues/534

### Ctrl-A / Ctrl-E not bound to line-start/line-end by default
- Symptom: Users expect readline-style Ctrl-A/Ctrl-E to move to line start/end; by default these are unbound (or bound to something else) in Elvish's native keymap.
- Root cause: Elvish's default (non-readline) keymap doesn't follow GNU readline conventions; enabling `readline-binding` is required, but that module also overrides other keys (e.g. Ctrl-L) users may not want touched.
- How elvish resolved it: Open — documented workaround is `use readline-binding` then re-binding unwanted overlaps individually.
- Issues: https://github.com/elves/elvish/issues/1366

### Delete/backspace key events get remapped by some terminal clients, breaking word-delete in listing modes
- Symptom: In one SSH client (xshell), physical Delete is delivered as `Ctrl-H`; Elvish's internal listing widgets (e.g. history-search box) then can't delete words because that combination isn't wired the same as literal DEL there.
- Root cause: Terminal-specific key encoding differences leak into mode-specific editing widgets that assume one canonical delete encoding.
- How elvish resolved it: Reported, root cause identified (event translated to an unexpected `Key{0x48, Ctrl}`); no universal fix, workaround is manual rebinding.
- Issues: https://github.com/elves/elvish/issues/1178

---

## Wrapping and geometry

### No handling of terminal rewrap when the width shrinks mid-session
- Symptom: If the terminal is narrowed while a multi-line command is present, Elvish's internal line-wrap bookkeeping doesn't reflow the already-displayed buffer to match, leaving a stale layout.
- Root cause: Elvish computes its own soft-wrap and tracks screen line count outside the terminal's own reflow, so it has no way to "know" how the terminal itself would rewrap already-emitted text.
- How elvish resolved it: Closed as "no easy way to deal with this" (won't fix / unresolved architecturally).
- Issues: https://github.com/elves/elvish/issues/108

### Argument-completion menu becomes cluttered at specific small window heights
- Symptom: A custom `arg-completer` for a command renders a garbled/cluttered completion listing only at certain terminal heights (e.g. 25–26 lines at 80 columns); larger or smaller windows are fine.
- Root cause: Unclear — reporter narrowed it to interaction between prompt height, right-prompt presence, and where the listbox layout gets "cut"; never fully root-caused.
- How elvish resolved it: Open, unresolved.
- Issues: https://github.com/elves/elvish/issues/1282

### Right prompt with embedded newlines disappears too eagerly as the command line grows
- Symptom: A multi-line right prompt (rprompt) that starts with `\n` gets hidden once the typed command is "long enough," even though the newline already puts it out of the way and there's no real overlap.
- Root cause: The visibility decision uses the rprompt's *total* string length instead of only the length up to its first newline.
- How elvish resolved it: Left open; simple fix proposed (measure only up to first `\n`) but not merged as of report.
- Issues: https://github.com/elves/elvish/issues/1512

### Multi-line command continuation indentation can't be disabled, breaking copy-paste
- Symptom: Elvish auto-indents wrapped/continued lines of a multi-line command to align under the prompt; this is visually nice but inserts leading whitespace that corrupts copy-pasting the command elsewhere.
- Root cause: The editor manages its own line breaks/indentation instead of relying on the terminal's native soft-wrap, and (at time of report) offered no toggle.
- How elvish resolved it: Open; maintainer noted a toggle is "relatively easy" but is entangled with Elvish managing line breaks itself rather than the terminal doing so, which is being revisited as part of a TUI rewrite.
- Issues: https://github.com/elves/elvish/issues/1834

---

## Prompt content

### Prompt/rprompt were not independently customizable, including a "persistent" right prompt
- Symptom: Users could not set a custom right prompt (e.g. a clock, exit status) or have it stay visible after command submission.
- Root cause: Early Elvish only exposed a single customizable prompt function, with no rprompt/persistence concept.
- How elvish resolved it: Resolved by adding `edit:prompt`/`edit:rprompt` customization and later a persistent-rprompt behavior.
- Issues: https://github.com/elves/elvish/issues/103, https://github.com/elves/elvish/issues/198

### Raw ANSI color escapes typed into a prompt function are stripped instead of interpreted
- Symptom: A prompt function that emits raw `tput`/ANSI SGR sequences shows the literal escape text (e.g. `[35m`) instead of color, because the renderer filters "unprintable" control bytes out of prompt text.
- Root cause: The prompt renderer treats prompt output as plain text and strips control sequences to keep its own layout math correct, with no sanctioned way to inject real styling.
- How elvish resolved it: Resolved by adding the `styled`/`edit:styled` builtin and later native parsing of ANSI SGR sequences in prompt output.
- Issues: https://github.com/elves/elvish/issues/199, https://github.com/elves/elvish/issues/814

### Regression: styled prompt segments start rendering as literal escape codes
- Symptom: A themed prompt using `styled` segments, which worked for months, suddenly showed raw color codes in the terminal instead of applying color, after an unrelated set of commits.
- Root cause: Unknown/unspecified — a change in 3 commits broke SGR-code emission for `styled` prompt output.
- How elvish resolved it: Fixed (exact commit not detailed in the issue thread).
- Issues: https://github.com/elves/elvish/issues/724

### CSI sequences other than plain color codes are silently discarded from prompts
- Symptom: Prompt-generator tools (e.g. Oh My Posh) rely on CSI sequences like "disable bold" (`\x1b[21m`); Elvish's SGR parser (`ui.ParseSGREscapedText`/`StylingFromSGR`) drops sequences it doesn't recognize, producing incomplete/incorrectly styled prompts.
- Root cause: Elvish converts prompt output into internal `Text`/styling objects via a limited SGR parser instead of passing unrecognized sequences through verbatim.
- How elvish resolved it: Open as of report.
- Issues: https://github.com/elves/elvish/issues/1836

### OSC / semantic-prompt escape sequences get mangled or are unsupported
- Symptom: A prompt function emitting an OSC sequence (e.g. custom terminal-integration markers, or OSC 133 semantic-prompt markers used by Kitty/WezTerm/Konsole) has the sequence corrupted in the rendered output (extra bytes/garbled `^[]…^G` sequences) rather than passed through, and there is no built-in support for emitting OSC 133 markers at all.
- Root cause: Same underlying prompt-text sanitization/parsing pipeline as the CSI issue above does not treat OSC sequences as pass-through either.
- How elvish resolved it: Both open as of report; workaround suggested via `edit:before-readline`/`edit:after-prompt` hooks, which are imperfect for multi-point OSC 133 markers.
- Issues: https://github.com/elves/elvish/issues/1855, https://github.com/elves/elvish/issues/1808

### External full-screen program launched from a keybinding leaves the cursor/prompt in a flickering state
- Symptom: Running `fzf` (or similar) from a custom keybinding, then doing further work (e.g. `sleep`) before returning to Elvish, causes the cursor to visibly drop/jump instead of staying put as it would in Bash.
- Root cause: By design, Elvish does not try to preserve cursor/prompt position across an external program's own screen writes; maintainers argued this is not fully controllable since the external program (not Elvish) already wrote a trailing newline.
- How elvish resolved it: Closed as working-as-intended / not fixable in general; reporter still felt it looked like a bug.
- Issues: https://github.com/elves/elvish/issues/1814

### Stale-prompt ("this prompt is slow to compute") marker flickers and isn't customizable
- Symptom: When a prompt function takes a noticeable time to run (e.g. it does a git-status lookup), a "stale" indicator flickers on/off distractingly, and there was no way to customize its appearance.
- Root cause: The stale marker was hardcoded rather than exposed as a customizable/styled value or transform hook.
- How elvish resolved it: Discussed a `$edit:-stale-prompt-transform` function-based approach as a more flexible design than a simple marker string; the issue was closed but the flicker-vs-customization tension is noted as informing later prompt redesign work.
- Issues: https://github.com/elves/elvish/issues/513

### Prompt trailing-newline handling differs from other shells, misplacing the cursor
- Symptom: A Python-based prompt (`pure`) ending in a newline results in Elvish inserting an extra blank line not seen in other shells, and after working around the newline, the cursor lands at the start of the line instead of after the prompt.
- Root cause: Elvish does not strip a trailing newline from prompt output the way other shells do (their `print` helper adds a newline unless told not to), and cursor placement logic didn't fully account for that.
- How elvish resolved it: Workaround documented (`print` with `end` argument to suppress trailing newline); underlying cursor-placement quirk left partly unresolved in the thread.
- Issues: https://github.com/elves/elvish/issues/805

### Prompt callback that mixes byte-stream and value-stream output has undefined ordering, and non-string values corrupt it
- Symptom: A prompt function using `put $n` (numeric) interleaved with `print` (string, byte stream) can render the parts in the wrong order or fail, since numbers weren't automatically stringified for prompt rendering.
- Root cause: Two independent output channels (byte stream vs. value stream) are merged for prompt rendering with no defined ordering guarantee, and only `string`-typed values were rendered; other types (e.g. `float64`) needed special-casing.
- How elvish resolved it: Resolved by special-casing `float64` conversion in prompt rendering (rather than a general implicit-stringify rule, judged too fragile).
- Issues: https://github.com/elves/elvish/issues/1186

---

## Resize

### SIGWINCH is not forwarded to a child process after `exec`
- Symptom: Running `tmux` (or a similar full-screen program) via `exec` from Elvish, then resizing the outer terminal, does not deliver SIGWINCH to the execed program, so it never learns about the new size.
- Root cause: The execed process ends up outside the foreground process group (not the controlling process), so terminal signals including SIGWINCH aren't routed to it.
- How elvish resolved it: Diagnosed as a process-group/`exec` foreground-group issue; tracked further in a related issue (#288); not confirmed fully fixed in this thread.
- Issues: https://github.com/elves/elvish/issues/287

### Completion listbox layout panics (integer divide-by-zero) on extreme resize
- Symptom: Resizing the terminal to an unreasonably small size (e.g. 7 columns x 3 rows) and then invoking tab-completion crashes Elvish with a divide-by-zero panic in the listbox layout code.
- Root cause: `getHorizontalWindow`/`listbox_window.go` computes a column layout by dividing by a window dimension that can be zero at extreme sizes, with no lower-bound guard.
- How elvish resolved it: Acknowledged as needing a graceful "refuse to render, don't crash" fallback (e.g. silently skip creating the list box, possibly log); exact final behavior not confirmed merged in the thread shown.
- Issues: https://github.com/elves/elvish/issues/1820

---

## Terminal queries and input races

### Terminal-specific function-key sequences rejected with hard errors instead of ignored
- Symptom: Pressing Home/End (and Ctrl-Left/Ctrl-Right) in `rxvt-unicode` prints visible error text at the prompt: `error when reading terminal: bad CSI: "\x1b[7~"` / `bad G3: "\x1bOd"`.
- Root cause: The terminal-input parser only recognized a fixed set of CSI/G3 sequences and surfaced unknown-but-valid sequences from less-common terminals as hard parse errors rather than silently ignoring/logging them.
- How elvish resolved it: Most urxvt-specific sequences were subsequently added to the recognized set.
- Issues: https://github.com/elves/elvish/issues/579

### Stray/unexpected ESC bytes crash the shell instead of being handled gracefully
- Symptom: Typing (or pasting) a bare ESC byte followed by Tab (triggering completion) crashes Elvish with `unexpected rune '\x1b'`.
- Root cause: The completion code path (`completeVariable`) did not defensively handle an unexpected/control rune reaching it, causing a panic instead of a no-op or error.
- How elvish resolved it: Fixed as part of the broader "completion panics on unparseable input" cluster (see Completions/modes redraw).
- Issues: https://github.com/elves/elvish/issues/1511

### Escape-sequence parsing regressed after an async-reader rewrite meant to fix a deadlock
- Symptom: Alt-key and arrow-key escape sequences (e.g. Alt-A = `\ea`, Left = `\e[D`) stopped being recognized and were instead treated as a bare Escape keypress.
- Root cause: A fix for a deadlock in the async terminal reader changed how much of the input stream was consumed per read, breaking multi-byte escape-sequence reassembly.
- How elvish resolved it: Identified as caused by the deadlock-fix commit; addressed as a follow-up fix.
- Issues: https://github.com/elves/elvish/issues/76

### Ctrl-C immediately followed by typing panics the editor
- Symptom: Type something, press Ctrl-C, then start typing again — Elvish panics with `slice bounds out of range`.
- Root cause: Internal state used for abbreviation-expansion bookkeeping (`literalInserts` vs. buffer length/dot) was not reset consistently by the Ctrl-C interrupt path, leaving stale/negative-length slices.
- How elvish resolved it: Root-caused to the commit that introduced the regression; fixed.
- Issues: https://github.com/elves/elvish/issues/598

### Non-blocking writes to the terminal fd fail with EWOULDBLOCK on BSDs
- Symptom: On FreeBSD/macOS, large writes to the terminal (heaviest in navigation mode, which does a lot of screen writes) intermittently abort the editor with "resource temporarily unavailable."
- Root cause: BSD terminal driver write buffers are smaller than Linux's; nonblocking writes can legitimately return EWOULDBLOCK under load, and the editor didn't retry/backoff on that condition.
- How elvish resolved it: Root cause diagnosed (platform buffer-size difference); fix approach not detailed in the fetched excerpt.
- Issues: https://github.com/elves/elvish/issues/140

### External command that dies abnormally leaves the TTY in a broken mode for the returning shell
- Symptom: If a full-screen external program (e.g. an editor) modifies terminal modes (raw mode, alternate screen, mouse reporting) and then dies without restoring them, the interactive Elvish session that regains control can be partially or fully unusable.
- Root cause: Terminal-mode restoration was the external program's responsibility; Elvish did not defensively re-sanitize/reset terminal state itself when regaining control after a command exits abnormally.
- How elvish resolved it: Open as of report; noted as related to Windows console setup issues as well (see Multiplexers/Windows section).
- Issues: https://github.com/elves/elvish/issues/1182

### Pasted text is "typed" character-by-character with live syntax validation instead of inserted instantly
- Symptom: Pasting a long command visibly "animates" letter-by-letter (with syntax-highlight recomputation per character) instead of appearing instantly, and a paste can't be canceled mid-flight.
- Root cause: In terminals/multiplexers without working bracketed-paste (e.g. `screen`), Elvish cannot distinguish a paste from fast typing, so it re-validates/highlights on every character; on older/slower machines this becomes visibly slow.
- How elvish resolved it: Largely a terminal/bracketed-paste-support prerequisite issue rather than an Elvish bug per se — maintainers could not reproduce on modern bracketed-paste-capable terminals; unresolved for non-bracketed-paste environments.
- Issues: https://github.com/elves/elvish/issues/1744

---

## Paste

### Newlines are stripped/reordered from multi-line pasted text
- Symptom: Pasting two lines (`echo 1\necho 2`) results in `echo 1echo 2` on a single command line, and an extra spurious prompt line appears in scrollback.
- Root cause: The paste-handling path did not preserve embedded newlines correctly, effectively moving/dropping them.
- How elvish resolved it: Fixed (found while diagnosing the paste-lockup issue below).
- Issues: https://github.com/elves/elvish/issues/890

### Editor completely locks up on large multi-line pastes
- Symptom: Pasting many lines (e.g. 50 lines of `#`) has a high chance of freezing Elvish entirely; this was a regression from a prior release.
- Root cause: The input-event channel has a bounded capacity (128); large bursts of paste-generated events block the writer, and the terminal reader goroutine had no defined way to know when a paste had ended, so it could stall indefinitely.
- How elvish resolved it: Fixed by reworking the terminal reader's paste-boundary/backpressure handling.
- Issues: https://github.com/elves/elvish/issues/887

### Bracketed paste is not recognized in some Windows terminal emulators
- Symptom: In ConEmu, pasted text arrives wrapped in raw `\e[200~` / `\e[201~` bracketed-paste markers that Elvish does not strip, so the literal marker bytes are inserted into the command line.
- Root cause: Elvish's Windows build uses an entirely different, more limited input reader that doesn't parse general VT sequences (including bracketed paste) at all, unlike the Unix VT-sequence-based reader.
- How elvish resolved it: Open — maintainers plan to unify the Windows reader with the VT-sequence-based Unix reader, but bracketed paste support was not grafted onto the old Windows reader.
- Issues: https://github.com/elves/elvish/issues/1761

---

## Completions/modes redraw

### Completion menu renders with spurious blank lines
- Symptom: The tab-completion popup shows unexpected blank lines interspersed with candidates.
- Root cause: Unspecified — a rendering bug in the completion listbox layout.
- How elvish resolved it: Fixed (no detailed root cause recorded).
- Issues: https://github.com/elves/elvish/issues/106

### Navigation-mode directory listing shows the wrong contents
- Symptom: The file/directory browser in navigation mode renders a listing that doesn't match the actual directory selected.
- Root cause: Unspecified rendering/state-sync bug.
- How elvish resolved it: Fixed.
- Issues: https://github.com/elves/elvish/issues/111

### Leftover colored background blocks bleed into navigation-mode output
- Symptom: Stray colored rectangles appear in the navigation-mode file browser (first suspected as a Termux terminal bug).
- Root cause: Elvish erased a screen region without first resetting SGR attributes (`\e[0m`), so background color from previous content bled into the cleared area.
- How elvish resolved it: Fixed by emitting an SGR reset before erasing.
- Issues: https://github.com/elves/elvish/issues/120

### Multi-line history entries corrupt the history-listing mode's layout
- Symptom: Browsing command history in history-listing mode renders incorrectly when one of the history entries itself spans multiple lines.
- Root cause: The listing renderer assumed one history entry = one display line.
- How elvish resolved it: Fixed.
- Issues: https://github.com/elves/elvish/issues/160

### Listbox widgets lacked scrollbar, horizontal-scroll, and page-up/down affordances
- Symptom: Long completion/navigation candidate lists gave no visual indication of more content, no way to scroll a horizontally-paginated list, and no Page-Up/Page-Down support.
- Root cause: Early listbox widget design only supported simple up/down navigation with no scroll-position indicator or horizontal paging model.
- How elvish resolved it: Partially addressed by rendering three coordinated `Listing`s with a scrollbar (using half-height Unicode block characters for 2x vertical resolution); PageUp/PageDown for horizontally-scrolling lists was explicitly declined as not making sense for that layout.
- Issues: https://github.com/elves/elvish/issues/164, https://github.com/elves/elvish/issues/165, https://github.com/elves/elvish/issues/183, https://github.com/elves/elvish/issues/191

### Preview pane in navigation mode can't scroll for files longer than the pane
- Symptom: Previewing a text file in navigation mode that's taller than the preview pane gives no way to scroll and see the rest.
- Root cause: No scroll support was implemented for the preview widget.
- How elvish resolved it: Left open ("there is no way to scroll yet") at time of report.
- Issues: https://github.com/elves/elvish/issues/381

### Completion panics when the command line has unparseable syntax at the cursor
- Symptom: Typing a lone backtick or other syntactically-incomplete token, then pressing Tab, crashes Elvish (`unexpected rune`) instead of just showing no/partial completions.
- Root cause: `completeVariable`/`Complete` in the completion package didn't defensively handle a parse state containing an unexpected token/rune, panicking instead.
- How elvish resolved it: Fixed after being reported multiple times (three duplicate issues).
- Issues: https://github.com/elves/elvish/issues/1511, https://github.com/elves/elvish/issues/1530, https://github.com/elves/elvish/issues/1557

### Listbox rendering panics on completion candidates with empty content
- Symptom: A custom `arg-completer` that returns a candidate with an empty `Value`/`Display` crashes Elvish during listbox rendering.
- Root cause: `croppedLines.Render`/listbox rendering code didn't handle a zero-length candidate string.
- How elvish resolved it: Root-caused (empty content case not handled); acknowledged as a bug to fix, "shouldn't cause a crash."
- Issues: https://github.com/elves/elvish/issues/1668

### `edit:complete-filename` panics when called with no arguments
- Symptom: Invoking the builtin filename-completion generator directly with zero arguments crashes the shell.
- Root cause: `GenerateFileNames` indexed into its argument slice without checking it was non-empty.
- How elvish resolved it: Acknowledged; fix implied by thank-you response (no further detail captured).
- Issues: https://github.com/elves/elvish/issues/1799

### Completion menu doesn't auto-dismiss when only one candidate exists
- Symptom: Tab-completing a path with a single matching completion both inserts the completion text and leaves a one-item "COMPLETING argument" menu open, requiring an extra Enter to dismiss it.
- Root cause: The completion-menu-display logic doesn't special-case "exactly one candidate, already applied" to skip showing a menu at all.
- How elvish resolved it: Open as of report; related to a similarly-themed issue (#474) about single-completion ergonomics.
- Issues: https://github.com/elves/elvish/issues/1826

---

## Key parsing

### Backspace unbound by default because terminals send Ctrl-H instead of DEL
- Symptom: Pressing Backspace at the prompt (in some terminal setups, e.g. a plain `TTY` session or `xterm`) produces `Unbound: Ctrl-H` instead of deleting a character.
- Root cause: Elvish's default keymap only bound the ASCII `DEL` (0x7F, `^?`) byte to backspace, not `Ctrl-H` (0x08), while some terminals/configs emit the latter for the Backspace key.
- How elvish resolved it: Fixed by adding `Ctrl-H` to the default binding alongside DEL.
- Issues: https://github.com/elves/elvish/issues/55, https://github.com/elves/elvish/issues/328

### Custom keybindings silently fail on case-mismatched symbolic key names
- Symptom: `set edit:insert:binding["Alt+Backspace"] = {...}` produces no error but also never fires; using `BackSpace` instead of `Backspace` throws `bad key`.
- Root cause: Key names became case-sensitive in a prior release, but this wasn't obvious/discoverable, and there was no tool to show what name a given physical keypress actually corresponds to.
- How elvish resolved it: Documented as intentional/case-sensitive by design; a `edit:key-reader`/keypress-introspection tool was proposed as the real fix for discoverability (tracked separately).
- Issues: https://github.com/elves/elvish/issues/1665

### Modifier-qualified special keys can't be distinguished due to legacy terminal encoding
- Symptom: Ctrl-J and Ctrl-M cannot be bound to different actions because they arrive with the same byte as Enter/Newline; similarly Ctrl-Shift-Backspace and Alt-Shift-Backspace bindings never fire even though the physical keys are pressed.
- Root cause: Legacy terminal encodings collapse multiple logical key+modifier combinations onto identical control bytes (`Tab`/`Ctrl-I` both send 0x09; `Ctrl-M`/Enter both send 0x0D; Backspace has no distinct modifier-qualified encoding in traditional terminal protocols).
- How elvish resolved it: Both closed as fundamental terminal-protocol limitations, not fixable without a newer keyboard protocol (see Kitty keyboard protocol request, still open).
- Issues: https://github.com/elves/elvish/issues/1244, https://github.com/elves/elvish/issues/1896

### Non-US/AltGr keyboard layouts misdecoded on Windows
- Symptom: On a German keyboard, AltGr+ß (used to type backslash) is parsed by Elvish as Ctrl-Alt-[ and reported unbound; on some layouts (German/Swiss, Brazilian ABNT2) plain letters are also misdecoded, e.g. always uppercased.
- Root cause: Windows console key-event decoding in Elvish's terminal reader does not correctly account for AltGr as a distinct modifier state or for non-US virtual-key mappings.
- How elvish resolved it: Reported as reproducible across multiple non-US layouts; not confirmed fixed in the thread shown.
- Issues: https://github.com/elves/elvish/issues/752

### NumLock state corrupts Windows console input, forcing uppercase
- Symptom: On Windows with NumLock enabled, all typed input is processed as if Shift were held (uppercased), only on certain Windows 10 builds.
- Root cause: A bug/quirk in how the Windows console input reader interpreted the NumLock modifier bit alongside other Windows Insider console changes; behavior differed across Windows 10 preview builds.
- How elvish resolved it: Diagnosed as Windows-console-specific; workaround was disabling NumLock; not a pure Elvish-side fix.
- Issues: https://github.com/elves/elvish/issues/567

### Alt+Arrow keybindings silently do nothing in some Windows terminal hosts
- Symptom: Custom `Alt-Left`/`Alt-Right` bindings work in some terminals but produce no effect at all in Windows Terminal Preview or the VS Code integrated terminal.
- Root cause: Different terminal emulators encode Alt+Arrow with different escape sequences; Elvish's recognized set didn't include the variant those hosts use.
- How elvish resolved it: Acknowledged as one of many terminal-emulation variants to support case-by-case; not confirmed fully resolved.
- Issues: https://github.com/elves/elvish/issues/912

### "End of history" indicator regressed twice across releases
- Symptom: Pressing Down at the end of command history should show an "End of history" message; across releases this silently regressed to no message, then later regressed again to show a confusing "Unbound key: Down" error instead.
- Root cause: Two separate unrelated commits, months apart, each broke the Down-key-at-end-of-history behavior without test coverage catching it.
- How elvish resolved it: Root-caused via bisection to specific commits; fixed.
- Issues: https://github.com/elves/elvish/issues/1738

---

## Multiplexers and remote/Windows

### tmux double-ESC forwarding breaks Alt/Ctrl-Arrow bindings
- Symptom: Inside tmux, Alt+Up/Down produce `Unbound: Ctrl-Alt-[` repeatedly instead of the bound action, and Ctrl-Left/Right are parsed as plain Left/Right.
- Root cause: tmux forwards Alt-modified keys as a doubled ESC sequence, which Elvish's key parser interprets as Ctrl-Alt-[ rather than recognizing the tmux-specific encoding.
- How elvish resolved it: Root-caused to tmux's escape-doubling behavior; no complete fix recorded in the thread.
- Issues: https://github.com/elves/elvish/issues/181

### tmux `default-shell` vs `default-command` breaks daemon/PATH setup
- Symptom: Setting Elvish as tmux's `default-shell` causes "cannot find elvish" / daemon-spawn failures and broken history at startup.
- Root cause: tmux's `default-shell` launches Elvish as a login shell with `argv[0]` set to `-elvish`, altering how it resolves its own binary path and environment (PATH) versus a normal invocation.
- How elvish resolved it: Workaround: use tmux's `default-command` instead of `default-shell`.
- Issues: https://github.com/elves/elvish/issues/787

### Working directory not propagated to tmux panes
- Symptom: tmux features that depend on `pane_current_path` (e.g. split-window inheriting cwd) don't work when the pane runs Elvish, even though `cd` was run.
- Root cause: Elvish does not automatically emit an OSC 7 "current directory" escape sequence, which is how modern terminals/tmux learn the shell's cwd; must be added manually in the prompt function.
- How elvish resolved it: Open — no auto OSC 7 support at report time; manual prompt-based workaround suggested but reporter couldn't get it fully working.
- Issues: https://github.com/elves/elvish/issues/1579

### Emacs shell-mode pseudo-tty reports a 0x0 window size, crashing the full-screen editor
- Symptom: Running Elvish inside Emacs's `shell`/`term`/`ansi-term` modes crashes with a nil-pointer panic; the underlying pseudo-tty always reports width=0, height=0 via the window-size ioctl.
- Root cause: Elvish's line editor is architected as a full-screen TUI that assumes valid terminal dimensions and cursor-control support; Emacs's shell-mode pty doesn't implement `TIOCGWINSZ` properly and has very limited terminal capability.
- How elvish resolved it: The specific nil-pointer crash was fixed; a proper line-oriented fallback mode for degraded terminals like this was requested but left open (the editor is inherently "screen-oriented" like Vim).
- Issues: https://github.com/elves/elvish/issues/40, https://github.com/elves/elvish/issues/41

### Raw ANSI/VT sequences leak into output on pre-Windows-10 consoles
- Symptom: On Windows versions before 10 (no native ANSI support), color/style output and exception messages show up as literal escape-code text (`^[[31;1m...^[[m`) instead of being rendered, and even some ordinary command output showed garbled control sequences.
- Root cause: Elvish relies on the terminal itself supporting ANSI/VT escape processing, which the legacy Windows console did not provide; needed to use the native Windows console API instead for those versions.
- How elvish resolved it: Fixed generally for supported Windows 10+ consoles; pre-10 support was explicitly dropped/declared out of scope once Windows 10 became the baseline.
- Issues: https://github.com/elves/elvish/issues/753, https://github.com/elves/elvish/issues/798

### External command run from the prompt function corrupts the terminal over SSH on Windows
- Symptom: On Windows+MSYS2 over an SSH connection specifically (not a native console), running an external command from inside the prompt function produces a garbled, unusable interactive session with visible raw escape sequences.
- Root cause: Determining whether stdout/stderr is attached to a real tty (vs. an SSH-forwarded pipe/pty) behaves differently on Windows, so ANSI-sequence emission/consumption gets out of sync specifically in that combination.
- How elvish resolved it: Diagnosed as tty-detection-over-SSH specific to Windows; not confirmed fully resolved in the thread.
- Issues: https://github.com/elves/elvish/issues/1165

### External program dying abnormally leaves Windows console/TTY modes inconsistent
- Symptom: After certain external commands exit (especially abnormally), subsequent output shows corrupted symbols ("Symbols Clobbered by Elvish?") instead of correct Unicode icons, only reproduced on Windows.
- Root cause: On Windows, TTY/console modes are not correctly restored after an external command exits, closely related to the general "external command death corrupts terminal state" issue (#1182); a fix was started but not finished.
- How elvish resolved it: Open/unresolved at report time; root cause identified by a maintainer with high confidence but no complete fix landed.
- Issues: https://github.com/elves/elvish/issues/1460 (cross-referenced with https://github.com/elves/elvish/issues/1182)

### Prompt-integration tool exec fails silently/confusingly on Windows path variants
- Symptom: `eval (starship init elvish)`-style prompt integration fails with `exec: file does not exist`, even though the binary is discoverable via `search-external`, when the PATH contains MSYS2-style `/c/Users/...` paths.
- Root cause: Path-format mismatch between MSYS2-style POSIX paths (as seen by Elvish's path resolution) and native Windows executable resolution used when actually exec'ing.
- How elvish resolved it: Not resolved in the thread shown; still investigating exact path-handling mismatch.
- Issues: https://github.com/elves/elvish/issues/1864

### Launching fzf via a keybinding disables terminal mouse text-selection afterward, Windows-only
- Symptom: After invoking `fzf` through a custom Elvish keybinding (Windows Terminal or WezTerm on Windows), the ability to select text with the mouse in the terminal is lost even after fzf exits; running fzf directly at the command line doesn't trigger it.
- Root cause: Unknown; suspected to involve terminal mouse-reporting mode not being fully restored after the keybinding's subprocess exits, specific to Windows terminal hosts.
- How elvish resolved it: Open, unresolved.
- Issues: https://github.com/elves/elvish/issues/1871

### Persistent command history silently doesn't work on Windows
- Symptom: The history database file exists at the expected path, but restarting Elvish always shows empty history.
- Root cause: Unresolved in the thread; deleting/recreating the DB file didn't help retain history going forward either.
- How elvish resolved it: Open, unresolved (data loss reported when working around it).
- Issues: https://github.com/elves/elvish/issues/1900

---

## Other

### Ctrl-L bound to an external `clear` command wipes the prompt without a real redraw
- Symptom: Binding Ctrl-L to run the external `clear` command clears the whole screen including both prompts, and the cursor is left in the wrong place; later, even the built-in `edit:redraw &full=$true` "fix" causes a visible cursor jump.
- Root cause: Elvish's delta-redraw logic assumes the terminal still looks the way its internal buffer thinks it does; when an external program (or a raw escape write) repaints the whole screen, that assumption is invalidated and the editor doesn't know to do a full instead of partial redraw.
- How elvish resolved it: Fixed generally by forcing a full redraw whenever a user *function* (vs. a built-in editor command) is invoked from a keybinding; residual Ctrl-L-specific and cursor-jump complaints persisted in follow-up reports.
- Issues: https://github.com/elves/elvish/issues/95, https://github.com/elves/elvish/issues/883, https://github.com/elves/elvish/issues/1238

### `edit:redraw` called directly from an interactive prompt crashes the shell
- Symptom: Running `edit:redraw` interactively (not from within a keybinding) segfaults with a nil-pointer dereference.
- Root cause: The redraw code path assumed it was always invoked with active editor-render state that isn't present when called this way.
- How elvish resolved it: Fixed.
- Issues: https://github.com/elves/elvish/issues/597

### History navigation crashes when the history daemon/store is unavailable
- Symptom: When the persistent-history daemon connection fails (e.g. over SSH without daemon support) and the user presses Up-arrow to recall history, Elvish crashes with a nil-pointer panic instead of degrading gracefully.
- Root cause: The history "Fuser"/walker assumed a non-nil store/daemon connection was always available, with no nil-check fallback path for "history unavailable."
- How elvish resolved it: Related code was later removed as part of eliminating the standalone daemon architecture; not clear a graceful-degradation fix landed for this exact code path before that.
- Issues: https://github.com/elves/elvish/issues/1216

### Data race between navigation-mode `chdir` and evaluator state
- Symptom: Go's race detector flagged a read/write data race involving `pkg/edit.TestNavigation_UsesEvalerChdir` and `Evaler.Chdir` during the test suite.
- Root cause: Navigation-mode directory-change handling and the evaluator's own chdir tracking access shared state without synchronization.
- How elvish resolved it: Reported from a test run; fix status not detailed in the fetched excerpt.
- Issues: https://github.com/elves/elvish/issues/1507

### macOS filename Unicode normalization looked like corruption but wasn't an Elvish bug
- Symptom: Filenames created by macOS screen capture (with accented characters, e.g. Finnish "ä"/"ö") appeared to double/garble when copied via Elvish to a remote Linux machine, differing from other shells.
- Root cause: macOS stores such filenames in NFD (decomposed) Unicode form; the other shell being compared against (zsh) happened to normalize to composed form on display, masking the same underlying bytes — a filesystem/locale normalization difference, not an Elvish string-handling bug.
- How elvish resolved it: Reporter self-diagnosed as "pilot error" (not an Elvish bug) after investigation; worth flagging as a normalization pitfall other shells should watch for when comparing to zsh/bash's more permissive display.
- Issues: https://github.com/elves/elvish/issues/663

---

**Total distinct failure modes: 67**
