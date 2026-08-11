# User-friendly bilingual README — E2E use cases

These journeys exercise the repository documentation through GitHub's Markdown API and the
repository-owned task wrapper. The application UI is not changed by this feature, so there is no
Playwright UI journey. The plan separately requires a manual visual inspection of the rendered HTML.

Last run: 2026-08-11. See
`docs/e2e/reports/2026-08-11-user-friendly-readme.md`.

## README-API-01 — An English reader finds and validates the download

- **ID:** `README-API-01`
- **Actor:** A macOS user discovering Loqui for the first time.
- **Scenario:** The user opens the default English project landing page and follows its download information.
- **Interface:** API.
- **Intent:** Ensure the canonical README renders as a product page and points to the current public arm64 release and checksum.
- **Setup:** A checkout containing `README.md` and the repository-owned banner; authenticated read-only access to GitHub's Markdown and Releases APIs.
- **Steps:**
  1. POST `README.md` to GitHub's `/markdown` endpoint in GFM mode and inspect the returned HTML.
  2. Read public release `v0.1.0` twice through the GitHub Releases API and resolve the repository HEAD twice.
- **Verification:** The HTML shows the banner first, English/Spanish navigation near the top, nine main sections, one engine table, twelve code blocks, and one closed-by-default maintainer disclosure. Both release reads report a public non-prerelease with exactly `Loqui-0.1.0-macos-arm64.dmg` and its `.sha256`; both repository reads resolve the same HEAD.
- **Persistence:** N/A — the rendered document and public release reads are stateless and no remote state is changed.

## README-API-02 — A Spanish reader gets the complete equivalent guide

- **ID:** `README-API-02`
- **Actor:** A Spanish-speaking macOS user or contributor.
- **Scenario:** The user follows the Español link and reads the same product, setup, privacy, and release guidance in Spanish.
- **Interface:** API.
- **Intent:** Ensure the translation is complete rather than a summary and can navigate back to English.
- **Setup:** A checkout containing `README.es.md`, `README.md`, and the shared banner; authenticated read-only access to GitHub's Markdown API.
- **Steps:**
  1. POST `README.es.md` to GitHub's `/markdown` endpoint in GFM mode and inspect the returned HTML structure and links.
  2. Render `README.md` the same way and compare heading levels, tables, code blocks, disclosure count, artifact names, commands, and security warnings.
- **Verification:** The Spanish HTML shows the banner and reciprocal English link, nine main sections, six development subsections, one engine table, twelve code blocks, and one closed-by-default disclosure. Both languages expose the same downloads, commands, compatibility limits, privacy warnings, and provider variables.
- **Persistence:** N/A — rendering is a stateless read and writes no repository or remote state.

## README-CLI-01 — A contributor can trust the documented project entry points

- **ID:** `README-CLI-01`
- **Actor:** A contributor preparing to run or build Loqui from source.
- **Scenario:** The contributor checks the documented wrappers and validates the checkout through the public task entry point.
- **Interface:** CLI.
- **Intent:** Prove the listed task names, release documentation contract, and full project gate remain executable after the README rewrite.
- **Setup:** An Apple Silicon development checkout with the documented Go, Node.js/npm, CMake, and Xcode/macOS SDK prerequisites installed.
- **Steps:**
  1. Run `./scripts/task.sh check` through the repository wrapper.
  2. Re-run the focused bilingual content contract, `git diff --check`, and `scripts/tests/github-release-workflow-test.sh` against the unchanged checkout.
- **Verification:** The complete test, vet, typecheck, packaging/release test suite exits 0; the follow-up checks confirm all documented task names and release literals, exact English/Spanish structure, valid local links, and no whitespace errors.
- **Persistence:** The follow-up invocation observes the same documentation and task graph after the full gate; no command mutates tracked source files.
