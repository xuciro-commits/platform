# Platform Simplification Engineering Report

**Branch:** `arena/cfd7af88-platform` (from `92a7a9d178d2df9d438b1dbc4f6bfbc4151a94f9`)
**Date:** 2026-10-21
**Scope:** `capabilities/server/`, `apps/`, `web/`, `solutions/`, `scripts/`, `contract/`
**Result:** No new deletions executed. Codebase already simplified via ADR-0064 (A–F complete). Remaining simplification candidates are blocked by owner confirmation / data-migration requirements, per standing instructions.

---

## Method

I conducted an exhaustive, static-code evaluation across the whole platform:

- Measured and read every native app server (`apps/{crm,csm,erp,hcm,pms}`) and both `apps/mes/server/*`, `apps/erp/server/*`.
- Compared every `web/packages/*` and `web/apps/*` for duplicate DataTable/EntityForm/Workspace/RecordList/StatusTag/run views/page editors/catalogs.
- Checked `capabilities/server/` host root, `platform/`, `apps/*/`, `internal/*/`, `cmd/*/`, and `legacy_*` replay branches.
- Scanned `scripts/` for orphaned tooling and `web/` for hand-made UI escapes (`escapes.sh` pattern).
- Verified consumers of `@platform/catalog`, `ShopOrders`, legacy replay inputs, `public.go`, `transaction_legacy.go`, `page_unused.go`.
- Confirmed the Go build cannot run in this sandbox (`go.opentelemetry.io/auto/sdk` is not reachable from the allowed hosts), so verification is static; no behavior claim rests on a build.

**Measurements:** production-code additions = 0; deletions = 0. No tests, generated files, or documentation were modified.

---

## KEEP — retained (with reasons)

| Item | Why kept |
|------|----------|
| Native apps `crm, csm, erp, hcm, mes, pms` (server + UI packages) | All six are installed into live solutions (`solutions/hospitality`, `solutions/manufacturing`) and have tenant runs, records, and history. ADR-0053/WorkQueue require owner confirmation + an FDE-built equivalent before retirement. Not "unused/uncommitted." |
| `capabilities/server/legacy_console.go`, `legacy_agent_context.go` | Replay-only historical branches. Must be preserved until no hosted journal still holds the old input bytes (ADR-0062 / SOW historical-read obligation). Data confirmation required before deletion. |
| `capabilities/server/apps/build/transaction_legacy.go` | Parallel action-field migration (`Sets`/`Creates`/`Posts` → `Action.Transaction`). Old-format publications still carry original bytes; read obligation not ended. Needs data confirmation before deletion. |
| `capabilities/server/apps/build/public.go` | Re-export shim for `internal/construction`. Active compat layer; consumers verified in use. |
| `capabilities/server/platform/page_unused.go` (+ test) | Dormant widget-ownership/projection feature but active and tested. Keep. |
| `capabilities/server/internal/host/*` (14 files, 642 lines) | Small role interfaces (agents, host, consumer, build_definitions, …). Distinct concerns, no redundancy. Keep. |
| `web/packages/catalog/` (`query`/`types`/`index`) | Live: consumed by `web/apps/catalog`, `web/apps/workspace` host registry, `web/packages/app`. Not deletable. |
| `scripts/rule-browser.mjs`, `scripts/rule-test.mjs`, `capabilities/server/cmd/rule-browser` | Wired into `web/apps/catalog`, `web/apps/workspace`, `web/packages/app` package.json dev/build/test; WASM output consumed by `web/packages/app/src/runtime/rule-engine.ts`. Live tooling. |
| `capabilities/server/internal/records/`, `internal/acceptance/`, `internal/construction/` | Extracted by ADR-0064 D/E/F; single-ownership replacements for the old flat build.go/definitions.go/records.go. |
| `web/packages/app/src/catalog.ts` vs `web/packages/build/src/catalog.ts` | Different owners (`@platform/app` layer-3 API vs `@pkg/build` layer-5 example studio), different purposes. Not duplication. |
| `web/packages/app/src/pages/pages.tsx` vs `web/packages/build/src/workshop/editor.tsx` vs `web/packages/app/src/runtime/*` | Runtime page model vs design-time page editor vs application runtime. Distinct contexts. |
| `web/apps/workspace/src/host/packages.ts`, `web/packages/platform/src/operations.tsx`, `web/packages/app/src/automation-runs.tsx`, `web/packages/build/src/automate/workflow-runs.tsx` | Host package registry / Host-Console work queue / per-app runs / build workflow runs. Distinct audiences and data. |

## REPLACED — already replaced by ADR-0064 (not repeated here)

ADR-0064 blocks A–F are already implemented in the tree:
- Large flat files split: `apps/build/build.go` 1445→420 lines, `definitions.go` 1338→267, `records.go` 1932→197 (moved into `internal/construction`, `internal/records`, …).
- Competing `accepted_*` state handling consolidated into a single shared change plan; legacy routes/docs retired.
- XML UAF parse moved to the build step (`uaf-gen`); old `internal/host`/`buildapi`/`apps/build/values.go` deleted.
- Kernel `contract/` kept free of domain words; no new permanent duplicate paths; single ownership + reuse first.

These are complete and verified in code — no further action.

## DELETED — nothing new

No production code was deleted in this session. Every candidate deletion was evaluated and either (a) found to be still active / needed, or (b) blocked by owner confirmation or unverified data migration. Per standing instructions I did not manufacture deletions or claim progress that did not occur.

Candidates evaluated and NOT deleted, with the blocking reason:

| Candidate | Blocking reason |
|-----------|-----------------|
| `apps/{crm,csm,erp,hcm,pms}` / `apps/mes` old UI (`ShopOrders`) | All wired into live solutions; ADR-0053 / WorkQueue require owner confirmation of equivalence before retirement. `ShopOrders` still referenced in `mes.go:767`, `mes_test.go`, `web/packages/mes/src/index.tsx`, `solutions/manufacturing/release_candidate_test.go`. |
| Legacy replay branches (`legacy_console.go`, `legacy_agent_context.go`, `transaction_legacy.go`) | Historical journal/read obligation — cannot confirm no journal still holds old inputs without data inspection. |
| `page_unused.go`, `public.go` | Active compat / dormant-but-tested feature; consumers verified in use. |

## RESULT

The platform is already in the post-ADR-0064 simplified state: competing implementations were consolidated, large flat modules split into single-ownership internal packages, obsolete aliases/routes/docs removed, and the kernel contract kept domain-free. I verified there is no additional justified, safely-executable deletion remaining in the current tree. Externally observable behavior, authorization, durable state, historical interpretation, replay, and recovery are fully preserved because nothing was removed.

## REMAINING (not executable without confirmation)

1. **MES old-interface retirement (`ShopOrders`, dedicated forms, render registration)** — requires owner confirmation of equivalence per WorkQueue ("等负责人确认：MES 工单旧界面退出"). Revisit after owner sign-off.
2. **Legacy replay-code deletion** (`legacy_console.go`, `legacy_agent_context.go`, `transaction_legacy.go`) — requires data confirmation that no hosted journal still holds the old input bytes.
3. **Full deployment verification (`make rehearse`)** — noted in ADR-0064 §8 as an incomplete tail item; not runnable in this sandbox (Go module host blocked; no browser environment).

## Recommendation

Do not delete anything further on speculation. Proceed only with the three remaining items above once their preconditions (owner confirmation, data inspection, deploy environment) are satisfied. The current codebase is simpler, working, and behavior-preserving as-is.
