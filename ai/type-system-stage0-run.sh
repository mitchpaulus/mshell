#!/bin/sh
# Stage 0: run the instrumented checker over the corpora.
# Apply ai/type-system-stage0-counters.patch and rebuild first. Output: $S/stats_{success,scripts,std}.tsv
S=${S:-$(mktemp -d)}
echo "stats in $S"
R=$(cd "$(dirname "$0")/.." && pwd)
rm -f $S/stats_*.tsv
: > $S/empty.msh
export MSHINIT=$S/empty.msh
MSH=$R/mshell/msh
cd $R/tests
export MSHSTDLIB=$R/lib/std.msh
for f in success/*.msh; do MSH_TC_FILE=$f MSH_TC_STATS=$S/stats_success.tsv $MSH --type-check-only "$f" >/dev/null 2>&1; done
for f in msh-scripts/*; do MSH_TC_FILE=$f MSH_TC_STATS=$S/stats_scripts.tsv $MSH --type-check-only "$f" >/dev/null 2>&1; done
MSH_TC_FILE=lib/std.msh MSHSTDLIB=$S/empty.msh MSH_TC_STATS=$S/stats_std.tsv $MSH --type-check-only ../lib/std.msh >/dev/null 2>&1
cd $S
for c in success scripts std; do echo "== $c"; cut -f1,2 stats_$c.tsv | sort -u | cut -f1 | sort | uniq -c | sort -rn; done
