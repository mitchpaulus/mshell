#!/bin/sh

TMP_INIT="$(mktemp)"
trap 'rm -f "$TMP_INIT"' EXIT
export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHINIT="$TMP_INIT"
MSH="${MSH:-$(realpath ../mshell/msh)}"

# Programs the old checker gets wrong on purpose until the switch-over
# (ai/type-system-plan.md, stage 6); typecheck_core_test.sh checks them.
listed() {
    grep -v '^#' "$1" 2>/dev/null | grep -qx "$2"
}

pass=0
fail=0
failed_files=""
for f in success/*.msh; do
    if listed old_checker_rejects.txt "$(basename "$f")"; then
        continue
    fi
    if "$MSH" --type-check-only "$f" >/dev/null 2>&1; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        failed_files="$failed_files $f"
    fi
done

for f in typecheck_fail/*.msh; do
    if listed old_checker_accepts.txt "$(basename "$f")"; then
        continue
    fi
    if "$MSH" --type-check-only "$f" >/dev/null 2>&1; then
        fail=$((fail+1))
        failed_files="$failed_files $f"
    else
        pass=$((pass+1))
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
