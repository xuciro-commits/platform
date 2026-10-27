#!/usr/bin/env bash
# One native API. Vite exposes the platform and its identity proxy at 8495.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
python3 deploy/local/bootstrap.py
set -a
source deploy/local/.env
set +a
port=18495
if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "Platform API port $port is occupied; stop its owning host first." >&2
  exit 1
fi
if [[ -n ${PLATFORM_DEV_LIGHT:-} ]]; then
  exec .build/dev/platform-server -addr "127.0.0.1:$port" -tenants "$PLATFORM_TENANTS_FILE" -templates deploy/templates
fi
compute=.build/dev/compute
[[ -S $compute/worker.sock ]] && export PLATFORM_WASM_WORKER_SOCKET=$compute/worker.sock PLATFORM_CODE_BUILDER_SOCKET=$compute/builder.sock
export PLATFORM_PACKAGES_DIR="$PWD/deploy/packages"
exec .build/dev/platform-server -addr "127.0.0.1:$port" \
  -database "postgres://platform:${POSTGRES_PASSWORD}@127.0.0.1:${PG_PORT}/${PLATFORM_DATABASE_NAME}" \
  -oidc-issuer "${PLATFORM_PUBLIC_URL}/auth/v1/" \
  -oidc-proxy "http://127.0.0.1:${IDP_PORT}" \
  -oidc-keys "http://127.0.0.1:${IDP_PORT}/auth/v1/oidc/certs" \
  -files "http://127.0.0.1:${FILES_PORT}/platform" \
  -tenants "$PLATFORM_TENANTS_FILE" -templates deploy/templates \
  -host-admins "$PLATFORM_HOST_ADMINS" -project
