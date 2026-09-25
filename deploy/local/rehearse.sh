#!/usr/bin/env bash
# Operations rehearsal for the manufacturing slice (docs/WorkQueue.md #87,
# Platform.md §7): principals from Rauthy, state that survives a restart, and a
# PostgreSQL backup restored into a new volume. Runs a disposable compose
# project on its own ports and removes it afterwards. Needs docker, curl, jq.
set -euo pipefail
cd "$(dirname "$0")"
export PG_PORT=55433 IDP_PORT=58480 MES_PORT=58490 SALES_PORT=58495 SINK_PORT=58497
compose() { docker compose -p platform-rehearsal -f compose.yaml "$@"; }
IDP=http://localhost:$IDP_PORT/auth/v1 MES=http://localhost:$MES_PORT SALES=http://localhost:$SALES_PORT SINK=http://localhost:$SINK_PORT
backup=$(mktemp -d)
trap 'compose down -v --remove-orphans >/dev/null 2>&1; rm -rf "$backup"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
wait_for() { for _ in $(seq 60); do curl -sf -o /dev/null "$1" && return; sleep 1; done; fail "$1 never answered"; }

compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up -d --build --quiet-pull >/dev/null 2>&1
wait_for "$IDP/.well-known/openid-configuration"
[[ $(curl -s "$IDP/.well-known/openid-configuration" | jq -r .issuer) == "http://localhost:$IDP_PORT/auth/v1/" ]] || fail "issuer"

