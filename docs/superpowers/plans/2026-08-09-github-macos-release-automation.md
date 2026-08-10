# GitHub macOS Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manually approved GitHub Action that releases the current `main` version as a signed,
notarized Apple Silicon DMG, then safely creates its version tag and public GitHub Release after all
assets are uploaded.

**Architecture:** Keep `./scripts/task.sh release:macos` as the Apple product authority. Add tested
repository seams for version metadata, GitHub preflight/publication, and an optional explicit notary
Keychain; a two-job `macos-26` workflow runs secret-free checks, waits on the protected `release`
Environment, rebuilds with temporary Apple credentials, and publishes last.

**Tech Stack:** Bash 3.2-compatible shell, Taskfile/Wails v3, Git/GitHub CLI, GitHub Actions,
GitHub-hosted `macos-26` arm64 runners, Go 1.25.0, Node 24/npm, Apple `security`, `codesign`,
`notarytool`, `stapler`, `spctl`, `hdiutil`, and SHA-256 via `shasum`.

## Global Constraints

- Task 1 starts only after `feat/developer-id-release` has a clean full-diff review, passes its
  second-Mac E2E, is committed through its ship gate, and is present on `main`. This prerequisite
  was satisfied by PR #1 and `main` commit `5d88c9c` on 2026-08-10.
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
- Existing tags and Releases are immutable automation inputs: never overwrite, move, resume, or
  automatically delete them. A human may delete only a verified partial, unannounced publication;
  a bad public release is superseded by a new patch version.
- Use GitHub-hosted Apple Silicon `macos-26`, Go 1.25.0, Node 24, the committed npm lockfile, and the
  repository-pinned Wails/Whisper/Azure versions.
- Run `scripts/vendor-speech-sdk.sh` in both jobs because `third_party/speech-sdk/` is gitignored and
  `release:macos` checks the framework before building.
- On the clean preflight runner, run `CI=true ./scripts/task.sh common:build:frontend` before
  invoking `check`; the shared dependency Task selects `npm ci` under CI and preserves local
  `npm install` behavior outside CI.
- Keep Apple secrets only in Environment `release`; preflight has `contents: read`, and only the
  protected job has `contents: write`.
- Pin every referenced Action to a full 40-character commit SHA with its reviewed version tag in a
  trailing comment, and verify each pin against the official Action repository before shipping.
- Preserve Bash 3.2 compatibility, quote all paths, keep shell tracing disabled, and never log secret
  values or environment dumps.
- The public GitHub Release contains exactly the DMG and `.sha256`. Sanitized evidence is a 14-day
  Actions artifact and must not be treated as confidential in this public repository.
- Do not claim live GitHub E2E before the workflow exists on the default branch. The bootstrap
  limitation and the selected closure path are recorded in Task 7.
- The first successful live E2E creates a permanent public release. Obtain separate owner
  acknowledgment of the exact version and commit as fit for publication immediately before dispatch.

## File map

| Path | Responsibility |
| --- | --- |
| `scripts/release-version.sh` | Parse and validate the sole release version |
| `scripts/tests/release-version-test.sh` | Hermetic version-parser matrix |
| `scripts/tests/testlib.sh` | Shared exact-failure helper used by negative shell contracts |
| `scripts/release-macos.sh` | Reuse the metadata reader and optionally target a CI Keychain |
| `scripts/tests/release-macos-test.sh` | Preserve local notarization behavior and prove explicit-Keychain arguments |
| `scripts/github-release.sh` | Validate GitHub state, prepare assets, publish, verify, and diagnose residual state |
| `scripts/tests/github-release-test.sh` | Fake-Git/GitHub lifecycle tests with no network or credentials |
| `.github/workflows/release.yml` | Two-job manual release orchestration and temporary credential lifecycle |
| `scripts/tests/github-release-workflow-test.sh` | Static workflow policy, permission, ordering, and pin contracts |
| `build/Taskfile.yml` | Select deterministic `npm ci` for CI package builds while preserving local installs |
| `Taskfile.yml` | Bind all new shell/policy tests into `test:macos-release` and `check` |
| `README.md` | Environment secrets, one-time GitHub setup, release operation, and recovery |
| `docs/e2e/use-cases/github-macos-release.md` | Live and pre-merge journeys with explicit bootstrap limitation |
| `docs/e2e/reports/2026-08-09-github-macos-release.md` | Execution evidence and final classifications |
| `docs/CHANGELOG.md` | Ship-time summary after all gates pass |
| `docs/research/2026-08-08-github-macos-release-automation.md` | Existing sourced external-technology research; update with reviewed API facts |
| `docs/superpowers/specs/2026-08-09-github-macos-release-automation-design.md` | Existing approved design; keep consistent with review corrections |
| `docs/superpowers/plans/2026-08-09-github-macos-release-automation.md` | This reviewed implementation plan |
| `.workflow/state.md` | Active gates, review record, bootstrap choice, and live status |
| `CONTINUITY.md` | Session handoff updated before each outward handoff |

---

### Task 1: Establish one strict release-version reader

**Files:**
- Create: `scripts/release-version.sh`
- Create: `scripts/tests/release-version-test.sh`
- Modify: `scripts/tests/testlib.sh`
- Modify: `scripts/release-macos.sh` (`read_release_version`, preflight reader, required scripts,
  and publication filename input)
- Modify: `scripts/tests/release-macos-test.sh` (preflight config/script fixtures, malformed-version
  diagnostic, and every `atomic_publish` call)
- Modify: `Taskfile.yml:34-46`

**Interfaces:**
- Consumes: `scripts/release-version.sh [--root ABSOLUTE_REPO_ROOT] [--dmg-name]`
- Produces: exactly one `MAJOR.MINOR.PATCH` line on stdout, or the canonical
  `Loqui-MAJOR.MINOR.PATCH-macos-arm64.dmg` with `--dmg-name`; nonzero with a prefixed diagnostic on
  stderr for missing, duplicate, empty, unquoted, single-quoted, prerelease, or malformed values.
