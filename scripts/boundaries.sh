#!/usr/bin/env bash
# Platform owners consume the public API and named private host roles.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

# AST inspection covers all production source, including files
# omitted by current OS/build tags. Import/authorization rules have no
# exceptions; structural debt has an explicit baseline that may only shrink.
(cd capabilities/server && go run ./cmd/check-boundaries -root ../..)

hits=$(cd capabilities/server && go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./apps/... 2>/dev/null | grep -E ' platformserver( |$)' || true)
[[ -z $hits ]] || fail "a platform app imports the host runtime, not the app API and internal/host:"$'\n'"$hits"

leaks=$(grep -rnE '\.(Changes|changes)\.' --include="*.go" capabilities/server/ --exclude-dir=platform --exclude-dir=vendor || true)
[[ -z $leaks ]] || fail "code outside platform touches change log directly without ledger lock:"$'\n'"$leaks"

# probing is a private decision mode, never a tenant-wide switch.
probe=$(sed -n '/^type Tenant struct {$/,/^}$/p' capabilities/server/host.go | grep -E '^[[:space:]]+probing[[:space:]]' || true)
[[ -z $probe ]] || fail "Tenant has a shared probing mode; use platform.Runtime.ProbeDecision"

# caller capabilities belong to named Runtime roles.
anonymous=$(grep -nE 'c\.rt\.\(interface' capabilities/server/platform/*.go | grep -v '_test.go:' || true)
[[ -z $anonymous ]] || fail "Caller probes anonymous Runtime capabilities:"$'\n'"$anonymous"
selection=$(grep -nE 'type (ResultApp|AcceptedActionApp|AcceptedWorker) interface|func .*acceptsGenerated' capabilities/server/platform/*.go capabilities/server/host.go || true)
[[ -z $selection ]] || fail "submission admission has optional legacy selectors:"$'\n'"$selection"

platformapps=$(cd capabilities/server && go list ./apps/... 2>/dev/null | wc -l | tr -d ' ')
echo "boundaries ok: $platformapps platform owners as packages"
