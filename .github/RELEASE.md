# Release Process

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions runs [GoReleaser](https://goreleaser.com/) which builds cross-platform binaries, creates a GitHub Release with checksums, and updates the Homebrew tap and Scoop bucket.

Tags containing `-` (e.g. `v0.1.0-beta.1`) are marked as pre-release.

## Version Format

Follow [Semantic Versioning](https://semver.org/): `MAJOR.MINOR.PATCH`

- Pre-release: `v0.1.0-beta.1`, `v0.1.0-rc.1`
- Stable: `v0.1.0`, `v1.0.0`
