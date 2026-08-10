#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-component-helpers.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
tmp_absolute="$(cd "$tmp" && pwd)"
fixture="$tmp/project"
fake_bin="$tmp/bin"
tool_log="$tmp/tool.log"
mkdir -p "$fixture/scripts" "$fixture/helpers" "$fake_bin"

cp "$repo_root/scripts/build-globe-listener.sh" "$fixture/scripts/"
cp "$repo_root/scripts/build-macos-stt.sh" "$fixture/scripts/"
put_file "$fixture/helpers/macos-globe-listener.swift"
put_file "$fixture/helpers/macos-stt.swift"

printf '%s\n' '#!/usr/bin/env bash' 'printf Darwin' >"$fake_bin/uname"
# The single-quoted lines below are the generated swiftc fixture body.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "swiftc %s\n" "$*" >>"$TOOL_LOG"' \
  'out=' \
  'while [ "$#" -gt 0 ]; do' \
  '  if [ "$1" = -o ]; then out="$2"; shift 2; else shift; fi' \
  'done' \
  '[ -n "$out" ]' \
  'mkdir -p "$(dirname "$out")"' \
  'printf fixture >"$out"' \
  'chmod 755 "$out"' >"$fake_bin/swiftc"
printf '%s\n' '#!/usr/bin/env bash' 'printf %s arm64-fixture' >"$fake_bin/file"
chmod +x "$fake_bin/uname" "$fake_bin/swiftc" "$fake_bin/file"

globe_output="$tmp/globe-output"
PATH="$fake_bin:$PATH" TOOL_LOG="$tool_log" LOQUI_HELPERS_OUTPUT_DIR="$globe_output" \
  "$fixture/scripts/build-globe-listener.sh"
assert_file "$globe_output/globe-listener"
assert_absent "$fixture/helpers/bin/globe-listener"

macos_output="$tmp/macos-output"
PATH="$fake_bin:$PATH" TOOL_LOG="$tool_log" LOQUI_HELPERS_OUTPUT_DIR="$macos_output" \
  "$fixture/scripts/build-macos-stt.sh"
assert_file "$macos_output/macos-stt"
assert_absent "$fixture/helpers/bin/macos-stt"

cp "$repo_root/scripts/build-whisper-stt.sh" "$fixture/scripts/"
cp "$repo_root/scripts/whisper-stt.CMakeLists.txt" "$fixture/scripts/"
put_file "$fixture/helpers/whisper-stt.cpp"

expected_commit="97c56f1dc1d1100a9d859c865a20c82d22f823ed"
expected_sdl_commit="5d249570393f7a37e037abf22cd6012a4cc56a71"
state_dir="$tmp/tool-state"
mkdir -p "$state_dir"

