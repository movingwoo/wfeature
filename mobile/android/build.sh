#!/bin/sh
# Builds the APK.
#
# There is no Gradle here, and that is deliberate: the app is one Activity and
# one bundled binary, and the Android build tools do that in six commands.
# Gradle would add a wrapper, a daemon, a plugin resolution step and several
# hundred megabytes of downloads to a project whose entire source is one Java
# file. What it would buy — incremental builds, variants, a dependency graph —
# this has no use for.
#
# Everything the build needs is named by an environment variable so that a
# machine with the tools somewhere else can still run it:
#
#   ANDROID_HOME   the SDK root (build-tools/ and platforms/ live under it)
#   JAVA_HOME      a JDK 17 or newer
#
# The one input that is not in this directory is the server: it is built from
# the Go source above and bundled as a native library, because the extracted
# library directory is the only place an Android app may execute a file from.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=$here/build

: "${ANDROID_HOME:=/opt/homebrew/share/android-commandlinetools}"
: "${JAVA_HOME:=/opt/homebrew/opt/openjdk@17}"
build_tools=${BUILD_TOOLS:-$(ls -1d "$ANDROID_HOME"/build-tools/* 2>/dev/null | sort -V | tail -1)}
platform=${PLATFORM:-$(ls -1d "$ANDROID_HOME"/platforms/* 2>/dev/null | sort -V | tail -1)}

[ -d "$build_tools" ] || { echo "no build-tools under $ANDROID_HOME" >&2; exit 1; }
[ -d "$platform" ] || { echo "no platform under $ANDROID_HOME" >&2; exit 1; }
[ -x "$JAVA_HOME/bin/javac" ] || { echo "no javac under $JAVA_HOME" >&2; exit 1; }

# d8, apksigner and aapt2 are shell wrappers that run whichever `java` is on
# the PATH, and a machine whose default is an older JDK fails inside them with
# a class-version error rather than at the check above.
export JAVA_HOME
PATH=$JAVA_HOME/bin:$PATH
export PATH

android_jar=$platform/android.jar
aapt2=$build_tools/aapt2
d8=$build_tools/d8
zipalign=$build_tools/zipalign
apksigner=$build_tools/apksigner

echo "build-tools $build_tools"
echo "platform    $platform"

rm -rf "$out"
mkdir -p "$out/compiled" "$out/gen" "$out/classes" "$out/dex" "$out/apk/lib/arm64-v8a"

# 1. The server. The same source a desktop release is built from, for the
#    phone's own architecture, named as a library so that the installer
#    extracts it into the one directory an app may execute from.
echo "==> building the server for android/arm64"
( cd "$root" && CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION:-dev}" \
    -o "$here/jniLibs/arm64-v8a/libwfeature.so" ./cmd/server )
cp "$here/jniLibs/arm64-v8a/libwfeature.so" "$out/apk/lib/arm64-v8a/libwfeature.so"

# 2. Resources.
echo "==> resources"
"$aapt2" compile --dir "$here/res" -o "$out/compiled/res.zip"

# 3. Link, which also writes the R class the source refers to.
echo "==> linking"
"$aapt2" link \
    -o "$out/base.apk" \
    -I "$android_jar" \
    --manifest "$here/AndroidManifest.xml" \
    --java "$out/gen" \
    --min-sdk-version 26 \
    --target-sdk-version 35 \
    --version-code "${VERSION_CODE:-1}" \
    --version-name "${VERSION:-dev}" \
    "$out/compiled/res.zip"

# 4. Compile. Java 11 bytecode: d8 reads it, and nothing here needs newer.
echo "==> compiling"
find "$here/src" "$out/gen" -name '*.java' > "$out/sources.txt"
"$JAVA_HOME/bin/javac" \
    --release 11 \
    -classpath "$android_jar" \
    -d "$out/classes" \
    -nowarn \
    @"$out/sources.txt"

# 5. Dex.
echo "==> dexing"
find "$out/classes" -name '*.class' > "$out/classes.txt"
"$d8" --lib "$android_jar" --output "$out/dex" --min-api 26 @"$out/classes.txt"

# 6. Assemble: the linked resources, the dex, and the server binary.
echo "==> assembling"
cp "$out/base.apk" "$out/unsigned.apk"
( cd "$out/dex" && zip -q "$out/unsigned.apk" classes.dex )
# Stored rather than deflated: Android maps an uncompressed library straight
# out of the APK, and a 20MB binary is not worth compressing twice.
( cd "$out/apk" && zip -q -0 -r "$out/unsigned.apk" lib )

"$zipalign" -f -p 4 "$out/unsigned.apk" "$out/aligned.apk"

# 7. Sign. A debug key, made once and kept, because a tester's phone refuses
#    an unsigned APK and an upgrade refuses a different key than the one
#    already installed.
keystore=${KEYSTORE:-$here/debug.keystore}
if [ ! -f "$keystore" ]; then
    echo "==> making a debug key"
    "$JAVA_HOME/bin/keytool" -genkeypair -v \
        -keystore "$keystore" -storepass android -keypass android \
        -alias wfeature -keyalg RSA -keysize 2048 -validity 10000 \
        -dname "CN=wfeature debug, OU=, O=, L=, S=, C=" > /dev/null
fi
"$apksigner" sign \
    --ks "$keystore" --ks-pass pass:android --key-pass pass:android \
    --out "$out/wfeature.apk" "$out/aligned.apk"

echo
echo "APK: $out/wfeature.apk"
ls -lh "$out/wfeature.apk" | awk '{print "     " $5}'
