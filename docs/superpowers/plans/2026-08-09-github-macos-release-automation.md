# GitHub macOS Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manually approved GitHub Action that releases the current `main` version as a signed,
notarized Apple Silicon DMG, then atomically creates its version tag and public GitHub Release.

**Architecture:** Keep `./scripts/task.sh release:macos` as the Apple product authority. Add tested
repository seams for version metadata, GitHub preflight/publication, and an optional explicit notary
Keychain; a two-job `macos-26` workflow runs secret-free checks, waits on the protected `release`
Environment, rebuilds with temporary Apple credentials, and publishes last.

**Tech Stack:** Bash 3.2-compatible shell, Taskfile/Wails v3, Git/GitHub CLI, GitHub Actions,
GitHub-hosted `macos-26` arm64 runners, Go 1.25.0, Node 24/npm, Apple `security`, `codesign`,
`notarytool`, `stapler`, `spctl`, `hdiutil`, and SHA-256 via `shasum`.

## Global Constraints

- Do not begin Task 1 until `feat/developer-id-release` has a clean full-diff review, passes its
  second-Mac E2E, is committed through its ship gate, and is present on `main`.
- Start implementation from updated `main` on a new `feat/github-macos-release` branch; never change
  code on `main`.
- Initialize a new `new-feature` `.workflow/state.md`; do not overwrite the active Developer ID state
  before that feature is finished.
- Obtain the project-required cross-engine plan review before implementation; resolve every open
  P0/P1/P2 finding.
- The project ship gate supersedes the planning skill's frequent-commit default: each task ends with
  a clean diff checkpoint, and the implementation commit is deferred until every locally closable
  box is checked plus the owner explicitly resolves the default-branch bootstrap gate in Task 7.
- `workflow_dispatch` is the only release trigger. There is no version, tag, branch, draft, or
  prerelease workflow input.
- `build/config.yml` is the only version source and must contain exactly one quoted stable
  `MAJOR.MINOR.PATCH` value beneath `info:`.
- A release is valid only while its recorded SHA remains the remote tip of `main`; recheck before
  credentials are decoded and immediately before GitHub publication.
- Existing tags and Releases are immutable inputs: never overwrite, move, resume, or automatically
  delete them.
- Use GitHub-hosted Apple Silicon `macos-26`, Go 1.25.0, Node 24, the committed npm lockfile, and the
  repository-pinned Wails/Whisper/Azure versions.
- Run `scripts/vendor-speech-sdk.sh` in both jobs because `third_party/speech-sdk/` is gitignored and
  `release:macos` checks the framework before building.
- Keep Apple secrets only in Environment `release`; preflight has `contents: read`, and only the
  protected job has `contents: write`.
- Pin every referenced Action to a full 40-character commit SHA.
- Preserve Bash 3.2 compatibility, quote all paths, keep shell tracing disabled, and never log secret
  values or environment dumps.
- The public GitHub Release contains exactly the DMG and `.sha256`. Sanitized evidence is a 14-day
  Actions artifact and must not be treated as confidential in this public repository.
- Do not claim live GitHub E2E before the workflow exists on the default branch. The bootstrap
  limitation and the selected closure path are recorded in Task 7.

## File map

| Path | Responsibility |
| --- | --- |
| `scripts/release-version.sh` | Parse and validate the sole release version |
| `scripts/tests/release-version-test.sh` | Hermetic version-parser matrix |
| `scripts/release-macos.sh` | Reuse the metadata reader and optionally target a CI Keychain |
| `scripts/tests/release-macos-test.sh` | Preserve local notarization behavior and prove explicit-Keychain arguments |
| `scripts/github-release.sh` | Validate GitHub state, prepare assets, publish, verify, and diagnose residual state |
| `scripts/tests/github-release-test.sh` | Fake-Git/GitHub lifecycle tests with no network or credentials |
| `.github/workflows/release.yml` | Two-job manual release orchestration and temporary credential lifecycle |
| `scripts/tests/github-release-workflow-test.sh` | Static workflow policy, permission, ordering, and pin contracts |
| `Taskfile.yml` | Bind all new shell/policy tests into `test:macos-release` and `check` |
| `README.md` | Environment secrets, one-time GitHub setup, release operation, and recovery |
| `docs/e2e/use-cases/github-macos-release.md` | Live and pre-merge journeys with explicit bootstrap limitation |
| `docs/e2e/reports/2026-08-09-github-macos-release.md` | Execution evidence and final classifications |
| `docs/CHANGELOG.md` | Ship-time summary after all gates pass |

---

### Task 1: Establish one strict release-version reader

**Files:**
- Create: `scripts/release-version.sh`
- Create: `scripts/tests/release-version-test.sh`
- Modify: `scripts/release-macos.sh:23-26,66-67`
- Modify: `scripts/tests/release-macos-test.sh:15-32`
- Modify: `Taskfile.yml:34-44`

**Interfaces:**
- Consumes: `scripts/release-version.sh [--root ABSOLUTE_REPO_ROOT]`
- Produces: exactly one `MAJOR.MINOR.PATCH` line on stdout; nonzero with a prefixed diagnostic on
  stderr for missing, duplicate, empty, unquoted, single-quoted, prerelease, or malformed values.
- Produces for later tasks: `scripts/release-macos.sh` calls this script rather than owning another
  YAML parser.

- [ ] **Step 1: Add the failing parser matrix**

Create `scripts/tests/release-version-test.sh` using the existing `testlib.sh` helpers. The matrix is
exact and includes a valid config plus every rejected shape:

