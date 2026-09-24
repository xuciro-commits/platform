#!/usr/bin/env bash
# Platform verification. Usage: scripts/verify.sh [contract|hotel]   (default: everything)
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

hotel() {
  step hotel-server bash -c 'cd slices/hotel/server && go vet ./... && go test -count=1 ./...'
  step hotel-client-k5 cargo test --manifest-path slices/hotel/client/src-tauri/Cargo.toml
  step hotel-flows slices/hotel/flows.sh
}

case "${1:-all}" in
  contract) contract ;;
  hotel) hotel ;;
  all) contract; hotel ;;
  *) echo "usage: $0 [contract|hotel]"; exit 2 ;;
esac

if ((${#failed[@]})); then echo "Failed: ${failed[*]}"; exit 1; fi
echo "All requested checks passed."
