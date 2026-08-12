# E2E report — Intuitive bilingual DMG installer

- **Branch:** feat/intuitive-dmg-installer
- **Executed:** 2026-08-12T17:05:19Z–2026-08-12T17:13:51Z
- **Target:** one local-only Developer ID release; no GitHub publication

VERDICT: PASS

All five real journeys pass. This final run used one credentialed local release invocation and
never created, moved, or deleted a Git tag; did not change the version; and made no GitHub
Release mutation. No credentials are recorded here.

## Corrective history and owner-approved presentation

Two prior E2E iterations correctly stopped as FAIL_BUG:

1. hide_extensions = ["Loqui.app"] placed com.apple.FinderInfo on the mounted signed app.
   The root fix is hide_extensions = [], so Loqui.app has no FinderInfo xattr.
2. A 660 × 360 outer Finder window allowed scrolling that exposed .background.tiff.
   The root fix uses a 660 × 384 outer window for the 660 × 360 composition and applies the
   Finder kIsInvisible bit to .background.tiff.

The owner reviewed the final native Finder appearance and explicitly accepted the remaining
user-level vertical scroll indicator and volume/path strip. They are reported below as observed
Finder state, rather than misrepresented as hidden chrome.

## IDMG-CLI-01 — Real signed and notarized local release

- **Classification:** PASS
- **Interface:** CLI
- **Sanitized command:** LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos
- **Result:** Exit 0 from the one and only credentialed release invocation. Sanitized output showed
  hdiutil verify is VALID; downloaded notary submission log
  a6156d60-6f56-4408-aa19-582acfa6290a; successful staple and validate; and outer-DMG
  Gatekeeper accepted, source=Notarized Developer ID.
- **Artifact:** bin/release/Loqui-0.1.0-macos-arm64.dmg, 11,439,938 bytes,
  SHA-256 89a1f6839e59da66f21bcc6d4bb34e08db224509064ffdbd203d2aa65173e193.
- **Persistence:** The later mounted-image checks, xcrun stapler validate, and structural
  verification all continued to pass against that exact local artifact.

## IDMG-CLI-02 — Read-only mounted-image contract

- **Classification:** PASS
- **Interface:** CLI
- **Sanitized commands:** hdiutil attach -readonly -nobrowse -mountpoint <unique>/mount <DMG>;
  verify-ds-store.py <mount>/.DS_Store; codesign, spctl, macos-audit, and
  xcrun stapler validate read-only checks.
- **Mount:** /private/tmp/loqui-dmg-e2e.6P3hK1/mount (unique and later removed).
- **Result:** The root payload was Loqui.app and Applications; normal service entries were
  excluded from the presentation contract. Hidden presentation files were .DS_Store
  (16,388 bytes) and .background.tiff (332,292 bytes). Applications resolved to
  /Applications. TIFF inspection found exactly 660 × 360 and 1320 × 720 RGBA frames.
  verify-ds-store: PASS confirmed the 660 × 384 window contract, 128-point icons, positions,
  background alias, and hidden toolbar/sidebar/status/tab/path settings. The background FinderInfo
  was a 32-byte value with flag 0x4000 (kIsInvisible); Loqui.app had no FinderInfo xattr.
- **Security evidence:** Strict deep codesign passed. Its DR was
  identifier "com.jualopezmo.loquigo" with Apple generic anchor and Team ID DT5NB5DE7U.
  Execution Gatekeeper accepted it as Notarized Developer ID from
  Developer ID Application: Juan Andres Lopez Motta (DT5NB5DE7U).
  macos-audit --channel production --version 0.1.0 returned ok; stapler validation worked.
- **Persistence:** Signature/audit observations were repeated on the same read-only mount before detach.

## IDMG-UI-03 — Native Finder presentation

- **Classification:** PASS
- **Interface:** UI (native Finder)
- **Driver:** Mounted DMG plus Finder. Playwright is inapplicable because this journey owns a
  macOS Finder window, not a browser or Wails webview; app root frontend, app URL
  wails://wails, persistence mechanism server.
- **Evidence:** git check-ignore -v confirmed
  .workflow/e2e-run/2026-08-12-intuitive-dmg-installer-final.png is ignored. The screenshot
  is retained locally only, was visually inspected, and is not a tracked artifact.
- **Measured/queried state:** Finder returned bounds 100, 956, 760, 1340, exactly
  660 × 384 outer size. Finder returned toolbar=false, status=false, path=true.
  The screenshot also shows the user-level vertical scroll indicator and a bottom volume/path
  strip; both are owner-approved in this final appearance.
- **Visual result:** The full 660 × 360 composition was visible, without a visible
  .background.tiff; exact copy was Drag Loqui to Applications and
  Arrastra Loqui a Aplicaciones. It showed a clean purple left-to-right arrow, real Loqui
  icon/Loqui label on the left, and real Applications icon/Applications label on the right.
  Toolbar, sidebar, status bar, and tab UI were absent.
- **Persistence:** The volume was not altered during observation; the Finder layout came from the
  read-only image metadata until cleanup detached it.
- **Limitation:** A screenshot is local visual evidence, not browser automation; no browser evidence
  is claimed for this native Finder journey.

## IDMG-UI-04 — Finder copy to controlled destination

- **Classification:** PASS
- **Interface:** UI (native Finder; Playwright inapplicable)
- **Method:** Finder public duplicate operation, not a mouse drag. It copied mounted Loqui.app
  to /private/tmp/loqui-dmg-copy.pfRni9/destination/Loqui.app, outside /Applications.
- **Result:** The copied bundle passed strict deep codesign, Gatekeeper (accepted,
  source=Notarized Developer ID), and macos-audit. Its DR exactly matched the mounted source.
  /Applications/Loqui.app existed before and after the test and was never overwritten or deleted.
- **Persistence:** All copied-bundle checks completed before its test-only temporary destination was removed.

## IDMG-CLI-05 — No-publication persistence

- **Classification:** PASS
- **Interface:** CLI
- **Baseline and two post-cleanup reads:** .forge-version diff remained empty; local tags were
  only v0.1.0; /Applications/Loqui.app remained present. Both read-only GitHub Release results
  were identical to baseline: v0.1.0 target
  50f53f7bcc3d35637df96cda106a6ea8e1ea97da, non-draft, non-prerelease, and exactly two assets:
  Loqui-0.1.0-macos-arm64.dmg
  (sha256:49acadc8ad7782f95e348ba4fe95f2a3ade21f933c3c53e0ddadbb805ad86bc1) and
  Loqui-0.1.0-macos-arm64.dmg.sha256
  (sha256:353a7ae8e06604a06a781a4efd960eefafa17318d853b55157c65320d601a33d).
- **Persistence:** The second post-cleanup Release read matched the first exactly. No
  github-release command, gh release create/edit, tag mutation, commit, push, or PR command
  was run.

## Cleanup, environment, and ship evidence

- The Finder volume window was closed, disk4 was detached, and only
  /private/tmp/loqui-dmg-e2e.6P3hK1 plus /private/tmp/loqui-dmg-copy.pfRni9 were removed.
  hdiutil info contained neither test image nor mount; /Applications/Loqui.app was preserved.
- **macOS:** 26.5.2 (25F84); **Xcode:** 26.6 (17F113); **notarytool:** 1.1.2 (41).
- Fresh post-E2E `CI=true ./scripts/task.sh check` exited 0, including Go tests/vet, frontend
  typecheck, every macOS release contract, and a real mounted DMG integration with `hdiutil`
  reporting `VALID`. `sh shared/scripts/check-gates.sh` then reported all six standard-profile
  boxes checked. The screenshot remains ignored local-only evidence.
