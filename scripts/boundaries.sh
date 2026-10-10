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
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "boundary: $*" >&2; exit 1; }

# Every Go module under apps/ and protocols/, named by its module path.
modules() { for m in "$@"; do [[ ! -f $m/go.mod ]] || echo "$m:$(awk '/^module /{print $2}' "$m/go.mod")"; done; }

# Listing is this check's precondition: a module whose packages cannot be listed
# (missing module cache, inconsistent vendor) must fail with that reason instead
# of shrinking the dependency set the check reads. An empty list is a fact only
# when `go list` says so.
# The listing reads sources, so it must not inherit the caller's build mode:
# modules under apps/ have no vendor directory, and an exported `-mod=vendor`
# would fail every one of them for a reason that is not a boundary.
list() { local dir=$1 out flags; shift
  flags=$(printf '%s' "${GOFLAGS:-}" | tr ' ' '\n' | grep -v '^-mod=' | tr '\n' ' ')
  if ! out=$(cd "$dir" && GOFLAGS="$flags" go list "$@" 2>&1); then
    fail "$dir cannot be listed, so its boundaries cannot be checked:"$'\n'"$out"
  fi; printf '%s\n' "$out"; }
apps=($(modules apps/*/server))   # macOS bash 3.2 has no mapfile
protocols=($(modules protocols/*))
names=$(for x in "${apps[@]}"; do echo "${x#*:}"; done)

for x in "${apps[@]}" "${protocols[@]}"; do
  dir=${x%%:*} self=${x#*:}
  deps=$(list "$dir" -deps ./...)
  for other in $names; do
    [[ $other == "$self" ]] && continue
    grep -qx "$other" <<<"$deps" && fail "$self depends on the app $other"
  done
  hits=$(list "$dir" -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | grep -vE '/cmd/|test:' | grep -E ' platformserver( |$)' || true)
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
hits=$(list capabilities/server -f '{{.ImportPath}}: {{join .Imports " "}}' ./apps/... | grep -E ' platformserver( |$)' || true)
[[ -z $hits ]] || fail "a platform app imports the host runtime, not the app API and internal/host:"$'\n'"$hits"

leaks=$(grep -rnE '\.(Changes|changes)\.' --include="*.go" apps/ protocols/ capabilities/server/ --exclude-dir=platform --exclude-dir=vendor || true)
[[ -z $leaks ]] || fail "code outside platform touches change log directly without ledger lock:"$'\n'"$leaks"

# A glob that finds nothing would pass every rule vacuously.
[[ ${#apps[@]} -gt 0 && ${#protocols[@]} -gt 0 ]] || fail "no app or protocol module found: the check would pass vacuously"

platformapps=$(list capabilities/server ./apps/... | wc -l | tr -d ' ')
echo "boundaries ok: ${#apps[@]} apps, ${#protocols[@]} protocols, $platformapps platform apps as packages"
