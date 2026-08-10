#!/usr/bin/env bash
# Build a pinned, relocatable whisper.cpp helper and its native libraries on macOS.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
die() { echo "build-whisper-stt: $*" >&2; exit 1; }

absolute_path() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *) printf '%s/%s\n' "$PWD" "$1" ;;
  esac
}

normalize_sdl_source_path() {
  local candidate leaf parent
  candidate="$(absolute_path "$1")"
  while [ "$candidate" != / ] && [ "${candidate%/}" != "$candidate" ]; do
    candidate="${candidate%/}"
  done
  leaf="${candidate##*/}"
  case "$leaf" in
    ''|.|..) die "unsafe SDL source path: $1" ;;
  esac
  parent="${candidate%/*}"
  [ -n "$parent" ] || parent=/
  [ -d "$parent" ] || die "SDL source parent does not exist: $parent"
  parent="$(cd "$parent" && pwd -L)"
  case "$parent" in
    /) printf '/%s\n' "$leaf" ;;
    *) printf '%s/%s\n' "$parent" "$leaf" ;;
  esac
}

output_dir="$(absolute_path "${LOQUI_HELPERS_OUTPUT_DIR:-$root/helpers/bin}")"
pinned_commit="97c56f1dc1d1100a9d859c865a20c82d22f823ed"
whisper_commit="${LOQUI_WHISPER_CPP_COMMIT:-$pinned_commit}"
vendor="$(absolute_path "${LOQUI_WHISPER_VENDOR_DIR:-$root/scripts/whisper-vendor/whisper.cpp}")"
whisper_build_dir="$(absolute_path "${LOQUI_WHISPER_BUILD_DIR:-$vendor/build-loqui}")"
deployment_target="14.0"
sdl_pinned_commit="5d249570393f7a37e037abf22cd6012a4cc56a71"
sdl_commit="${LOQUI_SDL_COMMIT:-$sdl_pinned_commit}"
sdl_vendor="$(normalize_sdl_source_path "${LOQUI_SDL_VENDOR_DIR:-$root/scripts/whisper-vendor/SDL-${sdl_pinned_commit:0:12}}")"
sdl_build_dir="${sdl_vendor}-build-loqui"
sdl_install_prefix="${sdl_vendor}-install-loqui"
sdl_dylib="$sdl_install_prefix/lib/libSDL2-2.0.0.dylib"

require_command() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

[ "$whisper_commit" = "$pinned_commit" ] || die "unsupported whisper.cpp commit: $whisper_commit"
[ "$sdl_commit" = "$sdl_pinned_commit" ] || die "unsupported SDL commit: $sdl_commit"
for command_name in git cmake otool install_name_tool codesign; do
  require_command "$command_name"
done

official_sdl_origin="https://github.com/libsdl-org/SDL.git"
if [ ! -d "$sdl_vendor/.git" ]; then
  [ ! -e "$sdl_vendor" ] || die "SDL vendor exists but is not a Git checkout: $sdl_vendor"
  echo ">> initializing pinned SDL source…"
  mkdir -p "$sdl_vendor"
  git -C "$sdl_vendor" init
  git -C "$sdl_vendor" remote add origin "$official_sdl_origin"
fi
actual_sdl_origin="$(git -C "$sdl_vendor" remote get-url origin)"
[ "$actual_sdl_origin" = "$official_sdl_origin" ] \
  || die "SDL origin is $actual_sdl_origin, expected $official_sdl_origin"
if ! git -C "$sdl_vendor" cat-file -e "$sdl_commit^{commit}" 2>/dev/null; then
  git -C "$sdl_vendor" fetch --depth 1 origin "$sdl_commit"
fi
git -C "$sdl_vendor" checkout --detach "$sdl_commit"
actual_sdl_commit="$(git -C "$sdl_vendor" rev-parse HEAD)"
[ "$actual_sdl_commit" = "$sdl_commit" ] \
  || die "SDL HEAD is $actual_sdl_commit, expected $sdl_commit"
