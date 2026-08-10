#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$repo_root/scripts/tests/testlib.sh"

bundle_script="${BUNDLE_SCRIPT:-$repo_root/scripts/macos-bundle.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-macos-bundle.XXXXXX")"
cleanup() {
  chmod -R u+w "$tmp" 2>/dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT
fixture="$tmp/fixture"
helpers="$fixture/helpers/bin"
out="$tmp/out"
fake_bin="$tmp/fake-bin"
state_dir="$tmp/state"
tool_log="$tmp/tool.log"
mkdir -p "$fixture/build/darwin" "$helpers" "$out" "$fake_bin" "$state_dir"

cp "$repo_root/build/darwin/Info.plist" "$fixture/build/darwin/Info.plist"
cp "$repo_root/build/darwin/Info.dev.plist" "$fixture/build/darwin/Info.dev.plist"
cp "$repo_root/build/darwin/icons.icns" "$fixture/build/darwin/icons.icns"
put_file "$fixture/bin/loqui"
for helper in globe-listener macos-stt whisper-stt; do put_file "$helpers/$helper"; done
put_file "$helpers/ggml-small.bin" model

for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do
  version="0.16.0"
  [ "$family" = libwhisper ] && version="1.9.1"
  put_file "$helpers/$family.$version.dylib"
  major="${version%%.*}"
  ln -s "$family.$version.dylib" "$helpers/$family.$major.dylib"
  ln -s "$family.$major.dylib" "$helpers/$family.dylib"
done
put_file "$helpers/libSDL2-2.0.0.dylib"
# Homebrew ships SDL read-only and with xattrs; assembly must sanitize its copy without EACCES.
xattr -w com.loqui.test.readonly fixture "$helpers/libSDL2-2.0.0.dylib"
assert_eq "$(xattr -p com.loqui.test.readonly "$helpers/libSDL2-2.0.0.dylib")" fixture
chmod 444 "$helpers/libSDL2-2.0.0.dylib"

framework="$fixture/third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework"
put_file "$framework/Versions/A/MicrosoftCognitiveServicesSpeech"
ln -s A "$framework/Versions/Current"
ln -s Versions/Current/MicrosoftCognitiveServicesSpeech "$framework/MicrosoftCognitiveServicesSpeech"
ln -s ../A/MicrosoftCognitiveServicesSpeech "$framework/Versions/A/SpeechAlias"
xattr -w com.loqui.test.readonly-dir fixture "$framework/Versions/A"
chmod 555 "$framework/Versions/A"

printf '%s\n' /fixture/checkout/frameworks >"$state_dir/loqui.rpaths"
# Prefix lookalikes are not portable Mach-O tokens and must be removed.
printf '%s\n' @loader_path-evil >"$state_dir/globe-listener.rpaths"
printf '%s\n' /fixture/checkout/swift >"$state_dir/macos-stt.rpaths"
printf '%s\n' @loader_path >"$state_dir/whisper-stt.rpaths"
printf '%s\n' /opt/homebrew/opt/sdl2/lib/libSDL2-2.0.0.dylib >"$state_dir/whisper-stt.deps"
printf '%s\n' @loader_path >"$state_dir/MicrosoftCognitiveServicesSpeech.rpaths"
for dylib in "$helpers"/*.dylib; do
  [ -L "$dylib" ] && continue
  base="$(basename "$dylib")"
  printf '%s\n' /fixture/checkout/whisper >"$state_dir/$base.rpaths"
  printf '@rpath/%s\n' "$base" >"$state_dir/$base.id"
done

# The single-quoted lines are source code for the fake, not expressions in this shell.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'target="${!#}"' \
  'base="$(basename "$target")"' \
  'case "$1" in' \
  '  -l)' \
  '    if [ -f "$STATE_DIR/$base.rpaths" ]; then' \
  '      while read -r value; do' \
  '        [ -n "$value" ] || continue' \
  '        printf "Load command 0\n          cmd LC_RPATH\n      cmdsize 48\n         path %s (offset 12)\n" "$value"' \
  '      done <"$STATE_DIR/$base.rpaths"' \
  '    fi' \
  '    case "$base" in libggml-metal.*.dylib) printf "  sectname __ggml_metallib\n   segname __DATA\n" ;; esac' \
  '    ;;' \
  '  -L)' \
  '    printf "%s:\n" "$target"' \
  '    [ ! -f "$STATE_DIR/$base.deps" ] || sed "s|^|\t|" "$STATE_DIR/$base.deps"' \
  '    printf "\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1.0.0)\n"' \
  '    [ "$base" != MicrosoftCognitiveServicesSpeech ] || printf "%s (architecture arm64):\n" "$target"' \
  '    ;;' \
  '  -D)' \
  '    printf "%s:\n" "$target"' \
  '    [ ! -f "$STATE_DIR/$base.id" ] || cat "$STATE_DIR/$base.id"' \
  '    ;;' \
  '  *) exit 2 ;;' \
  'esac' >"$fake_bin/otool"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$TOOL_LOG"' \
  'target="${!#}"' \
  'base="$(basename "$target")"' \
  'case "$1" in' \
  '  -delete_rpath)' \
  '    state="$STATE_DIR/$base.rpaths"; next="$state.next"' \
  '    { [ ! -f "$state" ] || grep -Fvx -- "$2" "$state"; } >"$next" || true' \
  '    mv "$next" "$state"' \
  '    ;;' \
  '  -add_rpath) printf "%s\n" "$2" >>"$STATE_DIR/$base.rpaths" ;;' \
  '  -change) printf "%s\n" "$3" >"$STATE_DIR/$base.deps" ;;' \
  '  -id) printf "%s\n" "$2" >"$STATE_DIR/$base.id" ;;' \
  '  *) exit 2 ;;' \
  'esac' >"$fake_bin/install_name_tool"

# Model fixtures are deliberately tiny. These fakes preserve the production pin at the script
# boundary while letting the test distinguish source validation from post-copy validation.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'target="${!#}"' \
  'if [ "$(basename "$target")" != ggml-small.bin ]; then exec /usr/bin/stat "$@"; fi' \
  'marker="$(/bin/cat "$target")"' \
  'case "$marker:$target" in' \
  '  wrong-size:*|destination-wrong-size:*/Contents/Resources/models/*) printf "1\n" ;;' \
  '  *) printf "487601967\n" ;;' \
  'esac' >"$fake_bin/stat"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'target="${!#}"' \
  'if [ "$(basename "$target")" != ggml-small.bin ]; then exec /usr/bin/shasum "$@"; fi' \
  'marker="$(/bin/cat "$target")"' \
  'good=1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b' \
  'bad=0000000000000000000000000000000000000000000000000000000000000000' \
  'case "$marker:$target" in' \
  '  wrong-digest:*|destination-wrong-digest:*/Contents/Resources/models/*) digest="$bad" ;;' \
  '  *) digest="$good" ;;' \
  'esac' \
  'printf "%s  %s\n" "$digest" "$target"' >"$fake_bin/shasum"
chmod +x "$fake_bin/otool" "$fake_bin/install_name_tool" "$fake_bin/stat" "$fake_bin/shasum"

export STATE_DIR="$state_dir" TOOL_LOG="$tool_log"
export PATH="$fake_bin:$PATH"
xattr -w com.apple.quarantine fixture "$fixture/bin/loqui"

development_app="$out/Loqui.dev.app"
"$bundle_script" --channel development --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$development_app"

assert_file "$development_app/Contents/MacOS/loqui"
for helper in globe-listener macos-stt whisper-stt; do
  assert_file "$development_app/Contents/Helpers/$helper"
done
assert_file "$development_app/Contents/Resources/icons.icns"
assert_absent "$development_app/Contents/Resources/Assets.car"
assert_absent "$development_app/Contents/Resources/helpers"
assert_absent "$development_app/Contents/Resources/models/ggml-small.bin"
[ -L "$development_app/Contents/Frameworks/libwhisper.dylib" ] || fail "libwhisper symlink flattened"
[ -L "$development_app/Contents/Frameworks/MicrosoftCognitiveServicesSpeech.framework/Versions/A/SpeechAlias" ] \
  || fail "contained parent-segment symlink was rejected or flattened"
[ -L "$development_app/Contents/Frameworks/MicrosoftCognitiveServicesSpeech.framework/Versions/Current" ] \
  || fail "contained directory symlink was rejected or flattened"
assert_eq "$(plutil -extract CFBundleIdentifier raw "$development_app/Contents/Info.plist")" \
  com.jualopezmo.loquigo.dev
if xattr -p com.apple.quarantine "$development_app/Contents/MacOS/loqui" >/dev/null 2>&1; then
  fail "quarantine attribute survived assembly"
fi

assert_eq "$(cat "$state_dir/loqui.rpaths")" @executable_path/../Frameworks
assert_eq "$(cat "$state_dir/whisper-stt.rpaths")" @loader_path/../Frameworks
assert_eq "$(cat "$state_dir/libwhisper.1.9.1.dylib.rpaths")" @loader_path
[ ! -s "$state_dir/globe-listener.rpaths" ] || fail "forbidden globe-listener rpath survived"
[ ! -s "$state_dir/macos-stt.rpaths" ] || fail "forbidden macos-stt rpath survived"
assert_eq "$(cat "$state_dir/MicrosoftCognitiveServicesSpeech.rpaths")" @loader_path
assert_eq "$(cat "$state_dir/whisper-stt.deps")" @rpath/libSDL2-2.0.0.dylib
assert_eq "$(cat "$state_dir/libSDL2-2.0.0.dylib.id")" @rpath/libSDL2-2.0.0.dylib
assert_eq "$(/usr/bin/stat -f '%Lp' "$helpers/libSDL2-2.0.0.dylib")" 444
assert_eq "$(xattr -p com.loqui.test.readonly "$helpers/libSDL2-2.0.0.dylib")" fixture

printf '%s\n' @rpath-evil/libInjected.dylib >"$state_dir/globe-listener.deps"
lookalike_dependency_log="$tmp/lookalike-dependency.log"
if "$bundle_script" --channel development --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/LookalikeDependency.app" \
  >"$lookalike_dependency_log" 2>&1; then
  fail "@rpath prefix lookalike dependency unexpectedly passed assembly"
fi
assert_contains "$lookalike_dependency_log" \
  "forbidden Mach-O path: @rpath-evil/libInjected.dylib"
rm "$state_dir/globe-listener.deps"

production_app="$out/Loqui.app"
LOQUI_BUNDLE_MODEL=1 "$bundle_script" --channel production --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$production_app"
assert_file "$production_app/Contents/Resources/models/ggml-small.bin"
[ ! -L "$production_app/Contents/Resources/models/ggml-small.bin" ] || fail "model symlink escaped bundle"
assert_eq "$(cat "$production_app/Contents/Resources/models/ggml-small.bin")" model

expect_model_failure() {
  marker="$1"
  expected="$2"
  candidate="$3"
  put_file "$helpers/ggml-small.bin" "$marker"
  log="$tmp/$candidate.log"
  if LOQUI_BUNDLE_MODEL=1 "$bundle_script" --channel production --root "$fixture" \
    --helpers-dir "$helpers" --executable "$fixture/bin/loqui" \
    --output "$out/$candidate.app" >"$log" 2>&1; then
    fail "bundled model marker '$marker' unexpectedly passed assembly"
  fi
  assert_contains "$log" "$expected"
}

expect_model_failure wrong-size "optional model source size is" ModelSourceSize
expect_model_failure wrong-digest "optional model source SHA-256 is" ModelSourceDigest
expect_model_failure destination-wrong-size "bundled model destination size is" ModelDestinationSize
expect_model_failure destination-wrong-digest "bundled model destination SHA-256 is" ModelDestinationDigest
put_file "$helpers/ggml-small.bin" model
assert_eq "$(/usr/bin/stat -f '%Lp' "$framework/Versions/A")" 555
assert_eq "$(xattr -p com.loqui.test.readonly-dir "$framework/Versions/A")" fixture
chmod 755 "$framework/Versions/A"

outside_dylib="$tmp/outside-source.dylib"
put_file "$outside_dylib" fixture 644
xattr -w com.loqui.test.outside fixture "$outside_dylib"
rm "$helpers/libwhisper.dylib"
ln -s "$outside_dylib" "$helpers/libwhisper.dylib"
escape_log="$tmp/escape.log"
if "$bundle_script" --channel production --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/Escape.app" >"$escape_log" 2>&1; then
  fail "absolute dylib symlink unexpectedly passed assembly"
fi
assert_contains "$escape_log" "absolute symlink in staged bundle"
assert_eq "$(/usr/bin/stat -f '%Lp' "$outside_dylib")" 644
assert_eq "$(xattr -p com.loqui.test.outside "$outside_dylib")" fixture
rm "$helpers/libwhisper.dylib"
ln -s libwhisper.1.dylib "$helpers/libwhisper.dylib"

relative_source="$out/outside-relative.dylib"
relative_stage_target="$out/out/outside-relative.dylib"
put_file "$relative_source" fixture 644
put_file "$relative_stage_target" fixture 644
xattr -w com.loqui.test.relative-outside fixture "$relative_stage_target"
rm "$helpers/libwhisper.dylib"
ln -s ../../../out/outside-relative.dylib "$helpers/libwhisper.dylib"
relative_escape_log="$tmp/relative-escape.log"
if "$bundle_script" --channel production --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/RelativeEscape.app" >"$relative_escape_log" 2>&1; then
  fail "escaping relative dylib symlink unexpectedly passed assembly"
fi
assert_contains "$relative_escape_log" "escaping symlink in staged bundle"
assert_eq "$(/usr/bin/stat -f '%Lp' "$relative_stage_target")" 644
assert_eq "$(xattr -p com.loqui.test.relative-outside "$relative_stage_target")" fixture
rm "$helpers/libwhisper.dylib"
ln -s libwhisper.1.dylib "$helpers/libwhisper.dylib"

directory_source="$out/directory-outside"
directory_stage_target="$out/out/directory-outside"
mkdir -p "$directory_source" "$directory_stage_target"
xattr -w com.loqui.test.directory-outside fixture "$directory_stage_target"
chmod 755 "$directory_stage_target"
ln -s ../../../../../../out/directory-outside "$framework/Versions/A/EscapeDir"
directory_escape_log="$tmp/directory-escape.log"
if "$bundle_script" --channel production --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/DirectoryEscape.app" >"$directory_escape_log" 2>&1; then
  fail "escaping relative directory symlink unexpectedly passed assembly"
fi
assert_contains "$directory_escape_log" "escaping symlink in staged bundle"
assert_eq "$(/usr/bin/stat -f '%Lp' "$directory_stage_target")" 755
assert_eq "$(xattr -p com.loqui.test.directory-outside "$directory_stage_target")" fixture
rm "$framework/Versions/A/EscapeDir"

run_expect_fail "$bundle_script" --channel invalid --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/Invalid.app"
put_file "$fixture/build/darwin/Assets.car" asset
run_expect_fail "$bundle_script" --channel production --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/Assets.app"
rm "$fixture/build/darwin/Assets.car"
mv "$helpers/macos-stt" "$helpers/macos-stt.absent"
run_expect_fail "$bundle_script" --channel production --root "$fixture" --helpers-dir "$helpers" \
  --executable "$fixture/bin/loqui" --output "$out/Missing.app"
mv "$helpers/macos-stt.absent" "$helpers/macos-stt"

echo "macos-bundle-test: PASS"
