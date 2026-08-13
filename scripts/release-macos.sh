#!/usr/bin/env bash
set -euo pipefail

release_root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
release_output_dir="$release_root_dir/bin/release"
release_output_dir_physical=""
dmgbuild_python="${LOQUI_DMGBUILD_PYTHON:-}"
dmg_verify_mount=""
dmg_verify_mounted=0
profile="${LOQUI_NOTARY_PROFILE:-loqui-notary}"
notary_keychain="${LOQUI_NOTARY_KEYCHAIN:-}"
notary_auth_args=(--keychain-profile "$profile")
if [ -n "$notary_keychain" ]; then
  notary_auth_args+=(--keychain "$notary_keychain")
fi
version=""
identity=""
stage=""
stage_lexical=""
tmp_root_lexical=""
tmp_root_physical=""
app=""
dmg=""
zip=""
submission_id=""
submission_status=""
submit_rc=1
log_rc=1
zip_submission_id=""
zip_submission_status=""
zip_submit_rc=1
zip_log_rc=1
signed_manifest=""
hidden_dmg_candidate=""
hidden_zip_candidate=""
hidden_evidence_candidate=""
hidden_dmg_candidate_owned=0
hidden_zip_candidate_owned=0
hidden_evidence_candidate_owned=0

die() { echo "release-macos: $*" >&2; return 1; }
require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    die "missing required command: $1"
    return 1
  fi
}

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

validate_dmgbuild() {
  [ -n "$dmgbuild_python" ] || {
    die "LOQUI_DMGBUILD_PYTHON is not set"
    return 1
  }
  case "$dmgbuild_python" in
    /*) ;;
    *) die "LOQUI_DMGBUILD_PYTHON must be absolute"; return 1 ;;
  esac
  [ -x "$dmgbuild_python" ] || {
    die "dmgbuild Python is not executable: $dmgbuild_python"
    return 1
  }
  if ! dmgbuild_version="$("$dmgbuild_python" -c \
    'import importlib.metadata; print(importlib.metadata.version("dmgbuild"))' 2>/dev/null)"; then
    die "could not read installed dmgbuild version"
    return 1
  fi
  [ "$dmgbuild_version" = 1.6.7 ] || {
    die "installed dmgbuild version is '$dmgbuild_version', expected '1.6.7'"
    return 1
  }
}

read_sips_property() {
  property_name="$1"
  property_dump="$2"
  printf '%s\n' "$property_dump" | awk -F': ' -v property="$property_name" \
    '$1 == "  " property {value=$2; count++} END {if (count == 1) print value; else exit 1}'
}

validate_dmg_background_asset() {
  background_name="$1"
  expected_width="$2"
  expected_height="$3"
  background_path="$release_root_dir/build/darwin/dmg/$background_name"
  if [ ! -f "$background_path" ] || [ -L "$background_path" ]; then
    die "DMG background is not a regular non-symlink file: $background_name"
    return 1
  fi
  if ! background_properties="$(sips -g format -g bitsPerSample -g samplesPerPixel \
    -g hasAlpha -g pixelWidth -g pixelHeight "$background_path" 2>/dev/null)"; then
    die "could not inspect DMG background: $background_name"
    return 1
  fi
  if ! background_format="$(read_sips_property format "$background_properties")" \
    || ! background_bits="$(read_sips_property bitsPerSample "$background_properties")" \
    || ! background_samples="$(read_sips_property samplesPerPixel "$background_properties")" \
    || ! background_alpha="$(read_sips_property hasAlpha "$background_properties")" \
    || ! background_width="$(read_sips_property pixelWidth "$background_properties")" \
    || ! background_height="$(read_sips_property pixelHeight "$background_properties")"; then
    die "$background_name has unexpected image properties"
    return 1
  fi
  if [ "$background_format" != png ] || [ "$background_bits" != 8 ] \
    || [ "$background_samples" != 4 ] || [ "$background_alpha" != yes ] \
    || [ "$background_width" != "$expected_width" ] \
    || [ "$background_height" != "$expected_height" ]; then
    die "$background_name has unexpected image properties"
    return 1
  fi
}

validate_dmg_backgrounds() {
  validate_dmg_background_asset background.png 660 360 || return 1
  validate_dmg_background_asset background@2x.png 1320 720 || return 1
  if ! (cd "$release_root_dir/build/darwin/dmg" \
    && shasum -a 256 -c background.sha256 >/dev/null); then
    die "DMG background checksum verification failed"
    return 1
  fi
}

probe_macho_file() {
  macho_probe_path="$1"
  macho_probe_is_macho=0
  if ! macho_probe_output="$(file -b "$macho_probe_path" 2>&1)"; then
    die "could not inspect file type: $macho_probe_path"
    return 1
  fi
  case "$macho_probe_output" in
    *Mach-O*) macho_probe_is_macho=1 ;;
  esac
}

probe_evidence_match() {
  grep_probe_kind="$1"
  grep_probe_pattern="$2"
  grep_probe_root="$3"
  grep_probe_label="$4"
  grep_probe_found=0
  grep_probe_rc=0
  case "$grep_probe_kind" in
    fixed)
      if LC_ALL=C grep -R -F -- "$grep_probe_pattern" "$grep_probe_root" >/dev/null; then
        grep_probe_rc=0
      else
        grep_probe_rc=$?
      fi
      ;;
    regex)
      if LC_ALL=C grep -R -E -- "$grep_probe_pattern" "$grep_probe_root" >/dev/null; then
        grep_probe_rc=0
      else
        grep_probe_rc=$?
      fi
      ;;
    *)
      die "invalid evidence probe kind: $grep_probe_kind"
      return 1
      ;;
  esac
  case "$grep_probe_rc" in
    0) grep_probe_found=1 ;;
    1) grep_probe_found=0 ;;
    *)
      die "could not scan evidence for $grep_probe_label"
      return 1
      ;;
  esac
}

initialize_tmp_roots() {
  requested_tmp="${TMPDIR:-/tmp}"
  if [ ! -d "$requested_tmp" ]; then
    die "temporary root is not a directory: $requested_tmp"
    return 1
  fi
  if ! tmp_root_lexical="$(cd "$requested_tmp" && pwd -L)"; then
    die "cannot resolve temporary root: $requested_tmp"
    return 1
  fi
  if ! tmp_root_physical="$(cd "$requested_tmp" && pwd -P)"; then
    die "cannot resolve physical temporary root: $requested_tmp"
    return 1
  fi
  if [ -z "$tmp_root_physical" ] || [ "$tmp_root_physical" = / ]; then
    die "unsafe physical temporary root: $tmp_root_physical"
    return 1
  fi
}

initialize_release_stage() {
  initialize_tmp_roots || return 1
  if ! stage_lexical="$(mktemp -d "$tmp_root_lexical/loqui-release.XXXXXX")"; then
    die "could not create release staging directory"
    return 1
  fi
  if ! stage="$(cd "$stage_lexical" && pwd -P)"; then
    die "could not resolve physical staging directory: $stage_lexical"
    return 1
  fi
  if ! safe_stage_path "$stage"; then
    die "unsafe staging path: $stage"
    return 1
  fi
}

safe_stage_path() {
  candidate_stage="$1"
  [ -n "$tmp_root_physical" ] && [ -d "$candidate_stage" ] || return 1
  [ "${candidate_stage%/*}" = "$tmp_root_physical" ] || return 1
  case "${candidate_stage##*/}" in
    loqui-release.??????) ;;
    *) return 1 ;;
  esac
  candidate_stage_physical="$(cd "$candidate_stage" && pwd -P)" || return 1
  [ "$candidate_stage" = "$candidate_stage_physical" ]
}

safe_release_candidate_path() {
  candidate_path="$1"
  candidate_kind="$2"
  [ -n "$candidate_path" ] && [ -n "$release_output_dir_physical" ] || return 1
  [ "${candidate_path%/*}" = "$release_output_dir_physical" ] || return 1
  [ -d "$release_output_dir_physical" ] && [ ! -L "$release_output_dir_physical" ] || return 1
  candidate_parent_physical="$(cd "${candidate_path%/*}" && pwd -P)" || return 1
  [ "$candidate_parent_physical" = "$release_output_dir_physical" ] || return 1
  candidate_name="${candidate_path##*/}"
  case "$candidate_kind:$candidate_name" in
    dmg:.Loqui-*.candidate.??????|zip:.Loqui-*.candidate.??????|evidence:.evidence-*.candidate.??????) return 0 ;;
    *) return 1 ;;
  esac
}

