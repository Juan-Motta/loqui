# Developer ID release E2E report

- **Feature:** local Developer ID signing, notarization, and arm64 DMG release
- **Branch:** `feat/developer-id-release`
- **Timestamp:** `2026-08-10T15:46:11Z`
- **Execution target:** local non-production release Mac

VERDICT: PASS

Every CLI and native UI journey passes, including notarized installation, permission attribution and
persistence, model recovery, installed providers, a second-candidate upgrade, and the clean
second-Mac offline-ticket journey. No screenshot or trace was retained; these journeys cross Finder,
native macOS TCC, restart, and network surfaces rather than a browser-controlled page. There is no
API journey applicable to this distribution feature.

## DID-CLI-01 — PASS

- **Interface:** CLI
- **Commands:** rebuilt native helpers at pinned whisper.cpp `97c56f1…`; ran
  `./scripts/task.sh package` twice; after each run invoked
  `./scripts/macos-audit.sh --channel production --version 0.1.0 bin/loqui.app`.
- **Observed output:** both package commands exited 0; both audits printed
  `macos-audit: ok bin/loqui.app`.
- **Verification:** both app bundles contained `icons.icns`, no `Assets.car`, no
  `Contents/Resources/helpers`, and no Mach-O under Resources. The integration run also exercised
  the universal Azure framework and complete Whisper/ggml/SDL symlink families.
- **Persistence re-check:** the second invocation replaced the same app path and the full audit
  remained green.

## DID-CLI-02 — PASS

- **Interface:** CLI
- **Commands:** built a temporary `DEV=true` binary; assembled and audited `Loqui.dev.app`; signed
  through `macos-sign.sh app --channel development`; invoked
  `macos-sign.sh resolve --channel development` again.
- **Observed output:** development audit printed `macos-audit: ok`; signing verification exited 0;
  resolver printed `TCC continuity is unavailable` and returned `-` because
  `security find-identity` reports zero valid identities.
- **Verification:** the development bundle had the `.dev` bundle ID, standard code layout, no code
  under Resources, and a valid explicit ad-hoc signature. It did not claim stable TCC continuity.
- **Persistence re-check:** the second resolver invocation observed the same honest fallback state.

## DID-CLI-03 — PASS

- **Interface:** CLI
- **Commands:** ran `./scripts/task.sh check`, validated the `loqui-notary` Keychain profile, and
  invoked `LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos` twice from dirty
  pre-commit tree `59eadef-dirty`, preserving candidate 1 before the second run.
- **Observed output:** both release commands exited 0. Each notarization was accepted; stapler
  validation, `hdiutil verify`, and Gatekeeper assessment passed with `source=Notarized Developer
  ID`; each run published `bin/release/Loqui-0.1.0-macos-arm64.dmg` only after those checks.
- **Verification:** two distinct submission evidence directories remain, each with all 11 expected
  files. Their repository revision, pinned Whisper revision, signed Mach-O inventory, Team ID set,
  and code-identifier set agree. Candidate app plus `globe-listener`, `macos-stt`, and `whisper-stt`
  designated requirements match exactly. Evidence scanning found no credential or checkout path.
- **Persistence re-check:** candidate 2 retained compatible production identities and did not
  overwrite candidate 1's submission evidence. Candidate 1 remains preserved outside the repository
  for the upgrade journey.
- **Regenerated artifact check:** after a contained test-fixture incident invalidated the ordinary
  output path, a deliberate third release was accepted as submission
  `4c9f131e-8378-4d3a-838f-8f6147b0e2c3`. The published 11,302,960-byte DMG has SHA-256
  `c9a4ab42a18158533cc8a7ca82f4e240ac34e6fb5ab0354bab41b556db99a864`. With native macOS Security
  access, `hdiutil`, `codesign`, stapler and DMG Gatekeeper checks passed; a read-only mount then
  passed the production audit, deep app signature and app Gatekeeper assessment. The new 14-file
  evidence directory has accepted submit/log state, identical pre/post-DMG designated requirements,
  and no checkout path or secret field. Candidate 1's hash remained unchanged.

## DID-UI-01 — PASS

- **Interface:** UI
- **Locator map:** Finder `Loqui.app` → `/Applications`; System Settings → Privacy & Security →
  Accessibility/Input Monitoring rows named `Loqui`; Loqui → Permissions → Recheck; target app's
  focused text field for visible pasted output.
- **Assertions:** Finder launch shows no unidentified-developer warning; macOS attributes the grants
  to Loqui; the fn trigger starts dictation; the dictated text appears in the target application.
- **Observed output:** the first Accessibility attempt exposed a stale TCC decision left by the
  previous ad-hoc app with the same bundle ID. After a bundle-scoped Accessibility reset and adding
  the exact `/Applications/Loqui.app`, the operator confirmed both native permission rows identify
  Loqui, the grant is recognized, fn works, and dictation pastes successfully.
