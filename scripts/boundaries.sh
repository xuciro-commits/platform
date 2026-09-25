#!/usr/bin/env bash
# Dependency boundaries across all apps (#103, #104 G1; Platform.md "Platform model"):
#   1. an app never depends on another app; a protocol depends on no app;
#   2. an app's and a protocol's own packages import the app API
#      (platformserver/platform) and never the host runtime (platformserver):
#      the runtime is reachable only through platform.Runtime, which the host
#      hands each caller. Their binaries (cmd/) compose tenants and may import
#      it, and so may test harnesses (packages named *test, like httptest).
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

apps=(apps/hotel/server:hotel apps/crm/server:crm apps/manufacturing/server:mes apps/hr/server:hr apps/helpdesk/server:helpdesk)
protocols=(protocols/lodging:lodging)
names=$(for x in "${apps[@]}"; do echo "${x#*:}"; done)

for x in "${apps[@]}" "${protocols[@]}"; do
  dir=${x%%:*} self=${x#*:}
  deps=$(cd "$dir" && go list -deps ./... 2>/dev/null)
  for other in $names; do
    [[ $other == "$self" ]] && continue
    grep -qx "$other" <<<"$deps" && fail "$self depends on the app $other"
  done
  hits=$(cd "$dir" && go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | grep -vE '/cmd/|test:' | grep -E ' platformserver( |$)' || true)
  [[ -z $hits ]] || fail "app code imports the host runtime, not the app API:"$'\n'"$hits"
done
echo "boundaries ok: ${#apps[@]} apps, ${#protocols[@]} protocol"
