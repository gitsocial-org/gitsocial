# macOS Code Signing & Notarization

Darwin release binaries (`darwin_amd64` + `darwin_arm64`) are signed with an Apple **Developer ID Application** certificate and notarized by Apple before publication. Users installing via `brew install gitsocial-org/tap/gitsocial` or downloading a release archive get a binary that launches without a Gatekeeper prompt and without needing `xattr -d com.apple.quarantine`.

## How it's wired

| Stage | Where | What runs |
| --- | --- | --- |
| Sign the binary | `.goreleaser.yaml` → `builds.darwin.hooks.post` | `rcodesign sign --p12-file=/tmp/cert.p12 --p12-password=… --code-signature-flags=runtime <binary>` runs during the build phase, before archiving, so the `.zip` wraps a signed binary |
| Archive | `.goreleaser.yaml` → `archives.darwin` | Each signed binary is wrapped in `.zip` (Apple's notary won't accept `.tar.gz`) |
| Publish | `goreleaser release` | Uploads archives + SBOMs + checksums to the GitHub release for the tag |
| Notarize | `.github/workflows/release.yml` → `Notarize macOS archives` | `rcodesign notary-submit --wait` runs against each `dist/*_darwin_*.zip` after goreleaser publishes. Apple registers the cdhash centrally; the archive itself is unchanged |

Signing uses [`rcodesign`](https://github.com/indygreg/apple-platform-rs) — a Linux-compatible signer/notarizer pinned to v0.29.0 in the workflow. CI runs on `ubuntu-latest`; no macOS runner needed.

## GitHub repo secrets

Set under Settings → Secrets and variables → Actions:

| Secret | Value |
| --- | --- |
| `APPLE_CERT_P12` | base64 of the Developer ID Application `.p12` (`base64 -i cert.p12`) |
| `APPLE_CERT_PASSWORD` | password set when exporting the `.p12` from Keychain |
| `APPLE_API_KEY_P8` | full contents of the App Store Connect `.p8` (PEM, headers and all) |
| `APPLE_API_KEY_ID` | Key ID (10 alphanumeric chars, shown in App Store Connect → Users and Access → Integrations → Team Keys) |
| `APPLE_API_ISSUER_ID` | Issuer UUID (shown on the same page) |

## Cert rotation (annual)

Apple Developer ID Application certificates expire after one year. Renewal:

1. **Generate a new cert** — Xcode → Settings → Accounts → Manage Certificates → "+" → *Developer ID Application*. Xcode creates the cert and private key in your login keychain.
2. **Export** — Keychain Access → right-click *Developer ID Application: …* → Export as `.p12`, set a password.
3. **Update secrets** — overwrite `APPLE_CERT_P12` (base64) and `APPLE_CERT_PASSWORD`.
4. **Verify on the next tagged release** — confirm `codesign -dv` on a downloaded archive shows the new certificate's serial number.
5. **Don't revoke the old cert** until the new one has shipped one successful release. Apple lets up to two Developer ID Application certs coexist on the account.

## App Store Connect API key rotation

The `.p8` API key never expires, but rotate after any suspected exposure (e.g. accidental paste into a log, ticket, or chat).

1. **Generate** — appstoreconnect.apple.com → Users and Access → Integrations → Team Keys → "+". Role: **Developer**. Download the `.p8` (one-time download).
2. **Record** Key ID and Issuer ID from the same page.
3. **Update secrets** — overwrite `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`.
4. **Revoke the old key** in App Store Connect once the new key has been used to notarize one successful release.

## Verifying a downloaded release

Anyone — user, fork operator, auditor — can confirm a release archive's signature and notarization status from a Mac:

```bash
curl -fsSLO https://github.com/gitsocial-org/gitsocial/releases/download/v<X.Y.Z>/gitsocial_<X.Y.Z>_darwin_arm64.zip
unzip gitsocial_<X.Y.Z>_darwin_arm64.zip

# signature check (what Gatekeeper runs)
codesign --verify --deep --strict --verbose=2 ./gitsocial
codesign -dv --verbose ./gitsocial 2>&1 | grep Authority
```

Expected:
- `valid on disk` + `satisfies its Designated Requirement`
- Three `Authority=…` lines: `Developer ID Application: Mukhsimjon Rakhimov (J33FZDK3T7)`, `Developer ID Certification Authority`, `Apple Root CA`

For notarization status, run `spctl --assess --type install --verbose ./gitsocial.zip` against the `.zip` (not the binary — `spctl -t exec` misreports CLI binaries as "not an app").

## Why a build hook, not `signs:` / `binary_signs:`

The natural goreleaser pattern for code signing is one of those blocks with `signature: "${artifact}"` to indicate in-place signing. Don't reach for it here. With that pattern, goreleaser registers the artifact a second time as a signature pointing at the same path; the GitHub release upload then POSTs each darwin archive twice and the second attempt fails with `422 already_exists`. The CI release fails at the publish step even though signing itself succeeded. The current config dodges this by signing in `builds.darwin.hooks.post` (no artifact-list registration) and doing notarization as a separate post-goreleaser CI step (`Notarize macOS archives` in `release.yml`).

## Troubleshooting

**Apple notary returns `Invalid`**
Open the notary log URL printed by `rcodesign notary-submit`. Common causes:
- Binary missing the hardened runtime flag → add `--code-signature-flags=runtime` to the build hook
- Wrong Authority chain → cert is wrong type (use *Developer ID Application*, not *Apple Development* or *Mac Developer*)
- Cert expired → rotate (see above)

**CI passes but `brew install` still warns about an unverified developer**
The cask in `homebrew-tap` may be pointing at an older release. Check the cask: `gh api repos/gitsocial-org/homebrew-tap/contents/Casks/gitsocial.rb --jq '.content' | base64 -d | grep version`. If goreleaser didn't update it, the `GORELEASER_HOMEBREW_CASK_GITHUB_TOKEN` secret may have insufficient scope (needs `repo` write on `homebrew-tap`).

**`brew install` says "Refusing to load cask from untrusted tap"**
Homebrew 5.0+ requires explicit trust for casks in third-party taps. Users hit this on first install. The README documents the one-time `brew trust gitsocial-org/tap` step. This isn't fixable by changing our signing — it's a Homebrew client-side policy. Switching to a Formula (`brews:` in goreleaser) would avoid the prompt but `brews:` is deprecated since goreleaser v2.10 and slated for removal; current trade-off favors keeping `homebrew_casks:` and documenting the trust step.
