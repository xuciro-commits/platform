#!/usr/bin/env bash
# The named verification steps the Makefile composes: each writes
# its log to .build/verify/<step>.log and prints one line. Run steps by name:
#   scripts/verify.sh contract format capabilities composition web-check
# `make check`, `make verify`, `make rehearse` choose the layer; this script is
# the step bodies, not a second entry point.
# Needs go, buf and protoc-gen-go; the web steps node and pnpm; deploy a running
# Docker, curl, jq and Chrome or Playwright's Chromium.
set -uo pipefail
cd "$(dirname "$0")/.."
mkdir -p .build/verify
failed=()

step() {
  local name=$1; shift
  if "$@" >".build/verify/$name.log" 2>&1; then
    printf '%-27s ok\n' "$name"
  else
    printf '%-27s FAILED  (.build/verify/%s.log)\n' "$name" "$name"
    [[ -n ${CI:-} ]] && tail -n 80 ".build/verify/$name.log"
    failed+=("$name")
  fi
}

# The kernel is domain-neutral: no domain vocabulary in the contract (contract/spec).
vocabulary() {
  ! grep -rniE '\b(music|track|album|artist|playlist|song|lyric|subsonic|openverse|hotel|room|reservation|guest)s?\b' \
      contract --exclude-dir=.build --exclude-dir=.lake --exclude-dir=gen
}

check_module() {
  local dir=$1; shift
  # These are functional checks. Allocation sampling becomes expensive across
  # many independent model fixtures and is not used by these checks.
  (cd "$dir" && go vet ./... && GODEBUG="${GODEBUG:+$GODEBUG,}memprofilerate=0" go test -count=1 "$@" ./...)
}

contract() {
  step contract-vocabulary vocabulary
  step contract-schema bash -c 'cd contract/proto && export PATH="$(go env GOPATH)/bin:$PATH" && gen() { find ../go/gen -type f -exec shasum {} + | sort; } && before=$(gen) && buf lint && buf generate && { [ "$before" = "$(gen)" ] || { echo "generated code was stale; buf generate updated contract/go/gen"; exit 1; }; }'
  step contract-go check_module contract/go
}

# The TypeScript contract types match the proto contract.
web_types() {
  step web-types bash -c 'cd web && gen() { find packages/kernel/src/gen -type f -exec shasum {} + | sort; } && before=$(gen) && pnpm --dir packages/kernel generate && { [ "$before" = "$(gen)" ] || { echo "TypeScript contract types were stale; regenerated"; exit 1; }; }'
}

web_check() {
  web_types
  step web bash -c 'cd web && pnpm install --frozen-lockfile && pnpm -r run typecheck'
}

web() {
  web_check
}

# Go code is gofmt-formatted (generated code aside), new files included.
format() {
  step go-format bash -c 'fmt=$(cd capabilities/server && go env GOROOT)/bin/gofmt; files=$("$fmt" -l $(git ls-files --cached --others --exclude-standard "*.go" | grep -vE "/(gen|vendor)/" | while IFS= read -r path; do [ ! -f "$path" ] || printf "%s\n" "$path"; done)); [ -z "$files" ] || { echo "not gofmt-formatted:"; echo "$files"; exit 1; }'
}

capabilities() {
  step capability-server check_module capabilities/server -timeout=45m
}

# No app reaches around the app API.
composition_static() {
  step app-boundaries scripts/boundaries.sh
}

composition() {
  composition_static
}

deploy() {
  step deploy-rehearsal deploy/local/rehearse.sh
}

(($#)) || { echo "usage: $0 step...   steps: contract format capabilities composition composition-static web-types web-check web deploy"; exit 2; }
for target in "$@"; do
case "$target" in
  contract) contract ;;
  web-types) web_types ;;
  web-check) web_check ;;
  web) web ;;
  capabilities) capabilities ;;
  format) format ;;
  composition) composition ;;
  composition-static) composition_static ;;
  deploy) deploy ;;
  *) echo "unknown step $target"; exit 2 ;;
esac
done

if ((${#failed[@]})); then echo "Failed: ${failed[*]}"; exit 1; fi
echo "All requested checks passed."
