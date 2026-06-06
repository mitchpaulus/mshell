"""Pandas: read CSV, filter value > 50, write fingerprint."""

from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "pandas"
OUT.mkdir(parents=True, exist_ok=True)

df = pd.read_csv(HERE / "test_dataset.csv")
result = df[df["value"] > 50]

lines = [
    f"rows\t{len(result)}",
    f"sum_id\t{int(result['id'].sum())}",
    f"sum_value_floor\t{int(np.floor(result['value']).sum())}",
]

(OUT / "filter.txt").write_text("\n".join(lines) + "\n")
