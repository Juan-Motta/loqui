# macOS automatic updates research

Checked: 2026-08-13

## Questions

1. Can Loqui use the updater already shipped by the pinned Wails version?
2. Which release artifact can be installed safely in place on macOS?
3. How can automatic checks remain unobtrusive, localized, and user-controlled?
4. What must change in the release pipeline without removing the existing DMG installer?

## Verified findings

### Wails updater fit

- The pinned `github.com/wailsapp/wails/v3 v3.0.0-alpha2.119` module already contains
  `pkg/updater`, the GitHub/endpoint/appcast providers, and `application.App.Updater`; adding a
  new updater dependency is unnecessary. This is a local source inspection of the pinned Go
  module, not an inference from a newer release.
- Wails documents `app.Updater.Init`, `Check`, and `CheckAndInstall`, with GitHub Releases,
  endpoint, and Sparkle AppCast providers. Its default UI can be replaced by a built-in or custom
  window. Source: [Wails self-update FAQ](https://v3.wails.io/faq/) and
  [self-update tutorial](https://v3.wails.io/tutorials/04-self-update-a-wails-app/), checked
  2026-08-13.
- The pinned extractor accepts ZIP and tar.gz archives and swaps an extracted `.app` bundle. It
  does not extract a DMG. Therefore the updater release must publish a ZIP containing one
  top-level `Loqui.app`; the DMG remains the manual installer.
- The GitHub provider can select a platform/architecture asset and consume a SHA-256 checksum
  asset. A custom matcher is needed when both DMG and ZIP are present so the updater never chooses
  the manual installer.
- In the pinned Wails implementation, `Config.CheckInterval` calls `CheckAndInstall` on each tick;
  that flow downloads and stages an update automatically. Loqui must therefore leave that option
  disabled and own a check-only scheduler that calls `Check`, preserving the explicit install
  confirmation requirement.

### macOS distribution and notarization

- Apple documents that ZIPs can be submitted for notarization, but a ZIP itself cannot be stapled;
  the app inside must be stapled before the final ZIP is assembled. Source:
  [Apple Customizing the notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow?changes=l_4),
  checked 2026-08-13.
- Sparkle is a mature native macOS updater using appcasts and EdDSA signatures, but adopting it
  would add a second updater stack beside Wails. Source: [Sparkle documentation](https://sparkle-project.org/documentation/),
  checked 2026-08-13.

### Repository fit

- `internal/app/about_service.go` already reads the running bundle's
  `CFBundleShortVersionString`; the updater should reuse that runtime version instead of parsing
  build metadata independently.
- `internal/store/config.go` persists settings while preserving unknown JSON keys. An
  `autoUpdateChecks` boolean can therefore be added compatibly with a default of enabled.
- The native tray is built in `main.go` (`newTray` / `buildTrayMenu`) and the About view is already
  bound through `internal/app/about.go` and `frontend/src/about.ts`; both are existing entry points
  for a manual “Check for updates” action.
- `.github/workflows/release.yml`, `scripts/release-macos.sh`, and `scripts/github-release.sh`
  currently publish only the canonical Apple Silicon DMG and its checksum. The release contract
  and tests must be extended rather than replacing the DMG.

## Prior-art comparison

| Option | Strength | Cost / risk |
| --- | --- | --- |
| Wails updater + GitHub Releases + ZIP | Reuses the pinned framework, no new runtime dependency, fits existing release hosting | Requires a ZIP release asset and careful asset matching; baseline checksum is weaker than a signed manifest |
| Wails endpoint/AppCast + Ed25519 manifest | Strong explicit signing and full control over rollout metadata | Adds manifest hosting, key lifecycle, and more release infrastructure |
| Sparkle integration | Mature macOS UX and signing model | Second native updater stack, extra integration surface, and duplicated lifecycle/UI work |

## Recommendation

Implement the first release with the existing Wails updater, a GitHub Releases provider, a
platform-specific ZIP asset, and a static SHA-256 checksum asset. Keep the update check automatic
but configurable, never install or restart silently, and expose an explicit tray/About action.
Leave the provider boundary open for a signed endpoint manifest if the threat model later requires
keyed update metadata or staged rollouts.

## Risks and follow-ups

- The first updater-enabled release cannot update installations of older releases until the user
  installs that release manually once; the existing `v0.2.0` has no updater ZIP.
- A checksum protects integrity when the release and checksum are fetched from the same trusted
  GitHub source, but it is not an independent publisher signature. Ed25519-signed manifests are a
  follow-up hardening option.
- Automatic checks must not block startup, spam the user, or invoke a restart without confirmation.
