#!/usr/bin/env bash
set -euo pipefail

die() { echo "macos-audit: $*" >&2; exit 1; }

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/whisper-model-integrity.sh
. "$script_root/scripts/whisper-model-integrity.sh"

channel=""
version=""
app=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --channel) [ "$#" -ge 2 ] || die "--channel requires a value"; channel="$2"; shift 2 ;;
    --version) [ "$#" -ge 2 ] || die "--version requires a value"; version="$2"; shift 2 ;;
    --*) die "unknown argument: $1" ;;
    *) [ -z "$app" ] || die "only one app path is allowed"; app="$1"; shift ;;
  esac
done

case "$channel" in
  production) expected_bundle_id="com.jualopezmo.loquigo" ;;
  development) expected_bundle_id="com.jualopezmo.loquigo.dev" ;;
  *) die "--channel must be production or development" ;;
esac
[ -n "$version" ] || die "missing --version"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "malformed --version: $version"
[ -n "$app" ] || die "missing app path"
[ -d "$app" ] || die "missing app: $app"

plist="$app/Contents/Info.plist"
macos_dir="$app/Contents/MacOS"
helpers_dir="$app/Contents/Helpers"
frameworks_dir="$app/Contents/Frameworks"
resources_dir="$app/Contents/Resources"
[ -f "$plist" ] || die "missing Contents/Info.plist"

plist_value() { plutil -extract "$1" raw "$plist" 2>/dev/null || true; }
require_plist_value() {
  key="$1"
  expected="$2"
  actual="$(plist_value "$key")"
  [ "$actual" = "$expected" ] || die "Info.plist $key is '$actual', expected '$expected'"
}

require_plist_value CFBundleIdentifier "$expected_bundle_id"
for privacy_key in NSMicrophoneUsageDescription NSSpeechRecognitionUsageDescription NSAppleEventsUsageDescription; do
  privacy_value="$(plist_value "$privacy_key")"
  [ -n "$privacy_value" ] || die "Info.plist $privacy_key is missing or empty"
done
require_plist_value LSMinimumSystemVersion 14.0.0
require_plist_value CFBundleShortVersionString "$version"
require_plist_value CFBundleVersion "$version"

bundle_executable="$(plist_value CFBundleExecutable)"
[ -n "$bundle_executable" ] || die "Info.plist CFBundleExecutable is missing or empty"
[ -d "$macos_dir" ] || die "missing Contents/MacOS"
main_count=0
main_path=""
while read -r candidate; do
  [ -n "$candidate" ] || continue
  main_count=$((main_count + 1))
  main_path="$candidate"
done < <(find "$macos_dir" -maxdepth 1 -type f -print | LC_ALL=C sort)
[ "$main_count" -eq 1 ] || die "Contents/MacOS contains $main_count real files, expected 1"
[ "$(basename "$main_path")" = "$bundle_executable" ] \
  || die "Info.plist CFBundleExecutable is '$bundle_executable', sole Contents/MacOS file is '$(basename "$main_path")'"

for required in \
  "$helpers_dir/globe-listener" \
  "$helpers_dir/macos-stt" \
  "$helpers_dir/whisper-stt" \
  "$resources_dir/icons.icns" \
  "$frameworks_dir/libSDL2-2.0.0.dylib" \
  "$frameworks_dir/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech"; do
  [ -f "$required" ] || die "missing ${required#"$app"/}"
done
[ ! -e "$resources_dir/Assets.car" ] || die "forbidden Contents/Resources/Assets.car"

bundled_model="$resources_dir/models/ggml-small.bin"
if [ -e "$bundled_model" ] || [ -L "$bundled_model" ]; then
  [ ! -L "$bundled_model" ] || die "bundled model must be a real file"
  verify_whisper_model "$bundled_model" "bundled model" || exit 1
fi

broken_link="$(find "$app" -type l ! -exec test -e {} \; -print -quit)"
[ -z "$broken_link" ] || die "broken symlink: ${broken_link#"$app"/}"

