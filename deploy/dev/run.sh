#!/usr/bin/env bash
# Starts the built solution host against the infrastructure containers of
# deploy/local/compose.yaml (make infra): PostgreSQL on PG_PORT, Rauthy on
# IDP_PORT, RustFS on FILES_PORT. With PLATFORM_DEV_LIGHT=1 the host keeps
# everything in memory and takes development tokens, so no container is needed.
# Usage: deploy/dev/run.sh <solution> <port>
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
solution=${1:-hospitality} port=${2:-8495}
[[ -f deploy/local/.env ]] && set -a && source deploy/local/.env && set +a
bin=".build/dev/$solution-server"
case "$solution" in
  hospitality) admin=user:manager@hotel.test ;;
  manufacturing) admin=user:sup@plant.test ;;
esac
if [[ -n ${PLATFORM_DEV_LIGHT:-} ]]; then
  exec "$bin" -addr "127.0.0.1:$port" -templates deploy/templates
fi
export PLATFORM_S3_ACCESS_KEY=platform PLATFORM_S3_SECRET_KEY=platform-files-local-only
export PLATFORM_SECRET_SINK=sinkLocalOnly0000000000000000000000000000000000000000000000000000
export PLATFORM_SECRET_OPENROUTER=${OPENROUTER_API_KEY:-}
compute=.build/dev/compute
[[ -S $compute/worker.sock ]] && export PLATFORM_WASM_WORKER_SOCKET=$compute/worker.sock PLATFORM_CODE_BUILDER_SOCKET=$compute/builder.sock
exec "$bin" -addr "127.0.0.1:$port" \
  -database "postgres://platform:platform-local-only@127.0.0.1:${PG_PORT:-5433}/platform" \
  -oidc-issuer "http://localhost:${IDP_PORT:-8480}/auth/v1/" \
  -oidc-keys "http://localhost:${IDP_PORT:-8480}/auth/v1/oidc/certs" \
  -files "http://127.0.0.1:${FILES_PORT:-9000}/platform" \
  -tenants "deploy/local/$solution/tenants.json" -templates deploy/templates \
  -host-admins "$admin" -project
