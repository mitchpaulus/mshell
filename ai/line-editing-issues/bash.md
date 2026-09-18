# bash / GNU readline interactive line-editing failure modes

Research compiled from bug-bash and bug-readline mailing list archives, the readline CHANGES
file, GitHub issue trackers (tmux, gdb, psql-adjacent tools, Ruby, Node), Stack Exchange /
Ask Ubuntu, and forum reports. Each entry is a distinct failure mode a line editor should
guard against.

---

## Width measurement

### Zero/ambiguous width Unicode (combining marks, ZWJ emoji, variation selectors)
- Symptom: Combining marks (Mn/Me category) and emoji built from multiple codepoints (ZWJ
  sequences, `U+FE0F` variation selector 16) are measured with the wrong on-screen column
  width, causing cursor/text misalignment after insert or delete.
- Root cause: wcwidth()-style width tables disagree with what terminal emulators actually
  render; some codepoints are "ambiguous width" and different implementations pick 0/1/2.
- How resolved: readline relies on the system `wcwidth()`/multibyte support added in 4.2/4.3;
  downstream libraries (e.g. Python's `wcwidth`) ship periodically-updated per-Unicode-version
  tables and bugfix releases; still an open moving target as Unicode adds new sequences.
- Sources: https://www.cl.cam.ac.uk/~mgk25/ucs/wcwidth.c , https://github.com/jquast/wcwidth ,
  https://www.cl.cam.ac.uk/~mgk25/unicode.html

### CJK / East Asian wide characters double-column mismatch
- Symptom: Double-width CJK characters cause cursor/block misalignment, overtyping, or
  1-column drift in prompts and editing, especially where "ambiguous width" characters are
  treated as single-width by the app but double-width by the terminal (or vice versa).
- Root cause: Application and terminal emulator disagree on whether ambiguous-width Unicode
  code points occupy 1 or 2 columns; assumption that all characters are 1 column wide.
- How resolved: Terminal-level configuration (e.g. PuTTY's "Treat CJK ambiguous characters as
  wide"); readline itself added multibyte-aware cursor-position calculation in 4.2+, refined
  through 7.0/8.x CHANGES entries for "redisplay of multibyte characters."
- Sources: https://www.codevat.com/articles/cjk-woes/ ,
  https://documentation.help/PuTTY/config-cjk-ambig-wide.html ,
  https://tiswww.case.edu/php/chet/readline/CHANGES

---

## Multibyte/cursor editing

### Backspace/Delete only removes one byte of a multibyte UTF-8 character
- Symptom: Typing a multibyte character (e.g. Cyrillic, accented Latin) then pressing
  Backspace once removes only the trailing byte, leaving a stray leading byte / garbled
  display; sometimes requires pressing Backspace twice.
- Root cause: The line discipline / display layer treated the multibyte sequence as N
  separate single-byte terminal cells rather than one logical character, so one erase
  operation only unwinds one byte; locale (`LANG`/`LC_CTYPE`) not set to UTF-8 exacerbates it.
- How resolved: Set `LANG`/`LC_CTYPE` to a UTF-8 locale; kernel tty `IUTF8` input flag; in
  readline itself, multibyte-aware editing added in readline 4.2/4.3 (`CHANGES`: "Added code
  to handle editing and displaying multibyte characters throughout the library").
- Sources: https://bugzilla.redhat.com/show_bug.cgi?id=142265 ,
  https://forums.freebsd.org/threads/backspace-doesnt-remove-utf-8-multibyte-characters-on-console.73817/ ,
  https://github.com/microsoft/terminal/issues/9205 ,
  https://forum.manjaro.org/t/backspace-deletes-only-part-of-a-unicode-character/99430

### Incomplete multibyte sequence inserted into line buffer
- Symptom: Typing a multibyte character across multiple keystrokes/bytes (e.g. fast paste or
  partial read) can leave an incomplete byte sequence in the edit buffer.
- Root cause: Read granularity vs. multibyte decode boundary mismatch.
- How resolved: readline 8.3 CHANGES: "Fix error that caused characters composing an
  incomplete multibyte character not to be inserted into the line."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### Forward/backward-word wrong for multibyte word characters
- Symptom: `M-f`/`M-b` (word motion) misjudge word boundaries in multibyte text.
- Root cause: Word-boundary logic originally byte-oriented, not codepoint/char-class aware.
- How resolved: readline 6.1 CHANGES: "Fixed forward-word and backward-word commands to work
  correctly with multibyte character words."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### psql/arrow-key history recall misplaces cursor with Unicode input
- Symptom: After typing Unicode characters and recalling history with Up-arrow, the cursor is
  placed incorrectly and new text insertion overwrites following characters instead of
  shifting them.
- Root cause: Suspected readline redisplay desync when combined with the host app's own
  handling of multibyte state (unconfirmed — reporter asks "is this a readline issue?").
- How resolved: Open/unresolved in the report; general guidance is to upgrade readline.
- Sources: https://github.com/microsoft/terminal/issues/6096 ,
  https://github.com/PostgresApp/PostgresApp/issues/165

---

## Wrapping and geometry

### Prompt escape sequences without `\[ \]` break line-wrap column counting (classic PS1 bug)
- Symptom: When PS1 contains raw ANSI color/escape sequences not wrapped in `\[...\]`,
  readline miscounts the visible prompt length, so long command lines wrap at the wrong
  column, characters overwrite the previous line, or history recall produces garbled text.
- Root cause: readline/bash counts prompt escape-sequence bytes as occupying screen columns
  because it cannot tell printing from non-printing characters unless told explicitly.
- How resolved: Wrap every non-printing/escape sequence in `\[` and `\]` in PS1 so readline
  excludes it from width calculations; documented in the bash FAQ.
- Sources: https://lists.gnu.org/archive/html/bug-bash/2001-03/msg00007.html ,
  https://lists.gnu.org/archive/html/bug-bash/2003-03/msg00037.html ,
  https://news.icourban.com/crypto-https-askubuntu.com/questions/1012763/wrapping-lines-bugs-when-trying-to-colour-terminal-ps1-even-when-escaping-non-p

### Time-escape (`\t`/`\T`/`\A`) at start of PS1 breaks wrap/redraw on long lines
- Symptom: With a PS1 beginning with a time escape, editing/recalling a long wrapped command
  via history produces corrupted/garbled displayed text.
- Root cause: Not conclusively identified by the reporter; suspected regression in prompt
  length recomputation when the prompt's expansion changes length between draws (clock
  ticking over) while a multi-line command is displayed.
- How resolved: Workaround only — remove time escapes from the start of PS1; no fix confirmed
  in thread.
- Sources: https://lists.gnu.org/archive/html/bug-bash/2014-01/msg00080.html

### `\[ \]` markers processed before command-substitution expansion in PS1
- Symptom: Zero-width escape markers embedded inside `$(...)` command substitution in PS1 are
  not honored, so dynamically generated invisible sequences are miscounted, breaking wrap.
- Root cause: bash resolves `\[`/`\]` in the raw PS1 template before expanding command
  substitutions, rather than on the fully expanded string.
- How resolved: Open at time of report; reporter's suggested fix (process markers after full
  expansion) not confirmed merged.
- Sources: https://lists.gnu.org/archive/html/bug-bash/2019-05/msg00002.html ,
  https://lists.gnu.org/archive/html/bug-bash/2019-05/msg00021.html

### Prompt longer than terminal width corrupts multi-line display
- Symptom: When the rendered prompt itself is longer than the terminal's column count (narrow
  window, long `\w`/branch name, etc.), bash prints fragments of the prompt onto the line
  above, duplicates prompt text, or spreads it across several increasingly truncated lines.
