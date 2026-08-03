#!/usr/bin/env bash
# mirror.sh - Mirror the local repo (branches, tags, refs/gitmsg/*) to a remote.
#
# Usage: scripts/mirror.sh <remote-name> <remote-url>
#
# The local workspace is the source of truth; mirrors (GitHub, GitLab,
# Codeberg) are passive copies, so everything is force-pushed as-is and
# nothing is fetched back from the mirror.
set -euo pipefail

REMOTE="${1:?Usage: scripts/mirror.sh <remote-name> <remote-url>}"
URL="${2:?Usage: scripts/mirror.sh <remote-name> <remote-url>}"

# --- add remote if needed ---
if ! git remote get-url "$REMOTE" >/dev/null 2>&1; then
  git remote add "$REMOTE" "$URL"
fi

# --- push branches, tags, and gitmsg state refs ---
git push "$REMOTE" --all --force
git push "$REMOTE" --tags --force
git push "$REMOTE" 'refs/gitmsg/*:refs/gitmsg/*' --force