token() { # user → access token (password grant on the operations client)
  curl -sf "$IDP/oidc/token" -d grant_type=password -d client_id=platform-cli \
    -d client_secret=cliLocalOnly0000000000000000000000000000000000000000000000000000 \
    -d username="$1" -d password=Plant-Local-1 | jq -r .access_token
}
SUP=$(token sup@plant.test) OP1=$(token op1@plant.test) OP2=$(token op2@plant.test)
code() { curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $1" "$MES/v1/me"; }
for _ in $(seq 30); do [[ $(code "$SUP") == 200 ]] && break; sleep 1; done
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/me" | jq -r .principalId) == sup-1 ]] || fail "OIDC principal"
[[ $(code supervisor) == 401 ]] || fail "demo token accepted in production mode"
IFS=. read -r head claims sig <<<"$OP2"
while (( ${#claims} % 4 )); do claims+="="; done
forged=$(printf %s "$claims" | tr _- /+ | base64 -d | jq -c '.email = "sup@plant.test"' | base64 | tr +/ -_ | tr -d '=\n')
[[ -n $forged ]] || fail "could not forge"
[[ $(code "$head.$forged.$sig") == 401 ]] || fail "forged claims accepted"
echo "ok   principals come from Rauthy (demo tokens and forged claims refused)"

submit() { # token key schema target-type target-id payload [expected-revision]
  local body who
  local server=${SERVER:-$MES}
  who=$(curl -s -H "Authorization: Bearer $1" "$server/v1/me" | jq -r .principalId)
  body=$(jq -n --arg w "$who" --arg k "$2" --arg s "$3" --arg tt "$4" --arg ti "$5" --arg p "$(printf %s "$6" | base64)" --arg r "${7:-}" \
    --arg e "${EVIDENCE:-}" --arg tenant "${TENANT:-plant-sz}" --arg authority "${AUTHORITY:-plant-server}" \
    '{tenantId:$tenant, principalId:$w, authority:$authority, idempotencyKey:$k,
      schema:{name:$s, version:1}, target:{type:$tt, id:$ti}, payload:$p}
     + (if $r == "" then {} else {expectedRevision:($r|tonumber)} end)
     + (if $e == "" then {} else {evidenceFactIds:[$e]} end)')
  curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$server/v1/submissions" -d "$body"
}
state() { { for path in "records/mes.order?limit=500" "records/mes.sfc?limit=500" downtime planned-orders notifications; do curl -s -H "Authorization: Bearer $SUP" "$MES/v1/$path"; done
  curl -s -H "Authorization: Bearer $SUP" "$MES/v1/connectors" | jq -c '[.[] | {id, disabled}]'
  curl -s -H "Authorization: Bearer $SUP" "$MES/v1/ai-usage" | jq -c '.totals'
  for path in customers records/hotel.reservation records/hotel.room-type members links timeline records/crm.account records/crm.opportunity records/crm.opportunity/OPP-1 records/hr.leave records/work.approval; do curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/$path"; done
  curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/protocols" | jq -c '[.[] | {id, bound}]'; } |
  jq -cS 'walk(if type == "object" then del(.changed, .created) else . end)'; } # when the host accepted a record is not state: a resent decision is accepted again

# Inputs of every kind the journal keeps: a poll page, decisions, a push batch.
(cd ../../slices/manufacturing/server && MES_GATEWAY_SECRET=gatewayLocalOnly000000000000000000000000000000000000000000000000 \
  MES_ERP_SECRET=erpLocalOnly0000000000000000000000000000000000000000000000000000 \
  go run ./cmd/gateway-sim -server "$MES" -oidc-token "$IDP/oidc/token" -batches 4 -every 200ms >/dev/null)
submit "$SUP" r-1 mes.order.release mes.order WO-1 '{"product":"P-100","quantity":2,"sfcs":2}' | jq -e .record >/dev/null || fail release

# Platform operations (ADR-0013): the gateway's downtime reached the supervisor
# of the line, not its operators; Settings disables the ERP connector, and that
# is a decision the restart below keeps.
curl -s -H "Authorization: Bearer $SUP" "$MES/v1/notifications" | jq -e 'any(.[]; .title | startswith("Downtime on"))' >/dev/null || fail "supervisor not notified"
[[ $(curl -s -H "Authorization: Bearer $OP1" "$MES/v1/notifications" | jq length) == 0 ]] || fail "operator notified"
AUTHORITY=platform submit "$SUP" o-1 platform.connector.disable platform.connector erp '{}' | jq -e .record >/dev/null || fail "disable connector"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/connectors" | jq -r '.[] | select(.id == "erp") | .health') == disabled ]] || fail "connector health"
# ERP write-back (#101): the plant's order confirmation goes to the ERP endpoint
# the administrator bound; the ERP's number comes back as an observation.
AUTHORITY=platform submit "$SUP" o-2 platform.endpoint.add platform.endpoint erp-api \
  '{"url":"http://webhook-sink:8080/erp","secret":"sink","effects":["mes/erp-confirmation"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "bind ERP endpoint"
claim=$(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/planned-orders" | jq -r '.[] | select(.erpId == "PO-9003") | .factId')
EVIDENCE=$claim submit "$SUP" r-2 mes.order.release mes.order WO-2 '{"product":"P-100","quantity":12,"sfcs":1,"planned":"PO-9003"}' | jq -e .record >/dev/null || fail "release WO-2"
rev=0
for resource in FURNACE-1 CNC-11 CMM-1; do
  submit "$OP1" "w2-s$rev" mes.sfc.start mes.sfc WO-2-001 "{\"resource\":\"$resource\"}" $rev | jq -e .record >/dev/null || fail "start WO-2 at $resource"
  submit "$OP1" "w2-c$rev" mes.sfc.complete mes.sfc WO-2-001 '{}' $((rev + 1)) | jq -e .record >/dev/null || fail "complete WO-2 at $resource"
  rev=$((rev + 2))
done
for _ in $(seq 20); do [[ $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/records/mes.order?limit=500" | jq -r '.records[] | select(.id == "WO-2") | .erp') == confirmed ]] && break; sleep 0.5; done
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/records/mes.order?limit=500" | jq -r '.records[] | select(.id == "WO-2") | .erp + " " + .confirmation') == "confirmed CONF-100001" ]] || fail "ERP write-back: $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/records/mes.order?limit=500" | jq -c '.records[] | select(.id == "WO-2")')"
echo "ok   operations: downtime notified to the line's supervisor only; ERP connector disabled from Settings; a finished order confirmed to the ERP, its number back on the order"
submit "$OP1" s-1 mes.sfc.start mes.sfc WO-1-001 '{"resource":"FURNACE-1"}' 0 | jq -e .record >/dev/null || fail start
[[ $(submit "$OP2" s-2 mes.sfc.start mes.sfc WO-1-002 '{"resource":"FURNACE-1"}' 0 | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "line policy"

# An AI agent is a client with a role and lines: its catalog holds only what it
# may call, and the server refuses the rest even when the adapter is bypassed.
agent() { (cd ../../slices/manufacturing/server && MES_AGENT_CLIENT=mes-assistant \
  MES_AGENT_SECRET=assistantLocalOnly0000000000000000000000000000000000000000000000 \
  go run ./cmd/mes-agent -server "$MES" -oidc-token "$IDP/oidc/token" "$@"); }
[[ $(agent actions | jq -c '[.[].schema | select(startswith("work.") | not)]') == '["platform.notification.read","mes.downtime.reason","mes.order.reconfirm"]' ]] || fail "assistant catalog"
event=$(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/downtime" | jq -r 'first(.[] | select(.resource == "CNC-11")).id')
agent do mes.downtime.reason "$event" '{"reason":"Setup"}' | jq -e .record >/dev/null || fail "assistant reason"
! agent do mes.order.release WO-9 '{}' 2>/dev/null || fail "assistant acted outside its catalog"
AGENT=$(curl -sf "$IDP/oidc/token" -d grant_type=client_credentials -d client_id=mes-assistant \
  -d client_secret=assistantLocalOnly0000000000000000000000000000000000000000000000 | jq -r .access_token)
[[ $(submit "$AGENT" a-1 mes.order.release mes.order WO-9 '{"product":"P-100","quantity":1,"sfcs":1}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "server let the assistant release"
echo "ok   AI assistant: catalog of one plant action (and its own notifications), acted within line L1, refused outside it"

# An order the ERP refuses (no planned order) is corrected by the assistant. The
# posting cannot be recalled and an AI agent caused it, so it waits for a
# person (ADR-0014 D6); the supervisor approves, and the ERP confirms it.
# Email (ADR-0014 D7): the plant's and the platform's notifications are mailed
# to members who sign in with an email address, through the sink's SMTP server.
AUTHORITY=platform submit "$SUP" o-3 platform.endpoint.add platform.endpoint mail \
  '{"kind":"email","url":"smtp://webhook-sink:2525","from":"plant@plant.test","notifications":["mes","platform"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "add email endpoint"
submit "$SUP" r-3 mes.order.release mes.order WO-3 '{"product":"P-100","quantity":1,"sfcs":1}' | jq -e .record >/dev/null || fail "release WO-3"
rev=0
for resource in FURNACE-1 CNC-11 CMM-1; do
  submit "$OP1" "w3-s$rev" mes.sfc.start mes.sfc WO-3-001 "{\"resource\":\"$resource\"}" $rev | jq -e .record >/dev/null || fail "start WO-3 at $resource"
  submit "$OP1" "w3-c$rev" mes.sfc.complete mes.sfc WO-3-001 '{}' $((rev + 1)) | jq -e .record >/dev/null || fail "complete WO-3 at $resource"
  rev=$((rev + 2))
done
wo3() { curl -s -H "Authorization: Bearer $SUP" "$MES/v1/records/mes.order?limit=500" | jq -r '.records[] | select(.id == "WO-3") | .erp'; }
for _ in $(seq 20); do [[ $(wo3) == refused ]] && break; sleep 0.5; done
[[ $(wo3) == refused ]] || fail "ERP refusal of WO-3: $(wo3)"
submit "$AGENT" a-2 mes.order.reconfirm mes.order WO-3 '{"planned":"PO-9001"}' | jq -e .record >/dev/null || fail "assistant resend"
held=$(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/effects" | jq -r 'first(.[] | select(.key == "WO-3#2")) | .state + " " + .id')
[[ ${held%% *} == held ]] || fail "the agent's ERP posting was not held: $held"
sleep 1.5 && [[ $(wo3) == sent ]] || fail "a held effect was sent"
[[ $(AUTHORITY=platform submit "$AGENT" a-3 platform.effect.approve platform.effect "${held#* }" '{}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "an agent approved"
AUTHORITY=platform submit "$SUP" a-4 platform.effect.approve platform.effect "${held#* }" '{}' | jq -e .record >/dev/null || fail "approve"
for _ in $(seq 20); do [[ $(wo3) == confirmed ]] && break; sleep 0.5; done
[[ $(wo3) == confirmed ]] || fail "WO-3 after approval: $(wo3)"
mailed() { curl -s "$SINK/mail" | jq -r '[.[] | select(.to == "sup@plant.test") | .subject] | join("|")'; }
for _ in $(seq 20); do [[ $(mailed) == *"Approve Order confirmation"* ]] && break; sleep 0.5; done
[[ $(mailed) == *"ERP refused the confirmation of WO-3"* && $(mailed) == *"Approve Order confirmation to the ERP for mes.order/WO-3"* ]] || fail "mail: $(curl -s "$SINK/mail")"
echo "ok   ERP correction: a refused order resent by the AI assistant, held until the supervisor approved, then confirmed; the refusal and the approval request mailed to the supervisor"


# AI providers (ADR-0015): the plant's administrator adds a local model server
# (the sink speaks the OpenAI wire) and opens a model to ai users; an operator
# calls it through the host, the AI assistant (no ai role) is refused, and the
# call's usage is journaled (compared again after the restart below).
AUTHORITY=ai submit "$SUP" ai-1 ai.provider.add ai.provider local '{"kind":"local","baseUrl":"http://webhook-sink:8080/v1"}' | jq -e .record >/dev/null || fail "add AI provider"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/ai/providers/local/models" | jq -r '.[].id') == echo ]] || fail "provider catalog"
AUTHORITY=ai submit "$SUP" ai-2 ai.model.enable ai.model local/echo '{"access":"users"}' | jq -e .record >/dev/null || fail "enable model"
chat() { curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$MES/v1/ai/chat" -d '{"model":"local/echo","messages":[{"role":"user","content":"line one is down"}]}'; }
[[ $(chat "$OP1" | jq -r .content) == "echo: line one is down" ]] || fail "model call: $(chat "$OP1")"
[[ $(chat "$AGENT" | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "the assistant called a model open to ai users only"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/ai-usage" | jq -c '[.totals[] | {member, model, calls, input, output}]') == '[{"member":"op-l1","model":"local/echo","calls":1,"input":4,"output":5}]' ]] || fail "AI usage: $(curl -s -H "Authorization: Bearer $SUP" "$MES/v1/ai-usage")"
echo "ok   AI providers: a local model server added and a model opened to ai users; an operator's call answered and metered, the assistant refused"

# The sales solution: the CRM books a stay through the lodging protocol and the
# hotel provides it (ADR-0011); the platform app revokes a role and the catalog
# follows on the next request; a hotel cancellation reaches the opportunity's
# timeline through the platform link; an MCP client acts with a member's grants.
SALES_TOKEN=$(token sales@hotel.test) MGR=$(token manager@hotel.test)
for _ in $(seq 30); do [[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$SALES/v1/me") == 200 ]] && break; sleep 1; done
sales() { SERVER=$SALES TENANT=hotel-a AUTHORITY=$1 submit "${@:2}"; }
sales crm-server "$SALES_TOKEN" s-a crm.account.create crm.account ACME '{"name":"Acme Corp","kind":"company"}' | jq -e .record >/dev/null || fail "account"
sales crm-server "$SALES_TOKEN" s-o crm.opportunity.open crm.opportunity OPP-1 '{"account":"ACME","title":"Board offsite"}' | jq -e .record >/dev/null || fail "opportunity"
sales crm-server "$SALES_TOKEN" s-b crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"suite","checkIn":"2026-10-01","checkOut":"2026-10-03","guest":"Acme board"}' | jq -e .record >/dev/null || fail "booking through the lodging protocol"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/protocols" | jq -r '.[] | select(.id == "lodging.booking/1") | "\(.bound) \(.consumers)"') == 'hotel ["crm"]' ]] || fail "protocol binding"
sales platform "$MGR" s-r platform.member.revoke platform.member sales-1 '{"app":"hotel"}' | jq -e .record >/dev/null || fail "revoke"
catalog=$(curl -s -H "Authorization: Bearer $SALES_TOKEN" "$SALES/v1/actions" | jq -c '[.[].schema | select(startswith("crm.") or startswith("hotel."))]')
[[ $catalog == '["crm.account.create","crm.account.edit","crm.account.archive","crm.opportunity.open","crm.opportunity.close"]' ]] || fail "catalog after revocation: $catalog"
[[ $(sales crm-server "$SALES_TOKEN" s-b2 crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"standard","checkIn":"2026-10-05","checkOut":"2026-10-06","guest":"x"}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "revoked member booked"
# Outbound effects (ADR-0014): the administrator subscribes a webhook endpoint to
# the protocol's cancellation; the cancellation below reaches the sink signed, once.
SERVER=$SALES TENANT=hotel-a AUTHORITY=platform submit "$MGR" w-1 platform.endpoint.add platform.endpoint sink \
  '{"url":"http://webhook-sink:8080/hook","secret":"sink","events":["lodging.booking/1#canceled"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "add endpoint"
sales hotel-server "$MGR" s-c hotel.reservation.cancel hotel.reservation OPP-1-B1 '{}' | jq -e .record >/dev/null || fail "hotel cancel"
note=$(curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/timeline" | jq -r '.[] | select(.entity == "crm.opportunity/OPP-1") | "\(.by): \(.text)"')
[[ $note == "app:hotel: Booking canceled (hotel.reservation/OPP-1-B1, by manager-1)" ]] || fail "timeline: $note"
for _ in $(seq 20); do [[ $(curl -s "$SINK/received" | jq '.kept | length') == 1 ]] && break; sleep 0.5; done
[[ $(curl -s "$SINK/received" | jq -r '.kept[] | .type + " " + .data.entity') == "lodging.booking/1#canceled hotel.reservation/OPP-1-B1" ]] || fail "webhook: $(curl -s "$SINK/received")"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/effects" | jq -r '.[0].state') == delivered ]] || fail "effect state"
[[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $SALES_TOKEN" "$SALES/v1/records/hotel.reservation") == 403 ]] || fail "hotel read without a hotel role"
mcp() { curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$SALES/mcp" -d "$2"; }
mcp "$MGR" '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | jq -e '.result.capabilities.tools' >/dev/null || fail "mcp initialize"
tools=$(mcp "$SALES_TOKEN" '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | jq -c '[.result.tools[].name]')
[[ $tools == *crm_opportunity_open* && $tools != *hotel_reservation_create* ]] || fail "mcp tools follow grants: $tools"
mcp "$SALES_TOKEN" '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"crm_opportunity_open","arguments":{"target":"OPP-2","account":"ACME","title":"Spring retreat","idempotencyKey":"mcp-1"}}}' | jq -e '.result.isError == false' >/dev/null || fail "mcp call"
# The application model (ADR-0016): one read contract for every entity type,
# scoped per member, with the record's history from the journal.
records() { curl -s -H "Authorization: Bearer $1" "$SALES/v1/records/$2"; }
[[ $(records "$SALES_TOKEN" 'crm.opportunity?sort=-id&limit=1' | jq -c '[.total, .records[0].id, .records[0].owner]') == '[2,"OPP-2","sales-1"]' ]] || fail "records: $(records "$SALES_TOKEN" 'crm.opportunity')"
[[ $(records "$SALES_TOKEN" 'crm.opportunity?domain=%5B%5B%22title%22,%22like%22,%22board%22%5D%5D' | jq -r '.records[].id') == OPP-1 ]] || fail "records domain"
[[ $(records "$MGR" 'crm.opportunity/OPP-1' | jq -c '[.record.booked, [.history[].schema]]') == '[1,["crm.opportunity.book","crm.opportunity.open"]]' ]] || fail "record history: $(records "$MGR" 'crm.opportunity/OPP-1')"
[[ $(records "$MGR" 'crm.account/ACME' | jq -r '.related[0].total') == 2 ]] || fail "related records"
echo "ok   application model: generic reads with a domain, the owner's scope, a record's history and its related records"
# Lifecycles, approvals and tasks (ADR-0017): a leave request waits for the
# manager found in the organisation, lands in their inbox, and is approved by
# the approval; the requester is told.
sales hr "$SALES_TOKEN" h-1 hr.leave.create hr.leave LV-1 '{"kind":"vacation","from":"2026-11-02","until":"2026-11-04"}' | jq -e .record >/dev/null || fail "draft leave"
[[ $(sales hr "$SALES_TOKEN" h-2 hr.leave.submit hr.leave LV-1 '{}' | jq -r .record.submission.schema.name) == work.approval.request ]] || fail "leave held for approval"
task=$(curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/inbox" | jq -r '.[0].ref')
[[ $task == work.approval/hr.h-2 ]] || fail "manager's inbox: $(curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/inbox")"
[[ $(sales work "$SALES_TOKEN" h-3 work.approval.approve work.approval hr.h-2 '{}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "the requester approved"
sales work "$MGR" h-4 work.approval.approve work.approval hr.h-2 '{}' | jq -e .record >/dev/null || fail "approve"
[[ $(records "$SALES_TOKEN" 'hr.leave/LV-1' | jq -r '.record.state') == approved ]] || fail "leave after approval: $(records "$SALES_TOKEN" 'hr.leave/LV-1')"
[[ $(curl -s -H "Authorization: Bearer $SALES_TOKEN" "$SALES/v1/requests" | jq -r '.[0].state') == approved ]] || fail "request state"
echo "ok   approvals: a leave request held, found in the manager's inbox through the organisation, approved, and applied by the approval"
# Two lodging providers (#99): the administrator sends new stays to serviced
# apartments; the hotel's stay stays on the opportunity, and the restart keeps the choice.
SERVER=$SALES TENANT=hotel-a AUTHORITY=platform submit "$MGR" p-1 platform.protocol.bind platform.protocol lodging.booking/1 '{"provider":"memstay"}' | jq -e .record >/dev/null || fail "choose provider"
sales crm-server "$MGR" s-b3 crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"loft","checkIn":"2026-10-05","checkOut":"2026-10-06","guest":"x"}' | jq -e .record >/dev/null || fail "book at the chosen provider"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$SALES/v1/customers" | jq -c '[.[].opportunities[] | select(.id == "OPP-1") | .stays[].roomType]') == '["suite","loft"]' ]] || fail "stays across providers"
echo "ok   sales solution: a stay through the lodging protocol; the administrator chooses the provider and stays at both remain; revocation on the next request; the cancellation on the opportunity's timeline; an MCP client acts with a member's grants; the cancellation reached a webhook endpoint signed, once"

before=$(state)
[[ $(jq -s '.[1].total' <<<"$before") == 4 && $(jq -s '.[2] | length' <<<"$before") -gt 0 ]] || fail "rehearsal data missing"

compose restart mes-server sales-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(code "$SUP") == 200 && $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$SALES/v1/me") == 200 ]] && break; sleep 1; done
[[ $(state) == "$before" ]] || fail "state after restart differs"
sleep 2; [[ $(curl -s "$SINK/received" | jq .calls) == 1 ]] || fail "a delivered webhook was sent again after the restart"
echo "ok   restart: mes $(compose logs mes-server | grep -o 'replayed [0-9]* entries' | tail -1), sales $(compose logs sales-server | grep -o 'replayed [0-9]* entries' | tail -1); same state, revocation kept"

compose exec -T postgres pg_dump -U platform -d platform -Fc >"$backup/platform.dump"
submit "$OP1" c-1 mes.sfc.complete mes.sfc WO-1-001 '{}' 1 | jq -e .record >/dev/null || fail complete
after=$(state)
[[ $after != "$before" ]] || fail "completion changed nothing"

# Disaster: the database volume is lost. Restore the backup into a new one.
compose stop mes-server sales-server >/dev/null 2>&1
compose rm -sf postgres >/dev/null 2>&1
docker volume rm platform-rehearsal_pgdata >/dev/null
compose up -d --wait postgres >/dev/null 2>&1
compose exec -T postgres pg_restore -U platform -d platform --no-owner <"$backup/platform.dump"
compose start mes-server sales-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(code "$SUP") == 200 && $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$SALES/v1/me") == 200 ]] && break; sleep 1; done
[[ $(state) == "$before" ]] || fail "restored state is not the backup's"
echo "ok   restore: new volume, state as of the backup"

# What happened after the backup is lost on the server, not at the edge: the
# operator's outbox still holds the completion and resends it with its key.
submit "$OP1" c-1 mes.sfc.complete mes.sfc WO-1-001 '{}' 1 | jq -e .record >/dev/null || fail resend
now=$(state); [[ $now == "$after" ]] || { diff <(jq . <<<"$after") <(jq . <<<"$now") >&2; fail "resent completion did not restore the later state"; }
echo "ok   the edge outbox resends what the backup missed; state matches again"

compose exec -T postgres createdb -U platform journal_test
(cd ../../capabilities/server && PLATFORM_TEST_DATABASE=postgres://platform:platform-local-only@localhost:$PG_PORT/journal_test \
  go test -count=1 -run TestJournal . >/dev/null) || fail "journal test"
echo "ok   journal numbering refuses a second writer (capabilities/server TestJournal)"
