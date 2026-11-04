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

stop
start
curl -fsS -H 'Authorization: Bearer admin' -H "Platform-Tenant: ${tenant}" \
  "http://127.0.0.1:${port}/v1/records/build.app/sqlproject" >"$work/record.json"
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])).get("record",{}); assert r.get("title")=="SQL project", r; assert r.get("revision")==1, r' "$work/record.json"
stop

echo "SQL verification passed: isolated PostgreSQL create, restart, read"
