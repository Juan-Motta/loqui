#!/usr/bin/env bash
# Compile the native macOS STT helper (Apple SpeechAnalyzer, macOS 26+).
#
# The binary lands in helpers/bin/, which is gitignored: build output next to the sources
# is how a 10 MB binary ends up committed by accident.
#
# It is fine for this to fail on an older macOS — the app hides the Apple engine at
# runtime when the helper is absent (hostCapabilities), rather than offering an engine it
# cannot run.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${LOQUI_HELPERS_OUTPUT_DIR:-$ROOT/helpers/bin}"
mkdir -p "$OUTPUT_DIR"
swiftc -target arm64-apple-macos26.0 -O "$ROOT/helpers/macos-stt.swift" -o "$OUTPUT_DIR/macos-stt"
echo "built $OUTPUT_DIR/macos-stt"
