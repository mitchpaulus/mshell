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
- [ ] Chunk 5 — layout: `layoutInto(dst, text, cursor, width, p) LayoutResult`;
      text = prompt+command pre-concatenated, cursor = byte offset, rows are
      no-copy slices with RowEnd enum (Final/SoftExact/SoftEarly; Hard in ch.6);
      dst reuse for zero-alloc repaints; CursorCol==width only when PendingWrap.
      PROPOSED. Open flag for phase 2: prompts with ANSI colors need SGR-aware
      width.
- [ ] Chunk 6 — RowEndHard: `\n`/`\r\n` clusters end the row (excluded from text
      and width; cursor-on-newline = end of row; trailing `\n` → empty final
      row). PROPOSED together with chunk 5.
- [ ] Chunk 7 — property tests: row widths ≤ terminal width, rows reconstruct the
      input, cursor always in bounds.

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

- 2026-08-15: workflow started; chunk 1 proposed.
- 2026-08-15: chunk 1 reworked for a branch-minimal ASCII hot path (len-1 byte
  dispatch, `isControlCluster` folded in); school fns named `widthByCluster` /
  `widthByCodepoints` (Mitchell's naming).
