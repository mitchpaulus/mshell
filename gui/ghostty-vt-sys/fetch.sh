#!/bin/sh
# Download libghostty-vt's release tarballs, check them against pinned
# SHA-256 sums, and build the library into gui/third_party/.
# Needs Zig 0.17.x (see PACKAGING-LIBGHOSTTY.md in the tarball). Takes ~5 min.
set -eu

VERSION=1.0.0
SRC_SHA=4a6ef758be721923e1969e7b81b443dcc92d8dd3fb5a39600c03a8817a753676
DEPS_SHA=4d5ea5e448e4c7ea8fb55ccbaa8668f2a97d44f438cee831e2b7ecd162d85c29
URL=https://release.files.ghostty.org/libghostty-vt/$VERSION

root=$(cd "$(dirname "$0")/.." && pwd)/third_party
dir=$root/libghostty-vt-$VERSION-build
mkdir -p "$dir"
cd "$dir"

fetch() {
    if [ ! -f "$1" ]; then
        curl -fL --proto '=https' -o "$1.part" "$URL/$1"
        mv "$1.part" "$1"
    fi
    echo "$2  $1" | sha256sum -c -
}
fetch libghostty-vt-$VERSION.tar.gz "$SRC_SHA"
fetch libghostty-vt-$VERSION-deps.tar.gz "$DEPS_SHA"

rm -rf libghostty-vt-$VERSION libghostty-vt-$VERSION-deps cache
tar -xzf libghostty-vt-$VERSION.tar.gz
tar -xzf libghostty-vt-$VERSION-deps.tar.gz

# The build cache must start empty (Zig 0.17 can crash on a cache holding
# package archives), and --system keeps the build offline.
cd libghostty-vt-$VERSION
ZIG_GLOBAL_CACHE_DIR="$dir/cache" zig build \
    --prefix "$root/libghostty-vt-$VERSION" \
    --system ../libghostty-vt-$VERSION-deps \
    -Doptimize=ReleaseFast
echo "installed to $root/libghostty-vt-$VERSION"
