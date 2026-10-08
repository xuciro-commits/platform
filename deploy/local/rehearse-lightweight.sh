#!/usr/bin/env bash
# The lightweight profile's rehearsal (ADR-0049 §3.5, §5): one host binary and
# one data directory, no Docker, no PostgreSQL, no external identity provider.
# It shows the whole durable state is that directory:
#
#   walk    start on a fresh directory, sign in with a token the host minted
#           itself, commit a decision through the file journal, stop, start
#           again on the same directory, and read the same state back - the
#           restart the ADR asks of every slice
#   verify  assert a running host (HOST and TOKEN) still serves that state
#   backup  copy the one directory into another, start a host on the copy, and
#           show it serves the same state with the token it mints there
#
# The delivery profile's route (drafts, candidate, activation, the business
# task) is rehearse.sh on the compose project; this script is the
# lightweight side of the same guarantee: the semantics are the platform's, only
# the storage, the file bytes and the identity provider are local.
#
#   go build -o /tmp/hospitality-server ./solutions/hospitality/cmd/hospitality-server
#   PLATFORM_REHEARSE_BIN=/tmp/hospitality-server bash deploy/local/rehearse-lightweight.sh walk
#   HOST=http://127.0.0.1:18499 TOKEN=... bash deploy/local/rehearse-lightweight.sh verify
#   PLATFORM_REHEARSE_BIN=/tmp/hospitality-server bash deploy/local/rehearse-lightweight.sh backup
#
# Needs: the host binary, curl, jq.
set -euo pipefail

