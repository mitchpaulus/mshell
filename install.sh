#!/bin/sh

# Can use this script like:
# curl -sL https://raw.githubusercontent.com/mitchpaulus/mshell/refs/heads/main/install.sh | sh -

DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/msh"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/msh"

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
curl -sL "https://github.com/mitchpaulus/mshell/releases/latest/download/${TARBALL}" | tar -xz -C "$HOME"/.local/bin

# Check for symlink, if it doesn't exist, create it
ln -s "$HOME"/.local/bin/msh "$HOME"/.local/bin/mshell 2>/dev/null

MSH_VERSION="$("$HOME/.local/bin/msh" --version)"

mkdir -p "$CONFIG_DIR/$MSH_VERSION"

# The standard library is built into the binary. Releases up to v0.14.0 read it
# from a file, std.msh in the tarball: move it to the versioned startup directory.
if [ -f "$HOME"/.local/bin/std.msh ]; then
    mkdir -p "$DATA_DIR/$MSH_VERSION"
    mv "$HOME"/.local/bin/std.msh "$DATA_DIR/$MSH_VERSION/std.msh"
fi

# Create an empty init.msh so startup succeeds on first run.
if [ ! -f "$CONFIG_DIR/$MSH_VERSION/init.msh" ]; then
    : > "$CONFIG_DIR/$MSH_VERSION/init.msh"
fi