sdl_source_changes="$(git -C "$sdl_vendor" status --porcelain --untracked-files=all)"
[ -z "$sdl_source_changes" ] \
  || die "SDL source has tracked, index, or untracked changes: $sdl_source_changes"

echo ">> building pinned SDL (macOS ${deployment_target}, arm64)…"
cmake -S "$sdl_vendor" -B "$sdl_build_dir" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DCMAKE_OSX_DEPLOYMENT_TARGET="$deployment_target" \
  -DCMAKE_INSTALL_PREFIX="$sdl_install_prefix" \
  -DCMAKE_INSTALL_LIBDIR=lib \
  -DSDL_SHARED=ON \
  -DSDL_STATIC=OFF \
  -DSDL_TEST=OFF \
  -DSDL_TESTS=OFF \
  >/dev/null
[ ! -e "$sdl_dylib" ] || chmod u+w "$sdl_dylib"
cmake --build "$sdl_build_dir" -j --target install
[ -f "$sdl_dylib" ] || die "SDL build did not produce $sdl_dylib"

if [ ! -d "$vendor" ]; then
  echo ">> cloning pinned whisper.cpp source…"
  git clone --no-checkout https://github.com/ggml-org/whisper.cpp.git "$vendor"
fi
if ! git -C "$vendor" cat-file -e "$whisper_commit^{commit}" 2>/dev/null; then
  git -C "$vendor" fetch --depth 1 origin "$whisper_commit"
fi
git -C "$vendor" checkout --detach "$whisper_commit"
actual_commit="$(git -C "$vendor" rev-parse HEAD)"
[ "$actual_commit" = "$whisper_commit" ] || die "whisper.cpp HEAD is $actual_commit, expected $whisper_commit"

echo ">> injecting whisper-stt target…"
mkdir -p "$vendor/examples/whisper-stt"
cp "$root/helpers/whisper-stt.cpp" "$vendor/examples/whisper-stt/whisper-stt.cpp"
cp "$root/scripts/whisper-stt.CMakeLists.txt" "$vendor/examples/whisper-stt/CMakeLists.txt"
grep -q "add_subdirectory(whisper-stt)" "$vendor/examples/CMakeLists.txt" \
  || echo "add_subdirectory(whisper-stt)" >>"$vendor/examples/CMakeLists.txt"

native=OFF
[ "${1:-}" = "--native" ] && native=ON
echo ">> building pinned whisper.cpp (GGML_NATIVE=$native)…"
cmake -S "$vendor" -B "$whisper_build_dir" \
  -DWHISPER_SDL2=ON \
  -DGGML_NATIVE="$native" \
  -DGGML_BLAS=ON \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DCMAKE_OSX_DEPLOYMENT_TARGET="$deployment_target" \
  -DCMAKE_PREFIX_PATH="$sdl_install_prefix" \
  -DSDL2_DIR="$sdl_install_prefix/lib/cmake/SDL2" \
  -DCMAKE_EXE_LINKER_FLAGS=-Wl,-headerpad_max_install_names \
  -DCMAKE_SHARED_LINKER_FLAGS=-Wl,-headerpad_max_install_names >/dev/null
cmake --build "$whisper_build_dir" -j --target whisper-stt

mkdir -p "$output_dir"
cp "$whisper_build_dir/bin/whisper-stt" "$output_dir/whisper-stt"

copied_dylib=0
for source_dylib in "$whisper_build_dir"/bin/*.dylib; do
  [ -e "$source_dylib" ] || [ -L "$source_dylib" ] || continue
  cp -a "$source_dylib" "$output_dir/"
  copied_dylib=1
done
[ "$copied_dylib" -eq 1 ] || die "build produced no Whisper dylibs"
[ ! -e "$output_dir/libSDL2-2.0.0.dylib" ] || chmod u+w "$output_dir/libSDL2-2.0.0.dylib"
cp -L "$sdl_dylib" "$output_dir/libSDL2-2.0.0.dylib"

sdl_load_count=0
while read -r sdl_load_path; do
  case "$sdl_load_path" in
    @rpath/libSDL2-2.0.0.dylib) sdl_load_count=$((sdl_load_count + 1)) ;;
    */libSDL2-2.0.0.dylib)
      install_name_tool -change "$sdl_load_path" '@rpath/libSDL2-2.0.0.dylib' "$output_dir/whisper-stt"
      sdl_load_count=$((sdl_load_count + 1))
      ;;
  esac
