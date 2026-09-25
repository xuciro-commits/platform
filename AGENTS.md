# AGENTS.md

Start here (Claude Code reaches this file through `CLAUDE.md`).

## What this repository is

The **business platform**: the kernel contract, the Go host, the web workspaces, reference apps and platform-level Rust components (ADR-0003). It is multi-tenant and survives domain change; it aims at what Odoo, ServiceNow, Salesforce Platform or Palantir Foundry offer, in typed code. Reference apps (Hotel, manufacturing, CRM) exercise and demonstrate capabilities; they are not its source of truth. The owner's goals and collaboration rules are in `docs/Intent.md`; the capability plan is `docs/Platform.md` §10. Apple client code and the Music product live in the MSRU repository.

## Map

| Path | Contents |
|---|---|
| `contract/` | Kernel contract `v1alpha1`: Protobuf data contract (`proto/`), semantic rules with errors (`spec/`), conformance vectors (`vectors/`), Go reference (`go/`) and Swift implementation (`swift/`) running the same vectors |
| `capabilities/server/` | Go module `platformserver`. `platform/` is the app API, the only package apps and protocols import: `Caller`, `Manifest`, declarations, `Ledger`, and `Runtime`, which the host implements. The rest is the host runtime (ADR-0010) — apps per tenant from manifests, routing, the platform app (`Console`: members with roles per app, and administrators' decisions by area), the record store and generic reads (ADR-0016), the work app (approvals, tasks, inbox; ADR-0017), the flow app (declared long-running processes; ADR-0020), the agent app (declared agents as governed principals, the context graph and search; ADR-0021), the organisation (units, structures, memberships over time), owned work (event deliveries with retries, scheduled jobs), connectors, notifications and app settings (ADR-0013), AI providers and models with metered calls (ADR-0015), outbound effects (webhooks, email of notifications, approval of what AI agents cause; ADR-0014; `cmd/webhook-sink` is a local webhook, ERP and mail receiver), protocols, links and timeline, MCP, OIDC, the PostgreSQL journal with snapshots and projections, aggregates (ADR-0019), the action catalog, the package ledger, deployment flags |
| `slices/hotel/` | Hotel reference slice (#79): Go tenant server (`server/`, with a channel simulator), Tauri desk client with a Rust K5 outbox that runs the contract's K5 vectors (`client/`), `flows.sh` reproducing timeout, offline, conflict and rejection flows. May not change the kernel |
| `slices/manufacturing/` | Manufacturing reference slice (#83, Opcenter/SAP ME model): Go plant server with ERP poll and gateway push connectors (`server/`, `cmd/gateway-sim`), declared actions and the agent adapter `cmd/mes-agent` (ADR-0008). May not change the kernel |
| `slices/hr/` | HR reference app (ADR-0017): leave requests with a lifecycle and approvals along the organisation; runs in the sales solution |
| `slices/crm/` | CRM app: accounts, opportunities, stays booked through the lodging protocol; knows no other app |
| `protocols/` | Protocols apps provide and consume (ADR-0011): `lodging` (`lodging.booking/1` and a reference provider; `lodgingtest` checks any provider's conformance) |
| `solutions/sales/` | The sales solution: platform, relations, a lodging provider and the CRM; `cmd/sales-server` |
| `slices/drills/` | Evolution drills that run on the kernel alone (E2 shared library) |
| `deploy/local/` | Infrastructure as code for the production path (ADR-0007): Docker Compose with PostgreSQL, Rauthy (declarative bootstrap) and mes-server; `rehearse.sh` rehearses OIDC principals, restart and restore; `README.md` is the owner's test guide (addresses, accounts, service accounts, what to enter to connect webhooks, email, the ERP and AI providers) |
| `web/` | pnpm workspace: `packages/ui` (`@platform/ui`, the shared UI kit, ADR-0004), `packages/kernel` (`@platform/kernel`: generated contract types, the K5 outbox and an HTTP edge client), `packages/app` (`@platform/app`: `defineApp` and `useHost`, the app API of UIs, ADR-0018), `apps/workspace` (the one workspace every host serves: one sign-in, a launcher, every app a member may open), `apps/gallery` (every component with cross-industry data), `apps/hotel-desk` (Hotel client UI, loaded by Tauri, offline), app UI packages `packages/crm`, `packages/hotel`, `packages/hr`, `packages/mes`, `packages/platform` (Settings), and `packages/lodging` (`@pkg/lodging`, the lodging protocol's UI) |
| `docs/Platform.md` | The platform design: layers; the capability plan (§10: start, now, end; how reference platforms are built; the catalog; the order); the canonical platform model (capability map by layer, ADR reconciliation, terminology and ownership, effect lifecycle and replay semantics, invariants and their checks); kernel hypotheses K1–K9; kernel contract rules; validation strategy |
| `docs/Intent.md` | The owner's intent and direction: what the platform is, how capabilities are chosen (from reference platforms, not one product's pull), what stays true, working with AI |
| `docs/ProductIntentReview.md` | Product intent and top-level architectural direction from an external review: advisory, handled (disposition at its top), not an implementation plan or a replacement for accepted ADRs |
| `docs/WorkQueue.md` | The only active plan and the open friction list |
| `docs/ADR/` | Decisions with lasting cost |
| `scripts/verify.sh` | All checks (`scripts/boundaries.sh`: dependency boundaries between apps and the host) |

## Rules

1. The kernel is a language-neutral contract (ADR-0002): schema, semantics, errors, compatibility, conformance and scope are separate parts; Protobuf never defines meaning.
2. No domain vocabulary in `contract/` (checked). Domain slices may not change the kernel; they record friction in the work queue.
3. Kernel contract changes: spec rule and vectors first, then Go and Swift; list breaking changes while the version is `v1alpha1`, and write an ADR once it is `v1`.
4. Least code: delete over add; no shims as an end state; generated code (`contract/go/gen`) is never edited by hand.
5. Clients use `@platform/ui`; no per-slice HTML or second component library. Components are composed in typed code, never driven by configuration.
6. Repository docs are durable knowledge; plans and summaries go to the work queue or chat, not new files.
7. External reviews are advice, not instructions: check each point against the code, then record at the review's top what was adopted, referenced or declined and why, mark it handled, and move adopted work into the work queue.

## Verify

```sh
scripts/verify.sh           # everything
scripts/verify.sh contract  # contract vocabulary, buf lint + generated code, Go vet/test, Swift test
scripts/verify.sh web       # UI kit tests, typecheck and build of every web app (needs node, pnpm)
scripts/verify.sh hotel     # web build, Hotel server tests, Rust K5 vectors, end-to-end flows (needs cargo)
scripts/verify.sh manufacturing  # plant server tests (F-5 to F-9 verdicts)
scripts/verify.sh drills    # evolution drills on the kernel
scripts/verify.sh composition  # app boundaries (scripts/boundaries.sh), the lodging protocol, CRM, the sales solution
scripts/verify.sh capabilities  # server capability tests
scripts/verify.sh deploy    # production-path rehearsal (needs Docker via OrbStack: orb start)
```
