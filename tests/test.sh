#!/bin/sh
find . -maxdepth 1 -name '*.msh' | parallel ./test_file.sh
