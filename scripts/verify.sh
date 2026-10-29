#!/usr/bin/env bash
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

vocabulary() {
  ! grep -rniE '\b(music|track|album|artist|playlist|song|lyric|subsonic|openverse|hotel|room|reservation|guest)s?\b' \
      contract --exclude-dir=.build --exclude-dir=.lake --exclude-dir=gen
}

check_module() {
  local dir=$1; shift
  (cd "$dir" && go build ./... && go vet ./...)
}

contract() {
  step contract-vocabulary vocabulary
  step contract-schema bash -c 'cd contract/proto && export PATH="$(go env GOPATH)/bin:$PATH" && gen() { find ../go/gen -type f -exec shasum {} + | sort; } && before=$(gen) && buf lint && buf generate && { [ "$before" = "$(gen)" ] || { echo "generated code was stale; buf generate updated contract/go/gen"; exit 1; }; }'
  step contract-go check_module contract/go
}

formal() {
  local proof_failures=${#failed[@]}
  step kernel-proofs scripts/formal.sh
  if ((${#failed[@]} == proof_failures)); then
    sed -n '1p' .build/verify/kernel-proofs.log
    cat .build/formal-tools/proof-axioms.log
  fi
}

web_types() {
  step web-types bash -c 'cd web && gen() { find packages/kernel/src/gen -type f -exec shasum {} + | sort; } && before=$(gen) && pnpm --dir packages/kernel generate && { [ "$before" = "$(gen)" ] || { echo "TypeScript contract types were stale; regenerated"; exit 1; }; }'
}

web_check() {
  web_types
  step web bash -c 'cd web && pnpm install --frozen-lockfile && pnpm catalog:check && pnpm -r run typecheck && pnpm -r run build'
}

format() {
  step go-format bash -c 'fmt=$(cd capabilities/server && go env GOROOT)/bin/gofmt; files=$("$fmt" -l $(git ls-files --cached --others --exclude-standard "*.go" | grep -vE "/(gen|vendor)/" | while IFS= read -r path; do [ ! -f "$path" ] || printf "%s\n" "$path"; done)); [ -z "$files" ] || { echo "not gofmt-formatted:"; echo "$files"; exit 1; }'
}

capabilities() {
  step capability-server check_module capabilities/server
}

composition_static() {
  step app-boundaries scripts/boundaries.sh
  step escapes scripts/escapes.sh
}

composition() {
  composition_static
}

(($#)) || { echo "usage: $0 step...   steps: contract formal format capabilities composition composition-static web-types web-check"; exit 2; }
for target in "$@"; do
case "$target" in
  contract) contract ;;
  formal) formal ;;
  web-types) web_types ;;
  web-check) web_check ;;
  capabilities) capabilities ;;
  format) format ;;
  composition) composition ;;
  composition-static) composition_static ;;
  *) echo "unknown step $target"; exit 2 ;;
esac
done

if ((${#failed[@]})); then echo "Failed: ${failed[*]}"; exit 1; fi
echo "All requested checks passed."
