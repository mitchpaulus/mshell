#!/bin/sh
# Regenerates the PDF fixtures used by PdfInfo_test.go from base.txt.
# base.txt has no xref table, so qpdf rebuilds it and exits with 3
# (success with warnings).
set -e
cd "$(dirname "$0")"
run() { qpdf --deterministic-id "$@" 2>/dev/null || [ $? -eq 3 ]; }
run --object-streams=disable base.txt classic.pdf
run --object-streams=generate base.txt objstm.pdf
run --linearize base.txt linearized.pdf
# qpdf does not allow --deterministic-id with encryption.
qpdf --encrypt "" owner 256 -- --object-streams=disable base.txt encrypted.pdf 2>/dev/null || [ $? -eq 3 ]
