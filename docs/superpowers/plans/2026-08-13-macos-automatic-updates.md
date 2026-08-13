# macOS Automatic Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Codex executes the tasks inline; each task still follows RED → GREEN → refactor.

**Goal:** Let installed Loqui builds discover newer GitHub Releases in the background and offer a verified, user-confirmed install/restart while preserving the DMG installer.

**Architecture:** A small `internal/app.UpdateService` owns the user-facing update state and a cancellable check-only scheduler. A Wails adapter in `main.go` configures the pinned `application.App.Updater` with a ZIP-only GitHub provider and SHA-256 sidecar; background checks call `Check`, while install and restart are separate explicit bindings. The release scripts publish both the existing DMG/checksum pair and a notarized ZIP/checksum pair.

**Tech Stack:** Go 1.25, Wails v3 `v3.0.0-alpha2.119` updater, TypeScript/Vite frontend, macOS `ditto`/`xcrun notarytool`/`stapler`, Bash contract tests, GitHub Releases.

**Spec:** `docs/superpowers/specs/2026-08-13-macos-automatic-updates-design.md`

## Global Constraints

- Reuse the pinned Wails updater; do not add Sparkle or another runtime updater dependency.
- Never set Wails `Config.CheckInterval`: in the pinned module it invokes `CheckAndInstall` and would stage an update without user confirmation.
- Automatic checks call `Check` only; install and restart are separate, explicit user actions.
- Runtime version comes from the packaged app's own `CFBundleShortVersionString`; unpackaged development runs expose no update backend.
- Existing DMG output remains unchanged; future releases add `Loqui-<version>-macos-arm64.zip` and `SHA256SUMS`.
- The ZIP contains exactly one top-level `Loqui.app`; all downloaded archives and checksums are validated before staging.
- No user-facing string is hard-coded outside the Spanish source catalogue and its English translation.
- No commit, push, or PR is made until `.workflow/state.md` has every standard gate checked.

---

### Task 1: Persist the automatic-check preference

**Files:**
- Modify: `internal/store/config.go` (`Settings`, `DefaultSettings`)
- Test: `internal/store/config_test.go`
- Modify: `internal/app/bootstrap.go` (`SettingsPayload` and payload assembly)
- Test: `internal/app/bootstrap_test.go`
- Modify: `internal/app/settings_write.go`
- Test: `internal/app/settings_write_test.go`

**Interfaces:**
- Produces `Settings.AutoUpdateChecks bool` with JSON key `autoUpdateChecks`, default `true`.
- Produces `SettingsPayload.AutoUpdateChecks bool`.
- Produces bound `SettingsService.SetAutoUpdateChecks(enabled bool) WriteResult`.

- [ ] **Step 1: Write failing store tests**

  Add tests that a fresh `DefaultSettings()` enables checks, an old JSON file with no key keeps
  the enabled default, and a JSON round trip preserves both `true` and `false`.

- [ ] **Step 2: Run the store tests and verify RED**

  Run `go test ./internal/store -run 'Test(DefaultSettings|AutoUpdate)' -count=1`.
  Expected: compile/test failure because the field and assertions do not exist.

- [ ] **Step 3: Implement the store field/default**

  Add `AutoUpdateChecks bool `json:"autoUpdateChecks"`` beside the other UI preferences and set
  `AutoUpdateChecks: true` in `DefaultSettings`; rely on the existing unmarshal-onto-defaults and
  unknown-key merge behavior.

- [ ] **Step 4: Write failing payload/setter tests**

  Assert `SettingsService.Load()` exposes the persisted value and `SetAutoUpdateChecks(false)`
  writes it, returns a fresh payload, and does not alter unrelated settings.

- [ ] **Step 5: Run the app tests and verify RED**

  Run `go test ./internal/app -run 'Test.*AutoUpdate' -count=1`.
  Expected: compile/test failure until the payload field and setter exist.