prepare_release_output_dir() {
  requested_output_dir="$1"
  release_output_dir_physical=""
  if ! repo_physical="$(cd "$release_root_dir" && pwd -P)"; then
    die "cannot resolve physical repository root: $release_root_dir"
    return 1
  fi
  if [ "$release_root_dir" != "$repo_physical" ]; then
    die "repository root is not physical: $release_root_dir"
    return 1
  fi
  expected_bin_dir="$repo_physical/bin"
  expected_output_dir="$expected_bin_dir/release"
  if [ "$requested_output_dir" != "$expected_output_dir" ]; then
    die "release output must be $expected_output_dir"
    return 1
  fi
  if [ -e "$expected_bin_dir" ] || [ -L "$expected_bin_dir" ]; then
    if [ ! -d "$expected_bin_dir" ] || [ -L "$expected_bin_dir" ]; then
      die "release bin path is not a physical directory: $expected_bin_dir"
      return 1
    fi
  else
    if ! mkdir "$expected_bin_dir"; then
      die "could not create release bin directory: $expected_bin_dir"
      return 1
    fi
  fi
  if ! bin_physical="$(cd "$expected_bin_dir" && pwd -P)"; then
    die "cannot resolve physical release bin directory: $expected_bin_dir"
    return 1
  fi
  if [ "$bin_physical" != "$expected_bin_dir" ]; then
    die "release bin directory resolves outside the repository: $expected_bin_dir"
    return 1
  fi

  if [ -e "$expected_output_dir" ] || [ -L "$expected_output_dir" ]; then
    if [ ! -d "$expected_output_dir" ] || [ -L "$expected_output_dir" ]; then
      die "release output is not a physical directory: $expected_output_dir"
      return 1
    fi
  else
    if ! mkdir "$expected_output_dir"; then
      die "could not create release output: $expected_output_dir"
      return 1
    fi
  fi
  if ! release_output_dir_physical="$(cd "$expected_output_dir" && pwd -P)"; then
    die "cannot resolve physical release output: $expected_output_dir"
    return 1
  fi
  if [ "$release_output_dir_physical" != "$expected_output_dir" ]; then
    release_output_dir_physical=""
    die "release output resolves outside the repository: $expected_output_dir"
    return 1
  fi
}

prepare_evidence_parent() {
  destination_root="$1"
  publication_version="$2"
  evidence_root="$destination_root/evidence"
  evidence_parent="$evidence_root/$publication_version"

  if [ "$destination_root" != "$release_output_dir_physical" ]; then
    die "evidence destination is not the physical release output: $destination_root"
    return 1
  fi
  if [ -e "$evidence_root" ] || [ -L "$evidence_root" ]; then
    if [ ! -d "$evidence_root" ] || [ -L "$evidence_root" ]; then
      die "release evidence directory is not physical: $evidence_root"
      return 1
    fi
  else
    if ! mkdir "$evidence_root"; then
      die "could not create release evidence directory: $evidence_root"
      return 1
    fi
  fi
  if ! evidence_root_physical="$(cd "$evidence_root" && pwd -P)"; then
    die "cannot resolve physical release evidence directory: $evidence_root"
    return 1
  fi
  if [ "$evidence_root_physical" != "$evidence_root" ]; then
    die "release evidence directory resolves outside release output: $evidence_root"
    return 1
  fi

  if [ -e "$evidence_parent" ] || [ -L "$evidence_parent" ]; then
    if [ ! -d "$evidence_parent" ] || [ -L "$evidence_parent" ]; then
      die "version evidence directory is not physical: $evidence_parent"
      return 1
    fi
  else
    if ! mkdir "$evidence_parent"; then
      die "could not create version evidence directory: $evidence_parent"
      return 1
    fi
  fi
  if ! evidence_parent_physical="$(cd "$evidence_parent" && pwd -P)"; then
    die "cannot resolve physical version evidence directory: $evidence_parent"
    return 1
  fi
  if [ "$evidence_parent_physical" != "$evidence_parent" ]; then
    die "version evidence directory resolves outside release output: $evidence_parent"
    return 1
  fi
}

replace_literal_in_file() {
  evidence_file="$1"
  literal_path="$2"
  replacement="$3"
  [ -n "$literal_path" ] || return 0
  literal_pattern="$literal_path"
  literal_pattern="${literal_pattern//\\/\\\\}"
  literal_pattern="${literal_pattern//\*/\\*}"
  literal_pattern="${literal_pattern//\?/\\?}"
  literal_pattern="${literal_pattern//\[/\\[}"
  literal_pattern="${literal_pattern//\]/\\]}"
  normalized_file="$evidence_file.normalize.$$"
  if ! while IFS= read -r evidence_line || [ -n "$evidence_line" ]; do
    evidence_line="${evidence_line//$literal_pattern/$replacement}"
    printf '%s\n' "$evidence_line"
  done <"$evidence_file" >"$normalized_file"; then
    die "could not normalize evidence file: $evidence_file"
    return 1
  fi
  if ! mv "$normalized_file" "$evidence_file"; then
    die "could not replace normalized evidence file: $evidence_file"
    return 1
  fi
}

normalize_evidence_paths() {
  evidence_dir="$1"
  if [ ! -d "$evidence_dir" ]; then
    die "missing evidence directory: $evidence_dir"
    return 1
  fi
  if [ -z "$stage" ] || [ -z "$stage_lexical" ]; then
    die "release staging paths are not initialized"
    return 1
  fi
  if ! evidence_files="$(find "$evidence_dir" -type f -print | LC_ALL=C sort)"; then
    die "could not enumerate evidence files: $evidence_dir"
    return 1
  fi
  while read -r evidence_file; do
    [ -n "$evidence_file" ] || continue
    replace_literal_in_file "$evidence_file" "$stage" "\$STAGE" || return 1
    if [ "$stage_lexical" != "$stage" ]; then
      replace_literal_in_file "$evidence_file" "$stage_lexical" "\$STAGE" || return 1
    fi
    replace_literal_in_file "$evidence_file" "$release_root_dir" "\$REPO" || return 1
  done <<<"$evidence_files"

  for original_path in "$stage" "$stage_lexical" "$release_root_dir"; do
    [ -n "$original_path" ] || continue
    probe_evidence_match fixed "$original_path" "$evidence_dir" \
      "unnormalized path: $original_path" || return 1
    if [ "$grep_probe_found" -eq 1 ]; then
      die "evidence contains an unnormalized path: $original_path"
      return 1
    fi
  done
  probe_evidence_match fixed "/private\$STAGE" "$evidence_dir" \
    "malformed marker /private\$STAGE" || return 1
  if [ "$grep_probe_found" -eq 1 ]; then
    die "evidence contains malformed /private\$STAGE path"
    return 1
  fi
}

cleanup_release() {
  if ! detach_dmg_verification_mount; then
    echo "release-macos: failed DMG verification mount cleanup" >&2
  fi
  if [ "$hidden_dmg_candidate_owned" -eq 1 ] && [ -n "$hidden_dmg_candidate" ]; then
    if safe_release_candidate_path "$hidden_dmg_candidate" dmg; then
      if rm -f "$hidden_dmg_candidate"; then
        hidden_dmg_candidate_owned=0
      else
        echo "release-macos: failed DMG candidate cleanup: $hidden_dmg_candidate" >&2
      fi
    else
      echo "release-macos: refusing unsafe DMG candidate cleanup: $hidden_dmg_candidate" >&2
    fi
  fi
  if [ "$hidden_zip_candidate_owned" -eq 1 ] && [ -n "$hidden_zip_candidate" ]; then
    if safe_release_candidate_path "$hidden_zip_candidate" zip; then
      if rm -f "$hidden_zip_candidate"; then
        hidden_zip_candidate_owned=0
      else
        echo "release-macos: failed ZIP candidate cleanup: $hidden_zip_candidate" >&2
      fi
    else
      echo "release-macos: refusing unsafe ZIP candidate cleanup: $hidden_zip_candidate" >&2
    fi
  fi
  if [ "$hidden_evidence_candidate_owned" -eq 1 ] && [ -n "$hidden_evidence_candidate" ]; then
    if safe_release_candidate_path "$hidden_evidence_candidate" evidence; then
      if rm -rf "$hidden_evidence_candidate"; then
        hidden_evidence_candidate_owned=0
      else
        echo "release-macos: failed evidence candidate cleanup: $hidden_evidence_candidate" >&2
      fi
    else
      echo "release-macos: refusing unsafe evidence candidate cleanup: $hidden_evidence_candidate" >&2
    fi
  fi
  if [ -n "$stage" ]; then
    if safe_stage_path "$stage"; then
      rm -rf "$stage" || echo "release-macos: failed stage cleanup: $stage" >&2
    else
      echo "release-macos: refusing unsafe stage cleanup: $stage" >&2
    fi
  fi
}

