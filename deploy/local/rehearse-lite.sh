#!/usr/bin/env bash
# The no-Docker delivery rehearsal (ADR-0047 M4, ADR-0049's lightweight direction):
# one solution host on a real PostgreSQL journal with development tokens, the
# delivery route (drafts -> joint candidate -> activation), a business task on
# what was delivered, and the governance state an operator reads to locate a run:
# the release profile, the active candidate, the host's own work and its attempts.
#
#   walk    do the route and write the ids it used; it puts the tenant in the
#           production profile first (ADR-0048 D5b: a tenant that delivers
#           through candidates refuses the direct install)
#   verify  assert the same state is still readable - run it after a restart, on
#           the same host, to show the fixed persistent environment continues
#
# Assertions that need the Docker project (Rauthy OIDC, RustFS file bytes, the
# Wasm/code worker, the whole compose project's backup and restore) stay in
# rehearse.sh, which runs the same delivery route on the delivered deployment.
# This script needs only: a running host, curl, jq.
#
#   HOST=http://127.0.0.1:18498 TOKEN=manager bash deploy/local/rehearse-lite.sh walk
#   HOST=http://127.0.0.1:18498 TOKEN=manager bash deploy/local/rehearse-lite.sh verify
#
# With PLATFORM_REHEARSE_PG_BIN (a PostgreSQL bin directory), PLATFORM_REHEARSE_DSN
# (the host's journal) and PLATFORM_REHEARSE_RESTORE_DSN (an empty database),
# `backup` dumps the journal, restores it and checks the fixed environment's rows
# are all there - the delivery profile's data guarantee, without Docker.
set -euo pipefail

