# Loqui macOS automatic updates — design

## Classification and goal

This is an architectural feature: it crosses app lifecycle, persisted preferences, native
updating, UI/tray actions, and the release pipeline. The goal is to let an installed Loqui app
learn about a newer GitHub Release and offer a safe, user-confirmed update while preserving the
current DMG download flow.

## User experience

- Automatic checks are enabled by default and run in the background after startup, then at a
  conservative interval. They do not delay app launch and do nothing visible when no update exists.
- The user can disable automatic checks in System settings.
- The tray menu and About view expose “Check for updates…”. The action reports checking, no update,
  update available, and failure in the existing English/Spanish catalogue.
- An update is never installed or restarted silently. The user explicitly confirms download/
  install and restart; cancelling leaves the current app running.
- The first updater-capable release is a bootstrap: users on older releases install it once from
  the DMG. Later releases publish both the DMG and updater ZIP.

## Architecture

1. **Version source.** Add a small runtime version accessor shared by About and updater setup,
   reading the running bundle's version and retaining the existing development fallback.
2. **Updater service.** Add an app-owned service that initializes `app.Updater` with the current
   version and GitHub provider, selects only the Apple Silicon ZIP, and exposes explicit check and
   install operations to the frontend. Configure `updater.WindowNone`; it translates updater
   results/errors into stable UI events and never uses Wails' `CheckAndInstall` for background
   checks. It does not own settings persistence.
3. **Lifecycle scheduler.** Start a cancellable background check after the app is ready when the
   preference is enabled. Use one initial delayed check and a long interval, calling only
   `Updater.Check` (the pinned Wails `CheckInterval` path calls `CheckAndInstall` and would stage an
   update without confirmation). Failures are logged and surfaced only for explicit/manual actions.
   Shutdown cancels the scheduler.
4. **Preference.** Add `autoUpdateChecks` to `store.Settings`, defaulting to `true`, with a normal
   settings setter and generated binding. Existing settings files remain valid.
5. **UI.** Add the setting to System and an update status/action to About. Reuse the existing
   locale catalogue and event conventions; the native tray action calls the same service method.
6. **Release artifacts.** Extend the macOS release script to create a notarized/stapled
   `Loqui-<version>-macos-arm64.zip` containing exactly `Loqui.app`, generate a static checksum
   asset, and retain the existing DMG/checksum. Extend GitHub release preflight/publication and
   contract tests to require both artifact pairs.

The updater provider boundary remains replaceable: a later signed endpoint/AppCast provider can
replace the GitHub provider without changing the preference or UI contract.

## Alternatives considered

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness / user risk |
| --- | --- | --- | --- | --- | --- |
| Wails + GitHub + ZIP + SHA-256 (selected) | Medium | Medium: app, release scripts, tests | High: provider boundary is isolated | Fastest because Wails is already pinned | Good for trusted GitHub distribution; explicit install avoids surprise restarts |
| Wails endpoint/AppCast + Ed25519 manifest | High | Medium/high: hosting and key lifecycle | Medium | Slower | Stronger authenticity, but more operational failure modes for this release |
| Manual browser check only | Low | Low | High | Fast | Does not meet automatic-update goal and leaves users unaware of releases |

## Implementation units

1. Add/update failing Go tests for runtime version, updater provider configuration, update-state
   mapping, and scheduler cancellation; implement the app service and lifecycle wiring.
2. Add failing store/service/frontend tests for the preference and EN/ES About/System/tray
   controls; implement persistence, bindings, and UI states.
3. Add failing release tests for ZIP naming, one top-level app, ZIP checksum, DMG preservation,
   GitHub preflight, and publication; implement the release script and workflow contract changes.
4. Run a delayed self-review (or a second engine when available), resolve all P0/P1/P2 findings,
   then run the repository check and an end-to-end manual update journey against a local fixture
   release. Do not contact or mutate the public release during tests.

## Failure and safety rules

- No update metadata, download, or release-network failure may prevent Loqui from starting or
  dictating.
- Malformed, missing, mismatched, or non-ZIP assets are treated as unavailable; the DMG is never
  selected by the updater.
- A failed download/install leaves the current app untouched and reports a retryable error.
- Restart happens only after an explicit user confirmation and a completed verified install.
- The release job must fail before tag/publication if the ZIP, checksum, notarization, or bundle
  audit is invalid.
- Existing releases and their DMG assets are immutable; the new ZIP contract applies to future
  updater-enabled releases.

## Acceptance criteria

- A fresh install has automatic checks enabled; toggling the setting persists across relaunches.
- Manual tray/About checks work in English and Spanish and distinguish no-update, available, and
  error states.
- The updater chooses only `Loqui-<version>-macos-arm64.zip`, verifies its checksum, and never
  opens or installs the DMG.
- No-update background checks are silent, startup remains non-blocking, and shutdown cancels the
  scheduler.
- An update install/restart requires explicit confirmation and leaves the app unchanged on cancel
  or failure.
- Future GitHub Releases contain the existing DMG/checksum plus the updater ZIP/checksum; the ZIP
  contains exactly one top-level `Loqui.app` with valid Developer ID signature, notarization, and
  Gatekeeper acceptance.
- Focused tests, `CI=true ./scripts/task.sh check`, and the documented E2E journey pass.
