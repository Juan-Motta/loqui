#!/usr/bin/env bash
# Fixture scripts and literal evidence intentionally defer expansion to child shells.
# shellcheck disable=SC2016,SC2329
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

release_script="${RELEASE_SCRIPT:-$repo_root/scripts/release-macos.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-release-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
test_tmp_physical="$(cd "$tmp" && pwd -P)"

snapshot_release_files() {
  snapshot_root="$1"
  snapshot_output="$2"
  if ! : >"$snapshot_output"; then
    return 1
  fi
  if [ ! -e "$snapshot_root" ] && [ ! -L "$snapshot_root" ]; then
    if ! printf '%s\n' 'A .' >"$snapshot_output"; then
      return 1
    fi
    return 0
  fi
  if [ -L "$snapshot_root" ]; then
    if ! snapshot_target="$(readlink "$snapshot_root")"; then
      return 1
    fi
    if ! printf 'L %s .\n' "$snapshot_target" >"$snapshot_output"; then
      return 1
    fi
    return 0
  fi
  [ -d "$snapshot_root" ] || fail "release snapshot root is not a directory: $snapshot_root"
  if ! printf '%s\n' 'D .' >"$snapshot_output"; then
    return 1
  fi
  snapshot_paths="$snapshot_output.paths"
  if ! find "$snapshot_root" -mindepth 1 -print | LC_ALL=C sort >"$snapshot_paths"; then
    return 1
  fi
  while read -r snapshot_path; do
    snapshot_relative="${snapshot_path#"$snapshot_root"/}"
    if [ -L "$snapshot_path" ]; then
      if ! snapshot_target="$(readlink "$snapshot_path")"; then
        return 1
      fi
      if ! printf 'L %s %s\n' "$snapshot_target" "$snapshot_relative"; then
        return 1
      fi
    elif [ -d "$snapshot_path" ]; then
      if ! printf 'D %s\n' "$snapshot_relative"; then
        return 1
      fi
    elif [ -f "$snapshot_path" ]; then
      if ! snapshot_size="$(stat -f '%z' "$snapshot_path")"; then
        return 1
      fi
      if ! snapshot_sha="$(shasum -a 256 "$snapshot_path" | awk '{print $1}')"; then
        return 1
      fi
      if ! printf 'F %s %s %s\n' "$snapshot_size" "$snapshot_sha" "$snapshot_relative"; then
        return 1
      fi
    else
      if ! printf 'O %s\n' "$snapshot_relative"; then
        return 1
      fi
    fi
  done <"$snapshot_paths" >>"$snapshot_output"
  if ! rm -f "$snapshot_paths"; then
    return 1
  fi
}

real_release_root="$repo_root/bin/release"
real_release_before="$tmp/real-release-before.txt"
snapshot_release_files "$real_release_root" "$real_release_before"

finalize_release_test() {
  test_rc=$?
  trap - EXIT
  real_release_after="$tmp/real-release-after.txt"
  if ! snapshot_release_files "$real_release_root" "$real_release_after"; then
    echo "FAIL: could not snapshot real release output after isolated tests" >&2
    test_rc=1
  elif ! diff -u "$real_release_before" "$real_release_after"; then
    echo "FAIL: real release output changed during isolated tests" >&2
    test_rc=1
  fi
  if ! rm -rf "$tmp"; then
    echo "FAIL: could not remove isolated release test directory: $tmp" >&2
    test_rc=1
  fi
  exit "$test_rc"
}
trap finalize_release_test EXIT

snapshot_absent="$tmp/snapshot-absent.txt"
snapshot_release_files "$tmp/absent-release" "$snapshot_absent"
assert_contains "$snapshot_absent" 'A .'

snapshot_empty_root="$tmp/snapshot-empty-release"
snapshot_empty="$tmp/snapshot-empty.txt"
mkdir "$snapshot_empty_root"
snapshot_release_files "$snapshot_empty_root" "$snapshot_empty"
assert_contains "$snapshot_empty" 'D .'

snapshot_tree_root="$tmp/snapshot-tree-release"
snapshot_tree="$tmp/snapshot-tree.txt"
mkdir -p "$snapshot_tree_root/nested"
put_file "$snapshot_tree_root/payload.txt" snapshot-data
ln -s payload.txt "$snapshot_tree_root/payload-link"
snapshot_release_files "$snapshot_tree_root" "$snapshot_tree"
assert_contains "$snapshot_tree" 'D .'
assert_contains "$snapshot_tree" 'D nested'
assert_contains "$snapshot_tree" \
  'F 14 9624dbf8eaf8fc4bf8f1d1fdc2c43ab4e3128f1218f6e7988a99fd508ba70621 payload.txt'
assert_contains "$snapshot_tree" 'L payload.txt payload-link'

# shellcheck source=scripts/release-macos.sh
. "$release_script"

assert_contains "$release_script" 'if [ "${BASH_SOURCE[0]}" = "$0" ]; then main "$@"; fi'
default_auth="$(bash -c '. "$1"; printf "<%s>\\n" "${notary_auth_args[@]}"' \
  _ "$release_script")"
assert_eq "$default_auth" $'<--keychain-profile>\n<loqui-notary>'

ci_keychain="$tmp/loqui-ci.keychain-db"
: >"$ci_keychain"
ci_auth="$(LOQUI_NOTARY_PROFILE=loqui-ci-notary LOQUI_NOTARY_KEYCHAIN="$ci_keychain" \
  bash -c '. "$1"; printf "<%s>\\n" "${notary_auth_args[@]}"' _ "$release_script")"
assert_eq "$ci_auth" \
  "$(printf '<%s>\n' --keychain-profile loqui-ci-notary --keychain "$ci_keychain")"

run_expect_fail_msg 'LOQUI_NOTARY_KEYCHAIN must be absolute' \
  env LOQUI_NOTARY_KEYCHAIN=relative bash -c '. "$1"; validate_notary_keychain' \
  _ "$release_script"
run_expect_fail_msg 'notary keychain does not exist' \
  env LOQUI_NOTARY_KEYCHAIN="$tmp/missing.keychain-db" \
  bash -c '. "$1"; validate_notary_keychain' _ "$release_script"

assert_test_tmp_path() {
  guarded_path="$1"
  case "$guarded_path" in
    "$tmp"/*|"$test_tmp_physical"/*) ;;
    *) fail "test mutation escaped temporary root: $guarded_path" ;;
  esac
  if [ -d "$guarded_path" ]; then
    guarded_physical="$(cd "$guarded_path" && pwd -P)" \
      || fail "cannot resolve guarded test directory: $guarded_path"
  else
    [ ! -L "$guarded_path" ] || fail "guarded test file is a symlink: $guarded_path"
    guarded_probe="$guarded_path"
    guarded_suffix=""
    if [ -e "$guarded_probe" ]; then
      guarded_name="${guarded_probe##*/}"
      guarded_suffix="/$guarded_name"
      guarded_probe="${guarded_probe%/*}"
    fi
    while [ ! -e "$guarded_probe" ] && [ ! -L "$guarded_probe" ]; do
      guarded_name="${guarded_probe##*/}"
      guarded_suffix="/$guarded_name$guarded_suffix"
      guarded_parent="${guarded_probe%/*}"
      [ "$guarded_parent" != "$guarded_probe" ] \
        || fail "cannot find existing guarded ancestor: $guarded_path"
      guarded_probe="$guarded_parent"
    done
    [ -d "$guarded_probe" ] && [ ! -L "$guarded_probe" ] \
      || fail "guarded test ancestor is not a physical directory: $guarded_probe"
    guarded_parent_physical="$(cd "$guarded_probe" && pwd -P)" \
      || fail "cannot resolve guarded test ancestor: $guarded_probe"
    guarded_physical="$guarded_parent_physical$guarded_suffix"
  fi
  case "$guarded_physical" in
    "$test_tmp_physical"/*) ;;
    *) fail "test mutation resolves outside temporary root: $guarded_path -> $guarded_physical" ;;
  esac
}

guarded_atomic_publish() {
  assert_test_tmp_path "$1"
  assert_test_tmp_path "$2"
  assert_test_tmp_path "$3"
  assert_test_tmp_path "$release_root_dir"
  [ "$(cd "$release_root_dir" && pwd -P)" = "$release_root_dir" ] \
    || fail "test release root is not physical: $release_root_dir"
  [ "$3" = "$release_root_dir/bin/release" ] \
    || fail "test release output is outside its fixture repo: $3"
  atomic_publish "$@"
}

guarded_cleanup_release() {
  assert_test_tmp_path "$release_root_dir"
  [ "$(cd "$release_root_dir" && pwd -P)" = "$release_root_dir" ] \
    || fail "test cleanup root is not physical: $release_root_dir"
  [ -z "$stage" ] || assert_test_tmp_path "$stage"
  if [ "$hidden_dmg_candidate_owned" -eq 1 ]; then
    assert_test_tmp_path "$hidden_dmg_candidate"
  fi
  if [ "$hidden_evidence_candidate_owned" -eq 1 ]; then
    assert_test_tmp_path "$hidden_evidence_candidate"
  fi
  cleanup_release
}

assert_contains "$repo_root/Taskfile.yml" "release:macos:"
assert_contains "$repo_root/Taskfile.yml" "test:macos-release:"
assert_contains "$repo_root/Taskfile.yml" "./scripts/tests/whisper-model-integrity-test.sh"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "./scripts/macos-bundle.sh"
assert_contains "$repo_root/build/darwin/Taskfile.yml" 'LOQUI_SIGN_SCRIPT:-./scripts/macos-sign.sh'
assert_contains "$repo_root/build/darwin/Taskfile.yml" "app --channel development"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "./scripts/release-macos.sh"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "DEV: '{{.DEV}}'"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "OUTPUT: '{{.OUTPUT}}'"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "PORTABLE: '{{.PORTABLE | default \"false\"}}'"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "common:build:frontend"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "common:generate:icons"
assert_contains "$repo_root/build/darwin/Taskfile.yml" "-mmacosx-version-min=14.0"
assert_contains "$repo_root/build/darwin/Taskfile.yml" 'MACOSX_DEPLOYMENT_TARGET: "14.0"'
assert_not_contains "$repo_root/build/darwin/Taskfile.yml" "-mmacosx-version-min=12.0"
assert_not_contains "$repo_root/build/Taskfile.yml" "-iconcomposerinput"
assert_not_contains "$repo_root/build/Taskfile.yml" "-macassetdir"
assert_not_contains "$repo_root/build/darwin/Taskfile.yml" "wails3 tool sign"
assert_contains "$repo_root/scripts/task.sh" 'WAILS_VERSION="v3.0.0-alpha2.119"'
# The literal variable reference is the contract being asserted.
# shellcheck disable=SC2016
assert_contains "$repo_root/scripts/task.sh" 'github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}'

developer_id_plan="$repo_root/docs/superpowers/plans/2026-08-07-developer-id-release.md"
assert_contains "$developer_id_plan" 'status --porcelain --untracked-files=all'
assert_contains "$developer_id_plan" "SDL_BUILD=\"\${SDL_VENDOR}-build-loqui\""
assert_contains "$developer_id_plan" "SDL_INSTALL_PREFIX=\"\${SDL_VENDOR}-install-loqui\""
assert_contains "$developer_id_plan" "cmake -S \"\$VENDOR\" -B \"\$VENDOR/build-loqui\""
assert_contains "$developer_id_plan" "-DCMAKE_PREFIX_PATH=\"\$SDL_INSTALL_PREFIX\""
assert_contains "$developer_id_plan" "-DSDL2_DIR=\"\$SDL_INSTALL_PREFIX/lib/cmake/SDL2\""
assert_contains "$developer_id_plan" "cp -a \"\$VENDOR\"/build-loqui/bin/*.dylib"
assert_not_contains "$developer_id_plan" "cmake -S \"\$VENDOR\" -B \"\$VENDOR/build\""

preflight_tools="$tmp/preflight-tools"
preflight_probe_python="$tmp/preflight-probe-python"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  '[ "$1" = -c ] || exit 91' \
  'printf "%s\n" 1.6.7' \
  >"$preflight_probe_python"
chmod +x "$preflight_probe_python"
dmgbuild_python="$preflight_probe_python"
set +e
(
  # shellcheck disable=SC2329
  require_command() {
    printf '%s\n' "$1" >>"$preflight_tools"
    [ "$1" != tiffutil ] || exit 73
  }
  phase_preflight
)
preflight_probe_rc=$?
set -e
assert_eq "$preflight_probe_rc" 73
assert_not_contains "$preflight_tools" sdl2-config
assert_contains "$preflight_tools" vtool
assert_contains "$preflight_tools" sips
assert_contains "$preflight_tools" tiffutil

expect_failure() {
  failure_name="$1"
  expected_failure="$2"
  shift 2
  failure_output="$tmp/$failure_name.out"
  set +e
  (set -e; "$@") >"$failure_output" 2>&1
  failure_rc=$?
  set -e
  [ "$failure_rc" -ne 0 ] || fail "$failure_name unexpectedly passed"
  [ -z "$expected_failure" ] || assert_contains "$failure_output" "$expected_failure"
}

preflight_root="$tmp/preflight-root"
preflight_bin="$tmp/preflight-bin"
mkdir -p "$preflight_root/scripts" "$preflight_root/build/darwin/dmg" \
  "$preflight_root/helpers" \
  "$preflight_root/third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework/Versions/A" \
  "$preflight_bin"
printf '%s\n' 'info:' '  version: "0.1.0"' >"$preflight_root/build/config.yml"
for preflight_source in \
  build/darwin/Info.plist build/darwin/Info.dev.plist build/darwin/icons.icns \
  build/darwin/dmg/settings.py build/darwin/dmg/verify-ds-store.py \
  helpers/macos-globe-listener.swift helpers/macos-stt.swift helpers/whisper-stt.cpp \
  third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech; do
  put_file "$preflight_root/$preflight_source" source
done
put_file "$preflight_root/build/darwin/dmg/background.png" background-1x
put_file "$preflight_root/build/darwin/dmg/background@2x.png" background-2x
(
  cd "$preflight_root/build/darwin/dmg"
  /usr/bin/shasum -a 256 background.png background@2x.png >background.sha256
)
for preflight_script in build-macos-helpers.sh macos-bundle.sh macos-audit.sh setup-dmgbuild.sh; do
  put_file "$preflight_root/scripts/$preflight_script" '#!/bin/bash'
done
printf '%s\n' \
  '#!/bin/bash' \
  "exec '$repo_root/scripts/release-version.sh' \"\$@\"" \
  >"$preflight_root/scripts/release-version.sh"
printf '%s\n' \
  '#!/bin/bash' \
  '[ "${PATCH_PLISTS_RC:-0}" -eq 0 ] || printf "%s\n" "plist fixture rejected" >&2' \
  'exit "${PATCH_PLISTS_RC:-0}"' \
  >"$preflight_root/scripts/patch-plists.sh"
printf '%s\n' \
  '#!/bin/bash' \
  '[ "$1" = resolve ] || exit 81' \
  'case "${PREFLIGHT_IDENTITY_MODE:-resolved}" in' \
  '  failure) printf "%s\n" "identity lookup failed" >&2; exit 87 ;;' \
  '  ambiguous) printf "%s\n" "ambiguous Developer ID Application identities" >&2; exit 88 ;;' \
  'esac' \
  'printf "%s\n" "Developer ID Application: Fixture (TEAMID1234)"' \
  >"$preflight_root/scripts/macos-sign.sh"
chmod +x "$preflight_root/scripts/"*.sh

preflight_python="$tmp/preflight-python"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  '[ "$1" = -c ] || exit 91' \
  '[ "${PREFLIGHT_DMGBUILD_PROBE_RC:-0}" -eq 0 ] || exit "$PREFLIGHT_DMGBUILD_PROBE_RC"' \
  'printf "%s\n" "${PREFLIGHT_DMGBUILD_VERSION:-1.6.7}"' \
  >"$preflight_python"
chmod +x "$preflight_python"

for preflight_tool in security codesign otool lipo install_name_tool hdiutil spctl ditto jq \
  cmake swiftc vtool file tiffutil; do
  printf '%s\n' '#!/bin/bash' 'exit 0' >"$preflight_bin/$preflight_tool"
  chmod +x "$preflight_bin/$preflight_tool"
done
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  'image=""' \
  'for argument in "$@"; do image="$argument"; done' \
  'case "$image" in' \
  '  */background.png) width=660; height=360 ;;' \
  '  */background@2x.png) width=1320; height=720 ;;' \
  '  *) exit 88 ;;' \
  'esac' \
  'if [ "${PREFLIGHT_BACKGROUND_SIZE:-valid}" = wrong ] && [[ "$image" = */background.png ]]; then width=659; fi' \
  'printf "%s\n" "$image" "  format: png" "  bitsPerSample: 8" "  samplesPerPixel: 4" "  hasAlpha: yes" "  pixelWidth: $width" "  pixelHeight: $height"' \
  >"$preflight_bin/sips"
