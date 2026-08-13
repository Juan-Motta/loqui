# Wails window drag regions must use the Wails CSS contract

## Symptom

The main macOS settings window could be resized but could not be moved by holding and dragging its
top or empty sidebar regions. The markup already appeared to declare drag handles, so the failure
looked like a native-window configuration problem.

## Root cause

The page declared Electron's `-webkit-app-region` property. Loqui is hosted by Wails v3, whose pinned
frontend runtime reads the inherited CSS custom property `--wails-draggable` from the pointer-event
target instead. The Electron declarations were valid CSS but had no effect on Wails' native window.

Because CSS custom properties inherit, a draggable container also makes its descendants draggable
unless interactive children explicitly override the value.

## Fix

- Changed the existing empty top, sidebar, and onboarding strips to
  `--wails-draggable: drag`.
- Changed sidebar navigation, footer links, and the Home activity link to
  `--wails-draggable: no-drag` so their clicks remain interactive.
- Left native window sizing, title-bar configuration, and resizing behavior unchanged.
- Added a frontend contract test that rejects Electron drag declarations and requires both the Wails
  drag regions and their interactive exclusions.

## Verification

The focused regression test failed against the Electron property before the production edit and
passed afterward. In the signed native development app, a real primary-button gesture moved the
window by exactly `(+150, +100)` while preserving its `899 × 639` size. Clicking History afterward
selected that view without changing the geometry. See
`docs/e2e/reports/2026-08-12-window-drag-region.md`.

## Reusable rule

For Wails v3 frameless or inset-titlebar windows, define drag behavior with
`--wails-draggable`, not Electron's `-webkit-app-region`. Treat the property as inherited: every
interactive descendant inside a draggable ancestor needs an explicit `no-drag` override.
