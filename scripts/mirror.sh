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

# --- fetch gitmsg refs + LFS from origin ---
git fetch origin '+refs/heads/gitmsg/*:refs/heads/gitmsg/*' '+refs/gitmsg/*:refs/gitmsg/*'
git lfs fetch origin --all

# --- push everything ---
git push "$REMOTE" --all --force
git push "$REMOTE" --tags --force

git lfs push "$REMOTE" --all || true
for ref in $(git for-each-ref --format='%(refname)' refs/gitmsg/); do
  git lfs push "$REMOTE" "$ref" 2>/dev/null || true
done

git push "$REMOTE" 'refs/gitmsg/*:refs/gitmsg/*' --force
