#!/bin/sh
# Installs the latest paranoid release binary.
#
#   curl -fsSL https://raw.githubusercontent.com/alainrk/paranoid/main/scripts/install.sh | sh
#
# Installs to $PARANOID_INSTALL_DIR, else ~/.local/bin. No sudo, no
# telemetry, nothing outside the install directory.
set -eu

REPO="alainrk/paranoid"
INSTALL_DIR="${PARANOID_INSTALL_DIR:-$HOME/.local/bin}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux|darwin) ;;
    *) echo "unsupported OS: $OS (use the Windows zip from the releases page)" >&2; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

TAG="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
if [ -z "$TAG" ]; then
    echo "could not find the latest release of $REPO" >&2
    exit 1
fi
VERSION="${TAG#v}"

ARCHIVE="paranoid_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$TAG/$ARCHIVE"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "downloading $URL"
curl -fsSL -o "$TMP/$ARCHIVE" "$URL"

# Verify the checksum when checksums.txt is published.
if curl -fsSL -o "$TMP/checksums.txt" \
    "https://github.com/$REPO/releases/download/$TAG/checksums.txt" 2>/dev/null; then
    EXPECTED="$(grep " $ARCHIVE\$" "$TMP/checksums.txt" | cut -d' ' -f1)"
    if [ -n "$EXPECTED" ]; then
        if command -v sha256sum >/dev/null 2>&1; then
            ACTUAL="$(sha256sum "$TMP/$ARCHIVE" | cut -d' ' -f1)"
        else
            ACTUAL="$(shasum -a 256 "$TMP/$ARCHIVE" | cut -d' ' -f1)"
        fi
        if [ "$EXPECTED" != "$ACTUAL" ]; then
            echo "checksum mismatch for $ARCHIVE" >&2
            exit 1
        fi
    fi
fi

tar -xzf "$TMP/$ARCHIVE" -C "$TMP" paranoid
mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP/paranoid" "$INSTALL_DIR/paranoid"

echo "installed $("$INSTALL_DIR/paranoid" version)"
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "note: add $INSTALL_DIR to your PATH" ;;
esac
