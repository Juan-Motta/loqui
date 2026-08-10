# GitHub macOS release automation design

Status: approved in conversation on 2026-08-09; review corrections incorporated on 2026-08-10

Research: `docs/research/2026-08-08-github-macos-release-automation.md`

## Goal

Provide a manually triggered GitHub Action named **Release** that takes the current tip of `main`,
reads Loqui's version from `build/config.yml`, builds the existing Apple Silicon Developer ID
release on a GitHub-hosted runner, notarizes and verifies its DMG, and then publishes a matching Git
tag and public GitHub Release containing the DMG and its SHA-256 checksum.

The workflow must expose no Apple credential during its secret-free validation phase and must wait
for an explicit approval through a protected GitHub Environment before the signing job starts. A
failed build or Apple verification must never create a tag or public Release.

## Sequencing constraint

This automation depends on the Developer ID pipeline formerly implemented on
`feat/developer-id-release`. Its review, second-Mac E2E, and ship gates passed, and PR #1 placed it on
`main` as commit `5d88c9c` on 2026-08-10. Automation implementation starts from that commit on the
separate `feat/github-macos-release` branch.

## Confirmed product decisions

- The workflow is manual (`workflow_dispatch`); pushes to `main` do not create releases.
- The selected source is `main`, and the release is allowed only while its recorded commit remains
  the remote tip of `main`.
- `build/config.yml` is the sole version authority. The workflow neither edits the version nor
  commits to `main`.
- Stable versions use exactly `MAJOR.MINOR.PATCH`; the corresponding tag is `vMAJOR.MINOR.PATCH`.
- An existing tag or GitHub Release for that version is a hard failure, not an overwrite or update.
- The build uses GitHub's Apple Silicon `macos-26` hosted runner.
- A secret-free preflight completes before the protected `release` Environment asks for approval.
- Notarization uses an App Store Connect API key, not an Apple ID password.
- Successful publication is automatic and public, with generated notes, the DMG, and a SHA-256 file.
- The existing repository-owned `release:macos` task remains the authority for assembly, signing,
  notarization, stapling, Gatekeeper assessment, and local artifact publication.

## Non-goals

- Releasing automatically on a push, merge, schedule, or version-file change.
- Accepting a version, tag, commit, branch, draft flag, or prerelease flag as workflow input.
- Updating `build/config.yml`, creating a version bump commit, or pushing to `main`.
- Intel or universal binaries; the first CI release remains Apple Silicon-only.
- Mac App Store distribution, provisioning profiles, or App Store uploads.
- Reimplementing the Apple release pipeline in workflow YAML.
- Transferring an unsigned app from preflight to the protected job.
- Automatically deleting a tag or Release after an ambiguous GitHub API failure.
- Adding dependency caches in the first implementation. They may be added later without changing
  correctness if runner time becomes material.

## Approaches considered

| Approach | Complexity | Supply-chain surface | Retry control | Fit |
| --- | --- | --- | --- | --- |
| Repository scripts plus `gh release create` | Low | Official setup actions plus GitHub CLI | Sufficient, with explicit residual-state reporting | **Selected**: preserves the existing release authority and adds little machinery |
| Third-party GitHub Release action | Low | Adds another action with `contents: write` | Depends on the action | Rejected: saves few lines and does not simplify Apple work |
| Custom GitHub REST state machine | High | Repo-owned code only | Strongest draft/asset/tag reconciliation | Deferred: useful only if partial-publication failures become an observed problem |

## Architecture

The repository adds one workflow with two jobs and a repository-owned CI preflight seam:

```text
workflow_dispatch on main
          |
          v
preflight (macos-26, no Environment, contents:read)
  - validate ref, remote-main SHA, version, tag and Release absence
  - install the pinned toolchain and dependencies
  - generate bindings and build frontend/dist from the clean checkout
  - run ./scripts/task.sh check
  - expose immutable SHA/version/tag outputs
          |
          v
protected Environment: release
  - wait for required reviewer approval
          |
          v
release (fresh macos-26 runner, contents:write)
  - checkout the exact preflight SHA
  - revalidate main/version/nonexistence
  - create temporary files and Keychain
  - import Developer ID certificate and store notary API profile
  - run ./scripts/task.sh release:macos
  - verify the expected DMG and checksum
  - upload sanitized evidence as a short-retention Actions artifact
  - revalidate main/version/nonexistence immediately before publication
  - create and verify the tag and public GitHub Release
  - always remove temporary credential material and Keychain
```

