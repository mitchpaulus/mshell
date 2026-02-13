#!/bin/sh

# Can use this script like:
# curl -sL https://raw.githubusercontent.com/mitchpaulus/mshell/refs/heads/main/install.sh | sh -

DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/mshell"

mkdir -p "$HOME"/.local/bin
mkdir -p "$DATA_DIR"
curl -sL 'https://github.com/mitchpaulus/mshell/releases/latest/download/linux_amd64.tar.gz' | tar -xz -C "$HOME"/.local/bin

# Check for symlink, if it doesn't exist, create it
ln -s "$HOME"/.local/bin/msh "$HOME"/.local/bin/mshell 2>/dev/null

# Move std.msh from the release tarball to the share directory
mv "$HOME"/.local/bin/std.msh "$DATA_DIR"/std.msh
# grep for 'export MSHSTDLIB' in .bashrc, if it doesn't exist, add it

# shellcheck disable=SC2016
grep -q 'export MSHSTDLIB' "$HOME"/.bashrc || \
    echo 'export MSHSTDLIB="${XDG_DATA_HOME:-$HOME/.local/share}/mshell/std.msh"' >> "$HOME"/.bashrc
