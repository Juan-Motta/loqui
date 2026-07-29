#!/usr/bin/env bash
# Captures the overlay pill at native resolution for diagnosing how it actually renders.
#
# WHY A SCRIPT. A pill that looks wrong cannot be diagnosed from a cropped or rescaled screenshot:
# resampling and compression both invent edges that are not on screen. This takes the whole screen
# untouched (-x no sound, -o no window shadow added by the screenshot system) so the pixels are the
# ones the compositor produced.
set -euo pipefail

out="${1:-/tmp/loqui-overlay.png}"
app="./bin/loqui.app/Contents/MacOS/loqui"

[ -x "$app" ] || { echo "build it first: ./scripts/task.sh package"; exit 1; }

pkill -x loqui 2>/dev/null || true
sleep 1

LOQUI_DEBUG_OVERLAY=1 "$app" > /tmp/loqui-overlay-run.log 2>&1 &
sleep 6
screencapture -x -o "$out"
sleep 1
pkill -x loqui 2>/dev/null || true

echo "captura: $out"
grep -E "OVERLAY-GEO|opaque" /tmp/loqui-overlay-run.log || true
