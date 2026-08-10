# GitHub macOS release automation research

Checked: 2026-08-08

## Questions

1. Can a GitHub-hosted runner build Loqui's Apple Silicon DMG with the existing release pipeline?
2. How should Developer ID and App Store Connect credentials be exposed to the workflow?
3. How can the workflow guarantee that it releases the intended `main` commit and version exactly once?
4. When should the Git tag and public GitHub Release be created so a failed Apple release does not look published?
5. What should be changed in the repository, and what must instead be configured in GitHub?

## Verified findings

### Runner and repository fit

- GitHub currently provides an Apple Silicon `macos-26` hosted-runner label. GitHub-hosted standard
  runners are free for public repositories, and this repository is public. Sources:
  [GitHub-hosted runners reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners),
  [runner-images repository](https://github.com/actions/runner-images), and
  [macOS 26 arm64 image inventory](https://github.com/actions/runner-images/blob/main/images/macos/macos-26-arm64-Readme.md),
  checked 2026-08-08.
- `go.mod` requires Go 1.25.0. The frontend has a committed `package-lock.json`; the build also
  requires Node/npm, CMake, jq, Swift, Xcode command-line tools, Wails, and Homebrew-accessible
  dependencies. SDL2 is now built from its exact repo-pinned SHA rather than installed from
  Homebrew. Those prerequisites can be installed or pinned on the hosted runner.
- `scripts/task.sh` installs the repository's pinned Wails version, and `scripts/vendor-speech-sdk.sh`
  downloads a pinned, checksum-verified Azure Speech SDK. The Whisper builder checks out a pinned
  upstream revision. A clean hosted runner is therefore compatible with the existing reproducible
  inputs, subject to the normal availability of those upstream downloads.
- `build/config.yml` is the existing version authority and currently declares `0.1.0`. The release
  script already reads that value and publishes
  `bin/release/Loqui-<version>-macos-arm64.dmg`; the Action should use the same source rather than
  introduce a second version input.
- The current `release:macos` task already performs the important product work: clean helper builds,
  bundle assembly and auditing, inside-out Developer ID signing, DMG creation and signing,
  notarization, ticket/log validation, stapling, Gatekeeper verification, and evidence generation.
  CI should call this task rather than reproduce those steps in workflow YAML.

### Manual trigger, approval, and least privilege

- `workflow_dispatch` provides the requested manual Action. A workflow file must exist on the
  default branch before GitHub exposes its manual Run button. Source:
  [Triggering a workflow](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow),
  checked 2026-08-08.
- A GitHub Environment can require a reviewer. A job referencing that Environment cannot access its
  environment secrets until the protection rules pass. Public repositories support Environments and
  the relevant deployment-protection features. Sources:
  [Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
  and [Reviewing deployments](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/review-deployments),
  checked 2026-08-08.
- The safe two-stage shape is therefore a secret-free preflight job followed by a release job bound
  to a protected `release` Environment. The latter can be limited to `main`, require human approval,
  and hold all Apple credentials.
- GitHub recommends granting `GITHUB_TOKEN` only the permissions a workflow needs. Validation needs
  `contents: read`; only the final release job needs `contents: write` to create the tag and Release.
  Source: [Using `GITHUB_TOKEN` for authentication](https://docs.github.com/en/actions/tutorials/authenticate-with-github_token),
  checked 2026-08-08.
- Repository secrets are not passed to workflows originating from forks, but this design is narrower:
  it has no `pull_request` or `push` trigger at all, and its Apple secrets live behind the protected
  Environment. Source: [Managing GitHub Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository),
  checked 2026-08-08.

### Signing and notarization credentials

- GitHub's official macOS signing example stores a base64-encoded `.p12` plus its password as
  secrets, creates a temporary Keychain, imports the certificate, configures `apple-tool:` access,
  builds, and deletes the temporary Keychain. Source:
  [Installing an Apple certificate on macOS runners](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications),
  checked 2026-08-08.
- `xcrun notarytool` on the installed Xcode supports App Store Connect API-key authentication using
  the `.p8` key, key ID, and issuer ID. `store-credentials` can save that authentication as a named
  profile in an explicitly selected Keychain; `submit`, `history`, and `log` accept both a profile
  and a Keychain path. Sources:
  [TN3147: Migrating to the latest notarization tool](https://developer.apple.com/documentation/technotes/tn3147-migrating-to-the-latest-notarization-tool)
  and [Notary API](https://developer.apple.com/documentation/notaryapi), checked 2026-08-08, plus
  local `xcrun notarytool help` verification.
- The existing script passes only `--keychain-profile`. To avoid modifying the runner's login
  Keychain or relying on global search-list state, it should gain one optional CI-safe input for an
  explicit notarization Keychain path and append `--keychain <path>` to each `notarytool` call.
  Local use remains unchanged when that input is absent.
- No provisioning profile is required for this Developer ID distribution path. The CI secrets are
  the exported Developer ID `.p12`, its export password, the App Store Connect `.p8`, its key ID,
  and its issuer ID. The temporary Keychain password should be generated per run rather than stored.

### Deterministic tagging and publication

- A manual run is tied to a specific dispatch commit. Guarding `refs/heads/main`, recording that SHA
  in preflight, and checking that the remote `main` tip still equals it before release prevents an
  approval delay from silently releasing an older commit. If `main` advances, the run should fail
  and the operator should dispatch a new one.
- Preflight must parse the version from `build/config.yml`, require strict semantic version syntax,
  derive `v<version>`, and fail if either that remote tag or a GitHub Release already exists. The
  Action must not edit or commit the version.
- `gh release create` can create a missing tag at a specific `--target`, generate notes, attach
  assets, and publish only after its internal draft/upload phase. It also supports
  `--fail-on-no-commits`. Source: [`gh release create`](https://cli.github.com/manual/gh_release_create),
  checked 2026-08-08 and verified against the locally installed CLI help.
- The tag and public Release should be the final step, after the signed/notarized/stapled DMG and its
  SHA-256 have passed verification. Passing the DMG and checksum to the same `gh release create`
  invocation reduces the window in which a tag or Release exists without its required asset.
- GitHub CLI may leave a draft or tag if publication fails partway through. Automatically deleting
  it is unsafe when the client result is ambiguous. Failure handling should inspect and report the
  residual state, leave it recoverable, and refuse a subsequent normal run until a human resolves it.

## Prior art and approach comparison

### Repository-owned shell plus GitHub CLI

Use official setup/checkout actions for the runner, keep Apple product logic in repository scripts,
and use the preinstalled authenticated `gh` CLI only for the GitHub Release transaction. This adds
the least new supply-chain surface and keeps the workflow readable.

### Third-party release action

An action such as a generic GitHub Release uploader can reduce a few shell lines, but it adds an
external action with write access to repository contents and does not simplify Apple signing or
notarization. It offers little value here.

### Direct REST release state machine

A custom script could create a draft Release, upload and verify each asset, create or reconcile the
tag, and finally publish. This offers the strongest retry/idempotency control, but it is substantially
more code and testing for a first release pipeline. It is a reasonable later step only if partial
GitHub publication failures become an observed problem.

## Confirmed product decisions

- Run manually, not on pushes to `main`.
- Use `build/config.yml` as the version source; never modify `main`; fail if `v<version>` exists.
- Use the GitHub-hosted Apple Silicon runner recommended for the current Xcode toolchain.
- Run secret-free preflight first, then require approval in a protected `release` Environment.
- Authenticate notarization with an App Store Connect API key, not an Apple ID app-specific password.
- After every build and Apple verification succeeds, automatically publish the Git tag and GitHub
  Release with generated notes, the DMG, and its SHA-256.
- If any prior stage fails, create neither the tag nor the public Release.

## Design implications

- Use two jobs. `preflight` validates the branch/SHA, version/tag uniqueness, repository checks, and
  release prerequisites without secrets. `release` depends on it, references the protected
  Environment, revalidates the SHA and version, builds on a fresh arm64 runner, and publishes last.
- Pin the checkout to the preflight SHA and use a workflow concurrency group with cancellation
  disabled so two release operators cannot race one another.
- Pin third-party/official action revisions by immutable commit SHA; keep write permission and Apple
  secrets scoped to the protected release job.
- Keep the signed DMG and checksum on the GitHub Release. Preserve the existing sanitized detailed
  evidence as a short-retention Actions artifact rather than attaching it to the Release. Because
  the repository is public, do not assume that artifact is confidential.
- Always clean the `.p12`, `.p8`, and temporary Keychain with a final `if: always()` step. Logs and
  artifacts must contain neither credentials nor decoded secret material.
- Add explicit CI tests for version/tag preflight, Keychain argument construction, workflow policy,
  and failure-before-publication behavior. The existing local release tests remain the product
  pipeline regression suite.

## Remaining risks and closure

- **Runner-image drift:** pin Go/Node/action revisions and assert required native tools at preflight;
  treat the hosted image label as an intentionally reviewed moving dependency.
- **Upstream download outage:** dependency fetches can still fail despite pinned checksums. Do not
  publish; rerun later. Caching may optimize later but should not be required for correctness.
- **Main advances during approval:** compare the recorded dispatch SHA to the current remote `main`
  SHA immediately before consuming signing secrets. Fail stale runs rather than silently retarget.
- **Partially failed GitHub publication:** inspect and report whether a draft, tag, or published
  release exists. Avoid automatic deletion after ambiguous network failures.
- **Repository configuration:** an owner must create the `release` Environment, restrict deployment
  to `main`, add a required reviewer, and enter the five Apple secrets. Those controls cannot be
  fully established by the workflow file itself.
