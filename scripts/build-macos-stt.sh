#!/usr/bin/env bash
# Compile the native macOS STT helper (Apple SpeechAnalyzer, macOS 26+).
# Output: resources/native/macos-stt (gitignored; built locally / at packaging).
set -euo pipefail
cd "$(dirname "$0")/.."
swiftc -O resources/native/macos-stt.swift -o resources/native/macos-stt
echo "built resources/native/macos-stt"
