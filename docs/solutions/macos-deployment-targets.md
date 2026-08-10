# macOS deployment targets must be enforced in every native build

## Symptom

The app plists and main executable originally declared macOS 12, but locally built
`globe-listener`, `whisper-stt`, SDL, and every Whisper/ggml dylib declared macOS 26 in
`LC_BUILD_VERSION`. A package could therefore pass the existing plist audit while its fn listener
and Whisper provider could not load on the declared floor.

`macos-stt` is intentionally different: it uses SpeechAnalyzer APIs introduced in macOS 26 and may
retain a macOS 26 deployment target.

## Root cause

- `swiftc` built `globe-listener` without `-target`, so it inherited the host SDK deployment target.
- The Whisper CMake build omitted `CMAKE_OSX_DEPLOYMENT_TARGET`, so the helper and all generated
  dylibs inherited the host deployment target.
- Reusing the generic upstream `build/` directory also retained an older Homebrew SDL path in
  `CMakeCache.txt`, even after a private `SDL2_DIR` was supplied.
- A relative vendor override made that private `SDL2_DIR` relative too; CMake could not resolve it
  from its build context and silently selected the discoverable Homebrew package instead.
- The build copied an ambient Homebrew SDL dylib. Rewriting its install name and rpath cannot lower
  the deployment target already encoded in the binary.
- `scripts/macos-audit.sh` checked only `LSMinimumSystemVersion`; it did not inspect the Mach-O load
  commands that determine whether each binary can load.

## Fix

- Adopt macOS 14 Sonoma as the global product floor (`LSMinimumSystemVersion=14.0.0`, cgo/linker
  target 14.0). Apple lists the original M1 MacBook Air/Pro, iMac, and Mac mini as Sonoma-compatible,
  and still lists those models for Tahoe 26.
- Compile `globe-listener` for `arm64-apple-macos14.0` and `macos-stt` explicitly for
  `arm64-apple-macos26.0`.
- Configure Whisper/ggml for arm64 and `CMAKE_OSX_DEPLOYMENT_TARGET=14.0`, retaining BLAS, CPU, and
  Metal. Normalize vendor/output overrides to absolute paths and use the dedicated `build-loqui`
  directory so an ambient upstream CMake cache cannot select Homebrew SDL. The BLAS
  `_cblas_sgemm$NEWLAPACK$ILP64` SDK floor is 13.3, inside the selected 14+ contract.
- Build SDL2 2.32.10 from the official pinned commit
  `5d249570393f7a37e037abf22cd6012a4cc56a71` with the same architecture and deployment target,
  then link and package that output instead of a Homebrew bottle. Keep build/install outputs in
  deterministic sibling directories outside the checkout. Fetch the exact SHA from the official
  origin and reject tracked, index, and non-ignored untracked changes before compiling.
- Audit `vtool -show-build` for every packaged Mach-O. Require every reported `minos` value to be
  at most 14.0, with `Contents/Helpers/macos-stt` required to report exactly 26.0.

## Verification

- The shell regression tests first reproduced both gaps: the audit accepted a fixture whose globe
  helper reported 26.0, and the component build never fetched the pinned SDL source.
- After the fix, `scripts/tests/macos-audit-test.sh` and
  `scripts/tests/build-component-helpers-test.sh` pass.
- A real arm64 build reports 14.0 for the globe listener, Whisper helper, SDL, and every
  Whisper/ggml dylib; `macos-stt` reports the intentional 26.0.
- A real bundle assembled from those outputs passes `scripts/macos-audit.sh`.

This verifies binary metadata and bundle enforcement. Actual execution on a macOS 14 machine still
requires an external E2E run; it is not implied by the load-command audit alone. Sources for the
hardware-floor decision, checked 2026-08-09: [Sonoma compatibility](https://support.apple.com/en-us/105113)
and [Tahoe 26 compatibility](https://support.apple.com/en-us/122867).
