# Developer ID release design

Status: approved in conversation on 2026-08-07

Research: `docs/research/2026-08-07-developer-id-release.md`

## Goal

Produce a locally built, Developer ID-signed, Apple-notarized DMG that can be installed on another
Apple Silicon Mac without Homebrew, this checkout, or ad-hoc-signature churn. After the one-time
migration from the current ad-hoc build, repeated signed builds must keep stable code identities so
macOS can recognize the app and its fn-key listener across rebuilds.

Daily Wails development must also stop churning TCC grants, without exposing the Developer ID private
key to routine builds. Development therefore uses an Apple Development identity and a separate dev
bundle identifier; production alone uses Developer ID.

## Confirmed product decisions

- Distribution is outside the Mac App Store.
- The owner has an active Apple Developer Program account.
- This work includes the complete local setup: certificate installation, notarization credentials,
  repository automation, one real notarization, and distribution verification.
- Releases are generated on this Mac, not in CI.
- The first release supports Apple Silicon (`arm64`) only.
- The app declares macOS 14 Sonoma or newer as its compatibility floor. Main, globe, Whisper, ggml,
  and the repo-built SDL declare 14.0; `macos-stt` is the sole intentional exception and declares
  exactly 26.0. Runtime Sonoma support remains unclaimed until the external macOS 14 E2E gate.
- Keep ggml BLAS, CPU, and Metal enabled. BLAS's macOS 13.3 Accelerate symbol floor is inside the
  selected macOS 14 contract.
- The output is a signed and notarized DMG.
- Day-to-day Wails builds use `Apple Development`; Developer ID is restricted to the explicit release
  command.
- Certificate private keys and Apple credentials remain in Keychain and never enter Git.

## Non-goals

- Intel or universal binaries.
- CI release automation or CI secret handling.
- Mac App Store distribution, sandboxing, or provisioning profiles.
- Restoring provider API keys from `secrets.json` to Keychain. That migration happens only after the
  new signature and Keychain behavior have been proven separately.
- Diagnosing the Apple SpeechAnalyzer stall unless the signed release itself changes the result.
- Changing the bundle identifier from `com.jualopezmo.loquigo` to the old Electron identifier.

## Current evidence

- This Mac has Xcode 26.6, `notarytool` 1.1.2, `codesign`, `stapler`, `spctl`, and `hdiutil`.
- It currently has zero valid code-signing identities.
- `bin/loqui.app` and its helpers are ad-hoc signed. The helpers' designated requirements contain a
  `cdhash`, so rebuilding changes the identity macOS records.
- The app, helpers, and Whisper dylibs are arm64. The Azure Speech framework is universal.
- Executable helpers and dylibs currently live in `Contents/Resources/helpers`, contrary to Apple's
  standard bundle locations.
- `whisper-stt` still links SDL through `/opt/homebrew/opt/sdl2/lib/libSDL2-2.0.0.dylib`, so the
  current bundle is not portable to a Mac without that Homebrew installation.
- Wails `v3.0.0-alpha2.119` signs app bundles with `codesign --deep`. Apple says complex products
  should instead identify and sign each code item from the inside out.

## Approaches considered

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness/user risk |
| --- | --- | --- | --- | --- | --- |
| Repo-owned Apple-native release pipeline | Medium: one release script plus focused packaging changes | Medium: macOS packaging and helper lookup | High: dev workflow and existing sources remain | Medium: requires a real notarization | Low: every code item and check is explicit |
| Wails built-in `sign:notarize` | Low: mostly configuration | Medium: delegates the whole bundle to one command | High | Low | High: pinned version uses `--deep`, creates no DMG, and hides nested layout errors |
| Wrap the build in an Xcode archive/export project | High: adds a second project model | High: build ownership moves away from Taskfile/Wails | Medium | High | Low after setup, but substantial long-term drift risk |

The selected approach is the repo-owned Apple-native pipeline. It follows Apple's external build
system model while retaining Wails for compilation and ordinary packaging.

## Architecture

### One release entry point

`./scripts/task.sh release:macos` is the only supported release command. It delegates to a Darwin
Taskfile task and a repo-owned release script. Individual internal phases may remain callable for
tests, but the documented operator path is one command.

The command either produces a fully verified final DMG or exits nonzero. It never reports a partial
app, a merely signed app, or an uploaded-but-rejected image as a release.

### Local configuration

- `LOQUI_SIGN_IDENTITY` optionally selects a certificate by SHA-1 hash or exact Keychain identity.
- If it is unset, preflight selects the only valid identity whose name begins with
  `Developer ID Application:`. Zero or multiple matches are an error, not a guess.
