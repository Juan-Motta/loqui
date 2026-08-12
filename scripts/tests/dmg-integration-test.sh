#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dmg-integration.XXXXXX")"
tmp="$(cd "$tmp" && pwd -P)"
dmg="$tmp/Loqui.dmg"
mountpoint="$tmp/mount"
mounted=0

detach_integration_mount() {
  [ "$mounted" -eq 1 ] || return 0
  for detach_attempt in 1 2 3; do
    if hdiutil detach "$mountpoint" >/dev/null; then
      mounted=0
      return 0
    fi
    [ "$detach_attempt" -eq 3 ] || sleep 1
  done
  if hdiutil detach -force "$mountpoint" >/dev/null 2>&1; then
    mounted=0
  fi
  echo 'dmg-integration-test: could not cleanly detach throwaway image' >&2
  return 1
}

cleanup_integration() {
  cleanup_rc=$?
  trap - EXIT
  set +e
  detach_integration_mount
  detach_rc=$?
  rm -f "$dmg"
  remove_image_rc=$?
  rm -rf "$tmp"
  remove_tmp_rc=$?
  set -e
  if [ "$detach_rc" -ne 0 ] || [ "$remove_image_rc" -ne 0 ] || [ "$remove_tmp_rc" -ne 0 ]; then
    cleanup_rc=1
  fi
  exit "$cleanup_rc"
}
trap cleanup_integration EXIT

if [ -n "${LOQUI_DMGBUILD_PYTHON:-}" ]; then
  dmgbuild_python="$LOQUI_DMGBUILD_PYTHON"
else
  dmgbuild_python="$("$repo_root/scripts/setup-dmgbuild.sh")"
fi
case "$dmgbuild_python" in
  /*) ;;
  *) fail "dmgbuild Python must be absolute: $dmgbuild_python" ;;
esac
[ -x "$dmgbuild_python" ] || fail "dmgbuild Python is not executable: $dmgbuild_python"
dmgbuild_version="$("$dmgbuild_python" -c \
  'import importlib.metadata; print(importlib.metadata.version("dmgbuild"))')"
assert_eq "$dmgbuild_version" 1.6.7

stub_app="$tmp/Loqui.app"
mkdir -p "$stub_app/Contents/MacOS"
printf '%s\n' \
  '<?xml version="1.0" encoding="UTF-8"?>' \
  '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
  '<plist version="1.0"><dict>' \
  '<key>CFBundleExecutable</key><string>loqui</string>' \
  '<key>CFBundleIdentifier</key><string>com.jualopezmo.loquigo.integration-fixture</string>' \
  '<key>CFBundleName</key><string>Loqui</string>' \
  '<key>CFBundlePackageType</key><string>APPL</string>' \
  '<key>CFBundleVersion</key><string>0</string>' \
  '</dict></plist>' \
  >"$stub_app/Contents/Info.plist"
printf '%s\n' '#!/bin/bash' 'exit 0' >"$stub_app/Contents/MacOS/loqui"
chmod +x "$stub_app/Contents/MacOS/loqui"
codesign --force --sign - "$stub_app"
codesign --verify --deep --strict "$stub_app"

settings="$repo_root/build/darwin/dmg/settings.py"
assets="$repo_root/build/darwin/dmg"
"$dmgbuild_python" -m dmgbuild \
  -s "$settings" \
  -D "app=$stub_app" \
  -D "assets=$assets" \
  Loqui "$dmg"
[ -f "$dmg" ] && [ ! -L "$dmg" ] || fail 'real dmgbuild did not create a regular image'
hdiutil verify "$dmg" >/dev/null

mkdir "$mountpoint"
hdiutil attach -readonly -nobrowse -mountpoint "$mountpoint" "$dmg" >/dev/null
mounted=1

visible_raw="$tmp/dmg-visible-root.raw"
visible_manifest="$tmp/dmg-visible-root.txt"
visible_expected="$tmp/dmg-visible-root.expected"
find "$mountpoint" -mindepth 1 -maxdepth 1 ! -name '.*' -print >"$visible_raw"
sed "s#^$mountpoint/##" "$visible_raw" | LC_ALL=C sort >"$visible_manifest"
printf '%s\n' Applications Loqui.app >"$visible_expected"
diff -u "$visible_expected" "$visible_manifest"

[ -L "$mountpoint/Applications" ] || fail 'mounted image Applications item is not a symlink'
assert_eq "$(readlink "$mountpoint/Applications")" /Applications
assert_dir "$mountpoint/Loqui.app"
if xattr -p com.apple.FinderInfo "$mountpoint/Loqui.app" >/dev/null 2>&1; then
  fail 'mounted Loqui.app has signing-blocking xattr com.apple.FinderInfo'
fi
codesign --verify --deep --strict "$mountpoint/Loqui.app"
[ -f "$mountpoint/.DS_Store" ] || fail 'mounted image is missing .DS_Store'
"$dmgbuild_python" "$repo_root/build/darwin/dmg/verify-ds-store.py" \
  "$mountpoint/.DS_Store" >"$tmp/dmg-ds-store.txt"
assert_eq "$(cat "$tmp/dmg-ds-store.txt")" 'verify-ds-store: PASS'

[ -f "$mountpoint/.background.tiff" ] || fail 'mounted image is missing .background.tiff'
background_finder_info="$tmp/dmg-background-finder-info.txt"
if ! xattr -px com.apple.FinderInfo "$mountpoint/.background.tiff" \
  >"$background_finder_info" 2>/dev/null; then
  fail 'mounted background is missing com.apple.FinderInfo'
fi
finder_info_hex="$(tr -d '[:space:]' <"$background_finder_info")"
case "$finder_info_hex" in
  *[!0-9A-Fa-f]*|'') fail 'mounted background has malformed com.apple.FinderInfo' ;;
esac
[ "${#finder_info_hex}" -eq 64 ] \
  || fail 'mounted background com.apple.FinderInfo is not 32 bytes'
finder_flags_hex="${finder_info_hex:16:4}"
finder_flags=$((16#$finder_flags_hex))
[ $((finder_flags & 16#4000)) -eq $((16#4000)) ] \
  || fail 'mounted background is missing kIsInvisible FinderInfo flag'
tiff_info="$tmp/dmg-background-tiff.txt"
tiffutil -info "$mountpoint/.background.tiff" >"$tiff_info"
directory_count="$(awk '/^Directory at /{count++} END{print count+0}' "$tiff_info")"
assert_eq "$directory_count" 2
frame_manifest="$tmp/dmg-background-frames.txt"
awk '$1 == "Image" && $2 == "Width:" && $4 == "Image" && $5 == "Length:" {print $3 "x" $6}' \
  "$tiff_info" | LC_ALL=C sort >"$frame_manifest"
frame_expected="$tmp/dmg-background-frames.expected"
printf '%s\n' 1320x720 660x360 | LC_ALL=C sort >"$frame_expected"
diff -u "$frame_expected" "$frame_manifest"

detach_integration_mount
[ "$mounted" -eq 0 ] || fail 'throwaway image remained mounted'
rm -f "$dmg"
[ ! -e "$dmg" ] || fail 'throwaway image was not removed'

echo 'dmg-integration-test: PASS'
