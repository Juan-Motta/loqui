#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
patch_script="${PATCH_SCRIPT:-$repo_root/scripts/patch-plists.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-patch-plists.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fixture="$tmp/repo"
mkdir -p "$fixture/build/darwin" "$fixture/scripts"
cp "$patch_script" "$fixture/scripts/patch-plists.sh"
chmod +x "$fixture/scripts/patch-plists.sh"
cp "$repo_root/build/darwin/Info.plist" "$fixture/build/darwin/Info.plist"
cp "$repo_root/build/darwin/Info.dev.plist" "$fixture/build/darwin/Info.dev.plist"
cp "$repo_root/build/config.yml" "$fixture/build/config.yml"

set_plist_string() {
  /usr/libexec/PlistBuddy -c "Set :$2 $3" "$1" >/dev/null
}

for plist in "$fixture/build/darwin/Info.plist" "$fixture/build/darwin/Info.dev.plist"; do
  set_plist_string "$plist" CFBundleShortVersionString 9.9.9
  set_plist_string "$plist" CFBundleVersion 9.9.9
  set_plist_string "$plist" LSMinimumSystemVersion 11.0.0
done
set_plist_string "$fixture/build/darwin/Info.plist" CFBundleIdentifier wrong.production
set_plist_string "$fixture/build/darwin/Info.dev.plist" CFBundleIdentifier wrong.development

before="$(shasum -a 256 "$fixture/build/darwin/Info.plist" "$fixture/build/darwin/Info.dev.plist")"
if "$fixture/scripts/patch-plists.sh" --check --root "$fixture"; then
  echo "FAIL: --check accepted drifted plists" >&2
  exit 1
fi
after="$(shasum -a 256 "$fixture/build/darwin/Info.plist" "$fixture/build/darwin/Info.dev.plist")"
[ "$after" = "$before" ] || { echo "FAIL: --check modified plist bytes" >&2; exit 1; }

"$fixture/scripts/patch-plists.sh" --root "$fixture"
want_version="$(awk '/^info:/{in_info=1; next} in_info && /^  version:/{gsub(/["'\'' ]/, "", $2); print $2; exit}' "$fixture/build/config.yml")"

assert_plist_string() {
  got="$(plutil -extract "$2" raw "$1")"
  [ "$got" = "$3" ] || { echo "FAIL: $1 $2 = $got, want $3" >&2; exit 1; }
}

assert_plist_string "$fixture/build/darwin/Info.plist" CFBundleIdentifier com.jualopezmo.loquigo
assert_plist_string "$fixture/build/darwin/Info.dev.plist" CFBundleIdentifier com.jualopezmo.loquigo.dev
for plist in "$fixture/build/darwin/Info.plist" "$fixture/build/darwin/Info.dev.plist"; do
  assert_plist_string "$plist" CFBundleShortVersionString "$want_version"
  assert_plist_string "$plist" CFBundleVersion "$want_version"
  assert_plist_string "$plist" LSMinimumSystemVersion 14.0.0
  assert_plist_string "$plist" NSMicrophoneUsageDescription "Loqui usa el micrófono para transcribir tu dictado."
  assert_plist_string "$plist" NSSpeechRecognitionUsageDescription "Loqui usa reconocimiento de voz en el dispositivo para transcribir tu dictado."
  assert_plist_string "$plist" NSAppleEventsUsageDescription "Loqui usa System Events para pegar el texto transcrito en la app que estás usando."
done

"$fixture/scripts/patch-plists.sh" --check --root "$fixture"
echo "patch-plists-test: PASS"
