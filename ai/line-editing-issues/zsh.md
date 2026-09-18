# zsh ZLE line-editing failure modes

Research compiled from zsh-workers/zsh-users mailing list archives, the zsh SourceForge
tracker, ChangeLog history, and downstream trackers (ohmyzsh, zsh-autosuggestions,
zsh-syntax-highlighting, powerlevel10k). Scope is limited to interactive line-editor
*display/editing* bugs; scripting/completion-logic/performance issues are excluded except
where they manifest as a display bug.

Note on the SourceForge tracker: it turned out to be essentially unused for this bug class —
searches against `sourceforge.net/p/zsh/bugs` returned no relevant, fetchable tickets for any
of the topics below. Real bug traffic for ZLE lives on zsh-workers/zsh-users and in the
ChangeLog/patch discussions instead.

---

## Width measurement

### Emoji variation-selector width mismatch (U+FE0F)
- Symptom: A base codepoint followed by the emoji-presentation variation selector U+FE0F (e.g. ☁️ = U+2601 + U+FE0F) gets the wrong on-screen width computed by ZLE, causing cursor drift and duplicated/misplaced characters, especially through bracketed paste.
- Root cause: ZLE relies on libc `wcwidth()`, which has no concept of variation selectors changing a base character's rendered width, and the Unicode standard doesn't guarantee emoji-presentation width either.
- How zsh resolved it: Unresolved/open — treated as a spec gap; a terminfo-based negotiation was discussed but not implemented.
- Sources: https://www.zsh.org/mla/workers/2024/msg00484.html, https://www.zsh.org/mla/workers/2024/msg00496.html

### Text-presentation variation selector (U+FE0E) miscounts columns
- Symptom: Typing a character followed by U+FE0E (request text/monochrome presentation) shifts ZLE's column bookkeeping by one for everything typed so far; backspace then behaves strangely. Reproduced across multiple Macs/terminals/zsh versions.
- Root cause: ZLE's cursor/column tracking doesn't treat the variation selector as width-0/attached-to-base the way the terminal does.
- How zsh resolved it: No fix recorded in the thread.
- Sources: https://www.zsh.org/mla/users/2017/msg00432.html

### Broken/incorrect platform `wcwidth()` (e.g. classic macOS)
- Symptom: Combining characters get reported as width 1 instead of 0 by the platform's libc `wcwidth()`, breaking cursor math and redraw.
- Root cause: The platform's `wcwidth()` implementation is simply wrong for certain codepoints.
- How zsh resolved it: zsh ships its own alternative `wcwidth()` plus a `BROKEN_WCWIDTH` compile-time option that additionally treats any character with `wcwidth()==0 && !iswcntrl()` as combining.
- Sources: https://www.zsh.org/mla/workers/2008/msg00502.html, https://www.zsh.org/mla/workers/2008/msg00522.html, https://www.zsh.org/mla/workers/2008/msg00469.html

### East Asian "Ambiguous width" disagreement between shell and terminal/font
- Symptom: Characters flagged "Ambiguous" in the East Asian Width property (e.g. ellipsis U+2026) render single-width on some platforms/fonts and double-width on others (Solaris `wcwidth()` reportedly always returns 2); when zsh's assumption disagrees with the terminal's actual rendering, cursor placement breaks.
- Root cause: Width for "ambiguous" characters is a locale/font policy choice, not a fixed fact, so shell and terminal can disagree.
- How zsh resolved it: Unresolved in general; remains a documented limitation in the zsh FAQ (no locale-aware width table or terminal-width-query mechanism was adopted).
- Sources: https://www.zsh.org/mla/users/2015/msg00570.html, https://zsh.sourceforge.io/FAQ/zshfaq05.html

### Emoji/multibyte width bug still present in modern zsh (5.4–5.9)
- Symptom: Typing a multibyte emoji (e.g. 🍾) causes cursor-position anomalies even though editing adjacent single-byte characters works fine; reproducible across zsh 5.4.2, 5.8, 5.9 and multiple locales/terminals.
- Root cause: Not conclusively diagnosed; consistent with the general wide-character width-accounting gap.
- How zsh resolved it: No confirmed fix.
- Sources: https://www.zsh.org/mla/users/2022/msg00320.html

### `wcwidth()` model diverges from actual terminal rendering (emoji, Nerd Font glyphs, Unicode-version skew)
- Symptom: e.g. inserting a snake emoji (🐍) renders as 2 cells as expected, but typing a subsequent character lands at the wrong offset; emoji with variation selectors render at a different width than zsh computed, visibly misaligned versus bash/fish in the same terminal.
- Root cause: zsh relies on the system libc `wcwidth()`, which can lag behind current Unicode versions, can't represent grapheme-cluster/variation-selector combined width, and can't know terminal/font-specific widths (e.g. Private-Use-Area Nerd Font icons).
- How zsh resolved it: Unresolved as a general fix; a proposed (unconfirmed-merged) workaround was to query the terminal directly with CSI 6n (cursor position report) to empirically learn a character's rendered width.
- Sources: https://www.zsh.org/mla/workers/2016/msg02326.html, https://www.zsh.org/mla/workers/2024/msg00496.html

