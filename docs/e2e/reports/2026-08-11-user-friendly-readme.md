# E2E report — User-friendly bilingual README

- **Feature:** Product-first English README with a complete Spanish translation and shared banner
- **Branch:** `docs/user-friendly-readme`
- **Executed:** automated journeys at `2026-08-11T17:00:15Z`; owner visual confirmation at
  `2026-08-11T17:55:50Z`

VERDICT: PASS

The API and CLI journeys pass. Browser automation was unavailable in the agent session, so the owner
opened the two generated local HTML files and confirmed the plan-required visual observations.

## README-API-01

- **Classification:** PASS
- **Interface:** API
- **Command:** Render `README.md` through `gh api -X POST /markdown` in GFM mode; read public release
  `v0.1.0` and repository HEAD twice through GitHub CLI/API operations.
- **Observed output:** Non-empty 18 KB HTML; 9 `<h2>` elements, 6 `<h3>` elements, one engine table,
  12 code blocks, one `<details>` without `open`, and the banner source
  `docs/assets/loqui-banner.png`. Repeated release and repository reads: `PASS`.
- **Verification:** The public release is non-draft and non-prerelease and contains exactly
  `Loqui-0.1.0-macos-arm64.dmg` plus `Loqui-0.1.0-macos-arm64.dmg.sha256`. The repeated reads were
  identical and the repository resolved the same HEAD.
- **Persistence re-check:** N/A for stateless reads; the second API/remote reads independently
  reproduced the first result without mutation.

## README-API-02

- **Classification:** PASS
- **Interface:** API
- **Command:** Render `README.es.md` and `README.md` through GitHub's `/markdown` endpoint, then run
  the bilingual structure and content contract against both source files.
- **Observed output:** Non-empty 20 KB Spanish HTML with the same 9/6 heading hierarchy, one table,
  12 code blocks, shared banner, reciprocal language link, and one closed-by-default disclosure.
  The focused contract printed `Final bilingual content contract: PASS`.
- **Verification:** Both languages expose the same release assets, compatibility boundaries,
  Whisper's user-initiated resumable model download, source-build requirements, permissions,
  cleartext credential warning, provider variables, task commands, and maintainer runbook.
- **Persistence re-check:** N/A for stateless rendering; the English render and source comparison
  independently confirmed parity after rendering Spanish.

## README-CLI-01

- **Classification:** PASS
- **Interface:** CLI
- **Command:** `./scripts/task.sh check`, followed by the focused bilingual contract,
  `git diff --check`, and `./scripts/tests/github-release-workflow-test.sh`.
- **Observed output:** The complete Go tests, vet, frontend typecheck, and macOS release-test suite
  exited 0. `github-release-workflow-test: PASS`; `Final bilingual content contract: PASS`.
  Existing linker warnings about duplicate rpaths and macOS target versions remained non-fatal.
- **Verification:** Every documented task exists, the release workflow's exact README literals are
  preserved, local files and wrapper entry points resolve, the banner is byte-identical to the
  supplied 1672×941 PNG, and `git diff --check` is clean.
- **Persistence re-check:** The focused checks ran after the full suite and observed the same
  unchanged documentation and task graph.

## Plan-required visual inspection

- **Classification:** PASS
- **Interface:** UI (manual local-render inspection; not a graduated application UI use case)
- **Setup:** GitHub's API generated `readme-en.html` and `readme-es.html` beside the shared banner
  under `/tmp/loqui-readme-render/`. Agent browser discovery returned no available browser (`[]`),
  so the owner performed the visual observation outside that unavailable automation surface.
- **Steps:**
  1. Open `/tmp/loqui-readme-render/readme-en.html` in a browser and inspect its top, engine table,
     command blocks, numbered lists, and maintainer disclosure.
  2. Open `/tmp/loqui-readme-render/readme-es.html` and compare the same elements and reading order.
- **Verification:** The owner confirmed both renders. The banner is visible; language navigation is
  near the top; tables fit; lists and code blocks render correctly; and the maintainer disclosure is
  collapsed by default in both languages.
- **Persistence re-check:** N/A — the renders are stateless local files. Source-level parity and a
  second GitHub API render independently confirmed the same document structure before inspection.
- **Artifacts:** No screenshot or trace was created or committed.

## Residual state

- Temporary rendered HTML remains under `/tmp/loqui-readme-render/`; it is outside the repository.
- No remote state was changed and no credentials or personal data were captured.
- All required journeys pass; this report can bind the E2E workflow gate.
