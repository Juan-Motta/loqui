# Windows release readiness research

Checked: 2026-08-13

## Questions

1. Can the current Loqui codebase compile and run on Windows as it stands?
2. Which product capabilities need Windows-specific implementations?
3. What native dependencies, installer contents, signing identity, and CI infrastructure are
   required for a trustworthy release?
4. Should the first Windows release use NSIS or MSIX, and which OS/architecture should it target?
5. Can Loqui's current automatic updater safely update a Windows installation?

## Executive conclusion

Loqui is **not ready to generate a usable Windows release by enabling the existing packaging
task**. The repository contains Wails-generated Windows scaffolding, but the application does not
currently compile for Windows, the installer only includes `loqui.exe`, and the Windows metadata
is still at `0.1.0` or contains template placeholders.

The lowest-risk first target is:

- Windows 11 x64;
- a per-user NSIS installer distributed through GitHub Releases;
- Authenticode signing of every shipped `.exe` and `.dll`, followed by signing the installer;
- the WebView2 Evergreen bootstrapper;
- a native `windows-2025` GitHub Actions build, plus acceptance on a real interactive Windows 11
  machine or VM;
- either a Windows C++ helper for Azure Speech or an explicit decision to mark only Azure Speech
  unavailable in the first Windows release;
- a Windows-specific updater that runs the signed installer, or no Windows automatic update until
  that path exists.

NSIS is recommended before MSIX because it matches the existing direct-download/GitHub release
model, already has Wails scaffolding in the repository, and is simpler for the app's native helper
files. MSIX/Microsoft Store remains a good second distribution channel after the Windows runtime is
stable.

## Verified repository findings

### Packaging scaffolding exists, but is not a release pipeline

- The root `Taskfile.yml` includes `build/windows/Taskfile.yml` and dispatches `build` and
  `package` by `GOOS`. There is no `release:windows` task or Windows release-test suite; the only
  release task and gate are macOS-specific (`Taskfile.yml:13-53`, `Taskfile.yml:91-97`).
- The Windows task can create an executable, an NSIS installer, or an MSIX package. Its default is
  `CGO_ENABLED=0`, NSIS, machine-wide installation, and output named
  `bin/loqui-<arch>-installer.exe` (`build/windows/Taskfile.yml:17-64`,
  `build/windows/Taskfile.yml:97-156`).