BIN=${PLATFORM_REHEARSE_BIN:-}
DATA=${DATA:-/tmp/platform-lightweight-rehearse}
ADDR=${ADDR:-127.0.0.1:18499}
HOST=${HOST:-http://$ADDR}
TENANT=${TENANT:-hotel-a}
SUBJECT=${SUBJECT:-manager}
MEMBER=${MEMBER:-manager-1}
LANGUAGE=${LANGUAGE:-zh-CN}
STATE=${STATE:-/tmp/platform-rehearse-lightweight.state}
mode=${1:-walk}
STAMP=${STAMP:-$(date +%s)$((RANDOM % 1000))}
PID=""
fail() { printf 'FAIL %s\n' "$1" >&2; exit 1; }
ok() { printf 'ok   %s\n' "$1"; }
cleanup() { [[ -n $PID ]] && stop || true; }
trap cleanup EXIT

get() { curl -sf -H "Authorization: Bearer ${TOKEN:-}" "$HOST/v1/$1" || fail "GET /v1/$1"; }
submit() { # <key> <payload json>
  local body
  body=$(jq -n --arg t "$TENANT" --arg w "$MEMBER" --arg k "$1" --arg p "$(printf '%s' "$2" | base64 -w0)" \
    '{tenantId:$t, principalId:$w, authority:"platform", idempotencyKey:$k,
      schema:{name:"platform.member.language", version:1}, target:{type:"platform.member", id:$w}, payload:$p}')
  curl -s -H "Authorization: Bearer ${TOKEN:-}" -H 'Content-Type: application/json' "$HOST/v1/submissions" -d "$body"
}
start() { # <data dir> <log file>
  "$BIN" -profile lightweight -data "$1" -addr "$ADDR" >>"$2" 2>&1 &
  PID=$!
  for _ in $(seq 40); do
    curl -sf "$HOST/healthz" >/dev/null 2>&1 && return 0
    kill -0 "$PID" 2>/dev/null || fail "the host exited: $(tail -3 "$2")"
    sleep 0.25
  done
  fail "the host did not answer on $HOST"
}
stop() {
  [[ -n $PID ]] || return 0
  kill -TERM "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
  PID=""
}
mint() { "$BIN" -profile lightweight -data "$1" -mint-token "$SUBJECT" || fail "-mint-token $SUBJECT"; }

if [[ $mode == verify ]]; then
  [[ -n ${TOKEN:-} ]] || fail "set TOKEN to a token of the running host"
  [[ -f $STATE ]] && jq -e . "$STATE" >/dev/null || fail "state file $STATE (run walk first)"
  CHG=$(jq -r .change "$STATE") LANG=$(jq -r .language "$STATE") KEY=$(jq -r .key "$STATE")
  me=$(get me)
  [[ $(jq -r .principalId <<<"$me") == "$MEMBER" ]] || fail "who the token is: $me"
  [[ $(jq -r .preferred <<<"$me") == "$LANG" ]] || fail "the language after the restart: $me"
  # The same request, under the key it was committed with, is still the same
  # decision - not a second one.
  same=$(submit "$KEY" "$(jq -n --arg l "$LANG" '{language:$l}')")
  [[ $(jq -r '.record.changeId // empty' <<<"$same") == "$CHG" ]] || fail "the committed answer: $same (expected $CHG)"
  ok "continuation: $MEMBER reads $LANG, the decision $CHG answers the same after the restart"
  exit 0
fi

[[ -n $BIN && -x $BIN ]] || fail "set PLATFORM_REHEARSE_BIN to a solution host binary"
empty=$(mktemp -d)
trap 'rm -rf "$empty"; cleanup' EXIT

if [[ $mode == walk ]]; then
  # 1. The profile is declared: a lightweight host without a key stops and says
  # how to make one, and a flag of the other profile is refused, not ignored.
  out=$("$BIN" -profile lightweight -data "$empty" -addr "$ADDR" 2>&1 || true)
  [[ $out == *-idp-new-key* ]] || fail "a host without a key: $out"
  out=$("$BIN" -profile lightweight -data "$empty" -database postgres://host/journal 2>&1 || true)
  [[ $out == *-database* ]] || fail "a lightweight host took -database: $out"
  ok "profile: no key is told how to make one, -database is refused (ADR-0049 D1)"

  # 2. One directory, one key: a second -idp-new-key never replaces it.
  [[ ! -e $DATA/journal ]] || fail "$DATA already holds a journal (rm -rf it, or set DATA)"
  mkdir -p "$DATA"
  "$BIN" -profile lightweight -data "$DATA" -idp-new-key >/dev/null || fail "-idp-new-key"
  out=$("$BIN" -profile lightweight -data "$DATA" -idp-new-key 2>&1 || true)
  [[ $out == *already\ exists* ]] || fail "a second -idp-new-key: $out"
  TOKEN=$(mint "$DATA")
  [[ -n $TOKEN ]] || fail "-mint-token printed nothing"
  ok "key: made once in $DATA/idp.key, never replaced; a token for $SUBJECT signed with it"

  # 3. The host serves, takes its own token and not the development form.
  start "$DATA" "$DATA/host.log"
  me=$(get me)
  [[ $(jq -r .principalId <<<"$me") == "$MEMBER" ]] || fail "who signed in: $me"
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $SUBJECT" "$HOST/v1/me")
  [[ $code == 401 ]] || fail "a lightweight host accepted the development token ($code)"
  ok "identity: /v1/me answers $MEMBER for the host's own token; the unsigned token is refused"

  # 4. Work goes through the journal in the data directory.
  want=$(jq -n --arg l "$LANGUAGE" '{language:$l}')
  first=$(submit "$STAMP-1" "$want")
  CHG=$(jq -r '.record.changeId // empty' <<<"$first")
  [[ -n $CHG ]] || fail "the first decision: $first"
  [[ $(get me | jq -r .preferred) == "$LANGUAGE" ]] || fail "the decision did not apply"
  ls "$DATA/journal/journal.jsonl" >/dev/null || fail "no journal in $DATA"
  ok "work: $CHG committed to $DATA/journal/journal.jsonl and applied ($MEMBER reads $LANGUAGE)"

  # 5. Stop and start again on the same directory: the state is replayed.
  stop
  start "$DATA" "$DATA/host.log"
  grep -qE "restored $TENANT from the snapshot at [0-9]+, then replayed [0-9]+ entries|replayed [0-9]+ entries for $TENANT" "$DATA/host.log" \
    || fail "the restart did not read the journal: $(tail -3 "$DATA/host.log")"
  me=$(get me)
  [[ $(jq -r .principalId <<<"$me") == "$MEMBER" && $(jq -r .preferred <<<"$me") == "$LANGUAGE" ]] || fail "after the restart: $me"
  again=$(submit "$STAMP-1" "$want")
  [[ $(jq -r '.record.changeId // empty' <<<"$again") == "$CHG" ]] || fail "the committed answer changed: $again"
  second=$(submit "$STAMP-2" "$want") # a new key, so a new decision
  [[ -n $(jq -r '.record.changeId // empty' <<<"$second") ]] || fail "new work after the restart: $second"
  ok "restart: the journal replayed, $CHG is still the answer of its key, and new work was taken"
  jq -n --arg chg "$CHG" --arg lang "$LANGUAGE" --arg key "$STAMP-1" '{change:$chg, language:$lang, key:$key}' >"$STATE"
  ok "state for verify: $STATE"
  stop
fi

if [[ $mode == backup ]]; then
  [[ -d $DATA/journal ]] || fail "$DATA has no journal (run walk first)"
  copy=$(mktemp -d)
  trap 'rm -rf "$empty" "$copy"; cleanup' EXIT
  tar -C "$(dirname "$DATA")" -czf "$copy/state.tar.gz" "$(basename "$DATA")" || fail "tar $DATA"
  mkdir -p "$copy/restored"
  tar -xzf "$copy/state.tar.gz" -C "$copy/restored" --strip-components=1 || fail "untar"
  # The copy holds the journal, the snapshots, the file bytes and the key: a
  # host started on it mints its own token and serves the same state.
  TOKEN=$(mint "$copy/restored")
  start "$copy/restored" "$copy/restored/host.log"
  me=$(get me)
  [[ $(jq -r .principalId <<<"$me") == "$MEMBER" ]] || fail "the copy's host: $me"
  [[ $(jq -r .preferred <<<"$me") == "$LANGUAGE" ]] || fail "the copy's state: $me"
  worked=$(submit "$STAMP-copy" "$(jq -n --arg l "$LANGUAGE" '{language:$l}')")
  [[ -n $(jq -r '.record.changeId // empty' <<<"$worked") ]] || fail "the copy took no new work: $worked"
  ok "backup: $(basename "$DATA") copied to $(dirname "$copy")/restored — journal, snapshots, bytes and key — and a host on the copy serves and continues $TENANT"
  stop
fi