- `LOQUI_NOTARY_PROFILE` defaults to `loqui-notary`. The profile name is not secret; its stored
  Apple ID, Team ID, and app-specific password are Keychain data.
- `LOQUI_DEV_SIGN_IDENTITY` optionally selects the Apple Development identity for daily builds. If it
  is unset, development selects the only valid `Apple Development:` identity. Zero matches trigger
  the labelled ad-hoc contributor fallback described below; multiple matches are an error.
- The release task validates the profile with `notarytool` before building the expensive artifact.
- `VERSION` is read exactly from `info.version` in `build/config.yml`; it is not inferred from Git or
  supplied independently at release time.
- No checked-in file contains an Apple ID, app-specific password, certificate, private key, or
  certificate password.

### One-time operator setup

The owner creates an `Apple Development` identity for daily work and creates or downloads a
`Developer ID Application` identity through Xcode or the Apple Developer portal. Both must be
installed with their private keys and listed as valid by `security find-identity -v -p codesigning`.
The Developer ID identity is exported once as an encrypted `.p12` backup and stored securely outside
the repository; it is a release credential, not an ordinary development asset.

The owner then creates the `loqui-notary` profile with `xcrun notarytool store-credentials`, using
their Apple ID, Team ID, and an app-specific password. The release preflight verifies the stored
profile without printing the credential values.

This setup is operator-assisted because Apple account authentication is external and interactive.
The repository automates every repeatable step after the identity and profile exist.

### Development identity isolation

`Info.dev.plist` uses `com.jualopezmo.loquigo.dev`, while production keeps
`com.jualopezmo.loquigo`. The dev app copies its helpers into its own standard bundle layout and signs
them with development-specific identifiers derived from the dev bundle ID. This prevents an Apple
Development rebuild from fighting the Developer ID app for the same TCC records.

Developer ID is never the automatic signer for `dev`, `run`, `build`, `test`, or `package`. If the
Apple Development identity is unavailable, the ordinary development path may retain an explicitly
labelled ad-hoc fallback for contributors, but it must print that TCC continuity is unavailable. The
owner's configured machine is expected to use stable Apple Development signing. An explicitly
configured but invalid `LOQUI_DEV_SIGN_IDENTITY` fails instead of falling back.

## Bundle layout

The packaged release uses standard macOS locations:

```text
loqui.app/
  Contents/
    MacOS/
      loqui
    Helpers/
      globe-listener
      macos-stt
      whisper-stt
    Frameworks/
      MicrosoftCognitiveServicesSpeech.framework/
      libSDL2-2.0.0.dylib
      libwhisper*.dylib
      libggml*.dylib
    Resources/
      icons.icns
      models/ggml-small.bin      # only with LOQUI_BUNDLE_MODEL=1
    Info.plist
```

- `Contents/Helpers` contains only executable helper tools.
- `Contents/Frameworks` contains frameworks and dynamic libraries. Versioned dylib symlinks are
  preserved rather than flattened into duplicate files.
- `Contents/Resources` contains only non-executable data.
- Framework copies use a symlink-preserving tool such as `ditto`.
- `icons.icns` is required. `Assets.car` is intentionally absent: the checked-in Wails asset task
  generates only the `.icns`, because this project's macOS 26 icon workaround records that an asset
  catalog degrades the icon. Assembly and audit reject a reintroduced `Assets.car`.

`internal/app.HelperPath` resolves bundled executable helpers from `Contents/Helpers` and keeps the
existing `helpers/bin` development fallback. `WhisperModelPath` resolves the optional bundled model
from `Contents/Resources/models`, independently of executable lookup. When
`LOQUI_BUNDLE_MODEL=1`, assembly validates the source by the production model's exact byte count and
SHA-256 before copying, then validates the staged destination again. The package audit repeats the
same fixed validation whenever the optional model is present; absence remains valid for the normal
release. A static contract test binds the shell constants to the canonical pin in
`internal/store/model.go` so neither copy can drift silently.

## Whisper portability

The Whisper build step must make every non-system dependency relocatable:

1. Copy the real Whisper/ggml dylibs and their versioned symlinks for packaging.
2. Copy the SDL dylib used at link time instead of assuming Homebrew exists on the recipient Mac.
3. Rewrite the helper's SDL load command away from `/opt/homebrew/...`.
4. Give `whisper-stt` an rpath that resolves its dylibs from the app's `Contents/Frameworks` when the
   helper runs from `Contents/Helpers`.