- Wails' current Windows packaging documentation confirms that `wails3 package GOOS=windows`
  builds the app, generates a WebView2 bootstrapper, and creates an NSIS installer; MSIX requires
  Windows SDK or standalone MSIX tooling. Source:
  [Wails v3 Windows Packaging](https://v3.wails.io/guides/build/windows/), checked 2026-08-13.
- The checked-in NSIS macro currently copies only the main executable. It does not install the
  Whisper helper, SDL/whisper runtime libraries, or any future Azure Speech helper/runtime
  (`build/windows/nsis/wails_tools.nsh:108-120`). A functioning installer must own the complete
  runtime tree and uninstall it consistently.
- The template's NSIS `!uninstfinalize` and `!finalize` signing hooks are commented out, while the
  Wails `sign:installer` task signs only the completed outer installer
  (`build/windows/nsis/project.nsi:70-72`, `build/windows/Taskfile.yml:181-195`). The generated
  `uninstall.exe` would therefore remain unsigned unless packaging is changed to sign it during
  generation.
- `build/windows/info.json`, `build/windows/wails.exe.manifest`,
  `build/windows/msix/template.xml`, and `build/windows/nsis/wails_tools.nsh` still identify
  version `0.1.0`, while `build/config.yml` is `0.3.0`. The MSIX app manifest also contains
  `com.example.loqui`, `CN=My Company`, `My Product`, and a hard-coded ARM64 architecture
  (`build/windows/msix/app_manifest.xml:10-38`). These files cannot be published as-is.
- The existing GitHub release workflow is macOS-only. It has no Windows job, signing stage,
  Windows artifact contract, installer audit, or Windows assets in the release publication step
  (`.github/workflows/release.yml`).

### The current code does not compile for Windows

The following compile-only probe was run from the current `main` baseline:

```text
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  GOCACHE=/private/tmp/loqui-windows-gocache \
  go test -exec=true ./...
```

It failed before packaging. Verified blockers were:

- `internal/macos` and `internal/permissions` contain only Darwin files, but are imported by
  platform-neutral application code;
- `internal/inject` has no Windows implementations of clipboard snapshot/write/change-count or
  paste-key injection;
- `internal/audio` uses `malgo`, whose native types are absent when CGO is disabled;
- `internal/stt/azure` uses the Microsoft Speech Go binding, whose native types are absent when
  CGO is disabled.

The platform inventory confirms that the only current OS-specific implementations are:

```text
internal/inject/focus_darwin.go
internal/inject/pasteboard_darwin.go
internal/macos/*_darwin.go
internal/permissions/permissions_darwin.go
```

There are no corresponding `_windows.go` files.

### User-facing Windows behavior is incomplete

- Loqui's normal workflow needs to preserve the focused application, copy the transcript to the
  clipboard, synthesize paste, restore the user's clipboard, and avoid injecting into protected
  fields. All of those operations are currently implemented only for macOS in
  `internal/inject`.
- Ordinary global accelerators are stored and validated, but are deliberately not registered.
  `wiring.go:507-517` logs that non-`fn` triggers are unsupported; `wiring.go:529-545` also rejects
  applying them live. Windows therefore has no usable keyboard trigger today.
- The pinned Wails line now exposes native global shortcuts and uses `RegisterHotKey` on Windows.
  That is a good fit for Loqui's existing **toggle** mode, but it is press-only. Loqui itself
  states that hold-to-talk needs both key-down and key-up and currently supports it only for the
  macOS `fn` helper (`internal/store/trigger.go:100-135`). A Windows hold mode would require a
  separate low-level keyboard listener; the first release can safely expose toggle mode only.
  Source: [Wails v3 global-shortcut changelog](https://v3.wails.io/changelog/), checked
  2026-08-13.
- `main.go` calls macOS appearance, locale, overlay, and activation adapters. About reports the OS
  as `macOS`, recognizes only `.app/Contents/MacOS` as packaged, and derives helper/model paths
  from `Contents/Helpers` and `Contents/Resources` (`internal/app/about_service.go`,
  `internal/app/about.go:66-73`, `internal/app/paths.go:10-74`). Windows adapters and a Windows
  installed-layout contract are required.
- Permission rows contain some runtime platform filtering, but the permission backend itself is
  Darwin-only. The Windows version should describe the desktop-app microphone privacy control and
  link to Windows Settings instead of simulating macOS Accessibility, Speech Recognition, and
  Input Monitoring grants.

### Engine compatibility is uneven

| Engine | Windows status from current code | Required work |
| --- | --- | --- |
| Whisper local | Declared Windows-capable in `internal/store/connection.go`, but no Windows helper is built or packaged | Build the pinned `whisper.cpp`/SDL helper on Windows, decide static vs DLL runtime, package and smoke-test it |
| Azure OpenAI Realtime | Transport is Go WebSocket/HTTP and its package compiles in the Windows probe | Platform shell work plus live Windows audio/E2E validation |
| OpenAI, Grok, ElevenLabs | Their Go packages compile in the Windows probe | Platform shell work plus live Windows audio/E2E validation |
| Azure Speech | Current Go/C SDK integration is not an officially supported Windows Go setup | Windows-native replacement or disable this one engine on Windows |
| Apple Speech | Correctly macOS-only | Hide as unsupported on Windows |

The local Whisper source is portable C++, and upstream supports Windows with MSVC and MinGW plus
the `WHISPER_SDL2` build option. Loqui's `scripts/build-whisper-stt.sh`, however, is explicitly a
macOS arm64 build: it emits dylibs, uses `otool`, `install_name_tool`, Metal, and `codesign`.
Source: [official whisper.cpp repository](https://github.com/ggml-org/whisper.cpp), checked
2026-08-13.

Azure Speech is the exceptional dependency. Microsoft's current installation guide lists the
Speech SDK for Go only on Ubuntu/Debian x64. The C++ SDK is supported on Windows 11 or later with a
64-bit target and the Visual C++ runtime. Source:
[Microsoft Speech SDK platform setup](https://learn.microsoft.com/en-us/azure/ai-services/speech-service/quickstarts/setup-platform),
checked 2026-08-13. The existing macOS integration is already a project-specific extension around
the SDK's C API; it is not evidence of a supported Windows Go toolchain.

The practical choices for Azure Speech are:

1. **Recommended for feature parity:** build a small Windows x64 C++ helper against the pinned
   Microsoft Speech SDK and communicate through a narrow, testable IPC protocol. Keep Azure OpenAI
   Realtime in Go; it does not need this helper.
2. **Recommended for the fastest Windows beta:** mark Azure Speech unavailable on Windows while
   keeping Azure OpenAI Realtime and the other engines. The UI already has an unsupported-engine
   state model.
3. Do not create an unsupported MinGW bridge to Microsoft's Windows C++ binaries or replace
   streaming recognition with the short-audio REST endpoint merely to make the build green. Both
   would introduce a new, fragile runtime contract.

## Installer and runtime recommendation

### First target: Windows 11 x64, per-user NSIS

Target Windows 11 x64 first. Wails itself supports Windows 10/11 and x64/ARM64, but full Azure
Speech parity raises the product minimum to Windows 11 x64. A single initial architecture also
keeps native Whisper, SDL, code signing, and installer verification bounded. Source:
[Wails v3 FAQ](https://v3.wails.io/faq/) and the Microsoft Speech SDK setup above, checked
2026-08-13.

Use NSIS with `INSTALL_SCOPE=user`:

- installation under `%LOCALAPPDATA%\Programs\Loqui` needs no UAC prompt;
- it matches the existing GitHub Releases/direct-download model;
- the repository already contains the Wails NSIS template and WebView2 bootstrapper flow;
- it leaves the installed files writable by the user, which is necessary if the application will
  later update itself without elevation.

The current default is machine scope under `Program Files` (`build/windows/Taskfile.yml:97-105`,
`build/windows/nsis/project.nsi:74-80`). That default should not be used with Loqui's current
self-update model.

The NSIS payload must include, at minimum:

- signed `loqui.exe`;
- signed `whisper-stt.exe` and every required Whisper/SDL DLL, unless the helper is made fully
  static;
- any signed Azure Speech helper and its redistributable Speech SDK DLLs, if Azure Speech ships;
- third-party notices/licenses required by the native SDKs;
- uninstaller, Start Menu shortcut, and optional desktop shortcut;
- WebView2 Evergreen bootstrapper;
- no 465 MB Whisper model by default—the existing first-use verified download can remain.

Microsoft recommends Evergreen WebView2 for most applications; it can be installed through the
small online bootstrapper, while the standalone installer is the offline alternative. Source:
[WebView2 Evergreen vs. Fixed Version](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/evergreen-vs-fixed-version),
checked 2026-08-13. Wails' NSIS task already generates and invokes the bootstrapper.

### Why not MSIX first

MSIX is viable later and is attractive through Microsoft Store because the Store signs MSIX
packages and avoids SmartScreen warnings. However, Loqui's checked-in MSIX identity, publisher,
architecture, visual assets, and capabilities are placeholders; direct sideloading still requires
a trusted matching signing identity. Source:
[Microsoft code-signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options),
checked 2026-08-13.

MSIX also introduces Store/Partner Center identity, package capability, and update-channel
decisions before the Windows application runtime has been proven. It should be a separate follow-up
after the NSIS build passes real Windows use cases.

## Signing and SmartScreen

Public Windows distribution should not ship unsigned. Microsoft reports that unsigned and
self-signed downloads show strong SmartScreen warnings, while a valid OV/EV or Artifact Signing
identity still needs to build reputation over time. The Microsoft Store is the only listed route
that avoids the warning immediately for an MSIX submission. Sources:
[Microsoft SmartScreen reputation](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)
and [code-signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options),
checked 2026-08-13.

Two production signing paths are realistic:

| Path | Fit | Constraint |
| --- | --- | --- |
| Azure Artifact Signing Public Trust | Managed keys, Windows GitHub Action, OIDC-friendly CI | Public Trust currently accepts organizations in USA/Canada/EU/UK and individual developers only in USA/Canada |
| Public OV certificate from a trusted CA | Available where a compatible CA can validate the publisher | Recurring cost and a hardware/cloud-backed key workflow must be integrated with CI |

Artifact Signing's geographic limits are explicit in Microsoft's current FAQ. Source:
[Artifact Signing FAQ](https://learn.microsoft.com/en-us/azure/artifact-signing/faq), checked
2026-08-13. Therefore eligibility must be confirmed before selecting it; an Azure subscription
alone is not enough. If eligible, use the official Windows-only Artifact Signing action with GitHub
OIDC and an Artifact Signing Certificate Profile Signer role. Otherwise obtain an OV certificate
whose private key can be used through the CA's supported cloud/HSM integration.

The signing order should be:

1. build all `.exe`/`.dll` payloads;
2. Authenticode-sign and RFC 3161 timestamp every executable payload;
3. verify each signature with `signtool verify /pa /all /v`;
4. create the NSIS installer;
5. sign and timestamp the installer;
6. verify the installer signature again;
7. compute and publish `SHA256SUMS` only after final signing.

SignTool is included in the Windows SDK and supports signing, timestamping, and verification; `/pa`
selects the Authenticode policy. Source:
[Microsoft SignTool reference](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool),
checked 2026-08-13.

Use a protected GitHub `windows-release` environment. Environment secrets are unavailable until
required reviewers approve when that protection is configured; branch/tag restrictions can also
limit what may deploy. Source:
[GitHub deployment environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments),
checked 2026-08-13.

## Automatic-update implications

Loqui's asset matcher currently rejects every platform except `darwin/arm64` and accepts only a
macOS ZIP (`internal/app/update_backend.go:12-25`). Merely publishing a Windows installer will not
enable Windows updates.

The updater is not even configured by a packaged Windows executable today. Its version source
recognizes only a `.app/Contents/MacOS` path and reads `Info.plist`; a Windows install is classified
as an unpackaged development run (`internal/app/about.go:66-73`,
`internal/app/about_service.go:39-66`). Windows needs a real packaged/version source before update
checks can start.

More importantly, the pinned Wails updater swaps one top-level target. Its documented archive
contract requires exactly one top-level entry, and on Windows its helper replaces the running
executable itself. It does not run an NSIS/MSI installer or atomically update a tree of helper
executables and DLLs. Source:
[Wails updater artifact formats and swap](https://v3.wails.io/guides/updater/), checked
2026-08-13.

Because a functional Windows Loqui install is multi-file, the safe choices are:

1. **Recommended:** implement a Windows update backend that downloads the signed NSIS installer,
   verifies checksum and Authenticode publisher identity, launches it in upgrade/silent mode only
   after user confirmation, and exits. Test upgrade, rollback/failure, settings preservation, and
   uninstall metadata.
2. Disable Windows automatic install for the first beta and make “Check for updates” open the
   GitHub release page. Do not feed an installer `.exe` to the current Wails binary-swap updater.

The Windows update should use the per-user installer so it does not unexpectedly request
administrator credentials. Separately, Loqui should harden its update metadata beyond the current
digest-only `SHA256SUMS`: Wails documents that a digest fetched beside an artifact detects
corruption but is not an independent publisher signature. Authenticode protects the Windows
payload; a pinned Wails update-signing public key would also authenticate release metadata.

## Proposed GitHub Actions release flow

Use a separate Windows job or reusable workflow first; do not weaken the already proven macOS
release path.

1. **Metadata/preflight (no signing secrets)**
   - validate a `vX.Y.Z` tag against every Windows/macOS version source;
   - assert a clean source checkout and canonical artifact names;
   - fail if Windows template placeholders or stale versions remain.
2. **Build on a pinned Windows image**
   - `runs-on: windows-2025` rather than floating implicitly;
   - `actions/setup-go` with Go 1.25.x and `actions/setup-node` with Node 24;
   - install the exact Wails CLI version matching `v3.0.0-alpha2.119`;
   - install/pin NSIS explicitly;
   - use CGO plus a known MinGW/MSVC strategy for `malgo`;
   - build the pinned Whisper/SDL helper and optional Azure Speech C++ helper;
   - build frontend and `loqui.exe`.
3. **Test before credentials**
   - Go/frontend tests and a Windows compile contract;
   - helper protocol tests;
   - native DLL dependency audit;
   - unsigned installer install/uninstall smoke test in an isolated temp/user context.
4. **Protected signing job**
   - enter `windows-release` only after tests pass;
   - sign payload binaries, verify them, create NSIS, sign/verify NSIS;
   - reject unexpected unsigned PE files and unexpected installer contents.
5. **Release artifact tests**
   - fresh Windows sandbox/VM install, launch, upgrade, uninstall;
   - `Get-AuthenticodeSignature` and `signtool verify /pa`;
   - SHA-256 manifest exactness;
   - artifact names and tag/version match.
6. **Publish**
   - upload only reviewed canonical assets to the same GitHub Release;
   - suggested names:
     `Loqui-X.Y.Z-windows-x64-setup.exe` and `SHA256SUMS`;
   - keep the macOS DMG and update ZIP intact.

GitHub's current `windows-2025` image provides CMake, GCC, Visual C++ runtimes, a Windows SDK,
PowerShell, Chocolatey, and other build tooling, but its preinstalled Go/Node versions are mutable;
the workflow should continue pinning them through setup actions. Source:
[GitHub Actions Windows 2025 image](https://github.com/actions/runner-images/blob/main/images/windows/Windows2025-Readme.md),
checked 2026-08-13.

## Required Windows implementation work

### Platform boundary

- split unconditional `internal/macos`/`internal/permissions` imports behind platform-neutral
  interfaces or add safe Windows implementations;
- implement Windows locale, OS version, About data, packaged detection, and helper/model paths;
- ensure macOS-only overlay/activation calls are compiled only on Darwin;
- adapt tray terminology and behavior where the macOS menu bar model leaks into copy or logs.

### Dictation workflow

- register ordinary Windows global shortcuts through Wails for toggle mode;
- expose no default shortcut until the user chooses one, as the current settings model intends;
- implement clipboard snapshot/write/restore and paste injection on Windows;
- define a protected-field/focus policy and test integrity-level/UIPI failure behavior;
- implement Windows microphone state/guidance and remove irrelevant macOS grants;
- validate `malgo` capture on default and selected Windows input devices with CGO enabled.

### Native engines

- write and pin a Windows Whisper/SDL build script; choose static linking where licensing and
  toolchain permit, otherwise enumerate and package exact DLLs;
- preserve the current pinned source commits and model hash/size checks;
- decide Azure Speech parity: C++ helper or explicitly unsupported on Windows;
- if a helper is built, pin the Microsoft Speech SDK version, package its redistributable runtime
  and notices, and verify the helper protocol under cancellation/network failure.

### Packaging/release

- create one canonical version source that updates EXE metadata, manifest, NSIS, and optional MSIX;
- replace all Wails template identity/product strings;
- expand NSIS to install every runtime file and localize at least English/Spanish;
- choose per-user scope and an upgrade-compatible stable installer identity;
- add Windows release scripts/tests rather than embedding untested PowerShell directly in YAML;
- sign all PE files and the final installer;
- extend GitHub release publication without changing current macOS artifact guarantees;
- add Windows README download, requirements, install, privacy, and troubleshooting sections.

## Verification matrix before public release

CI compilation is necessary but not sufficient. Native dictation requires an interactive Windows
desktop, real audio input, foreground switching, and installed-app paths. Execute these journeys on
a clean Windows 11 x64 VM or physical machine:

1. clean per-user install with and without preinstalled WebView2;
2. first launch, single-instance behavior, tray, window movement/resizing, light/dark appearance;
3. microphone denied, allowed, device removed, and device switched;
4. toggle shortcut from Notepad and another normal text field;
5. injection preserves clipboard contents and does not paste into a password/protected field;
6. Whisper model download, hash validation, first dictation, cancellation, and offline relaunch;
7. one live dictation and connection probe for each shipped cloud engine;
8. signed installer and all installed PE signatures validate after installation;
9. upgrade from release N to N+1 preserves settings, API keys, model, and history;
10. uninstall removes binaries/shortcuts/registry entries but follows the chosen policy for user
    data;
11. install/launch as a non-admin account and with a path containing spaces/non-ASCII characters;
12. SmartScreen behavior recorded on a clean-reputation machine rather than inferred from a local
    developer machine.

GitHub-hosted runners are suitable for deterministic builds and non-interactive tests. The final
microphone, global shortcut, clipboard injection, foreground, installer UI, and SmartScreen claims
need an interactive machine; they should not be attested from a headless CI job.

## Recommended delivery sequence

### Phase 1 — Windows compile and shell

Add platform boundaries, Windows About/paths/permissions, toggle shortcut, clipboard injection,
and CGO audio. Gate with a Windows-native CI compile/test job. Do not package yet.

### Phase 2 — engines

Build/package Whisper on Windows and validate every platform-neutral cloud engine. Make the Azure
Speech decision explicitly; if parity is required, add the C++ helper here.

### Phase 3 — installer and signing

Adapt NSIS to the complete runtime tree, synchronize metadata/versioning, configure the signing
identity, verify installed signatures, and exercise install/uninstall on Windows 11 x64.

### Phase 4 — release and updater

Add a protected Windows release job and canonical GitHub assets. Either ship a tested NSIS-based
Windows updater or deliberately expose check/open-download only. Do not reuse binary swap for the
multi-file installation.

### Phase 5 — optional expansion

After x64 stability: Microsoft Store/MSIX, ARM64, offline WebView2 bundle, Windows hold-to-talk,
and broader OS support if the Azure Speech dependency permits it.

## Open decisions and how to close them

1. **Must Azure Speech ship in Windows v1?** Product decision. If yes, run a one-day C++ Speech SDK
   helper spike on Windows 11 x64 before designing the full port. Acceptance: partial/final events,
   cancellation, PCM streaming, and redistributable DLL inventory all work.
2. **Is the publisher eligible for Azure Artifact Signing Public Trust?** Verify the legal identity
   type and country in Azure's identity-validation flow without purchasing or exposing credentials.
   If ineligible, obtain quotes/workflow requirements from two trusted OV certificate providers.
3. **Is Windows auto-update required for the first public build?** If yes, prototype a per-user NSIS
   N-to-N+1 upgrade before finalizing artifact names. If no, make the UI explicitly open the release
   page and keep install disabled on Windows.
4. **What is the Windows data-retention policy on uninstall?** Decide whether history, keys, logs,
   and the 465 MB model remain by default or are removable through an explicit checkbox. Encode it
   in installer tests and user documentation.
5. **Can Windows 10 be supported without Azure Speech?** Validate all remaining features on the
   oldest intended Windows 10 build only after the Windows 11 x64 path is green. Do not advertise
   Windows 10 solely because Wails supports it.

## Readiness definition

A Windows release is ready only when all of the following are true:

- Windows-native compile/test is green with CGO and every shipped native dependency;
- installer contents are complete and reproducible from pinned inputs;
- every shipped PE and the installer have a valid timestamped production signature;
- install, live dictation, injection, update (if offered), and uninstall pass on a clean
  interactive Windows 11 x64 environment;
- the release workflow publishes exact canonical Windows assets without weakening macOS gates;
- unsupported engines/features are hidden or clearly labeled rather than silently failing;
- README and release notes state OS/architecture, WebView2/network needs, engine support, and any
  known SmartScreen reputation warning.