- Root cause: Prompt-wrap geometry calculation breaks down when prompt length exceeds one
  screen line and the terminal itself is also narrower than expected; `checkwinsize` does not
  help since the true problem is prompt content vs. width, not stale width.
- How resolved: No general fix; workaround is to shorten the prompt. Related readline
  CHANGES fixes narrowed specific cases: 5.2 "redisplay bug ... when the prompt was one
  character longer than screen width"; 4.2a "problem with display when the last line of a
  multi-line prompt was longer than screen width."
- Sources: https://bugs.launchpad.net/ubuntu/+source/gnome-terminal/+bug/1929751 ,
  https://forums.linuxmint.com/viewtopic.php?t=357562 ,
  https://tiswww.case.edu/php/chet/readline/CHANGES

### Multiline prompts with embedded newlines mis-redraw
- Symptom: Prompts containing literal newlines (multi-line PS1) redisplay incorrectly —
  extra blank lines, wrong cursor row, or partial overwrite — particularly during history
  search or on redraw.
- Root cause: Line-break bookkeeping for prompts spanning multiple physical lines was
  historically buggy in several distinct ways (cursor-row tracking, invisible-character runs
  crossing a line break).
- How resolved: Iteratively patched across many releases — readline CHANGES: 4.0 "SIGWINCH
  code so multiline prompts with escape sequences redraw correctly," 4.1 "redisplay bug when
  the prompt spanned multiple physical lines with invisible characters," 7.0 "several bugs in
  the code that calculates line breaks when expanding prompts that span several lines," 8.1
  "redisplay problem with a prompt string containing embedded newlines."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### Command taller than the screen (more lines than terminal height)