phase_preflight() {
  validate_notary_keychain || return 1
  validate_dmgbuild || return 1
  if ! host_arch="$(uname -m)"; then
    die "could not determine release host architecture"
    return 1
  fi
  if [ "$host_arch" != arm64 ]; then
    die "release host must be arm64"
    return 1
  fi
  for tool_name in security codesign otool lipo install_name_tool hdiutil spctl ditto plutil jq \
    xcrun wails3 git cmake swiftc vtool shasum file sips tiffutil; do
    require_command "$tool_name" || return 1
  done
  for required_script in release-version.sh patch-plists.sh build-macos-helpers.sh macos-bundle.sh macos-audit.sh macos-sign.sh setup-dmgbuild.sh; do
    if [ ! -x "$release_root_dir/scripts/$required_script" ]; then
      die "missing executable script: scripts/$required_script"
      return 1
    fi
  done
  for required_source in \
    build/darwin/Info.plist build/darwin/Info.dev.plist build/darwin/icons.icns \
    build/darwin/dmg/settings.py build/darwin/dmg/verify-ds-store.py \
    build/darwin/dmg/background.png build/darwin/dmg/background@2x.png \
    build/darwin/dmg/background.sha256 \
    helpers/macos-globe-listener.swift helpers/macos-stt.swift helpers/whisper-stt.cpp \
    third_party/speech-sdk/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech; do
    if [ ! -e "$release_root_dir/$required_source" ]; then
      die "missing required source: $required_source"
      return 1
    fi
  done
  validate_dmg_backgrounds || return 1

  if ! wails_version="$(wails3 version 2>&1)"; then
    die "could not read wails3 version"
    return 1
  fi
  wails_version="${wails_version%$'\r'}"
  if [ "$wails_version" != v3.0.0-alpha2.119 ]; then
    die "wails3 version is '$wails_version', expected v3.0.0-alpha2.119"
    return 1
  fi

  if ! version="$("$release_root_dir/scripts/release-version.sh" --root "$release_root_dir")"; then
    die "could not read release version"
    return 1
  fi
  if ! "$release_root_dir/scripts/patch-plists.sh" --check --root "$release_root_dir"; then
    return 1
  fi
  for plist_name in Info.plist Info.dev.plist; do
    plist_path="$release_root_dir/build/darwin/$plist_name"
    for version_key in CFBundleShortVersionString CFBundleVersion; do
      if ! plist_version="$(plutil -extract "$version_key" raw "$plist_path" 2>/dev/null)"; then
        die "could not read $plist_name $version_key"
        return 1
      fi
      if [ "$plist_version" != "$version" ]; then
        die "$plist_name $version_key is '$plist_version', expected '$version'"
        return 1
      fi
    done
  done

  if ! identity="$("$release_root_dir/scripts/macos-sign.sh" resolve --channel release)"; then
    return 1
  fi
  if ! xcrun notarytool history "${notary_auth_args[@]}" --output-format json >/dev/null; then
    die "notary profile '$profile' is invalid or unavailable"
    return 1
  fi

  initialize_release_stage || return 1
  if ! mkdir -p "$stage/evidence-work"; then
    die "could not create release evidence workspace"
    return 1
  fi
  if ! git -C "$release_root_dir" rev-parse HEAD >"$stage/evidence-work/repo-head.txt"; then
    die "could not record repository HEAD"
    return 1
  fi
  if ! git -C "$release_root_dir" describe --always --dirty >"$stage/evidence-work/repo-describe.txt"; then
    die "could not record repository description"
    return 1
  fi
}

phase_build() {
  if ! "$release_root_dir/scripts/task.sh" darwin:build ARCH=arm64 PORTABLE=true OUTPUT="$stage/loqui"; then
    return 1
  fi
  if [ ! -f "$stage/loqui" ]; then
    die "portable build did not produce $stage/loqui"
    return 1
  fi
  if [ ! -f "$release_root_dir/build/darwin/icons.icns" ]; then
    die "icon generation did not produce icons.icns"
    return 1
  fi
  if [ -e "$release_root_dir/build/darwin/Assets.car" ]; then
    die "forbidden Assets.car was generated"
    return 1
  fi
}

phase_build_helpers() {
  sdl_vendor="$stage/sdl-src"
  if ! LOQUI_HELPERS_OUTPUT_DIR="$stage/helpers" \
  LOQUI_WHISPER_VENDOR_DIR="$stage/whisper-src" \
  LOQUI_SDL_VENDOR_DIR="$sdl_vendor" \
  LOQUI_SKIP_MODEL=1 "$release_root_dir/scripts/build-macos-helpers.sh"; then
    return 1
  fi
  if ! printf '%s\n' 97c56f1dc1d1100a9d859c865a20c82d22f823ed >"$stage/evidence-work/whisper-commit.txt"; then
    die "could not record whisper.cpp commit"
    return 1
  fi
  if ! actual_sdl_commit="$(git -C "$sdl_vendor" rev-parse HEAD)"; then
    die "could not read staged SDL commit"
    return 1
  fi
  if [ "$actual_sdl_commit" != 5d249570393f7a37e037abf22cd6012a4cc56a71 ]; then
    die "built SDL commit is $actual_sdl_commit, expected 5d249570393f7a37e037abf22cd6012a4cc56a71"
    return 1
  fi
  if ! printf '%s\n' "$actual_sdl_commit" >"$stage/evidence-work/sdl-commit.txt"; then
    die "could not record staged SDL commit"
    return 1
  fi
  if ! : >"$stage/evidence-work/staged-helper-sha256.txt"; then
    die "could not initialize staged helper checksums"
    return 1
  fi
  if ! helper_files="$(find "$stage/helpers" -type f -print | LC_ALL=C sort)"; then
    die "could not enumerate staged helper files"
    return 1
  fi
  while read -r native_file; do
    [ -n "$native_file" ] || continue
    probe_macho_file "$native_file" || return 1
    [ "$macho_probe_is_macho" -eq 1 ] || continue
    if ! shasum -a 256 "$native_file" >>"$stage/evidence-work/staged-helper-sha256.txt"; then
      die "could not checksum staged helper: $native_file"
      return 1
    fi
  done <<<"$helper_files"
}

phase_bundle() {
  app="$stage/Loqui.app"
  if ! env -u LOQUI_BUNDLE_MODEL "$release_root_dir/scripts/macos-bundle.sh" \
    --channel production --root "$release_root_dir" --executable "$stage/loqui" \
    --helpers-dir "$stage/helpers" --output "$app" >/dev/null; then
    return 1
  fi
}

phase_audit_unsigned() {
  if ! "$release_root_dir/scripts/macos-audit.sh" --channel production --version "$version" "$app" \
    >"$stage/evidence-work/unsigned-audit.txt"; then
    return 1
  fi
}

phase_sign_app() {
  if ! "$release_root_dir/scripts/macos-sign.sh" app --channel release --app "$app" --identity "$identity"; then
    return 1
  fi
}

entitlement_count() {
  if ! entitlement_xml="$(plutil -convert xml1 -o - "$1" 2>/dev/null)"; then
    return 1
  fi
  printf '%s\n' "$entitlement_xml" | awk '/<key>/{count++} END{print count+0}'
}

