# Release tests must isolate version fixtures

## Symptom

A correct stable-version bump can make the full release test suite fail even after
`patch-plists.sh --check` passes. The failures may claim that the audited app expected the old
version or that a publication helper received the new DMG filename instead of the old one.

## Root cause

Two distinct test styles had been mixed:

- behavior fixtures intentionally exercise a fixed example version such as `0.1.0`;
- integration fixtures copy or source files from the current repository and therefore observe the
  current stable version.

`macos-audit-test.sh` copied the repository's live production plist but passed a fixed `0.1.0` to
the audit. `release-macos-test.sh` pointed `phase_publish` at the live repository root while setting
its separate `version` variable and expected DMG name to `0.1.0`. Both tests were internally
consistent only while the public version happened to equal their fixture literal.

## Fix

- A behavior test that wants a fixed version must explicitly normalize its copied plist to that
  fixture version. The real propagation contract remains covered by `patch-plists-test.sh`.
- An integration test that points at the live repository must derive both the version and canonical
  DMG name through `release-version.sh`, the same interface production uses.
- Do not replace every old version literal globally. Most literals belong to isolated fixtures and
  should remain stable across public releases.

## Verification

The original failures were captured before either fix. After the focused tests passed,
`CI=true ./scripts/task.sh check` completed with exit 0, including `macos-audit-test.sh`,
`release-macos-test.sh`, the real DMG integration, and `hdiutil verify`.
