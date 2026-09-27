---
name: new-app
description: Build a new app on the platform, or add entities, actions, flows or translations to one. Use when asked to create an app (an ERP module, a tracker, any business domain) or to extend an app under apps/.
---

# Build an app

Read `docs/Intent.md` → `docs/Platform.md` §10 → `docs/WorkQueue.md`, then `docs/Apps.md`. The executable path today is create app → declare entities → declare actions → declare flows → add translations → run (ADR-0023 D8). ADR-0031's code, visual and AI building paths are a target; use a builder feature only when current code and the guide establish that it exists. The reference apps show the current steps at larger size (`apps/hcm` a lifecycle with approvals, `apps/csm` a flow and an agent, `apps/crm` a protocol consumer, `apps/mes` connectors and a protocol consumed from an ERP).

## 1. Scaffold, then change working code

```sh
cd capabilities/server && go run ./cmd/new-app -id <id> -entity <entity> -title "<Title>" -zh <中文> -app-title "<App>" -app-zh <中文>
cd ../../apps/<id>/server && go test ./...
```

Keep the tests green after every step; extend `TestApp` with each rule you add, and keep `platformserver.CheckReplay` at its end.

## 2. Rules that keep an app an app

- Before each piece of code, name the capability it is, its owner and its canonical path (AGENTS.md rule 11). If the platform has it, use it; if the platform lacks it and a second app would need it too, it belongs to the platform: record it in `docs/WorkQueue.md` and build it there, not in the app. `scripts/escapes.sh` fails on a new escape.
- An app is a probe (AGENTS.md rule 12): name the builder and the operator's complete task before choosing its depth. Prove the journey through modeling, UI, integration/AI where needed, testing, delivery and change. Shared frontend quality and FDE efficiency are platform concerns; unrelated industry feature depth waits in the work queue.

- Import `platformserver/platform` only; `platformserver` only in `_test.go` and `cmd/` (`scripts/boundaries.sh`). Never another app: meet it through a protocol (`protocols/`, ADR-0011).
- Declare, don't hand-write: entity types, fields' meaning (`help`, `synonyms`, `example`), lifecycles, standard actions, flows and agents; the host generates lists, forms, tool schemas, OpenAPI and the catalog from them.
- Every change is an action decided in `Submit`: `ledger.Generated` first, then `ledger.Receive` for the app's own rules. No state outside records and the ledger; what replay cannot rebuild is a bug.
- Every text has Simplified Chinese in `i18n/zh-CN.json` (`TestChinese`) and the UI package's `src/i18n.ts` (AGENTS.md rule 10). Texts the app writes with `fmt` get a pattern: `"Review {id}": "审核 {id}"`.
- The UI composes `@platform/ui` and `@platform/app` (`Records`, `GeneratedForm`, `useHost`); no app-private replacement of shared components (AGENTS.md rule 5). Controlled, typed definitions and visual composition may use the same owners and bindings as code when their canonical runtime exists. Extend that owner when needed; do not build a per-app interpreter or assume arbitrary tenant code execution.
- An app may not change the kernel or the host to fit itself: record the friction in `docs/WorkQueue.md` (AGENTS.md rule 2).

## 3. Run and check

- `go run ./cmd/<id>-server` (tokens `manager`, `member`); with the workspace: `pnpm --dir web/apps/workspace build`, then `-web ../../../web/apps/workspace/dist`.
- `scripts/verify.sh composition` (the app's tests and boundaries) and `scripts/verify.sh web` (UI tests, typechecks, builds, translations and the existing browser routes). The server-only scaffold check does not prove a newly generated UI.
- Walk the intended operator task with realistic data and permissions, including refusal and recovery states. For shared frontend changes, add representative gallery states and verify visual/keyboard behavior. Record exactly what was observed; test passes alone do not prove visual quality or FDE delivery speed.
- Then the close-out skill.
