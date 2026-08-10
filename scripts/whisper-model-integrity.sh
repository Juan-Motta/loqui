#!/usr/bin/env bash

# Derived from internal/store/model.go. Keep this shell boundary fixed: callers must not be able to
# weaken the production model identity through an environment variable or command-line flag.
readonly LOQUI_WHISPER_MODEL_BYTES=487601967
readonly LOQUI_WHISPER_MODEL_SHA256=1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b

verify_file_integrity() {
  local integrity_path="$1"
  local integrity_expected_bytes="$2"
  local integrity_expected_sha256="$3"
  local integrity_label="$4"
  local integrity_actual_bytes
  local integrity_digest_line
  local integrity_actual_sha256

  if [ ! -f "$integrity_path" ]; then
    printf '%s is missing: %s\n' "$integrity_label" "$integrity_path" >&2
    return 1
  fi

  if ! integrity_actual_bytes="$(LC_ALL=C stat -f '%z' "$integrity_path")"; then
    printf 'cannot read %s size: %s\n' "$integrity_label" "$integrity_path" >&2
    return 1
  fi
  if [ "$integrity_actual_bytes" != "$integrity_expected_bytes" ]; then
    printf '%s size is %s bytes, expected %s\n' \
      "$integrity_label" "$integrity_actual_bytes" "$integrity_expected_bytes" >&2
    return 1
  fi

  if ! integrity_digest_line="$(LC_ALL=C shasum -a 256 "$integrity_path")"; then
    printf 'cannot compute %s SHA-256: %s\n' "$integrity_label" "$integrity_path" >&2
    return 1
  fi
  integrity_actual_sha256="${integrity_digest_line%%[[:space:]]*}"
  if [ "$integrity_actual_sha256" != "$integrity_expected_sha256" ]; then
    printf '%s SHA-256 is %s, expected %s\n' \
      "$integrity_label" "$integrity_actual_sha256" "$integrity_expected_sha256" >&2
    return 1
  fi
}

verify_whisper_model() {
  verify_file_integrity "$1" \
    "$LOQUI_WHISPER_MODEL_BYTES" \
    "$LOQUI_WHISPER_MODEL_SHA256" \
    "$2"
}
