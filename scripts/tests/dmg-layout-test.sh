#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
. "$repo_root/scripts/tests/testlib.sh"

settings_path="$repo_root/build/darwin/dmg/settings.py"
verifier_path="$repo_root/build/darwin/dmg/verify-ds-store.py"
asset_root="$repo_root/build/darwin/dmg"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dmg-layout-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
tmp="$(cd "$tmp" && pwd -P)"

if [ -n "${LOQUI_DMGBUILD_PYTHON:-}" ]; then
  dmgbuild_python="$LOQUI_DMGBUILD_PYTHON"
else
  dmgbuild_python="$("$repo_root/scripts/setup-dmgbuild.sh")"
fi
[ -x "$dmgbuild_python" ] || fail "dmgbuild Python is not executable: $dmgbuild_python"

fixture_app="$tmp/Loqui.app"
fixture_assets="$tmp/dmg-assets"
mkdir -p "$fixture_app" "$fixture_assets"

settings_probe="$tmp/settings-probe.py"
cat >"$settings_probe" <<'PY'
import json
import sys
import tokenize

settings_path = sys.argv[1]
settings = {
    "defines": {
        "app": sys.argv[2],
        "assets": sys.argv[3],
    }
}
with tokenize.open(settings_path) as settings_file:
    exec(compile(settings_file.read(), settings_path, "exec"), settings, settings)
keys = (
    "format",
    "filesystem",
    "files",
    "hide",
    "hide_extensions",
    "symlinks",
    "background",
    "window_rect",
    "default_view",
    "arrange_by",
    "icon_size",
    "text_size",
    "label_pos",
    "icon_locations",
    "show_status_bar",
    "show_tab_view",
    "show_toolbar",
    "show_pathbar",
    "show_sidebar",
)
print(json.dumps({key: settings[key] for key in keys}, sort_keys=True))
PY

actual_settings="$tmp/actual-settings.json"
"$dmgbuild_python" "$settings_probe" "$settings_path" "$fixture_app" \
  "$fixture_assets" >"$actual_settings"

expected_settings="$tmp/expected-settings.json"
"$dmgbuild_python" - "$fixture_app" "$fixture_assets" >"$expected_settings" <<'PY'
import json
import os
import sys

application = sys.argv[1]
assets = sys.argv[2]
expected = {
    "arrange_by": None,
    "background": os.path.join(assets, "background.png"),
    "default_view": "icon-view",
    "files": [application],
    "filesystem": "HFS+",
    "format": "UDZO",
    "hide": [".background.tiff"],
    "hide_extensions": [],
    "icon_locations": {
        "Applications": [500, 215],
        "Loqui.app": [160, 215],
    },
    "icon_size": 128,
    "label_pos": "bottom",
    "show_pathbar": False,
    "show_sidebar": False,
    "show_status_bar": False,
    "show_tab_view": False,
    "show_toolbar": False,
    "symlinks": {"Applications": "/Applications"},
    "text_size": 14,
    "window_rect": [[100, 100], [660, 384]],
}
print(json.dumps(expected, sort_keys=True))
PY
cmp -s "$actual_settings" "$expected_settings" || {
  diff -u "$expected_settings" "$actual_settings" >&2 || true
  fail "settings JSON does not match the approved layout"
}

settings_failure_probe="$tmp/settings-failure-probe.py"
cat >"$settings_failure_probe" <<'PY'
import json
import sys
import tokenize

settings_path = sys.argv[1]
settings = {"defines": json.loads(sys.argv[2])}
try:
    with tokenize.open(settings_path) as settings_file:
        exec(compile(settings_file.read(), settings_path, "exec"), settings, settings)
except Exception as error:
    print(error, file=sys.stderr)
    raise SystemExit(1)
raise SystemExit(0)
PY

assert_exact_failure() {
  expected="$1"
  shift
  stdout_path="$tmp/failure.stdout"
  stderr_path="$tmp/failure.stderr"
  if "$@" >"$stdout_path" 2>"$stderr_path"; then
    fail "command unexpectedly passed: $*"
  fi
  assert_eq "$(cat "$stderr_path")" "$expected"
  [ ! -s "$stdout_path" ] || fail "failing command wrote to stdout: $*"
}

assert_exact_failure 'settings require -D app=/absolute/path/Loqui.app' \
  "$dmgbuild_python" "$settings_failure_probe" "$settings_path" \
  "{\"assets\": \"$fixture_assets\"}"
assert_exact_failure 'app path must be absolute' \
  "$dmgbuild_python" "$settings_failure_probe" "$settings_path" \
  "{\"app\": \"Loqui.app\", \"assets\": \"$fixture_assets\"}"
assert_exact_failure 'app path must end in Loqui.app' \
  "$dmgbuild_python" "$settings_failure_probe" "$settings_path" \
  "{\"app\": \"$tmp/Other.app\", \"assets\": \"$fixture_assets\"}"
