# E2E — Movable settings window

VERDICT: PASS

- **Feature:** restore click-and-hold movement for the main Loqui settings window.
- **Branch:** `fix/window-drag-region`.
- **Run:** 2026-08-12T17:59-18:02-05:00.
- **Build:** `bin/loqui.dev.app`, rebuilt from this branch with
  `./scripts/task.sh darwin:build DEV=true`, packaged through the native development task, and
  launched as the signed Wails macOS application.

## Why this is not Playwright

The behavior under test is movement of the native macOS host window. Loqui's UI runs in a Wails
WKWebView with generated Go bindings, and a standalone browser can neither move that host window nor
exercise its native lifecycle. The test therefore used macOS accessibility only to read geometry and
Core Graphics to emit the same primary-button pointer events as the user journey. The app itself was
not instrumented or given a test-only code path.

## WDR-UI-01 — move the settings window from a drag region: PASS

The running development window began at position `(865, 392)` with size `899 × 639`. A primary-button
drag on an empty draggable area moved the pointer by `(+150, +100)`. Native geometry after release was
`(1015, 492, 899, 639)`: the window moved by the exact requested offset and retained its exact size.

A second geometry read before shutdown returned the same `(1015, 492, 899, 639)`, confirming that the
post-drag state remained stable for the active session. Screenshot evidence is retained locally in
the ignored files `.workflow/e2e-run/window-drag-before.png` and
`.workflow/e2e-run/window-drag-after.png`.

The first diagnostic pointer gesture targeted the narrow boundary between the native title bar and
the web content and did not move the window. The accepted run used the unambiguously empty middle of
the sidebar, whose Wails drag value is inherited directly; the pointer endpoint and exact position
delta confirm that macOS received and applied the gesture.

## WDR-UI-02 — sidebar controls remain interactive: PASS

After the move, a normal primary-button click selected `History`. The native app logged
`UI-VIEW map[view:historial]`, and the local screenshot visibly shows the History navigation item and
view active. Geometry immediately after the click remained `(1015, 492, 899, 639)`, so the
interactive descendant handled the click without initiating another window drag or resize.

## Cleanup

The development process received a graceful interrupt after the final geometry read. The installed
`/Applications/Loqui.app` was not changed; it had only been closed to avoid Wails' single-instance
guard and was reopened successfully afterward from its original path.