printf '%s\n' \
  '#!/bin/bash' \
  'if [ "$1" = -m ]; then printf "%s\n" "${PREFLIGHT_ARCH:-arm64}"; exit 0; fi' \
  'exec /usr/bin/uname "$@"' \
  >"$preflight_bin/uname"
printf '%s\n' \
  '#!/bin/bash' \
  '[ "$1" = version ] || exit 82' \
  'printf "%s\n" "${PREFLIGHT_WAILS_VERSION:-v3.0.0-alpha2.119}"' \
  >"$preflight_bin/wails3"
printf '%s\n' \
  '#!/bin/bash' \
  'if [ "$1" = -extract ]; then printf "%s\n" "${PREFLIGHT_PLIST_VERSION:-0.1.0}"; exit 0; fi' \
  'exit 83' \
  >"$preflight_bin/plutil"
printf '%s\n' \
  '#!/bin/bash' \
  '[ "$1" = notarytool ] && [ "$2" = history ] || exit 84' \
  'exit "${PREFLIGHT_NOTARY_RC:-0}"' \
  >"$preflight_bin/xcrun"
printf '%s\n' \
  '#!/bin/bash' \
  'if [ "$1" = -C ]; then' \
  '  case "$3" in rev-parse) printf "%s\n" fixture-head ;; describe) printf "%s\n" fixture-dirty ;; *) exit 85 ;; esac' \
  '  exit 0' \
  'fi' \
  'exit 86' \
  >"$preflight_bin/git"
chmod +x "$preflight_bin/"*

run_preflight_fixture() {
  saved_preflight_root="$release_root_dir"
  saved_preflight_output="$release_output_dir"
  fixture_root="${PREFLIGHT_FIXTURE_ROOT:-$preflight_root}"
  release_root_dir="$fixture_root"
  release_output_dir="$fixture_root/bin/release"
  if [ "${PREFLIGHT_DMGBUILD_PATH+x}" = x ]; then
    dmgbuild_python="$PREFLIGHT_DMGBUILD_PATH"
  else
    dmgbuild_python="$preflight_python"
  fi
  PATH="$preflight_bin:/usr/bin:/bin" phase_preflight
  release_root_dir="$saved_preflight_root"
  release_output_dir="$saved_preflight_output"
}

preflight_non_executable="$tmp/preflight-non-executable-python"
put_file "$preflight_non_executable" '#!/bin/bash' 644

wrong_digest_root="$tmp/preflight-wrong-digest-root"
cp -R "$preflight_root" "$wrong_digest_root"
put_file "$wrong_digest_root/build/darwin/dmg/background.png" wrong-background

run_preflight_missing_tool() {
  missing_tool_bin="$tmp/preflight-missing-tool-bin"
  mkdir -p "$missing_tool_bin"
  printf '%s\n' '#!/bin/bash' 'printf "%s\n" arm64' >"$missing_tool_bin/uname"
  chmod +x "$missing_tool_bin/uname"
  PATH="$missing_tool_bin" phase_preflight
}

PREFLIGHT_ARCH=x86_64 expect_failure preflight-arch 'release host must be arm64' run_preflight_fixture
expect_failure preflight-missing-tool 'missing required command: security' run_preflight_missing_tool
PREFLIGHT_WAILS_VERSION=v9.9.9 expect_failure preflight-wails \
  "wails3 version is 'v9.9.9'" run_preflight_fixture
PATCH_PLISTS_RC=31 expect_failure preflight-plist-check 'plist fixture rejected' run_preflight_fixture
PREFLIGHT_NOTARY_RC=44 expect_failure preflight-notary \
  "notary profile 'loqui-notary' is invalid or unavailable" run_preflight_fixture
PREFLIGHT_IDENTITY_MODE=failure expect_failure preflight-identity-failure \
  'identity lookup failed' run_preflight_fixture
PREFLIGHT_IDENTITY_MODE=ambiguous expect_failure preflight-identity-ambiguous \
  'ambiguous Developer ID Application identities' run_preflight_fixture

missing_source_root="$tmp/preflight-missing-source-root"
cp -R "$preflight_root" "$missing_source_root"
rm "$missing_source_root/helpers/macos-stt.swift"
PREFLIGHT_FIXTURE_ROOT="$missing_source_root" expect_failure preflight-missing-source \
  'missing required source: helpers/macos-stt.swift' run_preflight_fixture

malformed_version_root="$tmp/preflight-malformed-version-root"
cp -R "$preflight_root" "$malformed_version_root"
printf '%s\n' 'info:' '  version: not-a-version' >"$malformed_version_root/build/config.yml"
PREFLIGHT_FIXTURE_ROOT="$malformed_version_root" expect_failure preflight-malformed-version \
  'info.version must appear once as quoted MAJOR.MINOR.PATCH' run_preflight_fixture

missing_dmg_source_root="$tmp/preflight-missing-dmg-source-root"
cp -R "$preflight_root" "$missing_dmg_source_root"
rm "$missing_dmg_source_root/build/darwin/dmg/settings.py"
PREFLIGHT_FIXTURE_ROOT="$missing_dmg_source_root" expect_failure preflight-missing-dmg-source \
  'missing required source: build/darwin/dmg/settings.py' run_preflight_fixture

missing_setup_root="$tmp/preflight-missing-setup-root"
cp -R "$preflight_root" "$missing_setup_root"
rm "$missing_setup_root/scripts/setup-dmgbuild.sh"
PREFLIGHT_FIXTURE_ROOT="$missing_setup_root" expect_failure preflight-missing-setup \
  'missing executable script: scripts/setup-dmgbuild.sh' run_preflight_fixture

symlink_background_root="$tmp/preflight-symlink-background-root"
cp -R "$preflight_root" "$symlink_background_root"
rm "$symlink_background_root/build/darwin/dmg/background.png"
ln -s "$preflight_root/build/darwin/dmg/background.png" \
  "$symlink_background_root/build/darwin/dmg/background.png"
PREFLIGHT_FIXTURE_ROOT="$symlink_background_root" expect_failure preflight-symlink-background \
  'DMG background is not a regular non-symlink file: background.png' run_preflight_fixture

or_list_phase_log="$tmp/or-list-phases.txt"
or_list_expected_phases="$tmp/or-list-expected-phases.txt"
or_list_tmp="$tmp/or-list-tmp"
mkdir "$or_list_tmp"
printf '%s\n' preflight >"$or_list_expected_phases"
saved_or_release_root="$release_root_dir"
saved_or_release_output="$release_output_dir"
saved_or_release_output_physical="$release_output_dir_physical"
release_root_dir="$(cd "$missing_source_root" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
release_output_dir_physical=""
stage=""
stage_lexical=""
or_list_rc=0
LOQUI_PHASE_LOG="$or_list_phase_log" LOQUI_NOTARY_LOG_RETRY_DELAY=0 \
TMPDIR="$or_list_tmp" PATH="$preflight_bin:/usr/bin:/bin" \
  run_release || or_list_rc=$?
guarded_cleanup_release
stage=""
stage_lexical=""
release_root_dir="$saved_or_release_root"
release_output_dir="$saved_or_release_output"
release_output_dir_physical="$saved_or_release_output_physical"
[ "$or_list_rc" -ne 0 ] || fail "OR-list release unexpectedly succeeded"
if ! diff -u "$or_list_expected_phases" "$or_list_phase_log" >"$tmp/or-list-phases.diff"; then
  fail "OR-list release continued after the real preflight failure"
fi

helper_fixture_root="$tmp/helper-fixture-root"
helper_fixture_bin="$tmp/helper-fixture-bin"
helper_fixture_stage="$tmp/helper-fixture-stage"
mkdir -p "$helper_fixture_root/scripts" "$helper_fixture_bin" \
  "$helper_fixture_stage/evidence-work"
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'mkdir -p "$LOQUI_HELPERS_OUTPUT_DIR" "$LOQUI_SDL_VENDOR_DIR"' \
  'printf "%s\n" helper >"$LOQUI_HELPERS_OUTPUT_DIR/fixture-helper"' \
  'printf "%s\n" "$LOQUI_SDL_VENDOR_DIR" >"$SDL_VENDOR_LOG"' \
  >"$helper_fixture_root/scripts/build-macos-helpers.sh"