verify_entitlements() {
  code_path="$1"
  expected_kind="$2"
  entitlement_dump="$stage/entitlements.$$.plist"
  if ! codesign -d --entitlements :- "$code_path" >"$entitlement_dump" 2>/dev/null; then
    if ! : >"$entitlement_dump"; then
      die "could not initialize entitlement dump: ${code_path#"$app"/}"
      return 1
    fi
  fi
  case "$expected_kind" in
    none)
      if [ -s "$entitlement_dump" ]; then
        if ! actual_entitlement_count="$(entitlement_count "$entitlement_dump")" \
          || [ "$actual_entitlement_count" -ne 0 ]; then
          die "unexpected entitlements: ${code_path#"$app"/}"
          return 1
        fi
      fi
      ;;
    audio)
      if ! actual_entitlement_count="$(entitlement_count "$entitlement_dump")" \
        || [ "$actual_entitlement_count" -ne 1 ]; then
        die "wrong audio-helper entitlement count: ${code_path#"$app"/}"
        return 1
      fi
      if ! audio_input_value="$(/usr/libexec/PlistBuddy -c 'Print :com.apple.security.device.audio-input' \
        "$entitlement_dump" 2>/dev/null)" || [ "$audio_input_value" != true ]; then
        die "missing audio-input entitlement: ${code_path#"$app"/}"
        return 1
      fi
      ;;
    host)
      if ! actual_entitlement_count="$(entitlement_count "$entitlement_dump")" \
        || [ "$actual_entitlement_count" -ne 2 ]; then
        die "wrong host entitlement count"
        return 1
      fi
      if ! audio_input_value="$(/usr/libexec/PlistBuddy -c 'Print :com.apple.security.device.audio-input' \
        "$entitlement_dump" 2>/dev/null)" || [ "$audio_input_value" != true ]; then
        die "missing host audio-input entitlement"
        return 1
      fi
      if ! apple_events_value="$(/usr/libexec/PlistBuddy -c \
        'Print :com.apple.security.automation.apple-events' "$entitlement_dump" 2>/dev/null)" \
        || [ "$apple_events_value" != true ]; then
        die "missing host Apple Events entitlement"
        return 1
      fi
      ;;
  esac
  if ! rm -f "$entitlement_dump"; then
    die "could not remove entitlement dump: $entitlement_dump"
    return 1
  fi
}

capture_designated_requirements() {
  designated_app="$1"
  designated_output="$2"
  if ! : >"$designated_output"; then
    die "could not initialize designated requirements evidence: $designated_output"
    return 1
  fi
  for designated_relative in \
    Loqui.app \
    Loqui.app/Contents/Helpers/globe-listener \
    Loqui.app/Contents/Helpers/macos-stt \
    Loqui.app/Contents/Helpers/whisper-stt; do
    if [ "$designated_relative" = Loqui.app ]; then
      designated_path="$designated_app"
    else
      designated_path="$designated_app/${designated_relative#Loqui.app/}"
    fi
    if ! designated_dump="$(codesign -d -r- "$designated_path" 2>&1)"; then
      die "could not read designated requirement: $designated_relative"
      return 1
    fi
    if ! designated_line="$(printf '%s\n' "$designated_dump" | sed -n '/^designated =>/p')"; then
      die "could not parse designated requirement: $designated_relative"
      return 1
    fi
    if ! designated_count="$(printf '%s\n' "$designated_line" | \
      awk '/^designated =>/{count++} END{print count+0}')"; then
      die "could not count designated requirements: $designated_relative"
      return 1
    fi
    if [ "$designated_count" -ne 1 ]; then
      die "${designated_relative#Loqui.app/} lacks one designated requirement"
      return 1
    fi
    if ! printf '## %s\n%s\n' "$designated_relative" "$designated_line" >>"$designated_output"; then
      die "could not record designated requirement: $designated_relative"
      return 1
    fi
  done
}

compare_designated_requirements() {
  first_requirements="$1"
  second_requirements="$2"
  if [ ! -f "$first_requirements" ]; then
    die "missing designated requirements evidence: $first_requirements"
    return 1
  fi
  if [ ! -f "$second_requirements" ]; then
    die "missing designated requirements evidence: $second_requirements"
    return 1
  fi
  if ! first_requirements_sha="$(shasum -a 256 "$first_requirements" | awk '{print $1}')"; then
    die "could not checksum designated requirements: $first_requirements"
    return 1
  fi
  if ! second_requirements_sha="$(shasum -a 256 "$second_requirements" | awk '{print $1}')"; then
    die "could not checksum designated requirements: $second_requirements"
    return 1
  fi
  if [ "$first_requirements_sha" != "$second_requirements_sha" ]; then
    die "designated requirements differ: $first_requirements and $second_requirements"
    return 1
  fi
}

verify_retina_tiff() {
  retina_path="$1"
  retina_evidence="$2"
  if ! tiffutil -info "$retina_path" >"$retina_evidence"; then
    die "could not inspect Retina background"
    return 1
  fi
  if ! retina_directory_count="$(awk '/^Directory at /{count++} END{print count+0}' \
    "$retina_evidence")"; then
    die "could not count Retina background image directories"
    return 1
  fi
  if [ "$retina_directory_count" -ne 2 ]; then
    die "Retina background must contain exactly two image directories"
    return 1
  fi
  retina_frames="$retina_evidence.frames"
  if ! awk '$1 == "Image" && $2 == "Width:" && $4 == "Image" && $5 == "Length:" \
      {print $3 "x" $6}' "$retina_evidence" | LC_ALL=C sort >"$retina_frames"; then
    die "could not parse Retina background frame dimensions"
    return 1
  fi
  retina_expected="$retina_evidence.expected"
  if ! printf '%s\n' 1320x720 660x360 | LC_ALL=C sort >"$retina_expected"; then
    die "could not record expected Retina background frames"
    return 1
  fi
  if ! diff -u "$retina_expected" "$retina_frames"; then
    die "Retina background has unexpected frame dimensions"
    return 1
  fi
}

detach_dmg_verification_mount() {
  [ "$dmg_verify_mounted" -eq 1 ] || return 0
  for detach_attempt in 1 2 3; do
    if hdiutil detach "$dmg_verify_mount" >/dev/null; then
      dmg_verify_mounted=0
      dmg_verify_mount=""
      return 0
    fi
    [ "$detach_attempt" -eq 3 ] || sleep "${LOQUI_DMG_DETACH_RETRY_DELAY:-1}"
  done
  if hdiutil detach -force "$dmg_verify_mount" >/dev/null 2>&1; then
    dmg_verify_mounted=0
    dmg_verify_mount=""
  fi
  die "could not cleanly detach DMG verification mount"
  return 1
}

