#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

production_script="$repo_root/scripts/github-release.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-github-release-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
fixture_root="$tmp/repo"
fake_bin="$tmp/fake-bin"
mkdir -p "$fixture_root/scripts" "$fixture_root/build" "$fake_bin"
cp "$production_script" "$fixture_root/scripts/github-release.sh"
cp "$repo_root/scripts/release-version.sh" "$fixture_root/scripts/release-version.sh"
printf '%s\n' 'info:' '  version: "0.1.0"' >"$fixture_root/build/config.yml"
chmod +x "$fixture_root/scripts/"*.sh
script="$fixture_root/scripts/github-release.sh"

sha=0123456789abcdef0123456789abcdef01234567
export GITHUB_REF=refs/heads/main
export GITHUB_SHA="$sha"
export GITHUB_REPOSITORY=Juan-Motta/loqui
export GH_TOKEN=fake-token
export GITHUB_OUTPUT="$tmp/outputs"
export GITHUB_STEP_SUMMARY="$tmp/summary"
export FAKE_HEAD_SHA="$sha"
export FAKE_MAIN_SHA="$sha"
export FAKE_CALLS="$tmp/calls"
export FAKE_GH_STATE="$tmp/gh-state"
export PATH="$fake_bin:/usr/bin:/bin"

cat >"$fake_bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'git' >>"$FAKE_CALLS"
printf ' <%s>' "$@" >>"$FAKE_CALLS"
printf '\n' >>"$FAKE_CALLS"
case "${1:-}" in
  rev-parse)
    [ "${2:-}" = HEAD ] || exit 97
    printf '%s\n' "${FAKE_HEAD_SHA:?}"
    ;;
  ls-remote)
    if [ "${FAKE_MAIN_QUERY_FAIL:-0}" = 1 ]; then exit 71; fi
    case "$*" in
      'ls-remote origin refs/heads/main')
        printf '%s\trefs/heads/main\n' "${FAKE_MAIN_SHA:?}"
        ;;
      'ls-remote --tags origin refs/tags/'*)
        if [ -n "${FAKE_TAG_SHA:-}" ]; then
          printf '%s\t%s\n' "$FAKE_TAG_SHA" "${*: -1}"
        fi
        ;;
      *) exit 97 ;;
    esac
    ;;
  *) exit 97 ;;
esac
EOF

cat >"$fake_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'gh' >>"$FAKE_CALLS"
printf ' <%s>' "$@" >>"$FAKE_CALLS"
printf '\n' >>"$FAKE_CALLS"
if [ "${1:-}" = --version ]; then
  if [ "${FAKE_GH_TOO_OLD:-0}" = 1 ]; then
    printf '%s\n' 'gh version 2.92.0 (fixture)'
  else
    printf '%s\n' 'gh version 2.96.0 (fixture)'
  fi
  exit 0
fi
if [ "${1:-}" = release ] && [ "${2:-}" = create ] && [ "${3:-}" = --help ]; then
  if [ "${FAKE_GH_NO_LATEST:-0}" = 1 ]; then printf '%s\n' 'FLAGS'; else printf '%s\n' '  --latest'; fi
  exit 0
fi
if [ "${1:-}" = release ] && [ "${2:-}" = create ]; then
  if [ "${FAKE_RELEASE_CREATE_FAIL:-0}" = 1 ]; then
    : >"$FAKE_GH_STATE/draft"
    exit 55
  fi
  : >"$FAKE_GH_STATE/published"
  exit 0
fi
if [ "${1:-}" = release ] && [ "${2:-}" = view ]; then
  [ -f "$FAKE_GH_STATE/published" ] || [ "${FAKE_RELEASE_VIEW_ANYWAY:-0}" = 1 ] || exit 1
  if [ "${FAKE_RELEASE_WRONG_ASSETS:-0}" = 1 ]; then
    assets='[{"name":"unexpected.zip"}]'
  else
    assets='[{"name":"Loqui-0.1.0-macos-arm64.dmg"},{"name":"Loqui-0.1.0-macos-arm64.dmg.sha256"},{"name":"Loqui-0.1.0-macos-arm64.zip"},{"name":"SHA256SUMS"}]'
  fi
  printf '{"url":"https://github.com/Juan-Motta/loqui/releases/tag/v0.1.0","isDraft":false,"isPrerelease":false,"tagName":"v0.1.0","targetCommitish":"%s","assets":%s}\n' \
    "$GITHUB_SHA" "$assets"
  exit 0
fi
if [ "${1:-}" = api ] && [ "${2:-}" = "repos/$GITHUB_REPOSITORY" ] && [ "${3:-}" = --silent ]; then
  [ "${FAKE_REPOSITORY_QUERY_FAIL:-0}" != 1 ] || exit 72
  exit 0
fi
if [ "${1:-}" = api ] && [ "${2:-}" = --paginate ]; then
  [ "${FAKE_RELEASE_LIST_FAIL:-0}" != 1 ] || exit 73
  if [ "${FAKE_DRAFT_EXISTS:-0}" = 1 ]; then printf 'v0.1.0\ttrue\n'; fi
  exit 0
