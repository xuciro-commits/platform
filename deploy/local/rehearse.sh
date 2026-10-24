#!/usr/bin/env bash
# Recover platform mechanisms in an owned,
# disposable deployment. Daily tenants, configurations and volumes stay intact.
set -euo pipefail
cd "$(dirname "$0")/../.."
export COMPOSE_PROJECT_NAME="platform-rehearsal-$$"
export COMPOSE_PARALLEL_LIMIT=1
export PLATFORM_DATA_NAMESPACE="$COMPOSE_PROJECT_NAME"
export PLATFORM_PERSONAL_TOKEN_KEY="$(openssl rand -hex 32)"
export PG_PORT=55433 IDP_PORT=58480 MANUFACTURING_PORT=58490 HOSPITALITY_PORT=58495 SINK_PORT=58497 FILES_PORT=59000 FILES_CONSOLE_PORT=59001
mkdir -p .build/rehearse
scratch=$(mktemp -d "$PWD/.build/rehearse/rehearsal-XXXXXX")
export PLATFORM_REHEARSAL_LIMITS_FILE="$scratch/runtime-limits.yaml"
cat >"$PLATFORM_REHEARSAL_LIMITS_FILE" <<'EOF'
services:
  hospitality-server:
    cpus: 1
    environment: { GOMAXPROCS: "1", GODEBUG: "memprofilerate=0" }
  manufacturing-server:
    cpus: 1
    environment: { GOMAXPROCS: "1", GODEBUG: "memprofilerate=0" }
EOF
export HOSPITALITY_TENANTS_FILE="$scratch/hospitality.json"
export MANUFACTURING_TENANTS_FILE="$scratch/manufacturing.json"
cp deploy/local/hospitality/tenants.json "$HOSPITALITY_TENANTS_FILE"
cp deploy/local/manufacturing/tenants.json "$MANUFACTURING_TENANTS_FILE"
chmod g+w "$HOSPITALITY_TENANTS_FILE" "$MANUFACTURING_TENANTS_FILE"
compose() { docker compose -f deploy/local/compose.yaml -f "$PLATFORM_REHEARSAL_LIMITS_FILE" "$@"; }
cleanup() {
  local status=$?
  trap - EXIT
  if ((status)); then
    compose logs --no-color hospitality-server manufacturing-server 2>/dev/null |
      grep -E 'quarantin|panic:|recovery|snapshot|replayed' | tail -25 >&2 || true
  fi
  compose down -v --rmi local --remove-orphans >/dev/null 2>&1 || true
  # Keep diagnostics, but no copied deployment inputs or credentials.
  rm -f "$HOSPITALITY_TENANTS_FILE" "$MANUFACTURING_TENANTS_FILE"
  exit "$status"
}
trap cleanup EXIT
pnpm --dir web/apps/workspace build >"$scratch/web.log" 2>&1
compose --progress plain build >"$scratch/build.log" 2>&1
compose up -d --quiet-pull >"$scratch/up.log" 2>&1
python3 deploy/local/platform_rehearsal.py "$scratch"
(cd capabilities/server && go build -o ../../.build/storage-files ./cmd/storage-files)
python3 deploy/local/storage_handover_rehearsal.py "$scratch"
compose exec -T postgres createdb -U platform rehearsal_test
PLATFORM_TEST_DATABASE="postgres://platform:platform-local-only@127.0.0.1:$PG_PORT/rehearsal_test" \
  make test-go PKG=. RUN='TestJournal|TestObservationIntervalProjectionFlowIdentityAndPureRecovery|TestPostgresTableProfilePullAndReplay|TestPostgresApplicationRuntimeAndDiagnosticReadScopes|TestPostgresRetainedFlowAliasQueryUsesConvertedRows|TestPostgresBusinessUniqueKeysSurvivePrivateFailureAndRestart' >"$scratch/postgres.log" 2>&1
printf 'Common host recovery and declared application rehearsal passed. Diagnostics: %s\n' "$scratch"