5. Rewrite SDL's absolute `LC_ID_DYLIB`, and delete build-tree `LC_RPATH` values from every real
   Whisper/ggml dylib. Each packaged dylib keeps only a portable `@loader_path` rpath and an
   `@rpath/...` ID.
6. Require the embedded `__DATA,__ggml_metallib` section in `libggml-metal`; do not copy an external
   Metal resource.
7. Reject the package if `otool -L`, `otool -D`, or `otool -l` exposes a checkout path, Homebrew
   path, or other unexpected absolute non-system dependency.

Vendored Whisper/ggml links reserve header space for install-name growth. After mutating development
Mach-O files, the build re-signs each one ad hoc and verifies it so direct `helpers/bin` execution is
not left with an invalid signature.

Development from `helpers/bin` must continue to work. The implementation may retain a local rpath in
the development artifact, but the packaged artifact must contain only release-safe references.

Release never consumes that ambient development directory. It rebuilds `globe-listener`,
`macos-stt`, `whisper-stt`, and the native dylibs into unique staging from the current repo commit,
pinned whisper.cpp commit `97c56f1dc1d1100a9d859c865a20c82d22f823ed`, and pinned official SDL
commit `5d249570393f7a37e037abf22cd6012a4cc56a71`. The SDL checkout is fetched by exact SHA, must retain
the official origin, and rejects tracked, index, and non-ignored untracked changes before
compilation. Its build/install outputs are deterministic siblings outside the source checkout.
Evidence records both upstream revisions, an honest clean/dirty marker, and every packaged Mach-O
SHA-256 as provenance. Dirty is expected for the workflow's pre-commit E2E artifact and is never
reported as a clean commit build.

## Signing identities and order

Stable identifiers are fixed as follows:

| Code item | Production identifier | Development identifier |
| --- | --- | --- |
| Main app | `com.jualopezmo.loquigo` | `com.jualopezmo.loquigo.dev` |
| fn listener | `com.jualopezmo.loquigo.globe-listener` | `com.jualopezmo.loquigo.dev.globe-listener` |
| Apple STT helper | `com.jualopezmo.loquigo.macos-stt` | `com.jualopezmo.loquigo.dev.macos-stt` |
| Whisper STT helper | `com.jualopezmo.loquigo.whisper-stt` | `com.jualopezmo.loquigo.dev.whisper-stt` |

The release script signs from the inside out:

1. Whisper/ggml/SDL dylibs and the Azure framework.
2. Each non-bundled helper executable with its explicit identifier.
3. The top-level app bundle.
4. The completed DMG after it is created.

Developer ID code uses a secure timestamp. Apple Development signing explicitly disables network
timestamping so daily builds work offline. Developer ID and Apple Development main executables use
Hardened Runtime. Apple's documented resource restrictions require Audio Input on the host,
`macos-stt`, and `whisper-stt`; the host also requires Apple Events to paste into other apps.
`globe-listener`, library code, and the framework receive no entitlement file. Ad-hoc packages use
no Hardened Runtime, timestamp, or entitlements. No undocumented Speech entitlement is claimed.

The script does not use `--deep` to sign. It may use `codesign --verify --deep --strict` after the
explicit signing pass because deep verification is an audit, not an instruction to mutate nested code.

## Release flow

1. **Preflight** — validate, without mutating the checkout, that generated plist IDs, privacy strings,
   versions, and the macOS 14 minimum match `build/config.yml`/the repo contract; then validate host arm64, Apple tools including `vtool`, exactly selected
   Developer ID identity, notarization profile, source helpers/framework, and a unique temporary
   staging target.
2. **Build/package** — use the existing Wails/Taskfile compilation, whose `darwin:build` dependency
   chain regenerates the production frontend and `icons.icns`; rebuild every helper/native dylib
   into staging from pinned/current sources; assemble the standard bundle in a
   staging directory, and keep the public output path untouched. An ad-hoc-signed artifact produced
   by ordinary Wails packaging is staging input only: the release signer replaces its signatures,
   and the intermediate app is neither launched nor published.
3. **Audit identity/layout/dependencies** — require the channel's exact bundle ID, non-empty
   microphone/speech/Apple Events usage strings, matching packaged plist versions, plist minimum
   14.0.0, and Mach-O `minos <= 14.0`; require `macos-stt` to report exactly 26.0;
   clear extended attributes, then reject signing-blocking Finder/resource-fork/quarantine metadata,
   Mach-O code under Resources, missing nested code, non-arm64 release code, broken symlinks,
   external Metal data, and any load path/install name outside the explicit system/token allowlist.
