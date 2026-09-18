# Line-editor failure modes from xonsh / prompt_toolkit issue history

Research method: `gh issue list --search ...` across `xonsh/xonsh` and
`prompt-toolkit/python-prompt-toolkit`, then `gh issue view --comments` on the
resulting candidates. Only issues actually retrieved are cited below.

---

## Width measurement

### wcwidth import/version breakage crashes startup
- Symptom: xonsh fails to even start ("wcwidth fails") when the installed `wcwidth` package version is incompatible with what prompt_toolkit expects.
- Root cause: prompt_toolkit depends on the third-party `wcwidth` package for width tables; a bad/old/broken install of that dependency is fatal rather than degraded.
- How resolved: treated as an environment/packaging problem; users told to pin/downgrade `wcwidth`; no in-app fallback added.
- Issues: https://github.com/xonsh/xonsh/issues/3607

### Wide/space unicode characters in the prompt miscount cursor column
- Symptom: Using a "two-space" unicode character in `$PROMPT` made xonsh place the caret at the wrong column.
- Root cause: `wcwidth`'s width table disagreed with what the actual terminal rendered for that codepoint.
- How resolved: acknowledged as an upstream `wcwidth`/prompt_toolkit table issue, not fixable in xonsh; documented as a known caveat.
- Issues: https://github.com/xonsh/xonsh/issues/1569

### Non-BMP characters (emoji, etc.) crash or silently fail on Windows input
- Symptom: Typing a non-BMP character (😀) via the Windows emoji picker throws an unhandled exception in the win32 input event loop on Windows Terminal, and silently does nothing in conhost.
- Root cause: Windows console API delivers surrogate-pair input as two separate `KEY_EVENT` records; prompt_toolkit's win32 input handling didn't reassemble them correctly.
- How resolved: fixed by PR #1482.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1477

### Lone surrogate / surrogate-pair strings crash history persistence
- Symptom: `FileHistory.store_string` raises `UnicodeEncodeError` when the buffer contains a lone surrogate (common after pasting emoji from Windows clipboard sources).
- Root cause: history file is written as UTF-8 text; lone/unpaired surrogates are not valid UTF-8 and the encode call isn't guarded.
- How resolved (or open): reported; fix status not confirmed in the fetched thread (no visible resolving comment).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/2061

### Decomposed/combining characters mis-measured and mis-edited
- Symptom: Combining-character sequences (e.g. `你̂好̄嗎̃`) render with wrong cell width, and the cursor can be positioned in the middle of a decomposed character; backspace could split it incorrectly.
- Root cause: renderer/cursor logic operated per-codepoint rather than per-grapheme-cluster, and initial fixes didn't account for double-width base characters combined with combining marks.
- How resolved (or open): partially fixed over multiple commits (zero-width rendering, then a double-width-aware fix); thread notes cursor movement across such sequences still requires multiple arrow presses (not truly atomic movement).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/274

### Renderer reserves a phantom extra column
- Symptom: The rightmost terminal column is never used by prompt_toolkit output, leaving a visibly unoccupied column on some terminals (Windows Terminal, conhost); a one-line renderer patch (`columns + 1`) "fixes" it for some users, but not universally.
- Root cause (as diagnosed): renderer unconditionally subtracts one from the reported terminal width, apparently as defensive margin against auto-wrap ambiguity, but this is unconditionally wrong on terminals that report width accurately.
- How resolved (or open): open; behavior is terminal-dependent so not clearly one bug.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1758

### Emoji/double-width rendering depends on a terminal-specific setting prompt_toolkit can't detect
- Symptom: `wcwidth('🐍')` returns 2 in prompt_toolkit, but some terminals (iTerm2 by default) render such emoji in a single cell, causing misalignment.
- Root cause: "ambiguous/emoji width" is configurable per terminal-emulator and not discoverable via any escape-sequence query; wcwidth's table is a best-effort default that some terminals don't match.
- How resolved (or open): open/unfixable in-library; documented as a terminal configuration issue (enable "double width emoji" in the terminal).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/617

---

## Grapheme/cursor editing

### Cursor drifts right after auto-wrap when the wrapped line starts with a space
- Symptom: In a wrap_lines=True, single-line `TextArea`, once a space character becomes the first character of a soft-wrapped visual line, the displayed cursor position drifts right by roughly one column per wrap; navigating with arrow keys corrects it.
- Root cause (as diagnosed): the layout's wrap-point/space-handling logic doesn't consistently discount a leading space that was "consumed" by the wrap when computing the visual column, drifting on every subsequent wrap.
- How resolved (or open): open at time of report; reproduced across at least two host environments including WSL.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/2071

### Zero-width style fragments at end of line are dropped
- Symptom: A zero-width-escape (styling-only) fragment appended at the very end of a rendered line is not drawn until another fragment is appended after it or the app closes.
- Root cause: the screen-diff renderer skips trailing zero-width fragments when diffing/outputting the last cell(s) of a line.
- How resolved (or open): open, no fix confirmed in fetched thread.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1651

### Cursor lands at column 0, overwriting/obscuring the prompt
- Symptom: After typing/entering a command, the cursor appears at the very start of the line (column 0), visually overwriting the prompt string, especially with certain custom `$PROMPT` values and narrow terminals.
- Root cause (as diagnosed): Mac-specific terminal-width/measurement edge case interacting with prompt length; several attempted fixes over a long thread did not fully resolve it for the original reporter.
- How resolved (or open): xonsh applied incremental Mac-specific renderer fixes; a duplicate report was closed as resolved by those fixes for most users, though the original thread never got a definitive final confirmation.
- Issues: https://github.com/xonsh/xonsh/issues/21, https://github.com/xonsh/xonsh/issues/456