chmod +x "$helper_fixture_root/scripts/build-macos-helpers.sh"
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'if [ "$1" = -C ] && [ "$3" = rev-parse ]; then printf "%s\n" "$EXPECTED_SDL_COMMIT"; exit 0; fi' \
  'exit 91' >"$helper_fixture_bin/git"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'if [ "${FILE_PROBE_RC:-0}" -ne 0 ]; then exit "$FILE_PROBE_RC"; fi' \
  'printf "%s\n" "Mach-O 64-bit executable arm64"' \
  >"$helper_fixture_bin/file"
chmod +x "$helper_fixture_bin/git" "$helper_fixture_bin/file"
saved_release_root_dir="$release_root_dir"
saved_stage="$stage"
release_root_dir="$helper_fixture_root"
stage="$helper_fixture_stage"
SDL_VENDOR_LOG="$tmp/release-sdl-vendor.log" \
EXPECTED_SDL_COMMIT=5d249570393f7a37e037abf22cd6012a4cc56a71 \
PATH="$helper_fixture_bin:$PATH" phase_build_helpers
assert_eq "$(cat "$tmp/release-sdl-vendor.log")" "$helper_fixture_stage/sdl-src"
assert_eq "$(cat "$helper_fixture_stage/evidence-work/sdl-commit.txt")" \
  5d249570393f7a37e037abf22cd6012a4cc56a71
run_helper_file_probe_failure() {
  SDL_VENDOR_LOG="$tmp/release-sdl-vendor-failure.log" \
  EXPECTED_SDL_COMMIT=5d249570393f7a37e037abf22cd6012a4cc56a71 \
  FILE_PROBE_RC=72 PATH="$helper_fixture_bin:$PATH" phase_build_helpers
}
expect_failure helper-file-probe-error 'could not inspect file type' \
  run_helper_file_probe_failure
release_root_dir="$saved_release_root_dir"
stage="$saved_stage"

dr_bin="$tmp/dr-bin"
mkdir -p "$dr_bin"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  '[ "$1" = -d ] && [ "$2" = -r- ] || exit 92' \
  'eval "target=\${$#}"' \
  'printf "Executable=%s\n" "$target" >&2' \
  '[ ! -f "$target.fake-metadata" ] || /bin/cat "$target.fake-metadata" >&2' \
  '[ ! -f "$target.fake-dr" ] || /bin/cat "$target.fake-dr" >&2' \
  >"$dr_bin/codesign"
chmod +x "$dr_bin/codesign"

make_designated_fixture() {
  fixture_app="$1/Loqui.app"
  metadata_value="$2"
  mkdir -p "$fixture_app/Contents/Helpers"
  put_file "$fixture_app/Contents/Helpers/globe-listener" globe
  put_file "$fixture_app/Contents/Helpers/macos-stt" apple
  put_file "$fixture_app/Contents/Helpers/whisper-stt" whisper
  for relative_path in \
    Loqui.app \
    Loqui.app/Contents/Helpers/globe-listener \
    Loqui.app/Contents/Helpers/macos-stt \
    Loqui.app/Contents/Helpers/whisper-stt; do
    target_path="$1/$relative_path"
    printf 'Timestamp=%s\n' "$metadata_value" >"$target_path.fake-metadata"
    case "$relative_path" in
      Loqui.app) requirement_identifier=com.jualopezmo.loquigo ;;
      *) requirement_identifier="com.jualopezmo.loquigo.$(basename "$relative_path")" ;;
    esac
    printf 'designated => identifier "%s" and anchor apple generic\n' \
      "$requirement_identifier" >"$target_path.fake-dr"
  done
}

dr_first_root="$tmp/dr-first"
dr_second_root="$tmp/dr-second"
make_designated_fixture "$dr_first_root" first-signature-metadata
make_designated_fixture "$dr_second_root" second-signature-metadata
dr_first="$tmp/designated-first.txt"
dr_second="$tmp/designated-second.txt"
PATH="$dr_bin:$PATH" capture_designated_requirements "$dr_first_root/Loqui.app" "$dr_first"
PATH="$dr_bin:$PATH" capture_designated_requirements "$dr_second_root/Loqui.app" "$dr_second"
compare_designated_requirements "$dr_first" "$dr_second"
diff -u "$dr_first" "$dr_second"
assert_contains "$dr_first" '## Loqui.app'
assert_contains "$dr_first" '## Loqui.app/Contents/Helpers/globe-listener'
assert_contains "$dr_first" '## Loqui.app/Contents/Helpers/macos-stt'
assert_contains "$dr_first" '## Loqui.app/Contents/Helpers/whisper-stt'
assert_contains "$dr_first" 'designated => identifier "com.jualopezmo.loquigo" and anchor apple generic'

rm -f "$dr_second_root/Loqui.app/Contents/Helpers/macos-stt.fake-dr"
set +e
PATH="$dr_bin:$PATH" capture_designated_requirements \
  "$dr_second_root/Loqui.app" "$tmp/designated-missing.txt" >"$tmp/designated-missing.out" 2>&1
missing_dr_rc=$?
set -e
[ "$missing_dr_rc" -ne 0 ] || fail "missing designated requirement unexpectedly passed"
assert_contains "$tmp/designated-missing.out" 'macos-stt lacks one designated requirement'

make_designated_fixture "$dr_second_root" third-signature-metadata
printf '%s\n' 'designated => identifier "com.example.changed" and anchor apple generic' \
  >"$dr_second_root/Loqui.app/Contents/Helpers/whisper-stt.fake-dr"
PATH="$dr_bin:$PATH" capture_designated_requirements "$dr_second_root/Loqui.app" "$dr_second"
set +e
compare_designated_requirements "$dr_first" "$dr_second" >"$tmp/designated-mismatch.out" 2>&1
mismatched_dr_rc=$?
set -e
[ "$mismatched_dr_rc" -ne 0 ] || fail "mismatched designated requirements unexpectedly passed"
assert_contains "$tmp/designated-mismatch.out" 'designated requirements differ'

verify_bin="$tmp/verify-bin"
verify_stage="$tmp/verify-stage"
app="$verify_stage/Loqui.app"
stage="$verify_stage"
mkdir -p "$verify_bin" "$stage/evidence-work" "$app/Contents/MacOS" "$app/Contents/Helpers" \
  "$app/Contents/Frameworks/Fixture.framework/Versions/A" "$app/Contents/Resources"
put_file "$app/Contents/MacOS/loqui" main
put_file "$app/Contents/Helpers/globe-listener" globe
put_file "$app/Contents/Helpers/macos-stt" apple
put_file "$app/Contents/Helpers/whisper-stt" whisper
put_file "$app/Contents/Frameworks/libfixture.dylib" dylib
put_file "$app/Contents/Frameworks/Fixture.framework/Versions/A/Fixture" framework
put_file "$app/Contents/Resources/readme.txt" data
ln -s libfixture.dylib "$app/Contents/Frameworks/libfixture-alias.dylib"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  'mutation="${VERIFY_MUTATION:-${DMG_MUTATION:-none}}"' \
  'eval "target=\${$#}"' \
  '[ -z "${VERIFY_CODESIGN_LOG:-}" ] || printf "%s\n" "$*" >>"$VERIFY_CODESIGN_LOG"' \
  'identifier_for() {' \
  '  case "$1" in' \
  '    *.app|*/Contents/MacOS/loqui) printf "%s" com.jualopezmo.loquigo ;;' \
  '    */Contents/Helpers/*) printf "%s" "com.jualopezmo.loquigo.$(/usr/bin/basename "$1")" ;;' \
  '    *) printf "%s" com.jualopezmo.loquigo.unknown ;;' \
  '  esac' \
  '}' \
  'if [ "$1" = --verify ]; then' \
  '  if [[ "$target" = */dmg-verify/Loqui.app ]]; then exit "${VERIFY_CODESIGN_MOUNTED_RC:-0}"; fi' \
  '  exit "${VERIFY_CODESIGN_RC:-0}"' \
  'fi' \
  'if [ "$1" = -dv ]; then' \
  '  identifier="$(identifier_for "$target")"' \
  '  [ "$mutation" != identifier ] || case "$target" in */globe-listener) identifier=com.example.wrong ;; esac' \
  '  authority="Developer ID Application: Fixture (TEAMID1234)"' \
  '  [ "$mutation" != authority ] || case "$target" in */Contents/MacOS/loqui) authority="Apple Development: Wrong" ;; esac' \
  '  team=TEAMID1234' \
  '  [ "$mutation" != team ] || case "$target" in */whisper-stt) team=OTHERTEAM0 ;; esac' \
  '  [ "$mutation" != dylib-team ] || case "$target" in */libfixture.dylib) team=OTHERTEAM0 ;; esac' \
  '  [ "$mutation" != framework-team ] || case "$target" in */Fixture.framework/Versions/A/Fixture) team=OTHERTEAM0 ;; esac' \
  '  flags="0x10000(runtime)"' \
  '  [ "$mutation" != runtime ] || case "$target" in */macos-stt) flags="0x0(none)" ;; esac' \
  '  printf "Executable=%s\nIdentifier=%s\nCodeDirectory v=20500 size=123 flags=%s\nAuthority=%s\nTeamIdentifier=%s\n" "$target" "$identifier" "$flags" "$authority" "$team" >&2' \
  '  if [ "$mutation" != timestamp ] || [[ "$target" != */whisper-stt ]]; then printf "%s\n" "Timestamp=Aug 9, 2026" >&2; fi' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = -d ] && [ "$2" = -r- ]; then' \
  '  requirement_identifier="$(identifier_for "$target")"' \
  '  [ "$mutation" != dmg-dr ] || case "$target" in */dmg-verify/Loqui.app/Contents/Helpers/whisper-stt) requirement_identifier=com.example.changed ;; esac' \
  '  printf "designated => identifier \"%s\" and anchor apple generic\n" "$requirement_identifier" >&2' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = -d ] && [ "$2" = --entitlements ]; then' \
  '  printf "%s\n" "<?xml version=\"1.0\" encoding=\"UTF-8\"?>" "<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">" "<plist version=\"1.0\">" "<dict>"' \
  '  case "$target" in' \
  '    *.app)' \
  '      if [ "$mutation" != host-audio ]; then printf "%s\n" "<key>com.apple.security.device.audio-input</key><true/>"; fi' \
  '      if [ "$mutation" != host-apple-events ]; then printf "%s\n" "<key>com.apple.security.automation.apple-events</key><true/>"; fi ;;' \
  '    */globe-listener)' \
  '      if [ "$mutation" = globe-entitlements ]; then printf "%s\n" "<key>com.apple.security.device.audio-input</key><true/>"; fi ;;' \
  '    */macos-stt|*/whisper-stt)' \
  '      if [ "$mutation" != entitlements ] || [[ "$target" != */macos-stt ]]; then printf "%s\n" "<key>com.apple.security.device.audio-input</key><true/>"; fi ;;' \
  '  esac' \
  '  printf "%s\n" "</dict>" "</plist>"' \
  '  exit 0' \
  'fi' \
  'exit 93' \
  >"$verify_bin/codesign"
printf '%s\n' \
  '#!/bin/bash' \
  'if [ "${FILE_PROBE_RC:-0}" -ne 0 ]; then exit "$FILE_PROBE_RC"; fi' \
  'eval "target=\${$#}"' \
  'case "$1" in' \
  '  -b) ;;' \
  '  *) target="$1" ;;' \
  'esac' \
  'case "$target" in' \
  '  */Contents/MacOS/loqui|*/Contents/Helpers/*|*/Contents/Frameworks/libfixture.dylib|*/Fixture.framework/Versions/A/Fixture) printf "%s\n" "Mach-O 64-bit executable arm64" ;;' \
  '  *) printf "%s\n" data ;;' \
  'esac' \
  >"$verify_bin/file"
chmod +x "$verify_bin/codesign" "$verify_bin/file"

PATH="$verify_bin:$PATH" VERIFY_MUTATION=none phase_verify_app
assert_file "$stage/evidence-work/designated-requirements.txt"
assert_eq "$(grep -c '^designated =>' "$stage/evidence-work/designated-requirements.txt")" 4
assert_eq "$(wc -l <"$signed_manifest" | tr -d ' ')" 6
assert_contains "$signed_manifest" 'Loqui.app/Contents/Frameworks/libfixture.dylib'
assert_contains "$signed_manifest" 'Loqui.app/Contents/Frameworks/Fixture.framework/Versions/A/Fixture'
assert_not_contains "$signed_manifest" 'libfixture-alias.dylib'
assert_not_contains "$signed_manifest" 'Contents/Resources/readme.txt'

