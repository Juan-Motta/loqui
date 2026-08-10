#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

audit_script="${AUDIT_SCRIPT:-$repo_root/scripts/macos-audit.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-macos-audit.XXXXXX")"
cleanup() {
  chmod -R u+rwX "$tmp" 2>/dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT
app="$tmp/Loqui.app"
fake_bin="$tmp/fake-bin"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" \
  "$app/Contents/Frameworks" "$app/Contents/Resources" "$fake_bin"

cp "$repo_root/build/darwin/Info.plist" "$app/Contents/Info.plist"
cp "$repo_root/build/darwin/icons.icns" "$app/Contents/Resources/icons.icns"
put_file "$app/Contents/MacOS/loqui"
for helper in globe-listener macos-stt whisper-stt; do
  put_file "$app/Contents/Helpers/$helper"
done

framework="$app/Contents/Frameworks/MicrosoftCognitiveServicesSpeech.framework"
put_file "$framework/Versions/A/MicrosoftCognitiveServicesSpeech"
ln -s A "$framework/Versions/Current"
ln -s Versions/Current/MicrosoftCognitiveServicesSpeech "$framework/MicrosoftCognitiveServicesSpeech"

for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do
  version=0.16.0
  [ "$family" = libwhisper ] && version=1.9.1
  put_file "$app/Contents/Frameworks/$family.$version.dylib"
  major="${version%%.*}"
  ln -s "$family.$version.dylib" "$app/Contents/Frameworks/$family.$major.dylib"
  ln -s "$family.$major.dylib" "$app/Contents/Frameworks/$family.dylib"
done
put_file "$app/Contents/Frameworks/libSDL2-2.0.0.dylib"

# The single-quoted lines below are generated fixture tools.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'target="${!#}"; base="$(basename "$target")"' \
  '[ ! -f "$TOOL_STATE/file-fail.$base" ] || { printf "fixture file failure for %s\n" "$target" >&2; exit 73; }' \
  'case "$target" in' \
  '  */Contents/MacOS/*|*/Contents/Helpers/*|*/Contents/Frameworks/*.dylib|*/Versions/A/MicrosoftCognitiveServicesSpeech|*/hidden-mach-o)' \
  '    printf "%s: Mach-O 64-bit executable arm64\n" "$target" ;;' \
  '  *) printf "%s: data\n" "$target" ;;' \
  'esac' >"$fake_bin/file"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'target="${!#}"; base="$(basename "$target")"' \
  'case "$base" in' \
  '  bad-x86.dylib) printf "x86_64\n" ;;' \
  '  MicrosoftCognitiveServicesSpeech) printf "arm64 x86_64\n" ;;' \
  '  *) printf "arm64\n" ;;' \
  'esac' >"$fake_bin/lipo"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'mode="$1"; target="$2"; base="$(basename "$target")"' \
  'case "$mode" in' \
  '  -L)' \
  '    [ ! -f "$TOOL_STATE/otool-L-fail.$base" ] || { printf "fixture otool -L failure for %s\n" "$target" >&2; exit 74; }' \
  '    printf "%s:\n" "$target"' \
  '    case "$base" in' \
  '      bad-homebrew) printf "\t/opt/homebrew/lib/libbad.dylib (compatibility version 1.0.0)\n" ;;' \
  '      *) printf "\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0)\n" ;;' \
  '    esac' \
  '    [ "$base" != MicrosoftCognitiveServicesSpeech ] || printf "%s (architecture arm64):\n" "$target"' \
  '    ;;' \
  '  -D)' \
  '    printf "%s:\n" "$target"' \
  '    case "$base" in bad-id.dylib) printf "/private/tmp/libbad.dylib\n" ;; *) printf "@rpath/%s\n" "$base" ;; esac' \
  '    ;;' \
  '  -l)' \
  '    case "$base" in' \
  '      loqui) rpath="@executable_path/../Frameworks" ;;' \
  '      whisper-stt) rpath="@loader_path/../Frameworks" ;;' \
  '      *.dylib) rpath="@loader_path" ;;' \
  '      *) rpath="" ;;' \
  '    esac' \
  '    case "$base" in bad-rpath.dylib|bad-checkout) rpath="/private/tmp/checkout/build" ;; esac' \
  '    [ -z "$rpath" ] || printf "cmd LC_RPATH\npath %s (offset 12)\n" "$rpath"' \
  '    case "$base" in libggml-metal.*.dylib) printf "segname __DATA\nsectname __ggml_metallib\n" ;; esac' \
  '    ;;' \
  'esac' >"$fake_bin/otool"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'target="$2"; base="$(basename "$target")"' \
  '[ ! -f "$TOOL_STATE/vtool-fail.$base" ] || { printf "fixture vtool failure for %s\n" "$target" >&2; exit 75; }' \
  'minos=14.0' \
  '[ "$base" != macos-stt ] || minos=26.0' \
  '[ ! -f "$TOOL_STATE/minos.$base" ] || minos="$(cat "$TOOL_STATE/minos.$base")"' \
  'printf "%s:\nLoad command 11\n      cmd LC_BUILD_VERSION\n platform MACOS\n" "$target"' \
  '[ "$minos" = none ] || printf "    minos %s\n" "$minos"' \
  >"$fake_bin/vtool"

# Model fixtures are deliberately tiny; production values remain fixed in the audited script.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'target="${!#}"' \
  'if [ "$(basename "$target")" != ggml-small.bin ]; then exec /usr/bin/stat "$@"; fi' \
  'case "$(/bin/cat "$target")" in wrong-size) printf "1\n" ;; *) printf "487601967\n" ;; esac' \
  >"$fake_bin/stat"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'target="${!#}"' \
  'if [ "$(basename "$target")" != ggml-small.bin ]; then exec /usr/bin/shasum "$@"; fi' \
  'good=1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b' \
  'bad=0000000000000000000000000000000000000000000000000000000000000000' \
  'case "$(/bin/cat "$target")" in wrong-digest) digest="$bad" ;; *) digest="$good" ;; esac' \
  'printf "%s  %s\n" "$digest" "$target"' >"$fake_bin/shasum"
chmod +x "$fake_bin/file" "$fake_bin/lipo" "$fake_bin/otool" "$fake_bin/vtool" \
  "$fake_bin/stat" "$fake_bin/shasum"

expect_failure() {
  expected="$1"
  shift
  output="$tmp/failure.log"
  if "$@" >"$output" 2>&1; then fail "command unexpectedly passed: $*"; fi
  assert_contains "$output" "$expected"
}

audit() { PATH="$fake_bin:$PATH" TOOL_STATE="$tmp" "$audit_script" "$@"; }
printf '%s\n' 10.15 >"$tmp/minos.MicrosoftCognitiveServicesSpeech"
audit --channel production --version 0.1.0 "$app"

model="$app/Contents/Resources/models/ggml-small.bin"
mkdir -p "$(dirname "$model")"
put_file "$model" valid
audit --channel production --version 0.1.0 "$app"
put_file "$model" wrong-size
expect_failure "bundled model size is" audit --channel production --version 0.1.0 "$app"
put_file "$model" wrong-digest
expect_failure "bundled model SHA-256 is" audit --channel production --version 0.1.0 "$app"
rm "$model"
outside_model="$tmp/outside-model.bin"
put_file "$outside_model" valid
ln -s "$outside_model" "$model"
expect_failure "bundled model must be a real file" audit --channel production --version 0.1.0 "$app"
rm "$model"

printf '%s\n' 26.0 >"$tmp/minos.globe-listener"
expect_failure "minimum macOS" audit --channel production --version 0.1.0 "$app"
rm "$tmp/minos.globe-listener"

printf '%s\n' 27.0 >"$tmp/minos.macos-stt"
expect_failure "exactly 26.0" audit --channel production --version 0.1.0 "$app"
printf '%s\n' none >"$tmp/minos.macos-stt"
expect_failure "no macOS minimum" audit --channel production --version 0.1.0 "$app"
rm "$tmp/minos.macos-stt"

put_file "$app/Contents/Resources/hidden-mach-o"
expect_failure hidden-mach-o audit --channel production --version 0.1.0 "$app"
rm "$app/Contents/Resources/hidden-mach-o"

permission_denied_resource="$app/Contents/Resources/permission-denied.bin"
put_file "$permission_denied_resource"
chmod 000 "$permission_denied_resource"
expect_failure "regular file is not readable: Contents/Resources/permission-denied.bin" \
  audit --channel production --version 0.1.0 "$app"
chmod 600 "$permission_denied_resource"
rm "$permission_denied_resource"

unreadable_resource="$app/Contents/Resources/unreadable-resource.bin"
put_file "$unreadable_resource"
put_file "$tmp/file-fail.unreadable-resource.bin"
expect_failure "file -b failed for Contents/Resources/unreadable-resource.bin" \
  audit --channel production --version 0.1.0 "$app"
assert_contains "$tmp/failure.log" "(exit 73)"
assert_contains "$tmp/failure.log" "fixture file failure"
rm "$tmp/file-fail.unreadable-resource.bin" "$unreadable_resource"

put_file "$tmp/otool-L-fail.loqui"
expect_failure "otool -L failed for Contents/MacOS/loqui" \
  audit --channel production --version 0.1.0 "$app"
assert_contains "$tmp/failure.log" "(exit 74)"
assert_contains "$tmp/failure.log" "fixture otool -L failure"
rm "$tmp/otool-L-fail.loqui"

put_file "$tmp/vtool-fail.loqui"
expect_failure "vtool -show-build failed for Contents/MacOS/loqui" \
  audit --channel production --version 0.1.0 "$app"
assert_contains "$tmp/failure.log" "(exit 75)"
assert_contains "$tmp/failure.log" "fixture vtool failure"
rm "$tmp/vtool-fail.loqui"

put_file "$app/Contents/Frameworks/bad-x86.dylib"
expect_failure bad-x86.dylib audit --channel production --version 0.1.0 "$app"
rm "$app/Contents/Frameworks/bad-x86.dylib"

ln -s missing.dylib "$app/Contents/Frameworks/broken.dylib"
expect_failure broken.dylib audit --channel production --version 0.1.0 "$app"
rm "$app/Contents/Frameworks/broken.dylib"

for bad_file in bad-homebrew bad-checkout; do
  put_file "$app/Contents/Helpers/$bad_file"
  expect_failure "$bad_file" audit --channel production --version 0.1.0 "$app"
  rm "$app/Contents/Helpers/$bad_file"
done
for bad_file in bad-id.dylib bad-rpath.dylib; do
  put_file "$app/Contents/Frameworks/$bad_file"
  expect_failure "$bad_file" audit --channel production --version 0.1.0 "$app"
  rm "$app/Contents/Frameworks/$bad_file"
done

metal="$app/Contents/Frameworks/libggml-metal.0.16.0.dylib"
mv "$metal" "$app/Contents/Frameworks/missing-metal.dylib"
expect_failure libggml-metal audit --channel production --version 0.1.0 "$app"
mv "$app/Contents/Frameworks/missing-metal.dylib" "$metal"

mv "$app/Contents/Helpers/macos-stt" "$app/Contents/Helpers/macos-stt.absent"
expect_failure macos-stt audit --channel production --version 0.1.0 "$app"
mv "$app/Contents/Helpers/macos-stt.absent" "$app/Contents/Helpers/macos-stt"

plist="$app/Contents/Info.plist"
plist_backup="$tmp/Info.plist"
cp "$plist" "$plist_backup"
for key in NSMicrophoneUsageDescription NSSpeechRecognitionUsageDescription NSAppleEventsUsageDescription; do
  plutil -replace "$key" -string '' "$plist"
  expect_failure "$key" audit --channel production --version 0.1.0 "$app"
  cp "$plist_backup" "$plist"
done
for key in CFBundleShortVersionString CFBundleVersion; do
  plutil -replace "$key" -string 9.9.9 "$plist"
  expect_failure "$key" audit --channel production --version 0.1.0 "$app"
  cp "$plist_backup" "$plist"
done
plutil -replace LSMinimumSystemVersion -string 11.0.0 "$plist"
expect_failure LSMinimumSystemVersion audit --channel production --version 0.1.0 "$app"
cp "$plist_backup" "$plist"
plutil -replace CFBundleIdentifier -string com.jualopezmo.loquigo.dev "$plist"
expect_failure CFBundleIdentifier audit --channel production --version 0.1.0 "$app"
audit --channel development --version 0.1.0 "$app"
cp "$plist_backup" "$plist"

plutil -replace CFBundleExecutable -string wrong "$plist"
expect_failure CFBundleExecutable audit --channel production --version 0.1.0 "$app"
cp "$plist_backup" "$plist"
put_file "$app/Contents/MacOS/extra"
expect_failure Contents/MacOS audit --channel production --version 0.1.0 "$app"
rm "$app/Contents/MacOS/extra"

expect_failure --version audit --channel production "$app"
expect_failure malformed audit --channel production --version invalid "$app"

for attribute in com.apple.ResourceFork com.apple.FinderInfo com.apple.quarantine; do
  if [ "$attribute" = com.apple.FinderInfo ]; then
    xattr -wx "$attribute" 0100000000000000000000000000000000000000000000000000000000000000 \
      "$app/Contents/Helpers/globe-listener"
  else
    xattr -w "$attribute" fixture "$app/Contents/Helpers/globe-listener"
  fi
  expect_failure "$attribute" audit --channel production --version 0.1.0 "$app"
  xattr -d "$attribute" "$app/Contents/Helpers/globe-listener"
done
xattr -w com.apple.provenance fixture "$app/Contents/Helpers/globe-listener"
audit --channel production --version 0.1.0 "$app"

echo "macos-audit-test: PASS"
