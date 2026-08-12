# Intuitive Bilingual DMG Installer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Loqui's generic DMG folder presentation with a deterministic, compact, bilingual
drag-to-Applications experience and prove it locally without publishing a new release.

**Architecture:** A hash-locked `dmgbuild==1.6.7` virtual environment generates the image from
repository-owned settings and 1×/2× AppKit-rendered backgrounds. The release script audits the
signed staged app before generation, mounts the generated DMG read-only, verifies its visible and
hidden presentation contract plus the copied app signature/designated requirements, then resumes
the unchanged sign/notarize/staple/Gatekeeper/atomic-publication pipeline.

**Tech Stack:** Bash 3.2-compatible scripts, Python 3.10+, `dmgbuild 1.6.7`, hashed pip
requirements, Swift/AppKit, Taskfile v3, `hdiutil`, `ditto`, `codesign`, GitHub Actions macOS 26.

## Global Constraints

- Work only on `feat/intuitive-dmg-installer`; never implement on `main`.
- Finder window bounds are 660 × 384 points around the 660 × 360 background composition. The
  extra 24 points preserve the complete labels when a user-level Finder path strip remains visible.
- Exact visible copy is `Drag Loqui to Applications` followed by
  `Arrastra Loqui a Aplicaciones`.
- Real Finder items stay at `Loqui.app = (160, 215)` and `Applications = (500, 215)`.
- Icon size is 128 points; label text is 14 points and appears below.
- Toolbar, sidebar, status bar, and tab view remain hidden. The settings request a hidden path bar,
  but E2E records and permits the user's global Finder path strip/vertical-scroll indicator.
- Background assets are exactly 660 × 360 and 1320 × 720 PNGs.
- The DMG remains HFS+ and UDZO with volume name `Loqui`.
- `dmgbuild` is exactly version `1.6.7`; all resolved Python artifacts are pinned with SHA-256
  hashes and installed with `--require-hashes --only-binary=:all:`.
- Do not add Finder/AppleScript automation or a license-acceptance dialog.
- Do not change `build/config.yml`, the app bundle, release filename, minimum macOS version, tag,
  GitHub Release, or public `v0.1.0` assets.
- Preserve all signing, designated-requirement, notarization, ticket, stapling, Gatekeeper,
  evidence, cleanup, and atomic-publication invariants.
- Do not add `Co-authored-by` trailers.
- Project ship gates override this skill's frequent-commit default: stage task outputs, but make no
  commit until the standard profile is fully green and `finish-branch` creates the single ship
  commit.

## File map

- Create `build/darwin/dmg/requirements.in` — reviewed direct Python requirement.
- Create `build/darwin/dmg/requirements.txt` — generated, fully pinned/hash-checked lock.
- Create `scripts/setup-dmgbuild.sh` — safe digest-addressed virtual-environment bootstrap.
- Create `scripts/tests/setup-dmgbuild-test.sh` — isolated bootstrap contract tests.
- Create `build/darwin/dmg/render-background.swift` — deterministic AppKit renderer.
- Create `build/darwin/dmg/background.png` — committed 1× release input.
- Create `build/darwin/dmg/background@2x.png` — committed Retina release input.
- Create `build/darwin/dmg/background.sha256` — reviewed digests for both committed backgrounds.
- Create `build/darwin/dmg/settings.py` — declarative Finder/DMG settings.
- Create `build/darwin/dmg/verify-ds-store.py` — semantic mounted Finder-metadata verifier.
- Create `scripts/tests/dmg-layout-test.sh` — settings, committed-asset, and `.DS_Store` contracts.
- Create `scripts/tests/dmg-integration-test.sh` — credential-free real-generator/mount integration.
- Modify `scripts/release-macos.sh` — pinned builder validation, generation, mount/audit/detach.
- Modify `scripts/tests/release-macos-test.sh` — generator and mounted-image failure contracts.
- Modify `build/darwin/Taskfile.yml` — prepare the pinned builder for local release.
- Modify `Taskfile.yml` — include focused setup/layout tests in the ship gate.
- Modify `.github/workflows/release.yml` — prepare the tool before Apple credentials.
- Modify `scripts/tests/github-release-workflow-test.sh` — bind setup ordering and release wiring.
- Create `docs/e2e/use-cases/intuitive-dmg-installer.md` — real Finder journey.
- Create `docs/e2e/reports/2026-08-11-intuitive-dmg-installer.md` — observed evidence.
- Modify `docs/CHANGELOG.md` — newest-first user-visible installer improvement.
- Modify `.workflow/state.md` — transient phase, review, E2E, and gate evidence.

---

### Task 1: Hash-locked dmgbuild bootstrap

**Files:**
- Create: `build/darwin/dmg/requirements.in`
- Create: `build/darwin/dmg/requirements.txt`
- Create: `scripts/setup-dmgbuild.sh`
- Create: `scripts/tests/setup-dmgbuild-test.sh`

**Interfaces:**
- Consumes: `python3 >= 3.10`, `shasum`, repository root, optional test-only
  `LOQUI_DMGBUILD_TOOLS_ROOT` and `LOQUI_PYTHON3`.
- Produces: `scripts/setup-dmgbuild.sh` printing one absolute virtual-environment Python path whose
  installed distribution metadata reports exactly `dmgbuild 1.6.7`.

- [ ] **Step 1: Write the failing bootstrap contract test**

Create `scripts/tests/setup-dmgbuild-test.sh` with an isolated fake Python that can emulate
`python -m venv`, pip installation, and package-version lookup:

