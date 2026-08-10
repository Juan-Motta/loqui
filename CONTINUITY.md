# Continuity — session handoff

- **Focus:** Bootstrap the protected manual GitHub Actions pipeline that builds, signs, notarizes,
  verifies, and publishes the Loqui macOS DMG from the exact `main` version/SHA.

- **NEXT STEP:** Review and merge the bootstrap PR from `feat/github-macos-release`; then fetch the
  merged `main` and obtain the owner's separate confirmation that version `0.1.0` at that exact SHA
  is fit for permanent public distribution before dispatching the first `Release` workflow.

- **Blockers:** Live `GMR-LIVE-01..04` cannot run until the workflow exists on default `main`. The
  owner explicitly approved the one-time bootstrap exception on 2026-08-10; this does not authorize
  publishing the first public release.

- **Active workflow:** `.workflow/state.md` (`new-feature`, phase `finish-branch`). The bootstrap
  report is `docs/e2e/reports/2026-08-10-github-macos-release.md` with an honest `VERDICT: FAIL`
  solely because the live workflow is not yet on `main`.

- **Handoff notes:**
  - GitHub Environment `release` requires reviewer `Juan-Motta`, allows exactly `main`, and contains
    all five required secret names; no values were recorded. The App Store Connect Team Key
    authenticated successfully through a read-only `notarytool history` call.
  - `./scripts/task.sh check`, the macOS release suite, ShellCheck, workflow YAML parsing, and
    `git diff --check` passed after implementation. The initial sandbox-only port-bind failures were
    rerun outside the sandbox and passed.
  - Code review found no P0/P1. Its four P2 findings are resolved or rebutted with current official
    GitHub CLI behavior and RED→GREEN regression coverage; no open P0/P1/P2 remains.
  - Publication is manual-only, Environment-protected, one-shot, and never deletes ambiguous remote
    state. A separate post-merge evidence PR must graduate live E2E and add the changelog entry.

- **Updated:** 2026-08-10
