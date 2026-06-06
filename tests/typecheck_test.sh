#!/bin/sh

TMP_INIT="$(mktemp)"
trap 'rm -f "$TMP_INIT"' EXIT
export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHINIT="$TMP_INIT"
MSH="${MSH:-$(realpath ../mshell/msh)}"

pass=0
fail=0
failed_files=""
for f in success/*.msh; do
    if "$MSH" --type-check-only "$f" >/dev/null 2>&1; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        failed_files="$failed_files $f"
    fi
done

echo "Passed: $pass"
echo "Failed: $fail"
if [ "$fail" -gt 0 ]; then
    echo "Failed files:"
    for f in $failed_files; do
        echo "  $f"
    done
    exit 1
fi
