#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
version_script="$root/scripts/release-version.sh"

die() {
  printf 'github-release: %s\n' "$*" >&2
  exit 1
}

is_sha() {
  [[ "$1" =~ ^[0-9a-f]{40}$ ]]
}

write_output() {
  [ "${preflight_quiet:-0}" != 1 ] || return 0
  [ -n "${GITHUB_OUTPUT:-}" ] || return 0
  printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"
}

write_summary() {
  [ "${preflight_quiet:-0}" != 1 ] || return 0
  [ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
  printf '%s\n' "$1" >>"$GITHUB_STEP_SUMMARY"
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
  checked_tag="$1"
  if tag_refs="$(git ls-remote --tags origin "refs/tags/$checked_tag" 2>&1)"; then
    :
  else
    die "cannot verify tag absence for $checked_tag: $tag_refs"
  fi
  [ -z "$tag_refs" ] || die "tag already exists: $checked_tag"
}

assert_release_absent() {
  checked_tag="$1"
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
    if awk -F '\t' -v tag="$checked_tag" \
      '$1 == tag && $2 == "true" {found=1} END {exit !found}' <<<"$release_rows"; then
      die "draft GitHub Release already exists: $checked_tag"
    fi
  fi
  if release_probe="$(gh api -i \
    "repos/$GITHUB_REPOSITORY/releases/tags/$checked_tag" 2>&1)"; then
    die "GitHub Release already exists: $checked_tag"
  fi
  release_status="$(printf '%s\n' "$release_probe" |
    awk '/^HTTP\/[0-9.]+ [0-9][0-9][0-9]/{print $2; exit}')"
  [ "$release_status" = 404 ] || die "cannot verify GitHub Release absence for $checked_tag"
}

