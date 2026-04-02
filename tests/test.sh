#!/bin/sh
find . -maxdepth 1 -name '*.msh' | sort -V | parallel -k ./test_file.sh
