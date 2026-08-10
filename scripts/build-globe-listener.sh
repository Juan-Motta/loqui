#!/usr/bin/env bash
# Compile the vendored macOS Globe/fn listener into a standalone binary.
# Requires the Xcode command line tools (swiftc). macOS only.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/helpers/macos-globe-listener.swift"
OUTPUT_DIR="${LOQUI_HELPERS_OUTPUT_DIR:-$ROOT/helpers/bin}"
OUT="$OUTPUT_DIR/globe-listener"
DEPLOYMENT_TARGET="14.0"

if [[ "$(uname)" != "Darwin" ]]; then
  echo "build-globe-listener: skipped (not macOS)" >&2
  exit 0
fi

if ! command -v swiftc >/dev/null 2>&1; then
  echo "build-globe-listener: swiftc not found — install Xcode CLT (xcode-select --install)" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"
echo "build-globe-listener: compiling $SRC -> $OUT"
swiftc -target "arm64-apple-macos${DEPLOYMENT_TARGET}" -O -o "$OUT" "$SRC"
echo "build-globe-listener: done ($(file "$OUT" | sed 's/^.*: //'))"