run_verify_mutation() {
  VERIFY_MUTATION="$1" PATH="$verify_bin:$PATH" phase_verify_app
}
expect_failure verify-team 'different TeamIdentifier' run_verify_mutation team
expect_failure verify-dylib-team 'different TeamIdentifier' run_verify_mutation dylib-team
expect_failure verify-framework-team 'different TeamIdentifier' run_verify_mutation framework-team
expect_failure verify-authority 'lacks Developer ID Application authority' run_verify_mutation authority
expect_failure verify-identifier "identifier is 'com.example.wrong'" run_verify_mutation identifier
expect_failure verify-runtime 'lacks Hardened Runtime' run_verify_mutation runtime
expect_failure verify-timestamp 'lacks secure timestamp' run_verify_mutation timestamp
expect_failure verify-entitlements 'wrong audio-helper entitlement count' run_verify_mutation entitlements
expect_failure verify-globe-entitlements 'unexpected entitlements' \
  run_verify_mutation globe-entitlements
expect_failure verify-host-audio 'wrong host entitlement count' run_verify_mutation host-audio
expect_failure verify-host-apple-events 'wrong host entitlement count' \
  run_verify_mutation host-apple-events

run_verify_file_probe_failure() {
  FILE_PROBE_RC=72 VERIFY_MUTATION=none PATH="$verify_bin:$PATH" phase_verify_app
}
expect_failure verify-file-probe-error 'could not inspect file type' \
  run_verify_file_probe_failure

run_verify_codesign_failure() {
  VERIFY_CODESIGN_RC=66 VERIFY_MUTATION=none PATH="$verify_bin:$PATH" phase_verify_app
}
expect_failure verify-codesign-failure '' run_verify_codesign_failure

dmg_fixture_root="$tmp/dmg-fixture-root"
dmg_fixture_bin="$tmp/dmg-fixture-bin"
mkdir -p "$dmg_fixture_root/scripts" "$dmg_fixture_root/build/darwin/dmg" "$dmg_fixture_bin"
put_file "$dmg_fixture_root/build/darwin/dmg/settings.py" settings
put_file "$dmg_fixture_root/build/darwin/dmg/verify-ds-store.py" verifier
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  'eval "audit_target=\${$#}"' \
  'printf "%s\n" "$audit_target" >>"$DMG_AUDIT_LOG"' \
  'if [ "${DMG_MUTATION:-none}" = mounted-audit ] && [[ "$audit_target" = */dmg-verify/Loqui.app ]]; then exit 71; fi' \
  'exit 0' \
  >"$dmg_fixture_root/scripts/macos-audit.sh"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  '[ "$#" -eq 2 ] || exit 96' \
  'printf "%s\n" "$*" >>"$DMG_DITTO_LOG"' \
  'exec /bin/cp -R "$1" "$2"' \
  >"$dmg_fixture_bin/ditto"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$DMG_HDIUTIL_LOG"' \
  'case "$1" in' \
  '  verify) exit "${DMG_VERIFY_RC:-0}" ;;' \
  '  attach)' \
  '    [ "${DMG_MUTATION:-none}" != attach ] || exit 72' \
  '    mountpoint=""' \
  '    previous=""' \
  '    for argument in "$@"; do' \
  '      if [ "$previous" = -mountpoint ]; then mountpoint="$argument"; break; fi' \
  '      previous="$argument"' \
  '    done' \
  '    [ -n "$mountpoint" ] || exit 73' \
  '    /bin/cp -R "$DMG_FIXTURE_SOURCE_APP" "$mountpoint/Loqui.app"' \
  '    if [ "${DMG_MUTATION:-none}" = mounted-app-symlink ]; then' \
  '      /bin/rm -rf "$mountpoint/Loqui.app"' \
  '      /bin/ln -s "$DMG_MOUNTED_APP_TARGET" "$mountpoint/Loqui.app"' \
  '    fi' \
  '    applications_target=/Applications' \
  '    [ "${DMG_MUTATION:-none}" != wrong-applications ] || applications_target=/Wrong' \
  '    /bin/ln -s "$applications_target" "$mountpoint/Applications"' \
  '    [ "${DMG_MUTATION:-none}" = extra-visible ] && printf "%s\n" extra >"$mountpoint/Unexpected.txt"' \
  '    if [ "${DMG_MUTATION:-none}" != missing-ds-store ]; then' \
  '      printf "%s\n" "${DMG_MUTATION:-valid}" >"$mountpoint/.DS_Store"' \
  '    fi' \
  '    [ "${DMG_MUTATION:-none}" = missing-background ] || printf "%s\n" tiff >"$mountpoint/.background.tiff"' \
  '    if [ "${DMG_MUTATION:-none}" = background-symlink ]; then' \
  '      /bin/rm -f "$mountpoint/.background.tiff"' \
  '      /bin/ln -s "$DMG_BACKGROUND_TARGET" "$mountpoint/.background.tiff"' \
  '    fi' \
  '    ;;' \
  '  detach)' \
  '    if [ "${2:-}" = -force ]; then exit "${DMG_FORCE_DETACH_RC:-0}"; fi' \
  '    detach_count=0' \
  '    [ ! -f "$DMG_DETACH_COUNTER" ] || detach_count="$(cat "$DMG_DETACH_COUNTER")"' \
  '    detach_count=$((detach_count + 1))' \
  '    printf "%s\n" "$detach_count" >"$DMG_DETACH_COUNTER"' \
  '    case "${DMG_DETACH_MODE:-success}" in' \
  '      transient) [ "$detach_count" -gt 1 ] || exit 74 ;;' \
  '      fail) exit 75 ;;' \
  '    esac' \
  '    ;;' \
  '  create) printf "%s\n" "old hdiutil create -srcfolder path rejected" >&2; exit 97 ;;' \
  '  *) exit 98 ;;' \
  'esac' \
  >"$dmg_fixture_bin/hdiutil"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  '[ "$1" = -info ] || exit 81' \
  'printf "%s\n" "$*" >>"$DMG_TIFFUTIL_LOG"' \
  'case "${DMG_MUTATION:-none}" in' \
  '  tiff-single)' \
  '    printf "%s\n" "Directory at 0x1" "  Image Width: 660 Image Length: 360" ;;' \
  '  tiff-wrong)' \
  '    printf "%s\n" "Directory at 0x1" "  Image Width: 660 Image Length: 360" "Directory at 0x2" "  Image Width: 1200 Image Length: 720" ;;' \
  '  *)' \
  '    printf "%s\n" "Directory at 0x1" "  Image Width: 660 Image Length: 360" "Directory at 0x2" "  Image Width: 1320 Image Length: 720" ;;' \
  'esac' \
  >"$dmg_fixture_bin/tiffutil"
dmgbuild_fixture_python="$dmg_fixture_bin/python"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  'if [ "$1" = -c ]; then printf "%s\n" 1.6.7; exit 0; fi' \
  'case "$1" in' \
  '  */verify-ds-store.py)' \
  '    printf "%s\n" "$*" >>"$DMG_DS_STORE_LOG"' \
  '    marker="$(cat "$2")"' \
  '    case "$marker" in' \
  '      ds-window) printf "%s\n" "verify-ds-store: bwsp.WindowBounds mismatch" >&2; exit 41 ;;' \
  '      ds-chrome) printf "%s\n" "verify-ds-store: bwsp.ShowToolbar mismatch" >&2; exit 42 ;;' \
  '      ds-icon-view) printf "%s\n" "verify-ds-store: icvp.iconSize mismatch" >&2; exit 43 ;;' \
  '      ds-iloc) printf "%s\n" "verify-ds-store: Applications.Iloc mismatch" >&2; exit 44 ;;' \
  '    esac' \
  '    printf "%s\n" "verify-ds-store: PASS"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  '[ "$1" = -m ] && [ "$2" = dmgbuild ] || exit 82' \
  'printf "%s\n" "$@" >"$DMGBUILD_LOG"' \
  '[ "${DMG_MUTATION:-none}" != generator ] || exit 83' \
  'for output_path in "$@"; do :; done' \
  'case "${DMG_MUTATION:-none}" in' \
  '  missing-output) ;;' \
  '  symlink-output) /bin/ln -s "$DMG_SYMLINK_TARGET" "$output_path" ;;' \
  '  *) printf "%s\n" dmg >"$output_path" ;;' \
  'esac' \
  >"$dmgbuild_fixture_python"
chmod +x "$dmg_fixture_root/scripts/macos-audit.sh" "$dmg_fixture_bin/ditto" \
  "$dmg_fixture_bin/hdiutil" "$dmg_fixture_bin/tiffutil" "$dmgbuild_fixture_python"
saved_dmg_release_root="$release_root_dir"
saved_dmg_version="$version"
saved_dmg_app="$app"
saved_dmg_stage="$stage"
saved_dmg_python="$dmgbuild_python"
dmg_source_app="$app"
dmg_source_requirements="$stage/evidence-work/designated-requirements.txt"
release_root_dir="$(cd "$dmg_fixture_root" && pwd -P)"
version=0.1.0
dmgbuild_python="$dmgbuild_fixture_python"
dmg_case_number=0

prepare_dmg_case() {
  dmg_case_name="$1"
  dmg_case_number=$((dmg_case_number + 1))
  dmg_case_stage="$tmp/dmg-case-$dmg_case_number-$dmg_case_name"
  mkdir -p "$dmg_case_stage/evidence-work"
  stage="$(cd "$dmg_case_stage" && pwd -P)"
  app="$dmg_source_app"
  cp "$dmg_source_requirements" "$stage/evidence-work/designated-requirements.txt"
  dmg=""
  dmg_verify_mount=""
  dmg_verify_mounted=0
  dmgbuild_log="$stage/dmgbuild.log"
  dmg_hdiutil_log="$stage/hdiutil.log"
  dmg_audit_log="$stage/audit.log"
  dmg_ditto_log="$stage/ditto.log"
  dmg_codesign_log="$stage/codesign.log"
  dmg_tiffutil_log="$stage/tiffutil.log"
  dmg_ds_store_log="$stage/ds-store.log"
  dmg_detach_counter="$stage/detach-count"
  dmg_phase_log="$stage/phases.log"
  : >"$dmgbuild_log"
  : >"$dmg_hdiutil_log"
  : >"$dmg_audit_log"
  : >"$dmg_ditto_log"
  : >"$dmg_codesign_log"
  : >"$dmg_tiffutil_log"
  : >"$dmg_ds_store_log"
}

run_dmg_release_fixture() (
  phase_preflight() { :; }
  phase_build() { :; }
  phase_build_helpers() { :; }
  phase_bundle() { :; }
  phase_audit_unsigned() { :; }
  phase_sign_app() { :; }
  phase_verify_app() { :; }
  phase_sign_dmg() { :; }
  phase_verify_dmg() { :; }
  phase_submit() { :; }
  phase_fetch_log() { :; }
  phase_check_log() { :; }
  phase_staple() { :; }
  phase_verify_staple() { :; }
  phase_gatekeeper() { :; }
  phase_publish() { :; }
  LOQUI_PHASE_LOG="$dmg_phase_log" run_release || return 1
  [ "$dmg_verify_mounted" -eq 0 ] \
    || fail 'successful DMG inspection left mounted state set'
)

run_dmg_case() {
  DMGBUILD_LOG="$dmgbuild_log" \
  DMG_HDIUTIL_LOG="$dmg_hdiutil_log" \
  DMG_AUDIT_LOG="$dmg_audit_log" \
  DMG_DITTO_LOG="$dmg_ditto_log" \
  DMG_DS_STORE_LOG="$dmg_ds_store_log" \
  DMG_DETACH_COUNTER="$dmg_detach_counter" \
  DMG_FIXTURE_SOURCE_APP="$dmg_source_app" \
  DMG_MOUNTED_APP_TARGET="$dmg_source_app" \
  DMG_BACKGROUND_TARGET="$tmp/dmg-background-target.tiff" \
  DMG_SYMLINK_TARGET="$tmp/dmg-symlink-target" \
  DMG_TIFFUTIL_LOG="$dmg_tiffutil_log" \
  VERIFY_CODESIGN_LOG="$dmg_codesign_log" \
  LOQUI_DMG_DETACH_RETRY_DELAY=0 \
  PATH="$dmg_fixture_bin:$verify_bin:/usr/bin:/bin" \
    run_dmg_release_fixture
}

assert_dmg_rejected_before_release() {
  assert_contains "$dmg_phase_log" create-dmg
  assert_not_contains "$dmg_phase_log" sign-dmg
  assert_not_contains "$dmg_phase_log" submit
  assert_not_contains "$dmg_phase_log" publish
}

run_rejected_dmg_case() {
  rejected_name="$1"
  rejected_message="$2"
  mutation="$3"
  prepare_dmg_case "$rejected_name"
  DMG_MUTATION="$mutation" expect_failure "$rejected_name" "$rejected_message" run_dmg_case
  assert_dmg_rejected_before_release
}

