#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
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
