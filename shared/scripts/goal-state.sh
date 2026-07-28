#!/bin/sh
# goal-state.sh — section-scoped, CRLF-tolerant readers/writers for /goal state.md (§4/§6.2/§10).
# field <name> [file] | round-count <plan|code> [file] | ship-red-count [file] | ship-red-bump [file]
set -eu
# section <header> <file>: lines under '## <header>' up to the next '## ' (CRLF-tolerant)
section() { awk -v h="$1" '{ sub(/\r$/,"") } $0=="## " h {i=1;next} /^## / {i=0} i {print}' "$2"; }
cmd="${1:-}"
case "$cmd" in
  field)
    name="${2:-}"; file="${3:-.workflow/state.md}"; [ -f "$file" ] || { printf ''; exit 0; }
    section "/goal loop" "$file" | awk -v k="$name" '
      $0 ~ ("^\\| *" k " *\\|") { s=$0; sub(/^\| *[^|]* *\| */,"",s); sub(/ *\|.*$/,"",s); print s; exit }'
    ;;
  round-count)
    loop="${2:-}"; file="${3:-.workflow/state.md}"; [ -f "$file" ] || { echo 0; exit 0; }
    section "Review log" "$file" | grep -c -e "loop=$loop .*kind=round" 2>/dev/null | head -1 || echo 0
    ;;
  ship-red-count)
    file="${2:-.workflow/state.md}"; [ -f "$file" ] || { echo 0; exit 0; }
    n=$(section "Attempts" "$file" | sed -n 's/.*ATTEMPT ship-red — n=\([0-9][0-9]*\).*/\1/p' | tail -1)
    [ -n "$n" ] && echo "$n" || echo 0
    ;;
  ship-red-bump)
    file="${2:-.workflow/state.md}"
    cur=$(sh "$0" ship-red-count "$file"); next=$((cur + 1))
    ts=$(date -u +%Y-%m-%dT%H:%M:%SZ); line="- ATTEMPT ship-red — n=$next — ts=$ts"
    [ -f "$file" ] || : > "$file"
    if grep -q '^## Attempts' "$file" 2>/dev/null; then
      awk -v L="$line" '
        { sub(/\r$/,"") }
        /^## / && insec { print L; insec=0 }
        { print }
        $0=="## Attempts" { insec=1 }
        END { if (insec) print L }' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
    else
      printf '\n## Attempts\n%s\n' "$line" >> "$file"
    fi
    echo "$next"
    ;;
  *) echo "goal-state: unknown subcommand '$cmd'" >&2; exit 3 ;;
esac
