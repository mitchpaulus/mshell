#!/bin/sh
set -eu

# Build from the repository module so this standalone experiment uses the same
# Go version and x/term dependency as mshell itself. The timed region is inside
# the binary, so compilation and process startup are never part of a sample.
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/mshell-cpr-latency.XXXXXX")

cleanup() {
    rm -rf -- "$build_dir"
}
trap cleanup EXIT HUP INT TERM

cd "$repo_dir/mshell"
go build -o "$build_dir/cursor-query-latency" ../tests/terminal_tests/cursor_query_latency.go

if [ "$#" -gt 0 ]; then
    "$build_dir/cursor-query-latency" "$@"
    exit
fi

echo "Burst profile (maximum query throughput)"
"$build_dir/cursor-query-latency" -count 1000 -warmup 50 -interval 0
echo
echo "Paced profile (5 ms between replies and queries)"
"$build_dir/cursor-query-latency" -count 500 -warmup 20 -interval 5ms
echo
echo "Idle profile (100 ms between replies and queries)"
"$build_dir/cursor-query-latency" -count 100 -warmup 10 -interval 100ms