### Word-case editing commands garble the buffer when "cursor wrap" is enabled
- Symptom: With a custom keybinding that lets Left/Right at line boundaries jump to the adjacent line ("cursor wrap"), running `uppercase_word`/`downcase_word` near a line boundary inserts text in the wrong place and garbles the following line.
- Root cause (as diagnosed): word-transform commands compute an end offset assuming the cursor stays within one logical line; the custom cross-line cursor movement violates that assumption.
- How resolved (or open): open; not a stock prompt_toolkit binding, so treated as a documented interaction hazard rather than an internal bug fix.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/469

### Up/Down move by logical line index, not visual (wrapped) row
- Symptom: No built-in way to have Up/Down keys move the cursor along the same visual column across soft-wrapped rows (like `gj`/`gk` in Vim or most GUI editors); they instead move to the same character offset in the previous/next logical line.
- Root cause: cursor vertical movement is implemented in terms of logical buffer lines, not the wrapped visual-row grid the renderer computes.
- How resolved (or open): open feature request; maintainer noted correct behavior needs to also work for full-screen apps and interacts with double-width characters, "not easy".
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1049

---

## Wrapping and geometry

### Long input line wraps over/covers the prompt instead of onto a new line
- Symptom: Sometimes a long input line wraps around and covers the prompt characters rather than continuing onto the next terminal row, even in a much wider terminal.
- Root cause: unknown/unclear-to-reporter at the time; fixed as part of general early renderer work.
- How resolved: fixed (confirmed by reporter, issue closed).
- Issues: https://github.com/xonsh/xonsh/issues/44

### Prompt/renderer's idea of terminal width disagrees with the terminal's own wrap column
- Symptom: In mintty (Cygwin), long commands wrap at 80 columns regardless of actual (wider) terminal width.
- Root cause: a terminal-emulator-specific width-reporting quirk (mintty) that the renderer's width query didn't correctly account for.
- How resolved: workaround applied by the user (terminal-side setting); not a prompt_toolkit code fix.
- Issues: https://github.com/xonsh/xonsh/issues/1236

### Background-color SGR escape leaks past a wrapped line boundary
- Symptom: A background color started on one visual line "bleeds" onto the next screen row in some terminals when the underlying logical line auto-wraps.
- Root cause (as diagnosed): the SGR reset is emitted at the logical end of line/content, not per physical (wrapped) row, so a wrap boundary can occur before the reset is written; also reproducible with plain bash, suggesting it's partly a terminal-emulator behavior rather than solely a prompt_toolkit bug.
- How resolved (or open): closed as "can't reproduce reliably / likely terminal issue," not fixed in xonsh.
- Issues: https://github.com/xonsh/xonsh/issues/4246

### No way to let the terminal (not the app) perform soft-wrap
- Symptom: Long lines are always wrapped by prompt_toolkit itself; there is no option to disable this and let the terminal's native line-wrap do it (as GNU readline does), which is desirable so that copy/paste of a long line doesn't pick up embedded newlines.
- Root cause: the renderer's whole architecture renders into an in-memory 2D screen buffer and diffs it against the previous frame; supporting terminal-native wrapping would require distinguishing "visual rows" from "physical rows" throughout cursor-movement, CPR handling, and mouse-position math.
- How resolved (or open): open; maintainer called it "really complex to get ... reliable on all terminals," citing also double-width character and tmux-reflow inconsistencies as complicating factors. A related request (different continuation prompt for hard vs. soft wraps, or none at all for soft wraps) is also open for the same architectural reason.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/709, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/493

### Cursor misaligned in multiline mode when the terminal is narrower than the prompt
- Symptom: When `$PROMPT` is wider than a small terminal, the cursor is placed at the end of the second (wrapped) line, and typing then overwrites the first prompt line instead of appending correctly.
- Root cause: multiline-prompt cursor-position math didn't account for the prompt itself wrapping across multiple physical rows before user input even begins.
- How resolved (or open): reported upstream to prompt_toolkit for a fix; xonsh side treats this as an upstream rendering limitation.
- Issues: https://github.com/xonsh/xonsh/issues/213 (filed against prompt-toolkit's predecessor project)

