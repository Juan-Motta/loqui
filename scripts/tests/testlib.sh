#!/usr/bin/env bash
set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }
assert_file() { [ -f "$1" ] || fail "missing file: $1"; }
assert_dir() { [ -d "$1" ] || fail "missing directory: $1"; }
assert_absent() { [ ! -e "$1" ] || fail "unexpected path: $1"; }
assert_eq() { [ "$1" = "$2" ] || fail "got '$1', want '$2'"; }
assert_contains() { grep -F -- "$2" "$1" >/dev/null || fail "$1 does not contain: $2"; }
assert_not_contains() { ! grep -F -- "$2" "$1" >/dev/null || fail "$1 contains: $2"; }

put_file() {
  mkdir -p "$(dirname "$1")"
  printf '%s\n' "${2:-fixture}" >"$1"
  chmod "${3:-755}" "$1"
}

run_expect_fail() {
  if "$@"; then
    fail "command unexpectedly passed: $*"
  fi
}

run_expect_fail_msg() {
  expected_stderr="$1"
  shift
  failure_dir="$(mktemp -d "${TMPDIR:-/tmp}/loqui-failure.XXXXXX")"
  failure_stdout="$failure_dir/stdout"
  failure_stderr="$failure_dir/stderr"
  failure_rc=0
  if "$@" >"$failure_stdout" 2>"$failure_stderr"; then
    rm -rf "$failure_dir"
    fail "command unexpectedly passed: $*"
  else
    failure_rc=$?
  fi
  if [ "$failure_rc" -eq 97 ]; then
    rm -rf "$failure_dir"
    fail "command hit reserved fake status 97: $*"
  fi
  if ! grep -F -- "$expected_stderr" "$failure_stderr" >/dev/null; then
    failure_text="$(cat "$failure_stderr")"
    rm -rf "$failure_dir"
    fail "stderr does not contain '$expected_stderr': $failure_text"
  fi
  rm -rf "$failure_dir"
}
