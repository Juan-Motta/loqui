#!/usr/bin/env bash
# GitHub expressions and documentation literals intentionally remain unexpanded.
# shellcheck disable=SC2016
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-workflow-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
fake_bin="$tmp/fake-bin"
npm_calls="$tmp/npm-calls"
mkdir "$fake_bin"
cat >"$fake_bin/npm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'npm' >>"$NPM_CALLS"
printf ' <%s>' "$@" >>"$NPM_CALLS"
printf '\n' >>"$NPM_CALLS"
EOF
chmod +x "$fake_bin/npm"

: >"$npm_calls"
NPM_CALLS="$npm_calls" CI=true PATH="$fake_bin:$PATH" \
  "$repo_root/scripts/task.sh" -f common:install:frontend:deps:npm >/dev/null
assert_contains "$npm_calls" 'npm <ci>'
assert_not_contains "$npm_calls" 'npm <install>'

: >"$npm_calls"
NPM_CALLS="$npm_calls" CI=false PATH="$fake_bin:$PATH" \
  "$repo_root/scripts/task.sh" -f common:install:frontend:deps:npm >/dev/null
assert_contains "$npm_calls" 'npm <install>'
assert_not_contains "$npm_calls" 'npm <ci>'

workflow_path="$repo_root/.github/workflows/release.yml"
assert_file "$workflow_path"
workflow="$tmp/workflow.yml"
cp "$workflow_path" "$workflow"

extract_job() {
  job_name="$1"
  awk -v target="  $job_name:" '
    $0 == target {found=1}
    found && $0 != target && $0 ~ /^  [A-Za-z0-9_-]+:/ {exit}
    found {print}
  ' "$workflow"
}

extract_top_block() {
  block_name="$1"
  awk -v target="$block_name:" '
    $0 == target {found=1; print; next}
    found && $0 ~ /^[A-Za-z0-9_-]+:/ {exit}
    found {print}
  ' "$workflow"
}

preflight_job="$tmp/preflight-job.yml"
release_job="$tmp/release-job.yml"
on_block="$tmp/on-block.yml"
extract_job preflight >"$preflight_job"
extract_job release >"$release_job"
extract_top_block on >"$on_block"

assert_contains "$on_block" '  workflow_dispatch:'
assert_not_contains "$on_block" 'inputs:'
on_keys="$(awk '/^  [A-Za-z0-9_-]+:/{print}' "$on_block")"
assert_eq "$on_keys" '  workflow_dispatch:'
assert_contains "$workflow" 'group: macos-release'
assert_contains "$workflow" 'cancel-in-progress: false'
assert_contains "$preflight_job" 'runs-on: macos-26'
assert_contains "$preflight_job" 'timeout-minutes: 60'
assert_contains "$preflight_job" './scripts/github-release.sh preflight'
assert_not_contains "$preflight_job" 'environment:'
assert_not_contains "$preflight_job" 'contents: write'
assert_contains "$release_job" 'needs: preflight'
assert_contains "$release_job" 'runs-on: macos-26'
assert_contains "$release_job" 'timeout-minutes: 120'
assert_contains "$release_job" 'environment: release'
assert_contains "$release_job" 'contents: write'
assert_contains "$release_job" 'ref: ${{ needs.preflight.outputs.sha }}'
assert_contains "$release_job" '--check-drafts'
assert_contains "$release_job" './scripts/vendor-speech-sdk.sh'
assert_contains "$release_job" './scripts/task.sh release:macos'
assert_contains "$release_job" 'retention-days: 14'
if grep -E '^    if:' "$release_job" >/dev/null; then
  fail 'release job must not have a job-level if'
fi

assert_contains "$preflight_job" 'CI=true ./scripts/task.sh common:build:frontend'
assert_contains "$preflight_job" './scripts/task.sh check'
assert_not_contains "$preflight_job" 'APP_STORE_CONNECT_'
assert_not_contains "$preflight_job" 'MACOS_CERTIFICATE_'
assert_not_contains "$workflow" 'LOQUI_RELEASE_TEST_MODE'
assert_contains "$release_job" 'EXPECTED_DMG_NAME: ${{ needs.preflight.outputs.dmg_name }}'
assert_eq "$(grep -F -c -- '--expect-dmg-name "$EXPECTED_DMG_NAME"' "$release_job")" 2

