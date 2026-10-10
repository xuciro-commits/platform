#!/usr/bin/env bash
# Capability escapes (AGENTS.md rule 11, Intent.md "One owner per capability"):
# business code building a second copy of a capability the platform owns.
#   ui     an app's or a platform page's UI hand-makes a control, table, dialog,
#          form or panel instead of composing @platform/ui (the kit is the owner;
#          what it lacks is added there);
#   host   app code (apps/*/server, protocols/*, outside cmd/ and tests) reaches
#          the outside world or keeps state of its own instead of going through
#          the app API: files, effects, connectors, records and the ledger are
#          the host's.
# A refusal without a reason is not counted: the host gives it one (explained).
# The known escapes below are counted per file, each with the work item that
# removes it. A count may only fall: a new file or a higher count fails. When
# an escape is removed, lower or delete its line here in the same change.
set -euo pipefail
cd "$(dirname "$0")/.."

known=$(cat <<'KNOWN'
host	apps/mes/server/mes.go	1	#129 decided (2026-10-10): the plant's gateway states are the app's own K2 facts, tenant state persisted through platform.SnapshotFacts/RestoreFacts and named by the change log's fact check - not a second ledger
host	apps/pms/server/pms.go	1	#129 decided (2026-10-10): the channel's messages are the app's own K2 facts, snapshotted with its ledger through the app API; no host-owned copy exists
ui	web/packages/build/src/functions/decision-table.tsx	1	ADR-0062: decision matrix as plain table until DataTable grows inline editing (kept on condition, not a delivery gap)
ui	web/packages/build/src/ontology/process.tsx	2	ADR-0053 §11: property and permission matrices as plain tables until DataTable grows inline editing (kept on condition)
ui	web/packages/build/src/projects/project-roles.tsx	1	ADR-0066: roles and scope matrix as plain table until DataTable grows inline editing (kept on condition)
ui	web/packages/build/src/workshop/editor.tsx	1	ADR-0053 §11: live variable values in the page dock as a plain table (kept on condition)
KNOWN
)

ui_pattern='<(button|input|select|textarea|table|dialog|form|section|details)[ >]|rounded-md border border-border bg-surface'
host_pattern='"(net|net/http|net/url|database/sql|os|os/exec|io/fs|path/filepath)"|github.com/minio|kernel\.NewFactLog|sync\.Map'

hits() {
  for f in $(find web/packages web/apps -path '*/src/*.tsx' -not -path 'web/packages/ui/*' -not -path 'web/packages/kernel/*' -not -name '*.test.tsx' | sort); do
    n=$(grep -cE "$ui_pattern" "$f" || true); [[ $n == 0 ]] || printf 'ui\t%s\t%s\n' "$f" "$n"
  done
  for f in $(find apps/*/server protocols -name '*.go' -not -name '*_test.go' -not -path '*/cmd/*' | sort); do
    n=$(grep -cE "$host_pattern" "$f" || true); [[ $n == 0 ]] || printf 'host\t%s\t%s\n' "$f" "$n"
  done
}

status=0 files=0 total=0
found=$(hits)
while IFS=$'\t' read -r kind file n; do
  [[ -n $file ]] || continue
  allowed=$(awk -F'\t' -v k="$kind" -v f="$file" '$1==k && $2==f {print $3}' <<<"$known")
  files=$((files+1)) total=$((total+n))
  if [[ -z $allowed ]]; then
    echo "escape: $file builds what the platform owns ($n × $kind); compose the owner's capability or extend it (AGENTS.md rule 11)" >&2; status=1
  elif (( n > allowed )); then
    echo "escape: $file has $n $kind escapes, known $allowed; compose the owner's capability or extend it (AGENTS.md rule 11)" >&2; status=1
  elif (( n < allowed )); then
    echo "escape: $file is down to $n $kind escapes; lower its known count in scripts/escapes.sh" >&2; status=1
  fi
done <<<"$found"
while IFS=$'\t' read -r kind file n _; do
  [[ -z $file ]] || grep -q "^$kind"$'\t'"$file"$'\t' <<<"$found" || { echo "escape: $file has no $kind escapes left; delete its line in scripts/escapes.sh" >&2; status=1; }
done <<<"$known"
(( status == 0 )) && echo "escapes ok: $total known in $files files, none new"
exit $status
