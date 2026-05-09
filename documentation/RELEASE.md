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