inspect_generated_dmg_contents() {
  visible_manifest="$stage/evidence-work/dmg-visible-root.txt"
  visible_raw="$stage/evidence-work/dmg-visible-root.raw"
  if ! find "$dmg_verify_mount" -mindepth 1 -maxdepth 1 ! -name '.*' -print \
      >"$visible_raw"; then
    die "could not inspect generated DMG root"
    return 1
  fi
  if ! sed "s#^$dmg_verify_mount/##" "$visible_raw" | LC_ALL=C sort >"$visible_manifest"; then
    die "could not normalize generated DMG root"
    return 1
  fi
  expected_visible="$stage/evidence-work/dmg-visible-root.expected"
  printf '%s\n' Applications Loqui.app >"$expected_visible"
  if ! diff -u "$expected_visible" "$visible_manifest"; then
    die "generated DMG has unexpected visible root items"
    return 1
  fi
  if [ ! -L "$dmg_verify_mount/Applications" ] \
    || [ "$(readlink "$dmg_verify_mount/Applications")" != /Applications ]; then
    die "generated DMG Applications link is invalid"
    return 1
  fi
  if [ ! -f "$dmg_verify_mount/.DS_Store" ]; then
    die "generated DMG is missing .DS_Store"
    return 1
  fi
  ds_store_evidence="$stage/evidence-work/dmg-ds-store.txt"
  if ! "$dmgbuild_python" "$release_root_dir/build/darwin/dmg/verify-ds-store.py" \
      "$dmg_verify_mount/.DS_Store" >"$ds_store_evidence"; then
    die "generated DMG Finder metadata is invalid"
    return 1
  fi
  mounted_dmg_background="$dmg_verify_mount/.background.tiff"
  if [ ! -f "$mounted_dmg_background" ]; then
    die "generated DMG is missing Retina background"
    return 1
  fi
  if [ -L "$mounted_dmg_background" ]; then
    die "generated DMG background is not a regular non-symlink file"
    return 1
  fi
  if ! mounted_dmg_background_parent="$(cd "${mounted_dmg_background%/*}" && pwd -P)"; then
    die "could not resolve generated DMG background parent"
    return 1
  fi
  if [ "$mounted_dmg_background_parent" != "$dmg_verify_mount_physical" ]; then
    die "generated DMG background resolves outside verification mount"
    return 1
  fi
  retina_info="$stage/evidence-work/dmg-background-tiff.txt"
  verify_retina_tiff "$mounted_dmg_background" "$retina_info" || return 1
  mounted_dmg_app="$dmg_verify_mount/Loqui.app"
  if [ ! -d "$mounted_dmg_app" ] || [ -L "$mounted_dmg_app" ]; then
    die "generated DMG app is not a regular non-symlink directory"
    return 1
  fi
  if ! mounted_dmg_app_parent="$(cd "${mounted_dmg_app%/*}" && pwd -P)"; then
    die "could not resolve generated DMG app parent"
    return 1
  fi
  if [ "$mounted_dmg_app_parent" != "$dmg_verify_mount_physical" ]; then
    die "generated DMG app resolves outside verification mount"
    return 1
  fi
  "$release_root_dir/scripts/macos-audit.sh" --channel production --version "$version" \
    "$mounted_dmg_app" >/dev/null || return 1
  codesign --verify --deep --strict "$mounted_dmg_app" || return 1
  dmg_designated_requirements="$stage/evidence-work/designated-requirements-dmg.txt"
  capture_designated_requirements \
    "$mounted_dmg_app" "$dmg_designated_requirements" || return 1
  compare_designated_requirements \
    "$stage/evidence-work/designated-requirements.txt" \
    "$dmg_designated_requirements"
}

verify_generated_dmg_contents() {
  dmg_verify_mount="$stage/dmg-verify"
  if [ -e "$dmg_verify_mount" ] || [ -L "$dmg_verify_mount" ]; then
    die "DMG verification mount path already exists"
    return 1
  fi
  if ! mkdir "$dmg_verify_mount"; then
    die "could not create DMG verification mount"
    return 1
  fi
  if ! dmg_verify_mount_physical="$(cd "$dmg_verify_mount" && pwd -P)"; then
    die "could not resolve DMG verification mount"
    return 1
  fi
  if [ "$dmg_verify_mount_physical" != "$stage/dmg-verify" ]; then
    die "DMG verification mount resolves outside release stage"
    return 1
  fi
  if ! hdiutil attach -readonly -nobrowse -mountpoint "$dmg_verify_mount" "$dmg" >/dev/null; then
    die "could not mount generated DMG"
    return 1
  fi
  dmg_verify_mounted=1

  inspection_status=0
  if ! inspect_generated_dmg_contents; then
    inspection_status=1
  fi
  detach_status=0
  if ! detach_dmg_verification_mount; then
    detach_status=1
  fi
  [ "$detach_status" -eq 0 ] || return 1
  [ "$inspection_status" -eq 0 ] || return 1
}

phase_verify_app() {
  if ! codesign --verify --deep --strict --verbose=2 "$app"; then
    return 1
  fi
  signed_manifest="$stage/signed-macho-manifest.txt"
  if ! : >"$signed_manifest"; then
    die "could not initialize signed Mach-O manifest"
    return 1
  fi
  if ! : >"$stage/evidence-work/signature-metadata.txt"; then
    die "could not initialize signature metadata evidence"
    return 1
  fi
  if ! app_files="$(find "$app" -type f -print | LC_ALL=C sort)"; then
    die "could not enumerate signed app files"
    return 1
  fi
  expected_team=""
  while read -r code_path; do
    [ -n "$code_path" ] || continue
    probe_macho_file "$code_path" || return 1
    [ "$macho_probe_is_macho" -eq 1 ] || continue
    relative="Loqui.app/${code_path#"$app"/}"
    if ! printf '%s\n' "$relative" >>"$signed_manifest"; then
      die "could not record signed Mach-O: $relative"
      return 1
    fi
    if ! codesign --verify --strict --verbose=2 "$code_path"; then
      return 1
    fi
    if ! metadata="$(codesign -dv --verbose=4 "$code_path" 2>&1)"; then
      die "could not read signature metadata: $relative"
      return 1
    fi
    if ! printf '## %s\n%s\n' "$relative" "$metadata" \
      >>"$stage/evidence-work/signature-metadata.txt"; then
      die "could not record signature metadata: $relative"
      return 1
    fi
    if ! printf '%s\n' "$metadata" | grep -F 'Authority=Developer ID Application:' >/dev/null; then
      die "$relative lacks Developer ID Application authority"
      return 1
    fi
    if ! team="$(printf '%s\n' "$metadata" | sed -n 's/^TeamIdentifier=//p' | head -1)"; then
      die "$relative lacks TeamIdentifier"
      return 1
    fi
    if [ -z "$team" ]; then
      die "$relative lacks TeamIdentifier"
      return 1
    fi
    if [ -z "$expected_team" ]; then
      expected_team="$team"
    elif [ "$team" != "$expected_team" ]; then
      die "$relative has different TeamIdentifier: $team"
      return 1
    fi
    case "$relative" in
      Loqui.app/Contents/MacOS/*|Loqui.app/Contents/Helpers/*)
        if ! printf '%s\n' "$metadata" | grep -E '^Timestamp=' >/dev/null; then
          die "$relative lacks secure timestamp"
          return 1
        fi
        if ! printf '%s\n' "$metadata" | grep -E '^CodeDirectory .*flags=.*runtime' >/dev/null; then
          die "$relative lacks Hardened Runtime"
          return 1
        fi
        ;;
    esac
    case "$relative" in
      Loqui.app/Contents/Helpers/*)
        helper_name="${code_path##*/}"
        expected_identifier="com.jualopezmo.loquigo.$helper_name"
        if ! actual_identifier="$(printf '%s\n' "$metadata" | sed -n 's/^Identifier=//p' | head -1)"; then
          die "could not read identifier: $relative"
          return 1
        fi
        if [ "$actual_identifier" != "$expected_identifier" ]; then
          die "$relative identifier is '$actual_identifier', expected '$expected_identifier'"
          return 1
        fi
        ;;
    esac
  done <<<"$app_files"
  if ! LC_ALL=C sort -u "$signed_manifest" -o "$signed_manifest"; then
    die "could not sort signed Mach-O manifest"
    return 1
  fi

  if ! app_metadata="$(codesign -dv --verbose=4 "$app" 2>&1)"; then
    die "could not read signed app metadata"
    return 1
  fi
  if ! app_identifier="$(printf '%s\n' "$app_metadata" | sed -n 's/^Identifier=//p' | head -1)"; then
    die "could not read signed app identifier"
    return 1
  fi
  if [ "$app_identifier" != com.jualopezmo.loquigo ]; then
    die "signed app identifier is '$app_identifier'"
    return 1
  fi
  verify_entitlements "$app" host || return 1
  verify_entitlements "$app/Contents/Helpers/globe-listener" none || return 1
  verify_entitlements "$app/Contents/Helpers/macos-stt" audio || return 1
  verify_entitlements "$app/Contents/Helpers/whisper-stt" audio || return 1
  capture_designated_requirements "$app" \
    "$stage/evidence-work/designated-requirements.txt" || return 1
}

zip_archive_from_app() {
  zip_root="$stage/zip-root"
  if [ -e "$zip_root" ] || [ -L "$zip_root" ]; then
    if [ -L "$zip_root" ] || [ ! -d "$zip_root" ]; then
      die "ZIP staging root is not a physical directory"
      return 1
    fi
    if ! rm -rf "$zip_root"; then
      die "could not reset ZIP staging root"
      return 1
    fi
  fi
  if ! mkdir "$zip_root"; then
    die "could not create ZIP staging root"
    return 1
  fi
  zip_app="$zip_root/Loqui.app"
  if ! ditto "$app" "$zip_app"; then
    die "could not stage app for ZIP"
    return 1
  fi
  if [ ! -d "$zip_app" ] || [ -L "$zip_app" ]; then
    die "staged ZIP app is not a physical directory"
    return 1
  fi
  if ! ditto -c -k --keepParent "$zip_app" "$zip"; then
    die "could not create update ZIP"
    return 1
  fi
  if [ ! -f "$zip" ] || [ -L "$zip" ]; then
    die "ZIP creation did not produce a regular archive"
    return 1
  fi
}

