#!/usr/bin/env bash
# Build the whisper.cpp STT helper (loqui "whisper" provider). Clones whisper.cpp
# into a gitignored vendor dir, injects helpers/whisper-stt.cpp as a CMake
# target (so linking against libwhisper/ggml/SDL is handled by whisper.cpp's build),
# builds it, and drops the binary + a multilingual model into helpers/bin/.
# Requires: git, cmake, sdl2 (installed via brew if missing). macOS/Linux.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
VENDOR="$ROOT/scripts/whisper-vendor/whisper.cpp"

command -v cmake       >/dev/null 2>&1 || { echo ">> installing cmake…"; brew install cmake; }
command -v sdl2-config >/dev/null 2>&1 || { echo ">> installing sdl2…";  brew install sdl2; }

if [ ! -d "$VENDOR" ]; then
  echo ">> cloning whisper.cpp…"
  git clone --depth 1 https://github.com/ggml-org/whisper.cpp.git "$VENDOR"
fi

echo ">> injecting whisper-stt target…"
mkdir -p "$VENDOR/examples/whisper-stt"
cp "$ROOT/helpers/whisper-stt.cpp" "$VENDOR/examples/whisper-stt/whisper-stt.cpp"
# The target file is shared with build-whisper-stt.ps1 so the two cannot drift.
cp "$ROOT/scripts/whisper-stt.CMakeLists.txt" "$VENDOR/examples/whisper-stt/CMakeLists.txt"
grep -q "add_subdirectory(whisper-stt)" "$VENDOR/examples/CMakeLists.txt" \
  || echo "add_subdirectory(whisper-stt)" >> "$VENDOR/examples/CMakeLists.txt"

# GGML_NATIVE=OFF by default, mirroring build-whisper-stt.ps1. ggml defaults it
# ON, which optimizes for the CPU OF THE BUILD MACHINE — fine while that was
# always the Mac that would run it, wrong now that CI builds the DMG. On Apple
# silicon the hazard is a newer chip's instructions (i8mm, SME) in a binary an M1
# then cannot execute: the same silent "Illegal instruction" already observed on
# Windows with AVX-512. Pass --native for a build that never leaves this machine.
NATIVE=OFF
[ "${1:-}" = "--native" ] && NATIVE=ON

echo ">> building (GGML_NATIVE=$NATIVE; a couple of minutes the first time)…"
cmake -S "$VENDOR" -B "$VENDOR/build" -DWHISPER_SDL2=ON -DGGML_NATIVE="$NATIVE" \
  -DCMAKE_BUILD_TYPE=Release >/dev/null
cmake --build "$VENDOR/build" -j --target whisper-stt

mkdir -p "$ROOT/helpers/bin"
cp "$VENDOR/build/bin/whisper-stt" "$ROOT/helpers/bin/whisper-stt"

# MAKE THE HELPER RELOCATABLE. whisper.cpp links libwhisper/libggml* dynamically and CMake
# bakes an ABSOLUTE rpath pointing at the build directory. Copy the binary alone and it keeps
# working on the machine that built it — and only there. Inside a distributed .app it cannot
# find its libraries at all, which is a failure invisible until someone else opens the DMG.
#
# (The Electron build has exactly this bug: its whisper-stt carries an rpath into
# scripts/whisper-vendor/... and ships no dylibs beside it.)
#
# So: bring the libraries along and point the rpath at wherever the binary happens to live.
echo ">> making the helper relocatable (dylibs + @loader_path)"
cp "$VENDOR"/build/bin/*.dylib "$ROOT/helpers/bin/" 2>/dev/null || true
# Drop every baked-in rpath, then add the only one that travels with the bundle.
while read -r old_rpath; do
  [ -n "$old_rpath" ] && install_name_tool -delete_rpath "$old_rpath" "$ROOT/helpers/bin/whisper-stt" 2>/dev/null || true
done < <(otool -l "$ROOT/helpers/bin/whisper-stt" | awk '/LC_RPATH/{f=1} f&&/path /{print $2; f=0}')
install_name_tool -add_rpath "@loader_path" "$ROOT/helpers/bin/whisper-stt"
# The ad-hoc signature is invalidated by editing the load commands; re-sign or macOS kills it.
codesign --force --sign - "$ROOT/helpers/bin/whisper-stt" 2>/dev/null || true

echo ">> built helpers/bin/whisper-stt"

# Multilingual model, placed next to the binary (main passes this path).
# The app downloads the model at runtime now (465 MB is not shipped in the DMG),
# so CI must not spend minutes and gigabytes fetching it just to build the binary.
if [ -n "${LOQUI_SKIP_MODEL:-}" ]; then
  echo ">> LOQUI_SKIP_MODEL set — skipping the model download"
  echo "DONE — helpers/whisper-stt ready (no model)."
  exit 0
fi

MODEL="$ROOT/helpers/bin/ggml-small.bin"
SPIKE_MODEL="$ROOT/scripts/spikes/whisper/whisper.cpp/models/ggml-small.bin"
if [ ! -f "$MODEL" ]; then
  if [ -f "$SPIKE_MODEL" ]; then
    echo ">> reusing model from the whisper spike…"; cp "$SPIKE_MODEL" "$MODEL"
  else
    echo ">> downloading model ggml-small (~466 MB)…"
    bash "$VENDOR/models/download-ggml-model.sh" small "$ROOT/resources/native"
  fi
fi

echo "DONE — helpers/whisper-stt + ggml-small.bin ready."
