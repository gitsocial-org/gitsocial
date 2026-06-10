#!/bin/sh
# install.sh - Download and install the latest gitsocial binary.
# Usage: curl -fsSL https://raw.githubusercontent.com/gitsocial-org/gitsocial/main/install.sh | sh
set -e

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
REPO="${REPO:-gitsocial-org/gitsocial}"
BINARY="gitsocial"

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       echo "unsupported" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)             echo "unsupported" ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"

if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
  echo "Error: unsupported platform $(uname -s)/$(uname -m)" >&2
  exit 1
fi

VERSION="${VERSION:-latest}"
if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)"
fi

if [ -z "$VERSION" ]; then
  echo "Error: could not determine latest version" >&2
  exit 1
fi

ARCHIVE="${BINARY}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading ${BINARY} ${VERSION} (${OS}/${ARCH})..."
curl -fsSL -o "${TMPDIR}/${ARCHIVE}" "$URL"
curl -fsSL -o "${TMPDIR}/checksums.txt" "$CHECKSUMS_URL"

echo "Verifying checksum..."
EXPECTED="$(grep "${ARCHIVE}$" "${TMPDIR}/checksums.txt" | awk '{print $1}')"
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')"
else
  echo "Warning: no sha256 tool found, skipping verification" >&2
  ACTUAL="$EXPECTED"
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "Error: checksum mismatch" >&2
  echo "  expected: ${EXPECTED}" >&2
  echo "  got:      ${ACTUAL}" >&2
  exit 1
fi

tar -xzf "${TMPDIR}/${ARCHIVE}" -C "${TMPDIR}" "${BINARY}"
chmod +x "${TMPDIR}/${BINARY}"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"