require_supported_gh() {
  if gh_output="$(gh --version 2>&1)"; then
    :
  else
    die "cannot read gh version: $gh_output"
  fi
  gh_line="$(printf '%s\n' "$gh_output" | sed -n '1p')"
  if [[ "$gh_line" =~ ^gh[[:space:]]version[[:space:]]([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
    gh_major="${BASH_REMATCH[1]}"
    gh_minor="${BASH_REMATCH[2]}"
  else
    die "cannot parse gh version: $gh_line"
  fi
  if [ "$gh_major" -lt 2 ] \
    || { [ "$gh_major" -eq 2 ] && [ "$gh_minor" -lt 93 ]; }; then
    die "gh 2.93.0 or newer is required"
  fi
  if gh_help="$(gh release create --help 2>&1)"; then
    :
  else
    die "cannot inspect gh release create"
  fi
  if ! grep -E '(^|[[:space:]])--latest([=[:space:]]|$)' <<<"$gh_help" >/dev/null; then
    die "gh release create lacks --latest"
  fi
}

preflight() {
  expected_sha=""
  expected_version=""
  expected_tag=""
  check_drafts=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --expect-sha)
        [ "$#" -ge 2 ] || die "missing value for --expect-sha"
        expected_sha="$2"
        shift 2
        ;;
      --expect-version)
        [ "$#" -ge 2 ] || die "missing value for --expect-version"
        expected_version="$2"
        shift 2
        ;;
      --expect-tag)
        [ "$#" -ge 2 ] || die "missing value for --expect-tag"
        expected_tag="$2"
        shift 2
        ;;
      --check-drafts)
        check_drafts=1
        shift
        ;;
      *) die "unknown preflight option: $1" ;;
    esac
  done

  [ "${GITHUB_REF:-}" = refs/heads/main ] \
    || die "release requires refs/heads/main"
  [[ "${GITHUB_REPOSITORY:-}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] \
    || die "invalid GITHUB_REPOSITORY"
  is_sha "${GITHUB_SHA:-}" || die "invalid GITHUB_SHA"
  [ -n "${GH_TOKEN:-}" ] || die "GH_TOKEN is required"
  require_supported_gh

  if checkout_sha="$(git rev-parse HEAD 2>&1)"; then
    :
  else
    die "cannot read checkout HEAD: $checkout_sha"
  fi
  is_sha "$checkout_sha" || die "checkout HEAD is not a full commit SHA"
  [ "$checkout_sha" = "$GITHUB_SHA" ] \
    || die "checkout HEAD does not match dispatch SHA"
  current_main_sha="$(remote_main_sha)"
  [ "$current_main_sha" = "$GITHUB_SHA" ] \
    || die "remote main does not match dispatch SHA"
  if [ -n "$expected_sha" ]; then
    is_sha "$expected_sha" || die "invalid SHA expectation"
    [ "$GITHUB_SHA" = "$expected_sha" ] || die "SHA expectation mismatch"
  fi

  if version="$("$version_script" --root "$root")"; then
    :
  else
    die "cannot read release version"
  fi
  if dmg_name="$("$version_script" --root "$root" --dmg-name)"; then
    :
  else
    die "cannot derive release DMG name"
  fi
  tag="v$version"
  if [ -n "$expected_version" ] && [ "$version" != "$expected_version" ]; then
    die "version expectation mismatch"
  fi
  if [ -n "$expected_tag" ] && [ "$tag" != "$expected_tag" ]; then
    die "tag expectation mismatch"
  fi

  assert_tag_absent "$tag"
  assert_release_absent "$tag" "$check_drafts"

  write_output sha "$GITHUB_SHA"
  write_output version "$version"
  write_output tag "$tag"
  write_output dmg_name "$dmg_name"
  write_summary "### Loqui release preflight"
  write_summary "- Version: $version"
  write_summary "- Tag: $tag"
  write_summary "- Commit: $GITHUB_SHA"
}

parse_release_args() {
  release_sha=""
  release_version=""
  release_tag=""
  expected_dmg_name=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --sha)
        [ "$#" -ge 2 ] || die "missing value for --sha"
        release_sha="$2"
        shift 2
        ;;
      --version)
        [ "$#" -ge 2 ] || die "missing value for --version"
        release_version="$2"
        shift 2
        ;;
      --tag)
        [ "$#" -ge 2 ] || die "missing value for --tag"
        release_tag="$2"
        shift 2
        ;;
      --expect-dmg-name)
        [ "$#" -ge 2 ] || die "missing value for --expect-dmg-name"
        expected_dmg_name="$2"
        shift 2
        ;;
      *) die "unknown release option: $1" ;;
    esac
  done

  is_sha "$release_sha" || die "invalid release SHA"
  [[ "$release_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
    || die "invalid release version"
  [ "$release_tag" = "v$release_version" ] || die "tag/version mismatch"
  [ "${GITHUB_SHA:-}" = "$release_sha" ] || die "release SHA does not match dispatch SHA"

  if current_version="$("$version_script" --root "$root")"; then
    :
  else
    die "cannot read current release version"
  fi
  [ "$current_version" = "$release_version" ] || die "release version is stale"
  if canonical_dmg_name="$("$version_script" --root "$root" --dmg-name)"; then
    :
  else
    die "cannot derive canonical DMG name"
  fi
  [ "$expected_dmg_name" = "$canonical_dmg_name" ] \
    || die "DMG name expectation mismatch"
}

resolve_release_assets() {
  dmg_path="$root/bin/release/$canonical_dmg_name"
  checksum_path="$dmg_path.sha256"
  evidence_root="$root/bin/release/evidence/$release_version"
  [ -f "$dmg_path" ] && [ ! -L "$dmg_path" ] || die "missing release DMG: $dmg_path"
  [ -d "$evidence_root" ] && [ ! -L "$evidence_root" ] \
    || die "missing release evidence: $evidence_root"

  if evidence_candidates="$(find "$evidence_root" -mindepth 1 -maxdepth 1 -type d -print 2>&1 | \
    LC_ALL=C sort)"; then
    :
  else
    die "cannot enumerate release evidence: $evidence_candidates"
  fi
  evidence_path=""
  evidence_count=0
  while IFS= read -r candidate; do
    [ -n "$candidate" ] || continue
    evidence_path="$candidate"
    evidence_count=$((evidence_count + 1))
  done <<<"$evidence_candidates"
  [ "$evidence_count" -eq 1 ] \
    || die "expected one evidence directory, found $evidence_count"
  [ ! -L "$evidence_path" ] || die "release evidence must not be a symlink"
  submission_id="${evidence_path##*/}"
  [[ "$submission_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] \
    || die "invalid evidence submission id"
  [ "$submission_id" != . ] && [ "$submission_id" != .. ] \
    || die "invalid evidence submission id"
}

prepare_assets() {
  parse_release_args "$@"
  resolve_release_assets
  if ! (cd "$(dirname "$dmg_path")" && \
    shasum -a 256 "$canonical_dmg_name" >"$canonical_dmg_name.sha256"); then
    die "could not create DMG checksum"
  fi
  if ! (cd "$(dirname "$dmg_path")" && \
    shasum -a 256 -c "$canonical_dmg_name.sha256"); then
    die "DMG checksum verification failed"
  fi
  checksum="$(awk 'NR == 1 {print $1}' "$checksum_path")"
  [[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || die "invalid SHA-256 output"

  write_output dmg_path "$dmg_path"
  write_output checksum_path "$checksum_path"
  write_output evidence_path "$evidence_path"
  write_output checksum "$checksum"
  write_output submission_id "$submission_id"
  write_summary "### Loqui release assets"
  write_summary "- Version: $release_version"
  write_summary "- Commit: $release_sha"
  write_summary "- Tag: $release_tag"
  write_summary "- SHA-256: $checksum"
  write_summary "- Notarization submission: $submission_id"
  write_summary "- Evidence artifact: loqui-release-evidence-$release_tag"
}

verify_prepared_assets() {
  resolve_release_assets
  [ -f "$checksum_path" ] && [ ! -L "$checksum_path" ] \
    || die "missing release checksum: $checksum_path"
  if ! (cd "$(dirname "$dmg_path")" && \
    shasum -a 256 -c "$canonical_dmg_name.sha256"); then
    die "DMG checksum verification failed"
  fi
  checksum="$(awk 'NR == 1 {print $1}' "$checksum_path")"
  [[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || die "invalid SHA-256 output"
}

record_publication_failure() {
  failed_tag="$1"
  write_summary "### GitHub publication requires inspection"
  if draft_rows="$(gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" \
    --jq '.[] | [.tag_name, .draft, .html_url] | @tsv' 2>&1)"; then
    matching_rows="$(awk -F '\t' -v tag="$failed_tag" '$1 == tag {print}' <<<"$draft_rows")"
    write_summary "- Matching Releases: ${matching_rows:-none observed}"
  else
    write_summary "- Matching Releases: lookup failed"
  fi
  if failed_tag_refs="$(git ls-remote --tags origin "refs/tags/$failed_tag" 2>&1)"; then
    write_summary "- Tag state: ${failed_tag_refs:-absent}"
  else
    write_summary "- Tag state: lookup failed"
  fi
  write_summary "- Recovery: inspect the exact tag and Release; automation did not delete anything."
}

published_failure() {
  failure_reason="$1"
  if published_json="$(gh release view "$release_tag" --repo "$GITHUB_REPOSITORY" \
    --json url,tagName,targetCommitish 2>&1)"; then
    published_url="$(jq -r '.url // "unknown"' <<<"$published_json" 2>/dev/null || printf unknown)"
  else
    published_url=unknown
  fi
  write_summary "- Release: $published_url"
  write_summary "- Tag: $release_tag"
  write_summary "- Commit: $release_sha"
  write_summary "- WARNING: the Release is PUBLISHED — do not delete; verify manually with gh release view."
  die "$failure_reason; the Release is PUBLISHED — do not delete"
}

resolve_published_tag_sha() {
  retry_delay="${LOQUI_GITHUB_RELEASE_RETRY_DELAY_SECONDS:-2}"
  [[ "$retry_delay" =~ ^[0-9]+$ ]] || die "invalid release verification retry delay"
  tag_attempt=1
  while [ "$tag_attempt" -le 3 ]; do
    if tag_object="$(gh api "repos/$GITHUB_REPOSITORY/git/ref/tags/$release_tag" \
      --jq '.object | [.type, .sha] | @tsv' 2>&1)"; then
      tag_type="${tag_object%%$'\t'*}"
      tag_sha="${tag_object#*$'\t'}"
      if [ "$tag_type" = tag ]; then
        if annotated_object="$(gh api "repos/$GITHUB_REPOSITORY/git/tags/$tag_sha" \
          --jq '.object | [.type, .sha] | @tsv' 2>&1)"; then
          tag_type="${annotated_object%%$'\t'*}"
          tag_sha="${annotated_object#*$'\t'}"
        else
          published_failure "cannot dereference annotated release tag"
        fi
      fi
      [ "$tag_type" = commit ] || published_failure "release tag does not resolve to a commit"
      is_sha "$tag_sha" || published_failure "release tag returned an invalid commit SHA"
      printf '%s\n' "$tag_sha"
      return 0
    fi
    if [ "$tag_attempt" -lt 3 ] && [ "$retry_delay" -gt 0 ]; then
      sleep "$retry_delay"
    fi
    tag_attempt=$((tag_attempt + 1))
  done
  published_failure "release tag was not visible after three attempts"
}

publish_release() {
  parse_release_args "$@"
  preflight_quiet=1
  preflight --expect-sha "$release_sha" --expect-version "$release_version" \
    --expect-tag "$release_tag" --check-drafts
  preflight_quiet=0
  verify_prepared_assets

  if gh release create "$release_tag" "$dmg_path" "$checksum_path" \
    --repo "$GITHUB_REPOSITORY" --target "$release_sha" \
    --title "Loqui $release_version" --generate-notes --latest; then
    :
  else
    create_rc=$?
    preflight_quiet=0
    record_publication_failure "$release_tag"
    printf 'github-release: publication failed; remote state preserved\n' >&2
    exit "$create_rc"
  fi

  published_tag_sha="$(resolve_published_tag_sha)"
  [ "$published_tag_sha" = "$release_sha" ] \
    || published_failure "release tag targets the wrong commit"
  if release_json="$(gh release view "$release_tag" --repo "$GITHUB_REPOSITORY" \
    --json url,isDraft,isPrerelease,tagName,targetCommitish,assets 2>&1)"; then
    :
  else
    published_failure "cannot verify published GitHub Release"
  fi
  if ! jq -e --arg tag "$release_tag" --arg sha "$release_sha" \
    --arg dmg "$canonical_dmg_name" --arg checksum_name "$canonical_dmg_name.sha256" '
      .isDraft == false and .isPrerelease == false and
      .tagName == $tag and .targetCommitish == $sha and
      ([.assets[].name] | sort) == ([$dmg, $checksum_name] | sort)
    ' <<<"$release_json" >/dev/null; then
    published_failure "published GitHub Release metadata is invalid"
  fi
  release_url="$(jq -r '.url' <<<"$release_json")"
  write_summary "### Loqui GitHub Release"
  write_summary "- Version: $release_version"
  write_summary "- Commit: $release_sha"
  write_summary "- SHA-256: $checksum"
  write_summary "- URL: $release_url"
}

main() {
  command_name="${1:-}"
  [ "$#" -gt 0 ] && shift
  case "$command_name" in
    preflight) preflight "$@" ;;
    prepare) prepare_assets "$@" ;;
    publish) publish_release "$@" ;;
    *) die "unknown command: $command_name" ;;
  esac
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