verify_zip_contents() {
  zip_verify_root="$stage/zip-verify"
  if [ -e "$zip_verify_root" ] || [ -L "$zip_verify_root" ]; then
    if [ -L "$zip_verify_root" ] || [ ! -d "$zip_verify_root" ]; then
      die "ZIP verification root is not a physical directory"
      return 1
    fi
    if ! rm -rf "$zip_verify_root"; then
      die "could not reset ZIP verification root"
      return 1
    fi
  fi
  if ! mkdir "$zip_verify_root"; then
    die "could not create ZIP verification root"
    return 1
  fi
  if ! ditto -x -k "$zip" "$zip_verify_root"; then
    die "could not extract update ZIP for verification"
    return 1
  fi
  if ! zip_top_level="$(find "$zip_verify_root" -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)"; then
    die "could not inspect update ZIP contents"
    return 1
  fi
  zip_top_count=0
  zip_top_path=""
  while IFS= read -r zip_entry; do
    [ -n "$zip_entry" ] || continue
    zip_top_count=$((zip_top_count + 1))
    zip_top_path="$zip_entry"
  done <<<"$zip_top_level"
  [ "$zip_top_count" -eq 1 ] || {
    die "update ZIP must contain exactly one top-level Loqui.app"
    return 1
  }
  if [ "$zip_top_path" != "$zip_verify_root/Loqui.app" ]; then
    die "update ZIP contains an unexpected top-level entry"
    return 1
  fi
  if [ ! -d "$zip_top_path" ] || [ -L "$zip_top_path" ]; then
    die "update ZIP Loqui.app is not a physical directory"
    return 1
  fi
  if ! codesign --verify --deep --strict "$zip_top_path"; then
    die "extracted update app failed code-signature verification"
    return 1
  fi
}

phase_create_zip() {
  if ! zip_name="$("$release_root_dir/scripts/release-version.sh" --root "$release_root_dir" --zip-name)"; then
    die "could not derive update ZIP name"
    return 1
  fi
  zip="$stage/$zip_name"
  zip_archive_from_app || return 1
  verify_zip_contents || return 1
}

phase_staple_app() {
  if ! xcrun stapler staple "$app"; then
    return 1
  fi
  if ! xcrun stapler validate "$app"; then
    return 1
  fi
  zip_archive_from_app || return 1
}

phase_verify_zip() {
  verify_zip_contents || return 1
  if ! xcrun stapler validate "$app"; then
    return 1
  fi
}

phase_create_dmg() {
  dmg_root="$stage/dmg-root"
  if [ -e "$dmg_root" ] || [ -L "$dmg_root" ]; then
    die "DMG staging root already exists"
    return 1
  fi
  if ! mkdir "$dmg_root"; then
    die "could not create DMG staging root"
    return 1
  fi
  if [ -L "$dmg_root" ]; then
    die "DMG staging root must not be a symlink"
    return 1
  fi
  if ! dmg_root_physical="$(cd "$dmg_root" && pwd -P)"; then
    die "could not resolve DMG staging root"
    return 1
  fi
  if [ "$dmg_root_physical" != "$stage/dmg-root" ]; then
    die "DMG staging root resolves outside release stage"
    return 1
  fi
  dmg_app="$dmg_root/Loqui.app"
  if ! ditto "$app" "$dmg_app"; then
    return 1
  fi
  if [ ! -d "$dmg_app" ] || [ -L "$dmg_app" ]; then
    die "staged DMG app is not a regular directory"
    return 1
  fi
  if ! dmg_app_parent="$(cd "${dmg_app%/*}" && pwd -P)"; then
    die "could not resolve staged DMG app parent"
    return 1
  fi
  if [ "$dmg_app_parent" != "$stage/dmg-root" ]; then
    die "staged DMG app resolves outside release stage"
    return 1
  fi
  if ! "$release_root_dir/scripts/macos-audit.sh" --channel production --version "$version" \
    "$dmg_app" >/dev/null; then
    return 1
  fi
  if ! codesign --verify --deep --strict "$dmg_app"; then
    return 1
  fi
  dmg_designated_requirements="$stage/evidence-work/designated-requirements-dmg.txt"
  if ! capture_designated_requirements "$dmg_app" "$dmg_designated_requirements"; then
    return 1
  fi
  if ! compare_designated_requirements \
    "$stage/evidence-work/designated-requirements.txt" "$dmg_designated_requirements"; then
    return 1
  fi
  dmg="$stage/Loqui.dmg"
  settings="$release_root_dir/build/darwin/dmg/settings.py"
  if ! "$dmgbuild_python" -m dmgbuild \
      -s "$settings" \
      -D "app=$dmg_app" \
      -D "assets=$release_root_dir/build/darwin/dmg" \
      Loqui "$dmg"; then
    die "could not create styled DMG"
    return 1
  fi
  if [ ! -f "$dmg" ] || [ -L "$dmg" ]; then
    die "dmgbuild did not create a regular DMG"
    return 1
  fi
  if ! hdiutil verify "$dmg"; then
    die "generated DMG failed hdiutil verification"
    return 1
  fi
  verify_generated_dmg_contents || return 1
}

phase_sign_dmg() {
  if ! "$release_root_dir/scripts/macos-sign.sh" dmg --dmg "$dmg" --identity "$identity"; then
    return 1
  fi
}

phase_verify_dmg() {
  if ! hdiutil verify "$dmg"; then
    return 1
  fi
  if ! codesign --verify --verbose=2 "$dmg"; then
    return 1
  fi
}

preserve_notary_failure() {
  failure_id="${1:-missing-id}"
  submit_path="${2:-}"
  log_path="${3:-}"
  configured_failure_dir="${LOQUI_NOTARY_FAILURE_DIR:-}"
  initialize_tmp_roots || return 1
  if ! failure_dir="$(mktemp -d "$tmp_root_physical/loqui-notary-failure.$failure_id.XXXXXX")"; then
    die "could not preserve notary failure evidence"
    return 1
  fi
  if [ -n "$submit_path" ] && [ -f "$submit_path" ]; then
    if ! cp "$submit_path" "$failure_dir/notary-submit.json"; then
      die "could not preserve notary submission response"
      return 1
    fi
  fi
  if [ -n "$log_path" ] && [ -f "$log_path" ]; then
    if ! cp "$log_path" "$failure_dir/notary-log.json"; then
      die "could not preserve notary log"
      return 1
    fi
  fi
  if [ -n "$configured_failure_dir" ]; then
    case "$configured_failure_dir" in
      /*) ;;
      *) die "LOQUI_NOTARY_FAILURE_DIR must be absolute"; return 1 ;;
    esac
    configured_failure_parent="$(dirname "$configured_failure_dir")"
    configured_failure_name="$(basename "$configured_failure_dir")"
    if [ "$configured_failure_name" = . ] || [ "$configured_failure_name" = .. ]; then
      die "unsafe LOQUI_NOTARY_FAILURE_DIR: $configured_failure_dir"
      return 1
    fi
    if [ ! -d "$configured_failure_parent" ]; then
      die "LOQUI_NOTARY_FAILURE_DIR parent does not exist: $configured_failure_parent"
      return 1
    fi
    if ! configured_failure_parent="$(cd "$configured_failure_parent" && pwd -P)"; then
      die "cannot resolve LOQUI_NOTARY_FAILURE_DIR parent"
      return 1
    fi
    if [ "$configured_failure_parent" = / ]; then
      die "unsafe LOQUI_NOTARY_FAILURE_DIR parent"
      return 1
    fi
    configured_failure_dir="$configured_failure_parent/$configured_failure_name"
    if [ -e "$configured_failure_dir" ] || [ -L "$configured_failure_dir" ]; then
      die "LOQUI_NOTARY_FAILURE_DIR already exists: $configured_failure_dir"
      return 1
    fi
    normalize_evidence_paths "$failure_dir" || return 1
    probe_evidence_match regex '/Users/|"(password|privateKey|apiKey)"[[:space:]]*:' \
      "$failure_dir" "notary failure checkout paths or secrets" || return 1
    if [ "$grep_probe_found" -eq 1 ]; then
      die "failure evidence contains a checkout path or secret field"
      return 1
    fi
    if ! mv "$failure_dir" "$configured_failure_dir"; then
      die "could not publish sanitized notary failure evidence"
      return 1
    fi
    failure_dir="$configured_failure_dir"
  fi
  if ! printf '%s\n' "$failure_dir"; then
    return 1
  fi
}

phase_submit() {
  if xcrun notarytool submit "$dmg" "${notary_auth_args[@]}" --wait --timeout 30m \
    --output-format json >"$stage/notary-submit.json"; then
    submit_rc=0
  else
    submit_rc=$?
  fi
  if ! submission_id="$(jq -er '.id | select(type == "string" and length > 0)' \
    "$stage/notary-submit.json" 2>/dev/null)"; then
    submission_id=""
  fi
  if ! submission_status="$(jq -er '.status | select(type == "string" and length > 0)' \
    "$stage/notary-submit.json" 2>/dev/null)"; then
    submission_status=""
  fi
  if [ -z "$submission_id" ]; then
    if ! failure_dir="$(preserve_notary_failure missing-id "$stage/notary-submit.json" "")"; then
      return 1
    fi
    die "missing submission id; raw response preserved at $failure_dir"
    return 1
  fi
}

phase_fetch_log() {
  log_rc=1
  log_attempt=1
  while [ "$log_attempt" -le 3 ]; do
    if xcrun notarytool log "$submission_id" "$stage/notary-log.json" "${notary_auth_args[@]}"; then
      log_rc=0
    else
      log_rc=$?
    fi
    if [ "$log_rc" -eq 0 ]; then
      break
    fi
    log_attempt=$((log_attempt + 1))
    if [ "$log_attempt" -le 3 ]; then
      if ! sleep "${LOQUI_NOTARY_LOG_RETRY_DELAY:-5}"; then
        die "notary log retry delay failed"
        return 1
      fi
    fi
  done
  if [ "$log_rc" -ne 0 ]; then
    if ! failure_dir="$(preserve_notary_failure "$submission_id" \
      "$stage/notary-submit.json" "$stage/notary-log.json")"; then
      return 1
    fi
    die "notary log retrieval failed for $submission_id; evidence preserved at $failure_dir"
    return 1
  fi
}

check_ticket_log() {
  log_path="$1"
  manifest_path="$2"
  if ! log_status="$(jq -er '.status | select(type == "string")' "$log_path" 2>/dev/null)"; then
    log_status=""
  fi
  if [ "$log_status" != Accepted ]; then
    die "notary log status is '$log_status', expected Accepted"
    return 1
  fi
  if ! error_count="$(jq '[((.issues // [])[]) | select(.severity == "error")] | length' \
    "$log_path" 2>/dev/null)"; then
    die "notary log contains invalid issues data"
    return 1
  fi
  if [ "$error_count" != 0 ]; then
    die "notary log contains $error_count error issue(s)"
    return 1
  fi
  if ! jq -e '.ticketContents | type == "array" and length > 0' "$log_path" >/dev/null 2>&1; then
    die "notary log has missing/null/empty ticketContents"
    return 1
  fi
  if ! ticket_paths="$(jq -r '.ticketContents[].path | select(type == "string")' "$log_path")"; then
    die "could not parse notary ticket paths"
    return 1
  fi
  while read -r expected_path; do
    [ -n "$expected_path" ] || continue
    covered=0
    while read -r ticket_path; do
      case "$ticket_path" in "$expected_path"|*/"$expected_path") covered=1; break ;; esac
    done <<<"$ticket_paths"
    if [ "$covered" -ne 1 ]; then
      die "ticketContents omit $expected_path"
      return 1
    fi
  done <"$manifest_path"
}