- Produces for later tasks: `scripts/release-macos.sh` calls this script rather than owning another
  YAML parser.

- [ ] **Step 1: Add the failing parser matrix**

First add `run_expect_fail_msg EXPECTED_STDERR COMMAND...` to `scripts/tests/testlib.sh`. It invokes
the command in an explicit `if`, captures stdout/stderr plus the real status, rejects an unexpected
success and reserved fake status `97`, and requires the expected fixed stderr substring. It never
uses `set +e`. Then create `scripts/tests/release-version-test.sh`; the matrix is exact and includes
a valid config plus every rejected shape:

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
assert_eq "$("$script" --root "$valid" --dmg-name)" "Loqui-1.2.3-macos-arm64.dmg"

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
  run_expect_fail_msg "info.version must appear once as quoted MAJOR.MINOR.PATCH" \
    "$script" --root "$fixture"
done

run_expect_fail_msg "missing $tmp/absent-root/build/config.yml" \
  "$script" --root "$tmp/absent-root"
run_expect_fail_msg "root must be absolute" "$script" --root relative-root
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

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
output_mode=version
while [ "$#" -gt 0 ]; do
  case "$1" in
    --root) [ "$#" -ge 2 ] || exit 2; root="$2"; shift 2 ;;
    --dmg-name) output_mode=dmg-name; shift ;;
    *) echo "release-version: usage: $0 [--root REPO_ROOT] [--dmg-name]" >&2; exit 2 ;;
  esac
done

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

if [ "$output_mode" = dmg-name ]; then
  printf 'Loqui-%s-macos-arm64.dmg\n' "$version"
else
  printf '%s\n' "$version"
fi
```

Use `chmod +x scripts/release-version.sh scripts/tests/release-version-test.sh`.

- [ ] **Step 4: Add local-release integration tests and verify RED**

Before changing production, update the preflight config/script fixtures and malformed-version
diagnostic described below. Add the wrong-version publication case with the exact causal assertion:

```bash
run_expect_fail_msg "publication DMG name does not match version" \
  guarded_atomic_publish "$source_dmg" "$source_evidence" "$destination_root" \
  0.1.0 submission-123 Loqui-9.9.9-macos-arm64.dmg
```

Stub `atomic_publish`, call the real `phase_publish`, and assert the exact six literal arguments and
their order. Run `bash scripts/tests/release-macos-test.sh` and observe RED because the shared reader,
new argument, equality guard, and forwarding do not exist yet.

- [ ] **Step 5: Make the local release consume the shared reader and canonical name**

Remove `read_release_version()` from `scripts/release-macos.sh`. In `phase_preflight`, use:

```bash
if version="$("$release_root_dir/scripts/release-version.sh" --root "$release_root_dir")"; then
  :
else
  die "could not read release version"
  return 1
fi
```

Add `release-version.sh` to the release script's required executable list. Preserve the existing
`BASH_SOURCE` main guard. In `release-macos-test.sh`, copy/source the guarded script under a fixture
root, change the happy-path preflight fixture to the required quoted version, include an executable
fixture `release-version.sh`, update the old malformed-version expectation to the shared reader's
diagnostic, give `build/config.yml` duplicate `info.version` entries, call real `phase_preflight`,
and require it to stop with the shared reader's diagnostic before any build/notary/publication
sentinel. Also retain a static assertion that the shared reader is referenced.

Compute the canonical DMG name once through `release-version.sh --dmg-name` in `phase_publish` and
pass it as a new final argument to `atomic_publish`. The latter validates the basename and uses it
for the destination instead of redeclaring the filename template. Update all existing focused
`atomic_publish` calls with literal canonical names so their adversarial path/rollback coverage is
preserved. Its validation must require exact equality with
`Loqui-$publication_version-macos-arm64.dmg`. Although that equality repeats the shape as a
validation boundary, only the shared reader produces names. Task 4 consumes this shared behavior;
it does not reopen `release-macos.sh`.

- [ ] **Step 6: Bind and run the focused GREEN suite**

Add `./scripts/tests/release-version-test.sh` before `release-macos-test.sh` in
`test:macos-release`, then run:

```bash
bash scripts/tests/release-version-test.sh
bash scripts/tests/release-macos-test.sh
shellcheck -x -s bash scripts/release-version.sh scripts/release-macos.sh \
  scripts/tests/release-version-test.sh scripts/tests/release-macos-test.sh
```

Expected: both tests print `PASS`; ShellCheck exits zero.

- [ ] **Step 7: Record the task checkpoint without committing**

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
- Modify: `scripts/release-macos.sh` (notary auth initialization/validation and the `history`,
  `submit`, and `log` invocations)
- Modify: `scripts/tests/release-macos-test.sh` (source guard and exact auth-array probes)

**Interfaces:**
- Consumes: optional `LOQUI_NOTARY_KEYCHAIN=/absolute/path/to/keychain-db`.
- Produces: global `notary_auth_args=(--keychain-profile "$profile" [--keychain "$path"])` used
  by `notarytool history`, `submit`, and `log`.
- Preserves: when the variable is absent, the command arguments remain profile-only for local
  `loqui-notary` releases.

- [ ] **Step 1: Add failing exact-argument tests**

Before changing production code, first assert the existing `BASH_SOURCE` guard remains at the end of
`release-macos.sh`; sourcing is safe only because that guard prevents `main` from running. Extend
`release-macos-test.sh` with subshell probes that source the script under both environments:

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

```