```bash
#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

script="${RELEASE_VERSION_SCRIPT:-$repo_root/scripts/release-version.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-version-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

write_config() {
  fixture_root="$1"
  body="$2"
  mkdir -p "$fixture_root/build"
  printf '%s\n' "$body" >"$fixture_root/build/config.yml"
}

valid="$tmp/valid"
write_config "$valid" $'version: \'3\'\ninfo:\n  productName: "Loqui"\n  version: "1.2.3"\nwindows:\n  version: "9.9.9"'
assert_eq "$("$script" --root "$valid")" "1.2.3"

invalid_cases=(missing duplicate empty unquoted single-quoted prerelease prefixed trailing leading-zero)
for case_name in "${invalid_cases[@]}"; do
  fixture="$tmp/$case_name"
  case "$case_name" in
    missing) body=$'info:\n  productName: "Loqui"' ;;
    duplicate) body=$'info:\n  version: "1.2.3"\n  version: "1.2.4"' ;;
    empty) body=$'info:\n  version: ""' ;;
    unquoted) body=$'info:\n  version: 1.2.3' ;;
    single-quoted) body=$'info:\n  version: \'1.2.3\'' ;;
    prerelease) body=$'info:\n  version: "1.2.3-beta.1"' ;;
    prefixed) body=$'info:\n  version: "v1.2.3"' ;;
    trailing) body=$'info:\n  version: "1.2.3" # comment' ;;
    leading-zero) body=$'info:\n  version: "01.2.3"' ;;
  esac
  write_config "$fixture" "$body"
  run_expect_fail "$script" --root "$fixture"
done

run_expect_fail "$script" --root "$tmp/absent-root"
run_expect_fail "$script" --root relative-root
echo "release-version-test: PASS"
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
bash scripts/tests/release-version-test.sh
```

Expected: nonzero because `scripts/release-version.sh` does not exist.

- [ ] **Step 3: Implement the strict reader**

Create an executable Bash script. Use one `awk` pass scoped to the top-level `info:` block and
require the exact quoted stable format:

```bash
#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [ "${1:-}" = "--root" ]; then
  [ "$#" -eq 2 ] || { echo "release-version: usage: $0 [--root REPO_ROOT]" >&2; exit 2; }
  root="$2"
elif [ "$#" -ne 0 ]; then
  echo "release-version: usage: $0 [--root REPO_ROOT]" >&2
  exit 2
fi

case "$root" in /*) ;; *) echo "release-version: root must be absolute" >&2; exit 2 ;; esac

config="$root/build/config.yml"
[ -f "$config" ] || { echo "release-version: missing $config" >&2; exit 1; }

if ! version="$(awk '
  $0 == "info:" { in_info=1; next }
  in_info && /^[^[:space:]]/ { in_info=0 }
  in_info && /^  version:/ {
    count++
    if ($0 !~ /^  version: "(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"[[:space:]]*$/) bad=1
    value=$0
    sub(/^  version: "/, "", value)
    sub(/"[[:space:]]*$/, "", value)
  }
  END {
    if (count != 1 || bad || value == "") exit 1
    print value
  }
' "$config")"; then
  echo "release-version: info.version must appear once as quoted MAJOR.MINOR.PATCH" >&2
  exit 1
fi

printf '%s\n' "$version"
```

Use `chmod +x scripts/release-version.sh scripts/tests/release-version-test.sh`.

- [ ] **Step 4: Make the local release consume the shared reader**

Remove `read_release_version()` from `scripts/release-macos.sh`. In `phase_preflight`, use:

```bash
version="$("$release_root_dir/scripts/release-version.sh" --root "$release_root_dir")" \
  || die "could not read release version"
```

Add `release-version.sh` to the release script's required executable list and add a static assertion
to `release-macos-test.sh` that the shared reader is referenced.

- [ ] **Step 5: Bind and run the focused GREEN suite**

Add `./scripts/tests/release-version-test.sh` before `release-macos-test.sh` in
`test:macos-release`, then run:

```bash
bash scripts/tests/release-version-test.sh
bash scripts/tests/release-macos-test.sh
shellcheck -x -s bash scripts/release-version.sh scripts/release-macos.sh \
  scripts/tests/release-version-test.sh scripts/tests/release-macos-test.sh
```

Expected: both tests print `PASS`; ShellCheck exits zero.

- [ ] **Step 6: Record the task checkpoint without committing**

Run:

```bash
git diff --check
git status --short
```

Expected: no whitespace errors; only Task 1 paths plus approved planning/workflow-state files change.
Do not commit while the ship-gate checklist is open.

---

### Task 2: Add an explicit temporary-Keychain seam to notarization

**Files:**
- Modify: `scripts/release-macos.sh:4-18,46-79,223-252`
- Modify: `scripts/tests/release-macos-test.sh:8-32,128-139`

**Interfaces:**
- Consumes: optional `LOQUI_NOTARY_KEYCHAIN=/absolute/path/to/keychain-db`.
- Produces: global `notary_auth_args=(--keychain-profile "$profile" [--keychain "$path"])` used
  by `notarytool history`, `submit`, and `log`.
- Preserves: when the variable is absent, the command arguments remain profile-only for local
  `loqui-notary` releases.

- [ ] **Step 1: Add failing exact-argument tests**

Before changing production code, extend `release-macos-test.sh` with subshell probes that source the
script under both environments:

```bash
default_auth="$(bash -c '. "$1"; printf "<%s>\n" "${notary_auth_args[@]}"' \
  _ "$release_script")"
assert_eq "$default_auth" $'<--keychain-profile>\n<loqui-notary>'

ci_keychain="$tmp/loqui-ci.keychain-db"
: >"$ci_keychain"
ci_auth="$(LOQUI_NOTARY_PROFILE=loqui-ci-notary LOQUI_NOTARY_KEYCHAIN="$ci_keychain" \
  bash -c '. "$1"; printf "<%s>\n" "${notary_auth_args[@]}"' _ "$release_script")"
assert_eq "$ci_auth" \
  "$(printf '<%s>\n' --keychain-profile loqui-ci-notary --keychain "$ci_keychain")"

assert_eq "$(grep -F -c '"${notary_auth_args[@]}"' "$release_script")" "3"
```

