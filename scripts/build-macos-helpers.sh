#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${LOQUI_HELPERS_OUTPUT_DIR:-$root/helpers/bin}"
whisper_commit="97c56f1dc1d1100a9d859c865a20c82d22f823ed"
sdl_commit="5d249570393f7a37e037abf22cd6012a4cc56a71"

export LOQUI_HELPERS_OUTPUT_DIR="$output_dir"
export LOQUI_WHISPER_VENDOR_DIR="${LOQUI_WHISPER_VENDOR_DIR:-$root/scripts/whisper-vendor/whisper.cpp-${whisper_commit:0:12}}"
export LOQUI_WHISPER_CPP_COMMIT="$whisper_commit"
whisper_vendor_parent="$(dirname "$LOQUI_WHISPER_VENDOR_DIR")"
export LOQUI_SDL_VENDOR_DIR="${LOQUI_SDL_VENDOR_DIR:-$whisper_vendor_parent/SDL-${sdl_commit:0:12}}"
export LOQUI_SDL_COMMIT="$sdl_commit"
export LOQUI_SKIP_MODEL=1

globe_builder="${GLOBE_BUILD_SCRIPT:-$root/scripts/build-globe-listener.sh}"
macos_stt_builder="${MACOS_STT_BUILD_SCRIPT:-$root/scripts/build-macos-stt.sh}"
whisper_builder="${WHISPER_BUILD_SCRIPT:-$root/scripts/build-whisper-stt.sh}"

mkdir -p "$output_dir"
"$globe_builder"
"$macos_stt_builder"
"$whisper_builder"

die() { echo "build-macos-helpers: $*" >&2; exit 1; }
for helper in globe-listener macos-stt whisper-stt; do
  [ -f "$output_dir/$helper" ] || die "builder did not produce $helper"
done
[ -f "$output_dir/libSDL2-2.0.0.dylib" ] || die "builder did not produce SDL"

for family in libwhisper libggml libggml-base libggml-cpu libggml-blas libggml-metal; do
  real_count=0
  link_count=0
  for candidate in "$output_dir"/"$family"*.dylib; do
    [ -e "$candidate" ] || [ -L "$candidate" ] || continue
    base="$(basename "$candidate")"
    case "$family:$base" in
      libggml:libggml-base*|libggml:libggml-cpu*|libggml:libggml-blas*|libggml:libggml-metal*) continue ;;
    esac
    if [ -L "$candidate" ]; then link_count=$((link_count + 1)); else real_count=$((real_count + 1)); fi
  done
  [ "$real_count" -ge 1 ] && [ "$link_count" -ge 1 ] || die "incomplete dylib family: $family"
done

echo "build-macos-helpers: ok $output_dir"
