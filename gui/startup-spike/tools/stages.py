"""Median of each traced stage over several runs of the spike (stderr on stdin)."""
import sys, collections, statistics
runs = collections.defaultdict(list); order = []
for line in sys.stdin:
    parts = line.split(None, 2)
    if len(parts) == 3 and parts[1] == 'ms':
        k = parts[2].strip()
        if k not in order: order.append(k)
        runs[k].append(float(parts[0]))
for k in sorted(order, key=lambda k: statistics.median(runs[k])):
    v = runs[k]; print(f"{statistics.median(v):8.2f} ms  (min {min(v):7.2f}, max {max(v):7.2f})  {k}")
