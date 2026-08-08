# Developer ID Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local Apple Silicon DMG whose app, helpers, frameworks, and dylibs have stable explicit identities, pass package audits, are signed with Developer ID, notarized by Apple, stapled, and safe to install on another Mac.

**Architecture:** Repo-owned shell entry points assemble and audit a standard macOS bundle, sign every code item explicitly from the inside out, and orchestrate DMG creation/notarization with an atomic final publish. Go keeps runtime resource lookup independent from packaging, while fake Apple tools make bundle, signing, audit, and failure behavior testable without a certificate or network.

**Tech Stack:** Go 1.25, Wails v3 `v3.0.0-alpha2.119`, Bash 3.2-compatible scripts, Taskfile v3, Apple `security`/`codesign`/`otool`/`lipo`/`install_name_tool`/`hdiutil`/`spctl`, Xcode `notarytool`/`stapler`, and ShellCheck.

## Global Constraints

- Production bundle identifier stays exactly `com.jualopezmo.loquigo`; development uses exactly `com.jualopezmo.loquigo.dev`.
- Production helper identifiers are `com.jualopezmo.loquigo.{globe-listener,macos-stt,whisper-stt}`; development inserts `.dev` before the helper suffix.
- Developer ID is used only by `./scripts/task.sh release:macos`; daily run/dev uses Apple Development when exactly one valid identity exists and otherwise prints an explicit ad-hoc/TCC warning.
- An explicitly configured invalid `LOQUI_DEV_SIGN_IDENTITY` or `LOQUI_SIGN_IDENTITY` is an error; ambiguity is never guessed.
- Release is macOS `arm64` only. The already-universal Azure framework is allowed because it contains `arm64`; the app, helpers, and Whisper/ggml/SDL dylibs must be exactly `arm64`.
- Executable helpers live under `Contents/Helpers`, frameworks and dylibs under `Contents/Frameworks`, and the optional model under `Contents/Resources/models`. No Mach-O code may remain under Resources.
- Release artifacts contain no Homebrew, checkout, `/Users/`, `helpers/bin`, or `scripts/whisper-vendor` load/rpath dependency.
- Sign nested code explicitly from the inside out. Never use `codesign --deep` to sign; `codesign --verify --deep --strict` is allowed only as a read-only audit.
- Developer ID and Apple Development signatures use Hardened Runtime on main executables. The app
  receives Audio Input plus Apple Events entitlements; `macos-stt` and `whisper-stt` receive Audio
  Input only; `globe-listener`, dylibs, and frameworks receive no entitlement file. Ad-hoc signing
  deliberately uses no Hardened Runtime, timestamp, or entitlements.
- Assembly sanitizes every real packaged Mach-O load command/install name, preserves dylib symlink
  chains, clears extended attributes before signing, and re-audits afterwards.
- `VERSION` is exactly `info.version` from `build/config.yml`; the final image is `bin/release/Loqui-${VERSION}-macos-arm64.dmg`.
- Both app plists must contain that same version in `CFBundleShortVersionString` and
  `CFBundleVersion`; a mismatch fails preflight before any Apple service call.
- A release either publishes an accepted, stapled, verified DMG atomically or exits nonzero while preserving the last successful artifact.
- Certificate private keys, Apple IDs, app-specific passwords, and notarization credentials remain outside Git and outside release logs.
- Do not restore provider credentials to Keychain as part of this feature.
- Release leaves `LOQUI_BUNDLE_MODEL` unset by default. The standard distributable exercises the
  signed first-use model download/retry path; embedding the model remains an explicit opt-in only.
- Codex executes the plan inline per `shared/rules/execution.md`; do not dispatch implementation subagents from Codex.
- The repository ship gate overrides per-task commit defaults: record red/green evidence after each task, but make the implementation commit only after plan review, TDD, code review, E2E disposition, and state gates are green.
- Every shell test entry point resolves the repository root from its own location and defaults its injectable script variable (`BUNDLE_SCRIPT`, `AUDIT_SCRIPT`, `SIGN_SCRIPT`, or `RELEASE_SCRIPT`) to the corresponding repo script; mutation tests override only that variable.

## File map

- Modify `internal/app/paths.go`: separate bundled helper, bundled model, and development fallback resolution.
- Create `internal/app/paths_test.go`: deterministic bundle/development/data-directory path tests.
- Modify `build/darwin/Info.dev.plist`: use the `.dev` bundle identifier.
- Modify `scripts/patch-plists.sh`: restore the distinct production/dev identifiers after generated asset updates.
- Create `build/darwin/Loqui.entitlements`: Audio Input and Apple Events for the host app.
- Create `build/darwin/LoquiAudioHelper.entitlements`: Audio Input for the two microphone-owning helpers.
- Modify `scripts/build-whisper-stt.sh`: copy SDL, preserve dylib symlinks, and sanitize every helper/dylib load command and install name.
- Create `scripts/macos-bundle.sh`: assemble production and development bundles in standard locations.
- Create `scripts/macos-audit.sh`: reject invalid layout, architecture, symlink, and Mach-O dependency state.
- Create `scripts/macos-sign.sh`: resolve identities and sign apps/DMGs explicitly without `--deep` mutation.
- Create `scripts/release-macos.sh`: preflight, stage, build, audit, sign, create/notarize/staple/verify DMG, and publish atomically.
- Create `scripts/tests/testlib.sh`: isolated shell assertions, fixtures, command recording, and fake macOS tools.
- Create `scripts/tests/macos-bundle-test.sh`: bundle layout, model, symlink, plist, and relocation regressions.
- Create `scripts/tests/macos-audit-test.sh`: malformed package/architecture/dependency regressions.
- Create `scripts/tests/macos-sign-test.sh`: identity selection, signing order/options/identifiers, and fallback regressions.
- Create `scripts/tests/release-macos-test.sh`: phase ordering, notary result, log, failure, cleanup, and atomic publication regressions.
- Create `scripts/tests/macos-release-mutations.sh`: prove the critical signing/layout/dependency/publication guards are detected.
- Create `scripts/update-build-assets.sh` and `scripts/wails-build-assets.patch`: regenerate pinned
  Wails assets and deterministically restore Loqui's repo-owned Taskfile/plist customizations.
- Modify `scripts/task.sh`: install and require the same Wails CLI version pinned by `go.mod`.
- Modify `build/darwin/Taskfile.yml`: portable build flag, shared bundle assembly, explicit ad-hoc/development signing, and release task.
- Modify `build/Taskfile.yml`: route generated-asset updates through the regeneration-safe wrapper.
- Modify `Taskfile.yml`: expose `release:macos` and include shell tests in `check` on macOS.
- Modify `README.md`: one-time certificate/profile setup, signing channels, release command, and arm64 scope.
- Modify `docs/CHANGELOG.md`: record stable development identity and Developer ID/notarized DMG behavior after verification.
- Create `docs/e2e/use-cases/developer-id-release.md`: local and second-Mac journeys.
- Create `docs/e2e/reports/2026-08-07-developer-id-release.md`: exact evidence and honest verdict from the real run.
- Modify `CONTINUITY.md`: preserve any remaining second-Mac blocker or identify the next project priority.
- Update `.workflow/state.md`: phase, TDD evidence, plan/code reviews, E2E path, external blockers, and gates.

## Pre-implementation gate

Before Task 1 changes any production or test file, run the project `review` workflow against this
plan, resolve every P0/P1/P2 finding, record the review in `.workflow/state.md`, and commit any
review-driven plan correction. The planning artifact itself may be committed before that review, but
the standard profile's plan-review gate remains unchecked until review evidence exists.

---

### Task 1: Separate runtime helper and model locations

**Files:**
- Modify: `internal/app/paths.go:10-58`
- Create: `internal/app/paths_test.go`
- Modify: `build/darwin/Info.dev.plist:13-14`
- Modify: `scripts/patch-plists.sh:22-44`

**Interfaces:**
- Consumes: `os.Executable()`, `os.Getwd()`, the existing public `HelperPath(name string) string`, and `WhisperModelPath(dataDir string) string` callers.
- Produces: testable internal `helperPath(executablePath, workingDir, name string) string` and `whisperModelPath(executablePath, workingDir, dataDir string) string`; public signatures remain unchanged.

- [ ] **Step 1: Write failing path-resolution tests**

