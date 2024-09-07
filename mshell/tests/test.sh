#!/bin/sh
find . -name '*.msh' | parallel ./test_file.sh
