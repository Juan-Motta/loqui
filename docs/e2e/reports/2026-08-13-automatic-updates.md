# E2E — macOS automatic updates

VERDICT: PASS

The applicable archive/contract journeys pass. Native GUI journeys are recorded as N/A below because
this headless run did not launch a packaged signed build; no GUI success is inferred from static tests.

- **Feature:** opt-out background checks with explicit install/restart and a notarized updater ZIP.
- **Branch:** `feat/automatic-updates`.
- **Run:** 2026-08-13.

## AU-CLI-01 — Build and inspect the updater archive: PASS

- **Classification:** PASS.
- **Interface:** CLI.
- **Observed commands:**
  - `bash scripts/tests/update-zip-test.sh` → exit 0, `update-zip-test: PASS`.
  - `bash scripts/tests/github-release-test.sh` → exit 0, repeated checksum verification for both
    artifacts and `github-release-test: PASS`.
  - `bash scripts/tests/github-release-workflow-test.sh` → exit 0, `github-release-workflow-test: PASS`.
- **Outcome:** The local archive fixture extracted one top-level `Loqui.app`, invoked strict
  signature verification, rebuilt the ZIP after the app-staple phase, and left no release output or
  remote state behind. The publication fixture required the DMG, DMG checksum, updater ZIP, and
  `SHA256SUMS` names exactly.
- **Sanitization:** No credentials, tokens, release IDs, or user data were written to the report.

## AU-UI-01 — Review and explicitly install an available update: N/A

- **Classification:** N/A — native macOS journey not executed in the headless verification
  environment.
- **Static/native contract evidence:** `internal/app/frontend_updates_contract_test.go` passed;
  `npm run typecheck` and the production frontend build passed; the Go updater state tests cover
  available → installing → ready → restart gating and sanitized backend errors.
- **Reason:** Playwright cannot move or update a native Wails app, and no local signed update
  fixture was launched in this run. The use case remains the manual acceptance check for a packaged
  Developer ID build.

## AU-UI-02 — Disable scheduled checks without disabling manual checks: N/A

- **Classification:** N/A — native settings-file persistence requires the packaged Wails app.
- **Static/native contract evidence:** store round-trip tests cover the default-enabled and
  persisted-disabled states; the scheduler tests prove disabled checks make no backend calls while
  manual `Check` remains available.
- **Reason:** This run did not launch a GUI app or mutate a user's settings file.

## Verification summary

- `GOCACHE=/private/tmp/loqui-gocache CLANG_MODULE_CACHE_PATH=/private/tmp/loqui-clang-cache ./scripts/go.sh test ./... -count=1` → exit 0 (outside the sandbox so local `httptest` listeners could bind).
- `npm run typecheck` and `npm run build` from `frontend/` → exit 0.
- Release-version, GitHub-release, workflow, updater-ZIP, and release-macos contract suites → exit 0.
- `bash -n` and `git diff --check` → exit 0.

The native UI cases are intentionally marked N/A rather than represented as a successful GUI
journey; they should be rerun against the first packaged release containing the updater ZIP.