Create `internal/app/paths_test.go` with one-file fixtures and these exact cases:

```go
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func putPathFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHelperPathPrefersContentsHelpers(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "Loqui.app", "Contents", "MacOS", "loqui")
	want := filepath.Join(root, "Loqui.app", "Contents", "Helpers", "globe-listener")
	putPathFile(t, want)
	legacy := filepath.Join(root, "Loqui.app", "Contents", "Resources", "helpers", "globe-listener")
	putPathFile(t, legacy)

	if got := helperPath(exe, t.TempDir(), "globe-listener"); got != want {
		t.Fatalf("helperPath() = %q, want %q", got, want)
	}
}

func TestHelperPathUsesDevelopmentFallback(t *testing.T) {
	working := t.TempDir()
	want := filepath.Join(working, "helpers", "bin", "macos-stt")
	putPathFile(t, want)
	if got := helperPath("", working, "macos-stt"); got != want {
		t.Fatalf("helperPath() = %q, want %q", got, want)
	}
}

func TestHelperPathRejectsLegacyAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "Loqui.app", "Contents", "MacOS", "loqui")
	putPathFile(t, filepath.Join(root, "Loqui.app", "Contents", "Resources", "helpers", "whisper-stt"))
	if got := helperPath(exe, t.TempDir(), "whisper-stt"); got != "" {
		t.Fatalf("legacy helper resolved as %q", got)
	}
}

func TestWhisperModelPathUsesBundledResourcesThenDevelopmentThenData(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "Loqui.app", "Contents", "MacOS", "loqui")
	working := t.TempDir()
	data := t.TempDir()
	bundled := filepath.Join(root, "Loqui.app", "Contents", "Resources", "models", "ggml-small.bin")
	development := filepath.Join(working, "helpers", "bin", "ggml-small.bin")
	putPathFile(t, development)
	putPathFile(t, bundled)
	if got := whisperModelPath(exe, working, data); got != bundled {
		t.Fatalf("bundled model = %q, want %q", got, bundled)
	}
	if err := os.Remove(bundled); err != nil { t.Fatal(err) }
	if got := whisperModelPath(exe, working, data); got != development {
		t.Fatalf("development model = %q, want %q", got, development)
	}
	if err := os.Remove(development); err != nil { t.Fatal(err) }
	wantData := filepath.Join(data, "models", "ggml-small.bin")
	if got := whisperModelPath(exe, working, data); got != wantData {
		t.Fatalf("data model = %q, want %q", got, wantData)
	}
}
```

- [ ] **Step 2: Run the focused tests and confirm RED**

Run:

```bash
./scripts/go.sh test ./internal/app -run 'Test(HelperPath|WhisperModelPath)' -count=1
```

Expected: build failure because `helperPath` and `whisperModelPath` do not exist.

- [ ] **Step 3: Implement the minimal resolvers**

Refactor `internal/app/paths.go` around these exact helpers:

```go
func existingPath(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func helperPath(executablePath, workingDir, name string) string {
	if executablePath != "" {
		bundled := filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", "Helpers", name))
		if found := existingPath(bundled); found != "" {
			return found
		}
	}
	return existingPath(filepath.Join(workingDir, "helpers", "bin", name))
}

func HelperPath(name string) string {
	executablePath, _ := os.Executable()
	workingDir, _ := os.Getwd()
	return helperPath(executablePath, workingDir, name)
}

func whisperModelPath(executablePath, workingDir, dataDir string) string {
	if executablePath != "" {
		bundled := filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", "Resources", "models", "ggml-small.bin"))
		if found := existingPath(bundled); found != "" {
			return found
		}
	}
	if found := existingPath(filepath.Join(workingDir, "helpers", "bin", "ggml-small.bin")); found != "" {
		return found
	}
	return filepath.Join(dataDir, "models", "ggml-small.bin")
}

func WhisperModelPath(dataDir string) string {
	executablePath, _ := os.Executable()
	workingDir, _ := os.Getwd()
	return whisperModelPath(executablePath, workingDir, dataDir)
}
```

Update comments to name `Contents/Helpers` and `Contents/Resources/models`; remove every claim that bundled models are found through `HelperPath`.

- [ ] **Step 4: Make the dev identifier regeneration-safe**

Change `build/darwin/Info.dev.plist` to `com.jualopezmo.loquigo.dev`. In `scripts/patch-plists.sh`, define `PRODUCTION_ID` and `DEVELOPMENT_ID`, keep the shared usage-string loop, and then set the identifiers explicitly:

```bash
PRODUCTION_ID="com.jualopezmo.loquigo"
DEVELOPMENT_ID="com.jualopezmo.loquigo.dev"

set_string build/darwin/Info.plist CFBundleIdentifier "$PRODUCTION_ID"
set_string build/darwin/Info.dev.plist CFBundleIdentifier "$DEVELOPMENT_ID"
```

Add a shell assertion after `./scripts/patch-plists.sh`:

```bash
test "$(plutil -extract CFBundleIdentifier raw build/darwin/Info.plist)" = "com.jualopezmo.loquigo"
test "$(plutil -extract CFBundleIdentifier raw build/darwin/Info.dev.plist)" = "com.jualopezmo.loquigo.dev"
```

- [ ] **Step 5: Run the focused and neighboring Go tests**

Run:

```bash
./scripts/go.sh test ./internal/app -count=1
```

Expected: PASS, including model download/provider fallback tests that consume `WhisperModelPath`.

- [ ] **Step 6: Record the task checkpoint without committing**

Set `.workflow/state.md` phase to `tdd` and record the RED build error and GREEN command. Leave the implementation uncommitted until the standard gate is green.

---

### Task 2: Assemble a portable standard bundle

**Files:**
- Modify: `scripts/build-whisper-stt.sh:42-64`
- Create: `scripts/macos-bundle.sh`
- Create: `scripts/tests/testlib.sh`
- Create: `scripts/tests/macos-bundle-test.sh`

**Interfaces:**
- Consumes: `build/darwin/Info.plist`, `build/darwin/Info.dev.plist`, `helpers/bin`, `third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework`, and one already-built main executable.
- Produces: `scripts/macos-bundle.sh --channel production|development --executable PATH --output PATH [--root PATH]`; the output app follows the design layout and is not signed.

- [ ] **Step 1: Create the shell test harness**

Create `scripts/tests/testlib.sh` with Bash 3.2-compatible helpers; do not use associative arrays:

```bash
#!/usr/bin/env bash
set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }
assert_file() { [ -f "$1" ] || fail "missing file: $1"; }
assert_dir() { [ -d "$1" ] || fail "missing directory: $1"; }
assert_absent() { [ ! -e "$1" ] || fail "unexpected path: $1"; }
assert_eq() { [ "$1" = "$2" ] || fail "got '$1', want '$2'"; }
assert_contains() { grep -F -- "$2" "$1" >/dev/null || fail "$1 does not contain: $2"; }
assert_not_contains() { ! grep -F -- "$2" "$1" >/dev/null || fail "$1 contains: $2"; }

put_file() {
  mkdir -p "$(dirname "$1")"
  printf '%s\n' "${2:-fixture}" >"$1"
  chmod "${3:-755}" "$1"
}

run_expect_fail() {
  if "$@"; then fail "command unexpectedly passed: $*"; fi
}
```

- [ ] **Step 2: Write the failing bundle test**

Create `scripts/tests/macos-bundle-test.sh`. Its fixture root contains minimal production/dev
plists, an executable, three helper files, one real file plus the complete symlink chain for each
required Whisper/ggml family, SDL, a model, icons, and a fake framework. Put recording
`install_name_tool` and fixture-aware `otool` commands first on `PATH`; set
`tool_log="$tmp/tool.log"`, export it as `TOOL_LOG`, and make the mutation fake append
`printf '%s\n' "$*" >>"$TOOL_LOG"`. Assert:

```bash
"$BUNDLE_SCRIPT" --channel development --root "$fixture" \
  --executable "$fixture/bin/loqui" --output "$out/Loqui.dev.app"

assert_file "$out/Loqui.dev.app/Contents/MacOS/loqui"
assert_file "$out/Loqui.dev.app/Contents/Helpers/globe-listener"
assert_file "$out/Loqui.dev.app/Contents/Helpers/macos-stt"
assert_file "$out/Loqui.dev.app/Contents/Helpers/whisper-stt"
assert_file "$out/Loqui.dev.app/Contents/Frameworks/libwhisper.1.9.1.dylib"
[ -L "$out/Loqui.dev.app/Contents/Frameworks/libwhisper.dylib" ] || fail "dylib symlink flattened"
assert_absent "$out/Loqui.dev.app/Contents/Resources/helpers"
assert_absent "$out/Loqui.dev.app/Contents/Resources/models/ggml-small.bin"
assert_eq "$(plutil -extract CFBundleIdentifier raw "$out/Loqui.dev.app/Contents/Info.plist")" \
  "com.jualopezmo.loquigo.dev"
assert_contains "$tool_log" "-change /opt/homebrew/opt/sdl2/lib/libSDL2-2.0.0.dylib @rpath/libSDL2-2.0.0.dylib"
assert_contains "$tool_log" "-add_rpath @loader_path/../Frameworks"
assert_contains "$tool_log" "-id @rpath/libSDL2-2.0.0.dylib"
assert_contains "$tool_log" "-delete_rpath /fixture/checkout/build/bin"
assert_contains "$tool_log" "-add_rpath @loader_path"

LOQUI_BUNDLE_MODEL=1 "$BUNDLE_SCRIPT" --channel production --root "$fixture" \
  --executable "$fixture/bin/loqui" --output "$out/Loqui.app"
assert_file "$out/Loqui.app/Contents/Resources/models/ggml-small.bin"
[ ! -L "$out/Loqui.app/Contents/Resources/models/ggml-small.bin" ] || fail "model escaped bundle through symlink"
```

Set one fixture extended attribute before assembly and assert the output bundle has none afterwards.
Also call the script with an unknown channel, missing executable, missing plist, missing framework,
and a missing/broken member in each required dylib symlink chain; each must fail with a diagnostic
that names the missing input.

- [ ] **Step 3: Run the bundle test and confirm RED**

Run:

```bash
chmod +x scripts/macos-bundle.sh scripts/tests/macos-bundle-test.sh
BUNDLE_SCRIPT=./scripts/macos-bundle.sh ./scripts/tests/macos-bundle-test.sh
```

Expected: FAIL because `scripts/macos-bundle.sh` does not exist.

- [ ] **Step 4: Make Whisper's build output portable**

In `scripts/build-whisper-stt.sh`:

1. Derive `SDL_PREFIX="$(sdl2-config --prefix)"` and
   `SDL_DYLIB="$SDL_PREFIX/lib/libSDL2-2.0.0.dylib"`; fail if the file is absent.
2. Copy Whisper/ggml dylibs with `cp -a` so upstream symlinks remain symlinks. Enumerate real files
   with `find ... -type f -name '*.dylib' -print | LC_ALL=C sort`; never mutate through each symlink.
3. Copy SDL to `helpers/bin/libSDL2-2.0.0.dylib` and change its ID from the Homebrew path to
   `@rpath/libSDL2-2.0.0.dylib`.
4. Replace the helper's absolute SDL load command with `@rpath/libSDL2-2.0.0.dylib`.
5. For `whisper-stt` and every real Whisper/ggml dylib, delete every existing `LC_RPATH`. Give the
   helper exactly `@loader_path`; give each real dylib exactly `@loader_path`. Keep upstream
   `@rpath` `LC_ID_DYLIB` values, and reject any remaining absolute ID/dependency/rpath.
6. Assert `libggml-metal` contains `__DATA,__ggml_metallib`; the current build embeds Metal and no
   external `.metallib` is copied.

The mutation block is:

```bash
SDL_PREFIX="$(sdl2-config --prefix)"
SDL_DYLIB="$SDL_PREFIX/lib/libSDL2-2.0.0.dylib"
[ -f "$SDL_DYLIB" ] || { echo "build-whisper-stt: missing $SDL_DYLIB" >&2; exit 1; }
cp -a "$VENDOR"/build/bin/*.dylib "$ROOT/helpers/bin/"
cp -L "$SDL_DYLIB" "$ROOT/helpers/bin/libSDL2-2.0.0.dylib"
install_name_tool -change "$SDL_DYLIB" '@rpath/libSDL2-2.0.0.dylib' "$ROOT/helpers/bin/whisper-stt"
install_name_tool -id '@rpath/libSDL2-2.0.0.dylib' "$ROOT/helpers/bin/libSDL2-2.0.0.dylib"
```

Implement the shared rpath-sanitizing helper once in the script and invoke it for the helper and
each real dylib. Add a final `otool -L/-D/-l` check so build output itself is portable before bundle
assembly.

- [ ] **Step 5: Implement the bundle assembler**

Create `scripts/macos-bundle.sh` with `set -euo pipefail`, explicit option parsing, and a root that defaults to the absolute result of resolving the script's parent directory. It must:

1. Resolve channel to exactly one plist and expected bundle ID.
2. Require an output basename ending in `.app`; remove only that exact resolved output after rejecting empty, `/`, repo root, `bin`, and `$HOME` values.
3. Create `Contents/{MacOS,Helpers,Frameworks,Resources}`.
4. Copy the main executable, selected plist, icon, optional `Assets.car`, and Azure framework (`ditto` preserves framework symlinks).
5. Copy only the named executable helpers into Helpers.
6. Copy the complete, explicitly required `libwhisper`, `libggml`, `libggml-base`, `libggml-cpu`,
   `libggml-blas`, and `libggml-metal` real-file/symlink families plus SDL with `cp -a` into
   Frameworks. Enumerate with `LC_ALL=C sort` and reject missing or broken chains.
7. Copy the model with `cp -L` only when `LOQUI_BUNDLE_MODEL=1`.
8. On the packaged Whisper copy, delete every existing `LC_RPATH`, add
   `@loader_path/../Frameworks`, and change any SDL `/opt/homebrew/...` reference to
   `@rpath/libSDL2-2.0.0.dylib`.
9. On every real packaged dylib, delete every existing `LC_RPATH`, add exactly `@loader_path`,
   require an `@rpath/...` ID, and reject any non-system dependency that is not `@rpath`,
   `@loader_path`, or `@executable_path`. Set SDL's ID explicitly to its `@rpath` name.
10. Run `xattr -cr "$output"`, then confirm `xattr -lr "$output"` is empty. This happens before
    either unsigned audit or signing.
11. Print the output app path and nothing resembling a successful release claim.

Use a `while read -r old_rpath` loop over `otool -l`; do not pipe into the loop because Bash 3.2 would run it in a subshell.

- [ ] **Step 6: Run bundle tests and ShellCheck**

Run:

```bash
BUNDLE_SCRIPT=./scripts/macos-bundle.sh ./scripts/tests/macos-bundle-test.sh
shellcheck -s bash scripts/macos-bundle.sh scripts/build-whisper-stt.sh scripts/tests/testlib.sh scripts/tests/macos-bundle-test.sh
```

Expected: both commands PASS.

- [ ] **Step 7: Record the task checkpoint without committing**

Record bundle test RED/GREEN and ShellCheck evidence in `.workflow/state.md`.

---

### Task 3: Reject malformed or machine-bound app bundles

**Files:**
- Create: `scripts/macos-audit.sh`
- Create: `scripts/tests/macos-audit-test.sh`

**Interfaces:**
- Consumes: one assembled `.app` and standard Apple inspection commands from `PATH`.
- Produces: `scripts/macos-audit.sh APP_PATH`; exits zero only when layout, required code, symlinks, architectures, load commands, and rpaths are distributable.

- [ ] **Step 1: Write the failing audit tests with fake inspection tools**

Create fake `file`, `lipo`, and `otool` dispatchers under a temporary `fake-bin`; behavior is
selected by fixture filename (`bad-x86`, `bad-homebrew`, `bad-id`, `bad-rpath`, `bad-checkout`,
`missing-metal`, `good`). Build a minimal app fixture with all three helpers, Azure framework, SDL,
the exact required Whisper/ggml real-file and symlink families, and no code under Resources.

The test cases are exact:

```bash
PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"

put_file "$good_app/Contents/Resources/hidden-mach-o"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Resources/hidden-mach-o"

touch "$good_app/Contents/Frameworks/bad-x86.dylib"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Frameworks/bad-x86.dylib"

ln -s missing.dylib "$good_app/Contents/Frameworks/broken.dylib"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Frameworks/broken.dylib"

touch "$good_app/Contents/Helpers/bad-homebrew"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Helpers/bad-homebrew"

touch "$good_app/Contents/Helpers/bad-checkout"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Helpers/bad-checkout"

touch "$good_app/Contents/Frameworks/bad-id.dylib"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Frameworks/bad-id.dylib"

touch "$good_app/Contents/Frameworks/bad-rpath.dylib"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
rm "$good_app/Contents/Frameworks/bad-rpath.dylib"

mv "$good_app/Contents/Frameworks/libggml-metal.0.16.0.dylib" \
  "$good_app/Contents/Frameworks/missing-metal.dylib"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
mv "$good_app/Contents/Frameworks/missing-metal.dylib" \
  "$good_app/Contents/Frameworks/libggml-metal.0.16.0.dylib"

mv "$good_app/Contents/Helpers/macos-stt" "$good_app/Contents/Helpers/macos-stt.absent"
run_expect_fail env PATH="$fake_bin:$PATH" "$AUDIT_SCRIPT" "$good_app"
mv "$good_app/Contents/Helpers/macos-stt.absent" "$good_app/Contents/Helpers/macos-stt"
```

The fake `file` reports each executable/dylib/framework executable as Mach-O and `hidden-mach-o` as
Mach-O. Fake `lipo -archs` reports `x86_64` only for `bad-x86`, `arm64 x86_64` for the Azure
framework executable, and `arm64` otherwise. Fake `otool -L/-l/-D` emits `/opt/homebrew/...` for
`bad-homebrew`, an absolute ID for `bad-id`, a checkout `LC_RPATH` for `bad-rpath`/`bad-checkout`,
omits `__ggml_metallib` for `missing-metal`, and emits only the allowed portable forms for good
files.

- [ ] **Step 2: Run the audit test and confirm RED**

Run:

```bash
chmod +x scripts/macos-audit.sh scripts/tests/macos-audit-test.sh
AUDIT_SCRIPT=./scripts/macos-audit.sh ./scripts/tests/macos-audit-test.sh
```

Expected: FAIL because `scripts/macos-audit.sh` does not exist.

- [ ] **Step 3: Implement exact package guards**

Create `scripts/macos-audit.sh` and enforce these checks in order:

1. App, plist, main executable, all three helpers, Azure framework executable, SDL, and exactly one
   valid real-file/symlink chain for each required family (`libwhisper`, `libggml`, `libggml-base`,
   `libggml-cpu`, `libggml-blas`, `libggml-metal`) exist. Enumerate with `LC_ALL=C sort`.
2. `find "$app" -type l ! -exec test -e {} \; -print` yields no broken symlink.
3. Every file under Resources reports non-Mach-O.
4. Every discovered Mach-O contains `arm64`; main/helper/Whisper/ggml/SDL entries reject additional `x86_64`, while the Azure framework may contain both.
5. `otool -L`, `otool -D`, and every `LC_RPATH` reject prefixes `/opt/homebrew`, `/usr/local`,
   `/Users`, repo root, `helpers/bin`, and `scripts/whisper-vendor`. Every dylib ID must begin
   `@rpath/`; every discovered non-system dependency must begin with `@rpath`, `@loader_path`, or
   `@executable_path`.
6. Every packaged real dylib has only the intentional portable rpath (`@loader_path`); the Whisper
   helper has only `@loader_path/../Frameworks`; the main executable has only the portable framework
   rpath. No other package-owned Mach-O may contain an absolute non-system rpath.
7. The real `libggml-metal` target contains section `__DATA,__ggml_metallib`; no external Metal
   source or `.metallib` appears under Resources.
8. `xattr -lr "$app"` is empty.

Each failure prints the offending bundle-relative path and value. Print `macos-audit: ok $app` only after all checks pass.

- [ ] **Step 4: Run audit tests and ShellCheck**

Run:

```bash
AUDIT_SCRIPT=./scripts/macos-audit.sh ./scripts/tests/macos-audit-test.sh
shellcheck -s bash scripts/macos-audit.sh scripts/tests/macos-audit-test.sh
```

Expected: PASS.

- [ ] **Step 5: Record the task checkpoint without committing**

Record malformed-fixture RED/GREEN evidence in `.workflow/state.md`.

---

### Task 4: Resolve stable identities and sign inside out

**Files:**
- Create: `build/darwin/Loqui.entitlements`
- Create: `build/darwin/LoquiAudioHelper.entitlements`
- Create: `scripts/macos-sign.sh`
- Create: `scripts/tests/macos-sign-test.sh`

**Interfaces:**
- Consumes: valid identities reported by `security find-identity -v -p codesigning`, an audited app, and optional `LOQUI_DEV_SIGN_IDENTITY`/`LOQUI_SIGN_IDENTITY`.
- Produces: `macos-sign.sh resolve --channel development|release`; `macos-sign.sh app --channel adhoc|development|release --app PATH [--identity VALUE]`; `macos-sign.sh dmg --dmg PATH --identity VALUE`.

- [ ] **Step 1: Write failing identity-selection tests**

Use a fake `security` whose output is controlled by a fixture file. Assert:

- zero Apple Development identities resolves to `-` and stderr contains `TCC continuity is unavailable`;
- one Apple Development identity resolves to its SHA-1 hash;
- two Apple Development identities fail unless `LOQUI_DEV_SIGN_IDENTITY` exactly matches one SHA-1 or full quoted name;
- zero or two Developer ID Application identities fail unless `LOQUI_SIGN_IDENTITY` matches exactly one;
- duplicate `security` rows with the same SHA-1 are deduplicated before ambiguity is evaluated;
- an explicitly configured unknown value always fails and never falls back.

Use fixed fake identities so expected output is literal:

```text
AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA Apple Development: Juan Motta (TEAM123456)
BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB Developer ID Application: Juan Motta (TEAM123456)
```

- [ ] **Step 2: Write the failing signing-order test**

Use a fake `codesign` that appends every argument vector to `$TOOL_LOG`. Give the fixture exactly
three real raw dylibs and run release app signing with identity
`BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB`. Normalize the signed path basenames into a file and
`diff -u` it against this exact order:

```text
libSDL2-2.0.0.dylib
libggml.0.16.0.dylib
libwhisper.1.9.1.dylib
MicrosoftCognitiveServicesSpeech.framework
globe-listener
macos-stt
whisper-stt
Loqui.app
```

