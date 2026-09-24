#!/usr/bin/env bash
# Dependency boundaries across all apps (#103, Platform.md "Platform model"):
#   1. an app never depends on another app; a protocol depends on no app;
#   2. an app's own code (not its cmd/ binaries or tests) uses the app-facing
#      platform API only: Caller, Manifest and the declarations, never the host
#      runtime (tenants, hosts, journals, deployment, the platform apps' types).
# A protocol's conformance harness drives a tenant and is the one exception.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

apps=(slices/hotel/server:hotel slices/crm/server:crm slices/manufacturing/server:mes)
protocols=(protocols/lodging:lodging)
names=$(for x in "${apps[@]}"; do echo "${x#*:}"; done)

for x in "${apps[@]}" "${protocols[@]}"; do
  dir=${x%%:*} self=${x#*:}
  deps=$(cd "$dir" && go list -deps ./... 2>/dev/null)
  for other in $names; do
    [[ $other == "$self" ]] && continue
    grep -qx "$other" <<<"$deps" && fail "$self depends on the app $other"
  done
done

host='NewTenant|Tenant|NewHost|Host|Journal|OpenJournal|Entry|Deployment|Flags|RunWork|Tokens|OIDC|NewDirectory|Directory|NewOrganization|Organization|NewRelations|Relations|Seat|Memberships|Reply|WriteJSON'
for x in "${apps[@]}" "${protocols[@]}"; do
  dir=${x%%:*}
  hits=$(grep -rnE "platformserver\.($host)\b" "$dir" --include='*.go' --exclude='*_test.go' --exclude='conformance.go' | grep -v '/cmd/' || true)
  [[ -z $hits ]] || fail "app code reaches the host runtime:"$'\n'"$hits"
done
echo "boundaries ok: ${#apps[@]} apps, ${#protocols[@]} protocol"
