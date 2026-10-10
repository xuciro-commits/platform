#!/usr/bin/env bash
# Operations rehearsal for the manufacturing and hospitality solutions (docs/WorkQueue.md #87,
# Platform.md §7): principals from Rauthy, state that survives a restart, and a
# PostgreSQL backup restored into a new volume. Runs a disposable compose
# project on its own ports and removes it afterwards. Needs docker, curl, jq.
set -euo pipefail
cd "$(dirname "$0")"
export PLATFORM_PERSONAL_TOKEN_KEY="$(openssl rand -hex 32)"
export PG_PORT=55433 IDP_PORT=58480 MANUFACTURING_PORT=58490 HOSPITALITY_PORT=58495 SINK_PORT=58497 FILES_PORT=59000 FILES_CONSOLE_PORT=59001
# The rehearsal owns its volumes: compose.yaml names them after
# PLATFORM_DATA_NAMESPACE when it is set (deploy/local/.env, README "重新走查"),
# and this project ends with `down -v`. Inheriting the owner's namespace
# would mount, and then delete, the running review database. The namespace is
# therefore pinned here; whatever .env says is ignored for this project.
export PLATFORM_DATA_NAMESPACE=platform-rehearsal
compose() { docker compose -p platform-rehearsal -f compose.yaml "$@"; }
if docker volume ls -q 2>/dev/null | grep -qx 'platform-rehearsal_pgdata' && docker ps --format '{{.Names}}' 2>/dev/null | grep -q '^platform-rehearsal-'; then
  echo "FAIL: a platform-rehearsal project is already running; stop it first (docker compose -p platform-rehearsal down -v)" >&2
  exit 1
fi
IDP=http://localhost:$IDP_PORT/auth/v1 MANUFACTURING=http://localhost:$MANUFACTURING_PORT HOSPITALITY=http://localhost:$HOSPITALITY_PORT SINK=http://localhost:$SINK_PORT
backup=$(mktemp -d)
cleanup() {
  local result=$?
  trap - EXIT
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$backup"
  exit "$result"
}
trap cleanup EXIT
fail() {
  echo "FAIL: $*" >&2
  # Retain tenant-local recovery diagnostics before the disposable project is
  # removed. Do not dump requests, environment variables or credentials.
  compose logs --no-color manufacturing-server hospitality-server 2>/dev/null |
    grep -E 'quarantin|accepted work|accepted result|record batch' | tail -20 >&2 || true
  if [[ -n ${SUP:-} ]]; then
    curl -s --max-time 3 -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/health" |
      jq -c '{status, recoveryError, queues, failed}' >&2 || true
  fi
  exit 1
}
wait_for() { for _ in $(seq 60); do curl -sf -o /dev/null "$1" && return; sleep 1; done; fail "$1 never answered"; }

compose down -v --remove-orphans >/dev/null 2>&1 || true
pnpm --dir ../../web/apps/workspace build >/dev/null # served by both hosts (ADR-0018)
# Built apart, with plain progress: an image build whose output goes to a file can stall otherwise.
compose build --progress plain >"$backup/build.log" 2>&1 || { tail -n 30 "$backup/build.log" >&2; fail "compose build"; }
# Build cache unused for three days goes; the Go module and build caches in use stay (Dockerfiles).
docker buildx prune -f --filter until=72h >/dev/null 2>&1 || true
compose up -d --quiet-pull >"$backup/up.log" 2>&1 || { tail -n 30 "$backup/up.log" >&2; fail "compose up"; }
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
# Health (ADR-0027 D6): the process, and each tenant's work for its administrators.
[[ $(curl -s "$MANUFACTURING/healthz" | jq -r .status) == ok && $(curl -s "$HOSPITALITY/healthz" | jq -r .status) == ok ]] || fail "healthz"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/health" | jq -r '.status + " " + (.apps|tostring)') =~ ^(ok|degraded)\ [0-9]+$ ]] || fail "tenant health: $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/health")"
echo "ok   health: the processes are alive; the plant's administrator reads its tenant's health"

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
  for path in records/pms.reservation records/pms.room-type members links timeline records/crm.account records/crm.opportunity records/crm.opportunity/OPP-1 records/hcm.leave records/work.approval; do curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/$path"; done
  curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/protocols" | jq -c '[.[] | {id, bound}]'
  curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/ai-usage" | jq -c '.totals'
  workflow_state
  function_state; } |
  jq -cS 'walk(if type == "object" then del(.changed, .created) else . end)'; } # when the host accepted a record is not state: a resent decision is accepted again
same_state() {
  local expected=$1 actual
  for _ in $(seq 20); do
    actual=$(state)
    [[ $actual == "$expected" ]] && return 0
    sleep 1
  done
  printf '%s\n' "$expected" >"$backup/state-expected.jsonl"
  printf '%s\n' "$actual" >"$backup/state-actual.jsonl"
  python3 - "$backup/state-expected.jsonl" "$backup/state-actual.jsonl" <<'PY'
import json, sys
left = [json.loads(line) for line in open(sys.argv[1])]
right = [json.loads(line) for line in open(sys.argv[2])]
print(f"state sections: expected {len(left)}, observed {len(right)}", file=sys.stderr)
for i, (a, b) in enumerate(zip(left, right)):
    if a == b: continue
    paths = []
    def changed(x, y, path):
        if x == y or len(paths) >= 20: return
        if isinstance(x, dict) and isinstance(y, dict):
            for key in sorted(x.keys() | y.keys()):
                if key not in x or key not in y: paths.append(f"{path}.{key} presence")
                else: changed(x[key], y[key], f"{path}.{key}")
        elif isinstance(x, list) and isinstance(y, list):
            if len(x) != len(y): paths.append(f"{path} length {len(x)} -> {len(y)}")
            for index, (xx, yy) in enumerate(zip(x, y)):
                changed(xx, yy, f"{path}[{index}]")
        else:
            paths.append(path)
    changed(a, b, f"section[{i}]")
    print("changed state paths: " + ", ".join(paths), file=sys.stderr)
PY
  return 1
}
settled_state() {
  local previous current stable=0
  previous=$(state)
  for _ in $(seq 30); do
    sleep 1
    current=$(state)
    if [[ $current == "$previous" ]]; then
      stable=$((stable+1))
      if ((stable >= 3)); then printf '%s\n' "$current"; return 0; fi
    else
      stable=0
    fi
    previous=$current
  done
  fail "rehearsal state did not settle before its recovery baseline"
}

