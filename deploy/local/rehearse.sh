#!/usr/bin/env bash
# Operations rehearsal for the manufacturing and hospitality solutions (docs/WorkQueue.md #87,
# Platform.md §7): principals from Rauthy, state that survives a restart, and a
# PostgreSQL backup restored into a new volume. Runs a disposable compose
# project on its own ports and removes it afterwards. Needs docker, curl, jq.
set -euo pipefail
cd "$(dirname "$0")"
export PG_PORT=55433 IDP_PORT=58480 MANUFACTURING_PORT=58490 HOSPITALITY_PORT=58495 SINK_PORT=58497
compose() { docker compose -p platform-rehearsal -f compose.yaml "$@"; }
IDP=http://localhost:$IDP_PORT/auth/v1 MANUFACTURING=http://localhost:$MANUFACTURING_PORT HOSPITALITY=http://localhost:$HOSPITALITY_PORT SINK=http://localhost:$SINK_PORT
backup=$(mktemp -d)
trap 'compose down -v --remove-orphans >/dev/null 2>&1; rm -rf "$backup"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
wait_for() { for _ in $(seq 60); do curl -sf -o /dev/null "$1" && return; sleep 1; done; fail "$1 never answered"; }

compose down -v --remove-orphans >/dev/null 2>&1 || true
pnpm --dir ../../web/apps/workspace build >/dev/null # served by both hosts (ADR-0018)
compose up -d --build --quiet-pull >"$backup/up.log" 2>&1 || { tail -n 30 "$backup/up.log" >&2; fail "compose up"; }
wait_for "$IDP/.well-known/openid-configuration"
[[ $(curl -s "$IDP/.well-known/openid-configuration" | jq -r .issuer) == "http://localhost:$IDP_PORT/auth/v1/" ]] || fail "issuer"

