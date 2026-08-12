# Intuitive bilingual DMG installer — E2E use cases

## Verification history and approved Finder boundary

The first real run found that hiding the Loqui.app extension set com.apple.FinderInfo on
the mounted app and invalidated the signing contract. The fix keeps hide_extensions empty.
The next native Finder run exposed .background.tiff through vertical scrolling. The final
fix sets the background kIsInvisible bit and uses a 660 × 384 outer Finder window for the
complete 660 × 360 composition. The owner reviewed and explicitly approved the remaining
user-level vertical scroll indicator and path strip. They are observed, not fabricated as
hidden chrome.

## IDMG-CLI-01 — Real signed and notarized local release

- **ID:** IDMG-CLI-01.
- **Actor:** Release operator.
- **Scenario:** The operator prepares one local macOS release image through the normal release task.
- **Interface:** CLI.
- **Intent:** Confirm that the real local release path signs, notarizes, staples, and Gatekeeper-assesses the canonical DMG without publishing anything.
- **Setup:** A configured Developer ID identity and existing loqui-notary Keychain profile are available; .forge-version has no diff; no GitHub publication command is invoked.
- **Steps:**
  1. Record the .forge-version diff, local tag list, and public v0.1.0 Release metadata.
  2. Run LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos once and record the canonical local DMG, checksum, and sanitized release checks.
- **Verification:** The task exits successfully; the sanitized output shows a signed, notarized, stapled DMG and a Gatekeeper result of accepted from Notarized Developer ID; the local artifact has the canonical name and checksum.
- **Persistence:** Re-read the artifact checksum and the public Release metadata after the task; the artifact remains readable and the public metadata is unchanged.

## IDMG-CLI-02 — Read-only mounted-image contract

- **ID:** IDMG-CLI-02.
- **Actor:** Release operator.
- **Scenario:** The operator mounts the generated local DMG without write access and inspects its installable contents.
- **Interface:** CLI.
- **Intent:** Confirm that the mounted image exposes the approved Finder payload and metadata while preserving the signed application.
- **Setup:** IDMG-CLI-01 produced the canonical local DMG; a unique temporary mount directory exists and cleanup will detach and remove it.
- **Steps:**
  1. Verify the image and attach it with hdiutil -readonly -nobrowse at the unique mount point.
  2. Inspect Finder-visible and hidden root entries, the Applications target, both TIFF frames, semantic .DS_Store metadata, kIsInvisible, and the mounted application signature, Gatekeeper result, designated requirement, and audit.
- **Verification:** Finder-visible root is exactly Loqui.app and Applications; hidden presentation files are .DS_Store and .background.tiff (normal macOS service entries are ignored); Applications resolves to /Applications; TIFF frames are 660 × 360 and 1320 × 720; .background.tiff has FinderInfo flag 0x4000 (kIsInvisible); Loqui.app has no FinderInfo xattr; semantic Finder, strict signature, Gatekeeper, DR, audit, and staple checks pass.
- **Persistence:** Repeat the read-only signature/audit observations before detach; they return the same valid payload.

## IDMG-UI-03 — Native Finder presentation

- **ID:** IDMG-UI-03.
- **Actor:** macOS user installing Loqui.
- **Scenario:** The user opens the read-only mounted DMG in Finder and sees the guided installation composition.
- **Interface:** UI.
- **Intent:** Confirm Finder presents the complete compact bilingual composition with real icons and no visible TIFF.
- **App root:** frontend.
- **App URL:** wails://wails.
- **Persistence mechanism:** server.
- **Setup:** IDMG-CLI-02 mounted the verified image read-only. Playwright is inapplicable because this journey owns a native macOS Finder window, not a browser or Wails webview; the sanctioned driver is the mounted DMG plus Finder.
- **Steps:**
  1. Open the mounted folder in Finder without changing .DS_Store or layout; check-ignore an evidence path under ignored .workflow/e2e-run and capture a local screenshot.
  2. Query Finder bounds and toolbar/status/path properties, then visually inspect the complete 660 × 360 composition for exact copy, arrow, icons, labels, and visibility of the background TIFF.
- **Verification:** Finder reports a 660 × 384 outer window containing the complete 660 × 360 composition. Exact copy is Drag Loqui to Applications and Arrastra Loqui a Aplicaciones. A clean purple left-to-right arrow leads from the real left Loqui icon and visible Loqui label to the real right Applications icon and visible Applications label. .background.tiff is not visible. Toolbar, sidebar, status, and tab UI are hidden. The observed user-level vertical scroll indicator and active volume/path strip are expressly owner-approved for this final Finder presentation.
- **Persistence:** Re-query the same read-only window before detach; the server-owned Finder metadata presents the same composition until the volume is detached.

## IDMG-UI-04 — Finder copy to controlled destination

- **ID:** IDMG-UI-04.
- **Actor:** macOS user installing Loqui.
- **Scenario:** The user copies the mounted Loqui app with Finder into a unique writable test location rather than replacing an existing /Applications/Loqui.app.
- **Interface:** UI.
- **Intent:** Confirm Finder’s public copy engine transfers the real mounted app bundle safely.
- **App root:** frontend.
- **App URL:** wails://wails.
- **Persistence mechanism:** server.
- **Setup:** The read-only mounted DMG is open in Finder; /Applications/Loqui.app is checked first and is not overwritten; a unique writable destination outside /Applications is created. Playwright is inapplicable because this is a native Finder-window journey; the sanctioned driver is Finder acting on the mounted DMG.
- **Steps:**
  1. Use Finder’s public duplicate operation from mounted Loqui.app to the controlled destination, recording that it is Finder’s copy engine rather than a mouse-drag claim.
  2. Verify the resulting bundle’s strict signature, Gatekeeper assessment, audit, and designated requirement against the mounted source, then remove only the controlled destination.
- **Verification:** Finder completes the duplicate; the destination contains a real Loqui.app with valid strict signature, accepted Gatekeeper result, successful audit, and the same DR as the mounted source. /Applications/Loqui.app remains untouched.
- **Persistence:** Rerun local signature, DR, Gatekeeper, and audit verification before cleanup; all identify the same valid copied app.

## IDMG-CLI-05 — No-publication persistence

- **ID:** IDMG-CLI-05.
- **Actor:** Release operator.
- **Scenario:** The operator confirms local E2E release preparation did not mutate version, tags, or the public GitHub Release.
- **Interface:** CLI.
- **Intent:** Preserve the immutable v0.1.0 public release while validating only a local candidate.
- **Setup:** Record the pre-run .forge-version diff, local tags, and public v0.1.0 Release metadata; do not run tag or GitHub Release mutation commands.
- **Steps:**
  1. After detaching the image, re-read the version diff, local tag list, and public v0.1.0 Release metadata.
  2. Repeat the public Release read and compare its target, draft/prerelease status, and canonical two assets to the baseline.
- **Verification:** .forge-version has no diff; local tags remain only v0.1.0; public Release stays non-draft/non-prerelease at the baseline commit with exactly the canonical DMG and SHA-256 asset; no publication operation occurs.
- **Persistence:** The second public read returns the same immutable Release metadata and asset set.
