# Continuity — session handoff

- **Focus:** Prepare and publish Loqui `v0.2.0`.

- **NEXT STEP:** Complete the release-preparation gates on `release/v0.2.0`, commit/push/open the
  PR, merge it into `main`, then dispatch exactly one protected Release workflow from the merged
  SHA. After publication, verify the public assets and update README download links separately.

- **Blockers:** none. GitHub Environment approval will be required after workflow preflight.

- **Active workflow:** `new-feature`; state is in `.workflow/state.md`.

- **Handoff notes:**
  - Owner approved semantic minor version `0.2.0` for the new Azure OpenAI model functionality and
    fixes accumulated since `v0.1.1`.
  - Canonical version and both generated macOS plist files now resolve to `0.2.0` on branch
    `release/v0.2.0`.
  - `v0.2.0`, its remote tag, GitHub Release, and remote release branch were absent at preparation
    start. Existing public releases are `v0.1.0` and `v0.1.1`.
  - Plan: `docs/plans/2026-08-13-loqui-v0.2.0-release.md`.
  - Preparation E2E: `docs/e2e/reports/2026-08-13-loqui-v0.2.0-release.md`.
  - Never delete or move an existing public tag/Release; ambiguous publication is investigated
    through read-only queries.

- **Updated:** 2026-08-13
