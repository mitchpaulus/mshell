#!/bin/sh
# Check the Rust lexer against msh's (ai/gui-terminal.typ, "Keeping it in
# sync"): build both, generate the cases, compare in both lexer modes.
# Usage: tools/check-lexer.sh [SEED [MUTATIONS]]
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "$here/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
(cd "$root/mshell" && go build -o "$tmp/msh" .)
(cd "$here" && cargo build --release --quiet --bin mshlex)
python3 -I "$here/tools/lexcases.py" "$root" "$tmp/cases.bin" --seed "${1:-1}" --mutations "${2:-50000}"
python3 -I "$here/tools/lexcompare.py" "$tmp/msh" "$here/../target/release/mshlex" "$tmp/cases.bin" --editor
python3 -I "$here/tools/lexcompare.py" "$tmp/msh" "$here/../target/release/mshlex" "$tmp/cases.bin"