```bash
#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
. "$repo_root/scripts/tests/testlib.sh"

setup_script="$repo_root/scripts/setup-dmgbuild.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dmgbuild-setup-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
tmp="$(cd "$tmp" && pwd -P)"
fake_python="$tmp/fake-python3"
calls="$tmp/calls"

cat >"$fake_python" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_PYTHON_CALLS"
if [ "${1:-}" = -m ] && [ "${2:-}" = venv ]; then
  target="$3"
  mkdir -p "$target/bin"
  cp "$0" "$target/bin/python"
  exit 0
fi
if [ "${1:-}" = -m ] && [ "${2:-}" = pip ]; then
  exit "${FAKE_PIP_RC:-0}"
fi
if [ "${1:-}" = -c ]; then
  case "$2" in
    *sys.version_info*) exit "${FAKE_PYTHON_VERSION_RC:-0}" ;;
    *os.rename*) mv "$3" "$4"; exit 0 ;;
  esac
  printf '%s\n' "${FAKE_DMGBUILD_VERSION:-1.6.7}"
  exit 0
fi
exit 91
FAKE
chmod +x "$fake_python"

: >"$calls"
python_path="$(
  FAKE_PYTHON_CALLS="$calls" \
  LOQUI_PYTHON3="$fake_python" \
  LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/tools" \
  "$setup_script"
)"
assert_eq "$python_path" "$tmp/tools/dmgbuild-$(shasum -a 256 \
  "$repo_root/build/darwin/dmg/requirements.txt" | awk '{print $1}')/bin/python"
assert_contains "$calls" '-m venv'
assert_contains "$calls" '-m pip install --disable-pip-version-check --isolated --no-cache-dir --require-hashes --only-binary=:all:'
assert_contains "$calls" 'importlib.metadata.version'

first_call_count="$(wc -l <"$calls" | tr -d ' ')"
FAKE_PYTHON_CALLS="$calls" \
LOQUI_PYTHON3="$fake_python" \
LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/tools" \
  "$setup_script" >/dev/null
assert_eq "$(wc -l <"$calls" | tr -d ' ')" "$((first_call_count + 2))"
assert_contains "$calls" 'sys.version_info'
assert_contains "$calls" 'importlib.metadata.version'

run_expect_fail_msg() {
  label="$1"
  expected="$2"
  shift 2
  output="$tmp/$label.out"
  if "$@" >"$output" 2>&1; then
    fail "$label unexpectedly succeeded"
  fi
  assert_contains "$output" "$expected"
}

run_expect_fail_msg wrong-version 'installed dmgbuild version is' \
  env FAKE_PYTHON_CALLS="$calls" FAKE_DMGBUILD_VERSION=9.9.9 \
    LOQUI_PYTHON3="$fake_python" LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/wrong-version" \
    "$setup_script"
run_expect_fail_msg relative-tools-root 'tools root must be absolute' \
  env FAKE_PYTHON_CALLS="$calls" LOQUI_PYTHON3="$fake_python" \
    LOQUI_DMGBUILD_TOOLS_ROOT=relative "$setup_script"
run_expect_fail_msg old-python 'Python 3.10 or newer is required' \
  env FAKE_PYTHON_CALLS="$calls" FAKE_PYTHON_VERSION_RC=1 \
    LOQUI_PYTHON3="$fake_python" LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/old-python" \
    "$setup_script"

echo 'setup-dmgbuild-test: PASS'
```

- [ ] **Step 2: Run the test and confirm RED**

Run:

```bash
./scripts/tests/setup-dmgbuild-test.sh
```

Expected: nonzero because `scripts/setup-dmgbuild.sh` and the requirement files do not exist.

- [ ] **Step 3: Add the direct requirement and generate the hash lock**

Create `build/darwin/dmg/requirements.in`:

```text
# Lock generator: pip-tools==7.5.2
dmgbuild==1.6.7
```

Generate the reviewed lock in an isolated temporary environment:

```bash
lock_env="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dmg-lock.XXXXXX")"
python3 -m venv "$lock_env"
"$lock_env/bin/python" -m pip install --disable-pip-version-check pip-tools==7.5.2
"$lock_env/bin/pip-compile" \
  --generate-hashes \
  --output-file build/darwin/dmg/requirements.txt \
  build/darwin/dmg/requirements.in
"$lock_env/bin/python" -m pip download --disable-pip-version-check --isolated --no-cache-dir \
  --require-hashes --only-binary=:all: --dest "$lock_env/wheelhouse" \
  -r build/darwin/dmg/requirements.txt
```

Inspect the generated file and require exact pins for `dmgbuild`, `ds-store`, and `mac-alias`,
with at least one `--hash=sha256:` per resolved distribution. Do not add badge-icon extras.

- [ ] **Step 4: Implement the safe bootstrap**

Create `scripts/setup-dmgbuild.sh`:

```bash
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
  -r "$requirements" || die "could not install locked dmgbuild dependencies"
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
```

Make the script executable.

- [ ] **Step 5: Run the focused test and real setup**

Run:

```bash
./scripts/tests/setup-dmgbuild-test.sh
python_path="$(./scripts/setup-dmgbuild.sh)"
"$python_path" -c 'import importlib.metadata; assert importlib.metadata.version("dmgbuild") == "1.6.7"'
"$python_path" -m dmgbuild --help >/dev/null
git check-ignore -q .task/tools/probe
[ -z "$(git status --porcelain -- .task)" ]
```

Expected: `setup-dmgbuild-test: PASS`, all commands exit 0, and the digest-addressed environment
remains ignored by the existing `.gitignore:28` `.task/` rule.

The first real setup (and therefore the first local `./scripts/task.sh check`) requires network
access to download hash-locked wheels from PyPI. Reuse of the digest-addressed environment is
offline. CI performs the one-time setup and the real DMG integration before importing Apple
credentials.

- [ ] **Step 6: Stage Task 1 without committing**

```bash
git add build/darwin/dmg/requirements.in build/darwin/dmg/requirements.txt \
  scripts/setup-dmgbuild.sh scripts/tests/setup-dmgbuild-test.sh
git diff --cached --check
```

Expected: no whitespace errors; do not commit.

---

### Task 2: Declarative layout and Retina background

**Files:**
- Create: `build/darwin/dmg/render-background.swift`
- Create: `build/darwin/dmg/background.png`
- Create: `build/darwin/dmg/background@2x.png`
- Create: `build/darwin/dmg/background.sha256`
- Create: `build/darwin/dmg/settings.py`
- Create: `build/darwin/dmg/verify-ds-store.py`
- Create: `scripts/tests/dmg-layout-test.sh`

**Interfaces:**
- Consumes: an absolute `Loqui.app` path in `defines["app"]` and the absolute repository asset
  directory in `defines["assets"]`.
