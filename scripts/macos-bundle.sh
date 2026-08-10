#!/usr/bin/env bash
set -euo pipefail

die() { echo "macos-bundle: $*" >&2; exit 1; }

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/whisper-model-integrity.sh
. "$script_root/scripts/whisper-model-integrity.sh"
root="$script_root"
channel=""
executable=""
helpers_dir=""
output=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --channel) [ "$#" -ge 2 ] || die "--channel requires a value"; channel="$2"; shift 2 ;;
    --executable) [ "$#" -ge 2 ] || die "--executable requires a path"; executable="$2"; shift 2 ;;
    --helpers-dir) [ "$#" -ge 2 ] || die "--helpers-dir requires a path"; helpers_dir="$2"; shift 2 ;;
    --output) [ "$#" -ge 2 ] || die "--output requires a path"; output="$2"; shift 2 ;;
    --root) [ "$#" -ge 2 ] || die "--root requires a path"; root="$2"; shift 2 ;;
    *) die "unknown argument: $1" ;;
  esac
done

case "$channel" in
  production) plist_name="Info.plist" ;;
  development) plist_name="Info.dev.plist" ;;
  *) die "channel must be production or development" ;;
esac

[ -n "$executable" ] || die "missing --executable"
[ -n "$helpers_dir" ] || die "missing --helpers-dir"
[ -n "$output" ] || die "missing --output"
case "$(basename "$output")" in *.app) ;; *) die "output must end in .app" ;; esac

root="$(cd "$root" && pwd)"
helpers_dir="$(cd "$helpers_dir" && pwd)"
[ -f "$executable" ] || die "missing executable: $executable"
mkdir -p "$(dirname "$output")"
output_parent="$(cd "$(dirname "$output")" && pwd)"
output_abs="$output_parent/$(basename "$output")"
[ "$output_abs" != "/" ] || die "refusing root output"
[ "$output_abs" != "$root" ] || die "refusing repo-root output"
[ "$output_abs" != "$root/bin" ] || die "refusing bin-directory output"
[ "$output_abs" != "${HOME:-/__loqui_no_home__}" ] || die "refusing home output"

plist="$root/build/darwin/$plist_name"
icon="$root/build/darwin/icons.icns"
framework_source="$root/third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework"
[ -f "$plist" ] || die "missing plist: $plist"
[ -f "$icon" ] || die "missing icon: $icon"
[ ! -e "$root/build/darwin/Assets.car" ] || die "unexpected forbidden asset: build/darwin/Assets.car"
[ -d "$framework_source" ] || die "missing framework: $framework_source"

for helper in globe-listener macos-stt whisper-stt; do
  [ -f "$helpers_dir/$helper" ] || die "missing helper: $helpers_dir/$helper"
done

rm -rf "$output_abs"
macos_dir="$output_abs/Contents/MacOS"
bundle_helpers="$output_abs/Contents/Helpers"
frameworks="$output_abs/Contents/Frameworks"
resources="$output_abs/Contents/Resources"
mkdir -p "$macos_dir" "$bundle_helpers" "$frameworks" "$resources"
cp "$executable" "$macos_dir/loqui"
cp "$plist" "$output_abs/Contents/Info.plist"
cp "$icon" "$resources/icons.icns"
ditto "$framework_source" "$frameworks/MicrosoftCognitiveServicesSpeech.framework"
for helper in globe-listener macos-stt whisper-stt; do
  cp "$helpers_dir/$helper" "$bundle_helpers/$helper"
done

is_packaged_dylib() {
  case "$1" in
    libwhisper.dylib|libwhisper.[0-9]*.dylib|\
    libggml.dylib|libggml.[0-9]*.dylib|\
    libggml-base.dylib|libggml-base.[0-9]*.dylib|\
    libggml-cpu.dylib|libggml-cpu.[0-9]*.dylib|\
    libggml-blas.dylib|libggml-blas.[0-9]*.dylib|\
    libggml-metal.dylib|libggml-metal.[0-9]*.dylib|\
    libSDL2-2.0.0.dylib) return 0 ;;
    *) return 1 ;;
  esac
}