Also call a focused `validate_notary_keychain` helper and assert that relative and missing absolute
paths fail before `notarytool` can run.

- [ ] **Step 2: Run the test and verify RED**

Run `bash scripts/tests/release-macos-test.sh`.

Expected: failure because `notary_auth_args` and explicit Keychain validation do not exist.

- [ ] **Step 3: Implement the minimal argument seam**

Immediately after `profile=...`, initialize the array without printing its values:

```bash
notary_keychain="${LOQUI_NOTARY_KEYCHAIN:-}"
notary_auth_args=(--keychain-profile "$profile")
if [ -n "$notary_keychain" ]; then
  notary_auth_args+=(--keychain "$notary_keychain")
fi
```

Add a focused validator and call it at the start of `phase_preflight`:

```bash
validate_notary_keychain() {
  [ -n "$notary_keychain" ] || return 0
  case "$notary_keychain" in /*) ;; *) die "LOQUI_NOTARY_KEYCHAIN must be absolute" ;; esac
  [ -f "$notary_keychain" ] || die "notary keychain does not exist: $notary_keychain"
}
```

Replace only the authentication arguments in all three commands:

```bash
xcrun notarytool history "${notary_auth_args[@]}" --output-format json
xcrun notarytool submit "$dmg" "${notary_auth_args[@]}" --wait --timeout 30m --output-format json
xcrun notarytool log "$submission_id" "$stage/notary-log.json" "${notary_auth_args[@]}"
```

Preserve the existing redirects, retries, return-code capture, evidence handling, and diagnostics.

- [ ] **Step 4: Run local and CI-mode GREEN tests**

Run:

```bash
bash scripts/tests/release-macos-test.sh
bash scripts/tests/macos-sign-test.sh
shellcheck -x -s bash scripts/release-macos.sh scripts/tests/release-macos-test.sh
```

Expected: both tests print `PASS`; the default probe has no `--keychain`; the CI probe has exactly
one explicit path; ShellCheck exits zero.

- [ ] **Step 5: Record the task checkpoint without committing**

Run `git diff --check` and inspect `git diff -- scripts/release-macos.sh
scripts/tests/release-macos-test.sh`. Confirm no signing, ticket, stapling, or publication phase was
otherwise altered. Do not commit.

---

### Task 3: Implement secret-free GitHub release preflight

**Files:**
- Create: `scripts/github-release.sh`
- Create: `scripts/tests/github-release-test.sh`
- Modify: `Taskfile.yml:34-44`

**Interfaces:**
- Consumes: `GITHUB_REF`, `GITHUB_SHA`, `GITHUB_REPOSITORY`, `GH_TOKEN`, optional
  `GITHUB_OUTPUT`, and fakeable `git`/`gh` commands on `PATH`.
- Consumes command:
  `scripts/github-release.sh preflight [--expect-sha SHA --expect-version VERSION --expect-tag TAG]`.
- Produces outputs: `sha`, `version`, `tag`, `dmg_name`; appends the selected/checked-out/remote SHA,
  version, and tag to `GITHUB_STEP_SUMMARY` when that path is present.
- Guarantees: success means checkout `HEAD`, remote `main`, expected metadata, absent tag, and absent
  GitHub Release all agree at that instant.

- [ ] **Step 1: Write fake Git/GitHub commands and the failing matrix**

Create `scripts/tests/github-release-test.sh`. Its fixture uses one exact SHA and logs every remote
operation:

```bash
sha=0123456789abcdef0123456789abcdef01234567
export GITHUB_REF=refs/heads/main GITHUB_SHA="$sha" GITHUB_REPOSITORY=Juan-Motta/loqui
export GH_TOKEN=fake-token
export GITHUB_OUTPUT="$tmp/outputs" GITHUB_STEP_SUMMARY="$tmp/summary"
export FAKE_HEAD_SHA="$sha" FAKE_MAIN_SHA="$sha"
export FAKE_GH_STATE="$tmp/gh-state" FAKE_CALLS="$tmp/calls"
export LOQUI_RELEASE_ROOT="$tmp/repo" LOQUI_RELEASE_VERSION_SCRIPT="$tmp/fake-version"
export PATH="$tmp/fake-bin:$PATH"
printf '#!/usr/bin/env bash\nprintf "0.1.0\\n"\n' >"$tmp/fake-version"
chmod +x "$tmp/fake-version"
```

The fake `git` implements only `rev-parse HEAD` and `ls-remote` for `main`/the version tag. The fake
`gh` implements `release view`, `release create`, and `api repos/Juan-Motta/loqui`; every invocation
is appended with shell-escaped arguments to `FAKE_CALLS`.

Add these cases, resetting fake state between each:

```bash
"$script" preflight
assert_contains "$GITHUB_OUTPUT" "sha=$sha"
assert_contains "$GITHUB_OUTPUT" "version=0.1.0"
assert_contains "$GITHUB_OUTPUT" "tag=v0.1.0"
assert_contains "$GITHUB_OUTPUT" "dmg_name=Loqui-0.1.0-macos-arm64.dmg"
assert_contains "$GITHUB_STEP_SUMMARY" "0.1.0"
assert_contains "$GITHUB_STEP_SUMMARY" "$sha"

GITHUB_REF=refs/heads/feature run_expect_fail "$script" preflight
FAKE_HEAD_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa run_expect_fail "$script" preflight
FAKE_MAIN_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb run_expect_fail "$script" preflight
FAKE_TAG_SHA="$sha" run_expect_fail "$script" preflight
FAKE_RELEASE_EXISTS=1 run_expect_fail "$script" preflight
FAKE_RELEASE_QUERY_FAIL=1 run_expect_fail "$script" preflight
run_expect_fail "$script" preflight --expect-version 0.1.1
run_expect_fail "$script" preflight --expect-tag v0.1.1
```

