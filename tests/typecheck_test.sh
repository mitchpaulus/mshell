#!/bin/sh
# Every tests/success program must pass the type checker, and every
# tests/typecheck_fail program must fail it.

cd "$(dirname "$0")" || exit 1

TMP="$(mktemp)"
# An empty init file, so the user's own does not change the results.
TMP_INIT="$(mktemp)"
trap 'rm -f "$TMP" "$TMP_INIT"' EXIT
export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHINIT="$TMP_INIT"
MSH="${MSH:-$(realpath ../mshell/msh)}"

pass=0
fail=0
for f in success/*.msh; do
    if "$MSH" --type-check-only "$f" > "$TMP" 2>&1; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        printf '%s: expected to pass\n' "$f"
        head -n 3 "$TMP" | sed 's/^/    /'
    fi
done

for f in typecheck_fail/*.msh; do
    if "$MSH" --type-check-only "$f" >/dev/null 2>&1; then
        fail=$((fail+1))
        printf '%s: expected to fail\n' "$f"
    else
        pass=$((pass+1))
    fi
done

echo "Passed: $pass"
echo "Failed: $fail"
test "$fail" -eq 0