family_member() {
  family="$1"
  base="$2"
  case "$family:$base" in
    libggml:libggml-base*|libggml:libggml-cpu*|libggml:libggml-blas*|libggml:libggml-metal*) return 1 ;;
    "$family":"$family".dylib|"$family":"$family".[0-9]*.dylib) return 0 ;;
    *) return 1 ;;
  esac
}

inspect_file_type() {
  inspected_path="$1"
  [ -r "$inspected_path" ] \
    || die "regular file is not readable: ${inspected_path#"$app"/}"
  file_description=""
  file_rc=0
  set +e
  file_description="$(file -b "$inspected_path" 2>&1)"
  file_rc=$?
  set -e
  [ "$file_rc" -eq 0 ] \
    || die "file -b failed for ${inspected_path#"$app"/} (exit $file_rc): $file_description"
}

for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do
  real_count=0
  link_count=0
  for candidate in "$frameworks_dir"/"$family"*.dylib; do
    [ -e "$candidate" ] || [ -L "$candidate" ] || continue
    family_member "$family" "$(basename "$candidate")" || continue
    if [ -L "$candidate" ]; then
      link_count=$((link_count + 1))
    else
      real_count=$((real_count + 1))
    fi
  done
  [ "$real_count" -eq 1 ] && [ "$link_count" -ge 1 ] && [ -L "$frameworks_dir/$family.dylib" ] \
    || die "invalid $family dylib chain: real=$real_count symlinks=$link_count"
done

while read -r resource; do
  [ -n "$resource" ] || continue
  inspect_file_type "$resource"
  if [[ "$file_description" == *Mach-O* ]]; then
    die "Mach-O code under Resources: ${resource#"$app"/}"
  fi
  case "$resource" in *.metal|*.metallib) die "external Metal resource: ${resource#"$app"/}" ;; esac
done < <(find "$resources_dir" -type f -print | LC_ALL=C sort)

manifest="$(mktemp "${TMPDIR:-/tmp}/loqui-macos-audit.XXXXXX")"
trap 'rm -f "$manifest"' EXIT
while read -r candidate; do
  [ -n "$candidate" ] || continue
  inspect_file_type "$candidate"
  [[ "$file_description" == *Mach-O* ]] && printf '%s\n' "$candidate" >>"$manifest"
done < <(find "$macos_dir" "$helpers_dir" "$frameworks_dir" -type f -print | LC_ALL=C sort)

require_macho() {
  grep -Fqx -- "$1" "$manifest" || die "expected Mach-O not found: ${1#"$app"/}"
}
require_macho "$main_path"
for helper in globe-listener macos-stt whisper-stt; do require_macho "$helpers_dir/$helper"; done
framework_executable="$frameworks_dir/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech"
require_macho "$framework_executable"
while read -r dylib; do require_macho "$dylib"; done \
  < <(find "$frameworks_dir" -type f -name '*.dylib' -print | LC_ALL=C sort)

