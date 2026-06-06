"""Polars: read CSV, filter value > 50, write fingerprint."""

from pathlib import Path

import polars as pl

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "polars"
OUT.mkdir(parents=True, exist_ok=True)

df = pl.read_csv(HERE / "test_dataset.csv")
result = df.filter(pl.col("value") > 50)

lines = [
    f"rows\t{result.height}",
    f"sum_id\t{int(result['id'].sum())}",
    f"sum_value_floor\t{int(result['value'].floor().sum())}",
]

(OUT / "filter.txt").write_text("\n".join(lines) + "\n")
