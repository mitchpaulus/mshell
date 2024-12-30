#!/bin/sh
find . -maxdepth 1 -name '*.msh' | sort -V | parallel ./test_file.sh