- Produces: `dmgbuild` globals `files`, `symlinks`, `format`, `filesystem`,
  `window_rect`, `background`, `icon_locations`, and Finder view constants; renderer writes the two
  committed PNG names into one requested output directory.

- [ ] **Step 1: Write the failing layout contract**

Create `scripts/tests/dmg-layout-test.sh`. It must:

1. execute `settings.py` with the same single globals/locals dictionary shape used by
   `dmgbuild 1.6.7`'s `load_settings` call
   `exec(compile(source, path, "exec"), settings, settings)`, with physical absolute fixture app
   and asset paths and no synthetic `__file__`;
2. JSON-serialize and assert every approved value;
3. verify missing and relative app or asset values fail with exact diagnostics;
4. require both committed PNGs and `background.sha256` to be regular non-symlink files;
5. verify both committed digests with `shasum -a 256 -c`;
6. use `sips` to assert PNG format, 8 bits/sample, 4 samples/pixel, alpha, and exact
   660 × 360 / 1320 × 720 dimensions;
7. create valid and mutated `.DS_Store` fixtures with the pinned `ds_store` package and prove the
   semantic verifier accepts only the approved window, chrome, icon-view, label, background, and
   icon-position records.

The test obtains the interpreter from `LOQUI_DMGBUILD_PYTHON` when provided, otherwise from
`scripts/setup-dmgbuild.sh`, so fixture generation and verification always use the locked package.

Use this settings probe:

```python
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
```

Expected JSON includes:

```json
{
  "arrange_by": null,
  "default_view": "icon-view",
  "filesystem": "HFS+",
  "format": "UDZO",
  "hide": [".background.tiff"],
  "hide_extensions": [],
  "icon_locations": {
    "Applications": [500, 215],
    "Loqui.app": [160, 215]
  },
  "icon_size": 128,
  "label_pos": "bottom",
  "show_pathbar": false,
  "show_sidebar": false,
  "show_status_bar": false,
  "show_tab_view": false,
  "show_toolbar": false,
  "symlinks": {
    "Applications": "/Applications"
  },
  "text_size": 14,
  "window_rect": [[100, 100], [660, 384]]
}
```

- [ ] **Step 2: Run the layout test and confirm RED**

Run:

```bash
./scripts/tests/dmg-layout-test.sh
```

Expected: nonzero because the settings, verifier, checksum, and PNGs do not exist.

- [ ] **Step 3: Implement the AppKit renderer**

Create `build/darwin/dmg/render-background.swift` with one point-based drawing function invoked at
scale 1 and 2. Use:

```swift
#!/usr/bin/env swift
import AppKit
import Foundation

let pointWidth = 660
let pointHeight = 360
let outputDirectory = CommandLine.arguments.count == 2
    ? URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
    : nil
guard let outputDirectory else {
    fputs("usage: render-background.swift OUTPUT_DIR\n", stderr)
    exit(64)
}

func color(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat) -> NSColor {
    NSColor(calibratedRed: red / 255, green: green / 255, blue: blue / 255, alpha: 1)
}

func drawText(_ text: String, y: CGFloat, size: CGFloat, weight: NSFont.Weight,
              color textColor: NSColor) {
    let paragraph = NSMutableParagraphStyle()
    paragraph.alignment = .center
    let attributes: [NSAttributedString.Key: Any] = [
        .font: NSFont.systemFont(ofSize: size, weight: weight),
        .foregroundColor: textColor,
        .paragraphStyle: paragraph,
    ]
    NSString(string: text).draw(
        in: NSRect(x: 40, y: y, width: 580, height: size + 12),
        withAttributes: attributes
    )
}

func render(scale: Int, filename: String) throws {
    guard let bitmap = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: pointWidth * scale,
        pixelsHigh: pointHeight * scale,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ) else { throw NSError(domain: "LoquiDMG", code: 1) }
    guard let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
        throw NSError(domain: "LoquiDMG", code: 3)
    }
    let previousContext = NSGraphicsContext.current
    NSGraphicsContext.current = context
    defer { NSGraphicsContext.current = previousContext }
    context.cgContext.scaleBy(x: CGFloat(scale), y: CGFloat(scale))
    let bounds = NSRect(x: 0, y: 0, width: pointWidth, height: pointHeight)
    NSGradient(
        starting: color(251, 250, 255),
        ending: color(241, 241, 255)
    )!.draw(in: bounds, angle: -90)

    drawText("Drag Loqui to Applications", y: 298, size: 24, weight: .semibold,
             color: color(31, 31, 46))
    drawText("Arrastra Loqui a Aplicaciones", y: 267, size: 17, weight: .medium,
             color: color(92, 92, 121))

    let arrowColor = color(91, 92, 246)
    arrowColor.setFill()
    let arrow = NSBezierPath()
    arrow.move(to: NSPoint(x: 270, y: 139))
    arrow.line(to: NSPoint(x: 357, y: 139))
    arrow.line(to: NSPoint(x: 357, y: 124))
    arrow.line(to: NSPoint(x: 397, y: 145))
    arrow.line(to: NSPoint(x: 357, y: 166))
    arrow.line(to: NSPoint(x: 357, y: 151))
    arrow.line(to: NSPoint(x: 270, y: 151))
    arrow.close()
    arrow.fill()

    guard let data = bitmap.representation(using: .png, properties: [:]) else {
        throw NSError(domain: "LoquiDMG", code: 2)
    }
    try data.write(to: outputDirectory.appendingPathComponent(filename), options: .atomic)
}

try FileManager.default.createDirectory(
    at: outputDirectory,
    withIntermediateDirectories: true
)
try render(scale: 1, filename: "background.png")
try render(scale: 2, filename: "background@2x.png")
```

The background contains only text, arrow, and subtle color; it must not paint either Finder icon.

- [ ] **Step 4: Implement declarative settings**

Create `build/darwin/dmg/settings.py`:

