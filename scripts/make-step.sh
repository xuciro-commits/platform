#!/usr/bin/env bash
set -euo pipefail
idx="$1"
label="$2"
shift 2
exec node "$(dirname "$0")/timing.mjs" run "$idx $label" bash -c "$*"
