#!/bin/sh

# Can use this script like:
# curl -sL https://raw.githubusercontent.com/mitchpaulus/mshell/refs/heads/main/install.sh | sh -

DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/mshell"

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux)  GOOS="linux" ;;
    Darwin) GOOS="darwin" ;;
    *)      echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect CPU architecture and map to Go's naming convention
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)        GOARCH="amd64" ;;
    i386|i686)     GOARCH="386" ;;
    armv6l|armv7l) GOARCH="arm" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *)             echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

TARBALL="${GOOS}_${GOARCH}.tar.gz"
echo "Detected platform: $GOOS/$GOARCH (tarball: $TARBALL)"

mkdir -p "$HOME"/.local/bin
mkdir -p "$DATA_DIR"
curl -sL "https://github.com/mitchpaulus/mshell/releases/latest/download/${TARBALL}" | tar -xz -C "$HOME"/.local/bin

# Check for symlink, if it doesn't exist, create it
ln -s "$HOME"/.local/bin/msh "$HOME"/.local/bin/mshell 2>/dev/null

# Move std.msh from the release tarball to the share directory
mv "$HOME"/.local/bin/std.msh "$DATA_DIR"/std.msh
# grep for 'export MSHSTDLIB' in .bashrc, if it doesn't exist, add it

# Add MSHSTDLIB export to the appropriate shell rc file
# shellcheck disable=SC2016
MSHSTDLIB_EXPORT='export MSHSTDLIB="${XDG_DATA_HOME:-$HOME/.local/share}/mshell/std.msh"'
if [ "$GOOS" = "darwin" ] && [ -f "$HOME/.zshrc" ]; then
    grep -q 'export MSHSTDLIB' "$HOME"/.zshrc || echo "$MSHSTDLIB_EXPORT" >> "$HOME"/.zshrc
else
    grep -q 'export MSHSTDLIB' "$HOME"/.bashrc || echo "$MSHSTDLIB_EXPORT" >> "$HOME"/.bashrc
fi