# The single-quoted lines below are generated fixture tools and expand when those tools run.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "git %s\n" "$*" >>"$TOOL_LOG"' \
  'if [ "$1" = clone ]; then' \
  '  for arg in "$@"; do destination="$arg"; done' \
  '  mkdir -p "$destination/.git" "$destination/examples" "$destination/build/bin"' \
  '  printf "add_subdirectory(cli)\n" >"$destination/examples/CMakeLists.txt"' \
  '  case "$destination" in *sdl*) touch "$TOOL_STATE/sdl-object" ;; *) touch "$TOOL_STATE/whisper-object" ;; esac' \
  'elif [ "$1" = -C ]; then' \
  '  repository="$2"; operation="$3"' \
  '  case "$repository" in *sdl*) kind=sdl ;; *) kind=whisper ;; esac' \
  '  case "$operation" in' \
  '    init) mkdir -p "$repository/.git" ;;' \
  '    remote)' \
  '      if [ "$4" = add ]; then printf "%s\n" "$6" >"$TOOL_STATE/$kind-origin"; fi' \
  '      if [ "$4" = get-url ]; then cat "$TOOL_STATE/$kind-origin"; fi' \
  '      ;;' \
  '    cat-file) [ -f "$TOOL_STATE/$kind-object" ] ;;' \
  '    fetch) touch "$TOOL_STATE/$kind-object" ;;' \
  '    checkout) : ;;' \
  '    rev-parse)' \
  '      case "$kind" in sdl) printf "%s\n" "$EXPECTED_SDL_COMMIT" ;; *) printf "%s\n" "$EXPECTED_COMMIT" ;; esac' \
  '      ;;' \
  '    status)' \
  '      if [ "$kind" = sdl ]; then' \
  '        if [ -d "$repository/-build-loqui" ] || [ -d "$repository/-install-loqui" ]; then' \
  '          printf "?? -build-loqui/generated.c\n"' \
  '        fi' \
  '        case "${SDL_DIRTY:-}" in' \
  '          worktree) printf " M src/audio.c\n" ;;' \
  '          index) printf "M  src/audio.c\n" ;;' \
  '          untracked) case " $* " in *" --untracked-files=all "*) printf "?? src/local-audio.m\n" ;; esac ;;' \
  '        esac' \
  '      fi' \
  '      ;;' \
  '    diff) [ "$kind" != sdl ] || [ -z "${SDL_DIRTY:-}" ] ;;' \
  '  esac' \
  'fi' >"$fake_bin/git"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "cmake %s\n" "$*" >>"$TOOL_LOG"' \
  '[ "$1" = --build ] || exit 0' \
  'build_dir="$2"' \
  'if [ "$build_dir" = "$SDL_BUILD_DIR" ] || [ "$build_dir" = "${SDL_SOURCE_DIR:-/__no_sdl_source__}/-build-loqui" ]; then' \
  '  install_prefix="$SDL_INSTALL_PREFIX"' \
  '  [ "$build_dir" != "${SDL_SOURCE_DIR:-/__no_sdl_source__}/-build-loqui" ] || install_prefix="$SDL_SOURCE_DIR/-install-loqui"' \
  '  mkdir -p "$build_dir" "$install_prefix/lib"' \
  '  printf fixture >"$install_prefix/lib/libSDL2-2.0.0.dylib"' \
  '  chmod 444 "$install_prefix/lib/libSDL2-2.0.0.dylib"' \
  '  exit 0' \
  'fi' \
  'mkdir -p "$build_dir/bin"' \
  'printf fixture >"$build_dir/bin/whisper-stt"' \
  'chmod 755 "$build_dir/bin/whisper-stt"' \
  'for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do' \
  '  version=0.16.0; [ "$family" = libwhisper ] && version=1.9.1' \
  '  printf fixture >"$build_dir/bin/$family.$version.dylib"' \
  '  major="${version%%.*}"' \
  '  ln -sfn "$family.$version.dylib" "$build_dir/bin/$family.$major.dylib"' \
  '  ln -sfn "$family.$major.dylib" "$build_dir/bin/$family.dylib"' \
  'done' >"$fake_bin/cmake"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'mode="$1"; target="$2"; base="$(basename "$target")"' \
  'case "$mode" in' \
  '  -l)' \
  '    rpath="/private/tmp/whisper-build"' \
  '    [ ! -f "$TOOL_STATE/rpath.$base" ] || rpath="$(cat "$TOOL_STATE/rpath.$base")"' \
  '    [ -z "$rpath" ] || printf "cmd LC_RPATH\npath %s (offset 12)\n" "$rpath"' \
  '    case "$base" in libggml-metal.*) printf "segname __DATA\nsectname __ggml_metallib\n" ;; esac' \
  '    ;;' \
  '  -L)' \
  '    printf "%s:\n" "$target"' \
  '    if [ "$base" = whisper-stt ]; then' \
  '      dependency="$SDL_INSTALL_NAME"' \
  '      [ ! -f "$TOOL_STATE/dependency.$base" ] || dependency="$(cat "$TOOL_STATE/dependency.$base")"' \
  '      printf "\t%s (compatibility version 1.0.0)\n" "$dependency"' \
  '    else printf "\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0)\n"; fi' \
  '    ;;' \
  '  -D)' \
  '    printf "%s:\n@rpath/%s\n" "$target" "$base"' \
  '    ;;' \
  'esac' >"$fake_bin/otool"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "install_name_tool %s\n" "$*" >>"$TOOL_LOG"' \
  'action="$1"' \
  'case "$action" in' \
  '  -delete_rpath) target="$3"; : >"$TOOL_STATE/rpath.$(basename "$target")" ;;' \
  '  -add_rpath) target="$3"; printf "%s" "$2" >"$TOOL_STATE/rpath.$(basename "$target")" ;;' \
  '  -change)' \
  '    target="$4"; base="$(basename "$target")"; current="$SDL_INSTALL_NAME"' \
  '    [ ! -f "$TOOL_STATE/dependency.$base" ] || current="$(cat "$TOOL_STATE/dependency.$base")"' \
  '    [ "$current" != "$2" ] || printf "%s" "$3" >"$TOOL_STATE/dependency.$base"' \
  '    ;;' \
  'esac' >"$fake_bin/install_name_tool"

# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf "codesign %s\n" "$*" >>"$TOOL_LOG"' >"$fake_bin/codesign"
chmod +x "$fake_bin/git" "$fake_bin/cmake" \
  "$fake_bin/otool" "$fake_bin/install_name_tool" "$fake_bin/codesign"

whisper_output="$tmp/whisper-output"
whisper_vendor="$tmp/whisper-vendor"
sdl_vendor="$tmp_absolute/sdl-vendor"
sdl_build_dir="$sdl_vendor-build-loqui"
sdl_install_prefix="$sdl_vendor-install-loqui"
fake_sdl_install_name="$sdl_install_prefix/lib/libSDL2-2.0.0.dylib"
PATH="$fake_bin:$PATH" \
TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" SDL_INSTALL_NAME="$fake_sdl_install_name" \
EXPECTED_COMMIT="$expected_commit" EXPECTED_SDL_COMMIT="$expected_sdl_commit" \
SDL_BUILD_DIR="$sdl_build_dir" SDL_INSTALL_PREFIX="$sdl_install_prefix" \
LOQUI_HELPERS_OUTPUT_DIR="$whisper_output" \
LOQUI_WHISPER_VENDOR_DIR="$whisper_vendor" LOQUI_WHISPER_CPP_COMMIT="$expected_commit" \
LOQUI_SDL_VENDOR_DIR="$sdl_vendor" LOQUI_SDL_COMMIT="$expected_sdl_commit" \
LOQUI_SKIP_MODEL=1 "$fixture/scripts/build-whisper-stt.sh"

dirty_output="$tmp/dirty-sdl.log"
if PATH="$fake_bin:$PATH" \
  TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" SDL_INSTALL_NAME="$fake_sdl_install_name" \
  EXPECTED_COMMIT="$expected_commit" EXPECTED_SDL_COMMIT="$expected_sdl_commit" \
  SDL_BUILD_DIR="$sdl_build_dir" SDL_INSTALL_PREFIX="$sdl_install_prefix" SDL_DIRTY=worktree \
  LOQUI_HELPERS_OUTPUT_DIR="$whisper_output" \
  LOQUI_WHISPER_VENDOR_DIR="$whisper_vendor" LOQUI_WHISPER_CPP_COMMIT="$expected_commit" \
  LOQUI_SDL_VENDOR_DIR="$sdl_vendor" LOQUI_SDL_COMMIT="$expected_sdl_commit" \
  LOQUI_SKIP_MODEL=1 "$fixture/scripts/build-whisper-stt.sh" >"$dirty_output" 2>&1; then
  fail "build accepted tracked SDL source changes"
fi
assert_contains "$dirty_output" "tracked"

