# E2E report — GitHub macOS release automation

- **Feature:** Protected manual GitHub macOS release automation
- **Evidence branch:** `docs/github-release-live-evidence`
- **Bootstrap implementation:** `b95861b7ea5f9ffa988a31c4a8348a53fc8f54cb`
- **Released `main` commit:** `50f53f7bcc3d35637df96cda106a6ea8e1ea97da`
- **Executed:** bootstrap checks at `2026-08-10T19:55:41Z`; live journeys from
  `2026-08-10T20:16:51Z` through `2026-08-10T20:42:11Z`
- **Environment reverified:** `2026-08-10T20:42:11Z`

VERDICT: PASS

The repository-owned CLI journeys, protected hosted-runner release, downloaded-artifact checks,
security audit, duplicate-version rejection, and non-`main` rejection all pass. The owner also
installed the public DMG and confirmed that Loqui launches and works.

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

- **Classification:** PASS
- **Interface:** GitHub Actions CLI/API plus native macOS verification.
- **Observed output:** [Run `31428242122`](https://github.com/Juan-Motta/loqui/actions/runs/31428242122)
  completed successfully on `50f53f7bcc3d35637df96cda106a6ea8e1ea97da`. Secret-free preflight
  passed in 5m36s, the `release` Environment paused for approval, and the protected job completed in
  7m47s. Apple accepted submission `9498b7b6-79ae-4b92-bb09-05f4a4d2c54d` with zero issues and 27
  ticket entries.
- **Verification:** [Release `v0.1.0`](https://github.com/Juan-Motta/loqui/releases/tag/v0.1.0) is
  public, neither draft nor prerelease, targets the exact released SHA, and contains only
  `Loqui-0.1.0-macos-arm64.dmg` plus its `.sha256` file. A fresh download matched SHA-256
  `49acadc8ad7782f95e348ba4fe95f2a3ade21f933c3c53e0ddadbb805ad86bc1`. `hdiutil verify`,
  `stapler validate`, quarantined DMG Gatekeeper assessment with primary-signature context,
  read-only mount, deep/strict app signature verification, app Gatekeeper assessment, and the
  production bundle audit all exited 0. The owner independently installed the public DMG and
  confirmed that Loqui works.
- **Persistence re-check:** Fresh Release and tag reads still reported the same SHA and exact two
  assets after every negative journey.

## GMR-LIVE-02

- **Classification:** PASS
- **Interface:** CLI
- **Observed output:** [Run `31429953529`](https://github.com/Juan-Motta/loqui/actions/runs/31429953529)
  failed in `Resolve immutable release metadata` with `tag already exists: v0.1.0`.
- **Verification:** The remaining preflight steps were skipped and the protected `release` job was
  skipped without importing Apple credentials or requesting approval.
- **Persistence re-check:** The original public Release remained non-draft, non-prerelease, bound to
  the same SHA, and retained exactly its original two assets.

## GMR-LIVE-03

- **Classification:** PASS
- **Interface:** CLI
- **Observed output:** The successful run's complete logs, job summaries, two public assets, and
  `loqui-release-evidence-v0.1.0` artifact were downloaded to a unique temporary directory. The
  artifact contains exactly 14 files, was created at `2026-08-10T20:31:00Z`, expires at
  `2026-08-24T20:30:59Z`, and is not expired.
- **Verification:** Structural scans found no private-key/certificate body, unmasked credential,
  password, or token marker across logs, summaries, assets, or evidence. The evidence contains no
  runner checkout/temp path; its repository HEAD matches the released SHA and its pre/post-DMG
  designated requirements are byte-identical. Secret names and GitHub's masked `GH_TOKEN: ***`
  marker are expected and no secret value was queried for this verification.
- **Persistence re-check:** A repeated public asset listing remained exactly the DMG and checksum.

## GMR-LIVE-04

- **Classification:** PASS
- **Interface:** CLI
- **Observed output:** Temporary branch
  `e2e/github-release-non-main-v0.1.0-31428242122` was created at the released SHA. [Run
  `31430266300`](https://github.com/Juan-Motta/loqui/actions/runs/31430266300) failed in repository
  preflight with `release requires refs/heads/main`; its protected job was skipped and pending
  deployments were empty.
- **Verification:** The Environment deployment-policy API independently returned exactly one branch
  rule named `main`. No protected credentials were reached.
- **Persistence re-check:** The exact temporary branch was deleted and a fresh remote-head lookup
  returned no result. The Environment policy and public Release remained unchanged.

## Residual state

- Permanent intended state is the public immutable `v0.1.0` tag and Release at
  `50f53f7bcc3d35637df96cda106a6ea8e1ea97da`, with exactly the DMG and checksum assets.
- The sanitized Actions evidence is retained for 14 days. No failure-evidence artifact exists
  because notarization succeeded.
- The temporary non-`main` branch was deleted. Temporary downloaded verification files were removed
  after recording their non-secret results.
- No credential value was written to this report.

## Gate closure

Before the one-time bootstrap merge, the deterministic gate checker correctly exited 1 with only the
live E2E box unchecked. The owner explicitly approved that bootstrap exception. With the workflow on
`main`, all four `GMR-LIVE-*` journeys now pass and the E2E box can be checked in workflow state. The
evidence-only closeout branch must pass the deterministic gate before commit/push:

```text
check-gates: profile 'standard' — 6/6 boxes checked — PASS.
```
