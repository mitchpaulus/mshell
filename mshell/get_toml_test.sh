#!/bin/sh
# Downloads the toml-test suite (https://github.com/toml-lang/toml-test) into
# testdata/toml-test, where TestTomlSuite and FuzzDecodeToml find it. The
# files are not kept in git; run this when you want to test the TOML parser.
set -eu

version=2.2.0
dir="$(cd "$(dirname "$0")" && pwd)/testdata/toml-test"

rm -rf "$dir"
mkdir -p "$dir"
curl -fsSL "https://github.com/toml-lang/toml-test/archive/refs/tags/v$version.tar.gz" |
	tar -xz -C "$dir" --strip-components=2 "toml-test-$version/tests"
echo "toml-test $version is in $dir"