prepare_dmg_case success
DMG_MUTATION=none run_dmg_case
expected_dmgbuild_log="$stage/expected-dmgbuild.log"
printf '%s\n' \
  -m dmgbuild \
  -s "$release_root_dir/build/darwin/dmg/settings.py" \
  -D "app=$stage/dmg-root/Loqui.app" \
  -D "assets=$release_root_dir/build/darwin/dmg" \
  Loqui "$stage/Loqui.dmg" \
  >"$expected_dmgbuild_log"
diff -u "$expected_dmgbuild_log" "$dmgbuild_log"
assert_not_contains "$dmg_hdiutil_log" 'create -srcfolder'
assert_contains "$dmg_hdiutil_log" "attach -readonly -nobrowse -mountpoint $stage/dmg-verify $stage/Loqui.dmg"
assert_eq "$(grep -Fxc -- "detach $stage/dmg-verify" "$dmg_hdiutil_log")" 1
assert_not_contains "$dmg_hdiutil_log" 'detach -force'
assert_contains "$dmg_audit_log" "$stage/dmg-root/Loqui.app"
assert_contains "$dmg_audit_log" "$stage/dmg-verify/Loqui.app"
assert_contains "$dmg_codesign_log" "$stage/dmg-verify/Loqui.app"
assert_contains "$dmg_codesign_log" "-d -r- $stage/dmg-verify/Loqui.app"
assert_contains "$dmg_ds_store_log" "$stage/dmg-verify/.DS_Store"
assert_file "$stage/evidence-work/designated-requirements-dmg.txt"
assert_file "$stage/evidence-work/dmg-visible-root.txt"
assert_file "$stage/evidence-work/dmg-ds-store.txt"
assert_file "$stage/evidence-work/dmg-background-tiff.txt"

run_rejected_dmg_case dmg-generator-failure 'could not create styled DMG' generator
run_rejected_dmg_case dmg-missing-output 'dmgbuild did not create a regular DMG' missing-output
put_file "$tmp/dmg-symlink-target" outside-dmg
run_rejected_dmg_case dmg-symlink-output 'dmgbuild did not create a regular DMG' symlink-output
prepare_dmg_case dmg-hdiutil-verify
DMG_VERIFY_RC=78 expect_failure dmg-hdiutil-verify \
  'generated DMG failed hdiutil verification' run_dmg_case
assert_dmg_rejected_before_release
run_rejected_dmg_case dmg-attach-failure 'could not mount generated DMG' attach
run_rejected_dmg_case dmg-extra-visible 'generated DMG has unexpected visible root items' extra-visible
run_rejected_dmg_case dmg-wrong-applications 'generated DMG Applications link is invalid' wrong-applications
run_rejected_dmg_case dmg-missing-ds-store 'generated DMG is missing .DS_Store' missing-ds-store
run_rejected_dmg_case dmg-ds-window 'generated DMG Finder metadata is invalid' ds-window
run_rejected_dmg_case dmg-ds-chrome 'generated DMG Finder metadata is invalid' ds-chrome
run_rejected_dmg_case dmg-ds-icon-view 'generated DMG Finder metadata is invalid' ds-icon-view
run_rejected_dmg_case dmg-ds-iloc 'generated DMG Finder metadata is invalid' ds-iloc
run_rejected_dmg_case dmg-missing-background 'generated DMG is missing Retina background' missing-background
put_file "$tmp/dmg-background-target.tiff" external-background
prepare_dmg_case dmg-background-symlink
DMG_MUTATION=background-symlink expect_failure dmg-background-symlink \
  'generated DMG background is not a regular non-symlink file' run_dmg_case
[ ! -s "$dmg_tiffutil_log" ] || fail 'background symlink reached TIFF inspection'
assert_not_contains "$dmg_audit_log" "$stage/dmg-verify/Loqui.app"
assert_not_contains "$dmg_codesign_log" "$stage/dmg-verify/Loqui.app"
assert_dmg_rejected_before_release
run_rejected_dmg_case dmg-tiff-single 'Retina background must contain exactly two image directories' tiff-single
run_rejected_dmg_case dmg-tiff-wrong 'Retina background has unexpected frame dimensions' tiff-wrong
prepare_dmg_case dmg-mounted-app-symlink
DMG_MUTATION=mounted-app-symlink expect_failure dmg-mounted-app-symlink \
  'generated DMG app is not a regular non-symlink directory' run_dmg_case
assert_not_contains "$dmg_audit_log" "$stage/dmg-verify/Loqui.app"
assert_not_contains "$dmg_codesign_log" "$stage/dmg-verify/Loqui.app"
assert_dmg_rejected_before_release
run_rejected_dmg_case dmg-mounted-audit '' mounted-audit

prepare_dmg_case dmg-mounted-signature
VERIFY_CODESIGN_MOUNTED_RC=79 expect_failure dmg-mounted-signature '' run_dmg_case
assert_dmg_rejected_before_release

run_rejected_dmg_case dmg-designated-requirements-mismatch \
  'designated requirements differ' dmg-dr

prepare_dmg_case dmg-detach-transient
DMG_DETACH_MODE=transient run_dmg_case
assert_eq "$(cat "$dmg_detach_counter")" 2
assert_not_contains "$dmg_hdiutil_log" 'detach -force'

prepare_dmg_case dmg-detach-exhausted
DMG_DETACH_MODE=fail expect_failure dmg-detach-exhausted \
  'could not cleanly detach DMG verification mount' run_dmg_case
assert_eq "$(cat "$dmg_detach_counter")" 3
assert_contains "$dmg_hdiutil_log" "detach -force $stage/dmg-verify"
assert_dmg_rejected_before_release

prepare_dmg_case dmg-inspection-and-detach
DMG_MUTATION=extra-visible DMG_DETACH_MODE=fail expect_failure dmg-inspection-and-detach \
  'could not cleanly detach DMG verification mount' run_dmg_case
assert_contains "$tmp/dmg-inspection-and-detach.out" \
  'generated DMG has unexpected visible root items'
assert_dmg_rejected_before_release

prepare_dmg_case dmg-root-containment
dmg_root_external="$tmp/dmg-root-external"
mkdir "$dmg_root_external"
put_file "$dmg_root_external/sentinel.txt" root-external-sentinel
ln -s "$dmg_root_external" "$stage/dmg-root"
DMG_MUTATION=none expect_failure dmg-root-containment \
  'DMG staging root already exists' run_dmg_case
assert_not_contains "$dmg_ditto_log" "$dmg_source_app"
[ ! -s "$dmgbuild_log" ] || fail 'unsafe dmg-root reached the generator'
assert_contains "$dmg_root_external/sentinel.txt" root-external-sentinel
assert_dmg_rejected_before_release

prepare_dmg_case dmg-mount-containment
dmg_mount_external="$tmp/dmg-mount-external"
mkdir "$dmg_mount_external"
put_file "$dmg_mount_external/sentinel.txt" mount-external-sentinel
ln -s "$dmg_mount_external" "$stage/dmg-verify"
DMG_MUTATION=none expect_failure dmg-mount-containment \
  'DMG verification mount path already exists' run_dmg_case
assert_not_contains "$dmg_hdiutil_log" attach
assert_contains "$dmg_mount_external/sentinel.txt" mount-external-sentinel
assert_dmg_rejected_before_release

real_tiff_fixture="$tmp/real-background.tiff"
tiffutil -cathidpicheck "$repo_root/build/darwin/dmg/background.png" \
  "$repo_root/build/darwin/dmg/background@2x.png" -out "$real_tiff_fixture" >/dev/null 2>&1
verify_retina_tiff "$real_tiff_fixture" "$tmp/real-background-tiff.txt"

release_root_dir="$saved_dmg_release_root"
version="$saved_dmg_version"
app="$saved_dmg_app"
stage="$saved_dmg_stage"
dmgbuild_python="$saved_dmg_python"

PREFLIGHT_DMGBUILD_PATH='' expect_failure preflight-dmgbuild-missing \
  'LOQUI_DMGBUILD_PYTHON is not set' run_preflight_fixture
PREFLIGHT_DMGBUILD_PATH=relative/python expect_failure preflight-dmgbuild-relative \
  'LOQUI_DMGBUILD_PYTHON must be absolute' run_preflight_fixture
PREFLIGHT_DMGBUILD_PATH="$preflight_non_executable" expect_failure preflight-dmgbuild-executable \
  'dmgbuild Python is not executable' run_preflight_fixture
PREFLIGHT_DMGBUILD_PROBE_RC=19 expect_failure preflight-dmgbuild-probe \
  'could not read installed dmgbuild version' run_preflight_fixture
PREFLIGHT_DMGBUILD_VERSION=9.9.9 expect_failure preflight-dmgbuild-version \
  "installed dmgbuild version is '9.9.9', expected '1.6.7'" run_preflight_fixture
PREFLIGHT_BACKGROUND_SIZE=wrong expect_failure preflight-background-size \
  'background.png has unexpected image properties' run_preflight_fixture
PREFLIGHT_FIXTURE_ROOT="$wrong_digest_root" expect_failure preflight-background-digest \
  'DMG background checksum verification failed' run_preflight_fixture

physical_tmp="$tmp/physical-tmp"
logical_tmp="$tmp/logical-tmp"
mkdir -p "$physical_tmp"
ln -s "$physical_tmp" "$logical_tmp"
TMPDIR="$logical_tmp/" initialize_release_stage
expected_physical_tmp="$(cd "$physical_tmp" && pwd -P)"
case "$stage" in "$expected_physical_tmp"/loqui-release.??????) ;; *) fail "stage is not physical: $stage" ;; esac
[ "$stage" != "$stage_lexical" ] || fail "symlink fixture did not produce distinct stage paths"
safe_stage_path "$stage" || fail "physical stage was rejected"
if safe_stage_path "$stage_lexical"; then fail "lexical stage alias was accepted"; fi
nested_stage="$expected_physical_tmp/loqui-release.abc/xy"
mkdir -p "$nested_stage"
if safe_stage_path "$nested_stage"; then fail "nested stage path was accepted"; fi
rm -rf "$expected_physical_tmp/loqui-release.abc"
mkdir -p "$stage/evidence"
printf '%s\n' \
  "physical=$stage/artifact" \
  "lexical=$stage_lexical/artifact" \
  "repo=$release_root_dir/source" \
  >"$stage/evidence/paths.txt"
normalize_evidence_paths "$stage/evidence"
assert_contains "$stage/evidence/paths.txt" 'physical=$STAGE/artifact'
assert_contains "$stage/evidence/paths.txt" 'lexical=$STAGE/artifact'
assert_contains "$stage/evidence/paths.txt" 'repo=$REPO/source'
assert_not_contains "$stage/evidence/paths.txt" "$stage"
assert_not_contains "$stage/evidence/paths.txt" "$stage_lexical"
assert_not_contains "$stage/evidence/paths.txt" '/private$STAGE'
printf '%s\n' 'malformed=/private$STAGE/artifact' >"$stage/evidence/malformed.txt"
expect_failure evidence-malformed-marker 'malformed /private$STAGE path' \
  normalize_evidence_paths "$stage/evidence"
rm -f "$stage/evidence/malformed.txt"

probe_bin="$tmp/probe-bin"
mkdir "$probe_bin"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'if [ "${FILE_PROBE_RC:-0}" -ne 0 ]; then exit "$FILE_PROBE_RC"; fi' \
  'exec /usr/bin/file "$@"' \
  >"$probe_bin/file"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'case "${GREP_PROBE_MODE:-none}" in' \
  '  fixed-error)' \
  '    for arg in "$@"; do [ "$arg" != -F ] || exit 72; done ;;' \
  '  marker-error)' \
  '    for arg in "$@"; do [ "$arg" != '"'"'/private$STAGE'"'"' ] || exit 73; done ;;' \
  '  regex-error)' \
  '    for arg in "$@"; do [ "$arg" != -E ] || exit 74; done ;;' \
  'esac' \
  'exec /usr/bin/grep "$@"' \
  >"$probe_bin/grep"
chmod +x "$probe_bin/file" "$probe_bin/grep"

run_normalized_path_grep_probe_failure() {
  GREP_PROBE_MODE=fixed-error PATH="$probe_bin:$PATH" \
    normalize_evidence_paths "$stage/evidence"
}
expect_failure evidence-path-grep-error 'could not scan evidence for unnormalized path' \
  run_normalized_path_grep_probe_failure

run_marker_grep_probe_failure() {
  GREP_PROBE_MODE=marker-error PATH="$probe_bin:$PATH" \
    normalize_evidence_paths "$stage/evidence"
}
expect_failure evidence-marker-grep-error 'could not scan evidence for malformed marker' \
  run_marker_grep_probe_failure

saved_project_release_root_dir="$release_root_dir"
saved_project_release_output_dir="$release_output_dir"
saved_project_release_output_dir_physical="$release_output_dir_physical"
cleanup_fixture_repo="$tmp/cleanup-fixture-repo"
mkdir -p "$cleanup_fixture_repo/bin/release"
release_root_dir="$(cd "$cleanup_fixture_repo" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
release_output_dir_physical="$release_output_dir"

