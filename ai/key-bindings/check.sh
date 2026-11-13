#!/usr/bin/env bash
# Checks the key resolution model: the design must pass, and the two
# broken variants must still fail, so the model keeps catching them.
set -euo pipefail

jar="${TLA2TOOLS_JAR:-$HOME/.local/share/tlaplus/tla2tools.jar}"

if [[ ! -f "$jar" ]]; then
    echo "TLA+ tools jar not found: $jar" >&2
    exit 2
fi

cd "$(dirname "$0")"

run() {
    java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -deadlock -workers auto -config "$1.cfg" KeyResolution.tla
}

echo "== Conflicts: expected to pass"
run Conflicts

expect_fail() {
    echo "== $1: expected to fail $2"
    if output=$(run "$1" 2>&1); then
        echo "$1 passed, but it should fail" >&2
        exit 1
    fi
    if ! grep -q "Invariant $2 is violated" <<<"$output"; then
        echo "$output" >&2
        echo "$1 failed for a different reason than expected" >&2
        exit 1
    fi
    echo "failed as expected"
}

expect_fail NoConflicts EffectsNeverUndone
expect_fail NoSnapshot StepMatches
