"""Pandas: read the CSV, write a fingerprint summary."""

from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "pandas"
OUT.mkdir(parents=True, exist_ok=True)

df = pd.read_csv(HERE / "test_dataset.csv")

cat_counts = df["category"].value_counts().to_dict()
lines = [
    f"rows\t{len(df)}",
    f"sum_id\t{int(df['id'].sum())}",
    f"sum_value_floor\t{int(np.floor(df['value']).sum())}",
]
for cat in ("A", "B", "C", "D"):
    lines.append(f"cat_{cat}\t{int(cat_counts.get(cat, 0))}")

(OUT / "load.txt").write_text("\n".join(lines) + "\n")