done < <(otool -L "$output_dir/whisper-stt" | awk '/^[[:space:]]/{print $1}')
[ "$sdl_load_count" -eq 1 ] || die "expected one SDL load command, found $sdl_load_count"
install_name_tool -id '@rpath/libSDL2-2.0.0.dylib' "$output_dir/libSDL2-2.0.0.dylib"

set_loader_rpath() {
  mach_o="$1"
  while read -r old_rpath; do
    [ -n "$old_rpath" ] || continue
    install_name_tool -delete_rpath "$old_rpath" "$mach_o"
  done < <(otool -l "$mach_o" | awk '/LC_RPATH/{seen=1; next} seen && /path /{print $2; seen=0}')
  install_name_tool -add_rpath '@loader_path' "$mach_o"
}

set_loader_rpath "$output_dir/whisper-stt"
while read -r real_dylib; do
  set_loader_rpath "$real_dylib"
done < <(find "$output_dir" -type f -name '*.dylib' -print | LC_ALL=C sort)

metal_dylib=""
while read -r candidate; do metal_dylib="$candidate"; break; done \
  < <(find "$output_dir" -type f -name 'libggml-metal*.dylib' -print | LC_ALL=C sort)
[ -n "$metal_dylib" ] || die "missing libggml-metal dylib"
otool -l "$metal_dylib" | awk '
  $1 == "segname" && $2 == "__DATA" { in_data=1; next }
  in_data && $1 == "sectname" && $2 == "__ggml_metallib" { found=1 }
  END { exit found ? 0 : 1 }
' || die "libggml-metal does not embed __DATA,__ggml_metallib"

verify_portable() {
  mach_o="$1"
  expected_rpaths="$(otool -l "$mach_o" | awk '/LC_RPATH/{seen=1; next} seen && /path /{print $2; seen=0}')"
  [ "$expected_rpaths" = '@loader_path' ] || die "unexpected rpath in $mach_o: $expected_rpaths"
  while read -r dependency; do
    case "$dependency" in
      /System/*|/usr/lib/*|@rpath/*|@loader_path/*|@executable_path/*) ;;
      *) die "absolute dependency in $mach_o: $dependency" ;;
    esac
  done < <(otool -L "$mach_o" | awk '/^[[:space:]]/{print $1}')
}

verify_portable "$output_dir/whisper-stt"
while read -r real_dylib; do
  dylib_id="$(otool -D "$real_dylib" | sed -n '2p')"
  case "$dylib_id" in @rpath/*) ;; *) die "unexpected dylib ID in $real_dylib: $dylib_id" ;; esac
  verify_portable "$real_dylib"
  codesign --force --sign - "$real_dylib"
  codesign --verify --strict "$real_dylib"
done < <(find "$output_dir" -type f -name '*.dylib' -print | LC_ALL=C sort)
codesign --force --sign - "$output_dir/whisper-stt"
codesign --verify --strict "$output_dir/whisper-stt"

echo ">> built relocatable helper in $output_dir"
if [ -n "${LOQUI_SKIP_MODEL:-}" ]; then
  echo ">> LOQUI_SKIP_MODEL set — skipping the model download"
  exit 0
fi

model="$output_dir/ggml-small.bin"
spike_model="$root/scripts/spikes/whisper/whisper.cpp/models/ggml-small.bin"
if [ ! -f "$model" ]; then
  if [ -f "$spike_model" ]; then
    cp -L "$spike_model" "$model"
  else
    bash "$vendor/models/download-ggml-model.sh" small "$output_dir"
  fi
fi
echo ">> model ready at $model"