### Prompt-embedded emoji eats the following space (downstream: powerlevel10k)
- Symptom: An emoji in a custom prompt segment causes the space character right after it to visually disappear; happened on FreeBSD but not macOS, with tmux involved.
- Root cause: Prompt width calculation mis-measures the emoji's rendered width relative to what the terminal (tmux) actually draws, shifting subsequent output.
- How zsh resolved it: Workaround only (remove the emoji or manually pad); no general fix.
- Source: https://www.zsh.org/mla/users/2020/msg00259.html

### Multiple wide/double-width emoji in one prompt segment break layout (powerlevel10k)
- Symptom: Using two or more double-width emoji glyphs (e.g. "⬇️" and "🥦") in git-status segments causes the prompt's computed length to be wrong, producing duplicated/re-rendered prompt lines that don't fit the terminal width.
- Root cause: Width accounting for concatenated wide glyphs undercounts/overcounts.
- How zsh resolved it: Workaround — swap to single-width icon alternatives; no upstream fix confirmed.
- Source: https://github.com/romkatv/powerlevel10k/issues/2646

### Historical: prompts not scanned for multibyte characters at all
- Symptom: Prompts containing multibyte UTF-8 characters had display width miscalculated (effectively 1 column per byte), producing cursor errors and display corruption after the prompt.
- Root cause: Per maintainer Peter Stephenson, early zsh "didn't scan prompts for multibyte characters" when computing width.
- How zsh resolved it: `wcwidth()`-based prompt width calculation was added, decoding multibyte sequences and zero-width escapes; truncation (`%12<...<`) and in-buffer text long continued to assume single-column-per-character in some paths.
- Source: http://www.zsh.org/mla/workers/2005/msg01086.html

---

## Grapheme/cursor editing

### ZWJ emoji sequences edited as separate codepoints, not one grapheme
- Symptom: A ZWJ-joined emoji sequence (e.g. the "couple with heart" family emoji) is displayed/edited by ZLE as several independent glyphs rather than one visual unit, unlike bash/fish and the terminal's own rendering; cursor movement/backspace step through it codepoint-by-codepoint.
- Root cause: ZLE has no grapheme-cluster segmentation — it operates on codepoints with `wcwidth`-based special-casing only for combining marks, not full Unicode grapheme boundaries including ZWJ joins.
- How zsh resolved it: Open/unresolved as of the report (Aug 2023).
- Source: https://www.zsh.org/mla/workers/2023/msg00789.html

### ZWJ sequence pasted from clipboard breaks editing
- Symptom: Pasting a ZWJ-containing sequence from the system clipboard produces broken line-editing behavior, distinct from the general typed-ZWJ display bug.
- Root cause: Unknown; consistent with the same missing grapheme-cluster handling, possibly compounded by the bracketed-paste code path.
- How zsh resolved it: Unresolved based on retrieved thread content.
- Sources: https://www.zsh.org/mla/users/2025/msg00061.html, https://www.zsh.org/mla/users/2025/msg00053.html

### Double-width-character insert/redraw corruption via `ich1` truncation
- Symptom: After typing five or more double-width (CJK) characters, moving to line start, and inserting a space, the screen clears/corrupts and the cursor jumps up a line; only triggered at 5+ wide characters, seen on rxvt-unicode/rxvt with `ich1` (insert-character) defined in terminfo.
- Root cause: zsh's line-refresh code (`zle_refresh.c`) pads double-width characters with internal `WEOF` filler cells representing the second column; the insert-character redraw path truncated the old line mid-way through a `WEOF` marker instead of at a true character boundary, desyncing the terminal's insert-mode state.
- How zsh resolved it: Fixed — truncation logic moved inside `#ifdef MULTIBYTE_SUPPORT` and changed to step backward past `WEOF` markers to find a real character boundary before truncating.
- Source: https://zsh-workers.zsh.narkive.com/SleSStFh/fun-redraw-issue-with-double-width-characters

### Vi visual-mode selection region doesn't match highlighted characters (low confidence)
- Symptom: In ZLE's vi visual mode, the region actually operated on (yank/delete) doesn't line up with what's visually highlighted on screen.
- Root cause: Not confirmed — only a title-level search hit, not independently fetched in full.
- How zsh resolved it: Unknown.
- Source: https://zsh.org/mla/users/2019/msg00533.html

---

## Wrapping and geometry

### PROMPT_SP soft-margin marker for unterminated output lines
- Symptom: When command output doesn't end in a newline, printing the next prompt directly after it lets terminal mouse-selection join the dangling output and the prompt line, and there's no visible cue the line was incomplete.
- Root cause: Prompt output has no way to mark "this line did not end with \n" before drawing the next prompt.
- How zsh resolved it: Added `PROMPT_SP` (on by default), which uses terminal auto-margin behavior plus save/restore-cursor termcap capabilities to push a soft marker (`PROMPT_EOL_MARK`/eolmark, default `%B%S%#%s%b`) to the right margin, forcing a wrap, then restores the cursor.
- Sources: https://zsh.org/mla/workers/2005/msg00819.html, https://www.zsh.org/mla/workers/2005/msg00847.html

### `PROMPT_EOL_MARK=''` couldn't actually be set empty
- Symptom: Setting `PROMPT_EOL_MARK=''` to disable the end-of-line marker didn't work; the default '%' marker kept appearing.
- Root cause: C code tested `if (!eolmark || !*eolmark)`, treating an empty string the same as unset.
- How zsh resolved it: Changed the check to `if (!eolmark)` only, so an explicit empty value now suppresses the marker.
- Source: https://www.zsh.org/mla/workers/2010/msg00924.html

