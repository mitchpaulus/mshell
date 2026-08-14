#!/usr/bin/env bash
set -euo pipefail

jar="${TLA2TOOLS_JAR:-$HOME/.local/share/tlaplus/tla2tools.jar}"

if [[ ! -f "$jar" ]]; then
    echo "TLA+ tools jar not found: $jar" >&2
    exit 2
fi

for model in TerminalControl POSIXTerminalControl WindowsTerminalControl StreamLifecycle; do
    java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -workers 1 -deadlock \
        -config "$model.cfg" "$model.tla"
done

java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -workers 1 -deadlock \
    -config POSIXNonTerminal.cfg POSIXTerminalControl.tla

java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -workers 1 -deadlock \
    -config POSIXSharedTerminal.cfg POSIXTerminalControl.tla