Also call a focused `validate_notary_keychain` helper and assert that relative and missing absolute
paths fail before `notarytool` can run. Assert each of the `history`, `submit`, and `log` command
blocks contains the array exactly once rather than counting matching lines globally.

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
  case "$notary_keychain" in
    /*) ;;
    *) die "LOQUI_NOTARY_KEYCHAIN must be absolute"; return 1 ;;
  esac
  if [ ! -f "$notary_keychain" ]; then
    die "notary keychain does not exist: $notary_keychain"
    return 1
  fi
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
export PATH="$tmp/fake-bin:$PATH"
mkdir -p "$tmp/repo/scripts"
cp "$repo_root/scripts/github-release.sh" "$tmp/repo/scripts/github-release.sh"
cp "$repo_root/scripts/release-version.sh" "$tmp/repo/scripts/release-version.sh"
mkdir -p "$tmp/repo/build"
printf '%s\n' 'info:' '  version: "0.1.0"' >"$tmp/repo/build/config.yml"
script="$tmp/repo/scripts/github-release.sh"
chmod +x "$tmp/repo/scripts/"*.sh
```

The copied production CLI derives its own fixture root normally, so tests exercise the real command
entry point without a root/version environment variable or command-line seam. Its committed
`BASH_SOURCE` guard remains independently asserted because focused function probes source it.

The fake `git` implements only `rev-parse HEAD` and `ls-remote` for `main`/the version tag. The fake
`gh` implements `--version`, `release create --help`, `release view`, `release create`, the successful
repository probe, an optional paginated Releases listing that includes drafts for the protected
write-token revalidation, and an `api -i` published-Release lookup with an HTTP status line; every
invocation is appended with shell-escaped arguments to `FAKE_CALLS`.

Add these cases, resetting fake state between each:

```bash
reset_fakes() {
  rm -rf "$FAKE_GH_STATE"
  mkdir -p "$FAKE_GH_STATE"
  : >"$FAKE_CALLS"
}
```

Call `reset_fakes` before every case and require `$FAKE_CALLS` to be empty before the command under
test. Every unhandled fake subcommand exits with reserved status `97`.

```bash
"$script" preflight
assert_contains "$GITHUB_OUTPUT" "sha=$sha"
assert_contains "$GITHUB_OUTPUT" "version=0.1.0"
assert_contains "$GITHUB_OUTPUT" "tag=v0.1.0"
assert_contains "$GITHUB_OUTPUT" "dmg_name=Loqui-0.1.0-macos-arm64.dmg"
assert_contains "$GITHUB_STEP_SUMMARY" "0.1.0"
assert_contains "$GITHUB_STEP_SUMMARY" "$sha"

GITHUB_REF=refs/heads/feature run_expect_fail_msg "requires refs/heads/main" "$script" preflight
FAKE_HEAD_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  run_expect_fail_msg "checkout HEAD does not match dispatch SHA" "$script" preflight
FAKE_MAIN_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  run_expect_fail_msg "remote main does not match dispatch SHA" "$script" preflight
FAKE_TAG_SHA="$sha" run_expect_fail_msg "tag already exists" "$script" preflight
FAKE_RELEASE_EXISTS=1 run_expect_fail_msg "GitHub Release already exists" "$script" preflight
FAKE_RELEASE_QUERY_FAIL=1 \
  run_expect_fail_msg "cannot verify GitHub Release absence" "$script" preflight
FAKE_REPOSITORY_QUERY_FAIL=1 \
  run_expect_fail_msg "cannot verify GitHub repository access" "$script" preflight
FAKE_MAIN_QUERY_FAIL=1 run_expect_fail_msg "cannot read remote main" "$script" preflight
FAKE_GH_TOO_OLD=1 run_expect_fail_msg "gh 2.93.0 or newer is required" "$script" preflight
FAKE_GH_NO_LATEST=1 run_expect_fail_msg "gh release create lacks --latest" "$script" preflight
FAKE_DRAFT_EXISTS=1 "$script" preflight
assert_not_contains "$FAKE_CALLS" '<--paginate>'
FAKE_DRAFT_EXISTS=1 \
  run_expect_fail_msg "draft GitHub Release already exists" "$script" preflight --check-drafts
FAKE_RELEASE_LIST_FAIL=1 \
  run_expect_fail_msg "cannot list GitHub Releases" "$script" preflight --check-drafts
run_expect_fail_msg "version expectation mismatch" "$script" preflight --expect-version 0.1.1
run_expect_fail_msg "tag expectation mismatch" "$script" preflight --expect-tag v0.1.1
```

The repository probe must succeed first. For the release-absence probe, fake `gh api -i` returns an
HTTP 404 status line; HTTP 401/403/5xx, missing status, and command/network failures must fail closed.

- [ ] **Step 2: Run the focused test and verify RED**

Run `bash scripts/tests/github-release-test.sh`.

Expected: nonzero because `scripts/github-release.sh` does not exist.

- [ ] **Step 3: Implement command dispatch and invariant helpers**

Create the executable script with strict SHA/repository validation and command dispatch. It exposes
no test-only root/version seam:

```bash
#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
version_script="$root/scripts/release-version.sh"

die() { printf 'github-release: %s\n' "$*" >&2; exit 1; }
is_sha() { [[ "$1" =~ ^[0-9a-f]{40}$ ]]; }
write_output() {
  [ -n "${GITHUB_OUTPUT:-}" ] || return 0
  printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"
}

remote_main_sha() {
  if remote_output="$(git ls-remote origin refs/heads/main 2>&1)"; then
    :
  else
    die "cannot read remote main: $remote_output"
  fi
  main_sha="$(printf '%s\n' "$remote_output" |
    awk '$2 == "refs/heads/main" {print $1}')"
  is_sha "$main_sha" || die "remote main did not resolve to one commit"
  printf '%s\n' "$main_sha"
}

assert_tag_absent() {
  tag="$1"
  if tag_refs="$(git ls-remote --tags origin "refs/tags/$tag" 2>&1)"; then
    :
  else
    die "cannot verify tag absence for $tag: $tag_refs"
  fi
  [ -z "$tag_refs" ] || die "tag already exists: $tag"
}

assert_release_absent() {
  tag="$1"
  check_drafts="$2"
  if ! gh api "repos/$GITHUB_REPOSITORY" --silent >/dev/null; then
    die "cannot verify GitHub repository access"
  fi
  if [ "$check_drafts" -eq 1 ]; then
    if release_rows="$(gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" \
      --jq '.[] | [.tag_name, .draft] | @tsv' 2>&1)"; then
      :
    else
      die "cannot list GitHub Releases: $release_rows"
    fi
    if printf '%s\n' "$release_rows" | awk -F '\t' -v tag="$tag" '$1 == tag && $2 == "true" {found=1} END {exit !found}'; then
      die "draft GitHub Release already exists: $tag"
    fi
  fi
  if release_probe="$(gh api -i "repos/$GITHUB_REPOSITORY/releases/tags/$tag" 2>&1)"; then
    die "GitHub Release already exists: $tag"
  fi
  release_status="$(printf '%s\n' "$release_probe" |
    awk '/^HTTP\/[0-9.]+ [0-9][0-9][0-9]/{print $2; exit}')"
  [ "$release_status" = 404 ] || die "cannot verify GitHub Release absence for $tag"
}
```

Implement `preflight` so it:

1. parses the three optional expectations plus the flag-only `--check-drafts` with a rejecting
   `case` loop; draft enumeration is disabled in the read-only job and required in protected
   revalidation;
2. validates `GITHUB_REPOSITORY` as `owner/name`, both SHAs as 40 lowercase hex, and ref exactly;
3. parses the first `gh --version` line as numeric `MAJOR.MINOR.PATCH`, compares its components
   numerically, requires version 2.93.0 or newer, and verifies the structural
   `gh release create --help` flag `--latest` exists;
4. compares checked-out `HEAD`, dispatch SHA, remote `main`, and optional expected SHA;
5. reads version and derives tag/DMG name;
6. compares optional expected version/tag;
7. calls both fail-closed absence assertions, including draft enumeration only when requested;
8. writes the four outputs and non-secret preflight summary only after all checks pass.

Do not use `set +e`, prose diagnostics, or an unguarded pipeline to infer command state. Every
expected nonzero is captured through an explicit `if command; then ... else rc=$?; ... fi` branch.

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
  `scripts/github-release.sh prepare --sha SHA --version VERSION --tag TAG --expect-dmg-name NAME`.
- Produces outputs: absolute `dmg_path`, `checksum_path`, `evidence_path`, lowercase `checksum`, and
  `submission_id` from the sole evidence-directory basename.
- Consumes command:
  `scripts/github-release.sh publish --sha SHA --version VERSION --tag TAG --expect-dmg-name NAME`.
- Produces: a public non-prerelease GitHub Release, exact tag target, two exact assets, and a summary
  URL; on ambiguous failure it reports residual tag/Release state and never deletes.

- [ ] **Step 1: Extend tests with asset and publication RED cases**

Build a valid local publication fixture:

```bash
release_root="$tmp/repo/bin/release"
dmg_name=Loqui-0.1.0-macos-arm64.dmg
put_file "$release_root/$dmg_name" "signed-notarized-fixture" 644
put_file "$release_root/evidence/0.1.0/submission-123/notary-log.json" '{"status":"Accepted"}' 644
: >"$GITHUB_OUTPUT"
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
assert_file "$release_root/$dmg_name.sha256"
(cd "$release_root" && shasum -a 256 -c "$dmg_name.sha256")
assert_contains "$GITHUB_OUTPUT" "evidence_path=$release_root/evidence/0.1.0/submission-123"
assert_contains "$GITHUB_OUTPUT" "submission_id=submission-123"
```

Pass the literal `--expect-dmg-name "$dmg_name"` in the happy path. Then cover failures for a
mismatched expected DMG name, missing DMG, wrong tag/version pairing, zero evidence directories, two
evidence directories, and a corrupted checksum. Call `reset_fakes` before every case, recreate
mutable fixture files rather than inheriting state, and make every negative use
`run_expect_fail_msg` with its unique causal diagnostic; a nonzero status alone is insufficient.
Every unhandled fake Git/GitHub command exits with reserved status 97.

The one-directory contract is intentional: `release-macos.sh` performs exactly one `notarytool
submit`; its bounded retry applies only to `notarytool log`, and every Actions job has a fresh
filesystem. Rejecting two directories catches contaminated or ambiguous output rather than trying to
guess a winner.

For publication, make fake `gh release create` create `$FAKE_GH_STATE/published` and make subsequent
`release view --json` return:

```json
{"url":"https://github.com/Juan-Motta/loqui/releases/tag/v0.1.0","isDraft":false,"isPrerelease":false,"tagName":"v0.1.0","targetCommitish":"0123456789abcdef0123456789abcdef01234567","assets":[{"name":"Loqui-0.1.0-macos-arm64.dmg"},{"name":"Loqui-0.1.0-macos-arm64.dmg.sha256"}]}
```

Assert the logged create call has `--target` plus the exact SHA, `--title` plus `Loqui 0.1.0`,
`--generate-notes`, `--latest`, and both asset paths. Add negative cases for draft, prerelease,
wrong target, missing/extra asset, stale `main` at final preflight, create failure, authenticated tag
API returning the wrong SHA, and tag visibility delayed for two attempts versus missing after all
three attempts. Production defaults `LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS` to two; tests set
that narrowly scoped verification-delay variable to zero. It never changes build, credential, or
publication behavior.

Finally use anchored command-line regexes to assert production contains no invocation of these
destructive forms while still allowing README/summary prose to describe manual recovery:

```bash
if grep -E '^[[:space:]]*(gh release delete([[:space:]]|$)|gh release delete-asset([[:space:]]|$)|git push --delete([[:space:]]|$)|git push[^#]*:[[:space:]]*refs/tags/|git tag -d([[:space:]]|$)|gh api[^#]*(--method|-X)[[:space:]]+DELETE)' "$script"; then
  fail "production script contains an automatic deletion command"
fi
```

- [ ] **Step 2: Run the expanded suite and verify RED**

Run `bash scripts/tests/github-release-test.sh`.

Expected: failure on unknown `prepare`/`publish` commands.

- [ ] **Step 3: Implement deterministic asset preparation**

Add shared option parsing that requires SHA/version/tag/expected-DMG-name and verifies
`tag == v$version`, the metadata reader's current version, and the shared reader's canonical name
equals the caller's expected name. Implement `prepare` around exact paths:

```bash
if dmg_name="$("$version_script" --root "$root" --dmg-name)"; then
  :
else
  die "could not derive canonical DMG name"
fi
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

The existing `release-macos.sh` publication path obtains the same canonical filename through
`release-version.sh --dmg-name`; focused tests assert both production paths call that shared
producer. `atomic_publish` intentionally repeats the literal shape only as an equality-validation
boundary against its version argument; it does not produce an alternate name.

- [ ] **Step 4: Implement final revalidation and one-shot publication**

Before publication, run the same preflight invariants with exact expectations and `--check-drafts`
while suppressing duplicate output writes. Re-verify the checksum, then make one `gh` call:

The reviewed GitHub CLI 2.93.0 explicitly documents that this single command first creates a draft,
uploads assets with separate API calls, and publishes only after those uploads succeed. Preflight
requires that version or newer rather than coupling availability to mutable help prose. Therefore an
upload failure leaves an unannounced mutable draft rather than a
public partial Release; do not replace this with a hand-rolled second publication transaction.

```bash
gh release create "$tag" \
  "$dmg_path" \
  "$checksum_path" \
  --repo "$GITHUB_REPOSITORY" \
  --target "$sha" \
  --title "Loqui $version" \
  --generate-notes \
  --latest
```

Invoke `gh release create` in an explicit `if`; on failure, capture its original status, query the
exact remote tag, the paginated authenticated Releases API (including drafts), and `gh release view`
in their own checked `if` branches, append their presence/absence plus a recovery warning to
`GITHUB_STEP_SUMMARY`, and `exit` with the original status. Do not use `set +e` and do not mutate
remote state.

On success:

- query `gh api "repos/$GITHUB_REPOSITORY/git/ref/tags/$tag"` up to three times with a two-second
  delay; accept a lightweight `commit` object or dereference an annotated `tag` object, then require
  the terminal commit SHA to equal `sha`;
- query `gh release view --json url,isDraft,isPrerelease,tagName,targetCommitish,assets`;
- use `jq -e` to require `isDraft == false`, `isPrerelease == false`, exact tag/target, and the sorted
  asset names equal exactly `[DMG, checksum]`;
- append version, SHA, checksum, and URL to `GITHUB_STEP_SUMMARY`.

Every post-publication verification failure first queries and appends the Release URL, tag, and SHA
plus the explicit warning `the Release is PUBLISHED — do not delete; verify manually with gh release
view`. It then exits nonzero without mutation, so a red verification cannot be mistaken for a
pre-publication failure.

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
- Modify: `build/Taskfile.yml:19-30`
- Modify: `Taskfile.yml:34-44`

**Interfaces:**
- Consumes preflight outputs: `sha`, `version`, `tag`, `dmg_name`.
- Consumes Environment: `release` with five named secrets and a required reviewer.
- Produces: temporary `loqui-ci-notary` profile in an explicit Keychain, local release artifacts,
  14-day sanitized evidence artifact, and the final GitHub Release.
- Uses pinned Action commits verified against their official repositories on 2026-08-10:
  `actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09 # v5.1.0`,
  `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6.5.0`,
  `actions/setup-node@a0853c24544627f65ddf259abe73b1d18a591444 # v5.0.0`, and
  `actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2`.

- [ ] **Step 1: Write the failing workflow-policy test**

Create `scripts/tests/github-release-workflow-test.sh`. Extract the `preflight:` and `release:` job
blocks by their two-space job indentation, then assert:

```bash
assert_contains "$workflow" "workflow_dispatch:"
assert_not_contains "$workflow" "inputs:"
assert_contains "$workflow" "group: macos-release"
assert_contains "$workflow" "cancel-in-progress: false"
assert_contains "$preflight_job" "runs-on: macos-26"
assert_contains "$preflight_job" "./scripts/github-release.sh preflight"
assert_not_contains "$preflight_job" "environment:"
assert_not_contains "$preflight_job" "contents: write"
assert_contains "$preflight_job" "timeout-minutes: 60"
assert_contains "$release_job" "needs: preflight"
assert_contains "$release_job" "runs-on: macos-26"
assert_contains "$release_job" "environment: release"
assert_contains "$release_job" "contents: write"
assert_contains "$release_job" "timeout-minutes: 120"
assert_contains "$release_job" 'if: ${{ always() }}'
assert_contains "$release_job" "./scripts/vendor-speech-sdk.sh"
assert_contains "$release_job" "./scripts/task.sh release:macos"
assert_contains "$release_job" "retention-days: 14"
```

Extract the `on:` block by indentation and require its only key to be an empty `workflow_dispatch`;
deny `inputs:` and every other trigger rather than maintaining a trigger denylist. Explicitly assert
that none of the five Apple secret names occurs in preflight; `${{ github.token }}`
is allowed only on its `gh` step. Reject any four-space job-level `if:` in `release`, require exactly
that the named final cleanup step has `if: ${{ always() }}`. Do not assert a global count of
conditional steps. Require the release job's permission keys to equal exactly `contents: write`.
Require the release job checkout `ref:` to equal `${{ needs.preflight.outputs.sha }}` rather than a
moving branch or dispatch ref.

For every non-comment `uses:` line, extract the suffix after `@` and require exactly 40 lowercase
hex characters plus a reviewed `# vMAJOR.MINOR.PATCH` comment. Use line-number checks to prove order:

```text
revalidate < credential import < release:macos < prepare < upload-artifact < publish < cleanup
```

Also require exactly one `${{ secrets.NAME }}` expression for each of the five names, all in the
single credential-setup step `env:` block, and zero Apple-secret expressions in preflight. Require
`CI=true ./scripts/task.sh common:build:frontend` before the preflight `check` command. The real Task
behavior test proves this invokes `npm ci`; do not duplicate dependency installation in a separate
workflow step. The publisher exposes no root/version test seams, so there are no test-only
environment controls for the workflow to misuse. Require `EXPECTED_DMG_NAME` to be mapped from
`needs.preflight.outputs.dmg_name` and passed through `--expect-dmg-name` in both prepare and publish.

The same test also executes the real `common:install:frontend:deps:npm` Task with `-f` and a
narrow fake `npm` that records arguments. Under `CI=true`, require the install command to be exactly
`npm ci`; under `CI=false`, require exactly `npm install`. Ignore the separate `npm version`
precondition call. This proves the final `release:macos` build remains lockfile-deterministic in CI
while preserving the existing local developer behavior.

- [ ] **Step 2: Run the policy test and verify RED**

Run `bash scripts/tests/github-release-workflow-test.sh`.

Expected: failure because `.github/workflows/release.yml` does not exist; after adding only an empty
fixture workflow, RED must also show the current Task invokes `npm install` under `CI=true`.

- [ ] **Step 3: Add the manual trigger, preflight job, and immutable outputs**

First change `build/Taskfile.yml` so the npm dependency task runs `npm ci` when `CI=true` and the
existing `npm install` otherwise. Keep its directory, sources, generates, and precondition intact.
Rerun the focused test to make this behavior GREEN while the missing workflow assertions remain RED.

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
    timeout-minutes: 60
    outputs:
      sha: ${{ steps.release-metadata.outputs.sha }}
      version: ${{ steps.release-metadata.outputs.version }}
      tag: ${{ steps.release-metadata.outputs.tag }}
      dmg_name: ${{ steps.release-metadata.outputs.dmg_name }}
```

Add pinned checkout with `ref: ${{ github.sha }}`, `fetch-depth: 0`, and
`persist-credentials: false`; pinned Go setup with `go-version: 1.25.0` and `cache: false`; pinned
Node setup with `node-version: 24`. For CMake and jq, use checked `brew list --formula` branches and
install only a missing formula; record `cmake --version`, `jq --version`, and `gh --version` in the
step summary. The official `macos-26` arm64 image manifest records GitHub CLI 2.96.0 as of
2026-08-10, above the repository's tested minimum 2.93.0; the script still fails closed if a future
image violates that baseline. Run `./scripts/vendor-speech-sdk.sh`.

Run `./scripts/github-release.sh preflight` in step `id: release-metadata`, with `GH_TOKEN` scoped to
that step as `${{ github.token }}`. Then run `CI=true ./scripts/task.sh common:build:frontend` and
`./scripts/task.sh check` in that order. Do not reference Environment or Apple secrets.

- [ ] **Step 4: Add protected release setup and stale-run revalidation**

Define the second job:

```yaml
  release:
    needs: preflight
    runs-on: macos-26
    timeout-minutes: 120
    environment: release
    permissions:
      contents: write
```

Repeat checkout/toolchain/dependency setup at the exact preflight SHA. Before credential setup, map
the immutable outputs into step-local environment variables and run (never interpolate `${{ }}`
directly into shell source):

```bash
./scripts/github-release.sh preflight \
  --expect-sha "$EXPECTED_SHA" \
  --expect-version "$EXPECTED_VERSION" \
  --expect-tag "$EXPECTED_TAG" \
  --check-drafts
```

Scope `GH_TOKEN: ${{ github.token }}` only to this revalidation step. The release job itself has no
job-level `if`; GitHub's default failed-`needs` behavior must prevent it from running when preflight
fails.

- [ ] **Step 5: Implement temporary credential setup without logging secrets**

Map the five Environment secrets into the setup step's environment. Use exact runner-temp paths,
generate the Keychain password in memory, and export only non-secret paths:

```bash
set -euo pipefail
umask 077
certificate_path="$RUNNER_TEMP/loqui-developer-id.p12"
api_key_path="$RUNNER_TEMP/loqui-notary-api-key.p8"
keychain_path="$RUNNER_TEMP/loqui-ci.keychain-db"
keychain_password="$(openssl rand -base64 32)"
echo "::add-mask::$keychain_password"

printf '%s' "$MACOS_CERTIFICATE_P12_BASE64" | base64 --decode >"$certificate_path"
printf '%s\n' "$APP_STORE_CONNECT_API_KEY_P8" >"$api_key_path"
[ -s "$certificate_path" ] || { echo "decoded certificate is empty" >&2; exit 1; }
[ -s "$api_key_path" ] || { echo "notary API key is empty" >&2; exit 1; }
if ! openssl pkey -in "$api_key_path" -noout >/dev/null 2>&1; then
  echo "notary API private key is invalid" >&2
  exit 1
fi
chmod 600 "$certificate_path" "$api_key_path"

security create-keychain -p "$keychain_password" "$keychain_path"
security set-keychain-settings -lut 21600 "$keychain_path"
security unlock-keychain -p "$keychain_password" "$keychain_path"
if ! security import "$certificate_path" -P "$MACOS_CERTIFICATE_PASSWORD" \
  -A -t cert -f pkcs12 -k "$keychain_path"; then
  echo "Developer ID archive could not be imported" >&2
  exit 1
fi
security set-key-partition-list -S apple-tool:,apple: -s \
  -k "$keychain_password" "$keychain_path"

original_keychains=()
while IFS= read -r keychain_line; do
  keychain_line="${keychain_line#*\"}"
  keychain_line="${keychain_line%\"*}"
  if [ -n "$keychain_line" ]; then
    original_keychains+=("$keychain_line")
  fi
done < <(security list-keychains -d user)
[ "${#original_keychains[@]}" -gt 0 ] || { echo "no original user Keychains found" >&2; exit 1; }
security list-keychains -d user -s "$keychain_path" "${original_keychains[@]}"

xcrun notarytool store-credentials loqui-ci-notary \
  --key "$api_key_path" \
  --key-id "$APP_STORE_CONNECT_KEY_ID" \
  --issuer "$APP_STORE_CONNECT_ISSUER_ID" \
  --keychain "$keychain_path"

rm -f -- "$certificate_path" "$api_key_path"
```

The step name contains no secret interpolation, and `set -x` is forbidden. `security import`
necessarily receives the `.p12` password through its `-P` argument; the accepted residual is limited
to this single-tenant ephemeral runner, and the value is masked. Apple's `security` commands also
necessarily receive the generated ephemeral Keychain password through `-p`/`-k`; it is masked,
exists only in the protected job process, and is never persisted as a secret. Delete the decoded `.p12` and `.p8`
immediately after their material is imported into the temporary Keychain; the final cleanup step
repeats those exact paths only as a failure fallback.

- [ ] **Step 6: Invoke the existing release and prepare/upload evidence**

Run the product authority with only its non-secret profile/path inputs:

```bash
LOQUI_NOTARY_PROFILE=loqui-ci-notary \
LOQUI_NOTARY_KEYCHAIN="$RUNNER_TEMP/loqui-ci.keychain-db" \
./scripts/task.sh release:macos
```

Then map the four exact preflight outputs into step-local `EXPECTED_SHA`, `EXPECTED_VERSION`,
`EXPECTED_TAG`, and `EXPECTED_DMG_NAME`, and run `scripts/github-release.sh prepare` with those shell variables in a step
with `id: release-assets`. Upload only `${{ steps.release-assets.outputs.evidence_path }}` via the pinned
artifact action, naming it `loqui-release-evidence-${{ needs.preflight.outputs.tag }}`, with
`retention-days: 14` and `if-no-files-found: error`.

Do not upload a broad failure glob. Set `LOQUI_NOTARY_FAILURE_DIR` to the exact runner-temporary
failure-evidence directory. The release script normalizes its copied JSON, scans for original paths
and secret fields, and atomically exposes the configured directory only after those checks pass.
Upload that exact path in a failure-only pinned artifact step with 14-day retention and
`if-no-files-found: warn`; cleanup removes the same exact path after the upload attempt.

- [ ] **Step 7: Publish last and add unconditional safe cleanup**

Map the same four exact preflight outputs into step-local variables and call
`scripts/github-release.sh publish` with those variables plus step-scoped
`GH_TOKEN: ${{ github.token }}`. It is the only remote write; no `${{ }}` expression is interpolated
inside its shell source.

Add the final `if: ${{ always() }}` step. Disable `errexit` for best-effort cleanup and accept only exact
runner-temp paths:

```bash
set +e
case "$RUNNER_TEMP" in /*) ;; *) echo "RUNNER_TEMP is not absolute" >&2; exit 1 ;; esac
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
- Produces E2E case IDs `GMR-CLI-01` through `GMR-CLI-04` and `GMR-LIVE-01` through `GMR-LIVE-04`.

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

   Before uploading, validate the `.p12` locally with `/usr/bin/openssl pkcs12 -noout` (the Apple
   system parser accepts legacy algorithms used by Keychain Access) and validate that the base64
   decodes to a non-empty file in a private temporary directory. CI uses Apple's `security import`
   as the authoritative parser rather than Homebrew OpenSSL. Never paste either form into a log,
   issue, PR, or repository file.

3. Create a **Team** App Store Connect API key with notarization access and retain the one-time
   `.p8`; the workflow intentionally passes `--issuer`, which is required for Team keys and must not
   be used with an Individual API key.
4. Create Environment `release`, restrict deployment to `main`, select a required reviewer, and
   leave “Prevent self-review” disabled for this single-maintainer repository. State plainly that
   this is an operator confirmation, not separation of duties.
5. Add the five exact Environment secrets and allow the requested `contents: write` workflow token.
6. Prepare a new version through a normal PR by changing `build/config.yml`, running
   `./scripts/patch-plists.sh`, and passing `./scripts/task.sh check`.
7. After merge, open **Actions → Release → Run workflow**, select `main`, inspect preflight, and
   approve the waiting Environment deployment.
   The repository-wide concurrency group serializes releases: do not park an approval indefinitely,
   and do not dispatch multiple replacement runs because GitHub may cancel an older pending run even
   when `cancel-in-progress` is false.
8. Verify the tag target, DMG, checksum, and generated notes.
9. If publication reports ambiguous residual state, inspect with:

   ```bash
   gh release view v0.1.0
   git ls-remote --tags origin refs/tags/v0.1.0
   ```

   The Action never deletes. If and only if inspection proves the state is partial and unannounced,
   a maintainer may deliberately run `gh release delete v0.1.0 --cleanup-tag --yes` and re-dispatch.
   Never delete a public release; supersede a bad public build with a new patch version.

- [ ] **Step 3: Write exact E2E journeys before executing them**

Create `docs/e2e/use-cases/github-macos-release.md` with these cases and observable outcomes:

- `GMR-CLI-01`: hermetic preflight succeeds only for equal checkout/dispatch/remote-main SHA; the
  read-only mode skips drafts and protected `--check-drafts` revalidation rejects them.
- `GMR-CLI-02`: malformed/existing version states fail before a publication call.
- `GMR-CLI-03`: local CI-Keychain argument construction affects all and only three `notarytool`
  calls; local profile-only behavior remains.
- `GMR-CLI-04`: workflow policy proves manual-only trigger, protected secrets, least privilege,
  pinned Actions, phase order, and cleanup.
- `GMR-LIVE-01`: first default-branch run passes preflight, waits for approval, signs/notarizes,
  publishes exact tag/SHA/assets, and exposes sanitized 14-day evidence, only after the owner
  acknowledges the exact version and commit as fit for permanent public distribution.
- `GMR-LIVE-02`: a second run at the unchanged version fails in preflight before Environment
  approval.
- `GMR-LIVE-03`: logs, summaries, Release assets, and evidence contain no `.p12`/`.p8` contents,
  password, decoded secret, or environment dump; sanitized evidence additionally contains no
  checkout path.
- `GMR-LIVE-04`: a dispatch from a throwaway branch fails the repository preflight's exact
  `refs/heads/main` invariant and never reaches the protected job. The Environment's exact `main`
  deployment policy is independently verified through GitHub's API; this case does not claim to
  exercise a policy that preflight intentionally prevents it from reaching.

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

Also resolve each reviewed Action tag through the official GitHub API and require its commit to equal
the pinned SHA. Expected: every command exits zero. Record command, timestamp, exit status, resolved
Action tags/SHAs, and salient output in the E2E report.

- [ ] **Step 3: Obtain a clean cross-engine code review**

Use the project `review` workflow against the full diff. Require severity-tagged findings and resolve
or rebut with file/line evidence. Repeat focused/full verification after any code change. Check the
code-review gate only when no P0/P1/P2 remains.

- [ ] **Step 4: Configure and independently inspect the GitHub Environment**

Create/update Environment `release`, limit it to the custom `main` branch policy, configure the
required reviewer with `prevent_self_review: false`, add the five secrets, and confirm Actions may
grant job-scoped `contents: write`. Independently inspect and save non-secret API output from:

```bash
gh api repos/Juan-Motta/loqui/environments/release \
  --jq '{protection_rules: .protection_rules, branch_policy: .deployment_branch_policy}'
gh api repos/Juan-Motta/loqui/environments/release/deployment-branch-policies
gh secret list --env release --repo Juan-Motta/loqui
gh secret list --repo Juan-Motta/loqui
```

Require a required-reviewer rule, custom-branch policy, exactly one branch rule named `main`, and all
five Environment secret names. Require none of the five names at repository scope. This repository
is owned by a personal account rather than an organization, so organization-scoped Actions secrets
do not apply. Record only names/presence/policy—never values—in the E2E report.

- [ ] **Step 5: Record the unavoidable default-branch bootstrap status honestly**

Before merge, execute `GMR-CLI-01..04` and write their actual outcomes. Mark `GMR-LIVE-01..04` as
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
changelog entry yet because the live path is still unverified. Verify every listed path exists, then
run:

```bash
git add .github/workflows/release.yml Taskfile.yml build/Taskfile.yml README.md \
  scripts/release-version.sh scripts/release-macos.sh scripts/github-release.sh \
  scripts/tests/testlib.sh scripts/tests/release-version-test.sh scripts/tests/release-macos-test.sh \
  scripts/tests/github-release-test.sh scripts/tests/github-release-workflow-test.sh \
  docs/research/2026-08-08-github-macos-release-automation.md \
  docs/superpowers/specs/2026-08-09-github-macos-release-automation-design.md \
  docs/superpowers/plans/2026-08-09-github-macos-release-automation.md \
  docs/e2e/use-cases/github-macos-release.md \
  docs/e2e/reports/2026-08-10-github-macos-release.md \
  CONTINUITY.md
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

Once the workflow exists on `main`, fetch the merged default branch and derive the version from that
exact remote state. Before dispatch, tell the owner that the next action permanently publishes the
exact version and SHA and obtain a separate acknowledgment that it is fit for public distribution.
Then dispatch **Release** at `main`, observe secret-free preflight, approve Environment `release`, and
execute `GMR-LIVE-01..04`. Verify:

```bash
gh release view "v$(./scripts/release-version.sh)" \
  --json url,isDraft,isPrerelease,tagName,targetCommitish,assets
git ls-remote origin "refs/tags/v$(./scripts/release-version.sh)"
```

Download the DMG and checksum to a unique temporary directory, run `shasum -a 256 -c`, scan logs for
secret material, and scan sanitized evidence for both secret material and checkout paths. Apply a
controlled `com.apple.quarantine` attribute to the CLI-downloaded DMG (the CLI itself is not expected
to add browser quarantine), assess the DMG with Gatekeeper, mount it read-only, and run `codesign
--verify --deep --strict` plus Gatekeeper assessment on the contained app before detaching it. Then
dispatch the unchanged version a second time and verify it fails before Environment approval. Use a
throwaway branch based on the released SHA to confirm the exact-main repository preflight blocks a
non-`main` dispatch before the protected job; independently re-read the Environment branch-policy
API as evidence for its exact `main` rule. Remove the throwaway branch only after recording the run
evidence.

If any live case fails, do not call the automation complete; open a focused hotfix workflow from
`main` and preserve the failed-run URL/evidence.

- [ ] **Step 10: Close E2E and the changelog in an evidence-only PR**

After all `GMR-LIVE-*` cases pass, fetch the merged remote default branch, immediately create
`docs/github-release-live-evidence` from `origin/main`, and only then edit files:

```bash
git fetch origin
git switch -c docs/github-release-live-evidence origin/main
```

Update the E2E report from `FAIL_INFRA` to its evidence-backed verdict, check the E2E box in
`.workflow/state.md`, update `CONTINUITY.md`, and add the ship-time `docs/CHANGELOG.md` entry for the
manual protected release, immutable tag/version rule, API-key notarization, and DMG/checksum assets.
Run the gate checker again; it must now exit zero. Then run:

```bash
git add docs/e2e/reports/2026-08-09-github-macos-release.md \
  docs/CHANGELOG.md CONTINUITY.md
git diff --cached --check
git commit -m "docs: record GitHub release verification"
git push -u origin docs/github-release-live-evidence
gh pr create --base main --head docs/github-release-live-evidence \
  --title "Record GitHub macOS release verification" \
  --body-file /private/tmp/loqui-github-release-evidence-pr.md
```

The evidence PR contains no secret values. The automation objective is complete only after this
closeout is reviewed and merged.
