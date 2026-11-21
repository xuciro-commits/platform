#!/usr/bin/env bash
set -uo pipefail

idx="$1"
label="$2"
shift 2

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_GREEN=$'\033[32m'
  C_RED=$'\033[31m'
  C_DIM=$'\033[2m'
  C_BOLD=$'\033[1m'
  C_RESET=$'\033[0m'
else
  C_GREEN="" C_RED="" C_DIM="" C_BOLD="" C_RESET=""
fi

start_ns=$(python3 -c 'import time; print(int(time.monotonic()*1000))' 2>/dev/null || echo 0)
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

bash -c "$*" >"$tmp" 2>&1
rc=$?

end_ns=$(python3 -c 'import time; print(int(time.monotonic()*1000))' 2>/dev/null || echo 0)
if [[ "$start_ns" -gt 0 && "$end_ns" -ge "$start_ns" ]]; then
  ms=$((end_ns - start_ns))
  sec=$(awk -v ms="$ms" 'BEGIN { printf "%.1fs", ms/1000 }')
  dur=" ${C_DIM}(${sec})${C_RESET}"
else
  dur=""
fi

if [[ "$rc" -eq 0 ]]; then
  printf '  %s✓%s %s%s%s %s%s\n' "$C_GREEN" "$C_RESET" "$C_DIM" "$idx" "$C_RESET" "$label" "$dur"
else
  printf '  %s✗%s %s%s%s %s%s%s%s\n' "$C_RED" "$C_RESET" "$C_DIM" "$idx" "$C_RESET" "$C_BOLD" "$label" "$C_RESET" "$dur"
  printf '%s' "$C_DIM"
  printf '  ──────────────────────────────────────────────────\n'
  printf '%s' "$C_RESET"
  sed 's/^/  /' "$tmp"
  printf '%s' "$C_DIM"
  printf '  ──────────────────────────────────────────────────\n'
  printf '%s' "$C_RESET"
  exit "$rc"
fi
