#!/usr/bin/env bash
# Capability escapes (AGENTS.md rule 11, Intent.md "One owner per capability"):
# business code building a second copy of a capability the platform owns.
#   ui     an app's or a platform page's UI hand-makes a control, table, dialog,
#          form or panel instead of composing @platform/ui (the kit is the owner;
#          what it lacks is added there);
#   host   app code (apps/*/server, protocols/*, outside cmd/ and tests) reaches
#          the outside world or keeps state of its own instead of going through
#          the app API: files, effects, connectors, records and the ledger are
#          the host's;
#   refusal app code refuses without saying why (a bare kernel.Error): every
#          refusal carries a reason through platform.Refuse (docs/Testing.md C3).
# The known escapes below are counted per file, each with the work item that
# removes it. A count may only fall: a new file or a higher count fails. When
# an escape is removed, lower or delete its line here in the same change.
set -euo pipefail
cd "$(dirname "$0")/.."

known=$(cat <<'KNOWN'
host	apps/mes/server/mes.go	1	#129 (6) the plant's own fact log beside the ledger (gateway states): judge whether the ledger owns it
host	apps/pms/server/pms.go	1	#129 (6) the channel's own fact log beside the ledger: judge whether the ledger owns it
refusal	apps/crm/server/crm.go	15	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/csm/server/csm.go	10	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/erp/server/erp.go	4	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/erpadapter/server/erpadapter.go	10	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/hcm/server/hcm.go	7	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/mes/server/equipment.go	2	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/mes/server/mes.go	25	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	apps/pms/server/pms.go	9	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
refusal	protocols/lodging/memory.go	7	#129 refusals without a reason: platform.Refuse says why (the host's fallback names only the code)
KNOWN
)

ui_pattern='<(button|input|select|textarea|table|dialog|form|section|details)[ >]|rounded-md border border-border bg-surface'
host_pattern='"(net|net/http|net/url|database/sql|os|os/exec|io/fs|path/filepath)"|github.com/minio|kernel\.NewFactLog|sync\.Map'
refusal_pattern='&kernel\.Error\{Code:|\bfail\(pb\.|return (nil, )?(invalid|notFound|conflict|denied|unknown)(\(\))?$'

hits() {
  for f in $(find web/packages -path '*/src/*.tsx' -not -path 'web/packages/ui/*' -not -path 'web/packages/kernel/*' -not -name '*.test.tsx' | sort); do
    n=$(grep -cE "$ui_pattern" "$f" || true); [[ $n == 0 ]] || printf 'ui\t%s\t%s\n' "$f" "$n"
  done
  for f in $(find apps/*/server protocols -name '*.go' -not -name '*_test.go' -not -path '*/cmd/*' | sort); do
    n=$(grep -cE "$host_pattern" "$f" || true); [[ $n == 0 ]] || printf 'host\t%s\t%s\n' "$f" "$n"
  done
  for f in $(find apps/*/server protocols -name '*.go' -not -name '*_test.go' -not -path '*/cmd/*' | sort); do
    n=$(grep -cE "$refusal_pattern" "$f" || true); [[ $n == 0 ]] || printf 'refusal\t%s\t%s\n' "$f" "$n"
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