4. **Sign inside out** — sign every explicit nested item, the helpers, and the app; capture the
   authority, Team ID, identifiers, hardened-runtime flags, and timestamps as evidence. Separately
   capture one normalized `codesign -d -r-` `designated =>` line, in a fixed order, for `Loqui.app`
   and each of its three helpers. A missing or duplicate designated-requirement line fails release.
5. **Verify app** — run strict signature validation. Do not require Gatekeeper acceptance before
   notarization; the pre-notary app is expected to lack an Apple ticket.
6. **Create DMG** — stage `Loqui.app` with `ditto`, add an `/Applications` symlink, re-audit and
   reverify the copied app, capture the copied app/helper designated requirements and require them
   to equal the original four-item capture, then create a compressed UDIF image, verify it with
   `hdiutil`, and sign the image. Preserve both DR captures as release evidence.
7. **Notarize outermost container** — submit the DMG with `notarytool --wait` and the Keychain
   profile. Save the submission ID and fetch the JSON log even when Apple reports `Accepted`.
8. **Staple and verify** — staple the outermost DMG only, validate the ticket, re-run
   signature/Gatekeeper checks, and inspect suffix-normalized `ticketContents` for every signed
   nested code item. Accepted logs fail on error-severity issues but preserve warning-only issues as
   evidence.
9. **Publish atomically** — move the accepted image to
   `bin/release/Loqui-${VERSION}-macos-arm64.dmg` only after every check passes.

## Failure behavior

- Work happens in a unique temporary staging directory. Both `TMPDIR` and that directory are resolved
  immediately with `pwd -P`; cleanup accepts only the physical temporary root plus the exact
  `loqui-release.??????` child. Cleanup never targets `bin`, the repository root, `$HOME`, a lexical
  symlink alias, or an unresolved variable.
- Publication requires the physical repository's literal `bin/release` directory; `bin`, `release`,
  `evidence`, and `evidence/<version>` must not be symlinks or resolve outside that repository.
  When `bin` or `bin/release` is absent in a fresh clone, create each with a simple one-level `mkdir`
  only after validating its physical parent, then require its `pwd -P` value to equal the literal
  expected path.
  Cleanup removes a hidden publication candidate only when this execution set its ownership flag
  after exclusive creation. Pre-existing colliding names and unrelated sentinels survive the trap.
- Release correctness is independent of Bash `errexit`: all internal phases and nested helpers
  propagate relevant command/assignment failures explicitly, and orchestration checks each phase
  result before starting the next even when callers use `run_release || rc=$?`.
- The last successful release remains untouched until a new candidate passes every check.
- Missing or ambiguous identity, invalid notarization profile, failed signature, unexpected
  dependency, rejected notarization, failed staple, or failed Gatekeeper assessment exits nonzero.
- A notary rejection prints the submission ID and the path to the saved log, not a generic failure.
- Notary-log retrieval uses explicit `if` branches to capture each command result, retries at most
  three times, and returns nonzero explicitly after preserving the raw submission response and any
  available log before staging cleanup. It does not depend on changing `set -e` state.
- Logs may contain certificate names, Team IDs, code identifiers, and Apple diagnostic text. They
  must not contain passwords, private keys, or environment dumps.
- Published text evidence replaces both lexical and physical staging prefixes with `$STAGE`, replacing
  the physical path first so `/tmp -> /private/tmp` cannot produce `/private$STAGE`. It then rejects
  either original prefix, the malformed marker, an unnormalized checkout path, or a secret field.
- The release script never falls back to ad-hoc signing.

## Migration behavior

The first Apple Development build and the first Developer ID build are each new identities relative to
the current ad-hoc build. Because dev and production have separate bundle/signing identifiers, the
user should expect to grant Accessibility, Input Monitoring, microphone, and speech permissions once
for each channel. The success criterion is that later builds in either channel do not invalidate that
channel's grants.

Moving helpers from Resources to Helpers also changes their path. The first signed release therefore
establishes the new durable path and identity together; the project does not promise to preserve an
ad-hoc helper's existing TCC record.

## Tests and verification

### Automated TDD

- Go tests cover executable-helper lookup in `Contents/Helpers`, development fallback, model lookup
  in `Contents/Resources/models`, and missing-file behavior.
- Tests assert distinct dev/production bundle IDs and helper identifiers so a generated asset update
  cannot silently collapse the two TCC identities again.
