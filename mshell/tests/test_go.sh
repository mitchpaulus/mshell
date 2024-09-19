#!/bin/sh
find . -name '*.msh' | parallel ./test_file_go.sh