```python
from pathlib import Path

try:
    application = Path(defines["app"])
except (KeyError, TypeError) as error:
    raise ValueError("settings require -D app=/absolute/path/Loqui.app") from error
try:
    asset_root = Path(defines["assets"])
except (KeyError, TypeError) as error:
    raise ValueError("settings require -D assets=/absolute/path/to/dmg-assets") from error

if not application.is_absolute():
    raise ValueError("app path must be absolute")
if application.name != "Loqui.app":
    raise ValueError("app path must end in Loqui.app")
if not asset_root.is_absolute():
    raise ValueError("assets path must be absolute")

background = str(asset_root / "background.png")

format = "UDZO"
filesystem = "HFS+"
files = [str(application)]
symlinks = {"Applications": "/Applications"}
hide = [".background.tiff"]
hide_extensions = []

window_rect = ((100, 100), (660, 384))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
show_icon_preview = False

arrange_by = None
label_pos = "bottom"
text_size = 14
icon_size = 128
icon_locations = {
    "Loqui.app": (160, 215),
    "Applications": (500, 215),
}
```

Finder normally displays the `Loqui.app` application bundle as `Loqui`, so the settings do not
force the legacy extension-hidden bit. With dmgbuild 1.6.7, `hide_extensions = ["Loqui.app"]`
applies `SetFile` after copying the signed bundle, adding `com.apple.FinderInfo` and invalidating
`codesign --verify --deep --strict`; the focused integration must reject that xattr and require
strict verification of the mounted copy.

The generated `.background.tiff` is not an icon-view item and receives no `Iloc` record. Dmgbuild
1.6.7 processes the supported `hide` list with `SetFile -a V`, so the background's FinderInfo has
the `kIsInvisible` (`0x4000`) flag and Finder excludes it from the visible/scrollable icon canvas.
The real mounted integration must require that exact flag.

Create `build/darwin/dmg/verify-ds-store.py` using the locked `ds_store.DSStore` API. It accepts one
`.DS_Store` path, requires a regular non-symlink file, and fails with field-specific diagnostics
unless all of these semantic records match:

```text
bwsp.WindowBounds = {{100, 100}, {660, 384}}
bwsp.ShowStatusBar = false
bwsp.ShowTabView = false
bwsp.ShowToolbar = false
bwsp.ShowPathbar = false
bwsp.ShowSidebar = false
icvp.arrangeBy = none
icvp.backgroundType = 2
icvp.iconSize = 128
icvp.textSize = 14
icvp.labelOnBottom = true
Loqui.app.Iloc = (160, 215)
Applications.Iloc = (500, 215)
```

The script exits 0 with `verify-ds-store: PASS` only after every record matches. The focused test
constructs the approved fixture and one mutation per record group using the same locked library;
this checks the parser without trusting a hand-written binary fixture.

- [ ] **Step 5: Generate and inspect both committed backgrounds**

Run the explicit regeneration command locally (never as part of CI or the release path):

```bash
swift build/darwin/dmg/render-background.swift build/darwin/dmg
sips -g pixelWidth -g pixelHeight build/darwin/dmg/background.png
sips -g pixelWidth -g pixelHeight build/darwin/dmg/background@2x.png
cd build/darwin/dmg && shasum -a 256 background.png background@2x.png > background.sha256
```

Expected dimensions: 660 × 360 and 1320 × 720. Visually inspect both PNGs and review the asset and
digest diffs before proceeding. Fresh renderer bytes are intentionally not compared in CI because
AppKit rendering and PNG encoding can vary across macOS/Xcode versions.

- [ ] **Step 6: Run the focused layout test and confirm GREEN**

```bash
chmod +x build/darwin/dmg/render-background.swift scripts/tests/dmg-layout-test.sh
./scripts/tests/dmg-layout-test.sh
```

Expected: `dmg-layout-test: PASS`.

- [ ] **Step 7: Stage Task 2 without committing**

```bash
git add build/darwin/dmg/render-background.swift build/darwin/dmg/settings.py \
  build/darwin/dmg/verify-ds-store.py \
  build/darwin/dmg/background.png build/darwin/dmg/background@2x.png \
  build/darwin/dmg/background.sha256 \
  scripts/tests/dmg-layout-test.sh
git diff --cached --check
```

Expected: no whitespace errors; do not commit.

---

### Task 3: Safe release generation and mounted-image verification

**Files:**
- Modify: `scripts/release-macos.sh:4-30`
- Modify: `scripts/release-macos.sh:340-410`
- Modify: `scripts/release-macos.sh:776-820`
- Modify: `scripts/tests/release-macos-test.sh:640-710`
- Modify: `scripts/tests/release-macos-test.sh:950-1030`
- Create: `scripts/tests/dmg-integration-test.sh`

**Interfaces:**
- Consumes: absolute executable `LOQUI_DMGBUILD_PYTHON` from Task 1, Task 2 settings/assets, signed
  `$app`, physical `$stage`.
- Produces: regular `$stage/Loqui.dmg`; mounted-image verification evidence at
  `$stage/evidence-work/designated-requirements-dmg.txt` plus semantic Finder-layout evidence; no
  mounted verification volume on return.

- [ ] **Step 1: Extend release tests for the new generator and confirm the old path is rejected**

In `scripts/tests/release-macos-test.sh`:

- create a fake dmgbuild Python that logs `-m dmgbuild` arguments and writes the requested output;
- make the fake `hdiutil` support `verify`, `attach -mountpoint`, and `detach`, and add a fake
  `tiffutil -info` that describes the expected 660 × 360 and 1320 × 720 frames;
- on attach, populate the mount with `Loqui.app`, `Applications -> /Applications`, `.DS_Store`,
  and `.background.tiff`;
- assert the generator receives exactly:

```text
-m dmgbuild
-s REPOSITORY/build/darwin/dmg/settings.py
-D app=STAGE/dmg-root/Loqui.app
-D assets=REPOSITORY/build/darwin/dmg
Loqui
STAGE/Loqui.dmg
```

- assert no `hdiutil create -srcfolder` call remains;
- make fake Python emulate `verify-ds-store.py` and add failures for wrong window/chrome/icon-view
  values and wrong `Iloc` records;
- assert image audit and designated requirements happen against the mounted `Loqui.app`;
- add preflight failures for a missing or relative `LOQUI_DMGBUILD_PYTHON`, a non-executable path,
  a version probe that exits nonzero, a wrong package version, and a wrong-sized background;
