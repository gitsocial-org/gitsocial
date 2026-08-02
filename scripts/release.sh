#!/usr/bin/env bash
# release.sh - The gitsocial release driver: owns an entire release end to end,
# run on the release machine as `scripts/release.sh vX.Y.Z`.
#
# Usage:
#   scripts/release.sh [--dry-run] vX.Y.Z
#
# Sequence (each step is idempotent — a mid-release failure is fixed by
# re-running the driver):
#   1. Preflight   — clean tree, up-to-date main, tests green, tools + full
#                    credential set present. Fails fast before anything ships.
#   2. Tag         — create vX.Y.Z locally (skip if it exists).
#   3. Build       — `goreleaser release --clean`: all-platform archives +
#                    checksums.txt + SBOMs into dist/, sign darwin binaries,
#                    publish to GitHub Releases, update Homebrew/Scoop taps
#                    (skip if the GitHub release already exists); then notarize
#                    the darwin zips.
#   4. Store       — upload dist archives + checksums.txt + SBOMs to the bucket
#                    at artifacts/<version>/ and advance artifacts/latest.txt.
#   5. Record      — create the gitmsg/release record carrying --artifact-url
#                    (skip if a record for the tag already exists).
#   6. Push        — tag + gitmsg/* + refs/gitmsg/* to the bucket (site) and the
#                    GitHub / GitLab / Codeberg mirrors.
#   7. install.sh  — upload scripts/install.sh to the bucket root (no-cache),
#                    keeping the published installer current.
#
# --dry-run prints every step (with the real commands) and touches nothing: no
# tag, no build, no publish, no upload. Working tree and git state stay clean.
#
# Required credentials/env (derived from .github/workflows/release.yml,
# mirror.yml and .goreleaser.yaml — this driver replaces that CI):
#   Apple signing + notarization (SIGNING.md):
#     APPLE_CERT_P12        base64 of the Developer ID Application .p12
#     APPLE_CERT_PASSWORD   password for that .p12
#     APPLE_API_KEY_P8      contents of the App Store Connect .p8 (PEM)
#     APPLE_API_KEY_ID      App Store Connect key id
#     APPLE_API_ISSUER_ID   App Store Connect issuer id
#   GitHub Releases + taps (goreleaser):
#     GITHUB_TOKEN                          publish releases on gitsocial-org/gitsocial
#     GORELEASER_HOMEBREW_CASK_GITHUB_TOKEN write access to the homebrew-tap repo
#     GORELEASER_SCOOP_GITHUB_TOKEN         write access to the scoop-bucket repo
#   Bucket (R2) — either the env pair, or a stored credentials.json entry for the
#   site remote's endpoint host (`gitsocial config credentials set site`):
#     GITSOCIAL_S3_ACCESS_KEY / GITSOCIAL_S3_SECRET_KEY
#   Git mirror push tokens:
#     GITLAB_PAT            push to gitlab.com/gitsocial-org/gitsocial
#     CODEBERG_PAT          push to codeberg.org/GitSocial/GitSocial
#
# Optional env:
#   GITSOCIAL_SITE_REMOTE  the s3 bucket remote name (default: site)
#   GITSOCIAL_ARTIFACT_BASE  base URL for the release record's artifact-url
#                          (default: the bucket remote's effective site url;
#                          set to https://gitsocial.org when the configured
#                          url is a temporary/soak domain)
#   GITSOCIAL_BINARY       gitsocial binary to drive the release (default: a
#                          fresh build of the current tree into bin/gitsocial,
#                          so the current CLI — including `site put` — is used)
set -euo pipefail

# --- mirror targets (match mirror.yml / .goreleaser.yaml) ---
GITHUB_REPO="gitsocial-org/gitsocial"
GITLAB_URL_PATH="gitlab.com/gitsocial-org/gitsocial.git"
CODEBERG_URL_PATH="codeberg.org/GitSocial/GitSocial.git"
SITE_REMOTE="${GITSOCIAL_SITE_REMOTE:-site}"