For the release-absence probe, fake `gh api repos/Juan-Motta/loqui/releases/tags/v0.1.0` returns an
`(HTTP 404)` diagnostic. Other nonzero diagnostics represent authentication/network failure and
must fail closed.

- [ ] **Step 2: Run the focused test and verify RED**

Run `bash scripts/tests/github-release-test.sh`.

Expected: nonzero because `scripts/github-release.sh` does not exist.

- [ ] **Step 3: Implement command dispatch and invariant helpers**

Create the executable script with a test-only root/version seam, strict SHA/repository validation,
and command dispatch:

```bash
#!/usr/bin/env bash
set -euo pipefail

root="${LOQUI_RELEASE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
version_script="${LOQUI_RELEASE_VERSION_SCRIPT:-$root/scripts/release-version.sh}"

die() { echo "github-release: $*" >&2; return 1; }
is_sha() { [[ "$1" =~ ^[0-9a-f]{40}$ ]]; }
write_output() { [ -z "${GITHUB_OUTPUT:-}" ] || printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"; }

remote_main_sha() {
  git ls-remote origin refs/heads/main | awk '$2 == "refs/heads/main" {print $1}'
}

assert_tag_absent() {
  tag="$1"
  set +e
  tag_refs="$(git ls-remote --tags origin "refs/tags/$tag")"
  tag_rc=$?
  set -e
  [ "$tag_rc" -eq 0 ] || die "cannot verify tag absence for $tag"
  [ -z "$tag_refs" ] || die "tag already exists: $tag"
}

assert_release_absent() {
  tag="$1"
  set +e
  release_probe="$(gh api "repos/$GITHUB_REPOSITORY/releases/tags/$tag" 2>&1)"
  release_rc=$?
  set -e
  if [ "$release_rc" -eq 0 ]; then die "GitHub Release already exists: $tag"; fi
  printf '%s\n' "$release_probe" | grep -F '(HTTP 404)' >/dev/null \
    || die "cannot verify GitHub Release absence for $tag"
}
```

Implement `preflight` so it:

1. parses the three optional expectations with a rejecting `case` loop;
2. validates `GITHUB_REPOSITORY` as `owner/name`, both SHAs as 40 lowercase hex, and ref exactly;
3. compares checked-out `HEAD`, dispatch SHA, remote `main`, and optional expected SHA;
4. reads version and derives tag/DMG name;
5. compares optional expected version/tag;
6. calls both fail-closed absence assertions;
7. writes the four outputs and non-secret preflight summary only after all checks pass.

The fake `gh api` returns an exact `(HTTP 404)` diagnostic for an absent Release and a different
nonzero diagnostic for network/authentication failure. Do not treat every nonzero result as absent.

- [ ] **Step 4: Run the preflight matrix GREEN**

Run:

```bash
bash scripts/tests/github-release-test.sh
shellcheck -x -s bash scripts/github-release.sh scripts/tests/github-release-test.sh
```

Expected: test prints `github-release-test: PASS`; ShellCheck exits zero.

- [ ] **Step 5: Bind the focused test and checkpoint**

Add `./scripts/tests/github-release-test.sh` to `test:macos-release`, run it through
`./scripts/task.sh test:macos-release`, then run `git diff --check`. Do not commit.

---

### Task 4: Prepare, publish, verify, and diagnose GitHub assets

**Files:**
- Modify: `scripts/github-release.sh`
- Modify: `scripts/tests/github-release-test.sh`

**Interfaces:**
- Consumes command:
  `scripts/github-release.sh prepare --sha SHA --version VERSION --tag TAG`.
- Produces outputs: absolute `dmg_path`, `checksum_path`, `evidence_path`, lowercase `checksum`, and
  `submission_id` from the sole evidence-directory basename.
- Consumes command:
  `scripts/github-release.sh publish --sha SHA --version VERSION --tag TAG`.
- Produces: a public non-prerelease GitHub Release, exact tag target, two exact assets, and a summary
  URL; on ambiguous failure it reports residual tag/Release state and never deletes.

- [ ] **Step 1: Extend tests with asset and publication RED cases**

Build a valid local publication fixture:

```bash
release_root="$LOQUI_RELEASE_ROOT/bin/release"
dmg_name=Loqui-0.1.0-macos-arm64.dmg
put_file "$release_root/$dmg_name" "signed-notarized-fixture" 644
put_file "$release_root/evidence/0.1.0/submission-123/notary-log.json" '{"status":"Accepted"}' 644
: >"$GITHUB_OUTPUT"
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0
assert_file "$release_root/$dmg_name.sha256"
(cd "$release_root" && shasum -a 256 -c "$dmg_name.sha256")
assert_contains "$GITHUB_OUTPUT" "evidence_path=$release_root/evidence/0.1.0/submission-123"
assert_contains "$GITHUB_OUTPUT" "submission_id=submission-123"
```

Then cover failures for missing DMG, wrong tag/version pairing, zero evidence directories, two
evidence directories, and a corrupted checksum.

For publication, make fake `gh release create` create `$FAKE_GH_STATE/published` and make subsequent
`release view --json` return:

```json
{"url":"https://github.com/Juan-Motta/loqui/releases/tag/v0.1.0","isDraft":false,"isPrerelease":false,"tagName":"v0.1.0","targetCommitish":"0123456789abcdef0123456789abcdef01234567","assets":[{"name":"Loqui-0.1.0-macos-arm64.dmg"},{"name":"Loqui-0.1.0-macos-arm64.dmg.sha256"}]}
```

Assert the logged create call has `--target` plus the exact SHA, `--title` plus `Loqui 0.1.0`,
`--generate-notes`, `--latest`, and both asset paths. Add negative cases for draft, prerelease,
wrong target, missing/extra asset, stale `main` at final preflight, and a create failure.

Finally assert production source contains none of these destructive forms:

```bash
assert_not_contains "$script" "release delete"
assert_not_contains "$script" "push --delete"
assert_not_contains "$script" "tag -d"
```