- add one generation/inspection failure case each for generator failure, missing output, symlink
  output, attach failure, extra visible root item, wrong Applications target, missing `.DS_Store`,
  missing background, single-frame or wrong-sized Retina background, mounted app audit failure,
  mounted signature failure, designated-requirement mismatch, detach failure, and the combined case
  where inspection and detach both fail;
- add a detach sequence that fails once with a transient error and then succeeds, and an exhaustion
  sequence that verifies force-detach cleanup still leaves the candidate ineligible;
- pre-create `dmg-root` as a symlink outside the stage, assert containment fails before `ditto` or
  generator invocation, and verify the external sentinel is untouched;
- pre-create `dmg-verify` as a symlink outside the stage, assert attach is never called, and verify
  the external sentinel is untouched;
- assert every case stops before `sign-dmg`, `submit`, and `publish`.

Also write `scripts/tests/dmg-integration-test.sh`. It must obtain or accept the pinned Python path,
create a unique temporary stub `Loqui.app`, invoke the real `python -m dmgbuild` with the production
settings/assets, verify and mount the throwaway image read-only, and run the same visible-root,
Applications target, two-frame TIFF, and `verify-ds-store.py` assertions. It always detaches and
removes its temporary image, uses no signing/notary credentials, and never writes under
`bin/release`.

- [ ] **Step 2: Run the focused release test and confirm RED**

```bash
./scripts/tests/release-macos-test.sh
```

Expected: nonzero because `release-macos.sh` still calls `hdiutil create -srcfolder` and never mounts
the generated image for inspection.

- [ ] **Step 3: Add pinned-tool preflight**

At the top of `scripts/release-macos.sh` add:

```bash
dmgbuild_python="${LOQUI_DMGBUILD_PYTHON:-}"
dmg_verify_mount=""
dmg_verify_mounted=0
```

Add `validate_dmgbuild`:

```bash
validate_dmgbuild() {
  [ -n "$dmgbuild_python" ] || {
    die "LOQUI_DMGBUILD_PYTHON is not set"
    return 1
  }
  case "$dmgbuild_python" in
    /*) ;;
    *) die "LOQUI_DMGBUILD_PYTHON must be absolute"; return 1 ;;
  esac
  [ -x "$dmgbuild_python" ] || {
    die "dmgbuild Python is not executable: $dmgbuild_python"
    return 1
  }
  if ! dmgbuild_version="$("$dmgbuild_python" -c \
    'import importlib.metadata; print(importlib.metadata.version("dmgbuild"))' 2>/dev/null)"; then
    die "could not read installed dmgbuild version"
    return 1
  fi
  [ "$dmgbuild_version" = 1.6.7 ] || {
    die "installed dmgbuild version is '$dmgbuild_version', expected '1.6.7'"
    return 1
  }
}
```

Call it from `phase_preflight`. Add the settings, `verify-ds-store.py`, both backgrounds,
`background.sha256`, and `scripts/setup-dmgbuild.sh` to required sources/scripts. Require `sips`
and `tiffutil` as tools.
Before any build phase, validate the PNGs are regular non-symlink files, verify their reviewed
digests, and use `sips` to require exact 660 × 360 / 1320 × 720 dimensions, 8 bits/sample, and four
samples/pixel with alpha. A wrong-size or wrong-digest fixture must fail before the fake generator
is called.

- [ ] **Step 4: Add a detach-safe mounted-image verifier**

Add helpers:

```bash
detach_dmg_verification_mount() {
  [ "$dmg_verify_mounted" -eq 1 ] || return 0
  # LOQUI_DMG_DETACH_RETRY_DELAY is test-only; production defaults to one second.
  for detach_attempt in 1 2 3; do
    if hdiutil detach "$dmg_verify_mount" >/dev/null; then
      dmg_verify_mounted=0
      dmg_verify_mount=""
      return 0
    fi
    [ "$detach_attempt" -eq 3 ] || sleep "${LOQUI_DMG_DETACH_RETRY_DELAY:-1}"
  done
  if hdiutil detach -force "$dmg_verify_mount" >/dev/null 2>&1; then
    dmg_verify_mounted=0
    dmg_verify_mount=""
  fi
  die "could not cleanly detach DMG verification mount"
  return 1
}

inspect_generated_dmg_contents() {
  visible_manifest="$stage/evidence-work/dmg-visible-root.txt"
  visible_raw="$stage/evidence-work/dmg-visible-root.raw"
  if ! find "$dmg_verify_mount" -mindepth 1 -maxdepth 1 ! -name '.*' -print \
      >"$visible_raw"; then
    die "could not inspect generated DMG root"
    return 1
  fi
  sed "s#^$dmg_verify_mount/##" "$visible_raw" | LC_ALL=C sort >"$visible_manifest" \
    || { die "could not normalize generated DMG root"; return 1; }
  expected_visible="$stage/evidence-work/dmg-visible-root.expected"
  printf '%s\n' Applications Loqui.app >"$expected_visible"
  diff -u "$expected_visible" "$visible_manifest" || {
    die "generated DMG has unexpected visible root items"
    return 1
  }
  [ -L "$dmg_verify_mount/Applications" ] &&
    [ "$(readlink "$dmg_verify_mount/Applications")" = /Applications ] || {
      die "generated DMG Applications link is invalid"
      return 1
    }
  [ -f "$dmg_verify_mount/.DS_Store" ] || {
    die "generated DMG is missing .DS_Store"
    return 1
  }
  ds_store_evidence="$stage/evidence-work/dmg-ds-store.txt"
  if ! "$dmgbuild_python" "$release_root_dir/build/darwin/dmg/verify-ds-store.py" \
      "$dmg_verify_mount/.DS_Store" >"$ds_store_evidence"; then
    die "generated DMG Finder metadata is invalid"
    return 1
  fi
  [ -f "$dmg_verify_mount/.background.tiff" ] || {
    die "generated DMG is missing Retina background"
    return 1
  }
  retina_info="$stage/evidence-work/dmg-background-tiff.txt"
  verify_retina_tiff "$dmg_verify_mount/.background.tiff" "$retina_info" || return 1
  "$release_root_dir/scripts/macos-audit.sh" --channel production --version "$version" \
    "$dmg_verify_mount/Loqui.app" >/dev/null || return 1
  codesign --verify --deep --strict "$dmg_verify_mount/Loqui.app" || return 1
  dmg_designated_requirements="$stage/evidence-work/designated-requirements-dmg.txt"
  capture_designated_requirements \
    "$dmg_verify_mount/Loqui.app" "$dmg_designated_requirements" || return 1
  compare_designated_requirements \
    "$stage/evidence-work/designated-requirements.txt" \
    "$dmg_designated_requirements"
}

verify_generated_dmg_contents() {
  dmg_verify_mount="$stage/dmg-verify"
  if [ -e "$dmg_verify_mount" ] || [ -L "$dmg_verify_mount" ]; then
    die "DMG verification mount path already exists"
    return 1
  fi
  mkdir -p "$dmg_verify_mount" || {
    die "could not create DMG verification mount"
    return 1
  }
  dmg_verify_mount_physical="$(cd "$dmg_verify_mount" && pwd -P)" || return 1
  [ "$dmg_verify_mount_physical" = "$stage/dmg-verify" ] || {
    die "DMG verification mount resolves outside release stage"
    return 1
  }
  if ! hdiutil attach -readonly -nobrowse -mountpoint "$dmg_verify_mount" "$dmg" >/dev/null; then
    die "could not mount generated DMG"
    return 1
  fi
  dmg_verify_mounted=1

  inspection_status=0
  if ! inspect_generated_dmg_contents; then
    inspection_status=1
  fi
  detach_status=0
  if ! detach_dmg_verification_mount; then
    detach_status=1
  fi
  [ "$detach_status" -eq 0 ] || return 1
  [ "$inspection_status" -eq 0 ] || return 1
}
```

