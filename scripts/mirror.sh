#!/usr/bin/env bash
# mirror.sh - Mirror repo (branches, tags, LFS, gitmsg refs) to a remote
#
# Usage: scripts/mirror.sh <remote-name> <remote-url>
#
# After pushing, prunes orphan release-artifact refs AND their LFS blobs on
# the mirror — neither Codeberg nor GitLab auto-cleans orphan LFS bytes for
# repo owners after a ref is deleted, so we trigger their cleanup endpoints
# explicitly. Without this, every release leaves ~50 MiB of dead bytes on
# each mirror.
set -euo pipefail

REMOTE="${1:?Usage: scripts/mirror.sh <remote-name> <remote-url>}"
URL="${2:?Usage: scripts/mirror.sh <remote-name> <remote-url>}"

# --- credential-embedded URL parsing (https://[user:]token@host/path[.git]) ---
parse_token() {
  local creds="${1#https://}"; creds="${creds%%@*}"
  if [[ "$creds" == *:* ]]; then echo "${creds#*:}"; else echo "$creds"; fi
}
parse_host() {
  local rest="${1#https://}"; rest="${rest#*@}"
  echo "${rest%%/*}"
}
parse_repo_path() {
  local rest="${1#https://}"; rest="${rest#*@}"; rest="${rest#*/}"
  echo "${rest%.git}"
}

# Forgejo (Codeberg) doesn't expose a per-OID LFS DELETE in /api/v1; the
# trash button on /settings/lfs posts to /{owner}/{repo}/settings/lfs/delete,
# which is a UI route protected by CSRF. We GET the settings page first to
# extract the token, then POST each delete with that token + a session cookie.
codeberg_delete_lfs() {
  local host="$1" repo_path="$2" token="$3"; shift 3
  local oids="$*"
  [ -z "$oids" ] && return 0

  local jar; jar=$(mktemp); trap "rm -f '$jar'" RETURN

  local html
  if ! html=$(curl -sf -c "$jar" \
      -H "Authorization: token $token" \
      "https://$host/$repo_path/settings/lfs"); then
    echo "warning: GET settings/lfs failed; skipping LFS cleanup on $host" >&2
    return 0
  fi

  local csrf
  csrf=$(printf '%s\n' "$html" \
    | grep -oE 'name="_csrf" value="[^"]*"' \
    | head -1 | sed 's/.*value="//; s/".*//')
  if [ -z "$csrf" ]; then
    echo "warning: no CSRF token in settings/lfs; skipping cleanup on $host" >&2
    return 0
  fi

  local oid deleted=0
  for oid in $oids; do
    if curl -sf -b "$jar" \
        -H "Authorization: token $token" \
        --data-urlencode "_csrf=$csrf" \
        --data-urlencode "oid=$oid" \
        "https://$host/$repo_path/settings/lfs/delete" -o /dev/null; then
      deleted=$((deleted + 1))
    fi
  done
  echo "  freed $deleted orphan LFS object(s) on $host"
}

# GitLab's per-OID LFS DELETE isn't public; project housekeeping covers
# orphan LFS as part of its cleanup pass on managed gitlab.com.
gitlab_trigger_housekeeping() {
  local host="$1" repo_path="$2" token="$3"
  local encoded; encoded=$(printf '%s' "$repo_path" | sed 's|/|%2F|g')
  if curl -sf -X POST \
      -H "PRIVATE-TOKEN: $token" \
      "https://$host/api/v4/projects/$encoded/housekeeping" \
      -o /dev/null; then
    echo "  housekeeping triggered on $host"
  fi
}

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

# --- collect orphan LFS OIDs from refs the mirror still has but origin doesn't.
#     Done BEFORE we drop local refs and BEFORE we push (the mirror's set of
#     orphan refs is the one we're about to clean up). Fetch each orphan ref
#     just far enough to read its tree — LFS data isn't needed, since
#     git lfs ls-files reads the pointer text files only. ---
ORPHAN_OIDS=""
if [ -n "$LATEST_ARTIFACT_REF" ]; then
  ORPHAN_REFS_ON_MIRROR=$(git ls-remote "$REMOTE" 'refs/gitmsg/release/*/artifacts' \
    | awk -v keep="$LATEST_ARTIFACT_REF" '$2 != keep {print $2}')
  for ref in $ORPHAN_REFS_ON_MIRROR; do
    git fetch "$REMOTE" "+$ref:$ref" 2>/dev/null || continue
    oids=$(git lfs ls-files -l "$ref" 2>/dev/null | awk '{print $1}' || true)
    ORPHAN_OIDS="$ORPHAN_OIDS $oids"
  done
  # Dedupe and exclude OIDs that LATEST still references (defensive — a release
  # could reuse an OID across versions if the artifact was bit-identical).
  if [ -n "${ORPHAN_OIDS// }" ]; then
    KEEP_OIDS=$(git lfs ls-files -l "$LATEST_ARTIFACT_REF" 2>/dev/null | awk '{print $1}' | sort -u || true)
    ORPHAN_OIDS=$(printf '%s\n' $ORPHAN_OIDS \
      | sort -u \
      | grep -vxFf <(printf '%s\n' $KEEP_OIDS) || true)
  fi
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

# --- free the LFS storage backing those orphan refs.
#     Without this step, deleting the ref leaves all the LFS bytes on
#     Codeberg/GitLab indefinitely; over many releases this accumulates
#     into hundreds of MiB of dead bytes. ---
if [ -n "${ORPHAN_OIDS// }" ]; then
  TOKEN=$(parse_token "$URL")
  HOST=$(parse_host "$URL")
  REPO_PATH=$(parse_repo_path "$URL")
  case "$HOST" in
    codeberg.org)
      codeberg_delete_lfs "$HOST" "$REPO_PATH" "$TOKEN" $ORPHAN_OIDS
      ;;
    gitlab.com|gitlab.*)
      gitlab_trigger_housekeeping "$HOST" "$REPO_PATH" "$TOKEN"
      ;;
  esac
fi
