#!/usr/bin/env bash
# release.sh - Create a gitsocial release from built artifacts
#
# Usage: scripts/release.sh <tag> [dist-dir]
#
# Expects goreleaser (or equivalent) to have already produced archives,
# checksums, and optionally SBOM files in dist-dir.
#
# Environment:
#   GITSOCIAL_BINARY  path to gitsocial binary (auto-detected from dist/ or PATH)
set -euo pipefail

TAG="${1:?Usage: scripts/release.sh <tag> [dist-dir]}"
DIST="${2:-dist}"
VERSION="${TAG#v}"

# --- find gitsocial binary ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); case "$ARCH" in x86_64) ARCH="amd64";; aarch64) ARCH="arm64";; esac
if [ -n "${GITSOCIAL_BINARY:-}" ]; then
  BIN="$GITSOCIAL_BINARY"
elif BIN=$(find "$DIST" -name gitsocial -path "*${OS}*${ARCH}*" -type f 2>/dev/null | head -1) && [ -n "$BIN" ]; then
  :
elif BIN=$(find "$DIST" -name gitsocial -type f 2>/dev/null | head -1) && [ -n "$BIN" ]; then
  :
elif command -v gitsocial >/dev/null 2>&1; then
  BIN="gitsocial"
else
  echo "error: no gitsocial binary found in $DIST/ or PATH" >&2
  echo "set GITSOCIAL_BINARY or build first" >&2
  exit 1
fi
chmod +x "$BIN"

# --- git identity (set defaults if not configured) ---
git config user.name >/dev/null 2>&1 || git config user.name "release-bot"
git config user.email >/dev/null 2>&1 || git config user.email "release-bot@noreply"

# --- fetch existing gitmsg refs ---
git fetch origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*' 'refs/gitmsg/*:refs/gitmsg/*' || true

# --- changelog ---
NOTES=$(mktemp)
trap 'rm -f "$NOTES"' EXIT
echo "$TAG" > "$NOTES"
echo >> "$NOTES"
PREV_TAG=$(git describe --tags --abbrev=0 "$TAG^" 2>/dev/null || echo "")
if [ -n "$PREV_TAG" ]; then
  git log "$PREV_TAG..$TAG" --oneline --no-decorate >> "$NOTES"
else
  git log --oneline --no-decorate >> "$NOTES"
fi

# --- gitsocial release ---
"$BIN" release init || true

SBOM_NAME=$(cd "$DIST" && ls ./*.sbom.json 2>/dev/null | head -1 | sed 's|^\./||')
ARTIFACTS=$(cd "$DIST" && ls ./*.tar.gz ./*.zip 2>/dev/null | sed 's|^\./||' | paste -sd, -)

"$BIN" release artifacts add "$VERSION" \
  "$DIST"/*.tar.gz "$DIST"/*.zip "$DIST"/checksums.txt \
  ${SBOM_NAME:+$(ls "$DIST"/*.sbom.json 2>/dev/null)}

CREATE_ARGS=(
  --tag "$TAG"
  --version "$VERSION"
  --artifacts "$ARTIFACTS"
  --checksums checksums.txt
)
[ -n "${SBOM_NAME:-}" ] && CREATE_ARGS+=(--sbom "$SBOM_NAME")

"$BIN" release create - "${CREATE_ARGS[@]}" < "$NOTES"

# --- push ---
NEW_ARTIFACT_REF="refs/gitmsg/release/v$VERSION/artifacts"
git lfs push origin "$NEW_ARTIFACT_REF"
git push origin gitmsg/release
git push origin 'refs/gitmsg/*:refs/gitmsg/*'

# --- prune older artifact refs on origin (keep only latest) ---
git ls-remote origin 'refs/gitmsg/release/*/artifacts' \
  | awk -v keep="$NEW_ARTIFACT_REF" '$2 != keep {print $2}' \
  | while read -r ref; do
      git push origin --delete "$ref" || true
    done
