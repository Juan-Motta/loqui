#!/usr/bin/env bash
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
host_entitlements="$script_root/build/darwin/Loqui.entitlements"
audio_entitlements="$script_root/build/darwin/LoquiAudioHelper.entitlements"

die() { echo "macos-sign: $*" >&2; exit 1; }

identity_records() {
  security find-identity -v -p codesigning |
    sed -n 's/^[[:space:]]*[0-9][0-9]*) \([0-9A-Fa-f][0-9A-Fa-f]*\) "\(.*\)"$/\1\	\2/p'
}

resolve_identity() {
  channel="$1"
  case "$channel" in
    development)
      prefix="Apple Development:"
      configured="${LOQUI_DEV_SIGN_IDENTITY:-}"
      ;;
    release)
      prefix="Developer ID Application:"
      configured="${LOQUI_SIGN_IDENTITY:-}"
      ;;
    *) die "resolve channel must be development or release" ;;
  esac

  records="$(identity_records | awk -F '\t' -v prefix="$prefix" 'index($2, prefix) == 1' | LC_ALL=C sort -u -k1,1)"
  if [ -n "$configured" ]; then
    matches="$(printf '%s\n' "$records" | awk -F '\t' -v wanted="$configured" '$1 == wanted || $2 == wanted')"
    match_count="$(printf '%s\n' "$matches" | awk 'NF {count++} END {print count+0}')"
    [ "$match_count" -eq 1 ] || die "configured identity '$configured' does not match exactly one $prefix identity"
    printf '%s\n' "$matches" | awk -F '\t' 'NF {print $1; exit}'
    return
  fi

  count="$(printf '%s\n' "$records" | awk 'NF {count++} END {print count+0}')"
  if [ "$count" -eq 0 ]; then
    if [ "$channel" = development ]; then
      echo "macos-sign: no Apple Development identity; using ad-hoc signing. TCC continuity is unavailable" >&2
      printf '%s\n' -
      return
    fi
    die "no valid Developer ID Application identity found"
  fi
  [ "$count" -eq 1 ] || die "ambiguous $prefix identities; configure the exact SHA-1 or full name"
  printf '%s\n' "$records" | awk -F '\t' 'NF {print $1; exit}'
}

run_sign() {
  signing_channel="$1"
  shift
  codesign "$@" && return 0
  status=$?
  if [ "$signing_channel" = release ]; then
    echo "macos-sign: Developer ID signing failed; check Apple timestamp-service/network access, then certificate validity and code layout" >&2
  fi
  return "$status"
}

sign_item() {
  mode="$1"
  identity="$2"
  target="$3"
  identifier="${4:-}"
  entitlements="${5:-}"
  runtime="${6:-0}"
  args=(--force)
  if [ "$mode" != adhoc ] && [ "$runtime" != 0 ]; then args+=(--options runtime); fi
  case "$mode" in
    release) args+=(--timestamp) ;;
    development) args+=(--timestamp=none) ;;
    adhoc) ;;
    *) die "unknown signing mode: $mode" ;;
  esac
  if [ "$mode" != adhoc ] && [ -n "$entitlements" ]; then args+=(--entitlements "$entitlements"); fi
  if [ "$mode" = adhoc ]; then args+=(--sign -); else args+=(--sign "$identity"); fi
  [ -z "$identifier" ] || args+=(--identifier "$identifier")
  args+=("$target")
  run_sign "$mode" "${args[@]}"
}