- [ ] **Step 6: Implement payload and setter**

  Copy the store value into `SettingsPayload` in the existing bootstrap snapshot and implement the
  setter through `UpdateSettings`, returning `ok`/`failed` exactly like the neighboring setters.

- [ ] **Step 7: Run focused tests and refactor**

  Run `go test ./internal/store ./internal/app -run 'Test.*AutoUpdate|TestDefaultSettings' -count=1`.
  Keep the tests green while removing duplication and checking `git diff --check`.

### Task 2: Add the testable updater service and scheduler

**Files:**
- Create: `internal/app/update_service.go`
- Test: `internal/app/update_service_test.go`
- Modify: `internal/i18n/en.go`
- Test: `internal/i18n/coverage_test.go` or a new catalogue test

**Interfaces:**
- `UpdateRelease { Version, Name, Notes, Artifact string }`.
- `UpdateStatus { State, CurrentVersion, AvailableVersion, Name, Notes, Error string; Ready bool }`.
- `UpdateResult { Status UpdateStatus; Error string }`.
- `UpdateBackend { CurrentVersion() string; Check(context.Context) (*UpdateRelease,error); DownloadAndInstall(context.Context) error; Restart(context.Context) error }`.
- `NewUpdateService(backend UpdateBackend, autoChecks func() bool, emit func(string, any), log func(string,string)) *UpdateService`.
- Bound methods `Status() UpdateStatus`, `Check() UpdateResult`, `Install() UpdateResult`, `Restart() UpdateResult`.
- Lifecycle methods `StartAutoChecks(initialDelay, interval time.Duration)` and `StopAutoChecks()`.

- [ ] **Step 1: Write failing service tests**

  Use a fake backend and event recorder to prove: `Check` maps no-update/update/error, `Install`
  refuses without an available release, `Restart` refuses before ready, successful install reaches
  `Ready`, backend errors do not expose secrets, background checks emit availability but never call
  `DownloadAndInstall`, disabled preferences make no calls, and `StopAutoChecks` cancels a pending
  timer without leaking a goroutine.

- [ ] **Step 2: Run service tests and verify RED**

  Run `go test ./internal/app -run 'TestUpdate' -count=1`.
  Expected: compile failure because `UpdateService` and its contracts do not exist.

- [ ] **Step 3: Implement the minimal state machine**

  Guard backend calls with a mutex, keep the latest release in memory, expose only sanitized status,
  call `Check` for background/manual checks, and require `StateAvailable` before `Install` and
  `StateReady` before `Restart`. Use a context derived by the scheduler and wait for its goroutine in
  `StopAutoChecks`.

- [ ] **Step 4: Add catalogue strings and translations**

  Add Spanish source strings and English entries for checking, no update, available, install,
  restart, failure, automatic checks, and the settings label; run the catalogue coverage test.

- [ ] **Step 5: Run focused tests and refactor**

  Run `go test ./internal/app ./internal/i18n -run 'TestUpdate|Test.*Catalog|Test.*Coverage' -count=1`.
  Refactor only with the state/event tests green.

### Task 3: Wire Wails, GitHub ZIP matching, tray, and lifecycle

**Files:**
- Modify: `main.go`
- Create: `internal/app/update_backend.go` if the Wails adapter is kept separate
- Test: `internal/app/update_backend_test.go` or `main_test.go` for matcher/configuration contracts
- Regenerate: `frontend/bindings/github.com/Juan-Motta/loqui-go/internal/app/`

**Interfaces:**
- Wails adapter maps `updater.Release` to `app.UpdateRelease` and delegates `Check`,
  `DownloadAndInstall`, and `Restart`.
- `configureUpdater(*application.App, string) error` initializes the pinned Wails updater with
  `Window: updater.WindowNone`, repository `Juan-Motta/loqui`, `ChecksumAsset: "SHA256SUMS"`,
  and a matcher accepting only `Loqui-*-macos-arm64.zip`.