physical_stage="$stage"
outside_stage="$tmp/outside-stage"
mkdir -p "$outside_stage"
stage="$outside_stage"
guarded_cleanup_release 2>"$tmp/unsafe-stage-cleanup.out"
assert_contains "$tmp/unsafe-stage-cleanup.out" 'refusing unsafe stage cleanup'
assert_dir "$outside_stage"
stage="$physical_stage"
guarded_cleanup_release
assert_absent "$physical_stage"
stage=""

physical_failure_submit="$tmp/physical-failure-submit.json"
put_file "$physical_failure_submit" '{"id":"physical-failure","status":"Invalid"}'
TMPDIR="$logical_tmp" initialize_tmp_roots
physical_failure_dir="$(TMPDIR="$logical_tmp" preserve_notary_failure \
  physical-failure "$physical_failure_submit" "")"
case "$physical_failure_dir" in
  "$expected_physical_tmp"/loqui-notary-failure.physical-failure.??????) ;;
  *) fail "notary failure directory is not physical: $physical_failure_dir" ;;
esac
assert_file "$physical_failure_dir/notary-submit.json"
rm -rf "$physical_failure_dir"

TMPDIR="$logical_tmp" initialize_release_stage
mkdir -p "$stage/evidence-work" "$stage/Loqui.app/Contents/MacOS"
put_file "$stage/Loqui.app/Contents/MacOS/loqui" fixture
signed_manifest="$stage/signed-macho-manifest.txt"
printf '%s\n' 'Loqui.app/Contents/MacOS/loqui' >"$signed_manifest"
printf '%s\n' '{"id":"evidence-id","status":"Accepted"}' >"$stage/notary-submit.json"
printf '%s\n' \
  '{"status":"Accepted","issues":null,"ticketContents":[{"path":"Loqui.dmg/Loqui.app/Contents/MacOS/loqui"}]}' \
  >"$stage/notary-log.json"
printf '%s\n' \
  '## Loqui.app' \
  'designated => identifier "com.jualopezmo.loquigo" and anchor apple generic' \
  >"$stage/evidence-work/designated-requirements.txt"
printf 'physical=%s\nlexical=%s\nrepo=%s\n' \
  "$stage/output" "$stage_lexical/output" "$release_root_dir/output" \
  >"$stage/evidence-work/source-paths.txt"
submit_rc=0
submission_status=Accepted
submission_id="evidence-id"
app="$stage/Loqui.app"
phase_check_log
assert_file "$stage/evidence/designated-requirements.txt"
assert_contains "$stage/evidence/source-paths.txt" 'physical=$STAGE/output'
assert_contains "$stage/evidence/source-paths.txt" 'lexical=$STAGE/output'
assert_contains "$stage/evidence/source-paths.txt" 'repo=$REPO/output'
assert_not_contains "$stage/evidence/source-paths.txt" '/private$STAGE'

dmg="$stage/Loqui.dmg"
put_file "$dmg" probe-dmg
file_probe_publish_repo="$tmp/file-probe-publish-repo"
grep_probe_publish_repo="$tmp/grep-probe-publish-repo"
mkdir "$file_probe_publish_repo" "$grep_probe_publish_repo"

run_file_probe_before_publish() {
  release_root_dir="$(cd "$file_probe_publish_repo" && pwd -P)"
  release_output_dir="$release_root_dir/bin/release"
  FILE_PROBE_RC=72 PATH="$probe_bin:$PATH" phase_check_log || return 1
  guarded_atomic_publish "$dmg" "$stage/evidence" "$release_output_dir" \
    0.1.0 file-probe-id Loqui-0.1.0-macos-arm64.dmg
}
expect_failure packaged-file-probe-error 'could not inspect file type' \
  run_file_probe_before_publish
assert_absent "$file_probe_publish_repo/bin"

run_secret_grep_before_publish() {
  release_root_dir="$(cd "$grep_probe_publish_repo" && pwd -P)"
  release_output_dir="$release_root_dir/bin/release"
  GREP_PROBE_MODE=regex-error PATH="$probe_bin:$PATH" phase_check_log || return 1
  guarded_atomic_publish "$dmg" "$stage/evidence" "$release_output_dir" \
    0.1.0 grep-probe-id Loqui-0.1.0-macos-arm64.dmg
}
expect_failure evidence-secret-grep-error 'could not scan evidence for checkout paths or secrets' \
  run_secret_grep_before_publish
assert_absent "$grep_probe_publish_repo/bin"

put_file "$stage/evidence/secret-probe.txt" '"password": "fixture-secret"'
expect_failure evidence-secret-found 'evidence contains a checkout path or secret field' \
  phase_check_log
rm -f "$stage/evidence/secret-probe.txt"
phase_check_stage="$stage"
guarded_cleanup_release
assert_absent "$phase_check_stage"
stage=""

notary_bin="$tmp/notary-bin"
notary_stage="$tmp/notary-stage"
mkdir -p "$notary_bin" "$notary_stage" "$tmp/notary-failures"
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  '[ "$1" = notarytool ] || exit 94' \
  'case "$2" in' \
  '  submit)' \
  '    counter="${NOTARY_SUBMIT_COUNTER:?}"' \
  '    count=0' \
  '    [ ! -f "$counter" ] || count="$(/bin/cat "$counter")"' \
  '    printf "%s\n" "$((count + 1))" >"$counter"' \
  '    json="${NOTARY_SUBMIT_JSON:-}"' \
  '    [ -n "$json" ] || json='"'"'{"id":"default-id","status":"Accepted"}'"'"'' \
  '    printf "%s\n" "$json"' \
  '    exit "${NOTARY_SUBMIT_RC:-0}"' \
  '    ;;' \
  '  log)' \
  '    counter="${NOTARY_LOG_COUNTER:?}"' \
  '    count=0' \
  '    [ ! -f "$counter" ] || count="$(/bin/cat "$counter")"' \
  '    count=$((count + 1))' \
  '    printf "%s\n" "$count" >"$counter"' \
  '    if [ "$count" -ge "${NOTARY_LOG_SUCCESS_ON:-99}" ]; then' \
  '      json="${NOTARY_LOG_JSON:-}"' \
  '      [ -n "$json" ] || json='"'"'{"status":"Accepted"}'"'"'' \
  '      printf "%s\n" "$json" >"$4"' \
  '      exit 0' \
  '    fi' \
  '    exit "${NOTARY_LOG_FAILURE_RC:-65}"' \
  '    ;;' \
  '  *) exit 95 ;;' \
  'esac' \
  >"$notary_bin/xcrun"
chmod +x "$notary_bin/xcrun"

stage="$notary_stage"
dmg="$stage/Loqui.dmg"
put_file "$dmg" dmg
notary_accepted_submit_count="$tmp/notary-accepted-submit-count"
PATH="$notary_bin:$PATH" \
NOTARY_SUBMIT_COUNTER="$notary_accepted_submit_count" \
NOTARY_SUBMIT_JSON='{"id":"accepted-id","status":"Accepted","message":"complete"}' \
NOTARY_SUBMIT_RC=0 phase_submit
assert_eq "$submission_id" accepted-id
assert_eq "$submission_status" Accepted
assert_eq "$submit_rc" 0
assert_eq "$(cat "$notary_accepted_submit_count")" 1
assert_contains "$stage/notary-submit.json" '"message":"complete"'

notary_invalid_submit_count="$tmp/notary-invalid-submit-count"
PATH="$notary_bin:$PATH" \
NOTARY_SUBMIT_COUNTER="$notary_invalid_submit_count" \
NOTARY_SUBMIT_JSON='{"id":"invalid-id","status":"Invalid","message":"rejected"}' \
NOTARY_SUBMIT_RC=64 phase_submit
assert_eq "$submission_id" invalid-id
assert_eq "$submission_status" Invalid
assert_eq "$submit_rc" 64
assert_eq "$(cat "$notary_invalid_submit_count")" 1
assert_contains "$stage/notary-submit.json" '"message":"rejected"'
printf '%s\n' '{"status":"Invalid","issues":[{"severity":"error"}]}' >"$stage/notary-log.json"
TMPDIR="$tmp/notary-failures" expect_failure submit-nonzero-check \
  "notary submission invalid-id ended 'Invalid' (rc=64)" phase_check_log

run_submit_fixture() {
  export NOTARY_SUBMIT_COUNTER="$1"
  export NOTARY_SUBMIT_JSON="$2"
  export NOTARY_SUBMIT_RC="${3:-0}"
  export TMPDIR="$tmp/notary-failures"
  PATH="$notary_bin:$PATH" phase_submit
}

expect_failure submit-malformed-json 'missing submission id' run_submit_fixture \
  "$tmp/notary-malformed-submit-count" not-json
assert_eq "$(cat "$tmp/notary-malformed-submit-count")" 1
expect_failure submit-missing-id 'missing submission id' run_submit_fixture \
  "$tmp/notary-missing-id-submit-count" '{"status":"Accepted"}'
assert_eq "$(cat "$tmp/notary-missing-id-submit-count")" 1
run_submit_fixture "$tmp/notary-missing-status-submit-count" '{"id":"missing-status-id"}'
assert_eq "$(cat "$tmp/notary-missing-status-submit-count")" 1
assert_eq "$submission_id" missing-status-id
assert_eq "$submission_status" ''
printf '%s\n' '{"status":"Accepted","issues":null,"ticketContents":[{"path":"Loqui.app"}]}' \
  >"$stage/notary-log.json"
TMPDIR="$tmp/notary-failures" expect_failure submit-missing-status-check \
  "notary submission missing-status-id ended '' (rc=0)" phase_check_log

run_fetch_log_fixture() {
  export NOTARY_LOG_COUNTER="$1"
  export NOTARY_LOG_SUCCESS_ON="$2"
  export NOTARY_LOG_FAILURE_RC=65
  export LOQUI_NOTARY_LOG_RETRY_DELAY=0
  export TMPDIR="$tmp/notary-failures"
  PATH="$notary_bin:$PATH" phase_fetch_log
}

submission_id=accepted-id
rm -f "$tmp/notary-success-count"
run_fetch_log_fixture "$tmp/notary-success-count" 3
assert_eq "$(cat "$tmp/notary-success-count")" 3
assert_eq "$log_rc" 0
assert_contains "$stage/notary-log.json" '"status":"Accepted"'

rm -f "$tmp/notary-failure-count"
expect_failure notary-log-bounded 'notary log retrieval failed for accepted-id' \
  run_fetch_log_fixture "$tmp/notary-failure-count" 99
assert_eq "$(cat "$tmp/notary-failure-count")" 3

boundary_bin="$tmp/boundary-bin"
boundary_log="$tmp/boundary.log"
mkdir -p "$boundary_bin"
for boundary_tool in xcrun hdiutil codesign spctl; do
  printf '%s\n' \
    '#!/bin/bash' \
    'set -euo pipefail' \
    'tool="$(/usr/bin/basename "$0")"' \
    'key="$tool"' \
    'if [ "$tool" = xcrun ]; then key="xcrun-$1-$2"; fi' \
    'if [ "$tool" = hdiutil ]; then key="hdiutil-$1"; fi' \
    'if [ "$tool" = codesign ]; then key="codesign-$1"; fi' \
    'if [ "$tool" = spctl ]; then key=spctl-assess; fi' \
    'printf "%s %s\n" "$tool" "$*" >>"$BOUNDARY_LOG"' \
    '[ "${FAIL_BOUNDARY:-none}" != "$key" ] || exit 67' \
    >"$boundary_bin/$boundary_tool"
  chmod +x "$boundary_bin/$boundary_tool"
done

: >"$boundary_log"
BOUNDARY_LOG="$boundary_log" PATH="$boundary_bin:$PATH" phase_staple
BOUNDARY_LOG="$boundary_log" PATH="$boundary_bin:$PATH" phase_verify_staple
BOUNDARY_LOG="$boundary_log" PATH="$boundary_bin:$PATH" phase_gatekeeper
assert_contains "$boundary_log" "xcrun stapler staple $dmg"
assert_contains "$boundary_log" "xcrun stapler validate $dmg"
assert_contains "$boundary_log" "hdiutil verify $dmg"
assert_contains "$boundary_log" "codesign --verify --verbose=2 $dmg"
assert_contains "$boundary_log" "spctl --assess --type open --context context:primary-signature --verbose=2 $dmg"
assert_eq "$(grep -c '^xcrun stapler staple ' "$boundary_log")" 1
assert_eq "$(grep -c "^xcrun stapler staple $dmg$" "$boundary_log")" 1
if grep '^xcrun stapler staple ' "$boundary_log" | grep -F '.app' >/dev/null; then
  fail "release stapled an inner app instead of only the outer DMG"
