#!/usr/bin/env python3
"""Terminal width stress report.

Probes the *terminal emulator* (not mshell) with normal and adversarial
grapheme clusters, measures how many cells the cursor actually advanced for
each via CPR (ESC[6n), and prints a report:

  - inferred width model (cluster vs codepoint/legacy), VS16 behavior,
    East Asian ambiguous width
  - per-probe: how it renders, measured advance, and the cluster-model and
    codepoint-model predictions
  - the maximum advance ever observed for a single grapheme cluster, i.e.
    an empirical answer to "does this terminal ever move more than 2 cells
    for one cluster?"

Run it directly in the terminal you want to measure:

    ./width_report.py

Probes are erased as they are measured (CR + EL); the report prints after
the terminal is restored. The report's "renders" column is padded using the
*measured* cell width, so columns line up on the terminal under test.
Requires a tty; exits with a message otherwise.
"""

import os
import re
import select
import sys
import termios
import tty

CPR = "\x1b[6n"
TIMEOUT = 0.30   # seconds to wait for each CPR reply
RENDER_COL = 12  # cells reserved for the "renders" column

ZWJ = "‍"
VS15 = "︎"
VS16 = "️"


def build_probes():
    """Each probe: (group, label, string, cluster_pred, codepoint_pred).

    Labels are ASCII-only (the report aligns them with plain padding).
    Predictions are cells for the WHOLE string. None = no firm prediction
    (that's what we're here to find out).
    """
    person = "\U0001f9d1"        # neutral person
    family3 = "\U0001f468‍\U0001f469‍\U0001f466"          # man+woman+boy
    family4 = "\U0001f468‍\U0001f469‍\U0001f467‍\U0001f466"  # man+woman+girl+boy
    probes = [
        ("baseline", "ASCII 'a'", "a", 1, 1),
        ("baseline", "wide CJK", "世", 2, 2),
        ("baseline", "precomposed e-acute", "é", 1, 1),
        ("baseline", "e + combining acute", "e\u0301", 1, 1),

        ("class", "family (3, RGI)", family3, 2, 6),
        ("class", "family (4, RGI)", family4, 2, 8),
        ("class", "heart + VS16", "❤" + VS16, 2, 1),
        ("class", "umbrella + VS15", "☂" + VS15, 1, 2),
        ("class", "flag pair (US)", "\U0001f1fa\U0001f1f8", 2, 4),
        ("class", "thumbs up + skin tone", "\U0001f44d\U0001f3fd", 2, 4),
        ("class", "Hangul, 3 conjoining jamo", "한", 2, 2),
        ("class", "degree sign (ambiguous)", "°", None, None),

        ("uniseg-quirk", "two-em dash U+2E3A", "⸺", 1, 1),
        ("uniseg-quirk", "three-em dash U+2E3B", "⸻", 1, 1),

        ("adversarial", "3 regional indicators",
         "\U0001f1fa\U0001f1f8\U0001f1fa", 4, 6),
        ("adversarial", "zalgo: e + 12 marks",
         "e" + "̧̨́̀̂̃̄̆̈̊̋̌", 1, 1),
        ("adversarial", "lone ZWJ", ZWJ, 0, 0),
        ("adversarial", "lone VS16", VS16, 0, 0),
        ("adversarial", "lone combining acute", "́", 0, 0),
    ]
    # Synthetic ZWJ chains: one grapheme cluster of N people. Not RGI
    # sequences past a point, so partial clusterers show themselves here.
    for n in (2, 4, 8, 16, 32):
        s = ZWJ.join([person] * n)
        probes.append(("chain", f"ZWJ chain of {n} people", s, 2, 2 * n))
    return probes


class Term:
    def __init__(self):
        self.fd = sys.stdout.fileno()
        self.in_fd = sys.stdin.fileno()
        self.saved = termios.tcgetattr(self.in_fd)
        tty.setraw(self.in_fd)

    def restore(self):
        termios.tcsetattr(self.in_fd, termios.TCSADRAIN, self.saved)

    def write(self, s):
        os.write(self.fd, s.encode())

    def cpr(self):
        """Send CPR, return (row, col) or None on timeout/garbage."""
        self.write(CPR)
        buf = b""
        while True:
            r, _, _ = select.select([self.in_fd], [], [], TIMEOUT)
            if not r:
                return None
            buf += os.read(self.in_fd, 64)
            m = re.search(rb"\x1b\[(\d+);(\d+)R", buf)
            if m:
                return int(m.group(1)), int(m.group(2))


def measure(term, cols, probe):
    """Print probe at column 1, return cells advanced (may span rows)."""
    term.write("\r\x1b[K")
    start = term.cpr()
    if start is None:
        return None
    term.write(probe)
    end = term.cpr()
    # Erase everything the probe touched, back to the starting row.
    term.write("\r\x1b[K")
    if end is not None:
        for _ in range(end[0] - start[0]):
            term.write("\x1b[A\x1b[2K")
    if end is None:
        return None
    return (end[0] - start[0]) * cols + (end[1] - 1)


