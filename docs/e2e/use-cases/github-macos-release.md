# GitHub macOS release automation — E2E use cases

The `GMR-CLI-*` journeys are read-only and may run before merge. The `GMR-LIVE-*` journeys require
the workflow on the default branch and explicit owner confirmation before permanent public
distribution; static or hermetic tests never count as their evidence.

## GMR-CLI-01 — Read-only immutable preflight

- **Actor:** Release operator.
- **Scenario:** The operator checks that the current checkout is still the exact releasable `main`.
- **Interface:** CLI.
- **Intent:** Refuse stale, non-main, already-tagged, or already-published release state before any
  protected credential is requested.
- **Setup:** Authenticated `gh`; checkout content based on the current remote `main`; no target tag or
  published Release exists.
- **Steps:**
  1. Invoke `scripts/github-release.sh preflight` with `GITHUB_REF=refs/heads/main`, the exact remote
     `main` SHA, repository name, and a read-only GitHub token.
  2. Invoke it again against the unchanged remote state using a fresh output and summary file.
- **Verification:** Both invocations report the same version, tag, DMG name, and commit; neither
  invocation calls a publication operation. Protected `--check-drafts` behavior remains covered by
  the hermetic release CLI regression because a read-only token cannot observe drafts.
- **Persistence:** The second invocation re-reads Git and GitHub and observes the same immutable
  release metadata.

## GMR-CLI-02 — Canonical version and artifact name

- **Actor:** Release operator.
- **Scenario:** The operator reads the single repository-owned release version and expected DMG.
- **Interface:** CLI.
- **Intent:** Ensure every release surface derives the same stable semantic version and filename.
- **Setup:** `build/config.yml` contains one quoted stable `info.version`.
- **Steps:**
  1. Invoke `scripts/release-version.sh` and record the version.
  2. Invoke `scripts/release-version.sh --dmg-name`, then repeat both reads.
- **Verification:** The version is stable `MAJOR.MINOR.PATCH`; the artifact is exactly
  `Loqui-<version>-macos-arm64.dmg`; repeated reads are byte-for-byte identical.
- **Persistence:** The repeated CLI reads observe the same checked-in metadata without mutation.

## GMR-CLI-03 — Existing Apple notarization authentication

- **Actor:** Release operator.
- **Scenario:** The operator confirms the established local notarization identity still authenticates
  before migrating the same release path to a temporary CI Keychain.
- **Interface:** CLI.
- **Intent:** Prove Apple accepts the configured team credentials without submitting new software.
- **Setup:** The local `loqui-notary` Keychain profile already exists; no password or credential is
  printed or copied.
- **Steps:**
  1. Invoke `xcrun notarytool history --keychain-profile loqui-notary --output-format json` and
     discard its response body.
  2. Repeat the same read-only history request.
- **Verification:** Both calls authenticate successfully and create no notarization submission.
- **Persistence:** The second request proves the Keychain-backed authentication remains available.

## GMR-API-01 — Protected GitHub Environment policy

- **Actor:** Repository maintainer.
- **Scenario:** The maintainer checks the live `release` Environment without reading secret values.
- **Interface:** API.
- **Intent:** Confirm approval, deployment-branch restriction, and required secret names are bound to
  the protected job.
- **Setup:** Authenticated GitHub CLI with administration access to `Juan-Motta/loqui`.
- **Steps:**
  1. GET the `release` Environment and its deployment branch policies through GitHub's API.
  2. List Environment secret metadata, then GET the Environment again as a persistence check.
- **Verification:** The Environment requires reviewer `Juan-Motta`, allows exactly custom branch
  `main`, and lists the five expected secret names without exposing values.
- **Persistence:** The second GET returns the same protection policy after the secret-metadata read.

## GMR-LIVE-01 — First protected public release

- **Actor:** Repository owner acting as release operator and Environment reviewer.
- **Scenario:** The owner publishes the first permanent macOS GitHub Release from `main`.
- **Interface:** CLI.
- **Intent:** Produce a signed, notarized, stapled DMG and immutable tag with exactly two public
  assets.
- **Setup:** Workflow merged to `main`; all five Environment secrets configured; owner separately
  confirms the exact version and commit are fit for permanent public distribution.
- **Steps:**
  1. Dispatch the `Release` workflow at `main`, inspect the preflight outputs, and approve the
     protected deployment.
  2. Wait for completion; download the DMG and checksum into a unique temporary directory; verify
     SHA-256, Gatekeeper, mounted app signature, and mounted app Gatekeeper assessment.
- **Verification:** The run succeeds; tag target equals the recorded SHA; the non-draft,
  non-prerelease Release contains only the canonical DMG and checksum; sanitized evidence is retained
  for 14 days; the downloaded app passes all local checks.
- **Persistence:** A fresh `gh release view` and fresh tag lookup observe the same SHA and two assets.

## GMR-LIVE-02 — Duplicate release fails early

- **Actor:** Release operator.
- **Scenario:** The operator accidentally dispatches the unchanged version after it is public.
- **Interface:** CLI.
- **Intent:** Preserve published version immutability and avoid accessing Apple credentials.
- **Setup:** `GMR-LIVE-01` passed and the repository version is unchanged.
- **Steps:**
  1. Dispatch `Release` again at `main`.
  2. Inspect the failed preflight and query the existing Release afterward.
- **Verification:** Preflight fails because the tag/Release exists; no protected release step runs;
  the original Release remains unchanged.
- **Persistence:** The follow-up Release query observes the original SHA and assets.

## GMR-LIVE-03 — No secret or checkout-path disclosure

- **Actor:** Security reviewer.
- **Scenario:** The reviewer audits the successful live release surfaces.
- **Interface:** CLI.
- **Intent:** Ensure CI credentials and runner paths never become public or retained in evidence.
- **Setup:** `GMR-LIVE-01` passed; reviewer can read Actions logs/artifacts and public Release assets.
- **Steps:**
  1. Download logs, summaries, Release assets, and the sanitized evidence artifact into a temporary
     directory.
  2. Scan for `.p12`/`.p8` material, passwords, decoded credentials, environment dumps, and hosted
     checkout paths; repeat the public-asset listing afterward.
- **Verification:** No sensitive material or hosted checkout path is found; public assets remain only
  the DMG and checksum.
- **Persistence:** The repeated public listing confirms the audit did not mutate the Release.

## GMR-LIVE-04 — Non-main dispatch blocked

- **Actor:** Repository maintainer.
- **Scenario:** The maintainer attempts a release from a temporary non-main branch.
- **Interface:** CLI.
- **Intent:** Prove the repository-owned exact-main invariant blocks the run before protected access.
- **Setup:** Workflow exists on `main`; create a temporary branch at the released SHA through normal
  Git/GitHub commands and record its exact name for cleanup.
- **Steps:**
  1. Dispatch `Release` for the temporary branch and observe the preflight result.
  2. Re-read the Environment branch policy, then delete only the temporary branch created by Setup.
- **Verification:** Repository preflight rejects the non-main ref before the protected job; the API
  still reports exactly `main` as the Environment policy.
- **Persistence:** The follow-up policy read is unchanged and the created temporary branch is removed.
