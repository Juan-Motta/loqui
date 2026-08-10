# Sanitize read-only macOS dependencies only after safe staging

## Symptom

The real Developer ID release failed during bundle assembly with `Permission denied` when
`xattr -cr` reached the staged `libSDL2-2.0.0.dylib`. Hermetic tests passed because their fixture
files were writable, and an existing development helper directory had also hidden the fresh-copy
behavior.

## Root cause

Homebrew supplies SDL as a read-only file carrying `com.apple.provenance`. The bundle assembler used
`cp -a`, correctly preserving dylib symlinks but also preserving SDL's mode and extended attributes.
The later `xattr -cr` therefore tried to mutate a read-only staged file and failed. Framework source
directories can have the same problem because recursive attribute removal mutates directory
metadata too.

Blindly applying a recursive `chmod` was unsafe: a staged symlink could point outside the app and a
later recursive mutation could then affect its target.

## Fix

- After all copies, resolve every staged symlink physically and reject absolute, unresolvable, or
  escaping targets before any mutation.
- Accept normal framework and dylib links, including contained `..` segments and directory links,
  based on their resolved location rather than their textual spelling.
- Once containment is proven, make only staged real files and directories user-writable with
  `find ... \( -type f -o -type d \) -exec chmod u+w`, which does not select symlinks.
- Continue clearing inherited attributes and normalizing Mach-O load commands only inside that safe
  staged tree. Source modes and attributes remain unchanged.

## Verification

- The regression fixture reproduces read-only SDL (`0444` plus an xattr) and a read-only framework
  directory (`0555` plus an xattr). Both failed before the fix and pass after it.
- Absolute and relative file escapes plus a relative directory escape are rejected before mutation;
  their external targets retain their original mode and xattr.
- Contained relative file and directory symlinks remain intact. A mutation that bypassed the
  directory-link resolution made its focused regression fail, then the restored guard passed.
- The focused bundle test, complete macOS release suite, ShellCheck, Bash syntax checks, and
  `./scripts/task.sh check` pass.
- Two fresh `release:macos` executions then completed signing, notarization, stapling, DMG
  verification, and Gatekeeper assessment. Their app and helper designated requirements match, and
  their evidence is retained separately without credentials or checkout paths.
