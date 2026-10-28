#!/usr/bin/env bash
# Exercise the current composition against a disposable, isolated PostgreSQL.
# No daily tenant configuration, journal, key or volume is opened.
set -euo pipefail
cd "$(dirname "$0")/../.."
if [[ -n ${PROFILE:-} ]]; then
  echo 'The retired Flow migration rehearsal is unavailable in this composition; use the current owner recovery checks.' >&2
  exit 2
fi
read -r -a pnpm <<< "${PNPM:-pnpm}"
(cd web && "${pnpm[@]}" --dir apps/workspace build)
make check-go
if ! docker image inspect platform-postgres:18-vector >/dev/null 2>&1; then
  docker build -f deploy/local/Dockerfile.postgres -t platform-postgres:18-vector .
fi
container="platform-rehearsal-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
# An anonymous disposable container owns this database; never mount daily volumes.
docker run -d --rm --name "$container" -p 127.0.0.1::5432 \
  -e POSTGRES_USER=platform -e POSTGRES_PASSWORD=rehearsal-local -e POSTGRES_DB=rehearsal \
  --tmpfs /var/lib/postgresql platform-postgres:18-vector >/dev/null
for ((attempt=0; attempt<60; attempt++)); do
  if docker exec "$container" pg_isready -U platform -d rehearsal >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec "$container" pg_isready -U platform -d rehearsal >/dev/null
port=$(docker port "$container" 5432/tcp)
port=${port##*:}
PLATFORM_TEST_DATABASE="postgres://platform:rehearsal-local@127.0.0.1:$port/rehearsal" \
  make test-go PKG=. RUN='TestSystemDefinitionsPostgresCheckpointRestoresSharedRuntime|TestAIModelConfigurationPostgresCheckpoint|TestJournalAcceptedJointFunctionActivationCrash|TestJournalAcceptedFunctionCrashBeforeApplication'
make test-go PKG='./cmd/platform-updater ./internal/systembuild ./internal/systemscope ./engine/organization'
make test-go PKG=. RUN='TestSystemFreeze|TestSystemDefinitions|TestSystemSaved|TestStaticHostMaintenance|TestNamed|TestParameterized|TestUnparameterized|TestCatalogCache'
printf 'Current composition, frozen system maintenance and isolated PostgreSQL checkpoint/recovery checks passed.\n'
