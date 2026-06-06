#!/bin/sh
rc=0

cd success || exit 1
find . -maxdepth 1 -name '*.msh' | sort -V | parallel -k ../test_file.sh || rc=$?

cd ../fail || exit 1
find . -maxdepth 1 -name '*.msh' | sort -V | parallel -k ../test_file.sh || rc=$?

exit "$rc"
