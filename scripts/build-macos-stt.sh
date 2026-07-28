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
cd "$(dirname "$0")/.."
mkdir -p helpers/bin
swiftc -O helpers/macos-stt.swift -o helpers/bin/macos-stt
echo "built helpers/bin/macos-stt"
