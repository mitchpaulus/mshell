# Interactive wrapping — chunk-by-chunk progress

Workflow: Claude proposes each small chunk in chat; Mitchell implements it by hand.
Claude does NOT write code to the repo. Phases per `ai/interactive-wrapping-plan.md`;
width semantics per design §05 in `ai/interactive-wrapping-design.html`.

## Phase 1: Width + Layout (pure functions)

- [x] Chunk 1 — `WidthParams`, `clusterWidth` skeleton: ASCII fast path, TAB
      stand-in (1), control stand-in (2); school branches stubbed.
      DONE 2026-08-15: implemented in `Main.go` (Mitchell prefers fewer files),
      tests deferred until schools are in.
- [ ] Chunk 2 — legacy wcwidth school → `widthByCodepoints`: feed uniseg one
      rune at a time ([4]byte+EncodeRune, alloc-free) and sum — probe-verified
      this reproduces the per-codepoint table (family [2 0 2 0 2]=6, ❤️=1,
      ZWJ/VS/combining=0). Sync `uniseg.EastAsianAmbiguousWidth` global from
      params in-function. Adds `rivo/uniseg@v0.4.7`. PROPOSED.
- [ ] Chunk 3 — cluster school → `widthByCluster`: uniseg's whole-cluster width
      directly (probe-verified: family=2, VS16=2, VS15=1, flag pair=2, amb via
      global); adjustment when Vs16Wide=false → strip U+FE0F, re-measure.
      (Chunk 2 DONE 2026-08-15: in Main.go + Main_test.go, test passing;
      leftover nits: uniseg misfiled as indirect in go.mod, "ambigous" typo,
      stale "chunks 2 and 3" stub comment.)
- [ ] Chunk 4 — segmentation: `clusters(s, p) iter.Seq2[string,int]` (Go 1.25
      range-over-func, uniseg state threaded across boundaries) + `stringWidth`;
      tests incl. flag-pair segmentation.
      DONE 2026-08-15 (raw loop, no iterator; ZwjJoins→WidthOnClusters rename
      agreed but NOT yet applied in code).
      (Chunk 3 DONE 2026-08-15: widthByCluster in Main.go, tests pass; per-call
      EastAsianAmbiguousWidth store kept — measured ~25ns, accepted.)
- [x] Chunk 5 — layout: `layoutInto(dst, text, cursor, width, p) LayoutResult`;
      text = prompt+command pre-concatenated, cursor = byte offset, rows are
      no-copy slices with RowEnd enum (Final/SoftExact/SoftEarly/Hard);
      dst reuse for zero-alloc repaints; CursorCol==width only when PendingWrap.
      DONE 2026-08-15: in Main.go with TestLayout. Open flag for phase 2:
      prompts with ANSI colors need SGR-aware width. Chunk 2/3 nits (go.mod
      indirect, typo, stale comment) confirmed cleaned up.
- [x] Chunk 6 — RowEndHard: `\n`/`\r\n` clusters end the row (excluded from text
      and width; cursor-on-newline = end of row; trailing `\n` → empty final
      row). DONE 2026-08-15: in layoutInto with TestLayoutHardNewline.
- [ ] Chunk 7 — property tests: seeded-rand generator over a stress alphabet
      (ASCII/controls/newlines/CJK/ZWJ/VS16/flags/combining/ambiguous), all
      param combos, cursor at every cluster boundary. Properties: rows tile the
      input (Hard rows skip exactly "\n"/"\r\n"), Width==stringWidth and
      ≤ terminal width, end types consistent (SoftEarly ⇒ next cluster wouldn't
      fit), cursor in bounds, PendingWrap ⇔ final Width==width. Full code
      provided 2026-08-16; implemented by Mitchell, caught the codepoint-model
      family-emoji overflow (4 people = 8 cells) on first run. Invariant
      corrections found: (a) CursorCol==width happens at end of ANY exactly-full
      row (e.g. cursor on \n after a full row), not only PendingWrap — renderer
      must key deferred-move on "end of full row"; (b) oversize-cluster rows —
      handled by chunk 7.5 below, after which row.Width ≤ termWidth is
      unconditional again.
- [ ] Chunk 7.5 — oversize clusters + width-2 floor (decided 2026-08-16,
      supersedes the earlier stand-in-filler idea): terminal width ≥ 2 is a hard
      assumption (renderer bails below it: paint nothing, input still works;
      layoutInto documents the precondition with a defensive clamp). A cluster
      wider than the total width (codepoint model only) SPLITS at codepoint-
      segment boundaries — segment = nonzero-width codepoint + zero-width
      followers, so segments are 1–2 cells and always fit. Whole-cluster
      placement stays the default; splitting engages only when cluster > total
      width. Codepoint widths are additive, so the row-honesty property survives
      rows that begin mid-cluster. Tests: drop checkWidth's single-cluster
      exemption (bound unconditional), unit cases: family at width 2 → four
      2-cell rows; family fits whole at line end when next row can hold it.
      LayoutRow.Text stays a no-copy subslice ALWAYS — stand-ins (▸, ^X) and
      SGR are paint-time emissions, never string rewrites.
      Em-dash override (found 2026-08-16): uniseg deliberately widths U+2E3A=3,
      U+2E3B=4, but both are EAW Neutral and glibc wcwidth=1 — terminals
      advance 1 cell. clusterWidth overrides both to 1 before model dispatch
      (else the unit-width ≤ 2 bound and the width-2 floor break, and any
      command containing ⸻ gets cursor drift at every width). Add ⸺/⸻ to the
      property-test pieces. Calibration (Phase 5) can verify the assumption
      per-terminal via probe.

## Phase 2: Renderer swap — not started
## Phase 3: Mode overhaul — not started
## Phase 4: Fenced query — not started
## Phase 5: Calibration — not started

## Terminology

Mitchell's rule: do NOT say "school" (design doc's term). Say "width model":
cluster model (`widthByCluster`) vs codepoint model (`widthByCodepoints`).
`WidthParams.ZwjJoins` renamed → `WidthOnClusters` (selects the model; the ZWJ
probe is just how it gets measured). Chunk 4 = raw loop, no iterator.

## Log

- 2026-08-16: Phase 2 design pinned in `ai/render-pipeline.html` (figures):
  opaque prompt measured by per-prompt CPR → startCol (no marker convention,
  no escape parsing; stronger than bash/zsh/fish, which all assume net-zero
  cursor movement); highlighting = style spans × layout rows intersected by
  byte offset in the painter (reset at row end, re-apply at row start; ghost
  joins layout text as a gray span); type vocabulary SourceText/PromptBytes/
  ScreenBytes/Cells/ByteOffset (defined types, not aliases — typed ints kill
  the promptLength+index bug class); cursor domain = grapheme-cluster gaps,
  encoded as ByteOffset always at a boundary; boundaries are DERIVED per
  keystroke, never stored (clusters unstable under edits: RI fusion) —
  future chunk: currentCommand []rune → SourceText + ByteOffset.
- 2026-08-15: workflow started; chunk 1 proposed.
- 2026-08-15: chunk 1 reworked for a branch-minimal ASCII hot path (len-1 byte
  dispatch, `isControlCluster` folded in); school fns named `widthByCluster` /
  `widthByCodepoints` (Mitchell's naming).