- Symptom: A command whose wrapped display exceeds the terminal's line count scrolls content
  off the top, and cursor-up navigation/redraw within the buffer breaks.
- Root cause: readline's redisplay model assumed the edited line(s) always fit on screen.
- How resolved: `horizontal-scroll-mode` (`.inputrc`) forces single-line, horizontally
  scrolling display instead of wrapping, avoiding the multi-screen-line case entirely;
  readline 8.0 CHANGES: "readline automatically switches to horizontal scrolling if the
  terminal has only one line;" 8.3 CHANGES: "added some support for lines that consume more
  than the physical number of screen lines."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### Pending-wrap / last-column cursor ambiguity
- Symptom: After a character fills exactly the last column, terminals differ on whether the
  cursor visually wraps immediately or stays "pending" until the next character — causing
  readline's internal cursor-position model to drift from the terminal's actual cursor,
  producing misplaced edits at the line's right edge.
- Root cause: DEC-style "deferred autowrap" behavior is a terminal-emulator implementation
  detail not uniformly exposed to applications.
- How resolved: readline computes wrap points itself from `COLUMNS` and avoids relying on
  terminal auto-wrap timing in most paths, but edge-of-screen bugs recur across versions
  (e.g. 8.0 CHANGES "fixed a line-wrapping issue that caused problems for some terminal
  emulators").
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

---

## Prompt content

### PS2 continuation-prompt interaction with wrapped multi-line commands
- Symptom: In a multi-line command (heredoc, unclosed quote, etc.) the `PS2` continuation
  prompt combined with long wrapped lines produces prompt text intermixed incorrectly with
  command text, or the wrong prompt length is used for the wrap calculation on continuation
  lines.
- Root cause: Continuation-line prompt width must be recomputed and tracked per physical
  line, similar to the main prompt bug but per-PS2 redraw.
- How resolved: General guidance is the same `\[ \]` escaping discipline as PS1; no dedicated
  standalone fix identified beyond the general multi-line prompt fixes in readline CHANGES.
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES ,
  https://www.gnu.org/software/bash/manual/html_node/Readline-Init-File-Syntax.html

### Digit-argument / mode-string prompt swap leaves stale display
- Symptom: After using a numeric argument prefix (`M-3 M-5 ...`) or switching vi
  insert/command mode indicator, returning to the normal prompt leaves redisplay artifacts.
- Root cause: The temporary prompt substitution (e.g. `(arg: N)`) and the mode-string
  indicator were not always correctly un-rendered when swapping back to the base prompt.
- How resolved: readline 6.2/8.2 CHANGES: "fixed a redisplay problem that occurred when
  switching from the digit-argument prompt back to the regular prompt;" 6.3 CHANGES: "fixed a
  bug causing mode strings to be displayed incorrectly if the prompt was shorter than the
  mode string."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

---

## Resize

### SIGWINCH silently lost / not delivered to shell
- Symptom: After resizing the terminal window, the shell keeps using stale dimensions
  (garbled wrapping, wrong wrap column) until some other event (e.g. running an external
  command) refreshes them; switching shells and back can "fix" it.
- Root cause: The SIGWINCH handler may not yet be installed when the signal arrives, or the
  signal is not delivered/processed reliably depending on shell state and how it was invoked
  (script vs interactive, subshell chains).
- How resolved: No definitive fix in the reported thread; `shopt -s checkwinsize` mitigates by
  re-checking size after each foreground command completes (not during editing/builtins).
- Sources: https://lists.gnu.org/archive/html/bug-bash/2007-01/msg00084.html

### readline no longer redraws prompt on resize (readline 6.3 signal-handling change)
- Symptom: Upgrading to readline 6.3, the prompt/line stops reflowing immediately when the
  terminal is resized; it only updates once the user provides more input.
- Root cause: Deliberate architecture change — readline 6.3 stopped doing work directly in
  the SIGWINCH signal handler (to avoid unsafe `malloc` calls from a signal handler, which
  could deadlock against glibc's malloc), deferring the redraw until the next `read()`
  returns; since SIGWINCH does not interrupt `read(2)`, no redraw happens until then.
- How resolved: Maintainer (Chet Ramey) called this intentional; recommends terminal
  emulators handle their own on-resize redraw rather than depending on synchronous readline
  behavior. Documented as a real behavior change, not purely a "bug."
- Sources: https://lists.gnu.org/archive/html/bug-readline/2014-05/msg00005.html

### Pane resize under tmux/screen corrupts bash/zsh prompt
- Symptom: Resizing or splitting a tmux pane containing an interactive shell corrupts the
  displayed prompt, especially with large or multi-line prompts (e.g. right-sided prompts);
  parts of the old prompt remain from a previous terminal width.
- Root cause: On redraw, bash/zsh start redrawing from the beginning of the *current terminal
  line*, not the logical "buffer line" as tracked by the multiplexer; if the line has
  reflowed due to the new width, part of the old prompt persists in what is now a different
  physical line.
- How resolved: Open issue in tmux tracker; general workaround is `Ctrl-L` to force a full
  redraw after resizing.
- Sources: https://github.com/tmux/tmux/issues/516

### Readline reads screen size only at startup (pre-checkwinsize bash 2.x)
- Symptom: If the terminal is resized (especially narrower) after the shell starts and before
  `checkwinsize` support existed, cursor position computed by readline no longer matches the
  terminal's real cursor position, causing garbage during editing.
- Root cause: Screen dimensions were cached once at start rather than re-queried.
- How resolved: `shopt -s checkwinsize` (bash 2.x+) re-reads terminal size after each
  foreground job so `COLUMNS`/`LINES`, and therefore readline's wrap math, stay current.
- Sources: https://bugs.launchpad.net/ubuntu/+source/gnome-terminal/+bug/1929751

### readline SIGWINCH handler racing with in-progress redisplay causes core dump
- Symptom: A resize signal arriving at an unlucky moment during redisplay could corrupt
  internal state or crash the process.
- Root cause: Non-reentrant redisplay code invoked again from within a signal handler mid-way
  through a redraw.
- How resolved: readline 5.0 CHANGES: "fixed the redisplay code to avoid core dumps from
  poorly-timed SIGWINCH signals;" 6.0 CHANGES: "SIGWINCH signal handler now avoids calling
  redisplay code if one arrives during redisplay;" 6.3: moved SIGWINCH work out of the
  handler entirely (see above).
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

---

## Terminal queries and input races

### keyseq-timeout / ESC ambiguity misfires
- Symptom: Pressing Esc alone should enter vi command mode promptly, but bound multi-key
  Esc-prefixed sequences (e.g. custom `\eBi` bindings) incur a spurious ~500ms delay even
  though the sequence is unambiguous by hand-typing speed; conversely, arrow-key escape
  sequences (`ESC [ A` etc.) can be misread as a lone Esc if keystrokes/bytes arrive slower
  than the timeout, especially over high-latency links.
- Root cause: readline cannot distinguish "a human typed Esc, pause, then more keys" from
  "a terminal is sending a multi-byte escape sequence" without a timeout; the default
  `keyseq-timeout` (500ms) is a heuristic, not a certainty, and interacts oddly with some
  custom keybindings even when the sequence given is technically unambiguous.
- How resolved: `keyseq-timeout` is user-tunable in `.inputrc`; setting it to 0 makes readline
  wait indefinitely for the disambiguating key (trading responsiveness of plain Esc for
  reliability of sequences). A separate report shows `keyseq-timeout 0` unexpectedly still
  resolves Esc immediately to vi-movement-mode in some cases, contradicting documented
  behavior — status unresolved in thread.
- Sources: https://lists.gnu.org/archive/html/bug-readline/2015-07/msg00017.html ,
  https://lists.gnu.org/archive/html/bug-readline/2024-02/msg00000.html ,
  https://man7.org/linux/man-pages/man3/readline.3.html

### Delete key / kdch1 terminfo mismatch
- Symptom: The Delete key prints a literal escape sequence (e.g. `^[[3~`) into the buffer
  instead of deleting a character, or Backspace/Delete are swapped.
- Root cause: bash/readline binds keys from `.inputrc`/termcap defaults, and if the terminal's
  `TERM`/terminfo `kdch1` (delete-character) or `kbs` (backspace) capability is missing,
  wrong, or doesn't match what the terminal emulator actually sends, the key press is not
  recognized as a bound function.
- How resolved: Workaround — explicitly set/correct `kdch1`/`kbs` in the terminfo entry, or
  bind the raw sequence in `.inputrc` (e.g. `"\e[3~": delete-char`); not a readline-internal
  fix, this is a terminal-database/config mismatch class of bug.
- Sources: https://tldp.org/HOWTO/pdf/BackspaceDelete.pdf ,
  https://www.jimbrooks.org/web/linux/docs/configuration/BackspaceAndDeleteConfigurationForLinux.php

### Bracketed-paste mode desync (mode toggle sequence dropped/reordered)
- Symptom: The shell appears to enter or stay in bracketed-paste mode incorrectly — pasted
  text arrives wrapped in stray bracket markers, or normal typed input is misinterpreted as a
  paste, especially over layers that re-parse/re-emit VT sequences (e.g. Windows ConPTY).
- Root cause: readline toggles bracketed paste on/off around each read cycle by emitting
  `ESC[?2004h` / `ESC[?2004l`; if one of these control sequences is dropped, delayed, or
  reordered by an intermediary (multiplexer, ConPTY, wrapper), the terminal's paste-mode
  state and readline's assumed state diverge — a race, not a configuration bug.
- How resolved: Documented as inherently fragile when anything reprocesses the VT stream;
  no universal fix — depends on the intermediary passing control sequences through faithfully
  and in order.
- Sources: https://github.com/arthjean/paneflow/issues/65 ,
  https://forum.cursor.com/t/run-terminal-cmd-tool-adds-incorrect-bracketed-paste-markers-2728/52558

---

## Paste

### Pasted multi-line text auto-executes each line (no bracketed paste)
- Symptom: Pasting a multi-line command block into bash executes each line as soon as its
  newline is seen, rather than treating the paste as one atomic insertion — can be a
  correctness and security hazard (e.g. pasted text containing an unexpected command).
- Root cause: Without bracketed paste, the terminal has no way to tell the application "this
  input came from a paste, not keystrokes," so embedded newlines are processed exactly as
  Enter keypresses.
- How resolved: Bracketed paste mode (readline 6.3+, enabled by default since 8.0) has the
  terminal wrap pasted text in `ESC[200~ ... ESC[201~`; readline then inserts the whole block
  as literal text without treating embedded newlines as "submit," requiring one extra Enter
  to actually run it.
- Sources: https://groups.google.com/g/gnu.bash.bug/c/xyDWdkwxr7U ,
  https://en.wikipedia.org/wiki/Bracketed-paste , https://tiswww.case.edu/php/chet/readline/CHANGES

### Bracketed paste interacting badly with incremental search / vi overstrike / char-search
- Symptom: Pasting while in reverse-i-search, vi overstrike mode, or during a character-search
  command (`f`/`t` in vi mode) either inserted extra/duplicated characters, or broke out of
  the mode incorrectly.
- Root cause: Bracketed-paste insertion path was implemented separately from, and initially
  not integrated with, these special input-consuming modes.
- How resolved: readline 7.0 CHANGES: "fixed a bug with pasting text into an incremental
  search string if bracketed paste mode is enabled;" "fixed a bug with bracketed-paste
  inserting more than one character and interacting with other functions;" 8.0 CHANGES:
  "bracketed paste mode works in more places: incremental search strings, vi overstrike mode,
  character search."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### Pasted newline handling debated (execute vs. literal insert vs. strip)
- Symptom: Users disagree/are surprised whether a paste containing a trailing newline should
  submit the command immediately, insert a literal newline, or have the trailing newline
  stripped; behavior differs between bash/readline and zsh.
- Root cause: No single "correct" UX for paste-with-newline; readline's design (require an
  explicit Enter after a bracketed paste) is a deliberate safety choice, not a bug, but
  surprises users expecting "paste = run."
- How resolved: Design decision, not a fix; zsh separately discussed and implemented
  optionally chopping trailing newlines from pasted text.
- Sources: https://groups.google.com/g/gnu.bash.bug/c/xyDWdkwxr7U ,
  https://www.zsh.org/mla/workers/2015/msg02360.html ,
  https://bugs.launchpad.net/ubuntu-online/+bug/1970644

---

## Completions/search redraw

### Reverse-i-search (`Ctrl-R`) display garbling with multibyte input
- Symptom: Entering multibyte characters while in an incremental reverse search can corrupt
  the displayed search prompt/string, or return malformed bytes for the match.
- Root cause: The i-search prompt-plus-match redisplay path was a separate code path from
  normal-line redisplay and lagged behind multibyte-awareness fixes.
- How resolved: readline 8.2 CHANGES: "fixed a problem with restoring the prompt when
  aborting an incremental search;" 6.2 CHANGES: "off-by-one error in the prompt printed when
  performing searches;" general multibyte redisplay fixes (7.0, 8.x) applicable.
- Sources: https://lists.gnu.org/archive/html/bug-readline/2015-05/msg00014.html ,
  https://tiswww.case.edu/php/chet/readline/CHANGES

### Completion listing doesn't restore/redraw the edited line correctly
- Symptom: After a completion helper (e.g. fzf integration) prints its own UI and exits, the
  original command line is not correctly redrawn — line appears truncated or missing typed
  text until a manual redraw (`Ctrl-L`/`redraw-current-line`) is issued.
- Root cause: External tools that take over the terminal for a completion UI must explicitly
  invoke readline's line-redraw on return; when they don't (or an intermediate library
  swallows the redraw call), stale display persists.
- How resolved: Application-level workaround — call redraw-current-line (e.g. bound to
  `Alt-R`) after returning control to readline.
- Sources: https://github.com/junegunn/fzf/issues/1134

### `.inputrc` `show-all-if-ambiguous`/`show-all-if-unmodified` corrupt display inside tmux
- Symptom: Certain readline completion-listing settings cause visible screen corruption
  specifically when running inside tmux.
- Root cause: Interaction between readline's completion-listing screen writes and tmux's own
  screen-buffer tracking; not fully diagnosed in the report.
- How resolved: Open issue; workaround is to disable those `.inputrc` settings under tmux.
- Sources: https://github.com/profanity-im/profanity/issues/681

---

## Key parsing

### `Ctrl-L` only clears the visible screen, not scrollback (frequent user confusion)
- Symptom: Users expect `Ctrl-L` to fully "clear" the terminal like the `clear` command, but
  it only redraws/resets the visible screen, leaving scrollback intact and reachable by
  scrolling up — reported repeatedly as "not working."
- Root cause: `Ctrl-L` is bound in readline to `clear-screen`, which resets cursor to top and
  redraws the current line — it was never meant to purge scrollback.
- How resolved: Not a bug — documented distinction; `clear`/`tput reset` or terminal-specific
  scrollback-clear is the correct tool for that separate goal.
- Sources: https://techglimpse.com/clear-screen-keyboard-shortcut-bash-shell/ ,
  https://anagogistis.com/notes/clear/

### Quoted-insert (`Ctrl-V`/`Ctrl-Q`) needed to insert literal control chars (e.g. Tab, `^S`/`^Q`)
- Symptom: Typing Tab or other control characters directly is intercepted by readline for
  completion/flow-control rather than inserted literally; `Ctrl-V` (or `Ctrl-Q`) followed by
  the character is required to insert it literally, which is non-obvious to users.
- Root cause: By design, most control characters are bound to editing functions, not literal
  self-insert; a prior bug additionally prevented `Ctrl-V` from working more than once in a
  session, and prevented inserting `^S`/`^Q` via quoted-insert at all.
- How resolved: readline 4.2 CHANGES: "fixed the tty code so that ^V works more than once and
  ^S and ^Q can be inserted with quoted-insert;" later versions added a numeric-argument form
  (`-N` quoted-insert) to insert N literal characters at once.
- Sources: https://en.wikipedia.org/wiki/Control-V , https://tiswww.case.edu/php/chet/readline/CHANGES

---

## Multiplexers and remote

### `TERM`/wrapped escape sequences break inside tmux's `\ePtmux;...\e\\` passthrough
- Symptom: A vi-mode indicator or other escape sequence that displays fine outside tmux
  breaks the prompt specifically when tmux wraps/forwards it, especially visible when
  scrolling bash history — stale text from a previous command remains and obscures the
  currently typed line.
- Root cause: tmux's DCS passthrough wrapper for pass-through escape sequences interacts
  badly with readline's assumptions about which bytes are visible vs. control, corrupting the
  redraw bookkeeping.
- How resolved: Open issue in tmux tracker at time of research; no confirmed upstream fix.
- Sources: https://github.com/tmux/tmux/issues/1684

### Terminal type set to "dumb"/unknown disables features silently
- Symptom: Under some remote/multiplexer/CI contexts `TERM` ends up as `dumb` or an unknown
  value, and readline silently disables bracketed paste (and other terminal-dependent
  behavior) rather than erroring, which can confuse users who don't realize why paste/redraw
  behaves differently than on their normal terminal.
- Root cause: readline treats unrecognized/`dumb` terminals conservatively since it cannot
  verify escape-sequence support.
- How resolved: By design/documented — readline 6.0 CHANGES: "terminals named 'dumb' or
  unknown do not enable bracketed paste by default."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### tmux requiring constant forced redraws over SSH
- Symptom: Over an SSH connection to a remote tmux session, the display periodically needs a
  manual forced redraw (`Ctrl-b :refresh-client` or similar) to stay in sync with what the
  shell inside believes it has drawn.
- Root cause: Reported as possibly related to incorrect `$TERM` negotiation between local and
  remote sides rather than a readline bug per se, but it manifests as line-editor display
  desync from the user's perspective.
- How resolved: Open/workaround (manual refresh, correcting `$TERM`); not a readline-side fix.
- Sources: https://github.com/cmderdev/cmder/issues/1178

### Mosh's client-side speculative local echo diverges from server state during editing
- Symptom: When typing into the middle of an existing line (or during rapid edits) over Mosh,
  locally predicted/underlined text can visually diverge from the authoritative line state
  until the server's real response arrives and overwrites the prediction.
- Root cause: Mosh intentionally predicts and locally echoes keystrokes (including line
  editing) before server acknowledgment to hide latency; on packet loss/reordering or
  editing-in-the-middle cases the prediction can be wrong and is visibly corrected later.
- How resolved: By design — predictions are visually marked (e.g. underlined) as unconfirmed;
  divergence is expected and self-corrects once server state arrives, not treated as a bug to
  "fix" away.
- Sources: https://mosh.org/ , https://raphamorim.io/mosh-a-replacement-for-ssh/

### GDB TUI: readline writes directly to stdout, garbling curses window
- Symptom: When GDB's curses-based TUI is active, readline's own output (if not fully
  redirected through GDB's display handler) can be written directly to the terminal,
  corrupting the curses-managed screen layout.
- Root cause: GDB's own source comments note readline historically was "not clean in its
  management of output," occasionally writing to stdout even when a custom redisplay handler
  is installed.
- How resolved: GDB works around this by redirecting/intercepting all readline output itself
  rather than relying on readline to never touch stdout directly.
- Sources: https://fossies.org/linux/gdb/gdb/tui/tui-io.c

---

## Other

### Custom redisplay functions (embedders) fight with readline's own redraw assumptions
- Symptom: Applications that install a custom readline redisplay function (e.g. to add a
  right-hand-side prompt, syntax highlighting, etc.) see display corruption or double-draws,
  particularly interacting with signal-driven redraws (SIGWINCH) or multi-line prompts.
- Root cause: readline's redisplay internals assumed its own default drawing model in several
  code paths; custom redisplay hooks are a secondary, less-tested path.
- How resolved: Iteratively patched — readline 4.3 CHANGES: "fixed the display code to work
  better when applications use custom redisplay functions;" recurs as apps add features
  (fzf, IPython, etc. all layer custom redisplay).
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

### Stray control/garbage bytes from a misbehaving program corrupt the terminal for the next prompt
- Symptom: After a program prints raw/unescaped control bytes (or crashes mid-escape-sequence)
  the shell's next prompt renders as garbage (wrong colors, invisible input, wrapped
  incorrectly) even though the shell itself did nothing wrong.
- Root cause: Terminal is left in a modified state (alternate charset, disabled echo, changed
  colors, etc.) by the prior program's incomplete output; the next readline prompt is drawn
  on top of that corrupted state.
- How resolved: Not a readline bug per se; standard remedies are `reset`, `stty sane`, or
  blind-typing `reset<Enter>`; documented as a recurring, expected class of issue.
- Sources: https://www.cyberciti.biz/tips/bash-fix-the-display.html

### Fuzzed/out-of-bounds memory reads in the line-editing/redisplay engine
- Symptom: Certain crafted or unusual input sequences to the editing engine could trigger
  reads before the start of the prompt/line buffer.
- Root cause: Pointer arithmetic in the C redisplay/movement code without sufficient bounds
  checks for edge cases (e.g. very large repeat counts, empty lines, freed memory reuse).
- How resolved: Patched across several releases after fuzzing: readline 4.0 "point could be
  less than zero when rl_forward was given very large arguments," 4.1/4.2a/5.1 "rl_forward /
  rl_backward could set point before the beginning of the line," 5.0 "fixed out-of-bounds and
  free memory read errors found via fuzzing with random input," 5.1 "readline could reference
  freed memory when attempting to display the prompt."
- Sources: https://tiswww.case.edu/php/chet/readline/CHANGES

---

## Summary of counts by section

- Width measurement: 2
- Multibyte/cursor editing: 4
- Wrapping and geometry: 6
- Prompt content: 2
- Resize: 4
- Terminal queries and input races: 3
- Paste: 3
- Completions/search redraw: 3
- Key parsing: 2
- Multiplexers and remote: 5
- Other: 3

**Total distinct failure modes: 37**
