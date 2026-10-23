#!/usr/bin/env bash
# Builds the common platform host air is watching (the solution is air's first argument after --).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
solution=${1:-${SOLUTION:-hospitality}}
mkdir -p .build/dev
(cd capabilities/server && go build -o "../../.build/dev/$solution-server" ./cmd/platform-server)
