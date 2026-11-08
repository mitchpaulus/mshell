#!/bin/sh
# Check the Rust key resolver, edit ops and grapheme segmentation against
# msh's (ai/gui-terminal.typ, "Keeping it in sync"): msh records resolver
# cases and grapheme boundaries, and the Rust side replays them.
# Usage: tools/check-resolver.sh [SEED [COUNT]]
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "$here/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
(cd "$root/mshell" && go build -o "$tmp/msh" .)
(cd "$here" && cargo build --release --quiet --bin mshlex --example resolvercheck)
"$tmp/msh" --resolver-cases "${1:-1}" "${2:-50000}" > "$tmp/resolver.jsonl"
"$here/../target/release/examples/resolvercheck" "$tmp/resolver.jsonl"
python3 -I "$here/tools/graphemecases.py" "$tmp/graphemes.bin" --seed "${1:-1}"
"$tmp/msh" --lex --lex-cases --lex-graphemes < "$tmp/graphemes.bin" > "$tmp/graphemes-go.txt"
"$here/../target/release/mshlex" --cases --graphemes "$tmp/graphemes.bin" > "$tmp/graphemes-rust.txt"
if cmp -s "$tmp/graphemes-go.txt" "$tmp/graphemes-rust.txt"; then
    echo "graphemes: all $(grep -c '^===' "$tmp/graphemes-go.txt") cases match"
else
    echo "graphemes: differ"; diff "$tmp/graphemes-go.txt" "$tmp/graphemes-rust.txt" | head -20; exit 1
fi
