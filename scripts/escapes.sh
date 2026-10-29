#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

(cd capabilities/server && go run ./cmd/check-boundaries -root ../..)

known=''

ui_pattern='<(button|input|select|textarea|table|dialog|form|section|details)[ >]|rounded-md border border-border bg-surface'

hits() {
  for f in $(find web/packages web/apps -path '*/src/*.tsx' -not -path 'web/packages/ui/*' -not -path 'web/packages/kernel/*' -not -name '*.test.tsx' | sort); do
    n=$(grep -cE "$ui_pattern" "$f" || true); [[ $n == 0 ]] || printf 'ui\t%s\t%s\n' "$f" "$n"
  done
}

status=0 files=0 total=0
found=$(hits)
while IFS=$'\t' read -r kind file n; do
  [[ -n $file ]] || continue
  allowed=$(awk -F'\t' -v k="$kind" -v f="$file" '$1==k && $2==f {print $3}' <<<"$known")
  files=$((files+1)) total=$((total+n))
  if [[ -z $allowed ]]; then
    echo "escape: $file builds what the platform owns ($n × $kind); compose the owner's capability or extend it (AGENTS.md rule 4)" >&2; status=1
  elif (( n > allowed )); then
    echo "escape: $file has $n $kind escapes, known $allowed; compose the owner's capability or extend it (AGENTS.md rule 4)" >&2; status=1
  elif (( n < allowed )); then
    echo "escape: $file is down to $n $kind escapes; lower its known count in scripts/escapes.sh" >&2; status=1
  fi
done <<<"$found"
while IFS=$'\t' read -r kind file n _; do
  [[ -z $file ]] || grep -q "^$kind"$'\t'"$file"$'\t' <<<"$found" || { echo "escape: $file has no $kind escapes left; delete its line in scripts/escapes.sh" >&2; status=1; }
done <<<"$known"
(( status == 0 )) && echo "escapes ok: $total known in $files files, none new"
exit $status
