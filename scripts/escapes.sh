#!/usr/bin/env bash
# Capability escapes (AGENTS.md rule 4 "单一归属，先复用"; Intent.md "平台、能力与应用"):
# business code building a second copy of a capability the platform owns.
#   ui     an app's or a platform page's UI hand-makes a control, table, dialog,
#          form or panel instead of composing @platform/ui (the kit is the owner;
#          what it lacks is added there).
# A refusal without a reason is not counted: the host gives it one (explained).
# The known escapes below are counted per file, each with the work item that
# removes it. A count may only fall: a new file or a higher count fails. When
# an escape is removed, lower or delete its line here in the same change.
#
# There is no host-side escape category any more: it scanned the external app and
# protocol modules (apps/*/server, protocols/*), which are retired. Business
# applications are declarative packages in deploy/packages, so no Go business code
# is left to reach past the app API; what replaced that guarantee is the import
# check in scripts/boundaries.sh and cmd/check-boundaries.
set -euo pipefail
cd "$(dirname "$0")/.."

# the source guard checks member-facing storage reads structurally,
# including files excluded by the current OS/build tags.
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
