#!/usr/bin/env bash
# bundle-darwin.sh - The goreleaser post hook of both darwin builds: signs the bare gitsocial, and makes GitSocial.app from the notifier.
# Usage: scripts/bundle-darwin.sh <binary> <arch> <version>
# The notifier of an arch gives the bundle at dist/bundle/<arch>/GitSocial.app, which is signed; with no APPLE_CERT_PASSWORD the signature is ad hoc.
set -euo pipefail
binary="$1" arch="$2" version="$3"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stage="$root/dist/bundle/$arch"
app="$stage/GitSocial.app"
name="$(basename "$binary")"

# sign signs a file or a bundle with the Developer ID of /tmp/cert.p12, or ad hoc with no password.
sign() {
	if [ -n "${APPLE_CERT_PASSWORD:-}" ]; then
		rcodesign sign --p12-file=/tmp/cert.p12 --p12-password="$APPLE_CERT_PASSWORD" --code-signature-flags=runtime "$1"
	else
		echo "bundle-darwin: no APPLE_CERT_PASSWORD, signing $(basename "$1") ad hoc" >&2
		rcodesign sign --code-signature-flags=runtime "$1"
	fi
}

# The zip has the bare gitsocial one time; the app holds only the notifier.
if [ "$name" = gitsocial ]; then
	sign "$binary"
	exit 0
fi
mkdir -p "$app/Contents/MacOS"
cp "$binary" "$app/Contents/MacOS/$name"

mkdir -p "$app/Contents/Resources"
xcrun actool "$root/documentation/images/GitSocial.icon" --compile "$app/Contents/Resources" \
	--platform macosx --minimum-deployment-target 26.0 --app-icon GitSocial \
	--output-partial-info-plist "$stage/icon.plist" >/dev/null
# actool writes GitSocial.icns next to Assets.car, and CFBundleIconFile names it for macOS before 26.
[ -f "$app/Contents/Resources/GitSocial.icns" ] || { echo "bundle-darwin: actool wrote no GitSocial.icns" >&2; exit 1; }
cat >"$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>org.gitsocial.GitSocial</string>
	<key>CFBundleName</key><string>GitSocial</string>
	<key>CFBundleDisplayName</key><string>GitSocial</string>
	<key>CFBundleExecutable</key><string>gitsocial-notifier</string>
	<key>CFBundleIconFile</key><string>GitSocial</string>
	<key>CFBundleIconName</key><string>GitSocial</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$version</string>
	<key>CFBundleVersion</key><string>$version</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSUIElement</key><true/>
</dict>
</plist>
EOF
sign "$app"
