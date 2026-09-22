# macOS Code Signing

Darwin release binaries are signed with an Apple Developer ID Application certificate and notarized, so they start without a Gatekeeper prompt or a quarantine workaround.

[Pipeline](#pipeline) · [Credentials](#credentials) · [Rotation](#rotation) · [Verifying a release](#verifying-a-release) · [Troubleshooting](#troubleshooting)

## Pipeline

| Stage | Where | What runs |
|---|---|---|
| Sign | `.goreleaser.yaml`, `builds.darwin.hooks.post` | `rcodesign sign --code-signature-flags=runtime <binary>`, before archiving |
| Archive | `.goreleaser.yaml`, `archives.darwin` | each signed binary in a `.zip`; the notary rejects `.tar.gz` |
| Publish | `goreleaser release` | archives, SBOMs and checksums to the GitHub release |
| Notarize | `scripts/release.sh`, the build step | `rcodesign notary-submit --wait` on each `dist/*_darwin_*.zip`; the archive is unchanged |

[`rcodesign`](https://github.com/indygreg/apple-platform-rs) runs on all operating systems, so no macOS host is necessary; `scripts/release.sh` only checks that it is on PATH.

Keep the signing in the build hook. Do not use the `signs:` or `binary_signs:` blocks: they register each archive two times, and the GitHub upload then fails with `422 already_exists`.

## Credentials

`scripts/release.sh` reads these variables from the environment of the release machine, and its header lists all of them.

| Variable | Value |
|---|---|
| `APPLE_CERT_P12` | base64 of the Developer ID Application `.p12` |
| `APPLE_CERT_PASSWORD` | the `.p12` export password |
| `APPLE_API_KEY_P8` | the App Store Connect `.p8`, PEM headers included |
| `APPLE_API_KEY_ID` | the key id, from App Store Connect, Users and Access, Integrations, Team Keys |
| `APPLE_API_ISSUER_ID` | the issuer id, from the same page |

## Rotation

The certificate expires after one year:

1. Xcode, Settings, Accounts, Manage Certificates: add a Developer ID Application certificate.
2. Keychain Access: export it as `.p12` with a password.
3. Update `APPLE_CERT_P12` and `APPLE_CERT_PASSWORD` on the release machine.
4. After the next release, confirm `codesign -dv` on a downloaded archive shows the new serial number.
5. Revoke the old certificate only after that release. Apple allows two certificates at the same time.

The API key does not expire. Rotate it after an exposure:

1. Create a new Team Key with the Developer role.
2. Download the `.p8`; you can download it only one time.
3. Update the three `APPLE_API_*` variables.
4. After one release is notarized with the new key, revoke the old key.

## Verifying a release

From a Mac:

```bash
curl -fsSLO https://gitsocial.org/artifacts/<X.Y.Z>/gitsocial_<X.Y.Z>_darwin_arm64.zip   # or the GitHub release
unzip gitsocial_<X.Y.Z>_darwin_arm64.zip
codesign --verify --deep --strict --verbose=2 ./gitsocial     # valid on disk, satisfies its Designated Requirement
codesign -dv --verbose ./gitsocial 2>&1 | grep Authority       # Developer ID Application: Mukhsimjon Rakhimov (J33FZDK3T7), then the two Apple authorities
spctl --assess --type install --verbose ./gitsocial_<X.Y.Z>_darwin_arm64.zip   # notarization; assess the zip, not the binary
```

## Troubleshooting

- The notary returns `Invalid`: open the log URL that `rcodesign notary-submit` prints. The usual causes are a missing hardened-runtime flag, the wrong certificate type (Apple Development or Mac Developer instead of Developer ID Application), or an expired certificate.
- `brew install` warns about an unverified developer: the cask in `homebrew-tap` can point to an older release. Check it with `gh api repos/gitsocial-org/homebrew-tap/contents/Casks/gitsocial.rb --jq '.content' | base64 -d | grep version`. If goreleaser did not update it, give `GORELEASER_HOMEBREW_CASK_GITHUB_TOKEN` `repo` write access to that repository.
- `brew install` refuses an untrusted tap: Homebrew 5 needs `brew trust gitsocial-org/tap` one time, as the README says.
