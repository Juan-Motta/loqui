#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
. "$repo_root/scripts/tests/testlib.sh"

setup_script="$repo_root/scripts/setup-dmgbuild.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-dmgbuild-setup-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
tmp="$(cd "$tmp" && pwd -P)"
fake_python="$tmp/fake-python3"
calls="$tmp/calls"

cat >"$fake_python" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_PYTHON_CALLS"
if [ "${1:-}" = -m ] && [ "${2:-}" = venv ]; then
  target="$3"
  mkdir -p "$target/bin"
  cp "$0" "$target/bin/python"
  exit 0
fi
if [ "${1:-}" = -m ] && [ "${2:-}" = pip ]; then
  printf '%s\n' 'fake pip output'
  exit "${FAKE_PIP_RC:-0}"
fi
if [ "${1:-}" = -c ]; then
  case "$2" in
    *sys.version_info*) exit "${FAKE_PYTHON_VERSION_RC:-0}" ;;
    *os.rename*) mv "$3" "$4"; exit 0 ;;
  esac
  printf '%s\n' "${FAKE_DMGBUILD_VERSION:-1.6.7}"
  exit 0
fi
exit 91
FAKE
chmod +x "$fake_python"

: >"$calls"
bootstrap_stderr="$tmp/bootstrap.stderr"
python_path="$(
  FAKE_PYTHON_CALLS="$calls" \
  LOQUI_PYTHON3="$fake_python" \
  LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/tools" \
  "$setup_script" 2>"$bootstrap_stderr"
)"
assert_eq "$python_path" "$tmp/tools/dmgbuild-$(shasum -a 256 \
  "$repo_root/build/darwin/dmg/requirements.txt" | awk '{print $1}')/bin/python"
assert_contains "$bootstrap_stderr" 'fake pip output'
assert_contains "$calls" '-m venv'
assert_contains "$calls" '-m pip install --disable-pip-version-check --isolated --no-cache-dir --require-hashes --only-binary=:all:'
assert_contains "$calls" 'importlib.metadata.version'

first_call_count="$(wc -l <"$calls" | tr -d ' ')"
FAKE_PYTHON_CALLS="$calls" \
LOQUI_PYTHON3="$fake_python" \
LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/tools" \
  "$setup_script" >/dev/null
assert_eq "$(wc -l <"$calls" | tr -d ' ')" "$((first_call_count + 2))"
assert_contains "$calls" 'sys.version_info'
assert_contains "$calls" 'importlib.metadata.version'

run_expect_fail_msg() {
  label="$1"
  expected="$2"
  shift 2
  output="$tmp/$label.out"
  if "$@" >"$output" 2>&1; then
    fail "$label unexpectedly succeeded"
  fi
  assert_contains "$output" "$expected"
}

run_expect_fail_msg wrong-version 'installed dmgbuild version is' \
  env FAKE_PYTHON_CALLS="$calls" FAKE_DMGBUILD_VERSION=9.9.9 \
    LOQUI_PYTHON3="$fake_python" LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/wrong-version" \
    "$setup_script"
run_expect_fail_msg relative-tools-root 'tools root must be absolute' \
  env FAKE_PYTHON_CALLS="$calls" LOQUI_PYTHON3="$fake_python" \
    LOQUI_DMGBUILD_TOOLS_ROOT=relative "$setup_script"
run_expect_fail_msg old-python 'Python 3.10 or newer is required' \
  env FAKE_PYTHON_CALLS="$calls" FAKE_PYTHON_VERSION_RC=1 \
    LOQUI_PYTHON3="$fake_python" LOQUI_DMGBUILD_TOOLS_ROOT="$tmp/old-python" \
    "$setup_script"

echo 'setup-dmgbuild-test: PASS'
