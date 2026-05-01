"""Pandas: read both CSVs, inner-join on id, write fingerprint."""

from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "out" / "pandas"
OUT.mkdir(parents=True, exist_ok=True)

left = pd.read_csv(HERE / "test_dataset.csv")
right = pd.read_csv(HERE / "join_sample.csv")
result = left.merge(right, on="id", how="inner", suffixes=("_l", "_r"))

lines = [
    f"rows\t{len(result)}",
    f"sum_id\t{int(result['id'].sum())}",
    f"sum_value_floor\t{int(np.floor(result['value_l']).sum())}",
]

(OUT / "join.txt").write_text("\n".join(lines) + "\n")
