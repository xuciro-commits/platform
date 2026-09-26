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
#      internal/host, never the host runtime (ADR-0025 D4).
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

# Every Go module under apps/ and protocols/, named by its module path.
modules() { for m in "$@"; do [[ ! -f $m/go.mod ]] || echo "$m:$(awk '/^module /{print $2}' "$m/go.mod")"; done; }
apps=($(modules apps/*/server))   # macOS bash 3.2 has no mapfile
protocols=($(modules protocols/*))
names=$(for x in "${apps[@]}"; do echo "${x#*:}"; done)

for x in "${apps[@]}" "${protocols[@]}"; do
  dir=${x%%:*} self=${x#*:}
  deps=$(cd "$dir" && go list -deps ./... 2>/dev/null)
  for other in $names; do
    [[ $other == "$self" ]] && continue
    grep -qx "$other" <<<"$deps" && fail "$self depends on the app $other"
  done
  hits=$(cd "$dir" && go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | grep -vE '/cmd/|test:' | grep -E ' platformserver( |$)' || true)
  [[ -z $hits ]] || fail "app code imports the host runtime, not the app API:"$'\n'"$hits"
done
for x in "${apps[@]}"; do
  dir=${x%%:*} id=${x#*:}
  [[ $dir == apps/$id/server ]] || fail "$dir holds the module $id: an app's directory, module and ID share one name"
  for f in "$id.go" "${id}_test.go" i18n/zh-CN.json "cmd/$id-server/main.go"; do
    [[ -f $dir/$f ]] || fail "$dir has no $f (ADR-0025 D2)"
  done
  ui=web/packages/$id
  [[ ! -d $ui ]] || [[ -f $ui/src/index.tsx && -f $ui/src/i18n.ts ]] || fail "$ui has no src/index.tsx and src/i18n.ts"
done
hits=$(cd capabilities/server && go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./apps/... 2>/dev/null | grep -E ' platformserver( |$)' || true)
[[ -z $hits ]] || fail "a platform app imports the host runtime, not the app API and internal/host:"$'\n'"$hits"
platformapps=$(cd capabilities/server && go list ./apps/... 2>/dev/null | wc -l | tr -d ' ')
echo "boundaries ok: ${#apps[@]} apps, ${#protocols[@]} protocols, $platformapps platform apps as packages"
