#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

build_script="${HELPERS_BUILD_SCRIPT:-$repo_root/scripts/build-macos-helpers.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-build-helpers.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
fake_bin="$tmp/builders"
output="$tmp/output"
tool_log="$tmp/tool.log"
sdl_vendor_log="$tmp/sdl-vendor.log"
mkdir -p "$fake_bin"

make_builder() {
  name="$1"
  # These lines are the body of the generated fixture script; expansion there is intentional.
  # shellcheck disable=SC2016
  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -euo pipefail' \
    'mkdir -p "$LOQUI_HELPERS_OUTPUT_DIR"' \
    "printf '%s\\n' '$name' >>\"\$TOOL_LOG\"" \
    "printf fixture >\"\$LOQUI_HELPERS_OUTPUT_DIR/$name\"" \
    "chmod 755 \"\$LOQUI_HELPERS_OUTPUT_DIR/$name\"" >"$fake_bin/$name"
  chmod +x "$fake_bin/$name"
}

make_builder globe-listener
make_builder macos-stt
make_builder whisper-stt
# These lines are appended to the generated whisper fixture and expand when that fixture runs.
# shellcheck disable=SC2016
printf '%s\n' \
  'for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do' \
  '  version=0.16.0; [ "$family" = libwhisper ] && version=1.9.1' \
  '  printf fixture >"$LOQUI_HELPERS_OUTPUT_DIR/$family.$version.dylib"' \
  '  major="${version%%.*}"' \
  '  ln -s "$family.$version.dylib" "$LOQUI_HELPERS_OUTPUT_DIR/$family.$major.dylib"' \
  '  ln -s "$family.$major.dylib" "$LOQUI_HELPERS_OUTPUT_DIR/$family.dylib"' \
  'done' \
  'printf fixture >"$LOQUI_HELPERS_OUTPUT_DIR/libSDL2-2.0.0.dylib"' \
  'printf "%s\n" "$LOQUI_SDL_VENDOR_DIR" >"$SDL_VENDOR_LOG"' >>"$fake_bin/whisper-stt"

export TOOL_LOG="$tool_log"
export SDL_VENDOR_LOG="$sdl_vendor_log"
GLOBE_BUILD_SCRIPT="$fake_bin/globe-listener" \
MACOS_STT_BUILD_SCRIPT="$fake_bin/macos-stt" \
WHISPER_BUILD_SCRIPT="$fake_bin/whisper-stt" \
LOQUI_HELPERS_OUTPUT_DIR="$output" \
LOQUI_WHISPER_VENDOR_DIR="$tmp/vendor/whisper" \
  "$build_script"

assert_eq "$(sed -n '1p' "$tool_log")" globe-listener
assert_eq "$(sed -n '2p' "$tool_log")" macos-stt
assert_eq "$(sed -n '3p' "$tool_log")" whisper-stt
assert_eq "$(cat "$sdl_vendor_log")" "$tmp/vendor/SDL-5d249570393f"
for helper in globe-listener macos-stt whisper-stt; do assert_file "$output/$helper"; done
for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do
  [ -L "$output/$family.dylib" ] || fail "missing symlink family: $family"
done
assert_file "$output/libSDL2-2.0.0.dylib"
assert_absent "$output/ggml-small.bin"

printf '%s\n' '#!/usr/bin/env bash' 'exit 0' >"$fake_bin/missing-macos-stt"
chmod +x "$fake_bin/missing-macos-stt"
run_expect_fail env \
  GLOBE_BUILD_SCRIPT="$fake_bin/globe-listener" \
  MACOS_STT_BUILD_SCRIPT="$fake_bin/missing-macos-stt" \
  WHISPER_BUILD_SCRIPT="$fake_bin/whisper-stt" \
  LOQUI_HELPERS_OUTPUT_DIR="$tmp/missing-output" \
  TOOL_LOG="$tool_log" \
  "$build_script"

echo "build-macos-helpers-test: PASS"
