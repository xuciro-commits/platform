#!/usr/bin/env bash
# The named verification steps the Makefile composes (ADR-0081): each writes
# its log to .build/verify/<step>.log and prints one line. Run steps by name:
#   scripts/verify.sh contract formal format capabilities composition web-check mes
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

# The kernel is domain-neutral: no domain vocabulary in the contract (Platform.md §4).
vocabulary() {
  ! grep -rniE '\b(music|track|album|artist|playlist|song|lyric|subsonic|openverse|hotel|room|reservation|guest)s?\b' \
      contract --exclude-dir=.build --exclude-dir=.lake --exclude-dir=gen
}

contract() {
  step contract-vocabulary vocabulary
  step contract-schema bash -c 'cd contract/proto && export PATH="$(go env GOPATH)/bin:$PATH" && gen() { find ../go/gen -type f -exec shasum {} + | sort; } && before=$(gen) && buf lint && buf generate && { [ "$before" = "$(gen)" ] || { echo "generated code was stale; buf generate updated contract/go/gen"; exit 1; }; }'
  step contract-go bash -c 'cd contract/go && go vet ./... && go test -count=1 ./...'
}

formal() {
  local proof_failures=${#failed[@]}
  step kernel-proofs scripts/formal.sh
  if ((${#failed[@]} == proof_failures)); then
    sed -n '1p' .build/verify/kernel-proofs.log
    cat .build/formal-tools/proof-axioms.log
  fi
}

# The TypeScript contract types match the proto contract.
web_types() {
  step web-types bash -c 'cd web && gen() { find packages/kernel/src/gen -type f -exec shasum {} + | sort; } && before=$(gen) && pnpm --dir packages/kernel generate && { [ "$before" = "$(gen)" ] || { echo "TypeScript contract types were stale; regenerated"; exit 1; }; }'
}

web_check() {
  web_types
  step web bash -c 'cd web && pnpm install --frozen-lockfile && pnpm catalog:check && pnpm -r run typecheck && pnpm -r run test && pnpm -r run build'
}

web() {
  local web_failures=${#failed[@]}
  web_check
  # docs/Testing.md's routes in a browser against a development host (web/e2e, F-35; needs Go and Chrome or Playwright's Chromium)
  if ((${#failed[@]} == web_failures)); then
    step web-routes bash -c 'cd web && pnpm --filter @platform/e2e e2e'
  else
    echo 'web-routes skipped: build and component checks must pass first.'
  fi
}

# Go code is gofmt-formatted (generated code aside), new files included.
format() {
  step go-format bash -c 'fmt=$(cd capabilities/server && go env GOROOT)/bin/gofmt; files=$("$fmt" -l $(git ls-files --cached --others --exclude-standard "*.go" | grep -vE "/(gen|vendor)/")); [ -z "$files" ] || { echo "not gofmt-formatted:"; echo "$files"; exit 1; }'
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

mes() {
  step mes-server bash -c 'cd apps/mes/server && go vet ./... && go test -count=1 ./...'
}

# Apps know no other app; they meet through protocols (ADR-0011); and no
# escape hatch appears without being listed.
composition_static() {
  step app-boundaries scripts/boundaries.sh
  step escapes scripts/escapes.sh
}

composition() {
  local dir
  composition_static
  for dir in protocols/*; do
    [[ -f $dir/go.mod ]] && step "$(basename "$dir")-protocol" bash -c "cd $dir && go vet ./... && go test -count=1 ./..."
  done
  # Every other app, including a new one, is checked as soon as it exists.
  for dir in apps/*/server; do
    [[ -f $dir/go.mod && $dir != apps/pms/* && $dir != apps/mes/* ]] || continue
    step "$(basename "$(dirname "$dir")")-server" bash -c "cd $dir && go vet ./... && go test -count=1 ./..."
  done
  for dir in solutions/*; do
    [[ -f $dir/go.mod ]] && step "$(basename "$dir")-solution" bash -c "cd $dir && go vet ./... && go test -count=1 ./..."
  done
}

deploy() {
  step deploy-rehearsal deploy/local/rehearse.sh
}

pms() {
  step pms-server bash -c 'cd apps/pms/server && go vet ./... && go test -count=1 ./...'
  step pms-client-k5 cargo test --manifest-path apps/pms/client/src-tauri/Cargo.toml
  step pms-flows apps/pms/flows.sh
}

(($#)) || { echo "usage: $0 step...   steps: contract formal format capabilities composition composition-static web-types web-check web pms mes deploy"; exit 2; }
for target in "$@"; do
case "$target" in
  contract) contract ;;
  formal) formal ;;
  web-types) web_types ;;
  web-check) web_check ;;
  web) web ;;
  pms) web; pms ;;
  capabilities) capabilities ;;
  format) format ;;
  mes) mes ;;
  composition) composition ;;
  composition-static) composition_static ;;
  deploy) deploy ;;
  *) echo "unknown step $target"; exit 2 ;;
esac
done

if ((${#failed[@]})); then echo "Failed: ${failed[*]}"; exit 1; fi
echo "All requested checks passed."