# Workflow probes add their private records and exact bindings to every state
# comparison below. Defined here; called after both OIDC builders are ready.
workflow_get() { curl -sf -H "Authorization: Bearer $1" "$SERVER/v1/$2"; }
workflow_post() { curl -sf -H "Authorization: Bearer $1" -H 'Content-Type: application/json' "$SERVER/v1/$2" -d "$3"; }
workflow_submit() {
  local result
  result=$(submit "$@")
  if ! jq -e .record <<<"$result" >/dev/null; then
    jq -c '{error}' <<<"$result" >&2 || true
    fail "workflow decision: $3"
  fi
}
workflow_state() {
  local SERVER actor suffix path
  for suffix in plant hotel; do
    if [[ $suffix == plant ]]; then SERVER=$MANUFACTURING; actor=$SUP; else SERVER=$HOSPITALITY; actor=$MGR; fi
    for path in records/build.object/WF-O records/build.process/WF-P records/build.testplan/WF-PLAN \
      records/build.rehearsal$suffix records/flow.instance/build.review$suffix:WF-OLD \
      records/flow.instance/build.review$suffix:WF-NEW \
      records/flow.instance/build.review$suffix:WF-JOINT releases/active; do
      workflow_get "$actor" "$path" || fail "workflow state $suffix $path"
    done
  done
}
function_state() {
  local SERVER actor suffix path tenant
  for suffix in plant hotel; do
    if [[ $suffix == plant ]]; then SERVER=$MANUFACTURING; actor=$SUP; tenant=plant-sz; else SERVER=$HOSPITALITY; actor=$MGR; tenant=hotel-a; fi
    for path in records/build.function/FN-F records/build.page/FN-P records/build.testplan/FN-PLAN records/build.function-call?limit=500 records/build.evaluation?limit=500 \
      records/build.code/CODE-DATA "capabilities/calls/compute/$tenant:build:operation:compute-$suffix:operation"; do
      workflow_get "$actor" "$path" || fail "function state $suffix $path"
    done
  done
}
workflow_wait() {
  local who=$1 id=$2 version=$3 release=$4 result
  for _ in $(seq 30); do
    result=$(workflow_get "$who" "records/flow.instance/$id" || true)
    if jq -e --arg r "$release" --argjson v "$version" \
      '.record | .state == "waiting" and .version == $v and .release == $r and (.dependencies | startswith("sha256-v1:"))' <<<"$result" >/dev/null 2>&1; then return; fi
    sleep 1
  done
  fail "workflow $id did not wait with its exact release"
}
workflow_activate() {
  local who=$1 n=$2 candidate
  candidate=$(workflow_post "$who" releases/preview '{"kind":"flow","id":"WF-P"}' | jq -er 'select(.diagnostic == null or .diagnostic == "") | .candidateId') || fail "workflow preview"
  workflow_post "$who" releases/candidates "{\"kind\":\"flow\",\"id\":\"WF-P\",\"candidateId\":\"$candidate\",\"key\":\"wf-save-$n\"}" | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "workflow save"
  workflow_post "$who" releases/active "{\"candidateId\":\"$candidate\",\"key\":\"wf-active-$n\"}" | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "workflow activate"
  printf %s "$candidate"
}
workflow_setup() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build builder=$3 operator=$4 builder_id=$5 operator_id=$6 suffix=$7
  local typ=build.rehearsal$suffix flow=build.review$suffix plan first second result
  # Grants belong to this disposable project, through the existing Console.
  AUTHORITY=platform workflow_submit "$builder" wf-builder platform.member.grant platform.member "$builder_id" '{"app":"build","role":"builder"}'
  AUTHORITY=platform workflow_submit "$builder" wf-user platform.member.grant platform.member "$operator_id" '{"app":"build","role":"user"}'
  workflow_submit "$builder" wf-object build.object.create build.object WF-O \
    "{\"name\":\"rehearsal$suffix\",\"title\":\"Workflow recovery sample\",\"fields\":[{\"name\":\"note\",\"title\":\"Note\",\"type\":\"text\"}],\"states\":[{\"name\":\"open\",\"title\":\"Open\"},{\"name\":\"done\",\"title\":\"Done\"},{\"name\":\"rejected\",\"title\":\"Rejected\"}],\"actions\":[{\"name\":\"close\",\"title\":\"Close\",\"from\":[\"open\"],\"to\":\"done\"},{\"name\":\"reject\",\"title\":\"Reject\",\"from\":[\"open\"],\"to\":\"rejected\"}]}"
  workflow_submit "$builder" wf-object-publish build.object.publish build.object WF-O '{}'
  workflow_submit "$builder" wf-process build.process.create build.process WF-P \
    "{\"name\":\"review$suffix\",\"title\":\"Recovery review\",\"object\":\"$typ\",\"when\":\"open\",\"steps\":[{\"name\":\"review\",\"ask\":\"user\",\"answers\":[\"approve\"],\"branches\":{\"approve\":\"close\"}},{\"name\":\"close\",\"act\":\"close\"}]}"
  plan=$(jq -n --arg typ "$typ" --arg flow "$flow" --arg who "$operator_id" \
    '{process:"WF-P",title:"Fixed workflow recovery plan",as:$who,at:"2026-10-01T09:00:00Z",steps:[{type:$typ,id:"WF-TEST",action:($typ+".create"),payload:"{\"note\":\"isolated\"}",expect:"accepted",advanceSeconds:2},{type:$typ,id:"WF-TEST",flow:$flow,step:"review",answer:"approve",payload:"{}",expect:"accepted",advanceSeconds:2}]}')
  workflow_submit "$builder" wf-plan build.testplan.create build.testplan WF-PLAN "$plan"
  workflow_post "$builder" simulate/candidate "$(jq '.processId=.process | del(.process,.title) | .steps |= map(.payload |= (if . == null then {} else fromjson end))' <<<"$plan")" | \
    jq -e '.passed and .recovered and (.steps[0].flows[0].dependencies | startswith("sha256-v1:"))' >/dev/null || fail "isolated workflow candidate"
  [[ $(workflow_get "$builder" "records/$typ" | jq .total) == 0 ]] || fail "workflow sample reached formal rows"
  workflow_submit "$builder" wf-publish-1 build.process.publish build.process WF-P '{}'
  first=$(workflow_activate "$builder" 1)
  workflow_submit "$operator" wf-old "$typ.create" "$typ" WF-OLD '{"note":"old native path"}'
  workflow_wait "$builder" "$flow:WF-OLD" 1 "$first"
  workflow_submit "$builder" wf-edit build.process.edit build.process WF-P \
    '{"steps":[{"name":"review","ask":"user","answers":["approve"],"branches":{"approve":"reject"}},{"name":"reject","act":"reject"}]}'
  workflow_submit "$builder" wf-publish-2 build.process.publish build.process WF-P '{}'
  second=$(workflow_activate "$builder" 2)
  [[ $first != "$second" ]] || fail "workflow branch change kept release ID"
  workflow_submit "$operator" wf-new "$typ.create" "$typ" WF-NEW '{"note":"new native path"}'
  workflow_wait "$builder" "$flow:WF-NEW" 2 "$second"
  result=$(submit "$builder" wf-archive-denied build.object.archive build.object WF-O '{}')
  jq -e '.error.code == "ERROR_CODE_CONFLICT"' <<<"$result" >/dev/null || fail "archived an installed workflow dependency"
  workflow_submit "$builder" wf-object-edit build.object.edit build.object WF-O '{"title":"Changed while waiting"}'
  result=$(submit "$builder" wf-object-denied build.object.publish build.object WF-O '{}')
  jq -e '.error.code == "ERROR_CODE_INVALID_ARGUMENT"' <<<"$result" >/dev/null || fail "changed live workflow dependency"
  workflow_submit "$builder" wf-object-reset build.object.edit build.object WF-O '{"title":"Workflow recovery sample"}'
  jq -n --arg first "$first" --arg second "$second" '{first:$first,second:$second}' >"$backup/workflow-$suffix.json"
}
function_wait() {
  local who=$1 id=$2 source=$3 release=$4 result
  for _ in $(seq 30); do
    result=$(workflow_get "$who" "records/build.function-call/$id" || true)
    if jq -e --arg source "$source" --arg release "$release" \
      '.record | .state == "ready" and .version == 1 and .source == $source and .release == $release and .model == "local/echo" and
        .metered == true and .tokensReported == true and .inputTokens > 0 and .outputTokens == 20 and
        .costReported == true and .costUsd == 0.01 and (.latencyMillis // 0) >= 0 and (.output | fromjson | .category == "routine")' \
      <<<"$result" >/dev/null 2>&1; then return; fi
    sleep 1
  done
  fail "function $id did not keep its source, version, release, measured usage and strict answer"
}
function_setup() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build builder=$3 operator=$4 suffix=$5 operator_id=$6
  local typ=build.rehearsal$suffix name=advice$suffix candidate definition page
  if [[ $suffix == plant ]]; then
    AUTHORITY=platform workflow_submit "$builder" fn-user platform.member.grant platform.member "$operator_id" '{"app":"build","role":"user"}'
  fi
  definition=$(jq -n --arg typ "$typ" --arg name "$name" '{name:$name,title:"Recovery advice",description:"A bounded suggestion for a person to review",object:$typ,fields:["note"],roles:["builder","user"],model:"local/echo",instructions:"Summarise only the provided record. Use category routine or review. Flag review when the record needs human attention; do not invent facts or propose executing actions.",maxInputBytes:4096,maxOutputBytes:1024,maxTokens:256,output:[{name:"summary",type:"string",required:true,description:"A short factual summary"},{name:"category",type:"string",required:true,description:"Routine or needs review",choices:["routine","review"]},{name:"review",type:"boolean",required:true,description:"Whether a person should review it"}]}')
  workflow_submit "$builder" fn-create build.function.create build.function FN-F "$definition"
  workflow_submit "$builder" fn-publish-1 build.function.publish build.function FN-F '{}'
  page=$(jq -n --arg typ "$typ" --arg name "$name" --arg suffix "$suffix" '{name:("advicepage"+$suffix),title:"Recovery advice",object:$typ,sections:[{widget:"table",fields:["note"]},{widget:"function",function:{name:$name,version:1}}]}')
  workflow_submit "$builder" fn-page build.page.create build.page FN-P "$page"
  workflow_submit "$builder" fn-page-publish build.page.publish build.page FN-P '{}'
  workflow_submit "$builder" fn-edit build.function.edit build.function FN-F '{"instructions":"Summarise only the provided record. This is the second retained version; do not execute actions."}'
  workflow_submit "$builder" fn-publish-2 build.function.publish build.function FN-F '{}'
  candidate=$(workflow_post "$builder" releases/preview '{"kind":"page","id":"FN-P"}' | jq -er 'select(.diagnostic == null or .diagnostic == "") | .candidateId') || fail "old function page preview"
  workflow_post "$builder" releases/candidates "{\"kind\":\"page\",\"id\":\"FN-P\",\"candidateId\":\"$candidate\",\"key\":\"fn-save\"}" | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "old function page save"
  local policy
  policy=$(jq -n --arg suffix "$suffix" '{title:("Recovery evaluation "+$suffix),function:"FN-F",model:"local/echo",at:"2026-09-29T00:00:00Z",steps:[{type:("build.rehearsal"+$suffix),id:"SAMPLE",action:("build.rehearsal"+$suffix+".create"),payload:"{\"note\":\"Synthetic\"}",expect:"accepted"}],evaluation:[{minQuality:1,maxCostUsd:0.05,maxLatencyMillis:300000,cases:[{name:"synthetic",input:{note:"Synthetic"},expected:{summary:"Record: {\"note\":\"Synthetic\"}",category:"routine",review:false}}]}]}')
  workflow_submit "$builder" fn-eval-plan build.testplan.create build.testplan FN-PLAN "$policy"
  local report
  report=$(workflow_post "$builder" releases/evaluations "{\"candidateId\":\"$candidate\",\"planId\":\"FN-PLAN\",\"key\":\"fn-eval\"}" | jq -er .id) || fail "start function evaluation"
  local report_state
  for _ in $(seq 30); do
    report_state=$(workflow_get "$builder" "records/build.evaluation/$report" | jq -r .record.state) || true
    [[ $report_state == passed ]] && break
    sleep 1
  done
  [[ $report_state == passed ]] || fail "measured function evaluation: $report_state"
  workflow_post "$builder" releases/active "{\"candidateId\":\"$candidate\",\"key\":\"fn-active\"}" | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "old function page activate"
  workflow_submit "$operator" fn-call-old build.function-call.start build.function-call FN-CALL-OLD "{\"name\":\"$name\",\"version\":1,\"source\":\"WF-OLD\"}"
  function_wait "$operator" FN-CALL-OLD "$typ/WF-OLD" "$candidate"
  jq -n --arg candidate "$candidate" --arg name "$name" '{candidate:$candidate,name:$name}' >"$backup/function-$suffix.json"
}
function_after_restart() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build operator=$3 suffix=$4
  local typ=build.rehearsal$suffix candidate joint name
  candidate=$(jq -r .candidate "$backup/function-$suffix.json")
  joint=$(jq -r .candidate "$backup/function-joint-$suffix.json")
  name=$(jq -r .name "$backup/function-$suffix.json")
  function_wait "$operator" FN-CALL-OLD "$typ/WF-OLD" "$candidate"
  workflow_submit "$operator" fn-call-new build.function-call.start build.function-call FN-CALL-NEW "{\"name\":\"$name\",\"version\":1,\"source\":\"WF-NEW\"}"
  function_wait "$operator" FN-CALL-NEW "$typ/WF-NEW" "$joint"
}
function_after_recovery() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build operator=$3 suffix=$4
  local typ=build.rehearsal$suffix candidate joint name
  candidate=$(jq -r .candidate "$backup/function-$suffix.json")
  joint=$(jq -r .candidate "$backup/function-joint-$suffix.json")
  name=$(jq -r .name "$backup/function-$suffix.json")
  function_wait "$operator" FN-CALL-OLD "$typ/WF-OLD" "$candidate"
  function_wait "$operator" FN-CALL-NEW "$typ/WF-NEW" "$joint"
  workflow_submit "$operator" fn-call-recovered build.function-call.start build.function-call FN-CALL-RECOVERED "{\"name\":\"$name\",\"version\":1,\"source\":\"WF-NEW\"}"
  function_wait "$operator" FN-CALL-RECOVERED "$typ/WF-NEW" "$joint"
}
function_joint_setup() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build builder=$3 suffix=$4
  local name=advice$suffix flow=build.review$suffix candidate result
  # The retained first function version now belongs to both operator paths.
  workflow_submit "$builder" fn-joint-flow-edit build.process.edit build.process WF-P \
    "{\"steps\":[{\"name\":\"infer\",\"function\":{\"name\":\"$name\",\"version\":1},\"next\":\"review\"},{\"name\":\"review\",\"ask\":\"user\",\"answers\":[\"approve\"],\"branches\":{\"approve\":\"reject\"}},{\"name\":\"reject\",\"act\":\"reject\"}]}"
  workflow_submit "$builder" fn-joint-flow-publish build.process.publish build.process WF-P '{}'
  result=$(workflow_post "$builder" releases/preview '{"kind":"object","id":"WF-O"}') || fail "joint function preview"
  candidate=$(jq -er 'select(.diagnostic == null or .diagnostic == "") | .candidateId' <<<"$result") || fail "joint function candidate"
  jq -e --arg name "$name" --arg flow "$flow" --arg page "advicepage$suffix" \
    '[.included[] | select(.app == "build" and ((.kind == "function" and .name == $name) or (.kind == "page" and .name == $page) or (.kind == "flow" and .name == $flow)))] | length == 3' \
    <<<"$result" >/dev/null || fail "joint candidate omitted function, page or flow"
  jq -n --arg candidate "$candidate" --arg flow "$flow" '{candidate:$candidate,flow:$flow}' >"$backup/function-joint-$suffix.json"
}
function_joint_api() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build operator=$3 suffix=$4
  local typ=build.rehearsal$suffix name=advice$suffix candidate
  candidate=$(jq -r .candidate "$backup/function-joint-$suffix.json")
  workflow_get "$operator" releases/active | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "browser did not activate the joint candidate"
  workflow_submit "$operator" fn-joint-source "$typ.create" "$typ" WF-JOINT '{"note":"Shared function release"}'
  workflow_submit "$operator" fn-joint-page build.function-call.start build.function-call FN-CALL-JOINT \
    "{\"name\":\"$name\",\"version\":1,\"source\":\"WF-JOINT\"}"
  function_wait "$operator" FN-CALL-JOINT "$typ/WF-JOINT" "$candidate"
}
function_joint_wait() {
  local SERVER=$1 TENANT=$2 AUTHORITY=build builder=$3 suffix=$4 candidate flow result
  local typ=build.rehearsal$suffix
  candidate=$(jq -r .candidate "$backup/function-joint-$suffix.json")
  flow=$(jq -r .flow "$backup/function-joint-$suffix.json")
  for _ in $(seq 30); do
    result=$(workflow_get "$builder" "records/build.function-call?limit=500") || true
    if jq -e --arg source "$typ/WF-JOINT" --arg release "$candidate" \
      '[.records[] | select(.source == $source and .release == $release and .state == "ready" and .version == 1 and .costReported == true)] | length == 2' \
      <<<"$result" >/dev/null 2>&1; then break; fi
    sleep 1
  done
  jq -e --arg source "$typ/WF-JOINT" --arg release "$candidate" \
    '[.records[] | select(.source == $source and .release == $release and .state == "ready" and .version == 1 and .costReported == true)] | length == 2' \
    <<<"$result" >/dev/null || fail "joint page and flow calls did not retain the same release"
  workflow_wait "$builder" "$flow:WF-JOINT" 3 "$candidate"
}
function_joint_finish() {
  local SERVER=$1 TENANT=$2 AUTHORITY=work builder=$3 operator=$4 suffix=$5 candidate flow task result
  local typ=build.rehearsal$suffix
  candidate=$(jq -r .candidate "$backup/function-joint-$suffix.json")
  flow=$(jq -r .flow "$backup/function-joint-$suffix.json")
  function_joint_wait "$SERVER" "$TENANT" "$builder" "$suffix"
  task=$(workflow_get "$builder" "records/flow.instance/$flow:WF-JOINT" | jq -er '.record.tokens[] | select(.waits == "ask") | .task') || fail "joint flow task missing"
  workflow_submit "$operator" fn-joint-answer work.task.complete work.task "$task" '{"answer":"approve"}'
  for _ in $(seq 30); do
    result=$(workflow_get "$builder" "records/$typ/WF-JOINT") || true
    [[ $(jq -r .record.state <<<"$result") == rejected ]] && break
    sleep 1
  done
  [[ $(jq -r .record.state <<<"$result") == rejected ]] || fail "joint flow did not finish after recovery"
  workflow_get "$builder" "records/flow.instance/$flow:WF-JOINT" |
    jq -e --arg r "$candidate" '.record | .state == "done" and .version == 3 and .release == $r' >/dev/null || fail "joint flow lost its release after recovery"
}
deployed_browser() {
  local phase=$1
  if ! PLATFORM_DEPLOY_PHASE="$phase" PLATFORM_DEPLOY_PASSWORD=Plant-Local-1 \
    pnpm --dir ../../web/e2e exec playwright test --config playwright.deploy.config.ts >"$backup/browser-$phase.log" 2>&1; then
    # Playwright traces are retained under web/e2e/test-results/deploy-* for diagnosis.
    # Do not print provider redirect URLs or browser session tokens in logs.
    fail "deployed browser function release ($phase); see the retained Playwright trace"
  fi
  echo "ok   deployed browser $phase: OIDC workspace, shared function page and native flow"
}

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
# Files (ADR-0028): bytes into RustFS, answered with their hash; a decision attaches them to a shop order; its readers download them.
up=$(curl -s -H "Authorization: Bearer $SUP" -H 'Content-Type: text/plain' --data-binary 'WO-1 inspection: all good' "$MANUFACTURING/v1/files?name=inspection.txt")
hash=$(jq -r .hash <<<"$up")
[[ ${#hash} == 64 ]] || fail "upload: $up"
AUTHORITY=files submit "$SUP" file-1 files.file.attach files.file PH-1 "{\"hash\":\"$hash\",\"name\":\"inspection.txt\",\"contentType\":\"text/plain\",\"size\":25,\"target\":\"mes.order/WO-1\"}" | jq -e .record >/dev/null || fail "attach the file"
download() { curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/files/PH-1"; }
[[ $(download) == "WO-1 inspection: all good" ]] || fail "download: $(download)"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/mes.order/WO-1" | jq -r '.files[0].name') == inspection.txt ]] || fail "the order's files"
echo "ok   files: uploaded to RustFS, attached to a shop order by a decision that names its hash, downloaded by the order's reader"
submit "$OP1" s-1 mes.sfc.start mes.sfc WO-1-001 '{"resource":"FURNACE-1"}' 0 | jq -e .record >/dev/null || fail start
[[ $(submit "$OP2" s-2 mes.sfc.start mes.sfc WO-1-002 '{"resource":"FURNACE-1"}' 0 | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "line policy"

# An AI agent is a client with a role and lines: its catalog holds only what it
# may call, and the server refuses the rest even when the adapter is bypassed.
agent() { (cd ../../apps/mes/server && MES_AGENT_CLIENT=mes-assistant \
  MES_AGENT_SECRET=assistantLocalOnly0000000000000000000000000000000000000000000000 \
  go run ./cmd/mes-agent -server "$MANUFACTURING" -oidc-token "$IDP/oidc/token" "$@"); }
catalog=$(agent actions | jq -c '[.[].schema | select(startswith("work.") or startswith("agent.") or startswith("files.") or IN("platform.link", "platform.unlink", "platform.note", "platform.comment.add", "platform.follow.add", "platform.follow.remove") | not)]')
[[ $catalog == '["platform.member.language","platform.notification.read","platform.operation.call","build.function-call.start","mes.downtime.reason","mes.order.reconfirm"]' ]] || fail "assistant catalog: $catalog"
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
[[ $(wo WO-3) == "refused agent-l1 holds no role in erp, so may not "*" MO-3" ]] || fail "the ERP took the assistant's confirmation, or refused it without saying why: $(wo WO-3)"
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
# Limits (ADR-0029 D1): the operator's own limit of 5 tokens a day is spent,
# so the next call is refused before it reaches the model. A streamed call
# answers as server-sent events (D2), from a server that does not stream too.
AUTHORITY=ai submit "$SUP" ai-3 ai.limit.set ai.limit op-l1 '{"dailyTokens":5}' | jq -e .record >/dev/null || fail "set AI limit"
[[ $(chat "$OP1" | jq -r .error.code) == QUOTA ]] || fail "a limit: $(chat "$OP1")"
streamed=$(curl -s -N -H "Authorization: Bearer $SUP" -H 'Content-Type: application/json' "$MANUFACTURING/v1/ai/chat" -d '{"model":"local/echo","stream":true,"messages":[{"role":"user","content":"hi"}]}')
[[ $streamed == *"event: delta"*"echo: hi"*"event: done"* ]] || fail "streamed call: $streamed"
echo "ok   AI providers: a local model server added and a model opened to ai users; an operator's call answered and metered, the assistant refused; past the operator's limit the call is refused; a call streams"

# ADR-0043 24b: the same bounded declaration and model effect path advises a
# real shop order. Advice never completes it or confirms production to ERP.
AUTHORITY=platform submit "$SUP" fn-model platform.setting.set platform.setting ai/app-model '{"value":"local/echo"}' | jq -e .record >/dev/null || fail "plant function model"
[[ $(submit "$OP1" fn-denied mes.order.advise mes.order WO-1 '{}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "operator called supervisor function"
submit "$SUP" fn-order mes.order.advise mes.order WO-1 '{}' | jq -e .record >/dev/null || fail "order advice"
for _ in $(seq 30); do [[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/mes.order/WO-1" | jq -r .record.adviceState) == ready ]] && break; sleep 0.5; done
curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/records/mes.order/WO-1" | jq -e '.record | .adviceState == "ready" and .status == "released" and .adviceCategory == "routine" and .adviceModel == "local/echo" and (.adviceDefinition | length == 64) and (.adviceSources | length == 3)' >/dev/null || fail "typed order result"

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
# Its chain shows the question it sent the supplier's agent (ADR-0029 D6).
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/chain/agent.run/PLAN-1" | jq -c '[.edges[] | .from + ">" + .to] | length > 0') == true ]] || fail "the planner's chain: $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/chain/agent.run/PLAN-1")"
[[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/chain/agent.run/PLAN-1" | jq -r '[.nodes[] | select(.kind == "effect")][0].detail') == supplier* ]] || fail "the supplier's task in the chain"
echo "ok   agent-to-agent: the plant's planner asked the supplier's agent over A2A through an effect and answered with its lead time; the question is in the run's chain"

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
[[ $catalog == '["crm.account.create","crm.account.edit","crm.account.archive","crm.opportunity.advise","crm.opportunity.open","crm.opportunity.close","crm.opportunity.plan","crm.opportunity.answer"]' ]] || fail "catalog after revocation: $catalog"
[[ $(hosp crm "$SALES_TOKEN" s-b2 crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"standard","checkIn":"2026-10-05","checkOut":"2026-10-06","guest":"x"}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "revoked member booked"
# Outbound effects (ADR-0014): the administrator subscribes a webhook endpoint to
# the protocol's cancellation; the cancellation below reaches the sink signed, once.
SERVER=$HOSPITALITY TENANT=hotel-a AUTHORITY=platform submit "$MGR" w-1 platform.endpoint.add platform.endpoint sink \
  '{"url":"http://webhook-sink:8080/hook","secret":"sink","events":["lodging.booking/1#canceled"],"allowPrivate":true}' | jq -e .record >/dev/null || fail "add endpoint"
hosp pms "$MGR" s-c pms.reservation.cancel pms.reservation OPP-1-B1 '{}' | jq -e .record >/dev/null || fail "PMS cancel"
note=$(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/records/crm.opportunity/OPP-1" | jq -r '.activity[] | "\(.by): \(.title) (\(.text))"')
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
[[ $(records "$MGR" 'crm.opportunity/OPP-1' | jq -c '[[.record.stays[].status], [.history[].schema]]') == '[["booked"],["crm.opportunity.answer","crm.opportunity.book","crm.opportunity.open"]]' ]] || fail "record history: $(records "$MGR" 'crm.opportunity/OPP-1')"
[[ $(records "$MGR" 'crm.account/ACME' | jq -r '.related[0].total') == 2 ]] || fail "related records"
echo "ok   application model: generic reads with a domain, the owner's scope, a record's history and its related records"
# Analytics (ADR-0019): aggregates with the member's scope; the records projected
# into PostgreSQL, readable by the tenant's reader role and no other tenant's.
agg() { curl -s -H "Authorization: Bearer $1" "$HOSPITALITY/v1/aggregates/$2"; }
[[ $(agg "$MGR" 'crm.opportunity?group=owner&measure=count,sum:rooms' | jq -c .rows) == '[{"count":2,"owner":"sales-1","sum:rooms":0}]' ]] || fail "aggregate: $(agg "$MGR" 'crm.opportunity?group=owner&measure=count,sum:rooms')"
sql() { compose exec -T postgres psql -U platform -d platform -qtAc "$1" 2>&1; }
for _ in $(seq 10); do [[ $(sql "set role tenant_hotel_a_reader; select count(*) from tenant_hotel_a.crm_opportunity") == 2 ]] && break; sleep 1; done
[[ $(sql "set role tenant_hotel_a_reader; select string_agg(id || ':' || stage, ',' order by id) from tenant_hotel_a.crm_opportunity") == "OPP-1:open,OPP-2:open" ]] || fail "projection: $(sql "select * from tenant_hotel_a.crm_opportunity")"
[[ $(sql "set role tenant_hotel_a_reader; select count(*) from tenant_hotel_a.crm_opportunity_changes where record_id = 'OPP-1'") == 3 ]] || fail "projected history"
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
approval=$(hosp work "$MGR" h-4 work.approval.approve work.approval hcm.h-2 '{}')
jq -e .record >/dev/null <<<"$approval" || fail "approve: $(jq -c '{error}' <<<"$approval")"
[[ $(records "$SALES_TOKEN" 'hcm.leave/LV-1' | jq -r '.record.state') == approved ]] || fail "leave after approval: $(records "$SALES_TOKEN" 'hcm.leave/LV-1')"
[[ $(curl -s -H "Authorization: Bearer $SALES_TOKEN" "$HOSPITALITY/v1/requests" | jq -r '.[0].state') == approved ]] || fail "request state"
echo "ok   approvals: a leave request held, found in the manager's inbox through the organisation, approved, and applied by the approval"
# Two lodging providers (#99): the administrator sends new stays to serviced
# apartments; the hotel's stay stays on the opportunity, and the restart keeps the choice.
SERVER=$HOSPITALITY TENANT=hotel-a AUTHORITY=platform submit "$MGR" p-1 platform.protocol.bind platform.protocol lodging.booking/1 '{"provider":"memstay"}' | jq -e .record >/dev/null || fail "choose provider"
hosp crm "$MGR" s-b3 crm.opportunity.book crm.opportunity OPP-1 '{"roomType":"loft","checkIn":"2026-10-05","checkOut":"2026-10-06","guest":"x"}' | jq -e .record >/dev/null || fail "book at the chosen provider"
[[ $(curl -s -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/records/crm.opportunity/OPP-1" | jq -c '[.linked[].records[].roomType]') == '["suite","loft"]' ]] || fail "stays across providers on the opportunity's page"
echo "ok   hospitality solution: a stay through the lodging protocol; the administrator chooses the provider and stays at both remain; revocation on the next request; the cancellation on the opportunity's timeline; an MCP client acts with a member's grants; the cancellation reached a webhook endpoint signed, once"

# Decisions across apps (ADR-0026): a group's rooms held at the provider until a
# cutoff, each answer on the opportunity; the hold survives the restart below.
hosp crm "$SALES_TOKEN" f-1 crm.opportunity.open crm.opportunity OPP-9 '{"account":"ACME","title":"Group retreat"}' | jq -e .record >/dev/null || fail "open for the block"
hosp crm "$SALES_TOKEN" f-2 crm.opportunity.plan crm.opportunity OPP-9 '{"rooms":2,"roomType":"standard","arrive":"2026-12-01","depart":"2026-12-03","cutoff":"2026-11-20"}' | jq -e .record >/dev/null || fail "plan the group block"
blockstate() { records "$MGR" 'crm.opportunity/OPP-9' | jq -r '.record.block + " " + ([.record.stays[].status] | join(","))'; }
[[ $(blockstate) == "held held,held" ]] || fail "group block: $(blockstate)"
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
hosp platform "$MGR" hd-3m platform.setting.set platform.setting ai/app-model '{"value":"local/echo"}' | jq -e .record >/dev/null || fail "the model apps ask"
hosp csm "$MGR" hd-5 csm.ticket.open csm.ticket T-1 '{"subject":"Wifi keeps dropping","customer":"anna@acme.test","account":"ACME"}' | jq -e .record >/dev/null || fail "open ticket"
ticket() { records "$MGR" 'csm.ticket/T-1' | jq -r '.record.status + " " + .record.priority + " " + .record.replied'; }
for _ in $(seq 40); do [[ $(ticket) == answered* ]] && break; sleep 0.5; done
[[ $(ticket) == "answered normal agent:csm.triage" ]] || fail "ticket after triage: $(ticket)"
# The helpdesk asked the tenant's model for apps for a line on the ticket, and took its answer (ADR-0029 D3).
for _ in $(seq 20); do [[ -n $(records "$MGR" 'csm.ticket/T-1' | jq -r '.record.summary // empty') ]] && break; sleep 0.5; done
[[ $(records "$MGR" 'csm.ticket/T-1' | jq -r .record.summary) == "echo: Wifi keeps dropping" ]] || fail "ticket summary: $(records "$MGR" 'csm.ticket/T-1' | jq -r .record.summary)"
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
echo "ok   CSM: summarised by the model for apps; published over A2A and answered a client outside; the triage agent found the house rules (knowledge, embedded on the local model), triaged and replied citing them; its transcripts kept; its reply's mail held, approved by the manager, and sent to the mail gateway"
hosp crm "$SALES_TOKEN" fn-opportunity crm.opportunity.advise crm.opportunity OPP-1 '{}' | jq -e .record >/dev/null || fail "opportunity advice"
[[ $(hosp crm "$SALES_TOKEN" fn-forged crm.opportunity.advice-answer crm.opportunity OPP-1 '{"outcome":"accepted","text":"{}"}' | jq -r .error.code) == ERROR_CODE_POLICY_DENIED ]] || fail "person forged a model reply"
for _ in $(seq 30); do [[ $(records "$MGR" 'crm.opportunity/OPP-1' | jq -r .record.adviceState) == ready ]] && break; sleep 0.5; done
records "$MGR" 'crm.opportunity/OPP-1' | jq -e '.record | .adviceState == "ready" and .stage == "open" and .adviceCategory == "routine" and .adviceModel == "local/echo" and (.adviceDefinition | length == 64) and (.adviceSources | length == 2)' >/dev/null || fail "typed opportunity result"
echo "ok   typed AI functions: CRM opportunity and MES order use the shared bounded path; structured advice, sources, definition/model bindings and usage persist; business states stay unchanged; unauthorized calls and forged replies refused"
workflow_setup "$MANUFACTURING" plant-sz "$SUP" "$OP1" sup-1 op-l1 plant
workflow_setup "$HOSPITALITY" hotel-a "$MGR" "$SALES_TOKEN" manager-1 sales-1 hotel
echo "ok   workflows: fixed isolated plans, exact releases, old/new native asks, incompatible dependency publication refused in both industries"
function_setup "$MANUFACTURING" plant-sz "$SUP" "$OP2" plant op-l2
function_setup "$HOSPITALITY" hotel-a "$MGR" "$SALES_TOKEN" hotel sales-1
echo "ok   builder AI functions: both industries activate a page pinned to retained version 1 after publishing version 2; operators keep strict results in separate call records"
function_joint_setup "$MANUFACTURING" plant-sz "$SUP" plant
function_joint_setup "$HOSPITALITY" hotel-a "$MGR" hotel
deployed_browser before
function_joint_api "$MANUFACTURING" plant-sz "$OP1" plant
function_joint_api "$HOSPITALITY" hotel-a "$SALES_TOKEN" hotel
function_joint_wait "$MANUFACTURING" plant-sz "$SUP" plant
function_joint_wait "$HOSPITALITY" hotel-a "$MGR" hotel
echo "ok   joint AI function release: both industries activate one evaluated candidate for page and native flow calls"
# Explicit v2 compute goes through the real compiler, worker, PostgreSQL
# accepted result and RustFS input/output artifacts. The comparisons below
# retain its exact ABI, call and staged result across restart/full replay.
(
  for suffix in plant hotel; do
    if [[ $suffix == plant ]]; then SERVER=$MANUFACTURING TENANT=plant-sz actor=$SUP language=go;
    else SERVER=$HOSPITALITY TENANT=hotel-a actor=$MGR language=tinygo; fi
    AUTHORITY=build
    definition=$(jq -n --arg language "$language" --arg name "data$suffix" \
      '{name:$name,title:"Data channel recovery",abi:"platform-wasip1-data/v2",language:$language,
        source:"package main\nimport \"strings\"\nfunc Run(input Input)(Output,error){return Output(strings.Repeat(string(input),70000)),nil}",
        input:{type:"string"},output:{type:"string"},roles:["builder"],
        limits:{timeoutMillis:30000,memoryPages:2048,maxInputBytes:4096,maxOutputBytes:4096,dataInputBytes:4194304,stagedOutputBytes:8388608}}')
    workflow_submit "$actor" code-create build.code.create build.code CODE-DATA "$definition"
    workflow_submit "$actor" code-compile build.code.compile build.code CODE-DATA '{}'
    for _ in $(seq 60); do
      code_state=$(workflow_get "$actor" records/build.code/CODE-DATA | jq -r .record.state)
      [[ $code_state == compiled ]] && break
      [[ $code_state == failed ]] && fail "data ABI isolated build failed"
      sleep 1
    done
    [[ $code_state == compiled ]] || fail "data ABI isolated build never completed"
    candidate=$(workflow_post "$actor" releases/preview '{"kind":"compute","id":"CODE-DATA"}' | jq -er 'select(.diagnostic == null or .diagnostic == "") | .candidateId') || fail "data ABI candidate"
    workflow_post "$actor" releases/candidates "{\"kind\":\"compute\",\"id\":\"CODE-DATA\",\"candidateId\":\"$candidate\",\"key\":\"code-save\"}" | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "data ABI candidate save"
    workflow_post "$actor" releases/active "{\"candidateId\":\"$candidate\",\"key\":\"code-activate\"}" | jq -e --arg id "$candidate" '.id == $id' >/dev/null || fail "data ABI activation"
    input=$(jq -n --arg name "data$suffix" --arg key "compute-$suffix" '{ref:{app:"build",kind:"compute",name:$name},version:1,key:$key,inputs:"<"}')
    call=$(workflow_post "$actor" capabilities/invoke "$input" | jq -er .call) || fail "data ABI invocation"
    retry=$(workflow_post "$actor" capabilities/invoke "$input" | jq -er .call) || fail "data ABI idempotent invocation"
    [[ $call == "$retry" ]] || fail "data ABI retry created another call"
    for _ in $(seq 60); do
      result=$(workflow_get "$actor" "capabilities/calls/compute/$call")
      if jq -e --arg call "$call" '.state == "completed" and .output.staged.call == $call and .output.staged.size > 49152' <<<"$result" >/dev/null; then break; fi
      [[ $(jq -r .state <<<"$result") == failed ]] && fail "data ABI execution failed"
      sleep 1
    done
    jq -e --arg call "$call" '.state == "completed" and .output.staged.call == $call and .output.staged.size > 49152' <<<"$result" >/dev/null || fail "data ABI result never completed"
  done
)
echo "ok   Go/TinyGo data ABI: real isolated compilation, call-owned RustFS input/output and PostgreSQL result; retries keep the original call"
before=$(settled_state) calls=$(curl -s "$SINK/received" | jq .calls)
[[ $(jq -s '.[1].total' <<<"$before") == 5 && $(jq -s '.[2] | length' <<<"$before") -gt 0 ]] || fail "rehearsal data missing"

compose restart manufacturing-server hospitality-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(code "$SUP") == 200 && $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/me") == 200 ]] && break; sleep 1; done
same_state "$before" || fail "state after restart differs"
# Snapshots (ADR-0019 D6): each host saved its tenant at shutdown and started from it.
logged() { for _ in $(seq 10); do compose logs "$1" | grep -q "$2" && return; sleep 1; done; return 1; }
for host in manufacturing-server hospitality-server; do
  logged $host "saved a snapshot of" || fail "$host saved no snapshot at shutdown"
  logged $host "from the snapshot at" || fail "$host did not start from its snapshot"
done
function_after_restart "$MANUFACTURING" plant-sz "$OP2" plant
function_after_restart "$HOSPITALITY" hotel-a "$SALES_TOKEN" hotel
function_joint_wait "$MANUFACTURING" plant-sz "$SUP" plant
function_joint_wait "$HOSPITALITY" hotel-a "$MGR" hotel
echo "ok   builder AI functions: saved answers survived PostgreSQL restart and new calls retained the activated page's older function version"
hosp crm "$SALES_TOKEN" f-3 crm.opportunity.close crm.opportunity OPP-9 '{"outcome":"won"}' | jq -e .record >/dev/null || fail "win the group after the restart"
[[ $(blockstate) == "confirmed booked,booked" ]] || fail "group block after winning: $(blockstate)"
echo "ok   decisions across apps: a group's rooms held at the provider until a cutoff survived the restart, and winning confirmed them, each answer on the opportunity"
before=$(settled_state) # what the backup below holds
sleep 2; [[ $(curl -s "$SINK/received" | jq .calls) == "$calls" ]] || fail "a delivered webhook was sent again after the restart"
echo "ok   restart: each host saved a snapshot at shutdown and started from it (plant $(compose logs manufacturing-server | grep -o 'snapshot at [0-9]*, then replayed [0-9]* entries' | tail -1)); same state, revocation kept"

# The journal is what to back up: the projections are copies rebuilt at start-up (ADR-0019).
accepted=$(compose exec -T postgres psql -U platform -d platform -Atc \
  "select count(distinct tenant) from journal where kind='accepted-result' and tenant in ('plant-sz','hotel-a')")
[[ $accepted == 2 ]] || fail "accepted-result entries absent from either industry journal"
echo "ok   accepted results: both industry journals kept generated record decisions before backup"
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
[[ $(download) == "WO-1 inspection: all good" ]] || fail "the file after the restore: $(download)"
echo "ok   restore: new volume, state as of the backup; files still in RustFS"

# What happened after the backup is lost on the server, not at the edge: the
# operator's outbox still holds the completion and resends it with its key.
submit "$OP1" c-1 mes.sfc.complete mes.sfc WO-1-001 '{}' 1 | jq -e .record >/dev/null || fail resend
now=$(state); [[ $now == "$after" ]] || { diff <(jq . <<<"$after") <(jq . <<<"$now") >&2; fail "resent completion did not restore the later state"; }
echo "ok   the edge outbox resends what the backup missed; state matches again"

compose exec -T postgres createdb -U platform journal_test
(cd ../../capabilities/server && PLATFORM_TEST_DATABASE=postgres://platform:platform-local-only@localhost:$PG_PORT/journal_test \
  go test -count=1 -run 'TestJournal|TestJournalAcceptedResult' . 2>&1) >"$backup/journal-test.log" || { cat "$backup/journal-test.log" >&2; fail "journal test"; }
echo "ok   journal numbering and accepted result retry/recovery (capabilities/server)"
for app in mes pms; do
  (cd "../../apps/$app/server" && PLATFORM_TEST_DATABASE=postgres://platform:platform-local-only@localhost:$PG_PORT/journal_test \
    go test -count=1 -run 'TestJournalAccepted' . 2>&1) >"$backup/$app-accepted-test.log" ||
    { cat "$backup/$app-accepted-test.log" >&2; fail "$app accepted input recovery"; }
  echo "ok   $app accepted input crash/restart against PostgreSQL"
done

# Tenant-local recovery without restarting its neighbor: a corrupt derived
# checkpoint isolates the plant on restart; retry rebuilds from the entire
# durable journal and replaces that checkpoint before the next restart.
[[ $(sql "select count(*) from snapshots where tenant='plant-sz'") -gt 0 ]] || fail "plant checkpoint absent for corruption drill"
# Stop before corrupting: graceful shutdown writes a fresh checkpoint, so
# corrupting a live checkpoint and then restarting would silently replace it.
compose stop manufacturing-server >/dev/null 2>&1
sql "update snapshots set state=decode('00','hex') where tenant='plant-sz'" >/dev/null
[[ $(sql "select count(*) from snapshots where tenant='plant-sz' and state<>decode('00','hex')") == 0 ]] ||
  fail "plant checkpoint corruption was not applied"
compose start manufacturing-server >/dev/null 2>&1
wait_for "$MANUFACTURING/healthz"
if [[ $(curl -s -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/health" | jq -r .status) != quarantined ]]; then
  sql "select seq,code,octet_length(state) from snapshots where tenant='plant-sz' order by seq" >&2 || true
  compose logs --no-color manufacturing-server 2>/dev/null |
    grep -E 'restored|replayed|quarantined|snapshot' | tail -15 >&2 || true
  fail "damaged plant checkpoint did not isolate its tenant"
fi
[[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/me") == 200 ]] ||
  fail "healthy hotel was stopped by the plant's recovery"
recovery=$(curl -s -X POST -H "Authorization: Bearer $SUP" "$MANUFACTURING/v1/recovery/retry")
[[ $(jq -r .status <<<"$recovery") == ok ]] ||
  fail "operator could not rebuild the plant without a process restart: $recovery"
[[ $(state) == "$now" ]] || fail "in-place tenant recovery changed the business state"
compose restart manufacturing-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(code "$SUP") == 200 ]] && break; sleep 1; done
[[ $(code "$SUP") == 200 && $(state) == "$now" ]] || fail "repaired checkpoint failed on the next restart"
echo "ok   operator recovery: quarantine, healthy neighbor, in-place journal rebuild, durable replacement checkpoint"

# The hotel also replays the full mixed journal, without a checkpoint. The
# plant's in-place recovery above already exercised that path with live asks.
compose stop hospitality-server >/dev/null 2>&1
sql "delete from snapshots where tenant='hotel-a'" >/dev/null
compose start hospitality-server >/dev/null 2>&1
for _ in $(seq 30); do [[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MGR" "$HOSPITALITY/v1/me") == 200 ]] && break; sleep 1; done
[[ $(state) == "$now" ]] || fail "hotel mixed journal replay changed the bound workflow state"

workflow_finish() {
  local SERVER=$1 TENANT=$2 AUTHORITY=work builder=$3 operator=$4 suffix=$5
  local flow=build.review$suffix typ=build.rehearsal$suffix task id expected release version result
  for id in WF-OLD WF-NEW; do
    if [[ $id == WF-OLD ]]; then expected=done; version=1; release=$(jq -r .first "$backup/workflow-$suffix.json");
    else expected=rejected; version=2; release=$(jq -r .second "$backup/workflow-$suffix.json"); fi
    workflow_wait "$builder" "$flow:$id" "$version" "$release"
    task=$(workflow_get "$builder" "records/flow.instance/$flow:$id" | jq -er '.record.tokens[] | select(.waits == "ask") | .task') || fail "recovered native task absent"
    workflow_submit "$operator" "wf-answer-$id" work.task.complete work.task "$task" '{"answer":"approve"}'
    for _ in $(seq 30); do
      result=$(workflow_get "$builder" "records/$typ/$id")
      [[ $(jq -r .record.state <<<"$result") == "$expected" ]] && break
      sleep 1
    done
    [[ $(jq -r .record.state <<<"$result") == "$expected" ]] || fail "recovered workflow $id took a different version's path"
    workflow_get "$builder" "records/flow.instance/$flow:$id" | jq -e --arg r "$release" --argjson v "$version" \
      '.record | .state == "done" and .version == $v and .release == $r' >/dev/null || fail "completed workflow lost its starting release"
  done
}
workflow_finish "$MANUFACTURING" plant-sz "$SUP" "$OP1" plant
workflow_finish "$HOSPITALITY" hotel-a "$MGR" "$SALES_TOKEN" hotel
echo "ok   workflows: PostgreSQL restart, backup restore and full mixed journal rebuild preserved plans, bindings, versions and inbox tasks; old/new asks continued along their original paths in both industries"
function_after_recovery "$MANUFACTURING" plant-sz "$OP2" plant
function_after_recovery "$HOSPITALITY" hotel-a "$SALES_TOKEN" hotel
function_joint_finish "$MANUFACTURING" plant-sz "$SUP" "$OP1" plant
function_joint_finish "$HOSPITALITY" hotel-a "$MGR" "$SALES_TOKEN" hotel
deployed_browser after
echo "ok   builder AI functions: both operators called the page's retained function again after backup restore and complete journal recovery"

# The delivery profile (ADR-0048 D5b). Every scenario above ran in the default
# development/import profile, which keeps the direct install. A tenant that
# declares production refuses it at the owner — a hidden button is not a
# retirement — names the candidate route, and still previews a saved candidate,
# including a joint selection of several drafts.
AUTHORITY=platform SERVER=$HOSPITALITY TENANT=hotel-a \
  workflow_submit "$MGR" rp-set platform.setting.set platform.setting build/releaseProfile '{"value":"production"}'
result=$(AUTHORITY=build SERVER=$HOSPITALITY TENANT=hotel-a \
  submit "$MGR" rp-direct build.object.publish build.object WF-O '{}')
jq -e '.error.code == "ERROR_CODE_POLICY_DENIED"' <<<"$result" >/dev/null || fail "production accepted a direct install"
jq -e '.error.message | test("release candidate")' <<<"$result" >/dev/null || fail "the refusal did not name the candidate route"
candidate=$(AUTHORITY=build workflow_post "$MGR" releases/preview '{"kind":"page","id":"FN-P"}' | jq -er 'select(.diagnostic == null or .diagnostic == "") | .candidateId') || fail "production preview"
joint=$(AUTHORITY=build workflow_post "$MGR" releases/preview '{"drafts":[{"kind":"object","id":"WF-O"},{"kind":"page","id":"FN-P"}]}' | jq -er 'select(.diagnostic == null or .diagnostic == "") | .candidateId') || fail "production joint preview"
echo "ok   delivery profile: production refuses the direct install with the candidate route named; single and joint candidates still preview"
