#!/bin/sh
# goal-digest.sh — /goal certification digest (design §6.1). ONE normalized git diff.
# Usage: sh goal-digest.sh <base_sha> [repo_dir] [--from-head]
# Exit: 0 ok · 3 bad env/args/tool/git failure (fail closed).
set -eu
LC_ALL=C; export LC_ALL
base="${1:-}"; repo="${2:-.}"; mode="${3:-}"
[ -n "$base" ] || { echo "goal-digest: missing base_sha" >&2; exit 3; }
command -v git >/dev/null 2>&1 || { echo "goal-digest: git not found" >&2; exit 3; }
if command -v sha256sum >/dev/null 2>&1; then SHA="sha256sum"
elif command -v shasum >/dev/null 2>&1; then SHA="shasum -a 256"
else echo "goal-digest: no sha256 tool" >&2; exit 3; fi
git -C "$repo" rev-parse --git-dir >/dev/null 2>&1 || { echo "goal-digest: not a repo" >&2; exit 3; }
git -C "$repo" cat-file -e "${base}^{commit}" 2>/dev/null || { echo "goal-digest: bad base" >&2; exit 3; }

set -f
set -- ':(exclude).workflow/*' ':(exclude)docs/e2e/reports/*' ':(exclude)CONTINUITY.md' \
       ':(exclude)docs/CHANGELOG.md' ':(exclude)VERSION'
DIFF="diff --full-index --no-ext-diff --no-textconv --default-prefix --no-renames --no-color"

out=$(mktemp); tmp_idx=""
cleanup() { rm -f "$out"; [ -n "$tmp_idx" ] && rm -f "$tmp_idx"; return 0; }
trap cleanup EXIT INT TERM

if [ "$mode" = "--from-head" ]; then
  git -C "$repo" -c core.quotepath=false $DIFF "$base" HEAD -- . "$@" > "$out" \
    || { echo "goal-digest: git diff failed" >&2; exit 3; }
else
  real_idx=$(git -C "$repo" rev-parse --git-path index)
  case "$real_idx" in /*) idx_abs="$real_idx" ;; *) idx_abs="$repo/$real_idx" ;; esac
  tmp_idx=$(mktemp)
  if [ -f "$idx_abs" ]; then cp "$idx_abs" "$tmp_idx"; else rm -f "$tmp_idx"; fi
  GIT_INDEX_FILE="$tmp_idx"; export GIT_INDEX_FILE
  git -C "$repo" add -N -- . "$@" >/dev/null 2>&1 \
    || { unset GIT_INDEX_FILE; echo "goal-digest: git add -N failed" >&2; exit 3; }
  git -C "$repo" -c core.quotepath=false $DIFF "$base" -- . "$@" > "$out" \
    || { unset GIT_INDEX_FILE; echo "goal-digest: git diff failed" >&2; exit 3; }
  unset GIT_INDEX_FILE
fi
$SHA < "$out" | cut -d' ' -f1