### Tall/long multiline input gets its top lines cut off ("shaved") on execution
- Symptom: A long multiline input that exceeds the prompt/terminal width wraps as expected while typing, but on submission the top 1-2 lines visually disappear; reproducible even in a tiny terminal with the standalone example app.
- Root cause: the renderer's "erase and redraw" logic on accept didn't correctly account for the number of wrapped visual rows the input occupied, so it erased fewer rows than were actually drawn.
- How resolved: fixed by an upstream commit; a follow-on regression (crash when the terminal is narrower than `$PROMPT`) surfaced immediately after and was fixed in a second commit; new release was cut.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/254 (root-caused from https://github.com/xonsh/xonsh/issues/703)

### Large buffers (thousands of lines) make editing slow
- Symptom: `BufferControl` becomes sluggish once the input buffer exceeds a few thousand lines, because every keystroke rebuilds and reprocesses (wraps, highlights) the entire document even though only a small viewport is visible.
- Root cause: the layout eagerly computed a full `Screen` for the whole document rather than lazily computing only the visible region.
- How resolved: refactored to a lazily-evaluated `Screen` object that only computes the lines actually displayed.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/222

---

## Prompt content

### Prompt string with ANSI escape codes isn't rendered/measured correctly
- Symptom: `$MULTILINE_PROMPT` in xonsh did not support embedded ANSI color escape codes the way `$PROMPT` does.
- Root cause: the multiline-continuation-prompt code path didn't parse/wrap raw ANSI the same way the main prompt formatter did.
- How resolved: fixed; xonsh added support for using ANSI (and `{COLOR}` tokens) in `$MULTILINE_PROMPT`.
- Issues: https://github.com/xonsh/xonsh/issues/5898

### Continuation-prompt renderer clobbers the last character of the prompt
- Symptom: `$MULTILINE_PROMPT` replaced the final character of the rendered continuation prompt with a space.
- Root cause: an off-by-one in how the continuation-prompt string was padded/truncated to the margin width.
- How resolved: fixed.
- Issues: https://github.com/xonsh/xonsh/issues/5899

### Prompt-embedded OSC (terminal-integration) sequences aren't escaped/filtered
- Symptom: OSC codes placed in `$MULTILINE_PROMPT` for shell-integration purposes (e.g. marking prompt/output boundaries per the finalterm/wezterm convention) are not filtered the way they are in the regular single-line `$PROMPT`, since prompt_toolkit requires such non-printing sequences to be wrapped in `\001..\002` zero-width markers to avoid corrupting the renderer's width math.
- Root cause: prompt_toolkit's own prompt formatter needs explicit zero-width-escape markers around raw terminal sequences so it doesn't count them toward the line width; the multiline path didn't add them.
- How resolved (or open): open at time of last comment; requires explicit escaping support to be added.
- Issues: https://github.com/xonsh/xonsh/issues/5058

### Terminal-title / mode-setting sequences in the prompt corrupt display under Emacs' ansi-term
- Symptom: Running xonsh inside Emacs' `ansi-term` produced a garbled prompt line containing literal escape-sequence text mixed into the visible prompt.
- Root cause: xonsh's terminal-title-setting code path checked `$TERM` against a narrow allow-list (`None`/`linux`) and didn't recognize the `TERM` values Emacs sets (`dumb`/`eterm-color`), so it emitted title-set sequences the terminal couldn't interpret, which then got echoed as visible garbage.
- How resolved: fix proposed (and applied) to also treat Emacs's `dumb`/`eterm-color` TERM values as "don't set title".
- Issues: https://github.com/xonsh/xonsh/issues/358

### Output that doesn't end in a newline gets overwritten by/merged into the next prompt
- Symptom: If a previously run command's output doesn't end with a trailing newline, the prompt is drawn on the same line and the leftover characters visually disappear/get overwritten.
- Root cause: prompt_toolkit has no reliable way to know whether the terminal cursor is already at column 0 (it would require an extra CPR round-trip just to check), so it cannot safely decide whether to emit a leading newline before drawing the next prompt.
- How resolved: closed as "very unlikely we can do something about this" — accepted as a fundamental limitation.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/453

### Un-erased prompt state leaves trailing whitespace that pollutes clipboard copies
- Symptom: The rendered prompt line contains trailing spaces padding it out to the terminal width; selecting/copying the line in a terminal picks up that trailing whitespace.
- Root cause: renderer pads/erases to end-of-line using spaces rather than a true erase-to-end-of-line sequence in the relevant code path, so the padding is visually invisible but textually present in the terminal's selection buffer.
- How resolved (or open): open at time of report.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1086

---

## Resize

### Terminal resize causes the prompt+input to be reprinted repeatedly ("spam")
- Symptom: Resizing the terminal window while a `PromptSession` is active causes the prompt and current input line to be printed over and over; gets dramatically worse with full-width UI elements like a bottom toolbar, and with narrower terminals.
- Root cause (as diagnosed, unresolved): investigation traced it to `renderer.erase()` not correctly accounting for text-wrapping when computing how many rows to erase before redraw, causing the prompt to visually "walk" upward on each small resize; not fully fixed.
- How resolved (or open): open; a contributor's partial fix ("works sometimes") did not fully resolve it. Related to a similar resize/duplicate-prompt report on macOS Terminal.app.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1933, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1675

### SIGWINCH (resize) only handled on the main thread
- Symptom: A full-screen `prompt_toolkit` application run in a background thread does not respond to terminal resize at all.
- Root cause: resize handling is wired through a `signal.signal(SIGWINCH, ...)` handler, and POSIX signal handlers can only be installed/received on the main thread.
- How resolved (or open): open as a design limitation; maintainer suggested mirroring the pattern used internally for `progress_bar`, which runs its own render loop in the background thread instead of relying on signal-driven resize.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/636

### Repeated/incremental resize on macOS accumulates rendering errors
- Symptom: Making a terminal window very narrow (so a long line wraps) and then continuously resizing it ("wiggling" the corner) produces accumulating artifacts — extra blank lines, duplicated printed text, and overwritten text — that get worse the more resize events fire.
- Root cause (as diagnosed): each resize event's incremental repaint logic doesn't independently reconstruct ground truth from the wrapped-line state, so small errors from one resize compound into the next; the terminal's own hardware-level line-wrap scrollback interacts with prompt_toolkit's own idea of what's on screen.
- How resolved (or open): open; acknowledged as fundamentally hard because terminal-side line-wrap history can't fully be predicted or queried.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/29

### Full-screen app resize fails when process launched via subprocess
- Symptom: A full-screen prompt_toolkit app started from another Python process via `subprocess` renders incorrectly on resize — duplicated text, wrong line lengths — and the problem worsens as the terminal grows; happens on both Windows PowerShell and other shells.
- Root cause (as diagnosed by reporter, unconfirmed): appeared related to inherited environment variables (`PYTHON*`) from the parent Python process; running the child with `python -E` (ignore `PYTHON*` env vars) worked around it.
- How resolved (or open): open; no official fix, workaround only.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1321

### Resize not propagated into a nested full-screen program run as a subprocess
- Symptom: Running a full-screen ncurses/slang program (`mc`) from inside xonsh does not respond correctly to terminal resize, unlike similar tools (`vim`, `nano`, `emacs`) which have the same underlying issue but weren't specifically called out.
- Root cause: the child process's controlling terminal/pty resize signal (`SIGWINCH`) propagation/foreground-process-group handling from the shell wasn't correctly forwarded for every subprocess type.
- How resolved (or open): closed with only a partial workaround identified (works for some tools like `fzf`/`ranger` "without modification", not for others).
- Issues: https://github.com/xonsh/xonsh/issues/2383

---

## Terminal queries and input races

### CPR (cursor position report) reply is misparsed and printed as literal text
- Symptom: The raw escape reply to a cursor-position request (e.g. `^[[24;1R`) appears visibly on screen as text, especially when a lot of output is being printed via `run_in_terminal`/`patch_stdout` concurrently with the prompt.
- Root cause: a timing race — the CPR reply from the terminal can arrive after prompt_toolkit has already stopped listening for it (e.g. input switched to "cooked mode", or the read loop exited), so it falls through to being treated as ordinary keyboard input/text instead of a control reply.
- How resolved: substantially improved (though not universally eliminated) by a rewrite of CPR/typeahead handling in the 2.0 branch, and by disabling "cooked mode" toggling around CPR waits.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/482, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/456

### wait_for_cpr_responses timeout crashes/hangs the event loop
- Symptom: When a CPR response doesn't arrive in time, `wait_for_cpr_responses` raised an unhandled exception, sometimes leading to a persistent "Press ENTER to continue..." recovery loop that could lock the terminal so badly Ctrl-C/Ctrl-Z no longer worked.
- Root cause: the timeout branch of the CPR-wait coroutine didn't handle the "no response, move on" case cleanly, letting an exception escape into the main event loop.
- How resolved: fixed by PR #1165; reporters confirmed the spontaneous crashes stopped after upgrading.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1164, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1169

### CPR wait blocks input for up to 0.5s on terminals that never reply
- Symptom: On terminal emulators that don't support/respond to CPR at all, every CPR request stalls the UI for the full timeout (0.5s) before continuing, and there's no way to know in advance a terminal won't respond.
- Root cause: no protocol exists for a terminal to declare "I don't support CPR" other than silence, so prompt_toolkit must always wait out the timeout the first time.
- How resolved: changed to a "try once, then remember" strategy — if no response is received, the terminal is marked as not supporting CPR and no further CPR requests (or waits) are issued for the remainder of the session; a warning message was added (with follow-up requests to make it suppressible).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/535

### CPR requested on transports that can't answer (pipes)
- Symptom: `PosixPipeInput` was hard-coded to report "does not respond to CPR" even when the pipe is actually connected to a real vt100 terminal downstream (e.g. a telnet/SSH server relaying to a real client terminal), so the app can't use CPR-dependent features there.
- Root cause: `responds_to_cpr` was a hardcoded `False` rather than a configurable flag on the pipe-input class.
- How resolved: agreed to add a `responds_to_cpr` boolean parameter to `PosixPipeInput`, defaulting to `False`.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/876

### CPR reply leaks to the terminal after a one-shot render helper exits
- Symptom: Using `print_container()` to render a single widget outside of an interactive session leaves a stray CPR reply code (e.g. `;1R`) printed to the terminal after the script exits.
- Root cause: `print_container` internally spins up an `Application` (which requests CPR as part of normal setup) but doesn't fully own/consume the async reply before tearing down, so it escapes to the real terminal instead of being consumed.
- How resolved (or open): open; a workaround (patching in a null `DummyInput`) was posted by a user, not an official fix.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1430

### Excess CPR/reset escape spam causes severe input lag in specific terminal emulators
- Symptom: With the prompt near the bottom of the screen, tab-completion became extremely laggy (10-20s) and would make iTerm2 spin, seemingly correlated with heavy use of CPR.
- Root cause: unrelated redundant `\x1b[0m` (reset style) escapes were being emitted far more often than necessary during screen-clear/redraw, which combined with a slow terminal emulator's escape-sequence processing produced visible input lag; the CPR round-trip itself amplified but wasn't solely the cause.
- How resolved: fixed by removing the unnecessary reset-escape spam in a renderer optimization; residual slowness attributed to the specific terminal emulator (iTerm2) being comparatively slow to process escapes, filed upstream to iTerm2 separately.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/617

### Holding Enter rapidly races CPR/redraw and garbles the prompt
- Symptom: Holding the Enter key down produced a visibly garbled prompt.
- Root cause (as diagnosed): rapid successive prompt-accept/redraw cycles raced with in-flight CPR requests/replies from the previous prompt render.
- How resolved: resolved as a side effect of the prompt_toolkit 2.x rewrite of the render/CPR handling.
- Issues: https://github.com/xonsh/xonsh/issues/1955

### Typeahead is unreliable across multiple lines / newlines
- Symptom: Typeahead (keystrokes buffered while the app is busy and not yet reading input) works for single lines but "eats" everything after the first newline, and sometimes drops parts of the typed text.
- Root cause: typeahead buffering/replay logic wasn't designed to correctly preserve embedded newlines/multiple Enter presses queued ahead of the reader being ready.
- How resolved: substantially reworked in the 2.0 branch (dedicated `typeahead.py` module); residual CPR-reply-printed-to-screen artifacts during typeahead replay persisted for at least one reporter on iTerm2 even after the rework.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/515

### patch_stdout corrupts or drastically slows rendering under heavy concurrent output
- Symptom: When a background thread prints a large volume of output while `patch_stdout()` is active, the terminal shows garbage escape-sequence fragments (corruption), and/or the program runs up to ~5x slower than without `patch_stdout`.
- Root cause (as diagnosed): each individual `write()` call triggers an erase-prompt / write-line / redraw-prompt cycle; under heavy print volume this both serializes badly (performance) and, combined with in-flight CPR requests from the renderer, can interleave badly with real output (corruption). Profiling also found `HSplit._divide_heights` recomputing expensive preferred-height layout on every single frame.
- How resolved (or open): open; no complete fix landed in the fetched thread — reporters suggest/attempt batching writes with a timer, with mixed results; a targeted patch removed unnecessary reset-escape spam elsewhere but this specific corruption/slowness combo remained open.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/681, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/682, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/680

---

## Paste

### Bracketed paste broken on Windows for multi-line content
- Symptom: Since a specific prompt_toolkit release, pasting multi-line text on Windows no longer triggers a proper bracketed paste, leading to garbled indentation of pasted code (e.g. in IPython).
- Root cause: a regression in the Windows input path's bracketed-paste detection/handling introduced in that release.
- How resolved (or open): reported as a regression; no confirming fix comment captured in the fetched thread (issue closed, but resolution mechanism not detailed in the retrieved comments).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1519

### Multi-line paste only shows the first line
- Symptom: Pasting multi-line text into an IPython session on an early prompt_toolkit 1.0.x release only displayed the first line; rest was effectively lost from the visible buffer.
- Root cause: bracketed-paste sequence parsing/consumption bug in that release.
- How resolved: fixed in the next point release (1.0.6).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/371

### Bracketed paste means pasted multi-line text is treated as one submission, surprising users
- Symptom: Pasting `help\nexit` into a single-line, non-multiline `PromptSession` shows both lines but only submits/processes them as one combined line after Enter, rather than running each line as a separate command the way some users expect from other shells.
- Root cause: this is the deliberate behavior of bracketed paste (prevents accidental execution of each pasted line, and defends against "paste-jacking" attacks where hidden newlines in clipboard content trigger unintended command execution); it isn't a bug but a security-motivated design choice that violates a common expectation.
- How resolved: documented; users advised to split on `\n` themselves, or replace the `Output` class to disable bracketed paste entirely if they don't want the protection.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1102, related design discussion at https://github.com/prompt-toolkit/python-prompt-toolkit/issues/806 (xonsh side, where the fix was to auto-switch to multiline mode on multi-line paste)

### Pasting before the prompt is ready splits into multiple separate prompt submissions
- Symptom: Pasting `a\nb\nc` after the `>` prompt is already visible correctly groups into one paste; pasting the same text before the prompt has finished appearing causes each line to be submitted as its own separate prompt invocation.
- Root cause: typeahead-buffered input arriving before the bracketed-paste-aware reader is attached isn't recognized as a single paste event, so it's processed as ordinary line-by-line keystrokes/Enters instead.
- How resolved (or open): open; flagged as related to a separate report about the general typeahead-vs-multiline interaction.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/816

### Copy/pasting a soft-wrapped long single line reproduces it as multiple lines
- Symptom: A single long command line that wraps visually across multiple terminal rows is captured by the terminal's own copy/select as separate lines (with embedded newlines) rather than the original single line, because prompt_toolkit (not the terminal) performs the wrapping.
- Root cause: same architectural cause as the "no terminal-native soft-wrap option" issue above — prompt_toolkit renders each wrapped visual row as a distinct terminal line with its own trailing newline in the actual terminal buffer, so terminal-level text selection sees real newlines.
- How resolved (or open): closed by xonsh as an upstream prompt_toolkit limitation; workarounds suggested (bind a key to copy the raw buffer content directly, bypassing terminal-selection).
- Issues: https://github.com/xonsh/xonsh/issues/4348

### Copy/pasting a command pads with a large amount of trailing whitespace
- Symptom: Copying and pasting a full command line elsewhere picked up a large run of trailing spaces (padded out to terminal width).
- Root cause: an older prompt_toolkit renderer version's line-padding/erase behavior (see "trailing whitespace pollutes clipboard" above) was more severe in the specific version in use.
- How resolved: fixed upstream; resolved for the reporter by upgrading `prompt_toolkit`.
- Issues: https://github.com/xonsh/xonsh/issues/3738

### Pasting certain unicode characters produces mojibake after a point-release regression
- Symptom: Pasting a specific emoji (🄲) resulted in `??` being inserted instead, starting with prompt_toolkit 3.0.49 (worked correctly in 3.0.48).
- Root cause: unknown/unconfirmed — flagged as a regression between two adjacent point releases, likely in paste-buffer decoding.
- How resolved (or open): open at time of report.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1978

---

## Suggestions/completions/menu redraw

### Auto-suggestion flickers on every keystroke
- Symptom: The greyed-out history-based auto-suggestion text visibly flickers each time a character is typed.
- Root cause: not independently diagnosed in this thread; tracked as a duplicate of an existing (unfetched) performance issue in the redraw path for the suggestion overlay.
- How resolved (or open): closed as duplicate; not independently confirmed fixed.
- Issues: https://github.com/xonsh/xonsh/issues/2689

### Ghosted autosuggestion text doesn't match actual Tab-completion candidates
- Symptom: The greyed-out inline suggestion shown while typing (e.g. after `git `) doesn't correspond to what pressing Tab actually offers as completions, which is confusing.
- Root cause: the autosuggestion source (history-based) and the Tab-completion source (context-aware completer) are independent subsystems with no guarantee of agreement.
- How resolved: closed after a related completion-handling PR was merged, improving (if not perfectly unifying) the two paths.
- Issues: https://github.com/xonsh/xonsh/issues/981

### Completion popup fails to appear for filenames containing spaces
- Symptom: The completion dialog sometimes doesn't show at all when completing a filename that contains a space, and separately produces "squirrelly"/glitchy completion output for the same case.
- Root cause: word-boundary/quoting logic used to compute the completion's "current word" mishandled embedded spaces, either failing to trigger the popup or computing the wrong replacement range.
- How resolved: fixed by xonsh PR #4477 for the popup-not-appearing case; a broader related report was later consolidated into a single tracking issue rather than fixed individually.
- Issues: https://github.com/xonsh/xonsh/issues/3843, https://github.com/xonsh/xonsh/issues/4378

### Completion-menu current-item highlight breaks after a prompt_toolkit point release
- Symptom: Upgrading to prompt_toolkit 3.0.39 broke the visual highlight indicating the currently-selected item in the completion menu.
- Root cause: unconfirmed in the fetched thread; maintainer could not reproduce on a later prompt_toolkit version (3.0.52), suggesting it was a transient regression already fixed upstream by the time of investigation.
- How resolved: closed as not reproducible on a newer version.
- Issues: https://github.com/xonsh/xonsh/issues/5179

### Completion insertion breaks when the typed text is longer than the chosen completion
- Symptom: A custom fuzzy completer that could return a completion shorter than what's already typed corrupted the buffer/cursor state when applied.
- Root cause: completion-application code assumed the replacement text is always at least as long as the text it's replacing (an off-by-length assumption).
- How resolved: fixed.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/116

### Completion-menu colors become unreadable after a point-release change
- Symptom: Upgrading from prompt_toolkit 2.0.5 to 2.0.6 made the completion menu's colors low-contrast/unreadable for users with specific 256-color terminal themes (base16-shell derived `xterm-256color` TERM overrides).
- Root cause: a change in how the completion menu's default style resolved colors interacted badly with terminals reporting 256-color support without matching true-color capability; effectively a color-depth/theme-detection mismatch, not unique to xonsh.
- How resolved (or open): worked around per-user by forcing true-color mode (`true_color = True`) when the terminal genuinely supports it, or via tmux `terminal-overrides`; no single root-cause fix landed for all cases in the fetched thread.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/766

### Completion menu leaves visual "traces" on Windows after it closes
- Symptom: In `cmd.exe`, vertical line artifacts from the completion menu's border remained on screen after the menu was dismissed.
- Root cause: a `cmd.exe`-specific redraw quirk where bulk console-buffer writes didn't fully invalidate previously drawn regions.
- How resolved: fixed by switching that Windows output path to draw characters one at a time instead of in bulk.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/127

### Dialog/menu rendering flickers on redraw
- Symptom: Modal dialogs (and, separately, a bottom toolbar re-shown between prompts) visibly flicker when rendered/re-rendered, sometimes intermittently.
- Root cause (as diagnosed): the dialog case was traced to rendering happening "too fast" relative to the terminal's own paint cycle (a manual `sleep()` between renders masked it); the toolbar case was traced to `erase_down()` clearing more of the screen than necessary (down to the very bottom) on every redraw instead of just down to the toolbar.
- How resolved (or open): both open; no principled fix, only user-side mitigations (added sleep; suggestion to only erase_down on `render.erase`, not regular renders — flagged by maintainer as "tricky... needs a lot of testing" across terminal types).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1664, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/547

### Reverse history search (Ctrl-R) keybinding repeatedly breaks across versions
- Symptom: Ctrl-R incremental reverse search stopped working at various points (OS X + prompt_toolkit shell in one report; "busted" more generally in a later report).
- Root cause: varied per report — one was an xonsh-side keybinding registration bug; a later report the maintainers could not reproduce at all.
- How resolved: fixed in one case (xonsh commit); unresolved/not reproducible in a later, differently-versioned report.
- Issues: https://github.com/xonsh/xonsh/issues/394, https://github.com/xonsh/xonsh/issues/2793, https://github.com/xonsh/xonsh/issues/5010

---

## Key parsing

### Escape key has an inherent ~0.5s response delay
- Symptom: A keybinding on the bare `escape` key fires noticeably late compared to other keys.
- Root cause: Escape (`\x1b`) is also the prefix byte for all Alt/Meta-modified key sequences, so the parser must wait out a timeout (`ttimeoutlen`/`timeoutlen`, default 0.5s) to be sure no further bytes are coming before deciding it was a bare Escape rather than the start of a longer sequence.
- How resolved: documented as expected behavior; users can reduce `Application.ttimeoutlen`/`timeoutlen`, though even reduced values didn't get delay under ~1s for one reporter, suggesting more than one timeout was in play.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1181

### Raw low-level key capture loses or mishandles the first Escape press
- Symptom: Using the documented "raw input capture" pattern (`create_input()` + `read_keys()`), the very first Escape keypress can be swallowed (only surfaces once a second key is pressed after it) unless the app also calls `flush_keys()`.
- Root cause: Escape detection needs the same "wait for more bytes, then flush" logic as normal key binding resolution, but the low-level `read_keys()` API in the documented pattern doesn't include that flush step, so a bare Escape sits buffered until more input triggers it.
- How resolved (or open): open; user found a workaround (iterate `chain(read_keys(), flush_keys())`), maintainer thread requested doc clarification but no code fix confirmed.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1655

### Modifier-arrow escape sequences mapped to the wrong (opposite) direction
- Symptom: Terminal-reported modified-arrow sequences like `ESC[1;6A` (Ctrl+Shift+Up) are bound in the ANSI sequence table to the wrong logical key — moving down instead of up — for 10 distinct modifier-combination entries.
- Root cause: a data-entry/transposition bug in the static `ansi_escape_sequences.py` lookup table, present since at least the 3.0.53 release.
- How resolved (or open): open at time of report (bug report includes the specific table rows that are self-inconsistent).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/2092

### Up/Down arrow keys broken on Windows by a Python 2/3 string-literal bug
- Symptom: After a specific commit, Up/Down arrow history navigation stopped working on Windows and instead raised a traceback.
- Root cause: `win32_input.py` compared byte/str values without `from __future__ import unicode_literals` under Python 2, causing a str/unicode mismatch.
- How resolved: fixed by adding the missing future-import; confirmed working by the reporter.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/316

### Arrow keys register but the visual highlight doesn't update on Windows Terminal
- Symptom: In a `radiolist_dialog`, arrow-key navigation works (confirmed via a raw keylogger) but the selection highlight is never drawn on Windows Terminal, while it renders correctly (as a blinking cursor) elsewhere.
- Root cause: unresolved in the fetched thread — narrowed to a Windows-Terminal-specific rendering gap for the selection indicator, not an input-parsing bug.
- How resolved (or open): open.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1789

### Forward Delete does nothing in some terminals (st)
- Symptom: In the `st` terminal, the Delete key sends `^[[P` by default, which prompt_toolkit doesn't map to forward-delete (it expects `^[[3~`, which `st` only sends if the user has separately configured `enable-keypad on` in `~/.inputrc`, an unrelated readline setting).
- Root cause: prompt_toolkit's key-sequence table doesn't include a binding for the `^[[P` variant some terminals use for Delete.
- How resolved (or open): open; workaround is to add a custom keybinding for that raw sequence.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1437

### Custom single-character key-table entries insert the raw escape sequence instead of the character
- Symptom: When a plain character (rather than a `Keys` enum member) is registered as the target of an `ANSI_SEQUENCES` table entry (as needed for xterm's `modifyOtherKeys=2` protocol, e.g. Shift+N sending `\x1b[27;2;78~`), the parser resolves the key correctly but then types the original escape sequence into the buffer instead of the intended character.
- Root cause: the sequence-to-key resolution path and the "insert this key as text" path disagree on how to interpret a bare-character `Keys` value coming from a custom table entry.
- How resolved (or open): open at time of report (includes a minimal repro).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/2086

### Terminal doesn't send modifier info for Shift+Arrow, so selection extension silently does nothing
- Symptom: `Shift+Right` (and other Shift+Arrow combinations), which should extend a selection in emacs-mode, does nothing in the default macOS Terminal.app, while working fine in xterm/Emacs.
- Root cause: macOS Terminal.app doesn't send the xterm-style modified-arrow escape sequences (`ESC[1;2C` etc.) that prompt_toolkit relies on to detect Shift+Arrow at all — a terminal-capability gap, not a parsing bug.
- How resolved: closed by xonsh as no-longer-applicable once it stopped vendoring its own prompt_toolkit fork.
- Issues: https://github.com/xonsh/xonsh/issues/3956

### tmux's "extended-keys" mode leaks raw xterm key-report sequences into the buffer
- Symptom: With `extended-keys on` set in tmux (commonly enabled so apps can distinguish Shift+Enter from plain Enter), pressing Shift+Space inserts the literal text `[27;2;32~` and Shift+Backspace inserts `[27;2;127~` into the input line instead of being interpreted as key presses.
- Root cause: the specific xterm `modifyOtherKeys` CSI sequences tmux forwards for these key combinations aren't recognized by prompt_toolkit's ANSI sequence table, so they fall through and get typed as literal text.
- How resolved (or open): open at time of report.
- Issues: https://github.com/xonsh/xonsh/issues/6597

### Carriage return (ControlM) misidentified as line feed (ControlJ)
- Symptom: A raw `\r` (ControlM) byte from the terminal is detected/dispatched as `ControlJ` (`\n`) instead.
- Root cause: the key-parser's byte-to-`Keys` mapping conflated the two control characters.
- How resolved: unknown from title alone; not deeply investigated in this pass (included as a known key-parsing pitfall).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/420

---

## Multiplexers and remote/Windows

### mosh transport causes constant blinking/flicker
- Symptom: Over a `mosh` (mobile shell) connection, a prompt_toolkit REPL renders as constantly blinking/broken.
- Root cause (as diagnosed): suspected to be an interaction between prompt_toolkit's vt100-oriented rendering optimizations (which assume a normal low-latency PTY) and mosh's predictive/differential screen-update model.
- How resolved: no code fix identified; the reporter's own workaround (switching to a mosh+tmux combination) incidentally avoided the problem, and a later mosh version release also appeared to resolve it independently.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/16

### Terminal-tab-switch under VTE terminals or tmux leaves ghost/duplicated UI
- Symptom: Switching tabs in a VTE-based terminal emulator (GNOME/XFCE Terminal) or in tmux causes a prompt_toolkit TUI to show duplicate status bars, ghost prompt lines, and stacked leftover render elements.
- Root cause: unconfirmed — the terminal's own tab-switch/redraw behavior appears to desync from prompt_toolkit's internal idea of what's currently on screen, so its incremental screen-diff renderer paints on top of stale terminal content rather than a real blank slate.
- How resolved (or open): open, root cause not yet diagnosed at time of report.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/2069

### Right prompt (rprompt) renders at the bottom of the terminal instead of the prompt line
- Symptom: A configured right-prompt (`rprompt=`) is drawn at the very bottom of the terminal window rather than aligned with the current prompt row; happens for both single-line and multi-line prompts, and moves back to the correct place only once input is submitted.
- Root cause: a specific renderer commit (07e3429) changed how the rprompt's row position is computed, breaking it for at least all-right-prompt and multiline-prompt-with-leading-rprompt-line use cases; a community-patched fix reverting the relevant logic circulated but wasn't merged upstream for a long time (thread notes "it's been a year" with no update).
- How resolved (or open): open for an extended period per the fetched thread; community fork/patch existed but official release status unclear.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1472, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1241

### Renderer checks host OS instead of the actual output transport, breaking SSH-from-Windows
- Symptom: Running a prompt_toolkit SSH server (`contrib/ssh`) from a Windows host produces incorrect cursor-position handling for connecting clients, even though the actual output object in use is a normal Vt100 output (appropriate for the remote client), not a Windows console.
- Root cause: `renderer.py` gates certain behavior on `is_windows()` (host platform) rather than checking the type of the actual `Output` object in use, so it incorrectly applies Windows-specific logic to a non-Windows output stream whenever the *host* happens to be Windows.
- How resolved (or open): open; reporter proposed a specific patch (checking output type instead of platform) but no merge confirmed.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1153

### Windows console-buffer-info calls fail intermittently across console host variants
- Symptom: Calls into the Win32 console API (`GetConsoleScreenBufferInfo` and friends) fail with pointer/struct-type errors or a "No Windows console found" exception, especially under IDE-integrated terminals (PyCharm), MSYS2, or other environments that don't provide a "real" Win32 console handle.
- Root cause: prompt_toolkit's Windows output path assumes a genuine Win32 console buffer is always available/attachable; terminal wrappers that emulate a console without exposing the real Win32 console API (PyCharm's run window historically, MSYS2 without `winpty`) don't satisfy that assumption.
- How resolved (or open): worked around per-environment (PyCharm added an "emulate terminal in output console" option; MSYS2 users wrap with `winpty`); a later comment notes `create_output` doesn't fall back to a plain-text output on Windows the way it does on POSIX when there's no real tty, which would be a more general fix. Long-running issue (reports from 2016 through 2022).
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/406, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1601

### Shared history storage misbehaves across simultaneous tmux panes
- Symptom: sqlite-backed shell history appeared to have "problems" when multiple tmux windows/panes were using xonsh simultaneously; one report traced it to a *different* concurrently open pane still using the older JSON history backend, and closing that pane fixed it.
- Root cause: unclear/unconfirmed generally — at least one case was cross-contamination between two different history-backend configurations open at once rather than a straightforward concurrent-write bug in the sqlite backend itself.
- How resolved (or open): closed without a definitive root-cause fix; anecdotal workaround only.
- Issues: https://github.com/xonsh/xonsh/issues/4944

### TERM=dumb / non-PTY output isn't gracefully degraded
- Symptom: Running inside a "dumb" terminal (Emacs shell/inferior-shell buffers, `expect`-driven automation) prompt_toolkit still attempts full escape-sequence-based rendering (cursor movement, highlighting, incremental search), which breaks output entirely rather than falling back to simple line-based I/O.
- Root cause: the renderer had no dedicated code path for degraded/non-escape-capable output; maintainer noted that without cursor positioning, most interactive editing features (especially multiline input) fundamentally can't work the same way, and initially argued that near-nothing would function without escapes.
- How resolved: a real fix eventually landed (PR #1035) — detect `TERM=dumb`/similar and skip rendering escape sequences, allowing basic `PromptSession().prompt()` output to work under Emacs and similar; multi-line input on dumb terminals was flagged as still imperfect (Emacs shell buffer users report partial improvement, not full support), and a "your terminal doesn't support CPR" warning was newly noticeable as a side effect for some users.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/390, https://github.com/prompt-toolkit/python-prompt-toolkit/issues/1032

### Nested REPL loses readline/backscroll behavior on Windows through layered consoles
- Symptom: Launching the plain Python REPL from within xonsh on Windows loses the ability to scroll back / use readline-style editing, unlike the same REPL launched directly under PowerShell.
- Root cause (as diagnosed): reproducible even without xonsh by nesting multiple layers of PowerShell before invoking Python — implicates how nested console-host layers negotiate which process "owns" the console's line-editing/readline features on Windows, not something specific to xonsh's own line editor.
- How resolved (or open): open; root-caused to general Windows console nesting behavior rather than a prompt_toolkit/xonsh bug per se.
- Issues: https://github.com/xonsh/xonsh/issues/5650

---

## Other

### Enabling mouse-reporting mode breaks the terminal's native scroll/selection
- Symptom: Setting `$MOUSE_SUPPORT=True` disables the terminal's own two-finger/scrollwheel scrolling and text selection/copy-paste (macOS Terminal, iTerm2), with no error shown, because the terminal is now sending all mouse events to the application instead of handling them itself.
- Root cause: enabling any mouse-tracking mode (needed for in-app mouse support, e.g. cursor placement by click) is a global terminal mode that necessarily also suppresses the terminal emulator's own native mouse-driven scroll/selection — an inherent trade-off of the VT100 mouse-reporting protocol, not a prompt_toolkit-specific bug.
- How resolved: not fixed in-library; documented workaround is a terminal-side setting (e.g. disabling "Report mouse wheel events" in iTerm2 preferences).
- Issues: https://github.com/xonsh/xonsh/issues/1930

### Terminal left in a broken mouse-tracking state after a mouse-enabled subprocess is killed
- Symptom: After force-killing a mouse-enabled full-screen program (`vim`) run as a subprocess, scrolling stops working afterward in the parent shell.
- Root cause: the child program enabled a mouse-tracking terminal mode and was killed before it could send the corresponding disable sequence, leaving the terminal (and thus the parent shell session) stuck in mouse-reporting mode.
- How resolved: closed as resolved (no detail on whether xonsh added its own mouse-mode-reset-on-subprocess-exit safeguard, or whether it was fixed upstream/by the terminal).
- Issues: https://github.com/xonsh/xonsh/issues/1609

### Cutting/truncating unicode text in captured subprocess output
- Symptom: When xonsh captures a subprocess's stdout (e.g. via `$()`), unicode characters in that captured text can be cut/truncated incorrectly.
- Root cause: not fully diagnosed in the fetched thread; likely a byte-vs-character boundary truncation bug in the output-capture buffering rather than the line editor itself, but included here since capture buffering interacts with the same encoding paths as interactive rendering.
- How resolved (or open): open; maintainers invited a PR rather than committing to a fix themselves.
- Issues: https://github.com/xonsh/xonsh/issues/5939

### Full-line-input performance collapses under many concurrently-updating UI regions
- Symptom: Applications with several frequently-updating regions (toolbars, status widgets) alongside the prompt see rendering CPU cost balloon; profiling showed layout-height computation (`HSplit._divide_heights`) being redone on every single frame even when nothing sizing-relevant changed.
- Root cause: preferred-height computation for child windows isn't cached/memoized between frames, so cost scales with total redraw frequency rather than with actual layout changes.
- How resolved (or open): open; identified as an easy caching opportunity by a contributor's profiling but no merged fix confirmed in the fetched thread.
- Issues: https://github.com/prompt-toolkit/python-prompt-toolkit/issues/682