assert_exact_failure 'settings require -D assets=/absolute/path/to/dmg-assets' \
  "$dmgbuild_python" "$settings_failure_probe" "$settings_path" \
  "{\"app\": \"$fixture_app\"}"
assert_exact_failure 'assets path must be absolute' \
  "$dmgbuild_python" "$settings_failure_probe" "$settings_path" \
  "{\"app\": \"$fixture_app\", \"assets\": \"dmg-assets\"}"

for asset in background.png background@2x.png background.sha256; do
  [ -f "$asset_root/$asset" ] || fail "missing regular file: $asset_root/$asset"
  [ ! -L "$asset_root/$asset" ] || fail "asset must not be a symlink: $asset_root/$asset"
done
(
  cd "$asset_root"
  shasum -a 256 -c background.sha256 >/dev/null
)

assert_png_properties() {
  image="$1"
  width="$2"
  height="$3"
  properties="$tmp/$(basename "$image").properties"
  sips -g format -g bitsPerSample -g samplesPerPixel -g hasAlpha \
    -g pixelWidth -g pixelHeight "$image" >"$properties"
  assert_contains "$properties" 'format: png'
  assert_contains "$properties" 'bitsPerSample: 8'
  assert_contains "$properties" 'samplesPerPixel: 4'
  assert_contains "$properties" 'hasAlpha: yes'
  assert_contains "$properties" "pixelWidth: $width"
  assert_contains "$properties" "pixelHeight: $height"
}
assert_png_properties "$asset_root/background.png" 660 360
assert_png_properties "$asset_root/background@2x.png" 1320 720

pixel_probe="$tmp/pixel-probe.swift"
cat >"$pixel_probe" <<'SWIFT'
import AppKit
import Foundation

func fail(_ message: String) -> Never {
    fputs("pixel-probe: \(message)\n", stderr)
    exit(1)
}

guard CommandLine.arguments.count == 3,
      let scale = Int(CommandLine.arguments[2]),
      scale == 1 || scale == 2 else {
    fail("usage: pixel-probe.swift IMAGE SCALE")
}
guard let image = NSImage(contentsOfFile: CommandLine.arguments[1]),
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff) else {
    fail("could not decode \(CommandLine.arguments[1])")
}

func isArrowPurple(x: Int, y: Int) -> Bool {
    guard let color = bitmap.colorAt(x: x * scale, y: y * scale)?
        .usingColorSpace(.deviceRGB) else {
        return false
    }
    let red = Int((color.redComponent * 255).rounded())
    let green = Int((color.greenComponent * 255).rounded())
    let blue = Int((color.blueComponent * 255).rounded())
    let alpha = Int((color.alphaComponent * 255).rounded())
    return red < 180 && green < 180 && blue > 220 && alpha == 255
}

// PNG rows are top-down: renderer y=150 and y=145 map to rows 210 and 215.
// These interior samples require the approved 12-point shaft and x=397 tip.
guard isArrowPurple(x: 272, y: 210) else {
    fail("missing strengthened arrow shaft sample")
}
guard isArrowPurple(x: 393, y: 215) else {
    fail("missing extended arrow tip sample")
}
SWIFT
swift -module-cache-path "$tmp/swift-module-cache" "$pixel_probe" \
  "$asset_root/background.png" 1
swift -module-cache-path "$tmp/swift-module-cache" "$pixel_probe" \
  "$asset_root/background@2x.png" 2

fixture_generator="$tmp/generate-ds-store-fixtures.py"
cat >"$fixture_generator" <<'PY'
import copy
import datetime
import os
import sys

from ds_store import DSStore
from mac_alias.alias import (
    ALIAS_FIXED_DISK,
    ALIAS_KIND_FILE,
    Alias,
    TargetInfo,
    VolumeInfo,
    mac_epoch,
)

output_directory = sys.argv[1]
alias_time = mac_epoch + datetime.timedelta(seconds=1)
background_alias = Alias(
    volume=VolumeInfo(
        "Loqui", alias_time, b"H+", ALIAS_FIXED_DISK, 0, b"\0\0",
        posix_path="/Volumes/Loqui",
    ),
    target=TargetInfo(
        ALIAS_KIND_FILE, ".background.tiff", 2, 3, alias_time, b"\0" * 4,
        b"\0" * 4, posix_path="/.background.tiff",
    ),
).to_bytes()
wrong_background_alias = Alias(
    volume=VolumeInfo(
        "Loqui", alias_time, b"H+", ALIAS_FIXED_DISK, 0, b"\0\0",
        posix_path="/Volumes/Loqui",
    ),
    target=TargetInfo(
        ALIAS_KIND_FILE, "wrong.tiff", 2, 3, alias_time, b"\0" * 4,
        b"\0" * 4, posix_path="/wrong.tiff",
    ),
).to_bytes()
bwsp = {
    "WindowBounds": "{{100, 100}, {660, 384}}",
    "ShowStatusBar": False,
    "ShowTabView": False,
    "ShowToolbar": False,
    "ShowPathbar": False,
    "ShowSidebar": False,
}
icvp = {
    "arrangeBy": "none",
    "backgroundType": 2,
    "iconSize": 128.0,
    "textSize": 14.0,
    "labelOnBottom": True,
    "backgroundImageAlias": background_alias,
}
locations = {
    "Loqui.app": (160, 215),
    "Applications": (500, 215),
}

