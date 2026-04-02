#!/bin/sh
set -eu

TEST_XDG_DATA_HOME="$(mktemp -d)"
TEST_XDG_CONFIG_HOME="$(mktemp -d)"

cleanup() {
    rm -rf "$TEST_XDG_DATA_HOME" "$TEST_XDG_CONFIG_HOME"
}

trap cleanup EXIT INT TERM

MSH_VERSION="$(../mshell/mshell --version)"

mkdir -p "$TEST_XDG_DATA_HOME/msh/lib/$MSH_VERSION"
cp ../lib/std.msh "$TEST_XDG_DATA_HOME/msh/lib/$MSH_VERSION/std.msh"

mkdir -p "$TEST_XDG_CONFIG_HOME/msh/init"
mkdir -p "$TEST_XDG_CONFIG_HOME/msh/init/$MSH_VERSION"
touch "$TEST_XDG_CONFIG_HOME/msh/init/init.msh"
touch "$TEST_XDG_CONFIG_HOME/msh/init/$MSH_VERSION/init.msh"

export XDG_DATA_HOME="$TEST_XDG_DATA_HOME"
export XDG_CONFIG_HOME="$TEST_XDG_CONFIG_HOME"

find . -maxdepth 1 -name '*.msh' | sort -V | parallel -k ./test_file.sh
