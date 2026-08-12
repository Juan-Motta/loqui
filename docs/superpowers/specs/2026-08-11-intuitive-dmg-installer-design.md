# Intuitive bilingual DMG installer — design

- **Status:** approved in conversation on 2026-08-11
- **Owner:** Juan Andres Lopez Motta
- **Driver:** Codex
- **Scope:** prepare and verify the DMG generator locally; do not publish a new version

## Problem

Loqui's release image currently contains `Loqui.app` and an `/Applications` symlink, but
`phase_create_dmg` passes that folder directly to `hdiutil create`. The resulting image has no
repository-owned Finder window settings, icon positions, background, arrow, or instruction. A user
can infer the drag-and-drop installation pattern, but the image does not clearly tell them what to
do and Finder may present the two items in a large, inconsistent window.

Apple documents the source-folder plus `hdiutil` flow used today, but it does not provide a
declarative presentation layer. `dmgbuild` fills that gap by writing Finder metadata without
automating Finder and supports fixed icon locations, window controls, backgrounds, symlinks, and
compressed UDIF output.

Sources:

- [Apple: Packaging Mac software for distribution](https://developer.apple.com/documentation/xcode/packaging-mac-software-for-distribution)
- [`dmgbuild` settings](https://dmgbuild.readthedocs.io/en/latest/settings.html)
- [`dmgbuild` implementation](https://github.com/dmgbuild/dmgbuild/blob/v1.6.7/src/dmgbuild/core.py)

## Goals

1. Make the drag-to-Applications action immediately understandable when the DMG opens.
2. Show the instruction in English and Spanish.
3. Use a compact, deterministic Finder window that matches Loqui's restrained visual identity.
4. Keep `Loqui.app` and `Applications` as real Finder items; the background must not fake either
   icon.
5. Produce the same visual result locally and on GitHub's macOS release runner without Finder or
   AppleScript automation.
6. Preserve the existing app audit, Developer ID signing, designated-requirement comparison,
   notarization, stapling, Gatekeeper, evidence, and atomic-publication contracts.
7. Build and inspect a real local DMG without creating a tag or GitHub Release.

## Non-goals

- Publishing `v0.1.1` or replacing the immutable `v0.1.0` public release.
- Adding an installer wizard, package installer, copy button, or automatic copy into
  `/Applications`.
- Adding a DMG license-acceptance dialog; the repository MIT license does not require click-through
  acceptance.
- Redesigning the Loqui app icon or adding a custom mounted-volume icon.
- Changing the app bundle, runtime code, permissions, minimum macOS version, release filename, or
  release approval model.

## User experience

Opening the DMG presents one icon-view Finder window with these properties:

- Finder window bounds: **660 × 384 points**, containing the complete **660 × 360 point**
  background composition;
- light off-white/lavender Loqui background;
- exact centered instructions:
  - `Drag Loqui to Applications`
  - `Arrastra Loqui a Aplicaciones`
- a prominent Loqui-purple arrow pointing left-to-right;
- the real `Loqui.app` icon at **(160, 215)**;
- the real `Applications` symlink icon at **(500, 215)**;
- icon size **128 points**, label position below, label text size **14 points**;
- visible Finder labels **Loqui** and **Applications**;
- icon view with automatic arrangement disabled;
- toolbar, sidebar, status bar, and tab view hidden; the DMG requests a hidden path bar, while a
  user-level Finder preference may keep the bottom path strip and its inactive vertical scrollbar;
- no visible files other than `Loqui.app` and `Applications`.

The generated `.background.tiff` is a Finder-invisible implementation resource, not an icon-view
item. The settings use dmgbuild 1.6.7's supported `hide = [".background.tiff"]` contract, which
applies Finder's `kIsInvisible` flag with `SetFile -a V`; they deliberately create no `Iloc` record
for that resource. The initial Finder view must not show the TIFF or clip either visible label.
The owner explicitly accepted Finder retaining its user-level path strip/vertical-scroll indicator
on 2026-08-12; that OS chrome is not treated as a presentation failure when the composition remains
complete and the hidden resource stays invisible.

The committed background has a 660 × 360 1× PNG and a 1320 × 720 `@2x` sibling. `dmgbuild`
detects scaled siblings and combines them into a multi-resolution TIFF with macOS `tiffutil`, so
the instruction and arrow remain crisp on Retina displays.

This behavior is source-verified in the pinned 1.6.7 `core.py`: it discovers matching `@Nx`
siblings, runs `/usr/bin/tiffutil -cathidpicheck`, and copies the result as `.background.tiff`.
The credential-free real-generator integration binds that behavior. If a future pinned version
breaks it, generation must fail closed; the documented contingency is to review and commit one
pre-built multi-resolution TIFF and point `background` to it explicitly, never to silently fall
back to the 1× PNG.

Finder normally displays the `Loqui.app` application bundle as **Loqui**, so the settings leave
`hide_extensions` empty. In dmgbuild 1.6.7, forcing the legacy extension-hidden bit on the app uses
`SetFile -a E` after the signed bundle has been copied; that mutation invalidates strict
code-signature verification. The mounted-image integration therefore requires both no
`com.apple.FinderInfo` on the app and `codesign --verify --deep --strict` success, while separately
requiring the generated background's FinderInfo to carry `kIsInvisible` (`0x4000`).

The arrow is one filled Loqui-purple polygon: a 12-point shaft from x=270 through x=357 and a clean
head ending at x=397, centered at y=145. A single filled shape avoids the overlap visible where the
previous stroked shaft met its separate triangular head.

## Approaches considered

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness / user risk |
| --- | --- | --- | --- | --- | --- |
| **Pinned `dmgbuild` settings — chosen** | Medium: one packaging dependency, settings, and two assets | Low: changes only DMG creation before existing signing | High: restore the old `hdiutil create` call | Medium: contract tests plus one real mount | Low: deterministic metadata without Finder automation |
| `create-dmg` shell tool | Medium: external shell tool and Finder customization flow | Medium: more interactions with mounted images and desktop services | High | Medium | Medium: Finder automation is more sensitive to runner/session state |
| Committed `.DS_Store` template | Low at build time, high when editing | Low | Medium: opaque binary must be regenerated | Fast initially, slow to diagnose | Medium/high: names or coordinates can silently drift |

The chosen approach is the smallest option that provides maintainable, reviewable layout settings
and deterministic headless generation. The new dependency is acceptable only when its version and
resolved artifacts are locked and verified before release credentials are imported.

## Architecture

### Presentation assets

`build/darwin/dmg/` owns the installer presentation:

- `settings.py` — declarative `dmgbuild` settings and validation of the required `app` define;
- `verify-ds-store.py` — semantic verifier for the mounted image's Finder metadata;
- `render-background.swift` — repository-owned AppKit renderer for the approved exact composition;
- `background.png` — the approved 660 × 360 1× composition;
- `background@2x.png` — the exact 1320 × 720 Retina composition;
- `background.sha256` — reviewed SHA-256 digests for both committed release inputs;
- `requirements.in` — the human-reviewed direct requirement `dmgbuild==1.6.7`;
- `requirements.txt` — all Python packaging dependencies pinned with SHA-256 hashes, including
  `dmgbuild==1.6.7` and its resolved transitive dependencies.

The raster files are durable release inputs, not generated opportunistically during a protected
release. The renderer uses only AppKit/Foundation already supplied by the required Xcode toolchain;
it writes both scales from the same point-based constants. Intentional visual changes use an
explicit local regeneration task, update both scales and their reviewed digests together, and pass
dimension, channel, content, and visual E2E checks. CI does not compare a fresh AppKit render
byte-for-byte because font rasterization, color management, and PNG encoding can differ across
macOS/Xcode versions.

### Tool setup

Add `scripts/setup-dmgbuild.sh` as the single setup entry point. It:

1. requires Python 3.10 or newer and fails with a direct diagnostic before package resolution;
2. creates an isolated virtual environment under ignored
   `.task/tools/dmgbuild-<requirements-sha256>`;
3. installs with `pip --require-hashes` from the repository lock file;
4. records the lock-file digest in the environment directory name;
5. reuses that digest-addressed environment only when
   `importlib.metadata.version("dmgbuild") == "1.6.7"` also matches;
6. prints the absolute virtual-environment Python path for callers to invoke with
   `-m dmgbuild`.

The GitHub release workflow runs this setup in a dedicated step before importing the Developer ID
certificate or exposing notarization credentials. Local release tasks call the same setup entry
point. Tests inject a fake executable and never access the network.

### DMG creation

`scripts/release-macos.sh` keeps its current signed-app staging and verification, then changes only
the final image assembly inside `phase_create_dmg`:

1. copy the already signed app to the unique physical staging root with `ditto`;
2. re-audit the staged copy, run strict code-signature verification, capture its designated
   requirements, and compare them with the pre-copy evidence exactly as today;
3. validate the two backgrounds, their reviewed hashes and dimensions, the settings file, and the
   exact installed `dmgbuild` package version;
4. invoke the pinned executable with `-s settings.py`,
   `-D app=$stage/dmg-root/Loqui.app`,
   `-D assets=$repo/build/darwin/dmg`, volume name `Loqui`, and output `$stage/Loqui.dmg`;
5. require one regular, non-symlink output file and run `hdiutil verify`;
6. mount the image read-only at a unique physical directory inside the stage, require only
   `Loqui.app` and `Applications` as visible root entries, require the symlink target
   `/Applications`, require `.DS_Store` plus a two-frame multi-resolution background whose frames
   are 660 × 360 and 1320 × 720, and parse `.DS_Store` with the hash-locked `ds_store` dependency to
   assert window bounds, hidden chrome, icon-view settings, icon coordinates, and background mode;
7. re-run the production app audit and strict code-signature verification against the app inside
   the mounted image, capture its designated requirements as
   `evidence-work/designated-requirements-dmg.txt`, and compare them to the original signed-app
   capture;
8. detach on every success or failure path before moving to DMG signing.

`settings.py` passes the staged app as `Loqui.app`, creates `Applications -> /Applications`, uses
HFS+ and `UDZO` to preserve the established compatibility/compression contract, and writes the exact
window/background/icon settings above. `dmgbuild` uses `/usr/bin/ditto` for the app copy; the
post-generation mounted-image audit independently proves that the copied bundle and its signatures
survived intact before any DMG signing or notarization occurs.

All later phases remain in their existing order:

1. sign the DMG;
2. verify image and signature;
3. submit only the outer DMG to Apple;
4. inspect the notary log and ticket coverage;
5. staple and validate only the outer DMG;
6. run Gatekeeper checks;
7. publish the accepted local artifact atomically.

No Python code or packaging dependency enters `Loqui.app` or the published image.

## Data flow

```text
locked requirements ─▶ isolated dmgbuild tool
                              │
signed + audited Loqui.app ───┼──▶ settings.py + 1×/2× backgrounds
                              │                 │
                              └──────────────▶ Loqui.dmg
                                                  │
                            existing sign/notarize/staple/audit flow
                                                  │
                                  local bin/release artifact only
```

## Failure behavior

- Missing or pre-3.10 Python, an unavailable/hash-mismatched dependency, or a wrong `dmgbuild`
  version fails before release credentials are imported in CI and before release phases locally.
- A missing, unreadable, symlinked, or incorrectly sized background fails before DMG creation.
- Missing/invalid settings or an app path outside the unique release stage fails before DMG
  creation.
- A nonzero `dmgbuild` exit, absent output, symlink output, invalid image, malformed visible-root
  layout, wrong Applications target, missing presentation metadata, copied-app audit failure, or
  designated-requirement mismatch stops before DMG signing and notarization.
- Read-only verification mounts are always detached; a clean-detach failure is itself fatal and
  leaves no candidate eligible for signing.
- Cleanup continues to accept only the unique physical staging directory and the existing
  repository-owned hidden publication candidates.
- Every failure preserves the last successfully published local DMG and creates no tag, GitHub
  Release, or partial public filename.

## Test strategy

Implementation follows red → green → refactor.

### Contract tests

Extend `scripts/tests/release-macos-test.sh` and add a focused settings/layout test to prove:

- preflight rejects a missing, relative, or non-executable Python path, an unreadable package
  version, or an installed `dmgbuild` package version other than `1.6.7`;
- `phase_create_dmg` invokes exactly the repository settings, staged app, volume name, and unique
  output path;
- settings resolve the app only from the injected define and reject an absent/non-absolute app;
- HFS+, UDZO, window rect, hidden chrome, icon view, icon size, labels, icon coordinates,
  Applications target, and background path equal the approved constants;
- both PNGs exist, are regular non-symlink files, match their reviewed SHA-256 digests, have exact
  660 × 360 / 1320 × 720 dimensions and expected channel layout, and represent the same aspect
  ratio; wrong-sized release inputs fail before generation;
- a generator failure, missing output, symlink output, or failed image verification propagates and
  prevents later sign/notary phases;
- a fake mounted image must expose exactly the intended visible items, symlink, two-frame 1×/2×
  presentation metadata, semantic `.DS_Store` layout values, and matching copied-app designated
  requirements; every mismatch fails before signing;
- attach, inspection, audit, signature, comparison, and detach failures all propagate, including
  the combined case where inspection and detach both fail;
- one credential-free integration test invokes the real locked `dmgbuild`, creates and mounts a
  throwaway DMG around a stub `Loqui.app`, and runs the same visible-root, TIFF, and `.DS_Store`
  assertions before the protected release path;
- the production release task and GitHub workflow prepare and integration-test the locked tool
  before release execution or credential import.

The first focused run must fail for the missing presentation behavior before implementation begins.

### Repository verification

Run the full `./scripts/task.sh check` gate, including Go tests, vet, frontend typecheck, macOS bundle,
signing, release, GitHub workflow, and documentation contract tests.

### Real local journey

Without changing `build/config.yml`, creating a Git tag, or publishing a GitHub Release:

1. build one local DMG through the real release entry point;
2. verify the image and any produced signature/ticket with the existing release checks;
3. mount it read-only with browsing disabled for structural inspection;
4. require exactly `Loqui.app` and `Applications` as visible root items, and require the latter to
   resolve to `/Applications`;
5. confirm the hidden Finder metadata and multi-resolution background exist;
6. open the mounted volume in Finder and capture evidence that the compact window, measured content
   area, both exact instruction lines, arrow, icon positions, labels, and requested chrome settings match the
   design; the 660 × 384 window must show the complete 660 × 360 composition without the hidden
   TIFF or clipped icon labels. A user-level path strip/vertical-scroll indicator is permitted by
   the owner's observed approval;
7. drag the real Loqui icon to the Applications link or a controlled test destination and confirm
   the app copy behaves normally;
8. detach the image and record the journey in a `VERDICT: PASS` E2E report.

## Acceptance criteria

1. A generated DMG opens in a 660 × 384 Finder window with the approved 660 × 360 bilingual
   drag-to-Applications composition, complete labels, and no visible TIFF.
2. The instruction text is exact, legible, and crisp on a Retina display.
3. `Loqui.app` and `Applications` are real 128-point Finder icons at the approved positions.
4. Dragging Loqui to the Applications link performs the normal Finder copy.
5. No Finder chrome or unintended visible root item distracts from the action.
6. Local and GitHub release paths use the same pinned settings and assets without Finder or
   AppleScript automation.
7. The existing app/DMG signing, designated requirements, notarization, ticket, stapling,
   Gatekeeper, evidence, cleanup, and atomic-publication tests remain green.
8. A real local DMG passes structural and visual E2E verification.
9. No tag, GitHub Release, public artifact, or version change occurs in this task.

## Documentation and shipping

- Add a newest-first `docs/CHANGELOG.md` entry only after implementation and verification.
- Add the real journey report under `docs/e2e/reports/` and update the corresponding use case if the
  established developer-ID release journey needs a presentation assertion.
- Update the maintainer release prerequisites only if the locked setup introduces a user-visible
  command outside the existing release task.
- Ship one reviewed feature branch through a PR after all standard-profile gates are checked.
