#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
set -a
source deploy/local/.env
set +a

port="${PLATFORM_SQL_PORT:-20196}"
db="platform_verify_$(date +%s)"
tenant="sqlverify$(date +%s)"
work="$(mktemp -d)"
pid=""

cleanup() {
  if [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  docker exec platform-postgres-1 sh -ec 'dropdb --if-exists -U "$POSTGRES_USER" "$1"' _ "$db" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

command -v docker >/dev/null || { echo "verify-sql needs Docker and the local PostgreSQL service" >&2; exit 1; }
docker exec platform-postgres-1 sh -ec 'createdb -U "$POSTGRES_USER" "$1"' _ "$db" >/dev/null

python3 - "$tenant" >"$work/tenants.json" <<'PY'
import json
import sys

print(json.dumps([{
    "id": sys.argv[1],
    "settings": {"language": "en", "timezone": "UTC", "currency": "USD"},
    "seats": [{
        "id": "admin-1",
        "subjects": ["admin"],
        "roles": {"platform": "admin", "build": "builder", "ai": "admin", "agent": "admin"},
    }, {
        "id": "viewer-1",
        "subjects": ["viewer"],
        "roles": {"build": "viewer"},
    }, {
        "id": "discoverer-1",
        "subjects": ["discoverer"],
        "roles": {"build": "viewer"},
    }, {
        "id": "delegate-1",
        "subjects": ["delegate"],
        "roles": {},
        "grants": [{"app": "build", "role": "builder", "by": "project:sqlproject"}],
    }],
}]))
PY

database="postgres://platform:${POSTGRES_PASSWORD}@127.0.0.1:${PG_PORT}/${db}"

start() {
  .build/host/platform-server -addr "127.0.0.1:${port}" -tenants "$work/tenants.json" \
    -database "$database" -web web/apps/workspace/dist >"$work/server.log" 2>&1 &
  pid=$!
  for _ in $(seq 1 120); do
    if curl -fsS "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "SQL verification host exited during startup" >&2
      tail -80 "$work/server.log" >&2
      return 1
    fi
    sleep 0.5
  done
  echo "SQL verification host did not become healthy" >&2
  tail -80 "$work/server.log" >&2
  return 1
}

stop() {
  kill "$pid"
  wait "$pid" 2>/dev/null || true
  pid=""
}

submit() {
  local subject="$1" authority="$2" type="$3" id="$4" schema="$5" key="$6" revision="$7" payload="$8"
  local principal encoded
  principal="$(curl -fsS -H "Authorization: Bearer ${subject}" -H "Platform-Tenant: ${tenant}" \
    "http://127.0.0.1:${port}/v1/me" | python3 -c 'import json,sys; print(json.load(sys.stdin)["principalId"])')"
  encoded="$(printf '%s' "$payload" | base64 | tr -d '\n')"
  python3 - "$tenant" "$principal" "$authority" "$type" "$id" "$schema" "$key" "$revision" "$encoded" >"$work/submission.json" <<'PY'
import json
import sys

print(json.dumps({
    "tenantId": sys.argv[1],
    "principalId": sys.argv[2],
    "authority": sys.argv[3],
    "target": {"type": sys.argv[4], "id": sys.argv[5]},
    "schema": {"name": sys.argv[6], "version": 1},
    "idempotencyKey": sys.argv[7],
    "expectedRevision": int(sys.argv[8]),
    "payload": sys.argv[9],
}))
PY
  curl -sS -H "Authorization: Bearer ${subject}" -H "Platform-Tenant: ${tenant}" \
    -H 'Content-Type: application/json' --data-binary @"$work/submission.json" \
    "http://127.0.0.1:${port}/v1/submissions" >"$work/answer.json"
  python3 -c 'import json,sys; a=json.load(open(sys.argv[1])); assert not a.get("error"), a' "$work/answer.json"
}

start
curl -fsS -H 'Authorization: Bearer admin' -H "Platform-Tenant: ${tenant}" \
  "http://127.0.0.1:${port}/v1/me" >"$work/me.json"
principal="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["principalId"])' "$work/me.json")"
payload="$(printf '%s' '{"name":"sqlproject","title":"SQL project"}' | base64 | tr -d '\n')"
python3 - "$tenant" "$principal" "$payload" >"$work/submission.json" <<'PY'
import json
import sys

print(json.dumps({
    "tenantId": sys.argv[1],
    "principalId": sys.argv[2],
    "authority": "build",
    "target": {"type": "build.app", "id": "sqlproject"},
    "schema": {"name": "build.app.create", "version": 1},
    "idempotencyKey": "sql-smoke-create",
    "expectedRevision": 0,
    "payload": sys.argv[3],
}))
PY
curl -fsS -H 'Authorization: Bearer admin' -H "Platform-Tenant: ${tenant}" \
  -H 'Content-Type: application/json' --data-binary @"$work/submission.json" \
  "http://127.0.0.1:${port}/v1/submissions" >"$work/answer.json"
python3 -c 'import json,sys; a=json.load(open(sys.argv[1])); assert not a.get("error"), a; assert a.get("record",{}).get("changeId"), a' "$work/answer.json"

submit admin platform platform.project sqlproject platform.project.save sql-smoke-project-members 0 \
  '{"title":"SQL project","members":[{"member":"admin-1","role":"owner"},{"member":"viewer-1","role":"viewer"},{"member":"discoverer-1","role":"discoverer"},{"member":"delegate-1","role":"editor"}]}'
submit delegate build build.object sqlobject build.object.create sql-smoke-object 0 \
  '{"name":"sqlobject","title":"SQL object","project":"sqlproject","fields":[{"name":"title","title":"Title","type":"text","required":true}],"access":[{"role":"user","read":"all"}],"states":[{"name":"open","title":"Open"}]}'

curl -fsS -H 'Authorization: Bearer viewer' -H "Platform-Tenant: ${tenant}" \
  "http://127.0.0.1:${port}/v1/resources" >"$work/viewer-resources.json"
curl -fsS -H 'Authorization: Bearer discoverer' -H "Platform-Tenant: ${tenant}" \
  "http://127.0.0.1:${port}/v1/resources" >"$work/discoverer-resources.json"
python3 - "$work/viewer-resources.json" "$work/discoverer-resources.json" <<'PY'
import json
import sys

def find(path):
    data = json.load(open(path))["resources"]
    matches = [r for r in data if r["ref"]["name"] in ("sqlobject", "build.sqlobject") and r["project"] == "sqlproject"]
    assert len(matches) == 1, matches
    return matches[0]

viewer = find(sys.argv[1])
discoverer = find(sys.argv[2])
assert viewer["canOpen"] is True and viewer["canEdit"] is False, viewer
assert discoverer["canOpen"] is False and discoverer["canEdit"] is False, discoverer
PY

stop
start
curl -fsS -H 'Authorization: Bearer admin' -H "Platform-Tenant: ${tenant}" \
  "http://127.0.0.1:${port}/v1/records/build.app/sqlproject" >"$work/record.json"
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])).get("record",{}); assert r.get("title")=="SQL project", r; assert r.get("revision")==1, r' "$work/record.json"
curl -fsS -H 'Authorization: Bearer viewer' -H "Platform-Tenant: ${tenant}" \
  "http://127.0.0.1:${port}/v1/resources" >"$work/recovered-resources.json"
python3 - "$work/recovered-resources.json" <<'PY'
import json
import sys

resources = json.load(open(sys.argv[1]))["resources"]
assert any(r["ref"]["name"] in ("sqlobject", "build.sqlobject") and r["project"] == "sqlproject" and r["canOpen"] for r in resources), resources
PY
stop

echo "SQL verification passed: isolated PostgreSQL create, viewer/discoverer delegation, restart, read"
