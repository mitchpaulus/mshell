"""Generate lexer comparison cases (ai/gui-terminal.typ, "Keeping it in sync").

Writes a stream of cases, each a u32 little-endian length and UTF-8 bytes:
every .msh file tracked in git, each distinct line, every prefix of each line
(what the editor holds while typing), and seeded random mutations of lines
using the characters that change how mshell lexes.

Usage: python3 -I tools/lexcases.py REPO_ROOT OUT [--seed N] [--mutations N]
"""
import random, struct, subprocess, sys

root, out = sys.argv[1], sys.argv[2]
seed = int(sys.argv[sys.argv.index('--seed') + 1]) if '--seed' in sys.argv else 1
mutations = int(sys.argv[sys.argv.index('--mutations') + 1]) if '--mutations' in sys.argv else 50000

files = subprocess.run(['git', '-C', root, 'ls-files', '*.msh'], capture_output=True, text=True, check=True).stdout.split()
seen, cases = set(), []

def add(text):
    if text not in seen:
        seen.add(text)
        cases.append(text)

lines = []
for f in files:
    try:
        text = open(f'{root}/{f}', encoding='utf-8').read()
    except (UnicodeDecodeError, FileNotFoundError):
        continue
    add(text)
    for line in text.split('\n'):
        if line.strip():
            lines.append(line)
            add(line)
            if len(line.encode()) <= 200:
                for i in range(1, len(line)):
                    add(line[:i])

# Characters that start or end tokens, escapes, numbers, interpolations,
# runes, paths, indexers and redirects; and a few multi-byte ones.
alphabet = list('"\'`$@{}[]()|:;,.!?<>=&*^~-+/\\#0123456789xXbBoOeE_ rUTV') + [
    '\t', '\n', '\r', '\x0b', '\x0c', '\0', 'é', '世', 'é', '\U0001F44D', ' ', '\u0085', ' ', '　',
    '٣',  # a digit outside ASCII (category Nd)
    '\x07', '\x1b',  # control characters, as in r'...'
    '9' * 20, '0' * 400,  # integers and floats that don't fit
    "r'", 'U+', '$"', '2>&1', '1>&2', '2024-', 'else*', '*if', 'tryAs',
]
rng = random.Random(seed)
for _ in range(mutations):
    s = list(rng.choice(lines))
    for _ in range(rng.randint(1, 4)):
        op = rng.randrange(3)
        i = rng.randint(0, len(s))
        if op == 0 or not s:
            s.insert(i, rng.choice(alphabet))
        elif op == 1:
            del s[min(i, len(s) - 1)]
        else:
            s[min(i, len(s) - 1)] = rng.choice(alphabet)
    add(''.join(s))

with open(out, 'wb') as f:
    for c in cases:
        b = c.encode()
        f.write(struct.pack('<I', len(b)))
        f.write(b)
print(f'{len(files)} files, {len(lines)} lines, {len(cases)} cases, '
      f'{sum(len(c.encode()) for c in cases) / 1e6:.1f} MB', file=sys.stderr)