Implement `verify_retina_tiff` with `tiffutil -info`: require exactly two image-directory records,
one 660 × 360 and one 1320 × 720, and reject any missing, duplicate, or additional frame. Exercise
the parser with the fake `tiffutil` plus one real renderer/`tiffutil -cathidpicheck` fixture in the
focused layout test.

Keep all inspection returns inside `inspect_generated_dmg_contents`; the outer function detaches
exactly once and treats detach failure as fatal even when inspection already failed. Call
`detach_dmg_verification_mount` at the start of `cleanup_release` only as a final safety net; cleanup
may log a second failure, but no caller may treat that candidate as verified.

The normal detach path retries three times. If all clean attempts fail, force-detach only to release
the mount resource and still return failure, so that image can never advance to signing. Tests set
the test-only retry delay to zero.

- [ ] **Step 5: Replace basic hdiutil creation with dmgbuild**

At the start of `phase_create_dmg`, require `$stage/dmg-root` not to exist, create it, reject a
symlink, and resolve it physically to exactly `$stage/dmg-root` before `ditto` copies anything. This
makes a pre-created redirect fail without touching its target. Keep the staged app audit, remove the
pre-created Applications symlink, then require the copied app to be a regular non-symlink directory
whose physical parent is that verified root. Reject anything else as outside the unique release
stage before invoking the generator. Then use:

```bash
dmg="$stage/Loqui.dmg"
settings="$release_root_dir/build/darwin/dmg/settings.py"
dmg_app="$dmg_root/Loqui.app"
[ -d "$dmg_app" ] && [ ! -L "$dmg_app" ] || {
  die "staged DMG app is not a regular directory"
  return 1
}
dmg_app_parent="$(cd "${dmg_app%/*}" && pwd -P)" || return 1
[ "$dmg_app_parent" = "$stage/dmg-root" ] || {
  die "staged DMG app resolves outside release stage"
  return 1
}
if ! "$dmgbuild_python" -m dmgbuild \
    -s "$settings" \
    -D "app=$dmg_app" \
    -D "assets=$release_root_dir/build/darwin/dmg" \
    Loqui "$dmg"; then
  die "could not create styled DMG"
  return 1
fi
if [ ! -f "$dmg" ] || [ -L "$dmg" ]; then
  die "dmgbuild did not create a regular DMG"
  return 1
fi
hdiutil verify "$dmg" || {
  die "generated DMG failed hdiutil verification"
  return 1
}
verify_generated_dmg_contents || return 1
```

Do not change later phase ordering.

- [ ] **Step 6: Run RED/GREEN failure matrix**

```bash
./scripts/tests/release-macos-test.sh
./scripts/tests/dmg-layout-test.sh
./scripts/tests/dmg-integration-test.sh
```

Expected: all three PASS; the integration test proves the real locked module, settings scoping,
HiDPI assembly, and Finder metadata before any credentialed release. Temporarily change
`Applications` to `/Wrong` in the fake mounted fixture, confirm the focused release test fails with
`generated DMG Applications link is invalid`, restore, and rerun to PASS.

- [ ] **Step 7: Stage Task 3 without committing**

```bash
git add scripts/release-macos.sh scripts/tests/release-macos-test.sh \
  scripts/tests/dmg-integration-test.sh
git diff --cached --check
```

Expected: no whitespace errors; do not commit.

---

### Task 4: Taskfile and protected GitHub workflow wiring

**Files:**
- Modify: `build/darwin/Taskfile.yml:177-181`
- Modify: `Taskfile.yml:31-48`
- Modify: `.github/workflows/release.yml:110-190`
- Modify: `scripts/tests/github-release-workflow-test.sh:55-140`

**Interfaces:**
- Consumes: `scripts/setup-dmgbuild.sh` and the release script's required
  `LOQUI_DMGBUILD_PYTHON`.
- Produces: local and CI release entry points that prepare the same digest-addressed environment
  before release execution; CI exports its exact Python path through `GITHUB_ENV` before Apple
  credential import. Root `release:macos` continues to delegate to `darwin:release`.

- [ ] **Step 1: Write failing Taskfile/workflow assertions**

Update `scripts/tests/github-release-workflow-test.sh` to require:

