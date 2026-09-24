#!/usr/bin/env bash
# Platform verification. Usage: scripts/verify.sh [contract|capabilities|web|hotel|manufacturing|composition|drills|deploy]   (default: everything)
# The web step needs node and pnpm (brew install node pnpm); deploy needs a running Docker (orb start), curl and jq.
# Needs go, buf and protoc-gen-go (brew install go bufbuild/buf/buf; go install google.golang.org/protobuf/cmd/protoc-gen-go@latest).
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
    failed+=("$name")
  fi
}

# The kernel is domain-neutral: no domain vocabulary in the contract (Platform.md §4).
vocabulary() {
  ! grep -rniE '\b(music|track|album|artist|playlist|song|lyric|subsonic|openverse|hotel|room|reservation|guest)s?\b' \
      contract --exclude-dir=.build --exclude-dir=gen --exclude-dir=.swiftpm
}

contract() {
  step contract-vocabulary vocabulary
  step contract-schema bash -c 'cd contract/proto && export PATH="$(go env GOPATH)/bin:$PATH" && gen() { find ../go/gen -type f -exec shasum {} + | sort; } && before=$(gen) && buf lint && buf generate && { [ "$before" = "$(gen)" ] || { echo "generated code was stale; buf generate updated contract/go/gen"; exit 1; }; }'
  step contract-go bash -c 'cd contract/go && go vet ./... && go test -count=1 ./...'
  step contract-swift swift test --package-path contract/swift
}

web() {
  step web bash -c 'cd web && pnpm install --frozen-lockfile && gen() { find packages/kernel/src/gen -type f -exec shasum {} + | sort; } && before=$(gen) && pnpm --dir packages/kernel generate && { [ "$before" = "$(gen)" ] || { echo "TypeScript contract types were stale; regenerated"; exit 1; }; } && pnpm check'
}

capabilities() {
  step capability-server bash -c 'cd capabilities/server && go vet ./... && go test -count=1 ./...'
}

manufacturing() {
  step manufacturing-server bash -c 'cd slices/manufacturing/server && go vet ./... && go test -count=1 ./...'
}

drills() {
  step drills bash -c 'cd slices/drills && go vet ./... && go test -count=1 ./...'
}

composition() {
  # Apps know no other app; they meet through protocols (ADR-0011).
  step app-independence bash -c '! (cd slices/crm/server && go list -deps ./...) | grep -qx hotel && ! (cd slices/hotel/server && go list -deps ./...) | grep -qx crm'
  step lodging-protocol bash -c 'cd protocols/lodging && go vet ./... && go test -count=1 ./...'
  step crm-server bash -c 'cd slices/crm/server && go vet ./... && go test -count=1 ./...'
  step sales-solution bash -c 'cd solutions/sales && go vet ./... && go test -count=1 ./...'
}

deploy() {
  step deploy-rehearsal deploy/local/rehearse.sh
}

hotel() {
  step hotel-server bash -c 'cd slices/hotel/server && go vet ./... && go test -count=1 ./...'
  step hotel-client-k5 cargo test --manifest-path slices/hotel/client/src-tauri/Cargo.toml
  step hotel-flows slices/hotel/flows.sh
}

case "${1:-all}" in
  contract) contract ;;
  web) web ;;
  hotel) web; hotel ;;
  capabilities) capabilities ;;
  manufacturing) manufacturing ;;
  drills) drills ;;
  composition) composition ;;
  deploy) deploy ;;
  all) contract; capabilities; web; hotel; manufacturing; composition; drills; deploy ;;
  *) echo "usage: $0 [contract|capabilities|web|hotel|manufacturing|composition|drills|deploy]"; exit 2 ;;
esac

if ((${#failed[@]})); then echo "Failed: ${failed[*]}"; exit 1; fi
echo "All requested checks passed."
