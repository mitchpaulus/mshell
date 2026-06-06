"""Pandas: read CSV, group by category aggregating value (count + floor-sum), write fingerprint."""

from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "pandas"
OUT.mkdir(parents=True, exist_ok=True)

df = pd.read_csv(HERE / "test_dataset.csv")
df["value_floor"] = np.floor(df["value"]).astype(np.int64)
agg = df.groupby("category", sort=True).agg(
    count=("value", "size"),
    sum_value_floor=("value_floor", "sum"),
)

lines = []
for cat, row in agg.iterrows():
    lines.append(f"count_{cat}\t{int(row['count'])}")
    lines.append(f"sum_value_floor_{cat}\t{int(row['sum_value_floor'])}")

(OUT / "groupby.txt").write_text("\n".join(lines) + "\n")
