# Developer ID release use cases

## DID-CLI-01 — Repeatable audited local package

- **Actor:** Loqui maintainer on the release Mac.
- **Scenario:** Build the ordinary production package twice from the same working tree.
- **Interface:** CLI.
- **Intent:** Prove the public package task assembles the standard portable layout repeatedly.
- **Setup:** macOS arm64 checkout with project dependencies and native helpers available.
- **Steps:**
  1. Run `./scripts/task.sh package`, then audit `bin/loqui.app` for production version `0.1.0`.
  2. Run the same package command again and repeat the audit and Resources inspection.
- **Verification:** Both audits say `macos-audit: ok`; the app has no `Assets.car`, no
  `Contents/Resources/helpers`, and no Mach-O under Resources.
- **Persistence:** The second invocation replaces the same app path and preserves the verified
  layout and behavior.

## DID-CLI-02 — Development signing fallback

- **Actor:** Loqui developer without an Apple Development identity.
- **Scenario:** Assemble and sign a development bundle on a zero-identity Mac.
- **Interface:** CLI.
- **Intent:** Keep development usable while making the loss of TCC continuity explicit.
- **Setup:** A development main binary plus rebuilt native helpers; no valid code-signing identity.
- **Steps:**
  1. Assemble `Loqui.dev.app`, run the development auditor, and sign through
     `macos-sign.sh app --channel development`.
  2. Resolve the development identity again through `macos-sign.sh resolve --channel development`.
- **Verification:** Audit succeeds; signing verifies; stderr says `TCC continuity is unavailable`;
  resolution returns `-` and does not claim stable signing.
- **Persistence:** The second identity resolution observes the same explicit fallback state.

## DID-CLI-03 — Developer ID release and evidence

- **Actor:** Release maintainer with Apple Developer Program access.
- **Scenario:** Produce two notarized arm64 DMGs through the public release entry point.
- **Interface:** CLI.
- **Intent:** Prove signing, ticket coverage, stapling, Gatekeeper verification, evidence retention,
  and stable designated requirements.
- **Setup:** One intended Apple Development identity, one intended Developer ID Application identity,
  and a validated `loqui-notary` Keychain profile.
- **Steps:**
  1. Run `./scripts/task.sh check`, then `LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos`.
  2. Preserve candidate 1, run the release again, and compare app/helper designated requirements and
     evidence directories.
- **Verification:** Both DMGs are accepted, stapled, Gatekeeper-approved, signed by the same Team ID,
  and have complete ticket coverage with no secret or checkout path in evidence.
- **Persistence:** The second release retains compatible production identities and does not overwrite
  the first submission's evidence.

## DID-UI-01 — Install, launch, and permission attribution

- **Actor:** Loqui user on the release Mac.
- **Scenario:** Mount the notarized DMG, install through Finder, launch, and grant first-use permissions.
- **Interface:** UI.
- **App root:** `frontend`.
- **App URL:** `wails://wails`.
- **Intent:** Prove Gatekeeper accepts the app and macOS attributes prompts to the expected Loqui code.
- **Setup:** Fresh notarized production DMG and reset test-user grants.
- **Steps:**
  1. Mount the DMG, drag Loqui to Applications, eject the DMG, and launch Loqui from Finder.
  2. Exercise Accessibility, Input Monitoring, microphone, and speech paths; record the visible
     process/app name and usage text for each prompt, then trigger the fn listener.
- **Verification:** Loqui opens without an unidentified-developer warning; prompts show the intended
  Loqui attribution and documented text; the fn trigger is received.
- **Persistence:** `server` — quit and relaunch the installed app; macOS TCC grants remain established.
- **Persistence mechanism:** `server`.

## DID-UI-02 — Clean Whisper download interruption and retry

- **Actor:** Loqui user with no managed or bundled Whisper model.
- **Scenario:** Start the supported first-use model download, interrupt it once, retry, then dictate.
- **Interface:** UI.
- **App root:** `frontend`.
- **App URL:** `wails://wails`.
- **Intent:** Prove a portable release can acquire and validate Whisper without Homebrew or checkout files.
- **Setup:** Installed notarized Loqui with the exact managed model safely absent or guarded for restore.
- **Steps:**
  1. Select Whisper, start its offered download, interrupt the controlled transfer, and retry it.
  2. Wait for digest/size validation, then dictate and observe the transcription.
- **Verification:** The interrupted partial file is not accepted; retry completes; the UI shows the
  model ready and displays a successful transcription without a bundled model.
- **Persistence:** `server` — relaunch Loqui and dictate again using the validated managed model.
- **Persistence mechanism:** `server`.

## DID-UI-03 — Native and cloud engines from the installed release

- **Actor:** Configured Loqui user.
- **Scenario:** Dictate with Apple STT where supported, Azure, and one additional configured cloud engine.
- **Interface:** UI.
- **App root:** `frontend`.
- **App URL:** `wails://wails`.
- **Intent:** Prove nested helpers/frameworks and provider wiring survive release packaging.
- **Setup:** Installed notarized app, required OS support, microphone grant, and user-entered provider keys.
- **Steps:**
  1. Select Apple STT and dictate; then select Azure and dictate.
  2. Select one other configured cloud provider and dictate again.
- **Verification:** Each supported engine shows a completed transcription in the target application;
  no helper, framework, or dynamic-library launch error appears.
- **Persistence:** `server` — relaunch Loqui and confirm the selected engine/configuration remains usable.
- **Persistence mechanism:** `server`.

## DID-UI-04 — Production upgrade preserves grants

- **Actor:** Loqui user who granted permissions to release candidate 1.
- **Scenario:** Install release candidate 2 over candidate 1.
- **Interface:** UI.
- **App root:** `frontend`.
- **App URL:** `wails://wails`.
- **Intent:** Prove stable production identities preserve Accessibility, Input Monitoring, and microphone grants.
- **Setup:** Two separately built/notarized candidates signed by the same Team ID; candidate 1 installed and granted.
- **Steps:**
  1. Complete a dictation and fn trigger with candidate 1, then install candidate 2 over it.
  2. Launch candidate 2 and repeat the fn trigger and dictation without re-granting permissions.
- **Verification:** Candidate 2 works immediately and macOS does not ask again for established grants.
- **Persistence:** `server` — reboot/relaunch and confirm the grants remain effective.
- **Persistence mechanism:** `server`.

## DID-UI-05 — Second-Mac offline ticket proof

- **Actor:** Loqui user on another Apple Silicon Mac with no checkout or Homebrew dependency.
- **Scenario:** Install both releases and prove first launch of the copied app with networking disabled.
- **Interface:** UI.
- **App root:** `frontend`.
- **App URL:** `wails://wails`.
- **Intent:** Prove the outer DMG ticket covers the installed nested code and the app is truly portable.
- **Setup:** Clean second Mac, two stapled DMGs, and network initially available only for assessment/mount.
- **Steps:**
  1. Assess and mount release 1 online, copy Loqui to Applications, eject, disable networking, reboot,
     then launch and run Gatekeeper assessment offline.
  2. Restore networking for model/provider checks, exercise Whisper/fn/cloud dictation, install release 2,
     and repeat the permission-continuity checks.
- **Verification:** Offline first launch and `spctl` succeed; no Homebrew/checkout dependency appears;
  real dictation works; release 2 preserves established grants.
- **Persistence:** `server` — after reboot and the second install, Gatekeeper trust and TCC grants remain.
- **Persistence mechanism:** `server`.
