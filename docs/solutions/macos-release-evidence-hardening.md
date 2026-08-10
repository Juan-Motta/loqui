# Harden macOS release evidence at the filesystem and signature boundaries

## Problem

The release captured verbose `codesign -dv` metadata but not the designated requirements that keep
TCC identity stable across rebuilds. Its evidence scrubber also replaced only the lexical staging
string. On macOS, `/tmp` and `/var` commonly resolve beneath `/private`; replacing the shorter alias
inside the physical path can create the invalid marker `/private$STAGE`. Cleanup then trusted a broad
`.*candidate*` name pattern rather than the exact candidate shapes created by publication.

## Durable pattern

1. Resolve `TMPDIR` with both `pwd -L` and `pwd -P`, resolve the newly created stage with `pwd -P`,
   and use the physical stage for all build/release work.
2. Let destructive cleanup accept only that physical root plus one exact six-character
   `loqui-release.` child. Candidate cleanup additionally requires a direct child of the release
   directory with an explicit ownership flag set only after exclusive `mktemp`/`mktemp -d`
   creation, and the randomized `.Loqui-*.candidate.??????` or
   `.evidence-*.candidate.??????` shape. A matching pre-existing name is never owned or deleted.
3. Normalize evidence with literal Bash replacement, physical path before lexical alias, then scan
   fail-closed for either original path and `/private$STAGE`.
4. Capture only the `designated =>` line from `codesign -d -r-` for the app and three helpers in a
   fixed order. Repeat that capture from the copied `dmg-root/Loqui.app`, preserve both files, and
   compare them before image creation. Keep verbose metadata in its separate file because timestamps
   may change while DR continuity remains intact.
5. Exercise production shell functions and fake only certificate/network/Apple command boundaries.
   A phase-order smoke test may replace whole phases, but it cannot substitute for branch coverage.
6. Resolve the repository, `bin`, `bin/release`, `evidence`, and `evidence/<version>` components
   physically before publication. Reject any symlink component or destination outside the physical
   repository; perform atomic publication and candidate cleanup only in that resolved output. A
   fresh clone may create missing `bin` and `bin/release` with one-level `mkdir` calls only after the
   repository is physical, followed by `pwd -P` equality checks.
7. Do not rely on `set -e` inside release functions: Bash disables it throughout a function invoked
   from an OR-list. Every production phase checks each relevant command/assignment and returns
   explicitly; `run_release` checks every `run_phase` before advancing.
8. Run final `codesign`, `stapler`, and `spctl` checks with native access to macOS Security and
   LaunchServices. A filesystem sandbox can let byte-level `hdiutil verify` pass while returning
   false invalid-signature, file-not-found, or internal-error results from those system services.
   Treat a sandbox-only failure as inconclusive, then repeat the same strict command outside the
   sandbox before classifying the artifact; do not weaken or skip the production checks.

## Regression coverage

`scripts/tests/release-macos-test.sh` covers symlinked temporary roots, matching/missing/mismatched
DRs, material preflight failures, all signed metadata/entitlement invariants, submit/log failure
branches and retry bounds, Apple verification propagation, final-rename rollback, and cleanup
containment. Publication tests use fixture repositories beneath one temporary root and snapshot the
real `bin/release` in an `EXIT` trap, including early failures. The typed snapshot records absence,
directories, file size/SHA-256, and symlink targets, so an empty or missing release tree is also a
valid baseline. An integrated post-package flow asserts exactly one submission and one outer-DMG
staple, never an app staple. The release remains a single outermost-DMG submission and staple.
