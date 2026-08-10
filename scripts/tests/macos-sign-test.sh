#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

sign_script="${SIGN_SCRIPT:-$repo_root/scripts/macos-sign.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-macos-sign.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
fake_bin="$tmp/fake-bin"
identity_file="$tmp/identities"
tool_log="$tmp/tool.log"
mkdir -p "$fake_bin"

# The single-quoted lines below are generated fixture tools.
# shellcheck disable=SC2016
printf '%s\n' '#!/usr/bin/env bash' 'cat "$IDENTITY_FILE"' >"$fake_bin/security"
# shellcheck disable=SC2016
printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "$*" >>"$TOOL_LOG"' >"$fake_bin/codesign"
chmod +x "$fake_bin/security" "$fake_bin/codesign"
export PATH="$fake_bin:$PATH" IDENTITY_FILE="$identity_file" TOOL_LOG="$tool_log"

sha_dev_a=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
sha_dev_c=CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC
sha_release_b=BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB
sha_release_d=DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD
dev_name='Apple Development: Juan Motta (TEAM123456)'
release_name='Developer ID Application: Juan Motta (TEAM123456)'

identity_row() { printf '  1) %s "%s"\n' "$1" "$2"; }
expect_failure() {
  expected="$1"
  shift
  output="$tmp/failure.log"
  if "$@" >"$output" 2>&1; then fail "command unexpectedly passed: $*"; fi
  assert_contains "$output" "$expected"
}

: >"$identity_file"
development_stderr="$tmp/development.stderr"
resolved="$($sign_script resolve --channel development 2>"$development_stderr")"
assert_eq "$resolved" -
assert_contains "$development_stderr" 'TCC continuity is unavailable'
expect_failure 'Developer ID Application' "$sign_script" resolve --channel release

identity_row "$sha_dev_a" "$dev_name" >"$identity_file"
assert_eq "$($sign_script resolve --channel development)" "$sha_dev_a"
identity_row "$sha_dev_a" "$dev_name" >>"$identity_file"
assert_eq "$($sign_script resolve --channel development)" "$sha_dev_a"

identity_row "$sha_dev_c" 'Apple Development: Second Person (TEAM654321)' >>"$identity_file"
expect_failure ambiguous "$sign_script" resolve --channel development
assert_eq "$(LOQUI_DEV_SIGN_IDENTITY="$dev_name" "$sign_script" resolve --channel development)" "$sha_dev_a"
expect_failure 'does not match' env LOQUI_DEV_SIGN_IDENTITY=unknown "$sign_script" resolve --channel development

identity_row "$sha_release_b" "$release_name" >"$identity_file"
assert_eq "$($sign_script resolve --channel release)" "$sha_release_b"
identity_row "$sha_release_d" 'Developer ID Application: Second Person (TEAM654321)' >>"$identity_file"
expect_failure ambiguous "$sign_script" resolve --channel release
assert_eq "$(LOQUI_SIGN_IDENTITY="$sha_release_b" "$sign_script" resolve --channel release)" "$sha_release_b"

for entitlements in Loqui.entitlements LoquiAudioHelper.entitlements; do
  plutil -lint "$repo_root/build/darwin/$entitlements" >/dev/null
done
assert_eq "$(/usr/libexec/PlistBuddy -c 'Print :com.apple.security.device.audio-input' "$repo_root/build/darwin/Loqui.entitlements")" true
assert_eq "$(/usr/libexec/PlistBuddy -c 'Print :com.apple.security.automation.apple-events' "$repo_root/build/darwin/Loqui.entitlements")" true
assert_eq "$(plutil -convert xml1 -o - "$repo_root/build/darwin/Loqui.entitlements" | grep -c '<key>')" 2
assert_eq "$(/usr/libexec/PlistBuddy -c 'Print :com.apple.security.device.audio-input' "$repo_root/build/darwin/LoquiAudioHelper.entitlements")" true
assert_eq "$(plutil -convert xml1 -o - "$repo_root/build/darwin/LoquiAudioHelper.entitlements" | grep -c '<key>')" 1

