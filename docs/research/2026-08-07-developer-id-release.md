# Developer ID release research

Checked: 2026-08-07

## Questions

1. What does Apple require for a Developer ID release built outside Xcode?
2. Can the pinned Wails signing task be the release authority for this bundle?
3. Where must Loqui's native helpers and dynamic libraries live?
4. What must be verified before an Apple Silicon DMG can be called distributable?
5. Which Hardened Runtime entitlements do Loqui's actual executable paths require?

## Verified findings

### Apple identity and privacy continuity

- A `Developer ID Application` certificate is the identity for distributing an app outside the
  Mac App Store, and Apple issues it only to Apple Developer Program or Enterprise Program members.
  Source: [Apple Developer ID certificate glossary](https://developer.apple.com/help/glossary/developer-id-certificate/),
  checked 2026-08-07.
- A code signature's designated requirement (DR) is how macOS recognizes later builds as the same
  code. Apple's current manual-signing guidance explicitly says macOS uses the DR to track access to
  privacy-protected resources such as the microphone. Source:
  [Creating distribution-signed code for macOS](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/),
  checked 2026-08-07.
- Apple DTS advises against using the valuable Developer ID private key for day-to-day development;
  an `Apple Development` identity is the appropriate development signer. Source:
  [The Care and Feeding of Developer ID](https://developer.apple.com/forums/thread/732320), checked
  2026-08-07.
- On this Mac, `security find-identity -v -p codesigning` reports zero valid identities. The current
  app reports `Signature=adhoc`, no Team ID, and each ad-hoc helper has a DR tied to its changing
  `cdhash`. This locally verifies why rebuilds do not preserve identity.

### Bundle structure and signing order

- Apple assigns helper tools to `Contents/Helpers/` or `Contents/MacOS/`, dynamic libraries and
  frameworks to `Contents/Frameworks/`, and non-code data to `Contents/Resources/`. Apple warns that
  misplaced code may work in development and fail only during notarization. Source:
  [Placing content in a bundle](https://developer.apple.com/documentation/bundleresources/placing-content-in-a-bundle),
  checked 2026-08-07.
- Apple requires signing nested code from the inside out and explicitly says not to use `codesign
  --deep` when signing a complex product. Non-bundled helper executables should receive an explicit
  signing identifier; Developer ID main executables use a secure timestamp and hardened runtime.
  Source:
  [Creating distribution-signed code for macOS](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/),
  checked 2026-08-07.
- Apple's manual-signing example signs a framework at the `.framework` bundle root, not its
  `Versions/A` directory. The framework executable is still the path that architecture and load-command
  auditing must inspect. The same guidance says entitlements belong on main executables and must not
  be applied to library code. Source: [Creating distribution-signed code for macOS](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/),
  checked 2026-08-07.
- Hardened Runtime blocks audio input unless the executable claims
  `com.apple.security.device.audio-input`. It also requires
  `com.apple.security.automation.apple-events` for a process that prompts to send Apple Events to
  other apps. Loqui's host captures audio and pastes into other apps; `whisper-stt` and `macos-stt`
  each capture audio themselves. Thus the host needs both entitlements, the two audio helpers need
  only Audio Input, and `globe-listener`/libraries/frameworks need neither. Sources:
  [Configuring the hardened runtime](https://developer.apple.com/documentation/xcode/configuring-the-hardened-runtime),
  [Audio Input Entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com_apple_security_device_audio-input),
  and [Apple Events Entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.automation.apple-events),
  checked 2026-08-07.
- `NSSpeechRecognitionUsageDescription` remains in both app plists for the Apple Speech authorization
  request. No additional Speech entitlement is documented for this non-sandboxed macOS Developer ID
  path, and the exact reviewer-suggested
  `com.apple.security.personal-information.speech-recognition` string is absent from the installed
  Xcode 26.6 SDK/specifications. It must not be invented; the real signed Apple engine remains an E2E requirement. Source:
  [NSSpeechRecognitionUsageDescription](https://developer.apple.com/documentation/bundleresources/information-property-list/nsspeechrecognitionusagedescription),
  checked 2026-08-07.
- The checked-in Wails version is `v3.0.0-alpha2.119` (`go.mod`). Its local `wails3 tool sign`
  implementation signs an app with `codesign --force --deep`, then notarizes a ZIP and staples the
  app. That is convenient prior art, but it conflicts with Apple's current inside-out guidance for
  this bundle's helpers, dylibs, and framework.
- Loqui currently copies every helper executable, every Whisper dylib, and optionally the model into
  `Contents/Resources/helpers` (`build/darwin/Taskfile.yml`). `internal/app/paths.go` reads all of them
  from that same directory. The release design must separate executable code, dynamic libraries, and
  model data.
- `build/darwin:build` already depends on the production frontend and icon generators. The icon
  generator intentionally emits `icons.icns` only and documents that `Assets.car` degrades the icon
  on macOS 26; release validation must therefore require the `.icns` and reject `Assets.car`, not
  mistake the intentional absence of an asset catalog for a skipped build.

### Hidden portability blocker

- The main app, `globe-listener`, `macos-stt`, `whisper-stt`, and Whisper dylibs currently contain
  only `arm64`; the owner selected Apple Silicon-only distribution for the first release. The Azure
  Speech framework is already universal, which is harmless in an arm64 bundle.
- `whisper-stt` still links SDL through the absolute development-machine path
  `/opt/homebrew/opt/sdl2/lib/libSDL2-2.0.0.dylib`. The build script relocates the ggml/Whisper
  libraries but does not copy or rewrite SDL. A recipient without that Homebrew installation cannot
  run Whisper even if signing and notarization succeed.
- Local `otool -l/-D` inspection found a second portability defect: every real Whisper/ggml dylib
  except `libggml-base` carries an `LC_RPATH` into
  `scripts/whisper-vendor/whisper.cpp/build/bin`, while Homebrew SDL's own `LC_ID_DYLIB` is its
  absolute `/opt/homebrew/...` path. Sanitizing only `whisper-stt` is insufficient; every copied real
  dylib must have portable rpaths/install names before signing, and the auditor must inspect
  `otool -L`, `otool -D`, and `LC_RPATH` for every packaged Mach-O.
- All three helper outputs live in gitignored `helpers/bin`, and the current Whisper script clones
  the moving upstream default branch. A release that merely checks those files can notarize stale
  local binaries. Release must rebuild into unique staging, pin whisper.cpp to the measured commit,
  and record repo/upstream commits plus packaged Mach-O hashes.
- The current Whisper build has `GGML_METAL_EMBED_LIBRARY=ON`, and local `otool` inspection finds the
  `__DATA,__ggml_metallib` section in `libggml-metal.0.dylib`. The release must assert that embedded
  section instead of copying an external `.metallib` resource.
- Current helper and Azure framework inputs carry `com.apple.provenance` extended attributes while
  current ad-hoc helper signatures still verify, so zero attributes is not a valid signing
  invariant. Assembly should clear attributes recursively, then specifically reject Finder info,
  resource forks, and quarantine metadata that can break signing/distribution; benign provenance
  metadata is not itself a failure.

### Notarization and DMG

- `notarytool` can store an Apple ID, Team ID, and app-specific password under a Keychain profile,
  avoiding plaintext credentials in scripts. It accepts signed disk images, can wait for the result,
  and Apple recommends checking the submission log even when accepted. `stapler` attaches the ticket
  for offline Gatekeeper verification. Source:
  [Customizing the notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow),
  checked 2026-08-07.
- Apple's accepted-log examples allow `issues` to be `null`, and its guidance says to inspect
  successful logs for warnings rather than treating every issue as a rejection. Release validation
  should fail accepted logs only for `severity == "error"`, preserve warnings as evidence, and
  reject missing/malformed logs. `ticketContents[].path` includes an archive/DMG prefix, so expected
  code paths must be matched by anchored suffix, not whole-string equality. Sources:
  [Customizing the notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)
  and [The Pros and Cons of Stapling](https://developer.apple.com/forums/thread/720093), checked
  2026-08-07.
- Apple's general DTS guidance is to sign everything inside out, notarize the outermost container,
  and staple that container. A DMG submission's ticket covers the nested app and its code; the notary
  log must still be checked to confirm every expected item was included. Source:
  [Apple Developer Forums: notarize the DMG and not the app?](https://developer.apple.com/forums/thread/125512),
  checked 2026-08-07.
- Wails v3 documents `SIGN_IDENTITY` and `KEYCHAIN_PROFILE`, but its generated tasks do not create a
  DMG. Its official packaging guide delegates DMG creation to `hdiutil` or another tool. Source:
  [Wails macOS packaging](https://v3.wails.io/guides/build/macos/), checked 2026-08-07.
- This Mac has Xcode 26.6, `notarytool` 1.1.2, `stapler`, `codesign`, `hdiutil`, and an arm64 host.
  The required native release tools are present; the certificate and Keychain profile are not.

## Prior art

### Wails built-in signing

Wails provides identity discovery, Keychain-backed notarization credentials, ZIP submission, and
automatic stapling. Reuse its configuration conventions (`SIGN_IDENTITY`, `KEYCHAIN_PROFILE`) and
native Apple tools, but do not delegate Loqui's nested-code signing to its current `--deep` call.

### Apple's external-build-system workflow

Apple's model for non-Xcode build systems is explicit and auditable: assemble the standard bundle,
identify each code item, sign inside out, verify, package, submit with `notarytool`, inspect the log,
and staple the accepted artifact. This is the safer template for Loqui.

## Inferences and design implications

- **Inference:** Developer ID should stabilize TCC identity for the main app and the fn listener,
  provided their bundle/signing identifiers stay fixed. This must be verified with two separately
  built signed releases; documentation alone cannot attest the machine's TCC behavior.
- A repo-owned release script/task should be the single authority for bundle layout, signing order,
  DMG construction, notarization, and verification. It should fail before signing if the selected
  identity, Keychain profile, expected architecture, or required nested binaries are missing.
- Rebuild every helper during release rather than consuming `helpers/bin`; keep that directory only
  as the direct-development output.
- Move helper executables to `Contents/Helpers`, move Whisper/ggml/SDL dylibs to
  `Contents/Frameworks`, and keep the optional model under `Contents/Resources`. Rewrite the
  Whisper helper and every real dylib's SDL/install-name/rpath metadata so no Homebrew or checkout
  path remains.
- Give each non-bundled helper a stable explicit identifier derived from
  `com.jualopezmo.loquigo`, then sign libraries/frameworks, helpers, and finally the app. Do not put
  app entitlements on library code.
- Apply the host entitlement file (Audio Input plus Apple Events) to the app executable and the
  audio-helper entitlement file (Audio Input only) to `whisper-stt` and `macos-stt` whenever signing
  with Hardened Runtime. Ad-hoc packages deliberately omit Hardened Runtime, secure timestamps, and
  entitlements; `globe-listener`, dylibs, and the framework never receive entitlement files.
- Keep normal local package/dev behavior available, but add a distinct release path that cannot
  silently fall back to ad-hoc signing. A release command either produces a verified notarized DMG
  or exits nonzero without claiming success.
- Use an Apple Development identity and a distinct `.dev` bundle identifier for the daily Wails app,
  while reserving Developer ID for production. Using two signing identities with one bundle ID would
  recreate the TCC collision that `build/config.yml` already avoids between Electron and this port.
- Do not restore Keychain credential storage in this change. First prove signature, notarization,
  launch, TCC continuity, and provider/helper behavior; moving API keys back is a separate migration.

## Open questions and how to close them

- **Certificate identity and Team ID:** create/download the `Developer ID Application` certificate
  through the owner's Apple account, install it with its private key, then select its SHA-1 identity
  from `security find-identity -v -p codesigning`.
- **Notarization profile name:** create a project-specific Keychain profile with `notarytool
  store-credentials`; validate it before the first upload. No Apple credential belongs in Git.
- **Distribution evidence:** install the accepted DMG on another Apple Silicon Mac and execute the
  main app, fn listener, Whisper, Apple Speech (where supported), and Azure. This is the only proof
  that no build-machine dependency remains.
