#!/bin/sh
# install.sh - Download and install the latest gitsocial binary from the bucket.
# Usage: curl -fsSL https://gitsocial.org/install.sh | sh
# Resolves the version from artifacts/latest.txt and downloads archives +
# checksums from artifacts/<version>/ (no GitHub dependency).
set -e

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BASE_URL="${GITSOCIAL_INSTALL_BASE:-https://gitsocial.org/}"
case "$BASE_URL" in */) ;; *) BASE_URL="${BASE_URL}/" ;; esac
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
  VERSION="$(curl -fsSL "${BASE_URL}artifacts/latest.txt" | tr -d '[:space:]')"
fi
VERSION="${VERSION#v}"

if [ -z "$VERSION" ]; then
  echo "Error: could not determine latest version" >&2
  exit 1
fi

# Archive naming and formats follow .goreleaser.yaml: zip for darwin, tar.gz
# for linux.
case "$OS" in
  darwin) EXT="zip" ;;
  *)      EXT="tar.gz" ;;
esac
ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="${BASE_URL}artifacts/${VERSION}/${ARCHIVE}"
CHECKSUMS_URL="${BASE_URL}artifacts/${VERSION}/checksums.txt"

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

if [ "$EXT" = "zip" ]; then
  unzip -q -o "${TMPDIR}/${ARCHIVE}" "${BINARY}" -d "${TMPDIR}"
else
  tar -xzf "${TMPDIR}/${ARCHIVE}" -C "${TMPDIR}" "${BINARY}"
fi
chmod +x "${TMPDIR}/${BINARY}"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"