The two jobs deliberately use separate runners. Preflight proves the repository at the selected SHA
is healthy without receiving signing credentials. The release job rebuilds from that SHA instead of
trusting an unsigned artifact produced outside the protected Environment.

## Components and boundaries

### Release workflow

`.github/workflows/release.yml` owns GitHub orchestration only:

- manual trigger and concurrency;
- runner selection and immutable checkout;
- explicit `GITHUB_TOKEN` permissions;
- Go 1.25, Node 24, npm lockfile installation, Wails, and native dependency setup;
- protected Environment binding;
- temporary credential and Keychain lifecycle;
- invocation of repository scripts;
- evidence upload and GitHub Release publication.

Official and third-party Actions are pinned to full commit SHAs. The file contains no certificate,
password, Apple identifier, decoded key, or operator email. Shell embedded in YAML is kept to
environment setup and short calls; policy and validation behavior belong in tested repository
scripts.

### Release metadata reader

A small repository script is the single parser for `info.version` in `build/config.yml`. It prints
one validated `MAJOR.MINOR.PATCH` value, or the canonical DMG filename with `--dmg-name`, and exits
nonzero for an absent, duplicate, empty, quoted incorrectly, or non-stable-semver value. Both
`scripts/release-macos.sh` and CI preflight use this reader so artifact, tag, and plist expectations
cannot drift between parsers.

The derived names are deterministic:

| Value | Format |
| --- | --- |
| Version | `MAJOR.MINOR.PATCH` |
| Tag | `vMAJOR.MINOR.PATCH` |
| DMG | `Loqui-MAJOR.MINOR.PATCH-macos-arm64.dmg` |
| Checksum | `Loqui-MAJOR.MINOR.PATCH-macos-arm64.dmg.sha256` |
| Evidence artifact | `loqui-release-evidence-vMAJOR.MINOR.PATCH` |

### GitHub preflight script

A repository script validates the GitHub release contract and writes `sha`, `version`, `tag`, and
`dmg_name` to `GITHUB_OUTPUT` when invoked by Actions. Its external Git/GitHub lookups remain behind
ordinary commands so tests can replace them with deterministic fakes.

Preflight requires:

1. `GITHUB_REF` is exactly `refs/heads/main`.
2. `GITHUB_SHA` is a full commit SHA and matches the checked-out `HEAD`.
3. `git ls-remote origin refs/heads/main` returns that same SHA.
4. GitHub CLI is numeric version 2.93.0 or newer and exposes the structural `--latest` flag; the
   official `macos-26` arm64 image manifest currently provides 2.96.0.
5. An authenticated repository probe succeeds before interpreting a Release lookup as absent.
6. The metadata reader returns a stable semantic version and canonical DMG name.
7. Neither `refs/tags/v<version>` nor a published GitHub Release named by that tag exists. The
   secret-free job cannot reliably observe drafts because its token is read-only.

The release job invokes the same checks with the preflight SHA, version, and tag as immutable
expectations and enables the paginated draft check while its token has `contents: write`. Any
mismatch or existing draft is a hard failure before Apple credentials are imported.

### Existing macOS release pipeline

`./scripts/task.sh release:macos` continues to build and publish the verified local DMG under
`bin/release/`. CI does not bypass its preflight, audits, inside-out signing, notary-log checks,
stapling, Gatekeeper assessment, or atomic local publication.

The only credential-interface extension is optional `LOQUI_NOTARY_KEYCHAIN`. When set,
`scripts/release-macos.sh` passes both its existing `--keychain-profile` and the explicit
`--keychain <path>` to every `notarytool history`, `submit`, and `log` call. When unset, local
`loqui-notary` behavior is byte-for-byte equivalent at the command boundary.

This avoids storing CI credentials in the runner's login Keychain while preserving the already
validated local operator path.

### GitHub publication

After `release:macos` exits zero, the workflow requires the exact expected DMG, creates its checksum
from the basename within `bin/release`, and verifies that checksum before publication. The public
assets are only:

