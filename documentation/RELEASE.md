# Release Extension

Releases stored as commits on the `gitmsg/release` branch, each pinning a git tag, a semver version, and optional artifacts, checksums, SBOM, and signing key.

> **Spec:** [GITRELEASE.md](../specs/GITRELEASE.md) — wire format for releases, artifacts, checksums, signatures, and SBOM.

## Initialize

```
gitsocial release init                # creates refs/gitmsg/release/config and the gitmsg/release branch
gitsocial release init -b <branch>    # initialize on a custom branch
gitsocial release config get / set / list
```

`init` is idempotent. The release branch holds release records; the actual code is identified via `tag`.

## Create

```
gitsocial release create "v1.0.0 — initial release" \
    --tag v1.0.0 --version 1.0.0 \
    --artifacts dist/app-linux-amd64.tar.gz,dist/app-darwin-arm64.tar.gz \
    --artifact-url https://releases.example.com/v1.0.0 \
    --checksums SHA256SUMS \
    --sbom sbom.spdx.json \
    --signed-by 0xABCD1234 \
    --prerelease
```

- `--tag` MUST already exist in the repo (release records pin existing tags).
- `--allow-duplicate` lets multiple release records share a tag (rare; useful when re-publishing artifacts).
- `--artifact-url` is the base URL — actual artifact filenames come from `--artifacts`.

## Edit and retract

```
gitsocial release edit <ref> --body "Updated release notes..."
gitsocial release edit <ref> --artifacts <new-list>
gitsocial release retract <ref>
```

Edits use the core versioning chain; the latest version wins in `release list`.

## Query

```
gitsocial release list                       # newest first (filter via --json + jq if needed)
gitsocial release show <ref>
gitsocial release artifacts list <ref>       # list artifacts + their hosted URLs
gitsocial release artifacts add <ref> ...    # attach more artifacts to an existing release
gitsocial release artifacts export <ref>     # download artifacts to a local directory
gitsocial release sbom <ref>                 # show parsed SBOM details
```

`artifacts list` and `artifacts export` read the git artifact ref first; for externally hosted releases (`artifact-url` set, no git ref) they fall back to the release record's `artifacts` filenames and download from `<artifact-url>/<filename>` (GITRELEASE.md §3.2).

## Push artifacts to an s3 remote

```
gitsocial release artifacts push <version> <file...> [--remote <name>]
```

Uploads the given files as plain bucket objects at `artifacts/<version>/<basename>` under the s3 push remote's prefix (`--remote` overrides; the default resolves like `gitsocial push`). A leading `v` on the version is tolerated (`v1.4.2` → `1.4.2`). Re-pushing a version silently overwrites its objects, so a failed release run is fixed by re-running.

Semantics:

- **`artifacts/latest.txt`** — after a successful upload, rewritten to the bare pushed version (trailing newline) iff it is a **newer non-prerelease** than the current content (missing/unparsable content always advances; a prerelease suffix like `-rc1` never does, though its artifacts still upload). Install scripts resolve the newest version from this key.
- **`artifact-url` catch-up** — the artifact base URL is the remote's effective site `url` (per-remote override wins) + `artifacts/<version>`. If the release record for the version exists without an `artifact-url`, it is set via a release edit; a missing record is skipped silently (create it with `release create --artifact-url` before or after). Without a configured site `url` the command fails: set it with `gitsocial config site set url https://...`.
- Version-directory objects are cached as immutable; `latest.txt` is `no-cache` (see [S3.md](S3.md) cache policy). The `artifacts/` prefix is part of the bucket's reserved root namespace — site maintenance never touches it.

The SBOM view parses an SPDX/CycloneDX file and reports format, package count, generator, license summary, and per-package items. Parsed summaries are cached in `release_sbom_cache` (keyed by repo URL + version).

## How releases surface in queries

Default `release list` filters:

- Excludes retracted (latest version wins).
- Excludes commits removed from the source branch.
- Order: by tag if semver-sortable, otherwise by timestamp, newest first.

## Notifications

New releases on subscribed repositories surface through core notifications. See [NOTIFICATIONS.md](NOTIFICATIONS.md).

## Operational checks

```bash
gitsocial release config get

# All releases on a repository
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT tag, version, prerelease FROM release_items_resolved
     WHERE repo_url = ? ORDER BY timestamp DESC"

# SBOM cache for a version
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT format, packages, generator FROM release_sbom_cache
     WHERE repo_url = ? AND version = ?"
```

The TUI's release section (`R` from any screen — Releases) lists releases with detail and SBOM views.
