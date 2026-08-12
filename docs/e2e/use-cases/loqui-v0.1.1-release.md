# Loqui v0.1.1 release use cases

## UC-REL-011-CLI-01 — Resolve consistent release metadata

- **Actor:** Loqui release maintainer
- **Scenario:** The maintainer has selected patch version `0.1.1` and regenerated the macOS
  plist metadata before opening the version-bump PR.
- **Interface:** CLI
- **Intent:** Confirm the canonical version, DMG filename, and persisted macOS bundle metadata
  agree before any remote release mutation.
- **Setup:** Check out `release/v0.1.1` after running the documented `patch-plists.sh` generator.
- **Steps:**
  1. Run `scripts/release-version.sh` to resolve the canonical stable version.
  2. Run `scripts/release-version.sh --dmg-name` to resolve the public artifact name.
  3. Run `scripts/patch-plists.sh --check` as a separate observer of the generated metadata.
- **Verification:** The first command prints exactly `0.1.1`, the second prints exactly
  `Loqui-0.1.1-macos-arm64.dmg`, and the independent plist check says `check ok` with exit 0.
- **Persistence:** Repository files; the third invocation independently observes the generated
  version state persisted by the earlier generator run.

## UC-REL-011-CLI-02 — Confirm the release identifier remains available

- **Actor:** Loqui release maintainer
- **Scenario:** The version-bump PR is being prepared and the maintainer must ensure no concurrent
  actor has claimed `v0.1.1` before shipping or dispatching the protected workflow.
- **Interface:** CLI
- **Intent:** Avoid colliding with or overwriting an existing immutable tag or GitHub Release.
- **Setup:** Use the configured `origin` remote and an authenticated GitHub CLI session with
  read access to `Juan-Motta/loqui`.
- **Steps:**
  1. Query `refs/tags/v0.1.1` from `origin` with `git ls-remote`.
  2. Query GitHub Releases for `v0.1.1` with `gh release view`.
  3. Repeat both queries in a second observation to detect a concurrent state change.
- **Verification:** Both tag queries return an empty result with exit 0; both Release queries
  return the expected `release not found` result; no delete, tag, release, or workflow command runs.
- **Persistence:** Remote stateless reads; the repeated second observation must show the same
  absence immediately before the branch is shipped.

## Interface coverage

- **API:** N/A — the repository's supported maintainer interface is the CLI and protected Action;
  direct API publication is intentionally forbidden.
- **UI:** N/A — this version-metadata change has no application UI. The intuitive DMG user journey
  already passed in `docs/e2e/reports/2026-08-11-intuitive-dmg-installer.md`; the new public asset
  will be checked after the protected workflow publishes it.
