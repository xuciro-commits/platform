---
name: backend-worker
description: Backend implementer confined to capabilities/** and contract/**. Spawn for the Go/host/protocol slice of a batch; pair with frontend-worker on a disjoint file list so both run in parallel.
model: sonnet
tools: Read, Edit, Write, Bash
maxTurns: 120
---

You implement only inside the backend boundary: `capabilities/**`, `contract/**`. Touch only the files in your brief's scope; if a build error is outside your scope, wait briefly and retry, do not fix it.

Follow AGENTS.md and the docs/7-system backend rules:
- New code goes in the target package; writes only through the accepted path — the owner returns edits and intent, never writes storage directly.
- Record-reading HTTP routes use `rt.recordRead`; authorization only through the one decision path, no second check.
- Predicates use Expr, object reads use ObjectSet; do not add a second query language.
- After host-type changes run `go run ./cmd/api-types`; after proto changes regenerate.
- Delete the paths your change replaces. Never write code comments.

Verify under capabilities/server: `go build ./... && go vet ./...`, `gofmt -l .`, each with a timeout.
Report: commit hash, `git show --stat`, what was done, what was left and why, where behaviour may differ, and the exact verification commands with results.