def write_fixture(
    name,
    bwsp_value=bwsp,
    icvp_value=icvp,
    locations_value=locations,
    vsrn_value=(b"long", 1),
    icvl_value=(b"type", b"icnv"),
):
    path = os.path.join(output_directory, name)
    with DSStore.open(path, "w+") as store:
        store["."]["bwsp"] = bwsp_value
        store["."]["icvp"] = icvp_value
        if vsrn_value is not None:
            store["."]["vSrn"] = vsrn_value
        if icvl_value is not None:
            store["."]["icvl"] = icvl_value
        for filename, location in locations_value.items():
            store[filename]["Iloc"] = location

write_fixture("valid.DS_Store")

write_fixture("mutated-missing-vsrn.DS_Store", vsrn_value=None)
write_fixture("mutated-view.DS_Store", icvl_value=(b"type", b"Nlsv"))

mutated_icvp = copy.deepcopy(icvp)
del mutated_icvp["backgroundImageAlias"]
write_fixture("mutated-missing-alias.DS_Store", icvp_value=mutated_icvp)

mutated_icvp = copy.deepcopy(icvp)
mutated_icvp["backgroundImageAlias"] = wrong_background_alias
write_fixture("mutated-wrong-alias.DS_Store", icvp_value=mutated_icvp)

mutated_bwsp = copy.deepcopy(bwsp)
mutated_bwsp["WindowBounds"] = "{{101, 100}, {660, 384}}"
write_fixture("mutated-window.DS_Store", bwsp_value=mutated_bwsp)

mutated_bwsp = copy.deepcopy(bwsp)
mutated_bwsp["ShowToolbar"] = True
write_fixture("mutated-chrome.DS_Store", bwsp_value=mutated_bwsp)

mutated_icvp = copy.deepcopy(icvp)
mutated_icvp["iconSize"] = 96.0
write_fixture("mutated-icon-view.DS_Store", icvp_value=mutated_icvp)

mutated_icvp = copy.deepcopy(icvp)
mutated_icvp["labelOnBottom"] = False
write_fixture("mutated-label.DS_Store", icvp_value=mutated_icvp)

mutated_icvp = copy.deepcopy(icvp)
mutated_icvp["backgroundType"] = 1
write_fixture("mutated-background.DS_Store", icvp_value=mutated_icvp)

mutated_locations = copy.deepcopy(locations)
mutated_locations["Applications"] = (499, 215)
write_fixture("mutated-position.DS_Store", locations_value=mutated_locations)

PY
"$dmgbuild_python" "$fixture_generator" "$tmp"

verifier_stdout="$tmp/verifier.stdout"
"$dmgbuild_python" "$verifier_path" "$tmp/valid.DS_Store" >"$verifier_stdout"
assert_eq "$(cat "$verifier_stdout")" 'verify-ds-store: PASS'

assert_exact_failure "verify-ds-store: vSrn: expected (b'long', 1), got <missing>" \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-missing-vsrn.DS_Store"
assert_exact_failure "verify-ds-store: icvl: expected (b'type', b'icnv'), got (b'type', b'Nlsv')" \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-view.DS_Store"
assert_exact_failure 'verify-ds-store: icvp.backgroundImageAlias: expected parseable alias, got <missing>' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-missing-alias.DS_Store"
assert_exact_failure 'verify-ds-store: icvp.backgroundImageAlias target: expected .background.tiff, got wrong.tiff' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-wrong-alias.DS_Store"

assert_exact_failure 'verify-ds-store: bwsp.WindowBounds: expected {{100, 100}, {660, 384}}, got {{101, 100}, {660, 384}}' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-window.DS_Store"
assert_exact_failure 'verify-ds-store: bwsp.ShowToolbar: expected false, got true' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-chrome.DS_Store"
assert_exact_failure 'verify-ds-store: icvp.iconSize: expected 128, got 96' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-icon-view.DS_Store"
assert_exact_failure 'verify-ds-store: icvp.labelOnBottom: expected true, got false' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-label.DS_Store"
assert_exact_failure 'verify-ds-store: icvp.backgroundType: expected 2, got 1' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-background.DS_Store"
assert_exact_failure 'verify-ds-store: Applications.Iloc: expected (500, 215), got (499, 215)' \
  "$dmgbuild_python" "$verifier_path" "$tmp/mutated-position.DS_Store"
ln -s "$tmp/valid.DS_Store" "$tmp/symlink.DS_Store"
assert_exact_failure "verify-ds-store: not a regular non-symlink file: $tmp/symlink.DS_Store" \
  "$dmgbuild_python" "$verifier_path" "$tmp/symlink.DS_Store"

echo 'dmg-layout-test: PASS'
