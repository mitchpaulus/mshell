#!/bin/bash
TMP_FILE="$(mktemp)"
mshell < "$1" > "$TMP_FILE"
if test "$?" -eq 0; then
    diff_output="$(diff "$TMP_FILE" "$1".stdout)"
    if test "$?" -eq 0; then
        printf "%s passed\n" "$1"
    else
        printf "%s FAILED\n" "$1"
        printf "==================\n"
        printf "%s\n" "$diff_output"
        exit 1
    fi
fi
rm "$TMP_FILE"
