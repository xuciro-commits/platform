#!/usr/bin/env bash
# Demo data for the local hospitality host (deploy/local, OIDC on): customers, an
# opportunity with a stay booked through the lodging protocol, and enough
# reservations for a night to be oversold, so the manager is notified (ADR-0013).
# Every entry is an ordinary decision with a fixed key: running it again changes nothing.
set -euo pipefail
IDP=${IDP:-http://localhost:8480/auth/v1} HOSPITALITY=${HOSPITALITY:-http://localhost:8495}
token() {
  curl -sf "$IDP/oidc/token" -d grant_type=password -d client_id=platform-cli \
    -d client_secret=cliLocalOnly0000000000000000000000000000000000000000000000000000 \
    -d username="$1" -d password=Plant-Local-1 | jq -r .access_token
}
MGR=$(token manager@hotel.test) REP=$(token sales@hotel.test)
submit() { # token authority key schema target-type target-id payload
  local who body
  who=$(curl -sf -H "Authorization: Bearer $1" "$HOSPITALITY/v1/me" | jq -r .principalId)
  body=$(jq -n --arg w "$who" --arg a "$2" --arg k "$3" --arg s "$4" --arg tt "$5" --arg ti "$6" --arg p "$(printf %s "$7" | base64)" \
    '{tenantId:"hotel-a", principalId:$w, authority:$a, idempotencyKey:$k, schema:{name:$s, version:1}, target:{type:$tt, id:$ti}, payload:$p}')
  printf '%-28s %-10s ' "$4" "$6"
  curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$HOSPITALITY/v1/submissions" -d "$body" | jq -c '.error // "ok"'
}
tomorrow=$(date -u -v+1d +%F 2>/dev/null || date -u -d tomorrow +%F)
after=$(date -u -v+3d +%F 2>/dev/null || date -u -d '+3 days' +%F)
submit "$REP" crm seed-a1 crm.account.create crm.account ACME '{"name":"Acme Corp","kind":"company"}'
submit "$REP" crm seed-a2 crm.account.create crm.account HARBOUR '{"name":"Harbour Travel","kind":"company"}'
submit "$REP" crm seed-o1 crm.opportunity.open crm.opportunity OPP-1 '{"account":"ACME","title":"Board offsite"}'
submit "$REP" crm seed-o2 crm.opportunity.open crm.opportunity OPP-2 '{"account":"HARBOUR","title":"Winter group series"}'
submit "$REP" crm seed-b1 crm.opportunity.book crm.opportunity OPP-1 "{\"roomType\":\"suite\",\"checkIn\":\"$tomorrow\",\"checkOut\":\"$after\",\"guest\":\"Acme board\"}"
for i in 1 2 3 4; do # three standard rooms and one of overbooking: the fourth oversells the night
  submit "$MGR" pms "seed-r$i" pms.reservation.create pms.reservation "R-10$i" \
    "{\"roomType\":\"standard\",\"checkIn\":\"$tomorrow\",\"checkOut\":\"$after\",\"guest\":\"Guest $i\"}"
done