- [ ] **Step 2: Run the expanded suite and verify RED**

Run `bash scripts/tests/github-release-test.sh`.

Expected: failure on unknown `prepare`/`publish` commands.

- [ ] **Step 3: Implement deterministic asset preparation**

Add shared option parsing that requires SHA/version/tag and verifies `tag == v$version` plus the
metadata reader's current version. Implement `prepare` around exact paths:

```bash
dmg_name="Loqui-$version-macos-arm64.dmg"
dmg_path="$root/bin/release/$dmg_name"
checksum_path="$dmg_path.sha256"
evidence_root="$root/bin/release/evidence/$version"

[ -f "$dmg_path" ] || die "missing release DMG: $dmg_path"
[ -d "$evidence_root" ] || die "missing release evidence: $evidence_root"

evidence_path=""
evidence_count=0
while IFS= read -r candidate; do
  evidence_path="$candidate"
  evidence_count=$((evidence_count + 1))
done < <(find "$evidence_root" -mindepth 1 -maxdepth 1 -type d -print | LC_ALL=C sort)
[ "$evidence_count" -eq 1 ] || die "expected one evidence directory, found $evidence_count"

(cd "$(dirname "$dmg_path")" && shasum -a 256 "$dmg_name" >"$dmg_name.sha256")
(cd "$(dirname "$dmg_path")" && shasum -a 256 -c "$dmg_name.sha256")
checksum="$(awk 'NR == 1 {print $1}' "$checksum_path")"
[[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || die "invalid SHA-256 output"
```

Require the evidence basename to be a non-empty safe identifier, then write all five outputs only
after verification. Append version, SHA, tag, checksum, notarization submission ID, and
`loqui-release-evidence-$tag` to `GITHUB_STEP_SUMMARY` without including any evidence body.

- [ ] **Step 4: Implement final revalidation and one-shot publication**

Before publication, run the same preflight invariants with exact expectations while suppressing
duplicate output writes. Re-verify the checksum, then make one `gh` call:

```bash
gh release create "$tag" \
  "$dmg_path#$dmg_name" \
  "$checksum_path#$dmg_name.sha256" \
  --repo "$GITHUB_REPOSITORY" \
  --target "$sha" \
  --title "Loqui $version" \
  --generate-notes \
  --latest
```

If it exits nonzero, query the exact remote tag and `gh release view` with `set +e`, append their
presence/absence plus a recovery warning to `GITHUB_STEP_SUMMARY`, and return the original failure.
Do not mutate remote state.

On success:

- require `git ls-remote origin refs/tags/$tag` to equal `sha`;
- query `gh release view --json url,isDraft,isPrerelease,tagName,targetCommitish,assets`;
- use `jq -e` to require `isDraft == false`, `isPrerelease == false`, exact tag/target, and the sorted
  asset names equal exactly `[DMG, checksum]`;
- append version, SHA, checksum, and URL to `GITHUB_STEP_SUMMARY`.

- [ ] **Step 5: Run GREEN publication tests**

Run:

```bash
bash scripts/tests/github-release-test.sh
shellcheck -x -s bash scripts/github-release.sh scripts/tests/github-release-test.sh
```

Expected: all preflight, prepare, successful publication, malformed-response, and ambiguous-failure
cases pass without network access.

- [ ] **Step 6: Record the task checkpoint without committing**

Run `git diff --check` and inspect the exact fake call log from the happy-path test. Confirm no `gh`
write occurs before final preflight and no deletion command exists. Do not commit.

---

### Task 5: Add the protected two-job GitHub workflow

**Files:**
- Create: `.github/workflows/release.yml`
- Create: `scripts/tests/github-release-workflow-test.sh`
- Modify: `Taskfile.yml:34-44`

**Interfaces:**
- Consumes preflight outputs: `sha`, `version`, `tag`, `dmg_name`.
- Consumes Environment: `release` with five named secrets and a required reviewer.
- Produces: temporary `loqui-ci-notary` profile in an explicit Keychain, local release artifacts,
  14-day sanitized evidence artifact, and the final GitHub Release.
- Uses pinned Action commits verified on 2026-08-09:
  `actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09`,
  `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16`,
  `actions/setup-node@a0853c24544627f65ddf259abe73b1d18a591444`, and
  `actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02`.

- [ ] **Step 1: Write the failing workflow-policy test**

Create `scripts/tests/github-release-workflow-test.sh`. Extract the `preflight:` and `release:` job
blocks by their two-space job indentation, then assert:

```bash
assert_contains "$workflow" "workflow_dispatch:"
assert_not_contains "$workflow" "push:"
assert_not_contains "$workflow" "pull_request:"
assert_not_contains "$workflow" "schedule:"
assert_contains "$workflow" "group: macos-release"
assert_contains "$workflow" "cancel-in-progress: false"
assert_contains "$preflight_job" "runs-on: macos-26"
assert_not_contains "$preflight_job" "environment:"
assert_not_contains "$preflight_job" 'secrets.'
assert_not_contains "$preflight_job" "contents: write"
assert_contains "$release_job" "needs: preflight"
assert_contains "$release_job" "environment: release"
assert_contains "$release_job" "contents: write"
assert_contains "$release_job" "if: always()"
assert_contains "$release_job" "./scripts/vendor-speech-sdk.sh"
assert_contains "$release_job" "./scripts/task.sh release:macos"
assert_contains "$release_job" "retention-days: 14"
```

For every non-comment `uses:` line, extract the suffix after `@` and require exactly 40 lowercase
hex characters. Use line-number checks to prove order:

```text
revalidate < credential import < release:macos < prepare < upload-artifact < publish < cleanup
```

Also require each of the five secret names exactly once in the protected job and zero times in the
preflight block.

- [ ] **Step 2: Run the policy test and verify RED**

Run `bash scripts/tests/github-release-workflow-test.sh`.

Expected: failure because `.github/workflows/release.yml` does not exist.

