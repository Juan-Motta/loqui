# Continuity — session handoff

- **Focus:** Close out the protected GitHub macOS release automation with live evidence from the
  first public release.

- **NEXT STEP:** Review and merge branch `docs/github-release-live-evidence`; the implementation and
  all live journeys are complete.

- **Blockers:** none.

- **Active workflow:** `.workflow/state.md` (`new-feature`, phase `finish-branch`). The report is
  `docs/e2e/reports/2026-08-10-github-macos-release.md` with `VERDICT: PASS`.

- **Handoff notes:**
  - PR #2 merged as `50f53f7bcc3d35637df96cda106a6ea8e1ea97da`. Release workflow run
    `31428242122` passed preflight, protected approval, signing, notarization, evidence upload,
    publication, and credential cleanup.
  - Public `v0.1.0` targets that exact SHA and contains only
    `Loqui-0.1.0-macos-arm64.dmg` plus its checksum. A fresh download passed SHA-256, `hdiutil`,
    stapler, DMG/app Gatekeeper, deep signature verification, and production bundle audit. The owner
    independently installed it and confirmed that Loqui works.
  - Duplicate run `31429953529` failed secret-free preflight because `v0.1.0` already exists; its
    protected job was skipped. Non-main run `31430266300` failed the exact-main check; no deployment
    approval existed, the Environment API still allows only `main`, and the temporary test branch
    was deleted.
  - The 14-file evidence artifact expires on 2026-08-24 and passed structural secret/path scans.
    No secret value appears in the report, changelog, or branch.

- **Updated:** 2026-08-10
