#!/usr/bin/env bash
# Survey --check-types across tests/*.msh.
#
# Usage:
#   ai/check_survey.sh                     # pass/total summary
#   ai/check_survey.sh top                 # error categories, sorted
#   ai/check_survey.sh files               # per-file error counts
#   ai/check_survey.sh runtime-failures    # files that type-check, then fail at runtime
#   ai/check_survey.sh expected            # expected static type failures
#   ai/check_survey.sh categories <file>   # category breakdown for one file
#   ai/check_survey.sh build               # rebuild mshell first, then summary
#
# Run from anywhere; resolves paths relative to the repo root.

set -u

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mshell_bin="$repo_root/mshell/mshell"
tests_dir="$repo_root/tests"
survey_init="$repo_root/.cache/check_survey_init.msh"

mode="${1:-summary}"

mkdir -p "$(dirname "$survey_init")"
: > "$survey_init"

lock_file="${TMPDIR:-/tmp}/mshell-check-survey.lock"
exec 9>"$lock_file"
flock 9

run_check() {
    local f="$1"
    local base
    base="$(basename "$f")"

    if printf %s "$base" | grep -q 'positional'; then
        (cd "$tests_dir" && MSHSTDLIB="$repo_root/lib/std.msh" MSHINIT="$survey_init" "$mshell_bin" --check-types "$base" Hello World)
    elif [ "$base" = "args.msh" ]; then
        (cd "$tests_dir" && MSHSTDLIB="$repo_root/lib/std.msh" MSHINIT="$survey_init" "$mshell_bin" --check-types "$base" Hello World)
    elif [ "$base" = "stdin_keyword.msh" ]; then
        (cd "$tests_dir" && MSHSTDLIB="$repo_root/lib/std.msh" MSHINIT="$survey_init" "$mshell_bin" --check-types stdin_keyword.msh < stdin_for_test.txt)
    elif [ "$base" = "pwd.msh" ]; then
        (cd "$tests_dir" && MSHSTDLIB="$repo_root/lib/std.msh" MSHINIT="$survey_init" "$mshell_bin" --check-types "pwd.msh" "$(pwd)")
    else
        (cd "$tests_dir" && MSHSTDLIB="$repo_root/lib/std.msh" MSHINIT="$survey_init" "$mshell_bin" --check-types < "$base")
    fi
}

expected_type_fail() {
    case "$(basename "$1")" in
        if_fail.msh) return 0 ;;
    esac
    return 1
}

type_error_count_file() {
    grep -a -c "type error" "$1" || true
}

static_check_result() {
    local f="$1"
    local output_file errs status
    output_file="$(mktemp)"
    run_check "$f" > "$output_file" 2>&1
    status=$?
    errs="$(type_error_count_file "$output_file")"
    rm -f "$output_file"

    if expected_type_fail "$f"; then
        if [ "$errs" != "0" ]; then
            return 0
        fi
        return 1
    fi

    if [ "$errs" = "0" ]; then
        return 0
    fi
    return 1
}

build_mshell() {
    (cd "$repo_root/mshell" && ./build.sh)
}

summary() {
    local pass=0 total=0
    for f in "$tests_dir"/*.msh; do
        total=$((total + 1))
        if static_check_result "$f"; then
            pass=$((pass + 1))
        fi
    done
    echo "static pass: $pass / $total"
}

per_file() {
    for f in "$tests_dir"/*.msh; do
        local output_file errs
        output_file="$(mktemp)"
        run_check "$f" > "$output_file" 2>&1
        errs="$(type_error_count_file "$output_file")"
        rm -f "$output_file"
        if [ "$errs" != "0" ] && ! expected_type_fail "$f"; then
            echo "$errs  $(basename "$f")"
        fi
    done | sort -rn
}

top_categories() {
    for f in "$tests_dir"/*.msh; do
        if ! expected_type_fail "$f"; then
            run_check "$f" 2>&1 | grep "type error" | sed 's/at line [0-9]*, column [0-9]*: //'
        fi
    done | sort | uniq -c | sort -rn | head -25
}

runtime_failures() {
    for f in "$tests_dir"/*.msh; do
        local output_file status errs
        output_file="$(mktemp)"
        run_check "$f" > "$output_file" 2>&1
        status=$?
        errs="$(type_error_count_file "$output_file")"
        rm -f "$output_file"
        if [ "$status" -ne 0 ] && [ "$errs" = "0" ]; then
            echo "$(basename "$f")"
        fi
    done
}

expected_failures() {
    for f in "$tests_dir"/*.msh; do
        if expected_type_fail "$f"; then
            echo "$(basename "$f")"
        fi
    done
}

categories_for_file() {
    local f="$1"
    if [ ! -f "$f" ] && [ -f "$tests_dir/$f" ]; then
        f="$tests_dir/$f"
    fi
    run_check "$f" 2>&1 | sed 's/at line [0-9]*, column [0-9]*: //' | sort | uniq -c | sort -rn
}

case "$mode" in
    summary) summary ;;
    files) per_file ;;
    top) top_categories ;;
    runtime-failures) runtime_failures ;;
    expected) expected_failures ;;
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
        echo "modes: summary | files | top | runtime-failures | expected | categories <file> | build" >&2
        exit 2
        ;;
esac