# --- arg parsing ---
DRY_RUN=false
TAG=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    -h|--help) sed -n '2,60p' "$0"; exit 0 ;;
    v*)        TAG="$arg" ;;
    *)         echo "error: unexpected argument: $arg" >&2; exit 2 ;;
  esac
done
[ -n "$TAG" ] || { echo "Usage: scripts/release.sh [--dry-run] vX.Y.Z" >&2; exit 2; }
case "$TAG" in
  v[0-9]*) ;;
  *) echo "error: tag must look like vX.Y.Z (got $TAG)" >&2; exit 2 ;;
esac
VERSION="${TAG#v}"

# --- output helpers ---
log()  { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
info() { printf '    %s\n' "$*"; }
skip() { printf '    (skip) %s\n' "$*"; }
die()  { printf '\033[31merror: %s\033[0m\n' "$*" >&2; exit 1; }
# fail aborts a real release, but only warns under --dry-run so a preview always
# runs to completion (and can be exercised on a machine without the secret set).
fail() { if $DRY_RUN; then printf '    (warn) %s\n' "$*"; else die "$*"; fi; }

# run executes a side-effecting command, echoing it first; under --dry-run it
# only echoes. Suitable for simple commands (no pipes/redirection).
run() {
  if $DRY_RUN; then
    printf '    [dry-run] %s\n' "$*"
  else
    printf '    + %s\n' "$*"
    "$@"
  fi
}

have() { command -v "$1" >/dev/null 2>&1; }

# require_env fails fast when a credential is missing (checked in preflight,
# before anything is published).
MISSING=""
require_env() { eval "v=\${$1:-}"; [ -n "$v" ] || MISSING="$MISSING $1"; }

# --- resolved paths ---
GS="${GITSOCIAL_BINARY:-bin/gitsocial}"

# ============================================================================
# 1. Preflight
# ============================================================================
preflight() {
  log "Preflight: $TAG"

  # Tooling.
  for tool in git go goreleaser syft rcodesign curl; do
    have "$tool" || fail "required tool not found on PATH: $tool"
  done
  info "tools checked: git go goreleaser syft rcodesign curl"

  # Credentials (documented in the header). Report everything missing at once.
  MISSING=""
  for c in APPLE_CERT_P12 APPLE_CERT_PASSWORD APPLE_API_KEY_P8 APPLE_API_KEY_ID \
           APPLE_API_ISSUER_ID GITHUB_TOKEN GORELEASER_HOMEBREW_CASK_GITHUB_TOKEN \
           GORELEASER_SCOOP_GITHUB_TOKEN GITLAB_PAT CODEBERG_PAT; do
    require_env "$c"
  done
  # R2 creds: env pair, or a stored credentials.json entry for the site remote.
  if [ -z "${GITSOCIAL_S3_ACCESS_KEY:-}" ] || [ -z "${GITSOCIAL_S3_SECRET_KEY:-}" ]; then
    if ! "$GS_FOR_CHECK" config credentials list 2>/dev/null | grep -q .; then
      MISSING="$MISSING R2-credentials(GITSOCIAL_S3_ACCESS_KEY/SECRET_KEY-or-'gitsocial-config-credentials-set-$SITE_REMOTE')"
    fi
  fi
  [ -z "$MISSING" ] && info "credential set present" || fail "missing credentials:$MISSING"

  # The site remote must exist (created in the runbook before the first release).
  if git remote get-url "$SITE_REMOTE" >/dev/null 2>&1; then
    info "bucket remote: $SITE_REMOTE"
  else
    fail "s3 bucket remote '$SITE_REMOTE' is not configured (set GITSOCIAL_SITE_REMOTE or add it)"
  fi

  # Clean working tree.
  [ -z "$(git status --porcelain)" ] || fail "working tree is not clean (commit or stash first)"

  # On up-to-date main (skip the network fetch under --dry-run).
  branch="$(git rev-parse --abbrev-ref HEAD)"
  [ "$branch" = "main" ] || fail "not on main (on $branch)"
  if $DRY_RUN; then
    printf '    [dry-run] git fetch origin main && verify main == origin/main\n'
  else
    git fetch origin main >/dev/null 2>&1 || die "git fetch origin main failed"
    [ "$(git rev-parse main)" = "$(git rev-parse origin/main)" ] \
      || die "main is not up to date with origin/main"
    info "on up-to-date main @ $(git rev-parse --short main)"
  fi

  # Tests green (real run only; dry-run just states it).
  if $DRY_RUN; then
    printf '    [dry-run] go test ./...\n'
  else
    info "running go test ./... (this can take a while)"
    go test ./... >/dev/null || die "go test ./... failed"
    info "tests green"
  fi
}

# ============================================================================
# 2. Tag
# ============================================================================
do_tag() {
  log "Tag: $TAG"
  if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
    skip "tag $TAG already exists"
  else
    run git tag -a "$TAG" -m "$TAG"
  fi
  # Push the annotated tag to GitHub before goreleaser runs: goreleaser
  # otherwise creates a lightweight tag with the release, and the later mirror
  # push is rejected (same commit, different ref value). Idempotent: re-pushing
  # the identical tag is a no-op.
  local github_url="https://x-access-token:${GITHUB_TOKEN:-}@github.com/${GITHUB_REPO}.git"
  if $DRY_RUN; then
    printf '    [dry-run] git push <GitHub> refs/tags/%s\n' "$TAG"
  else
    printf '    + git push <GitHub> refs/tags/%s\n' "$TAG"
    git push "$github_url" "refs/tags/$TAG" || die "tag push to GitHub failed"
  fi
}

# ============================================================================
# 3. Build + forge publish + notarize
# ============================================================================
# gh_release_exists returns 0 when the GitHub release for TAG already exists.
gh_release_exists() {
  curl -fsS -o /dev/null \
    -H "Authorization: Bearer $GITHUB_TOKEN" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/$GITHUB_REPO/releases/tags/$TAG"
}

# decode_apple materializes the Apple credential files goreleaser's sign hook
# (/tmp/cert.p12) and the notary step (/tmp/api-key.json) expect, mirroring
# release.yml's "Decode Apple credentials" step.
decode_apple() {
  if $DRY_RUN; then
    printf '    [dry-run] decode Apple credentials → /tmp/cert.p12, /tmp/api-key.json\n'
    return
  fi
  printf '%s' "$APPLE_CERT_P12" | base64 -d > /tmp/cert.p12
  printf '%s' "$APPLE_API_KEY_P8" > /tmp/api-key.p8
  rcodesign encode-app-store-connect-api-key -o /tmp/api-key.json \
    "$APPLE_API_ISSUER_ID" "$APPLE_API_KEY_ID" /tmp/api-key.p8
  info "Apple credentials decoded"
}

build_and_publish() {
  log "Build + forge publish: goreleaser release --clean"
  if ! $DRY_RUN && gh_release_exists; then
    skip "GitHub release $TAG already exists — not re-running goreleaser"
  else
    decode_apple
    # goreleaser inherits GITHUB_TOKEN, the tap tokens, and APPLE_CERT_PASSWORD
    # (the build sign hook) from the environment — all verified in preflight.
    # Ambient tokens for other forges are stripped: goreleaser publishes to
    # GitHub only, and refuses to run when multiple forge tokens are present.
    run env -u GITLAB_TOKEN -u GITEA_TOKEN goreleaser release --clean
  fi

  log "Notarize darwin archives"
  if $DRY_RUN; then
    printf '    [dry-run] rcodesign notary-submit --api-key-path=/tmp/api-key.json --wait dist/*_darwin_*.zip\n'
    return
  fi
  decode_apple
  local found=false
  for zip in dist/*_darwin_*.zip; do
    [ -e "$zip" ] || continue
    found=true
    run rcodesign notary-submit --api-key-path=/tmp/api-key.json --wait "$zip"
  done
  $found || skip "no dist/*_darwin_*.zip present (dist cleaned or build skipped) — nothing to notarize"
}

# ============================================================================
# 4. Store artifacts on the bucket
# ============================================================================
push_artifacts() {
  log "Store artifacts: artifacts/$VERSION/ on '$SITE_REMOTE'"
  if $DRY_RUN; then
    printf '    [dry-run] %s release artifacts push %s dist/*.tar.gz dist/*.zip dist/checksums.txt dist/*.sbom.json --remote %s\n' \
      "$GS" "$VERSION" "$SITE_REMOTE"
    return
  fi
  # Gather what goreleaser produced (globs expanded here so missing SBOMs don't
  # break the call). Re-pushing a version overwrites its objects — idempotent.
  local files=()
  for f in dist/*.tar.gz dist/*.zip dist/checksums.txt dist/*.sbom.json; do
    [ -e "$f" ] && files+=("$f")
  done
  [ ${#files[@]} -gt 0 ] || die "no dist artifacts found to push (run the build first)"
  run "$GS" release artifacts push "$VERSION" "${files[@]}" --remote "$SITE_REMOTE"
}

# ============================================================================
# 5. Create the release record
# ============================================================================
# record_exists returns 0 when a release record already pins this tag.
record_exists() {
  "$GS" release list --json 2>/dev/null | grep -qF "\"Tag\": \"$TAG\""
}

create_record() {
  log "Create release record: $TAG"
  if ! $DRY_RUN && record_exists; then
    skip "release record for $TAG already exists"
    return
  fi

  # Artifact base URL = the bucket remote's effective site url + artifacts/<ver>
  # (same value `release artifacts push` derives), so the record is born with it.
  # GITSOCIAL_ARTIFACT_BASE overrides the base when the configured site url is
  # temporary (e.g. a soak domain) but the durable record should carry the
  # canonical one.
  local site_url artifact_url
  if [ -n "${GITSOCIAL_ARTIFACT_BASE:-}" ]; then
    site_url="$GITSOCIAL_ARTIFACT_BASE"
  elif $DRY_RUN; then
    site_url="$("$GS" config site get url --remote "$SITE_REMOTE" 2>/dev/null || echo 'https://gitsocial.org/')"
  else
    site_url="$("$GS" config site get url --remote "$SITE_REMOTE")" \
      || die "no site url configured for '$SITE_REMOTE' (gitsocial config site set url https://...)"
  fi
  artifact_url="${site_url%/}/artifacts/$VERSION"

  # Artifact filenames (archives) and the SBOM/checksums names for the record.
  local artifacts="" sbom=""
  if ! $DRY_RUN; then
    artifacts="$(cd dist && ls ./*.tar.gz ./*.zip 2>/dev/null | sed 's|^\./||' | paste -sd, -)"
    sbom="$(cd dist && ls ./*.sbom.json 2>/dev/null | head -1 | sed 's|^\./||')"
    "$GS" release init >/dev/null 2>&1 || true
  fi

  # Notes: the tag line plus the changelog since the previous tag.
  local notes
  notes="$(mktemp)"
  { echo "$TAG"; echo; } > "$notes"
  local prev_tag
  prev_tag="$(git describe --tags --abbrev=0 "$TAG^" 2>/dev/null || echo "")"
  if [ -n "$prev_tag" ]; then
    git log "$prev_tag..$TAG" --oneline --no-decorate >> "$notes" 2>/dev/null || true
  else
    git log "$TAG" --oneline --no-decorate >> "$notes" 2>/dev/null || true
  fi

  if $DRY_RUN; then
    printf '    [dry-run] %s release create - --tag %s --version %s --artifacts <archives> --checksums checksums.txt --sbom <sbom> --artifact-url %s < <changelog>\n' \
      "$GS" "$TAG" "$VERSION" "$artifact_url"
    rm -f "$notes"
    return
  fi

  local args=(--tag "$TAG" --version "$VERSION" --artifacts "$artifacts"
              --checksums checksums.txt --artifact-url "$artifact_url")
  [ -n "$sbom" ] && args+=(--sbom "$sbom")
  printf '    + %s release create - %s < %s\n' "$GS" "${args[*]}" "$notes"
  "$GS" release create - "${args[@]}" < "$notes"
  rm -f "$notes"
}

# ============================================================================
# 6. Push everywhere
# ============================================================================
push_everywhere() {
  log "Push to bucket + mirrors"

  # Bucket: `gitsocial push` publishes data (tag, gitmsg/* branches,
  # refs/gitmsg/*, code) AND regenerates the site, so the release page, the
  # artifacts, and latest.txt go live together.
  run "$GS" push "$SITE_REMOTE"

  # Git mirrors: the tag, gitmsg/* branches, and refs/gitmsg/* to each, via
  # token-embedded URLs (never persisted as named remotes). Pushing an
  # already-present ref is a no-op, so re-runs are safe.
  local github_url="https://x-access-token:${GITHUB_TOKEN:-}@github.com/${GITHUB_REPO}.git"
  local gitlab_url="https://oauth2:${GITLAB_PAT:-}@${GITLAB_URL_PATH}"
  local codeberg_url="https://${CODEBERG_PAT:-}@${CODEBERG_URL_PATH}"

  push_mirror "GitHub"   "$github_url"
  push_mirror "GitLab"   "$gitlab_url"
  push_mirror "Codeberg" "$codeberg_url"
}

# push_mirror sends the release refs to one git mirror (redacting the token in
# the echoed command).
push_mirror() {
  local name="$1" url="$2"
  local refspecs=("refs/tags/$TAG"
                  "refs/heads/gitmsg/*:refs/heads/gitmsg/*"
                  "refs/gitmsg/*:refs/gitmsg/*")
  if $DRY_RUN; then
    printf '    [dry-run] git push <%s> %s\n' "$name" "${refspecs[*]}"
    return
  fi
  printf '    + git push <%s> %s\n' "$name" "${refspecs[*]}"
  git push "$url" "${refspecs[@]}"
}

# ============================================================================
# 7. install.sh to the bucket root
# ============================================================================
upload_installer() {
  log "Upload install.sh to the bucket root (no-cache)"
  run "$GS" site put install.sh scripts/install.sh \
    --remote "$SITE_REMOTE" --content-type text/plain
}

# ============================================================================
# Driver
# ============================================================================
main() {
  $DRY_RUN && log "DRY RUN — no tag, build, publish, or upload will happen"

  # Build the driver's gitsocial from the current tree (unless overridden), so
  # the current CLI (including `site put`) is used. bin/ is gitignored, so this
  # never dirties the working tree. In dry-run, use whatever is on PATH for the
  # (skipped) read-only checks and command display.
  if [ -n "${GITSOCIAL_BINARY:-}" ]; then
    GS_FOR_CHECK="$GITSOCIAL_BINARY"
  elif $DRY_RUN; then
    GS_FOR_CHECK="$(command -v gitsocial || echo "$GS")"
    GS="$GS_FOR_CHECK"
  else
    log "Build driver binary: bin/gitsocial (current tree)"
    go build -o bin/gitsocial ./cli/gitsocial || die "go build ./cli/gitsocial failed"
    GS="bin/gitsocial"
    GS_FOR_CHECK="bin/gitsocial"
  fi

  preflight
  do_tag
  build_and_publish
  push_artifacts
  create_record
  push_everywhere
  upload_installer

  log "Release $TAG complete"
  $DRY_RUN && info "(dry run — nothing was changed)"
}

main
