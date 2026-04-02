#!/bin/sh
set -e 

TMP_INIT="$(mktemp)"
trap 'rm -f "$TMP_INIT"' EXIT
export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHINIT="$TMP_INIT"

for f in *.msh; do
    echo "Type checking $f"
    msh --typecheck "$f"
done