token() { # user → access token (password grant on the operations client)
  curl -sf "$IDP/oidc/token" -d grant_type=password -d client_id=platform-cli \
    -d client_secret=cliLocalOnly0000000000000000000000000000000000000000000000000000 \
    -d username="$1" -d password=Plant-Local-1 | jq -r .access_token
}
SUP=$(token sup@plant.test) OP1=$(token op1@plant.test) OP2=$(token op2@plant.test)
code() { curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $1" "$MANUFACTURING/v1/me"; }
for _ in $(seq 30); do [[ $(code "$SUP") == 200 ]] && break; sleep 1; done
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/me" | jq -r .principalId) == sup-1 ]] || fail "OIDC principal"
[[ $(code supervisor) == 401 ]] || fail "demo token accepted in production mode"
# One workspace per host (ADR-0018): the page, how to sign in, and the apps a member may open.
curl -s "$MANUFACTURING/" | grep -q "<title>Workspace</title>" || fail "workspace page"
[[ $(curl -s "$HOSPITALITY/v1/sign-in" | jq -cS .) == "{\"client\":\"platform-web\",\"issuer\":\"$IDP/\"}" ]] || fail "sign-in: $(curl -s "$HOSPITALITY/v1/sign-in")"
[[ $(curl -s -H "Authorization: Bearer $OP1" "$MANUFACTURING/v1/me" | jq -c '[.apps[].id]') == '["ai","mes"]' ]] || fail "apps of op1: $(curl -s -H "Authorization: Bearer $OP1" "$MANUFACTURING/v1/me" | jq -c '[.apps[].id]')"
echo "ok   workspace: served by the host; one client signs in for every app; a member sees the apps they hold a role in"
IFS=. read -r head claims sig <<<"$OP2"
while (( ${#claims} % 4 )); do claims+="="; done
forged=$(printf %s "$claims" | tr _- /+ | base64 -d | jq -c '.email = "sup@plant.test"' | base64 | tr +/ -_ | tr -d '=\n')
[[ -n $forged ]] || fail "could not forge"
[[ $(code "$head.$forged.$sig") == 401 ]] || fail "forged claims accepted"
echo "ok   principals come from Rauthy (demo tokens and forged claims refused)"

submit() { # token key schema target-type target-id payload [expected-revision]
  local body who
  local server=${SERVER:-$MANUFACTURING}
  who=$(curl -s -H "Authorization: Bearer $1" "$server/v1/me" | jq -r .principalId)
  body=$(jq -n --arg w "$who" --arg k "$2" --arg s "$3" --arg tt "$4" --arg ti "$5" --arg p "$(printf %s "$6" | base64)" --arg r "${7:-}" \
    --arg e "${EVIDENCE:-}" --arg tenant "${TENANT:-plant-sz}" --arg authority "${AUTHORITY:-mes}" \
    '{tenantId:$tenant, principalId:$w, authority:$authority, idempotencyKey:$k,
      schema:{name:$s, version:1}, target:{type:$tt, id:$ti}, payload:$p}
     + (if $r == "" then {} else {expectedRevision:($r|tonumber)} end)
     + (if $e == "" then {} else {evidenceFactIds:[$e]} end)')
  curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$server/v1/submissions" -d "$body"
}
state() { { for path in "records/mes.order?limit=500" "records/mes.sfc?limit=500" downtime planned-orders notifications "records/erp.production?limit=500" trial-balance; do curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/$path"; done
  curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/connectors" | jq -c '[.[] | {id, disabled}]'
  curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/ai-usage" | jq -c '.totals'
  for path in customers records/pms.reservation records/pms.room-type members links timeline records/crm.account records/crm.opportunity records/crm.opportunity/OPP-1 records/hcm.leave records/work.approval; do curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/$path"; done
  curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/protocols" | jq -c '[.[] | {id, bound}]'; } |
  jq -cS 'walk(if type == "object" then del(.changed, .created) else . end)'; } # when the host accepted a record is not state: a resent decision is accepted again

# Inputs of every kind the journal keeps: decisions and a push batch.
(cd ../../apps/mes/server && MES_GATEWAY_SECRET=gatewayLocalOnly000000000000000000000000000000000000000000000000 \
  go run ./cmd/gateway-sim -server "$MANUFACTURING" -oidc-token "$IDP/oidc/token" -batches 4 -every 200ms >/dev/null)
submit "$SUP" r-1 mes.order.release mes.order WO-1 '{"product":"P-100","quantity":2,"sfcs":2}' | jq -e .record >/dev/null || fail release

# Platform operations (ADR-0013): the gateway's downtime reached the supervisor
# of the line, not its operators; Settings disables the gateway's connector, and
# that is a decision the restart below keeps.
curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/notifications" | jq -e 'any(.[]; .title | startswith("Downtime on"))' >/dev/null || fail "supervisor not notified"
[[ $(curl -s -H "Authorization: Bearer $OP1" "$MANUFACTURING/v1/notifications" | jq length) == 0 ]] || fail "operator notified"
AUTHORITY=platform submit "$SUP" o-1 platform.connector.disable platform.connector gateway-l1 '{}' | jq -e .record >/dev/null || fail "disable connector"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/connectors" | jq -r '.[] | select(.id == "gateway-l1") | .health') == disabled ]] || fail "connector health"
# The ERP (ADR-0024): the plant host seeded its books when its journal was
# empty. The ERP releases production orders; the plant makes WO-2 against MO-1,
# its confirmation flow confirms it through production.orders/1, and the ERP
# posts the cost with a number from the production journal.
erp() { AUTHORITY=erp submit "$SUP" "$@" | jq -e .record >/dev/null || fail "ERP: $*"; }
erp e-1 erp.production.create erp.production MO-1 '{"product":"P-100","quantity":12}'
erp e-2 erp.production.release erp.production MO-1 '{}'
erp e-3 erp.production.create erp.production MO-2 '{"product":"P-200","quantity":8}'
erp e-4 erp.production.release erp.production MO-2 '{}'
erp e-5 erp.production.create erp.production MO-3 '{"product":"P-100","quantity":1}'
erp e-6 erp.production.release erp.production MO-3 '{}'
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/planned-orders" | jq -r '[.[].number] | join(" ")') == MO/????/00001" "MO/????/00002" "MO/????/00003 ]] || fail "planned orders: $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/planned-orders")"
submit "$SUP" r-2 mes.order.release mes.order WO-2 '{"product":"P-100","quantity":12,"sfcs":1,"planned":"MO-1"}' | jq -e .record >/dev/null || fail "release WO-2"
rev=0
for resource in FURNACE-1 CNC-11 CMM-1; do
  submit "$OP1" "w2-s$rev" mes.sfc.start mes.sfc WO-2-001 "{\"resource\":\"$resource\"}" $rev | jq -e .record >/dev/null || fail "start WO-2 at $resource"
  submit "$OP1" "w2-c$rev" mes.sfc.complete mes.sfc WO-2-001 '{}' $((rev + 1)) | jq -e .record >/dev/null || fail "complete WO-2 at $resource"
  rev=$((rev + 2))
done
wo() { curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/mes.order/$1" | jq -r '.record | .erp + " " + (.confirmation // .erpDetail // "") + " " + (.planned // "")'; }
for _ in $(seq 20); do [[ $(wo WO-2) == confirmed* ]] && break; sleep 0.5; done
[[ $(wo WO-2) == "confirmed MJ/"????"/00001 MO-1" ]] || fail "WO-2 through the protocol: $(wo WO-2)"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/erp.production/MO-1" | jq -r '.record | .state + " " + .shopOrder + " " + (.yield | tostring)') == "confirmed WO-2 12" ]] || fail "the ERP's MO-1"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/trial-balance" | jq -r '[.[] | select(.account == "1405") | .balance][0]') == 14400 ]] || fail "finished goods in the books: $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/trial-balance")"
echo "ok   operations: downtime notified to the line's supervisor only; the gateway's connector disabled from Settings; the ERP's production order made by the plant, confirmed through the protocol and posted"
submit "$OP1" s-1 mes.sfc.start mes.sfc WO-1-001 '{"resource":"FURNACE-1"}' 0 | jq -e .record >/dev/null || fail start
[[ $(submit "$OP2" s-2 mes.sfc.start mes.sfc WO-1-002 '{"resource":"FURNACE-1"}' 0 | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "line policy"

# An AI agent is a client with a role and lines: its catalog holds only what it
# may call, and the server refuses the rest even when the adapter is bypassed.
agent() { (cd ../../apps/mes/server && MES_AGENT_CLIENT=mes-assistant \
  MES_AGENT_SECRET=assistantLocalOnly0000000000000000000000000000000000000000000000 \
  go run ./cmd/mes-agent -server "$MANUFACTURING" -oidc-token "$IDP/oidc/token" "$@"); }
[[ $(agent actions | jq -c '[.[].schema | select(startswith("work.") or startswith("agent.") | not)]') == '["platform.member.language","platform.notification.read","mes.downtime.reason","mes.order.reconfirm"]' ]] || fail "assistant catalog"
event=$(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/downtime" | jq -r 'first(.[] | select(.resource == "CNC-11")).id')
agent do mes.downtime.reason "$event" '{"reason":"Setup"}' | jq -e .record >/dev/null || fail "assistant reason"
! agent do mes.order.release WO-9 '{}' 2>/dev/null || fail "assistant acted outside its catalog"
AGENT=$(curl -sf "$IDP/oidc/token" -d grant_type=client_credentials -d client_id=mes-assistant \
  -d client_secret=assistantLocalOnly0000000000000000000000000000000000000000000000 | jq -r .access_token)
[[ $(submit "$AGENT" a-1 mes.order.release mes.order WO-9 '{"product":"P-100","quantity":1,"sfcs":1}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "server let the assistant release"
echo "ok   AI assistant: catalog of one plant action (and its own notifications), acted within line L1, refused outside it"

# An order released without a planned order is refused at once. The line's AI
# assistant resends it against MO-3, but it holds no role in the ERP, so the ERP
# refuses it too: an agent acts within its own grants in every app. The
# supervisor resends it and the ERP confirms it. Email (ADR-0014 D7): the
# plant's and the platform's notifications are mailed to members who sign in
# with an email address, through the sink's SMTP server.
AUTHORITY=platform submit "$SUP" o-3 platform.endpoint.add platform.endpoint mail \
  '{"kind":"email","url":"smtp://webhook-sink:2525","from":"plant@plant.test","notifications":["mes","platform"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "add email endpoint"
submit "$SUP" r-3 mes.order.release mes.order WO-3 '{"product":"P-100","quantity":1,"sfcs":1}' | jq -e .record >/dev/null || fail "release WO-3"
rev=0
for resource in FURNACE-1 CNC-11 CMM-1; do
  submit "$OP1" "w3-s$rev" mes.sfc.start mes.sfc WO-3-001 "{\"resource\":\"$resource\"}" $rev | jq -e .record >/dev/null || fail "start WO-3 at $resource"
  submit "$OP1" "w3-c$rev" mes.sfc.complete mes.sfc WO-3-001 '{}' $((rev + 1)) | jq -e .record >/dev/null || fail "complete WO-3 at $resource"
  rev=$((rev + 2))
done
for _ in $(seq 20); do [[ $(wo WO-3) == refused* ]] && break; sleep 0.5; done
[[ $(wo WO-3) == "refused no planned order to confirm against " ]] || fail "refusal of WO-3: $(wo WO-3)"
submit "$AGENT" a-2 mes.order.reconfirm mes.order WO-3 '{"planned":"MO-3"}' | jq -e .record >/dev/null || fail "assistant resend"
[[ $(wo WO-3) == "refused ERROR_CODE_POLICY_DENIED MO-3" ]] || fail "the ERP took the assistant's confirmation: $(wo WO-3)"
submit "$SUP" a-3 mes.order.reconfirm mes.order WO-3 '{}' | jq -e .record >/dev/null || fail "supervisor resend"
[[ $(wo WO-3) == "confirmed MJ/"????"/00002 MO-3" ]] || fail "WO-3 after the supervisor's resend: $(wo WO-3)"
mailed() { curl -s "$SINK/mail" | jq -r '[.[] | select(.to == "sup@plant.test") | .subject] | join("|")'; }
for _ in $(seq 20); do [[ $(mailed) == *"ERP refused the confirmation of WO-3"* ]] && break; sleep 0.5; done
[[ $(mailed) == *"ERP refused the confirmation of WO-3"* ]] || fail "mail: $(curl -s "$SINK/mail")"
echo "ok   ERP correction: a refused order resent by the AI assistant and refused by the ERP, where it holds no role, then resent by the supervisor and confirmed; the refusals mailed to the supervisor"

# AI providers (ADR-0015): the plant's administrator adds a local model server
# (the sink speaks the OpenAI wire) and opens a model to ai users; an operator
# calls it through the host, the AI assistant (no ai role) is refused, and the
# call's usage is journaled (compared again after the restart below).
AUTHORITY=ai submit "$SUP" ai-1 ai.provider.add ai.provider local '{"kind":"local","baseUrl":"http://webhook-sink:8080/v1"}' | jq -e .record >/dev/null || fail "add AI provider"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/ai/providers/local/models" | jq -c '[.[].id]') == '["echo","embed"]' ]] || fail "provider catalog"
AUTHORITY=ai submit "$SUP" ai-2 ai.model.enable ai.model local/echo '{"access":"users"}' | jq -e .record >/dev/null || fail "enable model"
chat() { curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$MANUFACTURING/v1/ai/chat" -d '{"model":"local/echo","messages":[{"role":"user","content":"line one is down"}]}'; }
[[ $(chat "$OP1" | jq -r .content) == "echo: line one is down" ]] || fail "model call: $(chat "$OP1")"
[[ $(chat "$AGENT" | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "the assistant called a model open to ai users only"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/ai-usage" | jq -c '[.totals[] | {member, model, calls, input, output}]') == '[{"member":"op-l1","model":"local/echo","calls":1,"input":4,"output":5}]' ]] || fail "AI usage: $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/ai-usage")"
echo "ok   AI providers: a local model server added and a model opened to ai users; an operator's call answered and metered, the assistant refused"

# Agents (ADR-0021): the confirmation flow's agent corrects a refused order. For
# WO-4 (P-200, released without a planned order) the plant refuses; its agent,
# on the local model, reads the planned orders and proposes MO-2; the
# supervisor approves in the inbox; the flow resends it and the ERP confirms.
AUTHORITY=platform submit "$SUP" ag-1 platform.setting.set platform.setting agent/model '{"value":"local/echo"}' | jq -e .record >/dev/null || fail "agents' model"
submit "$SUP" r-4 mes.order.release mes.order WO-4 '{"product":"P-200","quantity":8,"sfcs":1}' | jq -e .record >/dev/null || fail "release WO-4"
rev=0
for resource in CNC-21 ASM-1 TEST-1; do
  submit "$OP2" "w4-s$rev" mes.sfc.start mes.sfc WO-4-001 "{\"resource\":\"$resource\"}" $rev | jq -e .record >/dev/null || fail "start WO-4 at $resource"
  submit "$OP2" "w4-c$rev" mes.sfc.complete mes.sfc WO-4-001 '{}' $((rev + 1)) | jq -e .record >/dev/null || fail "complete WO-4 at $resource"
  rev=$((rev + 2))
done
proposal() { curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/inbox" | jq -r '.[] | select(.title | startswith("Resend WO-4")) | .title + "|" + .id'; }
for _ in $(seq 40); do [[ -n $(proposal) ]] && break; sleep 0.5; done
[[ $(proposal) == "Resend WO-4 to the ERP against MO-2?|"* ]] || fail "the agent's proposal: $(proposal) / $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/agent.run" | jq -c '[.records[] | {state, stopped, steps: [.steps[] | .tool + " " + .outcome[:60]]}]')"
AUTHORITY=work submit "$SUP" ag-2 work.task.complete work.task "$(proposal | cut -d'|' -f2)" '{"answer":"resend"}' >/dev/null
for _ in $(seq 30); do [[ $(wo WO-4) == confirmed* ]] && break; sleep 0.5; done
[[ $(wo WO-4) == "confirmed MJ/"????"/00003 MO-2" ]] || fail "WO-4 after the agent's correction: $(wo WO-4)"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/agent.run" | jq -r '[.records[] | select(.goal | contains("WO-4"))][0] | .agent + " " + .state + " " + ([.steps[].tool] | join(","))') == "mes.erp-fixer done read_planned_orders,finish" ]] || fail "the agent's run"
echo "ok   agents: a refused order corrected by the plant's agent on the local model, approved by the supervisor in the inbox, resent by the flow and confirmed"
# Agent-to-agent (ADR-0022 D7): the plant's planner asks the supplier's agent
# (the sink, over A2A 1.0) for a lead time through an effect; the answer comes
# back journaled and the run finishes with it.
AUTHORITY=platform submit "$SUP" a2a-1 platform.endpoint.add platform.endpoint supplier \
  '{"kind":"a2a","url":"http://webhook-sink:8080/a2a","effects":["mes/lead-time"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "supplier's agent endpoint"
AUTHORITY=agent submit "$SUP" a2a-2 agent.run.start agent.run PLAN-1 '{"agent":"mes.planner","goal":"What is the lead time of P-200?"}' | jq -e .record >/dev/null || fail "ask the planner"
plan() { curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/agent.run/PLAN-1" | jq -r '.record.state + " " + (.record.result // "")'; }
for _ in $(seq 40); do [[ $(plan) == done* ]] && break; sleep 0.5; done
[[ $(plan) == *'"leadTimeDays":12'* ]] || fail "the planner's answer: $(plan)"
echo "ok   agent-to-agent: the plant's planner asked the supplier's agent over A2A through an effect and answered with its lead time"

# The hospitality solution: the CRM books a stay through the lodging protocol and the
# PMS provides it (ADR-0011); the platform app revokes a role and the catalog
# follows on the next request; a hotel cancellation reaches the opportunity's
# timeline through the platform link; an MCP client acts with a member's grants.
SALES_TOKEN=$(token sales@hotel.test) MGR=$(token manager@hotel.test)
for _ in $(seq 30); do [[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/me") == 200 ]] && break; sleep 1; done
hosp() { SERVER=$HOSPITALITY TENANT=hotel-a AUTHORITY=$1 submit "${@:2}"; }
hosp crm "$SALES_TOKEN" s-a crm.account.create crm.account ACME '{"name":"Acme Corp","kind":"company"}' | jq -e .record >/dev/null || fail "account"
hosp crm "$SALES_TOKEN" s-o crm.opportunity.open crm.opportunity OPP-1 '{"account":"ACME","title":"Board offsite"}' | jq -e .record >/dev/null || fail "opportunity"
hosp crm "$SALES_TOKEN" s-b crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"suite","checkIn":"2026-10-01","checkOut":"2026-10-03","guest":"Acme board"}' | jq -e .record >/dev/null || fail "booking through the lodging protocol"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/protocols" | jq -r '.[] | select(.id == "lodging.booking/1") | "\(.bound) \(.consumers)"') == 'pms ["crm"]' ]] || fail "protocol binding"
hosp platform "$MGR" s-r platform.member.revoke platform.member sales-1 '{"app":"pms"}' | jq -e .record >/dev/null || fail "revoke"
catalog=$(curl -s -H "Authorization: Bearer $SALES_TOKEN" "$HOSPITALITY/v1/actions" | jq -c '[.[].schema | select(startswith("crm.") or startswith("pms."))]')
[[ $catalog == '["crm.account.create","crm.account.edit","crm.account.archive","crm.opportunity.open","crm.opportunity.close","crm.opportunity.plan"]' ]] || fail "catalog after revocation: $catalog"
[[ $(hosp crm "$SALES_TOKEN" s-b2 crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"standard","checkIn":"2026-10-05","checkOut":"2026-10-06","guest":"x"}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "revoked member booked"
# Outbound effects (ADR-0014): the administrator subscribes a webhook endpoint to
# the protocol's cancellation; the cancellation below reaches the sink signed, once.
SERVER=$HOSPITALITY TENANT=hotel-a AUTHORITY=platform submit "$MGR" w-1 platform.endpoint.add platform.endpoint sink \
  '{"url":"http://webhook-sink:8080/hook","secret":"sink","events":["lodging.booking/1#canceled"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "add endpoint"
hosp pms "$MGR" s-c pms.reservation.cancel pms.reservation OPP-1-B1 '{}' | jq -e .record >/dev/null || fail "PMS cancel"
note=$(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/timeline" | jq -r '.[] | select(.entity == "crm.opportunity/OPP-1") | "\(.by): \(.text)"')
[[ $note == "app:pms: Booking canceled (pms.reservation/OPP-1-B1, by manager-1)" ]] || fail "timeline: $note"
for _ in $(seq 20); do [[ $(curl -s "$SINK/received" | jq '.kept | length') == 1 ]] && break; sleep 0.5; done
[[ $(curl -s "$SINK/received" | jq -r '.kept[] | .type + " " + .data.entity') == "lodging.booking/1#canceled pms.reservation/OPP-1-B1" ]] || fail "webhook: $(curl -s "$SINK/received")"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/effects" | jq -r '.[0].state') == delivered ]] || fail "effect state"
[[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $SALES_TOKEN" "$HOSPITALITY/v1/records/pms.reservation") == 403 ]] || fail "PMS read without a PMS role"
mcp() { curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$HOSPITALITY/mcp" -d "$2"; }
mcp "$MGR" '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | jq -e '.result.capabilities.tools' >/dev/null || fail "mcp initialize"
tools=$(mcp "$SALES_TOKEN" '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | jq -c '[.result.tools[].name]')
[[ $tools == *crm_opportunity_open* && $tools != *pms_reservation_create* ]] || fail "mcp tools follow grants: $tools"
mcp "$SALES_TOKEN" '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"crm_opportunity_open","arguments":{"target":"OPP-2","account":"ACME","title":"Spring retreat","idempotencyKey":"mcp-1"}}}' | jq -e '.result.isError == false' >/dev/null || fail "mcp call"
# The application model (ADR-0016): one read contract for every entity type,
# scoped per member, with the record's history from the journal.
records() { curl -s -H "Authorization: Bearer $1" "$HOSPITALITY/v1/records/$2"; }
[[ $(records "$SALES_TOKEN" 'crm.opportunity?sort=-id&limit=1' | jq -c '[.total, .records[0].id, .records[0].owner]') == '[2,"OPP-2","sales-1"]' ]] || fail "records: $(records "$SALES_TOKEN" 'crm.opportunity')"
[[ $(records "$SALES_TOKEN" 'crm.opportunity?domain=%5B%5B%22title%22,%22like%22,%22board%22%5D%5D' | jq -r '.records[].id') == OPP-1 ]] || fail "records domain"
[[ $(records "$MGR" 'crm.opportunity/OPP-1' | jq -c '[.record.booked, [.history[].schema]]') == '[1,["crm.opportunity.book","crm.opportunity.open"]]' ]] || fail "record history: $(records "$MGR" 'crm.opportunity/OPP-1')"
[[ $(records "$MGR" 'crm.account/ACME' | jq -r '.related[0].total') == 2 ]] || fail "related records"
echo "ok   application model: generic reads with a domain, the owner's scope, a record's history and its related records"
# Analytics (ADR-0019): aggregates with the member's scope; the records projected
# into PostgreSQL, readable by the tenant's reader role and no other tenant's.
agg() { curl -s -H "Authorization: Bearer $1" "$HOSPITALITY/v1/aggregates/$2"; }
[[ $(agg "$MGR" 'crm.opportunity?group=owner&measure=count,sum:booked' | jq -c .rows) == '[{"count":2,"owner":"sales-1","sum:booked":1}]' ]] || fail "aggregate: $(agg "$MGR" 'crm.opportunity?group=owner&measure=count,sum:booked')"
sql() { compose exec -T postgres psql -U platform -d platform -qtAc "$1" 2>&1; }
for _ in $(seq 10); do [[ $(sql "set role tenant_hotel_a_reader; select count(*) from tenant_hotel_a.crm_opportunity") == 2 ]] && break; sleep 1; done
[[ $(sql "set role tenant_hotel_a_reader; select string_agg(id || ':' || booked, ',' order by id) from tenant_hotel_a.crm_opportunity") == "OPP-1:1,OPP-2:0" ]] || fail "projection: $(sql "select * from tenant_hotel_a.crm_opportunity")"
[[ $(sql "set role tenant_hotel_a_reader; select count(*) from tenant_hotel_a.crm_opportunity_changes where record_id = 'OPP-1'") == 2 ]] || fail "projected history"
[[ $(sql "set role tenant_hotel_a_reader; select count(*) from tenant_plant_sz.mes_sfc") == *"permission denied"* ]] || fail "a reader of another tenant"
echo "ok   analytics: an aggregate within the manager's scope; records and their history in PostgreSQL for the tenant's reader role only"
# Lifecycles, approvals and tasks (ADR-0017): a leave request waits for the
# manager found in the organisation, lands in their inbox, and is approved by
# the approval; the requester is told.
hosp hcm "$SALES_TOKEN" h-1 hcm.leave.create hcm.leave LV-1 '{"kind":"vacation","from":"2026-11-02","until":"2026-11-04"}' | jq -e .record >/dev/null || fail "draft leave"
[[ $(hosp hcm "$SALES_TOKEN" h-2 hcm.leave.submit hcm.leave LV-1 '{}' | jq -r .record.submission.schema.name) == work.approval.request ]] || fail "leave held for approval"
task=$(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/inbox" | jq -r '.[0].ref')
[[ $task == work.approval/hcm.h-2 ]] || fail "manager's inbox: $(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/inbox")"
[[ $(hosp work "$SALES_TOKEN" h-3 work.approval.approve work.approval hcm.h-2 '{}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "the requester approved"
hosp work "$MGR" h-4 work.approval.approve work.approval hcm.h-2 '{}' | jq -e .record >/dev/null || fail "approve"
[[ $(records "$SALES_TOKEN" 'hcm.leave/LV-1' | jq -r '.record.state') == approved ]] || fail "leave after approval: $(records "$SALES_TOKEN" 'hcm.leave/LV-1')"
[[ $(curl -s -H "Authorization: Bearer $SALES_TOKEN" "$HOSPITALITY/v1/requests" | jq -r '.[0].state') == approved ]] || fail "request state"
echo "ok   approvals: a leave request held, found in the manager's inbox through the organisation, approved, and applied by the approval"
# Two lodging providers (#99): the administrator sends new stays to serviced
# apartments; the hotel's stay stays on the opportunity, and the restart keeps the choice.
SERVER=$HOSPITALITY TENANT=hotel-a AUTHORITY=platform submit "$MGR" p-1 platform.protocol.bind platform.protocol lodging.booking/1 '{"provider":"memstay"}' | jq -e .record >/dev/null || fail "choose provider"
hosp crm "$MGR" s-b3 crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"loft","checkIn":"2026-10-05","checkOut":"2026-10-06","guest":"x"}' | jq -e .record >/dev/null || fail "book at the chosen provider"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/customers" | jq -c '[.[].opportunities[] | select(.id == "OPP-1") | .stays[].roomType]') == '["suite","loft"]' ]] || fail "stays across providers"
echo "ok   hospitality solution: a stay through the lodging protocol; the administrator chooses the provider and stays at both remain; revocation on the next request; the cancellation on the opportunity's timeline; an MCP client acts with a member's grants; the cancellation reached a webhook endpoint signed, once"

# Flows (ADR-0020): a won opportunity's planned rooms are booked through the
# lodging protocol by the group-stay flow, which then waits for its owner; the
# wait survives the restart below.
hosp crm "$SALES_TOKEN" f-1 crm.opportunity.open crm.opportunity OPP-9 '{"account":"ACME","title":"Group retreat"}' | jq -e .record >/dev/null || fail "open for the flow"
hosp crm "$SALES_TOKEN" f-2 crm.opportunity.plan crm.opportunity OPP-9 '{"rooms":2,"roomType":"standard","arrive":"2026-12-01","depart":"2026-12-03"}' | jq -e .record >/dev/null || fail "plan the group stay"
hosp crm "$SALES_TOKEN" f-3 crm.opportunity.close crm.opportunity OPP-9 '{"outcome":"won"}' | jq -e .record >/dev/null || fail "win for the flow"
flowstate() { curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/records/flow.instance/crm.group-stay:OPP-9" | jq -r '.record.state + " " + ([.record.trace[]?.what] | join(","))'; }
for _ in $(seq 20); do [[ $(flowstate) == waiting* ]] && break; sleep 0.5; done
[[ $(flowstate) == "waiting started,acted,chose,acted,chose,asked" ]] || fail "group-stay flow: $(flowstate)"
# Customer service (ADR-0021 D10 (2)): a ticket's triage agent, on the local model,
# triages and replies; the reply's mail is held because an agent wrote it, and
# reaches the mail gateway (the sink) once the manager approves it.
hosp ai "$MGR" hd-1 ai.provider.add ai.provider local '{"kind":"local","baseUrl":"http://webhook-sink:8080/v1"}' | jq -e .record >/dev/null || fail "hospitality AI provider"
hosp ai "$MGR" hd-2 ai.model.enable ai.model local/echo '{"access":"users"}' | jq -e .record >/dev/null || fail "hospitality model"
hosp platform "$MGR" hd-3 platform.setting.set platform.setting agent/model '{"value":"local/echo"}' | jq -e .record >/dev/null || fail "hospitality agents' model"
hosp platform "$MGR" hd-4 platform.endpoint.add platform.endpoint mail-gateway \
  '{"url":"http://webhook-sink:8080/hook","secret":"sink","effects":["csm/reply"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "mail gateway endpoint"
hosp ai "$MGR" hd-k1 ai.model.enable ai.model local/embed '{"access":"users"}' | jq -e .record >/dev/null || fail "embedding model"
hosp platform "$MGR" hd-k2 platform.setting.set platform.setting knowledge/embedding-model '{"value":"local/embed"}' | jq -e .record >/dev/null || fail "knowledge's model"
hosp knowledge "$MGR" hd-k3 knowledge.document.create knowledge.document RULES '{"title":"House rules","text":"# Wifi\n\nWifi keeps dropping? The password is on the key card, and the front desk resets it."}' | jq -e .record >/dev/null || fail "house rules"
kn() { curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/knowledge?q=wifi%20password" | jq -r '.[0].document'; }
[[ $(kn) == knowledge.document/RULES ]] || fail "knowledge search: $(kn)"
hosp csm "$MGR" hd-5 csm.ticket.open csm.ticket T-1 '{"subject":"Wifi keeps dropping","customer":"anna@acme.test","account":"ACME"}' | jq -e .record >/dev/null || fail "open ticket"
ticket() { records "$MGR" 'csm.ticket/T-1' | jq -r '.record.status + " " + .record.priority + " " + .record.replied'; }
for _ in $(seq 40); do [[ $(ticket) == answered* ]] && break; sleep 0.5; done
[[ $(ticket) == "answered normal agent:csm.triage" ]] || fail "ticket after triage: $(ticket)"
[[ $(records "$MGR" 'csm.ticket/T-1' | jq -r .record.reply) == *"House rules"* ]] || fail "the reply cites nothing: $(records "$MGR" 'csm.ticket/T-1' | jq -r .record.reply)"
[[ $(records "$MGR" 'agent.run?sort=-id' | jq -r '[.records[] | select(.goal | contains("T-1")) | .citations[0].document][0]') == knowledge.document/RULES ]] || fail "the run's citation"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/transcripts" | jq 'length > 0') == true ]] || fail "transcripts"
held=$(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/effects" | jq -r '.[] | select(.event == "csm/reply") | .state + " " + .id')
[[ ${held%% *} == held ]] || fail "the agent's reply was not held: $held"
hosp platform "$MGR" hd-6 platform.effect.approve platform.effect "${held#* }" '{}' | jq -e .record >/dev/null || fail "approve the reply"
for _ in $(seq 20); do [[ $(curl -s "$SINK/received" | jq '[.kept[] | select(.type == "csm/reply")] | length') == 1 ]] && break; sleep 0.5; done
[[ $(curl -s "$SINK/received" | jq -r '.kept[] | select(.type == "csm/reply") | .data.to') == anna@acme.test ]] || fail "reply mailed: $(curl -s "$SINK/received")"
# A client outside calls customer service's triage agent over A2A 1.0 once the
# manager publishes it: the card, then a task answered for the manager.
hosp platform "$MGR" hd-7 platform.setting.set platform.setting agent/published '{"value":"csm.triage"}' | jq -e .record >/dev/null || fail "publish the agent"
[[ $(curl -s "$HOSPITALITY/a2a/hotel-a/csm.triage/.well-known/agent-card.json" | jq -r '.supportedInterfaces[0].protocolBinding + " " + .securitySchemes.oidc.openIdConnectSecurityScheme.openIdConnectUrl') == "JSONRPC "*/.well-known/openid-configuration ]] || fail "agent card"
a2a=$(curl -s -H "Authorization: Bearer $MGR" -H 'A2A-Version: 1.0' -H 'Content-Type: application/json' "$HOSPITALITY/a2a/hotel-a/csm.triage" \
  -d '{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"ext-1","role":"ROLE_USER","parts":[{"text":"Triage and answer ticket T-1 from anna@acme.test (account ACME).\nSubject: Wifi keeps dropping"}]}}}')
[[ $(jq -r .result.task.status.state <<<"$a2a") == TASK_STATE_COMPLETED ]] || fail "A2A task: $a2a"
echo "ok   CSM: published over A2A and answered a client outside; the triage agent found the house rules (knowledge, embedded on the local model), triaged and replied citing them; its transcripts kept; its reply's mail held, approved by the manager, and sent to the mail gateway"
before=$(state) calls=$(curl -s "$SINK/received" | jq .calls)
[[ $(jq -s '.[1].total' <<<"$before") == 5 && $(jq -s '.[2] | length' <<<"$before") -gt 0 ]] || fail "rehearsal data missing"

compose restart manufacturing-server hospitality-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(code "$SUP") == 200 && $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/me") == 200 ]] && break; sleep 1; done
[[ $(state) == "$before" ]] || fail "state after restart differs"
# Snapshots (ADR-0019 D6): each host saved its tenant at shutdown and started from it.
logged() { for _ in $(seq 10); do compose logs "$1" | grep -q "$2" && return; sleep 1; done; return 1; }
for host in manufacturing-server hospitality-server; do
  logged $host "saved a snapshot of" || fail "$host saved no snapshot at shutdown"
  logged $host "from the snapshot at" || fail "$host did not start from its snapshot"
done
task=$(curl -s -H "Authorization: Bearer $SALES_TOKEN" "$HOSPITALITY/v1/inbox" | jq -r '.[] | select(.title | contains("Group retreat")) | .id')
hosp work "$SALES_TOKEN" f-4 work.task.complete work.task "$task" '{"answer":"confirmed"}' | jq -e .record >/dev/null || fail "answer the flow's question after the restart"
for _ in $(seq 20); do [[ $(flowstate) == done* ]] && break; sleep 0.5; done
[[ $(flowstate) == done* ]] || fail "flow after the restart: $(flowstate)"
echo "ok   flows: a won opportunity's rooms booked by the group-stay flow through the lodging protocol; its question to the owner survived the restart and its answer ended it"
sleep 2; [[ $(curl -s "$SINK/received" | jq .calls) == "$calls" ]] || fail "a delivered webhook was sent again after the restart"
echo "ok   restart: each host saved a snapshot at shutdown and started from it (plant $(compose logs manufacturing-server | grep -o 'snapshot at [0-9]*, then replayed [0-9]* entries' | tail -1)); same state, revocation kept"

# The journal is what to back up: the projections are copies rebuilt at start-up (ADR-0019).
compose exec -T postgres pg_dump -U platform -d platform -Fc --exclude-schema='tenant_*' >"$backup/platform.dump"
submit "$OP1" c-1 mes.sfc.complete mes.sfc WO-1-001 '{}' 1 | jq -e .record >/dev/null || fail complete
after=$(state)
[[ $after != "$before" ]] || fail "completion changed nothing"

# Disaster: the database volume is lost. Restore the backup into a new one.
compose stop manufacturing-server hospitality-server >/dev/null 2>&1
compose rm -sf postgres >/dev/null 2>&1
docker volume rm platform-rehearsal_pgdata >/dev/null
compose up -d --wait postgres >/dev/null 2>&1
compose exec -T postgres pg_restore -U platform -d platform --no-owner <"$backup/platform.dump"
compose start manufacturing-server hospitality-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(code "$SUP") == 200 && $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/me") == 200 ]] && break; sleep 1; done
[[ $(state) == "$before" ]] || fail "restored state is not the backup's"
echo "ok   restore: new volume, state as of the backup"

# What happened after the backup is lost on the server, not at the edge: the
# operator's outbox still holds the completion and resends it with its key.
submit "$OP1" c-1 mes.sfc.complete mes.sfc WO-1-001 '{}' 1 | jq -e .record >/dev/null || fail resend
now=$(state); [[ $now == "$after" ]] || { diff <(jq . <<<"$after") <(jq . <<<"$now") >&2; fail "resent completion did not restore the later state"; }
echo "ok   the edge outbox resends what the backup missed; state matches again"

compose exec -T postgres createdb -U platform journal_test
(cd ../../capabilities/server && PLATFORM_TEST_DATABASE=postgres://platform:platform-local-only@localhost:$PG_PORT/journal_test \
  go test -count=1 -run TestJournal . 2>&1) >"$backup/journal-test.log" || { cat "$backup/journal-test.log" >&2; fail "journal test"; }
echo "ok   journal numbering refuses a second writer (capabilities/server TestJournal)"