sign_app() {
  channel="$1"
  app="$2"
  identity="${3:-}"
  [ -d "$app" ] || die "missing app: $app"
  plist="$app/Contents/Info.plist"
  [ -f "$plist" ] || die "missing Contents/Info.plist"
  [ -f "$host_entitlements" ] || die "missing host entitlements"
  [ -f "$audio_entitlements" ] || die "missing audio-helper entitlements"

  case "$channel" in
    release)
      expected_app_id="com.jualopezmo.loquigo"
      helper_base="$expected_app_id"
      mode=release
      [ -n "$identity" ] || identity="$(resolve_identity release)"
      ;;
    development)
      expected_app_id="com.jualopezmo.loquigo.dev"
      helper_base="$expected_app_id"
      [ -n "$identity" ] || identity="$(resolve_identity development)"
      if [ "$identity" = - ]; then
        mode=adhoc
        echo "macos-sign: ad-hoc development signing; TCC continuity is unavailable" >&2
      else
        mode=development
      fi
      ;;
    adhoc)
      expected_app_id="com.jualopezmo.loquigo"
      helper_base="$expected_app_id"
      identity=-
      mode=adhoc
      ;;
    *) die "app channel must be adhoc, development, or release" ;;
  esac

  actual_app_id="$(plutil -extract CFBundleIdentifier raw "$plist" 2>/dev/null || true)"
  [ "$actual_app_id" = "$expected_app_id" ] \
    || die "CFBundleIdentifier is '$actual_app_id', expected '$expected_app_id'"

  frameworks="$app/Contents/Frameworks"
  helpers="$app/Contents/Helpers"
  while read -r dylib; do
    [ -n "$dylib" ] || continue
    sign_item "$mode" "$identity" "$dylib"
  done < <(find "$frameworks" -type f -name '*.dylib' -print | LC_ALL=C sort)

  speech_framework="$frameworks/MicrosoftCognitiveServicesSpeech.framework"
  [ -d "$speech_framework" ] || die "missing MicrosoftCognitiveServicesSpeech.framework"
  sign_item "$mode" "$identity" "$speech_framework"

  for helper in globe-listener macos-stt whisper-stt; do
    helper_path="$helpers/$helper"
    [ -f "$helper_path" ] || die "missing helper: $helper"
    helper_id="$helper_base.$helper"
    case "$helper" in
      globe-listener) sign_item "$mode" "$identity" "$helper_path" "$helper_id" "" 1 ;;
      macos-stt|whisper-stt) sign_item "$mode" "$identity" "$helper_path" "$helper_id" "$audio_entitlements" 1 ;;
    esac
  done
  sign_item "$mode" "$identity" "$app" "" "$host_entitlements" 1
  codesign --verify --deep --strict --verbose=2 "$app"
}

command_name="${1:-}"
[ -n "$command_name" ] || die "expected resolve, app, or dmg"
shift
case "$command_name" in
  resolve)
    channel=""
    while [ "$#" -gt 0 ]; do
      case "$1" in --channel) [ "$#" -ge 2 ] || die "--channel requires a value"; channel="$2"; shift 2 ;; *) die "unknown argument: $1" ;; esac
    done
    resolve_identity "$channel"
    ;;
  app)
    channel=""
    app=""
    identity=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --channel) [ "$#" -ge 2 ] || die "--channel requires a value"; channel="$2"; shift 2 ;;
        --app) [ "$#" -ge 2 ] || die "--app requires a path"; app="$2"; shift 2 ;;
        --identity) [ "$#" -ge 2 ] || die "--identity requires a value"; identity="$2"; shift 2 ;;
        *) die "unknown argument: $1" ;;
      esac
    done
    [ -n "$app" ] || die "missing --app"
    sign_app "$channel" "$app" "$identity"
    ;;
  dmg)
    dmg=""
    identity=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --dmg) [ "$#" -ge 2 ] || die "--dmg requires a path"; dmg="$2"; shift 2 ;;
        --identity) [ "$#" -ge 2 ] || die "--identity requires a value"; identity="$2"; shift 2 ;;
        *) die "unknown argument: $1" ;;
      esac
    done
    [ -f "$dmg" ] || die "missing DMG: $dmg"
    [ -n "$identity" ] || identity="$(resolve_identity release)"
    run_sign release --force --timestamp --sign "$identity" "$dmg"
    ;;
  *) die "unknown command: $command_name" ;;
esac
