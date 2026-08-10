# E2E report — GitHub macOS release automation

- **Feature:** Protected manual GitHub macOS release automation
- **Branch:** `feat/github-macos-release`
- **Commit under test:** `5d88c9ccc92b660b28808c669c2f78b03ac12c7a` plus the uncommitted feature diff
- **Executed at:** `2026-08-10T19:55:41Z`
- **Environment reverified at:** `2026-08-10T20:06:00Z`

VERDICT: FAIL

The repository-owned CLI journeys and live Environment policy pass. Shipping remains blocked only
because the workflow does not exist on default `main`; the live public-release journeys therefore
cannot be dispatched before the one-time bootstrap merge.

## GMR-CLI-01

- **Classification:** PASS
- **Interface:** CLI
- **Command:** Two real `scripts/github-release.sh preflight` invocations against remote `main`, each
  with fresh output/summary files and the authenticated GitHub CLI token supplied only through the
  environment.
- **Observed output:** `sha=5d88c9ccc92b660b28808c669c2f78b03ac12c7a`, `version=0.1.0`,
  `tag=v0.1.0`, `dmg_name=Loqui-0.1.0-macos-arm64.dmg`; exit 0.
- **Verification:** The two output files and summaries were byte-for-byte equal. Remote `main`, local
  `HEAD`, the dispatch SHA, published-Release absence, and tag absence were re-read through the CLI.
- **Persistence re-check:** The second invocation observed the same immutable metadata.

## GMR-CLI-02

- **Classification:** PASS
- **Interface:** CLI
- **Command:** Two successive version reads and two successive `--dmg-name` reads through
  `scripts/release-version.sh`.
- **Observed output:** `version=0.1.0`; `dmg=Loqui-0.1.0-macos-arm64.dmg`; exit 0.
- **Verification:** Both repetitions were byte-for-byte equal and the DMG matched the canonical name
  derived from the version.
- **Persistence re-check:** The second pair observed the same checked-in metadata.

## GMR-CLI-03

- **Classification:** PASS
- **Interface:** CLI
- **Command:** Two successive read-only `xcrun notarytool history` requests using Keychain profile
  `loqui-notary`; JSON bodies were discarded to avoid retaining account metadata.
- **Observed output:** No response body retained; both commands exited 0.
- **Verification:** Apple authenticated both requests and no submission command was executed.
- **Persistence re-check:** The second history request authenticated successfully.

## GMR-API-01

- **Classification:** PASS
- **Interface:** API
- **Command:** Two Environment GETs, one deployment-branch-policy GET, and an Environment secret-name
  metadata listing through authenticated GitHub CLI/API calls.
- **Observed output:** Required reviewer `Juan-Motta`; `prevent_self_review=false`; custom deployment
  policy contains exactly `main`; all five expected Environment secret names exist.
- **Verification:** Environment protection, exact branch policy, and the five-name secret contract
  pass. The App Store Connect Team Key also authenticated successfully through a separate read-only
  `notarytool history` call. No secret value was queried or printed.
- **Persistence re-check:** The second Environment GET was byte-for-byte equal to the first.

## GMR-LIVE-01

- **Classification:** FAIL_INFRA
- **Interface:** CLI
- **Observed output:** Not dispatched.
- **Verification:** Blocked because the workflow is not on default `main`. All five Environment
  secrets are configured; no public tag or Release was created.
- **Persistence re-check:** N/A; no live resource was created.

## GMR-LIVE-02

- **Classification:** FAIL_INFRA
- **Interface:** CLI
- **Observed output:** Not dispatched because `GMR-LIVE-01` has not produced a public release.
- **Verification:** No duplicate-release claim is made from static evidence.
- **Persistence re-check:** N/A; no live resource was created.

## GMR-LIVE-03

- **Classification:** FAIL_INFRA
- **Interface:** CLI
- **Observed output:** Not executed because there is no live workflow run or Actions evidence
  artifact to inspect.
- **Verification:** Local sanitizer and workflow-policy tests pass, but they do not graduate this live
  security journey.
- **Persistence re-check:** N/A; no live resource was created.

## GMR-LIVE-04

- **Classification:** FAIL_INFRA
- **Interface:** CLI
- **Observed output:** Not dispatched because the workflow is not on default `main`.
- **Verification:** The live Environment API independently confirms the exact `main` branch policy;
  no temporary branch was created from an unshipped workflow.
- **Persistence re-check:** The second Environment GET confirmed the policy remained unchanged.

## Residual state

- No tag, Release, notarization submission, or temporary remote branch was created.
- Temporary local output/summary files from the read-only preflight journey were removed.
- No credential value was written to this report.

## Bootstrap gate

The deterministic gate checker exited 1 with exactly one unchecked box:

```text
check-gates: profile 'standard' — 5/6 boxes checked — UNMET.
Unchecked ship-gate boxes (do NOT ship):
  ✗ E2E verified via verify-e2e (report: docs/e2e/reports/2026-08-10-github-macos-release.md)
```

This is not represented as a passing gate. The workflow must first be bootstrapped onto `main`; an
owner-approved one-time exception is required before the implementation commit and PR.