- `Loqui-<version>-macos-arm64.dmg`;
- `Loqui-<version>-macos-arm64.dmg.sha256`.

The existing sanitized evidence directory is uploaded through the official artifact action with a
14-day retention period and `if-no-files-found: error`. It is not attached to the GitHub Release,
but the public repository means it must not be treated as confidential. Evidence upload occurs
before publication; a failed audit-trail upload blocks publication but does not invalidate the local
DMG.

If notarization fails, `release:macos` copies only the submission and notary-log JSON into a
configured CI directory, normalizes checkout/staging paths, applies the same secret-field scan, and
atomically exposes the directory only after those checks pass. A failure-only artifact step uploads
that exact directory with 14-day retention and never uploads a broad temporary-directory glob.

The workflow then performs its final remote-main/tag/Release check and calls `gh release create`
once with:

- tag `v<version>`;
- target equal to the exact preflight SHA;
- title `Loqui <version>`;
- generated notes;
- latest-release selection;
- both required assets attached in the same command.

It does not create or push the tag separately. After the command succeeds, it queries the
authenticated GitHub API with a bounded retry to verify the tag target, then queries the Release and
verifies that it is public, names the expected tag, and lists both exact asset names. A failed
post-publication verification fails the workflow and reports the Release URL/state; it does not
delete anything automatically.

## Data and control flow

### Dispatch snapshot

The source of truth is the `main` commit selected at dispatch. Preflight records its SHA. Because
Environment approval may take time, the release job rejects the run if the remote tip of `main` no
longer matches. It repeats that check immediately before GitHub publication; therefore a push to
`main` during the Apple build also makes the run stale and prevents tag creation.

This deliberately favors “release the latest `main`” over completing an expensive stale run. The
operator starts a new manual run for the new tip.

### Concurrency

The workflow uses one repository-wide concurrency group for macOS publication with
`cancel-in-progress: false`. A second manual run waits instead of terminating a signing/notarization
run. Version/tag checks are still repeated after the wait because concurrency cannot prevent a human
or another integration from creating the tag.

### Toolchain setup

Both jobs use the same explicit setup contract where relevant:

- Apple Silicon `macos-26`;
- Go 1.25, matching `go.mod`;
- Node 24, compatible with the committed Vite engine requirement;
- GitHub CLI 2.93.0 or newer (the selected image currently documents 2.96.0);
- `CI=true ./scripts/task.sh common:build:frontend` before `check`, so a clean runner generates
  Wails bindings and `frontend/dist`; the shared npm dependency Task selects `npm ci` against the
  committed lockfile whenever `CI=true`, so the protected
  `release:macos` package build remains lockfile-deterministic while local development retains
  `npm install`;
- repository-pinned Wails through `scripts/task.sh`;
- `scripts/vendor-speech-sdk.sh` before either job invokes a task, because the pinned native Azure
  framework is intentionally gitignored and `release:macos` requires it during its own preflight;
- CMake, jq, and the native Apple/Xcode tools required by release preflight. SDL2 is built from the
  exact repo-pinned source SHA by the existing helper builder.

Preflight builds the frontend and then runs `./scripts/task.sh check`. The protected job does not repeat the entire check suite,
but `release:macos` performs its own clean product build and release-critical tests/audits. Both jobs
checkout the exact same commit with persisted Git credentials disabled unless a later step requires
authenticated GitHub access explicitly.

## Credential design

### Environment configuration

The repository owner creates a GitHub Environment named `release` with:

- deployment branches restricted to `main`;
- at least one required reviewer;
- the five Environment secrets below.

Setup is verified through GitHub's API rather than accepted from prose: the Environment response
must show the required-reviewer rule and custom branch policy, and its deployment-branch-policy list
must contain exactly the `main` branch rule. A negative live case dispatches from a throwaway branch
and confirms the repository's exact-main preflight prevents the protected job from being reached;
the Environment policy itself is independently evidenced by the API inspection.

For a single-maintainer repository, “Prevent self-review” remains disabled so the maintainer can
approve their own manually dispatched release. If a second trusted maintainer becomes responsible
for releases, the repository can enable it without changing the workflow.

