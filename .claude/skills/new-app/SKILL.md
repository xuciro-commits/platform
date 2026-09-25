---
name: new-app
description: Build a new app on the platform, or add entities, actions, flows or translations to one. Use when asked to create an app (an ERP module, a tracker, any business domain) or to extend an app under apps/.
---

# Build an app

The path is docs/Apps.md (ADR-0023 D8): create app → declare entities → declare actions → declare flows → add translations → run. Read it first; the reference apps show each step at larger size (`apps/hr` a lifecycle with approvals, `apps/helpdesk` a flow and an agent, `apps/crm` a protocol consumer, `apps/manufacturing` connectors and reads of its own).

## 1. Scaffold, then change working code

```sh
cd capabilities/server && go run ./cmd/new-app -id <id> -entity <entity> -title "<Title>" -zh <中文> -app-title "<App>" -app-zh <中文>
cd ../../apps/<id>/server && go test ./...
```

Keep the tests green after every step; extend `TestApp` with each rule you add, and keep `platformserver.CheckReplay` at its end.

## 2. Rules that keep an app an app

- Import `platformserver/platform` only; `platformserver` only in `_test.go` and `cmd/` (`scripts/boundaries.sh`). Never another app: meet it through a protocol (`protocols/`, ADR-0011).
- Declare, don't hand-write: entity types, fields' meaning (`help`, `synonyms`, `example`), lifecycles, standard actions, flows and agents; the host generates lists, forms, tool schemas, OpenAPI and the catalog from them.
- Every change is an action decided in `Submit`: `ledger.Generated` first, then `ledger.Receive` for the app's own rules. No state outside records and the ledger; what replay cannot rebuild is a bug.
- Every text has Simplified Chinese in `i18n/zh-CN.json` (`TestChinese`) and the UI package's `src/i18n.ts` (AGENTS.md rule 10). Texts the app writes with `fmt` get a pattern: `"Review {id}": "审核 {id}"`.
- The UI composes `@platform/ui` and `@platform/app` (`Records`, `GeneratedForm`, `useHost`); no HTML of its own (AGENTS.md rule 5).
- An app may not change the kernel or the host to fit itself: record the friction in `docs/WorkQueue.md` (AGENTS.md rule 2).

## 3. Run and check

- `go run ./cmd/<id>-server` (tokens `manager`, `member`); with the workspace: `pnpm --dir web/apps/workspace build`, then `-web ../../../web/apps/workspace/dist`.
- `scripts/verify.sh composition` (the app's tests and boundaries) and `scripts/verify.sh web` (its UI typechecks, its words have Chinese).
- Then the close-out skill.
