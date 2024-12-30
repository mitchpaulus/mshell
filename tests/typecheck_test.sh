#!/bin/sh
set -e 

for f in *.msh; do
    echo "Type checking $f"
    msh --typecheck "$f"
done
