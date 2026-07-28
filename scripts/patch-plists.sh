#!/usr/bin/env bash
# Inject the macOS keys Loqui cannot run without into the generated Info.plist files.
#
# WHY THIS IS A SCRIPT AND NOT A HAND EDIT: `wails3 task common:update:build-assets`
# regenerates build/darwin/Info*.plist from build/config.yml and overwrites anything
# added by hand. config.yml has no field for usage strings, so the keys have to be
# re-applied after every regeneration — and the failure mode of forgetting is brutal:
#
#   - A MISSING usage string does not produce a prompt the user can deny. macOS
#     TERMINATES the process the moment it touches the microphone. The app just
#     vanishes, with nothing in the log.
#   - LSUIElement decides whether the app owns a Dock icon. Loqui is a menu-bar app
#     that also has a real settings window, so it stays false, exactly as the
#     Electron build does (electron-builder.yml extendInfo).
#
# Idempotent: run it as often as you like. Called by build/darwin/Taskfile.yml
# before the .app bundle is assembled, so a fresh clone cannot get this wrong.
set -euo pipefail

cd "$(dirname "$0")/.."

MIC="Loqui usa el micrófono para transcribir tu dictado."
SPEECH="Loqui usa reconocimiento de voz en el dispositivo para transcribir tu dictado."
EVENTS="Loqui usa System Events para pegar el texto transcrito en la app que estás usando."

set_string() { # file key value
  /usr/libexec/PlistBuddy -c "Delete :$2" "$1" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :$2 string $3" "$1" >/dev/null
}
set_bool() {
  /usr/libexec/PlistBuddy -c "Delete :$2" "$1" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :$2 bool $3" "$1" >/dev/null
}

for plist in build/darwin/Info.plist build/darwin/Info.dev.plist; do
  [ -f "$plist" ] || { echo "patch-plists: missing $plist — run 'wails3 task common:update:build-assets'" >&2; exit 1; }
  set_string "$plist" NSMicrophoneUsageDescription "$MIC"
  set_string "$plist" NSSpeechRecognitionUsageDescription "$SPEECH"
  set_string "$plist" NSAppleEventsUsageDescription "$EVENTS"
  set_bool "$plist" LSUIElement false
  # The Apple SpeechAnalyzer helper needs macOS 26; the app itself must still install
  # and run on older systems with the other engines, so the floor stays at 12.0 and
  # engine availability is decided at runtime (see connectionStatus / hostCapabilities).
  echo "patch-plists: ok $plist"
done
