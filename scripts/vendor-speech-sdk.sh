#!/usr/bin/env bash
# Fetch the macOS Azure Speech SDK framework that the cgo binding links against.
#
# The Go binding (github.com/Microsoft/cognitive-services-speech-sdk-go) is cgo over
# the native SDK and ships NO `#cgo` directives of its own, so both the header include
# path and the link flags come from the environment. build/darwin/Taskfile.yml supplies
# them and points here; this script only has to put the framework where those flags
# expect it: third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework
#
# WHY A PINNED VERSION AND NOT aka.ms/csspeech/macosbinary: that short link always
# redirects to whatever is newest, so it would silently change the linked SDK under a
# build that already works. The version here is pinned to match the Go module exactly
# (both 1.51.1) — a mismatch between the binding and the native library is the kind of
# failure that shows up as a missing symbol at link time, or worse, at runtime.
#
# NOT COMMITTED: 10 MB of universal Mach-O. Run this after cloning. Idempotent.
set -euo pipefail

# Keep in lockstep with the version in go.mod.
VERSION="1.51.1"
SHA256="067bacff6e2ad4c08dc3a37f367b7e6f4fe66beb8fd626810bc5e352981a99f5"
URL="https://csspeechstorage.blob.core.windows.net/drop/${VERSION}/MicrosoftCognitiveServicesSpeech-MacOSXCFramework-${VERSION}.zip"

cd "$(dirname "$0")/.."
DEST="third_party/speech-sdk"
FRAMEWORK="$DEST/MicrosoftCognitiveServicesSpeech.framework"
STAMP="$DEST/.version"

if [ -d "$FRAMEWORK" ] && [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$VERSION" ]; then
  echo "vendor-speech-sdk: $VERSION already in place"
  exit 0
fi

# Verify the Go binding really is the version this framework matches, so the two can't
# drift apart unnoticed.
if [ -f go.mod ]; then
  GO_SDK="$(awk '/cognitive-services-speech-sdk-go/ {print $2}' go.mod | head -1 || true)"
  if [ -n "$GO_SDK" ] && [ "$GO_SDK" != "v${VERSION}" ]; then
    echo "vendor-speech-sdk: WARNING — go.mod has the binding at $GO_SDK but this script" >&2
    echo "  pins the native framework at v${VERSION}. Update both together." >&2
  fi
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "vendor-speech-sdk: downloading $VERSION"
curl -fsSL "$URL" -o "$TMP/sdk.zip"

echo "vendor-speech-sdk: verifying sha256"
ACTUAL="$(shasum -a 256 "$TMP/sdk.zip" | awk '{print $1}')"
if [ "$ACTUAL" != "$SHA256" ]; then
  echo "vendor-speech-sdk: digest mismatch" >&2
  echo "  expected $SHA256" >&2
  echo "  actual   $ACTUAL" >&2
  exit 1
fi

unzip -q "$TMP/sdk.zip" -d "$TMP/unpacked"
SRC="$TMP/unpacked/MicrosoftCognitiveServicesSpeech.xcframework/macos-arm64_x86_64/MicrosoftCognitiveServicesSpeech.framework"
[ -d "$SRC" ] || { echo "vendor-speech-sdk: unexpected archive layout — no macos framework at $SRC" >&2; exit 1; }
# The C headers are what the whole approach depends on; fail loudly rather than let a
# repackaged archive produce a confusing "speechapi_c_error.h not found" later.
[ -f "$SRC/Headers/speechapi_c_recognizer.h" ] || { echo "vendor-speech-sdk: framework has no C headers" >&2; exit 1; }

rm -rf "$DEST"
mkdir -p "$DEST"
# -R follows the framework's internal symlinks structure correctly for a copy.
cp -R "$SRC" "$DEST/"
cp "$TMP/unpacked/LICENSE.md" "$DEST/LICENSE.md" 2>/dev/null || true
cp "$TMP/unpacked/ThirdPartyNotices.md" "$DEST/ThirdPartyNotices.md" 2>/dev/null || true
echo "$VERSION" > "$STAMP"

echo "vendor-speech-sdk: installed $VERSION -> $FRAMEWORK"
