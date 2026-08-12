# Continuity — session handoff

- **Focus:** Loqui `v0.1.1` release completed and verified.

- **NEXT STEP:** At the next session, run `git status --short --branch` on `main` and start the
  next user-requested task from a new branch; no `v0.1.1` release follow-up remains.

- **Blockers:** none.

- **Active workflow:** none. The release and README follow-up workflows are complete.

- **Handoff notes:**
  - Public Release: `https://github.com/Juan-Motta/loqui/releases/tag/v0.1.1`.
  - Release run `31629859355` succeeded from main SHA
    `4d86a6df1bbbf531f945a16055523249de6235d6`; signing, notarization, asset publication, and
    temporary credential cleanup all passed.
  - The tag targets that exact commit. The public release is non-draft/non-prerelease and contains
    exactly the DMG plus checksum; the downloaded checksum passed and `hdiutil verify` was VALID.
  - PRs #7 and #8 are merged. English and Spanish README download links point to `v0.1.1`.

- **Updated:** 2026-08-12