- [ ] **Step 3: Add the manual trigger, preflight job, and immutable outputs**

Create the workflow header and first job with these exact policies:

```yaml
name: Release

on:
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: macos-release
  cancel-in-progress: false

jobs:
  preflight:
    runs-on: macos-26
    outputs:
      sha: ${{ steps.release-metadata.outputs.sha }}
      version: ${{ steps.release-metadata.outputs.version }}
      tag: ${{ steps.release-metadata.outputs.tag }}
      dmg_name: ${{ steps.release-metadata.outputs.dmg_name }}
```

Add pinned checkout with `ref: ${{ github.sha }}`, `fetch-depth: 0`, and
`persist-credentials: false`; pinned Go setup with `go-version: 1.25.0` and `cache: false`; pinned
Node setup with `node-version: 24`; dependency setup with
`HOMEBREW_NO_AUTO_UPDATE=1 brew install cmake jq`, `npm ci --prefix frontend`, and
`./scripts/vendor-speech-sdk.sh`.

Run `./scripts/github-release.sh preflight` in step `id: release-metadata`, with `GH_TOKEN` scoped to
that step, then run `./scripts/task.sh check`. Do not reference Environment or secrets.

- [ ] **Step 4: Add protected release setup and stale-run revalidation**

Define the second job:

```yaml
  release:
    needs: preflight
    runs-on: macos-26
    environment: release
    permissions:
      contents: write
```

Repeat checkout/toolchain/dependency setup at the exact preflight SHA. Before credential setup, run:

```bash
./scripts/github-release.sh preflight \
  --expect-sha "${{ needs.preflight.outputs.sha }}" \
  --expect-version "${{ needs.preflight.outputs.version }}" \
  --expect-tag "${{ needs.preflight.outputs.tag }}"
```

Scope `GH_TOKEN` only to this revalidation step.

- [ ] **Step 5: Implement temporary credential setup without logging secrets**

Map the five Environment secrets into the setup step's environment. Use exact runner-temp paths,
generate the Keychain password in memory, and export only non-secret paths:

```bash
set -euo pipefail
certificate_path="$RUNNER_TEMP/loqui-developer-id.p12"
api_key_path="$RUNNER_TEMP/loqui-notary-api-key.p8"
keychain_path="$RUNNER_TEMP/loqui-ci.keychain-db"
keychain_password="$(openssl rand -base64 32)"
echo "::add-mask::$keychain_password"

printf '%s' "$MACOS_CERTIFICATE_P12_BASE64" | base64 --decode >"$certificate_path"
printf '%s\n' "$APP_STORE_CONNECT_API_KEY_P8" >"$api_key_path"
chmod 600 "$certificate_path" "$api_key_path"

security create-keychain -p "$keychain_password" "$keychain_path"
security set-keychain-settings -lut 21600 "$keychain_path"
security unlock-keychain -p "$keychain_password" "$keychain_path"
security import "$certificate_path" -P "$MACOS_CERTIFICATE_PASSWORD" \
  -A -t cert -f pkcs12 -k "$keychain_path"
security set-key-partition-list -S apple-tool:,apple: -s \
  -k "$keychain_password" "$keychain_path"
security list-keychains -d user -s "$keychain_path"

xcrun notarytool store-credentials loqui-ci-notary \
  --key "$api_key_path" \
  --key-id "$APP_STORE_CONNECT_KEY_ID" \
  --issuer "$APP_STORE_CONNECT_ISSUER_ID" \
  --keychain "$keychain_path"

rm -f -- "$certificate_path" "$api_key_path"
```

The step name contains no secret interpolation, and `set -x` is forbidden. Delete the decoded
`.p12` and `.p8` immediately after their material is imported into the temporary Keychain; the final
cleanup step repeats those exact paths only as a failure fallback.

- [ ] **Step 6: Invoke the existing release and prepare/upload evidence**

Run the product authority with only its non-secret profile/path inputs:

```bash
LOQUI_NOTARY_PROFILE=loqui-ci-notary \
LOQUI_NOTARY_KEYCHAIN="$RUNNER_TEMP/loqui-ci.keychain-db" \
./scripts/task.sh release:macos
```

Then run `scripts/github-release.sh prepare` with exact preflight outputs in a step with
`id: release-assets`. Upload only `${{ steps.release-assets.outputs.evidence_path }}` via the pinned
artifact action, naming it `loqui-release-evidence-${{ needs.preflight.outputs.tag }}`, with
`retention-days: 14` and `if-no-files-found: error`.

- [ ] **Step 7: Publish last and add unconditional safe cleanup**

Call `scripts/github-release.sh publish` with exact outputs and step-scoped `GH_TOKEN`. It is the
only remote write.

Add the final `if: always()` step. Disable `errexit` for best-effort cleanup and accept only exact
runner-temp paths:

```bash
set +e
keychain_path="$RUNNER_TEMP/loqui-ci.keychain-db"
certificate_path="$RUNNER_TEMP/loqui-developer-id.p12"
api_key_path="$RUNNER_TEMP/loqui-notary-api-key.p8"
case "$keychain_path" in
  "$RUNNER_TEMP"/*) security delete-keychain "$keychain_path" ;;
esac
for path in "$certificate_path" "$api_key_path"; do
  case "$path" in "$RUNNER_TEMP"/*) rm -f -- "$path" ;; esac
done
exit 0
```

Do not clean a repository path, `$HOME`, `~`, a glob, or an empty/unresolved value.

- [ ] **Step 8: Run workflow-policy GREEN checks**

Run:

```bash
bash scripts/tests/github-release-workflow-test.sh
shellcheck -x -s bash scripts/tests/github-release-workflow-test.sh
git diff --check -- .github/workflows/release.yml scripts/tests/github-release-workflow-test.sh
```

Expected: policy test prints `github-release-workflow-test: PASS`; all action pins and ordering pass.

