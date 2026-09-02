#!/bin/sh
# Builds the IPA.
#
# There is no Xcode project here, for the reason there is no Gradle on the
# Android side: the app is one Swift file and one linked archive, and an
# .xcodeproj is a generated thing nobody would read. `swiftc` and `xcrun` do
# the same work in four commands.
#
# **The result is unsigned.** That is deliberate and it is the whole
# distribution story: a person who wants this puts it on their own phone with
# their own Apple ID — AltStore, Sideloadly, whatever they already use — and
# those tools re-sign it on the way in. Signing it here with a certificate this
# machine does not have would only be a signature nobody's phone accepts.
#
# Needs Xcode (not just the command line tools) for the iPhoneOS SDK.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=$here/build
app=$out/Payload/wfeature.app

sdk=$(xcrun --sdk iphoneos --show-sdk-path)
[ -d "$sdk" ] || { echo "no iPhoneOS SDK; Xcode is needed, not just the command line tools" >&2; exit 1; }
minimum=15.0

rm -rf "$out"
mkdir -p "$app"

# 1. The emulator, as an archive to link rather than a binary to run: iOS does
#    not let an app start a process, so the server has to live inside it.
echo "==> building the library for ios/arm64"
CGO_ENABLED=1 GOOS=ios GOARCH=arm64 \
    CC="$(xcrun --sdk iphoneos -f clang) -arch arm64 -isysroot $sdk -miphoneos-version-min=$minimum" \
    go build -trimpath -buildmode=c-archive \
    -ldflags="-s -w -X main.version=${VERSION:-dev}" \
    -o "$out/libwfeature.a" "$here/lib"

# 2. The app. The generated header is what Swift reads the three entry points
#    out of, and -parse-as-library is what stops swiftc from treating a single
#    file as a script — @main and top-level code cannot share a module.
echo "==> compiling the app"
cat > "$out/bridge.h" <<EOF
#include "libwfeature.h"
EOF
xcrun --sdk iphoneos swiftc \
    -target "arm64-apple-ios$minimum" \
    -sdk "$sdk" \
    -import-objc-header "$out/bridge.h" \
    -I "$out" \
    -O -whole-module-optimization \
    -parse-as-library \
    -framework UIKit -framework WebKit -framework Foundation \
    "$here/src/App.swift" "$out/libwfeature.a" \
    -o "$app/wfeature"

# 3. The bundle.
echo "==> assembling"
sdk_version=$(xcrun --sdk iphoneos --show-sdk-version)
sed -e "s/__VERSION__/${VERSION:-dev}/" -e "s/__SDK__/$sdk_version/" \
    "$here/Info.plist" > "$app/Info.plist"

# The icons, at the sizes the plist names them by. A loose PNG with nothing
# pointing at it is what a blank home-screen square looks like.
sips -z 120 120 "$root/web/icon-512.png" --out "$app/AppIcon60x60@2x.png" >/dev/null
sips -z 180 180 "$root/web/icon-512.png" --out "$app/AppIcon60x60@3x.png" >/dev/null
sips -z 152 152 "$root/web/icon-512.png" --out "$app/AppIcon76x76@2x~ipad.png" >/dev/null
xcrun plutil -convert binary1 "$app/Info.plist"

# 4. An ad-hoc signature, so that a phone has something to replace rather than
#    nothing to read. Sideloading tools re-sign anyway; this only keeps the
#    bundle from being rejected before they get to it.
codesign --force --sign - --timestamp=none "$app" 2>/dev/null || \
    echo "    (not ad-hoc signed; the sideloader will sign it)"

( cd "$out" && zip -qry "$here/build/wfeature.ipa" Payload )

echo
echo "IPA: $out/wfeature.ipa"
ls -lh "$out/wfeature.ipa" | awk '{print "     " $5}'
echo
echo "Unsigned. Install it with AltStore or Sideloadly, which sign it with"
echo "your own Apple ID on the way onto the phone."