```bash
assert_contains "$release_job" 'name: Prepare pinned DMG builder'
assert_contains "$release_job" './scripts/setup-dmgbuild.sh'
assert_contains "$release_job" 'LOQUI_DMGBUILD_PYTHON='
assert_contains "$release_job" '>> "$GITHUB_ENV"'
assert_contains "$release_job" 'name: Verify real DMG builder integration'
assert_contains "$release_job" './scripts/tests/dmg-integration-test.sh'
setup_line="$(line_number 'name: Prepare pinned DMG builder')"
integration_line="$(line_number 'name: Verify real DMG builder integration')"
[ "$setup_line" -lt "$credentials_line" ] \
  || fail 'DMG builder setup is not before protected credentials'
[ "$setup_line" -lt "$integration_line" ] && [ "$integration_line" -lt "$credentials_line" ] \
  || fail 'real DMG integration is not before protected credentials'
[ "$setup_line" -lt "$release_line" ] \
  || fail 'DMG builder setup is not before release build'
```

Add Taskfile contract assertions that root `release:macos` delegates to `darwin:release`, and that
the included `release` task captures the setup script output into `LOQUI_DMGBUILD_PYTHON` before
invoking `release-macos.sh`. Require
`test:macos-release` to include all three new focused/integration test scripts exactly once.

- [ ] **Step 2: Run workflow contract and confirm RED**

```bash
./scripts/tests/github-release-workflow-test.sh
./scripts/task.sh test:macos-release
```

Expected: the workflow test fails because no setup/integration steps exist; the macOS release gate
does not yet run all three new tests.

- [ ] **Step 3: Wire local Task targets**

Change `build/darwin/Taskfile.yml` `release` to:

```yaml
  release:
    summary: Builds, signs, notarizes, staples, and verifies an arm64 Developer ID DMG
    cmds:
      - |
        set -e
        dmgbuild_python="${LOQUI_DMGBUILD_PYTHON:-$(./scripts/setup-dmgbuild.sh)}"
        [ -n "$dmgbuild_python" ]
        LOQUI_DMGBUILD_PYTHON="$dmgbuild_python" ./scripts/release-macos.sh

  render:dmg-background:
    summary: Regenerates both reviewed DMG background scales and their digests
    cmds:
      - swift build/darwin/dmg/render-background.swift build/darwin/dmg
      - cd build/darwin/dmg && shasum -a 256 background.png background@2x.png > background.sha256
```

`darwin:render:dmg-background` is an explicit maintainer action: it is not a dependency of release
or CI. Review the two rendered assets and digest diff together after running it.

Add these before `release-macos-test.sh` in `Taskfile.yml`:

```yaml
      - ./scripts/tests/setup-dmgbuild-test.sh
      - ./scripts/tests/dmg-layout-test.sh
      - ./scripts/tests/dmg-integration-test.sh
```

- [ ] **Step 4: Wire CI before credential import**

Add after `Vendor pinned speech SDK` and before `Revalidate immutable release state`:

```yaml
      - name: Prepare pinned DMG builder
        run: |
          dmgbuild_python="$(./scripts/setup-dmgbuild.sh)"
          echo "LOQUI_DMGBUILD_PYTHON=$dmgbuild_python" >> "$GITHUB_ENV"

      - name: Verify real DMG builder integration
        run: ./scripts/tests/dmg-integration-test.sh
```

Do not pass secrets or grant additional permissions to this step. Assert the later
`./scripts/task.sh release:macos` step occurs after this export and therefore receives the variable;
the integration step exercises the real generator before credentials, and the release Task target
reuses the digest-addressed environment without another package download.

- [ ] **Step 5: Run focused and full packaging contracts**

```bash
./scripts/tests/github-release-workflow-test.sh
./scripts/task.sh test:macos-release
```

Expected: both exit 0 and every macOS release subtest reports PASS.

- [ ] **Step 6: Stage Task 4 without committing**

```bash
git add Taskfile.yml build/darwin/Taskfile.yml .github/workflows/release.yml \
  scripts/tests/github-release-workflow-test.sh
git diff --cached --check
```

Expected: no whitespace errors; do not commit.

---

### Task 5: Cross-review, simplification, and automated verification

**Files:**
- Modify only files already in the feature diff when resolving verified findings.
- Update: `.workflow/state.md`

**Interfaces:**
- Consumes: complete staged implementation from Tasks 1-4.
- Produces: one clean cross-engine code-review pass, a behavior-preserving simplification pass, and
  a green full repository check.

- [ ] **Step 1: Stage the complete intended diff for review**

Stage the approved durable design and plan alongside all Task 1-4 outputs:

```bash
git add docs/superpowers/specs/2026-08-11-intuitive-dmg-installer-design.md \
  docs/superpowers/plans/2026-08-11-intuitive-dmg-installer.md
git status --short
git diff --cached --check
```

Expected: only intended feature files are staged and no whitespace errors exist. Do not commit.

- [ ] **Step 2: Run the required cross-engine code review**

Use the repository `review` skill with a reviewer engine different from the active driver. Provide
the approved spec, this plan, and:

```bash
git diff --cached --stat
git diff --cached
```

Classify findings P0-P3. Resolve every P0/P1/P2 and record each iteration in
`.workflow/state.md`. Repeat until one complete pass is clean.

- [ ] **Step 3: Re-run focused tests after every finding fix**

```bash
./scripts/tests/setup-dmgbuild-test.sh
./scripts/tests/dmg-layout-test.sh
./scripts/tests/dmg-integration-test.sh
./scripts/tests/release-macos-test.sh
./scripts/tests/github-release-workflow-test.sh
```

Expected: all PASS.

- [ ] **Step 4: Run the `simplify` skill**

Limit cleanup to the changed shell/Python/Swift packaging paths. Preserve exact diagnostics,
coordinates, release phase ordering, and all test coverage. Do not refactor unrelated release code.

- [ ] **Step 5: Prove the full automated gate**

```bash
./scripts/task.sh check
git diff --cached --check
```

Expected: exit 0; Go tests, vet, frontend typecheck, and every macOS release test pass. Linker
warnings already present in the project are not failures.

- [ ] **Step 6: Update state for automated gates**