dirty_index_output="$tmp/dirty-sdl-index.log"
if PATH="$fake_bin:$PATH" \
  TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" SDL_INSTALL_NAME="$fake_sdl_install_name" \
  EXPECTED_COMMIT="$expected_commit" EXPECTED_SDL_COMMIT="$expected_sdl_commit" \
  SDL_BUILD_DIR="$sdl_build_dir" SDL_INSTALL_PREFIX="$sdl_install_prefix" SDL_DIRTY=index \
  LOQUI_HELPERS_OUTPUT_DIR="$whisper_output" \
  LOQUI_WHISPER_VENDOR_DIR="$whisper_vendor" LOQUI_WHISPER_CPP_COMMIT="$expected_commit" \
  LOQUI_SDL_VENDOR_DIR="$sdl_vendor" LOQUI_SDL_COMMIT="$expected_sdl_commit" \
  LOQUI_SKIP_MODEL=1 "$fixture/scripts/build-whisper-stt.sh" >"$dirty_index_output" 2>&1; then
  fail "build accepted staged SDL source changes"
fi
assert_contains "$dirty_index_output" "tracked"

dirty_untracked_output="$tmp/dirty-sdl-untracked.log"
if PATH="$fake_bin:$PATH" \
  TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" SDL_INSTALL_NAME="$fake_sdl_install_name" \
  EXPECTED_COMMIT="$expected_commit" EXPECTED_SDL_COMMIT="$expected_sdl_commit" \
  SDL_BUILD_DIR="$sdl_build_dir" SDL_INSTALL_PREFIX="$sdl_install_prefix" SDL_DIRTY=untracked \
  LOQUI_HELPERS_OUTPUT_DIR="$whisper_output" \
  LOQUI_WHISPER_VENDOR_DIR="$whisper_vendor" LOQUI_WHISPER_CPP_COMMIT="$expected_commit" \
  LOQUI_SDL_VENDOR_DIR="$sdl_vendor" LOQUI_SDL_COMMIT="$expected_sdl_commit" \
  LOQUI_SKIP_MODEL=1 "$fixture/scripts/build-whisper-stt.sh" >"$dirty_untracked_output" 2>&1; then
  fail "build accepted an untracked SDL source file"
fi
assert_contains "$dirty_untracked_output" "local-audio.m"

# Rebuilding into the same development output must handle the read-only staged SDL mode.
PATH="$fake_bin:$PATH" \
TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" SDL_INSTALL_NAME="$fake_sdl_install_name" \
EXPECTED_COMMIT="$expected_commit" EXPECTED_SDL_COMMIT="$expected_sdl_commit" \
SDL_BUILD_DIR="$sdl_build_dir" SDL_INSTALL_PREFIX="$sdl_install_prefix" \
LOQUI_HELPERS_OUTPUT_DIR="$whisper_output" \
LOQUI_WHISPER_VENDOR_DIR="$whisper_vendor" LOQUI_WHISPER_CPP_COMMIT="$expected_commit" \
LOQUI_SDL_VENDOR_DIR="$sdl_vendor" LOQUI_SDL_COMMIT="$expected_sdl_commit" \
LOQUI_SKIP_MODEL=1 "$fixture/scripts/build-whisper-stt.sh"

relative_output="$tmp_absolute/relative-output"
relative_whisper_vendor="$tmp_absolute/relative-whisper-vendor"
relative_sdl_vendor="$tmp_absolute/relative-sdl-vendor"
relative_sdl_build_dir="$relative_sdl_vendor-build-loqui"
relative_sdl_install_prefix="$relative_sdl_vendor-install-loqui"
for _ in first second; do
  (
    cd "$tmp"
    PATH="$fake_bin:$PATH" \
    TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" SDL_SOURCE_DIR="$relative_sdl_vendor" \
    SDL_INSTALL_NAME="$relative_sdl_install_prefix/lib/libSDL2-2.0.0.dylib" \
    EXPECTED_COMMIT="$expected_commit" EXPECTED_SDL_COMMIT="$expected_sdl_commit" \
    SDL_BUILD_DIR="$relative_sdl_build_dir" SDL_INSTALL_PREFIX="$relative_sdl_install_prefix" \
    LOQUI_HELPERS_OUTPUT_DIR="$relative_output" \
    LOQUI_WHISPER_VENDOR_DIR=relative-whisper-vendor LOQUI_WHISPER_CPP_COMMIT="$expected_commit" \
    LOQUI_SDL_VENDOR_DIR=relative-sdl-vendor/ LOQUI_SDL_COMMIT="$expected_sdl_commit" \
    LOQUI_SKIP_MODEL=1 "$fixture/scripts/build-whisper-stt.sh"
  )
