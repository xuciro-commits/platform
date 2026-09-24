#!/usr/bin/env bash
# Reproduces the Hotel slice flows end to end: timeout/unknown/retry, offline
# pending, capacity conflict with revision, and policy rejection.
set -euo pipefail
cd "$(dirname "$0")"
port=18480
(cd server && go build -o ../.build/hotel-server ./cmd/hotel-server)
.build/hotel-server -addr 127.0.0.1:$port -response-delay 4s -delay-count 1 &
server=$!
trap 'kill $server' EXIT
sleep 1
HOTEL_SERVER=http://127.0.0.1:$port cargo test --manifest-path client/src-tauri/Cargo.toml -- --ignored flows
