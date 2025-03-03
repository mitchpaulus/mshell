#!/bin/sh

mkdir -p "$HOME"/.local/bin
curl -sL 'https://github.com/mitchpaulus/mshell/releases/latest/download/linux_amd64.tar.gz' | tar -xz -C "$HOME"/.local/bin

# Check for symlink, if it doesn't exist, create it
ln -s "$HOME"/.local/bin/msh "$HOME"/.local/bin/mshell 2>/dev/null

mkdir -p "$HOME"/.local/share/mshell
curl -sL 'https://raw.githubusercontent.com/mitchpaulus/mshell/refs/heads/main/lib/std.msh' > "$HOME"/.local/share/mshell/std.msh
# grep for 'export MSHSTDLIB' in .bashrc, if it doesn't exist, add it

# shellcheck disable=SC2016
grep -q 'export MSHSTDLIB' "$HOME"/.bashrc || \
    echo 'export MSHSTDLIB="$HOME/.local/share/mshell/std.msh"' >> "$HOME"/.bashrc
