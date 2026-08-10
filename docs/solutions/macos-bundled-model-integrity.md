# Validate the optional bundled Whisper model at both boundaries

## Symptom

`LOQUI_BUNDLE_MODEL=1` copied `helpers/bin/ggml-small.bin` into the app without proving that the
source was the pinned Whisper model. The package auditor ignored a bundled model entirely. A
truncated file or an unrelated payload could therefore be signed and shipped, then fail later as a
misleading transcription problem.

## Root cause

The downloader owns a canonical production identity in `internal/store/model.go`: exact byte count
plus SHA-256. Bundle assembly treated the development-side filename as sufficient identity, and the
audit only enforced that Resources contained no Mach-O code.

## Fix

`scripts/whisper-model-integrity.sh` provides one fixed shell wrapper for the production size and
digest. It deliberately exposes no environment or command-line override for those values.

- Bundle assembly validates the source before `cp -L` and the staged destination after it.
- Package audit requires a real in-bundle file and validates it whenever it exists; the standard
  no-model release remains valid.
- `scripts/tests/whisper-model-integrity-test.sh` compares the shell constants with
  `internal/store/model.go`, turning pin drift into a test failure.

The generic integrity primitive accepts explicit expected values only so tests can exercise real
hashing with a tiny fixture. Production callers use the fixed wrapper.

## Verification

The focused shell tests demonstrated RED before implementation: bundle assembly accepted a
wrong-sized fixture and package audit accepted both wrong size and wrong digest. GREEN covers valid,
wrong-size, and wrong-digest models in both paths, plus path-specific failures proving that the
post-copy destination is checked and a symlink to an external model is rejected. Tool fakes preserve
the real production expectations without copying or hashing a 487 MB fixture.

This is an internal release-integrity guard, so user-facing E2E is not applicable; the observable
contract is that malformed artifacts fail before signing.
