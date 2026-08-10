#!/usr/bin/env bash
# Restore Loqui-owned macOS plist values after Wails build-asset generation.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root="$script_root"
mode="write"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --check)
      mode="check"
      shift
      ;;
    --root)
      [ "$#" -ge 2 ] || { echo "patch-plists: --root requires a path" >&2; exit 2; }
      repo_root="$2"
      shift 2
      ;;
    *)
      echo "usage: scripts/patch-plists.sh [--check] [--root PATH]" >&2
      exit 2
      ;;
  esac
done

repo_root="$(cd "$repo_root" && pwd)"
cd "$repo_root"

production_id="com.jualopezmo.loquigo"
development_id="com.jualopezmo.loquigo.dev"
minimum_system_version="14.0.0"
microphone="Loqui usa el micrófono para transcribir tu dictado."
speech="Loqui usa reconocimiento de voz en el dispositivo para transcribir tu dictado."
events="Loqui usa System Events para pegar el texto transcrito en la app que estás usando."

version="$(awk '/^info:/{in_info=1; next} in_info && /^  version:/{gsub(/["'\'' ]/, "", $2); print $2; exit}' build/config.yml)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "patch-plists: invalid info.version: $version" >&2
  exit 1
}

production_plist="build/darwin/Info.plist"
development_plist="build/darwin/Info.dev.plist"
for plist in "$production_plist" "$development_plist"; do
  [ -f "$plist" ] || {
    echo "patch-plists: missing $plist — run 'wails3 task common:update:build-assets'" >&2
    exit 1
  }
done

set_string() {
  /usr/libexec/PlistBuddy -c "Delete :$2" "$1" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :$2 string $3" "$1" >/dev/null
}

set_bool() {
  /usr/libexec/PlistBuddy -c "Delete :$2" "$1" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :$2 bool $3" "$1" >/dev/null
}

check_failed=0
check_value() {
  actual="$(plutil -extract "$2" raw "$1" 2>/dev/null || true)"
  if [ "$actual" != "$3" ]; then
    echo "patch-plists: $1 $2 = '$actual', want '$3'" >&2
    check_failed=1
  fi
}

if [ "$mode" = "check" ]; then
  check_value "$production_plist" CFBundleIdentifier "$production_id"
  check_value "$development_plist" CFBundleIdentifier "$development_id"
  for plist in "$production_plist" "$development_plist"; do
    check_value "$plist" CFBundleShortVersionString "$version"
    check_value "$plist" CFBundleVersion "$version"
    check_value "$plist" LSMinimumSystemVersion "$minimum_system_version"
    check_value "$plist" NSMicrophoneUsageDescription "$microphone"
    check_value "$plist" NSSpeechRecognitionUsageDescription "$speech"
    check_value "$plist" NSAppleEventsUsageDescription "$events"
    check_value "$plist" LSUIElement false
  done
  [ "$check_failed" -eq 0 ] || exit 1
  echo "patch-plists: check ok"
  exit 0
fi

for plist in "$production_plist" "$development_plist"; do
  set_string "$plist" CFBundleShortVersionString "$version"
  set_string "$plist" CFBundleVersion "$version"
  set_string "$plist" LSMinimumSystemVersion "$minimum_system_version"
  set_string "$plist" NSMicrophoneUsageDescription "$microphone"
  set_string "$plist" NSSpeechRecognitionUsageDescription "$speech"
  set_string "$plist" NSAppleEventsUsageDescription "$events"
  set_bool "$plist" LSUIElement false
done
set_string "$production_plist" CFBundleIdentifier "$production_id"
set_string "$development_plist" CFBundleIdentifier "$development_id"

echo "patch-plists: ok $production_plist"
echo "patch-plists: ok $development_plist"