fi

run_boundary_failure() {
  boundary_function="$1"
  export FAIL_BOUNDARY="$2"
  export BOUNDARY_LOG="$boundary_log"
  PATH="$boundary_bin:$PATH" "$boundary_function"
}
expect_failure boundary-staple '' run_boundary_failure phase_staple xcrun-stapler-staple
expect_failure boundary-validate '' run_boundary_failure phase_verify_staple xcrun-stapler-validate
expect_failure boundary-hdiutil '' run_boundary_failure phase_verify_staple hdiutil-verify
expect_failure boundary-codesign '' run_boundary_failure phase_verify_staple codesign---verify
expect_failure boundary-gatekeeper '' run_boundary_failure phase_gatekeeper spctl-assess

integrated_stage_lexical="$tmp/integrated-outermost-stage"
integrated_repo="$tmp/integrated-outermost-repo"
integrated_failures="$tmp/integrated-notary-failures"
integrated_boundary_log="$tmp/integrated-boundary.log"
integrated_submit_count="$tmp/integrated-submit-count"
integrated_log_count="$tmp/integrated-log-count"
mkdir -p "$integrated_stage_lexical/evidence-work" \
  "$integrated_stage_lexical/Loqui.app/Contents/MacOS" "$integrated_repo" "$integrated_failures"
put_file "$integrated_stage_lexical/Loqui.app/Contents/MacOS/loqui" integrated-app
put_file "$integrated_stage_lexical/Loqui.dmg" integrated-dmg
printf '%s\n' 'Loqui.app/Contents/MacOS/loqui' \
  >"$integrated_stage_lexical/signed-macho-manifest.txt"
printf '%s\n' \
  '## Loqui.app' \
  'designated => identifier "com.jualopezmo.loquigo" and anchor apple generic' \
  >"$integrated_stage_lexical/evidence-work/designated-requirements.txt"
cp "$integrated_stage_lexical/evidence-work/designated-requirements.txt" \
  "$integrated_stage_lexical/evidence-work/designated-requirements-dmg.txt"
: >"$integrated_boundary_log"

saved_integrated_stage="$stage"
saved_integrated_stage_lexical="$stage_lexical"
saved_integrated_app="$app"
saved_integrated_dmg="$dmg"
saved_integrated_manifest="$signed_manifest"
saved_integrated_release_root="$release_root_dir"
stage="$(cd "$integrated_stage_lexical" && pwd -P)"
stage_lexical="$stage"
app="$stage/Loqui.app"
dmg="$stage/Loqui.dmg"
signed_manifest="$stage/signed-macho-manifest.txt"
release_root_dir="$(cd "$integrated_repo" && pwd -P)"

run_integrated_outermost_flow() {
  export NOTARY_SUBMIT_COUNTER="$integrated_submit_count"
  export NOTARY_SUBMIT_JSON='{"id":"integrated-id","status":"Accepted"}'
  export NOTARY_SUBMIT_RC=0
  export NOTARY_LOG_COUNTER="$integrated_log_count"
  export NOTARY_LOG_SUCCESS_ON=1
  export NOTARY_LOG_JSON='{"status":"Accepted","issues":null,"ticketContents":[{"path":"Loqui.dmg/Loqui.app/Contents/MacOS/loqui"}]}'
  export LOQUI_NOTARY_LOG_RETRY_DELAY=0
  export TMPDIR="$integrated_failures"
  PATH="$notary_bin:$PATH" phase_submit || return 1
  PATH="$notary_bin:$PATH" phase_fetch_log || return 1
  phase_check_log || return 1
  BOUNDARY_LOG="$integrated_boundary_log" PATH="$boundary_bin:$PATH" phase_staple || return 1
  BOUNDARY_LOG="$integrated_boundary_log" PATH="$boundary_bin:$PATH" phase_verify_staple || return 1
  BOUNDARY_LOG="$integrated_boundary_log" PATH="$boundary_bin:$PATH" phase_gatekeeper || return 1
}
run_integrated_outermost_flow
assert_eq "$(cat "$integrated_submit_count")" 1
assert_eq "$(grep -c '^xcrun stapler staple ' "$integrated_boundary_log")" 1
assert_eq "$(grep -c "^xcrun stapler staple $dmg$" "$integrated_boundary_log")" 1
if grep '^xcrun stapler staple ' "$integrated_boundary_log" | grep -F '.app' >/dev/null; then
  fail "integrated release stapled an inner app"
fi
assert_file "$stage/evidence/designated-requirements.txt"
assert_file "$stage/evidence/designated-requirements-dmg.txt"

stage="$saved_integrated_stage"
stage_lexical="$saved_integrated_stage_lexical"
app="$saved_integrated_app"
dmg="$saved_integrated_dmg"
signed_manifest="$saved_integrated_manifest"
release_root_dir="$saved_integrated_release_root"

publish_repo="$tmp/publish-repo"
publish_root="$publish_repo/bin/release"
publish_evidence="$tmp/publish-source-evidence"
publish_source_dmg="$tmp/publish-source.dmg"
publish_bin="$tmp/publish-bin"
mkdir -p "$publish_root/evidence/0.1.0/older-id" "$publish_evidence" "$publish_bin"
put_file "$publish_root/Loqui-0.1.0-macos-arm64.dmg" 'old accepted DMG'
put_file "$publish_root/evidence/0.1.0/older-id/report.txt" 'older evidence'
put_file "$publish_source_dmg" 'new candidate DMG'
put_file "$publish_evidence/report.txt" 'new evidence'
printf '%s\n' \
  '#!/bin/bash' \
  'set -euo pipefail' \
  'if [ "${FAIL_FINAL_MV:-0}" = 1 ] && [[ "$2" = *.candidate.?????? ]]; then exit 86; fi' \
  'exec /bin/mv "$@"' \
  >"$publish_bin/mv"
chmod +x "$publish_bin/mv"

saved_release_root_dir="$release_root_dir"
saved_release_output_dir="$release_output_dir"
release_root_dir="$(cd "$publish_repo" && pwd -P)"
publish_root="$release_root_dir/bin/release"
release_output_dir="$publish_root"

wrong_name_repo="$tmp/wrong-name-publish-repo"
mkdir "$wrong_name_repo"
wrong_name_root="$(cd "$wrong_name_repo" && pwd -P)"
saved_wrong_name_release_root="$release_root_dir"
release_root_dir="$wrong_name_root"
run_expect_fail_msg "publication DMG name does not match version" \
  guarded_atomic_publish "$publish_source_dmg" "$publish_evidence" \
  "$wrong_name_root/bin/release" 0.1.0 wrong-name-id Loqui-9.9.9-macos-arm64.dmg
release_root_dir="$saved_wrong_name_release_root"

run_atomic_publish_failure() {
  FAIL_FINAL_MV=1 PATH="$publish_bin:$PATH" guarded_atomic_publish \
    "$publish_source_dmg" "$publish_evidence" "$publish_root" 0.1.0 failed-id \
    Loqui-0.1.0-macos-arm64.dmg
}
set +e
run_atomic_publish_failure >"$tmp/atomic-final-rename.out" 2>&1
atomic_failure_rc=$?
set -e
[ "$atomic_failure_rc" -ne 0 ] || fail "atomic final rename unexpectedly passed"
assert_contains "$publish_root/Loqui-0.1.0-macos-arm64.dmg" 'old accepted DMG'
assert_file "$publish_root/evidence/0.1.0/older-id/report.txt"
assert_absent "$publish_root/evidence/0.1.0/failed-id"
failed_hidden_dmg_candidate="$hidden_dmg_candidate"
assert_file "$failed_hidden_dmg_candidate"
stage=""
guarded_cleanup_release
assert_absent "$failed_hidden_dmg_candidate"

misleading_candidate="$publish_root/.user-candidate-not-owned"
outside_candidate="$tmp/outside-candidate.dmg"
outside_evidence="$tmp/outside-candidate-evidence"
put_file "$misleading_candidate" owner-data
put_file "$outside_candidate" outside-data
put_file "$outside_evidence/report.txt" outside-evidence
hidden_dmg_candidate="$misleading_candidate"
hidden_evidence_candidate="$outside_evidence"
hidden_dmg_candidate_owned=0
hidden_evidence_candidate_owned=0
guarded_cleanup_release
assert_file "$misleading_candidate"
assert_file "$outside_candidate"
assert_file "$outside_evidence/report.txt"
hidden_dmg_candidate="$outside_candidate"
hidden_evidence_candidate="$outside_evidence"
hidden_dmg_candidate_owned=1
hidden_evidence_candidate_owned=1
guarded_cleanup_release 2>"$tmp/outside-candidate-cleanup.out"
assert_contains "$tmp/outside-candidate-cleanup.out" 'refusing unsafe DMG candidate cleanup'
assert_contains "$tmp/outside-candidate-cleanup.out" 'refusing unsafe evidence candidate cleanup'
assert_file "$outside_candidate"
assert_file "$outside_evidence/report.txt"
safe_cleanup_dmg="$publish_root/.Loqui-0.1.0.cleanup-id.candidate.ABC123"
safe_cleanup_evidence="$publish_root/.evidence-0.1.0.cleanup-id.candidate.ABC123"
put_file "$safe_cleanup_dmg" safe-candidate
put_file "$safe_cleanup_evidence/report.txt" safe-candidate-evidence
hidden_dmg_candidate="$safe_cleanup_dmg"
hidden_evidence_candidate="$safe_cleanup_evidence"
hidden_dmg_candidate_owned=1
hidden_evidence_candidate_owned=1
guarded_cleanup_release
assert_absent "$safe_cleanup_dmg"
assert_absent "$safe_cleanup_evidence"

collision_dmg="$publish_root/.Loqui-0.1.0.collision-id.candidate.ZZZ999"
collision_evidence="$publish_root/.evidence-0.1.0.collision-id.candidate.ZZZ999"
put_file "$collision_dmg" preexisting-collision
put_file "$collision_evidence/report.txt" preexisting-collision-evidence
hidden_dmg_candidate="$collision_dmg"
hidden_evidence_candidate="$collision_evidence"
hidden_dmg_candidate_owned=0
hidden_evidence_candidate_owned=0
guarded_cleanup_release
assert_contains "$collision_dmg" preexisting-collision
assert_contains "$collision_evidence/report.txt" preexisting-collision-evidence

# These values are intentionally isolated so the real EXIT trap behavior can be exercised.
# shellcheck disable=SC2030
run_preexisting_collision_trap() (
  stage=""
  hidden_dmg_candidate="$collision_dmg"
  hidden_evidence_candidate="$collision_evidence"
  hidden_dmg_candidate_owned=0
  hidden_evidence_candidate_owned=0
  trap guarded_cleanup_release EXIT
  false
)
set +e
run_preexisting_collision_trap
collision_trap_rc=$?
set -e
[ "$collision_trap_rc" -ne 0 ] || fail "collision trap fixture unexpectedly passed"
assert_contains "$collision_dmg" preexisting-collision
assert_contains "$collision_evidence/report.txt" preexisting-collision-evidence

mktemp_collision_bin="$tmp/mktemp-collision-bin"
mkdir -p "$mktemp_collision_bin"
printf '%s\n' \
  '#!/bin/bash' \
  'printf "%s\n" "${MKTEMP_COLLISION_PATH:?}"' \
  'exit 73' \
  >"$mktemp_collision_bin/mktemp"
chmod +x "$mktemp_collision_bin/mktemp"
set +e
MKTEMP_COLLISION_PATH="$collision_dmg" PATH="$mktemp_collision_bin:$PATH" \
  guarded_atomic_publish "$publish_source_dmg" "$publish_evidence" "$publish_root" \
    0.1.0 collision-allocation-id Loqui-0.1.0-macos-arm64.dmg \
    >"$tmp/mktemp-collision-publish.out" 2>&1
mktemp_collision_rc=$?
set -e
[ "$mktemp_collision_rc" -ne 0 ] || fail "failed exclusive allocation unexpectedly published"
# The trap fixture above changed these names only in its subshell.
# shellcheck disable=SC2031
assert_eq "$hidden_dmg_candidate_owned" 0
# shellcheck disable=SC2031
assert_eq "$hidden_evidence_candidate_owned" 0
guarded_cleanup_release
assert_contains "$collision_dmg" preexisting-collision
assert_contains "$collision_evidence/report.txt" preexisting-collision-evidence