| Secret | Purpose |
| --- | --- |
| `MACOS_CERTIFICATE_P12_BASE64` | Base64-encoded Developer ID Application certificate plus private key |
| `MACOS_CERTIFICATE_PASSWORD` | Password used when the `.p12` was exported |
| `APP_STORE_CONNECT_API_KEY_P8` | Contents of the App Store Connect private API key |
| `APP_STORE_CONNECT_KEY_ID` | API key identifier |
| `APP_STORE_CONNECT_ISSUER_ID` | App Store Connect issuer UUID |

The repository's Actions settings must permit the workflow's explicitly requested
`contents: write` access. No pull-request approval permission is needed.

### Temporary Keychain lifecycle

The release runner:

1. creates private temporary paths under `RUNNER_TEMP`;
2. generates a random per-run Keychain password;
3. sets `umask 077`, decodes the `.p12`, writes the `.p8`, and validates both are non-empty without
   printing either value;
4. creates and unlocks a temporary Keychain;
5. imports the Developer ID identity and configures `apple-tool:`/`apple:` partition access;
6. prepends that Keychain to the runner's existing user search list;
7. stores a validated `loqui-ci-notary` profile in that same Keychain using key ID and issuer ID;
8. exports `LOQUI_NOTARY_PROFILE=loqui-ci-notary` and the explicit Keychain path only to the
   repository release command.

An `if: ${{ always() }}` cleanup step deletes the decoded files and temporary Keychain whether setup,
build, Apple submission, evidence upload, or GitHub publication succeeds or fails. Cleanup uses
exact paths created by the workflow, never `$HOME`, `~`, a glob, or an unresolved variable.

GitHub masking is added for sensitive values that are not automatically secret-masked. Commands do
not use shell tracing, print environment dumps, or interpolate secret values into step names,
outputs, summaries, artifacts, or command-line diagnostics.

## Permissions

The workflow default is `contents: read`. Preflight receives no Environment and no Apple secrets.
The release job overrides only `contents: write`, which is required for the tag and GitHub Release.
It receives no `pull-requests`, `issues`, `packages`, `id-token`, or administration permission.

The GitHub token is exposed to `gh` only for the short remote checks and final publication steps. It
is not forwarded to build scripts, npm lifecycle environment, Apple tools, or uploaded evidence.

## Failure behavior

### Before GitHub publication

The following failures exit nonzero without creating a tag or public Release:

- dispatch from anything other than `main`;
- stale or malformed SHA;
- invalid or inconsistent version metadata;
- existing tag or Release;
- failed repository checks or dependency setup;
- missing, malformed, or invalid Environment credentials;
- missing or ambiguous Developer ID identity after import;
- failed build, audit, signature, notarization, ticket inspection, staple, DMG verification, or
  Gatekeeper assessment;
- unexpected DMG filename or failed SHA-256 verification;
- missing/sensitive evidence or failed evidence upload;
- `main` moving before final publication.

Apple failures preserve sanitized diagnostics according to `release:macos`; credential cleanup
still runs afterward.

### During GitHub publication

`gh release create` internally uses a draft while uploading, but a network or API failure can leave
a draft or tag even when the client exits nonzero. The workflow responds by querying tag and Release
state and writing a concise diagnostic to the job summary.

The protected release job enumerates the authenticated Releases API with pagination so an existing
draft carrying the target tag name is visible even though the published-release-by-tag endpoint
returns 404 and no Git ref exists yet. The read-only job retains the published-by-tag and Git-ref
checks as independent guards without claiming draft visibility.

It never runs automatic `gh release delete`, `git push --delete`, or equivalent cleanup because a
client-side failure can be ambiguous after the server successfully publishes. A subsequent ordinary
run will pass the read-only job but fail the protected draft/published uniqueness revalidation until
the maintainer inspects and deliberately resolves the residual state.

The automation treats existing versions as immutable. Documentation may describe an explicit human
remediation only for a partial, unannounced publication: inspect the exact remote state first, then
deliberately delete the incomplete Release and its tag before retrying. A published release is never
deleted or replaced; a bad public build is superseded by a new patch version.

### After successful publication

Post-publication verification checks the public state and asset names. A mismatch fails visibly and
reports the URL but does not mutate the now-public Release. The maintainer resolves it explicitly;
the workflow never replaces assets or moves an existing version tag.

