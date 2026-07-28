#!/usr/bin/env bash
# Run a project task without needing `wails3` on your PATH.
#
#   ./scripts/task.sh build
#   ./scripts/task.sh probe:mic
#   ./scripts/task.sh test
#
# WHY: `wails3` installs into $(go env GOPATH)/bin, which is NOT on the PATH by default on
# macOS — nothing adds it unless you did. So `wails3 task ...` fails with "command not found"
# on a machine that has everything correctly installed.
#
# Putting GOPATH/bin on PATH here is not just for the outer call: the tasks themselves invoke
# `wails3` (generate bindings, update build-assets), so a child process that cannot find it
# fails halfway through a build rather than up front.
#
# Adding this to your shell profile makes `wails3` work everywhere and this wrapper
# unnecessary:
#
#   echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
set -euo pipefail

GOBIN="$(go env GOPATH)/bin"
export PATH="$PATH:$GOBIN"

if ! command -v wails3 >/dev/null 2>&1; then
  echo "task.sh: wails3 not found — installing it (one-off, a couple of minutes)" >&2
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
fi

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec wails3 task "$@"