- Release-script tests run real release functions against narrow fake Apple commands in a temporary
  directory. They prove material preflight rejection; Developer ID Team/authority/identifier,
  runtime/timestamp, and exact entitlement enforcement; deterministic app/helper designated
  requirements across metadata-changing rebuilds; submit response capture; a three-attempt notary-log
  bound; real-file-only ticket manifests; staple/image/signature/Gatekeeper failure propagation;
  physical-path cleanup; symlink-contained publication; evidence normalization; and atomic
  publication rollback. The publication fixtures live under a temporary physical repository, while
  a typed `EXIT`-trap snapshot records absent trees, directories, file size/hash, and symlink targets
  to prove the real release tree is identical even after early test failures. A real preflight
  failure called through a literal OR-list proves orchestration stops at the first phase; only the
  dedicated phase-order test replaces complete phases.
- Tests prove release assembly accepts only the freshly built staging helper directory and that
  signature verification rejects any nested Mach-O with a different Developer ID Team ID.
- A package audit test examines a real assembled app and fails for Mach-O code in Resources,
  non-arm64 code, missing `icons.icns`, unexpected `Assets.car`, missing required helpers, broken
  symlinks, signing-blocking xattrs, forbidden load/rpath prefixes, a native minimum above 14.0,
  or a `macos-stt` minimum other than exactly 26.0 (including absent metadata).

### Real local release

- Create/install the Developer ID certificate and `loqui-notary` profile.
- Build two separate development candidates and two separate release candidates.
- Confirm the development candidates use Apple Development and the `.dev` identifiers; confirm the
  release candidates use Developer ID and the production identifiers.
- Within each channel, confirm its two candidates retain the same Team ID and code identifiers and
  have compatible designated requirements. Development and production must belong to the same Apple
  team, but their signing identities, identifiers, and designated requirements are deliberately
  distinct.
- Submit one DMG and require `Accepted` plus a reviewed notary log.
- Require successful `codesign`, `spctl`, `stapler validate`, and `hdiutil verify` checks.
- Mount the DMG, copy the app to `/Applications`, launch it, and exercise the main app,
  `globe-listener`, a clean first-use Whisper model download plus interrupted-download retry, Apple
  STT where supported, and Azure without checkout/Homebrew paths. The normal release leaves
  `LOQUI_BUNDLE_MODEL` unset.

### Second-Mac evidence

The compatibility declaration is not runtime evidence. A macOS 14 Apple Silicon machine must run
the main/globe/Whisper/provider journey before Sonoma support is claimed; current-machine tests can
only prove the load-command contract. The Apple Speech helper remains macOS 26+.

Install the DMG on another Apple Silicon Mac that does not rely on this checkout. Verify launch,
permissions UI (including which process each prompt names), fn listener, Whisper, and at least one
cloud provider. For offline-ticket evidence, copy the assessed app out of the DMG, eject, disable
networking, reboot, then assess and launch the copied app. Then install a second signed build and
confirm the established permissions are not revoked.

If a second Mac is unavailable, implementation and local notarization may proceed, but the E2E ship
gate remains open and the work is not described as fully distribution-verified.

## Acceptance criteria

- `./scripts/task.sh release:macos` produces only
  `bin/release/Loqui-${VERSION}-macos-arm64.dmg` as the final distributable artifact, where `VERSION`
  is exactly `info.version` from `build/config.yml`.
- The DMG, app, framework, dylibs, and helpers are signed by the selected Developer ID identity in
  explicit inside-out order; signing uses no `--deep`.
- Hardened Runtime, secure timestamps, and the narrow host/audio-helper entitlement sets match the
  executable responsibilities; libraries/frameworks have no entitlement file.
- The app and all helper designated requirements remain compatible across two rebuilds.
- Day-to-day signed development uses Apple Development with the `.dev` bundle/helper identifiers and
  does not access the Developer ID identity.
- The DMG is accepted by Apple's notary service, its log is reviewed, and its ticket is stapled and
  validated.
- No executable code is stored under `Contents/Resources`.
- The packaged binaries are arm64 and have no dependency on Homebrew, the source checkout, or an
  unbundled third-party dylib/framework.
- Both app plist version keys equal `info.version`, the standard DMG omits the optional model, and
  signing-blocking extended attributes are absent before signing.
- Release failure leaves the previous accepted artifact intact and returns nonzero with a specific
  diagnostic.
- No signing or notarization secret is present in Git, command output, or release evidence.
- The normal development workflow remains usable.
- The second-Mac E2E report either passes or remains explicitly open; it is never silently waived.

## Documentation updates at implementation time

- README: one-time certificate/profile setup, release command, Apple Silicon scope, and one-time TCC
  migration expectation.
- `docs/CHANGELOG.md`: new Developer ID/notarized DMG capability and the resolved rebuild-permission
  churn.
- `CONTINUITY.md`: exact remaining external verification or next project priority.
- E2E use case/report: notarized DMG installation and permission continuity.
