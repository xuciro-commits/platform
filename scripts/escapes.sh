#!/usr/bin/env bash
# Capability escapes (AGENTS.md rule 11, Intent.md "One owner per capability"):
# business code building a second copy of a capability the platform owns.
#   ui     an app's or a platform page's UI hand-makes a control, table, dialog,
#          form or panel instead of composing @platform/ui (the kit is the owner;
#          what it lacks is added there);
#   host   app code (apps/*/server, protocols/*, outside cmd/ and tests) reaches
#          the outside world or keeps its own log instead of going through the
#          app API: files, effects, connectors and the ledger are the host's.
# The known escapes below are counted per file, each with the work item that
# removes it. A count may only fall: a new file or a higher count fails. When
# an escape is removed, lower or delete its line here in the same change.
set -euo pipefail
cd "$(dirname "$0")/.."

known=$(cat <<'KNOWN'
ui	web/packages/crm/src/index.tsx	2	#129 the opportunity's own activity timeline beside the platform's comments
ui	web/packages/erp/src/index.tsx	2	#129 on hand and the trial balance as hand-made tables (a report is a declared read the kit shows)
ui	web/packages/mes/src/index.tsx	3	#129 the ERP's refusals as the MES's own panel (the platform's tasks and answers); panels (kit: card)
ui	web/packages/app/src/agents.tsx	10	#129 run steps, citations and pickers as bare buttons and forms (kit: disclosure, link, list)
ui	web/packages/app/src/index.tsx	2	#129 the assistant's form and the CSV file input (kit: form, file picker)
ui	web/packages/app/src/actions.tsx	1	#129 a boolean field as a bare checkbox (kit: checkbox)
ui	web/packages/platform/src/ai.tsx	5	#129 checkboxes, the prompt, the limit form (kit: checkbox, form)
ui	web/packages/platform/src/apps.tsx	2	#129 app and package panels (kit: card)
ui	web/packages/platform/src/operations.tsx	7	#129 checkboxes and panels (kit: checkbox, card)
ui	web/packages/platform/src/processes.tsx	2	#129 the evaluation form and checkbox (kit: form, checkbox)
ui	web/packages/platform/src/knowledge.tsx	1	#129 the knowledge search form (kit: form)
ui	web/packages/platform/src/people.tsx	5	#129 the organisation tree as bare buttons, panels (kit: tree, card)
host	apps/mes/server/mes.go	1	#129 the plant's own fact log beside the ledger (gateway states): judge whether the ledger owns it
host	apps/pms/server/pms.go	1	#129 the channel's own fact log beside the ledger: judge whether the ledger owns it
KNOWN
)

ui_pattern='<(button|input|select|textarea|table|dialog|form|section)[ >]'
host_pattern='"(net|net/http|net/url|database/sql|os|os/exec|io/fs|path/filepath)"|github.com/minio|kernel\.NewFactLog'

hits() {
  for f in $(find web/packages -path '*/src/*.tsx' -not -path 'web/packages/ui/*' -not -path 'web/packages/kernel/*' -not -name '*.test.tsx' | sort); do
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