Check `Tests written (TDD) and passing` and `Code review clean — no open P0/P1/P2` only after the
fresh evidence above. Leave E2E and final-state boxes open.

---

### Task 6: Real local DMG journey, durable docs, and ship gate

**Files:**
- Create: `docs/e2e/use-cases/intuitive-dmg-installer.md`
- Create: `docs/e2e/reports/2026-08-11-intuitive-dmg-installer.md`
- Modify: `docs/CHANGELOG.md:1-15`
- Modify: `.workflow/state.md`

**Interfaces:**
- Consumes: reviewed green implementation, configured local Developer ID/notary profile, current
  `build/config.yml` version.
- Produces: real local verified DMG, `VERDICT: PASS` evidence, completed standard ship gate, no
  remote release mutation.

- [ ] **Step 1: Write the E2E use case before running it**

Define these scenarios in `docs/e2e/use-cases/intuitive-dmg-installer.md`:

1. real local release build completes through existing signature/notary/staple/Gatekeeper checks;
2. read-only mount exposes exactly the intended visible and hidden root contract;
3. Finder opens at 660 × 384 with the complete 660 × 360 bilingual composition, arrow, positions,
   icon scale, complete labels, and no visible TIFF; user-level path-strip/vertical-scroll chrome is
   permitted by the owner's observed approval;
4. dragging `Loqui.app` to the Applications link performs a normal Finder copy;
5. no tag, GitHub Release, version, or public asset changes.

- [ ] **Step 2: Build the real local DMG without publishing remotely**

Record the current version/tag/release state first:

```bash
git diff -- build/config.yml
git tag --list
gh release view v0.1.0 --json tagName,targetCommitish,isDraft,isPrerelease,assets
./scripts/task.sh release:macos
```

Expected: the release task exits 0 and writes the canonical local DMG under `bin/release`. Do not
run `github-release.sh publish`, `gh release create`, or any tag command.

- [ ] **Step 3: Inspect the generated image structurally**

Use a unique mount:

```bash
e2e_root="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dmg-e2e.XXXXXX")"
mkdir "$e2e_root/mount"
hdiutil verify bin/release/Loqui-0.1.0-macos-arm64.dmg
hdiutil attach -readonly -nobrowse -mountpoint "$e2e_root/mount" \
  bin/release/Loqui-0.1.0-macos-arm64.dmg
find "$e2e_root/mount" -mindepth 1 -maxdepth 1 ! -name '.*' -print | LC_ALL=C sort
[ -f "$e2e_root/mount/.DS_Store" ]
[ -f "$e2e_root/mount/.background.tiff" ]
tiffutil -info "$e2e_root/mount/.background.tiff"
readlink "$e2e_root/mount/Applications"
codesign --verify --deep --strict "$e2e_root/mount/Loqui.app"
```

Expected visible items: only `Applications` and `Loqui.app`. Expected hidden files:
`.DS_Store` and `.background.tiff`; ignore normal mount-service entries such as `.fseventsd` or
`.Trashes` rather than treating them as presentation inputs. Expected TIFF frames: exactly
660 × 360 and 1320 × 720. Expected link target: `/Applications`.

- [ ] **Step 4: Exercise the Finder journey**

Open `$e2e_root/mount` in Finder. Capture a screenshot and record:

- measured compact window;
- measured 660 × 384 Finder window, confirming the full 660 × 360 background and both labels are
  not clipped. Record whether Finder retains its user-level path strip/vertical-scroll indicator;
  this is accepted when the TIFF remains invisible and the composition is complete;
- both exact instruction lines;
- purple left-to-right arrow;
- Loqui at the left and Applications at the right;
- exact visible icon labels `Loqui` and `Applications`, plus normal drag behavior;
- no toolbar/sidebar/status/tab UI; record any user-level path strip rather than overriding the
  person's global Finder preference.

Perform the drag to `Applications` only after checking whether `/Applications/Loqui.app` already
exists; if it exists, use a controlled writable destination for the drag-copy observation rather
than replacing an installed app without explicit approval.

- [ ] **Step 5: Detach and confirm no remote publication**

```bash
hdiutil detach "$e2e_root/mount"
gh release view v0.1.0 --json tagName,targetCommitish,isDraft,isPrerelease,assets
git diff -- build/config.yml
git status --short --branch
```

Expected: mount detached; remote release JSON and version file unchanged; only intended feature
files appear in Git status.

- [ ] **Step 6: Write the evidence report and changelog**

Run the `verify-e2e` skill and write
`docs/e2e/reports/2026-08-11-intuitive-dmg-installer.md` with:

```text
VERDICT: PASS
```

Include commands, exit codes, tool/macOS versions, observed Finder state, screenshot path or owner
confirmation, drag result, and proof that no remote publication occurred.

Add the newest `docs/CHANGELOG.md` entry:

```markdown
## Intuitive bilingual DMG installer — 2026-08-11

- **Installing Loqui now explains itself.** The DMG opens in a compact branded Finder window with
  English and Spanish drag-to-Applications instructions, a clear arrow, and fixed real app/folder
  icons.
- **The presentation is reproducible and release-safe.** A hash-locked headless builder creates the
  Finder metadata, while the generated image is mounted and its layout, copied app signature, and
  designated requirements are verified before signing or notarization continues.
```

- [ ] **Step 7: Run final verification and close gates**

```bash
git add docs/e2e/use-cases/intuitive-dmg-installer.md \
  docs/e2e/reports/2026-08-11-intuitive-dmg-installer.md \
  docs/CHANGELOG.md
./scripts/task.sh check
git diff --cached --check
```

Update `.workflow/state.md` with the E2E report, check `E2E verified` and `State updated`, then run:

```bash
sh shared/scripts/check-gates.sh
```

Expected: all six standard boxes checked.

- [ ] **Step 8: Hand off to finish-branch**

Use `finish-branch` to inspect the intended diff, create one commit without coauthor trailers, push
`feat/intuitive-dmg-installer`, and open a PR describing:

- the bilingual Finder experience;
- pinned/headless generation;
- mounted-image signature/layout verification;
- automated and real local evidence;
- explicit confirmation that no new tag or GitHub Release was published.

Do not merge until the owner requests it.
