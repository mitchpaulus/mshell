"""Generate grapheme comparison cases: random strings of code points drawn
from every range of the grapheme break property table (src/grapheme_tables.rs,
from uniseg), ASCII, and random code points from all of Unicode, so each
rule of the segmentation meets each property. Same format as lexcases.py.

Usage: python3 -I tools/graphemecases.py OUT [--seed N] [--count N]
"""
import os, random, re, struct, sys

out = sys.argv[1]
seed = int(sys.argv[sys.argv.index('--seed') + 1]) if '--seed' in sys.argv else 1
count = int(sys.argv[sys.argv.index('--count') + 1]) if '--count' in sys.argv else 200000
here = os.path.dirname(os.path.abspath(__file__))
table = open(os.path.join(here, '..', 'src', 'grapheme_tables.rs')).read()
ranges = [(int(a, 16), int(b, 16)) for a, b in re.findall(r'\(0x([0-9A-F]+), 0x([0-9A-F]+), \d+\)', table)]

def valid(c):
    return not (0xD800 <= c <= 0xDFFF)

rng = random.Random(seed)
def pick():
    r = rng.random()
    if r < 0.6:
        lo, hi = rng.choice(ranges)
        c = rng.randint(lo, hi)
    elif r < 0.8:
        c = rng.randint(0x20, 0x7e)
    elif r < 0.85:
        c = rng.choice([0x0a, 0x0d, 0x200d, 0xfe0f, 0x20e3])  # LF, CR, ZWJ, VS16, keycap
    else:
        c = rng.randint(0, 0x10ffff)
    return chr(c) if valid(c) else 'x'

with open(out, 'wb') as f:
    for _ in range(count):
        b = ''.join(pick() for _ in range(rng.randint(1, 12))).encode()
        f.write(struct.pack('<I', len(b)))
        f.write(b)
