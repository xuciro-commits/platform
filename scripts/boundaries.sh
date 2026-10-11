#!/usr/bin/env bash
# Dependency boundaries across all apps (#103, #104 G1; Platform.md "Platform model"):
#   1. an app never depends on another app; a protocol depends on no app;
#   2. an app's and a protocol's own packages import the app API
#      (platformserver/platform) and never the host runtime (platformserver):
#      the runtime is reachable only through platform.Runtime, which the host
#      hands each caller. Their binaries (cmd/) compose tenants and may import
#      it, and so may test harnesses (packages named *test, like httptest);
#   3. every app has one shape (ADR-0025 D2): apps/<id>/server is the Go module
#      <id> with <id>.go, <id>_test.go, i18n/zh-CN.json and a development host
#      cmd/<id>-server; its UI, when it has one, is web/packages/<id> with
#      src/index.tsx and src/i18n.ts;
#   4. the host's own apps (capabilities/server/apps/*) import the app API and
#      internal/host, never the host runtime (ADR-0025 D4);
#   5. code outside platform never touches .Changes or .changes directly: all
#      reads and decisions go through locked methods (RecordsFor, Receive, etc.).
#
# The rules that read only source (the AST gate, layout, and the grep rules) run
# before the ones that resolve modules, so an environment without the app
# modules' dependencies (offline sandbox, fresh clone) still gets the full
# source-level bound, including ADR-0097's reverse-coupling rules. A module whose
# imports cannot be resolved here is named as unchecked rather than killing the
# script quietly on its way past every rule below it.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

# ADR-0097 W3: AST inspection covers all production source, including files
# omitted by current OS/build tags. No count allowance or migration exception.
# This is the rule that bounds host <-> app coupling in both directions, and it
# needs only the parser, so it runs first.
(cd capabilities/server && go run ./cmd/check-boundaries -root ../..)

# Every Go module under apps/ and protocols/, named by its module path.
modules() { for m in "$@"; do [[ ! -f $m/go.mod ]] || echo "$m:$(awk '/^module /{print $2}' "$m/go.mod")"; done; }
apps=($(modules apps/*/server))   # macOS bash 3.2 has no mapfile
protocols=($(modules protocols/*))

for x in ${apps[@]+"${apps[@]}"}; do
  dir=${x%%:*} id=${x#*:}
  [[ $dir == apps/$id/server ]] || fail "$dir holds the module $id: an app's directory, module and ID share one name"
  for f in "$id.go" "${id}_test.go" i18n/zh-CN.json "cmd/$id-server/main.go"; do
    [[ -f $dir/$f ]] || fail "$dir has no $f (ADR-0025 D2)"
  done
  ui=web/packages/$id
  [[ ! -d $ui ]] || [[ -f $ui/src/index.tsx && -f $ui/src/i18n.ts ]] || fail "$ui has no src/index.tsx and src/i18n.ts"
done

leaks=$(grep -rnE '\.(Changes|changes)\.' --include="*.go" apps/ protocols/ capabilities/server/ --exclude-dir=platform --exclude-dir=vendor || true)
[[ -z $leaks ]] || fail "code outside platform touches change log directly without ledger lock:"$'\n'"$leaks"

# ADR-0097: probing is a private decision mode, never a tenant-wide switch.
probe=$(sed -n '/^type Tenant struct {$/,/^}$/p' capabilities/server/host.go | grep -E '^[[:space:]]+probing[[:space:]]' || true)
[[ -z $probe ]] || fail "Tenant has a shared probing mode; use platform.Runtime.ProbeDecision"

# ADR-0097: caller capabilities belong to named Runtime roles.
anonymous=$(grep -nE 'c\.rt\.\(interface' capabilities/server/platform/*.go | grep -v '_test.go:' || true)
[[ -z $anonymous ]] || fail "Caller probes anonymous Runtime capabilities:"$'\n'"$anonymous"
selection=$(grep -nE 'type (ResultApp|AcceptedActionApp|AcceptedWorker) interface|func .*acceptsGenerated' capabilities/server/platform/*.go capabilities/server/host.go || true)
[[ -z $selection ]] || fail "submission admission has optional legacy selectors:"$'\n'"$selection"

# The transitive rules need each module's resolved imports.
names=$(for x in ${apps[@]+"${apps[@]}"}; do echo "${x#*:}"; done)
unchecked=()
for x in ${apps[@]+"${apps[@]}"} ${protocols[@]+"${protocols[@]}"}; do
  dir=${x%%:*} self=${x#*:}
  if ! deps=$(cd "$dir" && go list -deps ./... 2>/dev/null); then
    unchecked+=("$dir")
    continue
  fi
  for other in $names; do
    [[ $other == "$self" ]] && continue
    grep -qx "$other" <<<"$deps" && fail "$self depends on the app $other"
  done
  hits=$(cd "$dir" && go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | grep -vE '/cmd/|test:' | grep -E ' platformserver( |$)' || true)
  [[ -z $hits ]] || fail "app code imports the host runtime, not the app API:"$'\n'"$hits"
done

if platformView=$(cd capabilities/server && go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./apps/... 2>/dev/null); then
  hits=$(printf '%s\n' "$platformView" | grep -E ' platformserver( |$)' || true)
  [[ -z $hits ]] || fail "a platform app imports the host runtime, not the app API and internal/host:"$'\n'"$hits"
else
  unchecked+=("capabilities/server/apps (platform apps)")
fi

if platformpkgs=$(cd capabilities/server && go list ./apps/... 2>/dev/null); then
  platformapps=$(printf '%s\n' "$platformpkgs" | grep -c .)
else
  platformapps="unresolved here"
fi
if ((${#unchecked[@]})); then
  echo "boundaries: ${#unchecked[@]} of $(( ${#apps[@]} + ${#protocols[@]} )) modules unresolved here (${unchecked[*]}); their app-to-app and app-to-host runtime imports were not followed, direct imports in those files were covered by the AST gate"
fi
echo "boundaries ok: ${#apps[@]} apps, ${#protocols[@]} protocols, $platformapps platform apps as packages"
