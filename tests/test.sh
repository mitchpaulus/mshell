#!/bin/sh
cd success || exit 1
find . -maxdepth 1 -name '*.msh' | sort -V | parallel -k ../test_file.sh

cd ../fail || exit 1
find . -maxdepth 1 -name '*.msh' | sort -V | parallel -k ../test_file.sh