- [ ] **Step 9: Bind the test and checkpoint without committing**

Add the policy test to `test:macos-release`, run `./scripts/task.sh test:macos-release`, and inspect
the workflow diff for accidental triggers or job-wide secret/token environments. Do not commit.

---

### Task 6: Document setup, operation, recovery, and use cases

**Files:**
- Modify: `README.md:74-113`
- Create: `docs/e2e/use-cases/github-macos-release.md`
- Modify at ship time: `docs/CHANGELOG.md`

**Interfaces:**
- Documents Environment `release`, five exact secret names, same-maintainer review setting, workflow
  token permission, version preparation, manual operation, and residual-state inspection.
- Produces E2E case IDs `GMR-CLI-01` through `GMR-CLI-04` and `GMR-LIVE-01` through `GMR-LIVE-03`.

- [ ] **Step 1: Add a failing documentation-contract section to the workflow policy test**

Require `README.md` to contain all of:

```text
GitHub release automation
MACOS_CERTIFICATE_P12_BASE64
MACOS_CERTIFICATE_PASSWORD
APP_STORE_CONNECT_API_KEY_P8
APP_STORE_CONNECT_KEY_ID
APP_STORE_CONNECT_ISSUER_ID
Environment `release`
build/config.yml
```

Run the policy test and expect failure because the section is absent.

- [ ] **Step 2: Extend README without replacing the local release path**

Keep the current local `loqui-notary` instructions. Add a separate “GitHub release automation”
section covering:

1. Export the Developer ID identity with its private key as an encrypted `.p12`.
2. Base64 it without writing encoded material into the repository:

   ```bash
   base64 -i /absolute/path/DeveloperID.p12 | pbcopy
   ```

3. Create an App Store Connect API key with notarization access and retain the one-time `.p8`.
4. Create Environment `release`, restrict deployment to `main`, select a required reviewer, and
   leave “Prevent self-review” disabled for this single-maintainer repository.
5. Add the five exact Environment secrets and allow the requested `contents: write` workflow token.
6. Prepare a new version through a normal PR by changing `build/config.yml`, running
   `./scripts/patch-plists.sh`, and passing `./scripts/task.sh check`.
7. After merge, open **Actions → Release → Run workflow**, select `main`, inspect preflight, and
   approve the waiting Environment deployment.
8. Verify the tag target, DMG, checksum, and generated notes.
9. If publication reports ambiguous residual state, inspect with:

   ```bash
   gh release view v0.1.0
   git ls-remote --tags origin refs/tags/v0.1.0
   ```

   Deliberate deletion or repair remains a maintainer decision; the Action does not automate it.

- [ ] **Step 3: Write exact E2E journeys before executing them**

Create `docs/e2e/use-cases/github-macos-release.md` with these cases and observable outcomes:

- `GMR-CLI-01`: hermetic preflight succeeds only for equal checkout/dispatch/remote-main SHA.
- `GMR-CLI-02`: malformed/existing version states fail before a publication call.
- `GMR-CLI-03`: local CI-Keychain argument construction affects all and only three `notarytool`
  calls; local profile-only behavior remains.
- `GMR-CLI-04`: workflow policy proves manual-only trigger, protected secrets, least privilege,
  pinned Actions, phase order, and cleanup.
- `GMR-LIVE-01`: first default-branch run passes preflight, waits for approval, signs/notarizes,
  publishes exact tag/SHA/assets, and exposes sanitized 14-day evidence.
- `GMR-LIVE-02`: a second run at the unchanged version fails in preflight before Environment
  approval.
- `GMR-LIVE-03`: logs, summaries, Release assets, and evidence contain no `.p12`/`.p8` contents,
  password, decoded secret, or environment dump; sanitized evidence additionally contains no
  checkout path.

State explicitly that `GMR-LIVE-*` cannot execute until the workflow file exists on the default
branch; do not label those cases PASS from static evidence.

- [ ] **Step 4: Run documentation GREEN and checkpoint**

Run:

```bash
bash scripts/tests/github-release-workflow-test.sh
git diff --check -- README.md docs/e2e/use-cases/github-macos-release.md
```

Expected: documentation contract and whitespace checks pass. Do not add the changelog entry or
commit until the ship gate is genuinely green.

---

### Task 7: Verify, review, close the bootstrap gate, and ship

**Files:**
- Create/update: `docs/e2e/reports/2026-08-09-github-macos-release.md`
- Modify: `.workflow/state.md`
- Modify after verified success: `docs/CHANGELOG.md`
- Verify: every path changed in Tasks 1-6

**Interfaces:**
- Consumes: all focused tests, full project check, cross-engine code review, GitHub Environment
  configuration, and live workflow evidence.
- Produces: an honest pre-merge E2E limitation, an explicitly authorized bootstrap PR, post-merge
  live release evidence, and a separate evidence-closeout PR that makes the ship gate genuinely
  green.

- [ ] **Step 1: Run fresh focused verification**

Run:

```bash
bash scripts/tests/release-version-test.sh
bash scripts/tests/release-macos-test.sh
bash scripts/tests/github-release-test.sh
bash scripts/tests/github-release-workflow-test.sh
```

Expected: all four print `PASS` from a fresh invocation.

- [ ] **Step 2: Run static and full repository gates**

Run:

```bash
bash -n scripts/release-version.sh scripts/release-macos.sh scripts/github-release.sh \
  scripts/tests/release-version-test.sh scripts/tests/release-macos-test.sh \
  scripts/tests/github-release-test.sh scripts/tests/github-release-workflow-test.sh
shellcheck -x -s bash scripts/release-version.sh scripts/release-macos.sh scripts/github-release.sh \
  scripts/tests/release-version-test.sh scripts/tests/release-macos-test.sh \
  scripts/tests/github-release-test.sh scripts/tests/github-release-workflow-test.sh
./scripts/task.sh check
git diff --check
```