done
relative_sdl_status="$(
  PATH="$fake_bin:$PATH" TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" \
    SDL_SOURCE_DIR="$relative_sdl_vendor" "$fake_bin/git" -C "$relative_sdl_vendor" \
    status --porcelain --untracked-files=all
)"
[ -z "$relative_sdl_status" ] || fail "trailing-slash SDL checkout is dirty: $relative_sdl_status"

for unsafe_sdl_vendor in . .. /; do
  unsafe_output="$tmp/unsafe-sdl-$(printf '%s' "$unsafe_sdl_vendor" | tr '/.' '_').log"
  if (
    cd "$tmp"
    PATH="$fake_bin:$PATH" TOOL_LOG="$tool_log" TOOL_STATE="$state_dir" \
      LOQUI_SDL_VENDOR_DIR="$unsafe_sdl_vendor" LOQUI_SKIP_MODEL=1 \
      "$fixture/scripts/build-whisper-stt.sh"
  ) >"$unsafe_output" 2>&1; then
    fail "build accepted unsafe SDL vendor path: $unsafe_sdl_vendor"
  fi
  assert_contains "$unsafe_output" "unsafe SDL source path"
done

assert_file "$whisper_output/whisper-stt"
assert_file "$relative_output/whisper-stt"
assert_dir "$relative_sdl_build_dir"
assert_dir "$relative_sdl_install_prefix"
assert_absent "$relative_sdl_vendor/-build-loqui"
assert_absent "$relative_sdl_vendor/-install-loqui"
assert_file "$whisper_output/libSDL2-2.0.0.dylib"
[ -L "$whisper_output/libwhisper.dylib" ] || fail "whisper dylib symlink was flattened"
assert_absent "$whisper_output/ggml-small.bin"
assert_contains "$tool_log" "$expected_commit"
assert_contains "$tool_log" "$expected_sdl_commit"
assert_contains "$tool_log" "swiftc -target arm64-apple-macos14.0"
assert_contains "$tool_log" "swiftc -target arm64-apple-macos26.0"
assert_contains "$tool_log" "-DCMAKE_OSX_DEPLOYMENT_TARGET=14.0"
assert_contains "$tool_log" "-DCMAKE_OSX_ARCHITECTURES=arm64"
assert_contains "$tool_log" "-DSDL_TEST=OFF"
assert_contains "$tool_log" "-DGGML_BLAS=ON"
assert_contains "$tool_log" "cmake -S $whisper_vendor -B $whisper_vendor/build-loqui"
assert_contains "$tool_log" "cmake -S $relative_whisper_vendor -B $relative_whisper_vendor/build-loqui"
assert_contains "$tool_log" "-DSDL2_DIR=$relative_sdl_install_prefix/lib/cmake/SDL2"
assert_contains "$tool_log" "cmake -S $sdl_vendor -B $sdl_build_dir"
assert_contains "$tool_log" "cmake -S $relative_sdl_vendor -B $relative_sdl_build_dir"
assert_contains "$tool_log" "status --porcelain --untracked-files=all"
assert_not_contains "$tool_log" "cmake -S $sdl_vendor -B $sdl_vendor/build-loqui"
assert_contains "$tool_log" "-Wl,-headerpad_max_install_names"
assert_contains "$tool_log" "remote add origin https://github.com/libsdl-org/SDL.git"
assert_contains "$tool_log" "fetch --depth 1 origin $expected_sdl_commit"
assert_not_contains "$tool_log" "release-2.32.10"
assert_contains "$tool_log" "install_name_tool -change $fake_sdl_install_name @rpath/libSDL2-2.0.0.dylib"
assert_contains "$tool_log" "codesign --verify --strict"

echo "build-component-helpers-test: PASS"
