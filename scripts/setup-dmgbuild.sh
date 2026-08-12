#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
requirements="$repo_root/build/darwin/dmg/requirements.txt"
python3_bin="${LOQUI_PYTHON3:-python3}"
tools_root="${LOQUI_DMGBUILD_TOOLS_ROOT:-$repo_root/.task/tools}"

die() { echo "setup-dmgbuild: $*" >&2; exit 1; }
[ -f "$requirements" ] && [ ! -L "$requirements" ] || die "missing regular requirements lock"
case "$tools_root" in /*) ;; *) die "tools root must be absolute" ;; esac
command -v "$python3_bin" >/dev/null 2>&1 || die "missing python3"
"$python3_bin" -c \
  'import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)' \
  || die "Python 3.10 or newer is required"

mkdir -p "$tools_root"
tools_root_physical="$(cd "$tools_root" && pwd -P)" || die "cannot resolve tools root"
if [ -z "${LOQUI_DMGBUILD_TOOLS_ROOT:-}" ] &&
   [ "$tools_root_physical" != "$repo_root/.task/tools" ]; then
  die "default tools root resolves outside repository"
fi

lock_digest="$(shasum -a 256 "$requirements" | awk '{print $1}')"
[ "${#lock_digest}" -eq 64 ] || die "could not compute requirements digest"
case "$lock_digest" in
  *[!0-9a-f]*|'') die "could not compute requirements digest" ;;
esac
venv="$tools_root_physical/dmgbuild-$lock_digest"

verify_version() {
  candidate_python="$1"
  [ -x "$candidate_python" ] || return 1
  actual="$("$candidate_python" -c \
    'import importlib.metadata; print(importlib.metadata.version("dmgbuild"))' 2>/dev/null)" \
    || return 1
  [ "$actual" = 1.6.7 ]
}

if [ -e "$venv" ]; then
  verify_version "$venv/bin/python" \
    || die "installed dmgbuild version is not 1.6.7; remove '$venv' and re-run"
  printf '%s\n' "$venv/bin/python"
  exit 0
fi

candidate="$(mktemp -d "$tools_root_physical/.dmgbuild-$lock_digest.XXXXXX")" \
  || die "could not allocate virtual environment"
cleanup() {
  if [ -n "${candidate:-}" ]; then
    rm -rf "$candidate"
  fi
  return 0
}
trap cleanup EXIT

"$python3_bin" -m venv "$candidate" || die "could not create virtual environment"
"$candidate/bin/python" -m pip install \
  --disable-pip-version-check \
  --isolated \
  --no-cache-dir \
  --require-hashes \
  --only-binary=:all: \
  -r "$requirements" >&2 || die "could not install locked dmgbuild dependencies"
verify_version "$candidate/bin/python" \
  || die "installed dmgbuild version is not 1.6.7"

if ! "$python3_bin" -c \
    'import os, sys; os.rename(sys.argv[1], sys.argv[2])' "$candidate" "$venv" \
    >/dev/null; then
  [ -d "$venv" ] && verify_version "$venv/bin/python" \
    || die "could not publish virtual environment"
  rm -rf "$candidate"
fi
candidate=""
printf '%s\n' "$venv/bin/python"
