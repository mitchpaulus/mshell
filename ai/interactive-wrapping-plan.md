# Interactive wrapping — implementation plan
Design doc of record: `ai/interactive-wrapping-design.html` (all §09 questions resolved 2026-08-15).

## Decisions locked (2026-08-15)

- Q1: calibration probe ships in v1 (daily driver is konsole, the cluster-but-silent class), built as the *last* phase, after the editor is trusted.
- Q2: the `⏎` partial-line mark is derived from the column field of the per-prompt CPR; skipped on timeout; last-column-with-pending treated as mid-line.
- Q3: ISIG stays off; `^C` remains an in-band byte.
- Q4: control bytes never enter the buffer (paste maps TAB → one space, keeps `\n`, strips other C0); display stand-ins TAB → `▸` U+25B8 (1 cell), other controls `^X` (2 cells); `clusterWidth` stays total.
- Q5: multiline editing in v1; Alt-Enter inserts a literal newline; pasted newlines are kept; Enter submits.
- Q6: tmux gets no special-casing — same ladder as any terminal.
- Dependency `github.com/rivo/uniseg` approved (added at v0.4.7).
- Windows: full support in v1 — one renderer, two mode backends (termios and console-mode), per design §08.
- Lone-ESC disambiguation: out of scope for this work.

## Phases

1. **Layout + width (pure functions, no behavior change).**
   New files `mshell/Width.go`, `mshell/Layout.go` and tests.
   `WidthParams`/`clusterWidth` per design §05; `Layout(prompt, command, cursor, width, widthFunc)` → rows + cursor(row, col) + pending-wrap flag.
   Property tests: row widths never exceed terminal width, concatenated rows reconstruct the input, cursor always lands in bounds.
2. **Renderer swap.**
   `Render()` repaints the region from the layout model; `ScrollDown`/`ClearScreen`/`ensurePromptNewline` become bookkeeping off the per-prompt CPR; render-path CPRs deleted (with them, the `getCurrentPos` re-entrancy).
   Delete the dead `StdinReader`/pause-channel code.
3. **Mode overhaul.**
   Replace `MakeRaw` with the hand-picked edit mode (§07 table, ONLCR on); delete the cooked/raw prompt dance; donate/steal lifecycle around command execution; bracketed paste with Q4/Q5 sanitization; Alt-Enter binding.
   Windows console-mode backend implemented here (same interface).
4. **Fenced query.**
   One batched write per prompt: CPR (+ DECRQM 2027 / XTVERSION when needed), DA1 fence, timeout; width-model selection ladder (env vars → 2027 → legacy fallback).
5. **Calibration.**
   Probe per design §05, on-disk cache keyed by terminal identity, `msh recalibrate`.

## Status

- Phase 1: in progress (2026-08-15).
