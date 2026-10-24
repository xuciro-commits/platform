#!/usr/bin/env bash
# Dependency boundaries inside the platform host (see docs/Architecture.md):
#   1. the runtime imports no concrete platform app; an app reaches the runtime
#      only through the app API (platformserver/platform) and internal/host.
#      Binaries (cmd/) compose tenants and may import the runtime, and so may
#      test harnesses (packages named *test, like httptest);
#   2. a platform app (capabilities/server/apps/<id>) never imports the host
#      runtime or a sibling app;
#   3. code outside platform never touches .Changes or .changes directly: all
#      reads and decisions go through locked methods (RecordsFor, Receive, etc.).
# Business applications are declarative packages in deploy/packages, installed
# through Build; there is no second app tree to scan. The retired external app
# and protocol modules left no directory behind, so this script guards the one
# host module that remains.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

# AST inspection covers all production source, including files
# omitted by current OS/build tags. No count allowance or migration exception.
(cd capabilities/server && go run ./cmd/check-boundaries -root ../..)

# A platform app reaches the runtime through the app API and internal/host, never
# through the host runtime itself; only cmd/ binaries and test harnesses may.
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
echo "boundaries ok: $platformapps platform apps as packages, one host module"