for secret_name in \
  MACOS_CERTIFICATE_P12_BASE64 MACOS_CERTIFICATE_PASSWORD \
  APP_STORE_CONNECT_API_KEY_P8 APP_STORE_CONNECT_KEY_ID APP_STORE_CONNECT_ISSUER_ID; do
  secret_expression='${{ secrets.'"$secret_name"' }}'
  assert_eq "$(grep -F -c -- "$secret_expression" "$workflow")" 1
done

while IFS= read -r uses_line; do
  if [[ ! "$uses_line" =~ uses:[[:space:]]+[^@[:space:]]+@([0-9a-f]{40})[[:space:]]+#[[:space:]]+v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "Action is not pinned with a reviewed version comment: $uses_line"
  fi
done < <(grep -E '^[[:space:]]*uses:' "$workflow")

line_number() {
  pattern="$1"
  grep -n -F -- "$pattern" "$workflow" | head -1 | cut -d: -f1
}
revalidate_line="$(line_number 'name: Revalidate immutable release state')"
credentials_line="$(line_number 'name: Import protected Apple credentials')"
release_line="$(line_number 'name: Build, sign, and notarize DMG')"
failure_upload_line="$(line_number 'name: Upload sanitized notary failure evidence')"
prepare_line="$(line_number 'name: Prepare release assets')"
upload_line="$(line_number 'name: Upload sanitized release evidence')"
publish_line="$(line_number 'name: Publish GitHub Release')"
cleanup_line="$(line_number 'name: Clean temporary Apple credentials')"
[ "$revalidate_line" -lt "$credentials_line" ] || fail 'revalidation is not before credentials'
[ "$credentials_line" -lt "$release_line" ] || fail 'credentials are not before release build'
[ "$release_line" -lt "$failure_upload_line" ] || fail 'release build is not before failure evidence upload'
[ "$failure_upload_line" -lt "$prepare_line" ] || fail 'failure evidence upload is not before prepare'
[ "$release_line" -lt "$prepare_line" ] || fail 'release build is not before prepare'
[ "$prepare_line" -lt "$upload_line" ] || fail 'prepare is not before evidence upload'
[ "$upload_line" -lt "$publish_line" ] || fail 'evidence upload is not before publication'
[ "$publish_line" -lt "$cleanup_line" ] || fail 'publication is not before cleanup'

cleanup_block="$tmp/cleanup-block"
awk '/name: Clean temporary Apple credentials/ {found=1} found {print}' \
  "$release_job" >"$cleanup_block"
assert_contains "$cleanup_block" 'if: ${{ always() }}'
assert_contains "$cleanup_block" 'notary_failure_path="$RUNNER_TEMP/loqui-notary-failure-evidence"'
assert_contains "$cleanup_block" 'rm -rf -- "$notary_failure_path"'

failure_upload_block="$tmp/failure-upload-block"
awk '
  /name: Upload sanitized notary failure evidence/ {found=1}
  found && seen && /- name:/ {exit}
  found {print; seen=1}
' "$release_job" >"$failure_upload_block"
assert_contains "$release_job" 'LOQUI_NOTARY_FAILURE_DIR: ${{ runner.temp }}/loqui-notary-failure-evidence'
assert_contains "$failure_upload_block" 'if: ${{ failure() }}'
assert_contains "$failure_upload_block" 'path: ${{ runner.temp }}/loqui-notary-failure-evidence'
assert_contains "$failure_upload_block" 'if-no-files-found: warn'
assert_contains "$failure_upload_block" 'retention-days: 14'

for documentation_text in \
  'GitHub release automation' \
  'MACOS_CERTIFICATE_P12_BASE64' \
  'MACOS_CERTIFICATE_PASSWORD' \
  'APP_STORE_CONNECT_API_KEY_P8' \
  'APP_STORE_CONNECT_KEY_ID' \
  'APP_STORE_CONNECT_ISSUER_ID' \
  'Environment `release`' \
  'build/config.yml'; do
  assert_contains "$repo_root/README.md" "$documentation_text"
done

echo 'github-release-workflow-test: PASS'