For each helper, assert `--identifier` uses its exact production suffix. Assert the framework signing
target is the `.framework` bundle root (Apple's documented code-item boundary), while the auditor
separately resolves its `Versions/A/...` executable. Assert release/development signing applies:

- app: `--options runtime --timestamp --entitlements build/darwin/Loqui.entitlements`;
- `macos-stt` and `whisper-stt`: `--options runtime --timestamp --entitlements build/darwin/LoquiAudioHelper.entitlements`;
- `globe-listener`: `--options runtime --timestamp` and no entitlement file;
- libraries/framework: `--timestamp`, no `--options runtime`, and no entitlement file.

Run development signing and assert the `.dev` helper identifiers and the same narrow entitlement
assignment. Run ad-hoc signing and assert `--sign -` plus channel identifiers, with no `--timestamp`,
`--options runtime`, or `--entitlements`. Across the script, signing calls contain zero `--deep`;
the one read-only final verification call contains it exactly once.

- [ ] **Step 3: Run signing tests and confirm RED**

Run:

```bash
chmod +x scripts/macos-sign.sh scripts/tests/macos-sign-test.sh
SIGN_SCRIPT=./scripts/macos-sign.sh ./scripts/tests/macos-sign-test.sh
```

Expected: FAIL because `scripts/macos-sign.sh` does not exist.

- [ ] **Step 4: Implement identity parsing and validation**

Create `scripts/macos-sign.sh` with a function that converts `security` output to tab-separated SHA/name records and matches only these prefixes:

```bash
identity_records() {
  security find-identity -v -p codesigning |
    sed -n 's/^[[:space:]]*[0-9][0-9]*) \([0-9A-Fa-f][0-9A-Fa-f]*\) "\(.*\)"$/\1\t\2/p'
}
```

Pipe records through `LC_ALL=C sort -u -k1,1` so repeated Keychain rows with the same SHA-1 count
once. Normalize all successful selection to the SHA-1 hash. `resolve development` uses
`LOQUI_DEV_SIGN_IDENTITY`; `resolve release` uses `LOQUI_SIGN_IDENTITY`. Exact SHA or exact full
certificate name is accepted; substring matching is forbidden.

- [ ] **Step 5: Create and validate the minimal entitlement files**

Create standard XML plists with LF endings and no comments/BOM:

`build/darwin/Loqui.entitlements` contains only Boolean-true
`com.apple.security.device.audio-input` and `com.apple.security.automation.apple-events`.
`build/darwin/LoquiAudioHelper.entitlements` contains only Boolean-true
`com.apple.security.device.audio-input`. Validate both with `plutil -lint`, and add tests that extract
the exact keys and reject any extra entitlement. Do not add an undocumented Speech entitlement.

- [ ] **Step 6: Implement explicit signing order**

Determine the base identifier from the channel, not from a caller-provided arbitrary string. Sign:

1. Each real raw dylib file in stable sorted order.
2. `MicrosoftCognitiveServicesSpeech.framework`.
3. Helpers in the literal order `globe-listener`, `macos-stt`, `whisper-stt`, each with exact explicit identifier.
4. The top-level app.

Use arrays for argument construction. Release library/framework args are
`--force --timestamp --sign "$identity"`; release/development executables add `--options runtime`
and only the entitlement file assigned above. Development uses the selected Apple Development
identity. Ad-hoc uses `--force --sign -` and explicit identifiers with no Hardened Runtime,
timestamp, or entitlement file. Use `LC_ALL=C sort` for every filesystem-derived code-item list.
After app signing run:

```bash
codesign --verify --deep --strict --verbose=2 "$app"
```

That is the only allowed `--deep` occurrence in the script. DMG signing uses
`--force --timestamp --sign "$identity"` and no entitlements.

- [ ] **Step 7: Run signing tests, syntax checks, and ShellCheck**

Run:

```bash
SIGN_SCRIPT=./scripts/macos-sign.sh ./scripts/tests/macos-sign-test.sh
bash -n scripts/macos-sign.sh scripts/tests/macos-sign-test.sh
shellcheck -s bash scripts/macos-sign.sh scripts/tests/macos-sign-test.sh
plutil -lint build/darwin/Loqui.entitlements build/darwin/LoquiAudioHelper.entitlements
```

Expected: PASS.

- [ ] **Step 8: Record the task checkpoint without committing**

Record identity ambiguity, fallback, exact identifier, and inside-out GREEN evidence in `.workflow/state.md`.

---

### Task 5: Orchestrate notarization and atomic DMG publication

**Files:**
- Create: `scripts/release-macos.sh`
- Create: `scripts/tests/release-macos-test.sh`

**Interfaces:**
- Consumes: Tasks 2–4 scripts, `build/config.yml`, a Developer ID identity, `LOQUI_NOTARY_PROFILE` (default `loqui-notary`), and native Apple release tools.
- Produces: `scripts/release-macos.sh`; successful output is one accepted/stapled DMG plus evidence under `bin/release/evidence/${VERSION}/${SUBMISSION_ID}/`.

- [ ] **Step 1: Write failing phase-order and preflight tests**

Make `scripts/release-macos.sh` sourceable (`main` runs only when `BASH_SOURCE[0] == $0`). In the test, source it and override phase functions to append names to a log. Assert exact order:

```text
preflight
build
bundle
audit-unsigned
sign-app
verify-app
create-dmg
sign-dmg
verify-dmg
submit
fetch-log
check-log
staple
verify-staple
gatekeeper
publish
```

Separately use fake commands to assert preflight rejects non-arm64 host, missing Apple tool, invalid/ambiguous identity, invalid profile history, missing required helper/framework, and a version that is absent or not `MAJOR.MINOR.PATCH`.

Define `verify-app` narrowly: it performs local `codesign --verify --deep --strict`, then inspects the
app and each helper for the expected Developer ID authority, secure timestamp, Hardened Runtime flag,
identifier, and exact entitlement set. It does not run `spctl` before notarization; Gatekeeper is
reserved for the final stapled DMG.

- [ ] **Step 2: Write failing notary/failure/publication tests**

Cover these exact postconditions:

- `status=Invalid` fetches and copies the submission/log to a unique
  `${TMPDIR:-/tmp}/loqui-notary-failure.${SUBMISSION_ID}.*` directory, prints both the submission ID
  and preserved log path, exits nonzero, and never staples/publishes;
- malformed submit JSON or a missing/empty `.id` preserves the raw response, prints an actionable
  diagnostic, and never invokes `notarytool log` with an empty ID;
- accepted submission with any `.issues[] | select(.severity == "error")` fails before staple;
- accepted submission with `issues: null`, `issues: []`, or warning-only issues proceeds and
  preserves those warnings in evidence;
- accepted submission with missing/null `ticketContents`, or whose suffix-normalized paths omit any
  expected code item from the signed manifest (main executable, all three helpers, Azure framework
  executable, SDL, and every real Whisper/ggml dylib), fails before staple;
- ticket paths such as `Loqui.dmg/Loqui.app/Contents/MacOS/loqui` match the expected anchored suffix,
  while `OtherLoqui.app/Contents/MacOS/loqui` does not;
- staple, `hdiutil verify`, `codesign --verify`, or `spctl --assess --type open --context context:primary-signature` failure propagates nonzero;
- a previous final DMG containing `old accepted artifact` remains byte-identical when any earlier phase fails;
- successful publication first copies to a hidden temporary file inside `bin/release`, then renames it to `Loqui-0.1.0-macos-arm64.dmg`; no partial final name becomes visible;
- cleanup removes only the unique staging directory and hidden candidate file, never `bin/release`, the repository root, or the prior final DMG.

Use fixed JSON fixtures:

```json
{"id":"11111111-1111-1111-1111-111111111111","status":"Accepted"}
```

and:

```json
{
  "jobId":"11111111-1111-1111-1111-111111111111",
  "status":"Accepted",
  "issues":null,
  "ticketContents":[
    {"path":"Loqui.dmg/Loqui.app/Contents/MacOS/loqui"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Helpers/globe-listener"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Helpers/macos-stt"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Helpers/whisper-stt"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Frameworks/libSDL2-2.0.0.dylib"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Frameworks/libwhisper.1.9.1.dylib"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Frameworks/libggml.0.16.0.dylib"},
    {"path":"Loqui.dmg/Loqui.app/Contents/Frameworks/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech"}
  ]
}
```

The focused fixture's signed manifest contains only the displayed real dylibs; production derives
the manifest from the auditor's stable `LC_ALL=C` Mach-O inventory, so every other real ggml-family
target is required without hard-coding a stale upstream patch version.

- [ ] **Step 3: Run release tests and confirm RED**

Run:

```bash
chmod +x scripts/release-macos.sh scripts/tests/release-macos-test.sh
RELEASE_SCRIPT=./scripts/release-macos.sh ./scripts/tests/release-macos-test.sh
```

Expected: FAIL because `scripts/release-macos.sh` does not exist.

- [ ] **Step 4: Implement safe preflight and staging**

Create `scripts/release-macos.sh` with functions matching the phase names from Step 1. Preflight must:

- require `uname -m` equals `arm64`;
- require `security`, `codesign`, `otool`, `lipo`, `install_name_tool`, `hdiutil`, `spctl`, `ditto`, `plutil`, `jq`, `xcrun`, `wails3`, and the repo scripts;
- capture `wails3 version 2>&1`, trim its single trailing newline/optional CR, and require the
  remaining single line to equal exactly `v3.0.0-alpha2.119` (this pinned CLI writes the version to
  stderr, not stdout);
- resolve Developer ID before building;
- validate the default or selected profile with `xcrun notarytool history --keychain-profile "$profile" --output-format json`;
- read version with an anchored `awk` expression from the `info:` block and enforce `^[0-9]+\.[0-9]+\.[0-9]+$`;
- require both `build/darwin/Info.plist` and `Info.dev.plist` to have that exact value for
  `CFBundleShortVersionString` and `CFBundleVersion` before building;
- create staging with `mktemp -d "${TMPDIR:-/tmp}/loqui-release.XXXXXX"` and record the exact path for a guarded trap.

Use this exact version extraction so an unrelated commented `version:` cannot win:

```bash
version="$(awk '/^info:/{in_info=1; next} in_info && /^  version:/{gsub(/["'\'' ]/, "", $2); print $2; exit}' build/config.yml)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "release-macos: invalid info.version: $version" >&2
  return 1
}
```

Normalize `${TMPDIR:-/tmp}` by removing trailing slashes before constructing the template. The trap
accepts cleanup only when the path matches that normalized prefix plus `loqui-release.` and six
generated characters and is neither empty nor `/`; otherwise it prints a refusal and leaves the path
untouched. Use task-specific lowercase local names; do not shadow shell/system variables.

- [ ] **Step 5: Implement build through DMG signing**

The build phase runs the portable arm64 build into staging:

```bash
./scripts/task.sh darwin:build ARCH=arm64 PORTABLE=true OUTPUT="$stage/loqui"
```

Then call the bundle with `LOQUI_BUNDLE_MODEL` deliberately unset and confirm the Wails build ran in
its default production mode (production Go tags and frontend, never `DEV=true`). Run unsigned audit, release app
signing, and signed verification. Create DMG-root staging with `ditto` rather than `cp`, add the
`/Applications` symlink, and re-run both the bundle auditor and `codesign --verify --deep --strict`
against the copied app before image creation:

```bash
hdiutil create -volname Loqui -srcfolder "$stage/dmg-root" -ov -format UDZO "$stage/Loqui.dmg"
```

The DMG root contains `Loqui.app` and a symlink exactly `/Applications`. Sign through `macos-sign.sh dmg`; verify with `hdiutil verify` before upload.

- [ ] **Step 6: Implement notary submission, evidence, and ticket verification**

Use only the Keychain profile. Capture the submit exit code without allowing `set -e` to bypass log
retrieval on a rejected submission:

```bash
set +e
xcrun notarytool submit "$stage/Loqui.dmg" \
  --keychain-profile "$profile" --wait --timeout 30m \
  --output-format json >"$stage/notary-submit.json"
submit_rc=$?
set -e
submission_id="$(jq -er '.id | select(type == "string" and length > 0)' \
  "$stage/notary-submit.json" 2>/dev/null || true)"
status="$(jq -er '.status | select(type == "string" and length > 0)' \
  "$stage/notary-submit.json" 2>/dev/null || true)"
[ -n "$submission_id" ] || preserve_submit_and_fail "missing submission id"
xcrun notarytool log "$submission_id" "$stage/notary-log.json" \
  --keychain-profile "$profile"
```

Fetch the log whenever a submission ID exists, even when `submit_rc` is nonzero or status is not
Accepted. On rejection, timeout, malformed output, or log-validation failure, copy only the available
`notary-submit.json` and `notary-log.json` to the guarded failure directory before normal staging
cleanup. On acceptance, require `submit_rc == 0`, `status == Accepted`, a valid log whose status is
also Accepted, zero `severity == "error"` issues (null/empty/warnings are allowed and retained), and
anchored-suffix `ticketContents[].path` coverage for every entry in the signed Mach-O manifest.
Missing/null `ticketContents` is a hard evidence failure. Assemble submission JSON, notary log,
signature metadata, DRs,
architecture/dependency audit, and checksums under staging. Confirm staged evidence contains no
environment dump or secret field. Generate report dates from the actual execution date rather than a
hard-coded planning date.

Staple/verify with:

```bash
xcrun stapler staple "$stage/Loqui.dmg"
xcrun stapler validate "$stage/Loqui.dmg"
hdiutil verify "$stage/Loqui.dmg"
codesign --verify --verbose=2 "$stage/Loqui.dmg"
spctl --assess --type open --context context:primary-signature --verbose=2 "$stage/Loqui.dmg"
```

Per Apple's outermost-container guidance, notarize and staple the DMG only; do not add a second app
submission/staple. Assert the script contains no `stapler staple` call targeting `.app`. The DMG's
accepted ticket must cover the nested app/helpers, and opening the stapled outer container makes that
ticket available for later nested-code checks, including first launch without network.

- [ ] **Step 7: Implement same-filesystem atomic publication**

Create `bin/release` only after every verification passes. Copy the accepted DMG to a uniquely named
hidden candidate in that directory and require `cp` to exit zero. Copy evidence to a hidden sibling,
then atomically rename it to the new, never-reused
`evidence/${VERSION}/${SUBMISSION_ID}` directory. Finally rename the hidden DMG candidate over the
final path with `mv -f`. If that final rename fails, remove only the new submission-ID evidence
directory. The failure trap removes only its exact hidden candidates. This keeps the old final DMG
and every older evidence directory intact until the same-filesystem final rename.

- [ ] **Step 8: Run release tests and static checks**

Run:

```bash
RELEASE_SCRIPT=./scripts/release-macos.sh ./scripts/tests/release-macos-test.sh
bash -n scripts/release-macos.sh scripts/tests/release-macos-test.sh
shellcheck -s bash scripts/release-macos.sh scripts/tests/release-macos-test.sh
```

Expected: PASS without a real certificate or network because external phases are overridden/faked.

- [ ] **Step 9: Record the task checkpoint without committing**

Record rejected-notary, missing-ticket-content, failure propagation, old-artifact preservation, and successful atomic-publication evidence in `.workflow/state.md`.

---

### Task 6: Wire daily signing, release tasks, and mutation coverage

**Files:**
- Modify: `scripts/task.sh:22-28`
- Modify: `build/Taskfile.yml:254-258`
- Modify: `build/darwin/Taskfile.yml:46-71,133-249`
- Modify: `Taskfile.yml:18-70`
- Create: `scripts/update-build-assets.sh`
- Create: `scripts/wails-build-assets.patch`
- Create: `scripts/tests/macos-release-mutations.sh`

**Interfaces:**
- Consumes: Tasks 2–5 command interfaces.
- Produces: ordinary `package`, stable-signed `dev/run`, `test:macos-release`, and the only public release entry point `./scripts/task.sh release:macos`.

- [ ] **Step 1: Add a failing Taskfile contract test**

Extend `scripts/tests/release-macos-test.sh` with static contract assertions:

```bash
assert_contains Taskfile.yml "release:macos:"
assert_contains Taskfile.yml "test:macos-release:"
assert_contains build/darwin/Taskfile.yml "./scripts/macos-bundle.sh"
assert_contains build/darwin/Taskfile.yml "./scripts/macos-sign.sh app --channel development"
assert_contains build/darwin/Taskfile.yml "./scripts/release-macos.sh"
assert_contains build/darwin/Taskfile.yml "DEV: '{{.DEV}}'"
assert_contains build/darwin/Taskfile.yml "OUTPUT: '{{.OUTPUT}}'"
assert_not_contains build/darwin/Taskfile.yml "wails3 tool sign"
assert_contains scripts/task.sh 'WAILS_VERSION="v3.0.0-alpha2.119"'
assert_contains scripts/task.sh 'github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}'
assert_contains build/Taskfile.yml "./scripts/update-build-assets.sh"
```

Run the focused script and confirm RED because the tasks still delegate to Wails/deep signing and
the wrapper still installs an unpinned `latest` CLI. `DEV` and `OUTPUT` already propagate through
the current generated `build` task; preserve those verified interfaces rather than adding duplicate
variables.

- [ ] **Step 2: Pin the Wails build CLI**

Set `WAILS_VERSION="v3.0.0-alpha2.119"` in `scripts/task.sh`. Capture
`wails3 version 2>&1` because this CLI prints its exact one-line version to stderr. If `wails3` is
absent or the normalized single line differs, install exactly
`github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}` and verify the installed version before
executing the task. Never use `@latest` in the release/build wrapper.

- [ ] **Step 3: Make production app builds portable**

In `build:native`, parameterize only the second rpath:

```yaml
CGO_LDFLAGS: >-
  -mmacosx-version-min=12.0
  -F{{.ROOT_DIR}}/third_party/speech-sdk
  -framework MicrosoftCognitiveServicesSpeech
  -Wl,-rpath,@executable_path/../Frameworks
  {{if ne .PORTABLE "true"}}-Wl,-rpath,{{.ROOT_DIR}}/third_party/speech-sdk{{end}}
```

Propagate `PORTABLE` from `build` to `build:native`. `package` requests `PORTABLE=true`; bare development builds retain the checkout rpath.

- [ ] **Step 4: Make generated build-asset updates regeneration-safe**

Create `scripts/update-build-assets.sh`. It resolves the repo root, invokes the already-pinned
`wails3 update build-assets ...` command, applies committed `scripts/wails-build-assets.patch` with
`git apply --check` then `git apply`, runs `scripts/patch-plists.sh`, and executes the same static
Taskfile/plist contracts as the focused release test. The patch restores all Loqui-owned changes to
`build/Taskfile.yml` and `build/darwin/Taskfile.yml`, including routing the generated
`common:update:build-assets` task back through this wrapper. It is pinned to the exact Wails version,
so patch drift fails loudly instead of silently losing release behavior.

Test the wrapper against copied pristine generated fixtures in a temporary Git repository; assert a
first regeneration applies the patch, a second run is idempotent, and a deliberately drifted anchor
fails. Do not run a destructive regeneration against the working tree in the focused unit test.

- [ ] **Step 5: Replace inline bundle copying and deep signing**

Refactor `create:app:bundle` to call:

```yaml
- ./scripts/macos-bundle.sh --channel production --executable "{{.BIN_DIR}}/{{.APP_NAME}}" --output "{{.BIN_DIR}}/{{.APP_NAME}}.app"
- ./scripts/macos-sign.sh app --channel adhoc --app "{{.BIN_DIR}}/{{.APP_NAME}}.app"
```

Refactor `run` to assemble `loqui.dev.app`, call development signing (which handles the explicit ad-hoc warning fallback), then launch it. Delete the old `codesign:adhoc`, Wails `sign`, and Wails `sign:notarize` tasks so no documented path can invoke `--deep` signing.

- [ ] **Step 6: Add top-level release and shell-test tasks**

Add `darwin:release` calling only `./scripts/release-macos.sh`. Add these top-level tasks:

```yaml
release:macos:
  summary: Builds, signs, notarizes, staples, and verifies an arm64 Developer ID DMG
  cmds:
    - task: darwin:release

test:macos-release:
  summary: Runs macOS bundle, audit, signing, release, and mutation tests
  platforms: [darwin]
  cmds:
    - ./scripts/tests/macos-bundle-test.sh
    - ./scripts/tests/macos-audit-test.sh
    - ./scripts/tests/macos-sign-test.sh
    - ./scripts/tests/release-macos-test.sh
    - ./scripts/tests/macos-release-mutations.sh
```

Call `test:macos-release` from `check` on Darwin. Do not make ordinary `check` contact Apple or require any Keychain identity.

- [ ] **Step 7: Implement mutation checks against temporary script copies**

Create `scripts/tests/macos-release-mutations.sh`. The production scripts mark each critical line
with one unique trailing comment (`# guard: helper-location`, `# guard: forbidden-dependency`,
`# guard: no-deep-sign`, `# guard: inside-out-order`, and `# guard: atomic-publish`). Copy each target
script into a temporary directory and perform these mutations one at a time; run only the named
focused case and require the test suite to fail:

| Mutation | Required detecting case |
| --- | --- |
| Change `Contents/Helpers` to `Contents/Resources/helpers` in the bundle copy | bundle layout test |
| Remove the forbidden dependency return in `macos-audit.sh` | `bad-homebrew` audit test |
| Add `--deep` to the app signing argument array | no-deep signing test |
| Swap helper/app signing calls | signing-order test |
| Remove `--options runtime` from the app signing args | runtime signing test |
| Remove `--timestamp` from release executable signing | timestamp signing test |
| Remove the host or audio-helper `--entitlements` assignment | exact entitlement test |
| Skip one real dylib's rpath/ID sanitation | forbidden dependency/ID audit test |
| Replace hidden-candidate rename with direct final copy | old-artifact/atomic publication test |

Define a helper that verifies the marker occurs exactly once before applying the mutation:

```bash
mutate_once() {
  file="$1"
  marker="$2"
  expression="$3"
  count="$(grep -cF -- "$marker" "$file")"
  [ "$count" = "1" ] || fail "$marker occurs $count times in $file"
  perl -0pi -e "$expression" "$file"
}
```

Use it only on temporary copies. After each mutation, pass the copy through the corresponding
`BUNDLE_SCRIPT`, `AUDIT_SCRIPT`, `SIGN_SCRIPT`, or `RELEASE_SCRIPT` variable and require the focused
test process to exit nonzero. A mutant that survives is a test failure.

- [ ] **Step 8: Run the complete automated verification**

Run:

```bash
chmod +x scripts/tests/macos-release-mutations.sh
./scripts/task.sh test:macos-release
./scripts/go.sh test ./internal/app -count=1
./scripts/task.sh check
git diff --check
```

Expected: all PASS. `check` does not prompt for Apple credentials or access the notary service.

- [ ] **Step 9: Build/package twice and exercise the no-identity fallback deterministically**

Run the following local integration sequence before installing Apple identities. The zero-identity
fallback assertion itself uses the fake `security` fixture so this test remains deterministic after
the owner installs certificates; observe and record the real machine's zero-identity warning only
when `security find-identity` still reports zero:

```bash
./scripts/task.sh package
./scripts/macos-audit.sh bin/loqui.app
./scripts/task.sh package
./scripts/macos-audit.sh bin/loqui.app

dev_stage="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dev-package.XXXXXX")"
./scripts/task.sh darwin:build DEV=true OUTPUT="$dev_stage/loqui"
./scripts/macos-bundle.sh --channel development --executable "$dev_stage/loqui" \
  --output "$dev_stage/Loqui.dev.app"
./scripts/macos-sign.sh app --channel development --app "$dev_stage/Loqui.dev.app" \
  2>"$dev_stage/signing.stderr"
if security find-identity -v -p codesigning | grep -q '0 valid identities found'; then
  grep -F 'TCC continuity is unavailable' "$dev_stage/signing.stderr"
fi
```

Use a trap that removes only the validated `loqui-dev-package.*` directory. Confirm both production
packages pass the auditor and neither production nor development bundle contains code under
Resources.

- [ ] **Step 10: Record the task checkpoint without committing**

Record shell suite, mutation suite, Go suite, full check, package audit, and expected no-identity warning in `.workflow/state.md`.

---

### Task 7: Configure Apple credentials and verify a real distributable

**Files:**
- Modify: `README.md:56-72,97-120`
- Modify: `docs/CHANGELOG.md:7`
- Create: `docs/e2e/use-cases/developer-id-release.md`
- Create: `docs/e2e/reports/2026-08-07-developer-id-release.md`
- Modify: `CONTINUITY.md`
- Update: `.workflow/state.md`

**Interfaces:**
- Consumes: an active Apple Developer Program account, Apple Development and Developer ID Application private keys installed in login Keychain, and a validated `loqui-notary` profile.
- Produces: real signed/notarized DMG evidence, per-channel designated-requirement continuity evidence, and an honest local/second-Mac E2E verdict.

- [ ] **Step 1: Document the one-time operator setup before executing it**

Add a README section with these exact safe steps:

1. In Xcode Settings → Accounts → Manage Certificates, create `Apple Development` for daily builds.
2. Create/download `Developer ID Application` through the Apple account and confirm its private key appears beneath it in Keychain Access.
3. Export the Developer ID certificate plus private key once as an encrypted `.p12`; store it outside the repo in the owner's encrypted backup.
4. Create an app-specific Apple password, then run the interactive command below so the password is never written in shell history:

```bash
read -r -p "Apple ID: " APPLE_ID
read -r -p "Apple Team ID: " TEAM_ID
xcrun notarytool store-credentials loqui-notary \
  --apple-id "$APPLE_ID" --team-id "$TEAM_ID"
unset APPLE_ID TEAM_ID
```

`notarytool` securely prompts for the app-specific password because `--password` is omitted and validates before saving.

- [ ] **Step 2: Verify identities and profile without exposing secrets**

Run:

```bash
security find-identity -v -p codesigning
xcrun notarytool history --keychain-profile loqui-notary --output-format json >/dev/null
```

Expected: exactly one intended Apple Development identity, exactly one intended Developer ID Application identity, and both certificate names end in the same Team ID. Do not copy any credential value into the repo or report.

- [ ] **Step 3: Prove daily development identity continuity**

Build two separate dev candidates with the Apple Development identity:

```bash
dev_compare="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dev-identity.XXXXXX")"
for candidate in 1 2; do
  mkdir -p "$dev_compare/$candidate"
  ./scripts/task.sh darwin:build DEV=true OUTPUT="$dev_compare/$candidate/loqui"
  ./scripts/macos-bundle.sh --channel development \
    --executable "$dev_compare/$candidate/loqui" \
    --output "$dev_compare/$candidate/Loqui.dev.app"
  ./scripts/macos-sign.sh app --channel development \
    --app "$dev_compare/$candidate/Loqui.dev.app"
  for item in \
    "Loqui.dev.app" \
    "Loqui.dev.app/Contents/Helpers/globe-listener" \
    "Loqui.dev.app/Contents/Helpers/macos-stt" \
    "Loqui.dev.app/Contents/Helpers/whisper-stt"; do
    codesign -dv --verbose=4 "$dev_compare/$candidate/$item" \
      2>"$dev_compare/$candidate/$(basename "$item").metadata"
    codesign -d -r- "$dev_compare/$candidate/$item" 2>&1 |
      sed -n '/^designated =>/p' \
      >"$dev_compare/$candidate/$(basename "$item").dr"
  done
done
for name in Loqui.dev.app globe-listener macos-stt whisper-stt; do
  diff -u "$dev_compare/1/$name.dr" "$dev_compare/2/$name.dr"
done
```

For each app and helper, save only `codesign -dv --verbose=4` metadata and the extracted designated
requirement. Assert:

- app identifier is `com.jualopezmo.loquigo.dev`;
- helper identifiers use `.dev` suffixes;
- both candidates have the same Team ID and compatible DR expressions;
- the app claims exactly Audio Input plus Apple Events, `macos-stt`/`whisper-stt` claim exactly
  Audio Input, and `globe-listener` claims no entitlements;
- every executable signed with Apple Development reports the Hardened Runtime flag;
- no command accesses the Developer ID identity;
- Accessibility/Input Monitoring grants survive installing/launching the second development candidate after the one-time migration grant.

- [ ] **Step 4: Build, notarize, and retain two release candidates for continuity testing**

Run:

```bash
VERSION="$(awk '/^info:/{in_info=1; next} in_info && /^  version:/{gsub(/["'\'' ]/, "", $2); print $2; exit}' build/config.yml)"
LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos
release_pair="$(mktemp -d "${TMPDIR:-/tmp}/loqui-release-pair.XXXXXX")"
cp "bin/release/Loqui-${VERSION}-macos-arm64.dmg" "$release_pair/release-1.dmg"
LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos
cp "bin/release/Loqui-${VERSION}-macos-arm64.dmg" "$release_pair/release-2.dmg"

for candidate in 1 2; do
  mountpoint="$release_pair/mount-$candidate"
  mkdir -p "$mountpoint"
  hdiutil attach -nobrowse -readonly -mountpoint "$mountpoint" \
    "$release_pair/release-$candidate.dmg"
  for item in \
    "Loqui.app" \
    "Loqui.app/Contents/Helpers/globe-listener" \
    "Loqui.app/Contents/Helpers/macos-stt" \
    "Loqui.app/Contents/Helpers/whisper-stt"; do
    codesign -dv --verbose=4 "$mountpoint/$item" \
      2>"$release_pair/$candidate-$(basename "$item").metadata"
    codesign -d -r- "$mountpoint/$item" 2>&1 |
      sed -n '/^designated =>/p' \
      >"$release_pair/$candidate-$(basename "$item").dr"
  done
  hdiutil detach "$mountpoint"
done
for name in Loqui.app globe-listener macos-stt whisper-stt; do
  diff -u "$release_pair/1-$name.dr" "$release_pair/2-$name.dr"
done
```

If more than one Developer ID identity is installed, set `LOQUI_SIGN_IDENTITY` to the chosen SHA-1
from Step 2 for both commands. Expected: `bin/release/Loqui-${VERSION}-macos-arm64.dmg`, two
temporary notarized candidates for continuity testing, and non-secret evidence under
`bin/release/evidence/${VERSION}/`, with one submission-ID directory per run. Keep `release_pair`
only until the local and second-Mac journeys
finish; it is outside Git and must be deleted afterwards.

- [ ] **Step 5: Independently verify the produced DMG and mounted app**

Run:

```bash
hdiutil verify "bin/release/Loqui-${VERSION}-macos-arm64.dmg"
xcrun stapler validate "bin/release/Loqui-${VERSION}-macos-arm64.dmg"
codesign --verify --verbose=2 "bin/release/Loqui-${VERSION}-macos-arm64.dmg"
spctl --assess --type open --context context:primary-signature --verbose=2 \
  "bin/release/Loqui-${VERSION}-macos-arm64.dmg"
```

Mount the DMG, copy Loqui to `/Applications`, and run `codesign --verify --deep --strict --verbose=2 /Applications/Loqui.app` plus `spctl --assess --type execute --verbose=2 /Applications/Loqui.app`. Confirm Developer ID authority, expected Team ID, hardened runtime on executables, and stable production identifiers.
Also extract and compare the app/helper entitlement sets to the exact assignments above, verify both
plist version keys equal `VERSION`, and confirm the installed standard app has no bundled
`ggml-small.bin`.

- [ ] **Step 6: Execute the local E2E use cases**

Write `docs/e2e/use-cases/developer-id-release.md` before the run with these cases:

- clean mount/install/launch through Finder/Gatekeeper;
- one-time Accessibility, Input Monitoring, microphone, and speech permission migration;
- fn listener receives the trigger;
- with no pre-existing data-directory model and no bundled model, the first Whisper attempt exposes
  the supported download path; interrupt one controlled download, retry it, verify digest/size, then
  transcribe successfully without Homebrew or checkout paths;
- Apple STT runs where macOS supports it;
- Azure and at least one other configured cloud provider complete a dictation;
- second production release installs over the first without revoking established production grants;
- no release log/evidence contains provider keys, Apple credentials, `/Users/`, `/opt/homebrew`, or the checkout path.

Use `verify-e2e` to execute and write `docs/e2e/reports/2026-08-07-developer-id-release.md` with one classification per case and top-level `VERDICT`.

- [ ] **Step 7: Execute or honestly defer the second-Mac journey**

On another Apple Silicon Mac without this checkout and without relying on the build Mac's Homebrew
installation, install release 1, grant permissions, exercise the clean first-use Whisper
download/retry plus fn/cloud dictation, then install release 2 and confirm grants remain. Assess the
stapled DMG while offline once to prove the outer ticket is usable without the notary network; restore
network before the intentionally non-bundled model download. If no second Mac is available, the
report must say `VERDICT: PARTIAL`, name this exact blocker, and `.workflow/state.md` must leave the
E2E gate unchecked.

- [ ] **Step 8: Update durable documentation from measured results**

Only after the real run:

- update README claims from the commands that actually passed;
- add a changelog section stating stable Apple Development identity and Developer ID/notarized arm64 DMG behavior;
- rewrite `CONTINUITY.md` with the exact remaining second-Mac blocker or next priority;
- record certificate/profile presence without identity hashes or credentials;
- keep Keychain migration for provider API keys explicitly out of scope.

- [ ] **Step 9: Run cross-engine code review and final verification**

Use the project `review` workflow against the complete diff and resolve every P0/P1/P2. Then run fresh:

```bash
./scripts/task.sh test:macos-release
./scripts/go.sh test ./... -race -count=1
./scripts/task.sh check
git diff --check
git diff --cached --check
```

Expected: all commands PASS. Re-run the real artifact verification if review changes any bundle, signing, audit, or release script.

- [ ] **Step 10: Close gates and make the single implementation commit**

Check the standard gate only when its evidence exists. If second-Mac verification remains unavailable, do not mark the E2E gate complete and do not ship. When every required box is green, run `finish-branch`; use a commit message such as:

```bash
git commit -m "feat(macos): add Developer ID release pipeline"
```

Do not add a coauthor trailer. Push/PR remain separate explicit owner-approved actions.
