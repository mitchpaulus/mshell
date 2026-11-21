"""Compare the Rust lexer with msh's, case by case (see lexcases.py).

An ERROR token's message is not compared, only that there is an error and
where (ai/gui-terminal.typ: the port needs token kinds and positions, not
detailed error messages).

Usage: python3 -I tools/lexcompare.py MSH MSHLEX CASES [--editor] [--show N]
Exit status 1 if any case differs.
"""
import re, struct, subprocess, sys

msh, mshlex, cases_path = sys.argv[1:4]
editor = ['--lex-editor'] if '--editor' in sys.argv else []
show = int(sys.argv[sys.argv.index('--show') + 1]) if '--show' in sys.argv else 5

data = open(cases_path, 'rb').read()
cases, i = [], 0
while i < len(data):
    (n,) = struct.unpack_from('<I', data, i)
    cases.append(data[i + 4:i + 4 + n].decode())
    i += 4 + n

def run(cmd):
    out = subprocess.run(cmd, input=data, capture_output=True, check=True).stdout.decode()
    parts = re.split(r'^=== case \d+\n', out, flags=re.M)
    assert parts[0] == '' and len(parts) == len(cases) + 1, (cmd, len(parts), len(cases))
    # A lexeme can hold a newline, so only the start of a token line is
    # matched; the error message runs to the end of the case's output.
    return [re.sub(r'^(\d+:\d+:ERROR) .*', r'\1', p, flags=re.M | re.S) for p in parts[1:]]

want = run([msh, '--lex', *editor, '--lex-cases'])
got = run([mshlex, *(['--editor'] if editor else []), '--cases'])
bad = [k for k in range(len(cases)) if want[k] != got[k]]
mode = 'editor' if editor else 'batch'
print(f'{mode}: {len(cases) - len(bad)}/{len(cases)} cases match')
for k in bad[:show]:
    print(f'--- case {k}: {cases[k]!r}')
    w, g = want[k].splitlines(), got[k].splitlines()
    for j in range(max(len(w), len(g))):
        a = w[j] if j < len(w) else '<none>'
        b = g[j] if j < len(g) else '<none>'
        print(f'  {"  " if a == b else "!!"} msh {a!r}\n     rust {b!r}' if a != b else f'     {a!r}')
sys.exit(1 if bad else 0)