Expected: every command exits zero. Record command, timestamp, exit status, and salient output in the
E2E report.

- [ ] **Step 3: Obtain a clean cross-engine code review**

Use the project `review` workflow against the full diff. Require severity-tagged findings and resolve
or rebut with file/line evidence. Repeat focused/full verification after any code change. Check the
code-review gate only when no P0/P1/P2 remains.

- [ ] **Step 4: Configure and independently inspect the GitHub Environment**

The owner creates Environment `release`, limits it to `main`, configures the required reviewer,
adds the five secrets, and confirms Actions may grant `contents: write`. Record only secret names,
presence, reviewer policy, and branch policy—never values—in the E2E report.

- [ ] **Step 5: Record the unavoidable default-branch bootstrap status honestly**

Before merge, execute `GMR-CLI-01..04` and write their actual outcomes. Mark `GMR-LIVE-01..03` as
`FAIL_INFRA` with the precise GitHub constraint: a newly added `workflow_dispatch` workflow cannot
receive a dispatch until its file exists on the default branch.

At this point the E2E ship-gate is not green. Do not mark it PASS or claim the feature complete.
Present the evidence to the owner for the explicit bootstrap choice:

1. merge the statically/hermetically verified workflow with this documented one-time E2E exception,
   then execute `GMR-LIVE-*` immediately; or
2. configure a separate test repository/default branch and equivalent protected Environment, run
   `GMR-LIVE-*` there, then return to this repository.

The recommended choice is the scoped one-time merge exception because copying production signing
credentials to another repository increases credential surface. Record the owner's choice in
`.workflow/state.md`; do not infer approval.

- [ ] **Step 6: Run the deterministic gate and record the bootstrap exception**

Run the repository gate checker named by `finish-branch` after updating every locally applicable
box. For the recommended one-time bootstrap path, expect it to remain nonzero solely because live
E2E is `FAIL_INFRA`. Preserve that output in the report and obtain the owner's explicit approval to
make the bootstrap commit/PR despite this one declared gate. Do not rewrite `FAIL_INFRA` as PASS and
do not present the deterministic checker as green.

- [ ] **Step 7: Create the scoped bootstrap implementation commit**

Only after the explicit Step 6 approval, create the implementation commit. Do not add a ship-time
changelog entry yet because the live path is still unverified. Run:

```bash
git add .github/workflows/release.yml Taskfile.yml README.md \
  scripts/release-version.sh scripts/release-macos.sh scripts/github-release.sh \
  scripts/tests/release-version-test.sh scripts/tests/release-macos-test.sh \
  scripts/tests/github-release-test.sh scripts/tests/github-release-workflow-test.sh \
  docs/research/2026-08-08-github-macos-release-automation.md \
  docs/superpowers/specs/2026-08-09-github-macos-release-automation-design.md \
  docs/superpowers/plans/2026-08-09-github-macos-release-automation.md \
  docs/e2e/use-cases/github-macos-release.md \
  docs/e2e/reports/2026-08-09-github-macos-release.md \
  .workflow/state.md CONTINUITY.md
git diff --cached --check
git commit -m "feat: automate protected macOS releases"
```

Do not add unrelated dirty files and do not include a coauthor trailer.

- [ ] **Step 8: Push and create the explicitly authorized bootstrap PR**

After a final clean verification and explicit outward-action approval:

```bash
git push -u origin feat/github-macos-release
gh pr create --base main --head feat/github-macos-release \
  --title "Automate protected macOS releases" \
  --body-file /private/tmp/loqui-github-release-pr.md
```

The PR body lists architecture, tests, secret names/configuration, reviewer result, the still-open
E2E gate, the owner's recorded one-time exception, and the exact post-merge validation obligation.
Create the PR body through `apply_patch` or the approved artifact-writing mechanism, not shell
redirection. Do not claim that `finish-branch` or the complete ship gate passed.

- [ ] **Step 9: Execute and close the live default-branch E2E immediately after merge**

Once the workflow exists on `main`, dispatch **Release** at `main`, observe secret-free preflight,
approve Environment `release`, and execute `GMR-LIVE-01..03`. Verify:

```bash
gh release view "v$(./scripts/release-version.sh)" \
  --json url,isDraft,isPrerelease,tagName,targetCommitish,assets
git ls-remote origin "refs/tags/v$(./scripts/release-version.sh)"
```

Download the DMG and checksum to a unique temporary directory, run `shasum -a 256 -c`, scan logs for
secret material, and scan sanitized evidence for both secret material and checkout paths. Then
dispatch the unchanged version a second time and verify it fails before Environment approval.

If any live case fails, do not call the automation complete; open a focused hotfix workflow from
`main` and preserve the failed-run URL/evidence.

- [ ] **Step 10: Close E2E and the changelog in an evidence-only PR**

After all `GMR-LIVE-*` cases pass, update local `main`, immediately create
`docs/github-release-live-evidence`, and only then edit files:

```bash
git switch main
git pull --ff-only
git switch -c docs/github-release-live-evidence
```

Update the E2E report from `FAIL_INFRA` to its evidence-backed verdict, check the E2E box in
`.workflow/state.md`, update `CONTINUITY.md`, and add the ship-time `docs/CHANGELOG.md` entry for the
manual protected release, immutable tag/version rule, API-key notarization, and DMG/checksum assets.
Run the gate checker again; it must now exit zero. Then run:

```bash
git add docs/e2e/reports/2026-08-09-github-macos-release.md \
  docs/CHANGELOG.md .workflow/state.md CONTINUITY.md
git diff --cached --check
git commit -m "docs: record GitHub release verification"
git push -u origin docs/github-release-live-evidence
gh pr create --base main --head docs/github-release-live-evidence \
  --title "Record GitHub macOS release verification" \
  --body-file /private/tmp/loqui-github-release-evidence-pr.md
```

The evidence PR contains no secret values. The automation objective is complete only after this
closeout is reviewed and merged.
