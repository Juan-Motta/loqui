# macOS automatic updates implementation plan

## Goal

Add safe, opt-out automatic update checks to Loqui using the updater already bundled by the pinned
Wails version, while retaining the DMG as the manual installer and publishing a notarized ZIP for
self-update.

## Constraints

- Work only on `feat/automatic-updates`; do not modify `main` directly.
- Reuse the pinned Wails updater; do not add Sparkle or a second updater runtime dependency.
- Runtime version comes from the packaged app's own Info.plist and keeps the existing development
  fallback.
- Automatic behavior is check-only until the user confirms install and restart.
- Existing releases are immutable; future releases add a ZIP/checksum pair without removing the DMG
  pair.
- Do not expose credentials, update tokens, or unsanitized release logs.

## Approach comparison

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness / user risk |
| --- | --- | --- | --- | --- | --- |
| Wails GitHub provider + ZIP + SHA-256 | Medium | Medium | High | Fast | **Selected:** matches the pinned framework and current GitHub release flow; explicit confirmation prevents surprise restarts |
| Wails endpoint/AppCast + Ed25519 manifest | High | Medium/high | Medium | Slow | Better independent authenticity and rollout control, but adds hosting/key operations not needed for the first iteration |
| Manual browser check | Low | Low | High | Fast | Does not deliver automatic update discovery; rejected as incomplete |

## Work units

### A. Product and state model

- Add `autoUpdateChecks` to `internal/store/config.go` with default `true`.
- Add the settings write method and generated binding updates.
- Add locale strings for update actions/statuses in `internal/i18n/en.go` and the Spanish catalogue.

### B. Updater service and lifecycle

- Add a small runtime-version helper shared by About and updater initialization.
- Add `internal/app/updater.go` (or the smallest existing service boundary) that configures Wails
  `application.App.Updater` with `CurrentVersion`, `updater.WindowNone`, a GitHub provider, a
  ZIP-only asset matcher, and the `SHA256SUMS` checksum asset. Do not set Wails `CheckInterval`:
  the pinned implementation invokes `CheckAndInstall` there, which would stage an update on a
  background tick without user confirmation.
- Expose manual check/install methods and status events to the frontend; map updater errors to
  localized, non-sensitive states.
- Wire tray and app lifecycle without blocking startup; use a cancellable timer/ticker and stop it
  on application shutdown.
- Tests first: provider configuration, ZIP matcher, version fallback, state/error mapping, initial
  check timing, preference-off behavior, and cancellation.

### C. UI

- Add the automatic-check toggle to the existing System settings surface.
- Add an About update row/button using existing `about.ts` and event patterns.
- Add a tray “Check for updates…” entry that delegates to the same bound method.
- Keep both EN and ES text in the catalogue; no hard-coded user-facing strings.
- Tests first: rendered labels, disabled/loading states, no-update, available, failure, cancel, and
  persisted toggle behavior.

### D. Release artifacts

- Extend `scripts/release-macos.sh` to produce a clean ZIP containing exactly `Loqui.app`, with the
  signed/notarized/stapled app inside, and a deterministic updater ZIP filename.
- Preserve DMG creation, signing, notarization, stapling, audit, and evidence checks.
- Generate the ZIP SHA-256 alongside the existing DMG SHA-256; keep checksum contents deterministic.
- Extend `scripts/github-release.sh`, `.github/workflows/release.yml`, and contract tests so
  preflight/publication require both artifact pairs and reject missing/extra assets.
- Tests first: artifact naming/containment, checksum verification, DMG preservation, publication
  asset list, and failure-before-publication.

## Edge cases

- Development builds without a bundle use the existing fallback version and do not attempt a
  production update install.
- A release containing only a DMG or a ZIP with the wrong architecture is unavailable.
- A malformed checksum, network timeout, cancellation, or failed restart leaves the current app
  running and does not corrupt its bundle.
- `autoUpdateChecks=false` suppresses background checks but does not remove manual checks.
- The first updater-enabled version requires a one-time DMG installation from users of older
  versions.

## Verification plan

1. Capture RED for each unit with focused tests before production edits.
2. Implement the smallest GREEN change, then refactor while focused tests remain green.
3. Run a read-only cross-engine review when another engine is available; otherwise perform a delayed
   self-review and record the waiver in `.workflow/state.md`.
4. Run `CI=true ./scripts/task.sh check`, focused release tests, and shell syntax/static checks.
5. Execute an E2E fixture journey: enable/disable automatic checks, manual no-update/update/error,
   explicit install/cancel, and ZIP extraction/signature/checksum validation. No public release
   mutation is part of E2E.

## Acceptance criteria

- All criteria in `docs/superpowers/specs/2026-08-13-macos-automatic-updates-design.md` pass.
- Existing DMG release behavior remains green.
- New releases publish `Loqui-<version>-macos-arm64.zip` and its checksum in addition to the DMG
  pair.
- The user can discover, control, and explicitly install updates without silent restart or startup
  regression.
