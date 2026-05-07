#!/usr/bin/env bash
# Survey --check-types across tests/*.msh.
#
# Usage:
#   ai/check_survey.sh                     # pass/total summary
#   ai/check_survey.sh top                 # error categories, sorted
#   ai/check_survey.sh files               # per-file error counts
#   ai/check_survey.sh categories <file>   # category breakdown for one file
#   ai/check_survey.sh build               # rebuild mshell first, then summary
#
# Run from anywhere; resolves paths relative to the repo root.

set -u

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mshell_bin="$repo_root/mshell/mshell"
tests_dir="$repo_root/tests"

mode="${1:-summary}"

build_mshell() {
    (cd "$repo_root/mshell" && ./build.sh)
}

summary() {
    local pass=0 total=0
    for f in "$tests_dir"/*.msh; do
        total=$((total + 1))
        if "$mshell_bin" --check-types "$f" >/dev/null 2>&1; then
            pass=$((pass + 1))
        fi
    done
    echo "pass: $pass / $total"
}

per_file() {
    for f in "$tests_dir"/*.msh; do
        local errs
        errs=$("$mshell_bin" --check-types "$f" 2>&1 | grep -c "type error" || true)
        if [ "$errs" != "0" ]; then
            echo "$errs  $(basename "$f")"
        fi
    done | sort -rn
}

top_categories() {
    for f in "$tests_dir"/*.msh; do
        "$mshell_bin" --check-types "$f" 2>&1 | grep "type error" | sed 's/at line [0-9]*, column [0-9]*: //'
    done | sort | uniq -c | sort -rn | head -25
}

categories_for_file() {
    local f="$1"
    if [ ! -f "$f" ] && [ -f "$tests_dir/$f" ]; then
        f="$tests_dir/$f"
    fi
    "$mshell_bin" --check-types "$f" 2>&1 | sed 's/at line [0-9]*, column [0-9]*: //' | sort | uniq -c | sort -rn
}

case "$mode" in
    summary) summary ;;
    files) per_file ;;
    top) top_categories ;;
    categories)
        if [ $# -lt 2 ]; then
            echo "usage: $0 categories <file>" >&2
            exit 2
        fi
        categories_for_file "$2"
        ;;
    build)
        build_mshell || exit 1
        summary
        ;;
    *)
        echo "unknown mode: $mode" >&2
        echo "modes: summary | files | top | categories <file> | build" >&2
        exit 2
        ;;
esac
