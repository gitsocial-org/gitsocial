#!/usr/bin/env bash
# mirror.sh - Mirror repo (branches, tags, LFS, gitmsg refs) to a remote
#
# Usage: scripts/mirror.sh <remote-name> <remote-url>
set -euo pipefail

REMOTE="${1:?Usage: scripts/mirror.sh <remote-name> <remote-url>}"
URL="${2:?Usage: scripts/mirror.sh <remote-name> <remote-url>}"

# --- add remote if needed ---
if ! git remote get-url "$REMOTE" >/dev/null 2>&1; then
  git remote add "$REMOTE" "$URL"
fi

# --- fetch gitmsg refs from origin (LFS only for the latest artifact ref) ---
git fetch origin '+refs/heads/gitmsg/*:refs/heads/gitmsg/*' '+refs/gitmsg/*:refs/gitmsg/*'

LATEST_ARTIFACT_REF=$(git ls-remote origin 'refs/gitmsg/release/*/artifacts' \
  | awk '{print $2}' | sort -V | tail -1)
if [ -n "$LATEST_ARTIFACT_REF" ]; then
  git lfs fetch origin "$LATEST_ARTIFACT_REF"
fi

# --- locally drop older artifact refs so we don't push refs whose LFS objects we don't have ---
git for-each-ref --format='%(refname)' 'refs/gitmsg/release/*/artifacts' \
  | awk -v keep="$LATEST_ARTIFACT_REF" '$0 != keep {print}' \
  | while read -r ref; do
      git update-ref -d "$ref"
    done

# --- push branches and tags (no LFS pointers in these) ---
git push "$REMOTE" --all --force
git push "$REMOTE" --tags --force

# --- LFS objects must be uploaded before pushing refs that reference them ---
if [ -n "$LATEST_ARTIFACT_REF" ]; then
  git lfs push "$REMOTE" "$LATEST_ARTIFACT_REF" || true
fi

git push "$REMOTE" 'refs/gitmsg/*:refs/gitmsg/*' --force

# --- prune older artifact refs on the mirror (we no longer have them locally, so push didn't delete them) ---
# Skip if we couldn't determine latest, to avoid wiping the mirror on a transient ls-remote failure.
if [ -n "$LATEST_ARTIFACT_REF" ]; then
  git ls-remote "$REMOTE" 'refs/gitmsg/release/*/artifacts' \
    | awk -v keep="$LATEST_ARTIFACT_REF" '$2 != keep {print $2}' \
    | while read -r ref; do
        git push "$REMOTE" --delete "$ref" || true
      done
fi