def render_cell(s, cells):
    """The probe itself, padded to RENDER_COL using its MEASURED width."""
    if cells is None:
        return " " * RENDER_COL
    if cells > RENDER_COL:
        return "(too wide)".ljust(RENDER_COL)
    return s + " " * (RENDER_COL - cells)


def cps(s, width):
    """Space-joined hex codepoints, truncated with an ellipsis to fit."""
    out = " ".join(f"{ord(c):04X}" for c in s)
    if len(out) > width:
        out = out[: width - 1].rstrip() + "\u2026"
    return out


def main():
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        print("width_report.py: needs a tty (run it directly in the terminal under test)")
        return 1

    size = os.get_terminal_size()
    cols = size.columns
    probes = build_probes()

    term = Term()
    results = []
    try:
        # Make room so multi-row wraps don't scroll and break row math.
        term.write("\n" * 4 + "\x1b[4A")
        if term.cpr() is None:
            term.restore()
            print("width_report.py: no CPR reply — this terminal does not answer ESC[6n; cannot measure.")
            return 1
        for group, label, s, cl, cp in probes:
            cells = measure(term, cols, s)
            results.append((group, label, s, cl, cp, cells))
    finally:
        term.write("\r\x1b[K")
        term.restore()

    # ---- report ----
    env = os.environ
    print("terminal width report")
    print(f"  TERM={env.get('TERM', '?')}  TERM_PROGRAM={env.get('TERM_PROGRAM', '-')}  cols={cols}")
    print()
    CP_COL = 28
    print(f"  {'':<12}  {'':<26}  {'':<{CP_COL}}"
          f"  {'':>4}  {'cluster':>7}  {'codept':>6}  {'':>8}")
    print(f"  {'group':<12}  {'probe':<26}  {'codepoints':<{CP_COL}}"
          f"  {'#cp':>4}  {'width':>7}  {'width':>6}  {'measured':>8}  {'verdict':<10}  renders")
    print(f"  {'-'*12}  {'-'*26}  {'-'*CP_COL}  {'-'*4}  {'-'*7}  {'-'*6}  {'-'*8}  {'-'*10}  {'-'*RENDER_COL}")

    max_cluster_advance = 0
    max_cluster_label = ""
    votes = {"cluster": 0, "codepoint": 0}
    for group, label, s, cl, cp, cells in results:
        if cells is None:
            verdict = "NO REPLY"
        elif cl is None:
            verdict = ""  # informational probe, no prediction
        elif cl == cp:
            verdict = "ok" if cells == cl else f"NEITHER ({cl}?)"
        elif cells == cl:
            verdict = "cluster"
            votes["cluster"] += 1
        elif cells == cp:
            verdict = "codepoint"
            votes["codepoint"] += 1
        else:
            verdict = "NEITHER"
        shown = "?" if cells is None else cells
        print(f"  {group:<12}  {label:<26}  {cps(s, CP_COL):<{CP_COL}}"
              f"  {len(s):>4}  {fmt(cl):>7}  {fmt(cp):>6}  {shown:>8}  {verdict:<10}  {render_cell(s, cells)}")
        # Single-cluster probes only (the RI-run probe is 2 clusters, skip it).
        if cells is not None and group in ("baseline", "class", "uniseg-quirk", "chain") \
                and cells > max_cluster_advance:
            max_cluster_advance = cells
            max_cluster_label = label

    print()
    if votes["cluster"] or votes["codepoint"]:
        model = "cluster" if votes["cluster"] >= votes["codepoint"] else "codepoint"
        print(f"  inferred model: {model}  (cluster-matching probes: {votes['cluster']},"
              f" codepoint-matching: {votes['codepoint']})")
        if votes["cluster"] and votes["codepoint"]:
            print("  MIXED RESULTS: this terminal is a partial clusterer — no single model fits.")
    amb = next((c for g, l, s, _, _, c in results if "ambiguous" in l), None)
    if amb is not None:
        print(f"  East Asian ambiguous width: {amb}")
    vs16 = next((c for g, l, s, _, _, c in results if l == "heart + VS16"), None)
    if vs16 is not None:
        print(f"  VS16 promotes to wide: {'yes' if vs16 == 2 else 'no' if vs16 == 1 else f'odd ({vs16})'}")
    print()
    print(f"  max advance for a single grapheme cluster: {max_cluster_advance}"
          f"  ({max_cluster_label})")
    if max_cluster_advance <= 2:
        print("  => on this terminal, no single cluster ever moved more than 2 cells.")
    else:
        print("  => this terminal advanced MORE than 2 cells for a single cluster;")
        print("     it renders cluster internals as separate glyphs (codepoint-style).")
    return 0


def fmt(v):
    return "-" if v is None else str(v)


if __name__ == "__main__":
    sys.exit(main())