- [ ] **Step 1: Write failing matcher/backend tests**

  Test that an asset list containing both DMG and ZIP selects only the ZIP, rejects non-arm64 and
  non-ZIP names, maps release notes/version/artifact, and leaves a development build unconfigured.

- [ ] **Step 2: Run tests and verify RED**

  Run `go test ./internal/app -run 'Test.*Updater|Test.*Asset' -count=1`.
  Expected: failure because the adapter/configuration functions are absent.

- [ ] **Step 3: Implement provider and adapter wiring**

  Construct the update service before `application.New` so it can be registered as a binding; after
  app construction configure `app.Updater` and inject the adapter. Do not configure Wails' periodic
  interval. Start the service after the existing dictation startup and defer `StopAutoChecks`.

- [ ] **Step 4: Add tray action**

  Add a localized `Buscar actualizaciones…` item in `buildTrayMenu` that invokes the same service
  `Check` method and never installs directly.

- [ ] **Step 5: Regenerate bindings and run Go tests**

  Run `./scripts/task.sh common:generate:bindings` (or the repository's equivalent binding task),
  then `go test ./internal/app -run 'Test.*Updater|Test.*Asset' -count=1`.

### Task 4: Add System/About update controls

**Files:**
- Modify: `frontend/index.html`
- Modify: `frontend/src/system.ts`
- Modify: `frontend/src/about.ts`
- Modify: `frontend/src/settings.ts` only where existing paint/wiring registration requires it
- Regenerate: `frontend/bindings/.../models.js` and `updateservice.js`
- Test: `internal/app/frontend_update_contract_test.go` and focused TypeScript typecheck

**Interfaces:**
- System renders checkbox `#autoUpdateChecks` from `SettingsPayload.autoUpdateChecks` and calls
  `Settings.SetAutoUpdateChecks` on change.
- About renders `#aboutUpdateStatus`, `#aboutCheckUpdates`, `#aboutInstallUpdate`, and
  `#aboutRestartUpdate`; it calls `Update.Check`, `Update.Install`, and `Update.Restart` only from
  explicit buttons and consumes `updates:available`/`updates:ready` events.

- [ ] **Step 1: Write failing frontend contract tests**

  Assert the HTML contains the setting and About controls, TypeScript references the generated
  update service, and both Spanish source labels are present for catalogue coverage.

- [ ] **Step 2: Run contract/type tests and verify RED**

  Run `go test ./internal/app -run 'TestFrontend.*Update' -count=1` and
  `npm --prefix frontend run typecheck`; expected failure until markup/bindings code exists.

- [ ] **Step 3: Implement System control**

  Paint the checkbox from the authoritative payload and use the existing `run`/`onSaved` pattern so
  a failed write repaints the previous value.

- [ ] **Step 4: Implement About state/actions**

  Paint `Update.Status()` when About opens, show available version/notes, use localized confirmation
  before install and restart, disable duplicate actions while busy, and show retryable errors.

- [ ] **Step 5: Run frontend verification**

  Run `npm --prefix frontend run typecheck`, the focused Go contract tests, and a production
  frontend build. Ensure no hard-coded English/Spanish copy bypasses `data-i18n`/catalogue rules.

### Task 5: Publish a notarized updater ZIP

**Files:**
- Modify: `scripts/release-macos.sh`
- Modify: `scripts/release-version.sh` (add `--zip-name`)
- Modify: `scripts/github-release.sh`
- Modify: `.github/workflows/release.yml`
- Modify: `scripts/tests/release-version-test.sh`
- Modify: `scripts/tests/release-macos-test.sh`
- Modify: `scripts/tests/github-release-test.sh`
- Modify: `scripts/tests/github-release-workflow-test.sh`

**Interfaces:**
- `release-version.sh --zip-name` prints `Loqui-<version>-macos-arm64.zip`.
- `release-macos.sh` produces a ZIP with exactly one top-level `Loqui.app`, validates extraction,
  notarizes the ZIP, staples/validates the inner app before rebuilding the final ZIP, and continues
  through the existing DMG notarization/staple/Gatekeeper path.
- `github-release.sh prepare/publish` requires the DMG, DMG `.sha256`, ZIP, and `SHA256SUMS`; the
  static checksum contains the ZIP digest and is passed as a release asset.

- [ ] **Step 1: Write failing release contract tests**

  Add ZIP-name expectations, missing-ZIP/checksum failures, exact four-asset publication assertions,
  and phase assertions that ZIP creation/notarization happens before DMG publication.

- [ ] **Step 2: Run release tests and verify RED**

  Run `bash scripts/tests/release-version-test.sh`, `bash scripts/tests/github-release-test.sh`,
  and the focused release test; expected failures identify the old DMG-only contract.

- [ ] **Step 3: Implement deterministic ZIP creation/verification**

  Add safe stage paths, `ditto -c -k --keepParent`, extraction into a stage-only directory, exact
  top-level `Loqui.app` assertion, ZIP SHA-256 generation, and cleanup handling. Keep the existing
  DMG path and evidence publication intact.

- [ ] **Step 4: Implement ZIP notarization/stapling**

  Submit the ZIP with the existing notary credentials, validate its accepted ticket, staple the
  staged `Loqui.app`, rebuild the ZIP from that stapled app, and verify the ZIP again before creating
  the DMG. Keep DMG notarization as a separate required phase because a ZIP cannot be stapled.

- [ ] **Step 5: Extend GitHub release publication**

  Derive both canonical names, validate both checksum files, pass all four assets to `gh release
  create`, and require the exact four-name asset set in the post-publication API assertion.

- [ ] **Step 6: Run all release contract tests and shell checks**

  Run `bash scripts/tests/release-version-test.sh`, `bash scripts/tests/release-macos-test.sh`,
  `bash scripts/tests/github-release-test.sh`, `bash scripts/tests/github-release-workflow-test.sh`,
  `bash scripts/tests/darwin-taskflow-test.sh`, `bash -n` on changed scripts, and ShellCheck when
  available.

### Task 6: Review, E2E, and ship-gate evidence

**Files:**
- Create: `docs/e2e/use-cases/automatic-updates.md`
- Create: `docs/e2e/reports/2026-08-13-automatic-updates.md`
- Modify: `docs/CHANGELOG.md`
- Modify: `.workflow/state.md`

- [ ] **Step 1: Run delayed code review**

  Re-read the complete diff against the design, specifically checking silent installation, ZIP-only
  selection, checksum handling, release asset exactness, localization, and shutdown cancellation.
  Resolve every P0/P1/P2; if another engine remains unavailable, record the explicit single-engine
  waiver in `.workflow/state.md`.

- [ ] **Step 2: Run repository verification**

  Run `CI=true ./scripts/task.sh check`, all focused tests, `git diff --check`, and inspect generated
  bindings and release workflow diffs. Do not claim success from a stale earlier run.

- [ ] **Step 3: Execute the E2E fixture journey**

  Verify automatic checks enabled/disabled, manual no-update/update/error, explicit install/cancel,
  restart gating, localized System/About controls, ZIP exact-root/checksum/signature checks, and
  unchanged DMG behavior. Use a local fake provider/release fixture; do not mutate GitHub or publish
  a tag.

- [ ] **Step 4: Record evidence and update state**

  Write a top-level `VERDICT: PASS` report at the exact path named in `.workflow/state.md`, update
  `docs/CHANGELOG.md`, and check every standard gate only after its evidence exists.

- [ ] **Step 5: Run the final gate command**

  Run `sh shared/scripts/check-gates.sh`, review `git status`, and only then use the finishing branch
  workflow for commit/push/PR.