### Right-prompt/cursor off-by-one after line wrap at rightmost column
- Symptom: Since roughly zsh 5.5, when a line wraps, the cursor appears a few columns back from where it should be; after typing and deleting, the cursor snaps onto the left-prompt text instead of its end.
- Root cause: zsh's cursor-tracking can't determine whether the terminal actually wrapped the cursor to the next line after writing the last column, or left it pinned at the margin — terminal auto-wrap behavior at the last column isn't consistently defined across emulators.
- How zsh resolved it: No universal fix; mitigated via `ZLE_RPROMPT_INDENT` (default leaves a 1-column gap) so the right prompt never touches the true last column.
- Sources: https://zsh.org/mla/workers/2018/msg00575.html, https://www.zsh.org/mla/workers/2013/msg01176.html

### Extra blank line / mispositioned cursor with `ZLE_RPROMPT_INDENT=0`
- Symptom: Setting `ZLE_RPROMPT_INDENT=0` caused an unwanted extra blank line under the prompt in GNOME Terminal, and separately, wrong cursor column/row reproduced on the Linux console and WSL's Command Prompt, depending on whether the right prompt lands on the terminal's last visible row.
- Root cause: After writing the right prompt flush to the last column, zsh's internal tracked cursor column (`vcs`) wasn't updated to reflect the resulting (possibly wrapped) terminal cursor position; terminal auto-margin handling also differs between the bottom row and other rows.
- How zsh resolved it: A patch adjusts `vcs` right after writing the right prompt; a related patch for the general mispositioning was proposed but not confirmed validated for the last-row case specifically.
- Sources: https://www.zsh.org/mla/workers/2019/msg00360.html, https://www.zsh.org/mla/workers/2019/msg00367.html

### Long/wrapped command lines misbehave under prompt frameworks over SSH
- Symptom: A long command that wraps multiple terminal rows has its first wrapped line hidden, and moving backward through the buffer places the cursor at the wrong screen position; reproduced with Oh My Zsh over SSH but not with plain zsh or locally.
- Root cause: Unconfirmed — suggests an interaction between a prompt theme's escape-sequence usage and ZLE's wrap/redraw math.
- How zsh resolved it: Unresolved/closed without a documented fix.
- Source: https://github.com/robbyrussell/oh-my-zsh/issues/2314

### ZLE always clears to end-of-screen before drawing the prompt (design constraint, not a bug)
- Symptom: Users trying to manually reposition the cursor (e.g. emitting `ESC[H`) find zsh clears from the prompt to end-of-screen anyway, wiping content they expected kept.
- Root cause: By design — ZLE is a multi-line editor whose completion lists draw below the prompt, so it always clears-to-end-of-screen before printing the prompt to prevent stale content bleeding into the freshly drawn buffer.
- How zsh resolved it: Not a bug; documented workaround is embedding cursor positioning inside `PS1` itself.
- Source: https://www.zsh.org/mla/users/2023/msg00580.html

---

## Prompt content

### Width miscount without `%{...%}` around embedded escape sequences
- Symptom: Embedding raw ANSI escape sequences (e.g. color codes) directly in `PS1`/`RPS1` without marking them causes zsh to miscount visible prompt width, producing wrapping/cursor-position errors once the line wraps or a redraw/resize fires.
- Root cause: zsh counts prompt characters to compute visible width; it can't know a byte range is a non-printing escape unless told via `%{...%}`.
- How zsh resolved it: Documented convention — wrap zero-width/non-printing content in `%{...%}`.
- Sources: https://zsh.org/mla/workers/2016/msg01354.html, https://zsh.sourceforge.io/Doc/Release/Prompt-Expansion.html

### PS2 continuation-prompt lacks nesting-aware indentation by default
- Symptom: The default `PS2` (shown for incomplete multi-line input) doesn't track construct-nesting depth, making multi-line entry/copy-paste awkward without manual prompt-expansion tricks; a third-party plugin (`zsh-no-ps2`) exists purely to replace PS2 with a plain newline.
- Root cause: PS2 rendering by itself doesn't interact well with paste/history reconstruction or nesting depth.
- How zsh resolved it: Left to user-level customization (`%_`-keyed indent arrays) or third-party plugins rather than a built-in smart PS2.
- Sources: https://www.zsh.org/mla/users/2010/msg00624.html, https://www.zsh.org/mla/users/2023/msg00733.html

(See also the "wcwidth() model diverges from actual terminal rendering" entry under Width measurement, which is largely a prompt-content-width problem as well.)

---

## Resize

### Buffer/prompt corruption on rapid/horizontal terminal resize (SIGWINCH storms)
- Symptom: Resizing the terminal window (especially horizontally, especially reported in GNOME Terminal) corrupts content displayed above or at the prompt — text gets displaced/overwritten; can also erase multi-line prompts down to one line or duplicate the prompt.
- Root cause: ZLE recalculates prompt width/cursor position and redraws on every `SIGWINCH`; some terminals fire SIGWINCH once per incremental pixel/column of a drag-resize, and ZLE has no model of on-screen content beyond its own editable line, so repeated recalculation "wanders" relative to reality. Unbalanced `%{...%}` usage makes it worse.
- How zsh resolved it: No complete fix; mitigations are correct `%{...%}` delimiter usage and tuning `ZLE_RPROMPT_INDENT`/RPS1.
- Sources: https://www.zsh.org/mla/workers/2016/msg01354.html, https://www.zsh.org/mla/workers/2015/msg00847.html