fi
if [ "${1:-}" = api ] && [ "${2:-}" = -i ]; then
  if [ "${FAKE_RELEASE_EXISTS:-0}" = 1 ]; then printf '%s\n' 'HTTP/2.0 200 OK'; exit 0; fi
  if [ "${FAKE_RELEASE_QUERY_FAIL:-0}" = 1 ]; then printf '%s\n' 'HTTP/2.0 503 Service Unavailable'; exit 1; fi
  printf '%s\n' 'HTTP/2.0 404 Not Found'
  exit 1
fi
if [ "${1:-}" = api ] && [[ "${2:-}" = repos/*/git/ref/tags/* ]]; then
  attempts_file="$FAKE_GH_STATE/tag-attempts"
  attempts=0
  [ ! -f "$attempts_file" ] || attempts="$(cat "$attempts_file")"
  attempts=$((attempts + 1))
  printf '%s\n' "$attempts" >"$attempts_file"
  if [ "$attempts" -le "${FAKE_TAG_DELAY_ATTEMPTS:-0}" ]; then exit 1; fi
  [ "${FAKE_TAG_LOOKUP_FAIL:-0}" != 1 ] || exit 1
  printf 'commit\t%s\n' "${FAKE_PUBLISHED_TAG_SHA:-$GITHUB_SHA}"
  exit 0
fi
exit 97
EOF
chmod +x "$fake_bin/git" "$fake_bin/gh"

reset_fakes() {
  rm -rf "$FAKE_GH_STATE"
  mkdir -p "$FAKE_GH_STATE"
  : >"$FAKE_CALLS"
  : >"$GITHUB_OUTPUT"
  : >"$GITHUB_STEP_SUMMARY"
  unset FAKE_TAG_SHA FAKE_RELEASE_EXISTS FAKE_RELEASE_QUERY_FAIL FAKE_REPOSITORY_QUERY_FAIL
  unset FAKE_MAIN_QUERY_FAIL FAKE_GH_TOO_OLD FAKE_GH_NO_LATEST FAKE_DRAFT_EXISTS
  unset FAKE_RELEASE_LIST_FAIL FAKE_RELEASE_CREATE_FAIL FAKE_RELEASE_WRONG_ASSETS
  unset FAKE_RELEASE_VIEW_ANYWAY FAKE_TAG_DELAY_ATTEMPTS FAKE_TAG_LOOKUP_FAIL
  unset FAKE_PUBLISHED_TAG_SHA
}

reset_fakes
"$script" preflight
assert_contains "$GITHUB_OUTPUT" "sha=$sha"
assert_contains "$GITHUB_OUTPUT" 'version=0.1.0'
assert_contains "$GITHUB_OUTPUT" 'tag=v0.1.0'
assert_contains "$GITHUB_OUTPUT" 'dmg_name=Loqui-0.1.0-macos-arm64.dmg'
assert_contains "$GITHUB_OUTPUT" 'zip_name=Loqui-0.1.0-macos-arm64.zip'
assert_contains "$GITHUB_STEP_SUMMARY" '0.1.0'
assert_contains "$GITHUB_STEP_SUMMARY" "$sha"

reset_fakes
GITHUB_REF=refs/heads/feature run_expect_fail_msg 'requires refs/heads/main' "$script" preflight
reset_fakes
FAKE_HEAD_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  run_expect_fail_msg 'checkout HEAD does not match dispatch SHA' "$script" preflight
reset_fakes
FAKE_MAIN_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  run_expect_fail_msg 'remote main does not match dispatch SHA' "$script" preflight
reset_fakes
FAKE_TAG_SHA="$sha" run_expect_fail_msg 'tag already exists' "$script" preflight
reset_fakes
FAKE_RELEASE_EXISTS=1 run_expect_fail_msg 'GitHub Release already exists' "$script" preflight
reset_fakes
FAKE_DRAFT_EXISTS=1 "$script" preflight
assert_not_contains "$FAKE_CALLS" '<--paginate>'
reset_fakes
FAKE_DRAFT_EXISTS=1 run_expect_fail_msg 'draft GitHub Release already exists' \
  "$script" preflight --check-drafts
reset_fakes
FAKE_RELEASE_QUERY_FAIL=1 \
  run_expect_fail_msg 'cannot verify GitHub Release absence' "$script" preflight
reset_fakes
FAKE_RELEASE_LIST_FAIL=1 \
  run_expect_fail_msg 'cannot list GitHub Releases' "$script" preflight --check-drafts
reset_fakes
FAKE_REPOSITORY_QUERY_FAIL=1 \
  run_expect_fail_msg 'cannot verify GitHub repository access' "$script" preflight
reset_fakes
FAKE_MAIN_QUERY_FAIL=1 run_expect_fail_msg 'cannot read remote main' "$script" preflight
reset_fakes
FAKE_GH_TOO_OLD=1 run_expect_fail_msg 'gh 2.93.0 or newer is required' "$script" preflight
reset_fakes
FAKE_GH_NO_LATEST=1 run_expect_fail_msg 'gh release create lacks --latest' "$script" preflight
reset_fakes
run_expect_fail_msg 'version expectation mismatch' \
  "$script" preflight --expect-version 0.1.1
reset_fakes
run_expect_fail_msg 'tag expectation mismatch' "$script" preflight --expect-tag v0.1.1

fixture_root_physical="$(cd "$fixture_root" && pwd -P)"
release_root="$fixture_root_physical/bin/release"
dmg_name=Loqui-0.1.0-macos-arm64.dmg
zip_name=Loqui-0.1.0-macos-arm64.zip

reset_assets() {
  rm -rf "$release_root"
  mkdir -p "$release_root/evidence/0.1.0/submission-123"
  put_file "$release_root/$dmg_name" signed-notarized-fixture 644
  put_file "$release_root/$zip_name" signed-notarized-app-zip-fixture 644
  put_file "$release_root/evidence/0.1.0/submission-123/notary-log.json" \
    '{"status":"Accepted"}' 644
}

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
assert_file "$release_root/$dmg_name.sha256"
(cd "$release_root" && shasum -a 256 -c "$dmg_name.sha256")
assert_file "$release_root/SHA256SUMS"
(cd "$release_root" && shasum -a 256 -c SHA256SUMS)
assert_contains "$GITHUB_OUTPUT" "dmg_path=$release_root/$dmg_name"
assert_contains "$GITHUB_OUTPUT" "zip_path=$release_root/$zip_name"
assert_contains "$GITHUB_OUTPUT" "checksum_manifest_path=$release_root/SHA256SUMS"
assert_contains "$GITHUB_OUTPUT" "evidence_path=$release_root/evidence/0.1.0/submission-123"
assert_contains "$GITHUB_OUTPUT" 'submission_id=submission-123'

reset_assets
reset_fakes
run_expect_fail_msg 'DMG name expectation mismatch' "$script" prepare \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name Loqui-9.9.9-macos-arm64.dmg
reset_assets
reset_fakes
run_expect_fail_msg 'ZIP name expectation mismatch' "$script" prepare \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name" \
  --expect-zip-name Loqui-9.9.9-macos-arm64.zip
reset_assets
rm "$release_root/$dmg_name"
reset_fakes
run_expect_fail_msg 'missing release DMG' "$script" prepare \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"
reset_assets
rm "$release_root/$zip_name"
reset_fakes
run_expect_fail_msg 'missing release ZIP' "$script" prepare \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"
reset_assets
rm -rf "$release_root/evidence/0.1.0/submission-123"
reset_fakes
run_expect_fail_msg 'expected one evidence directory, found 0' "$script" prepare \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"
reset_assets
mkdir "$release_root/evidence/0.1.0/submission-456"
reset_fakes
run_expect_fail_msg 'expected one evidence directory, found 2' "$script" prepare \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
: >"$FAKE_CALLS"
LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS=0 "$script" publish \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"
assert_contains "$FAKE_CALLS" 'gh <release> <create> <v0.1.0>'
assert_contains "$FAKE_CALLS" '<--target> <0123456789abcdef0123456789abcdef01234567>'
assert_contains "$FAKE_CALLS" '<--title> <Loqui 0.1.0>'
assert_contains "$FAKE_CALLS" '<--generate-notes> <--latest>'
assert_contains "$GITHUB_STEP_SUMMARY" 'https://github.com/Juan-Motta/loqui/releases/tag/v0.1.0'

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
FAKE_TAG_DELAY_ATTEMPTS=2 LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS=0 "$script" publish \
  --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"
assert_eq "$(cat "$FAKE_GH_STATE/tag-attempts")" 3

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
FAKE_RELEASE_CREATE_FAIL=1 LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS=0 \
  run_expect_fail_msg 'publication failed; remote state preserved' "$script" publish \
    --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
FAKE_RELEASE_WRONG_ASSETS=1 LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS=0 \
  run_expect_fail_msg 'the Release is PUBLISHED — do not delete' "$script" publish \
    --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
FAKE_PUBLISHED_TAG_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS=0 \
  run_expect_fail_msg 'the Release is PUBLISHED — do not delete' "$script" publish \
    --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"

reset_assets
reset_fakes
"$script" prepare --sha "$sha" --version 0.1.0 --tag v0.1.0 \
  --expect-dmg-name "$dmg_name"
FAKE_TAG_LOOKUP_FAIL=1 LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS=0 \
  run_expect_fail_msg 'the Release is PUBLISHED — do not delete' "$script" publish \
    --sha "$sha" --version 0.1.0 --tag v0.1.0 --expect-dmg-name "$dmg_name"

if grep -E '^[[:space:]]*(gh release delete([[:space:]]|$)|gh release delete-asset([[:space:]]|$)|git push --delete([[:space:]]|$)|git push[^#]*:[[:space:]]*refs/tags/|git tag -d([[:space:]]|$)|gh api[^#]*(--method|-X)[[:space:]]+DELETE)' \
  "$production_script"; then
  fail 'production script contains an automatic deletion command'
fi

echo 'github-release-test: PASS'