phase_check_log() {
  if [ "$submit_rc" -ne 0 ] || [ "$submission_status" != Accepted ]; then
    if ! failure_dir="$(preserve_notary_failure "$submission_id" \
      "$stage/notary-submit.json" "$stage/notary-log.json")"; then
      return 1
    fi
    die "notary submission $submission_id ended '$submission_status' (rc=$submit_rc); evidence preserved at $failure_dir"
    return 1
  fi
  if ! check_ticket_log "$stage/notary-log.json" "$signed_manifest"; then
    if ! failure_dir="$(preserve_notary_failure "$submission_id" \
      "$stage/notary-submit.json" "$stage/notary-log.json")"; then
      return 1
    fi
    die "ticket validation failed for $submission_id; evidence preserved at $failure_dir"
    return 1
  fi

  evidence="$stage/evidence"
  if ! mkdir -p "$evidence"; then
    die "could not create release evidence"
    return 1
  fi
  if ! cp "$stage/notary-submit.json" "$evidence/"; then return 1; fi
  if ! cp "$stage/notary-log.json" "$evidence/"; then return 1; fi
  if ! cp "$signed_manifest" "$evidence/"; then return 1; fi
  if ! cp "$stage/evidence-work"/* "$evidence/"; then return 1; fi
  if ! date -u '+%Y-%m-%dT%H:%M:%SZ' >"$evidence/release-date-utc.txt"; then
    die "could not record release date"
    return 1
  fi
  if ! : >"$evidence/packaged-macho-sha256.txt"; then
    die "could not initialize packaged Mach-O checksums"
    return 1
  fi
  if ! packaged_files="$(find "$app" -type f -print | LC_ALL=C sort)"; then
    die "could not enumerate packaged app files"
    return 1
  fi
  while read -r code_path; do
    [ -n "$code_path" ] || continue
    probe_macho_file "$code_path" || return 1
    [ "$macho_probe_is_macho" -eq 1 ] || continue
    if ! shasum -a 256 "$code_path" >>"$evidence/packaged-macho-sha256.txt"; then
      die "could not checksum packaged Mach-O: $code_path"
      return 1
    fi
  done <<<"$packaged_files"
  normalize_evidence_paths "$evidence" || return 1
  probe_evidence_match regex '/Users/|"(password|privateKey|apiKey)"[[:space:]]*:' \
    "$evidence" "checkout paths or secrets" || return 1
  if [ "$grep_probe_found" -eq 1 ]; then
    die "evidence contains a checkout path or secret field"
    return 1
  fi
}

phase_submit_zip() {
  if xcrun notarytool submit "$zip" "${notary_auth_args[@]}" --wait --timeout 30m \
    --output-format json >"$stage/zip-notary-submit.json"; then
    zip_submit_rc=0
  else
    zip_submit_rc=$?
  fi
  if ! zip_submission_id="$(jq -er '.id | select(type == "string" and length > 0)' \
    "$stage/zip-notary-submit.json" 2>/dev/null)"; then
    zip_submission_id=""
  fi
  if ! zip_submission_status="$(jq -er '.status | select(type == "string" and length > 0)' \
    "$stage/zip-notary-submit.json" 2>/dev/null)"; then
    zip_submission_status=""
  fi
  if [ -z "$zip_submission_id" ]; then
    if ! failure_dir="$(preserve_notary_failure missing-zip-id \
      "$stage/zip-notary-submit.json" "")"; then
      return 1
    fi
    die "missing ZIP submission id; raw response preserved at $failure_dir"
    return 1
  fi
}

phase_fetch_zip_log() {
  zip_log_rc=1
  zip_log_attempt=1
  while [ "$zip_log_attempt" -le 3 ]; do
    if xcrun notarytool log "$zip_submission_id" "$stage/zip-notary-log.json" \
      "${notary_auth_args[@]}"; then
      zip_log_rc=0
    else
      zip_log_rc=$?
    fi
    if [ "$zip_log_rc" -eq 0 ]; then
      break
    fi
    zip_log_attempt=$((zip_log_attempt + 1))
    if [ "$zip_log_attempt" -le 3 ]; then
      if ! sleep "${LOQUI_NOTARY_LOG_RETRY_DELAY:-5}"; then
        die "ZIP notary log retry delay failed"
        return 1
      fi
    fi
  done
  if [ "$zip_log_rc" -ne 0 ]; then
    if ! failure_dir="$(preserve_notary_failure "$zip_submission_id" \
      "$stage/zip-notary-submit.json" "$stage/zip-notary-log.json")"; then
      return 1
    fi
    die "ZIP notary log retrieval failed for $zip_submission_id; evidence preserved at $failure_dir"
    return 1
  fi
}

phase_check_zip_log() {
  if [ "$zip_submit_rc" -ne 0 ] || [ "$zip_submission_status" != Accepted ]; then
    if ! failure_dir="$(preserve_notary_failure "$zip_submission_id" \
      "$stage/zip-notary-submit.json" "$stage/zip-notary-log.json")"; then
      return 1
    fi
    die "ZIP notary submission $zip_submission_id ended '$zip_submission_status' (rc=$zip_submit_rc); evidence preserved at $failure_dir"
    return 1
  fi
  if ! check_ticket_log "$stage/zip-notary-log.json" "$signed_manifest"; then
    if ! failure_dir="$(preserve_notary_failure "$zip_submission_id" \
      "$stage/zip-notary-submit.json" "$stage/zip-notary-log.json")"; then
      return 1
    fi
    die "ZIP ticket validation failed for $zip_submission_id; evidence preserved at $failure_dir"
    return 1
  fi
  if ! cp "$stage/zip-notary-submit.json" "$stage/evidence-work/"; then
    die "could not record ZIP notary submission evidence"
    return 1
  fi
  if ! cp "$stage/zip-notary-log.json" "$stage/evidence-work/"; then
    die "could not record ZIP notary log evidence"
    return 1
  fi
}

phase_staple() {
  if ! xcrun stapler staple "$dmg"; then return 1; fi
}

phase_verify_staple() {
  if ! xcrun stapler validate "$dmg"; then return 1; fi
  if ! hdiutil verify "$dmg"; then return 1; fi
  if ! codesign --verify --verbose=2 "$dmg"; then return 1; fi
}

phase_gatekeeper() {
  if ! spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"; then
    return 1
  fi
}

atomic_publish() {
  source_dmg="$1"
  source_evidence="$2"
  destination_root="$3"
  publication_version="$4"
  publication_id="$5"
  publication_dmg_name="$6"
  source_zip="${7:-}"
  publication_zip_name="${8:-}"
  publish_zip=0
  if [[ ! "$publication_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    die "invalid publication version: $publication_version"
    return 1
  fi
  if [[ ! "$publication_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] \
    || [ "$publication_id" = . ] || [ "$publication_id" = .. ]; then
    die "invalid publication id: $publication_id"
    return 1
  fi
  expected_publication_dmg_name="Loqui-$publication_version-macos-arm64.dmg"
  if [ "$publication_dmg_name" != "$expected_publication_dmg_name" ]; then
    die "publication DMG name does not match version: $publication_dmg_name"
    return 1
  fi
  if [ -n "$source_zip" ] || [ -n "$publication_zip_name" ]; then
    [ -n "$source_zip" ] && [ -n "$publication_zip_name" ] \
      || { die "ZIP publication arguments must be provided together"; return 1; }
    publish_zip=1
    expected_publication_zip_name="Loqui-$publication_version-macos-arm64.zip"
    if [ "$publication_zip_name" != "$expected_publication_zip_name" ]; then
      die "publication ZIP name does not match version: $publication_zip_name"
      return 1
    fi
  fi
  prepare_release_output_dir "$destination_root" || return 1
  destination_root="$release_output_dir_physical"
  if [ ! -f "$source_dmg" ]; then
    die "missing source DMG: $source_dmg"
    return 1
  fi
  if [ "$publish_zip" -eq 1 ] && [ ! -f "$source_zip" ]; then
    die "missing source ZIP: $source_zip"
    return 1
  fi
  if [ ! -d "$source_evidence" ]; then
    die "missing source evidence: $source_evidence"
    return 1
  fi
  final_dmg="$destination_root/$publication_dmg_name"
  final_zip=""
  if [ "$publish_zip" -eq 1 ]; then
    final_zip="$destination_root/$publication_zip_name"
  fi
  prepare_evidence_parent "$destination_root" "$publication_version" || return 1
  published_evidence="$evidence_parent/$publication_id"
  if [ -e "$published_evidence" ] || [ -L "$published_evidence" ]; then
    die "evidence already exists: $published_evidence"
    return 1
  fi
  hidden_dmg_candidate=""
  hidden_zip_candidate=""
  hidden_evidence_candidate=""
  hidden_dmg_candidate_owned=0
  hidden_zip_candidate_owned=0
  hidden_evidence_candidate_owned=0
  if ! hidden_dmg_candidate="$(mktemp "$destination_root/.Loqui-$publication_version.$publication_id.candidate.XXXXXX")"; then
    hidden_dmg_candidate=""
    die "could not create hidden DMG candidate"
    return 1
  fi
  hidden_dmg_candidate_owned=1
  if ! cp "$source_dmg" "$hidden_dmg_candidate"; then
    die "could not copy hidden DMG candidate"
    return 1
  fi
  if [ "$publish_zip" -eq 1 ]; then
    if ! hidden_zip_candidate="$(mktemp "$destination_root/.Loqui-$publication_version.$publication_id.candidate.XXXXXX")"; then
      hidden_zip_candidate=""
      die "could not create hidden ZIP candidate"
      return 1
    fi
    hidden_zip_candidate_owned=1
    if ! cp "$source_zip" "$hidden_zip_candidate"; then
      die "could not copy hidden ZIP candidate"
      return 1
    fi
  fi
  if ! hidden_evidence_candidate="$(mktemp -d "$destination_root/.evidence-$publication_version.$publication_id.candidate.XXXXXX")"; then
    hidden_evidence_candidate=""
    die "could not create hidden evidence candidate"
    return 1
  fi
  hidden_evidence_candidate_owned=1
  if ! cp -R "$source_evidence"/. "$hidden_evidence_candidate/"; then
    die "could not copy hidden evidence candidate"
    return 1
  fi
  if ! mv "$hidden_evidence_candidate" "$published_evidence"; then
    die "could not publish evidence: $published_evidence"
    return 1
  fi
  hidden_evidence_candidate=""
  hidden_evidence_candidate_owned=0
  if ! mv -f "$hidden_dmg_candidate" "$final_dmg"; then
    if ! rm -rf "$published_evidence"; then
      die "could not roll back published evidence: $published_evidence"
      return 1
    fi
    die "could not publish final DMG: $final_dmg"
    return 1
  fi
  hidden_dmg_candidate=""
  hidden_dmg_candidate_owned=0
  if [ "$publish_zip" -eq 1 ]; then
    if ! mv -f "$hidden_zip_candidate" "$final_zip"; then
      if ! rm -f "$final_dmg" || ! rm -rf "$published_evidence"; then
        die "could not roll back published release pair"
        return 1
      fi
      die "could not publish final ZIP: $final_zip"
      return 1
    fi
    hidden_zip_candidate=""
    hidden_zip_candidate_owned=0
  fi
  if ! printf '%s\n' "$final_dmg"; then
    return 1
  fi
}

phase_publish() {
  if ! publication_dmg_name="$("$release_root_dir/scripts/release-version.sh" \
    --root "$release_root_dir" --dmg-name)"; then
    die "could not derive publication DMG name"
    return 1
  fi
  if ! publication_zip_name="$("$release_root_dir/scripts/release-version.sh" --root "$release_root_dir" --zip-name)"; then
    die "could not derive publication ZIP name"
    return 1
  fi
  atomic_publish "$dmg" "$stage/evidence" "$release_output_dir" \
    "$version" "$submission_id" "$publication_dmg_name" "$zip" "$publication_zip_name" || return 1
}

run_phase() {
  phase_name="$1"
  phase_function="$2"
  if [ -n "${LOQUI_PHASE_LOG:-}" ]; then
    if ! printf '%s\n' "$phase_name" >>"$LOQUI_PHASE_LOG"; then
      die "could not record release phase: $phase_name"
      return 1
    fi
  fi
  if ! "$phase_function"; then
    return 1
  fi
}

run_release() {
  run_phase preflight phase_preflight || return 1
  run_phase build phase_build || return 1
  run_phase build-helpers phase_build_helpers || return 1
  run_phase bundle phase_bundle || return 1
  run_phase audit-unsigned phase_audit_unsigned || return 1
  run_phase sign-app phase_sign_app || return 1
  run_phase verify-app phase_verify_app || return 1
  run_phase create-zip phase_create_zip || return 1
  run_phase submit-zip phase_submit_zip || return 1
  run_phase fetch-zip-log phase_fetch_zip_log || return 1
  run_phase check-zip-log phase_check_zip_log || return 1
  run_phase staple-app phase_staple_app || return 1
  run_phase verify-zip phase_verify_zip || return 1
  run_phase create-dmg phase_create_dmg || return 1
  run_phase sign-dmg phase_sign_dmg || return 1
  run_phase verify-dmg phase_verify_dmg || return 1
  run_phase submit phase_submit || return 1
  run_phase fetch-log phase_fetch_log || return 1
  run_phase check-log phase_check_log || return 1
  run_phase staple phase_staple || return 1
  run_phase verify-staple phase_verify_staple || return 1
  run_phase gatekeeper phase_gatekeeper || return 1
  run_phase publish phase_publish || return 1
}

main() {
  trap cleanup_release EXIT
  if ! cd "$release_root_dir"; then
    die "could not enter repository root: $release_root_dir"
    return 1
  fi
  run_release || return 1
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then main "$@"; fi
