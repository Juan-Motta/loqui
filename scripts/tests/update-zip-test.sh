#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# shellcheck source=scripts/tests/testlib.sh
. "$repo_root/scripts/tests/testlib.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/loqui-update-zip-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

# shellcheck source=scripts/release-macos.sh
. "$repo_root/scripts/release-macos.sh"

fake_bin="$tmp/fake-bin"
mkdir "$fake_bin"
cat >"$fake_bin/codesign" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
cat >"$fake_bin/xcrun" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = stapler ] || exit 91
case "$2" in
  staple|validate) exit 0 ;;
  *) exit 92 ;;
esac
EOF
chmod +x "$fake_bin/codesign" "$fake_bin/xcrun"

stage="$tmp/stage"
mkdir -p "$stage/evidence-work/Loqui.app/Contents/MacOS"
app="$stage/Loqui.app"
put_file "$app/Contents/MacOS/loqui" "fixture executable" 755
release_root_dir="$repo_root"
version="$("$repo_root/scripts/release-version.sh" --root "$repo_root")"
PATH="$fake_bin:/usr/bin:/bin" phase_create_zip
assert_file "$zip"

extracted="$tmp/extracted"
mkdir "$extracted"
ditto -x -k "$zip" "$extracted"
assert_dir "$extracted/Loqui.app"
assert_file "$extracted/Loqui.app/Contents/MacOS/loqui"

PATH="$fake_bin:/usr/bin:/bin" phase_staple_app
PATH="$fake_bin:/usr/bin:/bin" phase_verify_zip
assert_file "$zip"

echo "update-zip-test: PASS"
