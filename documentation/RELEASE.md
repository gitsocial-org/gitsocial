# Release Extension

A release is a commit on the `gitmsg/release` branch ([GITRELEASE.md](../specs/GITRELEASE.md)) that names a tag and a version, and can also have artifacts, checksums, an SBOM and a signing key.

[Initialize](#initialize) · [Create](#create) · [Edit and retract](#edit-and-retract) · [Query](#query) · [Artifacts](#artifacts) · [Reference](#reference)

## Initialize

```
gitsocial release init
gitsocial release config get|set|list
```

`init` is idempotent. It creates `refs/gitmsg/release/config` and the `gitmsg/release` branch.

## Create

```
gitsocial release create "v1.0.0" --tag v1.0.0 --version 1.0.0 \
    --artifacts dist/app-linux-amd64.tar.gz,dist/app-darwin-arm64.tar.gz \
    --artifact-url https://releases.example.com/v1.0.0 \
    --checksums SHA256SUMS --sbom sbom.spdx.json --signed-by 0xABCD1234 --prerelease
```

- The tag must exist. `--allow-duplicate` lets a second release use the same tag.
- `--artifact-url` is the base URL for the file names in `--artifacts`.
- The body can come from stdin: `cat CHANGELOG.md | gitsocial release create - --tag v1.0.0 --version 1.0.0`.

## Edit and retract

```
gitsocial release edit <ref> --body "Updated notes" [--artifacts ...] [--prerelease]
gitsocial release retract <ref>
```

## Query

```
gitsocial release list [-n 20] [-r <repo>]
gitsocial release show <ref>
gitsocial release artifacts list <ref>
gitsocial release artifacts record <ref> <file...>      # add artifact names to a release, no upload
gitsocial release artifacts export <ref>                # download the artifacts
gitsocial release sbom <ref>                            # format, packages, generator, licenses
```

`artifacts list` and `export` read the git artifact ref if it exists, and otherwise download from `<artifact-url>/<name>`.

## Artifacts

```
gitsocial release artifacts push <version> <file...> [--remote <name>]
```

- Uploads the files to `artifacts/<version>/<name>` on the s3 push remote and removes a leading `v` from the version. A second push of a version overwrites its objects.
- If the version is not a prerelease and is newer than the version in `artifacts/latest.txt`, or that file does not exist, the command writes it to `latest.txt`, which install scripts read.
- If the release of the version has no `artifact-url`, the command sets it to the site `url` of the remote plus `artifacts/<version>`, and fails if the site has no `url`. When no release has the version, the command skips this step.
- Version objects are immutable and `latest.txt` is `no-cache` ([S3.md](S3.md#cache-policy)).

## Reference

- Artifact storage, checksums and SBOM: [GITRELEASE.md §3](../specs/GITRELEASE.md#3-artifact-storage).
- `release list` excludes retracted releases ([GITMSG.md §1.5](../specs/GITMSG.md#15-versioning)) and stale commits ([ARCHITECTURE.md](ARCHITECTURE.md#cache)). It sorts by tag when the tags are semver, and otherwise by time, newest first.
- SBOM summaries are cached in `release_sbom_cache` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)).
- New releases on followed repositories create [notifications](NOTIFICATIONS.md#types).
- In the TUI, `V` opens releases ([TUI-KEYS.md](TUI-KEYS.md#release-extension)).