### Prompt drawn on the wrong line after a window-size change
- Symptom: When the terminal reflows surrounding text on resize, zsh redraws the prompt on the wrong line — parts of the old prompt aren't erased, or prior lines disappear entirely.
- Root cause: zsh didn't account for how the prompt's own rendered height changes once the terminal reflows text around it on resize.
- How zsh resolved it: Patch saves the cursor position (`TCSAVECURSOR`) before drawing the prompt and, specifically on a detected resize event, restores it (`TCRESTRCURSOR`) instead of unconditionally repositioning to (0,0).
- Source: https://www.zsh.org/mla/workers/2019/msg00561.html

### Stale `COLUMNS`/`LINES` after remote/nested terminal size changes
- Symptom: After returning from a remote host where the terminal was resized mid-session, the local shell's `COLUMNS`/`LINES` don't match the terminal's actual current size.
- Root cause: `COLUMNS`/`LINES` refresh from `TIOCGWINSZ` on `SIGWINCH`; if the size change happens where that signal path doesn't fire as expected (e.g. across a remote session boundary), the shell's notion of size falls out of sync.
- How zsh resolved it: No single definitive fix; reliance remains on SIGWINCH + TIOCGWINSZ.
- Sources: https://www.zsh.org/mla/users/1999/msg01597.html, https://www.zsh.org/mla/users/1997/msg00424.html

### Prompt mode-indicator change (e.g. vi mode) triggers wrong-place redraw (downstream: powerline)
- Symptom: In a multi-line prompt showing a vi-mode indicator, switching edit mode mid-edit causes the redraw to land in the wrong screen location, relocating the in-progress command line.
- Root cause: Not fully diagnosed; consistent with the general pattern that any event changing prompt height/content invalidates zsh's cached notion of where the prompt/buffer sits on screen.
- How zsh resolved it: Unresolved/downstream-reported; no core zsh patch confirmed.
- Source: https://github.com/powerline/powerline/issues/1529 (downstream, lower confidence)

### tmux pane resize corrupts prompt display via repeated SIGWINCH redraws
- Symptom: Shrinking a tmux pane drastically and restoring it (or aggressive resizing generally) leaves the visible prompt duplicated/misaligned; reported for zsh, bash, and csh, with zsh/bash notably worse.
- Root cause: zsh/bash redraw eagerly on every SIGWINCH starting from the beginning of the current on-screen terminal line rather than the logical buffer line; as tmux reflows text across the resize, fragments of the old prompt are left on rows that no longer align with the new width, compounding on each subsequent redraw. csh defers redraw until the next keystroke and avoids the worst of it.
- How zsh resolved it: Unresolved/open in the referenced tmux discussion; no zsh-side change documented.
- Source: https://github.com/tmux/tmux/issues/516

---

## Terminal queries and input races

### Cursor Position Report (`\e[6n`) reply races with real keystrokes
- Symptom: A naive `pos=$(print "\e[6n")` comes back empty because the terminal's reply (`^[[y;xR`) is written to the input stream (as if typed), not to command output; it can also arrive interleaved with actual keys the user presses between the query and the reply.
- Root cause: CPR replies are asynchronous injected input, not synchronous command output, so a simple command substitution can't capture them, and there's an inherent race window with real typing.
- How zsh resolved it: Workaround only — write the query, then explicitly `read -rs -k1` in a loop collecting bytes until the trailing `R`; the race with real input is not eliminated.
- Sources: https://www.zsh.org/mla/users/2015/msg00861.html, https://www.zsh.org/mla/users/2015/msg00865.html, https://www.zsh.org/mla/users/2015/msg00866.html

---

## Paste

