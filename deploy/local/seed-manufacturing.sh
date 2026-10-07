#!/usr/bin/env bash
# Initialize the local manufacturing builder through its audited grant action.
# Keep initial seats unchanged so existing journals replay against their original roles.
set -euo pipefail
IDP=${IDP:-http://localhost:8480/auth/v1}
MANUFACTURING=${MANUFACTURING:-http://localhost:8490}
TOKEN=$(curl -sf "$IDP/oidc/token" -d grant_type=password -d client_id=platform-cli \
  -d client_secret=cliLocalOnly0000000000000000000000000000000000000000000000000000 \
  -d username=sup@plant.test -d password=Plant-Local-1 | jq -er .access_token)
ME=$(curl -sf -H "Authorization: Bearer $TOKEN" -H 'Platform-Tenant: plant-sz' "$MANUFACTURING/v1/me")
if [[ $(jq -r '.profile.roles.build // ""' <<< "$ME") == builder ]]; then
  echo 'plant-sz: manufacturing builder already initialized'
  exit 0
fi
BODY=$(jq -n --arg principal "$(jq -r .principalId <<< "$ME")" \
  --arg payload "$(printf '%s' '{"app":"build","role":"builder"}' | base64 | tr -d '\n')" \
  '{tenantId:"plant-sz",principalId:$principal,authority:"platform",idempotencyKey:"local-seed-manufacturing-builder-v1",schema:{name:"platform.member.grant",version:1},target:{type:"platform.member",id:$principal},payload:$payload}')
ANSWER=$(curl -sf -H "Authorization: Bearer $TOKEN" -H 'Platform-Tenant: plant-sz' -H 'Content-Type: application/json' \
  "$MANUFACTURING/v1/submissions" -d "$BODY")
jq -e 'if .error then error(.error.message // .error.code) else true end' <<< "$ANSWER" >/dev/null
curl -sf -H "Authorization: Bearer $TOKEN" -H 'Platform-Tenant: plant-sz' "$MANUFACTURING/v1/me" | jq -e '.profile.roles.build == "builder"' >/dev/null
echo 'plant-sz: manufacturing builder initialized'
