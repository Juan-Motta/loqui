#!/usr/bin/env bash
# Run any `go` command with the cgo flags the Azure Speech SDK needs.
#
#   ./scripts/go.sh run ./cmd/stt-probe -mic-only
#   ./scripts/go.sh test ./...
#   ./scripts/go.sh build ./...
#
# WHY THIS WRAPPER HAS TO EXIST. The Go binding for the Speech SDK declares no `#cgo`
# directives of its own, so the header path and link flags can only come from CGO_CFLAGS /
# CGO_LDFLAGS in the environment. Those are per-PROCESS, not per-module: Go has no file where
# a project can record them, and the package that needs them is the SDK's, not ours — so
# putting `#cgo` lines in our own code would not help.
#
# Without them, any plain `go run` / `go test` that transitively reaches the SDK dies with
# `fatal error: 'speechapi_c_error.h' file not found`. That includes commands that do not
# touch Azure at all, like `stt-probe -mic-only`, because the binary still links it.
#
# The paths must be ABSOLUTE: cgo runs the C compiler with the *package's* directory as the
# working directory, so a relative -I would resolve inside the module cache.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRAMEWORK="$ROOT/third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework"

# Fetch the SDK on first use so a fresh clone does not need to know about it.
if [ ! -d "$FRAMEWORK" ]; then
  "$ROOT/scripts/vendor-speech-sdk.sh"
fi

export CGO_ENABLED=1
export CGO_CFLAGS="-I$FRAMEWORK/Headers"
# Two rpaths: the bundle layout for a packaged app, and the checkout for a bare `go run`.
export CGO_LDFLAGS="-F$ROOT/third_party/speech-sdk -framework MicrosoftCognitiveServicesSpeech -Wl,-rpath,@executable_path/../Frameworks -Wl,-rpath,$ROOT/third_party/speech-sdk"

# Sourcing it (`. scripts/go.sh`) just exports the variables; running it executes go.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  exec go "$@"
fi