evidence_symlink_repo="$tmp/evidence-symlink-publish-repo"
evidence_symlink_external="$tmp/evidence-symlink-publish-external"
mkdir -p "$evidence_symlink_repo/bin/release" "$evidence_symlink_external"
put_file "$evidence_symlink_external/sentinel.txt" evidence-external-sentinel
ln -s "$evidence_symlink_external" "$evidence_symlink_repo/bin/release/evidence"
release_root_dir="$(cd "$evidence_symlink_repo" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
hidden_dmg_candidate=""
hidden_evidence_candidate=""
hidden_dmg_candidate_owned=0
hidden_evidence_candidate_owned=0
set +e
guarded_atomic_publish "$publish_source_dmg" "$publish_evidence" "$release_output_dir" \
  0.1.0 evidence-symlink-id Loqui-0.1.0-macos-arm64.dmg \
  >"$tmp/evidence-symlink-publish.out" 2>&1
evidence_symlink_publish_rc=$?
set -e
[ "$evidence_symlink_publish_rc" -ne 0 ] \
  || fail "symlinked evidence output unexpectedly published"
assert_contains "$evidence_symlink_external/sentinel.txt" evidence-external-sentinel
assert_absent "$evidence_symlink_external/0.1.0"
if find "$release_output_dir" -maxdepth 1 -name '.*candidate*' -print | grep -q .; then
  fail "rejected evidence symlink left a hidden candidate"
fi

version_symlink_repo="$tmp/version-symlink-publish-repo"
version_symlink_external="$tmp/version-symlink-publish-external"
mkdir -p "$version_symlink_repo/bin/release/evidence" "$version_symlink_external"
put_file "$version_symlink_external/sentinel.txt" version-external-sentinel
ln -s "$version_symlink_external" "$version_symlink_repo/bin/release/evidence/0.1.0"
release_root_dir="$(cd "$version_symlink_repo" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
hidden_dmg_candidate=""
hidden_evidence_candidate=""
hidden_dmg_candidate_owned=0
hidden_evidence_candidate_owned=0
set +e
guarded_atomic_publish "$publish_source_dmg" "$publish_evidence" "$release_output_dir" \
  0.1.0 version-symlink-id Loqui-0.1.0-macos-arm64.dmg \
  >"$tmp/version-symlink-publish.out" 2>&1
version_symlink_publish_rc=$?
set -e
[ "$version_symlink_publish_rc" -ne 0 ] \
  || fail "symlinked version evidence output unexpectedly published"
assert_contains "$version_symlink_external/sentinel.txt" version-external-sentinel
assert_absent "$version_symlink_external/version-symlink-id"
if find "$release_output_dir" -maxdepth 1 -name '.*candidate*' -print | grep -q .; then
  fail "rejected evidence version symlink left a hidden candidate"
fi

symlink_repo="$tmp/symlink-publish-repo"
symlink_external="$tmp/symlink-publish-external"
mkdir -p "$symlink_repo/bin" "$symlink_external"
put_file "$symlink_external/sentinel.txt" external-sentinel
ln -s "$symlink_external" "$symlink_repo/bin/release"
release_root_dir="$(cd "$symlink_repo" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
hidden_dmg_candidate=""
hidden_evidence_candidate=""
hidden_dmg_candidate_owned=0
hidden_evidence_candidate_owned=0
set +e
guarded_atomic_publish "$publish_source_dmg" "$publish_evidence" "$release_output_dir" \
  0.1.0 symlink-id Loqui-0.1.0-macos-arm64.dmg >"$tmp/symlink-publish.out" 2>&1
symlink_publish_rc=$?
set -e
[ "$symlink_publish_rc" -ne 0 ] || fail "symlinked release output unexpectedly published"
assert_contains "$symlink_external/sentinel.txt" external-sentinel
assert_absent "$symlink_external/Loqui-0.1.0-macos-arm64.dmg"
hidden_dmg_candidate=""
hidden_evidence_candidate=""
hidden_dmg_candidate_owned=0
hidden_evidence_candidate_owned=0
release_output_dir="$saved_release_output_dir"
release_root_dir="$saved_release_root_dir"
stage=""

phase_publish_args="$tmp/phase-publish-args"
original_atomic_publish="$(declare -f atomic_publish)"
atomic_publish() {
  printf '<%s>\n' "$@" >"$phase_publish_args"
}
dmg="$tmp/phase-publish.dmg"
stage="$tmp/phase-publish-stage"
release_output_dir="$tmp/phase-publish-output"
release_root_dir="$repo_root"
version=0.1.0
submission_id=phase-publish-id
phase_publish
phase_publish_expected="$tmp/phase-publish-expected"
printf '<%s>\n' \
  "$dmg" "$stage/evidence" "$release_output_dir" 0.1.0 phase-publish-id \
  Loqui-0.1.0-macos-arm64.dmg >"$phase_publish_expected"
diff -u "$phase_publish_expected" "$phase_publish_args"
eval "$original_atomic_publish"

phase_log="$tmp/phases"
export LOQUI_PHASE_LOG="$phase_log"
for function_name in \
  phase_preflight phase_build phase_build_helpers phase_bundle phase_audit_unsigned \
  phase_sign_app phase_verify_app phase_create_dmg phase_sign_dmg phase_verify_dmg \
  phase_submit phase_fetch_log phase_check_log phase_staple phase_verify_staple \
  phase_gatekeeper phase_publish; do
  eval "$function_name() { :; }"
done
run_release
expected_phases="$tmp/expected-phases"
printf '%s\n' preflight build build-helpers bundle audit-unsigned sign-app verify-app create-dmg \
  sign-dmg verify-dmg submit fetch-log check-log staple verify-staple gatekeeper publish \
  >"$expected_phases"
diff -u "$expected_phases" "$phase_log"

manifest="$tmp/signed-manifest"
printf '%s\n' \
  Loqui.app/Contents/MacOS/loqui \
  Loqui.app/Contents/Helpers/globe-listener \
  Loqui.app/Contents/Helpers/macos-stt \
  Loqui.app/Contents/Helpers/whisper-stt \
  Loqui.app/Contents/Frameworks/libSDL2-2.0.0.dylib \
  Loqui.app/Contents/Frameworks/libwhisper.1.9.1.dylib \
  Loqui.app/Contents/Frameworks/libggml.0.16.0.dylib \
  Loqui.app/Contents/Frameworks/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech \
  >"$manifest"

accepted_log="$tmp/accepted.json"
cat >"$accepted_log" <<'JSON'
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
JSON
check_ticket_log "$accepted_log" "$manifest"
jq '.issues=[]' "$accepted_log" >"$tmp/empty-issues.json"
check_ticket_log "$tmp/empty-issues.json" "$manifest"

expect_check_failure() {
  expected="$1"
  log="$2"
  output="$tmp/check-failure"
  if check_ticket_log "$log" "$manifest" >"$output" 2>&1; then fail "notary log unexpectedly passed"; fi
  assert_contains "$output" "$expected"
}

jq '.issues=[{"severity":"error","message":"bad signature"}]' "$accepted_log" >"$tmp/error.json"
expect_check_failure 'error issue' "$tmp/error.json"
jq 'del(.ticketContents)' "$accepted_log" >"$tmp/no-ticket.json"
expect_check_failure ticketContents "$tmp/no-ticket.json"
jq '.ticketContents=null' "$accepted_log" >"$tmp/null-ticket.json"
expect_check_failure ticketContents "$tmp/null-ticket.json"
jq '.ticketContents=[]' "$accepted_log" >"$tmp/empty-ticket.json"
expect_check_failure ticketContents "$tmp/empty-ticket.json"
jq '.ticketContents[0].path="OtherLoqui.app/Contents/MacOS/loqui"' "$accepted_log" >"$tmp/wrong-suffix.json"
expect_check_failure 'Contents/MacOS/loqui' "$tmp/wrong-suffix.json"
jq '.issues=[{"severity":"warning","message":"warning retained"}]' "$accepted_log" >"$tmp/warning.json"
check_ticket_log "$tmp/warning.json" "$manifest"

source_dmg="$tmp/source.dmg"
evidence="$tmp/evidence"
fresh_clone_repo="$tmp/fresh-clone-publish-repo"
mkdir "$fresh_clone_repo"
release_root_dir="$(cd "$fresh_clone_repo" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
put_file "$source_dmg" 'new accepted artifact'
put_file "$evidence/report.txt" evidence
set +e
fresh_clone_published="$(guarded_atomic_publish "$source_dmg" "$evidence" \
  "$release_output_dir" 0.1.0 fresh-clone-id Loqui-0.1.0-macos-arm64.dmg \
  2>"$tmp/fresh-clone-publish.out")"
fresh_clone_publish_rc=$?
set -e
[ "$fresh_clone_publish_rc" -eq 0 ] || fail "fresh-clone publication could not create bin/release"
assert_eq "$fresh_clone_published" \
  "$release_root_dir/bin/release/Loqui-0.1.0-macos-arm64.dmg"
assert_file "$fresh_clone_published"
assert_file "$release_root_dir/bin/release/evidence/0.1.0/fresh-clone-id/report.txt"

bottom_publish_repo="$tmp/bottom-publish-repo"
mkdir -p "$bottom_publish_repo/bin/release"
release_root_dir="$(cd "$bottom_publish_repo" && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
release_root="$release_output_dir"
final_dmg="$release_root/Loqui-0.1.0-macos-arm64.dmg"
put_file "$final_dmg" 'old accepted artifact'
published_path="$(guarded_atomic_publish "$source_dmg" "$evidence" "$release_root" 0.1.0 \
  11111111-1111-1111-1111-111111111111 Loqui-0.1.0-macos-arm64.dmg)"
assert_eq "$published_path" "$final_dmg"
assert_contains "$final_dmg" 'new accepted artifact'
assert_file "$release_root/evidence/0.1.0/11111111-1111-1111-1111-111111111111/report.txt"
if find "$release_root" -maxdepth 1 -name '.*candidate*' -print | grep -q .; then
  fail "atomic publication left a hidden candidate"
fi

failure_evidence_stage="$tmp/failure-evidence-stage"
failure_evidence_repo="$tmp/failure-evidence-repo"
mkdir -p "$failure_evidence_stage" "$failure_evidence_repo"
stage="$(cd "$failure_evidence_stage" && pwd -P)"
stage_lexical="$stage"
release_root_dir="$(cd "$failure_evidence_repo" && pwd -P)"
submit="$tmp/ci-notary-submit.json"
log="$tmp/ci-notary-log.json"
printf '{"id":"ci-id","status":"Invalid","path":"%s/artifact"}\n' "$stage" >"$submit"
printf '{"status":"Invalid","path":"%s/source"}\n' "$release_root_dir" >"$log"
configured_failure_dir="$test_tmp_physical/ci-notary-failure-evidence"
failure_dir="$(LOQUI_NOTARY_FAILURE_DIR="$configured_failure_dir" \
  TMPDIR="$tmp" preserve_notary_failure ci-id "$submit" "$log")"
assert_eq "$failure_dir" "$configured_failure_dir"
assert_file "$configured_failure_dir/notary-submit.json"
assert_file "$configured_failure_dir/notary-log.json"
assert_contains "$configured_failure_dir/notary-submit.json" '"path":"$STAGE/artifact"'
assert_contains "$configured_failure_dir/notary-log.json" '"path":"$REPO/source"'
assert_not_contains "$configured_failure_dir/notary-submit.json" "$stage"
assert_not_contains "$configured_failure_dir/notary-log.json" "$release_root_dir"

printf '%s\n' '{"id":"secret-id","password":"must-not-upload"}' >"$submit"
LOQUI_NOTARY_FAILURE_DIR="$test_tmp_physical/ci-notary-secret-evidence"
export LOQUI_NOTARY_FAILURE_DIR
expect_failure notary-failure-secret 'failure evidence contains a checkout path or secret field' \
  preserve_notary_failure secret-id "$submit" "$log"
assert_absent "$LOQUI_NOTARY_FAILURE_DIR"
unset LOQUI_NOTARY_FAILURE_DIR

submit="$tmp/notary-submit.json"
printf '%s\n' '{"id":"bad-id","status":"Invalid"}' >"$submit"
printf '%s\n' '{"status":"Invalid"}' >"$tmp/notary-log.json"
failure_dir="$(TMPDIR="$tmp" preserve_notary_failure bad-id "$submit" "$tmp/notary-log.json")"
assert_test_tmp_path "$failure_dir"
assert_file "$failure_dir/notary-submit.json"
assert_file "$failure_dir/notary-log.json"
failure_prefix="$test_tmp_physical"
case "$failure_dir" in "$failure_prefix"/loqui-notary-failure.bad-id.*) ;; *) fail "unsafe failure evidence path: $failure_dir" ;; esac
rm -rf "$failure_dir"

release_root_dir="$saved_project_release_root_dir"
release_output_dir="$saved_project_release_output_dir"
release_output_dir_physical="$saved_project_release_output_dir_physical"

echo "release-macos-test: PASS"
