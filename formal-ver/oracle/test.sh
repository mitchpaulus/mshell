#!/bin/sh
# Run the oracle on the queries in examples.txt and compare with the
# expected answers.  Also checks that examples.txt is what the oracle
# prints today (the example types are extracted from the proof).
set -e
cd "$(dirname "$0")"
./oracle --examples | diff -u examples.txt - || { echo "examples.txt is out of date: make examples.txt"; exit 1; }
grep -v '^#' examples.txt | cut -f1 | ./oracle > got.txt
grep -v '^#' examples.txt | cut -f2 > want.txt
if diff -u want.txt got.txt; then
  echo "oracle: $(wc -l < want.txt) examples agree"
  rm -f got.txt want.txt
else
  exit 1
fi
