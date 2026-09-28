#!/usr/bin/env bash
# Checks the terminal query model: the current design must pass, and the code
# before the fix must still fail, so the model keeps catching those bugs.
set -euo pipefail

jar="${TLA2TOOLS_JAR:-$HOME/.local/share/java/tla2tools.jar}"

if [[ ! -f "$jar" ]]; then
    echo "TLA+ tools jar not found: $jar" >&2
    exit 2
fi

cd "$(dirname "$0")"

run() {
    java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -workers auto -config "$1.cfg" TerminalQuery.tla
}

for model in Unix Windows; do
    echo "== $model: expected to pass"
    run "$model"
done

for model in UnixBefore WindowsBefore; do
    echo "== $model: expected to fail NeverPastDeadline"
    if output=$(run "$model" 2>&1); then
        echo "$model passed, but the code before the fix should fail" >&2
        exit 1
    fi
    if ! grep -q "Invariant NeverPastDeadline is violated" <<<"$output"; then
        echo "$output" >&2
        echo "$model failed for a different reason than expected" >&2
        exit 1
    fi
    echo "failed as expected"
done
