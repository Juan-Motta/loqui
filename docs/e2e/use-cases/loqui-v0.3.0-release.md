# Loqui v0.3.0 release use cases

## UC-REL-030-CLI-01 — Resolve consistent release metadata

- **ID:** UC-REL-030-CLI-01.
- **Actor:** Loqui release maintainer.
- **Scenario:** The maintainer has selected `0.3.0` and generated the macOS metadata before opening the preparation PR.
- **Interface:** CLI.
- **Intent:** Confirm the canonical version, both public artifact names, and macOS bundle metadata agree before any remote mutation.
- **Setup:** Check out `codex/release-v0.3.0` after using the repository's plist generator.
- **Steps:**
  1. Resolve the version, DMG name, and updater ZIP name with `scripts/release-version.sh`.
  2. Run `scripts/patch-plists.sh --check`, then resolve all three values again.
- **Verification:** Both observations return `0.3.0`, `Loqui-0.3.0-macos-arm64.dmg`, and `Loqui-0.3.0-macos-arm64.zip`; plist check exits 0 with `check ok`.
- **Persistence:** The second CLI invocation independently observes the values persisted in repository files.

## UC-REL-030-CLI-02 — Confirm the release identifier remains available

- **ID:** UC-REL-030-CLI-02.
- **Actor:** Loqui release maintainer.
- **Scenario:** The preparation is ready and the maintainer must ensure no concurrent actor has claimed `v0.3.0`.
- **Interface:** CLI.
- **Intent:** Prevent replacing or colliding with an immutable tag or GitHub Release.
- **Setup:** Use the configured `origin` and authenticated GitHub CLI only for read operations.
- **Steps:**
  1. Query `refs/tags/v0.3.0` and GitHub Releases for `v0.3.0`.
  2. Repeat both queries immediately before dispatch and compare the result.
- **Verification:** Both observations show no tag and `release not found`; no tag, Release, or workflow mutation occurs during preparation.
- **Persistence:** The second independent remote observation detects any state created after the first.

## Interface coverage

- **API:** N/A — publication is intentionally confined to the protected GitHub Action.
- **UI:** N/A — this change synchronizes release metadata and does not alter application UI.
