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
WAILS_VERSION="v3.0.0-alpha2.119"

installed_version=""
if command -v wails3 >/dev/null 2>&1; then
  installed_version="$(wails3 version 2>&1 || true)"
  installed_version="${installed_version%$'\r'}"
fi
if [ "$installed_version" != "$WAILS_VERSION" ]; then
  echo "task.sh: installing pinned wails3 $WAILS_VERSION (found: ${installed_version:-missing})" >&2
  go install github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}
  installed_version="$(wails3 version 2>&1 || true)"
  installed_version="${installed_version%$'\r'}"
  [ "$installed_version" = "$WAILS_VERSION" ] || {
    echo "task.sh: wails3 version check failed; raw output: $installed_version" >&2
    exit 1
  }
fi

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec wails3 task "$@"
