#!/bin/bash
TMP_FILE="$(mktemp)"
TMP_ERR="$(mktemp)"

MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHSTDLIB

if printf %s "$1" | grep -q 'positional'; then
    mshell "$1" Hello World > "$TMP_FILE" 2>"$TMP_ERR"
elif test "$(basename "$1")" = "args.msh"; then
    mshell "$1" Hello World > "$TMP_FILE" 2>"$TMP_ERR"
elif test "$(basename "$1")" = "stdin_keyword.msh"; then
    mshell stdin_keyword.msh < stdin_for_test.txt > "$TMP_FILE" 2>"$TMP_ERR"
elif test "$(basename "$1")" = "pwd.msh"; then
    mshell "pwd.msh" "$(pwd)" > "$TMP_FILE" 2>"$TMP_ERR"
else
    mshell < "$1" > "$TMP_FILE" 2>"$TMP_ERR"
fi

if test "$?" -eq 0; then
    diff_output="$(diff "$TMP_FILE" "$1".stdout)"
    if test "$?" -eq 0; then
        printf "%s %spassed%s\n" "$1" "${GREEN}" "${NC}"
    else
        printf "%s FAILED\n" "$1"
        printf "==================\n"
        printf "%s\n" "$diff_output"
        exit 1
    fi
else
    if test ! -f "$1".stderr; then
        printf "%s FAILED\n" "$1"
        printf "==================\n"
        printf "Expected success but got failure.\n"
        cat "$TMP_ERR"
        exit 1
    else
        diff_output="$(diff "$TMP_ERR" "$1".stderr)"
        if test "$?" -eq 0; then
            printf "%s passed\n" "$1"
        else
            printf "%s FAILED\n" "$1"
            printf "\n"
            printf "%s\n" "$diff_output"
            printf "\n"
            exit 1
        fi
    fi
fi

# Check for lines with '# FILE:<filename>' and check that the file exists and matches contents of <filename>.expected
# This is to test redirections
grep -E '^# FILE:.+$' "$1" | while read -r line; do
    filename="$(echo "$line" | cut -d: -f2)"
    diff_output="$(diff "$filename" "$filename".expected)"
    if test "$?" -eq 0; then
        printf "  %s redirect to '%s' passed\n" "$1" "$filename"
    else
        printf "  %s redirect to '%s' FAILED\n" "$1" "$filename"
        printf "==================\n"
        printf "%s\n" "$diff_output"
        exit 1
    fi
done

rm "$TMP_FILE"
rm "$TMP_ERR"
