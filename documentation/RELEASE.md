# Release Extension

A release is a commit on the `gitmsg/release` branch ([GITRELEASE.md](../specs/GITRELEASE.md)) that pins a tag and a version and may carry artifacts, checksums, an SBOM and a signing key.

[Initialize](#initialize) · [Create](#create) · [Edit and retract](#edit-and-retract) · [Query](#query) · [Artifacts](#artifacts) · [Reference](#reference)

## Initialize

```
gitsocial release init [-b <branch>]         # refs/gitmsg/release/config and the gitmsg/release branch
gitsocial release config get|set|list
```

`init` is idempotent.

## Create

```
gitsocial release create "v1.0.0" --tag v1.0.0 --version 1.0.0 \
    --artifacts dist/app-linux-amd64.tar.gz,dist/app-darwin-arm64.tar.gz \
    --artifact-url https://releases.example.com/v1.0.0 \
    --checksums SHA256SUMS --sbom sbom.spdx.json --signed-by 0xABCD1234 --prerelease
```

- The tag must exist. `--allow-duplicate` lets a second record share it.
- `--artifact-url` is the base; the file names come from `--artifacts`.
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
gitsocial release artifacts record <ref> <file...>      # add artifact names to a record, no upload
gitsocial release artifacts export <ref>                # download the artifacts
gitsocial release sbom <ref>                            # format, packages, generator, licenses
```

`artifacts list` and `export` read the git artifact ref when there is one and otherwise download from `<artifact-url>/<name>`.

## Artifacts

```
gitsocial release artifacts push <version> <file...> [--remote <name>]
```

- Uploads the files to `artifacts/<version>/<name>` on the s3 push remote; a leading `v` is dropped. Re-pushing a version overwrites its objects.
- `artifacts/latest.txt` advances to the version when it is a newer non-prerelease; install scripts read it.
- The release record gains an `artifact-url` of the remote's site `url` plus `artifacts/<version>`. Without a site `url` the command fails; a missing record is skipped.
- Version objects are immutable and `latest.txt` is `no-cache` ([S3.md](S3.md#cache-policy)).

## Reference

- Artifact storage, checksums and SBOM: [GITRELEASE.md §3](../specs/GITRELEASE.md#3-artifact-storage).
- `release list` excludes retracted records ([GITMSG.md §1.5](../specs/GITMSG.md#15-versioning)) and commits no longer on their branch ([ARCHITECTURE.md](ARCHITECTURE.md#cache)), ordered by tag when the tags sort as semver, else by time, newest first.
- SBOM summaries are cached in `release_sbom_cache` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)).
- New releases on followed repositories raise [notifications](NOTIFICATIONS.md#types).
- In the TUI, `V` opens releases ([TUI-KEYS.md](TUI-KEYS.md#release-extension)).
