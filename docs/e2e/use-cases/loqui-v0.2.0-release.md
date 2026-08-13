# Loqui v0.2.0 release use cases

## UC-REL-020-CLI-01 — Resolve consistent release metadata

- **ID:** UC-REL-020-CLI-01.
- **Actor:** Loqui release maintainer.
- **Scenario:** The maintainer has selected minor version `0.2.0` and regenerated the macOS plist metadata before opening the version-bump PR.
- **Interface:** CLI.
- **Intent:** Confirm the canonical version, DMG filename, and persisted macOS bundle metadata agree before any remote release mutation.
- **Setup:** Check out `release/v0.2.0` after running the documented `patch-plists.sh` generator.
- **Steps:**
  1. Run `scripts/release-version.sh` and `scripts/release-version.sh --dmg-name` to resolve the canonical public identifiers.
  2. Run `scripts/patch-plists.sh --check`, then invoke both release-version modes again as independent persistence observations.
- **Verification:** Both version observations print exactly `0.2.0`; both artifact observations print exactly `Loqui-0.2.0-macos-arm64.dmg`; the plist check says `check ok` with exit 0.
- **Persistence:** Repository files; the second invocation independently observes the version state persisted by the generator.

## UC-REL-020-CLI-02 — Confirm the release identifier remains available

- **ID:** UC-REL-020-CLI-02.
- **Actor:** Loqui release maintainer.
- **Scenario:** The version-bump PR is being prepared and the maintainer must ensure no concurrent actor has claimed `v0.2.0`.
- **Interface:** CLI.
- **Intent:** Avoid colliding with or overwriting an existing immutable tag or GitHub Release.
- **Setup:** Use the configured `origin` remote and an authenticated GitHub CLI session with read access to `Juan-Motta/loqui`; no publication command is allowed in this journey.
- **Steps:**
  1. Query `refs/tags/v0.2.0` from `origin` and query GitHub Releases for `v0.2.0`.
  2. Repeat both read-only queries and compare their results to detect a concurrent state change.
- **Verification:** Both tag queries return an empty result with exit 0; both Release queries return the expected `release not found`; no tag, release, or workflow mutation occurs.
- **Persistence:** Remote stateless reads; the second observation must show the same absence immediately before branch shipment.

## Interface coverage

- **API:** N/A — direct API publication is not the supported maintainer journey; publication is confined to the protected Action.
- **UI:** N/A — the bump changes release metadata, not application UI. The included user-facing changes have their own native E2E reports.
