# AGENTS.md

Start here (Claude Code reaches this file through `CLAUDE.md`).

## What this repository is

The **business platform**: the kernel contract, the Go host, the web workspaces, reference apps and platform-level Rust components (ADR-0003). It is multi-tenant and survives domain change; it aims at what Odoo, ServiceNow, Salesforce Platform or Palantir Foundry offer, in typed code. The target apps are CRM, MES and ERP; the PMS, HCM and CSM stay as reference apps, and every app carries its industry's name (ADR-0025). They exercise and demonstrate capabilities; they are not its source of truth. The owner's goals and collaboration rules are in `docs/Intent.md`; the capability plan is `docs/Platform.md` §10. The Music product and its Apple client live in the MSRU repository and are no longer a target of the platform (2026-09-26).

## Map

| Path | Contents |
|---|---|
| `contract/` | Kernel contract `v1alpha1`: Protobuf data contract (`proto/`), semantic rules with errors (`spec/`), conformance vectors (`vectors/`), Go reference (`go/`); Rust (the PMS desk's outbox) and TypeScript (`@platform/kernel`) run the K5 vectors |
| `capabilities/server/` | Go module `platformserver`. `platform/` is the app API, the only package apps and protocols import: `Caller`, `Manifest`, declarations, `Ledger`, and `Runtime`, which the host implements. `internal/host` is what the host offers its own apps beyond the app API (roles it detects, ADR-0025 D4), and `apps/<id>` holds the platform apps already moved there (`relations`, `org`). The rest is the host runtime (ADR-0010) — apps per tenant from manifests, routing, the platform app (`Console`: members with roles per app, and administrators' decisions by area), the record store and generic reads (ADR-0016), the work app (approvals, tasks, inbox; ADR-0017), the flow app (declared long-running processes; ADR-0020), the agent app (declared agents as governed principals, the context graph and search, memory, evaluation; ADR-0021), knowledge and A2A (ADR-0022), the organisation (units, structures, memberships over time), owned work (event deliveries with retries, scheduled jobs), connectors, notifications and app settings (ADR-0013), AI providers and models with metered calls (ADR-0015), outbound effects (webhooks, email of notifications, approval of what AI agents cause; ADR-0014; `cmd/webhook-sink` is a local webhook and mail receiver, model server and supplier's agent), protocols, links and timeline, MCP, OIDC, the PostgreSQL journal with snapshots and projections, aggregates (ADR-0019), the action catalog, the package ledger, deployment flags |
| `apps/pms/` | PMS, property management (ADR-0025; #79, OPERA model): room types and reservations from the desk or a channel (`cmd/channel-sim`), the lodging protocol it provides; a Tauri desk client whose Rust K5 outbox runs the contract's K5 vectors (`client/`); `flows.sh` reproducing timeout, offline, conflict and rejection flows |
| `apps/mes/` | MES, manufacturing execution (ADR-0025; #83, Opcenter/SAP ME model): shop orders, SFCs through routings, nonconformances with signed dispositions, downtime derived from a gateway's pushes (`cmd/gateway-sim`), the confirmation flow to an ERP through `production.orders/1`, and the agent adapter `cmd/mes-agent` (ADR-0008) |
| `apps/hcm/` | HCM, human capital management (ADR-0025, ADR-0017): leave requests with a lifecycle and approvals along the organisation |
| `apps/csm/` | CSM, customer service management (ADR-0025, ADR-0021): tickets with a lifecycle, a service-level flow, and a triage agent whose replies are mailed after a person approves them |
| `apps/<id>/` | Every app has one shape (ADR-0025 D2, checked by `scripts/boundaries.sh`): `server/` is the Go module `<id>` with `<id>.go`, `<area>.go`, `<id>_test.go`, `i18n/zh-CN.json` and its development host `cmd/<id>-server`; its UI is `web/packages/<id>`. Each works alone first and takes outside systems' input through its declared actions, inputs and protocols (D3) |
| `apps/crm/` | CRM app: accounts, opportunities, stays booked through the lodging protocol; knows no other app |
| `apps/erp/` | ERP app (ADR-0024): chart of accounts, journal entries posted with gapless numbers, reversal, periods, the trial balance; partners, products at standard cost with components, purchase orders with approval, receipts and bills, stock moves and on hand; production orders it provides to a plant through `production.orders/1` and posts when confirmed; `cmd/erp-server` is its development host |
| `apps/erpadapter/` | The ERP adapter for an ERP outside, such as SAP (ADR-0024 7d): provides `production.orders/1` from the ERP's polled planned orders and sends confirmations to it as effects |
| `protocols/` | Protocols apps provide and consume (ADR-0011): `lodging` (`lodging.booking/1` and a reference provider; `lodgingtest` checks any provider's conformance), `production` (`production.orders/1`: the ERP releases production orders, a plant confirms them) |
| `solutions/hospitality/` | The hospitality solution: platform, relations, the PMS and a second lodging provider, the CRM, HCM and CSM; `cmd/hospitality-server` |
| `solutions/manufacturing/` | The manufacturing solution (ADR-0024): platform, the MES and its books — the ERP, or the ERP adapter — meeting through `production.orders/1`; `cmd/manufacturing-server` (`-erp external` for the adapter) |
| `apps/drills/` | Evolution drills that run on the kernel alone (E2 shared library) |
| `deploy/local/` | Infrastructure as code for the production path (ADR-0007): Docker Compose with PostgreSQL, Rauthy (declarative bootstrap), manufacturing-server and hospitality-server; `rehearse.sh` rehearses OIDC principals, restart and restore; `README.md` is the owner's test guide (addresses, accounts, service accounts, what to enter to connect webhooks, email, the ERP and AI providers) |
| `web/` | pnpm workspace: `packages/ui` (`@platform/ui`, the shared UI kit, ADR-0004), `packages/kernel` (`@platform/kernel`: generated contract types, the K5 outbox and an HTTP edge client), `packages/app` (`@platform/app`: `defineApp` and `useHost`, the app API of UIs, ADR-0018; the assistant, agent runs and global search, ADR-0021), `apps/workspace` (the one workspace every host serves: one sign-in, a launcher, every app a member may open), `apps/gallery` (every component with cross-industry data), `apps/pms-desk` (the PMS desk's UI, loaded by Tauri, offline), one UI package per app `packages/<id>` (`crm`, `csm`, `erp`, `erpadapter`, `hcm`, `mes`, `pms`), `packages/platform` (Settings), and `packages/lodging` (`@pkg/lodging`, the lodging protocol's UI) |
| `i18n/`, `i18n.ts` | Languages (ADR-0023): each Go app embeds `i18n/<language>.json` (the host's own in `capabilities/server/i18n`), each web package registers `src/i18n.ts`; both keyed by the English text, and tests fail on a text without Chinese |
| `docs/Platform.md` | The platform design. §2 the product model: layers, how it fits together, where data lives, the capability map (what exists), terminology, effects and replay, invariants, open ADR promises. §4 kernel hypotheses K1–K9 and contract rules. §8 validation and what the stages taught. §9 risks. §10 where it is going: the reference platforms, their 2026 direction and what we take, what is missing, the order of stages |
| `docs/Apps.md` | Building an app (ADR-0023 D8): create app → declare entities → declare actions → declare flows → add translations → run; `capabilities/server/cmd/new-app` scaffolds one that already runs |
| `docs/Intent.md` | The owner's intent and direction: what the platform is, how capabilities are chosen (from reference platforms, not one product's pull), what stays true, working with AI |
| `docs/WorkQueue.md` | The only active plan and the open friction list |
| `docs/ADR/` | Decisions with lasting cost |
| `web/packages/kernel/src/gen/host.ts` | The host API's TypeScript types (exported as `Api` from `@platform/kernel`), generated from the host's Go types by `capabilities/server/cmd/api-types`; never edited (ADR-0023 D7) |
| `.github/workflows/verify.yml` | CI: `scripts/verify.sh ci`, `web` and `pms` on every push (the Docker rehearsal stays on the owner's Mac; timing bounds off with `PLATFORM_TIMING=0`) |
| `scripts/verify.sh` | All checks (`scripts/boundaries.sh`: dependency boundaries between apps and the host) |
| `.claude/skills/` | Procedures for coding agents in this repository: `architecture-gate` (open a stage with an ADR the owner decides), `close-out` (finish a batch: checks, documents, commit), `new-app` (build or extend an app along docs/Apps.md) |

## Rules

1. The kernel is a language-neutral contract (ADR-0002): schema, semantics, errors, compatibility, conformance and scope are separate parts; Protobuf never defines meaning.
2. No domain vocabulary in `contract/` (checked). Reference apps may not change the kernel; they record friction in the work queue.
3. Kernel contract changes: spec rule and vectors first, then Go, and Rust and TypeScript where the rule reaches the edge; list breaking changes while the version is `v1alpha1`, and write an ADR once it is `v1`.
4. Least code: delete over add; no shims as an end state; generated code (`contract/go/gen`) is never edited by hand.
5. Clients use `@platform/ui`; no per-slice HTML or second component library. Components are composed in typed code, never driven by configuration.
6. Repository docs are durable knowledge; plans and summaries go to the work queue or chat, not new files.
7. External reviews are advice, not instructions: check each point against the code, record what was adopted, referenced or declined and why, move adopted work into the work queue and the canonical docs, then delete the review (git keeps it).
8. A batch is done when its documents are: in the same commit, its ADR's "As built", the capability map and open promises (`docs/Platform.md` §2.4, §2.9), the work queue and the test guide's routes (`deploy/local/README.md`) say what now exists, and `scripts/verify.sh` passed for what it touched (skill `close-out`). Each fact has one home; do not copy status into a second place.
9. Commit messages are one imperative sentence of what changed, with the ADR and work item in parentheses: "Agent memory people see, keep and forget (ADR-0022 3b, #111)".
10. Every text people read is English source with its Simplified Chinese in the same change: declarations through the app's `i18n/zh-CN.json`, UI words through `t()` and the package's `i18n.ts` (ADR-0023). Record data is never translated.

## Verify

```sh
scripts/verify.sh           # everything
scripts/verify.sh contract  # contract vocabulary, buf lint + generated code, Go vet/test
scripts/verify.sh web       # UI kit tests, typecheck and build of every web app (needs node, pnpm)
scripts/verify.sh pms       # web build, PMS server tests, Rust K5 vectors, end-to-end flows (needs cargo)
scripts/verify.sh mes       # MES tests (F-5 to F-9 verdicts)
scripts/verify.sh drills    # evolution drills on the kernel
scripts/verify.sh composition  # app boundaries and layout (scripts/boundaries.sh), every protocol, every other app, every solution
scripts/verify.sh capabilities  # server capability tests
scripts/verify.sh deploy    # production-path rehearsal (needs Docker via OrbStack: orb start)
scripts/verify.sh format    # gofmt over every Go file
scripts/verify.sh ci        # what CI runs on Linux: contract, format, capabilities, mes, composition, drills
```
