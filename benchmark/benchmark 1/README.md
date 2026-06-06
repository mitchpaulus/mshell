Based on <https://pipeline2insights.substack.com/p/pandas-vs-polars-benchmarking-dataframe>.

# Benchmark 1: pandas vs. polars vs. mshell

End-to-end timing comparison across three implementations.
Each timing run includes process startup (`uv run` for Python, `mshell` binary launch),
CSV read, the operation, fingerprint computation, and writing the fingerprint to disk.

## Scenarios

The article runs five; we run four (sort skipped):

| scenario | what it does                                            |
| -------- | ------------------------------------------------------- |
| load     | read 20M-row CSV, write summary fingerprint             |
| filter   | filter rows where `value > 50`                          |
| groupby  | group by `category`, count + floor-sum of `value`       |
| join     | inner join main dataset against 1M-row sample on `id`   |

Each script writes a small **fingerprint** file rather than a full result CSV
(`out/<impl>/<scenario>.txt`).
The fingerprint contains a row count, integer sums, and per-category counts.
All sums use `floor(value)` so they are exact across implementations
(float associativity differs between libraries; integer sums do not).

## Layout

```
gen_data.py         seeded data generator
pyproject.toml      uv project (pandas + polars + numpy)
pandas/*.py         four pandas scripts
polars/*.py         four polars scripts
mshell/*.msh        four mshell scripts
run.msh             hyperfine driver
verify.msh          cross-impl fingerprint diff
out/<impl>/         per-impl fingerprint outputs
results/            hyperfine markdown/json + memory.tsv
```

## Running

```bash
# 1. generate the data once
uv run gen_data.py                     # 20M rows, ~1GB CSV
# uv run gen_data.py --rows 100000     # smaller smoke run

# 2. run the benchmark (mshell must be on PATH, or set $MSHELL)
mshell run.msh

# 3. verify all three impls produced identical fingerprints
mshell verify.msh
```

## Notes

- mshell's `parseCsv` returns all-string cells, so the mshell scripts include
  explicit `(toFloat?)`/`(toInt?)` `updateCol` calls before numeric work.
  This is part of the fair comparison: it is what a user would actually write.
- mshell does not currently have a Grid sort builtin, so the article's sort
  benchmark is omitted.
- Polars and pandas are run via `uv run`, so process startup and import cost
  are part of every measurement (this is the realistic workflow).
- Hyperfine settings: `--warmup 2 --runs 10`.
