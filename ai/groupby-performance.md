# Grid groupBy performance: state of the art and benchmarks

Measured 2026-09-26 on an AMD FX-8350 (2012, 8 cores, 8MB L3, no AVX2), Go 1.27.
Dataset: the nushell book's dataframe benchmark CSV (NZ business demography, `Data7602DescendingYearOrder.csv`),
5,429,252 rows, columns `anzsic06` (str), `Area` (str), `year`, `geo_count`, `ec_count` (int).
Every query is `sum(geo_count)` grouped by the listed keys.
All engines agree on group counts and the total (239,193,105).

## Queries

| Query | Keys | Groups |
|---|---|---|
| Q1 | year | 21 |
| Q2 | Area | 2,314 |
| Q3 | anzsic06, year | 6,636 |
| Q4 | Area, year | 48,121 |
| Q5 | anzsic06, Area | 393,918 |

## Group-by only, single thread (ms, data already in memory, best of N)

| Engine | Q1 | Q2 | Q3 | Q4 | Q5 |
|---|---|---|---|---|---|
| SQLite 3.50.4, no index | 2091 | 3115 | 3566 | 4122 | 4708 |
| SQLite, covering index on keys | 452 | 516 | 636 | 615 | 758 |
| pandas 3.0.6 | 90 | 226 | 358 | 429 | 730 |
| pandas, categorical keys | 88 | 168 | 307 | 377 | 617 |
| PyArrow 25 (Acero) | 90 | 236 | 322 | 316 | 718 |
| Polars 1.44 (compat build) | 77 | 418 | 230 | 660 | 1280 |
| Polars, categorical keys | 78 | 253 | 190 | 508 | 1047 |
| DuckDB 1.5.5 | 45 | 169 | 181 | 224 | 457 |
| mshell `main` | 1679 | 3450 | 5224 | 5569 | 7523 |
| mshell `perf-grid-groupby-key` branch | 390–528 | 604–721 | 482–565 | 780–804 | 1921–1961 |
| mshell prototype (typed factorize + CSR) | 198 | 324 | 294 | 449 | 1076 |
| Go lab: typed keys, short strings packed (D2) | 38 | 128 | 107 | 171 | 215 |
| Go lab: dictionary-encoded string columns (E) | 37 | 18 | 48 | 58 | 37 |

mshell rows include the aggregation quotation `("geo_count" gridCol sum)` run once per group.
Lab rows include a vectorized `sums[gid[i]] += v[i]`.
Run-to-run noise on this machine is roughly ±20–30%.

Polars is surprisingly weak here; the compatibility build it needs on this CPU likely costs it.

## Group-by only, 8 threads (ms)

| Engine | Q1 | Q2 | Q3 | Q4 | Q5 |
|---|---|---|---|---|---|
| DuckDB | 10 | 63 | 68 | 77 | 211 |
| PyArrow | 15 | 37 | 46 | 84 | 598 |
| Polars | 43 | 223 | 150 | 355 | 620 |

## Ingest: CSV into an in-memory typed table (ms)

| Engine | 1 thread | 8 threads |
|---|---|---|
| SQLite `.import` | 5568 | — |
| pandas `read_csv` | 3505 | — |
| DuckDB `read_csv` | 2304 | 507 |
| Polars `read_csv` | 1355 | 268 |
| PyArrow `read_csv` | 883 | 263 |
| mshell `parseCsv` + `toGrid` + `updateCol toInt` | ~9000 | — |
| Go `encoding/csv` alone to `[][]string` | 2167 | — |
| Go lab: bytes to typed int / dictionary columns | 1366 | 431 |

## What SQLite does

SQLite has no hash aggregation.
Without an index, `EXPLAIN QUERY PLAN` shows `USE TEMP B-TREE FOR GROUP BY`:
it encodes every row as a record, sorts them, and aggregates runs of equal keys.
Cost barely depends on the number of groups, 2–5 s here.
With a covering index it scans the index in key order, 0.45–0.76 s here, plus the one-time index build.

## Where the time goes in mshell

1. **Key construction.** One byte key per row plus a `map[string]int` lookup (branch), or `json.Marshal` per row (`main`).
2. **Row lists.** A `[]int` per group, grown by `append`.
3. **Aggregation.** One quotation evaluation per group.
   `"col" gridCol` on a `GridView` boxes every value into an `MShellObject` list before `sum` runs.
   At 394K groups this is about 600 ms by itself.
4. **Ingest.** `parseCsv` builds 27M boxed strings and 5.4M list headers of 184 bytes each.
   `toGrid` then infers column types, and `updateCol (toInt ?)` runs one quotation per cell.

## Techniques from the state of the art, ranked for mshell

Sources: DuckDB (radix-partitioned hash aggregate, salted entries, perfect hash aggregate for small integer domains),
ClickHouse (a hash-table variant chosen per key type: direct arrays for 8/16-bit keys, packed fixed-width keys,
`StringHashMap` storing short strings as integers, LowCardinality dictionary keys),
Velox (`VectorHasher`: array mode, normalized-key mode, hash mode),
Arrow and Polars (per-row group ids, then vectorized aggregation kernels),
Müller et al., "Cache-Efficient Aggregation: Hashing Is Sorting" (SIGMOD 2015),
Leis et al., "Morsel-Driven Parallelism" (SIGMOD 2014),
Richter et al., "A Seven-Dimensional Analysis of Hashing Methods" (PVLDB 2015).

