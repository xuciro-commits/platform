#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
set -a
source deploy/local/.env
set +a

port="${PLATFORM_LOAD_PORT:-20296}"
db="platform_load_$(date +%s)_$$"
tenant="load$(date +%s)$$"
work="$(mktemp -d)"
output="${PLATFORM_LOAD_OUTPUT:-$PWD/.build/load/$(date +%Y%m%d-%H%M%S)-$$}"
binary="${PLATFORM_LOAD_BINARY:-$PWD/.build/host/platform-server}"
pid=""
sampler_pid=""
mkdir -p "$output"

cleanup() {
  if [[ -n "$sampler_pid" ]]; then
    kill "$sampler_pid" 2>/dev/null || true
    wait "$sampler_pid" 2>/dev/null || true
  fi
  if [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [[ -f "$work/server.log" ]]; then cp "$work/server.log" "$output/server.log"; fi
  docker exec platform-postgres-1 sh -ec 'dropdb --if-exists -U "$POSTGRES_USER" "$1"' _ "$db" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

command -v docker >/dev/null || { echo "SQL load verification needs Docker and the local PostgreSQL service" >&2; exit 1; }
[[ -x "$binary" ]] || { echo "Build the load host first, or set PLATFORM_LOAD_BINARY" >&2; exit 1; }
if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "SQL load port $port is occupied" >&2
  exit 1
fi
docker exec platform-postgres-1 sh -ec 'createdb -U "$POSTGRES_USER" "$1"' _ "$db" >/dev/null
export PLATFORM_LOAD_BASE="http://127.0.0.1:${port}"
export PLATFORM_LOAD_TENANT="$tenant"
export PLATFORM_LOAD_OUTPUT="$output"
python3 - "$tenant" >"$work/tenants.json" <<'PY'
import json
import os
import sys

count = max(int(value) for value in os.environ.get("PLATFORM_LOAD_USERS", "500,1000,5000").split(","))
assert 1 <= count <= 5000
seats = [{"id": "loadadmin", "subjects": ["loadadmin"], "roles": {"platform": "admin", "build": "builder"},
          "grants": [{"app": "platform", "role": "admin"}, {"app": "build", "role": "builder"}, {"app": "build", "role": "user"}]}]
seats.extend({"id": f"load{i + 1:05}", "subjects": [f"load{i + 1:05}"], "roles": {"build": "user"}} for i in range(count))
print(json.dumps([{"id": sys.argv[1], "settings": {"language": "en", "timezone": "UTC", "currency": "USD"}, "seats": seats}]))
PY
database="postgres://platform:${POSTGRES_PASSWORD}@127.0.0.1:${PG_PORT}/${db}"

start() {
  "$binary" -addr "127.0.0.1:${port}" -tenants "$work/tenants.json" -database "$database" -project >>"$work/server.log" 2>&1 &
  pid=$!
  (
    while kill -0 "$pid" 2>/dev/null; do
      ps -o rss= -p "$pid" >>"$1" || break
      sleep 1
    done
  ) &
  sampler_pid=$!
  for _ in $(seq 1 240); do
    if curl --max-time 2 -fsS "${PLATFORM_LOAD_BASE}/healthz" >/dev/null 2>&1; then return 0; fi
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "Load host exited; see $output/server.log" >&2
      return 1
    fi
    sleep 0.5
  done
  echo "Load host did not become healthy; see $output/server.log" >&2
  return 1
}

stop() {
  if [[ -n "$sampler_pid" ]]; then
    kill "$sampler_pid" 2>/dev/null || true
    wait "$sampler_pid" 2>/dev/null || true
    sampler_pid=""
  fi
  kill "$pid"
  wait "$pid" 2>/dev/null || true
  pid=""
}

shasum -a 256 "$binary" | awk '{print $1}' >"$output/binary.sha256"
git rev-parse HEAD >"$output/source-head.txt"
start "$output/seed-rss.txt"
perl -e 'alarm 900; exec @ARGV' node scripts/check-load.mjs --seed
stop
start "$output/run-rss.txt"
result=0
perl -e 'alarm 1800; exec @ARGV' node scripts/check-load.mjs --run || result=$?
docker exec platform-postgres-1 sh -ec 'psql -U "$POSTGRES_USER" -d "$1" -tAc "select json_build_object('\''position'\'',(select seq from runtime_tenants where tenant='\''$2'\''),'\''records'\'',(select count(*) from runtime_records where tenant='\''$2'\'' and type='\''build.loaditem'\''))"' _ "$db" "$tenant" >"$output/sql-state.json"
stop
echo "SQL load verification finished: $output/results.json"
exit "$result"