for source in "$helpers_dir"/*.dylib; do
  [ -e "$source" ] || [ -L "$source" ] || continue
  base="$(basename "$source")"
  is_packaged_dylib "$base" || continue
  [ ! -L "$source" ] || [ -e "$source" ] || die "broken dylib symlink: $source"
  cp -a "$source" "$frameworks/"
done

for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do
  found_real=0
  found_link=0
  for candidate in "$frameworks"/"$family"*.dylib; do
    [ -e "$candidate" ] || [ -L "$candidate" ] || continue
    base="$(basename "$candidate")"
    case "$family:$base" in
      libggml:libggml-base*|libggml:libggml-cpu*|libggml:libggml-blas*|libggml:libggml-metal*) continue ;;
    esac
    if [ -L "$candidate" ]; then found_link=1; else found_real=1; fi
  done
  [ "$found_real" -eq 1 ] && [ "$found_link" -eq 1 ] || die "incomplete dylib family: $family"
done
[ -f "$frameworks/libSDL2-2.0.0.dylib" ] || die "missing SDL dylib"

if [ "${LOQUI_BUNDLE_MODEL:-}" = "1" ]; then
  model_source="$helpers_dir/ggml-small.bin"
  model_destination="$resources/models/ggml-small.bin"
  verify_whisper_model "$model_source" "optional model source" || exit 1
  mkdir -p "$resources/models"
  cp -L "$model_source" "$model_destination"
  verify_whisper_model "$model_destination" "bundled model destination" || exit 1
fi

output_physical="$(cd "$output_abs" && pwd -P)"
while read -r staged_link; do
  link_target="$(readlink "$staged_link")"
  case "$link_target" in
    /*) die "absolute symlink in staged bundle: ${staged_link#"$output_abs"/}" ;;
  esac
  if [ -d "$staged_link" ]; then
    resolved_target="$(cd "$staged_link" 2>/dev/null && pwd -P)" \
      || die "unresolvable symlink in staged bundle: ${staged_link#"$output_abs"/}"
  else
    link_dir="$(dirname "$staged_link")"
    target_dir="$(dirname "$link_target")"
    resolved_parent="$(cd "$link_dir/$target_dir" 2>/dev/null && pwd -P)" \
      || die "unresolvable symlink in staged bundle: ${staged_link#"$output_abs"/}"
    resolved_target="$resolved_parent/$(basename "$link_target")"
  fi
  case "$resolved_target" in
    "$output_physical"|"$output_physical"/*) ;;
    *) die "escaping symlink in staged bundle: ${staged_link#"$output_abs"/}" ;;
  esac
done < <(find "$output_abs" -type l -print | LC_ALL=C sort)

# Assembly mutates Mach-O load commands and clears inherited extended attributes.
# Inputs may supply read-only code directories, so make only staged real nodes writable.
find "$output_abs" \( -type f -o -type d \) -exec chmod u+w {} +

rpaths() {
  otool -l "$1" | awk '/cmd LC_RPATH/{seen=1; next} seen && /path /{print $2; seen=0}'
}

reset_rpaths() {
  target="$1"
  desired="$2"
  while read -r old_rpath; do
    [ -n "$old_rpath" ] && install_name_tool -delete_rpath "$old_rpath" "$target"
  done < <(rpaths "$target")
  install_name_tool -add_rpath "$desired" "$target"
}

preserve_portable_rpaths() {
  target="$1"
  while read -r old_rpath; do
    case "$old_rpath" in
      /usr/lib/*|/System/*|@rpath|@rpath/*|@loader_path|@loader_path/*|@executable_path|@executable_path/*) ;;
      *) [ -n "$old_rpath" ] && install_name_tool -delete_rpath "$old_rpath" "$target" ;;
    esac
  done < <(rpaths "$target")
}

main_executable="$macos_dir/loqui"
whisper_helper="$bundle_helpers/whisper-stt"
framework_executable="$frameworks/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech"
[ -f "$framework_executable" ] || die "missing framework executable: $framework_executable"
reset_rpaths "$main_executable" '@executable_path/../Frameworks'
reset_rpaths "$whisper_helper" '@loader_path/../Frameworks'
preserve_portable_rpaths "$bundle_helpers/globe-listener"
preserve_portable_rpaths "$bundle_helpers/macos-stt"
preserve_portable_rpaths "$framework_executable"

while read -r dependency; do
  case "$dependency" in
    /opt/homebrew/*libSDL2-2.0.0.dylib|/usr/local/*libSDL2-2.0.0.dylib)
      install_name_tool -change "$dependency" '@rpath/libSDL2-2.0.0.dylib' "$whisper_helper"
      ;;
  esac
done < <(otool -L "$whisper_helper" | awk '/^[[:space:]]/{print $1}')

while read -r dylib; do
  reset_rpaths "$dylib" '@loader_path'
  base="$(basename "$dylib")"
  if [ "$base" = "libSDL2-2.0.0.dylib" ]; then
    install_name_tool -id '@rpath/libSDL2-2.0.0.dylib' "$dylib"
  else
    dylib_id="$(otool -D "$dylib" | awk 'NR == 2 {print; exit}')"
    case "$dylib_id" in @rpath/*) ;; *) die "$base has non-portable dylib ID: $dylib_id" ;; esac
  fi
done < <(find "$frameworks" -type f -name '*.dylib' -print | LC_ALL=C sort)

xattr -cr "$output_abs"
if xattr -lr "$output_abs" 2>/dev/null | grep -E 'com\.apple\.(ResourceFork|FinderInfo|quarantine)' >/dev/null; then
  die "signing-blocking extended attribute remains"
fi

check_allowed_path() {
  case "$2" in
    /usr/lib/*|/System/*|@rpath|@rpath/*|@loader_path|@loader_path/*|@executable_path|@executable_path/*) ;;
    *) die "$1 has forbidden Mach-O path: $2" ;;
  esac
}

check_macho() {
  target="$1"
  while read -r dependency; do
    [ -n "$dependency" ] && check_allowed_path "$target" "$dependency"
  done < <(otool -L "$target" | awk '/^[[:space:]]/{print $1}')
  while read -r current_rpath; do
    [ -n "$current_rpath" ] && check_allowed_path "$target" "$current_rpath"
  done < <(rpaths "$target")
}

check_macho "$main_executable"
for helper in globe-listener macos-stt whisper-stt; do check_macho "$bundle_helpers/$helper"; done
check_macho "$framework_executable"
while read -r dylib; do
  check_macho "$dylib"
  dylib_id="$(otool -D "$dylib" | awk 'NR == 2 {print; exit}')"
  check_allowed_path "$dylib" "$dylib_id"
done < <(find "$frameworks" -type f -name '*.dylib' -print | LC_ALL=C sort)

metal="$(find "$frameworks" -type f -name 'libggml-metal.*.dylib' -print | LC_ALL=C sort | head -1)"
[ -n "$metal" ] || die "missing real libggml-metal dylib"
otool -l "$metal" | grep -q '__ggml_metallib' || die "libggml-metal lacks embedded metallib"

printf '%s\n' "$output_abs"
