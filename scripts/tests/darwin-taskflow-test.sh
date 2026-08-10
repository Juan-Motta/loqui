#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
task_runner="${TASK_RUNNER:-$repo_root/scripts/task.sh}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-darwin-taskflow.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fail() {
  echo "darwin-taskflow-test: $*" >&2
  exit 1
}

assert_single_helper_build_before_bundle() {
  local task_name="$1"
  local rendered helper_count helper_line bundle_line

  rendered="$($task_runner --dry "$task_name" 2>&1)"
  helper_count="$(printf '%s\n' "$rendered" | awk 'index($0, "build-macos-helpers.sh") { count++ } END { print count + 0 }')"
  [ "$helper_count" -eq 1 ] || fail "$task_name must render exactly one helper build (found $helper_count)"

  helper_line="$(printf '%s\n' "$rendered" | awk 'index($0, "build-macos-helpers.sh") { print NR; exit }')"
  bundle_line="$(printf '%s\n' "$rendered" | awk 'index($0, "macos-bundle.sh") { print NR; exit }')"
  [ -n "$bundle_line" ] || fail "$task_name did not render the macOS bundler"
  [ "$helper_line" -lt "$bundle_line" ] || fail "$task_name rendered the bundler before its helpers"
}

for task_name in darwin:package darwin:package:universal darwin:run; do
  assert_single_helper_build_before_bundle "$task_name"
done

helpers_dir="$tmp/helpers"
events="$tmp/events"
fake_bin="$tmp/bin"
mkdir -p "$tmp/fakes" "$fake_bin"
[ ! -e "$helpers_dir" ] || fail "hermetic helper output must start absent"

cat >"$tmp/fakes/build-helpers" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
: "${LOQUI_HELPERS_OUTPUT_DIR:?}"
: "${LOQUI_TASKFLOW_EVENTS:?}"
[ ! -e "$LOQUI_HELPERS_OUTPUT_DIR" ] || {
  echo "helper output was not empty" >&2
  exit 1
}
mkdir -p "$LOQUI_HELPERS_OUTPUT_DIR"
touch "$LOQUI_HELPERS_OUTPUT_DIR/complete"
printf '%s\n' helpers >>"$LOQUI_TASKFLOW_EVENTS"
SH

cat >"$tmp/fakes/patch-plists" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
: "${LOQUI_TASKFLOW_EVENTS:?}"
printf '%s\n' plists >>"$LOQUI_TASKFLOW_EVENTS"
SH

cat >"$tmp/fakes/bundle" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
: "${LOQUI_TASKFLOW_EVENTS:?}"

helpers_dir=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --helpers-dir) helpers_dir="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done

[ -f "$helpers_dir/complete" ] || {
  echo "bundler ran before helper output was complete" >&2
  exit 1
}
[ -n "$output" ] || {
  echo "missing --output" >&2
  exit 1
}
mkdir -p "$output/Contents/MacOS"
cat >"$output/Contents/MacOS/loqui" <<'APP'
#!/usr/bin/env bash
set -euo pipefail
: "${LOQUI_TASKFLOW_EVENTS:?}"
printf '%s\n' launch >>"$LOQUI_TASKFLOW_EVENTS"
APP
chmod +x "$output/Contents/MacOS/loqui"
printf '%s\n' bundle >>"$LOQUI_TASKFLOW_EVENTS"
SH

cat >"$tmp/fakes/sign" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
: "${LOQUI_TASKFLOW_EVENTS:?}"
printf '%s\n' sign >>"$LOQUI_TASKFLOW_EVENTS"
SH

chmod +x "$tmp/fakes/build-helpers" "$tmp/fakes/patch-plists" "$tmp/fakes/bundle" "$tmp/fakes/sign"

LOQUI_HELPERS_OUTPUT_DIR="$helpers_dir" \
LOQUI_HELPERS_BUILD_SCRIPT="$tmp/fakes/build-helpers" \
LOQUI_PATCH_PLISTS_SCRIPT="$tmp/fakes/patch-plists" \
LOQUI_BUNDLE_SCRIPT="$tmp/fakes/bundle" \
LOQUI_SIGN_SCRIPT="$tmp/fakes/sign" \
LOQUI_TASKFLOW_EVENTS="$events" \
  "$task_runner" darwin:run BIN_DIR="$fake_bin" >/dev/null

expected_events="$(printf '%s\n' helpers plists bundle sign launch)"
actual_events="$(cat "$events")"
[ "$actual_events" = "$expected_events" ] || {
  printf 'expected task flow:\n%s\nactual task flow:\n%s\n' "$expected_events" "$actual_events" >&2
  fail "run task did not build helpers exactly once before bundling"
}

echo "darwin-taskflow-test: PASS"