app="$tmp/Loqui.app"
mkdir -p "$app/Contents/Frameworks/MicrosoftCognitiveServicesSpeech.framework" "$app/Contents/Helpers"
cp "$repo_root/build/darwin/Info.plist" "$app/Contents/Info.plist"
for dylib in libSDL2-2.0.0.dylib libggml.0.16.0.dylib libwhisper.1.9.1.dylib; do
  put_file "$app/Contents/Frameworks/$dylib"
done
for helper in globe-listener macos-stt whisper-stt; do put_file "$app/Contents/Helpers/$helper"; done

: >"$tool_log"
"$sign_script" app --channel release --app "$app" --identity "$sha_release_b"
signed_order="$tmp/signed-order"
awk '!/--verify/ {print $NF}' "$tool_log" | while read -r path; do basename "$path"; done >"$signed_order"
expected_order="$tmp/expected-order"
printf '%s\n' \
  libSDL2-2.0.0.dylib \
  libggml.0.16.0.dylib \
  libwhisper.1.9.1.dylib \
  MicrosoftCognitiveServicesSpeech.framework \
  globe-listener macos-stt whisper-stt Loqui.app >"$expected_order"
diff -u "$expected_order" "$signed_order"

assert_contains "$tool_log" "--identifier com.jualopezmo.loquigo.globe-listener"
assert_contains "$tool_log" "--identifier com.jualopezmo.loquigo.macos-stt"
assert_contains "$tool_log" "--identifier com.jualopezmo.loquigo.whisper-stt"
assert_contains "$tool_log" "--options runtime --timestamp --entitlements $repo_root/build/darwin/Loqui.entitlements"
assert_contains "$tool_log" "--options runtime --timestamp --entitlements $repo_root/build/darwin/LoquiAudioHelper.entitlements"
app_sign_line="$(grep -F -- "--sign $sha_release_b $app" "$tool_log")"
case "$app_sign_line" in *--identifier*) fail "top-level app received an explicit identifier" ;; esac
assert_eq "$(grep -o -- '--deep' "$tool_log" | wc -l | tr -d ' ')" 1

cp "$repo_root/build/darwin/Info.dev.plist" "$app/Contents/Info.plist"
: >"$tool_log"
"$sign_script" app --channel development --app "$app" --identity "$sha_dev_a"
assert_contains "$tool_log" "--timestamp=none"
assert_contains "$tool_log" "--identifier com.jualopezmo.loquigo.dev.globe-listener"
assert_contains "$tool_log" "--options runtime"

: >"$identity_file"
: >"$tool_log"
fallback_stderr="$tmp/fallback.stderr"
"$sign_script" app --channel development --app "$app" 2>"$fallback_stderr"
assert_contains "$fallback_stderr" 'TCC continuity is unavailable'
assert_contains "$tool_log" "--identifier com.jualopezmo.loquigo.dev.whisper-stt"
assert_not_contains "$tool_log" "--timestamp"
assert_not_contains "$tool_log" "--options"
assert_not_contains "$tool_log" "--entitlements"

cp "$repo_root/build/darwin/Info.plist" "$app/Contents/Info.plist"
: >"$tool_log"
"$sign_script" app --channel adhoc --app "$app"
assert_contains "$tool_log" "--identifier com.jualopezmo.loquigo.whisper-stt"
assert_not_contains "$tool_log" "com.jualopezmo.loquigo.dev"

dmg="$tmp/Loqui.dmg"
put_file "$dmg"
: >"$tool_log"
"$sign_script" dmg --dmg "$dmg" --identity "$sha_release_b"
assert_eq "$(cat "$tool_log")" "--force --timestamp --sign $sha_release_b $dmg"

echo "macos-sign-test: PASS"
