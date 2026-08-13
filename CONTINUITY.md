# Continuity — session handoff

- **Focus:** Loqui `v0.3.0` is published. A Windows release-readiness research brief is recorded;
  no port work has started.

- **NEXT STEP:** Decide whether to open the Windows port. If yes, close the five open decisions in
  `docs/research/2026-08-13-windows-release-readiness.md` (Azure Speech in v1, signing-identity
  eligibility, Windows auto-update, uninstall data retention, Windows 10 support) and start with
  its Phase 1. Otherwise, wait for the owner's next case, then sync `main`, branch, and pick the
  workflow.

- **Blockers:** none.

- **Active workflow:** none. `.workflow/state.md` is a leftover from the shipped `v0.3.0` release
  (all boxes checked, branch `codex/release-v0.3.0` already merged and deleted) — reset it from
  `shared/state.template.md` when the next workflow starts.

- **Handoff notes:**
  - Public release: `https://github.com/Juan-Motta/loqui/releases/tag/v0.3.0`, marked `Latest`,
    with the DMG, its `.sha256`, the update ZIP, and `SHA256SUMS`. Release PRs #17 and #18 merged.
  - `./scripts/task.sh check` exits 0 on `main`: Go tests, `vet`, frontend typecheck, and the nine
    macOS release tests all pass. No open PRs; the last Release run on `main` succeeded.
  - Version is consistent across `build/config.yml`, both `Info.plist` files, and the README links.
  - Windows research conclusion: the code does **not** compile for `GOOS=windows` today, the NSIS
    payload ships only `loqui.exe`, Windows metadata is still `0.1.0` with MSIX placeholders, and
    the updater's asset matcher accepts `darwin/arm64` only. Enabling the existing packaging task
    would not produce a usable release.
  - Stale local branches with no unique commits: `feat/automatic-updates` (also still on `origin`)
    and `codex/research-windows-release`.

- **Updated:** 2026-08-13
