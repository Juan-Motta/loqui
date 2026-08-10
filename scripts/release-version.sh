#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
output_mode=version

usage() {
  echo "release-version: usage: $0 [--root REPO_ROOT] [--dmg-name]" >&2
  exit 2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --root)
      [ "$#" -ge 2 ] || usage
      root="$2"
      shift 2
      ;;
    --dmg-name)
      output_mode=dmg-name
      shift
      ;;
    *) usage ;;
  esac
done

case "$root" in
  /*) ;;
  *) echo "release-version: root must be absolute" >&2; exit 2 ;;
esac

config="$root/build/config.yml"
[ -f "$config" ] || { echo "release-version: missing $config" >&2; exit 1; }

if ! version="$(awk '
  $0 == "info:" { in_info=1; next }
  in_info && /^[^[:space:]]/ { in_info=0 }
  in_info && /^  version:/ {
    count++
    if ($0 !~ /^  version: "(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"[[:space:]]*$/) bad=1
    value=$0
    sub(/^  version: "/, "", value)
    sub(/"[[:space:]]*$/, "", value)
  }
  END {
    if (count != 1 || bad || value == "") exit 1
    print value
  }
' "$config")"; then
  echo "release-version: info.version must appear once as quoted MAJOR.MINOR.PATCH" >&2
  exit 1
fi

if [ "$output_mode" = dmg-name ]; then
  printf 'Loqui-%s-macos-arm64.dmg\n' "$version"
else
  printf '%s\n' "$version"
fi