relative_path() { printf '%s' "${1#"$app"/}"; }
allowed_macho_path() {
  case "$1" in
    /usr/lib/*|/System/*|@rpath|@rpath/*|@loader_path|@loader_path/*|@executable_path|@executable_path/*) return 0 ;;
    *) return 1 ;;
  esac
}
rpaths() { otool -l "$1" | awk '/cmd LC_RPATH/{seen=1; next} seen && /path /{print $2; seen=0}'; }
inspect_dependencies() {
  inspected_path="$1"
  otool_l_output=""
  otool_l_rc=0
  set +e
  otool_l_output="$(otool -L "$inspected_path" 2>&1)"
  otool_l_rc=$?
  set -e
  [ "$otool_l_rc" -eq 0 ] \
    || die "otool -L failed for ${inspected_path#"$app"/} (exit $otool_l_rc): $otool_l_output"
}
inspect_build_versions() {
  inspected_path="$1"
  vtool_output=""
  vtool_rc=0
  set +e
  vtool_output="$(vtool -show-build "$inspected_path" 2>&1)"
  vtool_rc=$?
  set -e
  [ "$vtool_rc" -eq 0 ] \
    || die "vtool -show-build failed for ${inspected_path#"$app"/} (exit $vtool_rc): $vtool_output"
}
version_at_most() {
  awk -v actual="$1" -v maximum="$2" 'BEGIN {
    split(actual, a, "."); split(maximum, m, ".")
    for (i = 1; i <= 3; i++) {
      av = (a[i] == "" ? 0 : a[i]) + 0
      mv = (m[i] == "" ? 0 : m[i]) + 0
      if (av < mv) exit 0
      if (av > mv) exit 1
    }
    exit 0
  }'
}

while read -r mach_o; do
  [ -n "$mach_o" ] || continue
  relative="$(relative_path "$mach_o")"
  archs="$(lipo -archs "$mach_o")"
  if [ "$mach_o" = "$framework_executable" ]; then
    case "$archs" in arm64|"arm64 x86_64"|"x86_64 arm64") ;; *) die "$relative architectures are '$archs', expected arm64" ;; esac
  else
    [ "$archs" = arm64 ] || die "$relative architectures are '$archs', expected exactly arm64"
  fi

  minos_count=0
  inspect_build_versions "$mach_o"
  minimum_versions="$(awk '$1 == "minos" { print $2 }' <<<"$vtool_output")"
  while read -r minimum_version; do
    [ -n "$minimum_version" ] || continue
    minos_count=$((minos_count + 1))
    if [ "$mach_o" = "$helpers_dir/macos-stt" ]; then
      [ "$minimum_version" = 26.0 ] \
        || die "$relative minimum macOS version is $minimum_version, expected exactly 26.0"
    else
      version_at_most "$minimum_version" 14.0 \
        || die "$relative minimum macOS version is $minimum_version, expected at most 14.0"
    fi
  done <<<"$minimum_versions"
  [ "$minos_count" -ge 1 ] || die "$relative has no macOS minimum version"

  inspect_dependencies "$mach_o"
  dependency_list="$(awk '/^[[:space:]]/{print $1}' <<<"$otool_l_output")"
  while read -r dependency; do
    [ -z "$dependency" ] || allowed_macho_path "$dependency" \
      || die "$relative has forbidden dependency: $dependency"
  done <<<"$dependency_list"

  current_rpaths="$(rpaths "$mach_o")"
  while read -r current_rpath; do
    [ -z "$current_rpath" ] || allowed_macho_path "$current_rpath" \
      || die "$relative has forbidden rpath: $current_rpath"
  done <<<"$current_rpaths"

  case "$mach_o" in
    "$main_path") [ "$current_rpaths" = '@executable_path/../Frameworks' ] \
      || die "$relative rpath is '$current_rpaths', expected @executable_path/../Frameworks" ;;
    "$helpers_dir/whisper-stt") [ "$current_rpaths" = '@loader_path/../Frameworks' ] \
      || die "$relative rpath is '$current_rpaths', expected @loader_path/../Frameworks" ;;
    *.dylib)
      [ "$current_rpaths" = '@loader_path' ] \
        || die "$relative rpath is '$current_rpaths', expected @loader_path"
      dylib_id="$(otool -D "$mach_o" | sed -n '2p')"
      case "$dylib_id" in @rpath/*) ;; *) die "$relative has forbidden dylib ID: $dylib_id" ;; esac
      ;;
  esac
done <"$manifest"

metal_dylib=""
while read -r candidate; do metal_dylib="$candidate"; break; done \
  < <(find "$frameworks_dir" -type f -name 'libggml-metal*.dylib' -print | LC_ALL=C sort)
[ -n "$metal_dylib" ] || die "missing real libggml-metal dylib"
otool -l "$metal_dylib" | awk '
  $1 == "segname" && $2 == "__DATA" { in_data=1; next }
  in_data && $1 == "sectname" && $2 == "__ggml_metallib" { found=1 }
  END { exit found ? 0 : 1 }
' || die "$(relative_path "$metal_dylib") lacks __DATA,__ggml_metallib"

while read -r path; do
  [ -n "$path" ] || continue
  for attribute in com.apple.ResourceFork com.apple.FinderInfo com.apple.quarantine; do
    if xattr -p "$attribute" "$path" >/dev/null 2>&1; then
      die "$(relative_path "$path") has signing-blocking xattr $attribute"
    fi
  done
done < <(find "$app" -print | LC_ALL=C sort)

echo "macos-audit: ok $app"
