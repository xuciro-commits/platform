#!/usr/bin/env bash
# Reproduces the Hotel slice flows end to end: timeout/unknown/retry, offline
# pending, capacity conflict with revision, and policy rejection.
set -euo pipefail
cd "$(dirname "$0")"
port=18480
(cd server && go build -o ../.build/pms-server ./cmd/pms-server)
.build/pms-server -addr 127.0.0.1:$port -response-delay 4s -delay-count 1 &
server=$!
trap 'kill $server' EXIT
for _ in $(seq 100); do curl -s -o /dev/null http://127.0.0.1:$port/v1/me && break; sleep 0.1; done
HOTEL_SERVER=http://127.0.0.1:$port cargo test --manifest-path client/src-tauri/Cargo.toml -- --ignored flows
