#!/usr/bin/env python3
"""Apply the classification tables (rangeN.tsv) to mshell/Evaluator.go.

M and C sites are rewritten here. E and S sites are listed for hand edits.
Run from the repository root.
"""
import glob
import sys

path = "mshell/Evaluator.go"
lines = open(path).read().split("\n")

rows = {}
for tsv in sorted(glob.glob("ai/failure-kinds/range*.tsv")):
    for raw in open(tsv):
        raw = raw.rstrip("\n")
        if not raw.strip():
            continue
        parts = raw.split("\t")
        line, kind = int(parts[0]), parts[1].strip()
        if line in rows:
            sys.exit(f"{tsv}: line {line} listed twice")
        rows[line] = (kind, parts[2] if len(parts) > 2 else "")

sites = [i + 1 for i, text in enumerate(lines)
         if "state.FailWithMessage(" in text or "state.failPtr(" in text]
missing = [n for n in sites if n not in rows]
extra = [n for n in rows if n not in sites]
if missing or extra:
    sys.exit(f"missing: {missing}\nnot a site: {extra}")

repl = {
    "M": ("state.TypeMismatch(", "state.mismatchPtr("),
    "C": ("state.CheckedFailure(", "state.checkedPtr("),
}
manual = []
for n, (kind, note) in sorted(rows.items()):
    if kind in repl:
        plain, ptr = repl[kind]
        text = lines[n - 1]
        text = text.replace("state.FailWithMessage(", plain)
        text = text.replace("state.failPtr(", ptr)
        lines[n - 1] = text
    else:
        manual.append((n, kind, note))

open(path, "w").write("\n".join(lines))
counts = {}
for kind, _ in rows.values():
    counts[kind] = counts.get(kind, 0) + 1
print("counts:", counts)
for n, kind, note in manual:
    print(f"{n}\t{kind}\t{note}")