1. **Group ids, not per-row keys.** Map each row to a first-seen group id per column (factorize),
   then combine columns by packing codes (`a*cardB + b`).
   Use a dense array when the product of cardinalities is small, otherwise a uint64 open-addressing table.
   Integer columns with a small min–max range use a dense array directly, with no hashing (a perfect hash).
   Measured: Q1 292 → 38 ms and Q5 1184 → 215 ms in the lab.
2. **Dictionary-encoded string columns** (categorical or LowCardinality).
   Store `codes []int32` plus `dict []string` in `GridColumn`, built during CSV ingest or `toGrid`.
   String group-bys then become integer group-bys: Q2 18 ms and Q5 37 ms,
   faster than every engine measured here (they hash raw strings unless given categoricals).
   Memory for low-cardinality string columns drops from 16 bytes of header plus data per cell to 4 bytes.
   Building the dictionaries costs about 410 ms when done after the fact,
   but it is nearly free inside a typed ingest, since hits cost a map lookup and no allocation.
3. **Short strings packed into a uint64** (≤7 bytes, length in the top byte), ClickHouse-style.
   Integer hashing and comparison instead of string hashing: Q2 217 → 128 ms.
   Could extend to ≤15 bytes with a 128-bit key.
4. **CSR row lists.** Count per group, take prefix sums, fill one backing array,
   and give each group a subslice.
   One allocation instead of one per group: 34–92 ms for 5.4M rows.
5. **Built-in vectorized reducers** for sum, count, min, max and mean over a column:
   `sums[gid[i]] += v[i]`, 11–14 ms for 5.4M rows, versus ~600 ms of per-group quotation calls at high cardinality.
   This needs an API decision (see below).
6. **Typed CSV ingest.** Parse bytes straight into typed columns (int inference, dictionary strings),
   parallel by chunks at newline boundaries, with a fallback for quoted input.
   1.37 s single-threaded and 0.43 s on 8 threads, versus ~9 s today.
7. **Parallelism.** Worth it only after items 1–5.
   Naive per-chunk Go maps plus an ordered merge gave little here, even though chunk order preserves first-seen order.
   DuckDB and Morsel-style designs use thread-local fixed-size tables and radix partitioning for high cardinality.

## Tried and rejected

- **Sort-based grouping** (LSD radix sort on packed codes, then runs): 590–1140 ms, 2–20x slower than hashing at every cardinality here.
- **Consecutive-key cache** (reuse the previous row's lookup when the key repeats): helps only on sorted columns.
  `anzsic06` is sorted in this file (Q3 211 → 99 ms), but it hurts unsorted `Area` (217 → 299 ms).
  Not worth it unconditionally.
- **Custom open-addressing tables vs Go 1.24+ swiss-table maps:** only a 10–15% gain on integer keys.
  The big wins come from choosing the method by key type, not from the hash table itself.

## Open design questions

- **Built-in reducers.** An aggregation spec such as `{ "name": "s", "sum": "geo_count" }`,
  or reducer words that the evaluator can run vectorized. The alternative is pattern-matching quotations like `("col" gridCol sum)`.
- **Dictionary encoding visibility.** Should dictionary columns be an internal storage type only,
  like `COL_INT` / `COL_STRING` today, or be exposed to users as categoricals?
  Internal-only keeps the language simple.
- **Numeric promotion.** Grid literals and `gridAddCol` promote a column mixing `1` and `1.0` to float,
  while grid `+` is documented as "no numeric promotion". These paths are inconsistent today.

## Implemented: techniques 1–4 (2026-09-26)

- `mshell/GridGroup.go`: per-column factorization chosen by storage type
  (direct array for small int ranges, packed short strings, dictionary code remap, open-addressing uint64 table,
  byte keys for generic columns), pairwise code combining (direct array when the product of cardinalities is at most
  max(2n, 65536), otherwise the uint64 table), and CSR row lists. Used by `groupBy` and by `pivot` row keys.
- `COL_DICT_STRING` column storage (`DictCodes []int32`, `DictValues []string`), internal only.
  `optimizeColumnStorage` builds it for all-string columns, and falls back to plain `COL_STRING`
  once the distinct count reaches max(n/2, 1024), which is exact regardless of row order.
  Concat, extend, sort, `gridSetCell` and join keys all handle it; plain and dictionary strings concat to plain strings.

Measured on the same machine and dataset, best of 6 runs, before = this branch with the byte-key fix only:

| Step | Before | After |
|---|---|---|
| Q1 year, sum agg / keys only | 392 / 307 | 178 / 109 |
| Q2 Area | 614 / 471 | 222 / 82 |
| Q3 anzsic06, year | 464 / 386 | 219 / 140 |
| Q4 Area, year | 774 / 552 | 361 / 150 |
| Q5 anzsic06, Area | 1871 / 1285 | 945 / 327 |
| `toGrid` | ~2100–2560 | ~2600–2890 |
| Live heap after load | ~590 MB | 208 MB |

Remaining per-call cost in keys-only grouping is mostly allocating and filling the identity `sourceIndices`
and the CSR backing array (5.4M `int` each). With aggregation, the per-group quotation dominates Q5 (~600 ms),
which is technique 5.