## Testing strategy

### Hermetic shell tests

Tests use temporary repositories and fake `git`, `gh`, `security`, and `xcrun` commands to cover:

- exact version parsing and malformed/duplicate/missing version failures;
- `main`-only dispatch and checked-out/remote SHA equality;
- missing versus existing tag and Release states;
- repository-probe, HTTP-status, network, and authenticated tag-verification failures;
- immutable preflight output and release-time expectation mismatches;
- explicit Keychain propagation to `history`, `submit`, and `log`;
- preservation of the local no-Keychain-argument behavior;
- checksum naming/content and rejection of a missing or incorrectly named DMG;
- failure before the GitHub publication call for every preceding phase;
- one final publication call containing exact SHA, tag, title, notes, and both assets;
- ambiguous publication failure reporting without deletion commands.

The tests assert normalized argument arrays rather than loose output substrings so secret-handling
and publication ordering are observable without real Apple or GitHub credentials.

### Workflow policy tests

A static contract test parses or narrowly inspects `.github/workflows/release.yml` and fails unless:

- `workflow_dispatch` is the only release trigger;
- both jobs use the expected Apple Silicon runner;
- preflight has no Environment, write permission, or secret references;
- release depends on preflight and uses Environment `release`;
- only release receives `contents: write`;
- concurrency does not cancel an in-flight release;
- cleanup is unconditional;
- `if: ${{ always() }}` occurs only on the final cleanup step, never at job scope;
- clean-checkout frontend generation precedes `check`;
- publication follows `release:macos`, checksum verification, evidence upload, and final revalidation;
- every referenced Action is pinned by a full commit SHA.

This test joins `test:macos-release`, so `./scripts/task.sh check` enforces it before the workflow is
merged.

### Real integration

The first manual run from `main` is the end-to-end proof and creates an irreversible public release.
Immediately before dispatch, the owner must separately acknowledge the exact version and commit as
fit for public distribution. It must show:

1. secret-free preflight passes on a clean hosted runner;
2. the job pauses for the `release` Environment approval;
3. the temporary identity resolves to the intended Developer ID Team ID;
4. Apple accepts the DMG and the existing release script validates/staples it;
5. GitHub publishes `v<version>` against the recorded `main` SHA;
6. the Release is public and has exactly the DMG and checksum assets;
7. the evidence artifact exists with 14-day retention;
8. a subsequent run for the same version fails before approval because the tag/Release exists.
9. a throwaway-branch dispatch fails the repository's exact-main preflight before the protected job,
   while API evidence independently confirms the Environment permits exactly `main`;
10. a freshly downloaded DMG passes its checksum, controlled-quarantine Gatekeeper assessment,
    read-only mount, contained-app signature verification, and contained-app Gatekeeper assessment.

No test workflow receives production Apple secrets on a pull request. The real integration occurs
only through the approved manual workflow after the implementation is merged to `main`.

## Observability and operator guidance

The workflow job summary reports only non-secret release metadata:

- selected and current `main` SHA;
- parsed version and tag;
- phase outcome;
- notarization submission ID when available;
- checksum;
- evidence artifact name;
- final GitHub Release URL or residual-state diagnosis.

Repository documentation explains the one-time setup: export and encode the `.p12`, create the App
Store Connect API key, configure the protected Environment and its five secrets, permit the required
workflow token scope, run the manual Action from `main`, and approve the waiting deployment.

## Acceptance criteria

- A maintainer can create a signed, notarized Apple Silicon Loqui Release from GitHub without using
  the local `loqui-notary` profile.
- Running the Action requires both a manual dispatch and protected-Environment approval.
- The published tag, Release title, DMG filename, and checksum filename all derive from the one
  version in `build/config.yml`.
- The tag targets the exact `main` SHA validated by preflight, and publication is blocked if that SHA
  becomes stale.
- No tag or public Release exists when any product, Apple, checksum, evidence, or final-state check
  fails before publication.
- Existing versions are never overwritten, moved, or silently resumed.
- Apple secrets exist only in the protected job and leave neither logs nor artifacts.
- Local `LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos` remains supported.
- The repository check suite covers the workflow contract and CI-specific shell behavior without
  production credentials.
