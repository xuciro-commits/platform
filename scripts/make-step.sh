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

tmp=$(mktemp) || exit 1
trap 'rm -f "$tmp"' EXIT

printf '  %s[%s] → %s %s%s\n' "$C_DIM" "$(date +%H:%M:%S)" "$idx" "$label" "$C_RESET"
TIMEFORMAT='%3R'
elapsed=$( { time bash -c "$*" >"$tmp" 2>&1; } 2>&1 )
rc=$?
dur=" ${C_DIM}($(date +%H:%M:%S) · ${elapsed}s)${C_RESET}"

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
