#!/usr/bin/env bash
# Platform verification. Usage: scripts/verify.sh [contract|capabilities|web|hotel|manufacturing|composition|drills|deploy|format|ci]   (default: everything)
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
    [[ -n ${CI:-} ]] && tail -n 80 ".build/verify/$name.log"
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
  [[ ${VERIFY_SWIFT:-1} == 1 ]] && step contract-swift swift test --package-path contract/swift
}

web() {
  step web bash -c 'cd web && pnpm install --frozen-lockfile && gen() { find packages/kernel/src/gen -type f -exec shasum {} + | sort; } && before=$(gen) && pnpm --dir packages/kernel generate && { [ "$before" = "$(gen)" ] || { echo "TypeScript contract types were stale; regenerated"; exit 1; }; } && pnpm check'
}

# Go code is gofmt-formatted (generated code aside).
format() {
  step go-format bash -c 'fmt=$(cd capabilities/server && go env GOROOT)/bin/gofmt; files=$("$fmt" -l $(git ls-files "*.go" | grep -v /gen/)); [ -z "$files" ] || { echo "not gofmt-formatted:"; echo "$files"; exit 1; }'
}

capabilities() {
  step capability-server bash -c 'cd capabilities/server && go vet ./... && go test -count=1 ./...'
  step app-scaffold scaffold
}

# The scaffold (cmd/new-app) writes an app whose tests pass, in a scratch copy
# of the repository's layout, so docs/Apps.md's first step always works.
scaffold() {
  local root=.build/scaffold
  rm -rf "$root" && mkdir -p "$root" && ln -s ../../contract ../../capabilities "$root"/ &&
    (cd capabilities/server && go run ./cmd/new-app -root "../../$root" -id scaffolded -entity request -web=false) &&
    (cd "$root/apps/scaffolded/server" && go vet ./... && go test -count=1 ./...)
}

manufacturing() {
  step manufacturing-server bash -c 'cd apps/manufacturing/server && go vet ./... && go test -count=1 ./...'
}

drills() {
  step drills bash -c 'cd apps/drills && go vet ./... && go test -count=1 ./...'
}

composition() {
  # Apps know no other app; they meet through protocols (ADR-0011).
  local dir
  step app-boundaries scripts/boundaries.sh
  for dir in protocols/*; do
    [[ -f $dir/go.mod ]] && step "$(basename "$dir")-protocol" bash -c "cd $dir && go vet ./... && go test -count=1 ./..."
  done
  # Every other app, including a new one, is checked as soon as it exists.
  for dir in apps/*/server; do
    [[ -f $dir/go.mod && $dir != apps/hotel/* && $dir != apps/manufacturing/* ]] || continue
    step "$(basename "$(dirname "$dir")")-server" bash -c "cd $dir && go vet ./... && go test -count=1 ./..."
  done
  for dir in solutions/*; do
    [[ -f $dir/go.mod ]] && step "$(basename "$dir")-solution" bash -c "cd $dir && go vet ./... && go test -count=1 ./..."
  done
}

deploy() {
  step deploy-rehearsal deploy/local/rehearse.sh
}

hotel() {
  step hotel-server bash -c 'cd apps/hotel/server && go vet ./... && go test -count=1 ./...'
  step hotel-client-k5 cargo test --manifest-path apps/hotel/client/src-tauri/Cargo.toml
  step hotel-flows apps/hotel/flows.sh
}

case "${1:-all}" in
  contract) contract ;;
  web) web ;;
  hotel) web; hotel ;;
  capabilities) capabilities ;;
  format) format ;;
  manufacturing) manufacturing ;;
  drills) drills ;;
  composition) composition ;;
  deploy) deploy ;;
  all) contract; format; capabilities; web; hotel; manufacturing; composition; drills; deploy ;;
  # What CI runs on Linux: Swift needs macOS and the rehearsal needs Docker, so both stay on the owner's Mac.
  ci) VERIFY_SWIFT=0 contract; format; capabilities; manufacturing; composition; drills ;;
  *) echo "usage: $0 [contract|capabilities|web|hotel|manufacturing|composition|drills|deploy|format|ci]"; exit 2 ;;
esac

if ((${#failed[@]})); then echo "Failed: ${failed[*]}"; exit 1; fi
echo "All requested checks passed."