### Trailing newline in bracketed paste creates ambiguous "did it execute?" state
- Symptom: A final newline in pasted text can make a user think their command was already submitted, since zsh strips the trailing newline at accept-line time rather than treating it as pressing Enter.
- Root cause: Design tension between not auto-executing pasted text that happens to end in newline, and making it clear whether execution happened.
- How zsh resolved it: No clean resolution — extensively debated; the pragmatic default (strip the trailing newline, don't auto-execute) was kept rather than a provably-correct fix.
- Sources: https://www.zsh.org/mla/workers/2015/msg01718.html, https://www.zsh.org/mla/workers/2015/msg02360.html

### `bracketed-paste-magic` widget errors on an empty paste buffer
- Symptom: Pasting in PuTTY produced `bracketed-paste-magic:zle:47: not enough arguments for -U` on every paste; paste still worked but threw an error each time.
- Root cause: `$PASTED` was referenced unquoted in an internal `zle -U $PASTED` call; an empty pasted buffer word-split away entirely, leaving `-U` with no argument. A regression — earlier code had this quoted correctly.
- How zsh resolved it: Restored quoting around `$PASTED`.
- Source: https://www.zsh.org/mla/workers/2019/msg00808.html

### `bracket-paste-magic` corrupts already-quoted URLs pasted mid-string
- Symptom: Pasting a full URL inside an already-quoted string caused zsh 5.2+ to insert spurious backslashes inside the quotes; zsh ≤5.0.7 (pre-rewrite) didn't have the problem. Only the whole-URL paste triggered it, not typing or a fragment.
- Root cause: Magic-quoting was applied character-by-character during the paste stream rather than to the whole buffer at once, so it couldn't tell it was already inside a quoted context.
- How zsh resolved it: Discussed moving the quoting logic to run once, after the full paste completes, via a `zle-line-pre-redraw` hook, processing the whole buffer retroactively.
- Sources: https://www.zsh.org/mla/workers/2016/msg00923.html, https://www.zsh.org/mla/workers/2016/msg00994.html, https://www.zsh.org/mla/workers/2016/msg00990.html

### Large pastes cause multi-minute 100% CPU stalls with widget-wrapping plugins
- Symptom: Pasting a long command with `zsh-syntax-highlighting` active (which enables `bracketed-paste-magic`) took 10+ minutes at 100% CPU; disabling the plugin made the same paste instant.
- Root cause: `bracketed-paste-magic`'s default `active-widgets` style re-invokes wrapped `self-*`/other zle widgets once per pasted character instead of treating the paste as one bulk insert, so highlighting/hooks reprocess the growing buffer per character.
- How zsh resolved it: Workaround via `zstyle ':bracketed-paste-magic' active-widgets '.self-*'`; later fixed upstream in zsh-syntax-highlighting by switching to a `zle-line-pre-redraw`-based implementation processing the paste once.
- Sources: https://github.com/zsh-users/zsh-syntax-highlighting/issues/295, https://github.com/zsh-users/zsh-autosuggestions/issues/102

### `url-quote-magic` silently stops working while paste itself keeps working
- Symptom: After some time in a session, typed/pasted URLs stop being quote-escaped, while bracketed-paste continues to function normally — intermittent and hard to reproduce.
- Root cause: A user's `disable -p #` (disabling the `#` glob-qualifier) leaked into `url-quote-magic`'s internal `(#b)(...)` pattern match; `setopt localoptions` didn't isolate the function from this global `disable`.
- How zsh resolved it: Replacing `setopt localoptions` with `emulate -L zsh` in the affected function fully resets the option/pattern environment for that scope.
- Source: https://zsh.org/mla/users/2022/msg00434.html

### Stale ghost text/autosuggestion survives a bracketed paste
- Symptom: After a bracketed paste, pressing right-arrow appends the pre-paste autosuggestion text instead of a fresh one, corrupting the buffer.
- Root cause: Autosuggestion state isn't invalidated on paste under `bracketed-paste-magic`.
- How zsh resolved it: Open at time of report.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/351

---

## Suggestions/completions/menu redraw

### Autosuggestion cursor misplacement on line wrap
- Symptom: When an autosuggestion is long enough to wrap to the next terminal line, the cursor jumps to the start of that next line instead of staying right after the typed text.
- Root cause: Unknown — redisplay math doesn't account for wrap point vs. typed-text end.
- How zsh resolved it: Open, no fix confirmed.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/226

### Autosuggestion misalignment under narrow terminal width
- Symptom: When terminal width is smaller than the suggested command's width, the displayed suggestion/cursor becomes visually misaligned.
- Root cause: Unresolved in thread.
- How zsh resolved it: Open; workaround is disabling the plugin.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/572

### Autosuggestion not cleared on kill-to-start (Ctrl-U)
- Symptom: `Ctrl-U` removes buffer text but leaves the grey suggestion text still rendered on screen.
- Root cause: The kill widget isn't wrapped/hooked by the suggestion-clearing logic.
- How zsh resolved it: Open.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/114

### Autosuggestion highlight color bleeds past the acceptance point
- Symptom: With `ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE` using palette colors <8, accepting/partially-accepting a suggestion leaves remaining/typed text still rendered in the suggestion's highlight color instead of resetting; traced to a zsh 5.9 behavior change.
- Root cause: Color-reset escape logic doesn't correctly clear low-numbered (`fg<8`/`bg<8`) SGR codes.
- How zsh resolved it: Open at time of report.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/698

### Discarded suggestion tail stays visible after partial Enter
- Symptom: Pressing Enter to execute only the typed prefix (ignoring the grey suggested continuation) leaves the untyped continuation still visibly rendered as if part of the executed command.
- Root cause: The accept-line widget didn't clear `POSTDISPLAY` before executing.
- How zsh resolved it: Reported as a feature request; predates later official clear-on-execute handling.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/162

### Dropped character when a long autosuggestion stops matching typed text
- Symptom: When a suggestion wider than the terminal is showing and typed text stops matching it, one character (e.g. a quote) fails to render even though it's in the buffer.
- Root cause: Unknown — partial-redraw path miscounts wrapped-line width when truncating the old long suggestion.
- How zsh resolved it: Open.
- Source: https://github.com/zsh-users/zsh-autosuggestions/issues/578

### Redraw races between zsh-autosuggestions and zsh-syntax-highlighting
- Symptom: With both plugins loaded, the highlighter intermittently renders the autosuggestion in the wrong color (e.g. white instead of faint gray) or misses redraw entirely.
- Root cause: Two plugins independently wrapping/hooking the same ZLE redraw widgets stomp on each other's state; no single shared hook existed until `zle-line-pre-redraw`.
- How zsh resolved it: zsh added the `zle-line-pre-redraw` hook upstream specifically so plugins can register non-conflicting callbacks instead of wrapping widgets.
- Sources: https://github.com/zsh-users/zsh-autosuggestions/issues/529, https://github.com/zsh-users/zsh-syntax-highlighting/issues/579

### Widget-wrapping drops call arguments
- Symptom: Wrapping a builtin ZLE widget (the old technique both plugins used to hook redraw points) silently discards the arguments the widget was invoked with, breaking widgets that take numeric/args.
- Root cause: The naive `zle -N widget wrapper` pattern doesn't forward `$@`/`NUMERIC` correctly.
- How zsh resolved it: Motivated the move to the `redrawhook`/`zle-line-pre-redraw` model to avoid widget wrapping entirely.
- Sources: https://github.com/zsh-users/zsh-autosuggestions/issues/857, https://github.com/zsh-users/zsh-syntax-highlighting/issues/245

### Syntax highlighting lost after a failed/aborted completion
- Symptom: After a Tab completion attempt that finds no match (or is aborted), the command word's highlighting disappears/reverts to plain text until another redraw-triggering key is pressed.
- Root cause: No `zle-line-pre-redraw` (or equivalent) callback fired after a failed completion, so the highlighter's redisplay pass was skipped.
- How zsh resolved it: Fixed conceptually by the `redrawhook`/`zle-line-pre-redraw` mechanism (confirmed for related issues); a later 2023 report shows the class recurring, with `COMPLETION_WAITING_DOTS=true` as a workaround that forces a redraw.
- Sources: https://github.com/zsh-users/zsh-syntax-highlighting/issues/245, https://github.com/zsh-users/zsh-syntax-highlighting/issues/919, https://github.com/zsh-users/zsh-syntax-highlighting/issues/310

### No highlighting update while cycling menu-completion
- Symptom: While tabbing through menu-completion candidates, command-line highlighting stays frozen from before completion started.
- Root cause: menu-select's internal cycling doesn't invoke a user-visible ZLE widget nor fire `zle-line-pre-redraw`, so highlighter callbacks never run during cycling.
- How zsh resolved it: Identified as fixable by adding another `redrawhook()` call point upstream in the menu-select code path.
- Source: https://github.com/zsh-users/zsh-syntax-highlighting/issues/375

### Cursor imprint/highlight artifacts not cleared on isearch accept
- Symptom: Accepting a command line from incremental-search's minibuffer leaves stale "cursor imprint" and partial-path highlighting artifacts because normal accept-line cleanup is bypassed.
- Root cause: isearch's line acceptance doesn't go through the `accept-*` widgets the highlighter hooks.
- How zsh resolved it: Fix identified as hooking `zle-line-finish` instead of relying solely on `accept-line` wrapping.
- Source: https://github.com/zsh-users/zsh-syntax-highlighting/issues/284

### Terminal cursor glyph disappears with a "cursor" highlighter active
- Symptom: With the `cursor` highlighter enabled, pressing left-arrow to move back through a typed command makes the terminal cursor become invisible (or renders the underlying char in italics in tmux) on several terminal emulators (gnome-terminal, urxvt).
- Root cause: Unknown — believed to be an interaction between the highlighter's cursor-cell styling escapes and specific terminal emulators' cursor rendering.
- How zsh resolved it: Open/terminal-dependent.
- Source: https://github.com/zsh-users/zsh-syntax-highlighting/issues/171

### Transient cursor jump/jitter on redraw (MSYS2/Windows terminal)
- Symptom: On MSYS2/Windows terminal, the cursor visibly snaps to line start and back on every redraw while typing the first few characters, producing flicker.
- Root cause: Unknown — attributed to plugin redraw calls interacting with Windows terminal cursor-positioning quirks.
- How zsh resolved it: Open.
- Sources: https://github.com/zsh-users/zsh-syntax-highlighting/issues/789, https://github.com/zsh-users/zsh-syntax-highlighting/issues/765

### Flicker from `zle reset-prompt` after OSC 133 shell-integration markers (powerlevel10k)
- Symptom: After p10k began emitting OSC 133 `k=r`/`k=s` markers, any widget calling `zle reset-prompt` (e.g. autocomplete/snippet-expansion) causes about one frame of visible flicker, even on terminals unrelated to the original iTerm2-specific gating.
- Root cause: OSC 133 mark emission tied to `reset-prompt`'s redraw path is unconditional rather than gated to the terminal it was meant for; switching to the lighter `zle -R` doesn't avoid it, indicating the flicker originates in the redisplay layer itself.
- How zsh resolved it: Open; workaround is pinning to the pre-change commit.
- Source: https://github.com/romkatv/powerlevel10k/issues/2959

### Menu-completion list corruption / segfaults after `reset-prompt`
- Symptom: Completion menu items disappear mid-scroll, or zsh segfaults, when `zle reset-prompt` is invoked while a completion menu is active.
- Root cause: ZLE's internal completion-menu state becomes invalidated by `reset-prompt`'s redraw, compounded by `precmd` hooks firing mid-menu and state-sync bugs in zsh ≤5.8.
- How zsh resolved it: Fixed upstream in zsh 5.8.1+ (synchronization patch between `reset-prompt` and the menu redraw cycle); older-zsh workaround is `zle -R` instead of `reset-prompt`.
- Source: https://linuxvox.com/blog/zsh-menu-completion-causes-problems-after-zle-reset-prompt/

---

## Key parsing

### KEYTIMEOUT forces a tradeoff between instant Escape and correct arrow/Alt-key parsing
- Symptom: Arrow keys, Alt-combinations, and other multi-byte sequences all begin with ESC; ZLE must wait up to `KEYTIMEOUT` (default 0.4s, in 1/100s units) after a lone ESC before deciding whether it was plain Escape or the start of a sequence. Setting `KEYTIMEOUT` very low to make Escape feel instant breaks multi-key vi-normal-mode bindings and can split/misinterpret arrow-key sequences under any latency (SSH, slow terminal, fast typing).
- Root cause: Escape-prefixed multi-byte sequences are structurally ambiguous with a standalone Escape keypress at the byte-stream level; classic zsh has no CSI-u/Kitty-keyboard-protocol style disambiguation from the terminal.
- How zsh resolved it: No structural fix — `KEYTIMEOUT` remains a user-tunable delay/accuracy tradeoff; users are warned not to set it to 0 since real inter-byte latency is never actually zero.
- Sources: https://www.johnhawthorn.com/2012/09/vi-escape-delays/, https://devedge.github.io/2025/05/09/eliminating-esc-delays-in-tmux-vim-and-zsh/

### Queued keystrokes during a busy foreground job break arrow-key "buffer edited" detection (zsh-history-substring-search)
- Symptom: If the user types while a long-running foreground command holds the terminal, then presses Up-arrow once the shell regains control, `zsh-history-substring-search` fails to trigger and falls back to plain history navigation; Page Up/Page Down bound to the same action work fine in the identical scenario.
- Root cause (as diagnosed by reporter): The plugin's "was the buffer edited since last search" detection isn't correctly updated when arrow-key input arrives from a backlog of queued bytes rather than a live keypress.
- How zsh resolved it: Open/unresolved; workaround is binding Page Up/Down instead.
- Source: https://github.com/zsh-users/zsh-history-substring-search/issues/43

### Delete key (`kdch1`) binding depends on terminfo correctness and terminal mode
- Symptom: The Delete key does nothing, or is confused with Backspace, in various terminal/terminfo combinations; common workaround is manually forcing `bindkey '^[[3~' delete-char` rather than trusting `${terminfo[kdch1]}`.
- Root cause: ZLE's terminfo-driven key bindings are only valid while the terminal is actually in the mode terminfo describes; mismatches between a terminal's real escape sequences and its advertised terminfo entry silently break the mapping.
- How zsh resolved it: No general core fix; standard advice is to manually bind the observed literal sequence.
- Sources: https://bbs.archlinux.org/viewtopic.php?id=175991, https://zshwiki.org/home/keybindings/

### Arrow keys stop matching after normal/application cursor-key mode gets out of sync
- Symptom: Arrow keys (and Home/End) produce different escape sequences depending on terminal cursor-key mode ("normal" vs "application"); if another program switches the mode and fails to restore it before returning control, zsh's bindings (expecting one specific sequence) silently stop matching.
- Root cause: Cursor-key mode is global, mutable terminal state controlled by escape sequences any program can send; ZLE binds only the sequence set for the mode it expects.
- How zsh resolved it: Documented workaround is binding both possible sequences per arrow key (e.g. both `\e[A` and `\eOA`) rather than fixing the mode-consistency problem itself.
- Sources: https://zsh.sourceforge.io/FAQ/zshfaq.txt, https://gitlab.com/gnachman/iterm2/-/issues/8743

---

## Multiplexers and remote

### tmux `TERM=screen`/`screen-256color` under-advertises capabilities, breaking Unicode rendering
- Symptom: Running zsh inside tmux over SSH shows garbled/mangled Unicode characters.
- Root cause: tmux sets `$TERM` to `screen`/`screen-256color` by default; that terminfo entry predates reliable UTF-8 support guarantees, so terminfo-driven rendering assumptions are wrong.
- How zsh resolved it: Not a zsh-side fix — standard guidance is to configure tmux/terminfo (`tmux-256color`, proper `default-terminal`).
- Source: https://xhinker.medium.com/fixing-garbled-unicode-characters-in-tmux-over-ssh-8a4d9239fa76

(See also "tmux pane resize corrupts prompt display via repeated SIGWINCH redraws" under Resize, which is also a multiplexer-specific finding.)

---

## Other

### `clear` doesn't actually purge terminal scrollback
- Symptom: Running `clear` under Oh My Zsh visually blanks the screen, but the user can still scroll up and see the "cleared" previous content.
- Root cause: Unclear — likely the terminal/terminfo `clear` sequence doesn't purge the scrollback buffer (distinct from an alternate-screen-based clear).
- How zsh resolved it: Open/unresolved; community workaround binds a key to also send the terminal's erase-scrollback sequence (`\e[3J`).
- Source: https://github.com/ohmyzsh/ohmyzsh/issues/9626

### Multi-line/transient prompt loses its first line on screen clear (powerlevel10k)
- Symptom: With a two-line prompt and transient-prompt enabled, clearing the terminal (Cmd-K in iTerm2, or `clear` inside tmux) redraws only the second/input line, dropping the first prompt line until Enter is pressed again.
- Root cause: The clear/redraw path re-renders only the "current" prompt segment it tracks, not the full multi-line prompt state.
- How zsh resolved it: Open in both reports.
- Sources: https://github.com/romkatv/powerlevel10k/issues/782, https://github.com/romkatv/powerlevel10k/issues/1390

### `reset-prompt` doesn't pick up state changes made by the calling widget
- Symptom: A custom widget that does `cd ..` then calls `zle .reset-prompt` doesn't show the updated working directory in the redrawn prompt until the next full prompt cycle.
- Root cause: Prompt-string expansion for `reset-prompt` is computed from state captured before the widget's own state change.
- How zsh resolved it: Documented as expected/inherent behavior, not fixed — prompt themes must explicitly re-trigger expansion.
- Source: https://github.com/romkatv/powerlevel10k/issues/72

### `zle redisplay`/`reset-prompt` erases a multi-line prompt down to one line (zsh 5.3 regression)
- Symptom: Calling `zle redisplay` from a widget (e.g. to show progress dots during completion) with a multi-line prompt erases the prompt to just the first line at the correct column, rather than fully restoring the multi-line prompt.
- Root cause: Unknown — reported as a regression vs. earlier zsh versions with no documented incompatibility.
- How zsh resolved it: Unresolved in the thread; only a full completion-menu selection forces a correct full redraw.
- Source: https://www.zsh.org/mla/users/2017/msg00018.html

### Multi-line ZLE buffer corrupted by an async "preprompt" re-render (downstream: Pure prompt)
- Symptom: In prompts (e.g. Pure) that asynchronously redraw a "preprompt" segment above the editable line, if the user's typed command itself spans multiple lines, the preprompt update writes to the wrong screen row and corrupts/overwrites the multi-line command buffer being edited.
- Root cause: The prompt's line-count bookkeeping tracks only lines used by the preprompt segment, not lines already consumed by the user's multi-line buffer content; further complicated because ZLE's `CONTEXT` parameter flushes prior buffer lines so simple line-counting can't recover the true cursor row.
- How zsh resolved it: Open — proposed fix is tracking buffer line counts across command continuations via ZLE widgets; not merged upstream at time reported.
- Source: https://github.com/sindresorhus/pure/issues/143

### Prompt duplication when the terminal window shrinks (iTerm2, downstream: ohmyzsh)
- Symptom: Shrinking the iTerm2 window causes the shell prompt to visibly render twice.
- Root cause: Unknown.
- How zsh resolved it: Open, unresolved.
- Source: https://github.com/ohmyzsh/ohmyzsh/issues/10750

### Redraw artifacts left after repeated window maximize/restore (powerlevel10k)
- Symptom: Repeatedly maximizing/unmaximizing the terminal window leaves stale prompt text at the old position after the prompt is redrawn at its new position.
- Root cause: Unknown — resize-driven redraw path doesn't erase the prior render region before drawing the new one.
- How zsh resolved it: Open.
- Source: https://github.com/romkatv/powerlevel10k/issues/1358

### Cursor tracking desyncs when editing a long/wrapped multi-line command in a narrow terminal
- Symptom: Editing a long command that wraps across multiple terminal rows (especially after recalling via up-arrow) makes the cursor's on-screen position an unreliable "blind guess," worse in narrow terminals; disabling Oh My Zsh entirely fixes it.
- Root cause: Unknown — implicated theme/plugin wrapping of ZLE redraw interacting with multi-row line-wrap cursor math.
- How zsh resolved it: Open; workaround is disabling Oh My Zsh.
- Source: https://github.com/ohmyzsh/ohmyzsh/issues/6606

### History incremental-search (Ctrl-R) buffer bleed
- Symptom: While using Ctrl-R history search and navigating with arrows or typing more search text, the first couple of characters from the searched line get prepended/duplicated onto the displayed search line.
- Root cause: Unknown — reported as a regression ("started happening out of nowhere"), not diagnosed further.
- How zsh resolved it: Open, unresolved.
- Source: https://github.com/ohmyzsh/ohmyzsh/issues/9197

---

## Coverage notes / gaps

- **RTL/bidirectional text**: No zsh-specific bug reports on RTL/bidi prompt or command-line rendering were found despite targeted searches (`site:zsh.org/mla` + bidi/RTL terms); this is a documented gap in what could be substantiated, not confirmed absence of the bug class.
- **GNU screen, mosh, Windows Terminal (native, non-WSL)**: No zsh-workers-specific threads were found for these; only tmux-specific and generic WSL/VS-Code shell-integration results turned up, and the latter weren't genuine ZLE bugs, so they're excluded.
- **SourceForge tracker**: Confirmed effectively unused for this bug class; all substantive material came from zsh-workers/zsh-users mailing lists, the ChangeLog/patch discussions, and downstream GitHub trackers.
- Two entries above are flagged lower-confidence in their own text: the vi visual-mode selection mismatch (title-only search hit) and the powerline vi-mode-indicator redraw report (downstream, not zsh-workers).
