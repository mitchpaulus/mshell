#!/bin/bash
# The soundness oracle on the test corpus (plan stage 7): run every program
# in tests/success and tests/fail that passes the type checker, and fail if
# it stops with a type mismatch, which a checked program must never reach.
# A program that fails the checker is not run.

cd "$(dirname "$0")" || exit 1

TMP_INIT="$(mktemp)"
TMP_ERR="$(mktemp)"
trap 'rm -f "$TMP_INIT" "$TMP_ERR"' EXIT
export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHINIT="$TMP_INIT"
export MSH_ERROR_KIND=1
BINARY="$(realpath ../mshell/mshell)"

checked=0
skipped=0
bad=0

# run_like_test_file runs $1 the way test_file.sh does, from its directory.
run_like_test_file() {
    case "$(basename "$1")" in
        *positional*|args.msh) "$BINARY" "$1" Hello World ;;
        dash_stdin.msh) "$BINARY" - Hello World < "$1" ;;
        stdin_keyword.msh) "$BINARY" stdin_keyword.msh < stdin_for_test.txt ;;
        pwd.msh) "$BINARY" pwd.msh "$(pwd)" ;;
        *) "$BINARY" < "$1" ;;
    esac
}

for dir in success fail; do
    for f in "$dir"/*.msh; do
        if ! "$BINARY" --type-check-only "$f" > /dev/null 2>&1; then
            skipped=$((skipped+1))
            continue
        fi
        checked=$((checked+1))
        (cd "$dir" && run_like_test_file "$(basename "$f")") > /dev/null 2> "$TMP_ERR" < /dev/null
        if grep -q '^msh error kind: type mismatch$' "$TMP_ERR" || grep -q '^panic: ' "$TMP_ERR"; then
            bad=$((bad+1))
            printf '%s: a checked program reached a type mismatch\n' "$f"
            tail -n 5 "$TMP_ERR" | sed 's/^/    /'
        fi
    done
done

echo "Checked and run: $checked"
echo "Not checked (not run): $skipped"
echo "Type mismatches: $bad"
test "$bad" -eq 0