- **Tool/browser versions:** current native Finder/System Settings and the installed Wails app;
  Playwright is not applicable to Finder/TCC prompts. No credential, transcript, screenshot, or trace
  was captured.
- **Command/exit status:** supporting repeated `codesign --verify --deep --strict` and Gatekeeper
  assessment exited 0; native journey result confirmed by the operator.
- **Persistence re-check:** after fully quitting and relaunching Loqui, Accessibility remained
  granted and fn-triggered dictation still pasted into another application.

## DID-UI-02 — PASS

- **Interface:** UI
- **Locator map:** Loqui engine selector → Whisper; managed-model row → download/cancel/retry actions;
  target application's focused text field → visible transcription.
- **Assertions:** an interrupted transfer does not become ready; retry completes validation; Whisper
  dictation appears in the target application without a bundled model.
- **Observed output:** the operator followed the interruption/retry sequence, confirmed the model
  became usable after retry, and observed successful Whisper dictation. Transcript content was not
  captured.
- **Tool/browser versions:** installed native Wails app; no browser automation or retained artifact.
- **Command/exit status:** native journey result confirmed by the operator.
- **Persistence re-check:** after relaunch, the validated managed model remained usable and Whisper
  dictation still worked.

## DID-UI-03 — PASS

- **Interface:** UI
- **Locator map:** Loqui engine selector → Apple STT/Azure/another configured cloud provider; target
  application's focused text field → visible completed transcription.
- **Assertions:** each selected engine completes dictation and the text appears in the target
  application without a helper, framework, or dynamic-library error.
- **Observed output:** the operator confirmed successful installed-release dictation through Apple
  STT, Azure, and another configured provider. Provider keys and transcript text were neither read
  nor captured.
- **Tool/browser versions:** installed native Wails app; no browser automation or retained artifact.
- **Command/exit status:** native journey result confirmed by the operator.
- **Persistence re-check:** a subsequent app relaunch/upgrade retained the selected configuration and
  installed dictation remained usable.

## DID-UI-04 — PASS

- **Interface:** UI
- **Locator map:** candidate-2 DMG → Finder `Loqui.app` → Applications `Replace`; relaunched Loqui;
  fn trigger and target application's focused text field.
- **Assertions:** candidate 2 works immediately after replacing candidate 1; fn and dictation remain
  functional; macOS does not request established permissions again.
- **Observed output:** the operator installed candidate 2 over candidate 1 without changing any TCC
  setting and confirmed immediate fn-triggered dictation and paste with no renewed Accessibility,
  Input Monitoring, or microphone prompt.
- **Tool/browser versions:** native Finder/System Settings and installed Wails app; no browser
  automation or retained artifact.
- **Command/exit status:** native journey result confirmed by the operator.
- **Persistence re-check:** after fully quitting and relaunching candidate 2, fn dictation continued
  working without re-granting permissions.

## DID-UI-05 — PASS

- **Interface:** UI
- **Locator map:** candidate-1/current DMG → Finder mount → `Loqui.app` → Applications; macOS network
  control → offline; Apple menu → restart; Finder → installed Loqui; Terminal → Gatekeeper
  assessment; Loqui engine selector → Whisper/cloud; fn trigger → dictation; current DMG → Finder
  `Replace` over candidate 1.
- **Assertions:** after online assess/mount/copy/eject, offline reboot, first launch and Gatekeeper
  assessment succeed; no Homebrew/checkout dependency appears; dictation works after networking is
  restored; installing the current release over candidate 1 preserves established permissions.
- **Observed output:** the operator confirmed the complete second-Mac journey works: the copied app
  launched after the offline reboot, Gatekeeper accepted it without network access, provider/model
  and fn-triggered dictation worked, and the second install retained the established grants. No
  credential, transcript, screenshot, or trace was captured.
- **Tool/browser versions:** clean second Apple Silicon Mac using native Finder, Gatekeeper, network
  settings, restart, and the installed Wails app; browser automation is not applicable.
- **Command/exit status:** offline Gatekeeper assessment and first launch were confirmed successful
  by the operator; native journey result confirmed in-session on 2026-08-10.
- **Persistence re-check:** after reboot and installing the current release over candidate 1,
  Gatekeeper trust remained effective and the previously granted permissions continued working.

## Residual state and next execution

- Local build artifacts remain under ignored `bin/` and `helpers/bin/`; candidate 1 is preserved as
  `bin/release/Loqui-0.1.0-macos-arm64-candidate-1.dmg`, while the ordinary release path now contains
  the regenerated notarized artifact. Dummy incident evidence ID
  `11111111-1111-1111-1111-111111111111` remains deliberately preserved. A validated managed model
  and TCC grants remain in the test user's normal data. No credential was read, copied, or captured.
- Both stapled candidates were exercised on a clean second Apple Silicon Mac; no additional E2E
  residual state was captured in the repository.
- Every required E2E case passes. This report satisfies the E2E evidence portion of the ship gate;
  the deterministic gate checker and final verification still govern branch shipping.
