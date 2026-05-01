"""Polars: read CSV, group by category aggregating count + floor-sum, write fingerprint."""

from pathlib import Path

import polars as pl

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "polars"
OUT.mkdir(parents=True, exist_ok=True)

df = pl.read_csv(HERE / "test_dataset.csv")
agg = (
    df.group_by("category")
    .agg(
        pl.len().alias("count"),
        pl.col("value").floor().sum().alias("sum_value_floor"),
    )
    .sort("category")
)

lines = []
for row in agg.iter_rows(named=True):
    cat = row["category"]
    lines.append(f"count_{cat}\t{int(row['count'])}")
    lines.append(f"sum_value_floor_{cat}\t{int(row['sum_value_floor'])}")

(OUT / "groupby.txt").write_text("\n".join(lines) + "\n")
