# Keep Finder presentation metadata off signed macOS app bundles

## Symptom

A `dmgbuild` image passed `hdiutil verify`, but release verification rejected the mounted
`Loqui.app` because it contained `com.apple.FinderInfo`. Finder also treated the generated
`.background.tiff` as scrollable content and could expose it below the installer composition.

## Root cause

`dmgbuild 1.6.7` applies two different Finder operations after copying the signed bundle:

- `hide_extensions` calls `SetFile -a E` on the named item. Doing this to `Loqui.app` adds
  `com.apple.FinderInfo` after signing and makes `codesign --verify --deep --strict` reject the
  mounted copy.
- `hide` calls `SetFile -a V`. The generated background was not in this list, so a dot-prefixed
  filename alone did not keep Finder from treating it as view content.

Giving `.background.tiff` an explicit `Iloc` is not a fix: Finder renders it as an icon-view item.

## Fix

- Leave `hide_extensions = []`; Finder normally displays an application bundle named `Loqui.app`
  as `Loqui` without modifying the signed bundle.
- Set `hide = [".background.tiff"]`, and do not add that resource to `icon_locations`.
- Verify the mounted app has no `com.apple.FinderInfo` and still passes strict deep code-signing.
- Verify the mounted background has a 32-byte FinderInfo value whose flags contain
  `kIsInvisible` (`0x4000`).
- Keep the semantic `.DS_Store` contract limited to the real `Loqui.app` and `Applications` icon
  locations.

## Verification

The real-generator regression was observed RED in both directions: the original settings added
FinderInfo to the mounted app, and the intermediate background configuration lacked the invisible
flag. With the fix, a real mounted DMG passed `hdiutil verify`, strict app signing, FinderInfo flag,
Retina TIFF, and semantic `.DS_Store` checks. A Developer ID release then passed notarization,
stapling, Gatekeeper, mounted audit/designated-requirement checks, and native Finder E2E. Evidence is
in `docs/e2e/reports/2026-08-11-intuitive-dmg-installer.md`.
