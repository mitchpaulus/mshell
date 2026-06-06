"""Polars: read both CSVs, inner-join on id, write fingerprint."""

from pathlib import Path

import polars as pl

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "polars"
OUT.mkdir(parents=True, exist_ok=True)

left = pl.read_csv(HERE / "test_dataset.csv")
right = pl.read_csv(HERE / "join_sample.csv")
result = left.join(right, on="id", how="inner", suffix="_r")

lines = [
    f"rows\t{result.height}",
    f"sum_id\t{int(result['id'].sum())}",
    f"sum_value_floor\t{int(result['value'].floor().sum())}",
]

(OUT / "join.txt").write_text("\n".join(lines) + "\n")
