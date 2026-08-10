#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"
integrity_script="$repo_root/scripts/whisper-model-integrity.sh"
[ -f "$integrity_script" ] || fail "missing production model-integrity helper"
# shellcheck source=scripts/whisper-model-integrity.sh
. "$integrity_script"

store_bytes="$(awk '
  /^[[:space:]]*Bytes:[[:space:]]*[0-9]+,/ {
    value=$2; sub(/,$/, "", value); print value; exit
  }
' "$repo_root/internal/store/model.go")"
store_sha256="$(awk '
  /^[[:space:]]*SHA256:[[:space:]]*"[0-9a-f]+",/ {
    value=$2; gsub(/[",]/, "", value); print value; exit
  }
' "$repo_root/internal/store/model.go")"

[ -n "$store_bytes" ] || fail "could not read WhisperModel.Bytes from internal/store/model.go"
[ -n "$store_sha256" ] || fail "could not read WhisperModel.SHA256 from internal/store/model.go"
assert_eq "$LOQUI_WHISPER_MODEL_BYTES" "$store_bytes"
assert_eq "$LOQUI_WHISPER_MODEL_SHA256" "$store_sha256"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-model-integrity.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
fixture="$tmp/model.bin"
put_file "$fixture" model
fixture_bytes="$(/usr/bin/stat -f '%z' "$fixture")"
fixture_sha256="$(/usr/bin/shasum -a 256 "$fixture" | awk '{ print $1 }')"

verify_file_integrity "$fixture" "$fixture_bytes" "$fixture_sha256" "test model"

if verify_file_integrity "$fixture" "$((fixture_bytes + 1))" "$fixture_sha256" \
  "test model" >"$tmp/size.log" 2>&1; then
  fail "wrong expected size unexpectedly passed"
fi
assert_contains "$tmp/size.log" "test model size is"

if verify_file_integrity "$fixture" "$fixture_bytes" \
  0000000000000000000000000000000000000000000000000000000000000000 \
  "test model" >"$tmp/digest.log" 2>&1; then
  fail "wrong expected digest unexpectedly passed"
fi
assert_contains "$tmp/digest.log" "test model SHA-256 is"

echo "whisper-model-integrity-test: PASS"