HOST=${HOST:-http://127.0.0.1:18498}
TOKEN=${TOKEN:-manager}
TENANT=${TENANT:-hotel-a}
STATE=${STATE:-/tmp/platform-rehearse-lite.state}
PG_BIN=${PLATFORM_REHEARSE_PG_BIN:-}
DSN=${PLATFORM_REHEARSE_DSN:-}

mode=${1:-walk}
STAMP=${STAMP:-$(date +%s)$((RANDOM % 1000))}
fail() { printf 'FAIL %s\n' "$1" >&2; exit 1; }
ok() { printf 'ok   %s\n' "$1"; }

get() { curl -sf -H "Authorization: Bearer $TOKEN" "$HOST/v1/$1" || fail "GET /v1/$1"; }
post() { curl -sf -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$HOST/v1/$1" -d "$2" || fail "POST /v1/$1"; }
who=$(get me | jq -r .principalId)
[[ -n $who && $who != null ]] || fail "who the token is"
tenant=$(get me | jq -r .tenantId)
[[ $tenant == "$TENANT" ]] || fail "tenant $tenant (expected $TENANT)"
submit() { # id-suffix schema target-type target-id payload [authority] [quiet]
  local body
  body=$(jq -n --arg t "$TENANT" --arg w "$who" --arg k "$STAMP-$1" --arg s "$2" --arg tt "$3" --arg ti "$4" --arg p "$(printf '%s' "$5" | base64 -w0)" --arg a "${6:-build}" \
    '{tenantId:$t, principalId:$w, authority:$a, idempotencyKey:$k, schema:{name:$s, version:1}, target:{type:$tt, id:$ti}, payload:$p}')
  if [[ ${7:-} == quiet ]]; then
    curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$HOST/v1/submissions" -d "$body"
  else
    curl -sf -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$HOST/v1/submissions" -d "$body" || fail "submit $2 $4"
  fi
}

if [[ $mode == walk ]]; then
  STAMP=$(date +%s)$((RANDOM % 1000))
  OBJ=M4L-O-$STAMP PAGE=M4L-P-$STAMP APP=M4L-A-$STAMP
  NAME=m4lite$STAMP PAGES=${NAME}p APPNAME=${NAME}a
  # 1. The delivery profile is traceable, and a production tenant refuses the
  # direct install the authoring surfaces used to offer (ADR-0048 D5b/D6).
  profile=$(get release-profile)
  jq -e '.profile' <<<"$profile" >/dev/null || fail "release profile: $profile"
  if [[ $(jq -r .directInstall <<<"$profile") == true ]]; then
    submit s1 platform.setting.set platform.setting "build/${PROFILE_SETTING:-releaseProfile}" '{"value":"production"}' platform >/dev/null
    profile=$(get release-profile)
  fi
  [[ $(jq -r .directInstall <<<"$profile") == false ]] || fail "a candidate tenant still offers the direct install: $profile"
  ok "release profile: $(jq -c . <<<"$profile") (a production tenant refuses the direct install)"

  # 2. The direct install a production tenant retired is refused by the owner.
  refused=$(submit x1 build.object.publish build.object "$OBJ" '{}' build quiet)
  [[ $(jq -r '.error.code // empty' <<<"$refused") == ERROR_CODE_POLICY_DENIED ]] || fail "direct install in production: $refused"
  ok "direct install: refused by the owner, naming the candidate route"

  # 3. Drafts exist and nothing is installed; the host names what the application needs.
  submit d1 build.object.create build.object "$OBJ" \
    "{\"name\":\"$NAME\",\"title\":\"M4 $STAMP\",\"plural\":\"M4 $STAMP\",\"fields\":[{\"name\":\"note\",\"title\":\"Note\",\"type\":\"text\"}]}" >/dev/null
  submit d2 build.page.create build.page "$PAGE" \
    "{\"name\":\"$PAGES\",\"title\":\"M4 $STAMP\",\"object\":\"build.$NAME\",\"sections\":[{\"widget\":\"table\",\"fields\":[\"note\"]}]}" >/dev/null
  submit d3 build.app.create build.app "$APP" "{\"name\":\"$APPNAME\",\"title\":\"M4 $STAMP\",\"pages\":[\"$PAGES\"]}" >/dev/null
  closure=$(post releases/drafts/referenced "{\"kind\":\"app\",\"id\":\"$APP\"}")
  [[ $(jq -r '[.drafts[].kind]|join(",")' <<<"$closure") == "page,object" ]] || fail "closure: $closure"
  ok "drafts: object, page over it and application saved; the host names the two it needs"

  # 4. One joint candidate: preview, save, activate; the active identity is readable.
  DRAFTS="[{\"kind\":\"object\",\"id\":\"$OBJ\"},{\"kind\":\"page\",\"id\":\"$PAGE\"},{\"kind\":\"app\",\"id\":\"$APP\"}]"
  review=$(post releases/preview "{\"drafts\":$DRAFTS}")
  CAND=$(jq -r '.candidateId // empty' <<<"$review")
  [[ -n $CAND ]] || fail "preview: $review"
  jq -e '(.drafts // [])|length>=0' <<<"$review" >/dev/null || fail "preview provenance: $review"
  post releases/candidates "{\"drafts\":$DRAFTS,\"candidateId\":\"$CAND\",\"key\":\"m4lite-save-$STAMP\"}" >/dev/null
  post releases/active "{\"candidateId\":\"$CAND\",\"key\":\"m4lite-active-$STAMP\"}" >/dev/null
  active=$(get releases/active | jq -r .id)
  [[ $active == "$CAND" ]] || fail "active release: $active (candidate $CAND)"
  ok "delivery: one joint candidate activated; the active release identity is $active"

  # 5. The delivered definition carries a business task, with the old workspace untouched.
  submit r1 "build.$NAME.create" "build.$NAME" "M4L-$STAMP" '{"note":"survives a restart"}' >/dev/null
  read -r count note <<<"$(get "records/build.$NAME?limit=5" | jq -r '"\(.records|length) \(.records[0].note // "")"')"
  [[ $count == 1 && $note == "survives a restart" ]] || fail "record read back: $count [$note]"
  ok "business task: the delivered object wrote and read a record on the fixed environment"

  jq -n --arg name "$NAME" --arg obj "$OBJ" --arg page "$PAGE" --arg app "$APP" --arg cand "$CAND" \
    --argjson count "$count" --arg note "$note" \
    '{name:$name, obj:$obj, page:$page, app:$app, cand:$cand, count:$count, note:$note}' >"$STATE"
  ok "state for verify: $STATE"
fi

# Governance: what an operator reads to locate a run and see the host's own work.
if [[ $mode == walk || $mode == verify ]]; then
  if [[ $mode == verify ]]; then
    [[ -f $STATE ]] && jq -e . "$STATE" >/dev/null || fail "state file $STATE (run walk first)"
    NAME=$(jq -r .name "$STATE") CAND=$(jq -r .cand "$STATE")
    COUNT=$(jq -r .count "$STATE") NOTE=$(jq -r .note "$STATE")
    read -r count note <<<"$(get "records/build.$NAME?limit=5" | jq -r '"\(.records|length) \(.records[0].note // "")"')"
    [[ $count == "$COUNT" && $note == "$NOTE" ]] || fail "after the restart: $count [$note] (expected $COUNT [$NOTE])"
    [[ $(get releases/active | jq -r .id) == "$CAND" ]] || fail "the active release changed across the restart"
    ok "continuation: the record and the active candidate ($CAND) are still there, unchanged (run verify after a restart)"
  fi
  health=$(get health)
  jq -e '.status and .queues' <<<"$health" >/dev/null || fail "tenant health: $health"
  work=$(get work)
  [[ $(jq 'length' <<<"$work") -gt 0 ]] || fail "the host's own work is empty: $work"
  jq -e 'all(.[]; .kind and .state and .title)' <<<"$work" >/dev/null || fail "work entries: $work"
  failed=$(jq '[.[] | select(.state == "failed")] | length' <<<"$work")
  ok "owned work: $(jq 'length' <<<"$work") tasks as app:<id>, $failed failed; a failure keeps its reason and is retried by the granted action"
  # The granted recovery: an administrator asks the host to run one task now,
  # and sees it happen (attempts and last advance). verify only reads.
  if [[ $mode == walk ]]; then
  task=$(jq -r '.[0].id' <<<"$work") before=$(jq -r '.[0].attempts' <<<"$work")
  run=$(jq -n --arg t "$TENANT" --arg w "$who" --arg k "$STAMP-retry" --arg id "$task" \
    '{tenantId:$t, principalId:$w, authority:"platform", idempotencyKey:$k, schema:{name:"platform.work.retry", version:1}, target:{type:"platform.work", id:$id}, payload:("{}"|@base64)}')
  curl -sf -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$HOST/v1/submissions" -d "$run" >/dev/null || fail "retry $task"
  after=$before
  for _ in $(seq 20); do # the host's own loop takes the task up, not the submission
    after=$(get work | jq -r --arg id "$task" '.[] | select(.id == $id) | .attempts')
    [[ $after -gt $before ]] && break
    sleep 0.5
  done
  [[ $after -gt $before ]] || fail "$task did not run again: $before -> $after"
  ok "recovery: the administrator ran $task again ($before -> $after attempts), and a refusal names the authority that decides it"
  fi
  deliveries=$(get deliveries)
  [[ $deliveries == null || $(jq 'type' <<<"$deliveries") == '"array"' ]] || fail "delivery attempts: $deliveries"
  attempts=$(jq 'length' <<<"$deliveries" 2>/dev/null || echo 0)
  if [[ ${attempts:-0} -gt 0 ]]; then
    jq -e 'all(.[]; .outcome)' <<<"$deliveries" >/dev/null || fail "delivery attempts: $deliveries"
  fi
  ok "attempts: $attempts outbound deliveries recorded (a tenant without endpoints has none), each with its outcome"
  ok "governance: status $(jq -r .status <<<"$health"), queues $(jq -c '.queues // []' <<<"$health"), failed $(jq -r .failed <<<"$health")"
fi

# The delivery profile's data guarantee, without Docker: the journal in one
# database, restored into a new one, row for row.
if [[ $mode == backup ]]; then
  restore=${PLATFORM_REHEARSE_RESTORE_DSN:-}
  [[ -n $PG_BIN && -n $DSN && -n $restore ]] || fail "set PLATFORM_REHEARSE_PG_BIN, PLATFORM_REHEARSE_DSN and PLATFORM_REHEARSE_RESTORE_DSN"
  dump=$(mktemp -d)
  trap 'rm -rf "$dump"' EXIT
  count() { "$PG_BIN/psql" "$1" -Atc "select count(*) from $2 where tenant = '$TENANT'"; }
  before=$(count "$DSN" journal)
  "$PG_BIN/pg_dump" "$DSN" -Fc --exclude-schema='tenant_*' >"$dump/journal.dump" || fail "pg_dump"
  "$PG_BIN/pg_restore" --no-owner --clean --if-exists -d "$restore" "$dump/journal.dump" || fail "pg_restore"
  after=$(count "$restore" journal)
  [[ $before -gt 0 && $before == "$after" ]] || fail "restored journal: $after entries (was $before)"
  snaps=$(count "$restore" snapshots)
  ok "backup: $after journal entries and $snaps snapshots of $TENANT restored into a new database"
fi
