#!/usr/bin/env bash
# Builds every release artifact into dist/:
#   Patchbay-<v>-macOS.dmg                 universal (Apple silicon + Intel) menu-bar app
#   Patchbay-<v>-windows-{x64,arm64}.zip   tray app (no console window) + CLI
#   patchbay_<v>_{darwin_universal,linux_amd64,linux_arm64}.tar.gz  CLI (tray-enabled)
#   checksums.txt                          SHA-256 of all of the above
#
# Runs on macOS: the .app needs cgo for the Cocoa menu bar, and sips, iconutil,
# lipo, codesign and hdiutil. Windows and Linux builds are pure Go.
#
# Usage: scripts/build-release.sh v0.1.0-beta.1
set -euo pipefail

VERSION="${1:?usage: $0 <version, e.g. v0.1.0-beta.1>}"
SHORT="${VERSION#v}"
SHORT="${SHORT%%-*}" # 0.1.0 — Info.plist and Windows file versions must be numeric

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
WORK="$(mktemp -d)"
SYSO_GLOB="$ROOT/cmd/patchbay/rsrc_windows_*.syso"
cleanup() {
	rm -rf "$WORK"
	rm -f $SYSO_GLOB
}
trap cleanup EXIT

cd "$ROOT"
rm -rf "$DIST"
mkdir -p "$DIST"

LDFLAGS="-s -w -X main.version=$VERSION"
APP_LDFLAGS="$LDFLAGS -X main.appBuild=1"
export GOFLAGS="-trimpath"

build() { # build <goos> <goarch> <cgo> <ldflags> <out>
	CGO_ENABLED="$3" GOOS="$1" GOARCH="$2" go build -tags tray -ldflags "$4" -o "$5" ./cmd/patchbay
}

echo "==> Linux CLI"
for arch in amd64 arm64; do
	d="$WORK/patchbay_${VERSION}_linux_${arch}"
	mkdir -p "$d"
	build linux "$arch" 0 "$LDFLAGS" "$d/patchbay"
	cp LICENSE README.md "$d/"
	tar -C "$WORK" -czf "$DIST/$(basename "$d").tar.gz" "$(basename "$d")"
done

echo "==> Windows app + CLI"
go run github.com/tc-hib/go-winres@v0.3.3 simply \
	--arch amd64,arm64 \
	--out "$ROOT/cmd/patchbay/rsrc" \
	--manifest gui \
	--icon "$ROOT/assets/icon.png" \
	--product-name Patchbay \
	--file-description "Patchbay — one local endpoint for every model" \
	--product-version "$SHORT.0" \
	--file-version "$SHORT.0" \
	--copyright "MIT License. Copyright (c) 2026 Patchbay contributors." \
	--original-filename Patchbay.exe
for arch in amd64 arm64; do
	label="$arch"
	[ "$arch" = amd64 ] && label=x64
	d="$WORK/Patchbay-$VERSION-windows-$label"
	mkdir -p "$d/cli"
	# -H windowsgui: double-clicking starts the tray without a console window.
	build windows "$arch" 0 "$APP_LDFLAGS -H windowsgui" "$d/Patchbay.exe"
	build windows "$arch" 0 "$LDFLAGS" "$d/cli/patchbay.exe"
	cp LICENSE README.md "$d/"
	(cd "$WORK" && zip -qr "$DIST/$(basename "$d").zip" "$(basename "$d")")
done
rm -f $SYSO_GLOB

echo "==> macOS app"
build darwin arm64 1 "$APP_LDFLAGS" "$WORK/patchbay-darwin-arm64"
CC="clang -arch x86_64" build darwin amd64 1 "$APP_LDFLAGS" "$WORK/patchbay-darwin-amd64"
lipo -create -output "$WORK/patchbay-darwin-universal" "$WORK/patchbay-darwin-arm64" "$WORK/patchbay-darwin-amd64"

APP="$WORK/dmg/Patchbay.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$WORK/patchbay-darwin-universal" "$APP/Contents/MacOS/Patchbay"
sed -e "s/__SHORT_VERSION__/$SHORT/" -e "s/__VERSION__/${VERSION#v}/" \
	packaging/macos/Info.plist >"$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist" >/dev/null

ICONSET="$WORK/Patchbay.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
	sips -z "$s" "$s" assets/icon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
	sips -z $((s * 2)) $((s * 2)) assets/icon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/Patchbay.icns"

# Ad-hoc signature: required to run on Apple silicon. Not notarized, so the
# first launch of a downloaded copy needs right-click → Open (see README).
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

ln -s /Applications "$WORK/dmg/Applications"
# hdiutil intermittently fails with "Resource busy" on CI runners; retry.
for attempt in 1 2 3; do
	hdiutil create -quiet -volname "Patchbay" -srcfolder "$WORK/dmg" -fs HFS+ -format UDZO \
		-ov "$DIST/Patchbay-$VERSION-macOS.dmg" && break
	[ "$attempt" = 3 ] && exit 1
	sleep 5
done

echo "==> macOS CLI"
d="$WORK/patchbay_${VERSION}_darwin_universal"
mkdir -p "$d"
cp "$WORK/patchbay-darwin-universal" "$d/patchbay"
codesign --force --sign - "$d/patchbay"
cp LICENSE README.md "$d/"
tar -C "$WORK" -czf "$DIST/$(basename "$d").tar.gz" "$(basename "$d")"

echo "==> Checksums"
(cd "$DIST" && shasum -a 256 -- * >checksums.txt)
ls -lh "$DIST"
