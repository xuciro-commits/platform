#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
mkdir -p .build/dev
(cd capabilities/server && go build -o ../../.build/dev/platform-server ./cmd/platform-server)
