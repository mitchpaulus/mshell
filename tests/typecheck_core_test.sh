#!/bin/sh
# Runs the core type checker (MSH_CHECKER=core, ai/type-system-plan.md stage 3)
# over tests/success and tests/typecheck_fail.
#
# Every success program must pass, except those listed in
# core_expected_rejections.txt (rejected on purpose by the new rules).
# Every typecheck_fail program must fail, except those listed in
# core_no_longer_errors.txt. Each list has one file name per line, then a
# tab and the reason; lines starting with # are comments.
#
# While the core checker is being built, programs that use a construct it
# does not check yet are counted separately and do not fail the run.

cd "$(dirname "$0")" || exit 1

export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSH_CHECKER=core
BINARY=../mshell/msh
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

listed() {
    grep -v '^#' "$1" 2>/dev/null | cut -f1 | grep -qx "$2"
}

passed=0
unexpected=0
unsupported=0

for f in success/*.msh; do
    name="$(basename "$f")"
    if $BINARY --type-check-only "$f" > "$TMP" 2>&1; then
        if listed core_expected_rejections.txt "$name"; then
            printf '%s: listed as rejected, but passes\n' "$f"
            unexpected=$((unexpected + 1))
        else
            passed=$((passed + 1))
        fi
    elif listed core_expected_rejections.txt "$name"; then
        passed=$((passed + 1))
    elif grep -q 'does not check .* yet' "$TMP"; then
        unsupported=$((unsupported + 1))
    else
        printf '%s: expected to pass\n' "$f"
        head -n 3 "$TMP" | sed 's/^/    /'
        unexpected=$((unexpected + 1))
    fi
done

for f in typecheck_fail/*.msh; do
    name="$(basename "$f")"
    if $BINARY --type-check-only "$f" > "$TMP" 2>&1; then
        if listed core_no_longer_errors.txt "$name"; then
            passed=$((passed + 1))
        else
            printf '%s: expected to fail\n' "$f"
            unexpected=$((unexpected + 1))
        fi
    elif grep -q 'does not check .* yet' "$TMP"; then
        unsupported=$((unsupported + 1))
    elif listed core_no_longer_errors.txt "$name"; then
        printf '%s: listed as no longer an error, but fails\n' "$f"
        head -n 3 "$TMP" | sed 's/^/    /'
        unexpected=$((unexpected + 1))
    else
        passed=$((passed + 1))
    fi
done

printf 'Passed: %d\nUnexpected: %d\nNot checked yet: %d\n' "$passed" "$unexpected" "$unsupported"
test "$unexpected" -eq 0
