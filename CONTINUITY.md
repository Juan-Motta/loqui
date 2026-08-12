# Continuity — session handoff

- **Focus:** Publish the merged intuitive bilingual DMG installer as immutable GitHub Release
  `v0.1.1` through the protected manual Release Action.

- **NEXT STEP:** Merge the verified `release/v0.1.1` version-bump PR into `main`; revalidate that
  tag/Release `v0.1.1` remain absent; dispatch `.github/workflows/release.yml` exactly once from
  the merged `main` SHA; approve the protected `release` Environment and monitor to completion.

- **Blockers:** none. The Environment approval may require the repository owner when the protected
  job reaches its deployment gate.

- **Active workflow:** `.workflow/state.md` (`new-feature`, standard profile, phase `ship`).
  Pre-publication E2E evidence:
  `docs/e2e/reports/2026-08-12-loqui-v0.1.1-release.md` with `VERDICT: PASS`.

- **Handoff notes:**
  - Owner selected patch version `0.1.1`. Before preparation, its branch, remote tag, and GitHub
    Release were all absent; two E2E observations reconfirmed tag/Release absence.
  - `build/config.yml` and both Darwin plist files carry `0.1.1`; the canonical public artifact is
    `Loqui-0.1.1-macos-arm64.dmg`.
  - TDD captured stale-plist RED before the official generator produced GREEN. The full gate also
    exposed two tests coupled to the old public version; their focused REDs were captured and their
    fixtures were made release-independent.
  - Fresh `CI=true ./scripts/task.sh check` exited 0 after both fixes, including the real DMG
    integration and `hdiutil verify` VALID. Bash syntax and ShellCheck pass on both modified tests.
  - No `v0.1.1` tag, GitHub Release, workflow run, or public asset has been created yet. Never
    delete or move `v0.1.0`; ambiguous publication state must be inspected, not cleaned up.
  - After public verification, update README.md and README.es.md download metadata in a normal PR.

- **Updated:** 2026-08-12
