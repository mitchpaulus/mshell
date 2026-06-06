"""Polars: read the CSV, write a fingerprint summary."""

from pathlib import Path

import polars as pl

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "polars"
OUT.mkdir(parents=True, exist_ok=True)

df = pl.read_csv(HERE / "test_dataset.csv")

rows = df.height
sum_id = int(df["id"].sum())
sum_value_floor = int(df["value"].floor().sum())
cat_counts = (
    df.group_by("category")
    .agg(pl.len().alias("n"))
    .sort("category")
)
counts = {row[0]: int(row[1]) for row in cat_counts.iter_rows()}

lines = [
    f"rows\t{rows}",
    f"sum_id\t{sum_id}",
    f"sum_value_floor\t{sum_value_floor}",
]
for cat in ("A", "B", "C", "D"):
    lines.append(f"cat_{cat}\t{counts.get(cat, 0)}")

(OUT / "load.txt").write_text("\n".join(lines) + "\n")
