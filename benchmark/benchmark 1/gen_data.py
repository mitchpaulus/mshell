"""Generate the benchmark CSVs. Seeded for reproducibility.

Usage:
    uv run gen_data.py            # 20,000,000 rows (default)
    uv run gen_data.py --rows 100000   # smaller smoke-test dataset

Produces:
    test_dataset.csv  -- main dataset
    join_sample.csv   -- 1/20 random sample of rows for the join benchmark
"""

import argparse
from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).resolve().parent


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--rows", type=int, default=20_000_000)
    ap.add_argument("--sample-frac", type=float, default=0.05)
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    rng = np.random.default_rng(args.seed)
    n = args.rows

    ids = np.arange(n, dtype=np.int64)
    categories = rng.choice(np.array(["A", "B", "C", "D"]), size=n)
    values = rng.uniform(0.0, 100.0, size=n)
    start = np.datetime64("2020-01-01T00:00:00")
    offsets = rng.integers(0, 5 * 365 * 24 * 3600, size=n, dtype=np.int64)
    timestamps = (start + offsets.astype("timedelta64[s]")).astype("datetime64[s]")

    df = pd.DataFrame(
        {
            "id": ids,
            "category": categories,
            "value": values,
            "timestamp": timestamps,
        }
    )

    main_path = HERE / "test_dataset.csv"
    df.to_csv(main_path, index=False, float_format="%.6f")

    sample_n = max(1, int(n * args.sample_frac))
    sample_idx = rng.choice(n, size=sample_n, replace=False)
    sample_idx.sort()
    sample = df.iloc[sample_idx]
    sample_path = HERE / "join_sample.csv"
    sample.to_csv(sample_path, index=False, float_format="%.6f")

    print(f"wrote {main_path} ({n:,} rows)")
    print(f"wrote {sample_path} ({sample_n:,} rows)")


if __name__ == "__main__":
    main()
